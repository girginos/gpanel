package marka

// depo.go — bayi markaları (cp_marka, göç 0122) + süreç içi önbellek.
//
// 🔴 Kimliksiz uç istek başına DB'ye gitmez: rid→kayıt, anahtar→rid,
// domain→rid ve host→rid eşlemeleri 60 sn önbelleklenir; her yazımda ilgili
// girdi düşürülür. DB hatasında SON BİLİNEN değer kullanılır; hiç değer yoksa
// kök markasına düşülür (görünüm ayarı fail-open; yetki kapısı değildir).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// AnahtarDeseni — bayi markası anahtarı: 20 karakter base32 (~100 bit).
// "kok" kök markasının sabit anahtarıdır. Başka her değer geçersizdir.
var AnahtarDeseni = regexp.MustCompile(`^[a-z2-7]{20}$`)

const KokAnahtar = "kok"

const onbellekOmur = 60 * time.Second

// BayiKayit — bir bayinin marka satırı (ya da satır yoksa varsayılanlar).
type BayiKayit struct {
	RID         int64
	Anahtar     string
	Ayar        Marka // Normalize edilmiş metin alanları (görsel bayrakları diskten)
	Kayitli     bool  // bayi kendi markasını kaydetti mi (etkin=1)
	Surum       int64
	Izin        bool // kök: bayi markasını yönetebilir mi (varsayılan açık)
	AlanadiIzni bool // kök: özel panel alan adı açık mı (varsayılan KAPALI)
	KokKapatti  bool // kök moderasyonu: marka gösterilmez
	SatirVar    bool
}

// Gecerli — bu kayıt görünür bir bayi markası mı.
func (k BayiKayit) Gecerli() bool { return k.SatirVar && k.Kayitli && k.Izin && !k.KokKapatti }

// Dizin — bayinin görsel dizini. Anahtar regex'le doğrulanmamışsa "".
func BayiDizin(anahtar string) string {
	if !AnahtarDeseni.MatchString(anahtar) {
		return ""
	}
	return filepath.Join(Kok, "bayi", anahtar)
}

// YeniAnahtar — crypto/rand ile 20 karakter base32 (küçük harf).
func YeniAnahtar() (string, error) {
	const abc = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = abc[int(b[i])&31]
	}
	return string(b), nil
}

// ── önbellek ────────────────────────────────────────────────────────────────

type obGirdi[T any] struct {
	v  T
	ts time.Time
}

type obHarita[K comparable, T any] struct {
	mu sync.Mutex
	m  map[K]obGirdi[T]
}

func (o *obHarita[K, T]) al(k K) (T, bool, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.m[k]
	if !ok {
		var z T
		return z, false, false
	}
	return g.v, true, time.Since(g.ts) < onbellekOmur
}

func (o *obHarita[K, T]) koy(k K, v T) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.m == nil {
		o.m = map[K]obGirdi[T]{}
	}
	// Sınırsız büyümesin (bilinmeyen anahtar denemeleri burada TUTULMAZ, ama
	// domain/rid sayısı da sınırsız değil): kaba sınır.
	if len(o.m) > 20000 {
		o.m = map[K]obGirdi[T]{}
	}
	o.m[k] = obGirdi[T]{v, time.Now()}
}

func (o *obHarita[K, T]) sil(k K) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.m, k)
}

func (o *obHarita[K, T]) temizle() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m = nil
}

var (
	obBayi    obHarita[int64, BayiKayit]
	obAnahtar obHarita[string, int64]
	obDomain  obHarita[int64, int64]
	obAyar    obHarita[string, string]
)

// OnbellekTemizle — testler ve toplu değişiklikler için.
func OnbellekTemizle() {
	obBayi.temizle()
	obAnahtar.temizle()
	obDomain.temizle()
	obAyar.temizle()
	hostTemizle()
}

func bayiDusur(k BayiKayit) {
	obBayi.sil(k.RID)
	if k.Anahtar != "" {
		obAnahtar.sil(k.Anahtar)
	}
}

// ── okuma ───────────────────────────────────────────────────────────────────

const bayiSecim = `SELECT reseller_id, anahtar, ayar_json, surum, etkin, izin, alanadi_izni, kok_kapatti FROM cp_marka`

func bayiTara(sc interface{ Scan(...any) error }) (BayiKayit, error) {
	var k BayiKayit
	var ham string
	var etkin, izin, ai, kk int
	if err := sc.Scan(&k.RID, &k.Anahtar, &ham, &k.Surum, &etkin, &izin, &ai, &kk); err != nil {
		return k, err
	}
	k.SatirVar, k.Kayitli, k.Izin, k.AlanadiIzni, k.KokKapatti = true, etkin == 1, izin == 1, ai == 1, kk == 1
	m := Varsayilan()
	if strings.TrimSpace(ham) != "" {
		if err := json.Unmarshal([]byte(ham), &m); err != nil {
			m = Varsayilan()
			k.Kayitli = false // bozuk satır görünür marka sayılmaz
		}
	}
	// 🔴 OKURKEN DE doğrula (DB'ye elle yazılmış değer) + bayi webmail'i yönetmez.
	m = Normalize(m)
	m.WebmailMarka = false
	if d := BayiDizin(k.Anahtar); d != "" {
		m = gorselleriOlc(m, d)
	} else {
		m = gorselleriOlc(m, filepath.Join(Kok, "bayi", "_gecersiz_"))
	}
	k.Ayar = m
	return k, nil
}

