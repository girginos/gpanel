// Package dbgovernor: MariaDB CPU kısıcısı (DB governor).
//
// Sorun: bir hostingin yazılım hatası yüzünden sürekli CPU yakan SQL sorguları ortak MariaDB
// sürecini ve dolayısıyla sunucudaki öteki müşterileri yavaşlatır. Mevcut governor (kaynaklimit)
// bağlantı sayısı/saatlik sorgu sayısı ve tek sorgunun süresini sınırlar; çok sayıda KISA ağır
// sorgu bu sınırlara takılmaz ve MariaDB ortak süreç olduğu için hostingin systemd slice'ındaki
// CPU kotasına da sayılmaz.
//
// Çözüm: DB kullanıcısı başına CPU süresini ölç (information_schema.USER_STATISTICS.CPU_TIME,
// userstat=1), kullanıcıyı panelde hostinge → bayiye eşle, kayan pencerede çekirdek cinsinden
// hızı eşikle karşılaştır ve aşanı KADEMELİ kıs:
//
//  1. MAX_USER_CONNECTIONS düşür (varsayılan 2): eşzamanlı ağır sorgu sayısını sınırlar.
//  2. Kısıt sürüyorsa (varsayılan 45 sn): bu hostingin 10 sn'den uzun OKUMA sorgularını KILL QUERY.
//  3. Soğuma (varsayılan 5 dk eşik altında): önceki değeri geri yaz.
//
// Güvenlik ilkeleri: sistem/servis hesapları (root, panel, pma, roundcube, mail…) ASLA ölçülmez ve
// kısılmaz; ölçüm/DB okunamazsa YENİ kısıt konmaz (var olanlar süre dolunca kalkar); açık kısıtlar
// db_cpu_kisit'te kalıcıdır ve panel açılışında geri alınır; bir kullanıcıdaki hata ötekileri
// etkilemez; SQL'e yalnız doğrulanmış kimlik/host/tamsayı girer.
package dbgovernor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// BirimRef: bir kısıta yol açan eşik birimi (hosting ya da bayi).
type BirimRef struct {
	Tur  string  // "hosting" | "bayi"
	ID   int64   // domain_id | bayi users.id
	Esik float64 // aşılan eşik (çekirdek)
}

// Kisit: bir DB kullanıcısının üzerindeki etkin kısıt (bellek + db_cpu_kisit satırı).
type Kisit struct {
	KayitID    int64
	Kullanici  string
	Kimlik     Kimlik
	Birimler   []BirimRef
	Seviye     int // 1 bağlantı kısıtı; 2 + okuma sorgusu KILL; 3 + bağlantılar KILL CONNECTION ile kesildi
	Onceki     int // kısıttan önceki MAX_USER_CONNECTIONS (0 = sınırsız)
	KisitDeger int
	Baslangic  time.Time
	SonAsim    time.Time
	Tepe       float64 // kısıt süresince en yüksek ölçüm (çekirdek)
	Oldurulen  int
	Kesilen    int  // 3. seviyede kesilen bağlantı sayısı
	Uygulandi  bool // false: gözlem modu — MariaDB'ye dokunulmadı
	Kurtar     bool // açılışta açık bulundu: ilk fırsatta geri al

	sonYaz   time.Time // DB'ye son yazım
	seviye2T time.Time // 2. seviyeye çıkış anı (3. seviye süresi buradan sayılır; yalnız bellek)
	// Ardışık eşik aşımı (yalnız bellek): tırmanma saatleri kısıt başlangıcından değil, KESİNTİSİZ
	// aşımın başladığı andan sayılır. Soğuma beklerken gelen yeni bir sıçrama 1. seviyeden başlar.
	asimBasi    time.Time
	sonEsikUstu time.Time
	// geriYazildi: bırakma başladı — MAX_USER_CONNECTIONS geri yazıldı (ya da yazılıyor), kayıt
	// kapanışı bekliyor. Bu kısıt artık doğrulanmaz/tırmandırılmaz; plan kancası planı aynen geçirir.
	geriYazildi bool
	birakNeden  string // geri yazma başladığında seçilen bitiş nedeni (yeniden denemede korunur)
	// 3. seviye (yalnız bellek): çıkış anı ve son "sürekli aşım" bildirimi.
	seviye3T time.Time
	surekliT time.Time
}

