package apianahtar

import (
	"errors"
	"sort"
	"strings"
)

// ── İzin modeli: VARSAYILAN REDDET ─────────────────────────────────────────
//
// 🔴 Anahtar YALNIZ aşağıdaki Rotalar tablosundaki uçlara erişir; tabloda olmayan
// her uç (bugün var olan ya da yarın eklenecek) anahtara KAPALIDIR. Yeni bir rota
// bilinçli olarak buraya eklenene kadar anahtarla çağrılamaz.
//
// Etkili yetki = mevcut rol ara yazılımı (AdminOnly/SahipYonetim/MusteriScope…)
// VE anahtar izni. Anahtar izni yalnız DARALTIR: bayi anahtarı kök uçlarına zaten
// AdminOnly ile giremez; kapsam (rid) JWT ile birebir aynı biçimde uygulanır.
//
// Tablo anahtarı chi'nin RoutePattern() çıktısıdır ("METOD /api/v1/desen"),
// tam eşleşme — önek eşleşmesi YOK (TestRoutePatternBicimi ile ölçülür).

// Erisim düzeyi. Yaz, Oku'yu kapsar.
const (
	Oku = "oku"
	Yaz = "yaz"
)

// Rol adları (users.role).
const (
	RolKok  = "admin"
	RolBayi = "reseller"
)

// Grup: izin grubu (arayüz kataloğu bundan üretilir).
type Grup struct {
	Ad       string   `json:"ad"`
	Etiket   string   `json:"etiket"`
	Aciklama string   `json:"aciklama"`
	Roller   []string `json:"roller"`
	// YalnizOku: grupta yazma ucu yok (denetim, bildirim).
	YalnizOku bool `json:"yalniz_oku"`
}

// Kural: bir rotanın gerektirdiği izin.
type Kural struct {
	Grup   string
	Erisim string
}

// Gruplar: v1 grupları. Sıra arayüzdeki sıradır.
var Gruplar = []Grup{
	{Ad: "hesap", Etiket: "Hosting hesapları", Aciklama: "Hosting hesabı listeleme, oluşturma, silme, askı, plan ve PHP değişikliği", Roller: []string{RolKok, RolBayi}},
	{Ad: "parola", Etiket: "Hosting parolaları", Aciklama: "Hosting hesabının FTP ve veritabanı parolasını değiştirme (faturalama otomasyonu ChangePassword)", Roller: []string{RolKok, RolBayi}},
	{Ad: "plan", Etiket: "Hosting planları", Aciklama: "Hosting planlarını listeleme ve yönetme", Roller: []string{RolKok, RolBayi}},
	{Ad: "dns", Etiket: "DNS", Aciklama: "Alan adı DNS kayıtları", Roller: []string{RolKok, RolBayi}},
	{Ad: "veritabani", Etiket: "Veritabanları", Aciklama: "Veritabanı listeleme, oluşturma, silme", Roller: []string{RolKok, RolBayi}},
	{Ad: "ssl", Etiket: "SSL", Aciklama: "SSL sertifikası durumu, alma ve kaldırma", Roller: []string{RolKok, RolBayi}},
	{Ad: "yedek", Etiket: "Yedekler", Aciklama: "Hosting yedeği alma, listeleme, geri yükleme, zamanlama", Roller: []string{RolKok, RolBayi}},
	{Ad: "bayi", Etiket: "Bayiler", Aciklama: "Bayi hesapları ve bayi paketleri (yalnız kök)", Roller: []string{RolKok}},
	{Ad: "denetim", Etiket: "Denetim kaydı", Aciklama: "Denetim kaydını okuma (bayi yalnız kendi kayıtlarını görür)", Roller: []string{RolKok, RolBayi}, YalnizOku: true},
	{Ad: "bildirim", Etiket: "Bildirimler", Aciklama: "Bildirimleri okuma", Roller: []string{RolKok, RolBayi}, YalnizOku: true},
}

const p = "/api/v1"

