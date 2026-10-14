package dbgovernor

import (
	"context"
	"database/sql"
	"log"
	"runtime"
	"sync/atomic"
	"time"
)

// genel: üretimdeki Motor (BaglantiKancasi ve Durum için).
var genel atomic.Pointer[Motor]

// BaglantiKancasi: kaynaklimit.DBBaglantiKancasi'na bağlanır. Plan MAX_USER_CONNECTIONS değerini
// yazarken kısıtlı kullanıcının kısıtını EZMEZ (plan değeri not edilir; kısıt kalkınca yazılır).
func BaglantiKancasi(kullanici string, plan int) int {
	if m := genel.Load(); m != nil {
		return m.baglantiKancasi(kullanici, plan)
	}
	return plan
}

// YeniMotor: üretim bağımlılıklarıyla Motor kurar.
func YeniMotor(db *sql.DB) *Motor {
	cli := CLI{}
	return &Motor{
		DB:          db,
		Depo:        &SQLDepo{DB: db},
		Olcer:       cli,
		Yon:         cli,
		AyarOku:     func(ctx context.Context) (Ayar, error) { return ayarOku(ctx, db) },
		NCPU:        float64(runtime.NumCPU()),
		UserstatCnf: UserstatCnfYolu,
	}
}

// Calistir: kısıcıyı panel ömrü boyunca arka planda çalıştırır (main.go `go dbgovernor.Calistir`).
// Önce önceki çalışmadan kalan kısıtları geri alır, sonra her Poll aralığında bir tur koşar.
// Tek turdaki panik/hata döngüyü öldürmez.
func Calistir(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	m := YeniMotor(db)
	genel.Store(m)
	log.Printf("DB kısıcı: başladı (CPU_TIME muhasebesi, kademeli kısma)")
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("DB kısıcı: açılış kurtarma paniği: %v", r)
			}
		}()
		kctx, iptal := context.WithTimeout(ctx, 2*time.Minute)
		defer iptal()
		m.Kurtar(kctx)
	}()
	aralik := VarsayilanAyar().Poll
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(aralik):
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("DB kısıcı: tur paniği: %v", r)
				}
			}()
			tctx, iptal := context.WithTimeout(ctx, 2*time.Minute)
			defer iptal()
			m.Tick(tctx)
		}()
		// ayar değişirse aralık da değişsin
		if a := m.sonAyarPoll(); a > 0 {
			aralik = a
		}
	}
}

func (m *Motor) sonAyarPoll() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Duration(m.durum.PollSn) * time.Second
}
