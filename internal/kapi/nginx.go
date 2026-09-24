package kapi

// nginx dosyalarını kapı ayarına uyumlayan SAF fonksiyonlar (G/Ç yok → birim test
// edilir). Dosya okuma/yazma, `nginx -t`, geri alma ve reload provisioner'ın işidir.
//
// 🔴 TEK BLOK İLKESİ: _panel.conf'u rol başına ayrı server bloklarına BÖLMÜYORUZ.
// Canlı dosya çalışma anında birçok araç tarafından düzenleniyor (panelhost:
// server_name + default_server, erisim: 127.0.0.1'e çekme, panelhardening:
// X-Gosp-Proxy / limit_conn / pma-redeem, TLS aracı). Bölmek bu eklemeleri
// kaybettirir ve her aracın çok-bloğu öğrenmesini gerektirirdi; panelhardening
// ayrıca `strings.Index` ile yalnız İLK `location /api/`'yi yamalıyor. Onun
// yerine rol portları AYNI bloğa listen olarak eklenir ve kapı `$server_port`
// üzerinden map'le belirlenir.
//
// 🔴 EKLEYİCİ İLKE: uzlaştırma yalnız EKSİK olanı ekler. Operatörün bilinçli
// seçimlerini (erisim'in bir portu 127.0.0.1'e çekmesi / IPv6 satırını yoruma
// alması) ASLA geri almaz. Kaldırma ayrı ve korumalı bir adımdır (GenelListenKaldir).

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// listenRe: etkin listen satırı. Grup 1: girinti, 2: adres, 3: kalan.
var listenRe = regexp.MustCompile(`^(\s*)listen\s+(\S+)(.*)$`)

// erisimYorumRe: erisim'in masaüstü modunda yoruma aldığı listen satırı.
var erisimYorumRe = regexp.MustCompile(`^\s*#\s*\[erisim:masaustu\]\s*listen\s+(\S+)`)

// Dinleme: ayrıştırılmış bir listen satırı.
type Dinleme struct {
	Satir    int  // satır indeksi
	Port     int  // 0 = ayrıştırılamadı
	IPv6     bool // [..] adresi
	Loopback bool // 127.x / ::1 / localhost — yalnız yerel (erisim masaüstü / tünel)
	Yorum    bool // erisim'in yoruma aldığı satır (etkin DEĞİL ama "port bilinçli kapalı")
	// Adresli: belirli bir (joker ve loopback OLMAYAN) adrese bağlı — ör. iç ağ/VPN IP'si.
	// Operatörün bilinçli ağ kısıtlamasıdır; rol kapıları bunu joker listen'la aşmamalı.
	Adresli bool
}

// Genel: dış arayüzlerde dinleyen ETKİN satır mı?
func (d Dinleme) Genel() bool { return !d.Yorum && !d.Loopback && d.Port > 0 }

