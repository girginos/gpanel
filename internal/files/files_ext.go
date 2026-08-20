package files

// files_ext.go — Yaz/Rename/Chmod + symlink-aware jail
// (jailJoin orijinali files.go'da; bu dosya ek handler'lar + sıkılastirma)

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/archivex"
	"girginospanel/internal/httpx"
)

// jailJoinStrict: symlink-aware. Parent dizini EvalSymlinks ile resolve eder,
// sonra leaf'i join eder. Symlink ile dis-cikis engellenir.
func jailJoinStrict(home, rel string) (string, error) {
	rel = filepath.Clean("/" + rel)
	wanted := filepath.Clean(filepath.Join(home, rel))

	// homeResolved
	homeResolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		homeResolved = home
	}

	// wanted'in resolve edilebilir kismini bul
	test := wanted
	for {
		if r, err := filepath.EvalSymlinks(test); err == nil {
			// test bulundu, kalan path'i ekle ve kontrol et
			rest := strings.TrimPrefix(wanted, test)
			full := filepath.Clean(filepath.Join(r, rest))
			if full == homeResolved || strings.HasPrefix(full, homeResolved+string(filepath.Separator)) {
				return full, nil
			}
			return "", errEscape
		}
		// yoksa parent'a cik
		parent := filepath.Dir(test)
		if parent == test {
			// kök, devam edemez
			break
		}
		test = parent
	}
	// hicbir ata resolve olmadi (cok nadir); plain check
	if wanted == homeResolved || strings.HasPrefix(wanted, homeResolved+string(filepath.Separator)) {
		return wanted, nil
	}
	return "", errEscape
}

// ----- Yaz (editor save) -----

type yazReq struct {
	Yol    string `json:"yol"`
	Icerik string `json:"icerik"`
}

func (h *Handlers) Yaz(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req yazReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.Icerik) > 5*1024*1024 {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "5 MB üstü editor ile kaydedilemez")
		return
	}
	// TOCTOU symlink-güvenli yazma: hedefi openat2 ile aç (ara-bileşen/leaf symlink REDDEDİLİR),
	// fd'ye yaz, fd üzerinden tenant'a chown (bkz. safeio.go). Mevcut dosyanın izinleri korunur
	// (open create-dışında mode'a dokunmaz); yeni dosya 0644. Eski os.WriteFile(abs) resolved-
	// string üzerinde çalışıp ara-dizin symlink takasıyla jail-dışına yazmaya kandırılabilirdi.
	if err := writeBeneath(home, req.Yol, []byte(req.Icerik), 0644, sk); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"yol":   req.Yol,
		"boyut": len(req.Icerik),
	})
}

// ----- Rename / Move -----

type renameReq struct {
	Eski string `json:"eski"`
	Yeni string `json:"yeni"`
}

func (h *Handlers) Rename(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req renameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if p := relClean(req.Eski); p == "" || p == "." {
		httpx.WriteError(w, http.StatusBadRequest, "ana ev dizini taşınamaz")
		return
	}
	if p := relClean(req.Yeni); p == "" || p == "." {
		httpx.WriteError(w, http.StatusBadRequest, "ana ev dizini taşınamaz")
		return
	}
	// TOCTOU symlink-güvenli taşıma: kaynak+hedef PARENT'larını openat2 ile pinle, Renameat
	// ile taşı (rename final-bileşen symlink'ini takip etmez). Hedef ara-dizinler O_NOFOLLOW
	// mkdir-p ile oluşturulur (bkz. safeio.go). Eski os.Rename(eski, yeni) resolved-string'ler
	// üzerinde çalışıp ara-dizin symlink takasıyla jail-dışına taşımaya kandırılabilirdi.
	if err := renameBeneath(home, req.Eski, req.Yeni, sk); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "rename: "+err.Error())
		return
	}
	_ = chownTreeBeneath(home, req.Yeni, sk) // taşınan öğeyi tenant'a chown (symlink-güvenli)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eski": req.Eski, "yeni": req.Yeni})
}

// ----- Chmod -----

type chmodReq struct {
	Yol string `json:"yol"`
	Mod string `json:"mod"` // "0644" gibi octal string
}

