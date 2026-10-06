package tasima

// d3a_kayit_test.go — D3a (DB'siz): kaynak kayıt defteri, cPanel/DirectAdmin
// bağdaştırıcılarının güvenilmez çıktı ayrıştırması, posta/sertifika keşfi, bayi
// günlük süzgeci, istek kapsamı, hız sınırı ve bayi hedef (yalnız genel IP) denetimi.

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/auth"
)

func TestKaynakKayit_Tanimlar(t *testing.T) {
	beklenen := map[string]struct {
		olgunluk string
		etkin    bool
		posta    bool
		sahipler bool
	}{
		"plesk":       {olgunlukDogrulandi, true, true, true},
		"gpanel":      {olgunlukDogrulandi, true, false, true},
		"cpanel":      {olgunlukDeneysel, false, true, false},
		"directadmin": {olgunlukDeneysel, false, true, false},
		// Arşiv kaynakları (faz 2): ayrı kod, deneysel, varsayılan KAPALI, sahip taşımaz.
		"arsiv-cpanel":      {olgunlukDeneysel, false, true, false},
		"arsiv-directadmin": {olgunlukDeneysel, false, true, false},
	}
	liste := kaynakSurucuSirali()
	if len(liste) != len(beklenen) {
		t.Fatalf("kayıtlı kaynak sayısı %d, beklenen %d", len(liste), len(beklenen))
	}
	for i, s := range liste {
		b, ok := beklenen[s.Kod]
		if !ok {
			t.Errorf("beklenmeyen kaynak %q", s.Kod)
			continue
		}
		if s.Olgunluk != b.olgunluk || s.VarsayilanEtkin != b.etkin || s.Yetenekler.Posta != b.posta || s.Yetenekler.Sahipler != b.sahipler {
			t.Errorf("%s = (%s etkin=%v posta=%v sahipler=%v), beklenen %+v", s.Kod, s.Olgunluk, s.VarsayilanEtkin, s.Yetenekler.Posta, s.Yetenekler.Sahipler, b)
		}
		// Arşiv kaynağında posta uzak keşiften değil arşivden gelir (arsivMailAktar).
		if !s.Arsiv && (s.postaKesfet != nil) != s.Yetenekler.Posta {
			t.Errorf("%s: posta yeteneği ile keşif fonksiyonu tutarsız", s.Kod)
		}
		if s.Arsiv && (s.VarsayilanEtkin || s.Olgunluk != olgunlukDeneysel || !s.Yetenekler.Cron) {
			t.Errorf("%s: arşiv kaynağı deneysel+varsayılan kapalı+cron yetenekli olmalı", s.Kod)
		}
		if (s.sahipKomutu != "") != s.Yetenekler.Sahipler {
			t.Errorf("%s: sahip yeteneği ile komut tutarsız", s.Kod)
		}
		if i > 0 && liste[i-1].Sira > s.Sira {
			t.Errorf("sıralama bozuk: %s önce %s", liste[i-1].Kod, s.Kod)
		}
	}
	if (&Kaynak{Tip: "joomla", Host: "198.51.100.1", Port: 22, Parola: "x"}).Dogrula() == nil {
		t.Error("kayıtta olmayan kaynak türü reddedilmeli")
	}
	if _, err := (&Kaynak{Tip: "joomla"}).Kesfet(context.Background()); !errors.Is(err, errDesteklenmeyenTip) {
		t.Errorf("bilinmeyen tipte keşif errDesteklenmeyenTip vermeli: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("aynı kod iki kez kaydedilememeli (panik beklenir)")
		}
	}()
	kaynakKaydet(&kaynakSurucu{Kod: "plesk", Olgunluk: olgunlukDeneysel, kesif: cpanelKesfet})
}