// adresAyristir: "8443" | "[::]:8443" | "127.0.0.1:9443" | "0.0.0.0:8443" | "[::1]:9443"
func adresAyristir(adr string) (port int, ipv6, loopback bool) {
	adr = strings.TrimSuffix(adr, ";")
	host, portStr := "", adr
	if strings.HasPrefix(adr, "[") {
		ipv6 = true
		if i := strings.Index(adr, "]:"); i >= 0 {
			host, portStr = adr[1:i], adr[i+2:]
		} else {
			return 0, true, false
		}
	} else if i := strings.LastIndex(adr, ":"); i >= 0 {
		host, portStr = adr[:i], adr[i+1:]
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 1 || p > 65535 {
		return 0, ipv6, false
	}
	switch {
	case host == "localhost", host == "::1", strings.HasPrefix(host, "127."):
		loopback = true
	}
	return p, ipv6, loopback
}

// adresBagli: listen adresi belirli bir arayüze mi bağlı? Joker (boş, *, 0.0.0.0, [::])
// ve loopback değilse evet.
func adresBagli(adr string) bool {
	adr = strings.TrimSuffix(adr, ";")
	host := ""
	if strings.HasPrefix(adr, "[") {
		if i := strings.Index(adr, "]:"); i >= 0 {
			host = adr[1:i]
		}
	} else if i := strings.LastIndex(adr, ":"); i >= 0 {
		host = adr[:i]
	}
	switch host {
	case "", "*", "0.0.0.0", "::", "localhost", "::1":
		return false
	}
	return !strings.HasPrefix(host, "127.")
}

// Dinlemeler: dosyadaki tüm listen satırları (etkin + erisim-yorumlu).
func Dinlemeler(satirlar []string) []Dinleme {
	var out []Dinleme
	for i, ln := range satirlar {
		if m := erisimYorumRe.FindStringSubmatch(ln); m != nil {
			p, v6, lb := adresAyristir(m[1])
			out = append(out, Dinleme{Satir: i, Port: p, IPv6: v6, Loopback: lb, Yorum: true})
			continue
		}
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if m := listenRe.FindStringSubmatch(ln); m != nil {
			p, v6, lb := adresAyristir(m[2])
			out = append(out, Dinleme{Satir: i, Port: p, IPv6: v6, Loopback: lb, Adresli: adresBagli(m[2])})
		}
	}
	return out
}

// GenelPortlar: dış arayüzde dinlenen portlar (tekrarsız, dosya sırasıyla).
func GenelPortlar(conf string) []int {
	var out []int
	gorulen := map[int]bool{}
	for _, d := range Dinlemeler(strings.Split(conf, "\n")) {
		if d.Genel() && !gorulen[d.Port] {
			gorulen[d.Port] = true
			out = append(out, d.Port)
		}
	}
	return out
}

// KapiSatiri: X-Real-IP çapasının yanına eklenen başlık satırı (girintisiz). Değer
// kapıyı, isteği kabul eden GERÇEK portu ve paylaşılan proxy gizlisini taşır (bkz.
// kanitliKapiPort) — gizli değişince satır da değişir, Heal yeniden yazar.
func KapiSatiri(gizli string) string {
	return fmt.Sprintf("proxy_set_header %s \"$gp_kapi $server_port %s\";", Baslik, gizli)
}

// realIPCapa: panelhardening'in X-Gosp-Proxy için kullandığı AYNI çapa → kapı
// başlığı tam olarak Go backend'e giden location'lara (/api/ + login) girer.
const realIPCapa = "proxy_set_header X-Real-IP $remote_addr;"

// Secenek: Uzlastir'in ortam bilgisi (dosya/G-Ç okuması provisioner'dadır).
type Secenek struct {
	// Gizli: paylaşılan proxy gizlisi — kapı başlığı değerine gömülür.
	Gizli string
	// AdminLoopback: operatör erisim "masaüstü" modunda (yönetici yüzeyi internete
	// kapalı). Yeni admin kapısı da yalnız 127.0.0.1'de dinler, IPv6 satırı erisim
	// biçiminde yorumlu yazılır: admin portu değişince operatörün bilinçli kapatması
	// sessizce geri alınmasın (firewall'da da açılmaz — genel değildir).
	AdminLoopback bool
}

// Uzlastir: _panel.conf'u ayara uyumlar (EKLEYİCİ):
//  1. her kapı portu (roller + açık eski portlar) için HİÇ satırı yoksa
//     `listen p ssl;` (+ dosyada IPv6 dinleme varsa `listen [::]:p ssl;`) ekler.
//     Rol listen'ları default_server ALMAZ: panelhost'un catchall bloğu o portların
//     varsayılanıdır; iki default_server aynı portta `nginx -t`'yi düşürür.
//  2. her X-Real-IP çapasının hemen ardına kapı başlığını yerleştirir (idempotent).
//
// Çapa ya da gizli yoksa hata döner: başlık enjekte edilmeden zorlama AÇILMAMALI.
func Uzlastir(conf string, a *Ayar, simdi int64, s Secenek) (string, error) {
	if s.Gizli == "" {
		return conf, fmt.Errorf("proxy gizlisi yok — kapı başlığı yazılamaz")
	}
	satirlar := strings.Split(conf, "\n")
	dinlemeler := Dinlemeler(satirlar)
	if len(dinlemeler) == 0 {
		return conf, fmt.Errorf("_panel.conf'ta listen satırı yok")
	}
	var var4, var6 = map[int]bool{}, map[int]bool{}
	ipv6Var := false
	sonListen := -1
	for _, d := range dinlemeler {
		if d.IPv6 {
			var6[d.Port] = true
			// Yalnız ETKİN bir IPv6 dinleme IPv6'nın açık olduğunu kanıtlar; erisim'in
			// yoruma aldığı satır IPv6'yı bilinçli kapatmış olabilir. IPv6'sız çekirdekte
			// `listen [::]:p` `nginx -t`'yi düşürür.
			if !d.Yorum {
				ipv6Var = true
			}
		} else {
			var4[d.Port] = true
		}
		if d.Satir > sonListen {
			sonListen = d.Satir
		}
	}
	girinti := "    "
	if m := listenRe.FindStringSubmatch(satirlar[dinlemeler[0].Satir]); m != nil {
		girinti = m[1]
	}
	var eklenecek []string
	for _, p := range SiraliPortlar(a.PortHaritasi(simdi)) {
		// Bir portun IPv4 ya da IPv6 satırı (etkin, loopback ya da yorumlu) varsa o
		// port "bilinçli yönetiliyor" sayılır; yalnız HİÇ izi olmayan portlar eklenir.
		if var4[p] || var6[p] {
			continue
		}
		if p == a.Admin && s.AdminLoopback {
			eklenecek = append(eklenecek, fmt.Sprintf("%slisten 127.0.0.1:%d ssl; # gp-kapi", girinti, p))
			if ipv6Var {
				eklenecek = append(eklenecek, fmt.Sprintf("%s# [erisim:masaustu] listen [::]:%d ssl; # gp-kapi", girinti, p))
			}
			continue
		}
		eklenecek = append(eklenecek, fmt.Sprintf("%slisten %d ssl; # gp-kapi", girinti, p))
		if ipv6Var {
			eklenecek = append(eklenecek, fmt.Sprintf("%slisten [::]:%d ssl; # gp-kapi", girinti, p))
		}
	}
	if len(eklenecek) > 0 {
		yeni := make([]string, 0, len(satirlar)+len(eklenecek))
		yeni = append(yeni, satirlar[:sonListen+1]...)
		yeni = append(yeni, eklenecek...)
		yeni = append(yeni, satirlar[sonListen+1:]...)
		satirlar = yeni
	}
	sonuc, n := basligiYerlestir(strings.Join(satirlar, "\n"), KapiSatiri(s.Gizli))
	if n == 0 {
		return conf, fmt.Errorf("X-Real-IP çapası yok — kapı başlığı enjekte edilemedi")
	}
	return sonuc, nil
}

// basligiYerlestir: eski kapı başlığı satırlarını siler, her X-Real-IP çapasının
// ardına satir'ı ekler. Kaç çapaya eklendiğini döner.
func basligiYerlestir(conf, satir string) (string, int) {
	var out []string
	n := 0
	for _, ln := range strings.Split(conf, "\n") {
		if strings.Contains(ln, "proxy_set_header "+Baslik+" ") {
			continue
		}
		out = append(out, ln)
		if strings.Contains(ln, realIPCapa) {
			ind := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			out = append(out, ind+satir)
			n++
		}
	}
	return strings.Join(out, "\n"), n
}

// KapiBasliginiSil: kapı modu kapalıyken başlık satırlarını temizler (idempotent).
func KapiBasliginiSil(conf string) string {
	var out []string
	for _, ln := range strings.Split(conf, "\n") {
		if strings.Contains(ln, "proxy_set_header "+Baslik+" ") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// BaslikTamMi: her X-Real-IP çapasının hemen ardında GÜNCEL gizliyle kapı başlığı var
// mı? Zorlama YALNIZ bu true iken açılır — başlıksız ya da eski gizlili bir location
// kalırsa oradaki istekler kapısız gelir; eksik enjeksiyon sessiz kilitlenmedir.
func BaslikTamMi(conf, gizli string) bool {
	if gizli == "" {
		return false
	}
	beklenen := KapiSatiri(gizli)
	satirlar := strings.Split(conf, "\n")
	capa := 0
	for i, ln := range satirlar {
		if !strings.Contains(ln, realIPCapa) {
			continue
		}
		capa++
		if i+1 >= len(satirlar) || strings.TrimSpace(satirlar[i+1]) != beklenen {
			return false
		}
	}
	return capa > 0
}

// GenelListenKaldir: `tut` dışındaki GENEL (etkin, loopback olmayan) listen
// satırlarını siler; loopback ve erisim-yorumlu satırlara DOKUNMAZ. Silinen portları
// (tekrarsız, artan) döner.
//
// 🔴 ÇAĞIRAN SORUMLULUĞU: bunu yalnız admin kapısının GERÇEKTEN dinlediği
// doğrulandıktan sonra çağır — yoksa son çalışan portu kapatıp herkesi kilitlersin.
func GenelListenKaldir(conf string, tut map[int]bool) (string, []int) {
	satirlar := strings.Split(conf, "\n")
	sil := map[int]bool{}
	silinen := map[int]bool{}
	for _, d := range Dinlemeler(satirlar) {
		if d.Genel() && !tut[d.Port] {
			sil[d.Satir] = true
			silinen[d.Port] = true
		}
	}
	if len(sil) == 0 {
		return conf, nil
	}
	out := make([]string, 0, len(satirlar))
	for i, ln := range satirlar {
		if !sil[i] {
			out = append(out, ln)
		}
	}
	portlar := make([]int, 0, len(silinen))
	for p := range silinen {
		portlar = append(portlar, p)
	}
	sort.Ints(portlar)
	return strings.Join(out, "\n"), portlar
}

// YerelPortlar: panel bloğunda YALNIZ loopback'te dinlenen (genel satırı olmayan) ve kapı
// haritasında bulunmayan portlar — erisim masaüstü / SSH tüneli yolu (ör. geçişten önce
// 127.0.0.1'e çekilmiş 9443). Haritada YÖNETİCİ kapısına eşlenir: oraya yalnız sunucunun
// kendisi ve SSH tüneli ulaşır. Eşlenmezse `default bilinmeyen` geçişten sonra tünel
// yolunu sessizce öldürürdü (giriş 404, API 401).
func YerelPortlar(conf string, a *Ayar, simdi int64) []int {
	harita := a.PortHaritasi(simdi)
	genel, yerel := map[int]bool{}, map[int]bool{}
	for _, d := range Dinlemeler(strings.Split(conf, "\n")) {
		switch {
		case d.Port == 0 || d.Yorum:
		case d.Loopback:
			yerel[d.Port] = true
		default:
			genel[d.Port] = true
		}
	}
	var out []int
	for p := range yerel {
		if !genel[p] && harita[p] == "" {
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}

// MapIcerigi: 00-gosp-kapi.conf içeriği. yerel: YerelPortlar (yönetici kapısına eşlenir).
//
// 🔴 a == nil (kapı modu kapalı) iken de $gp_kapi TANIMLI kalır: tanımsız bir nginx
// değişkeni `nginx -t`'yi GLOBAL düşürür — hiçbir domain reload edilemez (bkz.
// provisioner.uygulamaUpgradeMapEnsure). Mod kapalıyken başlık satırları da silinir
// (KapiBasliginiSil); $gp_kapi yalnız atıf kalmışsa `nginx -t` düşmesin diye tanımlıdır.
//
// Zorlama açıkken varsayılan "bilinmeyen": haritada olmayan bir porttan gelen
// (yanlış yapılandırma) istek sessizce "eski" gibi davranmaz, reddedilir.
func MapIcerigi(a *Ayar, simdi int64, yerel []int) string {
	var b strings.Builder
	b.WriteString("# GirginOSPanel — panel kapı haritası (ADR-GP-001). OTOMATİK — elle düzenlemeyin.\n")
	b.WriteString("# Kaynak: " + AyarYolu + " · yönetim: girginospanel-server kapi <durum|uzat|eski-kapat|rol-port>\n")
	b.WriteString("map $server_port $gp_kapi {\n")
	if a == nil {
		b.WriteString("    default \"\";\n")
	} else {
		b.WriteString("    default " + Bilinmeyen + ";\n")
		m := a.PortHaritasi(simdi)
		for _, p := range SiraliPortlar(m) {
			fmt.Fprintf(&b, "    %d %s;\n", p, m[p])
		}
		for _, p := range yerel {
			if m[p] == "" {
				fmt.Fprintf(&b, "    %d %s; # yalnız loopback (SSH tüneli)\n", p, Admin)
			}
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// IPv6Etkin: dosyada ETKİN (yorumsuz) bir IPv6 listen var mı? IPv6'sız çekirdekte
// `listen [::]:p` `nginx -t`'yi düşürür; IPv6 satırı yalnız bu kanıtla eklenir.
func IPv6Etkin(conf string) bool {
	for _, d := range Dinlemeler(strings.Split(conf, "\n")) {
		if d.IPv6 && !d.Yorum {
			return true
		}
	}
	return false
}

// CatchallUzlastir: panelhost'un yakala-hepsi (bilinmeyen Host → 444) bloğunun GENEL
// listen satırlarını panelin genel portlarıyla eşitler. Loopback (erisim masaüstü) ve
// yorumlu satırlara DOKUNMAZ. IPv6 satırı yalnız ipv6 true ise yazılır.
//
// 🔴 NEDEN: catchall eskiden TEK porta (panelin ilk listen'ı) kuruluyordu. Rol
// portlarında catchall olmazsa panel bloğu o portun örtük varsayılanı olur ve
// bilinmeyen Host'lar da panele ulaşır — panelhost'un tüm koruması rol
// portlarında SESSİZCE devre dışı kalırdı.
func CatchallUzlastir(catchall string, portlar []int, ipv6 bool) string {
	satirlar := strings.Split(catchall, "\n")
	genel := map[int]bool{}
	for _, d := range Dinlemeler(satirlar) {
		if d.Genel() {
			genel[d.Satir] = true
		}
	}
	idx := -1
	girinti := "    "
	var out []string
	for i, ln := range satirlar {
		if m := listenRe.FindStringSubmatch(ln); m != nil && !strings.HasPrefix(strings.TrimSpace(ln), "#") {
			if idx < 0 {
				idx = len(out)
				girinti = m[1]
			}
			if genel[i] {
				continue // genel listen'lar atılır, ilk listen konumuna yenileri yazılır
			}
		}
		out = append(out, ln)
	}
	if idx < 0 {
		return catchall // listen'sız catchall: dokunma (beklenmeyen biçim)
	}
	var yeni []string
	for _, p := range portlar {
		yeni = append(yeni, fmt.Sprintf("%slisten %d ssl default_server;", girinti, p))
		if ipv6 {
			yeni = append(yeni, fmt.Sprintf("%slisten [::]:%d ssl default_server;", girinti, p))
		}
	}
	sonuc := make([]string, 0, len(out)+len(yeni))
	sonuc = append(sonuc, out[:idx]...)
	sonuc = append(sonuc, yeni...)
	sonuc = append(sonuc, out[idx:]...)
	return strings.Join(sonuc, "\n")
}

// GenelPanelPortu: panelin HERKESE AÇIK portu — :80→HTTPS yönlendirmesi, uygulama
// subdomain vhost'ları ve webhook adresleri bunu kullanır. Kapı modunda canlıysa
// müşteri kapısı; değilse admin/bayi OLMAYAN ilk genel port (ör. geçişteki 8443).
// Admin/bayi kapısı ASLA dönmez: yönlendirme ya da paylaşılan bir uygulama adresi
// gizli yönetici portunu herkese sızdırırdı. Kapı yoksa ilk genel listen. Yoksa 0.
func GenelPanelPortu(conf string, a *Ayar) int {
	genel := GenelPortlar(conf)
	if a == nil {
		if len(genel) > 0 {
			return genel[0]
		}
		return 0
	}
	for _, p := range genel {
		if p == a.Musteri {
			return p
		}
	}
	for _, p := range genel {
		if p != a.Admin && p != a.Bayi {
			return p
		}
	}
	return 0
}

// acmeYonRe: panelhost'un :80 mini vhost'undaki HTTPS yönlendirmesi (portu grup 2).
var acmeYonRe = regexp.MustCompile(`(return\s+301\s+https://\$server_name:)(\d+)(\$request_uri;)`)

// AcmeYonlendirmeUzlastir: :80 mini vhost'unun (panel hostname'i) HTTPS yönlendirme
// portunu port'a eşitler. Tarayıcıya "http://panel.alan.adi" yazan kullanıcı genel
// panel portuna gider; eski port kapandıktan sonra ölü adrese yönlenmez.
func AcmeYonlendirmeUzlastir(acme string, port int) string {
	if port <= 0 {
		return acme
	}
	return acmeYonRe.ReplaceAllString(acme, "${1}"+strconv.Itoa(port)+"${3}")
}
