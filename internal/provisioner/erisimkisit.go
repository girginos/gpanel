package provisioner

// Domain bazlı erişim kısıtlama (Access Restrictions) — nginx server bloğuna
// allow/deny satırları basar.
//
// ─── Neden nginx, neden nft değil ───────────────────────────────────────────
// Panelin mevcut güvenlik duvarı (inet girginos_fw) nft tabanlı ve paket
// seviyesinde çalışır. Tek sunucu IP'sinde onlarca alan adı barındığı için
// nft, bir paketin HANGİ alan adına gittiğini göremez — alan adı ancak TLS SNI
// ya da HTTP Host başlığından bilinir. Bu yüzden domain bazlı kısıtlama
// zorunlu olarak HTTP katmanında, panelin zaten ürettiği per-domain server
// bloğunda uygulanır. (Azure'un "Access Restrictions" özelliği de ağ firewall'u
// değil, ön uç proxy'de allow/deny listesidir.)
//
// ─── Semantik ───────────────────────────────────────────────────────────────
// ngx_http_access_module: kurallar yukarıdan aşağı, İLK EŞLEŞEN KAZANIR.
//   varsayilan='red'  → liste beyaz liste olur ("allow ...; deny all;")
//   varsayilan='izin' → liste kara liste olur ("deny ...; allow all;")
//
// 🔴 acme-challenge MUAF: server bağlamındaki allow/deny tüm location'lara
// miras kalır. Muafiyet olmasaydı "deny all" Let's Encrypt HTTP-01 doğrulamasını
// da keserdi → sertifika 90 gün sonra yenilenemez → site TLS hatasıyla ölür.
// Muafiyet vhost şablonundaki acme location'ına "allow all;" ile konur.

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
)