// Rotalar: anahtara AÇIK uçların TEK kaynağı. Değişiklik testdata altın dosyasında
// diff olarak görünür (TestAnahtarUcListesiAltin).
var Rotalar = map[string]Kural{
	// hesap
	"GET " + p + "/domains":                  {"hesap", Oku},
	"GET " + p + "/domains/{id}":             {"hesap", Oku},
	"GET " + p + "/subdomains":               {"hesap", Oku},
	"GET " + p + "/reseller/ozet":            {"hesap", Oku},
	"GET " + p + "/domains/{id}/kaynak":      {"hesap", Oku},
	"GET " + p + "/domains/{id}/istatistik":  {"hesap", Oku},
	"POST " + p + "/domains":                 {"hesap", Yaz},
	"DELETE " + p + "/domains/{id}":          {"hesap", Yaz},
	"POST " + p + "/domains/toplu/durum":     {"hesap", Yaz},
	"PUT " + p + "/domains/{id}/plan":        {"hesap", Yaz},
	"POST " + p + "/domains/{id}/askiya-al":  {"hesap", Yaz},
	"POST " + p + "/domains/{id}/askidan-al": {"hesap", Yaz},
	"PUT " + p + "/domains/{id}/php":         {"hesap", Yaz},
	// parola (hosting hesabının kaynak parolaları — anahtar SAHİBİNİN kimliği DEĞİL)
	"PUT " + p + "/domains/{id}/ftp/password": {"parola", Yaz},
	"PUT " + p + "/databases/{dbid}/password": {"parola", Yaz},
	// plan
	"GET " + p + "/plans":              {"plan", Oku},
	"GET " + p + "/plans/{id}":         {"plan", Oku},
	"GET " + p + "/plans/{id}/domains": {"plan", Oku},
	"POST " + p + "/plans":             {"plan", Yaz},
	"PUT " + p + "/plans/{id}":         {"plan", Yaz},
	"DELETE " + p + "/plans/{id}":      {"plan", Yaz},
	// dns
	"GET " + p + "/domains/{id}/dns":              {"dns", Oku},
	"GET " + p + "/domains/{id}/dns/soa":          {"dns", Oku},
	"GET " + p + "/domains/{id}/dns/dnssec":       {"dns", Oku},
	"GET " + p + "/domains/{id}/dns/disa-aktar":   {"dns", Oku},
	"POST " + p + "/domains/{id}/dns":             {"dns", Yaz},
	"PUT " + p + "/domains/{id}/dns/{rid}":        {"dns", Yaz},
	"DELETE " + p + "/domains/{id}/dns/{rid}":     {"dns", Yaz},
	"POST " + p + "/domains/{id}/dns/sablon":      {"dns", Yaz},
	"POST " + p + "/domains/{id}/dns/toplu-sil":   {"dns", Yaz},
	"POST " + p + "/domains/{id}/dns/toplu-durum": {"dns", Yaz},
	"PUT " + p + "/domains/{id}/dns/soa":          {"dns", Yaz},
	"POST " + p + "/domains/{id}/dns/dnssec":      {"dns", Yaz},
	"POST " + p + "/domains/{id}/dns/ice-aktar":   {"dns", Yaz},
	// veritabani
	"GET " + p + "/domains/{id}/databases":     {"veritabani", Oku},
	"POST " + p + "/domains/{id}/databases":    {"veritabani", Yaz},
	"DELETE " + p + "/databases/{dbid}":        {"veritabani", Yaz},
	"POST " + p + "/databases/{dbid}/optimize": {"veritabani", Yaz},
	// ssl
	"GET " + p + "/domains/{id}/ssl":             {"ssl", Oku},
	"GET " + p + "/domains/{id}/ssl/ilerleme":    {"ssl", Oku},
	"GET " + p + "/domains/{id}/ssl/kapsam":      {"ssl", Oku},
	"POST " + p + "/domains/{id}/ssl/issue":      {"ssl", Yaz},
	"POST " + p + "/domains/{id}/ssl/dns-yenile": {"ssl", Yaz},
	"DELETE " + p + "/domains/{id}/ssl":          {"ssl", Yaz},
	// yedek (indir/icerik ve uzak hedef — kimlik bilgisi taşır — DIŞARIDA)
	"GET " + p + "/domains/{id}/backups":                  {"yedek", Oku},
	"GET " + p + "/domains/{id}/backups/ilerleme":         {"yedek", Oku},
	"GET " + p + "/domains/{id}/backup-schedule":          {"yedek", Oku},
	"POST " + p + "/domains/{id}/backups":                 {"yedek", Yaz},
	"DELETE " + p + "/domains/{id}/backups/{bid}":         {"yedek", Yaz},
	"POST " + p + "/domains/{id}/backups/{bid}/geriyukle": {"yedek", Yaz},
	"PUT " + p + "/domains/{id}/backup-schedule":          {"yedek", Yaz},
	// bayi (yalnız kök; PUT /resellers/{id} gövdesindeki parola alanı anahtarla
	// REDDEDİLİR — reseller.Guncelle; kimlik devralma yolu olmasın)
	"GET " + p + "/resellers":              {"bayi", Oku},
	"GET " + p + "/reseller-plans":         {"bayi", Oku},
	"GET " + p + "/reseller-plans/{id}":    {"bayi", Oku},
	"POST " + p + "/resellers":             {"bayi", Yaz},
	"PUT " + p + "/resellers/{id}":         {"bayi", Yaz},
	"DELETE " + p + "/resellers/{id}":      {"bayi", Yaz},
	"POST " + p + "/reseller-plans":        {"bayi", Yaz},
	"PUT " + p + "/reseller-plans/{id}":    {"bayi", Yaz},
	"DELETE " + p + "/reseller-plans/{id}": {"bayi", Yaz},
	// salt okuma
	"GET " + p + "/denetim":     {"denetim", Oku},
	"GET " + p + "/bildirimler": {"bildirim", Oku},
}

