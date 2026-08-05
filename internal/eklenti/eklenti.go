package eklenti

// Eklenti (plugin) runtime — out-of-process eklentiler.
//
// TASARIM: Eklenti AYRI bir systemd servisi olarak çalışır ve yalnızca bir UNIX
// soketi dinler (TCP port AÇMAZ → dışarıdan erişilemez). Core:
//   - kaydı tutar (cp_eklentiler), paralı gate'i uygular (aktif=0 => 402)
//   - JWT'yi KENDİ doğrular, eklentiye kimliği güvenilir header ile geçirir
//   - /api/v1/eklenti/{ad}/* isteklerini sokete proxy'ler (SSE dahil)
//   - eklentinin frontend bundle'ını servis eder (/eklentiler/{ad}/app.js)
//
// KAZANÇ: core'da eklenti kodu YOK (yayınlanabilir kalır) ve eklenti çökse
// panel ayakta kalır — "panel çökmemeli" şartı mimariden gelir.

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/lisans"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

const bundleKok = "/opt/girginospanel/eklentiler"

type Handlers struct{ DB *sql.DB }

type Eklenti struct {
	Ad     string `json:"ad"`
	Etiket string `json:"etiket"`
	Surum  string `json:"surum"`
	Aktif  bool   `json:"aktif"`
	UI     bool   `json:"ui"`
	Saglik string `json:"saglik"`
	soket  string
}

