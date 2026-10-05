package gdrive

// drive.go — hedef.Hedef'in Google Drive uygulaması.
//
// Eşleme: uzak ad "a/b/c.age" → Drive'da "<Klasor>/a/b" klasör zinciri + "c.age"
// dosyası (Drive yol değil kimlik bilir; zincir adla çözülür, yoksa açılır).
// drive.file kapsamı yalnız uygulamanın kendi oluşturduğu klasör/dosyaları
// gösterir: kullanıcının diğer dosyaları ne listelenir ne silinir.
//
// Atomiklik: resumable yüklemede dosya son parça alınınca oluşur; kopan/iptal
// yüklemede nihai adda dosya ve geçici dosya YOKTUR (oturum bir hafta sonra
// Google'da kendiliğinden düşer). Üzerine yazmada mevcut dosyanın İÇERİĞİ yeni
// oturum tamamlanınca değişir (kimlik korunur, kopya oluşmaz).
//
// Drive aynı klasörde aynı adlı çok dosyaya izin verir: süreç içi ad kilidi
// eşzamanlı aynı-ad yüklemelerini sıraya sokar; yine de çift dosya varsa
// Listele/Indir en yenisini seçer, Sil hepsini siler.

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec G501 -- Drive'ın md5Checksum alanıyla aktarım doğrulaması, güvenlik amaçlı değil
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups/hedef"
)

type driveHedef struct {
	ayar   Ayar
	kimlik Kimlik
	u      uclar
	http   *http.Client
	jeton  *jetonYonetici
	uyku   func(context.Context, time.Duration) error
	simdi  func() time.Time

	kilit    *kilitler
	dizinOnb map[string]string // dizin yolu → klasör kimliği ("" = taban); "klasor" kilidi altında
}

var _ hedef.Hedef = (*driveHedef)(nil)

func (h *driveHedef) Tur() string { return TurAdi }

func (h *driveHedef) Yetenekler() hedef.Yetenek {
	return hedef.YetenekAtomikYukle | hedef.YetenekOnekListele | hedef.YetenekBantSiniri | hedef.YetenekIlerleme
}

func (h *driveHedef) sirlar() []string { return h.jeton.sirlar() }

// ─── kilitler ───────────────────────────────────────────────────────────────

// kilitler — ada göre karşılıklı dışlama; bekleme ctx ile kesilebilir.
type kilitler struct {
	mu sync.Mutex
	m  map[string]*kilitGirdi
}

type kilitGirdi struct {
	ch  chan struct{}
	ref int
}

func (k *kilitler) al(ctx context.Context, ad string) (func(), error) {
	k.mu.Lock()
	g := k.m[ad]
	if g == nil {
		g = &kilitGirdi{ch: make(chan struct{}, 1)}
		k.m[ad] = g
	}
	g.ref++
	k.mu.Unlock()
	birak := func() {
		k.mu.Lock()
		g.ref--
		if g.ref == 0 {
			delete(k.m, ad)
		}
		k.mu.Unlock()
	}
	select {
	case g.ch <- struct{}{}:
		return func() { <-g.ch; birak() }, nil
	case <-ctx.Done():
		birak()
		return nil, ctx.Err()
	}
}

// ─── düşük seviye istek ─────────────────────────────────────────────────────

