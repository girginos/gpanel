package reseller

// kanca_test.go — bayi kaynak havuzu kancaları (bayiHavuzUygula / bayiHavuzTemizle) testlerde
// GERÇEK sunucu dosyalarına (systemd birimleri, /etc/girginospanel, xfs_quota) dokunmaz: çağrılar
// kaydedilir ve testler "doğru olayda çağrıldı mı" diye sınar.

import (
	"context"
	"database/sql"
	"sync"

	"girginospanel/internal/bayikaynak"
)

var havuzKanca struct {
	mu      sync.Mutex
	uygula  int
	temizle []int64
}

// Yönetici eylemlerinin sunucuya dokunan uçları (tam uzlaştırma, grubby ile rootflags) da sahte:
// hiçbir test gerçek uzlaştırma / grubby çalıştıramaz. Çağrılar kaydedilir.
var eylemKanca struct {
	mu          sync.Mutex
	uzlastir    int
	etkinlestir []bool // uygula bayrakları, çağrı sırasıyla
	etkHata     error
	asim        []bayikaynak.AsimBayi
}

func init() {
	bayiHavuzUygula = func(*sql.DB) {
		havuzKanca.mu.Lock()
		havuzKanca.uygula++
		havuzKanca.mu.Unlock()
	}
	bayiHavuzTemizle = func(_ context.Context, _ *sql.DB, id int64) {
		havuzKanca.mu.Lock()
		havuzKanca.temizle = append(havuzKanca.temizle, id)
		havuzKanca.mu.Unlock()
	}
	kaynakUzlastirTam = func(context.Context, *sql.DB) bayikaynak.Rapor {
		eylemKanca.mu.Lock()
		eylemKanca.uzlastir++
		eylemKanca.mu.Unlock()
		return bayikaynak.Rapor{}
	}
	kaynakAsimdakiler = func(context.Context, *sql.DB) ([]bayikaynak.AsimBayi, error) {
		eylemKanca.mu.Lock()
		defer eylemKanca.mu.Unlock()
		return append([]bayikaynak.AsimBayi(nil), eylemKanca.asim...), nil
	}
	kaynakDiskEtkinlestir = func(_ context.Context, uygula bool) (bayikaynak.EtkinlestirSonuc, error) {
		eylemKanca.mu.Lock()
		defer eylemKanca.mu.Unlock()
		eylemKanca.etkinlestir = append(eylemKanca.etkinlestir, uygula)
		s := bayikaynak.EtkinlestirSonuc{Komut: "grubby --SAHTE", Mesaj: "sahte"}
		if eylemKanca.etkHata != nil && uygula {
			return s, eylemKanca.etkHata
		}
		s.Uygulandi = uygula
		return s, nil
	}
}

func eylemKancaSifirla(etkHata error, asim ...bayikaynak.AsimBayi) {
	eylemKanca.mu.Lock()
	eylemKanca.uzlastir, eylemKanca.etkinlestir, eylemKanca.etkHata, eylemKanca.asim = 0, nil, etkHata, asim
	eylemKanca.mu.Unlock()
}

func eylemKancaOku() (uzlastir int, etkinlestir []bool) {
	eylemKanca.mu.Lock()
	defer eylemKanca.mu.Unlock()
	return eylemKanca.uzlastir, append([]bool(nil), eylemKanca.etkinlestir...)
}

func havuzKancaSifirla() {
	havuzKanca.mu.Lock()
	havuzKanca.uygula, havuzKanca.temizle = 0, nil
	havuzKanca.mu.Unlock()
}

func havuzKancaOku() (uygula int, temizle []int64) {
	havuzKanca.mu.Lock()
	defer havuzKanca.mu.Unlock()
	return havuzKanca.uygula, append([]int64(nil), havuzKanca.temizle...)
}
