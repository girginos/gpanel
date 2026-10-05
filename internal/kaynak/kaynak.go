// Package kaynak: per-domain kaynak kullanim + plan limit ozeti
package kaynak

import (
	"context"
	"database/sql"
	"log"
	"math"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"

	"github.com/go-chi/chi/v5"
)

type Limit struct {
	Kullanim int64 `json:"kullanim"`
	Limit    int64 `json:"limit"` // 0 = sınırsız
}

type Ozet struct {
	AlanAdi  string `json:"alan_adi"`
	SK       string `json:"sk"`
	PlanAdi  string `json:"plan_adi"`
	PHPSurum string `json:"php_surum"`
	IPv4     string `json:"ipv4"`
	SSLAktif bool   `json:"ssl_aktif"`
	SSLBitis string `json:"ssl_bitis,omitempty"`

	// Limit'li metrikler (kullanim / limit)
	DiskMB     Limit `json:"disk_mb"`     // disk_kota_mb plan'dan (kota aktifse XFS'ten gerçek)
	TrafikMB   Limit `json:"trafik_mb"`   // trafik_kota_mb plan'dan
	DBSayisi   Limit `json:"db_sayisi"`   // max_db plan'dan
	FTPSayisi  Limit `json:"ftp_sayisi"`  // max_ftp plan'dan
	EpostaSayi Limit `json:"eposta_sayi"` // max_email plan'dan
	DomainSayi Limit `json:"domain_sayi"` // max_domain plan'dan: hesapta toplam alan adı (ana + ek; alt alan adı sayılmaz)

	// Inode kotası (yalnız XFS user quota AKTİF ise dolu — aksi halde 0)
	InodeKullanim int64 `json:"inode_kullanim"`
	InodeLimit    int64 `json:"inode_limit"`

	// DBMB: alan adının tüm veritabanlarının toplam boyutu (MB, data+index,
	// information_schema). Ölçülemezse (mysql soketi yok, sorgu hatası) alan YOK.
	// Veritabanı yoksa 0 (ölçülmüş sıfır).
	DBMB *float64 `json:"db_mb,omitempty"`

	// EpostaOlculmuyor: eposta_sayi.kullanim gerçek bir sayım DEĞİL (sabit 0).
	// Posta kutuları mail eklentisinin kendi veritabanında (mailserver.virtual_users);
	// panelin DB kullanıcısının orada yetkisi yok. Çekirdeğin eklentiyle konuştuğu
	// yol (soket API'si, bkz. domains/mailsync.go) lisanssız sunucuda "lisans_yok"
	// döner ve /kaynak'a 6 sn'lik bir dış çağrı eklerdi. Ön yüz bu bayrakla çubuğu gizler.
	EpostaOlculmuyor bool `json:"eposta_olculmuyor"`

	// Bonus sayaclar (limit yok)
	DNSKayit    int64 `json:"dns_kayit"`
	CronIs      int64 `json:"cron_is"`
	YedekSayisi int64 `json:"yedek_sayisi"`
	YedekMB     int64 `json:"yedek_mb"`
}

type Handlers struct {
	DB *sql.DB
}

// duMB: home dizinin disk kullanimi MB cinsinden
func duMB(home string) int64 {
	out, err := exec.Command("du", "-sm", home).CombinedOutput()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(parts[0], 10, 64)
	return n
}

// reDBAdi: sorguya gömülecek veritabanı adı allowlist'i (panel <sk>_… / wp_… üretir).
var reDBAdi = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// mysqlKok: root soketinden tek sorgu (-N -B: başlıksız, sekmeyle ayrılmış). Panelin
// DB kullanıcısı yalnız `panel`.* yetkisine sahip. information_schema.tables ona
// kiracı veritabanlarının tablolarını GÖSTERMEZ, toplam hep 0 çıkardı. ListDatabases
// de aynı nedenle root soketini kullanır. Testte geçici veritabanına çevrilir.
var mysqlKok = func(ctx context.Context, sorgu string) ([]byte, error) {
	return exec.CommandContext(ctx, "mysql", "-N", "-B", "-e", sorgu).Output()
}

