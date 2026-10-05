package gdrive

// ortak_test.go — ortak (GirginOS'a ait) OAuth istemcisi: kaynak önceliği,
// dosya izni/sahiplik reddi, secret sızıntısı yok, Yeni() ortak çözümü ve
// istemci kimliği değişince "yeniden yetkilendirme".

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/backups/hedef"
)

const (
	oSirDosya = "SAHTE-ORTAK-DOSYA-0001"
	oIDDosya  = "ortak-dosya-111.apps.googleusercontent.com"
	oSirGomu  = "SAHTE-ORTAK-GOMULU-0002"
	oIDGomulu = "ortak-gomulu-222.apps.googleusercontent.com"
)

// ortakKur — gömülü değerleri temizler, dosya yolunu geçici dizine yönlendirir.
func ortakKur(t *testing.T, icerik string, mod os.FileMode) string {
	t.Helper()
	eskiID, eskiSecret, eskiYol := GomuluClientID, GomuluClientSecret, OrtakDosyaYolu
	t.Cleanup(func() { GomuluClientID, GomuluClientSecret, OrtakDosyaYolu = eskiID, eskiSecret, eskiYol })
	GomuluClientID, GomuluClientSecret = "", ""
	yol := filepath.Join(t.TempDir(), "gdrive-istemci.json")
	OrtakDosyaYolu = yol
	if icerik != "" {
		if err := os.WriteFile(yol, []byte(icerik), mod); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(yol, mod) // umask'tan bağımsız
	}
	return yol
}

func dosyaIcerik(id, secret string) string {
	b, _ := json.Marshal(map[string]string{"client_id": id, "client_secret": secret})
	return string(b)
}

func TestOrtakYoksaErrOrtakYok(t *testing.T) {
	ortakKur(t, "", 0)
	if _, err := OrtakIstemci(); !errors.Is(err, ErrOrtakYok) {
		t.Fatalf("dosya yoksa ErrOrtakYok bekleniyordu: %v", err)
	}
	if OrtakHazir() {
		t.Error("ortak yokken OrtakHazir true")
	}
}

func TestOrtakDosyadanOkunur(t *testing.T) {
	ortakKur(t, dosyaIcerik(oIDDosya, oSirDosya), 0o600)
	k, err := OrtakIstemci()
	if err != nil || k.ClientID != oIDDosya || k.ClientSecret != oSirDosya {
		t.Fatalf("dosyadan okunamadı: %v", err)
	}
	if !OrtakHazir() {
		t.Error("OrtakHazir false")
	}
}

func TestOrtakDosyaIzniGevsekseReddedilir(t *testing.T) {
	for _, mod := range []os.FileMode{0o644, 0o640, 0o604, 0o660, 0o666} {
		ortakKur(t, dosyaIcerik(oIDDosya, oSirDosya), mod)
		k, err := OrtakIstemci()
		if !errors.Is(err, ErrOrtakGuvensiz) {
			t.Errorf("mod %o: ErrOrtakGuvensiz bekleniyordu: %v", mod, err)
		}
		if k.ClientSecret != "" || strings.Contains(err.Error(), oSirDosya) {
			t.Errorf("mod %o: reddedilen dosyadan değer sızdı", mod)
		}
	}
}

func TestOrtakDosyaSymlinkReddedilir(t *testing.T) {
	yol := ortakKur(t, dosyaIcerik(oIDDosya, oSirDosya), 0o600)
	link := yol + ".link"
	if err := os.Symlink(yol, link); err != nil {
		t.Skip("symlink yok")
	}
	OrtakDosyaYolu = link
	if _, err := OrtakIstemci(); err == nil {
		t.Fatal("symlink ortak dosya KABUL edildi")
	}
}

func TestOrtakDosyaBozukVeBilinmeyenAlan(t *testing.T) {
	for _, icerik := range []string{
		`not json`,
		`{"client_id":"` + oIDDosya + `","client_secret":"` + oSirDosya + `","token_url":"https://evil.example"}`,
		dosyaIcerik("kotu-id", oSirDosya),
		dosyaIcerik(oIDDosya, "kisa"),
	} {
		ortakKur(t, icerik, 0o600)
		_, err := OrtakIstemci()
		if !errors.Is(err, ErrOrtakBozuk) {
			t.Errorf("ErrOrtakBozuk bekleniyordu (%.40s): %v", icerik, err)
		}
		if err != nil && strings.Contains(err.Error(), oSirDosya) {
			t.Error("hata metnine secret sızdı")
		}
	}
}