func (h *Handlers) Chmod(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req chmodReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	mod := strings.TrimPrefix(req.Mod, "0")
	n, err := strconv.ParseUint(mod, 8, 32)
	if err != nil || n > 0o777 {
		httpx.WriteError(w, http.StatusBadRequest, "mod oktal olmalı (0000-0777)")
		return
	}
	// TOCTOU symlink-güvenli chmod: hedefi openat2 ile aç (ara-bileşen/leaf symlink REDDEDİLİR),
	// Fchmod (bkz. safeio.go). Eski os.Chmod(abs) resolved-string üzerinde çalışıp ara-dizin
	// symlink takasıyla jail-dışı (ör. /etc) dosyaya chmod'a kandırılabilirdi (LPE).
	if err := chmodBeneath(home, req.Yol, uint32(n)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "chmod: "+err.Error())
		return
	}
	_ = chownTreeBeneath(home, req.Yol, sk) // sahiplik domain user'ında kalsın (symlink-güvenli)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "yol": req.Yol, "mod": req.Mod})
}

var _ = errors.New // keep import

// ----- Extract (ZIP / TAR / TAR.GZ aç) -----

type extractReq struct {
	Yol   string `json:"yol"`   // arşivin yolu
	Hedef string `json:"hedef"` // çıkarılacak dizin (opsiyonel; boşsa arşivin dizini)
}

// extractIs: asenkron çıkarma işinin ilerleme durumu. Büyük arşivlerde (100k+
// dosya) senkron HTTP dakikalarca askıda kalıyordu ve kullanıcı hiçbir geri
// bildirim görmüyordu → iş goroutine'e taşındı, UI ilerlemeyi poll eder.
type extractIs struct {
	mu     sync.Mutex
	Durum  string // "calisiyor" | "tamam" | "hata"
	Toplam int    // arşivdeki üye sayısı (0 = henüz sayılıyor)
	Cikan  int    // şu ana dek çıkarılan üye
	Hata   string
	bitti  time.Time
}

var (
	extractIslerMu sync.Mutex
	extractIsler   = map[string]*extractIs{}
)

// extractIsID: tahmin edilemez iş kimliği (crypto/rand). Sıralı sayaç olsaydı
// başka bir oturum yabancı işin ilerlemesini gözetleyebilirdi.
func extractIsID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// extractIsTemizle: bitmiş işleri 10 dk sonra haritadan düşür (bellek sızıntısı olmasın).
func extractIsTemizle() {
	extractIslerMu.Lock()
	defer extractIslerMu.Unlock()
	for id, is := range extractIsler {
		is.mu.Lock()
		eski := is.Durum != "calisiyor" && !is.bitti.IsZero() && time.Since(is.bitti) > 10*time.Minute
		is.mu.Unlock()
		if eski {
			delete(extractIsler, id)
		}
	}
}

