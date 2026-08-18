package antivirus

// Karantina yönetimi — liste, geri yükle, sil, incele.
//
// 🔴 NEDEN: karantina tek yön değildir. Yanlış pozitif GERÇEK: obfuscated
// meşru bir eklenti webshell'e benzer. Geri yükleme olmadan karantina, kalıcı
// veri kaybı riskidir ve operatör özelliği kapatır. Bu dosya karantinayı
// GERİ ALINABİLİR bir işleme çevirir.

import (
	"errors"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sys/unix"

	"girginospanel/internal/httpx"
)

type karantinaKayit struct {
	ID           int64  `json:"id"`
	OrijinalYol  string `json:"orijinal_yol"`
	KarantinaYol string `json:"karantina_yol"`
	Imza         string `json:"imza"`
	Seviye       string `json:"seviye"`
	Puan         int    `json:"puan"`
	Durum        string `json:"durum"`
	Tarih        string `json:"tarih"`
	Boyut        int64  `json:"boyut"`
	Mevcut       bool   `json:"mevcut"` // karantina dosyası hâlâ diskte mi
}

// KarantinaListe — GET /domains/{id}/antivirus/karantina/liste
func (h *Handlers) KarantinaListe(w http.ResponseWriter, r *http.Request) {
	id, _, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, orijinal_yol, karantina_yol, imza, seviye, puan, durum,
		        DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		   FROM av_bulgular
		  WHERE domain_id=? AND durum IN ('karantina','geri_yuklendi','silindi')
		  ORDER BY created_at DESC LIMIT 500`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []karantinaKayit{}
	for rows.Next() {
		var k karantinaKayit
		if rows.Scan(&k.ID, &k.OrijinalYol, &k.KarantinaYol, &k.Imza,
			&k.Seviye, &k.Puan, &k.Durum, &k.Tarih) != nil {
			continue
		}
		if k.KarantinaYol != "" {
			if fi, err := os.Stat(k.KarantinaYol); err == nil {
				k.Mevcut = true
				k.Boyut = fi.Size()
			}
		}
		out = append(out, k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// bulguSahipMi — bid bu domaine ait mi + karantina yollarını döner.
// 🔴 Cross-user koruması: bulgu domain_id'si isteğin domain'iyle eşleşmeli VE
// yollar /home/<sk>/ içinde olmalı (path-traversal + başka müşteriye erişim).
func (h *Handlers) bulguSahipMi(r *http.Request, sk string, id int64) (bid int64, orij, kar, durum string, ok bool) {
	bid, _ = strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	if bid == 0 {
		return 0, "", "", "", false
	}
	var domID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT domain_id, orijinal_yol, karantina_yol, durum FROM av_bulgular WHERE id=?`, bid).
		Scan(&domID, &orij, &kar, &durum)
	if err != nil || domID != id {
		return 0, "", "", "", false
	}
	home := "/home/" + sk + "/"
	// Her iki yol da bu kiracının evinde olmalı.
	if orij != "" && !strings.HasPrefix(filepath.Clean(orij)+"/", home) &&
		filepath.Clean(orij) != "/home/"+sk {
		return 0, "", "", "", false
	}
	if kar != "" && !strings.HasPrefix(filepath.Clean(kar)+"/", home) {
		return 0, "", "", "", false
	}
	return bid, orij, kar, durum, true
}

