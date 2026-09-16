// Package erisim: domain bazlı erişim kısıtlama (Access Restrictions) API'si.
//
// GET/PUT  /domains/{id}/erisim          — toggle + varsayılan politika
// POST     /domains/{id}/erisim/kural    — kural ekle
// DELETE   /domains/{id}/erisim/kural/{kid} — kural sil
//
// Yazma işlemlerinden sonra vhost yeniden render edilir. RerenderVhost içinde
// `nginx -t` kapısı ve bozuk config'te eski dosyaya geri dönüş var; bu yüzden
// hatalı bir kural canlı nginx'i düşüremez.
package erisim

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	DB *sql.DB
}

// denetle — erisim politikasi degisikliklerini audit_log'a yazar.
//
// 🔴 Denetimde bulundu: onlarca politika degisikligi yapildi, audit_log'da TEK
// SATIR yoktu. Bir bayi, bir domainin erisim politikasini (ya da tum sunucuyu
// etkileyen guvenilir vekil listesini) iz birakmadan degistirebiliyordu.
// Guvenlik ozelliginin kendisi denetlenebilir olmali.
func (h *Handlers) denetle(r *http.Request, domainID int64, eylem, hedef, detay string, ok bool) {
	var uid int64
	var kullanici string
	if c := middleware.ClaimsFrom(r); c != nil {
		uid, kullanici = c.UserID, c.Username
	} else if mc := middleware.MusteriClaimsFrom(r); mc != nil {
		// 🔴 Musteri token'i da AKTORDUR. Onceki surum yalniz panel
		// (admin/bayi) claim'ine bakiyordu; hosting sahibinin kendi
		// panelinden yaptigi degisiklik denetime "actor_user_id=NULL,
		// actor_username=''" olarak, yani ANONIM dusuyordu. Denetim
		// bosluguna karsi yazilan kayit, en sik kullanilan yolda kimseyi
		// yazmiyordu.
		kullanici = "musteri:" + strconv.FormatInt(mc.DomainID, 10)
	}
	if domainID > 0 {
		httpx.DenetimDomain(h.DB, r, uid, kullanici, eylem, hedef, detay, domainID, ok)
		return
	}
	httpx.DenetimSistem(h.DB, uid, kullanici, eylem, hedef, detay, 0, ok)
}

// kisaHata — istemciye donen mesajdan dosya yollarini ve ham motor
// ciktisini ayiklar.
//
// 🔴 Onceki surum `nginx -t` ciktisinin TAMAMINI (komsu domainlerin conf
// yollari dahil) ve ham DB hatalarini istemciye yaziyordu; bir bayiye komsu
// yapilandirma yollari ve sema detaylari siziyordu.
func kisaHata(err error) string {
	m := err.Error()
	if i := strings.Index(m, "/etc/nginx"); i >= 0 {
		m = m[:i] + "(yapılandırma dosyası)"
	}
	if len(m) > 180 {
		m = m[:180] + "…"
	}
	return m
}

type Kural struct {
	ID       int64  `json:"id"`
	Tip      string `json:"tip"`      // "izin" | "red"
	CIDR     string `json:"cidr"`     // tek IP ya da CIDR
	Aciklama string `json:"aciklama"` // serbest not
	Sira     int    `json:"sira"`
}

