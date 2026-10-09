package provisioner

// tenantfpm_guard_aski_test.go — async post-start guard ↔ hesap askısı yarışı (VPS provası 2026-10-09).
//
// Gözlenen hata (yayınlı 20261008'de de VAR): PHP ayarı kaydedilir (guard 15 sn izler), hemen ardından hesap
// askıya alınıp geri açılırsa guard servisin BİLEREK durdurulmasını "temiz çıkış / crash-loop" sanıyor ve
// tenant'ı paylaşılan FPM'e indiriyordu: izolasyon kaybı + guard'ın İLK kaydın (bayat) PHP sürümüyle rollback
// etmesi → DB 8.3 derken site 8.2 sunuyordu. 6 turluk VPS provasında 1 turda tetiklendi (askı 3 sn sürerse).
//
// Bu testler systemctl'e / gerçek askı durumuna dokunmaz: dikişler (guard*) sahtedir, unit dosyası geçici dizindedir.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const gk_sk = "c_guardk"

var (
	gk_olu     = map[string]string{"NRestarts": "0", "ActiveState": "inactive", "SubState": "dead", "Result": "success", "ExecMainCode": "1", "ExecMainStatus": "0", "LoadState": "loaded"}
	gk_aktif   = map[string]string{"NRestarts": "0", "ActiveState": "active", "SubState": "running", "Result": "success", "ExecMainCode": "0", "ExecMainStatus": "0", "LoadState": "loaded"}
	gk_cokme   = map[string]string{"NRestarts": "0", "ActiveState": "activating", "SubState": "auto-restart", "Result": "exit-code", "ExecMainCode": "1", "ExecMainStatus": "1", "LoadState": "loaded"}
	gk_pencere = 400 * time.Millisecond
)

type gk_rb struct{ surum, sebep string }

type gk_sahte struct {
	mu        sync.Mutex
	unit      func(anahtarlar []string) map[string]string
	askida    func(cagri int) bool
	askidaN   int
	rollbacks []gk_rb
	surumDB   string
}

// gk_kur: guard dikişlerini sahtelere çevirir, unit dosyasını geçici dizinde oluşturur; Cleanup eskiyi geri koyar.
func gk_kur(t *testing.T) *gk_sahte {
	t.Helper()
	d := &gk_sahte{surumDB: "8.3"}
	dizin := t.TempDir()
	eskiDizin := tenantUnitDir
	tenantUnitDir = dizin
	if err := os.WriteFile(filepath.Join(dizin, tenantUnitName(gk_sk)), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eskiPoll, eskiBekle := guardPollAraligi, guardOluBekleme
	eskiProps, eskiAskida, eskiRb, eskiSurum := guardUnitProps, guardHesapAskida, guardRollback, guardGuncelSurum
	guardPollAraligi, guardOluBekleme = 2*time.Millisecond, 2*time.Millisecond
	guardUnitProps = func(_ string, anahtarlar ...string) map[string]string {
		d.mu.Lock()
		f := d.unit
		d.mu.Unlock()
		if f == nil {
			return nil
		}
		return f(anahtarlar)
	}
	guardHesapAskida = func(string) bool {
		d.mu.Lock()
		d.askidaN++
		n, f := d.askidaN, d.askida
		d.mu.Unlock()
		return f != nil && f(n)
	}
	guardRollback = func(_ *sql.DB, _ int64, _ string, surum string, sebep ...string) error {
		d.mu.Lock()
		d.rollbacks = append(d.rollbacks, gk_rb{surum, strings.Join(sebep, " ")})
		d.mu.Unlock()
		return nil
	}
	guardGuncelSurum = func(_ *sql.DB, _ int64, _ string) string { return d.surumDB }
	t.Cleanup(func() {
		tenantUnitDir = eskiDizin
		guardPollAraligi, guardOluBekleme = eskiPoll, eskiBekle
		guardUnitProps, guardHesapAskida, guardRollback, guardGuncelSurum = eskiProps, eskiAskida, eskiRb, eskiSurum
		fpmAskiGecis.Delete(gk_sk)
	})
	return d
}

func (d *gk_sahte) rb() []gk_rb {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]gk_rb(nil), d.rollbacks...)
}

func gk_hep(m map[string]string) func([]string) map[string]string {
	return func([]string) map[string]string { return m }
}

