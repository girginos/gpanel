// hiz.go — WordPress Toolkit hız katmanı.
//
// Ölçülen kök neden (bkz. görev raporu): liste/durum uçları her istekte site başına
// wp-cli (PHP + WP bootstrap, 1-5 sn) çalıştırıyordu; önbellek yoktu, wordpress.org
// güncelleme kontrolü istek içinde ve her site için ayrı yapılıyordu.
//
// Bu dosya:
//   - sürümü wp-cli'siz, dosyadan (symlink-güvenli openat2) okur;
//   - çekirdek güncelleme kontrolünü SÜRÜM başına tek (tekilleştirilmiş, TTL'li) HTTP
//     isteğine indirir — kiracı PHP'si hiç çalışmaz;
//   - ağır wp-cli çıktılarını (eklenti/tema/kullanıcı listesi, DB boyutu) TTL'li,
//     tekilleştirilmiş, eşzamanlılığı sınırlı bir önbellekten sunar;
//   - mutasyonlarda önbelleği geçersiz kılar (nesil sayacıyla; uçuştaki eski sonuç yazılmaz).
//
// GÜVENLİK: dosyadan/ağdan/wp-cli'den gelen her değer kiracı kontrollü ya da dış
// kaynaklıdır → katı biçim doğrulaması (reSurum) olmadan hiçbir yere gitmez. Önbellek
// anahtarı kiracıyı (sk) ve mutlak dizini içerir; çapraz-kiracı paylaşımı yoktur.
package wordpress

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// wpStdoutFn: test kancası (gerçek wp-cli yerine sahte betik). Üretimde wpStdout.
var wpStdoutFn = wpStdout

// wpEsZamanli: eşzamanlı wp-cli OKUMA işi üst sınırı (liste/durum). Panel cgroup'unda
// koşan ağır işi sınırlar; istek sayısından bağımsız.
const wpEsZamanli = 4

var wpOkuSem = make(chan struct{}, wpEsZamanli)

// wpOku: wp-cli'yi (hafif bootstrap: eklenti/tema yüklemeden) eşzamanlılık sınırı ve
// zaman aşımıyla çalıştırır. Sıra beklemesi de ctx'e tabidir.
func wpOku(ctx context.Context, sk string, args ...string) ([]byte, error) {
	select {
	case wpOkuSem <- struct{}{}:
		defer func() { <-wpOkuSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	args = append(args, "--skip-plugins", "--skip-themes")
	return wpStdoutFn(ctx, sk, args...)
}

// ---------------------------------------------------------------- sürüm (dosyadan)

// reSurum: WordPress sürüm dizgisi (ör. 6.4.2, 6.5, 7.0-beta1). Kiracı dosyasından ve
// dış API'den gelen değer bununla DOĞRULANMADAN kullanılmaz.
var reSurum = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){1,2}(-[A-Za-z0-9.]{1,24})?$`)

var reSurumSatir = regexp.MustCompile(`(?m)^\s*\$wp_version\s*=\s*'([^'\r\n]{1,40})'\s*;`)

