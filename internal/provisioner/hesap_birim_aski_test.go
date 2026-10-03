package provisioner

// hesap_birim_aski_test.go — askıda hesabın KENDİ systemd birimleri (User=<sk>:
// Uygulama Çalıştırıcı eklentisinin gosp-app-… birimleri, Laravel işleyicisi)
// durur ve hiçbir yoldan başlayamaz; askıdan alınca YALNIZ önceden etkin olan
// etkinleşir, önceden çalışan başlar. Birimler adından değil User= TAM
// eşitliğinden bulunur: komşu kiracıya (c_shop ≠ c_shop_store) dokunulmaz.
//
// Birim dizini geçici, komutlar sahte — ana makinenin systemd'sine dokunulmaz.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hb_ortam struct {
	dizin    string
	komutlar *[]string
	durum    map[string]string // "is-enabled <birim>" / "is-active <birim>" → çıktı
}

func hb_dikis(t *testing.T) hb_ortam {
	t.Helper()
	e1, e2 := askiKomut, hesapBirimDizini
	t.Cleanup(func() { askiKomut, hesapBirimDizini = e1, e2 })
	o := hb_ortam{dizin: t.TempDir(), komutlar: &[]string{}, durum: map[string]string{}}
	hesapBirimDizini = o.dizin
	askiKomut = func(ad string, arg ...string) ([]byte, error) {
		*o.komutlar = append(*o.komutlar, ad+" "+strings.Join(arg, " "))
		if len(arg) == 2 && (arg[0] == "is-enabled" || arg[0] == "is-active") {
			if s, ok := o.durum[arg[0]+" "+arg[1]]; ok {
				return []byte(s + "\n"), nil
			}
			return []byte("inactive\n"), errors.New("exit status 3")
		}
		return nil, nil
	}
	return o
}

