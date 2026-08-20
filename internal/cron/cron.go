// Package cron: domain user'in crontab'i icin CRUD
package cron

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const (
	maxGorev   = 100
	maxKomut   = 1024
	bannerLine = "# girginospanel cron — bu dosya panel tarafindan yonetiliyor; elle duzenlemeyin"
)

type Gorev struct {
	Idx    int    `json:"idx"`
	Dakika string `json:"dakika"`
	Saat   string `json:"saat"`
	Gun    string `json:"gun"`
	Ay     string `json:"ay"`
	Hafta  string `json:"hafta"`
	Komut  string `json:"komut"`
	Yorum  string `json:"yorum,omitempty"`
	// Plesk-tarzı zengin alanlar. Metadata crontab yorumunda (# gosp-meta:) saklanır.
	Etkin    bool   `json:"etkin"`               // false → cron satırı '#' ile pasif
	Tip      string `json:"tip,omitempty"`       // "komut" | "url" | "php" (boş=komut)
	PhpSurum string `json:"php_surum,omitempty"` // tip=php: hangi PHP sürümü
	Bildirim string `json:"bildirim,omitempty"`  // "bilgi" | "hata" | "her" | "yok"
}

type Handlers struct {
	DB *sql.DB
}

var (
	errDemo = errors.New("demo aboneliğin cron'u yönetilemez")
	errBad  = errors.New("güvenlik: c_ prefix'siz user reddedildi")
)

func (h *Handlers) lookup(r *http.Request) (string, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if isDemo == 1 {
		return "", errDemo
	}
	if !strings.HasPrefix(sk, "c_") {
		return "", errBad
	}
	return sk, nil
}

// crontab path
func cronPath(sk string) string {
	return "/var/spool/cron/" + sk
}

func read(sk string) ([]Gorev, error) {
	p := cronPath(sk)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []Gorev{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := make([]Gorev, 0)
	sc := bufio.NewScanner(f)
	var lastYorum string
	var meta map[string]string
	idx := 0
	// cronSatiriParse: bir cron satırını (5 zaman alanı + komut) Gorev'e çevirir.
	// pasif=satır '#' ile başlıyordu (etkin=false).
	ekle := func(line string, pasif bool) {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return
		}
		g := Gorev{
			Idx:    idx,
			Dakika: fields[0], Saat: fields[1], Gun: fields[2],
			Ay: fields[3], Hafta: fields[4],
			Komut: strings.Join(fields[5:], " "),
			Yorum: lastYorum,
			Etkin: !pasif,
			Tip:   "komut",
		}
		if meta != nil {
			if v := meta["tip"]; v != "" {
				g.Tip = v
			}
			g.PhpSurum = meta["php_surum"]
			g.Bildirim = meta["bildirim"]
			if meta["etkin"] == "0" {
				g.Etkin = false
			}
		}
		out = append(out, g)
		idx++
		lastYorum = ""
		meta = nil
	}
	for sc.Scan() {
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" {
			lastYorum = ""
			meta = nil
			continue
		}
		if strings.HasPrefix(line, "#") {
			c := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			// Panel metadata satırı: "# gosp-meta: tip=php php_surum=8.3 bildirim=hata etkin=1"
			if strings.HasPrefix(c, "gosp-meta:") {
				meta = parseMeta(strings.TrimSpace(strings.TrimPrefix(c, "gosp-meta:")))
				continue
			}
			// PASİF görev: "#0 3 * * * komut" (cron satırı comment'lenmiş). Ayırt et:
			// '#' sonrası ilk alan cron zaman alanı gibiyse (rakam/*/,/-// içerir) görevdir.
			if looksCron(c) {
				ekle(c, true)
				continue
			}
			// kendi banner satirimizi atla, gerisi açıklama
			if !strings.HasPrefix(c, "girginospanel") {
				lastYorum = c
			}
			continue
		}
		ekle(line, false)
	}
	return out, sc.Err()
}

