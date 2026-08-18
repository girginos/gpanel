package avmotor

import (
	"os"
	"testing"
)

// 🔴 dogrulaVeCoz gercek uretim anahtariyla imzalanmis paketi cozer mi.
// /tmp/kurallar.gospav gosp-avkural ile uretildi (uretim anahtari + gomulu pubkey).
func TestUzakPaketDogrulama(t *testing.T) {
	ham, err := os.ReadFile("/tmp/kurallar.gospav")
	if err != nil {
		t.Skip("test paketi yok (once gosp-avkural ile uret)")
	}
	set, _, ok := dogrulaVeCoz(ham)
	if !ok {
		t.Fatal("gercek imzali paket DOGRULANMADI — pubkey uyusmuyor olabilir")
	}
	if set.Surum != 1 {
		t.Errorf("surum 1 bekleniyor, %d", set.Surum)
	}
	if len(set.Kurallar) == 0 {
		t.Error("kurallar bos")
	}
	t.Logf("dogrulandi: surum %d, %d kural", set.Surum, len(set.Kurallar))
}

// 🔴 KURCALANMIS paket REDDEDILMELI (sahte kural enjeksiyonu savunmasi).
func TestUzakPaketKurcalama(t *testing.T) {
	ham, err := os.ReadFile("/tmp/kurallar.gospav")
	if err != nil {
		t.Skip("test paketi yok")
	}
	k := make([]byte, len(ham))
	copy(k, ham)
	k[len(k)-3] ^= 0xff // sifreli govde
	if _, _, ok := dogrulaVeCoz(k); ok {
		t.Fatal("kurcalanmis paket KABUL edildi — imza kapisi acik")
	}
}

// 🔴 GuncelSet: paket yoksa/dogrulanmazsa TABAN sete duser (asla bos).
func TestGuncelSetTabanaDuser(t *testing.T) {
	// aginenKapali=true → uzak denenmez; disk de yoksa taban.
	set := GuncelSet(0, true)
	if len(set.Kurallar) == 0 {
		t.Fatal("GuncelSet bos set dondurdu — taban sete dusmedi")
	}
	t.Logf("taban set: %d kural", len(set.Kurallar))
}