func (o hb_ortam) birim(t *testing.T, ad, govde string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(o.dizin, ad), []byte(govde), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hb_govde(kul string) string {
	return "[Unit]\nDescription=GirginOS uygulama\n\n[Service]\nType=simple\nUser=" + kul + "\nGroup=" + kul +
		"\nExecStart=/bin/true\nRestart=on-failure\nRestartSec=3\n\n[Install]\nWantedBy=multi-user.target\n"
}

func (o hb_ortam) komutVar(k string) bool {
	for _, c := range *o.komutlar {
		if c == k {
			return true
		}
	}
	return false
}

const (
	hb_web    = "gosp-app-c_shop-web.service"     // etkin + çalışıyor
	hb_isci   = "gosp-app-c_shop-isci-i1.service" // devre dışı + durmuş
	hb_komsu  = "gosp-app-c_shop_store-web.service"
	hb_benzer = "gosp-app-c_shop-store-x.service" // adı c_shop'unki gibi, kullanıcısı komşu
)

func hb_kurulum(t *testing.T) hb_ortam {
	t.Helper()
	o := hb_dikis(t)
	o.birim(t, hb_web, hb_govde("c_shop"))
	o.birim(t, hb_isci, hb_govde("c_shop"))
	o.birim(t, hb_komsu, hb_govde("c_shop_store"))
	o.birim(t, hb_benzer, hb_govde("c_shop_store"))
	o.birim(t, "gosp-app-c_shop-sablon@.service", hb_govde("c_shop"))
	// User= yalnız [Service] bölümünde anlamlıdır.
	o.birim(t, "baska.service", "[Service]\nUser=c_diger\n[X-Ek]\nUser=c_shop\n")
	o.durum["is-enabled "+hb_web], o.durum["is-active "+hb_web] = "enabled", "active"
	o.durum["is-enabled "+hb_isci], o.durum["is-active "+hb_isci] = "disabled", "inactive"
	// Komşu kiracı da askıda: koruması c_shop'un işlemlerinden etkilenmemeli.
	if err := askiDropInYaz(birimAskiYolu(hb_komsu), birimAskiGovde(birimAskiKaydi{birim: hb_komsu, hesap: "c_shop_store", etkin: true, calisiyor: true})); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestHesapBirimleriAski_GidisDonus_KomsuyaDokunmaz(t *testing.T) {
	o := hb_kurulum(t)
	komsuKoruma := ha_oku(t, birimAskiYolu(hb_komsu))

	if err := HesapBirimleriAski("c_shop", true); err != nil {
		t.Fatalf("askıya alma: %v", err)
	}
	for b, w := range map[string][2]string{hb_web: {"1", "1"}, hb_isci: {"0", "0"}} {
		g := ha_oku(t, birimAskiYolu(b))
		for _, parca := range []string{"# hesap=c_shop\n", "# etkin=" + w[0] + "\n", "# calisiyor=" + w[1] + "\n",
			"[Unit]\nConditionPathExists=!" + birimAskiYolu(b) + "\n"} {
			if !strings.Contains(g, parca) {
				t.Fatalf("%s koruması %q içermeli:\n%s", b, parca, g)
			}
		}
		if !o.komutVar("systemctl disable --now " + b) {
			t.Fatalf("%s durdurulmalı: %v", b, *o.komutlar)
		}
	}
	reload, ilkDurdurma := -1, -1
	for i, k := range *o.komutlar {
		if k == "systemctl daemon-reload" && reload < 0 {
			reload = i
		}
		if strings.HasPrefix(k, "systemctl disable") && ilkDurdurma < 0 {
			ilkDurdurma = i
		}
		if strings.Contains(k, "c_shop_store") || strings.Contains(k, "c_shop-store") || strings.Contains(k, "@") || strings.Contains(k, "baska") {
			t.Fatalf("başka hesabın / şablon birimine dokunulmamalı: %q", k)
		}
	}
	if reload < 0 || reload > ilkDurdurma {
		t.Fatalf("koruma durdurmadan ÖNCE yürürlüğe girmeli (daemon-reload): %v", *o.komutlar)
	}
	for _, b := range []string{hb_benzer, "gosp-app-c_shop-sablon@.service", "baska.service"} {
		ha_yok(t, birimAskiYolu(b))
	}
	if ha_oku(t, birimAskiYolu(hb_komsu)) != komsuKoruma {
		t.Fatal("komşunun koruması değişmemeli")
	}

	// İdempotent: birim artık (bizim yüzümüzden) kapalı — saklanan durum EZİLMEZ.
	o.durum["is-enabled "+hb_web], o.durum["is-active "+hb_web] = "disabled", "inactive"
	*o.komutlar = nil
	if err := HesapBirimleriAski("c_shop", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ha_oku(t, birimAskiYolu(hb_web)), "# etkin=1\n# calisiyor=1\n") {
		t.Fatalf("yeniden askıya alma önceki durumu ezmemeli:\n%s", ha_oku(t, birimAskiYolu(hb_web)))
	}
	if o.komutVar("systemctl daemon-reload") || !o.komutVar("systemctl disable --now "+hb_web) {
		t.Fatalf("yeni koruma yokken reload gereksiz, durdurma yinelenir: %v", *o.komutlar)
	}

	*o.komutlar = nil
	if err := HesapBirimleriAski("c_shop", false); err != nil {
		t.Fatalf("askıdan alma: %v", err)
	}
	ha_yok(t, filepath.Dir(birimAskiYolu(hb_web)))
	ha_yok(t, filepath.Dir(birimAskiYolu(hb_isci)))
	for _, k := range []string{"systemctl daemon-reload", "systemctl enable " + hb_web, "systemctl start " + hb_web} {
		if !o.komutVar(k) {
			t.Fatalf("%q bekleniyordu: %v", k, *o.komutlar)
		}
	}
	for _, k := range *o.komutlar {
		if strings.Contains(k, hb_isci) || strings.Contains(k, "c_shop_store") {
			t.Fatalf("önceden kapalı birim açılmamalı / komşuya dokunulmamalı: %q", k)
		}
	}
	if ha_oku(t, birimAskiYolu(hb_komsu)) != komsuKoruma {
		t.Fatal("askıdan almada komşunun koruması kalmalı")
	}
}

// Askıdayken kaldırılan birim (uygulama silindi) askıdan alınınca başlatılmaz;
// koruması yine kalkar.
func TestHesapBirimleriAski_SilinenBirimBaslatilmaz(t *testing.T) {
	o := hb_kurulum(t)
	if err := HesapBirimleriAski("c_shop", true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.dizin, hb_web)); err != nil {
		t.Fatal(err)
	}
	*o.komutlar = nil
	if err := HesapBirimleriAski("c_shop", false); err != nil {
		t.Fatal(err)
	}
	ha_yok(t, birimAskiYolu(hb_web))
	for _, k := range *o.komutlar {
		if strings.Contains(k, hb_web) {
			t.Fatalf("kaldırılmış birim için komut çalışmamalı: %q", k)
		}
	}
}

// Hesap silinirken YALNIZ o hesabın korumaları kalkar (sk yeniden kullanılabilir).
func TestAskiBirimArtiklariniSil(t *testing.T) {
	o := hb_kurulum(t)
	if err := HesapBirimleriAski("c_shop", true); err != nil {
		t.Fatal(err)
	}
	*o.komutlar = nil
	askiBirimArtiklariniSil("c_shop")
	ha_yok(t, birimAskiYolu(hb_web))
	ha_yok(t, birimAskiYolu(hb_isci))
	if _, err := os.Stat(birimAskiYolu(hb_komsu)); err != nil {
		t.Fatalf("komşunun koruması kalmalı: %v", err)
	}
	if strings.Join(*o.komutlar, "|") != "systemctl daemon-reload" {
		t.Fatalf("yalnız daemon-reload beklenirdi (başlatma yok): %v", *o.komutlar)
	}
}

func TestHesapBirimleriAski_GecersizHesap(t *testing.T) {
	o := hb_kurulum(t)
	for _, sk := range []string{"root", "c_../x", ""} {
		if err := HesapBirimleriAski(sk, true); err == nil {
			t.Fatalf("%q reddedilmeli", sk)
		}
	}
	if len(*o.komutlar) != 0 {
		t.Fatalf("reddedilen istekte komut çalışmamalı: %v", *o.komutlar)
	}
}
