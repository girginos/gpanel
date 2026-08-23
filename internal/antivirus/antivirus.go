// Package antivirus: per-domain zararlı yazılım taraması (ClamAV + hafif heuristik).
// Güvenlik: sunucu genelinde TEK tarama (atomic mutex → RAM/OOM koruması, box 3.8G),
// karantina = aynı-dosya-sistemi rename (fuser/rm YOK), yol domain home'una kilitli.
package antivirus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const clamBin = "/usr/bin/clamscan"

// 🔴 avYakinda — Antivirus "yakinda": motor kararli olana dek tarama endpointleri
// DEVRE DISI (yanlis-pozitif firtinasi cozulene dek). Geri acmak: false + reship.
const avYakinda = true

type Handlers struct{ DB *sql.DB }

// scanning: 0=boş 1=tarama sürüyor. TÜM sunucu için tek (ClamAV DB ~1.5G RAM → eşzamanlı tarama OOM riski).
var scanning int32

var errCap = errors.New("dosya-siniri")

// Düşük yanlış-pozitif, yüksek sinyalli PHP webshell/obfuscation imzaları
var heuristics = []struct {
	ad string
	re *regexp.Regexp
}{
	{"PHP.Webshell.EvalBase64", regexp.MustCompile(`(?i)eval\s*\(\s*(base64_decode|gzinflate|gzuncompress|str_rot13|convert_uudecode)\s*\(`)},
	{"PHP.Webshell.PregReplaceE", regexp.MustCompile(`(?i)preg_replace\s*\(\s*['"][^'"]{0,200}/e['"]`)},
	{"PHP.Webshell.AssertInput", regexp.MustCompile(`(?i)assert\s*\(\s*\$_(GET|POST|REQUEST|COOKIE)`)},
	{"PHP.Webshell.SystemInput", regexp.MustCompile(`(?i)(shell_exec|passthru|system|popen|proc_open)\s*\(\s*\$_(GET|POST|REQUEST|COOKIE|SERVER)`)},
	{"PHP.Webshell.KnownMarker", regexp.MustCompile(`(?i)(c99shell|r57shell|b374k|wso[_ ]?shell|filesman|indoxploit|angelshell|priv8|mini\s*shell)`)},
	{"PHP.Obf.CreateFunc", regexp.MustCompile(`(?i)create_function\s*\([^)]*base64_decode`)},
	{"PHP.Obf.CharObfEval", regexp.MustCompile(`(?i)\$\{?['"]?\w+['"]?\}?\s*\(\s*\$\{?['"]?\w+['"]?\}?\s*\)\s*;.*base64`)},
}

type Bulgu struct {
	Dosya     string `json:"dosya"`
	Imza      string `json:"imza"`
	Motor     string `json:"motor"`
	Karantina int    `json:"karantina"`
}