// cPanel keşif çıktısı (kaynak sunucudan gelir → DÜŞMAN).
const cpanelOrnekCikti = `###USER:ahmet
###DB:ahmet_wp,ahmet_shop,mysql,ahmet;drop,
###Y:---
###Y:main_domain: ahmet.com
###Y:addon_domains:
###Y:  ekalan.net: ekalan.ahmet.com
###Y:  'yolyok.org': yolyok.ahmet.com
###Y:parked_domains:
###Y:  - park.com
###Y:sub_domains:
###Y:  - blog.ahmet.com
###Y:  - ekalan.ahmet.com
###Y:  - kotu.ahmet.com
###Y:cp_php_magic_include_path.conf: 0
###CPUD:ahmet.com|/home/ahmet/public_html|ea-php81|120
###CPUD:ekalan.ahmet.com|/home/ahmet/ekalan.net|ea-php82|33
###CPUD:blog.ahmet.com|/home/ahmet/public_html/blog||7
###CPUD:kotu.ahmet.com|/home/ahmet/../root|ea-php81|1
###USER:kotu;kul
###Y:main_domain: kotu.com
###USER:capa
###Y:main_domain: &a capa.com
###USER:veli
###Y:main_domain: veli.org
###CPUD:veli.org|/home/veli/www;rm -rf /|ea-php74|5
`

func TestCpanelAyristir_UserdataEslemesi(t *testing.T) {
	hs := cpanelAyristir(cpanelOrnekCikti)
	bul := func(a string) *Hesap {
		for i := range hs {
			if hs[i].AlanAdi == a {
				return &hs[i]
			}
		}
		return nil
	}
	ana := bul("ahmet.com")
	if ana == nil || ana.WebRoot != "/home/ahmet/public_html" || ana.PHPSurum != "8.1" || ana.KaynakHesap != "ahmet" {
		t.Fatalf("ana alan = %+v", ana)
	}
	if strings.Join(ana.DBler, ",") != "ahmet_wp,ahmet_shop" {
		t.Errorf("DB listesi süzülmeli (sistem DB + geçersiz ad atılır): %v", ana.DBler)
	}
	if !strings.Contains(ana.Not, "park.com") {
		t.Errorf("park alan adı notta raporlanmalı: %q", ana.Not)
	}
	// 🔴 Eski hata: ek alan ana sitenin public_html'ine düşüyordu.
	ek := bul("ekalan.net")
	if ek == nil || ek.WebRoot != "/home/ahmet/ekalan.net" || ek.PHPSurum != "8.2" || len(ek.DBler) != 0 {
		t.Errorf("ek alan belge kökü alt alan userdata'sından gelmeli: %+v", ek)
	}
	if bul("yolyok.org") != nil {
		t.Error("userdata'sı olmayan ek alan listelenmemeli (yanlış köke düşmesin)")
	}
	blog := bul("blog.ahmet.com")
	if blog == nil || blog.WebRoot != "/home/ahmet/public_html/blog" {
		t.Errorf("alt alan ayrı site olmalı: %+v", blog)
	}
	if bul("ekalan.ahmet.com") != nil {
		t.Error("ek alanın arka plandaki alt alanı ayrıca listelenmemeli")
	}
	if bul("kotu.ahmet.com") != nil {
		t.Error("'..' içeren belge kökü atılmalı")
	}
	if bul("kotu.com") != nil || bul("capa.com") != nil {
		t.Error("geçersiz hesap adı ya da YAML çapası içeren hesap atlanmalı")
	}
	// veli: belge kökü metakarakterli → userdata atılır → cPanel varsayılanına düşer.
	if v := bul("veli.org"); v == nil || v.WebRoot != "/home/veli/public_html" {
		t.Errorf("geçersiz userdata yolu yerine varsayılan kök: %+v", v)
	}
	if len(hs) != 4 {
		t.Errorf("beklenen 4 site (ahmet.com, ekalan.net, blog.ahmet.com, veli.org), bulunan %d: %+v", len(hs), hs)
	}
}

func TestCpanelMainAyristir_DarAltKume(t *testing.T) {
	for ad, satirlar := range map[string][]string{
		"capa":        {"main_domain: a.com", "addon_domains: &x", "  b.com: b.a.com"},
		"takma":       {"main_domain: *x"},
		"birlestirme": {"main_domain: a.com", "addon_domains:", "  <<: x"},
		"akis":        {"main_domain: a.com", "sub_domains: [b.a.com]"},
		"liste-bozuk": {"main_domain: a.com", "sub_domains:", "  b.a.com"},
		"ana-yok":     {"addon_domains:"},
	} {
		if _, ok := cpanelMainAyristir(satirlar); ok {
			t.Errorf("%s: desteklenmeyen YAML reddedilmeli", ad)
		}
	}
	cok := []string{"main_domain: a.com", "sub_domains:"}
	for i := 0; i < cpanelYAMLElemanSiniri+1; i++ {
		cok = append(cok, "  - s.a.com")
	}
	if _, ok := cpanelMainAyristir(cok); ok {
		t.Error("eleman sınırı aşılınca reddedilmeli")
	}
}

