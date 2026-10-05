package subdomain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tasimaKur — geçici ev dizini; hesap ev dizini + eski klasör altında verilen alt klasörler.
func tasimaKur(t *testing.T, alanlar ...string) (sk string, eski, yeni string, liste []ekTasimaAlan) {
	t.Helper()
	ek_dikisler(t)
	sk = ek_sk1
	eski = evKoku + "/" + sk + "/" + ekAlanEskiKlasor
	yeni = ekDocrootTaban(sk)
	for i, a := range alanlar {
		if err := os.MkdirAll(eski+"/"+a+"/public_html", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(eski+"/"+a+"/public_html/index.php", []byte("veri-"+a), 0o644); err != nil {
			t.Fatal(err)
		}
		liste = append(liste, ekTasimaAlan{ID: int64(i + 1), Ad: a})
	}
	return
}

func tasimaTamam(ctx context.Context, id int64) error { return nil }

func okuS(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func yokMu(p string) bool { _, err := os.Lstat(p); return os.IsNotExist(err) }

func TestEkKlasorTasi_HedefYok_ButunRename(t *testing.T) {
	sk, eski, yeni, l := tasimaKur(t, "a.example.org", "b.example.org")
	n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if okuS(t, yeni+"/a.example.org/public_html/index.php") != "veri-a.example.org" {
		t.Fatal("veri taşınmadı")
	}
	if !yokMu(eski) {
		t.Fatal("eski klasör kalmamalı")
	}
	if n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam); n != 0 || err != nil {
		t.Fatalf("ikinci koşu no-op olmalı: %d %v", n, err)
	}
}

func TestEkKlasorTasi_HedefVar_Cakismasiz(t *testing.T) {
	sk, eski, yeni, l := tasimaKur(t, "a.example.org")
	_ = os.MkdirAll(yeni+"/kiraci-klasoru", 0o755)
	_ = os.WriteFile(yeni+"/kiraci-klasoru/x", []byte("kiraci"), 0o644)
	n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if okuS(t, yeni+"/a.example.org/public_html/index.php") != "veri-a.example.org" || okuS(t, yeni+"/kiraci-klasoru/x") != "kiraci" {
		t.Fatal("taşıma/kiracı verisi bozuldu")
	}
	if !yokMu(eski) {
		t.Fatal("boşalan eski klasör kaldırılmalı")
	}
}

func TestEkKlasorTasi_Cakisma_VeriEzilmez(t *testing.T) {
	sk, eski, yeni, l := tasimaKur(t, "a.example.org", "b.example.org")
	_ = os.MkdirAll(yeni+"/b.example.org", 0o755)
	_ = os.WriteFile(yeni+"/b.example.org/hedef", []byte("hedef-veri"), 0o644)
	n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam)
	if err == nil || n != 0 || !strings.Contains(err.Error(), "çakışma") {
		t.Fatalf("çakışma hata vermeli: %d %v", n, err)
	}
	if yokMu(eski+"/a.example.org") || !yokMu(yeni+"/a.example.org") {
		t.Fatal("çakışmada kısmi taşıma yapıldı")
	}
	if okuS(t, yeni+"/b.example.org/hedef") != "hedef-veri" || okuS(t, eski+"/b.example.org/public_html/index.php") != "veri-b.example.org" {
		t.Fatal("çakışmada veri değişti")
	}
}

func TestEkKlasorTasi_EslesmeyenYerindeKalir(t *testing.T) {
	sk, eski, yeni, l := tasimaKur(t, "a.example.org")
	_ = os.MkdirAll(eski+"/kiracinin-kendi", 0o755)
	_ = os.WriteFile(eski+"/kiracinin-kendi/dosya", []byte("k"), 0o644)
	_ = os.WriteFile(eski+"/duz-dosya", []byte("d"), 0o644)
	_ = os.MkdirAll(eski+"/kayitsiz.example.org", 0o755)
	n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, p := range []string{eski + "/kiracinin-kendi/dosya", eski + "/duz-dosya", eski + "/kayitsiz.example.org"} {
		if yokMu(p) {
			t.Fatalf("eşleşmeyen içerik taşındı/silindi: %s", p)
		}
	}
	if yokMu(yeni+"/a.example.org") || !yokMu(yeni+"/kiracinin-kendi") {
		t.Fatal("yalnız kayıtlı alan taşınmalı")
	}
}

