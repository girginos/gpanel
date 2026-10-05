package gdrive

// sahte_test.go — httptest ile SAHTE Google (Drive v3 + OAuth) sunucusu.
// Gerçek Google'a ASLA bağlanılmaz. Resumable yükleme, sayfalı liste, q sorgusu,
// jeton yenileme/cihaz akışı ve hata/kopma enjeksiyonu (hook) içerir.

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sahteDosya struct {
	ID, Ad, Ust, Mime  string
	Veri               []byte
	Olusturma, Degisim time.Time
	Ozellik            map[string]string
}

type sahteOturum struct {
	ad, ust, mevcutID string
	toplam            int64
	tampon            []byte
	ozellik           map[string]string
	bitti             *sahteDosya
}

type sahteIstek struct{ Metod, Yol string }

// hookYanit — hook'un verdiği hazır yanıt.
type hookYanit struct {
	Kod    int
	Baslik map[string]string
	Govde  string
}

type sahteDrive struct {
	mu  sync.Mutex
	srv *httptest.Server

	dosyalar   map[string]*sahteDosya
	oturumlar  map[string]*sahteOturum
	sayac      int
	saat       time.Time
	gecerli    map[string]bool // geçerli erişim jetonları
	jetonSayac int

	yenilemeJetonu    string
	clientID          string
	clientSecret      string
	gecersizYenile    bool // yenileme invalid_grant
	yenilemeSayisi    int
	cihazDurum        string // pending|slow|denied|expired|ok
	cihazKapsam       string // başarılı yanıtta dönen scope
	dogrulamaURL      string
	istenenKapsam     string
	iptalEdilen       []string
	iptalYanit        int
	kotaLimit         string
	kotaKullanim      string
	sayfaBoyutu       int
	md5Boz            bool
	kopOfset          int64    // >0: bir sonraki parça gövdesi bu kadar bayt okununca bağlantı kopar (bir kez)
	indirKop          int      // >0: indirme gövdesi yarıda kopar (kalan kez)
	indirKesik        int      // >0: indirme gövdesi Content-Length'siz, yarıda ama TEMİZ biter (kalan kez)
	araliklar         []string // oturum PUT'larının Content-Range başlıkları
	oturumAcmaSayisi  int
	hook              func(f *sahteDrive, r *http.Request, n int) *hookYanit
	istekler          []sahteIstek
	sayimlar          map[string]int // "METOD yol" → adet
	gordugumBasliklar []string
}

func yeniSahteDrive() *sahteDrive {
	f := &sahteDrive{
		dosyalar: map[string]*sahteDosya{}, oturumlar: map[string]*sahteOturum{},
		saat: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), gecerli: map[string]bool{},
		yenilemeJetonu: "1//SAHTE-YENILEME-JETONU-0001", clientID: "test-123456.apps.googleusercontent.com",
		clientSecret: "SAHTE-CLIENT-SECRET-0001", sayimlar: map[string]int{},
		cihazDurum: "pending", cihazKapsam: Kapsam, dogrulamaURL: "https://www.google.com/device",
		iptalYanit: 200,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.sun))
	return f
}

func (f *sahteDrive) kapat() { f.srv.Close() }

func (f *sahteDrive) uclar() uclar {
	return uclar{api: f.srv.URL + "/drive/v3", yukleme: f.srv.URL + "/upload/drive/v3",
		token: f.srv.URL + "/token", cihaz: f.srv.URL + "/device/code", iptal: f.srv.URL + "/revoke"}
}

// ayarla — kilit altında durum değiştirir.
func (f *sahteDrive) ayarla(fn func(f *sahteDrive)) { f.mu.Lock(); defer f.mu.Unlock(); fn(f) }

func (f *sahteDrive) say(metod, onek string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, v := range f.sayimlar {
		if strings.HasPrefix(k, metod+" "+onek) {
			n += v
		}
	}
	return n
}

