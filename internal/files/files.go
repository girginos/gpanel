// Package files: domain ev dizinine chroot edilmis dosya yoneticisi API'si
package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// MaxUploadBytes: tek yükleme için üst sınır. Onceden 10 GiB idi ve ParseMultipartForm'a
// maxMemory olarak veriliyordu → tek istekle 10 GiB RAM ayrilabiliyordu (DoS).
const MaxUploadBytes = 2 * 1024 * 1024 * 1024 // 2 GiB — hata mesajlariyla ESIT; bos-disk sinirina uygun

// maxMultipartMemory: multipart ayrıştırmada RAM'de tutulacak azami tampon. Fazlası
// otomatik olarak geçici diske taşar → büyük yüklemelerde RAM patlamaz.
const maxMultipartMemory = 32 << 20 // 32 MiB

type Handlers struct {
	DB *sql.DB
}

// home: domain id -> /home/c_<user>
func (h *Handlers) home(r *http.Request) (string, string, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", os.ErrNotExist
	}
	if err != nil {
		return "", "", err
	}
	if isDemo == 1 {
		return "", "", errDemo
	}
	if !strings.HasPrefix(sk, "c_") {
		return "", "", errBadUser
	}
	return "/home/" + sk, sk, nil
}

var (
	errDemo    = errors.New("demo aboneliğin dosyaları yönetilemez")
	errBadUser = errors.New("güvenlik: geçersiz sistem kullanıcısı")
	errEscape  = errors.New("güvenlik: ev dizini dışına çıkış engellendi")
)

type Entry struct {
	Adi      string `json:"adi"`
	Yol      string `json:"yol"` // home'a goreceli (panel UI icin)
	Tip      string `json:"tip"` // "klasor" | "dosya" | "sembolik"
	BoyutB   int64  `json:"boyut_b"`
	Mod      string `json:"mod"`      // "0644"
	Yetkiler string `json:"yetkiler"` // rwx dizesi: "rwxr-xr-x"
	Sahip    string `json:"sahip"`    // owner kullanıcı adı (uid çözülemezse sayı)
	Grup     string `json:"grup"`     // grup adı (gid çözülemezse sayı)
	Degisme  string `json:"degisme"`  // RFC3339
}

// uid/gid → isim çözüm cache'i (aynı user/grup tekrar tekrar lookup edilmesin).
var (
	uidAdiMu  sync.RWMutex
	uidAdiMap = map[uint32]string{}
	gidAdiMu  sync.RWMutex
	gidAdiMap = map[uint32]string{}
)

func uidAdi(uid uint32) string {
	uidAdiMu.RLock()
	if v, ok := uidAdiMap[uid]; ok {
		uidAdiMu.RUnlock()
		return v
	}
	uidAdiMu.RUnlock()
	ad := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(ad); err == nil && u.Username != "" {
		ad = u.Username
	}
	uidAdiMu.Lock()
	uidAdiMap[uid] = ad
	uidAdiMu.Unlock()
	return ad
}

func gidAdi(gid uint32) string {
	gidAdiMu.RLock()
	if v, ok := gidAdiMap[gid]; ok {
		gidAdiMu.RUnlock()
		return v
	}
	gidAdiMu.RUnlock()
	ad := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(ad); err == nil && g.Name != "" {
		ad = g.Name
	}
	gidAdiMu.Lock()
	gidAdiMap[gid] = ad
	gidAdiMu.Unlock()
	return ad
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	abs, err := jailJoinStrict(home, rel)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := os.ReadDir(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma: "+err.Error())
		return
	}
	out := make([]Entry, 0, len(dir))
	for _, e := range dir {
		info, err := e.Info()
		if err != nil {
			continue
		}
		tip := "dosya"
		if info.IsDir() {
			tip = "klasor"
		} else if info.Mode()&os.ModeSymlink != 0 {
			tip = "sembolik"
		}
		// Sahip / grup (Linux): FileInfo.Sys() → *syscall.Stat_t. Çözülemezse "-".
		sahip, grup := "-", "-"
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			sahip = uidAdi(st.Uid)
			grup = gidAdi(st.Gid)
		}
		out = append(out, Entry{
			Adi:      e.Name(),
			Yol:      filepath.ToSlash(filepath.Join(rel, e.Name())),
			Tip:      tip,
			BoyutB:   info.Size(),
			Mod:      "0" + strconv.FormatInt(int64(info.Mode().Perm()), 8),
			Yetkiler: yetkiRWX(info.Mode()),
			Sahip:    sahip,
			Grup:     grup,
			Degisme:  info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	// klasörler önce, sonra alfabetik
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Tip == "klasor") != (out[j].Tip == "klasor") {
			return out[i].Tip == "klasor"
		}
		return strings.ToLower(out[i].Adi) < strings.ToLower(out[j].Adi)
	})

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":    filepath.ToSlash(rel),
		"icerik": out,
		"toplam": len(out),
	})
}