// surumOku: dir/wp-includes/version.php içinden $wp_version değerini wp-cli'siz okur.
// openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS) ile /home/<sk> altında kalır (kiracı
// symlink'iyle root okumasını başka yere çeviremez), O_NONBLOCK ile FIFO'da takılmaz,
// yalnız düz dosya, en çok 32 KiB. Okunamazsa/biçim dışıysa "" döner.
func surumOku(sk, dir string) string {
	home := "/home/" + sk
	rel, err := filepath.Rel(home, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return surumOkuRel(home, filepath.Join(rel, "wp-includes", "version.php"))
}

func surumOkuRel(home, rel string) string {
	hf, err := unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer unix.Close(hf)
	fd, err := unix.Openat2(hf, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return ""
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(f, 32<<10))
	m := reSurumSatir.FindSubmatch(b)
	if len(m) != 2 || !reSurum.Match(m[1]) {
		return ""
	}
	return string(m[1])
}

// ---------------------------------------------------------------- çekirdek güncelleme

// cekGunc: bir sürüm için güncelleme sonucu.
type cekGunc struct {
	Bilinen bool   // false → kontrol yapılamadı ("bilinmiyor")
	Var     bool   // güncelleme var
	Hedef   string // hedef sürüm (Var ise)
}

type cekOge struct {
	sonuc    cekGunc
	gecerli  time.Time
	bekleyen chan struct{} // uçuşta ise dolu
}

// cekirdekKontrol: wordpress.org version-check önbelleği (sürüm başına tekilleştirilmiş).
type cekirdekKontrol struct {
	mu      sync.Mutex
	m       map[string]*cekOge
	api     string
	istemci *http.Client
	sem     chan struct{}
	simdi   func() time.Time
	ttlOK   time.Duration
	ttlHata time.Duration
	azami   int
}

const cekirdekAPI = "https://api.wordpress.org/core/version-check/1.7/"

func yeniCekirdekKontrol(api string) *cekirdekKontrol {
	return &cekirdekKontrol{
		m: map[string]*cekOge{}, api: api,
		istemci: &http.Client{Timeout: 8 * time.Second},
		sem:     make(chan struct{}, wpEsZamanli),
		simdi:   time.Now,
		ttlOK:   6 * time.Hour, ttlHata: 3 * time.Minute,
		azami: 256,
	}
}

var cekKontrol = yeniCekirdekKontrol(cekirdekAPI)

// sorgu: sürüm için güncelleme durumu. Taze kayıt varsa ANINDA; uçuşta olan sorguya
// katılır; yoksa tek HTTP isteği atar. Hata → Bilinen=false (kısa TTL ile saklanır, her
// istekte dışarıya yeniden gidilmez).
func (c *cekirdekKontrol) sorgu(ctx context.Context, surum string) cekGunc {
	if !reSurum.MatchString(surum) {
		return cekGunc{}
	}
	for {
		c.mu.Lock()
		e := c.m[surum]
		if e != nil && e.bekleyen == nil && c.simdi().Before(e.gecerli) {
			s := e.sonuc
			c.mu.Unlock()
			return s
		}
		if e != nil && e.bekleyen != nil {
			ch := e.bekleyen
			c.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return cekGunc{}
			}
		}
		// Kiracı sahte sürümlerle haritayı/dış isteği şişiremesin.
		if e == nil && len(c.m) >= c.azami {
			c.sureDolanlariAt()
			if len(c.m) >= c.azami {
				c.mu.Unlock()
				return cekGunc{}
			}
		}
		e = &cekOge{bekleyen: make(chan struct{})}
		c.m[surum] = e
		c.mu.Unlock()

		// Ortak iş tek isteğin bağlamına bağlanmasın.
		gctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
		s := c.getir(gctx, surum)
		iptal()

		c.mu.Lock()
		e.sonuc = s
		ttl := c.ttlOK
		if !s.Bilinen {
			ttl = c.ttlHata
		}
		e.gecerli = c.simdi().Add(ttl)
		ch := e.bekleyen
		e.bekleyen = nil
		c.mu.Unlock()
		close(ch)
		return s
	}
}

// sureDolanlariAt: mu kilitliyken çağrılır.
func (c *cekirdekKontrol) sureDolanlariAt() {
	now := c.simdi()
	for k, e := range c.m {
		if e.bekleyen == nil && !now.Before(e.gecerli) {
			delete(c.m, k)
		}
	}
}