// bayiOkuDB — doğrudan DB. Satır yoksa varsayılan kayıt (SatirVar=false, Izin=true).
func bayiOkuDB(ctx context.Context, db *sql.DB, rid int64) (BayiKayit, error) {
	k, err := bayiTara(db.QueryRowContext(ctx, bayiSecim+` WHERE reseller_id=?`, rid))
	if errors.Is(err, sql.ErrNoRows) {
		return BayiKayit{RID: rid, Izin: true, Ayar: Varsayilan()}, nil
	}
	return k, err
}

// bayiOku — önbellekli. DB hatasında son bilinen değer (varsa) döner.
func bayiOku(ctx context.Context, db *sql.DB, rid int64) (BayiKayit, error) {
	if v, var_, taze := obBayi.al(rid); var_ && taze {
		return v, nil
	}
	k, err := bayiOkuDB(ctx, db, rid)
	if err != nil {
		if v, var_, _ := obBayi.al(rid); var_ {
			return v, nil
		}
		return k, err
	}
	obBayi.koy(rid, k)
	if k.Anahtar != "" {
		obAnahtar.koy(k.Anahtar, rid)
	}
	return k, nil
}

// anahtarRID — anahtar → rid. Bilinmeyen anahtar (0,false); önbelleğe
// YAZILMAZ (numaralandırma denemesi belleği şişirmesin).
func anahtarRID(ctx context.Context, db *sql.DB, anahtar string) (int64, bool) {
	if !AnahtarDeseni.MatchString(anahtar) || db == nil {
		return 0, false
	}
	if v, var_, taze := obAnahtar.al(anahtar); var_ && taze {
		return v, true
	}
	var rid int64
	err := db.QueryRowContext(ctx, `SELECT reseller_id FROM cp_marka WHERE anahtar=?`, anahtar).Scan(&rid)
	if err != nil {
		if v, var_, _ := obAnahtar.al(anahtar); var_ && !errors.Is(err, sql.ErrNoRows) {
			return v, true
		}
		return 0, false
	}
	obAnahtar.koy(anahtar, rid)
	return rid, true
}

// domainRID — domain'in bayisi (0 = kök). Hata → (0,false).
func domainRID(ctx context.Context, db *sql.DB, domainID int64) (int64, bool) {
	if domainID <= 0 || db == nil {
		return 0, false
	}
	if v, var_, taze := obDomain.al(domainID); var_ && taze {
		return v, true
	}
	var rid int64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).Scan(&rid)
	if err != nil {
		if v, var_, _ := obDomain.al(domainID); var_ {
			return v, true
		}
		return 0, false
	}
	obDomain.koy(domainID, rid)
	return rid, true
}

// Yedek politikası (cp_ayarlar.marka_bayi_yedek): bayinin görünür markası
// yoksa müşterisi ne görür. "kok" (varsayılan) | "notr".
const (
	YedekKok  = "kok"
	YedekNotr = "notr"
)

func yedekPolitika(ctx context.Context, db *sql.DB) string {
	if v, var_, taze := obAyar.al("marka_bayi_yedek"); var_ && taze {
		return v
	}
	v := YedekKok
	if db != nil {
		var d string
		if err := db.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar='marka_bayi_yedek'`).Scan(&d); err == nil {
			if d == YedekNotr {
				v = YedekNotr
			}
		} else if eski, var_, _ := obAyar.al("marka_bayi_yedek"); var_ && !errors.Is(err, sql.ErrNoRows) {
			return eski
		}
	}
	obAyar.koy("marka_bayi_yedek", v)
	return v
}

// Notr — "nötr" marka: kimseye ait iz taşımaz.
func Notr() Marka {
	m := Varsayilan()
	m.PanelAdi, m.Baslik, m.KisaAd = "Kontrol Paneli", "Kontrol Paneli", "Panel"
	m.SurumGoster = false
	return m
}

// BayiEtkin — bayi (rid) için GÖRÜNEN marka + görsel anahtarı. rid<=0 → kök.
// Bayinin görünür markası yoksa yedek politikası uygulanır.
func BayiEtkin(ctx context.Context, db *sql.DB, rid int64) (Marka, string) {
	if rid <= 0 || db == nil {
		return Oku(), KokAnahtar
	}
	k, err := bayiOku(ctx, db, rid)
	if err == nil && k.Gecerli() {
		return k.Ayar, k.Anahtar
	}
	if yedekPolitika(ctx, db) == YedekNotr {
		return Notr(), KokAnahtar
	}
	return Oku(), KokAnahtar
}

// RIDIcin — bildirim/e-posta gibi sunucu tarafı yüzeyler için rid'in görünen
// markası (D3b). rid=0 → kök.
func RIDIcin(ctx context.Context, db *sql.DB, rid int64) Marka {
	m, _ := BayiEtkin(ctx, db, rid)
	return m
}

// DomainIcin — bir hosting hesabının (domain) müşterisinin gördüğü marka.
func DomainIcin(ctx context.Context, db *sql.DB, domainID int64) (Marka, string) {
	rid, ok := domainRID(ctx, db, domainID)
	if !ok {
		return Oku(), KokAnahtar
	}
	return BayiEtkin(ctx, db, rid)
}
