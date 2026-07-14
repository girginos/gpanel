// Package backups: per-domain tar + DB dump yedek
package backups

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const BackupRoot = "/var/backups/girginospanel"

// RemoveDomainBackups: bir domainin per-domain backup dizinini kaldırır.
// ÖNEMLİ: Domain silme akışından ÇAĞRILMAZ — müşteri yanlışlıkla silmiş olabilir,
// yedekler kurtarma için kasıtlı saklanır. Bu sadece operatörün açıkça istediği
// manuel temizlik (ör. "silinmiş domain yedeğini kalıcı kaldır") için bir yardımcıdır.
// Guard: sk mutlaka "c_" ile başlamalı ve yol BackupRoot altında olmalı (path-escape koruması).
func RemoveDomainBackups(sk string) error {
	if !strings.HasPrefix(sk, "c_") {
		return fmt.Errorf("geçersiz kullanıcı: %q", sk)
	}
	dir := filepath.Join(BackupRoot, sk)
	if dir == BackupRoot || !strings.HasPrefix(dir, BackupRoot+"/") {
		return fmt.Errorf("güvensiz backup yolu: %q", dir)
	}
	return os.RemoveAll(dir)
}

type Yedek struct {
	ID        int64  `json:"id"`
	DomainID  int64  `json:"domain_id"`
	Tip       string `json:"tip"`
	Dosya     string `json:"dosya"`
	BoyutB    int64  `json:"boyut_b"`
	Notlar    string `json:"notlar"`
	Olusturma string `json:"olusturma"`
}

type Handlers struct {
	DB *sql.DB
}

func (h *Handlers) lookupDomain(r *http.Request) (id int64, alanAdi, sk string, demo bool, err error) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var dmo int
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &dmo)
	demo = dmo == 1
	return
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, domain_id, tip, dosya, boyut_b, notlar, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		 FROM backups WHERE domain_id=? ORDER BY id DESC`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := make([]Yedek, 0)
	for rows.Next() {
		var y Yedek
		if err := rows.Scan(&y.ID, &y.DomainID, &y.Tip, &y.Dosya, &y.BoyutB, &y.Notlar, &y.Olusturma); err == nil {
			out = append(out, y)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// OzetSatir: bir domainin yedek özeti (sunucu-geneli görünüm için).
type OzetSatir struct {
	DomainID int64  `json:"domain_id"`
	AlanAdi  string `json:"alan_adi"`
	Sayi     int    `json:"sayi"`
	ToplamB  int64  `json:"toplam_b"`
	SonYedek string `json:"son_yedek"`
}

// Ozet: GET /admin/backups/ozet — TÜM domainlerin yedek özeti (dosya sisteminden, gerçek disk kullanımı).
func (h *Handlers) Ozet(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, alan_adi, sistem_kullanici FROM domains ORDER BY alan_adi`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	out := []OzetSatir{}
	var toplamB int64
	var toplamSayi int
	for rows.Next() {
		var id int64
		var alanAdi, sk string
		if err := rows.Scan(&id, &alanAdi, &sk); err != nil {
			continue
		}
		s := OzetSatir{DomainID: id, AlanAdi: alanAdi}
		var sonMod time.Time
		if entries, e := os.ReadDir(filepath.Join(BackupRoot, sk)); e == nil {
			for _, en := range entries {
				if en.IsDir() || !strings.HasSuffix(en.Name(), ".tar.gz") {
					continue
				}
				fi, e2 := en.Info()
				if e2 != nil {
					continue
				}
				s.Sayi++
				s.ToplamB += fi.Size()
				if fi.ModTime().After(sonMod) {
					sonMod = fi.ModTime()
				}
			}
		}
		if !sonMod.IsZero() {
			s.SonYedek = sonMod.Format("2006-01-02 15:04")
		}
		out = append(out, s)
		toplamB += s.ToplamB
		toplamSayi += s.Sayi
	}
	_ = rows.Err()
	var hedefSayisi int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM backup_destinations WHERE aktif=1`).Scan(&hedefSayisi)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"domainler":      out,
		"toplam_boyut_b": toplamB,
		"toplam_yedek":   toplamSayi,
		"hedef_sayisi":   hedefSayisi,
		"zamanlama":      "Her gün 03:00 (otomatik)",
	})
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	id, alanAdi, sk, demo, err := h.lookupDomain(r)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin yedeği alınamaz")
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik")
		return
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(BackupRoot, sk)
	_ = os.MkdirAll(dir, 0700)
	dosya := fmt.Sprintf("%s-%s.tar.gz", sk, stamp)
	abs := filepath.Join(dir, dosya)

	// DB dump
	dbName := sk + "_main"
	sqlDump := filepath.Join(dir, dosya+".sql")
	if out, derr := exec.Command("bash", "-c",
		fmt.Sprintf("mysqldump --single-transaction %s > %s 2>&1 || true", dbName, sqlDump)).CombinedOutput(); derr != nil {
		_ = os.WriteFile(sqlDump+".err", out, 0600)
	}

	// tar + dump beraber
	args := []string{
		"czf", abs,
		"-C", "/home", sk,
		"-C", dir, dosya + ".sql",
	}
	if out, terr := exec.Command("tar", args...).CombinedOutput(); terr != nil {
		_ = os.Remove(sqlDump)
		httpx.WriteError(w, http.StatusInternalServerError,
			"tar: "+strings.TrimSpace(string(out)))
		return
	}
	_ = os.Remove(sqlDump)

	st, _ := os.Stat(abs)
	var boyut int64
	if st != nil {
		boyut = st.Size()
	}

	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO backups(domain_id, tip, dosya, boyut_b, notlar) VALUES(?,?,?,?,?)`,
		id, "tam", dosya, boyut, "alan adı: "+alanAdi)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB kayıt: "+err.Error())
		return
	}
	yid, _ := res.LastInsertId()
	// Uzak hedef varsa arkaplanda yükle (API cevabını bloke etme)
	pushToDestinationAsync(h.DB, id, abs, dosya)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":      true,
		"id":      yid,
		"dosya":   dosya,
		"boyut_b": boyut,
		"yol":     abs,
	})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	var sk, dosya string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if err == nil {
		_ = os.Remove(filepath.Join(BackupRoot, sk, dosya))
	}
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM backups WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	var sk, dosya string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &dosya)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	abs := filepath.Join(BackupRoot, sk, dosya)
	f, err := os.Open(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+dosya+`"`)
	if st != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	_, _ = io.Copy(w, f)
}