func (f *sahteDrive) dosyaVar(ad string) []*sahteDosya {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*sahteDosya
	for _, d := range f.dosyalar {
		if d.Ad == ad && d.Mime != klasorMime {
			out = append(out, d)
		}
	}
	return out
}

func (f *sahteDrive) dosyaSayisi() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.dosyalar {
		if d.Mime != klasorMime {
			n++
		}
	}
	return n
}

func (f *sahteDrive) yeniID() string {
	f.sayac++
	return fmt.Sprintf("id%04d", f.sayac)
}

func (f *sahteDrive) zaman() time.Time {
	f.saat = f.saat.Add(time.Second)
	return f.saat
}

func yaz(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

func driveHata(w http.ResponseWriter, kod int, reason string) {
	yaz(w, kod, map[string]any{"error": map[string]any{"code": kod, "message": "sahte hata",
		"errors": []map[string]string{{"reason": reason, "domain": "global"}}}})
}

func oauthHata(w http.ResponseWriter, kod int, e string) {
	yaz(w, kod, map[string]string{"error": e, "error_description": "sahte"})
}

func (f *sahteDrive) sun(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.istekler = append(f.istekler, sahteIstek{r.Method, r.URL.Path})
	f.sayimlar[r.Method+" "+r.URL.Path]++
	f.gordugumBasliklar = append(f.gordugumBasliklar, r.Header.Get("Authorization"))
	n := len(f.istekler)
	hook := f.hook
	var hy *hookYanit
	if hook != nil {
		hy = hook(f, r, n)
	}
	f.mu.Unlock()
	if hy != nil {
		for k, v := range hy.Baslik {
			w.Header().Set(k, v)
		}
		if hy.Govde != "" && w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(hy.Kod)
		_, _ = io.WriteString(w, hy.Govde)
		return
	}
	switch {
	case r.URL.Path == "/token":
		f.token(w, r)
	case r.URL.Path == "/device/code":
		f.cihazKodu(w, r)
	case r.URL.Path == "/revoke":
		f.iptal(w, r)
	default:
		f.drive(w, r)
	}
}

// ─── OAuth ──────────────────────────────────────────────────────────────────

func (f *sahteDrive) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.PostForm.Get("client_id") != f.clientID || r.PostForm.Get("client_secret") != f.clientSecret {
		oauthHata(w, 401, "invalid_client")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "refresh_token":
		if f.gecersizYenile || r.PostForm.Get("refresh_token") != f.yenilemeJetonu {
			oauthHata(w, 400, "invalid_grant")
			return
		}
		f.yenilemeSayisi++
		f.jetonSayac++
		t := fmt.Sprintf("ya29.SAHTE-ERISIM-%d", f.jetonSayac)
		f.gecerli[t] = true
		yaz(w, 200, map[string]any{"access_token": t, "expires_in": 3599, "token_type": "Bearer", "scope": Kapsam})
	case "urn:ietf:params:oauth:grant-type:device_code":
		switch f.cihazDurum {
		case "pending":
			oauthHata(w, 428, "authorization_pending")
		case "slow":
			oauthHata(w, 403, "slow_down")
		case "denied":
			oauthHata(w, 403, "access_denied")
		case "expired":
			oauthHata(w, 400, "expired_token")
		case "ok":
			f.jetonSayac++
			t := fmt.Sprintf("ya29.SAHTE-ERISIM-%d", f.jetonSayac)
			f.gecerli[t] = true
			yaz(w, 200, map[string]any{"access_token": t, "expires_in": 3599, "token_type": "Bearer",
				"refresh_token": "1//SAHTE-YENI-YENILEME-0002", "scope": f.cihazKapsam})
		}
	default:
		oauthHata(w, 400, "unsupported_grant_type")
	}
}

func (f *sahteDrive) cihazKodu(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.PostForm.Get("client_id") != f.clientID {
		oauthHata(w, 401, "invalid_client")
		return
	}
	f.istenenKapsam = r.PostForm.Get("scope")
	yaz(w, 200, map[string]any{"device_code": "SAHTE-CIHAZ-KODU-GIZLI", "user_code": "ABCD-EFGH",
		"verification_url": f.dogrulamaURL, "expires_in": 1800, "interval": 5})
}

