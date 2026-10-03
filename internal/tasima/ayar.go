package tasima

// ayar.go — kökün taşıma anahtarları ve rol farkındalıklı kaynak listesi.
//
//   - cp_ayarlar 'tasima_bayi_acik': bayide taşıma (kullanıcı kararı: VARSAYILAN
//     KAPALI; satır yoksa ya da '1' değilse kapalı). Kök tek anahtarla açar.
//   - tasima_kaynak_ayar(kod, etkin): kaynak başına kökün anahtarı. Satır yoksa
//     sürücünün VarsayilanEtkin'i (doğrulanmış açık, deneysel kapalı). Kapalı kaynak
//     KİMSE için kullanılamaz (kök yeniden açar); bayi listesinde hiç görünmez.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/httpx"
)

// Bayiye/köke açıkça gösterilen ret nedenleri (sabit metin, iç ayrıntı yok).
var (
	errTasimaKapali = errors.New("taşıma bu panelde kapalı")
	errKaynakKapali = errors.New("bu kaynak türü bu panelde kullanılamıyor")
)

// bayiAcikMi — kök bayilere taşımayı açtı mı? Okuma hatası = kapalı (güvenli taraf).
func (h *Handlers) bayiAcikMi(ctx context.Context) bool {
	var v string
	if err := h.DB.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar='tasima_bayi_acik'`).Scan(&v); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("tasima: tasima_bayi_acik okunamadi: %v", err)
		}
		return false
	}
	return v == "1"
}

// kaynakDurumlari — kod → etkin (kayıttaki her kaynak için; satır yoksa varsayılan).
// Okuma hatasında YALNIZ varsayılanlar döner ve günlüğe yazılır.
func (h *Handlers) kaynakDurumlari(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for _, s := range kaynakSurucuSirali() {
		out[s.Kod] = s.VarsayilanEtkin
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT kod, etkin FROM tasima_kaynak_ayar`)
	if err != nil {
		log.Printf("tasima: kaynak ayarlari okunamadi: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var kod string
		var etkin bool
		if rows.Scan(&kod, &etkin) == nil {
			if _, ok := out[kod]; ok {
				out[kod] = etkin
			}
		}
	}
	return out
}

// kaynakKullanilabilir — bu kapsam bu kaynak türüyle taşıma başlatabilir/test
// edebilir mi? Bayi: önce genel anahtar (errTasimaKapali), sonra kaynak anahtarı.
func (h *Handlers) kaynakKullanilabilir(ctx context.Context, kp Kapsam, tip string) error {
	if kp.Bayi() && !h.bayiAcikMi(ctx) {
		return errTasimaKapali
	}
	if _, ok := surucuBul(tip); !ok {
		return errKaynakKapali
	}
	if !h.kaynakDurumlari(ctx)[tip] {
		return errKaynakKapali
	}
	return nil
}

// kaynakYanit — GET /tasima/kaynaklar öğesi (lib/tasima.ts sözleşmesi).
type kaynakYanit struct {
	Kod        string  `json:"kod"`
	Etiket     string  `json:"etiket"`
	Olgunluk   string  `json:"olgunluk"`
	Etkin      bool    `json:"etkin"`
	Yetenekler Yetenek `json:"yetenekler"`
}

// Kaynaklar — GET /tasima/kaynaklar.
//   - Kök: {acik:true, bayi_acik, kaynaklar: HEPSİ (etkin bayrağıyla)}.
//   - Bayi: anahtar kapalıysa {acik:false, kaynaklar:[]}; açıksa yalnız ETKİN
//     kaynaklar, "sahipler" yeteneği HER ZAMAN false (bayi sahip taşıyamaz).
//     bayi_acik alanı bayiye false döner (kök ayarı bayinin bilgisi değildir).
func (h *Handlers) Kaynaklar(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if kod != 0 {
		httpx.WriteError(w, kod, msj)
		return
	}
	durum := h.kaynakDurumlari(r.Context())
	liste := []kaynakYanit{}
	if kp.Bayi() {
		if !h.bayiAcikMi(r.Context()) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"acik": false, "bayi_acik": false, "kaynaklar": liste})
			return
		}
		for _, s := range kaynakSurucuSirali() {
			if !durum[s.Kod] {
				continue
			}
			y := s.Yetenekler
			y.Sahipler = false
			liste = append(liste, kaynakYanit{Kod: s.Kod, Etiket: s.Etiket, Olgunluk: s.Olgunluk, Etkin: true, Yetenekler: y})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"acik": true, "bayi_acik": false, "kaynaklar": liste})
		return
	}
	for _, s := range kaynakSurucuSirali() {
		liste = append(liste, kaynakYanit{Kod: s.Kod, Etiket: s.Etiket, Olgunluk: s.Olgunluk, Etkin: durum[s.Kod], Yetenekler: s.Yetenekler})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"acik": true, "bayi_acik": h.bayiAcikMi(r.Context()), "kaynaklar": liste})
}