func TestDirectAdminKesif_AltAlanlar(t *testing.T) {
	cikti := "###USER:veli\n###DB:veli_wp,\n" +
		"###DOM:veli.com|/home/veli/domains/veli.com/public_html|8.2|10|ana\n" +
		"###DOM:shop.veli.com|/home/veli/domains/veli.com/public_html/shop|8.2|3|ek\n" +
		"###DOM:kotu.veli.com|/home/veli/domains/veli.com/public_html/../../x|8.2|3|ek\n"
	hs := ayristirBlok(cikti)
	if len(hs) != 2 || hs[1].AlanAdi != "shop.veli.com" || hs[1].WebRoot != "/home/veli/domains/veli.com/public_html/shop" || len(hs[1].DBler) != 0 {
		t.Fatalf("DA alt alanı ek site olmalı, '..' atılmalı: %+v", hs)
	}
	if !strings.Contains(komutDirectAdmin, ".subdomains") || !strings.Contains(komutDirectAdmin, "mysql.conf") {
		t.Error("DA keşfi alt alanları ve mysql.conf kimliğini kullanmalı")
	}
}

func TestCpanelDAPostaKesfi(t *testing.T) {
	for _, tip := range []string{"cpanel", "directadmin"} {
		kay := &kaydedici{yanit: func(string) string {
			return "###HOME:/home/ahmet\ninfo\nInfo\n../komsu\nsatis\n"
		}}
		k := &Kaynak{Tip: tip, kabuk: kay}
		s, _ := surucuBul(tip)
		hs, err := s.postaKesfet(context.Background(), k, Hesap{KaynakHesap: "ahmet", AlanAdi: "ahmet.com"})
		if err != nil {
			t.Fatalf("%s: %v", tip, err)
		}
		if len(hs) != 3 { // info (tekil), ../komsu (mailAktar ad kuralında elenir), satis
			t.Fatalf("%s: kutular = %+v", tip, hs)
		}
		yol := hs[0].uzakYol("info")
		beklenen := map[string]string{"cpanel": "/home/ahmet/mail/ahmet.com/info/", "directadmin": "/home/ahmet/imap/ahmet.com/info/Maildir/"}[tip]
		if yol != beklenen {
			t.Errorf("%s: uzak maildir %q, beklenen %q", tip, yol, beklenen)
		}
		// Ev dizini düşmansa (kabuk metakarakteri / göreli) keşif reddedilir.
		kay.yanit = func(string) string { return "###HOME:/home/a;rm -rf /\ninfo\n" }
		if _, err := s.postaKesfet(context.Background(), k, Hesap{KaynakHesap: "ahmet", AlanAdi: "ahmet.com"}); err == nil {
			t.Errorf("%s: geçersiz ev dizini reddedilmeli", tip)
		}
		// Hesap/alan adı doğrulaması uzak komuttan ÖNCE.
		n := len(kay.cagrilar)
		if _, err := s.postaKesfet(context.Background(), k, Hesap{KaynakHesap: "a'b", AlanAdi: "ahmet.com"}); err == nil || len(kay.cagrilar) != n {
			t.Errorf("%s: geçersiz hesap adı uzak komuta varmamalı", tip)
		}
	}
	// gPanel: posta yeteneği yok → mailAktar kaynağa komut göndermez.
	kay := &kaydedici{}
	n, uy, err := (&Handlers{}).mailAktar(context.Background(), &Kaynak{Tip: "gpanel", kabuk: kay}, Hesap{AlanAdi: "a.com"}, func(string, ...any) {})
	if err != nil || n != 0 || len(uy) != 1 || len(kay.cagrilar) != 0 {
		t.Errorf("gpanel posta: n=%d uy=%v err=%v çağrı=%d", n, uy, err, len(kay.cagrilar))
	}
}