// erisimKuralGecerli — bir kural değerinin nginx'e basılmaya uygun olup
// olmadığını söyler.
//
// 🔴 Bu doğrulama API'de de yapılır; burada TEKRAR yapılması bilinçli. DB'den
// gelen değer doğrudan nginx yapılandırmasına yazıldığı için, DB'ye başka bir
// yoldan (elle SQL, eski kayıt, gelecekteki bir hata) girmiş bozuk bir değer
// config enjeksiyonuna dönüşebilirdi. Tek IP ya da CIDR dışında hiçbir şey
// kabul edilmez — "all" bile; varsayılan politika ayrı alandan gelir.
func erisimKuralGecerli(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return false
	}
	if strings.ContainsAny(s, " \t\r\n;{}#'\"\\") {
		return false
	}
	// 🔴 nginx SEMANTIGI, Go semantigi DEGIL. `net.ParseIP` bu degeri gecerli
	// sayar ama nginx reddeder:
	//     nginx: [emerg] invalid parameter "255.255.255.255"
	// Kabul edilirse o domainin HER render'i duser — SSL yenilemesi, PHP
	// surum degisimi, askidan alma dahil. Yani tek bir kural degeri, sertifika
	// yenilemesini kalici bloke edip 90 gun sonra "TLS hatasi" olarak,
	// sebebinden cok uzakta ortaya cikar.
	if s == "255.255.255.255" {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// erisimBlokUret — verilen kural satırlarından nginx bloğunu üretir.
// Kısıtlama pasifse boş string döner (vhost hiç değişmez).
func erisimBlokUret(aktif bool, varsayilan string, kurallar []erisimKural) string {
	if !aktif {
		return ""
	}
	var b strings.Builder
	b.WriteString("    # ---- Erişim kısıtlama (panel; ilk eşleşen kazanır) ----\n")
	yazilan := 0
	for _, k := range kurallar {
		if !erisimKuralGecerli(k.CIDR) {
			// Bozuk kayıt atlanır: nginx'e yazılsaydı `nginx -t` patlar ve
			// RerenderVhost geri alma yapardı — yani TÜM ayar kaydedilemezdi.
			continue
		}
		// 🔴 FAIL-CLOSED: "izin mi?" diye sorulur, "red mi?" diye DEGIL.
		// Onceki surum `k.Tip == "red"` bakiyordu; kolon utf8mb4_unicode_ci
		// (harf duyarsiz) oldugu icin "RED" DB'ye girebiliyor ama Go
		// karsilastirmasi kaciriyordu ve kural sessizce TERSINE donuyordu:
		// "bu IP'yi engelle" -> "bu IP'ye izin ver". Olculdu.
		// `varsayilan` icin ayni tehdide karsi EqualFold yazilmisti; `tip`
		// unutulmus. Tanimadigimiz her deger ENGELLER.
		yon := "deny"
		if strings.EqualFold(strings.TrimSpace(k.Tip), "izin") {
			yon = "allow"
		}
		// 🔴 TRIM'LENMIS degeri yaz: erisimKuralGecerli basta TrimSpace yapiyor,
		// yani "1.2.3.4\u00a0" (NBSP) dogrulamayi GECIYOR ama ham hâliyle
		// yazilirsa `nginx -t` duser ve o domainin vhost'u bir daha hic
		// guncellenemez (SSL yenileme, PHP surumu dahil). Dogrulanan deger ile
		// yazilan deger AYNI olmali.
		// Normalizasyon render'da TEKRAR uygulanır: API'yi atlayarak DB'ye
		// girmiş host-bitli bir kayıt da kanonik hâliyle yazılsın.
		fmt.Fprintf(&b, "    %s %s;\n", yon, erisimKuralNormalize(k.CIDR))
		yazilan++
	}
	// 🔴 Veri bozulmasi korumasi: listede kayit VAR ama hicbiri gecerli
	// degilse, asagidaki "deny all" tek basina kalir ve siteyi TAMAMEN
	// kapatir. Bu bir niyet degil, bozuk veridir -- kisitlamayi hic uygulama.
	// (Liste bastan BOSSA bu bir niyettir: "herkesi kapat" gecerli bir ayardir.)
	if len(kurallar) > 0 && yazilan == 0 {
		return ""
	}
	// 🔴 KISMI BOZULMA: kayitlarin BAZILARI gecersizse, gecerli olanlar
	// yazilip gecersizler SESSIZCE dusuyordu. Panel 3 izinli IP gosterirken
	// nginx 1 tanesini uyguluyor, digerlerinin sahipleri hicbir uyari
	// olmadan kilitleniyordu. Artik gorunur bir iz birakilir: nginx
	// yapilandirmasina yorum dusulur ve log'a yazilir.
	if yazilan < len(kurallar) {
		fmt.Fprintf(&b, "    # UYARI: %d kuraldan %d tanesi GECERSIZ oldugu icin uygulanmadi\n",
			len(kurallar), len(kurallar)-yazilan)
		log.Printf("🔴 erisim kisitlama: %d kuraldan %d tanesi gecersiz — UYGULANMADI (panel bunlari 'aktif' gosteriyor olabilir)",
			len(kurallar), len(kurallar)-yazilan)
	}

	// Varsayılan politika HER ZAMAN yazılır; liste boş olsa bile niyet nettir.
	//
	// 🔴 FAIL-CLOSED: karsilastirma "red mi?" degil, "izin mi?" seklinde.
	// Onceki surum `== "red"` bakiyordu; MySQL kolonu utf8mb4_unicode_ci
	// (buyuk/kucuk harf duyarsiz) oldugu icin "RED" degeri DB'ye girebiliyor
	// ama Go karsilastirmasi kaciriyor ve kisit sessizce `allow all`'a
	// dusuyordu — yani taniinmayan her deger korumayi IPTAL ediyordu.
	// Artik yalniz acik acik "izin" acar; tanimadigimiz her sey KAPATIR.
	if strings.EqualFold(strings.TrimSpace(varsayilan), "izin") {
		b.WriteString("    allow all;\n")
	} else {
		b.WriteString("    deny all;\n")
	}
	return b.String()
}

type erisimKural struct {
	Tip  string
	CIDR string
}

// erisimKisitOku — domain için toggle + sıralı kural listesini okur.
//
// 🔴 FAIL-OPEN DÜZELTİLDİ. Önceki sürüm her okuma hatasında sessizce
// `false, "izin", nil` dönüyordu. Sonuç: GEÇİCİ bir DB hatası, aktif bir IP
// beyaz listesini vhost'tan TAMAMEN siliyor, API `ok:true` dönüyor ve site
// bir sonraki render'a kadar herkese açık kalıyordu. Denetimde ölçüldü:
// kurallar tablosu erişilemez yapıldı → PUT `ok:true` → site 403'ten 200'e
// düştü, hiçbir uyarı yok.
//
// Artık hata YUKARI TAŞINIR ve renderAndReload vhost'u HİÇ DEĞİŞTİRMEZ:
// diskte duran (korumalı) yapılandırma yerinde kalır. Bir okuma hatası,
// korumayı kaldırma gerekçesi değildir.
func erisimKisitOku(db *sql.DB, domainID int64) (bool, string, []erisimKural, error) {
	if db == nil || domainID <= 0 {
		return false, "izin", nil, nil
	}
	var aktif int
	var varsayilan string
	err := db.QueryRow(
		`SELECT aktif, varsayilan FROM domain_erisim_kisit WHERE domain_id=?`, domainID).
		Scan(&aktif, &varsayilan)
	if err == sql.ErrNoRows {
		return false, "izin", nil, nil // hiç ayar yok: kısıt kapalı (gerçek durum)
	}
	if err != nil {
		return false, "", nil, fmt.Errorf("erisim kisit ayari okunamadi: %w", err)
	}
	if aktif != 1 {
		return false, "izin", nil, nil
	}
	rows, err := db.Query(
		`SELECT tip, cidr FROM domain_erisim_kurallari WHERE domain_id=? ORDER BY sira ASC, id ASC`,
		domainID)
	if err != nil {
		return false, "", nil, fmt.Errorf("erisim kurallari okunamadi: %w", err)
	}
	defer rows.Close()
	var out []erisimKural
	for rows.Next() {
		var k erisimKural
		if err := rows.Scan(&k.Tip, &k.CIDR); err != nil {
			return false, "", nil, fmt.Errorf("erisim kurali cozulemedi: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return false, "", nil, fmt.Errorf("erisim kurallari taramasi yarim kaldi: %w", err)
	}
	return true, varsayilan, out, nil
}

// buildErisimKisit — renderAndReload'ın çağırdığı giriş noktası.
// sistem kullanıcısından domain id'sine çevirir, bloğu üretir.
func buildErisimKisit(sk string) (string, error) {
	if pkgDB == nil || strings.TrimSpace(sk) == "" {
		return "", nil
	}
	var domainID int64
	err := pkgDB.QueryRow(`SELECT id FROM domains WHERE sistem_kullanici=?`, sk).Scan(&domainID)
	if err == sql.ErrNoRows {
		return "", nil // domain yok: kisit da yok
	}
	if err != nil {
		return "", fmt.Errorf("domain id cozulemedi (%s): %w", sk, err)
	}
	aktif, varsayilan, kurallar, err := erisimKisitOku(pkgDB, domainID)
	if err != nil {
		return "", err
	}
	return erisimBlokUret(aktif, varsayilan, kurallar), nil
}

// ErisimKisitUygula — API katmanı ayar değiştirince vhost'u yeniden render eder.
// RerenderVhost içinde `nginx -t` kapısı + bozuk config'te geri alma var, yani
// hatalı bir kural canlı nginx'i düşüremez.
func ErisimKisitUygula(db *sql.DB, domainID int64) error {
	return RerenderVhost(db, domainID)
}

// erisimKuralNormalize — kuralı KANONİK ağ biçimine çevirir.
//
// 🔴 NEDEN ZORUNLU: nginx bir CIDR'ın host bitlerini yok sayar. `1.2.3.4/0`
// yazıldığında uyguladığı şey `0.0.0.0/0`'dır — yani TÜM İNTERNET. Panel ise
// kuralı yazıldığı gibi listeler; kullanıcı "sadece 1.2.3.4 girebilir" diye
// okur. Denetimde ölçüldü:
//
//	POST cidr=1.2.3.4/0        -> 200, panel "kısıt aktif" gösteriyor
//	vhost: allow 1.2.3.4/0; deny all;
//	listede OLMAYAN 127.0.0.1  -> 200   (herkes giriyor)
//	nginx: [warn] low address bits of 1.2.3.4/0 are meaningless
//
// nginx uyarıyı log'a yazar, kullanıcı onu HİÇ görmez. Aynı tuzak
// `192.168.1.55/8` gibi her host-bitli yazımda var.
//
// Çözüm: yazmadan önce kanonikleştir. `1.2.3.4/0` -> `0.0.0.0/0` olur; o
// zaman hem kullanıcı ne olduğunu görür hem de arayüzdeki "/0 herkesi
// kapsıyor" uyarısı devreye girer. Tek IP'ler olduğu gibi kalır.
func erisimKuralNormalize(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		return s
	}
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil || ipnet == nil {
		return s // geçersiz zaten reddedilecek
	}
	return ipnet.String()
}

// ErisimKuralNormalize — API için dışa açık kopya.
func ErisimKuralNormalize(s string) string { return erisimKuralNormalize(s) }

// ErisimKuralGecerli — API doğrulaması için dışa açık kopya.
func ErisimKuralGecerli(s string) bool { return erisimKuralGecerli(s) }

// vekilAraligiGecerli — GÜVENİLİR VEKİL aralığı için ek genişlik kapısı.
//
// 🔴 Erişim kuralı ile güvenilir vekil aynı doğrulamayı PAYLAŞAMAZ.
// `0.0.0.0/0` bir erişim kuralı olarak anlamlıdır ("herkese izin ver"), ama
// GÜVENİLİR VEKİL olarak felakettir: nginx o zaman HERKESİN gönderdiği
// X-Forwarded-For'a güvenir ve kısıtlama tek başlıkla atlatılır. Denetimde
// ölçüldü: set_real_ip_from 0.0.0.0/0 ile "XFF: 203.0.113.7" gönderen
// istemci, yalnız o IP'ye açık siteye 200 ile girdi.
//
// Bu yüzden vekil aralığında IPv4 için /8'den, IPv6 için /32'den geniş
// prefix kabul edilmez. Gerçek CDN aralıkları (Cloudflare /20, /22 …) bu
// kapının çok altındadır.
func vekilAraligiGecerli(s string) bool {
	s = strings.TrimSpace(s)
	if !erisimKuralGecerli(s) {
		return false
	}
	// 🔴 YEREL ADRES ASLA GUVENILIR VEKIL OLAMAZ.
	//
	// Denetimde olculdu: guvenilir vekil listesinde 127.0.0.0/8 varken, bir
	// kiracinin PHP'si kendi kutusundan 127.0.0.1'e baglanip
	// "X-Forwarded-For: <izinli-ip>" gondererek KOMSUSUNUN beyaz listesini
	// atladi (403 -> 200). Ayni sey RFC1918/CGNAT icin de gecerli: kutunun
	// ICINDEN uretilebilen hicbir adres, kimlik kaniti olamaz.
	//
	// Guvenilir vekil kavrami YUKARI AKIS bir CDN/proxy icindir; loopback ya
	// da ozel aralik ise her zaman yerel bir surectir.
	if ip := net.ParseIP(strings.Split(s, "/")[0]); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsPrivate() {
			return false
		}
		// CGNAT (100.64.0.0/10) — Go'nun IsPrivate'i kapsamiyor.
		if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
	}
	if !strings.Contains(s, "/") {
		return true // tek adres: en dar hâl, güvenli
	}
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return false
	}
	bit, toplam := ipnet.Mask.Size()
	if toplam == 32 {
		return bit >= 8
	}
	return bit >= 32
}