// istek — kimlikli istek + yeniden deneme: 401'de jeton bir kez yenilenir; 429,
// 5xx ve oran-sınırı 403'lerinde Retry-After ya da üstel geri çekilmeyle en çok
// azamiDeneme kez tekrar. 2xx yanıtı (gövde AÇIK) döner; hata *hedef.Hata'dır.
func (h *driveHedef) istek(ctx context.Context, islem string, yap func(jeton string) (*http.Request, error)) (*http.Response, error) {
	yenilendi := false
	for deneme := 0; ; deneme++ {
		jeton, err := h.jeton.Al(ctx)
		if err != nil {
			return nil, hedef.Sarmala(islem, err, h.sirlar()...)
		}
		req, err := yap(jeton)
		if err != nil {
			return nil, hedef.Sarmala(islem, err, h.sirlar()...)
		}
		req.Header.Set("Authorization", "Bearer "+jeton)
		resp, err := h.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, hedef.Sarmala(islem, ctx.Err())
			}
			if reddiMi(err) || deneme >= azamiDeneme {
				return nil, hedef.Sarmala(islem, agHatasi(err), h.sirlar()...)
			}
			if uerr := h.uyku(ctx, geriCekilme(deneme)); uerr != nil {
				return nil, hedef.Sarmala(islem, uerr)
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		hdr := resp.Header
		a := govdeCoz(resp.StatusCode, govdeOku(resp))
		if resp.StatusCode == http.StatusUnauthorized && !yenilendi {
			yenilendi = true
			h.jeton.Sifirla()
			deneme--
			continue
		}
		if a.yenidenDenenir() && deneme < azamiDeneme {
			bekle, ok := retryAfter(hdr, h.simdi())
			if !ok {
				bekle = geriCekilme(deneme)
			}
			if uerr := h.uyku(ctx, bekle); uerr != nil {
				return nil, hedef.Sarmala(islem, uerr)
			}
			continue
		}
		return nil, a.hata(islem, h.sirlar())
	}
}

// cagri — JSON istek/yanıt (küçük meta çağrıları; 90 sn).
func (h *driveHedef) cagri(ctx context.Context, islem, metod, adres string, govde, cikti any) error {
	ctx, iptal := context.WithTimeout(ctx, 90*time.Second)
	defer iptal()
	var b []byte
	if govde != nil {
		b, _ = json.Marshal(govde)
	}
	resp, err := h.istek(ctx, islem, func(string) (*http.Request, error) {
		var rd io.Reader
		if b != nil {
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, metod, adres, rd)
		if err == nil && b != nil {
			req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		}
		return req, err
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if cikti == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, govdeSiniri))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(cikti); err != nil {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, islem, errors.New("drive: yanıt çözülemedi"), h.sirlar()...)
	}
	return nil
}

// ─── Drive meta ─────────────────────────────────────────────────────────────

type dosyaMeta struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     string `json:"size"`
	MD5      string `json:"md5Checksum"`
	Modified string `json:"modifiedTime"`
	Created  string `json:"createdTime"`
	Mime     string `json:"mimeType"`
}

var kimlikRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// kacisQ — Drive q ifadesindeki tek tırnaklı değer için kaçış.
func kacisQ(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

func (h *driveHedef) dosyalarURL(q url.Values) string { return h.u.api + "/files?" + q.Encode() }

// liste — q sorgusuna uyan öğeler (sayfalı).
func (h *driveHedef) liste(ctx context.Context, islem, q, alanlar string) ([]dosyaMeta, error) {
	var out []dosyaMeta
	sayfa := ""
	for sayfaNo := 0; sayfaNo < 1000; sayfaNo++ {
		v := url.Values{"q": {q}, "fields": {"nextPageToken,files(" + alanlar + ")"}, "pageSize": {"1000"}, "spaces": {"drive"}}
		if sayfa != "" {
			v.Set("pageToken", sayfa)
		}
		var yanit struct {
			Next  string      `json:"nextPageToken"`
			Files []dosyaMeta `json:"files"`
		}
		if err := h.cagri(ctx, islem, http.MethodGet, h.dosyalarURL(v), nil, &yanit); err != nil {
			return nil, err
		}
		for _, f := range yanit.Files {
			if !kimlikRe.MatchString(f.ID) {
				return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, islem, errors.New("drive: geçersiz dosya kimliği"))
			}
			out = append(out, f)
		}
		if yanit.Next == "" {
			return out, nil
		}
		sayfa = yanit.Next
	}
	return out, nil
}

const alanlarDosya = "id,name,size,md5Checksum,modifiedTime,createdTime"