func TestPemAyir(t *testing.T) {
	// Blok başlıkları parçalanarak kurulur (sır tarayıcısı sahte anahtar bloğunu
	// gerçek anahtar sanmasın).
	blok := func(baslik, govde, ayrac string) string {
		return "-----BEGIN " + baslik + "-----" + ayrac + govde + ayrac + "-----END " + baslik + "-----" + ayrac
	}
	ozel := "PRIV" + "ATE KEY"
	girdi := "çöp\r\n" + blok("RSA "+ozel, "AAA", "\r\n") +
		blok("CERTIFICATE", "BBB", "\n") + "ara\n" +
		blok("CERTIFICATE", "CCC", "\n") +
		blok(ozel, "IKINCI", "\n")
	cert, key := pemAyir(girdi)
	if strings.Count(cert, "BEGIN CERTIFICATE") != 2 || strings.Contains(cert, "ara") || strings.Contains(cert, "çöp") {
		t.Errorf("sertifika zinciri: %q", cert)
	}
	if !strings.Contains(key, "AAA") || strings.Contains(key, "IKINCI") {
		t.Errorf("yalnız ilk özel anahtar alınmalı: %q", key)
	}
	if c, k := pemAyir("yok"); c != "" || k != "" {
		t.Error("PEM yoksa boş dönmeli")
	}
}

func TestBayiSatirSuz(t *testing.T) {
	for _, c := range []struct{ girdi, yasak string }{
		{"dosyalar aktariliyor: /var/www/vhosts/a.com/httpdocs → /home/c_a/public_html", "/home/"},
		{"ad alanı: bu alan adı zaten kayıtlı (sahibi: bayi #9)", "bayi #9"},
		{"saglama: nginx -t: [emerg] /etc/nginx/conf.d/komsu.conf", "komsu"},
		{"Offending key in /var/lib/girginospanel/tasima/7/known_hosts:3", "known_hosts"},
		{"geçersiz bayi (reseller_id=9)", "reseller_id"},
		{"rsync hata parola=gizli-parola-77", "gizli-parola-77"},
	} {
		got := bayiSatirSuz(c.girdi, "gizli-parola-77")
		if strings.Contains(got, c.yasak) {
			t.Errorf("%q → %q: %q sızdı", c.girdi, got, c.yasak)
		}
	}
	if got := bayiSatirSuz("TAMAM: a.com (1.0 MB, 1 DB, 2 DNS)"); got != "TAMAM: a.com (1.0 MB, 1 DB, 2 DNS)" {
		t.Errorf("zararsız satır değişmemeli: %q", got)
	}
	if got := bayiSatirSuz("──── [1/3] a.com ────"); !strings.Contains(got, "[1/3]") {
		t.Errorf("ilerleme satırı korunmalı: %q", got)
	}
}

func TestKapsamClaims(t *testing.T) {
	for _, c := range []struct {
		cl  auth.Claims
		kod int
		rid int64
	}{
		{auth.Claims{UserID: 1, Role: "admin"}, 0, 0},
		{auth.Claims{UserID: 1, Role: "admin", ResellerID: 7}, 0, 7},
		{auth.Claims{UserID: 7, Role: "reseller", ResellerID: 7}, 0, 7},
		{auth.Claims{UserID: 7, Role: "reseller"}, http.StatusForbidden, 0},
		{auth.Claims{UserID: 7, Role: "reseller", ResellerID: -3}, http.StatusForbidden, 0},
		{auth.Claims{UserID: 5, Role: "musteri"}, http.StatusForbidden, 0},
	} {
		cl := c.cl
		ik, kod, _ := kapsamClaims(&cl)
		if kod != c.kod || (kod == 0 && ik.RID != c.rid) {
			t.Errorf("%+v → kod=%d rid=%d, beklenen kod=%d rid=%d", c.cl, kod, ik.RID, c.kod, c.rid)
		}
	}
	if kimlikBaglam(Kapsam{RID: 7}, "h.test") == kimlikBaglam(Kapsam{}, "h.test") ||
		kimlikBaglam(Kapsam{RID: 7}, "h.test") == kimlikBaglam(Kapsam{RID: 8}, "h.test") {
		t.Error("kimlik bağlamı kiracıya özgü olmalı")
	}
	if kimlikBaglam(Kapsam{}, "h.test") != "h.test" {
		t.Error("kök bağlamı eski değer (host) kalmalı — eski satırlar çözülsün")
	}
}

