package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

// Job: yedek/geri-yükleme işi (Plesk tarzı liste + ilerleme çubuğu).
type Job struct {
	ID          int64  `json:"id"`
	Tur         string `json:"tur"`   // manuel | otomatik
	Islem       string `json:"islem"` // yedek | geri
	Durum       string `json:"durum"` // calisiyor | tamam | kismi | hata
	Toplam      int    `json:"toplam"`
	Tamamlanan  int    `json:"tamamlanan"`
	Basari      int    `json:"basari"`
	Hata        int    `json:"hata"`
	BoyutB      int64  `json:"boyut_b"`
	AktifDomain string `json:"aktif_domain"`
	Mod         string `json:"mod"`
	Baslatan    string `json:"baslatan"`
	Baslangic   string `json:"baslangic"`
	Bitis       string `json:"bitis"`
}

func jobDurum(basari, hata int) string {
	if hata == 0 {
		return "tamam"
	}
	if basari == 0 {
		return "hata"
	}
	return "kismi"
}

// birDomainYedekle: tek domain arşivini üret + backups satırı (job_id ile) + uzak-hedef push.
// Retention/prune ÇAĞIRANDA (manuel=pruneManuelYedek, oto=pruneOld).
func birDomainYedekle(ctx context.Context, db *sql.DB, domainID int64, sk, tip, notlar string, jobID int64) (int64, string, error) {
	if !strings.HasPrefix(sk, "c_") {
		return 0, "", fmt.Errorf("güvensiz sk: %s", sk)
	}
	dir := filepath.Join(BackupRoot, sk)
	_ = os.MkdirAll(dir, 0700)
	stamp := time.Now().UTC().Format("20060102-150405")
	ek := ""
	if tip == "oto" {
		ek = "-auto"
	}
	dosya := fmt.Sprintf("%s%s-%s.tar.gz", sk, ek, stamp)
	boyut, err := arsivOlustur(ctx, db, domainID, sk, dir, dosya, time.Now().UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, "", err
	}
	var jid any
	if jobID > 0 {
		jid = jobID
	}
	if _, err := db.Exec(
		`INSERT INTO backups(domain_id, tip, dosya, boyut_b, notlar, job_id) VALUES(?,?,?,?,?,?)`,
		domainID, tip, dosya, boyut, notlar, jid); err != nil {
		return boyut, dosya, err
	}
	pushToDestinationAsync(db, domainID, filepath.Join(dir, dosya), dosya)
	return boyut, dosya, nil
}

// geriYukleCekirdek: bir backup'i verilen KABA modda (tam|dosyalar|veritabani) uygular.
// HTTP dışı — çoklu-domain restore job'u kullanır. Granüler dosya/DB seçimi tekil sayfada.
func geriYukleCekirdek(db *sql.DB, domainID, backupID int64, mod string, temiz bool) (string, error) {
	var sk, dosya string
	var isDemo int
	err := db.QueryRow(
		`SELECT d.sistem_kullanici, d.is_demo, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id WHERE b.id=? AND b.domain_id=?`, backupID, domainID).
		Scan(&sk, &isDemo, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("yedek bulunamadı")
	}
	if err != nil {
		return "", err
	}
	if isDemo == 1 {
		return "", fmt.Errorf("demo aboneliğe geri yükleme yapılamaz")
	}
	if !strings.HasPrefix(sk, "c_") {
		return "", fmt.Errorf("güvenlik")
	}
	abs := filepath.Join(BackupRoot, sk, dosya)
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("yedek dosyası diskte yok")
	}
	uyeListesi, _ := arsivUyeListesi(abs)
	uyeler := cikarUyeleri(mod, sk, uyeListesi, nil)
	if len(uyeler) == 0 {
		return "", fmt.Errorf("bu mod için içerik yok")
	}
	tmpDir, _ := os.MkdirTemp("/var/tmp", "gosp-restore-*")
	defer os.RemoveAll(tmpDir)
	if out, err := arsivUyeCikarRoot(abs, tmpDir, uyeler); err != nil {
		m := err.Error()
		if strings.TrimSpace(out) != "" {
			m += ": " + strings.TrimSpace(out)
		}
		return "", fmt.Errorf("%s", m)
	}
	switch mod {
	case "tam":
		homeGeriYukle(tmpDir, sk, temiz)
		tumDBGeriYukle(db, domainID, tmpDir, sk, "")
		return "tam geri yüklendi", nil
	case "dosyalar":
		homeGeriYukle(tmpDir, sk, temiz)
		return "dosyalar geri yüklendi", nil
	case "veritabani":
		tumDBGeriYukle(db, domainID, tmpDir, sk, "")
		return "veritabanları geri yüklendi", nil
	}
	return "", fmt.Errorf("geçersiz mod: %s", mod)
}