// klasorBulOlustur — ust klasörün altında ad klasörünü bulur; yoksa olustur ise açar.
func (h *driveHedef) klasorBulOlustur(ctx context.Context, islem, ust, ad string, olustur bool) (string, bool, error) {
	q := fmt.Sprintf("name = '%s' and '%s' in parents and mimeType = '%s' and trashed = false", kacisQ(ad), ust, klasorMime)
	v := url.Values{"q": {q}, "fields": {"files(id,createdTime)"}, "orderBy": {"createdTime"}, "pageSize": {"10"}, "spaces": {"drive"}}
	var yanit struct {
		Files []dosyaMeta `json:"files"`
	}
	if err := h.cagri(ctx, islem, http.MethodGet, h.dosyalarURL(v), nil, &yanit); err != nil {
		return "", false, err
	}
	for _, f := range yanit.Files {
		if kimlikRe.MatchString(f.ID) {
			return f.ID, true, nil // en eski
		}
	}
	if !olustur {
		return "", false, nil
	}
	var yeniK dosyaMeta
	err := h.cagri(ctx, islem, http.MethodPost, h.u.api+"/files?fields=id", map[string]any{
		"name": ad, "mimeType": klasorMime, "parents": []string{ust},
	}, &yeniK)
	if err != nil {
		return "", false, err
	}
	if !kimlikRe.MatchString(yeniK.ID) {
		return "", false, hedef.YeniHata(hedef.KategoriBilinmeyen, islem, errors.New("drive: geçersiz klasör kimliği"))
	}
	return yeniK.ID, true, nil
}

// dizinCoz — "a/b" dizin yolunun klasör kimliği ("" = yedek klasörünün kendisi).
// olustur=false iken yoksa bulundu=false döner.
func (h *driveHedef) dizinCoz(ctx context.Context, islem, dizin string, olustur bool) (string, bool, error) {
	birak, err := h.kilit.al(ctx, "klasor")
	if err != nil {
		return "", false, hedef.Sarmala(islem, err)
	}
	defer birak()
	id, ok := h.dizinOnb[""]
	if !ok {
		id, ok, err = h.klasorBulOlustur(ctx, islem, "root", h.ayar.Klasor, olustur)
		if err != nil || !ok {
			return "", false, err
		}
		h.dizinOnb[""] = id
	}
	if dizin == "" {
		return id, true, nil
	}
	yol := ""
	for _, p := range strings.Split(dizin, "/") {
		if yol == "" {
			yol = p
		} else {
			yol += "/" + p
		}
		if c, var_ := h.dizinOnb[yol]; var_ {
			id = c
			continue
		}
		c, ok, err := h.klasorBulOlustur(ctx, islem, id, p, olustur)
		if err != nil || !ok {
			return "", false, err
		}
		h.dizinOnb[yol] = c
		id = c
	}
	return id, true, nil
}

func (h *driveHedef) onbellekTemizle(ctx context.Context) {
	if birak, err := h.kilit.al(ctx, "klasor"); err == nil {
		h.dizinOnb = map[string]string{}
		birak()
	}
}

// dosyalar — klasördeki dosyalar (alt klasörler hariç).
func (h *driveHedef) dosyalar(ctx context.Context, islem, dirID, tamAd string) ([]dosyaMeta, error) {
	q := fmt.Sprintf("'%s' in parents and trashed = false and mimeType != '%s'", dirID, klasorMime)
	if tamAd != "" {
		q += fmt.Sprintf(" and name = '%s'", kacisQ(tamAd))
	}
	return h.liste(ctx, islem, q, alanlarDosya)
}

