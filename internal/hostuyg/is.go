package hostuyg

// Async iş yönetimi — kurulum/kaldırma uzun sürer (indir + start + healthcheck
// 30-60s). Handler async iş başlatır, UI polling ile durumu izler.
//
// panelhost desenini birebir kopyalar: memory-only Is + DB persist.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

type Adim struct {
	Zaman  time.Time `json:"zaman"`
	Mesaj  string    `json:"mesaj"`
	Basari bool      `json:"basari"`
}

type Is struct {
	ID         string    `json:"id"`
	Tip        string    `json:"tip"` // "kur" | "kaldir" | "yedek" | "restore" | "test"
	Kod        string    `json:"kod"`
	UygulamaID int64     `json:"uygulama_id,omitempty"`
	Durum      string    `json:"durum"` // "kosuyor" | "bitti" | "hata"
	Basla      time.Time `json:"basla"`
	Bitis      time.Time `json:"bitis,omitempty"`
	Hata       string    `json:"hata,omitempty"`
	Adimlar    []Adim    `json:"adimlar"`

	mu sync.Mutex
}

var (
	isMu    sync.RWMutex
	isKayit = map[string]*Is{}
)

// DB — main.go tarafından set edilir. Nil ise persist NO-OP.
var DB *sql.DB

func yeniID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// IsBaslat — yeni iş.
func IsBaslat(tip, kod string, uygulamaID int64) *Is {
	is := &Is{
		ID:         yeniID(),
		Tip:        tip,
		Kod:        kod,
		UygulamaID: uygulamaID,
		Durum:      "kosuyor",
		Basla:      time.Now(),
		Adimlar:    []Adim{},
	}
	isMu.Lock()
	isKayit[is.ID] = is
	isMu.Unlock()
	persistYaz(is)
	return is
}

// AdimEkle — kilit-güvenli.
func (i *Is) AdimEkle(mesaj string, basari bool) {
	i.mu.Lock()
	i.Adimlar = append(i.Adimlar, Adim{Zaman: time.Now(), Mesaj: mesaj, Basari: basari})
	i.mu.Unlock()
}

// Bitir — durum=bitti/hata + persist. Idempotent (MV#10): tekrar çağrı no-op.
func (i *Is) Bitir(hata string) {
	i.mu.Lock()
	if !i.Bitis.IsZero() {
		i.mu.Unlock()
		return // zaten bitmiş
	}
	i.Bitis = time.Now()
	if hata != "" {
		i.Durum = "hata"
		i.Hata = hata
	} else {
		i.Durum = "bitti"
	}
	i.mu.Unlock()
	persistGuncelle(i)
}

