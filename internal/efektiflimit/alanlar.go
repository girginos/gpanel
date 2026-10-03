// Package efektiflimit — hostingin EFEKTİF limitlerinin TEK kaynağı.
//
// Hosting her zaman bir katalog planına bağlıdır (domains.plan_id). Hostinge özel
// farklı değerler hosting_limit_ust tablosunda durur (göç 0120); her sütunda NULL
// "plandan al" demektir. Efektif değer alan bazında şu sırayla çözülür:
//
//  1. hosting_limit_ust.<alan>              (üst yazım)
//  2. domains.<alan> > 0                    (yalnız disk_kota_mb / inode_kota — 0037 mirası)
//  3. service_plans.<alan>                  (plan atanmışsa; 0 = planın kendi anlamı)
//  4. çağıranın bugünkü varsayılanı        (plan yok, üst yazım yok)
//
// Paket YAPRAKTIR: yalnız database/sql + context + standart kitaplık import eder;
// provisioner, kaynaklimit, domains, plans… hepsi bunu kullanabilir (import döngüsü yok).
//
// Efektif limiti okuyan HER yer ya Oku/Sonuc'u ya da SQL'de Join+Ifade'yi kullanır;
// kaynak_tarama_test.go service_plans'tan limit sütununu doğrudan okuyan yeni
// bir yer eklenirse düşer.
package efektiflimit

import (
	"fmt"
	"strings"
)

// Sıfırın anlamı (istemciye gösterim + doğrulama için).
const (
	SifirSinirsiz    = "sinirsiz"    // 0 = sınırsız
	SifirOtomatik    = "otomatik"    // 0 = otomatik türet (pm_max_children)
	SifirYasak       = "yasak"       // 0 kabul edilmez (cgroup varsayılana düşerdi — yanıltıcı)
	SifirKullanilmaz = "kullanilmaz" // 0 = sorgu öldürme yok vb. (sınırsız ile aynı ama metin farklı)
)

// Alan — üst yazılabilen bir limit sütunu. Sütun adları SABİTTİR; istemciden gelen
// anahtar bu listeye karşı doğrulanır (SQL'e yalnız buradaki adlar girer).
type Alan struct {
	Sutun string
	Grup  string // disk | kaynak | io | db | sayi | posta
	Birim string // MB | MB/ay | adet | % | MB/s | IOPS | sn | /saat | ağırlık
	Sifir string
	Min   int
	Max   int
	// Ozette: kökün global planının bayiye seçim özetinde (plans.PlanOzet) GÖRÜNEN
	// alan. false → GL-06 sınıfı iç ayar: bayi, kökün değerini göremez.
	Ozette bool
	// BayiSifirYasak: bayi 0 (sınırsız) yazamaz (bayi planı kuralıyla aynı: disk/trafik).
	BayiSifirYasak bool
	// AlanMiras: domains.<sutun> (göç 0037, 0 = miras) ikinci kaynak olarak okunur.
	AlanMiras bool
}

const ustSinir = 1 << 30 // INT sütununa güvenle sığar

