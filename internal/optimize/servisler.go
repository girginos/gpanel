package optimize

// 4 servis için mevcut değer okuma + log sinyal + öneri motoru.
//
// MariaDB: /etc/my.cnf.d/girginospanel-tuning.cnf
// nginx:   /etc/nginx/nginx.conf (main)
// httpd:   /etc/httpd/conf.modules.d/00-mpm.conf + /etc/httpd/conf/httpd.conf
// PHP-FPM: /etc/php-fpm.d/www.conf (varsayılan pool) + per-tenant .conf'lar
//
// Her modül:
//   OneriHesapla(sistem) → []Oneri (mevcut/onerilen dahil)
//   LogSinyalTopla()     → []string (son 200-1000 satır bakış)

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

/* ================================================================
   MARIADB
   ================================================================ */

const MariaDBTuningYolu = "/etc/my.cnf.d/girginospanel-tuning.cnf"

func mariadbAnaliz(s *Sistem) ServisAnaliz {
	a := ServisAnaliz{Kod: "mariadb", Ad: "MariaDB", Ikon: "🐬", Durum: ServisDurumOku("mariadb")}
	if !ServisAktif("mariadb") {
		a.NotYok = "mariadb servisi çalışmıyor"
		return a
	}
	// Mevcut değerler — SHOW GLOBAL VARIABLES tercih, fallback dosya oku
	mev := mariadbGlobalVars()
	a.LogSinyal = mariadbLogSinyal()

	// Öneri matrisi (RAM tabanlı)
	bpMB := mariadbBpBoyut(s.RAMToplamMB)
	_ = max(1, min(8, bpMB/1024)) // deprecated (MariaDB 10.5+)
	logMB := clamp((bpMB/3/128)*128, 128, 512)
	threadCache := min(100, s.CPUCekirdek*16)
	ioThreads := clamp(s.CPUCekirdek, 4, 8)
	status := mariadbStatusOku()
	mcHesap := hesaplaMaxConnections(s, mev, status)

	oneriler := []Oneri{
		mariadbOneri("innodb_buffer_pool_size", "InnoDB buffer pool", mev["innodb_buffer_pool_size"], fmt.Sprintf("%dM", bpMB),
			fmt.Sprintf("RAM'in %%%d'si — en kritik cache; disk I/O düşer.", mariadbBpPct(s.RAMToplamMB)), SeviyeKritik),
		mariadbOneri("innodb_log_file_size", "InnoDB redo log", mev["innodb_log_file_size"], fmt.Sprintf("%dM", logMB),
			"Yazma yükü düşer; commit uzun sürmez.", SeviyeOnemli),
		mariadbOneri("innodb_flush_log_at_trx_commit", "Redo commit modu", mev["innodb_flush_log_at_trx_commit"], "2",
			"Performans/durabilite dengesi (1s'de bir fsync).", SeviyeBilgi),
		mariadbOneri("innodb_flush_method", "Flush yöntemi", mev["innodb_flush_method"], "O_DIRECT",
			"Double buffering'i devre dışı — SSD için ideal.", SeviyeBilgi),
		mariadbOneri("innodb_io_capacity", "I/O kapasitesi", mev["innodb_io_capacity"], "1000",
			"SSD/NVMe için background flush hızlanır.", SeviyeBilgi),
		mariadbOneri("innodb_io_capacity_max", "I/O kapasite tavanı", mev["innodb_io_capacity_max"], "2000",
			"Burst yükte flush hızı üst sınırı.", SeviyeBilgi),
		mariadbOneri("innodb_read_io_threads", "Read I/O thread", mev["innodb_read_io_threads"], fmt.Sprintf("%d", ioThreads),
			"CPU çekirdeğine göre paralel okuma.", SeviyeBilgi),
		mariadbOneri("innodb_write_io_threads", "Write I/O thread", mev["innodb_write_io_threads"], fmt.Sprintf("%d", ioThreads),
			"CPU çekirdeğine göre paralel yazma.", SeviyeBilgi),
		mariadbOneri("key_buffer_size", "MyISAM key buffer", mev["key_buffer_size"], "8M",
			"InnoDB kullanıyorsan MyISAM cache gereksiz; RAM tasarrufu.", SeviyeOnemli),
		mariadbOneri("tmp_table_size", "Temp tablo boyutu", mev["tmp_table_size"], "128M",
			"Buyuk sorgular disk temp yerine RAM'de tutulur.", SeviyeBilgi),
		mariadbOneri("max_heap_table_size", "Heap tablo boyutu", mev["max_heap_table_size"], "128M",
			"tmp_table_size ile paralel olmali.", SeviyeBilgi),
		mariadbOneri("innodb_log_buffer_size", "Log buffer", mev["innodb_log_buffer_size"], "32M",
			"Buyuk transactionlar icin stabil deger.", SeviyeBilgi),
		mariadbOneri("query_cache_type", "Query cache", mev["query_cache_type"], "OFF",
			"MariaDB 10.5+ query cache yavaslatir - kapali olmali.", SeviyeBilgi),
		mariadbOneri("max_connections", "Maks bağlantı", mev["max_connections"], mcHesap.Onerilen,
			mcHesap.Gerekce, mcHesap.Seviye),
		mariadbOneri("thread_cache_size", "Thread cache", mev["thread_cache_size"], fmt.Sprintf("%d", threadCache),
			"Bağlantı reuse — CPU tasarrufu.", SeviyeBilgi),
		mariadbOneri("skip_name_resolve", "DNS çözümleme", mev["skip_name_resolve"], "ON",
			"Bağlantı başında DNS lookup atlanır — latency düşer.", SeviyeBilgi),
	}
	// Sadece "değişecek" olanları öner
	for _, o := range oneriler {
		if !mariadbAyni(o.Mevcut, o.Onerilen) {
			a.Oneriler = append(a.Oneriler, o)
		}
	}
	return a
}