// domainSK — domain'in sistem kullanıcısını döndürür; yoksa 404 yazar.
// MusteriScope middleware'i zaten sahiplik kısıtını uyguluyor; burada yalnız
// varlık kontrolü yapılır.
func (h *Handlers) domainSK(w http.ResponseWriter, r *http.Request, id int64) (string, bool) {
	var sk string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici FROM domains WHERE id=?`, id).Scan(&sk)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return "", false
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return "", false
	}
	return sk, true
}

// GET /domains/{id}/erisim
func (h *Handlers) Goster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := h.domainSK(w, r, id); !ok {
		return
	}

	aktif, varsayilan := 0, "izin"
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT aktif, varsayilan FROM domain_erisim_kisit WHERE domain_id=?`, id).
		Scan(&aktif, &varsayilan)

	kurallar := []Kural{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, tip, cidr, aciklama, sira FROM domain_erisim_kurallari
		 WHERE domain_id=? ORDER BY sira ASC, id ASC`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k Kural
			if err := rows.Scan(&k.ID, &k.Tip, &k.CIDR, &k.Aciklama, &k.Sira); err == nil {
				kurallar = append(kurallar, k)
			}
		}
	}

	// 🔴 Kilitlenme koruması için UI'ın göstermesi gereken iki bilgi:
	//
	//  istemci_ip  — isteği yapan kişinin IP'si. "Kendimi kilitledim" hatasının
	//                en yaygın sebebi, kullanıcının kendi IP'sini bilmemesi.
	//  vekil_sayisi— tanımlı güvenilir vekil (CDN) aralığı sayısı. SIFIR ise ve
	//                site bir CDN arkasındaysa kısıtlama SESSİZCE yanlış çalışır;
	//                UI bunu uyarı olarak gösterir.
	var vekilSayisi int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM guvenilir_vekil`).Scan(&vekilSayisi)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"aktif":        aktif == 1,
		"varsayilan":   varsayilan,
		"kurallar":     kurallar,
		"istemci_ip":   httpx.ClientIP(r),
		"vekil_sayisi": vekilSayisi,
	})
}