func (k *Kisit) nedenMetni() string {
	var h, b bool
	for _, u := range k.Birimler {
		if u.Tur == "bayi" {
			b = true
		} else {
			h = true
		}
	}
	switch {
	case h && b:
		return "hosting+bayi"
	case b:
		return "bayi"
	}
	return "hosting"
}

func (k *Kisit) enDusukEsik() float64 {
	var e float64
	for i, u := range k.Birimler {
		if i == 0 || u.Esik < e {
			e = u.Esik
		}
	}
	return e
}

// nedenBirimleri: DB'deki neden metninden (yalnız kurtarma için) birim listesi kurar.
func nedenBirimleri(neden string, kim Kimlik, esik float64) []BirimRef {
	var l []BirimRef
	if strings.Contains(neden, "hosting") {
		l = append(l, BirimRef{Tur: "hosting", ID: kim.DomainID, Esik: esik})
	}
	if strings.Contains(neden, "bayi") {
		l = append(l, BirimRef{Tur: "bayi", ID: kim.ResellerID, Esik: esik})
	}
	return l
}

func (k *Kisit) birimEkle(r BirimRef) {
	for i := range k.Birimler {
		if k.Birimler[i].Tur == r.Tur && k.Birimler[i].ID == r.ID {
			k.Birimler[i].Esik = r.Esik
			return
		}
	}
	k.Birimler = append(k.Birimler, r)
}

// Durum: kısıcının anlık durumu (GET /db-cpu/durum).
type Durum struct {
	Mod            Mod       `json:"mod"`
	Userstat       string    `json:"userstat"` // acik | bekliyor | hata
	UserstatKalici bool      `json:"userstat_kalici"`
	SonYoklama     time.Time `json:"son_yoklama"`
	SonHata        string    `json:"son_hata,omitempty"`
	AktifKisit     int       `json:"aktif_kisit"`
	Yoklama        int64     `json:"yoklama_sayisi"`
	PollSn         int       `json:"poll_sn"`
	PencereSn      int       `json:"pencere_sn"`
}

// Olay: Motor'un ürettiği denetim/bildirim olaylarının kaydı (testler ve hata ayıklama için).
type Olay struct {
	Tur    string // "denetim" | "bildirim"
	Eylem  string // denetim eylem kodu | bildirim başlığı
	Hedef  string
	Detay  string
	Domain int64
	Bayi   int64
}

const (
	kimlikTTL = 60 * time.Second
	esikTTL   = 60 * time.Second
	yazAralik = 60 * time.Second
	dilimSn   = 300
)

type kimlikKaydi struct {
	k  Kimlik
	ts time.Time
}
type esikKaydi struct {
	v  float64
	ts time.Time
}

// Motor: kısıcı. Tick tek goroutine'den çağrılır; BaglantiKancasi ve Durum başka goroutine'lerden.
type Motor struct {
	// DB: yalnız bildirim/denetim yazıcıları için (nil → yazılmaz; olaylar yine de Olaylar()'a düşer).
	DB      *sql.DB
	Depo    Depo
	Olcer   Olcer
	Yon     Yonetici
	AyarOku func(ctx context.Context) (Ayar, error)
	Simdi   func() time.Time
	NCPU    float64
	// UserstatCnf: kalıcı yapılandırma dosyası; "" ise yazılmaz.
	UserstatCnf string

	mu        sync.Mutex
	aktif     map[string]*Kisit
	planAnlik map[string]int
	// kapananOldurulen: hostingin bırakılmış kullanıcılarının öldürdüğü okuma sorgusu toplamı (hostingin son
	// kullanıcısı bırakılınca "kısıt kaldırıldı" denetimi TÜM kullanıcıların toplamını yazsın).
	kapananOldurulen map[int64]int
	durum            Durum
	olaylar          []Olay

	// yoklama durumu (yalnız Tick)
	onceki      map[string]float64
	sonT        time.Time
	halkalar    map[string]*halka
	kimlikOn    map[string]kimlikKaydi
	esikOn      map[int64]esikKaydi
	yoklama     int64
	sonTemizlik time.Time
	userstatTur int
	// sonTur: son Tick'in süresi (boşluk eşiği yavaş turlara uyarlanır).
	sonTur time.Duration
	// uzun: kullanıcı → QueryID → son görülen süre (CPU yakan uzun deyimler; canlı gösterge bunları sayar).
	uzun map[string]map[int64]time.Duration
	// kredi: kullanıcı → BİTMİŞ uzun deyimlerin canlı gösterge ile ZATEN sayılmış CPU saniyesi.
	kredi map[string]*krediHavuzu
}

