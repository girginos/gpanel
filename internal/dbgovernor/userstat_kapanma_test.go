package dbgovernor

import "testing"

// userstat açıkken dışarıdan kapatılırsa (SET GLOBAL userstat=0) kısıcı bunu SONRAKİ turda görür, yeniden
// açar ve durum doğru kalır. Eskiden açıkken 40 turda bir bakılıyordu: ~10 dk sayaçlar donuk kalıyor, kısıt
// "yük bitti" sanılıp soğumayla kalkıyor ve durum hâlâ "acik" görünüyordu (VM-B ölçümü).
func TestUserstatDisaridanKapatilincaHemenAcilir(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	if d := o.m.DurumAl(); d.Userstat != "acik" {
		t.Fatalf("başlangıçta açık olmalı: %+v", d)
	}
	o.calis(3, nil)
	o.yon.mu.Lock()
	o.yon.userstat = false
	o.yon.mu.Unlock()
	o.calis(1, nil)
	o.yon.mu.Lock()
	acik := o.yon.userstat
	o.yon.mu.Unlock()
	if !acik || o.m.DurumAl().Userstat != "acik" {
		t.Fatalf("bir tur içinde yeniden açılmalı: userstat=%v durum=%+v", acik, o.m.DurumAl())
	}
}
