// Package logs: per-domain nginx log dosyalari + hesabin PHP-FPM gunlugu + SSE canli tail.
//
// Log hedefi: bir hosting hesabinin ana alan adi, ek (addon) alan adlari ve alt
// alanlari ayni hesap kimligi altinda secilebilir. Hedef sunucuda COZULUR: istemci
// yalniz {tur, id} gonderir (?hedef=ana|ek:<id>|alt:<id>), alan adi DB'den hesap
// sahipligi dogrulanarak gelir, dosya adi karakter kumesi dogrulanir ve okuma
// yalniz LogDizin icinde, sembolik baga izin vermeden yapilir. Dosya yolu
// istemciye DONMEZ.
package logs

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// LogDizin: nginx vhost'larinin access/error log dizini (testte degistirilir).
var LogDizin = "/var/log/nginx"

// PHPLogKok: kiraci PHP-FPM gunluk dizinlerinin koku — /var/log/php-fpm-<sk>/tenant.log
// (provisioner.tenantPerSkLogFile ile AYNI yol). Testte degistirilir.
var PHPLogKok = "/var/log"

// skRe: panel sistem kullanicisi (c_ oneki; kucuk harf/rakam/alt cizgi).
var skRe = regexp.MustCompile(`^c_[a-z0-9_]{1,60}$`)

const (
	azamiOkumaB   = 8 << 20 // tek istekte dosya sonundan en cok bu kadar bayt okunur
	azamiSatirSay = 2000
	azamiAdUzun   = 253
)

type Handlers struct {
	DB *sql.DB
}

type LogDosya struct {
	Anahtar string `json:"anahtar"` // "access" | "error" | "php"
	Etiket  string `json:"etiket"`
	BoyutB  int64  `json:"boyut_b"`
	Degisme string `json:"degisme"`
	Mevcut  bool   `json:"mevcut"`
}

// LogHedefi: log-hedefleri satiri. Dosya yolu YOK.
type LogHedefi struct {
	Hedef         string `json:"hedef"` // "ana" | "ek:<id>" | "alt:<id>"
	Tur           string `json:"tur"`   // "ana" | "ek" | "alt"
	AlanAdi       string `json:"alan_adi"`
	EkAlanID      int64  `json:"ek_alan_id,omitempty"`
	AltAlanID     int64  `json:"alt_alan_id,omitempty"`
	AccessVar     bool   `json:"access_var"`
	ErrorVar      bool   `json:"error_var"`
	AccessBoyut   int64  `json:"access_boyut"`
	SonDegisiklik string `json:"son_degisiklik"`
}

var errYol = errors.New("geçersiz log hedefi")

// alanAdiRe: DNS etiketleri (harf/rakam/tire) noktayla ayrilir; "..", bas/son nokta ve tire yok.
var alanAdiRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// DosyaYolu: alan adi + anahtar -> LogDizin altindaki log dosyasi. Alan adi DB'den gelse
// de dogrulanir (savunma derinligi): karakter kumesi, uzunluk, filepath.Base ve dizin icinde kalma.
func DosyaYolu(alanAdi, anahtar string) (string, error) {
	var son string
	switch anahtar {
	case "access":
		son = ".access.log"
	case "error":
		son = ".error.log"
	default:
		return "", errYol
	}
	if alanAdi == "" || len(alanAdi) > azamiAdUzun || !alanAdiRe.MatchString(alanAdi) {
		return "", errYol
	}
	ad := alanAdi + son
	if filepath.Base(ad) != ad {
		return "", errYol
	}
	kok := filepath.Clean(LogDizin)
	p := filepath.Join(kok, ad)
	if filepath.Dir(p) != kok {
		return "", errYol
	}
	return p, nil
}