func TestOrtakGomuluOncelikliVeBozukGomuluDosyayaDuser(t *testing.T) {
	ortakKur(t, dosyaIcerik(oIDDosya, oSirDosya), 0o600)
	GomuluClientID, GomuluClientSecret = oIDGomulu, oSirGomu
	k, err := OrtakIstemci()
	if err != nil || k.ClientID != oIDGomulu || k.ClientSecret != oSirGomu {
		t.Fatalf("gömülü öncelikli olmalı: %v", err)
	}
	// Gömülü bozuk (biçim) → dosya kullanılır.
	GomuluClientID = "bozuk"
	k, err = OrtakIstemci()
	if err != nil || k.ClientID != oIDDosya {
		t.Fatalf("bozuk gömülü dosyaya düşmeli: %v", err)
	}
	// Gömülü bozuk ve dosya da yok → ErrOrtakBozuk (sessiz "yok" değil).
	_ = os.Remove(OrtakDosyaYolu)
	if _, err := OrtakIstemci(); !errors.Is(err, ErrOrtakBozuk) {
		t.Fatalf("bozuk gömülü + dosya yok: ErrOrtakBozuk bekleniyordu: %v", err)
	}
}

func TestMaskeID(t *testing.T) {
	m := MaskeID(oIDDosya)
	if m != "ortak-do…" || strings.Contains(m, "111") {
		t.Errorf("maske: %q", m)
	}
	if MaskeID("kisa") != "kisa" {
		t.Error("kısa kimlik değişmemeli")
	}
}

func TestYeniOrtakIstemciSecretSirlardaYok(t *testing.T) {
	ortakKur(t, dosyaIcerik(oIDDosya, oSirDosya), 0o600)
	ayar := json.RawMessage(`{"client_id":"` + oIDDosya + `","istemci":"ortak"}`)
	// sirlarda client_secret YOK, yalnız yenileme jetonu.
	h, err := hedef.Kur(TurAdi, ayar, map[string]string{SirYenileme: nobetYenileme}, hedef.KurSecenek{})
	if err != nil || h.Tur() != "drive" {
		t.Fatalf("ortak istemciyle kurulamadı: %v", err)
	}
	if dh, ok := h.(*driveHedef); !ok || dh.kimlik.ClientSecret != oSirDosya {
		t.Fatal("ortak secret çalışma anında çözülmedi")
	}
}

func TestYeniOrtakYoksaVeIstemciDegisirse(t *testing.T) {
	yol := ortakKur(t, "", 0)
	ayar := json.RawMessage(`{"client_id":"` + oIDDosya + `","istemci":"ortak"}`)
	sirlar := map[string]string{SirYenileme: nobetYenileme}
	_, err := hedef.Kur(TurAdi, ayar, sirlar, hedef.KurSecenek{})
	if err == nil || !errors.Is(err, ErrOrtakYok) || hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Fatalf("ortak yok: kimlik kategorisi + ErrOrtakYok bekleniyordu: %v", err)
	}
	// Ortak istemci kimliği değişti → sessiz hata değil, kimlik (yeniden yetkilendirme).
	if err := os.WriteFile(yol, []byte(dosyaIcerik("baska-id-333.apps.googleusercontent.com", oSirDosya)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = hedef.Kur(TurAdi, ayar, sirlar, hedef.KurSecenek{})
	if err == nil || hedef.KategoriAl(err) != hedef.KategoriKimlik {
		t.Fatalf("kimlik değişimi kimlik hatası vermeli: %v", err)
	}
	if strings.Contains(err.Error(), oSirDosya) {
		t.Error("hata metnine secret sızdı")
	}
	if h, ok := err.(*hedef.Hata); ok && strings.Contains(h.Ayrinti(), oSirDosya) {
		t.Error("hata ayrıntısına secret sızdı")
	}
}

func TestAyarIstemciTuruDogrulanir(t *testing.T) {
	if _, err := AyarCoz(json.RawMessage(`{"client_id":"` + oIDDosya + `","istemci":"ortak"}`)); err != nil {
		t.Fatalf("ortak ayar: %v", err)
	}
	if _, err := AyarCoz(json.RawMessage(`{"client_id":"` + oIDDosya + `","istemci":"baska"}`)); err == nil {
		t.Error("bilinmeyen istemci türü kabul edildi")
	}
}