// gecerliAd — yol/enjeksiyon güvenliği: yalnız harf/rakam/tire.
func gecerliAd(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func (h *Handlers) getir(ctx context.Context, ad string) (*Eklenti, error) {
	var e Eklenti
	var aktif, ui int
	err := h.DB.QueryRowContext(ctx,
		`SELECT ad, etiket, surum, aktif, ui, saglik, COALESCE(soket,'') FROM cp_eklentiler WHERE ad=?`, ad).
		Scan(&e.Ad, &e.Etiket, &e.Surum, &aktif, &ui, &e.Saglik, &e.soket)
	if err != nil {
		return nil, err
	}
	e.Aktif, e.UI = aktif == 1, ui == 1
	return &e, nil
}

// Liste — frontend hangi eklentiyi mount edeceğini buradan öğrenir.
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT ad, etiket, surum, aktif, ui, saglik FROM cp_eklentiler ORDER BY etiket`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "eklentiler alınamadı")
		return
	}
	defer rows.Close()
	out := []Eklenti{}
	for rows.Next() {
		var e Eklenti
		var aktif, ui int
		if err := rows.Scan(&e.Ad, &e.Etiket, &e.Surum, &aktif, &ui, &e.Saglik); err != nil {
			continue
		}
		e.Aktif, e.UI = aktif == 1, ui == 1
		out = append(out, e)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// Bundle — eklentinin frontend bundle'ı (/eklentiler/{ad}/app.js).
func (h *Handlers) Bundle(w http.ResponseWriter, r *http.Request) {
	ad := chi.URLParam(r, "ad")
	if !gecerliAd(ad) {
		http.NotFound(w, r)
		return
	}
	e, err := h.getir(r.Context(), ad)
	if err != nil || !e.Aktif || !e.UI {
		http.NotFound(w, r)
		return
	}
	yol := filepath.Join(bundleKok, ad, "app.js") // ad doğrulandı → path traversal yok
	if _, err := os.Stat(yol); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // sürüm atlamasın
	http.ServeFile(w, r, yol)
}

// Proxy — /api/v1/eklenti/{ad}/* → eklentinin UNIX soketi.
// SSE için ResponseController ile flush edilir (httputil.ReverseProxy stream'i korur).
func (h *Handlers) Proxy(w http.ResponseWriter, r *http.Request) {
	ad := chi.URLParam(r, "ad")
	if !gecerliAd(ad) {
		httpx.WriteError(w, http.StatusNotFound, "eklenti yok")
		return
	}
	e, err := h.getir(r.Context(), ad)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "eklenti kayıtlı değil: "+ad)
		return
	}
	if !e.Aktif {
		// paralı gate — modül kapalı
		httpx.WriteError(w, http.StatusPaymentRequired, "bu eklenti etkin değil: "+e.Etiket)
		return
	}
	if e.soket == "" {
		httpx.WriteError(w, http.StatusServiceUnavailable, "eklenti soketi tanımsız")
		return
	}
	// 🔴 Ücretli eklentiye giden TEK meşru yol burasıdır: kurulu ikili imzalı
	// paket başlığıyla uyuşmuyorsa (yamalanmış) istek GEÇMEZ. Periyodik denetim
	// servisi durdurur ama root onu geri başlatabilir; kapı burada durmazsa bir
	// sonraki taramaya kadar ücretli yüzey açık kalırdı.
	if izin, mesaj := lisans.ButunlukKapisi(ad); !izin {
		httpx.WriteError(w, http.StatusPaymentRequired, mesaj)
		return
	}

	rp := &httputil.ReverseProxy{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				// 🔴 Eklenti yeniden başlarken (kurulum/güncelleme) soket ~1-2sn YOK olur.
				// Dial'ı kısa aralıklarla birkaç kez dene: istek yalnız dial BAŞARILI
				// olunca gönderilir → retry METHOD-GÜVENLİ (POST çift işlenmez). Panel
				// zaten ayakta; bu yalnız geçici pencereyi ŞEFFAF yapar.
				var son error
				for i := 0; i < 6; i++ {
					c, err := d.DialContext(ctx, "unix", e.soket)
					if err == nil {
						return c, nil
					}
					son = err
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(250 * time.Millisecond):
					}
				}
				return nil, son
			},
			ResponseHeaderTimeout: 0, // SSE: yanıt başlığı uzun sürebilir, sınırlama
		},
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "eklenti" // unix soket — host anlamsız, sabit
			// /api/v1/eklenti/ai/sohbetler → /sohbetler
			on := "/api/v1/eklenti/" + ad
			req.URL.Path = strings.TrimPrefix(req.URL.Path, on)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			// Kimliği güvenilir header ile geçir — eklenti JWT doğrulamaz,
			// yalnız core'a (sokete) güvenir.
			// 🔴 Dışarıdan gelen taklit header'ları ÖNCE TEMİZLE (spoof koruması).
			req.Header.Del("X-Gosp-Kullanici")
			req.Header.Del("X-Gosp-Uid")
			req.Header.Del("X-Gosp-Rol")
			req.Header.Del("X-Gosp-Sahip") // spoof koruması
			if c := middleware.ClaimsFrom(req); c != nil {
				req.Header.Set("X-Gosp-Uid", strconv.FormatInt(c.UserID, 10))
				req.Header.Set("X-Gosp-Kullanici", c.Username)
				req.Header.Set("X-Gosp-Rol", c.Role)
				// Reseller ise: eklenti sahiplik-scope'u uygulasın diye sahip olduğu
				// domain adlarını güvenilir header ile geç (admin kısıtsız — header yok).
				if c.Role == "reseller" {
					req.Header.Set("X-Gosp-Sahip", h.resellerDomainAdlari(req.Context(), c.ResellerID))
				}
			}
		},
		FlushInterval: -1, // SSE: her yazımda anında flush
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// eklenti ölü/yeniden başlıyor olabilir — panel AYAKTA kalır. Kullanıcıya
			// ham teknik hata (dial unix ... connect ...) DEĞİL, nazik/eyleme dönük
			// mesaj göster; gerçek hata yalnız log'a.
			log.Printf("eklenti proxy [%s] ulaşılamadı: %v", ad, err)
			w.Header().Set("Retry-After", "3")
			httpx.WriteError(w, http.StatusServiceUnavailable,
				"Bu modül şu an hazırlanıyor ya da yeniden başlatılıyor. Lütfen birkaç saniye sonra tekrar deneyin.")
		},
	}
	rp.ServeHTTP(w, r)
}

// SaglikTara — tüm eklentilerin soketini yoklar, cp_eklentiler.saglik günceller.
func (h *Handlers) SaglikTara(ctx context.Context) {
	rows, err := h.DB.QueryContext(ctx, `SELECT ad, COALESCE(soket,'') FROM cp_eklentiler WHERE aktif=1`)
	if err != nil {
		return
	}
	type kayit struct{ ad, soket string }
	var liste []kayit
	for rows.Next() {
		var k kayit
		if err := rows.Scan(&k.ad, &k.soket); err == nil {
			liste = append(liste, k)
		}
	}
	rows.Close()
	for _, k := range liste {
		saglik := "saglksiz"
		if k.soket != "" {
			c, err := net.DialTimeout("unix", k.soket, 2*time.Second)
			if err == nil {
				_ = c.Close()
				saglik = "saglikli"
			}
		}
		_, _ = h.DB.ExecContext(ctx,
			`UPDATE cp_eklentiler SET saglik=?, son_kontrol=NOW() WHERE ad=?`, saglik, k.ad)
	}
}

// SaglikDongusu — periyodik sağlık taraması (main'den goroutine olarak çağrılır).
func (h *Handlers) SaglikDongusu(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	h.SaglikTara(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.SaglikTara(ctx)
		}
	}
}

// Routes — core'a bağlanan uçlar. Hepsi AdminOnly (eklentiler admin yüzeyi).
func (h *Handlers) Routes(r chi.Router) {
	// Admin VE reseller: reseller kendi domainleri için mail vb. eklentiyi kullanır
	// (sahiplik-scope'u eklenti tarafında X-Gosp-Sahip ile uygulanır).
	r.With(middleware.AdminVeyaReseller).Get("/eklentiler", h.Liste)
	r.With(middleware.AdminVeyaReseller).HandleFunc("/eklenti/{ad}/*", h.Proxy)
}

// resellerDomainAdlari — reseller'in sahip olduğu domain adlarını boşlukla ayrılmış
// döndürür. Eklenti bu listeyi X-Gosp-Sahip'ten okuyup her uçta hedef-domain'in
// listede olmasını şart koşar (yatay IDOR koruması). Admin'e header geçilmez → kısıtsız.
func (h *Handlers) resellerDomainAdlari(ctx context.Context, rid int64) string {
	if rid <= 0 {
		return ""
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT alan_adi FROM domains WHERE reseller_id=?`, rid)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && d != "" {
			out = append(out, d)
		}
	}
	return strings.Join(out, " ")
}
