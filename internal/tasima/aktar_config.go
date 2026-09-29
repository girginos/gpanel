package tasima

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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

// configtenDBBul — kopyalanan site yapilandirmasindan (wp-config.php/.env/…) DB
// ADLARINI cikarir. Kesif DB listesi bos kaldiginda YEDEK yol: gercek DB adi
// yapilandirmada yazilidir ve kaynakta da AYNI isimdir → dokumu oradan alinir.
// Yalniz reDBAd'e uyan, sistemDB olmayan adlar dondurulur (guvenlik).
func configtenDBBul(webRoot string) []string {
	gorulen := map[string]bool{}
	var out []string
	for _, rel := range yapilandirmaAdaylari {
		yol := filepath.Join(webRoot, rel)
		st, err := os.Lstat(yol)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			continue
		}
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
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
	}
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
func configtenDBKimlik(webRoot string) map[string]dbKimlik {
	out := map[string]dbKimlik{}
	for _, rel := range yapilandirmaAdaylari {
		yol := filepath.Join(webRoot, rel)
		st, err := os.Lstat(yol)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			continue
		}
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
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
	}
	return out
}

// configYenidenYaz — site yapilandirmasindaki DB baglanti bilgilerini gunceller.
//
// 🔴 Yalnizca BILINEN ANAHTARLARIN degeri degistirilir. Onceki surum metinde
// "<hesap>_" gecen her yeri degistiriyordu: DB adini kullanici adiyla eziyor ve
// icinde apostrof olan PHP dosyalarini sozdizimsel olarak BOZUYORDU.
func (h *Handlers) configYenidenYaz(webRoot string, esleme map[string]dbHedef, kaynakHesap, yeniPw string, log func(string, ...any)) int {
	if len(esleme) == 0 {
		return 0
	}
	var hedefKul string
	adEsleme := map[string]string{}
	for eski, hedef := range esleme {
		adEsleme[eski] = hedef.Ad
		hedefKul = hedef.Kul
	}

	sayac := 0
	for _, rel := range yapilandirmaAdaylari {
		yol := filepath.Join(webRoot, rel)
		st, err := os.Lstat(yol)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			continue
		}
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		yeni := string(ham)
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
		if yeni == string(ham) {
			continue
		}
		if err := yedekYaz(webRoot, rel, ham); err != nil {
			log("uyari: %s yedegi alinamadi, dosya degistirilmedi: %v", rel, err)
			continue
		}
		if err := atomikYaz(yol, []byte(yeni), st); err == nil {
			sayac++
			log("yapilandirma guncellendi: %s", rel)
		}
	}
	return sayac
}

// atomikYaz — ayni dizine gecici yaz + rename. Yarida cokme yarim
// wp-config.php birakmasin; sahiplik/izin korunur.
func atomikYaz(yol string, veri []byte, st os.FileInfo) error {
	dizin := filepath.Dir(yol)
	tmp, err := os.CreateTemp(dizin, ".gosp_cfg_*")
	if err != nil {
		return err
	}
	ad := tmp.Name()
	if _, err := tmp.Write(veri); err != nil {
		tmp.Close()
		os.Remove(ad)
		return err
	}
	tmp.Close()
	_ = os.Chmod(ad, st.Mode().Perm())
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(ad, int(sys.Uid), int(sys.Gid))
	}
	if err := os.Rename(ad, yol); err != nil {
		os.Remove(ad)
		return err
	}
	return nil
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