// ASKIYA ALINCA izleme bırakılır: servis askıdan SONRA ölü görünse de rollback YOK (yarışın asıl hâli).
func TestGuard_AskiGorununceIzlemeBirakilir_RollbackYok(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_olu)
	d.askida = func(int) bool { return true }
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err != nil {
		t.Fatalf("askıdaki hesapta guard hata dönmemeli: %v", err)
	}
	if got := d.rb(); len(got) != 0 {
		t.Fatalf("askıdaki hesap paylaşılan FPM'e İNDİRİLMEMELİ: %+v", got)
	}
}

// Askı, "ölü" onay döngüsünün ORTASINDA başlarsa (izleme askıdan önce "ölü"yü görmüş) onay iptal olur.
func TestGuard_AskiOnayDongusuOrtasindaBaslarsa_RollbackYok(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_olu)
	d.askida = func(n int) bool { return n >= 2 } // 1. çağrı (poll) askıda DEĞİL, sonrakiler askıda
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err != nil {
		t.Fatalf("guard hata dönmemeli: %v", err)
	}
	if got := d.rb(); len(got) != 0 {
		t.Fatalf("onay döngüsü sırasında askıya alınan hesap İNDİRİLMEMELİ: %+v", got)
	}
}

// Askı yok + servis GERÇEKTEN temiz çıkmış → rollback HÂLÂ yapılır (guard'ın asıl işi kaybolmamalı),
// üstelik DB'deki GÜNCEL sürümle (guard'ın başında yakaladığı bayat 8.2 ile DEĞİL).
func TestGuard_GercekTemizCikis_Rollback_GuncelSurumle(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_olu)
	d.askida = func(int) bool { return false }
	d.surumDB = "8.3"
	err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere)
	if err == nil {
		t.Fatal("gerçek temiz çıkışta guard hata dönmeli (tenant paylaşılana indirildi)")
	}
	got := d.rb()
	if len(got) != 1 {
		t.Fatalf("tam 1 rollback beklenirdi: %+v", got)
	}
	if got[0].surum != "8.3" {
		t.Fatalf("rollback DB'deki GÜNCEL sürümle (8.3) yapılmalı, bayat %q ile değil", got[0].surum)
	}
	if !strings.Contains(got[0].sebep, "clean_exit") {
		t.Fatalf("sebep clean_exit olmalı: %q", got[0].sebep)
	}
}

// Kilit beklenirken servis yeniden canlanmışsa (askıdan alma / yeniden başlatma) rollback YOK.
func TestGuard_KilitAltindaYenidenCanlandi_RollbackYok(t *testing.T) {
	d := gk_kur(t)
	d.askida = func(int) bool { return false }
	// izleme + 3 onay örneği ölü; kilit altındaki tek-anahtarlı ActiveState okuması canlı.
	d.unit = func(k []string) map[string]string {
		if len(k) == 1 && k[0] == "ActiveState" {
			return map[string]string{"ActiveState": "active"}
		}
		return gk_olu
	}
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err != nil {
		t.Fatalf("canlanan servis için guard hata dönmemeli: %v", err)
	}
	if got := d.rb(); len(got) != 0 {
		t.Fatalf("yeniden canlanan servis İNDİRİLMEMELİ: %+v", got)
	}
}

// Askıdan alma geçişi işaretliyken (enable --now sürüyor) "ölü" gözlemi rollback'e yol açmaz.
func TestGuard_AskidanAlmaGecisinde_RollbackYok(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_olu)
	d.askida = func(int) bool { return false }
	fpmAskiGecis.Store(gk_sk, struct{}{})
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err != nil {
		t.Fatalf("askı geçişinde guard hata dönmemeli: %v", err)
	}
	if got := d.rb(); len(got) != 0 {
		t.Fatalf("askı geçişindeki tenant İNDİRİLMEMELİ: %+v", got)
	}
}

// GERÇEK crash-loop (SubState auto-restart) askı kapısından ETKİLENMEZ: askıda değilse rollback yapılır.
func TestGuard_GercekCrashLoop_Rollback(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_cokme)
	d.askida = func(int) bool { return false }
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err == nil {
		t.Fatal("gerçek crash-loop'ta guard hata dönmeli")
	}
	got := d.rb()
	if len(got) != 1 || got[0].surum != "8.3" || !strings.Contains(got[0].sebep, "substate") {
		t.Fatalf("crash-loop rollback'i (substate, güncel sürüm) beklenirdi: %+v", got)
	}
}