func zaman(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// yeniOnce — a, b'den daha yeni mi (değişim, eşitse oluşturma, eşitse kimlik).
func yeniOnce(a, b dosyaMeta) bool {
	if ta, tb := zaman(a.Modified), zaman(b.Modified); !ta.Equal(tb) {
		return ta.After(tb)
	}
	if ta, tb := zaman(a.Created), zaman(b.Created); !ta.Equal(tb) {
		return ta.After(tb)
	}
	return a.ID > b.ID
}

// ─── Listele ────────────────────────────────────────────────────────────────

func (h *driveHedef) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	if err := ctx.Err(); err != nil {
		return nil, hedef.Sarmala("listele", err)
	}
	dizin, adOneki, err := hedef.OnekBol(onek)
	if err != nil {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "listele", err)
	}
	dirID, bulundu, err := h.dizinCoz(ctx, "listele", dizin, false)
	if err != nil || !bulundu {
		return nil, err
	}
	dosyalar, err := h.dosyalar(ctx, "listele", dirID, "")
	if err != nil {
		return nil, err
	}
	enYeni := map[string]dosyaMeta{}
	for _, f := range dosyalar {
		if !strings.HasPrefix(f.Name, adOneki) || hedef.AdDogrula(f.Name) != nil {
			continue
		}
		if o, var_ := enYeni[f.Name]; !var_ || yeniOnce(f, o) {
			enYeni[f.Name] = f
		}
	}
	out := make([]hedef.Nesne, 0, len(enYeni))
	for ad, f := range enYeni {
		boyut, _ := strconv.ParseInt(f.Size, 10, 64)
		tam := ad
		if dizin != "" {
			tam = dizin + "/" + ad
		}
		out = append(out, hedef.Nesne{Ad: tam, Boyut: boyut, Degisim: zaman(f.Modified)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ad < out[j].Ad })
	return out, nil
}

// ─── Sil ────────────────────────────────────────────────────────────────────

func (h *driveHedef) Sil(ctx context.Context, uzakAd string) error {
	if err := hedef.AdDogrula(uzakAd); err != nil {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "sil", err)
	}
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("sil", err)
	}
	dizin, dosya := hedef.AdBol(uzakAd)
	dirID, bulundu, err := h.dizinCoz(ctx, "sil", dizin, false)
	if err != nil || !bulundu {
		return err
	}
	bulunan, err := h.dosyalar(ctx, "sil", dirID, dosya)
	if err != nil {
		return err
	}
	for _, f := range bulunan {
		if err := h.dosyaSil(ctx, "sil", f.ID); err != nil {
			return err
		}
	}
	return nil
}

func (h *driveHedef) dosyaSil(ctx context.Context, islem, id string) error {
	err := h.cagri(ctx, islem, http.MethodDelete, h.u.api+"/files/"+id, nil, nil)
	if err != nil && hedef.KategoriAl(err) == hedef.KategoriBulunamadi {
		return nil // zaten yok
	}
	return err
}

// ─── Indir ──────────────────────────────────────────────────────────────────

// sayacOkuyucu — gövde boyutu beklenenle eşleşmezse EOF yerine hata verir
// (sunucu bağlantıyı erken kapatırsa kesik dosya "başarı" sayılmasın).
type sayacOkuyucu struct {
	r        io.Reader
	beklenen int64
	n        int64
}

func (s *sayacOkuyucu) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.n += int64(n)
	if err == io.EOF && s.beklenen >= 0 && s.n != s.beklenen {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func (h *driveHedef) Indir(ctx context.Context, uzakAd, yerelYol string) error {
	if err := hedef.AdDogrula(uzakAd); err != nil {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "indir", err)
	}
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("indir", err)
	}
	dizin, dosya := hedef.AdBol(uzakAd)
	yok := hedef.YeniHata(hedef.KategoriBulunamadi, "indir", errors.New("drive: dosya bulunamadı"))
	dirID, bulundu, err := h.dizinCoz(ctx, "indir", dizin, false)
	if err != nil {
		return err
	}
	if !bulundu {
		return yok
	}
	bulunan, err := h.dosyalar(ctx, "indir", dirID, dosya)
	if err != nil {
		return err
	}
	if len(bulunan) == 0 {
		return yok
	}
	f := bulunan[0]
	for _, c := range bulunan[1:] {
		if yeniOnce(c, f) {
			f = c
		}
	}
	beklenen, perr := strconv.ParseInt(f.Size, 10, 64)
	if perr != nil {
		beklenen = -1
	}
	adres := h.u.api + "/files/" + f.ID + "?alt=media"
	// Gövde akışı sırasında kopma: tüm indirme (geçici ada) bir kez daha denenir.
	var son error
	for deneme := 0; deneme < 3; deneme++ {
		resp, err := h.istek(ctx, "indir", func(string) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
		})
		if err != nil {
			return err
		}
		err = hedef.AtomikYaz(ctx, yerelYol, &sayacOkuyucu{r: resp.Body, beklenen: beklenen})
		resp.Body.Close()
		if err == nil {
			return nil
		}
		son = err
		if ctx.Err() != nil || hedef.KategoriAl(err) != hedef.KategoriAg {
			break
		}
		if uerr := h.uyku(ctx, geriCekilme(deneme)); uerr != nil {
			return hedef.Sarmala("indir", uerr)
		}
	}
	return hedef.Sarmala("indir", son, h.sirlar()...)
}