// VekilAraligiGecerli — API doğrulaması için dışa açık kopya.
func VekilAraligiGecerli(s string) bool { return vekilAraligiGecerli(s) }

// KullaniciDirektifTemizle — kiracının yazdığı nginx direktiflerinden sapma
// imzasını ayıklar.
//
// 🔴 NEDEN: sapma nöbetçisi vhost'ta sabit bir yorum satırı arıyor. O yorum
// kiracının `ek_direktifler` alanına yazılabildiği için nöbetçi KİRACI
// KONTROLÜNDEYDİ. Ölçüldü — iki yönde de sömürüldü:
//
//	(a) KÖRLEŞTİRME: kiracı imzayı kendi direktiflerine ekler, sonra gerçek
//	    kısıt bloğunu vhost'tan sildirir. Nöbetçi imzayı hâlâ gördüğü için
//	    "tutarlı" der; panel "kısıtlı" gösterirken site tamamen açıktır.
//	(b) SAHTE ALARM: DB'de kısıt yokken imza dosyada durur; nöbetçi her
//	    açılışta "SAPMA" deyip "düzeltildi" raporlar, ama hiçbir şey
//	    düzelmez — sonsuz yanlış-pozitif, gerçek sapmayı maskeler.
//
// Kiracının nginx yorumuna ihtiyacı yok; yalnız bu imzayı taşıyan satırlar
// atılır, diğer yorumlar korunur.
func KullaniciDirektifTemizle(s string) string {
	if !strings.Contains(s, erisimBlokImzasi) {
		return s
	}
	var tut []string
	for _, satir := range strings.Split(s, "\n") {
		if strings.Contains(satir, erisimBlokImzasi) {
			continue
		}
		tut = append(tut, satir)
	}
	return strings.Join(tut, "\n")
}