// HerZaman: izin grubundan bağımsız her geçerli anahtara açık uçlar.
//   - /api-anahtari/ben: "bağlantıyı test et" (anahtarın kendi meta verisi)
//   - /kapi/adres: eski kapı kapanınca otomasyonun yeni adresi öğrenmesi
var HerZaman = map[string]bool{
	"GET " + p + "/api-anahtari/ben": true,
	"GET " + p + "/kapi/adres":       true,
}

// KimlikUclari: jeton/oturum BASAN ya da kimlik/kimlik bilgisi DEĞİŞTİREN uçlar —
// izin ne olursa olsun anahtar ASLA erişemez (kullanıcı kararı 2026-10-03).
// Varsayılan-reddet bunları zaten kapatır; bu liste (1) Rotalar'a yanlışlıkla
// eklenmelerini testle engeller, (2) rota envanteri testinin "kimlik benzeri yeni
// uç sınıflandırılmadı" denetiminin bilinen kümesidir. Yeni bir kimlik ucu
// eklendiğinde TestKimlikUclariSiniflandirildi düşer → buraya (ya da bilinçli
// olarak KimlikBenzeriIzinli'ye) eklenmeden birleşmez.
var KimlikUclari = map[string]bool{
	"POST " + p + "/auth/login":                                              true,
	"POST " + p + "/musteri/login":                                           true,
	"POST " + p + "/musteri/adina-giris":                                     true,
	"POST " + p + "/musteri/adina-cikis":                                     true,
	"POST " + p + "/domains/{id}/adina-oturum":                               true,
	"GET " + p + "/me":                                                       true,
	"PUT " + p + "/me":                                                       true,
	"POST " + p + "/me/parola":                                               true,
	"GET " + p + "/me/2fa/setup":                                             true,
	"POST " + p + "/me/2fa/enable":                                           true,
	"POST " + p + "/me/2fa/disable":                                          true,
	"POST " + p + "/databases/{dbId}/pma-token":                              true,
	"GET " + p + "/api-anahtarlari":                                          true,
	"GET " + p + "/api-anahtarlari/izin-gruplari":                            true,
	"POST " + p + "/api-anahtarlari":                                         true,
	"POST " + p + "/api-anahtarlari/{id}/yenile":                             true,
	"DELETE " + p + "/api-anahtarlari/{id}":                                  true,
	"PUT " + p + "/domains/{id}/ssh/anahtar":                                 true,
	"POST " + p + "/domains/{id}/wordpress/kullanici-parola":                 true,
	"POST " + p + "/domains/{id}/subdomain/{sid}/wordpress/kullanici-parola": true,
	"GET " + p + "/system/tasima/anahtar":                                    true,
	"GET " + p + "/settings/session-idle":                                    true,
	"PUT " + p + "/settings/session-idle":                                    true,
	"POST " + p + "/git-webhook/{secret}":                                    true,
	"POST " + p + "/internal/pma-redeem":                                     true,
	// Site taşıma kayıtlı bağlantı oturumları: kaynak sunucu kimlik bilgisi taşır.
	"GET " + p + "/system/tasima/oturumlar":      true,
	"GET " + p + "/system/tasima/oturum/{id}":    true,
	"DELETE " + p + "/system/tasima/oturum/{id}": true,
	// Eklenti vekili: eklentiye X-Gosp-Rol ile kimlik geçirir (eleştiri #21).
	"* " + p + "/eklenti/{ad}/*": true,
}

