package dbgovernor

// sahte_test.go — birim testlerinin sahte MariaDB / panel DB / zaman altyapısı.
// USER_STATISTICS için sahte kaynak dikişi (Olcer) BURADADIR: testler kümülatif CPU_TIME
// sayaçlarını ve çalışan deyim listesini elle sürer.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

type sahteOlcer struct {
	mu      sync.Mutex
	cum     map[string]float64
	calisan []Sorgu
	hata    error
	onKos   func() // her KullaniciCPU çağrısı başında (yavaş DB benzetimi)
}

func (s *sahteOlcer) KullaniciCPU(ctx context.Context) (map[string]float64, error) {
	if s.onKos != nil {
		s.onKos()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hata != nil {
		return nil, s.hata
	}
	m := map[string]float64{}
	for k, v := range s.cum {
		m[k] = v
	}
	return m, nil
}

func (s *sahteOlcer) Calisanlar(ctx context.Context) ([]Sorgu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sorgu(nil), s.calisan...), nil
}

type sahteYon struct {
	mu           sync.Mutex
	hesap        map[string][]Hesap
	alterler     []string
	killler      []int64
	userstat     bool
	userstatAcHt error
	hesapHata    error
	alterHata    map[string]error // "user@host" → hata
	killHata     error

	// bağlantı benzetimi (3. seviye): kullanıcı → açık bağlantı id'leri; MariaDB'nin hesap başına
	// bağlantı sayacı önbelleği — yeni MAX_USER_CONNECTIONS, hesabın bağlantı sayısı 0'a inene kadar
	// UYGULANMAZ (VM-C'de ölçüldü; ALTER/FLUSH bunu tazelemez).
	conns         map[string][]int64
	efektif       map[string]int
	nextConn      int64
	yabanci       []Baglanti // Baglantilar'a sızan YABANCI satırlar (hatalı kaynak benzetimi)
	kesilenler    []int64
	kesHata       error
	baglantiHata  error
	baglantiSorgu []string
	sira          []string // ALTER/KILL CONNECTION işlem sırası
}

var errLimit = errors.New("ERROR 1226: User has exceeded the 'max_user_connections' resource")

// baglan: bir istemci bağlanır (gerçek MariaDB davranışı: sayaç 0 iken limit tazelenir).
func (y *sahteYon) baglan(u string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.conns == nil {
		y.conns, y.efektif = map[string][]int64{}, map[string]int{}
	}
	if len(y.conns[u]) == 0 {
		lim := 0
		if h := y.hesap[u]; len(h) > 0 {
			lim = h[0].Baglanti
		}
		y.efektif[u] = lim
	}
	if e := y.efektif[u]; e > 0 && len(y.conns[u]) >= e {
		return errLimit
	}
	y.nextConn++
	y.conns[u] = append(y.conns[u], 1000+y.nextConn)
	return nil
}

func (y *sahteYon) baglantiSayisi(u string) int {
	y.mu.Lock()
	defer y.mu.Unlock()
	return len(y.conns[u])
}

func (y *sahteYon) Baglantilar(ctx context.Context, u string) ([]Baglanti, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.baglantiSorgu = append(y.baglantiSorgu, u)
	if y.baglantiHata != nil {
		return nil, y.baglantiHata
	}
	var l []Baglanti
	for _, id := range y.conns[u] {
		l = append(l, Baglanti{ID: id, Kullanici: u})
	}
	return append(l, y.yabanci...), nil
}

func (y *sahteYon) BaglantiKes(ctx context.Context, id int64) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.kesHata != nil {
		return y.kesHata
	}
	y.kesilenler = append(y.kesilenler, id)
	y.sira = append(y.sira, "kes")
	for u, l := range y.conns {
		for i, c := range l {
			if c == id {
				y.conns[u] = append(l[:i:i], l[i+1:]...)
				return nil
			}
		}
	}
	return nil
}

func (y *sahteYon) Hesaplar(ctx context.Context, kul []string) (map[string][]Hesap, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.hesapHata != nil {
		return nil, y.hesapHata
	}
	out := map[string][]Hesap{}
	for _, u := range kul {
		if h, ok := y.hesap[u]; ok {
			out[u] = append([]Hesap(nil), h...)
		}
	}
	return out, nil
}

