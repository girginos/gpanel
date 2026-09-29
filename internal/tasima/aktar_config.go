package tasima

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Yapilandirma dosyasi yeniden yazma
// ---------------------------------------------------------------------------

// dbAnahtarlari — degeri DB ADI olan yapilandirma anahtarlari.
var dbAdAnahtarlari = []string{"DB_NAME", "DB_DATABASE", "DATABASE", "dbname", "database", "db"}

// dbKullaniciAnahtarlari — degeri DB KULLANICISI olan anahtarlar.
var dbKulAnahtarlari = []string{"DB_USER", "DB_USERNAME", "DATABASE_USER"}

// dbParolaAnahtarlari — degeri DB PAROLASI olan anahtarlar.
var dbPwAnahtarlari = []string{"DB_PASSWORD", "DB_PASS", "DATABASE_PASSWORD"}

// yapilandirmaAdaylari — DB baglanti bilgisi barindiran bilinen yapilandirma dosyalari.
var yapilandirmaAdaylari = []string{
	"wp-config.php", ".env", "configuration.php", "config.php",
	"app/etc/env.php", "sites/default/settings.php", "config/db.php",
	"application/config/database.php", "includes/config.php",
	"src/config.php", // XenForo
}

// xenforoDBOku — XenForo src/config.php'den $config['db']['<key>'] değerini okur.
// XenForo iç-içe-bracket kullandığı için generic reAnahtarSatir bunu tanımaz;
// DB'nin ORİJİNAL kimliğini (ad/kullanıcı/parola) çıkarıp korumak için ayrı okunur.
func xenforoDBOku(icerik, key string) string {
	re := regexp.MustCompile(`\$config\s*\[\s*['"]db['"]\s*\]\s*\[\s*['"]` + key + `['"]\s*\]\s*=\s*['"]([^'"]*)['"]`)
	// 🔴 SON eşleşme: config'de birden çok $config['db'] bloğu olabilir (eski + yeni);
	// PHP'de SON atama aktiftir. İlk'i almak yanlış (eski/ölü DB'yi çıkarır).
	ms := re.FindAllStringSubmatch(icerik, -1)
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1][1]
}

// xenforoDBYaz — XenForo $config['db']['<key>'] değerini yeni değerle değiştirir.
// Yalnız DB adı hedefte çakışıp yeniden adlandırıldığında (orijinal korunamadığında)
// devreye girer. val alfanumerik (RandomParola / sk_) olduğundan tek-tırnak güvenli.
func xenforoDBYaz(icerik, key, val string) string {
	re := regexp.MustCompile(`(\$config\s*\[\s*['"]db['"]\s*\]\s*\[\s*['"]` + key + `['"]\s*\]\s*=\s*)['"][^'"]*['"]`)
	return re.ReplaceAllString(icerik, `${1}'`+val+`'`)
}

// kiraciYapilandirmalari — webRoot altındaki bilinen yapılandırma dosyalarını
// (yapilandirmaAdaylari sırasıyla) okur ve her biri için f'i çağırır; d f
// dönene kadar açıktır (f onu yerine yazabilir).
//
// 🔴 webRoot kiracının ağacıdır (kiracı her bileşeni bağa/FIFO'ya çevirebilir):
// dosyalar ev köküne KİLİTLİ os.Root altında, yalnız kiracının kendi düzenli ve
// tek bağlantılı dosyasıysa açılır (kiraciDosyasiAc). Uygun olmayan aday
// sessizce atlanır. webRoot bir kiracı evinde değilse hiçbir dosya açılmadan
// hata döner (okuyucular için "yapılandırma yok" demektir).
func kiraciYapilandirmalari(webRoot string, f func(rel string, d *kiraciDosyasi, ham []byte)) error {
	ki, kok, err := kiraciYolu(webRoot)
	if err != nil {
		return errors.New("web koku kiraci ev dizininde degil")
	}
	uid, _, err := uidGid(ki.Kul)
	if err != nil {
		return errors.New("sistem kullanicisi bulunamadi")
	}
	ev, err := ki.EvAc()
	if err != nil {
		return errors.New("ev dizini acilamadi")
	}
	defer ev.Close()
	for _, rel := range yapilandirmaAdaylari {
		d, err := kiraciDosyasiAc(ev, path.Join(kok, rel), uid)
		if err != nil {
			continue
		}
		if ham, err := d.Oku(yapilandirmaMaxBayt); err == nil {
			f(rel, d, ham)
		}
		d.Kapat()
	}
	return nil
}