// ─── Yukle ──────────────────────────────────────────────────────────────────

var errOturumGecersiz = errors.New("drive: yükleme oturumu geçersiz")

// yerelOzet — dosyanın boyutu, sha256 ve md5 özeti (tek geçiş; iptal edilebilir).
func yerelOzet(ctx context.Context, f *os.File) (boyut int64, sha, md string, err error) {
	h256, hm := sha256.New(), md5.New() // #nosec G401
	boyut, err = io.Copy(io.MultiWriter(h256, hm), hedef.IlerlemeOkuyucu(ctx, f, nil))
	if err != nil {
		return 0, "", "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return 0, "", "", err
	}
	return boyut, hex.EncodeToString(h256.Sum(nil)), hex.EncodeToString(hm.Sum(nil)), nil
}

// kotaDene — yükleme öncesi depolama kotası: kalan yer dosyadan azsa yüklemeye
// hiç başlanmaz (limit yoksa sınırsız).
func (h *driveHedef) kotaDene(ctx context.Context, boyut int64) error {
	var yanit struct {
		Kota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	v := url.Values{"fields": {"storageQuota"}}
	if err := h.cagri(ctx, "yukle", http.MethodGet, h.u.api+"/about?"+v.Encode(), nil, &yanit); err != nil {
		return err
	}
	if yanit.Kota.Limit == "" {
		return nil
	}
	limit, e1 := strconv.ParseInt(yanit.Kota.Limit, 10, 64)
	kullanilan, e2 := strconv.ParseInt(yanit.Kota.Usage, 10, 64)
	if e1 != nil || e2 != nil {
		return nil
	}
	if limit-kullanilan < boyut {
		return hedef.YeniHata(hedef.KategoriKota, "yukle", errors.New("drive: depolama kotası yetersiz"))
	}
	return nil
}

func (h *driveHedef) Yukle(ctx context.Context, yerelYol, uzakAd string, sec hedef.YukleSecenek) error {
	if err := hedef.AdDogrula(uzakAd); err != nil {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", err)
	}
	if hedef.GeciciMi(uzakAd) {
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", hedef.ErrGecersizAd)
	}
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("yukle", err)
	}
	f, err := os.Open(yerelYol)
	if err != nil {
		return hedef.Sarmala("yukle", err)
	}
	defer f.Close()
	boyut, sha, md, err := yerelOzet(ctx, f)
	if err != nil {
		return hedef.Sarmala("yukle", err)
	}
	if sec.Sha256 != "" && !strings.EqualFold(sec.Sha256, sha) {
		// Yerel dosya beklenen özetle eşleşmiyor: hiçbir şey yüklenmeden reddedilir.
		return hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("drive: yerel dosya sha256 özeti beklenenle uyuşmuyor"))
	}
	birak, err := h.kilit.al(ctx, "ad:"+uzakAd)
	if err != nil {
		return hedef.Sarmala("yukle", err)
	}
	defer birak()
	if err := h.kotaDene(ctx, boyut); err != nil {
		return err
	}
	dizin, dosya := hedef.AdBol(uzakAd)
	for deneme := 0; deneme < 2; deneme++ {
		dirID, _, err := h.dizinCoz(ctx, "yukle", dizin, true)
		if err != nil {
			return err
		}
		mevcut, err := h.dosyalar(ctx, "yukle", dirID, dosya)
		if err != nil {
			return err
		}
		sirala := func(i, j int) bool { return yeniOnce(mevcut[i], mevcut[j]) }
		sort.Slice(mevcut, sirala)
		mevcutID := ""
		if len(mevcut) > 0 {
			mevcutID = mevcut[0].ID
		}
		oturum, err := h.oturumAc(ctx, dirID, dosya, mevcutID, boyut, sha)
		if err != nil {
			if hedef.KategoriAl(err) == hedef.KategoriBulunamadi && deneme == 0 {
				// Üst klasör kullanıcı tarafından silinmiş olabilir: önbellek atılıp yeniden denenir.
				h.onbellekTemizle(ctx)
				continue
			}
			return err
		}
		meta, err := h.parcalar(ctx, oturum, f, boyut, sec)
		if errors.Is(err, errOturumGecersiz) {
			if deneme == 0 {
				continue
			}
			return hedef.YeniHata(hedef.KategoriAg, "yukle", errOturumGecersiz)
		}
		if err != nil {
			return err
		}
		if meta.MD5 != "" && !strings.EqualFold(meta.MD5, md) {
			if mevcutID == "" && kimlikRe.MatchString(meta.ID) {
				kctx, kiptal := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				_ = h.dosyaSil(kctx, "yukle", meta.ID)
				kiptal()
			}
			return hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("drive: yükleme sonrası özet uyuşmuyor (bozuk aktarım)"))
		}
		if bs, perr := strconv.ParseInt(meta.Size, 10, 64); perr == nil && bs != boyut {
			return hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("drive: yükleme sonrası boyut uyuşmuyor"))
		}
		// Çift kopya (başka süreç/yarış) varsa fazlalıklar silinir (en iyi çaba).
		for _, c := range mevcut {
			if c.ID != mevcutID && c.ID != meta.ID && ctx.Err() == nil {
				_ = h.dosyaSil(ctx, "yukle", c.ID)
			}
		}
		return nil
	}
	return hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("drive: yükleme tamamlanamadı"))
}