// PHPDosyaYolu: hesabin PHP-FPM gunlugu (/var/log/php-fpm-<sk>/tenant.log).
//
// NEDEN: PHP uygulama hatalari (Fatal/Warning/Notice — catch_workers_output ile
// "child N said into stderr") ve FPM uyarilari (max_children doldu, execution timed
// out, takili sendmail/postdrop) BURAYA duser; nginx error.log'da GORUNMEZ. 2026-10-12
// .180'de WHMCS "site cokuyor" (mail()'de asili isci) yalniz bu dosyadan anlasildi.
// Gunluk HESAP duzeyidir (ana/ek/alt alan adlarinin hepsi ayni kiraci havuzunda).
// Dizin systemd LogsDirectory (root sahipli): kiraci yazamaz/bag koyamaz; okuma yine
// guvenliAc ile (Lstat + SameFile) yapilir.
func PHPDosyaYolu(sk string) (string, error) {
	if !skRe.MatchString(sk) {
		return "", errYol
	}
	kok := filepath.Clean(PHPLogKok)
	dizin := filepath.Join(kok, "php-fpm-"+sk)
	if filepath.Dir(dizin) != kok {
		return "", errYol
	}
	return filepath.Join(dizin, "tenant.log"), nil
}

// dosyaYolu: istekteki dosya anahtari -> okunacak dosya ("php" hedeften bagimsiz, hesap duzeyi).
func dosyaYolu(alanAdi, sk, anahtar string) (string, error) {
	if anahtar == "php" {
		return PHPDosyaYolu(sk)
	}
	return DosyaYolu(alanAdi, anahtar)
}

// dosyaBilgi: sembolik bagi TAKIP ETMEDEN (Lstat) salt-okuma bilgi. Sembolik bag ya da
// duz dosya olmayan = yok sayilir.
func dosyaBilgi(p string) (os.FileInfo, bool) {
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	return st, true
}