// Alanlar — 22 üst yazılabilir alan. Sıra arayüzdeki sıradır.
// Kapsam DIŞI (bilinçli): php_surum, fastcgi_cache, client_max_body_mb,
// nginx_ek_direktifler, waf_* — bunların hosting seviyesinde zaten sahibi var
// (SetPHP, nginx ayarları, domains.waf_*); plandan yalnız oluşturmada tohumlanır.
var Alanlar = []Alan{
	{Sutun: "disk_kota_mb", Grup: "disk", Birim: "MB", Sifir: SifirSinirsiz, Max: ustSinir, Ozette: true, BayiSifirYasak: true, AlanMiras: true},
	{Sutun: "trafik_kota_mb", Grup: "disk", Birim: "MB/ay", Sifir: SifirSinirsiz, Max: ustSinir, Ozette: true, BayiSifirYasak: true},
	{Sutun: "inode_kota", Grup: "disk", Birim: "adet", Sifir: SifirSinirsiz, Max: ustSinir, AlanMiras: true},
	{Sutun: "cpu_yuzde", Grup: "kaynak", Birim: "%", Sifir: SifirYasak, Min: 1, Max: 100000, Ozette: true},
	{Sutun: "ram_mb", Grup: "kaynak", Birim: "MB", Sifir: SifirYasak, Min: 1, Max: ustSinir, Ozette: true},
	{Sutun: "max_process", Grup: "kaynak", Birim: "adet", Sifir: SifirYasak, Min: 1, Max: 1000000},
	{Sutun: "io_agirlik", Grup: "kaynak", Birim: "ağırlık", Sifir: SifirYasak, Min: 1, Max: 10000},
	{Sutun: "pm_max_children", Grup: "kaynak", Birim: "adet", Sifir: SifirOtomatik, Max: 10000},
	{Sutun: "io_read_mbps", Grup: "io", Birim: "MB/s", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "io_write_mbps", Grup: "io", Birim: "MB/s", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "io_read_iops", Grup: "io", Birim: "IOPS", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "io_write_iops", Grup: "io", Birim: "IOPS", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "mysql_max_baglanti", Grup: "db", Birim: "adet", Sifir: SifirSinirsiz, Max: 100000},
	{Sutun: "db_max_queries_per_hour", Grup: "db", Birim: "/saat", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "db_max_updates_per_hour", Grup: "db", Birim: "/saat", Sifir: SifirSinirsiz, Max: ustSinir},
	{Sutun: "db_max_query_seconds", Grup: "db", Birim: "sn", Sifir: SifirKullanilmaz, Max: 86400},
	{Sutun: "max_domain", Grup: "sayi", Birim: "adet", Sifir: SifirSinirsiz, Max: 1000000, Ozette: true},
	{Sutun: "max_db", Grup: "sayi", Birim: "adet", Sifir: SifirSinirsiz, Max: 1000000, Ozette: true},
	{Sutun: "max_ftp", Grup: "sayi", Birim: "adet", Sifir: SifirSinirsiz, Max: 1000000, Ozette: true},
	{Sutun: "max_email", Grup: "posta", Birim: "adet", Sifir: SifirSinirsiz, Max: 1000000, Ozette: true},
	{Sutun: "saatlik_mail_limiti", Grup: "posta", Birim: "/saat", Sifir: SifirSinirsiz, Max: 10000000, Ozette: true},
	{Sutun: "mail_kutu_kota_mb", Grup: "posta", Birim: "MB", Sifir: SifirSinirsiz, Max: ustSinir, Ozette: true},
}

var alanHarita = func() map[string]Alan {
	m := make(map[string]Alan, len(Alanlar))
	for _, a := range Alanlar {
		m[a.Sutun] = a
	}
	return m
}()

// Bul — sütun üst yazılabilir mi? (istemci anahtarı doğrulaması)
func Bul(sutun string) (Alan, bool) {
	a, ok := alanHarita[sutun]
	return a, ok
}

// IcAlan — sütun kökün İÇ AYARI mı (Ozette=false: inode, process, IO, FPM, MySQL
// bağlantı + governor)? Bayi bu alanların kökten gelen değerini GÖREMEZ ve hiçbir
// yoldan (hosting limitleri, kendi planları) YAZAMAZ. Okuma maskesi (domains.limitGorunum,
// plans.PlanOzet) ile yazma reddi (domains.limitYaz, plans.Create/Update) bu TEK
// listeyi kullanır — kayma olmasın.
func IcAlan(sutun string) bool {
	a, ok := alanHarita[sutun]
	return ok && !a.Ozette
}

// IcAlanlar — iç alanların sütun adları (Alanlar sırasıyla).
func IcAlanlar() []string {
	var out []string
	for _, a := range Alanlar {
		if !a.Ozette {
			out = append(out, a.Sutun)
		}
	}
	return out
}

// IcAlanlarIcinde — anahtar kümesinde (istek gövdesi) bulunan iç alanlar, Alanlar
// sırasıyla. Boş = bayi isteği iç alan taşımıyor.
func IcAlanlarIcinde[V any](m map[string]V) []string {
	var out []string
	for _, a := range Alanlar {
		if _, var_ := m[a.Sutun]; var_ && !a.Ozette {
			out = append(out, a.Sutun)
		}
	}
	return out
}

// phpTuretilen — php_settings / subdomain_php_settings'te bir iç alandan TÜRETİLEN
// havuz anahtarları: per-tenant havuz (ve PlanHavuzTavani ile paylaşılan / alt alan
// havuzları) start/min_spare/max_spare'ı pm_max_children'dan hesaplar.
var phpTuretilen = map[string]string{
	"pm_start_servers":     "pm_max_children",
	"pm_min_spare_servers": "pm_max_children",
	"pm_max_spare_servers": "pm_max_children",
}

