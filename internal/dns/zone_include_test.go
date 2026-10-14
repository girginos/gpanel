package dns

// zone_include_test.go — paylaşılan include dosyasının garantileri: include commit edilmiş
// DB durumundan uzlaştırılır (ne bayat anlık ne de başka bir işlemin görünümü kalır),
// eşzamanlı yazımlar birbirinin geçici dosyasını ezmez, WriteZone aynı domainin
// zoneliIslem yazımlarıyla seridir. Zone araçları sahte, yollar geçici dizin (zd_kur);
// DB'li (GOSP_TEST_GECICI_DSN yoksa ATLANIR).

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func zinc_beklenen(t *testing.T, db *sql.DB) string {
	t.Helper()
	s, err := buildZoneIncludes(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// zinc_ekle — tx içinde domain 42'ye zone'u geçerli kılan NS + glue kaydı ekler.
func zinc_ekle(tx *sql.Tx) error {
	for _, k := range [][]any{{"@", "NS", "ns1.bayialan.test"}, {"ns1", "A", "203.0.113.1"}} {
		if _, err := tx.Exec(`INSERT INTO dns_records(domain_id, ad, tip, deger, ttl, oncelik, aktif)
			VALUES(42,?,?,?,3600,0,1)`, k...); err != nil {
			return err
		}
	}
	return nil
}

// Yeniden yükleme başarısız → işlem geri alınır; include ne bayat anlıkta ne de işlemin
// (commit edilmemiş) görünümünde kalır: commit edilmiş DB durumuna uzlaşır.
func TestZoneliIslem_YuklemeHatasi_IncludeCommitEdilmisDurumaUzlasir(t *testing.T) {
	_, inc, kmd := zd_kur(t)
	db := zi_db(t)
	zi_ekle(t, db, 43, "@", "NS", "ns1.baska.test", 86400, 0, 1)
	zi_ekle(t, db, 43, "ns1", "A", "203.0.113.2", 3600, 0, 1)
	zd_yaz(t, inc, zdEskiInclude) // bayat: commit edilmiş durumla uyuşmuyor
	hata := func([]string) ([]byte, error) { return []byte("fail"), errors.New("exit status 1") }
	kmd.davranis["rndc"] = hata
	kmd.davranis["systemctl:reload"] = hata
	kmd.davranis["named-checkconf:bos"] = hata

	h := &Handlers{DB: db}
	err := h.zoneliIslem(context.Background(), 42, zinc_ekle)
	var zh *zoneIslemHatasi
	if !errors.As(err, &zh) {
		t.Fatalf("zone hatası beklenirdi: %v", err)
	}
	i, _ := zd_oku(t, inc)
	if i != zinc_beklenen(t, db) {
		t.Errorf("include commit edilmiş durumla uzlaşmadı: %q", i)
	}
	if strings.Contains(i, `zone "bayialan.test"`) || !strings.Contains(i, `zone "baska.test"`) {
		t.Errorf("geri alınan domain include'da kaldı ya da diğer domain kayboldu: %q", i)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 0 {
		t.Errorf("işlem geri alınmadı: %d kayıt", n)
	}
	if kmd.say("rndc") < 2 {
		t.Errorf("geri alma sonrası named yeniden yüklenmeli: %v", kmd.cagrilar)
	}
}

// Başarılı işlem sürerken başka bir domainin işlemi include'u kendi görünümüyle (bu işlemi
// görmeden) yazarsa commit sonrası uzlaştırma bu domainin zone'unu geri getirir.
func TestZoneliIslem_Basari_EszamanliIncludeYazimiUzlastirilir(t *testing.T) {
	_, inc, kmd := zd_kur(t)
	db := zi_db(t)
	const baskasi = "// BASKA-ISLEMIN-GORUNUMU\n"
	ilk := true
	kmd.davranis["rndc"] = func([]string) ([]byte, error) {
		if ilk { // bu işlemin yeniden yüklemesi; tam o sırada başka işlem include'u ezer
			ilk = false
			if err := os.WriteFile(inc, []byte(baskasi), 0o644); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	h := &Handlers{DB: db}
	if err := h.zoneliIslem(context.Background(), 42, zinc_ekle); err != nil {
		t.Fatalf("başarı beklenirdi: %v", err)
	}
	i, _ := zd_oku(t, inc)
	if !strings.Contains(i, `zone "bayialan.test"`) || i != zinc_beklenen(t, db) {
		t.Errorf("commit sonrası include uzlaştırılmadı: %q", i)
	}
	if n := kmd.say("rndc"); n != 2 {
		t.Errorf("include değiştiği için named bir kez daha yüklenmeli: rndc=%d", n)
	}
}

// Include zaten commit edilmiş durumdaysa uzlaştırma dosyaya ve named'e dokunmaz;
// yukle=true ise (geri alma yolu) named yine yüklenir.
func TestIncludeTazele_DegisiklikYoksaDokunmaz(t *testing.T) {
	_, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	zd_yaz(t, inc, zinc_beklenen(t, db))
	includeTazele(context.Background(), db, false)
	if len(kmd.cagrilar) != 0 {
		t.Errorf("değişiklik yokken komut çalışmamalı: %v", kmd.cagrilar)
	}
	includeTazele(context.Background(), db, true)
	if kmd.say("rndc") != 1 || kmd.say("named-checkconf") != 0 {
		t.Errorf("yukle=true yalnız named'i yüklemeli: %v", kmd.cagrilar)
	}
}

// Eşzamanlı include yazımları seri yürür ve birbirinin geçici dosyasını ezmez.
func TestUpdateZoneIncludes_EszamanliYazimlarCakismaz(t *testing.T) {
	_, inc, _ := zd_kur(t)
	db, _ := zd_alan(t)
	beklenen := zinc_beklenen(t, db)
	eskiK := komutCalistir
	t.Cleanup(func() { komutCalistir = eskiK })
	var icerde, enCok int32
	komutCalistir = func(ad string, arg ...string) ([]byte, error) {
		if ad == "named-checkconf" && len(arg) == 1 {
			n := atomic.AddInt32(&icerde, 1)
			defer atomic.AddInt32(&icerde, -1)
			for {
				m := atomic.LoadInt32(&enCok)
				if n <= m || atomic.CompareAndSwapInt32(&enCok, m, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			if b, err := os.ReadFile(arg[0]); err != nil || string(b) != beklenen {
				return []byte("yarım ya da ezilmiş geçici dosya"), errors.New("exit status 1")
			}
		}
		return nil, nil
	}
	var wg sync.WaitGroup
	hatalar := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hatalar <- updateZoneIncludes(context.Background(), db)
		}()
	}
	wg.Wait()
	close(hatalar)
	for err := range hatalar {
		if err != nil {
			t.Errorf("eşzamanlı include yazımı düştü: %v", err)
		}
	}
	if n := atomic.LoadInt32(&enCok); n > 1 {
		t.Errorf("include yazımları seri olmalı: en çok %d eşzamanlı", n)
	}
	if i, _ := zd_oku(t, inc); i != beklenen {
		t.Errorf("include beklenen içerikte değil: %q", i)
	}
	if m, _ := filepath.Glob(inc + ".tmp*"); len(m) != 0 {
		t.Errorf("geçici dosya kaldı: %v", m)
	}
}

// WriteZone (kilitsiz çağıranlar: alt alan, marka, DKIM) aynı domainin zoneliIslem
// yazımlarıyla seridir: domain kilidi tutulurken bekler.
func TestWriteZone_DomainKilidiniBekler(t *testing.T) {
	zd_kur(t)
	db, _ := zd_alan(t)
	k := domainKilidi(42)
	k.Lock()
	bitti := make(chan error, 1)
	go func() { bitti <- WriteZone(context.Background(), db, 42) }()
	select {
	case err := <-bitti:
		k.Unlock()
		t.Fatalf("WriteZone domain kilidini beklemeliydi (döndü: %v)", err)
	case <-time.After(150 * time.Millisecond):
	}
	k.Unlock()
	select {
	case err := <-bitti:
		if err != nil {
			t.Fatalf("WriteZone: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WriteZone kilit bırakılınca bitmedi")
	}
}