func TestEkKlasorTasi_SymlinkTasinmaz(t *testing.T) {
	sk, eski, yeni, _ := tasimaKur(t)
	dis := t.TempDir()
	_ = os.WriteFile(dis+"/dokunma", []byte("x"), 0o644)
	_ = os.MkdirAll(eski, 0o755)
	if err := os.Symlink(dis, eski+"/s.example.org"); err != nil {
		t.Fatal(err)
	}
	l := []ekTasimaAlan{{ID: 1, Ad: "s.example.org"}}
	n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if fi, e := os.Lstat(eski + "/s.example.org"); e != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink yerinde kalmalı")
	}
	if !yokMu(yeni + "/s.example.org") {
		t.Fatal("symlink taşındı")
	}
	// eski klasörün kendisi symlink
	_ = os.RemoveAll(eski)
	if err := os.Symlink(dis, eski); err != nil {
		t.Fatal(err)
	}
	if n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam); err == nil || n != 0 {
		t.Fatalf("sembolik eski klasör reddedilmeli: %d %v", n, err)
	}
	if !yokMu(yeni) {
		t.Fatal("hedef oluşturuldu")
	}
	// hedef taban symlink → atla
	_ = os.Remove(eski)
	_ = os.MkdirAll(eski+"/s.example.org/public_html", 0o755)
	if err := os.Symlink(dis, yeni); err != nil {
		t.Fatal(err)
	}
	if n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam); err == nil || n != 0 {
		t.Fatalf("sembolik hedef taban reddedilmeli: %d %v", n, err)
	}
	if !yokMu(dis + "/s.example.org") {
		t.Fatal("dış dizine yazıldı (kaçış)")
	}
}

func TestEkKlasorTasi_BosKaynakKaldirilir(t *testing.T) {
	sk, eski, _, l := tasimaKur(t)
	_ = os.MkdirAll(eski, 0o755)
	if n, err := ekKlasorTasi(context.Background(), sk, l, tasimaTamam); n != 0 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if !yokMu(eski) {
		t.Fatal("boş kaynak kaldırılmalı")
	}
}

func TestEkKlasorTasi_HesapYok_GuvensizSK(t *testing.T) {
	tasimaKur(t)
	if n, err := ekKlasorTasi(context.Background(), "c_yokhesap", nil, tasimaTamam); n != 0 || err != nil {
		t.Fatalf("hesap dizini yok → no-op: %d %v", n, err)
	}
	if _, err := ekKlasorTasi(context.Background(), "../etc", nil, tasimaTamam); err == nil {
		t.Fatal("güvensiz sk reddedilmeli")
	}
}