// PHPAyarIcAnahtarlari — PHP ayar gövdesinde (PUT /domains/{id}/php-settings ve alt
// alan eşi) kökün İÇ ALANINA ait anahtarlar: iç alan pm_max_children + ondan türetilen
// havuz sayıları. Yönetici dışı oturum (müşteri, bayi, müşteri adına) bunları
// GÖREMEZ ve YAZAMAZ (değer korunur) — bayi kararıyla aynı kural, aynı tek liste.
func PHPAyarIcAnahtarlari() []string {
	var out []string
	if IcAlan("pm_max_children") {
		out = append(out, "pm_max_children")
	}
	for _, k := range []string{"pm_start_servers", "pm_min_spare_servers", "pm_max_spare_servers"} {
		if IcAlan(phpTuretilen[k]) {
			out = append(out, k)
		}
	}
	return out
}

// IcAlanRedMesaji — bayi isteğinde iç alan geldiğinde dönen (403) hata metni.
func IcAlanRedMesaji(alanlar []string) string {
	return "bu ayarlar yöneticinize aittir, değiştirilemez: " + strings.Join(alanlar, ", ")
}

// Join — efektif limit SQL'i için domains d'ye eklenecek birleşimler.
// Kullanım: "SELECT "+Ifade("ram_mb")+" FROM domains d"+Join+"WHERE d.id=?"
const Join = " LEFT JOIN service_plans p ON p.id=d.plan_id LEFT JOIN hosting_limit_ust u ON u.domain_id=d.id "

// Ifade — bir alanın efektif değerinin SQL ifadesi (plan yok + üst yazım yok → NULL).
// Disk/inode: domains.<alan> > 0 (0037) üst yazımla plan arasındadır. Bilinmeyen
// sütun → panic (programlama hatası; istemci girdisi buraya ulaşmaz).
func Ifade(sutun string) string {
	a, ok := alanHarita[sutun]
	if !ok {
		panic(fmt.Sprintf("efektiflimit: bilinmeyen alan %q", sutun))
	}
	if a.AlanMiras {
		return fmt.Sprintf("COALESCE(u.`%[1]s`, NULLIF(d.`%[1]s`,0), p.`%[1]s`)", a.Sutun)
	}
	return fmt.Sprintf("COALESCE(u.`%[1]s`, p.`%[1]s`)", a.Sutun)
}

// IfadePlanla — hosting BAŞKA bir plana (takma adı planTakma olan service_plans
// birleşimi) geçseydi alanın efektif değeri ne olurdu: üst yazım korunur (kullanıcı
// kararı 2026-09-29: plan değişince hostinge özel limitler KORUNUR). Plan değişimi
// havuz/kullanım kapılarında kullanılır.
func IfadePlanla(sutun, planTakma string) string {
	a, ok := alanHarita[sutun]
	if !ok || !sutunTakmaAdi(planTakma) {
		panic(fmt.Sprintf("efektiflimit: geçersiz alan/takma ad %q/%q", sutun, planTakma))
	}
	if a.AlanMiras {
		return fmt.Sprintf("COALESCE(u.`%[1]s`, NULLIF(d.`%[1]s`,0), %[2]s.`%[1]s`)", a.Sutun, planTakma)
	}
	return fmt.Sprintf("COALESCE(u.`%[1]s`, %[2]s.`%[1]s`)", a.Sutun, planTakma)
}

func sutunTakmaAdi(s string) bool {
	if s == "" || len(s) > 16 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z') && c != '_' {
			return false
		}
	}
	return true
}

// IfadeVarsayilan — Ifade, NULL yerine verilen sayı (SUM/karşılaştırma için).
func IfadeVarsayilan(sutun string, varsayilan int) string {
	return fmt.Sprintf("COALESCE(%s, %d)", Ifade(sutun), varsayilan)
}

// OzelVarIfade — hostingte en az bir üst yazım var mı (0/1).
const OzelVarIfade = "(u.domain_id IS NOT NULL)"

// kokCoz / kokYaz — kok_alanlar CSV <-> küme (yalnız bilinen sütunlar).
func kokCoz(s string) map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if _, ok := alanHarita[p]; ok {
			m[p] = true
		}
	}
	return m
}

func kokYaz(m map[string]bool) string {
	var out []string
	for _, a := range Alanlar {
		if m[a.Sutun] {
			out = append(out, a.Sutun)
		}
	}
	return strings.Join(out, ",")
}