func mariadbOneri(param, etiket, mevcut, onerilen, gerekce string, seviye Seviye) Oneri {
	return Oneri{
		ID:       "mariadb:" + param,
		Servis:   "mariadb",
		Param:    param,
		Etiket:   etiket,
		Mevcut:   mevcut,
		Onerilen: onerilen,
		Gerekce:  gerekce,
		Seviye:   seviye,
		Dosya:    MariaDBTuningYolu,
		Etki:     "restart",
	}
}

func mariadbAyni(a, b string) bool {
	// Sayısal + M/G eşdeğerliği: "128M" == "134217728"
	na, ok1 := mariadbSayi(a)
	nb, ok2 := mariadbSayi(b)
	if ok1 && ok2 {
		return na == nb
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func mariadbSayi(s string) (int64, bool) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "ON" || s == "1" || s == "TRUE" {
		return 1, true
	}
	if s == "OFF" || s == "0" || s == "FALSE" {
		return 0, true
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult = 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "M"):
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "G"):
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n * mult, true
}

func mariadbBpPct(ramMB int) int {
	switch {
	case ramMB < 2048:
		return 20
	case ramMB < 4096:
		return 25
	case ramMB < 8192:
		return 40
	default:
		return 50
	}
}
func mariadbBpBoyut(ramMB int) int {
	mb := ramMB * mariadbBpPct(ramMB) / 100
	mb = (mb / 256) * 256
	if mb < 256 {
		mb = 256
	}
	return mb
}

func mariadbGlobalVars() map[string]string {
	out := map[string]string{}
	q := `SHOW GLOBAL VARIABLES WHERE Variable_name IN (
		'innodb_buffer_pool_size','innodb_buffer_pool_instances','innodb_log_file_size',
		'innodb_flush_log_at_trx_commit','innodb_flush_method','innodb_io_capacity','innodb_io_capacity_max',
		'innodb_read_io_threads','innodb_write_io_threads','max_connections',
		'thread_cache_size','skip_name_resolve','key_buffer_size','tmp_table_size','max_heap_table_size','innodb_log_buffer_size','query_cache_type')`
	b, err := exec.Command("mysql", "-N", "-e", q).Output()
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 {
			out[f[0]] = f[1]
		}
	}
	return out
}

func mariadbLogSinyal() []string {
	var sinyal []string
	// Aborted connections + max_connections aşımı → SHOW GLOBAL STATUS
	if b, err := exec.Command("mysql", "-N", "-e",
		"SHOW GLOBAL STATUS WHERE Variable_name IN ('Aborted_connects','Max_used_connections','Slow_queries','Threads_connected','Innodb_buffer_pool_reads')").Output(); err == nil {
		st := map[string]int64{}
		for _, ln := range strings.Split(string(b), "\n") {
			f := strings.Fields(ln)
			if len(f) >= 2 {
				n, _ := strconv.ParseInt(f[1], 10, 64)
				st[f[0]] = n
			}
		}
		if st["Aborted_connects"] > 100 {
			sinyal = append(sinyal, fmt.Sprintf("Aborted_connects: %d — bağlantı sızıntısı veya timeout var", st["Aborted_connects"]))
		}
		if st["Max_used_connections"] > 150 {
			sinyal = append(sinyal, fmt.Sprintf("Max_used_connections: %d — max_connections'a yakın", st["Max_used_connections"]))
		}
		if st["Slow_queries"] > 0 {
			sinyal = append(sinyal, fmt.Sprintf("Slow_queries: %d — slow query log incelemesi öner", st["Slow_queries"]))
		}
		if st["Innodb_buffer_pool_reads"] > 100000 {
			sinyal = append(sinyal, fmt.Sprintf("InnoDB buffer pool reads: %d — buffer pool küçük olabilir", st["Innodb_buffer_pool_reads"]))
		}
	}
	// Error log son 100 satır
	if b, err := exec.Command("tail", "-n", "300", "/var/log/mariadb/mariadb.log").Output(); err == nil {
		errCnt := 0
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.Contains(strings.ToLower(ln), "error") {
				errCnt++
			}
		}
		if errCnt > 5 {
			sinyal = append(sinyal, fmt.Sprintf("mariadb.log son 300 satırda %d hata satırı", errCnt))
		}
	}
	return sinyal
}

// mariadbStatusOku — SHOW GLOBAL STATUS'tan Threads_connected + Max_used_connections vb.
func mariadbStatusOku() map[string]int64 {
	out := map[string]int64{}
	b, err := exec.Command("mysql", "-N", "-e",
		"SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Max_used_connections','Threads_running')").Output()
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			out[f[0]] = n
		}
	}
	return out
}

type mcOneriHesap struct {
	Onerilen string
	Gerekce  string
	Seviye   Seviye
}