// IsGetir — memory'den snapshot; miss durumunda DB'den hydrate (I6 fix:
// panel restart sonrası UI polling 404 almasın).
func IsGetir(id string) *Is {
	isMu.RLock()
	orig := isKayit[id]
	isMu.RUnlock()
	if orig != nil {
		orig.mu.Lock()
		defer orig.mu.Unlock()
		k := &Is{
			ID: orig.ID, Tip: orig.Tip, Kod: orig.Kod, UygulamaID: orig.UygulamaID,
			Durum: orig.Durum, Basla: orig.Basla, Bitis: orig.Bitis, Hata: orig.Hata,
		}
		if len(orig.Adimlar) > 0 {
			k.Adimlar = make([]Adim, len(orig.Adimlar))
			copy(k.Adimlar, orig.Adimlar)
		} else {
			k.Adimlar = []Adim{}
		}
		return k
	}
	// Memory miss → DB
	if DB == nil {
		return nil
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	k := &Is{ID: id, Adimlar: []Adim{}}
	var uygID sql.NullInt64
	var bitis sql.NullTime
	var adimJson string
	err := DB.QueryRowContext(ctx,
		`SELECT tip, kod, uygulama_id, durum, basla, bitis, hata, adimlar_json
		 FROM cp_host_uyg_islemler WHERE is_id=?`, id).Scan(
		&k.Tip, &k.Kod, &uygID, &k.Durum, &k.Basla, &bitis, &k.Hata, &adimJson)
	if err != nil {
		return nil
	}
	if uygID.Valid {
		k.UygulamaID = uygID.Int64
	}
	if bitis.Valid {
		k.Bitis = bitis.Time
	}
	_ = json.Unmarshal([]byte(adimJson), &k.Adimlar)
	if k.Adimlar == nil {
		k.Adimlar = []Adim{}
	}
	return k
}

/* -------- persist -------- */

func persistYaz(i *Is) {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	adim, _ := json.Marshal(i.Adimlar)
	var uid interface{}
	if i.UygulamaID != 0 {
		uid = i.UygulamaID
	}
	_, err := DB.ExecContext(ctx,
		`INSERT INTO cp_host_uyg_islemler (is_id, uygulama_id, tip, kod, durum, basla, adimlar_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.ID, uid, i.Tip, i.Kod, i.Durum, i.Basla, string(adim))
	if err != nil {
		log.Printf("hostuyg.persistYaz fail: %v", err)
	}
}

func persistGuncelle(i *Is) {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	adim, _ := json.Marshal(i.Adimlar)
	var bitis interface{}
	if !i.Bitis.IsZero() {
		bitis = i.Bitis
	}
	_, err := DB.ExecContext(ctx,
		`UPDATE cp_host_uyg_islemler SET durum=?, bitis=?, hata=?, adimlar_json=?
		 WHERE is_id=?`,
		i.Durum, bitis, i.Hata, string(adim), i.ID)
	if err != nil {
		log.Printf("hostuyg.persistGuncelle fail: %v", err)
	}
}

// IsRestartTemizle — startup'ta yarım kalan işleri temizle.
// MV#10 fix: mid-yedek/restore panel restart olursa unit STOP kalmış olabilir
// (defer UnitEnableStart hiç çalışmadı). Startup'ta yarım yedek/restore işleri
// için ilgili unit'i tekrar START etmeyi dene (customer uygulaması up olsun).
func IsRestartTemizle() {
	if DB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
	defer iptal()

	// Önce yarım kalan yedek/restore işlerinin unit'lerini bul + start dene
	rows, err := DB.QueryContext(ctx, `SELECT DISTINCT i.uygulama_id, u.systemd_unit
		FROM cp_host_uyg_islemler i JOIN cp_host_uygulamalar u ON u.id=i.uygulama_id
		WHERE i.durum='kosuyor' AND i.tip IN ('yedek','restore')`)
	if err != nil {
		log.Printf("hostuyg.IsRestartTemizle: yarım iş sorgusu fail: %v", err)
	}
	yeniden := 0
	if rows != nil {
		for rows.Next() {
			var uygID int64
			var unit string
			if rows.Scan(&uygID, &unit) == nil && unit != "" {
				startCtx, iptal2 := context.WithTimeout(context.Background(), 15*time.Second)
				if err := UnitEnableStart(startCtx, unit); err == nil {
					yeniden++
					log.Printf("hostuyg.IsRestartTemizle: mid-yedek panel restart → %s yeniden başlatıldı", unit)
				} else {
					// MV#11 fix: start fail → app tablosunda durum='hata' set
					// (sessiz DOWN state kapatıldı, kullanıcı gerçek durumu görsün)
					_, _ = DB.ExecContext(context.Background(),
						`UPDATE cp_host_uygulamalar SET durum='hata',
						 son_hata='panel restart sonrası mid-yedek — unit start fail, manuel restart gerekli'
						 WHERE id=?`, uygID)
					log.Printf("hostuyg.IsRestartTemizle: %s start fail: %v → app durum='hata' set", unit, err)
				}
				iptal2()
			}
		}
		rows.Close()
	}
	if yeniden > 0 {
		log.Printf("hostuyg.IsRestartTemizle: %d app yeniden başlatıldı (mid-yedek)", yeniden)
	}

	res, err := DB.ExecContext(ctx,
		`UPDATE cp_host_uyg_islemler
		 SET durum='hata', bitis=NOW(), hata='panel restart oldu'
		 WHERE durum='kosuyor'`)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("hostuyg: %d yarım kalmış iş temizlendi", n)
		}
	}
	// Uygulamalar tablosunda "kurulmakta"/"kaldirilmakta" da temizle → hata
	_, _ = DB.ExecContext(ctx,
		`UPDATE cp_host_uygulamalar SET durum='hata', son_hata='panel restart oldu'
		 WHERE durum IN ('kurulmakta','kaldirilmakta')`)
}