// ExtractProgress: GET .../files/extract-progress?id=<is_id>
func (h *Handlers) ExtractProgress(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	extractIslerMu.Lock()
	is := extractIsler[id]
	extractIslerMu.Unlock()
	if is == nil {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı (bitmiş olabilir)")
		return
	}
	is.mu.Lock()
	resp := map[string]any{
		"durum":  is.Durum,
		"toplam": is.Toplam,
		"cikan":  is.Cikan,
		"hata":   is.Hata,
	}
	is.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handlers) Extract(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req extractReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	abs, err := jailJoinStrict(home, req.Yol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "dosya bulunamadı veya klasör")
		return
	}

	hedef := req.Hedef
	if hedef == "" {
		hedef = filepath.Dir(req.Yol)
	}
	hedefAbs, err := jailJoinStrict(home, hedef)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef: "+err.Error())
		return
	}
	if err := os.MkdirAll(hedefAbs, 0755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "mkdir hedef: "+err.Error())
		return
	}
	// GÜVENLİK: hedef dizini çıkarmadan ÖNCE tenant kullanıcısına devret ki
	// çıkarma root DEĞİL, tenant olarak (DAC altında) çalışabilsin.
	_, _ = exec.Command("chown", sk+":"+sk, hedefAbs).CombinedOutput()

	low := strings.ToLower(abs)
	gzTek := strings.HasSuffix(low, ".gz") && archivex.TuruBelirle(low) == archivex.TurBilinmeyen
	tur := archivex.TuruBelirle(low)
	if !gzTek && tur == archivex.TurBilinmeyen {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen format (zip, tar, tar.gz/tgz, tar.bz2, tar.xz, gz, rar)")
		return
	}

	// ── ASENKRON: büyük arşivde (100k+ üye) senkron HTTP dakikalarca askıda
	// kalıyordu, kullanıcı hiçbir şey görmüyordu. İş goroutine'de yürür; yanıt
	// hemen is_id döner, UI /extract-progress ile ilerlemeyi izler.
	extractIsTemizle()
	isID := extractIsID()
	is := &extractIs{Durum: "calisiyor"}
	extractIslerMu.Lock()
	extractIsler[isID] = is
	extractIslerMu.Unlock()

	basarisiz := func(msg string) {
		is.mu.Lock()
		is.Durum = "hata"
		is.Hata = msg
		is.bitti = time.Now()
		is.mu.Unlock()
	}

	go func() {
		if gzTek {
			// Tek dosyalık .gz: üye yolu yoktur; tek risk çıktı dosyasının symlink
			// üzerinden dışarı yazması. jailJoinStrict + O_NOFOLLOW ile kapat.
			is.mu.Lock()
			is.Toplam = 1
			is.mu.Unlock()
			rel := filepath.Join(hedef, strings.TrimSuffix(filepath.Base(abs), ".gz"))
			gzHedef, jerr := jailJoinStrict(home, rel)
			if jerr != nil {
				basarisiz("gz hedef: " + jerr.Error())
				return
			}
			// O_NOFOLLOW: gzHedef bir symlink ise ELi'ni takip etmeden hata ver.
			gzOut, gzErr := os.OpenFile(gzHedef, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0644)
			if gzErr != nil {
				basarisiz("gz hedef: " + gzErr.Error())
				return
			}
			var eb bytes.Buffer
			gzc := exec.Command("gunzip", "-k", "-c", abs)
			gzc.Stdout = gzOut
			gzc.Stderr = &eb
			runErr := gzc.Run()
			gzOut.Close()
			if runErr != nil {
				basarisiz("extract: " + strings.TrimSpace(eb.String()))
				return
			}
			is.mu.Lock()
			is.Cikan = 1
			is.mu.Unlock()
		} else {
			// Toplam üye sayısı → ilerleme çubuğu paydası. Sayım hatası ölümcül
			// değil (Toplam=0 → UI belirsiz-mod çubuk gösterir).
			if n, serr := archivex.UyeSay(abs, tur); serr == nil {
				is.mu.Lock()
				is.Toplam = n
				is.mu.Unlock()
			}
			// zip / tar ailesi: ORTAK güvenli-extract helper (çift savunma:
			// tenant-user DAC + üye-yolu doğrulama, symlink/hardlink reddi).
			out, exErr := archivex.GuvenliCikarIlerleme(abs, hedefAbs, sk, func(delta int) {
				is.mu.Lock()
				is.Cikan += delta
				is.mu.Unlock()
			})
			if exErr != nil {
				msg := exErr.Error()
				if strings.TrimSpace(out) != "" {
					msg += ": " + strings.TrimSpace(out)
				}
				basarisiz("extract: " + msg)
				return
			}
		}

		// İzole ortam: çıkartılan tüm dosyaları domain user'ına chown (+ SELinux context).
		_, _ = exec.Command("chown", "-R", sk+":"+sk, hedefAbs).CombinedOutput()
		_, _ = exec.Command("restorecon", "-R", hedefAbs).CombinedOutput()
		// Per-user izin modeli (FIX 1): çıkarılan içeriğe nginx okuma-ACL'ini teyit et. docroot'un
		// default-ACL'i genelde bunu zaten miras verir; hedef docroot-dışıysa/ACL yoksa garanti.
		// setfacl yoksa (acl paketi eksik) sessiz atlanır — dosyalar tenant'ta, site diğer yolla servis edilir.
		if _, err := exec.LookPath("setfacl"); err == nil {
			_, _ = exec.Command("setfacl", "-R", "-m", "u:nginx:rX", hedefAbs).CombinedOutput()
			_, _ = exec.Command("setfacl", "-R", "-d", "-m", "u:nginx:rX", hedefAbs).CombinedOutput()
		}
		is.mu.Lock()
		is.Durum = "tamam"
		is.bitti = time.Now()
		is.mu.Unlock()
	}()

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"is_id": isID,
		"yol":   req.Yol,
		"hedef": hedef,
	})
}