func (y *sahteYon) BaglantiAyarla(ctx context.Context, u, host string, n int) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if e := y.alterHata[u+"@"+host]; e != nil {
		return e
	}
	y.alterler = append(y.alterler, fmt.Sprintf("%s@%s=%d", u, host, n))
	y.sira = append(y.sira, fmt.Sprintf("%s@%s=%d", u, host, n))
	for i := range y.hesap[u] {
		if y.hesap[u][i].Host == host {
			y.hesap[u][i].Baglanti = n
		}
	}
	return nil
}

func (y *sahteYon) SorguOldur(ctx context.Context, qid int64) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.killHata != nil {
		return y.killHata
	}
	y.killler = append(y.killler, qid)
	return nil
}

func (y *sahteYon) UserstatAcikMi(ctx context.Context) (bool, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.userstat, nil
}

func (y *sahteYon) UserstatAc(ctx context.Context) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.userstatAcHt != nil {
		return y.userstatAcHt
	}
	y.userstat = true
	return nil
}

func (y *sahteYon) baglanti(u string) []int {
	y.mu.Lock()
	defer y.mu.Unlock()
	var l []int
	for _, h := range y.hesap[u] {
		l = append(l, h.Baglanti)
	}
	return l
}

func (y *sahteYon) alterSayisi() int {
	y.mu.Lock()
	defer y.mu.Unlock()
	return len(y.alterler)
}

type sahteKayit struct {
	k          Kisit
	acik       bool
	bitisNeden string
}

type sahteDepo struct {
	mu        sync.Mutex
	kimlik    map[string]Kimlik
	domKul    map[int64][]string
	esik      map[int64]float64
	bayi      map[int64]float64
	plan      map[int64]int // planlı hostinglerin plan bağlantı değeri
	kayit     map[int64]*sahteKayit
	sonID     int64
	olcum     []OlcumSatiri
	esleHata  error
	acHata    error
	kapatHata error
	dilimler  []int64
	temizSay  int
	esleSay   int
}

func (d *sahteDepo) Esle(ctx context.Context, kul []string) (map[string]Kimlik, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.esleSay++
	if d.esleHata != nil {
		return nil, d.esleHata
	}
	out := map[string]Kimlik{}
	for _, u := range kul {
		if k, ok := d.kimlik[u]; ok {
			out[u] = k
		} else {
			out[u] = Kimlik{Yok: true}
		}
	}
	return out, nil
}

func (d *sahteDepo) DomainKullanicilari(ctx context.Context, id int64) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.domKul[id]...), nil
}

func (d *sahteDepo) HostingEsik(ctx context.Context, id int64, v float64) (float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.esik[id]; ok {
		return e, nil
	}
	return v, nil
}

func (d *sahteDepo) BayiCPU(ctx context.Context, id int64) (float64, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.bayi[id]
	return v, ok, nil
}

func (d *sahteDepo) PlanBaglanti(ctx context.Context, id int64) (int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, ok := d.plan[id]
	return n, ok, nil
}

func (d *sahteDepo) Ac(ctx context.Context, k *Kisit) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.acHata != nil {
		return 0, d.acHata
	}
	d.sonID++
	c := *k
	c.KayitID = d.sonID
	d.kayit[d.sonID] = &sahteKayit{k: c, acik: true}
	return d.sonID, nil
}

func (d *sahteDepo) Guncelle(ctx context.Context, k *Kisit) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.kayit[k.KayitID]; r != nil && r.acik {
		r.k = *k
	}
	return nil
}

func (d *sahteDepo) Kapat(ctx context.Context, id, ts int64, neden string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.kapatHata != nil {
		return d.kapatHata
	}
	if r := d.kayit[id]; r != nil && r.acik {
		r.acik, r.bitisNeden = false, neden
	}
	return nil
}

func (d *sahteDepo) AcikKayitlar(ctx context.Context) ([]*Kisit, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var l []*Kisit
	for _, r := range d.kayit {
		if r.acik {
			c := r.k
			l = append(l, &c)
		}
	}
	sort.Slice(l, func(i, j int) bool { return l[i].KayitID < l[j].KayitID })
	return l, nil
}