// hesaplaMaxConnections — peak + RAM tavanı üzerinden akıllı hesap.
func hesaplaMaxConnections(sys *Sistem, mev map[string]string, status map[string]int64) mcOneriHesap {
	mevN, _ := strconv.Atoi(mev["max_connections"])
	peak := status["Max_used_connections"]
	current := status["Threads_connected"]

	// Per-thread bellek tahmini (MB) — read_buffer + sort_buffer + join_buffer + thread_stack + binlog + tmp
	perThreadMB := 4 // MariaDB defaults ~2-4MB per idle connection
	// Buffer pool + genel overhead için 400MB ayır
	ramReserve := 400
	if bp, ok := mariadbSayi(mev["innodb_buffer_pool_size"]); ok {
		ramReserve += int(bp / (1024 * 1024))
	}
	ramKalanMB := sys.RAMToplamMB - ramReserve
	if ramKalanMB < 100 {
		ramKalanMB = 100
	}
	ramTavan := ramKalanMB / perThreadMB
	if ramTavan > 500 {
		ramTavan = 500
	}

	// Peak * 2.5 (guvenlik payi) + min taban 50
	peakTemelli := int(peak) * 5 / 2
	if peakTemelli < 50 {
		peakTemelli = 50
	}
	oneri := peakTemelli
	if oneri > ramTavan {
		oneri = ramTavan
	}
	// 20-500 arası clamp
	oneri = clamp(oneri, 20, 500)

	seviye := SeviyeBilgi
	// Mevcut çok yüksekse (peak'ın 5 katından fazla) uyar
	if mevN > 0 && peak > 0 && int64(mevN) > peak*5 && int64(mevN) > 200 {
		seviye = SeviyeOnemli
	}
	// Mevcut peak'a çok yakınsa (10 içinde) risk var - artır
	if int64(mevN) > 0 && peak > 0 && peak >= int64(mevN)-10 {
		seviye = SeviyeKritik
	}

	gerekce := fmt.Sprintf("Canli %d bagl., peak %d. Peak x2.5 + RAM tavani (%d) = onerilen. Her connection ~%dMB tuketir.",
		current, peak, ramTavan, perThreadMB)
	return mcOneriHesap{Onerilen: strconv.Itoa(oneri), Gerekce: gerekce, Seviye: seviye}
}

/* ================================================================
   NGINX
   ================================================================ */

const NginxAnaConfYolu = "/etc/nginx/nginx.conf"

func nginxAnaliz(s *Sistem) ServisAnaliz {
	a := ServisAnaliz{Kod: "nginx", Ad: "Nginx", Ikon: "🌐", Durum: ServisDurumOku("nginx")}
	if !ServisAktif("nginx") {
		a.NotYok = "nginx servisi çalışmıyor"
		return a
	}
	mev := nginxMevcutDegerler()
	a.LogSinyal = nginxLogSinyal()

	workerConn := 4096
	if s.RAMToplamMB < 2048 {
		workerConn = 2048
	}
	if s.RAMToplamMB > 8192 {
		workerConn = 8192
	}

	oneriler := []Oneri{
		nginxOneri("worker_processes", "Worker process", mev["worker_processes"], "auto",
			"CPU çekirdek sayısına göre otomatik ayar.", SeviyeOnemli),
		nginxOneri("worker_connections", "Worker connections", mev["worker_connections"], fmt.Sprintf("%d", workerConn),
			"Eş zamanlı bağlantı üst sınırı — RAM bazlı.", SeviyeOnemli),
		nginxOneri("worker_rlimit_nofile", "Worker dosya limiti", mev["worker_rlimit_nofile"], "65535",
			"Yüksek FD kapasitesi.", SeviyeBilgi),
		nginxOneri("keepalive_timeout", "Keepalive timeout", mev["keepalive_timeout"], "30",
			"Uzun tutulan boş bağlantı sınırı düşer.", SeviyeBilgi),
		nginxOneri("gzip", "Gzip", mev["gzip"], "on",
			"Bandwidth tasarrufu.", SeviyeBilgi),
		nginxOneri("client_max_body_size", "Body üst sınır", mev["client_max_body_size"], "128M",
			"Büyük dosya upload'u destekler.", SeviyeBilgi),
		nginxOneri("open_file_cache", "Dosya cache", mev["open_file_cache"], "max=10000 inactive=60s",
			"Statik dosya sunumu hızlanır.", SeviyeBilgi),

		// --- TCP + I/O tuning ---
		nginxOneri("sendfile", "sendfile", mev["sendfile"], "on",
			"Kernel-space dosya gönderimi — statik içerik hızlanır.", SeviyeOnemli),
		nginxOneri("tcp_nopush", "TCP nopush", mev["tcp_nopush"], "on",
			"Paket dolduktan sonra gönder (sendfile ile).", SeviyeBilgi),
		nginxOneri("tcp_nodelay", "TCP nodelay", mev["tcp_nodelay"], "on",
			"Küçük paketler bekletilmez — keepalive için.", SeviyeBilgi),
		nginxOneri("reset_timedout_connection", "Timeout reset", mev["reset_timedout_connection"], "on",
			"Boş bağlantı hafızası hızlı temizlenir.", SeviyeBilgi),
		nginxOneri("keepalive_requests", "Keepalive istek", mev["keepalive_requests"], "1000",
			"Bir bağlantıda maksimum istek — 100'den yüksek.", SeviyeBilgi),

		// --- Güvenlik ---
		nginxOneri("server_tokens", "Server tokens", mev["server_tokens"], "off",
			"nginx sürümü gizlenir — güvenlik.", SeviyeOnemli),

		// --- Timeout ---
		nginxOneri("client_body_timeout", "Body timeout", mev["client_body_timeout"], "12",
			"Slowloris koruma; POST body okuma süresi.", SeviyeBilgi),
		nginxOneri("client_header_timeout", "Header timeout", mev["client_header_timeout"], "12",
			"Slowloris koruma; header okuma süresi.", SeviyeBilgi),
		nginxOneri("send_timeout", "Send timeout", mev["send_timeout"], "10",
			"Client'e yazma zaman aşımı.", SeviyeBilgi),

		// --- Gzip detayları ---
		nginxOneri("gzip_comp_level", "Gzip sıkıştırma seviye", mev["gzip_comp_level"], "5",
			"CPU/bandwidth dengesi — 1 hızlı, 9 küçük.", SeviyeBilgi),
		nginxOneri("gzip_min_length", "Gzip min boyut", mev["gzip_min_length"], "1024",
			"Küçük dosyaları gzip'leme — CPU tasarrufu.", SeviyeBilgi),
		nginxOneri("gzip_vary", "Gzip vary header", mev["gzip_vary"], "on",
			"Cache-friendly — CDN'ler farklı sürümleri ayırt eder.", SeviyeBilgi),
		nginxOneri("gzip_proxied", "Gzip proxied", mev["gzip_proxied"], "any",
			"Proxy arkasında da gzip yap.", SeviyeBilgi),
		nginxOneri("gzip_types", "Gzip MIME tipleri", mev["gzip_types"], "text/plain text/css application/json application/javascript text/xml application/xml image/svg+xml",
			"Hangi content-type'lar gzip'lenecek.", SeviyeOnemli),

		// --- SSL/TLS ---
		nginxOneri("ssl_session_cache", "SSL session cache", mev["ssl_session_cache"], "shared:SSL:10m",
			"TLS handshake maliyeti düşer — 10MB ~40k oturum.", SeviyeOnemli),
		nginxOneri("ssl_session_timeout", "SSL session süresi", mev["ssl_session_timeout"], "1d",
			"Uzun cache — daha az handshake.", SeviyeBilgi),
		nginxOneri("ssl_protocols", "TLS protokolleri", mev["ssl_protocols"], "TLSv1.2 TLSv1.3",
			"Eski TLS'leri kapat — güvenlik + Mozilla modern.", SeviyeKritik),
		nginxOneri("ssl_prefer_server_ciphers", "Server cipher tercihi", mev["ssl_prefer_server_ciphers"], "off",
			"TLS 1.3'te off modern — client seçer.", SeviyeBilgi),
		nginxOneri("ssl_session_tickets", "Session tickets", mev["ssl_session_tickets"], "off",
			"Forward secrecy — tickets kapalı önerilir.", SeviyeBilgi),

		// --- Hash boyutları ---
		nginxOneri("types_hash_max_size", "Types hash", mev["types_hash_max_size"], "2048",
			"MIME type sayısı büyükse uyarı önler.", SeviyeBilgi),
		nginxOneri("server_names_hash_bucket_size", "Server names hash", mev["server_names_hash_bucket_size"], "128",
			"Çok domain varsa gerekli.", SeviyeBilgi),
	}
	for _, o := range oneriler {
		if !mariadbAyni(o.Mevcut, o.Onerilen) {
			a.Oneriler = append(a.Oneriler, o)
		}
	}
	return a
}