func TestEkKlasorTasi_VhostHatasiGeriAlir(t *testing.T) {
	for _, mod := range []string{"butun", "tek"} {
		t.Run(mod, func(t *testing.T) {
			sk, eski, yeni, l := tasimaKur(t, "a.example.org", "b.example.org")
			if mod == "tek" {
				_ = os.MkdirAll(yeni+"/kiraci", 0o755)
			}
			for _, a := range l {
				if err := os.WriteFile(ekConfPath(sk, a.Ad), []byte("ESKI "+a.Ad), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cagri := 0
			kur := func(ctx context.Context, id int64) error {
				cagri++
				for _, a := range l {
					if a.ID == id {
						_ = os.WriteFile(ekConfPath(sk, a.Ad), []byte("YENI "+a.Ad), 0o644)
					}
				}
				if cagri == 2 {
					return errors.New("nginx -t başarısız")
				}
				return nil
			}
			n, err := ekKlasorTasi(context.Background(), sk, l, kur)
			if err == nil || n != 0 || !strings.Contains(err.Error(), "GERİ ALINDI") {
				t.Fatalf("geri alma beklenir: %d %v", n, err)
			}
			for _, a := range l {
				if okuS(t, eski+"/"+a.Ad+"/public_html/index.php") != "veri-"+a.Ad {
					t.Fatalf("%s eski yerine geri dönmedi", a.Ad)
				}
				if !yokMu(yeni + "/" + a.Ad) {
					t.Fatalf("%s yeni yerde kaldı", a.Ad)
				}
				if okuS(t, ekConfPath(sk, a.Ad)) != "ESKI "+a.Ad {
					t.Fatalf("%s vhost eski içeriğe dönmedi", a.Ad)
				}
			}
			if mod == "butun" && !yokMu(yeni) {
				t.Fatal("geri alma sonrası hedef taban kalmamalı")
			}
			if mod == "tek" && yokMu(yeni+"/kiraci") {
				t.Fatal("kiracı klasörü silindi")
			}
		})
	}
}

// Uçtan uca (DB'li): eski düzende kurulu ek alan → heal taşır, vhost yeni kökü gösterir,
// ikinci koşu nginx'e dokunmaz; taşıması atlanan alanın vhost'u HealEkAlanlar'ca bozulmaz.
func TestHealEkAlanKlasorTasima_UcAUca(t *testing.T) {
	db := ek_db(t)
	d := ek_dikisler(t)
	if kod, govde := ek_ekle(t, db, ek_dom1, "tasi.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	if kod, govde := ek_ekle(t, db, ek_dom1, "cakisan.example.org"); kod != 200 {
		t.Fatalf("kurulum: %d %s", kod, govde)
	}
	taban := ekDocrootTaban(ek_sk1)
	eski := evKoku + "/" + ek_sk1 + "/" + ekAlanEskiKlasor
	_ = os.WriteFile(taban+"/tasi.example.org/public_html/veri.txt", []byte("v"), 0o644)
	_ = os.MkdirAll(eski, 0o755)
	if err := os.Rename(taban+"/tasi.example.org", eski+"/tasi.example.org"); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(eski+"/cakisan.example.org", 0o755)
	_ = os.WriteFile(ekConfPath(ek_sk1, "tasi.example.org"), []byte("eski vhost"), 0o644)
	HealEkAlanKlasorTasima(db)
	if yokMu(eski+"/tasi.example.org") || !yokMu(taban+"/tasi.example.org") {
		t.Fatal("çakışmalı hesapta taşıma yapılmamalı")
	}
	HealEkAlanlar(db, "203.0.113.7")
	if okuS(t, ekConfPath(ek_sk1, "tasi.example.org")) != "eski vhost" {
		t.Fatal("taşıması atlanan alanın vhost'u HealEkAlanlar'ca değiştirildi")
	}
	_ = os.RemoveAll(eski + "/cakisan.example.org")
	HealEkAlanKlasorTasima(db)
	if okuS(t, taban+"/tasi.example.org/public_html/veri.txt") != "v" || !yokMu(eski) {
		t.Fatal("taşıma/eski klasör kaldırma beklenir")
	}
	if c := okuS(t, ekConfPath(ek_sk1, "tasi.example.org")); !strings.Contains(c, taban+"/tasi.example.org/public_html") || strings.Contains(c, ekAlanEskiKlasor) {
		t.Fatalf("vhost yeni kökü göstermeli:\n%s", c)
	}
	once := d.say("systemctl reload nginx")
	HealEkAlanKlasorTasima(db)
	HealEkAlanlar(db, "203.0.113.7")
	if d.say("systemctl reload nginx") != once {
		t.Fatal("ikinci koşu no-op olmalı (reload yok)")
	}
	_ = os.RemoveAll(taban + "/tasi.example.org")
	HealEkAlanlar(db, "203.0.113.7")
	if _, err := os.Stat(filepath.Join(taban, "tasi.example.org", "public_html", "index.html")); err != nil {
		t.Fatal("eksik docroot yeniden oluşturulmalı")
	}
}