// Dosya icerigini ham olarak donder (download)
func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
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
	info, err := os.Stat(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "klasör indirilemez")
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "açılamadı: "+err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+info.Name()+"\"")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

// Metin dosyasini okuma (editor icin)
func (h *Handlers) Read(w http.ResponseWriter, r *http.Request) {
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
	info, err := os.Stat(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "bulunamadı")
		return
	}
	if info.Size() > 2*1024*1024 {
		httpx.WriteError(w, http.StatusBadRequest, "dosya 2 MB'tan büyük; düzenleme için uygun değil")
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"yol":    rel,
		"icerik": string(data),
		"boyut":  info.Size(),
	})
}

type mkdirReq struct {
	Yol string `json:"yol"`
}

func (h *Handlers) Mkdir(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req mkdirReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	// TOCTOU symlink-güvenli: mkdir -p'yi her bileşende Mkdirat+O_NOFOLLOW ile yürüt; yeni
	// dizinler fd üzerinden tenant'a chown edilir (bkz. safeio.go). Eski os.MkdirAll(abs)
	// resolved-string üzerinde çalışıp ara-dizin symlink takasına açıktı.
	if err := mkdirAllBeneath(home, req.Yol, sk); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "mkdir: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "yol": req.Yol})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	home, _, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	if p := relClean(rel); p == "" || p == "." {
		httpx.WriteError(w, http.StatusBadRequest, "ana ev dizini silinemez")
		return
	}
	// TOCTOU symlink-güvenli: parent'ı openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS) ile pinle,
	// fd-özyinelemeli unlinkat ile sil (bkz. safeio.go). Eski os.RemoveAll(abs) resolved-string
	// üzerinde çalışıp ara-dizin symlink takasıyla jail-dışı silmeye kandırılabilirdi.
	if err := removeAllBeneath(home, rel); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silme: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": rel})
}

// uploadRezMu/uploadRezerve: eszamanli upload'larda toplam gecici-disk kullanimini
// sinirlar. Her upload MaxUploadBytes kadar "rezerve" eder; bos-disk - rezerve < gerekli
// ise 507 doner. Kucuk bos-diskte ( or. 3.3GB) iki paralel 2GiB upload'in /tmp'i doldurup
// tum servisleri dusurmesini onler (kaynak-tukenme DoS).
var (
	uploadRezMu   sync.Mutex
	uploadRezerve int64
)

// diskRezerveEt: yer varsa rezerve eder (true), yoksa false. serbestBirak ile geri verilir.
func diskRezerveEt() bool {
	gerekli := int64(MaxUploadBytes) + 256*1024*1024 // 2GiB + 256MB emniyet payi
	uploadRezMu.Lock()
	defer uploadRezMu.Unlock()
	var st syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &st); err != nil {
		return true // statfs basarisizsa engelleme (mevcut davranis)
	}
	bos := int64(st.Bavail) * int64(st.Bsize)
	if bos-uploadRezerve < gerekli {
		return false
	}
	uploadRezerve += int64(MaxUploadBytes)
	return true
}