func nginxOneri(param, etiket, mevcut, onerilen, gerekce string, seviye Seviye) Oneri {
	return Oneri{
		ID:       "nginx:" + param,
		Servis:   "nginx",
		Param:    param,
		Etiket:   etiket,
		Mevcut:   mevcut,
		Onerilen: onerilen,
		Gerekce:  gerekce,
		Seviye:   seviye,
		// 🔴 Hedef dosya SABIT OLAMAZ. Yedek/geri alma bu alana gore
		// calisiyor: http baglamindaki direktifler artik perf.conf'a
		// yaziliyor, ama Dosya hep nginx.conf gosteriyordu -> perf.conf
		// YEDEKLENMEDEN degistirilirdi ve geri alma onu geri getirmezdi.
		Dosya: NginxHedefDosya(param),
		Etki:  "reload",
	}
}

var nginxDirRe = regexp.MustCompile(`^\s*([a-z_]+)\s+([^;#]+);`)

// nginxOkunanDosyalar — mevcut degerlerin arandigi dosyalar.
// 🔴 Sadece nginx.conf okunuyordu; oysa http baglamindaki direktiflerin
// cogu panelin kendi perf.conf'unda tanimli. Sonuc: kullanici arayuzde
// "Mevcut" alanini BOS goruyordu ve degeri hic degistirmemis gibi
// gorunuyordu — oysa deger vardi, sadece baska dosyadaydi.
func nginxOkunanDosyalar() []string {
	return []string{NginxAnaConfYolu, nginxHTTPConfYolu}
}

func nginxMevcutDegerler() map[string]string {
	out := map[string]string{}
	for _, yol := range nginxOkunanDosyalar() {
		b, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if m := nginxDirRe.FindStringSubmatch(ln); len(m) == 3 {
				k := strings.TrimSpace(m[1])
				v := strings.TrimSpace(m[2])
				// Ilk bulunan kazanir; nginx.conf once okunur.
				if _, ok := out[k]; !ok {
					out[k] = v
				}
			}
		}
	}
	return out
}

func nginxLogSinyal() []string {
	var sinyal []string
	if b, err := exec.Command("tail", "-n", "500", "/var/log/nginx/error.log").Output(); err == nil {
		lines := strings.Split(string(b), "\n")
		var wc, upstream, workerExh int
		for _, ln := range lines {
			low := strings.ToLower(ln)
			switch {
			case strings.Contains(low, "worker_connections are not enough"):
				workerExh++
			case strings.Contains(low, "upstream timed out"), strings.Contains(low, "upstream prematurely closed"):
				upstream++
			case strings.Contains(low, "too many open files"):
				wc++
			}
		}
		if workerExh > 0 {
			sinyal = append(sinyal, fmt.Sprintf("worker_connections yetersiz: %d kez → worker_connections artırılmalı", workerExh))
		}
		if upstream > 5 {
			sinyal = append(sinyal, fmt.Sprintf("upstream timeout/close: %d kez → PHP-FPM/backend yavaş olabilir", upstream))
		}
		if wc > 0 {
			sinyal = append(sinyal, fmt.Sprintf("too many open files: %d kez → worker_rlimit_nofile artırılmalı", wc))
		}
	}
	return sinyal
}

/* ================================================================
   APACHE (httpd MPM)
   ================================================================ */

const ApacheMPMYolu = "/etc/httpd/conf.modules.d/00-mpm.conf"
const ApacheEventConfYolu = "/etc/httpd/conf.d/mpm-tuning.conf"