func (f *sahteDrive) iptal(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iptalEdilen = append(f.iptalEdilen, r.PostForm.Get("token"))
	if f.iptalYanit != 200 {
		oauthHata(w, f.iptalYanit, "invalid_token")
		return
	}
	w.WriteHeader(200)
}

// ─── Drive ──────────────────────────────────────────────────────────────────

func (f *sahteDrive) yetkili(r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := r.Header.Get("Authorization")
	return strings.HasPrefix(a, "Bearer ") && f.gecerli[strings.TrimPrefix(a, "Bearer ")]
}

func (f *sahteDrive) drive(w http.ResponseWriter, r *http.Request) {
	if !f.yetkili(r) {
		driveHata(w, 401, "authError")
		return
	}
	p := r.URL.Path
	switch {
	case p == "/drive/v3/about" && r.Method == "GET":
		f.mu.Lock()
		l, u := f.kotaLimit, f.kotaKullanim
		f.mu.Unlock()
		kota := map[string]string{"usage": "0"}
		if l != "" {
			kota["limit"], kota["usage"] = l, u
		}
		yaz(w, 200, map[string]any{"storageQuota": kota})
	case p == "/drive/v3/files" && r.Method == "GET":
		f.liste(w, r)
	case p == "/drive/v3/files" && r.Method == "POST":
		f.klasorOlustur(w, r)
	case strings.HasPrefix(p, "/drive/v3/files/"):
		f.dosyaIslem(w, r, strings.TrimPrefix(p, "/drive/v3/files/"))
	case p == "/upload/drive/v3/files" && r.Method == "POST":
		f.oturumBaslat(w, r, "")
	case strings.HasPrefix(p, "/upload/drive/v3/files/") && r.Method == "PATCH":
		f.oturumBaslat(w, r, strings.TrimPrefix(p, "/upload/drive/v3/files/"))
	case strings.HasPrefix(p, "/upload/session/") && r.Method == "PUT":
		f.oturumPut(w, r, strings.TrimPrefix(p, "/upload/session/"))
	default:
		driveHata(w, 404, "notFound")
	}
}

var (
	qAdRe   = regexp.MustCompile(`name = '((?:[^'\\]|\\.)*)'`)
	qUstRe  = regexp.MustCompile(`'([^']+)' in parents`)
	qMimeRe = regexp.MustCompile(`mimeType (=|!=) '([^']+)'`)
)