// ----- Copy / Move (toplu) -----

type bulkMoveCopyReq struct {
	Kaynaklar []string `json:"kaynaklar"`
	Hedef     string   `json:"hedef"` // hedef KLASÖR (içine konulacak)
}

func (h *Handlers) Copy(w http.ResponseWriter, r *http.Request) {
	h.bulkMoveCopy(w, r, false)
}

func (h *Handlers) Move(w http.ResponseWriter, r *http.Request) {
	h.bulkMoveCopy(w, r, true)
}

func (h *Handlers) bulkMoveCopy(w http.ResponseWriter, r *http.Request, move bool) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req bulkMoveCopyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// TOCTOU symlink-güvenli: hedef klasörü openat2 ile doğrula (ara-bileşen symlink REDDEDİLİR).
	hedefRel := relClean(req.Hedef)
	if ok, err := isDirBeneath(home, hedefRel); err != nil || !ok {
		httpx.WriteError(w, http.StatusBadRequest, "hedef klasör değil")
		return
	}

	basarili := 0
	hatalar := []string{}
	for _, k := range req.Kaynaklar {
		kRel := relClean(k)
		if kRel == "" || kRel == "." {
			hatalar = append(hatalar, k+": geçersiz kaynak")
			continue
		}
		dstRel := filepath.Join(hedefRel, filepath.Base(kRel))
		if dstRel == kRel {
			hatalar = append(hatalar, k+": kaynak ve hedef aynı")
			continue
		}
		// Symlink-güvenli taşı/kopyala: parent'lar openat2 ile pinlenir, hiçbir symlink takip
		// edilmez; kopyada jail-dışı symlink İÇERİĞİ okunmaz (bilgi sızması yok) (bkz. safeio.go).
		var op error
		if move {
			op = renameBeneath(home, kRel, dstRel, sk)
		} else {
			op = copyTreeBeneath(home, kRel, dstRel, sk)
		}
		if op != nil {
			hatalar = append(hatalar, k+": "+op.Error())
			continue
		}
		_ = chownTreeBeneath(home, dstRel, sk)
		basarili++
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": len(hatalar) == 0, "basarili": basarili, "hatalar": hatalar,
	})
}

// copyAny/copyFile/copyDir KALDIRILDI: path-tabanlı eski kopya (os.Open/os.OpenFile string
// yol üzerinde) TOCTOU symlink-yarışına açıktı. Kopya artık safeio.go'daki symlink-güvenli
// copyTreeBeneath (openat2 + O_NOFOLLOW, jail-dışı symlink içeriğini sızdırmaz) ile yapılır.

// ----- Arşivle (seçili dosyaları zip yap) -----

type archiveReq struct {
	Kaynaklar []string `json:"kaynaklar"`
	CiktiYol  string   `json:"cikti_yol"` // örn /public_html/yedek.zip
	Format    string   `json:"format"`    // zip | tar.gz
}