// parseMeta: "k=v k=v" → map. Değerlerde boşluk yok (tip/sürüm/bildirim).
func parseMeta(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Fields(s) {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// looksCron: satır bir cron zaman-tanımı gibi mi (5 alan + komut, ilk alan cron-token).
func looksCron(s string) bool {
	f := strings.Fields(s)
	if len(f) < 6 {
		return false
	}
	// ilk alan yalnız cron karakterleri içermeli (rakam * , - /)
	for _, c := range f[0] {
		if !(c >= '0' && c <= '9') && c != '*' && c != ',' && c != '-' && c != '/' {
			return false
		}
	}
	return true
}

func write(sk string, list []Gorev) error {
	var buf bytes.Buffer
	buf.WriteString(bannerLine + "\n")
	buf.WriteString("# son güncelleme: " + sk + "\n\n")
	for _, g := range list {
		// Metadata satırı (tip/php/bildirim/etkin) — panel read'de geri okur.
		var mp []string
		if g.Tip != "" && g.Tip != "komut" {
			mp = append(mp, "tip="+g.Tip)
		}
		if g.PhpSurum != "" {
			mp = append(mp, "php_surum="+g.PhpSurum)
		}
		if g.Bildirim != "" {
			mp = append(mp, "bildirim="+g.Bildirim)
		}
		if !g.Etkin {
			mp = append(mp, "etkin=0")
		}
		if len(mp) > 0 {
			fmt.Fprintf(&buf, "# gosp-meta: %s\n", strings.Join(mp, " "))
		}
		if g.Yorum != "" {
			fmt.Fprintf(&buf, "# %s\n", strings.ReplaceAll(g.Yorum, "\n", " "))
		}
		// Pasif görev cron satırı '#' ile comment'lenir (crond çalıştırmaz) ama
		// panel geri okuyabilir (looksCron).
		pre := ""
		if !g.Etkin {
			pre = "#"
		}
		fmt.Fprintf(&buf, "%s%s %s %s %s %s %s\n",
			pre, g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta, g.Komut)
	}
	p := cronPath(sk)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	_ = os.Chmod(p, 0600)
	// chown user:user
	if out, err := exec.Command("chown", sk+":"+sk, p).CombinedOutput(); err != nil {
		return fmt.Errorf("chown: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// SELinux context — system_cron_spool_t
	_, _ = exec.Command("restorecon", p).CombinedOutput()
	return nil
}

func validate(g Gorev) error {
	if g.Dakika == "" || g.Saat == "" || g.Gun == "" || g.Ay == "" || g.Hafta == "" {
		return fmt.Errorf("tüm zaman alanları zorunlu")
	}
	if g.Komut == "" {
		return fmt.Errorf("komut boş olamaz")
	}
	if len(g.Komut) > maxKomut {
		return fmt.Errorf("komut çok uzun (max %d)", maxKomut)
	}
	for _, f := range []string{g.Dakika, g.Saat, g.Gun, g.Ay, g.Hafta} {
		if strings.ContainsAny(f, ";|&`\n") {
			return fmt.Errorf("zaman alanlarında geçersiz karakter")
		}
	}
	if strings.ContainsAny(g.Komut, "\n\r") {
		return fmt.Errorf("komutta satır sonu olamaz")
	}
	return nil
}

func statusFromErr(err error) int {
	switch err {
	case os.ErrNotExist:
		return http.StatusNotFound
	case errDemo:
		return http.StatusForbidden
	case errBad:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sistem_kullanici": sk,
		"toplam":           len(list),
		"gorevler":         list,
	})
}

// gorevInput — Create/Update gövdesi: Gorev + görev tipine özel ham alanlar
// (backend bunlardan Komut üretir; ham alanlar crontab'a yazılmaz).
type gorevInput struct {
	Gorev
	URL    string `json:"url,omitempty"`    // tip=url
	Script string `json:"script,omitempty"` // tip=php: PHP dosya yolu
	Args   string `json:"args,omitempty"`   // tip=php: argümanlar
}

// phpBin — sürüm ("8.3") için PHP ikilisinin yolu. Remi düzeni önce denenir.
func phpBin(surum string) string {
	kod := strings.ReplaceAll(surum, ".", "")
	if kod != "" {
		remi := "/opt/remi/php" + kod + "/root/usr/bin/php"
		if _, err := os.Stat(remi); err == nil {
			return remi
		}
	}
	return "/usr/bin/php"
}

// tehlikeliMeta — komut üretiminde ham alanlara izin verilmeyen shell metakarakterleri.
const tehlikeliMeta = "'\n\r`;|&<>$\""

// komutUret — görev tipine göre çalıştırılacak komutu üretir. url/php ham
// girdileri shell-metakarakterlerine karşı doğrulanır (injection önlenir).
func komutUret(in gorevInput) (string, error) {
	switch in.Tip {
	case "url":
		u := strings.TrimSpace(in.URL)
		if u == "" {
			return "", fmt.Errorf("URL boş olamaz")
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return "", fmt.Errorf("URL http:// veya https:// ile başlamalı")
		}
		if strings.ContainsAny(u, tehlikeliMeta) {
			return "", fmt.Errorf("URL geçersiz karakter içeriyor")
		}
		return fmt.Sprintf("curl -fsS -o /dev/null --max-time 300 '%s'", u), nil
	case "php":
		s := strings.TrimSpace(in.Script)
		if s == "" {
			return "", fmt.Errorf("PHP dosya yolu boş olamaz")
		}
		if strings.ContainsAny(s, tehlikeliMeta) {
			return "", fmt.Errorf("PHP dosya yolu geçersiz karakter içeriyor")
		}
		cmd := fmt.Sprintf("%s -q '%s'", phpBin(in.PhpSurum), s)
		if a := strings.TrimSpace(in.Args); a != "" {
			if strings.ContainsAny(a, tehlikeliMeta) {
				return "", fmt.Errorf("argümanlar geçersiz karakter içeriyor")
			}
			cmd += " " + a
		}
		return cmd, nil
	default: // "komut" veya boş
		return in.Komut, nil
	}
}

// hazirla — gorevInput'tan yazılacak Gorev'i türetir (Komut üretilir, defaultlar).
func hazirla(in gorevInput) (Gorev, error) {
	g := in.Gorev
	if g.Tip == "" {
		g.Tip = "komut"
	}
	if g.Bildirim == "" {
		g.Bildirim = "bilgi"
	}
	komut, err := komutUret(in)
	if err != nil {
		return Gorev{}, err
	}
	g.Komut = komut
	if err := validate(g); err != nil {
		return Gorev{}, err
	}
	return g, nil
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var in gorevInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	g, err := hazirla(in)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(list) >= maxGorev {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("en fazla %d görev olabilir", maxGorev))
		return
	}
	list = append(list, g)
	if err := write(sk, list); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "idx": len(list) - 1})
}