func (d *sahteDepo) OlcumYaz(ctx context.Context, dilim int64, s []OlcumSatiri) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.olcum = append(d.olcum, s...)
	d.dilimler = append(d.dilimler, dilim)
	return nil
}

func (d *sahteDepo) Temizle(ctx context.Context, t time.Time) error {
	d.mu.Lock()
	d.temizSay++
	d.mu.Unlock()
	return nil
}

func (d *sahteDepo) acikSayisi() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, r := range d.kayit {
		if r.acik {
			n++
		}
	}
	return n
}

func (d *sahteDepo) bitisNedenleri() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var l []string
	for _, r := range d.kayit {
		if !r.acik {
			l = append(l, r.bitisNeden)
		}
	}
	sort.Strings(l)
	return l
}

// ortam: bir senaryonun tüm sahteleri + zaman.
type ortam struct {
	t     *testing.T
	m     *Motor
	olcer *sahteOlcer
	yon   *sahteYon
	depo  *sahteDepo
	simdi time.Time
	ayar  Ayar
	ayarH error
}

func testAyar() Ayar {
	a := VarsayilanAyar()
	a.Mod = ModUygula // VARSAYILAN gozlem'dir; kısma senaryoları uygula modunu açıkça ister
	a.Poll, a.Pencere = 15*time.Second, 60*time.Second
	return a
}

// yeniOrtam: iki hosting: 1 → a_db (bayi yok), 2 → b_db + b_wp (bayi yok). NCPU=4.
func yeniOrtam(t *testing.T) *ortam {
	o := &ortam{t: t, simdi: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), ayar: testAyar()}
	o.olcer = &sahteOlcer{cum: map[string]float64{}}
	o.yon = &sahteYon{hesap: map[string][]Hesap{}, userstat: true, alterHata: map[string]error{}}
	o.depo = &sahteDepo{
		kimlik: map[string]Kimlik{}, domKul: map[int64][]string{}, esik: map[int64]float64{},
		bayi: map[int64]float64{}, plan: map[int64]int{}, kayit: map[int64]*sahteKayit{},
	}
	o.m = &Motor{
		Depo: o.depo, Olcer: o.olcer, Yon: o.yon, NCPU: 4,
		Simdi:   func() time.Time { return o.simdi },
		AyarOku: func(ctx context.Context) (Ayar, error) { return o.ayar, o.ayarH },
	}
	o.hosting(1, 0, "a.test", "a_db")
	o.hosting(2, 0, "b.test", "b_db", "b_wp")
	return o
}

// hosting: panelde bir hosting + DB kullanıcıları (hepsi localhost + 127.0.0.1 hesabı, bağlantı sınırı 0).
func (o *ortam) hosting(id, rid int64, alan string, kullanicilar ...string) {
	for _, u := range kullanicilar {
		o.depo.kimlik[u] = Kimlik{DomainID: id, ResellerID: rid, AlanAdi: alan}
		o.yon.hesap[u] = []Hesap{{Host: "localhost"}, {Host: "127.0.0.1"}}
		o.olcer.cum[u] = 0
	}
	o.depo.domKul[id] = kullanicilar
}

// adim: sn saniye ilerler; her kullanıcının sayacına hiz*sn CPU saniyesi ekler ve bir tur koşar.
func (o *ortam) adim(sn float64, hizlar map[string]float64) {
	o.simdi = o.simdi.Add(time.Duration(sn * float64(time.Second)))
	o.olcer.mu.Lock()
	for u, h := range hizlar {
		o.olcer.cum[u] += h * sn
	}
	o.olcer.mu.Unlock()
	o.m.Tick(context.Background())
}

// calis: n tur (15 sn) aynı hızla.
func (o *ortam) calis(n int, hizlar map[string]float64) {
	for i := 0; i < n; i++ {
		o.adim(15, hizlar)
	}
}

func (o *ortam) olayAdet(tur, eylem string) int {
	n := 0
	for _, e := range o.m.Olaylar() {
		if e.Tur == tur && e.Eylem == eylem {
			n++
		}
	}
	return n
}

var errSahte = errors.New("sahte hata")

func esitInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