func (h *Handlers) Archive(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req archiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.Kaynaklar) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kaynak yok")
		return
	}
	if req.Format == "" {
		req.Format = "zip"
	}
	ciktiAbs, err := jailJoinStrict(home, req.CiktiYol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "cikti: "+err.Error())
		return
	}
	_ = os.MkdirAll(filepath.Dir(ciktiAbs), 0755)

	// Tum kaynaklarin home-altinda abs yolunu hazirla, relative isimlerle arşivle
	// Stratejisi: ortak parent'i bul, oradan çalış
	var args []string
	if req.Format == "zip" {
		args = []string{"-r", "-q", ciktiAbs}
		for _, k := range req.Kaynaklar {
			kAbs, err := jailJoinStrict(home, k)
			if err != nil {
				continue
			}
			// chdir + relative isim
			args = append(args, kAbs)
		}
		out, err := exec.Command("zip", args...).CombinedOutput()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "zip: "+strings.TrimSpace(string(out)))
			return
		}
	} else { // tar.gz
		args = []string{"-czf", ciktiAbs}
		for _, k := range req.Kaynaklar {
			kAbs, err := jailJoinStrict(home, k)
			if err != nil {
				continue
			}
			args = append(args, kAbs)
		}
		out, err := exec.Command("tar", args...).CombinedOutput()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "tar: "+strings.TrimSpace(string(out)))
			return
		}
	}
	_, _ = exec.Command("chown", sk+":"+sk, ciktiAbs).CombinedOutput()
	_, _ = exec.Command("restorecon", ciktiAbs).CombinedOutput()

	info, _ := os.Stat(ciktiAbs)
	var boyut int64
	if info != nil {
		boyut = info.Size()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "cikti_yol": req.CiktiYol, "boyut": boyut,
	})
}

// ----- Yeni boş dosya -----

type yeniDosyaReq struct {
	Yol string `json:"yol"`
}

func (h *Handlers) YeniDosya(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req yeniDosyaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// TOCTOU symlink-güvenli yeni-dosya: openat2 + O_EXCL (ara-bileşen/leaf symlink REDDEDİLİR),
	// fd üzerinden tenant'a chown (bkz. safeio.go). Eski os.Stat+os.OpenFile(abs) resolved-string
	// üzerinde çalışıp ara-dizin symlink takasına açıktı.
	if err := createExclBeneath(home, req.Yol, sk); err != nil {
		if errors.Is(err, os.ErrExist) {
			httpx.WriteError(w, http.StatusConflict, "dosya zaten var")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "yol": req.Yol})
}

// ----- Boyut hesapla (du -sb) -----

func (h *Handlers) BoyutHesapla(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := exec.Command("du", "-sb", abs).CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "du: "+strings.TrimSpace(string(out)))
		return
	}
	parts := strings.Fields(string(out))
	if len(parts) < 1 {
		httpx.WriteError(w, http.StatusInternalServerError, "du çıktı parse edilemedi")
		return
	}
	var b int64
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			break
		}
		b = b*10 + int64(c-'0')
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":     rel,
		"boyut_b": b,
	})
}

// ----- Arama (recursive find by name pattern) -----

func (h *Handlers) Ara(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"icerik": []any{}, "toplam": 0})
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	baseAbs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Güvenlik: q sadece dosya adı pattern, shell injection olmaması için iname kullan
	q = strings.ReplaceAll(q, "*", "")
	q = strings.ReplaceAll(q, "?", "")
	pattern := "*" + q + "*"

	out, _ := exec.Command("find", baseAbs, "-iname", pattern, "-printf", "%p\t%s\t%y\t%T@\n").Output()
	results := []Entry{}
	for _, ln := range strings.Split(string(out), "\n") {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		absp := parts[0]
		size := int64(0)
		for _, c := range parts[1] {
			if c < '0' || c > '9' {
				break
			}
			size = size*10 + int64(c-'0')
		}
		tip := "dosya"
		if parts[2] == "d" {
			tip = "klasor"
		} else if parts[2] == "l" {
			tip = "sembolik"
		}
		// rel yol home altina goreceli
		yolRel := strings.TrimPrefix(absp, home)
		if yolRel == "" {
			yolRel = "/"
		}
		info, _ := os.Stat(absp)
		mod := "0644"
		var degisme string
		if info != nil {
			mod = "0" + strconv.FormatInt(int64(info.Mode().Perm()), 8)
			degisme = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		results = append(results, Entry{
			Adi: filepath.Base(absp), Yol: filepath.ToSlash(yolRel),
			Tip: tip, BoyutB: size, Mod: mod, Degisme: degisme,
		})
		if len(results) >= 500 {
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"icerik": results, "toplam": len(results), "q": q,
	})
}