func (m *Motor) simdi() time.Time {
	if m.Simdi != nil {
		return m.Simdi()
	}
	return time.Now()
}

func (m *Motor) hazirla() {
	if m.aktif == nil {
		m.aktif = map[string]*Kisit{}
	}
	if m.planAnlik == nil {
		m.planAnlik = map[string]int{}
	}
	if m.kapananOldurulen == nil {
		m.kapananOldurulen = map[int64]int{}
	}
	if m.onceki == nil {
		m.onceki = map[string]float64{}
	}
	if m.halkalar == nil {
		m.halkalar = map[string]*halka{}
	}
	if m.kimlikOn == nil {
		m.kimlikOn = map[string]kimlikKaydi{}
	}
	if m.esikOn == nil {
		m.esikOn = map[int64]esikKaydi{}
	}
	if m.uzun == nil {
		m.uzun = map[string]map[int64]time.Duration{}
	}
	if m.kredi == nil {
		m.kredi = map[string]*krediHavuzu{}
	}
	if m.NCPU <= 0 {
		m.NCPU = 1
	}
}

func (m *Motor) hata(s string) {
	m.mu.Lock()
	m.durum.SonHata = s
	m.mu.Unlock()
	if s != "" {
		log.Printf("DB kısıcı: %s", s)
	}
}

func (m *Motor) olayKaydet(o Olay) {
	m.mu.Lock()
	if len(m.olaylar) >= 500 {
		m.olaylar = m.olaylar[1:]
	}
	m.olaylar = append(m.olaylar, o)
	m.mu.Unlock()
}

// Olaylar: kaydedilen olayların kopyası (testler).
func (m *Motor) Olaylar() []Olay {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Olay(nil), m.olaylar...)
}

// DurumAl: anlık durum kopyası.
func (m *Motor) DurumAl() Durum {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.durum
	d.AktifKisit = len(m.aktif)
	return d
}

// AktifKullanicilar: etkin kısıtlı DB kullanıcıları (sıralı; testler).
func (m *Motor) AktifKullanicilar() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var l []string
	for u := range m.aktif {
		l = append(l, u)
	}
	sort.Strings(l)
	return l
}

// AktifKisit: bir kullanıcının etkin kısıtının kopyası.
func (m *Motor) AktifKisit(u string) (Kisit, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.aktif[u]; ok {
		return *k, true
	}
	return Kisit{}, false
}