// guvenliAc: Lstat duz dosya + acilan tanimlayici ayni dosya (TOCTOU'da sembolik bag takasi reddedilir).
func guvenliAc(p string) (*os.File, error) {
	ls, ok := dosyaBilgi(p)
	if !ok {
		if _, err := os.Lstat(p); err != nil {
			return nil, err // yok: os.IsNotExist
		}
		return nil, errors.New("log dosyası okunamaz")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	fs, err := f.Stat()
	if err != nil || !os.SameFile(ls, fs) {
		_ = f.Close()
		return nil, errors.New("log dosyası okunamaz")
	}
	return f, nil
}

type hataKodlu struct {
	kod int
	msg string
}

func (e *hataKodlu) Error() string { return e.msg }

// cozum: hedefin alan adi + hesabin sistem kullanicisi (PHP gunlugu icin) ya da hata.
func (h *Handlers) cozum(r *http.Request) (alanAdi, sistemKul string, err error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var ana, sk string
	var isDemo int
	e := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo FROM domains WHERE id=?`, id).Scan(&ana, &sk, &isDemo)
	if errors.Is(e, sql.ErrNoRows) {
		return "", "", &hataKodlu{http.StatusNotFound, "abonelik bulunamadı"}
	}
	if e != nil {
		return "", "", &hataKodlu{http.StatusInternalServerError, "log hedefi çözülemedi"}
	}
	if isDemo == 1 {
		return "", "", &hataKodlu{http.StatusForbidden, "demo aboneliğin logları yönetilemez"}
	}
	hedef := strings.TrimSpace(r.URL.Query().Get("hedef"))
	if sidStr := chi.URLParam(r, "sid"); sidStr != "" {
		if hedef != "" {
			return "", "", &hataKodlu{http.StatusBadRequest, "geçersiz log hedefi"}
		}
		hedef = "alt:" + sidStr
	}
	if hedef == "" || hedef == "ana" {
		return ana, sk, nil
	}
	tur, num, ok := strings.Cut(hedef, ":")
	n, perr := strconv.ParseInt(num, 10, 64)
	if !ok || perr != nil || n <= 0 || (tur != "ek" && tur != "alt") {
		return "", "", &hataKodlu{http.StatusBadRequest, "geçersiz log hedefi"}
	}
	q := `SELECT alan_adi FROM ek_alan_adlari WHERE id=? AND domain_id=?`
	if tur == "alt" {
		q = `SELECT tam_ad FROM subdomanlar WHERE id=? AND domain_id=?`
	}
	var ad string
	e = h.DB.QueryRowContext(r.Context(), q, n, id).Scan(&ad)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && ad == "") {
		// başka hesabın kimliği = olmayanla aynı yanıt (IDOR)
		return "", "", &hataKodlu{http.StatusNotFound, "log hedefi bulunamadı"}
	}
	if e != nil {
		return "", "", &hataKodlu{http.StatusInternalServerError, "log hedefi çözülemedi"}
	}
	return ad, sk, nil
}

func hataYaz(w http.ResponseWriter, err error) {
	var hk *hataKodlu
	if errors.As(err, &hk) {
		httpx.WriteError(w, hk.kod, hk.msg)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "log okunamadı")
}

// Hedefler: GET /domains/{id}/log-hedefleri
func (h *Handlers) Hedefler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var ana string
	var isDemo int
	e := h.DB.QueryRowContext(r.Context(), `SELECT alan_adi, is_demo FROM domains WHERE id=?`, id).Scan(&ana, &isDemo)
	if errors.Is(e, sql.ErrNoRows) {
		hataYaz(w, &hataKodlu{http.StatusNotFound, "abonelik bulunamadı"})
		return
	}
	if e != nil {
		hataYaz(w, &hataKodlu{http.StatusInternalServerError, "log hedefleri okunamadı"})
		return
	}
	if isDemo == 1 {
		hataYaz(w, &hataKodlu{http.StatusForbidden, "demo aboneliğin logları yönetilemez"})
		return
	}
	out := []LogHedefi{h.hedefSatiri(LogHedefi{Hedef: "ana", Tur: "ana", AlanAdi: ana})}
	for _, k := range []struct{ tur, tablo, sut string }{
		{"ek", "ek_alan_adlari", "alan_adi"},
		{"alt", "subdomanlar", "tam_ad"},
	} {
		rows, err := h.DB.QueryContext(r.Context(), "SELECT id, "+k.sut+" FROM "+k.tablo+" WHERE domain_id=? ORDER BY id", id) //nolint:gosec // G202: tablo/sutun SABIT dilimden, kullanici girdisi yok
		if err != nil {
			hataYaz(w, &hataKodlu{http.StatusInternalServerError, "log hedefleri okunamadı"})
			return
		}
		for rows.Next() {
			var n int64
			var ad string
			if rows.Scan(&n, &ad) != nil {
				continue
			}
			t := LogHedefi{Hedef: k.tur + ":" + strconv.FormatInt(n, 10), Tur: k.tur, AlanAdi: ad}
			if k.tur == "ek" {
				t.EkAlanID = n
			} else {
				t.AltAlanID = n
			}
			out = append(out, h.hedefSatiri(t))
		}
		_ = rows.Close()
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) hedefSatiri(t LogHedefi) LogHedefi {
	if p, err := DosyaYolu(t.AlanAdi, "access"); err == nil {
		if st, ok := dosyaBilgi(p); ok {
			t.AccessVar = true
			t.AccessBoyut = st.Size()
			t.SonDegisiklik = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	if p, err := DosyaYolu(t.AlanAdi, "error"); err == nil {
		if st, ok := dosyaBilgi(p); ok {
			t.ErrorVar = true
			if t.SonDegisiklik == "" || st.ModTime().UTC().Format("2006-01-02T15:04:05Z") > t.SonDegisiklik {
				t.SonDegisiklik = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
		}
	}
	return t
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	alanAdi, sk, err := h.cozum(r)
	if err != nil {
		hataYaz(w, err)
		return
	}
	out := []LogDosya{}
	for _, k := range []struct{ Anahtar, Etiket string }{
		{"access", "Erişim (access)"},
		{"error", "Hata (error)"},
		{"php", "PHP (yazılım hataları)"},
	} {
		entry := LogDosya{Anahtar: k.Anahtar, Etiket: k.Etiket}
		if p, perr := dosyaYolu(alanAdi, sk, k.Anahtar); perr == nil {
			if st, ok := dosyaBilgi(p); ok {
				entry.Mevcut = true
				entry.BoyutB = st.Size()
				entry.Degisme = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
		}
		out = append(out, entry)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// Son N satiri okur (default 200, max 2000)
func (h *Handlers) Read(w http.ResponseWriter, r *http.Request) {
	alanAdi, sk, err := h.cozum(r)
	if err != nil {
		hataYaz(w, err)
		return
	}
	anahtar := r.URL.Query().Get("dosya")
	if anahtar == "" {
		anahtar = "access"
	}
	p, perr := dosyaYolu(alanAdi, sk, anahtar)
	if perr != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz dosya anahtarı")
		return
	}
	son, _ := strconv.Atoi(r.URL.Query().Get("son"))
	if son <= 0 {
		son = 200
	}
	if son > azamiSatirSay {
		son = azamiSatirSay
	}

	satirlar, err := sonNSatir(p, son)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"dosya": anahtar, "alan_adi": alanAdi, "satirlar": []string{}, "mevcut": false,
			})
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "log okunamadı") // yol/hata ayrıntısı sızdırılmaz
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"dosya": anahtar, "alan_adi": alanAdi, "satirlar": satirlar, "mevcut": true,
	})
}

// SSE canli tail: log dosyasinin sonuna seek, yeni satirlari "data: ..." ile pushlar
func (h *Handlers) Tail(w http.ResponseWriter, r *http.Request) {
	alanAdi, sk, err := h.cozum(r)
	if err != nil {
		hataYaz(w, err)
		return
	}
	anahtar := r.URL.Query().Get("dosya")
	if anahtar == "" {
		anahtar = "access"
	}
	p, perr := dosyaYolu(alanAdi, sk, anahtar)
	if perr != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz dosya anahtarı")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, "stream desteklenmiyor")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, ": tail %s baslatildi\n\n", anahtar) //nolint:gosec // G705 FP: Content-Type text/event-stream (HTML DEGIL); anahtar yalniz {access,error,php} allowlist (aksi halde ustte 400) - tarayici govdeyi HTML yorumlamaz.
	flusher.Flush()

	f, err := guvenliAc(p)
	if err != nil {
		_, _ = fmt.Fprint(w, "event: hata\ndata: log dosyası açılamadı\n\n") // yol/hata ayrıntısı sızdırılmaz
		flusher.Flush()
		return
	}
	defer f.Close()
	// Önce son ~200 satırı gönder
	if mevcut, err := sonNSatir(p, 200); err == nil {
		for _, ln := range mevcut {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(ln, "\n", " ")) //nolint:gosec // G705 FP: SSE (text/event-stream); satir EventSource ile JS metnine, oradan React textContent icine gider (dangerouslySetInnerHTML yok). \n temizlendigi icin SSE alan-enjeksiyonu da kapali.
		}
		flusher.Flush()
	}
	// Sona atla
	_, _ = f.Seek(0, io.SeekEnd)

	reader := bufio.NewReader(f)
	ctx := r.Context()
	tick := time.NewTicker(15 * time.Second) // keepalive
	defer tick.Stop()

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return
		}
		if line != "" {
			ln := strings.TrimRight(line, "\n\r")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(ln, "\n", " ")) //nolint:gosec // G705 FP: SSE (text/event-stream); satir EventSource ile JS metnine, oradan React textContent icine gider (dangerouslySetInnerHTML yok). \n temizlendigi icin SSE alan-enjeksiyonu da kapali.
			flusher.Flush()
		}
		if err == io.EOF {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_, _ = fmt.Fprintln(w, ": keepalive")
				flusher.Flush()
			case <-time.After(500 * time.Millisecond):
				// dosya rotate vs. olabilir; size sıfırlandıysa yeniden aç
				if st, err := os.Stat(p); err == nil {
					if cur, _ := f.Seek(0, io.SeekCurrent); cur > st.Size() {
						f.Close()
						f, err = os.Open(p)
						if err != nil {
							return
						}
						reader = bufio.NewReader(f)
					}
				}
			}
		}
	}
}

// sonNSatir: dosyanin sonundan N satir oku. Okuma penceresi azamiOkumaB ile sinirlidir
// (tek satirlik devasa log dosyasi bellegi sisiremez); pencere dosya basindan basliyorsa
// ilk satir tamdir, aksi halde kesik ilk satir atilir. Sembolik bag reddedilir.
func sonNSatir(yol string, n int) ([]string, error) {
	f, err := guvenliAc(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size == 0 {
		return []string{}, nil
	}
	const blok = 8192
	var buf []byte
	var read int64
	for read < size && read < azamiOkumaB && countLines(buf) < n+1 {
		read += blok
		if read > size {
			read = size
		}
		tmp := make([]byte, read)
		m, rerr := f.ReadAt(tmp, size-read)
		if rerr != nil && rerr != io.EOF {
			return nil, rerr
		}
		buf = tmp[:m]
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if read < size && len(lines) > 1 {
		lines = lines[1:] // pencere satir ortasinda basladi
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