const alanlarYukleme = "id,name,size,md5Checksum"

// oturumAc — resumable yükleme oturumu; dönen adres (taşıyıcı kimlik) hata metnine girmez.
func (h *driveHedef) oturumAc(ctx context.Context, dirID, dosya, mevcutID string, boyut int64, sha string) (string, error) {
	ctx, iptal := context.WithTimeout(ctx, 90*time.Second)
	defer iptal()
	meta := map[string]any{"appProperties": map[string]string{"sha256": sha}}
	metod, adres := http.MethodPost, h.u.yukleme+"/files?uploadType=resumable&fields="+url.QueryEscape(alanlarYukleme)
	if mevcutID != "" {
		metod, adres = http.MethodPatch, h.u.yukleme+"/files/"+mevcutID+"?uploadType=resumable&fields="+url.QueryEscape(alanlarYukleme)
	} else {
		meta["name"] = dosya
		meta["parents"] = []string{dirID}
	}
	b, _ := json.Marshal(meta)
	resp, err := h.istek(ctx, "yukle", func(string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, metod, adres, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
		req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(boyut, 10))
		return req, nil
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, govdeSiniri))
	loc := resp.Header.Get("Location")
	if u, perr := url.Parse(loc); perr != nil || loc == "" || u.Host == "" {
		return "", hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("drive: yükleme oturumu adresi alınamadı"), h.sirlar()...)
	}
	return loc, nil
}