func apacheAnaliz(s *Sistem) ServisAnaliz {
	a := ServisAnaliz{Kod: "apache", Ad: "Apache (httpd)", Ikon: "🪶", Durum: ServisDurumOku("httpd")}
	if !ServisAktif("httpd") {
		a.NotYok = "httpd servisi çalışmıyor"
		return a
	}
	mev := apacheMevcutMPM()
	a.LogSinyal = apacheLogSinyal()

	// event MPM ölçüsü: RAM MB / ~15MB per worker
	maxReq := (s.RAMToplamMB / 15)
	maxReq = clamp(maxReq, 50, 800)
	threadsPerChild := 25
	serverLimit := maxReq / threadsPerChild
	if serverLimit < 2 {
		serverLimit = 2
	}
	startServers := clamp(s.CPUCekirdek, 2, 4)

	oneriler := []Oneri{
		apacheOneri("StartServers", "Başlangıç sunucu", mev["StartServers"], fmt.Sprintf("%d", startServers),
			"CPU'ya göre ilk child sayısı.", SeviyeBilgi),
		apacheOneri("MinSpareThreads", "Min boş thread", mev["MinSpareThreads"], "25",
			"Ani yük için tampon.", SeviyeBilgi),
		apacheOneri("MaxSpareThreads", "Max boş thread", mev["MaxSpareThreads"], "75",
			"Kaynağı israf etme sınırı.", SeviyeBilgi),
		apacheOneri("ThreadsPerChild", "Child başına thread", mev["ThreadsPerChild"], fmt.Sprintf("%d", threadsPerChild),
			"Standart event MPM değeri.", SeviyeBilgi),
		apacheOneri("MaxRequestWorkers", "Max eş zamanlı istek", mev["MaxRequestWorkers"], fmt.Sprintf("%d", maxReq),
			"RAM/child bellek payı ile hesaplandı.", SeviyeOnemli),
		apacheOneri("ServerLimit", "Server limit", mev["ServerLimit"], fmt.Sprintf("%d", serverLimit),
			"MaxRequestWorkers / ThreadsPerChild.", SeviyeOnemli),
		apacheOneri("MaxConnectionsPerChild", "Child başına istek", mev["MaxConnectionsPerChild"], "10000",
			"Bellek sızıntısı restart barajı.", SeviyeBilgi),
	}
	for _, o := range oneriler {
		if !mariadbAyni(o.Mevcut, o.Onerilen) {
			a.Oneriler = append(a.Oneriler, o)
		}
	}
	return a
}

func apacheOneri(param, etiket, mevcut, onerilen, gerekce string, seviye Seviye) Oneri {
	return Oneri{
		ID:       "apache:" + param,
		Servis:   "apache",
		Param:    param,
		Etiket:   etiket,
		Mevcut:   mevcut,
		Onerilen: onerilen,
		Gerekce:  gerekce,
		Seviye:   seviye,
		Dosya:    ApacheEventConfYolu,
		Etki:     "reload",
	}
}

var apacheDirRe = regexp.MustCompile(`(?i)^\s*(StartServers|MinSpareThreads|MaxSpareThreads|ThreadsPerChild|MaxRequestWorkers|ServerLimit|MaxConnectionsPerChild)\s+(\S+)`)

// apacheMPMVarsayilan — Apache 2.4 event/worker MPM'inin DERLENMIS
// varsayilanlari.
//
// 🔴 Neden gerekli: bu direktiflerin hicbiri AlmaLinux'ta varsayilan
// olarak bir dosyada TANIMLI DEGIL — httpd derlenmis degerlerle calisir.
// Eski kod yalnizca dosyalara bakip bulamayinca "Mevcut" alanini BOS
// birakiyordu: kullanici arayuzde neyi neyle degistirdigini goremiyor,
// ayrica "mevcut == onerilen" filtresi calismadigi icin zaten dogru olan
// degerler bile oneri olarak listeleniyordu (7/7 gereksiz oneri).
var apacheMPMVarsayilan = map[string]string{
	"StartServers":           "3",
	"MinSpareThreads":        "75",
	"MaxSpareThreads":        "250",
	"ThreadsPerChild":        "25",
	"MaxRequestWorkers":      "400",
	"ServerLimit":            "16",
	"MaxConnectionsPerChild": "0",
}