func diskSerbestBirak() {
	uploadRezMu.Lock()
	uploadRezerve -= int64(MaxUploadBytes)
	if uploadRezerve < 0 {
		uploadRezerve = 0
	}
	uploadRezMu.Unlock()
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	rel := r.URL.Query().Get("yol")
	if rel == "" {
		rel = "/"
	}
	// Eszamanli upload disk-tukenme koruması: yer yoksa 507 (diski doldurup servisleri
	// dusurmek yerine reddet). Basarida rezervasyon her cikista serbest birakilir.
	if !diskRezerveEt() {
		httpx.WriteError(w, http.StatusInsufficientStorage, "sunucu disk alanı geçici olarak yetersiz, yükleme daha sonra deneyin")
		return
	}
	defer diskSerbestBirak()
	// DoS savunması: istek gövdesini üst sınırla kes. MaxBytesReader hem RAM'i hem
	// diski korur; sınır aşılınca okuma *http.MaxBytesError döner.
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	// maxMemory küçük → gövde RAM yerine geçici diske taşar (RAM DoS engellenir).
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || strings.Contains(err.Error(), "too large") {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "yükleme boyutu sınırı aştı (max 2 GiB)")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "form parse: "+err.Error())
		return
	}
	// DoS: 32MB ustu multipart geciciyi DISKE tasir; RemoveAll cagrilmazsa
	// /tmp/multipart-* 201 sonrasi kalir → yavas disk doldurma. Her cikista temizle.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, fh, err := r.FormFile("dosya")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya alanı bulunamadı: "+err.Error())
		return
	}
	defer file.Close()
	if fh.Size > MaxUploadBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "dosya çok büyük (max 2 GiB)")
		return
	}
	// Kiraci XFS disk kotasi: panel ROOT olarak yazip tenant'a chown'ladigindan kernel
	// EDQUOT'u baypas oluyordu (plan kotasi asilabiliyordu). Yazmadan ONCE kotayi kontrol et.
	if !uploadKotaYeterli(sk, fh.Size) {
		httpx.WriteError(w, http.StatusInsufficientStorage, "disk kotanız dolu — bu yükleme plan kotanızı aşıyor")
		return
	}
	// TOCTOU symlink-güvenli: hedefi openat2 ile aç (ara-bileşen/leaf symlink REDDEDİLİR),
	// fd'ye akışla kopyala, sonra fd üzerinden tenant'a chown (bkz. safeio.go). Eski
	// os.Create(abs) resolved-string üzerinde çalışıp symlink takasına açıktı.
	dstRel := filepath.Join(rel, fh.Filename)
	written, err := copyStreamBeneath(home, dstRel, file, sk)
	if err != nil {
		_ = removeAllBeneath(home, dstRel)
		httpx.WriteError(w, http.StatusInternalServerError, "yazma: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":    true,
		"yol":   filepath.ToSlash(filepath.Join(rel, fh.Filename)),
		"boyut": written,
		"isim":  fh.Filename,
	})
}

// yetkiRWX: izin bitlerini rwx dizesine çevirir (ör. 0644 → "rw-r--r--").
func yetkiRWX(m os.FileMode) string {
	const rwx = "rwxrwxrwx"
	p := m.Perm()
	b := []byte("---------")
	for i := 0; i < 9; i++ {
		if p&(1<<uint(8-i)) != 0 {
			b[i] = rwx[i]
		}
	}
	return string(b)
}

func statusFromErr(err error) int {
	switch err {
	case os.ErrNotExist:
		return http.StatusNotFound
	case errDemo:
		return http.StatusForbidden
	case errBadUser, errEscape:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// chown helper — dosyayı domain user'ına ata + SELinux context'ini düzelt (restorecon).
// restorecon ŞART: panel root olarak çalışır; oluşturduğu/değiştirdiği dosya doğru
// SELinux context'i (httpd_sys_content_t) almazsa nginx/php-fpm erişemez ve
// "dosya izinleri bozuldu" gibi görünür (SELinux Enforcing sunucularda).
func chown(path, sistemKullanici string) {
	if uu, err := userLookup(sistemKullanici); err == nil {
		_ = osChown(path, uu.UID, uu.GID)
	}
	_, _ = exec.Command("restorecon", path).CombinedOutput()
}

// uploadKotaYeterli: tenant'in XFS kotasinda ekBayt icin yer var mi? xfs_quota'dan used/hard
// (KB) okunur. Kota belirlenemezse (arac yok/parse hata) fail-open (true) → yuklemeyi bozma.
// hard=0 → sinirsiz. Boylece root-yazim + tenant-chown yolundaki kota-baypasi kapanir.
func uploadKotaYeterli(sk string, ekBayt int64) bool {
	if !strings.HasPrefix(sk, "c_") {
		return true
	}
	// XFS kota MOUNT'a baglidir: /home ayri mount ise orada, degilse rootfs / uzerinde
	// (rootflags=uquota). Her ikisini de dene; ilk gecerli satiri kullan.
	for _, mp := range []string{"/home", "/"} {
		out, err := exec.Command("xfs_quota", "-x", "-c", "quota -u -b -N "+sk, mp).CombinedOutput()
		if err != nil {
			continue
		}
		f := strings.Fields(string(out))
		if len(f) < 4 {
			continue
		}
		usedKB, e1 := strconv.ParseInt(f[1], 10, 64)
		hardKB, e2 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		if hardKB <= 0 {
			return true // sinirsiz
		}
		return usedKB*1024+ekBayt <= hardKB*1024
	}
	return true // kota belirlenemedi -> fail-open (yuklemeyi bozma)
}