// erisimBlokImzasi — üretilen blokta bulunan sabit yorum satırı. Sapma
// tespiti bunu arar.
const erisimBlokImzasi = "# ---- Erişim kısıtlama (panel;"

// ErisimSapmasiVar — dosya içeriği ile beklenen blok tutuyor mu?
//
// beklenen boşsa: dosyada kısıt bloğu HİÇ olmamalı.
// beklenen doluysa: dosya o bloğu AYNEN içermeli (içerik sapması da yakalanır).
func ErisimSapmasiVar(dosya, beklenen string) bool {
	if strings.TrimSpace(beklenen) == "" {
		return strings.Contains(dosya, erisimBlokImzasi)
	}
	return !strings.Contains(dosya, beklenen)
}

// HealErisimSapmasi — vhost dosyası ile DB'nin birbirini tutmadığı domainleri
// yeniden render eder.
//
// 🔴 NEDEN GEREKLİ: DB, kısıtlamanın tek kaynağıdır — ama YALNIZCA render
// anında okunur. Kayıt API dışında bir yoldan silinirse (elle SQL, yedekten
// dönüş, toplu temizlik, bir göç) vhost'taki blok YERİNDE KALIR ve site,
// panelde hiçbir kısıt görünmemesine rağmen 403 dönmeye devam eder.
//
// Bu geliştirme sırasında iki kez yaşandı: temizlik satırları DB'den silindi,
// render tetiklenmedi, `test2.girginos.app` HTTPS'te 403'te takılı kaldı ve
// panel "kısıtlama yok" gösteriyordu. Müşteri için bu, sebebi panelde
// GÖRÜNMEYEN bir kesinti demektir.
//
// Nöbetçi ÜRETİMİ ölçer: DB'ye değil, diskteki gerçek dosyaya bakar.
// [[feedback_guard_must_measure_production]]
func HealErisimSapmasi() {
	if pkgDB == nil {
		return
	}
	rows, err := pkgDB.Query(`SELECT id, sistem_kullanici FROM domains WHERE COALESCE(askida,0)=0`)
	if err != nil {
		return
	}
	type dom struct {
		id int64
		sk string
	}
	var liste []dom
	for rows.Next() {
		var d dom
		if rows.Scan(&d.id, &d.sk) == nil {
			liste = append(liste, d)
		}
	}
	rows.Close()

	duzeltilen := 0
	for _, d := range liste {
		yol := "/etc/nginx/conf.d/dom_" + d.sk + ".conf"
		b, e := os.ReadFile(yol)
		if e != nil {
			continue
		}
		beklenen, berr := buildErisimKisit(d.sk)
		if berr != nil {
			// Okunamıyorsa DOKUNMA — bu, korumayı kaldırma gerekçesi değildir.
			log.Printf("erisim sapma kontrolu atlandi (%s): %v", d.sk, berr)
			continue
		}
		// 🔴 İÇERİK karşılaştırılır, yalnız VARLIK değil.
		//
		// Önceki sürüm `dosyadaVar == beklenenVar` yapıyordu; bu, DB'de
		// `allow 1.2.3.4` varken dosyada `allow 9.9.9.9` yazan bir vhost'u
		// "tutarlı" sayıyordu — sapmanın en tehlikeli biçimi (yanlış IP'ye
		// açık kalmak) tam da görünmez olan biçimdi.
		if ErisimSapmasiVar(string(b), beklenen) == false {
			continue // tutarlı
		}
		log.Printf("🔴 erisim kisitlama SAPMASI: %s — dosya ile DB tutmuyor — yeniden render ediliyor", d.sk)
		if e := RerenderVhost(pkgDB, d.id); e != nil {
			log.Printf("🔴 %s yeniden render edilemedi: %v", d.sk, e)
			continue
		}
		duzeltilen++
	}
	if duzeltilen > 0 {
		log.Printf("✓ erisim kisitlama sapmasi duzeltildi: %d domain", duzeltilen)
	}
}