// rangeOfset — "Range: bytes=0-N" başlığından sonraki bayt ofseti (yoksa 0).
func rangeOfset(hdr http.Header) int64 {
	r := strings.TrimSpace(hdr.Get("Range"))
	i := strings.LastIndexByte(r, '-')
	if !strings.HasPrefix(r, "bytes=") || i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(r[i+1:], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n + 1
}

// durumSorgu — oturumun kaç baytı aldığını sorar (kopan yüklemeyi sürdürmek için).
// Tamamlanmışsa meta döner.
func (h *driveHedef) durumSorgu(ctx context.Context, oturum string, boyut int64) (int64, *dosyaMeta, error) {
	jeton, err := h.jeton.Al(ctx)
	if err != nil {
		return 0, nil, hedef.Sarmala("yukle", err, h.sirlar()...)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, oturum, nil)
	if err != nil {
		return 0, nil, hedef.Sarmala("yukle", err)
	}
	req.Header.Set("Authorization", "Bearer "+jeton)
	req.Header.Set("Content-Range", "bytes */"+strconv.FormatInt(boyut, 10))
	req.ContentLength = 0
	resp, err := h.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	hdr := resp.Header
	govde := govdeOku(resp)
	switch {
	case resp.StatusCode == 200 || resp.StatusCode == 201:
		var m dosyaMeta
		if json.Unmarshal(govde, &m) != nil || !kimlikRe.MatchString(m.ID) {
			return 0, nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("drive: yükleme yanıtı çözülemedi"))
		}
		return boyut, &m, nil
	case resp.StatusCode == 308:
		return rangeOfset(hdr), nil, nil
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return 0, nil, errOturumGecersiz
	}
	return 0, nil, govdeCoz(resp.StatusCode, govde).hata("yukle", h.sirlar())
}

// parcaGonder — [ofset, ofset+n) aralığını tek PUT ile yollar.
func (h *driveHedef) parcaGonder(ctx context.Context, oturum string, f *os.File, ofset, n, boyut int64, sec hedef.YukleSecenek, bildir func(int64)) (int, http.Header, []byte, error) {
	jeton, err := h.jeton.Al(ctx)
	if err != nil {
		return 0, nil, nil, hedef.Sarmala("yukle", err, h.sirlar()...)
	}
	var govde io.Reader = http.NoBody
	if n > 0 {
		var r io.Reader = io.NewSectionReader(f, ofset, n)
		r = hedef.IlerlemeOkuyucu(ctx, r, func(a int64) { bildir(ofset + a) })
		r = hedef.SinirliOkuyucu(ctx, r, sec.BantKBps)
		govde = r
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, oturum, govde)
	if err != nil {
		return 0, nil, nil, hedef.Sarmala("yukle", err)
	}
	req.ContentLength = n
	req.Header.Set("Authorization", "Bearer "+jeton)
	req.Header.Set("Content-Type", "application/octet-stream")
	if boyut == 0 {
		req.Header.Set("Content-Range", "bytes */0")
	} else {
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ofset, ofset+n-1, boyut))
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	hdr := resp.Header
	return resp.StatusCode, hdr, govdeOku(resp), nil
}