func apacheMevcutMPM() map[string]string {
	out := map[string]string{}
	// Konfig kaynaklari: mpm-tuning.conf, ana httpd.conf ve conf.modules.d
	// (MPM secimi + bazi dagitimlarda tuning orada olur).
	kaynaklar := []string{
		ApacheEventConfYolu,
		"/etc/httpd/conf/httpd.conf",
		ApacheMPMYolu,
	}
	for _, y := range kaynaklar {
		b, err := os.ReadFile(y)
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if m := apacheDirRe.FindStringSubmatch(ln); len(m) == 3 {
				out[m[1]] = m[2]
			}
		}
	}
	// conf.d/*.conf de taranir — operator kendi dosyasina yazmis olabilir.
	if ekler, err := os.ReadDir("/etc/httpd/conf.d"); err == nil {
		for _, e := range ekler {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
				continue
			}
			b, err := os.ReadFile("/etc/httpd/conf.d/" + e.Name())
			if err != nil {
				continue
			}
			for _, ln := range strings.Split(string(b), "\n") {
				t := strings.TrimSpace(ln)
				if t == "" || strings.HasPrefix(t, "#") {
					continue
				}
				if m := apacheDirRe.FindStringSubmatch(ln); len(m) == 3 {
					if _, zaten := out[m[1]]; !zaten {
						out[m[1]] = m[2]
					}
				}
			}
		}
	}
	// Hicbir yerde tanimli olmayanlar icin derlenmis varsayilani kullan.
	for k, v := range apacheMPMVarsayilan {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func apacheLogSinyal() []string {
	var sinyal []string
	if b, err := exec.Command("tail", "-n", "500", "/var/log/httpd/error_log").Output(); err == nil {
		var reached, spawnFail int
		for _, ln := range strings.Split(string(b), "\n") {
			low := strings.ToLower(ln)
			if strings.Contains(low, "reached maxrequestworkers") || strings.Contains(low, "reached maxclients") {
				reached++
			}
			if strings.Contains(low, "cannot fork") || strings.Contains(low, "unable to fork") {
				spawnFail++
			}
		}
		if reached > 0 {
			sinyal = append(sinyal, fmt.Sprintf("MaxRequestWorkers'a %d kez ulaşıldı → artırılmalı", reached))
		}
		if spawnFail > 0 {
			sinyal = append(sinyal, fmt.Sprintf("Process fork başarısız %d kez → RAM/ulimit yetersiz", spawnFail))
		}
	}
	return sinyal
}

/* ================================================================
   PHP-FPM (per-pool)
   ================================================================ */

const PHPFPMPoolDir = "/etc/php-fpm.d"

type FPMPool struct {
	Ad           string
	Dosya        string
	MaxChildren  int
	StartServers int
	MinSpare     int
	MaxSpare     int
	MaxRequests  int
}

func phpfpmAnaliz(s *Sistem) ServisAnaliz {
	a := ServisAnaliz{Kod: "phpfpm", Ad: "PHP-FPM", Ikon: "🐘", Durum: ServisDurumOku("php-fpm")}
	if !ServisAktif("php-fpm") {
		a.NotYok = "php-fpm servisi çalışmıyor"
		return a
	}
	pools := phpfpmPoollar()
	a.LogSinyal = phpfpmLogSinyal(pools)

	// RAM/pool payı — havuz sayısına böl
	if len(pools) == 0 {
		a.NotYok = "aktif pool bulunamadı"
		return a
	}
	perProcMB := 40             // PHP script başına yaklaşık
	toplam := s.RAMToplamMB / 3 // %33'ünü PHP-FPM'e ayır
	perPoolMax := max(5, toplam/(len(pools)*perProcMB))

	for _, p := range pools {
		mc := clamp(perPoolMax, 5, 50)
		ss := max(2, mc/4)
		mns := max(1, mc/8)
		mxs := max(3, mc/2)
		oneriler := []Oneri{
			phpfpmOneri(p.Ad, p.Dosya, "pm.max_children", "Max child", strconv.Itoa(p.MaxChildren), strconv.Itoa(mc),
				"RAM/pool payı / süreç başına ~40MB.", SeviyeOnemli),
			phpfpmOneri(p.Ad, p.Dosya, "pm.start_servers", "Başlangıç", strconv.Itoa(p.StartServers), strconv.Itoa(ss),
				"max_children / 4.", SeviyeBilgi),
			phpfpmOneri(p.Ad, p.Dosya, "pm.min_spare_servers", "Min boş", strconv.Itoa(p.MinSpare), strconv.Itoa(mns),
				"max_children / 8.", SeviyeBilgi),
			phpfpmOneri(p.Ad, p.Dosya, "pm.max_spare_servers", "Max boş", strconv.Itoa(p.MaxSpare), strconv.Itoa(mxs),
				"max_children / 2.", SeviyeBilgi),
			phpfpmOneri(p.Ad, p.Dosya, "pm.max_requests", "İstek/child", strconv.Itoa(p.MaxRequests), "500",
				"Sızıntı önleme (child recycle).", SeviyeBilgi),
		}
		for _, o := range oneriler {
			if o.Mevcut != o.Onerilen && o.Mevcut != "" {
				a.Oneriler = append(a.Oneriler, o)
			}
		}
	}
	// Sadece ilk 30 öneri göster (çok pool varsa scroll bombardımanı olmasın)
	if len(a.Oneriler) > 30 {
		a.Oneriler = a.Oneriler[:30]
	}
	return a
}

func phpfpmOneri(pool, dosya, param, etiket, mevcut, onerilen, gerekce string, seviye Seviye) Oneri {
	return Oneri{
		ID:       fmt.Sprintf("phpfpm:%s:%s", pool, param),
		Servis:   "phpfpm",
		Param:    fmt.Sprintf("[%s] %s", pool, param),
		Etiket:   etiket,
		Mevcut:   mevcut,
		Onerilen: onerilen,
		Gerekce:  gerekce,
		Seviye:   seviye,
		Dosya:    dosya,
		Etki:     "reload",
	}
}

var fpmKeyRe = regexp.MustCompile(`^\s*(pm\.\w+)\s*=\s*(\S+)`)

func phpfpmPoollar() []FPMPool {
	var out []FPMPool
	filepath.WalkDir(PHPFPMPoolDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".conf") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		p := FPMPool{Ad: strings.TrimSuffix(filepath.Base(path), ".conf"), Dosya: path}
		for _, ln := range strings.Split(string(b), "\n") {
			if m := fpmKeyRe.FindStringSubmatch(ln); len(m) == 3 {
				n, _ := strconv.Atoi(m[2])
				switch m[1] {
				case "pm.max_children":
					p.MaxChildren = n
				case "pm.start_servers":
					p.StartServers = n
				case "pm.min_spare_servers":
					p.MinSpare = n
				case "pm.max_spare_servers":
					p.MaxSpare = n
				case "pm.max_requests":
					p.MaxRequests = n
				}
			}
		}
		if p.MaxChildren > 0 {
			out = append(out, p)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Ad < out[j].Ad })
	return out
}

func phpfpmLogSinyal(pools []FPMPool) []string {
	var sinyal []string
	logDosyalari := []string{
		"/var/log/php-fpm/error.log",
		"/var/log/php-fpm/www-error.log",
	}
	var maxReached, seg int
	for _, y := range logDosyalari {
		if b, err := exec.Command("tail", "-n", "500", y).Output(); err == nil {
			for _, ln := range strings.Split(string(b), "\n") {
				low := strings.ToLower(ln)
				if strings.Contains(low, "server reached pm.max_children") {
					maxReached++
				}
				if strings.Contains(low, "segfault") || strings.Contains(low, "signal 11") {
					seg++
				}
			}
		}
	}
	if maxReached > 0 {
		sinyal = append(sinyal, fmt.Sprintf("pm.max_children'a ulaşıldı %d kez → pool başına artır", maxReached))
	}
	if seg > 0 {
		sinyal = append(sinyal, fmt.Sprintf("segfault %d kez → PHP extension sorun olabilir", seg))
	}
	_ = pools
	return sinyal
}

