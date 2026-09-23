package kapi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGenelPanelPortuOku(t *testing.T) {
	d := t.TempDir()
	vhost, ayar := filepath.Join(d, "_panel.conf"), filepath.Join(d, "kapi.json")
	_ = os.WriteFile(vhost, []byte("listen 21001 ssl;\nlisten 8443 ssl;\nlisten 23003 ssl;"), 0o644)

	if p := genelPanelPortuOku(vhost, ayar); p != 21001 {
		t.Errorf("kapı yokken ilk genel listen, got %d", p)
	}
	_ = Yaz(ayar, ornekAyar())
	if p := genelPanelPortuOku(vhost, ayar); p != 23003 {
		t.Errorf("kapı modunda müşteri kapısı, got %d", p)
	}
	// Bozuk kapi.json: roller bilinmiyor → 0 (ilk listen admin olabilir; tahmin sızdırır).
	_ = os.WriteFile(ayar, []byte("{bozuk"), 0o600)
	if p := genelPanelPortuOku(vhost, ayar); p != 0 {
		t.Errorf("okunamayan kapi.json → 0, got %d", p)
	}
	if p := genelPanelPortuOku(filepath.Join(d, "yok.conf"), ayar); p != 0 {
		t.Errorf("vhost yoksa 0, got %d", p)
	}
}

func TestPanelPortlari(t *testing.T) {
	d := t.TempDir()
	vhost, ayar := filepath.Join(d, "_panel.conf"), filepath.Join(d, "kapi.json")
	_ = os.WriteFile(vhost, []byte("listen 7443 ssl;\nlisten 127.0.0.1:9443 ssl;"), 0o644)
	if got := panelPortlari(vhost, ayar, 0); !reflect.DeepEqual(got, []int{7443}) {
		t.Errorf("kapısız: yalnız genel listen (loopback değil), got %v", got)
	}
	a := ornekAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 100
	_ = Yaz(ayar, a)
	// Eski kapı açık (admin doğrulamadı) → 8443 de korunur; henüz nginx'e yazılmamış rol
	// portları da korunur (Heal eklemeden önce firewall'da kapatılmasın).
	want := []int{7443, 8443, 21001, 22002, 23003}
	if got := panelPortlari(vhost, ayar, 50); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