// Update: PUT /domains/{id}/cron/{idx} — mevcut görevi düzenler (aynı index).
func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "idx"))
	var in gorevInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	g, err := hazirla(in)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if idx < 0 || idx >= len(list) {
		httpx.WriteError(w, http.StatusNotFound, "index aralık dışında")
		return
	}
	list[idx] = g
	if err := write(sk, list); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "idx": idx})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "idx"))
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if idx < 0 || idx >= len(list) {
		httpx.WriteError(w, http.StatusNotFound, "index aralık dışında")
		return
	}
	silinen := list[idx]
	list = append(list[:idx], list[idx+1:]...)
	if err := write(sk, list); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": silinen})
}

// Calistir: bir cron görevini ELLE tetikler (test/doğrulama). Görevin komutu
// tenant kullanıcısı (sk) olarak, panel sırları OLMADAN temiz env ile, 120sn
// zaman aşımıyla çalıştırılır; birleşik çıktının son ~8KB'ı döner.
//
// GÜVENLİK: komut zaten tenant'ın KENDİ crontab'ından okunur (kendi yazdığı) ve
// zaten kendi kimliğinde koşacaktı — bu yalnız zamanı öne alır. runuser tenant'a
// düşürür; ek ayrıcalık yoktur. lookup demo/sk kontrolünü yapar.
func (h *Handlers) Calistir(w http.ResponseWriter, r *http.Request) {
	sk, err := h.lookup(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	idx, _ := strconv.Atoi(chi.URLParam(r, "idx"))
	list, err := read(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if idx < 0 || idx >= len(list) {
		httpx.WriteError(w, http.StatusNotFound, "index aralık dışında")
		return
	}
	komut := list[idx].Komut

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "runuser", "-u", sk, "--", "/bin/sh", "-c", komut)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/home/" + sk,
	}
	out, runErr := cmd.CombinedOutput()
	cikti := string(out)
	if len(cikti) > 8192 {
		cikti = "…(kısaltıldı)\n" + cikti[len(cikti)-8192:]
	}
	resp := map[string]any{"ok": runErr == nil, "cikti": cikti, "komut": komut}
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			resp["hata"] = "zaman aşımı (120sn) — görev arka planda sürebilir"
		} else {
			resp["hata"] = runErr.Error()
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