// parcalar — 8 MiB parçalarla yükler; kopmada oturumdan ofseti sorup sürdürür.
func (h *driveHedef) parcalar(ctx context.Context, oturum string, f *os.File, boyut int64, sec hedef.YukleSecenek) (*dosyaMeta, error) {
	var (
		ofset, enYuksek int64
		bmu             sync.Mutex
	)
	bildir := func(v int64) {
		bmu.Lock()
		defer bmu.Unlock()
		if sec.Ilerleme != nil && v > enYuksek {
			enYuksek = v
			sec.Ilerleme(v)
		}
	}
	ardisik, ilerlemesiz, yenilendi := 0, 0, false
	// tekrar — hata sonrası: bekle, oturumdan ofseti sor (tamamlandıysa meta döner).
	tekrar := func(bekle time.Duration) (*dosyaMeta, error) {
		if err := h.uyku(ctx, bekle); err != nil {
			return nil, hedef.Sarmala("yukle", err)
		}
		o, m, err := h.durumSorgu(ctx, oturum, boyut)
		if err != nil {
			if errors.Is(err, errOturumGecersiz) {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, hedef.Sarmala("yukle", ctx.Err())
			}
			if reddiMi(err) {
				return nil, hedef.Sarmala("yukle", agHatasi(err), h.sirlar()...)
			}
			if _, ok := err.(*hedef.Hata); ok && hedef.KategoriAl(err) != hedef.KategoriAg {
				return nil, err
			}
			return nil, nil // durum sorgusu da koptu: döngü yeniden dener
		}
		if m != nil {
			return m, nil
		}
		if o > ofset {
			ilerlemesiz = 0
		}
		ofset = o
		return nil, nil
	}
	for {
		son := ofset + ParcaBoyutu
		if son > boyut {
			son = boyut
		}
		kod, hdr, govde, err := h.parcaGonder(ctx, oturum, f, ofset, son-ofset, boyut, sec, bildir)
		if err != nil {
			if ctx.Err() != nil {
				return nil, hedef.Sarmala("yukle", ctx.Err())
			}
			var h0 *hedef.Hata
			if errors.As(err, &h0) {
				return nil, err
			}
			if reddiMi(err) {
				return nil, hedef.Sarmala("yukle", agHatasi(err), h.sirlar()...)
			}
			ardisik++
			if ardisik > azamiDeneme {
				return nil, hedef.Sarmala("yukle", agHatasi(err), h.sirlar()...)
			}
			if m, err := tekrar(geriCekilme(ardisik - 1)); err != nil || m != nil {
				return m, err
			}
			continue
		}
		switch {
		case kod == 200 || kod == 201:
			var m dosyaMeta
			if json.Unmarshal(govde, &m) != nil || !kimlikRe.MatchString(m.ID) {
				return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("drive: yükleme yanıtı çözülemedi"))
			}
			bildir(boyut)
			return &m, nil
		case kod == 308:
			yeniOfset := rangeOfset(hdr)
			if yeniOfset <= ofset {
				ilerlemesiz++
				if ilerlemesiz > azamiDeneme {
					return nil, hedef.YeniHata(hedef.KategoriAg, "yukle", errors.New("drive: yükleme ilerlemiyor"))
				}
			} else {
				ilerlemesiz = 0
			}
			if yeniOfset > boyut {
				return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "yukle", errors.New("drive: sunucu ofseti geçersiz"))
			}
			ofset, ardisik = yeniOfset, 0
		case kod == 401 && !yenilendi:
			yenilendi = true
			h.jeton.Sifirla()
		case kod == 404 || kod == 410:
			return nil, errOturumGecersiz
		default:
			a := govdeCoz(kod, govde)
			if !a.yenidenDenenir() {
				return nil, a.hata("yukle", h.sirlar())
			}
			ardisik++
			if ardisik > azamiDeneme {
				return nil, a.hata("yukle", h.sirlar())
			}
			bekle, ok := retryAfter(hdr, h.simdi())
			if !ok {
				bekle = geriCekilme(ardisik - 1)
			}
			if m, err := tekrar(bekle); err != nil || m != nil {
				return m, err
			}
		}
	}
}

// ─── Test ───────────────────────────────────────────────────────────────────

// Test — uçtan uca kanıt: klasör aç → (kota) → yaz → oku → karşılaştır → sil.
func (h *driveHedef) Test(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("test", err)
	}
	h.onbellekTemizle(ctx)
	dir, err := os.MkdirTemp("", "gosp-drive-test-*")
	if err != nil {
		return hedef.Sarmala("test", err)
	}
	defer os.RemoveAll(dir)
	veri := make([]byte, 1024)
	_, _ = rand.Read(veri)
	yerel := filepath.Join(dir, "kaynak.bin")
	if err := os.WriteFile(yerel, veri, 0o600); err != nil {
		return hedef.Sarmala("test", err)
	}
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	ad := "gosp-baglanti-testi-" + hex.EncodeToString(rnd[:]) + ".tmp"
	defer func() {
		sctx, iptal := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer iptal()
		_ = h.Sil(sctx, ad)
	}()
	if err := h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{}); err != nil {
		return hedef.Sarmala("test", err)
	}
	inen := filepath.Join(dir, "inen.bin")
	if err := h.Indir(ctx, ad, inen); err != nil {
		return hedef.Sarmala("test", err)
	}
	got, err := os.ReadFile(inen)
	if err != nil || !bytes.Equal(got, veri) {
		return hedef.YeniHata(hedef.KategoriAg, "test", errors.New("drive: okunan içerik yazılanla uyuşmuyor"))
	}
	return hedef.Sarmala("test", h.Sil(ctx, ad))
}