/* ================================================================
   SYSCTL
   ================================================================ */

// SysctlYolu — panel sysctl optimizasyonlarının yazıldığı dosya. `zz` öneki
// KASITLI: `sysctl --system` /etc/sysctl.d/*.conf'u SÖZLÜK SIRASIYLA yükler ve
// SONRAKİ dosya öncekini EZER. VM guest node'larda `99-girginosvm-perf.conf`
// (altyapı) panelin dosyasından SONRA yüklenip ortak parametreleri (swappiness,
// somaxconn, tcp_congestion_control…) eziyordu → kullanıcı "uyguladım ama runtime
// değişmedi" görüyordu. `99-zz-` her zaman EN SON yüklenir → panelin bilinçli
// seçimi tüm diğer 99-* dosyalarını override eder.
const SysctlYolu = "/etc/sysctl.d/99-zz-gpanel-optimize.conf"

// SysctlEskiYolu — override-öncesi ad. Aynı parametreler iki dosyada kalırsa
// çakışma sürer; apply sırasında eski dosya varsa temizlenir (bkz. handler).
const SysctlEskiYolu = "/etc/sysctl.d/99-gpanel-optimize.conf"

func sysctlAnaliz(s *Sistem) ServisAnaliz {
	a := ServisAnaliz{Kod: "sysctl", Ad: "Kernel (sysctl)", Ikon: "⚙️", Durum: &ServisDurum{Aktif: true, Durum: "N/A"}}

	mev := sysctlMevcut([]string{
		"net.core.somaxconn", "net.ipv4.tcp_fin_timeout", "vm.swappiness",
		"fs.file-max", "net.ipv4.tcp_max_syn_backlog", "net.core.netdev_max_backlog",
		// TCP performans
		"net.ipv4.tcp_congestion_control", "net.core.default_qdisc",
		"net.ipv4.tcp_slow_start_after_idle", "net.ipv4.tcp_tw_reuse",
		"net.ipv4.ip_local_port_range", "net.core.rmem_max", "net.core.wmem_max",
		"net.ipv4.tcp_rmem", "net.ipv4.tcp_wmem",
		"net.ipv4.tcp_keepalive_time", "net.ipv4.tcp_max_tw_buckets",
		"net.ipv4.tcp_syncookies", "net.ipv4.tcp_mtu_probing",
		// VM/Memory
		"vm.dirty_ratio", "vm.dirty_background_ratio",
		"vm.overcommit_memory", "vm.vfs_cache_pressure", "vm.max_map_count",
		// Filesystem
		"fs.nr_open", "fs.aio-max-nr", "fs.inotify.max_user_watches",
		// Conntrack + kernel
		"net.netfilter.nf_conntrack_max", "kernel.pid_max",
	})
	// Load sinyal: swap yüksekse swappiness=10 önerisi kritik
	oneriler := []Oneri{
		sysctlOneri("net.core.somaxconn", "TCP accept kuyruğu", mev["net.core.somaxconn"], "4096",
			"Yüksek trafikte SYN drop önler.", SeviyeOnemli),
		sysctlOneri("net.ipv4.tcp_fin_timeout", "TIME_WAIT süresi", mev["net.ipv4.tcp_fin_timeout"], "15",
			"Boş bağlantılar hızlı temizlenir.", SeviyeBilgi),
		sysctlOneri("vm.swappiness", "Swap eğilimi", mev["vm.swappiness"],
			mapSeciciSwap(s.SwapKullanim),
			"Swap kullanımı yüksekse düşür (10), RAM bolsa 20.",
			swapSeviye(s.SwapKullanim)),
		sysctlOneri("fs.file-max", "Sistem FD üst sınırı", mev["fs.file-max"], "500000",
			"Yüksek eş zamanlı dosya/soket.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_max_syn_backlog", "SYN backlog", mev["net.ipv4.tcp_max_syn_backlog"], "4096",
			"SYN flood tampon.", SeviyeBilgi),
		sysctlOneri("net.core.netdev_max_backlog", "Netdev backlog", mev["net.core.netdev_max_backlog"], "5000",
			"NIC → kernel kuyruğu.", SeviyeBilgi),

		// --- TCP performans (yüksek etki) ---
		sysctlOneri("net.ipv4.tcp_congestion_control", "TCP congestion (BBR)", mev["net.ipv4.tcp_congestion_control"], "bbr",
			"Google BBR — throughput %30-90 artar, latency düşer. Kernel modülü açık olmalı.", SeviyeKritik),
		sysctlOneri("net.core.default_qdisc", "Default qdisc", mev["net.core.default_qdisc"], "fq",
			"BBR ile birlikte fair-queue — packet pacing.", SeviyeOnemli),
		sysctlOneri("net.ipv4.tcp_slow_start_after_idle", "Slow-start after idle", mev["net.ipv4.tcp_slow_start_after_idle"], "0",
			"Idle sonrası yavaş başlatmayı kapat — keepalive bağlantılar hızlı kalır.", SeviyeOnemli),
		sysctlOneri("net.ipv4.tcp_tw_reuse", "TIME_WAIT yeniden kullan", mev["net.ipv4.tcp_tw_reuse"], "1",
			"Outgoing bağlantılar TIME_WAIT socket'lerini kullanır — port tükenmez.", SeviyeOnemli),
		sysctlOneri("net.ipv4.ip_local_port_range", "Ephemeral port aralığı", mev["net.ipv4.ip_local_port_range"], "1024	65535",
			"Outgoing bağlantılar için port havuzu genişler.", SeviyeBilgi),
		sysctlOneri("net.core.rmem_max", "Max receive socket buf", mev["net.core.rmem_max"], "16777216",
			"Büyük receive window — yüksek RTT bağlantılarda throughput.", SeviyeBilgi),
		sysctlOneri("net.core.wmem_max", "Max send socket buf", mev["net.core.wmem_max"], "16777216",
			"Büyük send window.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_rmem", "TCP receive buffer", mev["net.ipv4.tcp_rmem"], "4096	87380	16777216",
			"min/default/max — dinamik receive window.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_wmem", "TCP send buffer", mev["net.ipv4.tcp_wmem"], "4096	65536	16777216",
			"min/default/max — dinamik send window.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_keepalive_time", "TCP keepalive başlangıç", mev["net.ipv4.tcp_keepalive_time"], "600",
			"7200s default çok yüksek — ölü bağlantılar tespit edilmiyor.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_max_tw_buckets", "Max TIME_WAIT socket", mev["net.ipv4.tcp_max_tw_buckets"], "2000000",
			"Yüksek trafikte TIME_WAIT stampede önlenir.", SeviyeBilgi),
		sysctlOneri("net.ipv4.tcp_syncookies", "SYN cookies", mev["net.ipv4.tcp_syncookies"], "1",
			"SYN flood koruması — üretimde açık olmalı.", SeviyeOnemli),
		sysctlOneri("net.ipv4.tcp_mtu_probing", "MTU probing", mev["net.ipv4.tcp_mtu_probing"], "1",
			"MTU black-hole sorunlarını otomatik çözer.", SeviyeBilgi),

		// --- VM / Memory ---
		sysctlOneri("vm.dirty_ratio", "Dirty page eşiği", mev["vm.dirty_ratio"], "15",
			"RAM'in %15'i dirty olunca yazma başlar — 20 default'tan güvenli.", SeviyeBilgi),
		sysctlOneri("vm.dirty_background_ratio", "Background writeback", mev["vm.dirty_background_ratio"], "5",
			"Erken async yazma — I/O spike'ları düşer.", SeviyeBilgi),
		sysctlOneri("vm.overcommit_memory", "Overcommit", mev["vm.overcommit_memory"], "1",
			"Redis/MariaDB fork() için gerekli — heuristic'in altında.", SeviyeBilgi),
		sysctlOneri("vm.vfs_cache_pressure", "VFS cache pressure", mev["vm.vfs_cache_pressure"], "50",
			"Inode/dentry cache'i agresif temizleme (100 default), 50 dostluk.", SeviyeBilgi),
		sysctlOneri("vm.max_map_count", "Max VMA", mev["vm.max_map_count"], "262144",
			"ElasticSearch, Redis, MongoDB için gerekli.", SeviyeBilgi),

		// --- Filesystem ---
		sysctlOneri("fs.nr_open", "Process başına FD üst sınırı", mev["fs.nr_open"], "1048576",
			"file-max ile birlikte — process başına ceiling.", SeviyeBilgi),
		sysctlOneri("fs.aio-max-nr", "Async I/O max", mev["fs.aio-max-nr"], "1048576",
			"MariaDB innodb_use_native_aio için gerekli.", SeviyeBilgi),
		sysctlOneri("fs.inotify.max_user_watches", "inotify watch limit", mev["fs.inotify.max_user_watches"], "524288",
			"IDE/CI/file-sync araçları için (VS Code, Watchman).", SeviyeBilgi),

		// --- Conntrack + kernel ---
		sysctlOneri("net.netfilter.nf_conntrack_max", "Conntrack max", mev["net.netfilter.nf_conntrack_max"], "524288",
			"iptables/nft NAT büyük trafik için (küçük sunucu için 262144 yeterli).", SeviyeBilgi),
		sysctlOneri("kernel.pid_max", "Max PID", mev["kernel.pid_max"], "4194304",
			"Çok process/container için gerekli.", SeviyeBilgi),
	}
	for _, o := range oneriler {
		if !mariadbAyni(o.Mevcut, o.Onerilen) {
			a.Oneriler = append(a.Oneriler, o)
		}
	}
	if s.SwapKullanim > 50 {
		a.LogSinyal = append(a.LogSinyal, fmt.Sprintf("Swap %%%d kullanılıyor — RAM baskı altında. vm.swappiness=10 kritik.", s.SwapKullanim))
	}
	return a
}

func sysctlOneri(param, etiket, mevcut, onerilen, gerekce string, seviye Seviye) Oneri {
	return Oneri{
		ID:       "sysctl:" + param,
		Servis:   "sysctl",
		Param:    param,
		Etiket:   etiket,
		Mevcut:   mevcut,
		Onerilen: onerilen,
		Gerekce:  gerekce,
		Seviye:   seviye,
		Dosya:    SysctlYolu,
		Etki:     "reload",
	}
}

func sysctlMevcut(anahtarlar []string) map[string]string {
	out := map[string]string{}
	for _, k := range anahtarlar {
		b, err := exec.Command("sysctl", "-n", k).Output()
		if err == nil {
			out[k] = strings.TrimSpace(string(b))
		}
	}
	return out
}

func mapSeciciSwap(swapPct int) string {
	if swapPct > 30 {
		return "10"
	}
	return "20"
}
func swapSeviye(swapPct int) Seviye {
	if swapPct > 50 {
		return SeviyeKritik
	}
	if swapPct > 20 {
		return SeviyeOnemli
	}
	return SeviyeBilgi
}

/* ================================================================
   Yardımcı
   ================================================================ */

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clamp(v, lo, hi int) int {
	return min(hi, max(lo, v))
}