func qCoz(s string) string {
	s = strings.ReplaceAll(s, `\'`, `'`)
	return strings.ReplaceAll(s, `\\`, `\`)
}

func (f *sahteDrive) meta(d *sahteDosya) map[string]any {
	sm := md5.Sum(d.Veri)
	m := map[string]any{"id": d.ID, "name": d.Ad, "mimeType": d.Mime,
		"modifiedTime": d.Degisim.Format(time.RFC3339), "createdTime": d.Olusturma.Format(time.RFC3339)}
	if d.Mime != klasorMime {
		m["size"] = strconv.Itoa(len(d.Veri))
		m["md5Checksum"] = hex.EncodeToString(sm[:])
	}
	return m
}

func (f *sahteDrive) liste(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	f.mu.Lock()
	defer f.mu.Unlock()
	var sonuc []*sahteDosya
	for _, d := range f.dosyalar {
		if m := qUstRe.FindStringSubmatch(q); m != nil && d.Ust != m[1] {
			continue
		}
		if m := qAdRe.FindStringSubmatch(q); m != nil && d.Ad != qCoz(m[1]) {
			continue
		}
		if m := qMimeRe.FindStringSubmatch(q); m != nil {
			esit := d.Mime == m[2]
			if (m[1] == "=") != esit {
				continue
			}
		}
		sonuc = append(sonuc, d)
	}
	sort.Slice(sonuc, func(i, j int) bool {
		if !sonuc[i].Olusturma.Equal(sonuc[j].Olusturma) {
			return sonuc[i].Olusturma.Before(sonuc[j].Olusturma)
		}
		return sonuc[i].ID < sonuc[j].ID
	})
	boyut, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if boyut <= 0 || (f.sayfaBoyutu > 0 && f.sayfaBoyutu < boyut) {
		boyut = f.sayfaBoyutu
	}
	bas, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
	if bas > len(sonuc) {
		bas = len(sonuc)
	}
	son := len(sonuc)
	yanit := map[string]any{}
	if boyut > 0 && bas+boyut < son {
		son = bas + boyut
		yanit["nextPageToken"] = strconv.Itoa(son)
	}
	out := []map[string]any{}
	for _, d := range sonuc[bas:son] {
		out = append(out, f.meta(d))
	}
	yanit["files"] = out
	yaz(w, 200, yanit)
}

func (f *sahteDrive) klasorOlustur(w http.ResponseWriter, r *http.Request) {
	var g struct {
		Name     string   `json:"name"`
		MimeType string   `json:"mimeType"`
		Parents  []string `json:"parents"`
	}
	if json.NewDecoder(r.Body).Decode(&g) != nil || g.Name == "" || len(g.Parents) != 1 {
		driveHata(w, 400, "badRequest")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &sahteDosya{ID: f.yeniID(), Ad: g.Name, Ust: g.Parents[0], Mime: g.MimeType, Olusturma: f.zaman()}
	d.Degisim = d.Olusturma
	f.dosyalar[d.ID] = d
	yaz(w, 200, map[string]any{"id": d.ID})
}

func (f *sahteDrive) dosyaIslem(w http.ResponseWriter, r *http.Request, id string) {
	f.mu.Lock()
	d := f.dosyalar[id]
	var veri []byte
	if d != nil {
		veri = append([]byte(nil), d.Veri...)
	}
	f.mu.Unlock()
	if d == nil {
		driveHata(w, 404, "notFound")
		return
	}
	switch {
	case r.Method == "DELETE":
		f.mu.Lock()
		delete(f.dosyalar, id)
		f.mu.Unlock()
		w.WriteHeader(204)
	case r.Method == "GET" && r.URL.Query().Get("alt") == "media":
		f.mu.Lock()
		kop := f.indirKop > 0
		if kop {
			f.indirKop--
		}
		kesik := f.indirKesik > 0
		if kesik {
			f.indirKesik--
		}
		f.mu.Unlock()
		if kesik && len(veri) > 10 {
			_, _ = w.Write(veri[:len(veri)/2]) // Content-Length yok: chunked, temiz bitiş
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(veri)))
		if kop && len(veri) > 10 {
			_, _ = w.Write(veri[:len(veri)/2])
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				_ = c.Close()
			}
			return
		}
		_, _ = w.Write(veri)
	default:
		driveHata(w, 405, "badRequest")
	}
}

func (f *sahteDrive) oturumBaslat(w http.ResponseWriter, r *http.Request, mevcutID string) {
	var g struct {
		Name    string            `json:"name"`
		Parents []string          `json:"parents"`
		Props   map[string]string `json:"appProperties"`
	}
	_ = json.NewDecoder(r.Body).Decode(&g)
	toplam, _ := strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.oturumAcmaSayisi++
	if mevcutID != "" && f.dosyalar[mevcutID] == nil {
		driveHata(w, 404, "notFound")
		return
	}
	if mevcutID == "" {
		if len(g.Parents) != 1 || f.dosyalar[g.Parents[0]] == nil {
			driveHata(w, 404, "notFound") // üst klasör yok
			return
		}
	}
	sid := fmt.Sprintf("s%04d", f.yeniSayac())
	f.oturumlar[sid] = &sahteOturum{ad: g.Name, mevcutID: mevcutID, toplam: toplam, ozellik: g.Props}
	if len(g.Parents) == 1 {
		f.oturumlar[sid].ust = g.Parents[0]
	}
	w.Header().Set("Location", f.srv.URL+"/upload/session/"+sid+"?upload_id=GIZLI-OTURUM-KIMLIGI")
	w.WriteHeader(200)
}

func (f *sahteDrive) yeniSayac() int { f.sayac++; return f.sayac }

var contentRangeRe = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+)$`)

func (f *sahteDrive) bitir(s *sahteOturum) *sahteDosya {
	var d *sahteDosya
	if s.mevcutID != "" {
		d = f.dosyalar[s.mevcutID]
		d.Veri = s.tampon
		d.Degisim = f.zaman()
	} else {
		d = &sahteDosya{ID: f.yeniID(), Ad: s.ad, Ust: s.ust, Mime: "application/octet-stream", Veri: s.tampon,
			Olusturma: f.zaman()}
		d.Degisim = d.Olusturma
		f.dosyalar[d.ID] = d
	}
	d.Ozellik = s.ozellik
	s.bitti = d
	return d
}

func (f *sahteDrive) bitisYaniti(w http.ResponseWriter, d *sahteDosya) {
	m := f.meta(d)
	if f.md5Boz {
		m["md5Checksum"] = strings.Repeat("0", 32)
	}
	yaz(w, 200, m)
}

func (f *sahteDrive) oturumPut(w http.ResponseWriter, r *http.Request, sid string) {
	f.mu.Lock()
	s := f.oturumlar[sid]
	f.mu.Unlock()
	if s == nil {
		driveHata(w, 404, "notFound")
		return
	}
	cr := r.Header.Get("Content-Range")
	f.mu.Lock()
	f.araliklar = append(f.araliklar, cr)
	f.mu.Unlock()
	if strings.HasPrefix(cr, "bytes */") {
		f.mu.Lock()
		defer f.mu.Unlock()
		toplam, _ := strconv.ParseInt(strings.TrimPrefix(cr, "bytes */"), 10, 64)
		if s.bitti == nil && toplam == 0 && s.toplam == 0 {
			f.bitir(s)
		}
		if s.bitti != nil {
			f.bitisYaniti(w, s.bitti)
			return
		}
		if len(s.tampon) > 0 {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.tampon)-1))
		}
		w.WriteHeader(308)
		return
	}
	m := contentRangeRe.FindStringSubmatch(cr)
	if m == nil {
		driveHata(w, 400, "badRequest")
		return
	}
	a, _ := strconv.ParseInt(m[1], 10, 64)
	b, _ := strconv.ParseInt(m[2], 10, 64)
	f.mu.Lock()
	hizasiz := a != int64(len(s.tampon)) && s.bitti == nil
	kopOfset := f.kopOfset
	if kopOfset > 0 {
		f.kopOfset = 0
	}
	f.mu.Unlock()
	if hizasiz {
		f.mu.Lock()
		if len(s.tampon) > 0 {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.tampon)-1))
		}
		f.mu.Unlock()
		w.WriteHeader(308)
		return
	}
	n := b - a + 1
	if kopOfset > 0 && kopOfset < n {
		kismi := make([]byte, kopOfset)
		okunan, _ := io.ReadFull(r.Body, kismi)
		f.mu.Lock()
		// Google yalnız 256 KiB'nin tam katlarını saklar.
		sakla := (int64(okunan) / (256 << 10)) * (256 << 10)
		s.tampon = append(s.tampon, kismi[:sakla]...)
		f.mu.Unlock()
		if hj, ok := w.(http.Hijacker); ok {
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}
		return
	}
	govde, err := io.ReadAll(r.Body)
	if err != nil || int64(len(govde)) != n {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s.tampon = append(s.tampon, govde...)
	if int64(len(s.tampon)) == s.toplam {
		f.bitisYaniti(w, f.bitir(s))
		return
	}
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.tampon)-1))
	w.WriteHeader(308)
}

var _ = url.QueryEscape