// dbBoyutMB: verilen veritabanlarının toplam boyutu (MB, 2 ondalık). ok=false →
// ölçülemedi (alan yanıttan çıkar). Boş liste → (0, true).
func dbBoyutMB(ctx context.Context, adlar []string) (float64, bool) {
	if len(adlar) == 0 {
		return 0, true
	}
	var in strings.Builder
	for i, ad := range adlar {
		// Ad sorguya gömülür (CLI parametre bağlamaz). Biçim dışı tek ad bile
		// varsa kısmi toplam vermek yerine ölçülemedi denir.
		if !reDBAdi.MatchString(ad) {
			log.Printf("kaynak: db boyutu ölçülmedi, biçim dışı veritabanı adı %q", ad)
			return 0, false
		}
		if i > 0 {
			in.WriteByte(',')
		}
		in.WriteString("'" + ad + "'")
	}
	ctx, iptal := context.WithTimeout(ctx, 5*time.Second)
	defer iptal()
	out, err := mysqlKok(ctx, "SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema IN ("+in.String()+")")
	if err != nil {
		log.Printf("kaynak: db boyutu sorgusu başarısız: %v", err)
		return 0, false
	}
	bayt, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || bayt < 0 {
		return 0, false
	}
	return math.Round(bayt/(1024*1024)*100) / 100, true
}