func (h *Handlers) domain(r *http.Request) (id int64, sk string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(is_demo,0) FROM domains WHERE id=?`, id).Scan(&sk, &isDemo); err != nil {
		return id, "", false, false
	}
	return id, sk, isDemo == 1, true
}

func newestClamDB() string {
	var newest time.Time
	for _, f := range []string{"daily.cld", "daily.cvd", "main.cld", "main.cvd"} {
		if fi, err := os.Stat("/var/lib/clamav/" + f); err == nil {
			if fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
	}
	if newest.IsZero() {
		return ""
	}
	return newest.Format("2006-01-02 15:04")
}

func motorAdi() string {
	if _, err := os.Stat(clamBin); err == nil {
		return "clamav+heuristik"
	}
	return "heuristik"
}

// GET /domains/{id}/antivirus
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, clamErr := os.Stat(clamBin)
	resp := map[string]any{
		"clamav":      clamErr == nil,
		"imza_tarihi": newestClamDB(),
		"kullanici":   sk,
		"son_tarama":  nil,
		"bulgular":    []Bulgu{},
	}
	// Bulgular HER ZAMAN domain_id'den okunur (tarama-bağımsız): host-geneli
	// zamanlanmış tarama (domain_id=0) da bu domainin bulgularını üretir; eski
	// "tarama_id=domain-özel-tarama" yolu o bulguları hiç göstermiyordu.
	dbulgular := h.bulgularDomain(r.Context(), id)
	resp["bulgular"] = dbulgular

	// Son tarama: önce domain-özel, yoksa son HOST-geneli (domain_id=0) tarama.
	var sid int64
	var durum, motor, bas string
	var bitis sql.NullString
	var taranan, enfekte int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, durum, motor, taranan, enfekte, baslangic, bitis
		   FROM av_taramalar WHERE domain_id=? ORDER BY id DESC LIMIT 1`, id).
		Scan(&sid, &durum, &motor, &taranan, &enfekte, &bas, &bitis)
	if err != nil {
		err = h.DB.QueryRowContext(r.Context(),
			`SELECT id, durum, motor, taranan, enfekte, baslangic, bitis
			   FROM av_taramalar WHERE domain_id=0 AND durum='tamam' ORDER BY id DESC LIMIT 1`).
			Scan(&sid, &durum, &motor, &taranan, &enfekte, &bas, &bitis)
	}
	if err == nil {
		resp["son_tarama"] = map[string]any{
			"id": sid, "durum": durum, "motor": motor, "taranan": taranan,
			// enfekte: bu domainin AKTIF bulgu sayisi (host toplami degil — yaniltmaz).
			"enfekte": len(dbulgular), "baslangic": bas, "bitis": bitis.String,
		}
	} else if len(dbulgular) > 0 {
		// Hiç tarama kaydı yok ama bulgu var → yine de bulguları göster.
		resp["son_tarama"] = map[string]any{"durum": "tamam", "motor": "G-AV", "enfekte": len(dbulgular)}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// bulgularDomain — domainin AKTIF/karantina bulgulari (tarama-bagimsiz). Host
// zamanlanmis tarama domain_id ile bulgu yazar; domain sayfasi bunlari gostersin.
func (h *Handlers) bulgularDomain(ctx context.Context, did int64) []Bulgu {
	out := []Bulgu{}
	rows, err := h.DB.QueryContext(ctx,
		`SELECT dosya, imza, COALESCE(motor,''), karantina FROM av_bulgular
		  WHERE domain_id=? AND durum IN ('aktif','karantina') ORDER BY id DESC LIMIT 500`, did)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b Bulgu
		if err := rows.Scan(&b.Dosya, &b.Imza, &b.Motor, &b.Karantina); err == nil {
			out = append(out, b)
		}
	}
	_ = rows.Err()
	return out
}

func (h *Handlers) bulgular(ctx context.Context, sid int64) []Bulgu {
	out := []Bulgu{}
	rows, err := h.DB.QueryContext(ctx, `SELECT dosya, imza, motor, karantina FROM av_bulgular WHERE tarama_id=? ORDER BY id`, sid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b Bulgu
		if err := rows.Scan(&b.Dosya, &b.Imza, &b.Motor, &b.Karantina); err == nil {
			out = append(out, b)
		}
	}
	_ = rows.Err()
	return out
}

// POST /domains/{id}/antivirus/tara
func (h *Handlers) Tara(w http.ResponseWriter, r *http.Request) {
	if avYakinda {
		httpx.WriteError(w, http.StatusServiceUnavailable, "Antivirüs geçici olarak devre dışı — yakında.")
		return
	}
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	root := "/home/" + sk + "/public_html"
	if _, err := os.Stat(root); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "public_html bulunamadı")
		return
	}
	if !atomic.CompareAndSwapInt32(&scanning, 0, 1) {
		httpx.WriteError(w, http.StatusConflict, "sunucuda başka bir tarama sürüyor, lütfen bekleyin")
		return
	}
	res, err := h.DB.Exec(`INSERT INTO av_taramalar (domain_id, durum, motor) VALUES (?,?,?)`, id, "calisiyor", motorAdi())
	if err != nil {
		atomic.StoreInt32(&scanning, 0)
		httpx.WriteError(w, http.StatusInternalServerError, "tarama kaydı oluşturulamadı")
		return
	}
	sid, _ := res.LastInsertId()
	go func() {
		defer atomic.StoreInt32(&scanning, 0)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		taranan, findings := runScan(ctx, root)
		for _, f := range findings {
			_, _ = h.DB.Exec(`INSERT INTO av_bulgular (tarama_id, domain_id, dosya, imza, motor) VALUES (?,?,?,?,?)`,
				sid, id, f.Dosya, f.Imza, f.Motor)
		}
		_, _ = h.DB.Exec(`UPDATE av_taramalar SET durum='bitti', taranan=?, enfekte=?, bitis=NOW() WHERE id=?`,
			taranan, len(findings), sid)
	}()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"scan_id": sid})
}

