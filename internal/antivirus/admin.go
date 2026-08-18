package antivirus

// Sunucu-geneli (admin) antivirüs paneli — sol menüdeki "Antivirüs" sayfasının
// arka ucu. Domain-içi handler'lardan farkı: TÜM domainler kapsanır, kimlik URL
// {id}'den değil bulgunun domain_id'sinden çözülür. Yıkıcı işlemler yine
// symlink-güvenli guvenliGeriTasi ile ve /home/<sk>/ sınırında yapılır.

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/avmotor"
	"girginospanel/internal/httpx"
)

// AdminDurum — GET /antivirus/durum
func (h *Handlers) AdminDurum(w http.ResponseWriter, r *http.Request) {
	ajanKurulu := false
	if _, err := os.Stat(avajanYolu); err == nil {
		ajanKurulu = true
	}

	var toplamKarantina, toplamBulgu, tarananDomain int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM av_bulgular WHERE durum='karantina'`).Scan(&toplamKarantina)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM av_bulgular`).Scan(&toplamBulgu)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT domain_id) FROM av_taramalar`).Scan(&tarananDomain)

	type trow struct {
		ID      int64  `json:"id"`
		Alan    string `json:"alan_adi"`
		Durum   string `json:"durum"`
		Kaynak  string `json:"kaynak"`
		Taranan int    `json:"taranan"`
		Enfekte int    `json:"enfekte"`
		Bitis   string `json:"bitis"`
	}
	sonTara := []trow{}
	if rows, err := h.DB.QueryContext(r.Context(),
		`SELECT t.id, COALESCE(d.alan_adi,''), t.durum, COALESCE(t.kaynak,''), t.taranan, t.enfekte,
		        COALESCE(DATE_FORMAT(t.bitis,'%Y-%m-%d %H:%i'),'')
		   FROM av_taramalar t LEFT JOIN domains d ON d.id=t.domain_id
		  ORDER BY t.id DESC LIMIT 8`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var t trow
			if rows.Scan(&t.ID, &t.Alan, &t.Durum, &t.Kaynak, &t.Taranan, &t.Enfekte, &t.Bitis) == nil {
				sonTara = append(sonTara, t)
			}
		}
	}

	// Kural seti sürümü (ağ denemez — gömülü taban + varsa imzalı diski birleştirir).
	set := avmotor.GuncelSet(avmotor.TabanSet().Surum, true)
	kuralSurum, kuralUretim, kuralSayi := 0, "", 0
	if m, _ := avmotor.Yeni(set, 0); m != nil {
		kuralSurum, kuralUretim, kuralSayi = m.Surum(), m.Uretim(), m.KuralSayisi()
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ajan_kurulu":      ajanKurulu,
		"izleyici_aktif":   servisAktif("girginospanel-avizle"),
		"slice_aktif":      servisAktif("girginos-av.slice"),
		"kural_surum":      kuralSurum,
		"kural_uretim":     kuralUretim,
		"kural_sayisi":     kuralSayi,
		"toplam_karantina": toplamKarantina,
		"toplam_bulgu":     toplamBulgu,
		"taranan_domain":   tarananDomain,
		"son_taramalar":    sonTara,
	})
}

// AdminKarantinaListe — GET /antivirus/karantina (tüm domainler)
func (h *Handlers) AdminKarantinaListe(w http.ResponseWriter, r *http.Request) {
	type kar struct {
		ID          int64  `json:"id"`
		Alan        string `json:"alan_adi"`
		DomainID    int64  `json:"domain_id"`
		OrijinalYol string `json:"orijinal_yol"`
		Imza        string `json:"imza"`
		Seviye      string `json:"seviye"`
		Puan        int    `json:"puan"`
		Durum       string `json:"durum"`
		Tarih       string `json:"tarih"`
		Mevcut      bool   `json:"mevcut"`
	}
	out := []kar{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT b.id, COALESCE(d.alan_adi,''), b.domain_id, b.orijinal_yol, b.karantina_yol,
		        b.imza, b.seviye, b.puan, b.durum,
		        COALESCE(DATE_FORMAT(b.created_at,'%Y-%m-%d %H:%i:%s'),'')
		   FROM av_bulgular b LEFT JOIN domains d ON d.id=b.domain_id
		  WHERE b.durum IN ('karantina','geri_yuklendi','silindi')
		  ORDER BY b.created_at DESC LIMIT 500`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k kar
		var karYol string
		if rows.Scan(&k.ID, &k.Alan, &k.DomainID, &k.OrijinalYol, &karYol,
			&k.Imza, &k.Seviye, &k.Puan, &k.Durum, &k.Tarih) != nil {
			continue
		}
		if karYol != "" {
			if _, e := os.Stat(karYol); e == nil {
				k.Mevcut = true
			}
		}
		out = append(out, k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// adminBulgu — bid'den domain/sk çözer + yolları /home/<sk>/ sınırına hapseder.
func (h *Handlers) adminBulgu(r *http.Request) (bid, domID int64, sk, orij, kar, durum string, ok bool) {
	bid, _ = strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	if bid == 0 {
		return 0, 0, "", "", "", "", false
	}
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT b.domain_id, COALESCE(d.sistem_kullanici,''), b.orijinal_yol, b.karantina_yol, b.durum
		   FROM av_bulgular b LEFT JOIN domains d ON d.id=b.domain_id WHERE b.id=?`, bid).
		Scan(&domID, &sk, &orij, &kar, &durum)
	if err != nil || sk == "" || !strings.HasPrefix(sk, "c_") {
		return 0, 0, "", "", "", "", false
	}
	home := "/home/" + sk + "/"
	if orij != "" && !strings.HasPrefix(filepath.Clean(orij)+"/", home) {
		return 0, 0, "", "", "", "", false
	}
	if kar != "" && !strings.HasPrefix(filepath.Clean(kar)+"/", home) {
		return 0, 0, "", "", "", "", false
	}
	return bid, domID, sk, orij, kar, durum, true
}

// AdminKarantinaGeriYukle — POST /antivirus/karantina/{bid}/geri-yukle
func (h *Handlers) AdminKarantinaGeriYukle(w http.ResponseWriter, r *http.Request) {
	bid, _, sk, orij, kar, durum, ok := h.adminBulgu(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "bulgu bulunamadı")
		return
	}
	if durum != "karantina" {
		httpx.WriteError(w, http.StatusBadRequest, "bu bulgu karantinada değil (durum: "+durum+")")
		return
	}
	if kar == "" || orij == "" {
		httpx.WriteError(w, http.StatusBadRequest, "karantina/orijinal yol kayıtlı değil")
		return
	}
	if _, err := os.Lstat(kar); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "karantina dosyası bulunamadı")
		return
	}
	uid, gid, uok := kiraciUID(sk)
	if !uok {
		httpx.WriteError(w, http.StatusInternalServerError, "kiracı kullanıcı çözümlenemedi")
		return
	}
	// Symlink-güvenli geri yükleme (per-domain ile aynı sertleştirme).
	if err := guvenliGeriTasi("/home/"+sk, kar, orij, uid, gid); err != nil {
		switch {
		case os.IsExist(err):
			httpx.WriteError(w, http.StatusConflict, "orijinal konumda zaten bir dosya var — elle çözün")
		case err == errGuvenliYol:
			httpx.WriteError(w, http.StatusBadRequest, "hedef yol güvenli değil (symlink/ev dışı) — geri yüklenmedi")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "geri yüklenemedi: "+err.Error())
		}
		return
	}
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='geri_yuklendi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "geri_yuklendi": orij})
}

// AdminKarantinaSil — POST /antivirus/karantina/{bid}/sil
func (h *Handlers) AdminKarantinaSil(w http.ResponseWriter, r *http.Request) {
	bid, _, sk, _, kar, _, ok := h.adminBulgu(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "bulgu bulunamadı")
		return
	}
	if kar != "" {
		if err := guvenliSil("/home/"+sk, kar); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
			return
		}
	}
	_, _ = h.DB.Exec(`UPDATE av_bulgular SET durum='silindi', karantina=0 WHERE id=?`, bid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silindi": true})
}

// AdminKarantinaIncele — GET /antivirus/karantina/{bid}/incele (ÇALIŞTIRMADAN)
func (h *Handlers) AdminKarantinaIncele(w http.ResponseWriter, r *http.Request) {
	_, _, sk, _, kar, _, ok := h.adminBulgu(r)
	if !ok || kar == "" {
		httpx.WriteError(w, http.StatusNotFound, "bulgu bulunamadı")
		return
	}
	f, err := guvenliAc("/home/"+sk, kar)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya açılamadı (silinmiş olabilir)")
		return
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, _ := f.Read(buf)
	icerik := buf[:n]
	if hasNUL(icerik) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ikili": true, "boyut": n, "icerik": "[ikili dosya — metin gösterilemiyor]"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ikili": false, "boyut": n, "kesik": n == len(buf), "icerik": string(icerik)})
}

// AdminTaraTumu — POST /antivirus/tara-tumu → zamanlı tarama servisini şimdi çalıştır.
func (h *Handlers) AdminTaraTumu(w http.ResponseWriter, r *http.Request) {
	// Oneshot tarama birimini tetikle (tüm /home'u ajan-zamanlı modda tarar,
	// bulgu+bildirim üretir; oto-karantina yalnız ayar açıksa).
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "start", "--no-block", "girginospanel-avtara.service").CombinedOutput()
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tarama başlatılamadı: "+strings.TrimSpace(string(out))+" ("+err.Error()+")")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "baslatildi": true})
}

func servisAktif(birim string) bool {
	out, _ := exec.Command("systemctl", "is-active", birim).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// AdminGecmis — GET /antivirus/gecmis (tüm bulgular zaman çizelgesi/olay günlüğü)
func (h *Handlers) AdminGecmis(w http.ResponseWriter, r *http.Request) {
	type g struct {
		ID       int64  `json:"id"`
		Alan     string `json:"alan_adi"`
		DomainID int64  `json:"domain_id"`
		Dosya    string `json:"dosya"`
		Imza     string `json:"imza"`
		Motor    string `json:"motor"`
		Seviye   string `json:"seviye"`
		Puan     int    `json:"puan"`
		Durum    string `json:"durum"`
		Tarih    string `json:"tarih"`
	}
	out := []g{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT b.id, COALESCE(d.alan_adi,''), b.domain_id, b.dosya, b.imza, COALESCE(b.motor,''),
		        b.seviye, b.puan, b.durum, COALESCE(DATE_FORMAT(b.created_at,'%Y-%m-%d %H:%i:%s'),'')
		   FROM av_bulgular b LEFT JOIN domains d ON d.id=b.domain_id
		  ORDER BY b.created_at DESC, b.id DESC LIMIT 500`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x g
		if rows.Scan(&x.ID, &x.Alan, &x.DomainID, &x.Dosya, &x.Imza, &x.Motor, &x.Seviye, &x.Puan, &x.Durum, &x.Tarih) == nil {
			out = append(out, x)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// AdminDomainler — GET /antivirus/domainler: her domainin AV özeti (son tarama,
// aktif bulgu, karantina). Domain-bazlı tarama sekmesi için.
func (h *Handlers) AdminDomainler(w http.ResponseWriter, r *http.Request) {
	type dom struct {
		ID          int64  `json:"id"`
		Alan        string `json:"alan_adi"`
		SK          string `json:"sistem_kullanici"`
		SonTarama   string `json:"son_tarama"`
		SonTaranan  int    `json:"son_taranan"`
		SonEnfekte  int    `json:"son_enfekte"`
		AktifBulgu  int    `json:"aktif_bulgu"`
		Karantina   int    `json:"karantina"`
	}
	out := []dom{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT d.id, d.alan_adi, d.sistem_kullanici,
		        COALESCE((SELECT DATE_FORMAT(t.bitis,'%Y-%m-%d %H:%i') FROM av_taramalar t
		                   WHERE t.domain_id=d.id AND t.bitis IS NOT NULL ORDER BY t.id DESC LIMIT 1),'') son_tarama,
		        COALESCE((SELECT t.taranan FROM av_taramalar t WHERE t.domain_id=d.id ORDER BY t.id DESC LIMIT 1),0) son_taranan,
		        COALESCE((SELECT t.enfekte FROM av_taramalar t WHERE t.domain_id=d.id ORDER BY t.id DESC LIMIT 1),0) son_enfekte,
		        (SELECT COUNT(*) FROM av_bulgular b WHERE b.domain_id=d.id AND b.durum='aktif') aktif_bulgu,
		        (SELECT COUNT(*) FROM av_bulgular b WHERE b.domain_id=d.id AND b.durum='karantina') karantina
		   FROM domains d
		  WHERE d.sistem_kullanici LIKE 'c_%'
		  ORDER BY d.alan_adi LIMIT 1000`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x dom
		if rows.Scan(&x.ID, &x.Alan, &x.SK, &x.SonTarama, &x.SonTaranan, &x.SonEnfekte, &x.AktifBulgu, &x.Karantina) == nil {
			out = append(out, x)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}