func (c *cekirdekKontrol) getir(ctx context.Context, surum string) cekGunc {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return cekGunc{}
	}
	u := c.api + "?version=" + url.QueryEscape(surum) + "&locale=en_US"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return cekGunc{}
	}
	req.Header.Set("User-Agent", "GirginOSPanel-WPToolkit")
	resp, err := c.istemci.Do(req)
	if err != nil {
		return cekGunc{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cekGunc{}
	}
	var yanit struct {
		Offers []struct {
			Response string `json:"response"`
			Current  string `json:"current"`
		} `json:"offers"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 512<<10)).Decode(&yanit) != nil || len(yanit.Offers) == 0 {
		return cekGunc{}
	}
	o := yanit.Offers[0] // wp-cli check-update ile aynı: ilk teklif = en yeni
	switch o.Response {
	case "upgrade":
		// Dış veri: yalnız biçimi geçerli ve mevcuttan farklı sürüm hedef olur.
		if reSurum.MatchString(o.Current) && o.Current != surum {
			return cekGunc{Bilinen: true, Var: true, Hedef: o.Current}
		}
		return cekGunc{Bilinen: true}
	case "latest":
		return cekGunc{Bilinen: true}
	}
	return cekGunc{}
}

// ---------------------------------------------------------------- TTL önbellek

type onbOge struct {
	deger    any
	gecerli  time.Time
	bekleyen chan struct{}
}

// onbellek: TTL'li, anahtar başına tekilleştirilmiş (single-flight) önbellek.
// Hatalar önbelleğe ALINMAZ. Sil() nesli artırır: Sil'den önce başlamış işin sonucu
// yazılmaz (mutasyondan sonra bayat veri dönmez).
type onbellek struct {
	mu    sync.Mutex
	m     map[string]*onbOge
	nesil uint64
	ttl   time.Duration
	azami int
	simdi func() time.Time
}

func yeniOnbellek(ttl time.Duration, azami int) *onbellek {
	return &onbellek{m: map[string]*onbOge{}, ttl: ttl, azami: azami, simdi: time.Now}
}

func onbAnahtar(sk, dir, tur string) string { return sk + "\x00" + dir + "\x00" + tur }

// Bak: taze değer varsa döner (iş başlatmaz).
func (o *onbellek) Bak(key string) (any, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if e := o.m[key]; e != nil && e.bekleyen == nil && o.simdi().Before(e.gecerli) {
		return e.deger, true
	}
	return nil, false
}

// Yap: taze değer varsa onu; uçuştaki işe katılır; yoksa fn'i (kendi 40sn bağlamında)
// çalıştırır. Bekleme ctx'e tabidir, ortak iş tek isteğe bağlı değildir.
func (o *onbellek) Yap(ctx context.Context, key string, sure time.Duration, fn func(context.Context) (any, error)) (any, error) {
	for {
		o.mu.Lock()
		e := o.m[key]
		if e != nil && e.bekleyen == nil && o.simdi().Before(e.gecerli) {
			d := e.deger
			o.mu.Unlock()
			return d, nil
		}
		if e != nil && e.bekleyen != nil {
			ch := e.bekleyen
			o.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		eski := e
		e = &onbOge{bekleyen: make(chan struct{})}
		if eski != nil && eski.deger != nil {
			// yenileme sürerken bayat değer okunabilir kalır (BayatBak); Sil bunu temizler.
			e.deger, e.gecerli = eski.deger, eski.gecerli
		}
		o.sigdir(key)
		o.m[key] = e
		nesil := o.nesil
		o.mu.Unlock()

		gctx, iptal := context.WithTimeout(context.Background(), sure)
		d, err := fn(gctx)
		iptal()

		o.mu.Lock()
		ch := e.bekleyen
		e.bekleyen = nil
		if err == nil && nesil == o.nesil {
			e.deger, e.gecerli = d, o.simdi().Add(o.ttl)
		} else if o.m[key] == e {
			delete(o.m, key)
		}
		o.mu.Unlock()
		close(ch)
		return d, err
	}
}

// Arka: istek beklemeden arka planda hesaplatır (taze/uçuşta ise no-op).
func (o *onbellek) Arka(key string, sure time.Duration, fn func(context.Context) (any, error)) {
	if _, ok := o.Bak(key); ok {
		return
	}
	o.mu.Lock()
	if e := o.m[key]; e != nil && e.bekleyen != nil {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()
	go func() { _, _ = o.Yap(context.Background(), key, sure, fn) }()
}

// sigdir: mu kilitliyken; taşmada süresi dolanları, yetmezse rastgele birini atar.
func (o *onbellek) sigdir(key string) {
	if _, varMi := o.m[key]; varMi || len(o.m) < o.azami {
		return
	}
	now := o.simdi()
	for k, e := range o.m {
		if e.bekleyen == nil && !now.Before(e.gecerli) {
			delete(o.m, k)
		}
	}
	for k, e := range o.m {
		if len(o.m) < o.azami {
			break
		}
		if e.bekleyen == nil {
			delete(o.m, k)
		}
	}
}

// Sil: önek ile başlayan tüm kayıtları atar ve nesli artırır.
func (o *onbellek) Sil(onek string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nesil++
	for k, e := range o.m {
		if !strings.HasPrefix(k, onek) {
			continue
		}
		if e.bekleyen == nil {
			delete(o.m, k)
		} else {
			// uçuştaki yenilemenin eski (mutasyon öncesi) değeri artık sunulmaz.
			e.deger, e.gecerli = nil, time.Time{}
		}
	}
}

// BayatBak: değer taze değilse bile, süresi dolalı en çok bayat kadar olmuşsa döner
// (yenileme sürüyor olsa da). Sil'den sonra hiçbir bayat değer kalmaz.
func (o *onbellek) BayatBak(key string, bayat time.Duration) (any, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if e := o.m[key]; e != nil && e.deger != nil && o.simdi().Before(e.gecerli.Add(bayat)) {
		return e.deger, true
	}
	return nil, false
}

// YapBayat (stale-while-revalidate): taze → onu; bayat penceresinde → bayat değeri HEMEN
// döner ve yenileme arka planda başlar; hiç yoksa Yap gibi bekler. Hata önbelleğe alınmaz.
func (o *onbellek) YapBayat(ctx context.Context, key string, sure, bayat time.Duration, fn func(context.Context) (any, error)) (any, error) {
	if d, ok := o.Bak(key); ok {
		return d, nil
	}
	if d, ok := o.BayatBak(key, bayat); ok {
		o.Arka(key, sure, fn)
		return d, nil
	}
	return o.Yap(ctx, key, sure, fn)
}

var (
	// listeOnb: eklenti/tema/kullanıcı listesi (gonderJSON'dan geçmiş, doğrulanmış dizi).
	listeOnb = yeniOnbellek(60*time.Second, 1024)
	// agirOnb: DB boyutu gibi ağır, nadir değişen alanlar (arka planda hesaplanır).
	agirOnb = yeniOnbellek(10*time.Minute, 2048)
	// siteOnb: wp-cli siteurl (nadiren değişir).
	siteOnb = yeniOnbellek(time.Hour, 2048)
)

// wpOnbellekSil: (kiracı, dizin) için tüm önbellekleri geçersiz kılar — her mutasyon
// SONRASINDA çağrılır (defer).
func wpOnbellekSil(sk, dir string) {
	p := sk + "\x00" + dir + "\x00"
	listeOnb.Sil(p)
	agirOnb.Sil(p)
	siteOnb.Sil(p)
}

// Bayat pencereleri: değer TTL'den sonra da bu süre boyunca HEMEN sunulur, yenileme arka
// planda yürür (stale-while-revalidate). Mutasyon (wpOnbellekSil) bayat değeri de atar.
// WordPress güncelleme bilgisini zaten saatlerce önbellekler → liste bayatlığı zararsız.
const (
	listeBayat = 30 * time.Minute
	dbBayat    = 24 * time.Hour
)

// listeTanim: sekme listesinin wp-cli argv'si + süre sınırı (tek yer: uç ve ön-ısıtma aynı).
func listeTanim(tur, dir string) ([]string, time.Duration) {
	switch tur {
	case "plugin", "theme":
		return []string{tur, "list", "--path=" + dir, "--format=json",
			"--fields=name,status,version,update,update_version"}, 40 * time.Second
	case "user":
		return []string{"user", "list", "--path=" + dir, "--format=json",
			"--fields=ID,user_login,user_email,display_name,roles"}, 30 * time.Second
	}
	return nil, 0
}

// listeTur: tanımlı liste türünü (plugin/theme/user) önbellekli getirir.
func listeTur(ctx context.Context, sk, dir, tur string) []json.RawMessage {
	args, sure := listeTanim(tur, dir)
	if args == nil {
		return []json.RawMessage{}
	}
	return listeJSON(ctx, sk, dir, tur, sure, args...)
}

// listeJSON: wp-cli JSON dizisi çıktısını doğrulayıp önbellekle döner (SWR). Hata → []
// (eski davranış), hata önbelleğe alınmaz.
func listeJSON(ctx context.Context, sk, dir, tur string, sure time.Duration, args ...string) []json.RawMessage {
	d, err := listeOnb.YapBayat(ctx, onbAnahtar(sk, dir, tur), sure, listeBayat, func(c context.Context) (any, error) {
		raw, e := wpOku(c, sk, args...)
		if e != nil {
			return nil, e
		}
		var arr []json.RawMessage
		if bt := strings.TrimSpace(string(raw)); bt != "" {
			if json.Unmarshal([]byte(bt), &arr) != nil {
				return nil, io.ErrUnexpectedEOF
			}
		}
		if arr == nil {
			arr = []json.RawMessage{}
		}
		return arr, nil
	})
	if err != nil {
		return []json.RawMessage{}
	}
	return d.([]json.RawMessage)
}

// dbIlkBekle: Durum yanıtı DB boyutunu en çok bu kadar bekler (sonra yukleniyor=true). Test kancası.
var dbIlkBekle = time.Second

func dbBoyutuCek(sk, dir string) func(context.Context) (any, error) {
	return func(c context.Context) (any, error) {
		b, e := wpOku(c, sk, "db", "size", "--size_format=mb", "--path="+dir)
		if e != nil {
			return nil, e
		}
		return dbBoyutuTemiz(string(b)), nil
	}
}

// dbBoyutuAl: DB boyutu (MB). Önbellekte (bayat olsa da) varsa HEMEN; yoksa en çok bekle
// kadar bekler, yetişmezse yukleniyor=true (iş arkada sürer, istemci yeniden sorar).
func dbBoyutuAl(ctx context.Context, sk, dir string, bekle time.Duration) (mb string, yukleniyor bool) {
	key := onbAnahtar(sk, dir, "dbmb")
	if d, ok := agirOnb.BayatBak(key, dbBayat); ok {
		if _, taze := agirOnb.Bak(key); !taze {
			agirOnb.Arka(key, 40*time.Second, dbBoyutuCek(sk, dir))
		}
		mb, _ = d.(string)
		return mb, false
	}
	type sonuc struct {
		v  string
		ok bool
	}
	ch := make(chan sonuc, 1)
	go func() {
		d, err := agirOnb.Yap(context.Background(), key, 40*time.Second, dbBoyutuCek(sk, dir))
		v, _ := d.(string)
		ch <- sonuc{v, err == nil}
	}()
	t := time.NewTimer(bekle)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.v, false // hata → "" ve yukleniyor=false: istemci "—" gösterir, döngü yok
	case <-t.C:
		return "", true
	case <-ctx.Done():
		return "", true
	}
}

// ---------------------------------------------------------------- arka plan ön-ısıtma

var (
	isitDevam sync.Map     // "sk\x00dir" → iş sürüyor (tekilleştirme)
	isitSayac atomic.Int32 // sırada/çalışan toplam iş (üst sınır)
	isitSem   = make(chan struct{}, 1)
)

const isitAzami = 64

// wpIsit: bir WordPress'in DB boyutu + eklenti/tema/kullanıcı listelerini ARKA PLANDA,
// TEK TEK (genel tek slot: CPU'yu kiracılardan çalmaz) önbelleğe koyar; sekmeye geçildiğinde
// liste hazırdır. Zaten (bayat da olsa) önbellekte olan türler atlanır. Aynı site için
// tekilleştirilir, toplam iş sayısı sınırlıdır; hata sessizce yutulur (istek yolu yeniden dener).
func wpIsit(sk, dir string) {
	k := sk + "\x00" + dir
	if _, var_ := isitDevam.LoadOrStore(k, struct{}{}); var_ {
		return
	}
	if isitSayac.Add(1) > isitAzami {
		isitSayac.Add(-1)
		isitDevam.Delete(k)
		return
	}
	go func() {
		defer func() { isitSayac.Add(-1); isitDevam.Delete(k) }()
		isitSem <- struct{}{}
		defer func() { <-isitSem }()
		ctx, iptal := context.WithTimeout(context.Background(), 3*time.Minute)
		defer iptal()
		if _, ok := agirOnb.BayatBak(onbAnahtar(sk, dir, "dbmb"), dbBayat); !ok {
			dbBoyutuAl(ctx, sk, dir, 40*time.Second)
		}
		for _, tur := range []string{"plugin", "theme", "user"} {
			if ctx.Err() != nil {
				return
			}
			if _, ok := listeOnb.BayatBak(onbAnahtar(sk, dir, tur), listeBayat); ok {
				continue
			}
			listeTur(ctx, sk, dir, tur)
		}
	}()
}

// wpMutasyonSonu: mutasyon SONRASI — önbelleği at (bayat dahil) ve arkada yeniden ısıt.
func wpMutasyonSonu(sk, dir string) {
	wpOnbellekSil(sk, dir)
	wpIsit(sk, dir)
}

// phpSurumu: panelin wp-cli'de kullandığı php'nin sürümü (tüm siteler için aynı) — site
// başına `wp eval` yerine saatte bir hafif sorgu.
var phpOnb = yeniOnbellek(time.Hour, 4)

// phpCalistir: test kancası. Kiracı kimliği/PHP'si GEREKMEZ (-n: ini yok, yalnız sürüm).
var phpCalistir = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "/usr/bin/php", "-n", "-r", "echo PHP_VERSION;").Output()
}

func phpSurumu(ctx context.Context) string {
	d, err := phpOnb.Yap(ctx, "php", 5*time.Second, func(c context.Context) (any, error) {
		b, e := phpCalistir(c)
		return strings.TrimSpace(string(b)), e
	})
	if err != nil {
		return ""
	}
	return d.(string)
}