// GET /domains/{id}/antivirus/tara/{sid}
func (h *Handlers) TaraDurum(w http.ResponseWriter, r *http.Request) {
	id, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	sid, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	var durum, motor, bas string
	var bitis sql.NullString
	var taranan, enfekte int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT durum, motor, taranan, enfekte, baslangic, bitis FROM av_taramalar WHERE id=? AND domain_id=?`, sid, id).
		Scan(&durum, &motor, &taranan, &enfekte, &bas, &bitis); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "tarama bulunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": sid, "durum": durum, "motor": motor, "taranan": taranan,
		"enfekte": enfekte, "baslangic": bas, "bitis": bitis.String,
		"bulgular": h.bulgular(r.Context(), sid),
	})
}

// POST /domains/{id}/antivirus/karantina  {dosya}
func (h *Handlers) Karantina(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	var req struct {
		Dosya string `json:"dosya"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	home := "/home/" + sk
	root := home + "/public_html"
	clean := filepath.Clean(req.Dosya)
	// Yol domain'in public_html'i içinde OLMALI (path-traversal + cross-user koruması)
	if clean != root && !strings.HasPrefix(clean, root+"/") {
		httpx.WriteError(w, http.StatusBadRequest, "yol domain dizini dışında")
		return
	}
	fi, err := os.Lstat(clean)
	if err != nil || fi.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "dosya bulunamadı")
		return
	}
	qdir := home + "/.karantina"
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "karantina dizini oluşturulamadı")
		return
	}
	hedef := filepath.Join(qdir, time.Now().Format("20060102_150405")+"_"+filepath.Base(clean))
	if err := os.Rename(clean, hedef); err != nil { // aynı dosya-sistemi → atomik; fuser/rm YOK
		httpx.WriteError(w, http.StatusInternalServerError, "taşınamadı: "+err.Error())
		return
	}
	_ = os.Chmod(hedef, 0o000) // çalıştırılamaz/okunamaz
	// 🔴 durum/orijinal_yol/karantina_yol'u DA yaz: yeni karantina yönetimi
	// (liste/geri-yükle/sil/incele) bu alanlara bakar. Yalnız karantina=1 yazmak
	// dosyayı taşır ama panelde "Karantina" bölümünde GÖRÜNMEZ → geri yüklenemez.
	res, _ := h.DB.Exec(
		`UPDATE av_bulgular SET karantina=1, durum='karantina', orijinal_yol=?, karantina_yol=?
		   WHERE domain_id=? AND dosya=?`, clean, hedef, id, clean)
	// Bulgu kaydı yoksa (ör. dış tarama) elle bir karantina kaydı oluştur ki
	// operatör geri yükleyebilsin — sessizce kaybolmasın.
	if res != nil {
		if n, _ := res.RowsAffected(); n == 0 {
			// 🔴 tarama_id NOT NULL, default YOK ve FK YOK → 0 = "taramasız manuel
			// karantina" sentinel. Atlanırsa INSERT sessizce ERROR 1364 ile düşer ve
			// dosya .karantina'da DB kaydı olmadan orphan kalır (geri yüklenemez).
			_, _ = h.DB.Exec(
				`INSERT INTO av_bulgular (tarama_id, domain_id, dosya, imza, motor, seviye, durum, karantina, orijinal_yol, karantina_yol)
				 VALUES (0,?,?,?,?,?,?,?,?,?)`,
				id, clean, "manuel-karantina", "operator", "kritik", "karantina", 1, clean, hedef)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "hedef": hedef})
}

// POST /domains/{id}/antivirus/imza-guncelle  → freshclam
func (h *Handlers) ImzaGuncelle(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat("/usr/bin/freshclam"); err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "freshclam kurulu değil")
		return
	}
	if !atomic.CompareAndSwapInt32(&scanning, 0, 1) {
		httpx.WriteError(w, http.StatusConflict, "başka bir işlem sürüyor, bekleyin")
		return
	}
	defer atomic.StoreInt32(&scanning, 0)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/freshclam").CombinedOutput()
	cikti := string(out)
	if len(cikti) > 4000 {
		cikti = cikti[len(cikti)-4000:]
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": err == nil, "imza_tarihi": newestClamDB(), "cikti": cikti,
	})
}

const avajanYolu = "/usr/local/bin/girginospanel-avajan"

// runScan: panelin "Tara" düğmesi.
//
// 🔴 ARTIK YENİ MOTORU + KAYNAK DİLİMİNİ kullanır. Eskiden bu fonksiyon
// clamscan'i DOĞRUDAN exec ediyordu ve panelin kendi cgroup'unda (slice DIŞI)
// çalışıyordu — adversaryel denetimde ölçüldü: interaktif tarama hiçbir kaynak
// limitine tabi değildi ve clamscan PHP webshell'lerinde zayıf. Şimdi:
//
//	systemd-run --slice=girginos-av.slice girginospanel-avajan --tara <root> --json
//
// ile çalışır → hem KENDİ motorumuz (avmotor) hem SLICE limiti (CPU/RAM/IO).
// Bu, iki paralel AV yolunu (eski clamscan + yeni avajan) TEKE indirir.
func runScan(ctx context.Context, root string) (int, []Bulgu) {
	if _, err := os.Stat(avajanYolu); err == nil {
		if taranan, bulgular, ok := avajanIleTara(ctx, root); ok {
			return taranan, bulgular
		}
		// avajan var ama çalıştırılamadı → sessizce eski yola düşmüyoruz;
		// eski yol clamscan'e bağlı ve o da olmayabilir. Yine de fallback
		// olarak devam (aşağıdaki eski kod), ama bu nadir bir yol.
	}
	return runScanEski(ctx, root)
}