// AyarGuncelle — PUT /tasima/ayar {bayi_acik} (AdminOnly + kök kapsamı).
// Kapatınca bayilerin SIRADAKİ işleri iptal edilir (kimlikleri silinir); çalışan
// iş biter, geçmiş okunur kalır.
func (h *Handlers) AyarGuncelle(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if kod != 0 || kp.Bayi() {
		if kod == 0 {
			kod, msj = http.StatusForbidden, "sadece yöneticiler için"
		}
		httpx.WriteError(w, kod, msj)
		return
	}
	var g struct {
		BayiAcik *bool `json:"bayi_acik"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&g); err != nil || g.BayiAcik == nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	deger := "0"
	if *g.BayiAcik {
		deger = "1"
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('tasima_bayi_acik', ?)
		 ON DUPLICATE KEY UPDATE deger=VALUES(deger)`, deger); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayar kaydedilemedi")
		return
	}
	iptal := int64(0)
	if !*g.BayiAcik {
		iptal = h.bayiKuyrugunuIptalEt(r.Context(), "", "yönetici bayi taşımasını kapattı")
	}
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_bayi_ayar", "tasima_bayi_acik", deger, 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "bayi_acik": *g.BayiAcik, "iptal_edilen": iptal})
}

// KaynakGuncelle — PUT /tasima/kaynaklar/{kod} {etkin} (AdminOnly + kök kapsamı).
// Kayıtta olmayan kod → 404. Kapatınca o kaynağın SIRADAKİ işleri (kök + bayi)
// iptal edilir; çalışan iş biter.
func (h *Handlers) KaynakGuncelle(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if kod != 0 || kp.Bayi() {
		if kod == 0 {
			kod, msj = http.StatusForbidden, "sadece yöneticiler için"
		}
		httpx.WriteError(w, kod, msj)
		return
	}
	kaynakKod := chi.URLParam(r, "kod")
	if _, ok := surucuBul(kaynakKod); !ok {
		httpx.WriteError(w, http.StatusNotFound, "kaynak bulunamadı")
		return
	}
	var g struct {
		Etkin *bool `json:"etkin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&g); err != nil || g.Etkin == nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO tasima_kaynak_ayar (kod, etkin, guncelleyen_uid) VALUES (?,?,NULLIF(?,0))
		 ON DUPLICATE KEY UPDATE etkin=VALUES(etkin), guncelleyen_uid=VALUES(guncelleyen_uid)`,
		kaynakKod, *g.Etkin, kp.UID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ayar kaydedilemedi")
		return
	}
	iptal := int64(0)
	if !*g.Etkin {
		iptal = h.kaynakKuyrugunuIptalEt(r.Context(), kaynakKod)
	}
	deger := "kapali"
	if *g.Etkin {
		deger = "etkin"
	}
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_kaynak_ayar", kaynakKod, deger, 0, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "kod": kaynakKod, "etkin": *g.Etkin, "iptal_edilen": iptal})
}

// bayiKuyrugunuIptalEt — bayilerin (sahip_rid>0) sıradaki işlerini iptal eder.
// tip boş değilse yalnız o kaynak türü.
func (h *Handlers) bayiKuyrugunuIptalEt(ctx context.Context, tip, neden string) int64 {
	q := `UPDATE tasima_isleri SET durum='iptal', hata=?, bitis=NOW(),
	        kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
	      WHERE durum='kuyrukta' AND sahip_rid>0`
	args := []any{neden}
	if tip != "" {
		q += ` AND kaynak_tip=?`
		args = append(args, tip)
	}
	res, err := h.DB.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("tasima: bayi kuyrugu iptal edilemedi: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// kaynakKuyrugunuIptalEt — kapatılan kaynağın sıradaki TÜM işleri (kök + bayi).
func (h *Handlers) kaynakKuyrugunuIptalEt(ctx context.Context, tip string) int64 {
	res, err := h.DB.ExecContext(ctx,
		`UPDATE tasima_isleri SET durum='iptal', hata='yönetici bu kaynak türünü kapattı', bitis=NOW(),
		   kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
		 WHERE durum='kuyrukta' AND kaynak_tip=?`, tip)
	if err != nil {
		log.Printf("tasima: kaynak kuyrugu iptal edilemedi: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}