// baglantiKancasi: kaynaklimit.MySQLLimitUygula plan değerini yazarken kısıtlı kullanıcıda kısıtın
// ÜSTÜNE çıkmasın. Plan değeri not edilir: kısıt kalkınca güncel plan değeri geri yazılır.
func (m *Motor) baglantiKancasi(user string, plan int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.aktif[user]
	if k == nil || !k.Uygulandi {
		return plan
	}
	m.planAnlik[user] = plan
	if k.geriYazildi {
		return plan // kısıt bırakılıyor: planı kısıt değeriyle ezip kalıcı düşük sınır bırakma
	}
	if plan > 0 && plan < k.KisitDeger {
		return plan
	}
	return k.KisitDeger
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// kimlikler: kullanıcı → Kimlik (60 sn önbellek). Hata → (nil, err).
func (m *Motor) kimlikler(ctx context.Context, kullanicilar []string, now time.Time) (map[string]Kimlik, error) {
	out := make(map[string]Kimlik, len(kullanicilar))
	var eksik []string
	for _, u := range kullanicilar {
		if c, ok := m.kimlikOn[u]; ok && now.Sub(c.ts) < kimlikTTL {
			out[u] = c.k
		} else {
			eksik = append(eksik, u)
		}
	}
	if len(eksik) > 0 {
		yeni, err := m.Depo.Esle(ctx, eksik)
		if err != nil {
			return nil, err
		}
		for _, u := range eksik {
			k, ok := yeni[u]
			if !ok {
				k = Kimlik{Yok: true}
			}
			m.kimlikOn[u] = kimlikKaydi{k: k, ts: now}
			out[u] = k
		}
	}
	// önbellek şişmesin
	if len(m.kimlikOn) > 20000 {
		for u, c := range m.kimlikOn {
			if now.Sub(c.ts) > 10*kimlikTTL {
				delete(m.kimlikOn, u)
			}
		}
	}
	return out, nil
}

// hostingEsik: hostingin eşiği (60 sn önbellek). Okunamazsa ok=false (o hostinge YENİ kısıt konmaz).
func (m *Motor) hostingEsik(ctx context.Context, domainID int64, ayar Ayar, now time.Time) (float64, bool) {
	if c, ok := m.esikOn[domainID]; ok && now.Sub(c.ts) < esikTTL {
		return c.v, true
	}
	v, err := m.Depo.HostingEsik(ctx, domainID, ayar.EsikHosting)
	if err != nil {
		m.hata(fmt.Sprintf("hosting %d eşiği okunamadı: %v", domainID, err))
		return 0, false
	}
	m.esikOn[domainID] = esikKaydi{v: v, ts: now}
	return v, true
}

// turSiniri: boşluk eşiğine yansıyan en uzun tur süresi (Tick bağlamı 2 dk ile sınırlı).
const turSiniri = 90 * time.Second

// krediOmur: bitmiş deyimin kredisinin geçerli kaldığı tur sayısı (sayaç, deyimin bitişinden en çok
// bu kadar tur sonra görünür: ölçüm ve işlem listesi aynı anda okunmaz).
const krediOmur = 2

// krediHavuzu: bitmiş uzun CPU deyimlerinden kalan "zaten canlı gösterge ile sayıldı" CPU saniyesi.
type krediHavuzu struct {
	sn  float64
	yas int
}

// krediGuncelle: bu turun işlem listesinden uzun CPU deyimlerini izler; önceki turda görünüp artık
// listede OLMAYAN (bitmiş/öldürülmüş) deyimin son görülen süresi kullanıcının kredisine eklenir.
// Kredi yalnız BİTEN deyimden doğar (sonsuza dek süren sahte "canlı" deyim kredi biriktirmez:
// yoğun kısa sorgu yükü tam algılanır); deyimin süresi CPU'sunun üst sınırıdır. Kötüye kullanım
// sınırı: en kötü durumda gizlenebilen CPU, canlı göstergenin zaman integralini (≈ eşik x süre) aşmaz.
func (m *Motor) krediGuncelle(calisan []Sorgu, ayar Ayar, bosluk bool) {
	tum := map[int64]bool{}
	yeni := map[string]map[int64]time.Duration{}
	for _, q := range calisan {
		if q.QueryID <= 0 {
			continue
		}
		tum[q.QueryID] = true
		if q.Sure >= ayar.UzunSorgu && cpuYakiyorMu(q.Durum) {
			if yeni[q.Kullanici] == nil {
				yeni[q.Kullanici] = map[int64]time.Duration{}
			}
			yeni[q.Kullanici][q.QueryID] = q.Sure
		}
	}
	if bosluk {
		m.kredi = map[string]*krediHavuzu{}
	}
	for u, onceki := range m.uzun {
		for id, sure := range onceki {
			if tum[id] { // hâlâ çalışıyor (durumu değişmiş olabilir): izlemeye devam, ikinci kez kredi yok
				if yeni[u] == nil {
					yeni[u] = map[int64]time.Duration{}
				}
				if _, ok := yeni[u][id]; !ok {
					yeni[u][id] = sure
				}
				continue
			}
			if bosluk {
				continue
			}
			h := m.kredi[u]
			if h == nil {
				h = &krediHavuzu{}
				m.kredi[u] = h
			}
			h.sn += sure.Seconds()
			h.yas = 0
		}
	}
	m.uzun = yeni
}

// krediDus: ham deltadan kullanıcının kredisini düşer; karar örneğine girecek CPU'yu döner.
func (m *Motor) krediDus(u string, ham float64) float64 {
	h := m.kredi[u]
	if h == nil || h.sn <= 0 || ham <= 0 {
		return ham
	}
	t := ham
	if t > h.sn {
		t = h.sn
	}
	h.sn -= t
	return ham - t
}

// krediYaslan: tüketilmemiş kredi krediOmur tur sonra düşer (sayaç hiç gelmediyse başka CPU'yu örtmesin).
func (m *Motor) krediYaslan() {
	for u, h := range m.kredi {
		h.yas++
		if h.yas > krediOmur || h.sn <= 0 {
			delete(m.kredi, u)
		}
	}
}

type domToplam struct {
	id, rid      int64
	alan         string
	askida       bool
	cpu, canli   float64
	kullanicilar []string
	pay          map[string]float64 // kullanıcı → penceredeki tüketim (çekirdek): max(cpu hızı, canlı gösterge)
}

func (d *domToplam) eff() float64 { return maxf(d.cpu, d.canli) }

// Tick: tek ölçüm + karar turu.
func (m *Motor) Tick(ctx context.Context) {
	m.hazirla()
	now := m.simdi()
	defer func() {
		s := m.simdi().Sub(now)
		if s > turSiniri {
			s = turSiniri
		}
		if s > 0 {
			m.sonTur = s
		}
	}()
	m.mu.Lock()
	m.yoklama++
	m.durum.Yoklama = m.yoklama
	m.durum.SonYoklama = now
	m.mu.Unlock()

	ayar, err := m.AyarOku(ctx)
	if err != nil {
		m.hata("ayar okunamadı: " + err.Error())
		m.birakPass(ctx, now, VarsayilanAyar(), nil)
		return
	}
	m.mu.Lock()
	m.durum.Mod, m.durum.PollSn, m.durum.PencereSn = ayar.Mod, int(ayar.Poll.Seconds()), int(ayar.Pencere.Seconds())
	m.mu.Unlock()

	if ayar.Mod == ModKapali {
		m.birakPass(ctx, now, ayar, nil)
		m.hata("")
		return
	}
	m.userstatHazirla(ctx)

	cum, e1 := m.Olcer.KullaniciCPU(ctx)
	var calisan []Sorgu
	var e2 error
	if e1 == nil {
		calisan, e2 = m.Olcer.Calisanlar(ctx)
	}
	if e1 != nil || e2 != nil {
		// FAIL-SAFE: ölçemiyorsak yeni kısıt koymayız; var olanlar süre dolunca kalkar.
		hata := e1
		if hata == nil {
			hata = e2
		}
		m.hata("MariaDB ölçümü okunamadı: " + hata.Error())
		m.sonT = time.Time{}
		m.birakPass(ctx, now, ayar, nil)
		return
	}

	dt := 0.0
	bosluk := m.sonT.IsZero()
	if !bosluk {
		dt = now.Sub(m.sonT).Seconds()
		// Eşik yavaş turlara uyarlanır: tur 25 sn sürüyorsa aralık 25 sn + Poll olur; sabit 2,5xPoll bunu
		// "kesinti" sayıp pencereyi her turda sıfırlar (kısıcı tam DB yüklüyken kör kalırdı).
		ref := ayar.Poll
		if m.sonTur > ref {
			ref = m.sonTur
		}
		if dt <= 0 || dt > 2.5*ref.Seconds() {
			bosluk = true // uzun kesinti: biriken CPU'yu bu aralığa yüklemek sahte sıçrama olur
		}
	}
	deltalar := map[string]float64{}
	for u, c := range cum {
		p, var_ := m.onceki[u]
		switch {
		case bosluk:
			// yalnız taban
		case !var_:
			deltalar[u] = c // yeni görülen kullanıcı: sayaç userstat açıldığından beri bu aralıkta doğdu
		case c >= p:
			deltalar[u] = c - p
		default:
			deltalar[u] = c // sayaç sıfırlandı (MariaDB yeniden başladı / FLUSH USER_STATISTICS)
		}
		m.onceki[u] = c
	}
	m.sonT = now

	canli := map[string]float64{}
	for _, q := range calisan {
		if q.Sure >= ayar.UzunSorgu && cpuYakiyorMu(q.Durum) {
			canli[q.Kullanici]++
		}
	}
	m.krediGuncelle(calisan, ayar, bosluk)

	// aday kullanıcılar: etkinlik gösteren, penceresi dolu olan ve etkin kısıtlı kiracı kullanıcıları
	adSet := map[string]bool{}
	for u, d := range deltalar {
		if d > 0 {
			adSet[u] = true
		}
	}
	for u := range canli {
		adSet[u] = true
	}
	for u := range m.halkalar {
		adSet[u] = true
	}
	m.mu.Lock()
	for u := range m.aktif {
		adSet[u] = true
	}
	m.mu.Unlock()
	var adaylar []string
	for u := range adSet {
		if kullaniciGecerli(u) {
			adaylar = append(adaylar, u)
		}
	}
	sort.Strings(adaylar)
	kim, err := m.kimlikler(ctx, adaylar, now)
	if err != nil {
		m.hata("panel veritabanı okunamadı (kullanıcı eşleme): " + err.Error())
		m.birakPass(ctx, now, ayar, nil)
		return
	}
	m.hata("")

	if bosluk {
		m.halkalar = map[string]*halka{}
		m.birakPass(ctx, now, ayar, kim)
		return
	}

	// örnekleri ekle + ölçüm dilimi satırları
	sinir := m.NCPU * dt
	var satirlar []OlcumSatiri
	for _, u := range adaylar {
		k := kim[u]
		if k.Yok {
			delete(m.halkalar, u)
			delete(m.kredi, u)
			continue
		}
		h := m.halkalar[u]
		if h == nil {
			h = &halka{}
			m.halkalar[u] = h
		}
		h.kimlik = k
		ham := deltalar[u]
		// Karar örneği: sayaçtaki CPU deyim BİTİNCE görünür; bitmiş uzun deyimin CPU'su çalışırken canlı
		// gösterge (~1 çekirdek) olarak zaten sayıldı. Çift sayım karar örneğinden düşülür; db_cpu_olcum'a
		// (muhasebe) HAM değer yazılır.
		d := m.krediDus(u, ham)
		if d > sinir {
			d = sinir // fiziksel üst sınır (NCPU x dt)
		}
		h.ekle(ornek{t: now, dt: dt, cpu: d, canli: canli[u]}, ayar.Pencere)
		if ham > 0 {
			satirlar = append(satirlar, OlcumSatiri{Kullanici: u, DomainID: k.DomainID, CPUms: int64(ham*1000 + 0.5)})
		}
	}
	m.krediYaslan()
	for u, h := range m.halkalar {
		h.buda(now, ayar.Pencere)
		if h.bos() {
			delete(m.halkalar, u)
		}
	}
	if len(satirlar) > 0 {
		if err := m.Depo.OlcumYaz(ctx, now.Unix()/dilimSn*dilimSn, satirlar); err != nil {
			m.hata("ölçüm yazılamadı: " + err.Error())
		}
	}

	// hosting / bayi toplamları
	doms := map[int64]*domToplam{}
	for u, h := range m.halkalar {
		c, g := h.oranlar(now, ayar.Pencere)
		d := doms[h.kimlik.DomainID]
		if d == nil {
			d = &domToplam{id: h.kimlik.DomainID, rid: h.kimlik.ResellerID, alan: h.kimlik.AlanAdi, askida: h.kimlik.Askida}
			doms[h.kimlik.DomainID] = d
		}
		d.cpu += c
		d.canli += g
		d.kullanicilar = append(d.kullanicilar, u)
		if d.pay == nil {
			d.pay = map[string]float64{}
		}
		d.pay[u] = maxf(c, g)
	}
	bayiCPU, bayiCanli := map[int64]float64{}, map[int64]float64{}
	bayiDomlar := map[int64][]*domToplam{}
	for _, d := range doms {
		if d.rid > 0 {
			bayiCPU[d.rid] += d.cpu
			bayiCanli[d.rid] += d.canli
			bayiDomlar[d.rid] = append(bayiDomlar[d.rid], d)
		}
	}
	bayiEff := func(rid int64) float64 { return maxf(bayiCPU[rid], bayiCanli[rid]) }

	if ayar.Mod == ModUygula || ayar.Mod == ModGozlem {
		m.esikleriDegerlendir(ctx, now, ayar, calisan, doms, bayiDomlar, bayiEff)
	}

	// soğuma kanıtı + tepe: etkin kısıtların birimleri hâlâ (eşik x oran) üstünde mi?
	m.mu.Lock()
	for _, k := range m.aktif {
		for _, r := range k.Birimler {
			var e float64
			switch r.Tur {
			case "hosting":
				if d := doms[r.ID]; d != nil {
					e = d.eff()
				}
			case "bayi":
				e = bayiEff(r.ID)
			}
			if e > r.Esik*ayar.SogumaOrani {
				k.SonAsim = now
			}
			k.Tepe = maxf(k.Tepe, e)
		}
	}
	m.mu.Unlock()

	if ayar.Mod == ModUygula {
		m.killPass(ctx, ayar, calisan)
		m.dogrulaPass(ctx)
	}
	m.birakPass(ctx, now, ayar, kim)
	m.yazPass(ctx, now)

	if now.Sub(m.sonTemizlik) > time.Hour {
		m.sonTemizlik = now
		if err := m.Depo.Temizle(ctx, now); err != nil {
			m.hata("eski ölçümler temizlenemedi: " + err.Error())
		}
	}
}

type asimBirim struct {
	ref  BirimRef
	dom  *domToplam
	olc  float64
	baya string
}

// esikleriDegerlendir: eşiği aşan hosting/bayi birimlerini bulur ve kısar.
func (m *Motor) esikleriDegerlendir(ctx context.Context, now time.Time, ayar Ayar, calisan []Sorgu,
	doms map[int64]*domToplam, bayiDomlar map[int64][]*domToplam, bayiEff func(int64) float64) {

	var asimlar []asimBirim
	// hosting eşiği
	ids := make([]int64, 0, len(doms))
	for id := range doms {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		d := doms[id]
		if d.eff() < MinEsik || d.askida {
			continue // en düşük olası eşiğin altı: eşik sorgusuna bile gerek yok
		}
		esik, ok := m.hostingEsik(ctx, d.id, ayar, now)
		if !ok {
			continue
		}
		if d.eff() > esik {
			asimlar = append(asimlar, asimBirim{ref: BirimRef{Tur: "hosting", ID: d.id, Esik: esik}, dom: d, olc: d.eff()})
		}
	}
	// bayi eşiği: sınırı aşan bayide, çıkarılınca bayiyi sınırın altına indirecek en büyük
	// tüketicilerden başlayarak hostingler seçilir.
	rids := make([]int64, 0, len(bayiDomlar))
	for rid := range bayiDomlar {
		rids = append(rids, rid)
	}
	sort.Slice(rids, func(i, j int) bool { return rids[i] < rids[j] })
	for _, rid := range rids {
		toplam := bayiEff(rid)
		if toplam <= 0 {
			continue
		}
		limit, ok, err := m.Depo.BayiCPU(ctx, rid)
		if err != nil {
			m.hata(fmt.Sprintf("bayi %d CPU sınırı okunamadı: %v", rid, err))
			continue
		}
		if !ok || toplam <= limit {
			continue
		}
		ds := append([]*domToplam(nil), bayiDomlar[rid]...)
		sort.Slice(ds, func(i, j int) bool {
			if ds[i].eff() != ds[j].eff() {
				return ds[i].eff() > ds[j].eff()
			}
			return ds[i].id < ds[j].id
		})
		kalan := toplam
		for _, d := range ds {
			if d.askida {
				continue
			}
			asimlar = append(asimlar, asimBirim{ref: BirimRef{Tur: "bayi", ID: rid, Esik: limit}, dom: d, olc: toplam})
			kalan -= d.eff()
			if kalan <= limit {
				break
			}
		}
	}
	for _, a := range asimlar {
		m.birimKisitla(ctx, now, ayar, a)
	}
}