// KarantinaGeriYukle — POST .../karantina/{bid}/geri-yukle
// 🔴 Yanlış pozitif kurtarma: karantinadaki dosyayı orijinal yerine taşır.
func (h *Handlers) KarantinaGeriYukle(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, orij, kar, durum, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if durum != "karantina" {
		httpx.WriteError(w, http.StatusBadRequest, "bu bulgu karantinada değil (durum: "+durum+")")
		return
	}
	if kar == "" || orij == "" {
		httpx.WriteError(w, http.StatusBadRequest, "karantina/orijinal yol kayıtlı değil — geri yüklenemez")
		return
	}
	if _, err := os.Lstat(kar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "karantina dosyası bulunamadı (elle silinmiş olabilir)")
		return
	}
	uid, gid, uok := kiraciUID(sk)
	if !uok {
		httpx.WriteError(w, http.StatusInternalServerError, "kiracı kullanıcı çözümlenemedi")
		return
	}
	// 🔴 SYMLINK-GÜVENLİ geri yükleme. Panel-server ROOT çalışır ve orij yolu
	// TAMAMEN kiracının mülkündedir (/home/<sk>/public_html/...). os.Rename ara
	// dizin bileşenlerindeki symlink'leri İZLER (CWE-59) ve os.Stat↔os.Rename
	// arası TOCTOU (CWE-367) vardır: kiracı bir ara bileşeni başka kiracının
	// public_html'ine symlink yapıp root'a ev DIŞINA yazdırabilir → cross-tenant
	// RCE. Çözüm: openat(O_NOFOLLOW) zinciriyle hedefi kiracı evine ÇAPALA, tüm
	// FS işlemlerini pinlenmiş dir-fd'lere göre (renameat/fchmodat/fchownat) yap.
	home := "/home/" + sk
	if err := guvenliGeriTasi(home, kar, orij, uid, gid); err != nil {
		switch {
		case errors.Is(err, os.ErrExist):
			httpx.WriteError(w, http.StatusConflict, "orijinal konumda zaten bir dosya var — elle çözün")
		case errors.Is(err, errGuvenliYol):
			httpx.WriteError(w, http.StatusBadRequest, "hedef yol güvenli değil (symlink/ev dışı) — geri yüklenmedi")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "geri yüklenemedi: "+err.Error())
		}
		return
	}
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='geri_yuklendi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "geri_yuklendi": orij})
}

// KarantinaSil — POST .../karantina/{bid}/sil
// 🔴 Kalıcı silme: karantinadaki dosyayı diskten kaldırır (temizlik).
func (h *Handlers) KarantinaSil(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if kar != "" {
		if err := os.Remove(kar); err != nil && !os.IsNotExist(err) {
			httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
			return
		}
	}
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='silindi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silindi": true})
}

// KarantinaIncele — GET .../karantina/{bid}/incele
// 🔴 GÜVENLİ İNCELEME: dosyayı ASLA ÇALIŞTIRMADAN, ilk 64 KB'ını düz metin
// döner. Operatör yanlış pozitif mi gerçek mi kendi görür. İçerik JSON string
// olarak döner (tarayıcı çalıştırmaz).
func (h *Handlers) KarantinaIncele(w http.ResponseWriter, r *http.Request) {
	id, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok || kar == "" {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	f, err := os.Open(kar)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya açılamadı (silinmiş olabilir)")
		return
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, _ := f.Read(buf)
	icerik := buf[:n]
	// İkili dosya mı? NUL varsa hex özet ver, ham değil.
	if hasNUL(icerik) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ikili":  true,
			"boyut":  n,
			"icerik": "[ikili dosya — metin gösterilemiyor]",
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ikili":  false,
		"boyut":  n,
		"kesik":  n == len(buf),
		"icerik": string(icerik),
	})
}

func hasNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// chownKiraci — geri yüklenen dosyayı kiracı kullanıcı:grup yapar (best-effort).
// 🔴 root taşıdı; sahiplik verilmezse site FPM'i dosyayı okuyamaz. Hata sessiz
// (dosya root kalır, panel yine erişir; operatör görür).
func chownKiraci(yol, sk string) {
	u, err := user.Lookup(sk)
	if err != nil {
		return
	}
	uid, e1 := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e1 == nil && e2 == nil {
		_ = os.Chown(yol, uid, gid)
	}
}


var errGuvenliYol = errors.New("guvenli-olmayan yol (symlink/ev disi)")

// kiraciUID — sk kullanıcısının uid/gid'i (fail-closed).
func kiraciUID(sk string) (int, int, bool) {
	u, err := user.Lookup(sk)
	if err != nil {
		return 0, 0, false
	}
	uid, e1 := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	return uid, gid, true
}