// Sağlıklı servis: izleme pencere boyunca temiz kalır, rollback YOK.
func TestGuard_SaglikliServis_RollbackYok(t *testing.T) {
	d := gk_kur(t)
	d.unit = gk_hep(gk_aktif)
	d.askida = func(int) bool { return false }
	if err := guardPostStart(nil, 7, gk_sk, "8.2", gk_pencere); err != nil {
		t.Fatalf("sağlıklı serviste hata olmamalı: %v", err)
	}
	if got := d.rb(); len(got) != 0 {
		t.Fatalf("sağlıklı servis İNDİRİLMEMELİ: %+v", got)
	}
}

// Askı geçiş işareti tenantFPMAski ile konur ve İŞ BİTİNCE kalkar (iki yönde de).
func TestTenantFPMAski_GecisIsaretiKonurVeKalkar(t *testing.T) {
	for _, askida := range []bool{true, false} {
		gorulen := false
		eskiK, eskiS, eskiP := askiKomut, askiSoketBekle, askiPaylasilana
		askiKomut = func(string, ...string) ([]byte, error) {
			gorulen = gorulen || askiGecisteMi(gk_sk)
			return nil, nil
		}
		askiSoketBekle = func(string) bool { return true }
		askiPaylasilana = func(*sql.DB, int64, string, string, ...string) error { return nil }
		if err := tenantFPMAski(nil, 7, gk_sk, "8.3", askida); err != nil {
			t.Fatalf("askida=%v: %v", askida, err)
		}
		askiKomut, askiSoketBekle, askiPaylasilana = eskiK, eskiS, eskiP
		if !gorulen {
			t.Errorf("askida=%v: systemctl çağrısı sırasında askı geçiş işareti YOKTU", askida)
		}
		if askiGecisteMi(gk_sk) {
			t.Errorf("askida=%v: iş bitince askı geçiş işareti KALMAMALI", askida)
		}
		if _, kaldi := fpmInflight.Load(gk_sk); kaldi {
			t.Errorf("askida=%v: iş bitince fpmInflight KALMAMALI", askida)
		}
	}
}

// Varsayılan dikiş (guardAskidaMi) GERÇEK askı durumuna bağlı: askıdaysa true, değilse false,
// durum OKUNAMIYORSA da true (belirsizlikte rollback yok).
func TestGuardAskidaMi_GercekDurum(t *testing.T) {
	db := ha_panel(t) // domains(id=1, sk=ha_sk, askida)
	if guardAskidaMi(ha_sk) {
		t.Fatal("askıda olmayan hesap için false beklenirdi")
	}
	ha_askiya(t, db, 1)
	if !guardAskidaMi(ha_sk) {
		t.Fatal("askıdaki hesap için true beklenirdi")
	}
	_ = db.Close()
	if !guardAskidaMi(ha_sk) {
		t.Fatal("askı durumu okunamıyorsa true (rollback yok) beklenirdi")
	}
}

// domainGuncelPHPSurum DB'den OKUR; okunamaz/boşsa yedeği döner.
func TestDomainGuncelPHPSurum(t *testing.T) {
	db := ha_panel(t)
	if _, err := db.Exec(`ALTER TABLE domains ADD COLUMN php_surum VARCHAR(8) NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	if got := domainGuncelPHPSurum(db, 1, "8.2"); got != "8.2" {
		t.Fatalf("boş sütunda yedek beklenirdi, %q geldi", got)
	}
	if _, err := db.Exec(`UPDATE domains SET php_surum='8.3' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if got := domainGuncelPHPSurum(db, 1, "8.2"); got != "8.3" {
		t.Fatalf("DB'deki güncel sürüm (8.3) beklenirdi, %q geldi", got)
	}
	if got := domainGuncelPHPSurum(db, 999, "8.2"); got != "8.2" {
		t.Fatalf("olmayan domain için yedek beklenirdi, %q geldi", got)
	}
	if got := domainGuncelPHPSurum(nil, 1, "8.2"); got != "8.2" {
		t.Fatalf("db=nil için yedek beklenirdi, %q geldi", got)
	}
	_ = db.Close()
	if got := domainGuncelPHPSurum(db, 1, "8.2"); got != "8.2" {
		t.Fatalf("DB kapalıyken yedek beklenirdi, %q geldi", got)
	}
}