func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ctx := r.Context()

	var o Ozet
	var planID sql.NullInt64
	var sslBitis sql.NullString
	var sslA int
	err := h.DB.QueryRowContext(ctx,
		`SELECT d.alan_adi, d.sistem_kullanici, d.php_surum, d.ipv4, d.ssl_aktif,
		        DATE_FORMAT(d.ssl_bitis,'%Y-%m-%d'), d.plan_id
		 FROM domains d WHERE d.id=?`, id).
		Scan(&o.AlanAdi, &o.SK, &o.PHPSurum, &o.IPv4, &sslA, &sslBitis, &planID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	o.SSLAktif = sslA == 1
	if sslBitis.Valid {
		o.SSLBitis = sslBitis.String
	}

	// EFEKTİF limitler (plan ⊕ hostinge özel üst yazım — tek kaynak efektiflimit).
	var diskKota, trafikKota, maxDom, maxDB, maxEmail, maxFTP int64
	o.PlanAdi = "Sınırsız (paket atanmadı)"
	if el, e := efektiflimit.Oku(ctx, h.DB, id); e == nil {
		if planID.Valid && el.PlanVar {
			o.PlanAdi = el.PlanAd
		}
		diskKota = int64(el.Efektif("disk_kota_mb", 0))
		trafikKota = int64(el.Efektif("trafik_kota_mb", 0))
		maxDom = int64(el.Efektif("max_domain", 0))
		maxDB = int64(el.Efektif("max_db", 0))
		maxEmail = int64(el.Efektif("max_email", 0))
		maxFTP = int64(el.Efektif("max_ftp", 0))
	}

	// Disk kullanım
	home := "/home/" + o.SK
	o.DiskMB.Limit = diskKota

	// ─── KOTA ÖNCE, `du` YALNIZCA GERİ DÜŞÜŞ ───────────────────────────────
	//
	// 🔴 ÖLÇÜLMÜŞ İSRAF: önceki sıra `du -sm` ile başlıyordu ve sonucu hemen
	// ardından gelen kota değeriyle EZİYORDU. Yani her istek, sonucu atılacak
	// bir dizin taraması yapıyordu:
	//
	//	du -sm /home/c_test_com     1985 ms
	//	du -sm /home/c_kaan_com     1559 ms
	//	du -sm /home/c_cageb_local   757 ms
	//	quota -us <kiracı>             5 ms   ← aynı bilgi, üstelik daha doğru
	//
	// Domain detay sayfası `/kaynak`'ı İKİ ayrı bileşenden çağırıyor (sağdaki
	// kaynak kartı ve istatistik kutusundaki "Disk alanı"), yani bedel iki kat
	// ödeniyordu. Kullanıcı bunu "sağ bar bekliyor, sayfada yavaşlık var"
	// olarak bildirdi ve haklıydı: kart, sonucu kullanılmayacak bir taramanın
	// bitmesini bekliyordu.
	//
	// Kota AKTİF olduğunda `du`'ya hiç gerek yok — kota çekirdek tarafından
	// zaten sayılır ve dosya (inode) sayısını da verir. `du` yalnız kotanın
	// kapalı olduğu kurulumlarda çalışır; orada da doğru davranış odur.
	kMB, klimMB, kIno, klimIno := kaynaklimit.KotaDurum(o.SK)
	if klimMB > 0 || klimIno > 0 {
		o.InodeKullanim = int64(kIno)
		o.InodeLimit = int64(klimIno)
		if klimMB > 0 {
			o.DiskMB.Limit = int64(klimMB)
		}
		if kMB > 0 {
			o.DiskMB.Kullanim = int64(kMB)
		} else {
			// Kota "0 kullanım" diyorsa ev gerçekten boş olabilir; yine de
			// doğrulanır. Boş bir dizinde `du` zaten anlıktır.
			o.DiskMB.Kullanim = duMB(home)
		}
	} else {
		// Kota kapalı: tek kaynak `du`.
		o.DiskMB.Kullanim = duMB(home)
	}
	_, _ = h.DB.ExecContext(ctx, `UPDATE domains SET boyut_kb=? WHERE id=?`, o.DiskMB.Kullanim*1024, id)

	// Trafik: domains.trafik_kb (KB → MB)
	var trafikKB int64
	_ = h.DB.QueryRowContext(ctx, `SELECT trafik_kb FROM domains WHERE id=?`, id).Scan(&trafikKB)
	o.TrafikMB.Kullanim = trafikKB / 1024
	o.TrafikMB.Limit = trafikKota

	// DB sayısı + toplam boyut. Liste eksik okunduysa boyut verilmez (kısmi toplam
	// gerçek boyut gibi görünürdü).
	dbAdlari := []string{}
	listeTam := false
	if rows, err := h.DB.QueryContext(ctx, `SELECT db_name FROM db_accounts WHERE domain_id=?`, id); err == nil {
		listeTam = true
		for rows.Next() {
			var ad string
			if rows.Scan(&ad) != nil {
				listeTam = false
				continue
			}
			dbAdlari = append(dbAdlari, ad)
		}
		if rows.Err() != nil {
			listeTam = false
		}
		rows.Close()
	}
	o.DBSayisi.Kullanim = int64(len(dbAdlari))
	o.DBSayisi.Limit = maxDB
	if listeTam {
		if mb, ok := dbBoyutMB(ctx, dbAdlari); ok {
			o.DBMB = &mb
		}
	}

	// FTP sayısı
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=?`, id).Scan(&o.FTPSayisi.Kullanim)
	o.FTPSayisi.Limit = maxFTP

	// E-posta: çekirdekte sayım kaynağı yok (bkz. Ozet.EpostaOlculmuyor). 0 bir
	// ölçüm değildir; bayrak ön yüze bunu söyler. Limit plandan gelir.
	o.EpostaSayi.Kullanim = 0
	o.EpostaSayi.Limit = maxEmail
	o.EpostaOlculmuyor = true

	// Domain sayısı: bu zaten 1 — ama abonelik kapsamında subdomain'leri saymak gerek
	// (Plesk modeli: subscription primary + 0 subdomain) — şimdilik kendisi = 1
	// max_domain = hesapta toplam alan adı (ana + EK alan adları); alt alan adı sayılmaz.
	o.DomainSayi.Kullanim = 1
	var ekN int64
	if err := h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ek_alan_adlari WHERE domain_id=?`, id).Scan(&ekN); err == nil {
		o.DomainSayi.Kullanim += ekN
	}
	o.DomainSayi.Limit = maxDom

	// DNS kayıt
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_records WHERE domain_id=?`, id).Scan(&o.DNSKayit)

	// Cron iş sayısı (host'taki user crontab)
	if out, err := exec.Command("crontab", "-u", o.SK, "-l").CombinedOutput(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			s := strings.TrimSpace(ln)
			if s != "" && !strings.HasPrefix(s, "#") {
				o.CronIs++
			}
		}
	}

	// Yedek sayısı + toplam boyut
	// 🔴 Kolon adi `boyut` DEGIL `boyut_b` (migrations/0008_accounts_backups.sql).
	// Yanlis kolon "Unknown column" hatasi veriyor, hata da `_ =` ile yutuluyordu:
	// panodaki yedek sayaclari KALICI 0 kaliyordu. Ayni sinif hata bir daha sessiz
	// kalmasin diye hata artik loglaniyor.
	if qerr := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(boyut_b),0) FROM backups WHERE domain_id=?`, id).
		Scan(&o.YedekSayisi, &o.YedekMB); qerr != nil {
		log.Printf("kaynak: yedek sayaci sorgusu basarisiz (domain=%d): %v", id, qerr)
	}
	o.YedekMB = o.YedekMB / (1024 * 1024) // byte → MB

	httpx.WriteJSON(w, http.StatusOK, o)
}