// KimlikBenzeriIzinli: adı kimlik/parola çağrıştıran ama BİLİNÇLİ olarak anahtara
// açılan uçlar (gerekçeli). Rota envanteri testi bu ikisini ayırt eder.
var KimlikBenzeriIzinli = map[string]string{
	"PUT " + p + "/domains/{id}/ftp/password": "hosting hesabının FTP parolası (WHMCS ChangePassword) — anahtar sahibinin kimliği değil; ayrı 'parola' izni ister",
	"PUT " + p + "/databases/{dbid}/password": "hosting veritabanı parolası — ayrı 'parola' izni ister",
	"GET " + p + "/kapi/adres":                "yalnız yeni panel adresi döner, jeton basmaz",
	"GET " + p + "/api-anahtari/ben":          "anahtarın kendi meta verisi (gizli/özet DÖNMEZ)",
}

// GrupBul: ada göre grup.
func GrupBul(ad string) (Grup, bool) {
	for _, g := range Gruplar {
		if g.Ad == ad {
			return g, true
		}
	}
	return Grup{}, false
}

func rolIzinli(g Grup, rol string) bool {
	for _, r := range g.Roller {
		if r == rol {
			return true
		}
	}
	return false
}

// ErrIzin: izin listesi geçersiz.
var ErrIzin = errors.New("geçersiz izin")

// IzinleriKanonik: ["hesap:yaz","dns:oku",...] → doğrulanmış, tekilleştirilmiş,
// sıralı "grup:erisim" listesi. Aynı grupta oku+yaz → yaz. Rolün seçemeyeceği grup
// (bayinin "bayi" grubu) ve yalnız-okuma grubunda "yaz" reddedilir. Boş liste
// reddedilir (izinsiz anahtar anlamsız).
func IzinleriKanonik(ham []string, rol string) ([]string, error) {
	enIyi := map[string]string{}
	for _, h := range ham {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			continue
		}
		grup, erisim, ok := strings.Cut(h, ":")
		if !ok || (erisim != Oku && erisim != Yaz) {
			return nil, ErrIzin
		}
		g, bulundu := GrupBul(grup)
		if !bulundu || !rolIzinli(g, rol) {
			return nil, ErrIzin
		}
		if g.YalnizOku && erisim == Yaz {
			return nil, ErrIzin
		}
		if enIyi[grup] != Yaz {
			enIyi[grup] = erisim
		}
	}
	if len(enIyi) == 0 {
		return nil, ErrIzin
	}
	out := make([]string, 0, len(enIyi))
	for g, e := range enIyi {
		out = append(out, g+":"+e)
	}
	sort.Strings(out)
	return out, nil
}

// IzinleriCoz: DB'deki virgüllü kanonik dizgeyi listeye çevirir.
func IzinleriCoz(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// Karar sonuçları.
const (
	KararIzinli     = 0
	KararUcKapali   = 1 // uç tabloda yok (varsayılan reddet) ya da kimlik ucu
	KararIzinYetmez = 2 // uç açık ama anahtarın grubu/erişimi yetmiyor
)

// Karar: "METOD desen" için bu rol + izinlerle erişim kararı.
// 🔴 Önce KimlikUclari (asla), sonra HerZaman, sonra Rotalar; hiçbiri değilse reddet.
func Karar(metodDesen, rol string, izinler []string) int {
	if KimlikUclari[metodDesen] || kimlikJoker(metodDesen) {
		return KararUcKapali
	}
	if HerZaman[metodDesen] {
		return KararIzinli
	}
	k, ok := Rotalar[metodDesen]
	if !ok {
		return KararUcKapali
	}
	g, ok := GrupBul(k.Grup)
	if !ok || !rolIzinli(g, rol) {
		return KararIzinYetmez
	}
	for _, iz := range izinler {
		grup, erisim, _ := strings.Cut(iz, ":")
		if grup != k.Grup {
			continue
		}
		if erisim == Yaz || (erisim == Oku && k.Erisim == Oku) {
			return KararIzinli
		}
	}
	return KararIzinYetmez
}

// kimlikJoker: KimlikUclari'ndaki "* desen" girdileri metottan bağımsızdır.
func kimlikJoker(metodDesen string) bool {
	_, desen, ok := strings.Cut(metodDesen, " ")
	return ok && KimlikUclari["* "+desen]
}