// configtenDBBul — kopyalanan site yapilandirmasindan (wp-config.php/.env/…) DB
// ADLARINI cikarir. Kesif DB listesi bos kaldiginda YEDEK yol: gercek DB adi
// yapilandirmada yazilidir ve kaynakta da AYNI isimdir → dokumu oradan alinir.
// Yalniz reDBAd'e uyan, sistemDB olmayan adlar dondurulur (guvenlik). Dosyalar
// kiraciYapilandirmalari ile okunur: kök komşunun yapılandırmasını okuyamaz.
func configtenDBBul(webRoot string) []string {
	gorulen := map[string]bool{}
	var out []string
	_ = kiraciYapilandirmalari(webRoot, func(_ string, _ *kiraciDosyasi, ham []byte) {
		for _, satir := range strings.Split(string(ham), "\n") {
			m := reAnahtarSatir.FindStringSubmatch(satir)
			if m == nil {
				continue
			}
			dbAnahtar := false
			for _, a := range dbAdAnahtarlari {
				if strings.EqualFold(m[2], a) {
					dbAnahtar = true
					break
				}
			}
			if !dbAnahtar {
				continue
			}
			deger, _ := degerAyikla(m[3])
			deger = strings.TrimSpace(deger)
			if deger == "" || gorulen[deger] || !reDBAd.MatchString(deger) || sistemDB(deger) {
				continue
			}
			gorulen[deger] = true
			out = append(out, deger)
		}
		// XenForo dbname (generic satır tanımaz) — orijinal DB adı korunsun diye keşfe ekle.
		if xa := xenforoDBOku(string(ham), "dbname"); xa != "" && !gorulen[xa] && reDBAd.MatchString(xa) && !sistemDB(xa) {
			gorulen[xa] = true
			out = append(out, xa)
		}
	})
	return out
}

func anahtardaMi(anahtar string, liste []string) bool {
	for _, a := range liste {
		if strings.EqualFold(anahtar, a) {
			return true
		}
	}
	return false
}

type dbKimlik struct{ Kul, Pw string }

// configtenDBKimlik — kopyalanan yapilandirmadan DB adi -> {kullanici, parola}.
// DB'yi ORIJINAL kimligiyle tasimak icin (wp-config DEGISMESIN, ad kisa kalsin).
// Dosyalar kiraciYapilandirmalari ile okunur: bağla komşunun DB kimliği
// (ad/kullanıcı/parola) okunup bu hesaba aktarılamaz.
func configtenDBKimlik(webRoot string) map[string]dbKimlik {
	out := map[string]dbKimlik{}
	_ = kiraciYapilandirmalari(webRoot, func(_ string, _ *kiraciDosyasi, ham []byte) {
		ad, kul, pw := "", "", ""
		for _, satir := range strings.Split(string(ham), "\n") {
			m := reAnahtarSatir.FindStringSubmatch(satir)
			if m == nil {
				continue
			}
			deger, _ := degerAyikla(m[3])
			deger = strings.TrimSpace(deger)
			if deger == "" {
				continue
			}
			switch {
			case ad == "" && anahtardaMi(m[2], dbAdAnahtarlari):
				ad = deger
			case kul == "" && anahtardaMi(m[2], dbKulAnahtarlari):
				kul = deger
			case pw == "" && anahtardaMi(m[2], dbPwAnahtarlari):
				pw = deger
			}
		}
		// XenForo ($config['db'][...]) — generic reAnahtarSatir tanımaz. ORİJİNAL DB
		// kimliğini buradan çıkar ki taşımada DB adı/kullanıcı DEĞİŞMESİN (korunur).
		if (ad == "" || kul == "" || pw == "") && strings.Contains(string(ham), "'db'") {
			if v := xenforoDBOku(string(ham), "dbname"); v != "" {
				ad = v
			}
			if v := xenforoDBOku(string(ham), "username"); v != "" {
				kul = v
			}
			if v := xenforoDBOku(string(ham), "password"); v != "" {
				pw = v
			}
		}
		if ad != "" && kul != "" && pw != "" {
			if _, v := out[ad]; !v {
				out[ad] = dbKimlik{Kul: kul, Pw: pw}
			}
		}
	})
	return out
}

// configYenidenYaz — site yapilandirmasindaki DB baglanti bilgilerini gunceller.
//
// 🔴 Yalnizca BILINEN ANAHTARLARIN degeri degistirilir. Onceki surum metinde
// "<hesap>_" gecen her yeri degistiriyordu: DB adini kullanici adiyla eziyor ve
// icinde apostrof olan PHP dosyalarini sozdizimsel olarak BOZUYORDU.
//
// 🔴 webRoot kiracının ağacıdır (kiracı her bileşeni bağa çevirebilir): dosyalar
// ev köküne KİLİTLİ os.Root altında, yalnız kiracının kendi düzenli dosyasıysa
// okunup atomik yenilenir (kiraciYapilandirmalari). Eskiden kök yol üzerinden
// okuyup yazıyordu → public_html komşu evine bağlıysa KOMŞUNUN yapılandırması
// ezilirdi.
func (h *Handlers) configYenidenYaz(webRoot string, esleme map[string]dbHedef, kaynakHesap, yeniPw string, log func(string, ...any)) int {
	if len(esleme) == 0 {
		return 0
	}
	sayac := 0
	err := kiraciYapilandirmalari(webRoot, func(rel string, d *kiraciDosyasi, ham []byte) {
		yeni := yapilandirmaDegistir(string(ham), esleme, yeniPw)
		if yeni == string(ham) {
			return
		}
		if err := yedekYaz(webRoot, rel, ham); err != nil {
			log("uyari: %s yedegi alinamadi, dosya degistirilmedi: %v", rel, err)
			return
		}
		if err := d.Degistir([]byte(yeni), d.izin()); err == nil {
			sayac++
			log("yapilandirma guncellendi: %s", rel)
		}
	})
	if err != nil {
		log("uyari: yapilandirma guncellenmedi: %v", err)
	}
	return sayac
}