// evAltDirAc — home kökünden hedefDir'e bileşen bileşen O_NOFOLLOW ile iner.
// Bir bileşen symlink ise (ELOOP) ya da hedef home dışına çıkıyorsa errGuvenliYol
// döner. eksikOlustur=true ise olmayan ara dizinleri kiracı uid'iyle üretir.
// Dönen fd hedefDir'in GERÇEK (symlink'siz) halidir; çağıran KAPATMALI.
func evAltDirAc(home, hedefDir string, uid, gid int, eksikOlustur bool) (int, error) {
	home = filepath.Clean(home)
	hedefDir = filepath.Clean(hedefDir)
	if hedefDir != home && !strings.HasPrefix(hedefDir+"/", home+"/") {
		return -1, errGuvenliYol
	}
	// home güven çapasıdır (root-yönetimli); NOFOLLOW olmadan açılır.
	dirFd, err := unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if hedefDir == home {
		return dirFd, nil
	}
	rel, err := filepath.Rel(home, hedefDir)
	if err != nil {
		unix.Close(dirFd)
		return -1, errGuvenliYol
	}
	for _, parca := range strings.Split(rel, "/") {
		if parca == "" || parca == "." || parca == ".." {
			unix.Close(dirFd)
			return -1, errGuvenliYol
		}
		next, err := unix.Openat(dirFd, parca, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if eksikOlustur && errors.Is(err, unix.ENOENT) {
				if e := unix.Mkdirat(dirFd, parca, 0o755); e != nil {
					unix.Close(dirFd)
					return -1, e
				}
				_ = unix.Fchownat(dirFd, parca, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
				next, err = unix.Openat(dirFd, parca, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
			if err != nil {
				unix.Close(dirFd)
				// ELOOP: bileşen symlink (O_NOFOLLOW). ENOTDIR: bileşen dizin değil
				// ya da symlink-to-dir O_DIRECTORY ile reddedildi. İkisi de
				// symlink/kötü-yol saldırısı işaretidir → güvenli-değil.
				if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
					return -1, errGuvenliYol
				}
				return -1, err
			}
		}
		unix.Close(dirFd)
		dirFd = next
	}
	return dirFd, nil
}

// guvenliGeriTasi — kar dosyasını orij'e symlink-güvenli taşır. Root ayrıcalığı
// altında bile kiracı-denetimli yol üzerinden ev DIŞINA yazımı engeller: hem
// kaynak hem hedef dizin openat(O_NOFOLLOW) ile kiracı evine çapalanır, taşıma
// ve izin/sahiplik işlemleri pinlenmiş dir-fd'lere göre yapılır (TOCTOU-güvenli).
func guvenliGeriTasi(home, kar, orij string, uid, gid int) error {
	destDir := filepath.Dir(orij)
	destBase := filepath.Base(orij)
	karDir := filepath.Dir(kar)
	karBase := filepath.Base(kar)

	destFd, err := evAltDirAc(home, destDir, uid, gid, true)
	if err != nil {
		return err
	}
	defer unix.Close(destFd)

	// Çakışma: hedef zaten var mı (symlink izlemeden, pinlenmiş fd'ye göre).
	var st unix.Stat_t
	if unix.Fstatat(destFd, destBase, &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
		return os.ErrExist
	}

	karFd, err := evAltDirAc(home, karDir, uid, gid, false)
	if err != nil {
		return err
	}
	defer unix.Close(karFd)

	if err := unix.Renameat(karFd, karBase, destFd, destBase); err != nil {
		return err
	}
	// İzin/sahiplik: pinlenmiş hedef dir-fd'ye göre; symlink yeniden devreye
	// giremez çünkü destBase az önce rename ile konan gerçek dosyadır.
	_ = unix.Fchmodat(destFd, destBase, 0o644, 0)
	_ = unix.Fchownat(destFd, destBase, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
	return nil
}
