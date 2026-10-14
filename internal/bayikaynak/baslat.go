package bayikaynak

import (
	"context"
	"database/sql"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

// Hook'lar: üst katman (kaynaklimit) üyelik değişince hosting plan dilimlerini yeniden yazar.
// Döngü paketi o paketi içe aktaramaz (kaynaklimit bu paketi içe aktarır).
type Hooklar struct {
	// UyelikDegisti: Uzlastir üyelik ekledi/kaldırdı; plan dilimlerini yeni adla yeniden yaz.
	UyelikDegisti func(ctx context.Context, db *sql.DB, r Rapor)
}

var (
	hooklar   Hooklar
	hookKilit sync.Mutex
)

// HookKur: üst katman bağlantısı (kaynaklimit init'te çağırır).
func HookKur(h Hooklar) {
	hookKilit.Lock()
	hooklar = h
	hookKilit.Unlock()
}

func hookAl() Hooklar {
	hookKilit.Lock()
	defer hookKilit.Unlock()
	return hooklar
}

// guvenli: panik paneli düşürmesin.
func guvenli(ad string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 bayikaynak %s PANIK: %v\n%s", ad, r, debug.Stack())
		}
	}()
	f()
}

// UzlastirTam: tek tam tur — havuz dilimleri/üyelik/drop-in → üst katman kancası (plan
// dilimlerini yeniden yaz) → artık dilim temizliği → disk projeleri. Hata günlüğe yazılır.
// diskDahil=false: disk ataması atlanır (olay yolları diskleri ayrıca arka planda çalıştırır).
func UzlastirTam(ctx context.Context, db *sql.DB, diskDahil bool) Rapor {
	var r Rapor
	guvenli("uzlastir", func() {
		var err error
		r, err = Uzlastir(ctx, db)
		if err != nil {
			log.Printf("bayikaynak: uzlaştırma atlandı: %v", err)
			return
		}
		if h := hookAl(); h.UyelikDegisti != nil && (len(r.UyelikEklenen)+len(r.UyelikKalkan)) > 0 {
			h.UyelikDegisti(ctx, db, r)
		}
		ArtikTemizle(ctx)
		if diskDahil {
			pr, err := ProjeUygula(ctx, db, 0)
			if err != nil {
				log.Printf("bayikaynak: disk uzlaştırma atlandı: %v", err)
			} else if pr.Atanan+pr.Temizlenen+pr.SinirYazilan+pr.SinirKalkan+len(pr.Hatalar) > 0 {
				log.Printf("bayikaynak: disk — mod=%s atanan=%d temizlenen=%d sınır yazılan=%d kalkan=%d hata=%d",
					pr.Mod, pr.Atanan, pr.Temizlenen, pr.SinirYazilan, pr.SinirKalkan, len(pr.Hatalar))
			}
		}
	})
	return r
}

// Test dikişleri: açılışta beklenen süre ve tur aralığı (üretimde sırasıyla 30 sn ve OrnekAraligi).
var (
	acilisBekleme   = 30 * time.Second
	calistirAraligi = OrnekAraligi
)

// Calistir: panel ömrü boyunca arka plan döngüsü (main.go `go bayikaynak.Calistir(...)`).
//
//	açılış   : tam uzlaştırma (dilimler, üyelik, drop-in, disk projeleri)
//	5 dk     : kullanım örneği + artık dilim temizliği
//	30 dk    : tam uzlaştırma (sürüklenmeyi onarır: elle silinen dosya vb.)
//
// ctx iptalinde çıkar. Açılışta bir miktar beklenir: HealTenantFPM'in FPM birimlerini
// ayağa kaldırması önce gelsin.
func Calistir(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	select {
	case <-time.After(acilisBekleme):
	case <-ctx.Done():
		return
	}
	UzlastirTam(ctx, db, true)
	top := &Toplayici{DB: db}
	tik := time.NewTicker(calistirAraligi)
	defer tik.Stop()
	n := 0
	for {
		guvenli("kullanim", func() {
			if err := top.Tek(ctx, SimdiUTC()); err != nil {
				log.Printf("bayikaynak: kullanım örneği alınamadı: %v", err)
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-tik.C:
		}
		n++
		turSonu(ctx, db, n)
	}
}

// turSonu: n. örnekleme turundan sonra yapılan bakım — her 6. turda (OrnekAraligi=5 dk → 30 dk)
// sürüklenmeyi onaran tam uzlaştırma, diğer turlarda yalnız hafif artık dilim temizliği.
func turSonu(ctx context.Context, db *sql.DB, n int) {
	if n%6 == 0 {
		UzlastirTam(ctx, db, true)
		return
	}
	guvenli("artik", func() { ArtikTemizle(ctx) })
}

// HookKurulu: üst katman kancası bağlı mı? (testler ve açılış tanısı için)
func HookKurulu() bool { return hookAl().UyelikDegisti != nil }
