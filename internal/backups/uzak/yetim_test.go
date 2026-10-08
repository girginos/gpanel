package uzak

// yetim_test.go — backups.domain_id FK'si kalktı (0217): domain silinince yedek satırı KALIR ve
// domain_id=0'a taşınır (yetim; eski kimlik eski_domain_id'de). Yetim satırlar uzak gönderim akışına
// (toplu aday, tekil kuyruğa alma), yerel-silme taramasına ve işçiye GİRMEMELİ; domain kimliği
// yeniden kullanılsa bile başka müşterinin dizinine çözülmemeli. 'kurtarma' tipi (silinmiş hesaptan
// bağlanan eski yedek) job_id taşısa bile uzağa gönderim adayı DEĞİLDİR.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// yetimYap — 0217 sonrası biçim: FK kalkar, satır domain_id=0'a taşınır.
func (o *ortam) yetimYap(yedekID int64) {
	o.t.Helper()
	_, _ = o.db.Exec(`ALTER TABLE backups DROP FOREIGN KEY fk_backup_domain`) // ikinci çağrıda zaten yok: hata yok sayılır
	if _, err := o.db.Exec(`UPDATE backups SET domain_id=0 WHERE id=?`, yedekID); err != nil {
		o.t.Fatal(err)
	}
}

func TestYetimYedek_UzakAkislaraGirmez(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar(nil)
	canli := o.yedek(1, "c_canli", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	yetim := o.yedek(2, "c_yetim", "20260901-020000", sahteIcerik(10, 2), "oto", nil)
	o.yetimYap(yetim)
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE id=?`, yetim).Scan(&n)
	if n != 1 {
		t.Fatal("kurulum: yetim satır kalmalı")
	}

	ids, on, err := TopluAdaylar(context.Background(), o.db, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != canli || on.Domain != 1 {
		t.Errorf("toplu aday yalnız canlı domainin yedeği olmalı: %v %+v", ids, on)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, yetim); !errors.Is(err, ErrYedekYok) {
		t.Errorf("yetim yedek kuyruğa alınmamalı (ErrYedekYok): %v", err)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, canli); err != nil {
		t.Errorf("canlı yedek kuyruğa alınabilmeli: %v", err)
	}
	kayitlar, err := yerelKayitlari(context.Background(), o.db, o.kok, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kayitlar {
		if k.yedekID == yetim {
			t.Error("yerel-silme taraması yetim yedeği içermemeli")
		}
	}
}

// S4: işçi, iş kaydındaki domain_id'yi (eski kimlik) domains'te çözer. Yedek yetimse (backups.domain_id=0)
// ya da sahibi değiştiyse, kimlik başka müşteriye yeniden verilmiş olsa bile dizin ÇÖZÜLMEZ: kalıcı hata.
func TestYetimYedek_IsciYerelYol_KimlikYenidenKullanilsaBileCozmez(t *testing.T) {
	o := yeniOrtam(t)
	ctx := context.Background()
	yid := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	var dosya string
	_ = o.db.QueryRow(`SELECT dosya FROM backups WHERE id=?`, yid).Scan(&dosya)
	is := &isSatir{id: 1, domainID: 1, yedekID: sql.NullInt64{Int64: yid, Valid: true}, yedekDosya: dosya}

	yol, h := o.isci.yerelYol(ctx, is)
	if h != nil || yol != filepath.Join(o.kok, "c_a", dosya) {
		t.Fatalf("pozitif kontrol: canlı yedek için yol çözülmeli: %q %v", yol, h)
	}

	// Domain silindi, yedek yetim (domain_id=0); AYNI kimlik başka müşteriye (sk c_baska) verildi.
	o.yetimYap(yid)
	if _, err := o.db.Exec(`DELETE FROM domains WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'baska.example','c_baska')`); err != nil {
		t.Fatal(err)
	}
	yol, h = o.isci.yerelYol(ctx, is)
	if h == nil || !h.kalici || yol != "" {
		t.Fatalf("🔴 yetim yedek yeniden kullanılan kimliğin dizinine çözüldü: %q %+v", yol, h)
	}

	// Sahibi başka domaine geçmiş satır (bağlama): iş kaydı eski domaini gösteriyorsa kalıcı hata.
	if _, err := o.db.Exec(`UPDATE backups SET domain_id=2 WHERE id=?`, yid); err != nil {
		t.Fatal(err)
	}
	if _, h = o.isci.yerelYol(ctx, is); h == nil || !h.kalici {
		t.Errorf("sahibi değişen yedek için kalıcı hata beklenirdi: %+v", h)
	}
	// Satır hiç yoksa da kalıcı hata.
	if _, err := o.db.Exec(`DELETE FROM backups WHERE id=?`, yid); err != nil {
		t.Fatal(err)
	}
	if _, h = o.isci.yerelYol(ctx, is); h == nil || !h.kalici {
		t.Errorf("olmayan yedek için kalıcı hata beklenirdi: %+v", h)
	}
}

func TestKurtarmaTipi_UzakGonderimAdayiDegil_JobIDOlsaBile(t *testing.T) {
	if KokYedegiMi("kurtarma", true) || KokYedegiMi("kurtarma", false) {
		t.Error("kurtarma tipi job_id taşısa bile kök yedeği sayılmamalı")
	}
	for _, c := range []struct {
		tip string
		job bool
		bek bool
	}{{"oto", false, true}, {"planli", false, true}, {"tam", true, true}, {"tam", false, false}} {
		if KokYedegiMi(c.tip, c.job) != c.bek {
			t.Errorf("KokYedegiMi(%q,%v) != %v", c.tip, c.job, c.bek)
		}
	}

	o := yeniOrtam(t)
	o.ayar(nil)
	oto := o.yedek(1, "c_a", "20260901-020000", sahteIcerik(10, 1), "oto", nil)
	kur := o.yedek(1, "c_a", "20260801-020000", sahteIcerik(10, 2), "kurtarma", 77) // eski toplu iş kaydı da var
	ids, _, err := TopluAdaylar(context.Background(), o.db, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != oto {
		t.Errorf("toplu aday yalnız oto olmalı (kurtarma hariç): %v", ids)
	}
	if _, _, err := KuyrugaAl(context.Background(), o.db, kur); !errors.Is(err, ErrKokDegil) {
		t.Errorf("kurtarma yedeği kuyruğa alınmamalı (ErrKokDegil): %v", err)
	}
	kayitlar, err := yerelKayitlari(context.Background(), o.db, o.kok, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	bulundu := 0
	for _, k := range kayitlar {
		switch k.yedekID {
		case kur:
			bulundu++
			if k.kok {
				t.Error("yerel-silme taraması kurtarma yedeğini kök (sistem) yedeği saymamalı")
			}
		case oto:
			bulundu++
			if !k.kok {
				t.Error("kontrol: oto yedek kök yedeği sayılmalı")
			}
		}
	}
	if bulundu != 2 {
		t.Errorf("her iki kayıt da taramada olmalı: %d", bulundu)
	}
}