func TestBayiHizIzni(t *testing.T) {
	eski := hizSaat
	simdi := time.Unix(1_800_000_000, 0)
	hizSaat = func() time.Time { return simdi }
	t.Cleanup(func() { hizSaat = eski; hizMu.Lock(); hizKovas = map[int64]*hizKova{}; hizMu.Unlock() })
	for i := 0; i < bayiHizSiniri; i++ {
		if !bayiHizIzni(Kapsam{RID: 41}) {
			t.Fatalf("%d. istek reddedilmemeli", i+1)
		}
	}
	if bayiHizIzni(Kapsam{RID: 41}) {
		t.Error("sınırdan sonraki istek reddedilmeli")
	}
	if !bayiHizIzni(Kapsam{RID: 42}) || !bayiHizIzni(Kapsam{}) {
		t.Error("başka bayi ve kök etkilenmemeli")
	}
	simdi = simdi.Add(bayiHizPencere)
	if !bayiHizIzni(Kapsam{RID: 41}) {
		t.Error("pencere bitince yeniden izin verilmeli")
	}
}

func TestHedefHazirla_BayiYalnizGenelIP(t *testing.T) {
	eskiC, eskiK, eskiD := hedefCozucu, hedefKendiAdresleri, tasimaKeyDir
	tasimaKeyDir = t.TempDir()
	var yanit []string
	hedefCozucu = func(context.Context, string) ([]net.IPAddr, error) {
		var out []net.IPAddr
		for _, s := range yanit {
			out = append(out, net.IPAddr{IP: net.ParseIP(s)})
		}
		return out, nil
	}
	hedefKendiAdresleri = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("95.216.1.10"), Mask: net.CIDRMask(32, 32)}}, nil
	}
	t.Cleanup(func() { hedefCozucu, hedefKendiAdresleri, tasimaKeyDir = eskiC, eskiK, eskiD })

	for _, c := range []struct {
		ad    string
		host  string
		ipler []string
		izin  bool
	}{
		{"genel-ad", "kaynak.example", []string{"93.184.216.34"}, true},
		{"genel-ip", "93.184.216.34", nil, true},
		{"genel-v6", "kaynak6.example", []string{"2a01:4f8::1"}, true},
		{"rfc1918", "ic.example", []string{"10.0.0.5"}, false},
		{"loopback", "lo.example", []string{"127.0.0.1"}, false},
		{"metadata", "md.example", []string{"169.254.169.254"}, false},
		{"cgnat", "cg.example", []string{"100.64.1.1"}, false},
		{"ula", "ula.example", []string{"fd00::1"}, false},
		{"karisik", "mix.example", []string{"93.184.216.34", "192.168.1.1"}, false},
		{"kendi", "ben.example", []string{"95.216.1.10"}, false},
		{"literal-ozel", "172.16.0.1", nil, false},
		{"bos", "bos.example", nil, false},
	} {
		yanit = c.ipler
		k := &Kaynak{Host: c.host}
		err := k.hedefHazirla(context.Background(), Kapsam{RID: 7})
		if c.izin != (err == nil) {
			t.Errorf("%s: izin=%v, hata=%v", c.ad, c.izin, err)
			continue
		}
		if err != nil {
			if !errors.Is(err, errHedefIzinsiz) {
				t.Errorf("%s: bayiye sabit ret metni dönmeli: %v", c.ad, err)
			}
			continue
		}
		if k.baglanIP == "" || !k.bayi || k.knownHosts != filepath.Join(tasimaKeyDir, "7", "known_hosts") {
			t.Errorf("%s: bağlantı IP/known_hosts kiracıya özel olmalı: %+v", c.ad, k)
		}
		args := strings.Join(k.sshOrtakArgs(""), " ")
		if !strings.Contains(args, "HostKeyAlias="+c.host) || !strings.Contains(args, "UserKnownHostsFile="+k.knownHosts) {
			t.Errorf("%s: ssh argümanları: %s", c.ad, args)
		}
	}
	// Kök: çözümleme yok, eski known_hosts, HostKeyAlias yok.
	yanit = []string{"10.0.0.5"}
	k := &Kaynak{Host: "ic.example"}
	if err := k.hedefHazirla(context.Background(), Kapsam{}); err != nil || k.baglanIP != "" || k.knownHosts != kokKnownHosts {
		t.Errorf("kök hedefi değişmemeli: %+v err=%v", k, err)
	}
	if strings.Contains(strings.Join(k.sshOrtakArgs(""), " "), "HostKeyAlias") {
		t.Error("kökte HostKeyAlias eklenmemeli")
	}
	// IPv6 rsync hedefi köşeli parantezle.
	if got := (&Kaynak{baglanIP: "2a01:4f8::1"}).rsyncHedefi(); got != "[2a01:4f8::1]" {
		t.Errorf("rsync v6 hedefi: %q", got)
	}
}