// ---- Handlers ----

// JobYedekBaslat: POST /admin/backups/jobs — TÜM (veya seçili) domainleri yedekleyen bir iş başlatır.
func (h *Handlers) JobYedekBaslat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainIDs []int64 `json:"domain_ids"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	type dm struct {
		id     int64
		sk, ad string
	}
	q := `SELECT id, sistem_kullanici, alan_adi FROM domains WHERE is_demo=0`
	args := []any{}
	if len(req.DomainIDs) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(req.DomainIDs)), ",")
		q += " AND id IN (" + ph + ")"
		for _, id := range req.DomainIDs {
			args = append(args, id)
		}
	}
	rows, err := h.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var liste []dm
	for rows.Next() {
		var d dm
		if rows.Scan(&d.id, &d.sk, &d.ad) == nil && strings.HasPrefix(d.sk, "c_") {
			liste = append(liste, d)
		}
	}
	rows.Close()
	if len(liste) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "yedeklenecek domain yok")
		return
	}

	_, kul := middleware.Aktor(r)
	res, err := h.DB.Exec(
		`INSERT INTO backup_jobs(tur, islem, durum, toplam, baslatan) VALUES('manuel','yedek','calisiyor',?,?)`,
		len(liste), kul)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jid, _ := res.LastInsertId()

	go func() {
		var toplamB int64
		basari, hata := 0, 0
		for _, d := range liste {
			h.DB.Exec(`UPDATE backup_jobs SET aktif_domain=? WHERE id=?`, d.ad, jid)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			b, _, err := birDomainYedekle(ctx, h.DB, d.id, d.sk, "tam", "Toplu yedek", jid)
			cancel()
			if err != nil {
				hata++
			} else {
				basari++
				toplamB += b
				pruneManuelYedek(h.DB, d.id, d.sk)
			}
			h.DB.Exec(`UPDATE backup_jobs SET tamamlanan=?, basari=?, hata=?, boyut_b=? WHERE id=?`,
				basari+hata, basari, hata, toplamB, jid)
		}
		h.DB.Exec(`UPDATE backup_jobs SET durum=?, aktif_domain='', bitis=NOW() WHERE id=?`,
			jobDurum(basari, hata), jid)
	}()

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "job_id": jid, "toplam": len(liste)})
}

// JobListe: GET /admin/backups/jobs — son işler (ilerleme için client 1-2sn'de bir poll'lar).
func (h *Handlers) JobListe(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, tur, islem, durum, toplam, tamamlanan, basari, hata, boyut_b, aktif_domain, geri_mod, baslatan,
		        DATE_FORMAT(baslangic,'%Y-%m-%d %H:%i'), COALESCE(DATE_FORMAT(bitis,'%Y-%m-%d %H:%i'),'')
		 FROM backup_jobs ORDER BY id DESC LIMIT 60`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if rows.Scan(&j.ID, &j.Tur, &j.Islem, &j.Durum, &j.Toplam, &j.Tamamlanan, &j.Basari, &j.Hata,
			&j.BoyutB, &j.AktifDomain, &j.Mod, &j.Baslatan, &j.Baslangic, &j.Bitis) == nil {
			out = append(out, j)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// JobDetay: GET /admin/backups/jobs/{jid} — yedek işi ise domain+backup listesi; geri işi ise per-domain sonuç.
func (h *Handlers) JobDetay(w http.ResponseWriter, r *http.Request) {
	jid, _ := strconv.ParseInt(chi.URLParam(r, "jid"), 10, 64)
	var j Job
	var detay sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, tur, islem, durum, toplam, tamamlanan, basari, hata, boyut_b, aktif_domain, geri_mod, baslatan,
		        DATE_FORMAT(baslangic,'%Y-%m-%d %H:%i'), COALESCE(DATE_FORMAT(bitis,'%Y-%m-%d %H:%i'),''), detay
		 FROM backup_jobs WHERE id=?`, jid).
		Scan(&j.ID, &j.Tur, &j.Islem, &j.Durum, &j.Toplam, &j.Tamamlanan, &j.Basari, &j.Hata,
			&j.BoyutB, &j.AktifDomain, &j.Mod, &j.Baslatan, &j.Baslangic, &j.Bitis, &detay)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]any{"is": j}
	if j.Islem == "geri" {
		var arr any
		if detay.Valid && detay.String != "" {
			_ = json.Unmarshal([]byte(detay.String), &arr)
		}
		resp["sonuclar"] = arr
	} else {
		type Kalem struct {
			BackupID int64  `json:"backup_id"`
			DomainID int64  `json:"domain_id"`
			AlanAdi  string `json:"alan_adi"`
			SK       string `json:"sistem_kullanici"`
			BoyutB   int64  `json:"boyut_b"`
			Tip      string `json:"tip"`
		}
		rows, _ := h.DB.QueryContext(r.Context(),
			`SELECT b.id, b.domain_id, d.alan_adi, d.sistem_kullanici, b.boyut_b, b.tip
			 FROM backups b JOIN domains d ON d.id=b.domain_id
			 WHERE b.job_id=? ORDER BY d.alan_adi`, jid)
		kalemler := []Kalem{}
		if rows != nil {
			for rows.Next() {
				var k Kalem
				if rows.Scan(&k.BackupID, &k.DomainID, &k.AlanAdi, &k.SK, &k.BoyutB, &k.Tip) == nil {
					kalemler = append(kalemler, k)
				}
			}
			rows.Close()
		}
		resp["domainler"] = kalemler
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// JobGeriBaslat: POST /admin/backups/restore — çoklu domain/app geri yükleme işi (ilerleme çubuklu).
func (h *Handlers) JobGeriBaslat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mod   string `json:"mod"` // tam | dosyalar | veritabani
		Temiz bool   `json:"temiz"`
		Items []struct {
			DomainID int64 `json:"domain_id"`
			BackupID int64 `json:"backup_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek")
		return
	}
	req.Mod = strings.TrimSpace(req.Mod)
	if req.Mod == "" {
		req.Mod = "tam"
	}
	if req.Mod != "tam" && req.Mod != "dosyalar" && req.Mod != "veritabani" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz mod")
		return
	}
	if len(req.Items) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geri yüklenecek öğe seçilmedi")
		return
	}

	// Öğeleri alan adıyla zenginleştir (ilerleme etiketi + detay için).
	type oge struct {
		domainID, backupID int64
		ad                 string
	}
	var oglar []oge
	for _, it := range req.Items {
		var ad string
		if h.DB.QueryRow(`SELECT alan_adi FROM domains WHERE id=?`, it.DomainID).Scan(&ad) == nil {
			oglar = append(oglar, oge{it.DomainID, it.BackupID, ad})
		}
	}
	if len(oglar) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçerli öğe yok")
		return
	}

	_, kul := middleware.Aktor(r)
	res, err := h.DB.Exec(
		`INSERT INTO backup_jobs(tur, islem, durum, toplam, geri_mod, baslatan) VALUES('manuel','geri','calisiyor',?,?,?)`,
		len(oglar), req.Mod, kul)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jid, _ := res.LastInsertId()

	go func() {
		type sonuc struct {
			DomainID int64  `json:"domain_id"`
			AlanAdi  string `json:"alan_adi"`
			Durum    string `json:"durum"`
			Mesaj    string `json:"mesaj"`
		}
		sonuclar := []sonuc{}
		basari, hata := 0, 0
		for _, o := range oglar {
			h.DB.Exec(`UPDATE backup_jobs SET aktif_domain=? WHERE id=?`, o.ad, jid)
			msg, err := geriYukleCekirdek(h.DB, o.domainID, o.backupID, req.Mod, req.Temiz)
			s := sonuc{DomainID: o.domainID, AlanAdi: o.ad}
			if err != nil {
				hata++
				s.Durum = "hata"
				s.Mesaj = err.Error()
			} else {
				basari++
				s.Durum = "tamam"
				s.Mesaj = msg
			}
			sonuclar = append(sonuclar, s)
			b, _ := json.Marshal(sonuclar)
			h.DB.Exec(`UPDATE backup_jobs SET tamamlanan=?, basari=?, hata=?, detay=? WHERE id=?`,
				basari+hata, basari, hata, string(b), jid)
		}
		h.DB.Exec(`UPDATE backup_jobs SET durum=?, aktif_domain='', bitis=NOW() WHERE id=?`,
			jobDurum(basari, hata), jid)
	}()

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "job_id": jid, "toplam": len(oglar)})
}