// avajanIleTara — girginospanel-avajan'ı slice içinde çalıştırıp JSON'unu parse eder.
func avajanIleTara(ctx context.Context, root string) (int, []Bulgu, bool) {
	// systemd-run: --scope + --slice ile ajanı av dilimine sokar; --wait sonucu
	// bekler. Slice yoksa systemd-run yine çalışır (default slice), ajan yine
	// kendi motorunu kullanır — yani en kötü durumda motor doğru, limit yok.
	// 🔴 --json, pozisyonel <root>'tan ÖNCE gelmeli: Go flag paketi ilk pozisyonel
	// argümanda ayrıştırmayı DURDURUR. "--tara root --json" verilirse --json hiç
	// okunmaz → ajan METİN döker → panelin json.Unmarshal'ı çöker → panel sessizce
	// clamscan'e düşer (bizim motor DEVREDIŞI). "--tara --json root" doğru sıradır.
	args := []string{"--scope", "--quiet", "--slice=girginos-av.slice",
		avajanYolu, "--tara", "--json", root}
	cmd := exec.CommandContext(ctx, "systemd-run", args...)
	out, err := cmd.Output()
	if err != nil {
		// systemd-run yoksa (konteyner/eski) doğrudan çalıştır — motor doğru,
		// limit yok. Motor > limitsizlik; sessiz clamscan'e düşmekten iyidir.
		cmd = exec.CommandContext(ctx, avajanYolu, "--tara", "--json", root)
		out, err = cmd.Output()
		if err != nil {
			return 0, nil, false
		}
	}
	var y struct {
		Taranan  int `json:"taranan"`
		Bulgular []struct {
			Dosya    string `json:"dosya"`
			Seviye   string `json:"seviye"`
			Aciklama string `json:"aciklama"`
		} `json:"bulgular"`
	}
	if e := json.Unmarshal(out, &y); e != nil {
		return 0, nil, false
	}
	var b []Bulgu
	for _, f := range y.Bulgular {
		b = append(b, Bulgu{Dosya: f.Dosya, Imza: f.Aciklama, Motor: "gosp/" + f.Seviye})
	}
	return y.Taranan, b, true
}

// runScanEski: clamscan + heuristik (avajan yoksa fallback).
func runScanEski(ctx context.Context, root string) (int, []Bulgu) {
	var findings []Bulgu
	seen := map[string]bool{}

	// 1) ClamAV
	if _, err := os.Stat(clamBin); err == nil {
		cmd := exec.CommandContext(ctx, clamBin, "-r", "-i", "--no-summary", "--stdout",
			"--max-filesize=25M", "--max-scansize=500M", root)
		out, _ := cmd.CombinedOutput()
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasSuffix(line, " FOUND") {
				if i := strings.LastIndex(line, ": "); i > 0 {
					dosya := line[:i]
					imza := strings.TrimSuffix(line[i+2:], " FOUND")
					if !seen["c|"+dosya] {
						seen["c|"+dosya] = true
						findings = append(findings, Bulgu{Dosya: dosya, Imza: imza, Motor: "clamav"})
					}
				}
			}
		}
	}

	// 2) Heuristik PHP webshell taraması
	taranan := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".karantina":
				return filepath.SkipDir
			}
			return nil
		}
		if !phpish(strings.ToLower(filepath.Ext(p))) {
			return nil
		}
		fi, e := d.Info()
		if e != nil || fi.Size() > 3*1024*1024 {
			return nil
		}
		taranan++
		if taranan > 50000 {
			return errCap
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		for _, hs := range heuristics {
			if hs.re.Match(b) {
				k := "h|" + p + "|" + hs.ad
				if !seen[k] {
					seen[k] = true
					findings = append(findings, Bulgu{Dosya: p, Imza: hs.ad, Motor: "heuristik"})
				}
			}
		}
		return nil
	})
	return taranan, findings
}

func phpish(ext string) bool {
	switch ext {
	case ".php", ".phtml", ".php3", ".php4", ".php5", ".php7", ".php8", ".phar", ".inc", ".pht":
		return true
	}
	return false
}