// ── Alt alan adı erişim kısıtlama ───────────────────────────────────────────
//
// Ana domainle AYNI motoru kullanır: aynı doğrulama, aynı normalizasyon, aynı
// fail-closed varsayılan, aynı bozuk-veri koruması. Tek fark tablo adları.
// İki ayrı uygulama olsaydı kaçınılmaz olarak ayrışırlardı — ve güvenlik
// kararının iki farklı cevabı olması, kusurdan beterdir.

// altAlanErisimOku — alt alan için toggle + sıralı kural listesi.
func altAlanErisimOku(db *sql.DB, sid int64) (bool, string, []erisimKural, error) {
	if db == nil || sid <= 0 {
		return false, "izin", nil, nil
	}
	var aktif int
	var varsayilan string
	err := db.QueryRow(
		`SELECT aktif, varsayilan FROM subdomain_erisim_kisit WHERE subdomain_id=?`, sid).
		Scan(&aktif, &varsayilan)
	if err == sql.ErrNoRows {
		return false, "izin", nil, nil
	}
	if err != nil {
		return false, "", nil, fmt.Errorf("alt alan erisim ayari okunamadi: %w", err)
	}
	if aktif != 1 {
		return false, "izin", nil, nil
	}
	rows, err := db.Query(
		`SELECT tip, cidr FROM subdomain_erisim_kurallari WHERE subdomain_id=? ORDER BY sira ASC, id ASC`,
		sid)
	if err != nil {
		return false, "", nil, fmt.Errorf("alt alan erisim kurallari okunamadi: %w", err)
	}
	defer rows.Close()
	var out []erisimKural
	for rows.Next() {
		var k erisimKural
		if err := rows.Scan(&k.Tip, &k.CIDR); err != nil {
			return false, "", nil, fmt.Errorf("alt alan kurali cozulemedi: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return false, "", nil, fmt.Errorf("alt alan kurallari taramasi yarim kaldi: %w", err)
	}
	return true, varsayilan, out, nil
}

// AltAlanErisimBloku — alt alan vhost'una basılacak allow/deny bloğu.
//
// 🔴 Hata döndüğünde çağıran taraf vhost'a DOKUNMAMALIDIR: diskteki
// yapılandırma korumalı olabilir ve okunamayan bir ayar yüzünden onu ezmek,
// korumayı sessizce kaldırmak olurdu (ana domainde bu tam olarak yaşandı).
func AltAlanErisimBloku(db *sql.DB, sid int64) (string, error) {
	aktif, varsayilan, kurallar, err := altAlanErisimOku(db, sid)
	if err != nil {
		return "", err
	}
	return erisimBlokUret(aktif, varsayilan, kurallar), nil
}

// RenderKilidiAl — vhost yazma/doğrulama/reload bölümünü kilitler; dönen
// fonksiyon kilidi bırakır.
//
// Alt alan render'ı da AYNI global `nginx -t`'yi çalıştırıyor; ana domain
// render'ıyla aynı kilidi paylaşmazsa, ölçülmüş olan "komşunun geçici bozuk
// dosyası benim render'ımı düşürüyor" yarışı alt alan tarafından yeniden
// açılırdı.
func RenderKilidiAl() func() {
	renderKilidi.Lock()
	return renderKilidi.Unlock
}

// ── Güvenilir vekil (CDN) → 00-gosp-realip.conf ─────────────────────────────
//
// 🔴 Erişim kısıtlaması, nginx'in gördüğü $remote_addr üzerinden çalışır. Site
// bir CDN'in (Cloudflare vb.) arkasındaysa $remote_addr ziyaretçi DEĞİL, CDN
// kenar sunucusudur. O durumda:
//   - CDN aralıklarına izin verirsen  → herkes girer (kısıtlama yok gibi)
//   - Gerçek ziyaretçi IP'sine izin verirsen → hiç kimse giremez (site kapanır)
// İkisi de sessiz başarısızlık. Bu yüzden güvenilir vekil aralıkları
// tanımlandığında set_real_ip_from + real_ip_header üretilir; ancak o zaman
// $remote_addr gerçek ziyaretçiye eşit olur ve kısıtlama doğru çalışır.

const realIPConfYolu = "/etc/nginx/conf.d/00-gosp-realip.conf"

// RealIPConfYaz — guvenilir_vekil tablosundan global real_ip yapılandırmasını
// üretir. Tablo boşsa dosya SİLİNİR (yapılandırma yoksa varsayılan davranış:
// $remote_addr = doğrudan bağlanan istemci).
func RealIPConfYaz(db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	rows, err := db.Query(`SELECT cidr FROM guvenilir_vekil ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var cidrler []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return 0, err
		}
		if vekilAraligiGecerli(c) {
			cidrler = append(cidrler, strings.TrimSpace(c))
		} else {
			log.Printf("🔴 guvenilir vekil araligi REDDEDILDI (cok genis ya da gecersiz): %q", c)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(cidrler) == 0 {
		// Yapılandırma yoksa dosya da olmamalı: kalıntı bir dosya, kaldırılmış
		// bir CDN'i hâlâ güvenilir vekil sayardı (XFF sahteciliğine açık kapı).
		// Boş gövde yazmak = dosyayı silmek (helper boş içeriği görünce siler).
		if _, err := os.Stat(realIPConfYolu); os.IsNotExist(err) {
			return 0, nil
		}
		eski, _ := os.ReadFile(realIPConfYolu)
		if err := os.Remove(realIPConfYolu); err != nil {
			return 0, err
		}
		if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
			_ = os.WriteFile(realIPConfYolu, eski, 0o644)
			return 0, fmt.Errorf("`nginx -t` başarısız, realip conf GERİ ALINDI: %s",
				strings.TrimSpace(string(out)))
		}
		_ = exec.Command("systemctl", "reload", "nginx").Run()
		return 0, nil
	}

	baslik := "X-Forwarded-For"
	var h string
	if err := db.QueryRow(`SELECT deger FROM cp_ayarlar WHERE anahtar='realip_header'`).Scan(&h); err == nil {
		h = strings.TrimSpace(h)
		// Yalnız bilinen iki başlık; serbest metin nginx'e yazılmaz.
		if strings.EqualFold(h, "CF-Connecting-IP") {
			baslik = "CF-Connecting-IP"
		}
	}

	var b strings.Builder
	b.WriteString("# GirginOSPanel — güvenilir vekil (CDN) gerçek IP çözümlemesi.\n")
	b.WriteString("# Bu dosya panel tarafından üretilir; elle düzenleme KORUNMAZ.\n")
	b.WriteString("#\n")
	b.WriteString("# Domain bazlı erişim kısıtlaması $remote_addr'e bakar. Site bir CDN\n")
	b.WriteString("# arkasındaysa bu blok olmadan $remote_addr CDN'in kendisidir ve\n")
	b.WriteString("# kısıtlama sessizce yanlış çalışır.\n")
	for _, c := range cidrler {
		fmt.Fprintf(&b, "set_real_ip_from %s;\n", c)
	}
	fmt.Fprintf(&b, "real_ip_header %s;\n", baslik)
	if baslik == "X-Forwarded-For" {
		// Zincirin SONUNDAN geriye doğru, güvenilir vekilleri atlayarak gerçek
		// istemciyi bulur. Bu olmadan istemcinin uydurduğu XFF değeri kabul edilir.
		b.WriteString("real_ip_recursive on;\n")
	}
	// 🔴 Yaz → `nginx -t` → reload → hata olursa ESKİ İÇERİĞE geri al.
	// Bu dosya conf.d'de global: bozuk olursa TEK bir siteyi değil, sunucudaki
	// TÜM siteleri etkiler (nginx reload edilemez hâle gelir).
	if err := nginxDosyaYazDogrula(realIPConfYolu, b.String()); err != nil {
		return 0, err
	}
	return len(cidrler), nil
}