func TestSSHHataYorumla_BayiYolGostermez(t *testing.T) {
	k := &Kaynak{bayi: true, knownHosts: "/var/lib/girginospanel/tasima/7/known_hosts", Parola: "pw-1234"}
	for _, stderr := range []string{
		"@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! Host key verification failed. Offending key in /var/lib/girginospanel/tasima/7/known_hosts:2",
		"rsync: open \"/home/c_komsu/x\" failed: pw-1234",
	} {
		got := k.sshHataYorumla(stderr)
		if strings.Contains(got, "/var/lib") || strings.Contains(got, "/home/") || strings.Contains(got, "pw-1234") {
			t.Errorf("bayi hata metni panel yolu/sır içeriyor: %q", got)
		}
	}
	kok := &Kaynak{}
	if got := kok.sshHataYorumla("Host key verification failed"); !strings.Contains(got, kokKnownHosts) {
		t.Errorf("kök eski yönlendirmeyi görmeli: %q", got)
	}
}

// Orijinal DB kimliği yolu (kaynağın wp-config'indeki DÜŞMAN ad/kullanıcı/parola)
// bayide HİÇ denenmez; kökte denenir. Canlı MariaDB'ye dokunulmaz: yapılandırma
// okuyucusu ve karar sahte, DB erişilemez bir adrese açıktır (geri dönüş yolu hata
// verip döner).
func TestVeritabanlari_BayideOrijinalKimlikYoluKapali(t *testing.T) {
	eskiS, eskiK, eskiO := sandboxKontrol, configtenDBKimlikOku, orijinalKimlikDene
	t.Cleanup(func() { sandboxKontrol, configtenDBKimlikOku, orijinalKimlikDene = eskiS, eskiK, eskiO })
	sandboxKontrol = func() bool { return true }
	configtenDBKimlikOku = func(string) map[string]dbKimlik {
		return map[string]dbKimlik{"ahmet_wp": {Kul: "ahmet_u", Pw: "kaynak-pw"}}
	}
	cagri := 0
	orijinalKimlikDene = func(*Handlers, context.Context, int64, string, dbKimlik) bool { cagri++; return false }
	db, err := sql.Open("mysql", "yok:yok@tcp(127.0.0.1:1)/yok?timeout=1s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := &Handlers{DB: db}
	hs := Hesap{KaynakHesap: "ahmet", DBler: []string{"ahmet_wp"}}
	_, _, _ = h.veritabanlariniAktar(context.Background(), &Kaynak{}, hs, "c_x", "/nonexistent", Kapsam{RID: 7}, &AktarSonuc{}, func(string, ...any) {})
	if cagri != 0 {
		t.Errorf("bayide orijinal kimlik yolu denenmemeli (%d çağrı)", cagri)
	}
	_, _, _ = h.veritabanlariniAktar(context.Background(), &Kaynak{}, hs, "c_x", "/nonexistent", Kapsam{}, &AktarSonuc{}, func(string, ...any) {})
	if cagri != 1 {
		t.Errorf("kökte orijinal kimlik yolu denenmeli (%d çağrı)", cagri)
	}
}
