package tasima

// GirginOSPanel kaynak kesfi — cozumleyici testi.
//
// Girdi, GERCEK bir gPanel sunucusunda (.181) `komutGpanel` calistirilarak
// alinmis ciktidir. Uydurma bir ornek degil: ozel web_root (`public_html/public`),
// alt alanlar, coklu WordPress veritabani ve bayi-sahipli bir domain hepsi
// gercek kurulumdan geliyor.

import "testing"

const gpanelOrnekCikti = `###USER:c_cagea_local
###OWNER:|admin||
###DB:
###DOM:cagea.local|/home/c_cagea_local/public_html|8.3|1|ana
###USER:c_girginos_app
###OWNER:|admin||
###DB:c_girginos_app_main,
###DOM:girginos.app|/home/c_girginos_app/public_html|8.3|1|ana
###DOM:test1.girginos.app|/home/c_girginos_app/subdomains/test1.girginos.app|8.3|1|ek
###USER:c_kaan_com
###OWNER:|admin||
###DB:c_kaan_com_main,wp_83ab02bc,wp_0aafe435,
###DOM:kaan.com|/home/c_kaan_com/public_html/public|8.3|1|ana
###DOM:girgin.kaan.com|/home/c_kaan_com/subdomains/girgin.kaan.com|8.3|104|ek
###USER:c_kaanreseller_com
###OWNER:girgin|reseller||
###DB:c_kaanreseller_com_main,
###DOM:kaanreseller.com|/home/c_kaanreseller_com/public_html|8.3|1|ana
`

func TestGpanelKesifCozumleme(t *testing.T) {
	h := ayristirBlok(gpanelOrnekCikti)
	if len(h) != 6 {
		t.Fatalf("6 site beklendi, %d bulundu", len(h))
	}
	ile := map[string]Hesap{}
	for _, x := range h {
		ile[x.AlanAdi] = x
	}

	// 🔴 OZEL WEB_ROOT KORUNMALI. gPanel'de docroot `public_html` olmak zorunda
	// degil (Laravel/Symfony `public_html/public` kullanir). Tahmin edilseydi
	// tasima yanlis dizini kopyalar ve site bos acilirdi.
	if got := ile["kaan.com"].WebRoot; got != "/home/c_kaan_com/public_html/public" {
		t.Errorf("kaan.com docroot yanlis: %q", got)
	}

	// 🔴 DB'LER YALNIZ ANA DOMAINE. Alt alanlar `ek` oldugu icin DB almamali;
	// aksi halde ayni veritabani birden cok kez tasinir (tuzak #7).
	if n := len(ile["kaan.com"].DBler); n != 3 {
		t.Errorf("kaan.com 3 DB beklendi, %d: %v", n, ile["kaan.com"].DBler)
	}
	if n := len(ile["girgin.kaan.com"].DBler); n != 0 {
		t.Errorf("alt alan DB ALMAMALI, %d aldi: %v", n, ile["girgin.kaan.com"].DBler)
	}

	// Alt alanlar ayri site olarak listelenir (kullanici secer).
	if got := ile["girgin.kaan.com"].WebRoot; got != "/home/c_kaan_com/subdomains/girgin.kaan.com" {
		t.Errorf("alt alan docroot yanlis: %q", got)
	}
	if ile["girgin.kaan.com"].BoyutMB != 104 {
		t.Errorf("alt alan boyutu yanlis: %d", ile["girgin.kaan.com"].BoyutMB)
	}

	// 🔴 SAHIPLIK. Bayi-sahipli domain bayiye, digerleri admin'e.
	if s := ile["kaanreseller.com"]; s.SahipTip != "reseller" || s.SahipLogin != "girgin" || s.Reseller != "girgin" {
		t.Errorf("bayi sahipligi yanlis: tip=%q login=%q reseller=%q", s.SahipTip, s.SahipLogin, s.Reseller)
	}
	if s := ile["kaan.com"]; s.SahipLogin != "" {
		t.Errorf("admin-sahipli domain'de sahip login bos olmali, %q", s.SahipLogin)
	}

	// DB'si olmayan site de listelenmeli (bos ###DB satiri).
	if _, ok := ile["cagea.local"]; !ok {
		t.Error("DB'siz site listelenmedi")
	}
}

// Sahip cozumleyicisi — gPanel bicimi (limitler BAYT olarak gelir).
func TestGpanelSahipCozumleme(t *testing.T) {
	// 10240 MB = 10737418240 bayt (komut MB'yi 1048576 ile carpar).
	cikti := "girgin|reseller|Girgin Bayi|b@ornek.com||10737418240|10|0\n" +
		"m7|client|Ahmet Yilmaz|a@ornek.com||0|0|0\n"
	s := ayristirSahipler(cikti)
	if len(s) != 2 {
		t.Fatalf("2 sahip beklendi, %d", len(s))
	}
	// 🔴 BAYT->MB donusumu. Carpim unutulsaydi 10240 MB'lik bayi hedefte
	// 10240 BAYT limitle acilirdi.
	if s[0].DiskMB != 10240 {
		t.Errorf("bayi disk limiti yanlis: %d MB", s[0].DiskMB)
	}
	if s[0].MaxDomain != 10 {
		t.Errorf("bayi domain limiti yanlis: %d", s[0].MaxDomain)
	}
	if s[0].TrafikMB != 0 {
		t.Errorf("0 = sinirsiz olmali, %d", s[0].TrafikMB)
	}
	// Musteri eslestirme anahtari "m<id>" — reHesap'tan gecmeli.
	if s[1].Tip != "client" || s[1].Login != "m7" {
		t.Errorf("musteri cozumlenemedi: %+v", s[1])
	}
	if s[1].Ad != "Ahmet Yilmaz" {
		t.Errorf("musteri adi kayboldu: %q", s[1].Ad)
	}
}

// Panel tipi allowlist'i gpanel'i kabul etmeli.
func TestGpanelGecerliTip(t *testing.T) {
	if !gecerliTipler["gpanel"] {
		t.Error("gpanel gecerli panel tipi olmali")
	}
	if gecerliTipler["bilinmiyor"] {
		t.Error("bilinmeyen tip kabul EDILMEMELI")
	}
}