// yapilandirmaDegistir — yapılandırma metnindeki DB adı/kullanıcı/parola
// değerlerini eşlemeye göre yeniler (yalnız bilinen anahtarlar).
func yapilandirmaDegistir(ham string, esleme map[string]dbHedef, yeniPw string) string {
	var hedefKul string
	adEsleme := map[string]string{}
	for eski, hedef := range esleme {
		adEsleme[eski] = hedef.Ad
		hedefKul = hedef.Kul
	}
	yeni := ham
	for _, a := range dbAdAnahtarlari {
		yeni = anahtarDegeriDegistirMap(yeni, a, adEsleme)
	}
	if hedefKul != "" {
		for _, a := range dbKulAnahtarlari {
			yeni = anahtarDegeriDegistir(yeni, a, hedefKul, "")
		}
	}
	if yeniPw != "" {
		for _, a := range dbPwAnahtarlari {
			yeni = anahtarDegeriDegistir(yeni, a, yeniPw, "")
		}
	}
	// XenForo ($config['db'][...]) — generic anahtar tanımaz. YALNIZ orijinal DB
	// adı hedefte çakışıp yeniden adlandırıldığında gerekir (aksi halde orijinal
	// kimlik korunur, config zaten doğrudur). username/password + eşlenen dbname.
	if strings.Contains(yeni, "'db'") || strings.Contains(yeni, "\"db\"") {
		if cur := xenforoDBOku(yeni, "dbname"); cur != "" {
			if hedef, ok := esleme[cur]; ok && hedef.Ad != "" && hedef.Ad != cur {
				yeni = xenforoDBYaz(yeni, "dbname", hedef.Ad)
			}
		}
		if hedefKul != "" {
			yeni = xenforoDBYaz(yeni, "username", hedefKul)
		}
		if yeniPw != "" {
			yeni = xenforoDBYaz(yeni, "password", yeniPw)
		}
	}
	return yeni
}

var reAnahtarSatir = regexp.MustCompile(`^(\s*)(?:define\s*\(\s*)?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?\s*(?:,|=>|=|:)\s*(.*)$`)

// anahtarDegeriDegistir — "<anahtar> = <deger>" bicimindeki satirlarda degeri
// yenisiyle degistirir. Tirnak turu korunur. onekSarti verilirse yalnizca o
// onekle baslayan degerler degistirilir (yanlislikla baska degeri ezmemek icin).
func anahtarDegeriDegistir(metin, anahtar, yeni, onekSarti string) string {
	satirlar := strings.Split(metin, "\n")
	for i, s := range satirlar {
		m := reAnahtarSatir.FindStringSubmatch(s)
		if m == nil || !strings.EqualFold(m[2], anahtar) {
			continue
		}
		eskiDeger, tirnak := degerAyikla(m[3])
		if eskiDeger == "" {
			continue
		}
		if onekSarti != "" && !strings.HasPrefix(eskiDeger, onekSarti+"_") && eskiDeger != onekSarti {
			continue
		}
		satirlar[i] = strings.Replace(s, tirnak+eskiDeger+tirnak, tirnak+yeni+tirnak, 1)
	}
	return strings.Join(satirlar, "\n")
}

// anahtarDegeriDegistirMap — degeri eslemede bulunan anahtarlari degistirir.
func anahtarDegeriDegistirMap(metin, anahtar string, esleme map[string]string) string {
	satirlar := strings.Split(metin, "\n")
	for i, s := range satirlar {
		m := reAnahtarSatir.FindStringSubmatch(s)
		if m == nil || !strings.EqualFold(m[2], anahtar) {
			continue
		}
		eskiDeger, tirnak := degerAyikla(m[3])
		yeni, ok := esleme[eskiDeger]
		if !ok || eskiDeger == "" {
			continue
		}
		satirlar[i] = strings.Replace(s, tirnak+eskiDeger+tirnak, tirnak+yeni+tirnak, 1)
	}
	return strings.Join(satirlar, "\n")
}

// degerAyikla — "'abc' );" → ("abc","'") · "abc" → ("abc","")
func degerAyikla(kalan string) (string, string) {
	kalan = strings.TrimSpace(kalan)
	if kalan == "" {
		return "", ""
	}
	if kalan[0] == '\'' || kalan[0] == '"' {
		t := string(kalan[0])
		son := strings.Index(kalan[1:], t)
		if son < 0 {
			return "", ""
		}
		return kalan[1 : 1+son], t
	}
	// tirnaksiz: satir sonuna / ayraca kadar
	son := strings.IndexAny(kalan, " \t;,)#")
	if son >= 0 {
		kalan = kalan[:son]
	}
	return strings.TrimSpace(kalan), ""
}