// PUT /domains/{id}/erisim   body: {"aktif":true,"varsayilan":"red"}
func (h *Handlers) Kaydet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := h.domainSK(w, r, id); !ok {
		return
	}
	var req struct {
		Aktif      bool   `json:"aktif"`
		Varsayilan string `json:"varsayilan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	vs := strings.ToLower(strings.TrimSpace(req.Varsayilan))
	if vs != "izin" && vs != "red" {
		httpx.WriteError(w, http.StatusBadRequest, "varsayilan 'izin' ya da 'red' olmalı")
		return
	}

	// Geri alma icin onceki durum (yoksa varsayilan kapali).
	eskiAktif, eskiVarsayilan := 0, "izin"
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT aktif, varsayilan FROM domain_erisim_kisit WHERE domain_id=?`, id).
		Scan(&eskiAktif, &eskiVarsayilan)

	aktifVal := 0
	if req.Aktif {
		aktifVal = 1
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO domain_erisim_kisit (domain_id, aktif, varsayilan) VALUES (?,?,?)
		 ON DUPLICATE KEY UPDATE aktif=VALUES(aktif), varsayilan=VALUES(varsayilan)`,
		id, aktifVal, vs); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}

	if err := provisioner.ErisimKisitUygula(h.DB, id); err != nil {
		// 🔴 GERI AL. Onceki surum DB'yi birakip 500 donuyordu; sonuc
		// "basarisizlik" degil ERTELEME oluyordu: kullanici hata gorurken
		// kayit DB'de kaliyor ve ILGISIZ bir tetikleyici (WAF degisimi, PHP
		// surumu, ASKIDAN ALMA, SSL YENILEME) saatler sonra o degisikligi
		// SESSIZCE yururluge sokuyordu. Denetimde uctan uca olculdu.
		if _, e := h.DB.ExecContext(r.Context(),
			`INSERT INTO domain_erisim_kisit (domain_id, aktif, varsayilan) VALUES (?,?,?)
			 ON DUPLICATE KEY UPDATE aktif=VALUES(aktif), varsayilan=VALUES(varsayilan)`,
			id, eskiAktif, eskiVarsayilan); e == nil {
			_ = provisioner.ErisimKisitUygula(h.DB, id) // eski hâli geri uygula
		}
		h.denetle(r, id, "erisim.ayar", strconv.FormatInt(id, 10),
			"render basarisiz, GERI ALINDI: "+err.Error(), false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"ayar uygulanamadı, önceki durum geri yüklendi: "+kisaHata(err))
		return
	}
	h.denetle(r, id, "erisim.ayar", strconv.FormatInt(id, 10),
		fmt.Sprintf("aktif=%v varsayilan=%s", req.Aktif, vs), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "aktif": req.Aktif, "varsayilan": vs})
}

// POST /domains/{id}/erisim/kural   body: {"tip":"izin","cidr":"1.2.3.4","aciklama":"ofis"}
func (h *Handlers) KuralEkle(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, ok := h.domainSK(w, r, id); !ok {
		return
	}
	var req struct {
		Tip      string `json:"tip"`
		CIDR     string `json:"cidr"`
		Aciklama string `json:"aciklama"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tip := strings.ToLower(strings.TrimSpace(req.Tip))
	if tip != "izin" && tip != "red" {
		httpx.WriteError(w, http.StatusBadRequest, "tip 'izin' ya da 'red' olmalı")
		return
	}
	// Kanonik ağ biçimine çevir: "1.2.3.4/0" -> "0.0.0.0/0". Böylece DB'de,
	// panelde ve nginx'te AYNI şey yazar; kullanıcı ne uygulandığını görür.
	cidr := provisioner.ErisimKuralNormalize(req.CIDR)
	// Doğrulama provisioner ile AYNI fonksiyon — iki katmanın kuralı ayrışamaz.
	if !provisioner.ErisimKuralGecerli(cidr) {
		httpx.WriteError(w, http.StatusBadRequest,
			"geçersiz IP/CIDR (örnek: 203.0.113.45 ya da 198.51.100.0/24)")
		return
	}
	aciklama := strings.TrimSpace(req.Aciklama)
	if len(aciklama) > 190 {
		aciklama = aciklama[:190]
	}
	// Ust sinir: her kural tam bir vhost render + GLOBAL `nginx -t` + reload
	// tetikliyor. Sinirsiz liste, hem render suresini hem de komsulari
	// etkileyen kilit suresini buyutur.
	var mevcut int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM domain_erisim_kurallari WHERE domain_id=?`, id).Scan(&mevcut)
	if mevcut >= 200 {
		httpx.WriteError(w, http.StatusBadRequest, "kural sınırına ulaşıldı (en fazla 200)")
		return
	}

	// 🔴 Sıra numarası TEK İFADEDE hesaplanır. Önceki sürüm önce MAX(sira)
	// okuyup sonra INSERT ediyordu; araya giren eşzamanlı bir ekleme aynı
	// sırayı üretiyordu (12 eşzamanlı eklemede 9 benzersiz sıra ölçüldü).
	// Bugün zararsız — `ORDER BY sira, id` beraberliği id ile bozuyor ve id
	// ekleme sırasına eşit — ama ileride bir "yeniden sırala" ucu eklenirse
	// bu latent hata gerçek hataya döner. Okuma ile yazma arasındaki boşluğu
	// bırakmamak, sonra hatırlamaktan ucuz.
	// 🔴 `INSERT ... SELECT` GERI ALINDI. Sira benzersizligi icin denenmisti
	// ama olcum, ilacin hastaliktan kotu oldugunu gosterdi: ayni tabloya
	// INSERT...SELECT, MariaDB 10.11 + innodb_autoinc_lock_mode=1 altinda
	// eszamanlilikta ~%10 oraninda ham hata donduruyor:
	//     Error 1467: Failed to read auto-increment value from storage engine
	// Duz INSERT VALUES ayni yukte 20/20 temiz.
	//
	// Yarisin kendisi ZARARSIZ: siralama `ORDER BY sira ASC, id ASC` ve id
	// ekleme sirasina esit oldugu icin beraberlik dogru cozuluyor. Latent bir
	// kusuru, GERCEK bir araliklı arizayla takas etmeye degmez.
	var sira int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(sira),0)+10 FROM domain_erisim_kurallari WHERE domain_id=?`, id).Scan(&sira)
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO domain_erisim_kurallari (domain_id, tip, cidr, aciklama, sira) VALUES (?,?,?,?,?)`,
		id, tip, cidr, aciklama, sira)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			// Benzersizlik anahtari (domain/alt alan + cidr) `tip`'i kapsamiyor;
			// kullanici bir kurali izin<->red cevirmek isteyince buraya dusuyor
			// ve neden oldugunu anlamiyordu. Mesaj eylemi soylesin.
			httpx.WriteError(w, http.StatusConflict,
				"bu IP/CIDR zaten listede — izin/red yönünü değiştirmek için önce mevcut kuralı silin")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	kid, _ := res.LastInsertId()

	if err := provisioner.ErisimKisitUygula(h.DB, id); err != nil {
		// Telafi: eklenen satiri geri al ve onceki hâli yeniden uygula.
		// Aksi halde nginx'in reddettigi bir deger DB'de kalir ve o domainin
		// SONRAKI TUM render'larini (SSL yenilemesi dahil) kalici olarak
		// dusurur -- denetimde uctan uca olculdu.
		_, _ = h.DB.ExecContext(r.Context(),
			`DELETE FROM domain_erisim_kurallari WHERE id=? AND domain_id=?`, kid, id)
		_ = provisioner.ErisimKisitUygula(h.DB, id)
		h.denetle(r, id, "erisim.kural.ekle", strconv.FormatInt(id, 10),
			"render basarisiz, GERI ALINDI: "+tip+" "+cidr, false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"kural uygulanamadı, eklenmedi: "+kisaHata(err))
		return
	}
	h.denetle(r, id, "erisim.kural.ekle", strconv.FormatInt(id, 10),
		fmt.Sprintf("%s %s", tip, cidr), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": kid})
}

// DELETE /domains/{id}/erisim/kural/{kid}
func (h *Handlers) KuralSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	kid, _ := strconv.ParseInt(chi.URLParam(r, "kid"), 10, 64)
	if _, ok := h.domainSK(w, r, id); !ok {
		return
	}
	// domain_id koşulu ZORUNLU: yalnız kural id'siyle silmek, başka bir
	// müşterinin kuralını silmeye izin verirdi (IDOR).
	// Geri koyabilmek icin satiri ONCE oku.
	var yTip, yCidr, yAciklama string
	var ySira int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT tip, cidr, aciklama, sira FROM domain_erisim_kurallari WHERE id=? AND domain_id=?`,
		kid, id).Scan(&yTip, &yCidr, &yAciklama, &ySira)

	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM domain_erisim_kurallari WHERE id=? AND domain_id=?`, kid, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.WriteError(w, http.StatusNotFound, "kural bulunamadı")
		return
	}
	if err := provisioner.ErisimKisitUygula(h.DB, id); err != nil {
		// Telafi: silinen kurali geri koy (silme, korumayi GENISLETEBILIR;
		// basarisiz bir silme sessizce yururluge girmemeli).
		if yCidr != "" {
			_, _ = h.DB.ExecContext(r.Context(),
				`INSERT INTO domain_erisim_kurallari (id, domain_id, tip, cidr, aciklama, sira)
				 VALUES (?,?,?,?,?,?)`, kid, id, yTip, yCidr, yAciklama, ySira)
			_ = provisioner.ErisimKisitUygula(h.DB, id)
		}
		h.denetle(r, id, "erisim.kural.sil", strconv.FormatInt(id, 10),
			"render basarisiz, GERI ALINDI: kural_id="+strconv.FormatInt(kid, 10), false)
		httpx.WriteError(w, http.StatusInternalServerError,
			"kural silinemedi, geri yüklendi: "+kisaHata(err))
		return
	}
	h.denetle(r, id, "erisim.kural.sil", strconv.FormatInt(id, 10),
		"kural_id="+strconv.FormatInt(kid, 10), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── Güvenilir vekil (CDN) yönetimi — AdminOnly ──────────────────────────────

// GET /guvenilir-vekil
func (h *Handlers) VekilListe(w http.ResponseWriter, r *http.Request) {
	type vekil struct {
		ID       int64  `json:"id"`
		CIDR     string `json:"cidr"`
		Aciklama string `json:"aciklama"`
		// 🔴 Render katmani cok genis / yerel araliklari REDDEDIYOR ama satir
		// DB'de kaliyordu ve panel onu yapilandirilmis gibi listeliyordu —
		// "panel X gosteriyor, nginx Y yapiyor" sapmasi. Artik isaretli.
		Gecerli bool `json:"gecerli"`
	}
	out := []vekil{}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, cidr, aciklama FROM guvenilir_vekil ORDER BY id ASC`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v vekil
		if err := rows.Scan(&v.ID, &v.CIDR, &v.Aciklama); err == nil {
			v.Gecerli = provisioner.VekilAraligiGecerli(v.CIDR)
			out = append(out, v)
		}
	}
	baslik := "X-Forwarded-For"
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT deger FROM cp_ayarlar WHERE anahtar='realip_header'`).Scan(&baslik)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"vekiller": out, "baslik": baslik})
}

// POST /guvenilir-vekil   body: {"cidr":"173.245.48.0/20","aciklama":"Cloudflare","baslik":"CF-Connecting-IP"}
func (h *Handlers) VekilEkle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR     string `json:"cidr"`
		Aciklama string `json:"aciklama"`
		Baslik   string `json:"baslik"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	cidr := strings.TrimSpace(req.CIDR)
	// Vekil araliginda EK genislik kapisi: 0.0.0.0/0 gibi bir aralik, nginx'in
	// HERKESIN X-Forwarded-For'una guvenmesine ve kisitlamanin tek baslikla
	// atlatilmasina yol acar.
	if !provisioner.VekilAraligiGecerli(cidr) {
		httpx.WriteError(w, http.StatusBadRequest,
			"geçersiz ya da fazla geniş aralık (IPv4 en fazla /8, IPv6 en fazla /32 olabilir)")
		return
	}
	if b := strings.TrimSpace(req.Baslik); b != "" {
		if !strings.EqualFold(b, "X-Forwarded-For") && !strings.EqualFold(b, "CF-Connecting-IP") {
			httpx.WriteError(w, http.StatusBadRequest, "baslik yalnız X-Forwarded-For ya da CF-Connecting-IP olabilir")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`INSERT INTO cp_ayarlar (anahtar, deger) VALUES ('realip_header', ?)
			 ON DUPLICATE KEY UPDATE deger=VALUES(deger)`, b); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT IGNORE INTO guvenilir_vekil (cidr, aciklama) VALUES (?,?)`,
		cidr, strings.TrimSpace(req.Aciklama)); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	h.vekilUygula(w, r, "guvenilir_vekil.ekle", cidr)
}

// DELETE /guvenilir-vekil/{id}
func (h *Handlers) VekilSil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM guvenilir_vekil WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt işlemi başarısız")
		return
	}
	h.vekilUygula(w, r, "guvenilir_vekil.sil", strconv.FormatInt(id, 10))
}

// vekilUygula — 00-gosp-realip.conf'u tazeler ve nginx'i doğrulayıp yeniden yükler.
func (h *Handlers) vekilUygula(w http.ResponseWriter, r *http.Request, vekilEylem, vekilHedef string) {
	n, err := provisioner.RealIPConfYaz(h.DB)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "realip conf yazılamadı: "+err.Error())
		return
	}
	// RealIPConfYaz kendi içinde `nginx -t` + reload + geri alma yapıyor;
	// ayrıca reload etmek gereksiz ikinci bir kesinti penceresi açardı.
	h.denetle(r, 0, vekilEylem, vekilHedef,
		fmt.Sprintf("aralik_sayisi=%d", n), true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "vekil_sayisi": n})
}
