// template.go — Merkezi (sunucu geneli) düzenlenebilir DNS şablonu + DKIM anahtar üretimi.
// Domain eklerken ve "Varsayılan Şablonu Uygula" butonunda bu şablon DİNAMİK okunur.
// Placeholder'lar: {DOMAIN} alan adı, {IP} domain IPv4, {SELECTOR} DKIM seçici, {DKIM} DKIM public TXT.
package dns

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// TemplateRow: şablondaki tek kayıt satırı.
type TemplateRow struct {
	ID      int64  `json:"id"`
	Ad      string `json:"ad"`
	Tip     string `json:"tip"`
	Deger   string `json:"deger"`
	TTL     int    `json:"ttl"`
	Oncelik int    `json:"oncelik"`
	Sira    int    `json:"sira"`
	Aktif   bool   `json:"aktif"`
}

// TemplateMeta: şablon geneli SOA + DKIM ayarları (tek satır, id=1).
type TemplateMeta struct {
	SOARefresh   int    `json:"soa_refresh"`
	SOARetry     int    `json:"soa_retry"`
	SOAExpire    int    `json:"soa_expire"`
	SOAMinimum   int    `json:"soa_minimum"`
	SOATTL       int    `json:"soa_ttl"`
	DKIMSelector string `json:"dkim_selector"`
	DKIMAktif    bool   `json:"dkim_aktif"`
}

// builtinDefaults: tablo boşsa/ilk kurulumda tohumlanan varsayılan şablon.
// DMARC + SPF + DKIM dahil (Bug 5) — mail kimlik doğrulama kayıtları hazır gelir.
func builtinDefaults() []TemplateRow {
	return []TemplateRow{
		{Ad: "@", Tip: "A", Deger: "{IP}", TTL: 3600, Oncelik: 0, Sira: 10, Aktif: true},
		{Ad: "www", Tip: "A", Deger: "{IP}", TTL: 3600, Oncelik: 0, Sira: 20, Aktif: true},
		{Ad: "mail", Tip: "A", Deger: "{IP}", TTL: 3600, Oncelik: 0, Sira: 30, Aktif: true},
		{Ad: "@", Tip: "MX", Deger: "mail.{DOMAIN}", TTL: 3600, Oncelik: 10, Sira: 40, Aktif: true},
		{Ad: "@", Tip: "TXT", Deger: "v=spf1 a mx ip4:{IP} ~all", TTL: 3600, Oncelik: 0, Sira: 50, Aktif: true},
		{Ad: "_dmarc", Tip: "TXT", Deger: "v=DMARC1; p=quarantine; rua=mailto:postmaster@{DOMAIN}; ruf=mailto:postmaster@{DOMAIN}; fo=1; adkim=r; aspf=r", TTL: 3600, Oncelik: 0, Sira: 60, Aktif: true},
		{Ad: "{SELECTOR}._domainkey", Tip: "TXT", Deger: "{DKIM}", TTL: 3600, Oncelik: 0, Sira: 70, Aktif: true},
		{Ad: "ns1", Tip: "A", Deger: "{IP}", TTL: 3600, Oncelik: 0, Sira: 80, Aktif: true},
		{Ad: "ns2", Tip: "A", Deger: "{IP}", TTL: 3600, Oncelik: 0, Sira: 90, Aktif: true},
		{Ad: "@", Tip: "NS", Deger: "ns1.{DOMAIN}", TTL: 86400, Oncelik: 0, Sira: 100, Aktif: true},
		{Ad: "@", Tip: "NS", Deger: "ns2.{DOMAIN}", TTL: 86400, Oncelik: 0, Sira: 110, Aktif: true},
	}
}

// SeedTemplateIfEmpty: dns_template tablosu boşsa varsayılan şablonu tohumlar.
// Boş değilse (admin düzenlemiş) DOKUNMAZ — her açılışta çağrılır, idempotenttir.
func SeedTemplateIfEmpty(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=0`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, t := range builtinDefaults() {
		ak := 0
		if t.Aktif {
			ak = 1
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO dns_template(ad,tip,deger,ttl,oncelik,sira,aktif, reseller_id)
			 VALUES(?,?,?,?,?,?,?,?)`,
			t.Ad, t.Tip, t.Deger, t.TTL, t.Oncelik, t.Sira, ak, 0); err != nil {
			log.Printf("dns_template seed %s/%s: %v", t.Ad, t.Tip, err)
		}
	}
	return nil
}

// LoadTemplate: global (reseller_id=0) şablon satırları. Geriye-uyum sarmalayıcı.
func LoadTemplate(ctx context.Context, db *sql.DB) ([]TemplateRow, error) {
	return LoadTemplateFor(ctx, db, 0)
}

// LoadTemplateFor: reseller'in KENDI sablonu varsa onu, yoksa global (reseller_id=0)
// sablonu doner. Boylece her bayi kendi DNS sablonunu tanimlayabilir; tanimlamazsa
// sunucu geneli sablon kullanilir.
func LoadTemplateFor(ctx context.Context, db *sql.DB, resellerID int64) ([]TemplateRow, error) {
	if resellerID > 0 {
		var n int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_template WHERE reseller_id=?`, resellerID).Scan(&n)
		if n == 0 {
			resellerID = 0 // bayinin sablonu yok -> global
		}
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, ad, tip, deger, ttl, oncelik, sira, aktif FROM dns_template WHERE reseller_id=? ORDER BY sira, id`, resellerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TemplateRow, 0)
	for rows.Next() {
		var t TemplateRow
		var ak int
		if err := rows.Scan(&t.ID, &t.Ad, &t.Tip, &t.Deger, &t.TTL, &t.Oncelik, &t.Sira, &ak); err == nil {
			t.Aktif = ak == 1
			out = append(out, t)
		}
	}
	return out, nil
}

// LoadTemplateMeta: SOA + DKIM ayarlarını okur (yoksa makul default).
func LoadTemplateMeta(ctx context.Context, db *sql.DB) TemplateMeta {
	m := TemplateMeta{SOARefresh: 3600, SOARetry: 900, SOAExpire: 1209600, SOAMinimum: 3600, SOATTL: 3600, DKIMSelector: "default", DKIMAktif: true}
	var dk int
	_ = db.QueryRowContext(ctx,
		`SELECT soa_refresh, soa_retry, soa_expire, soa_minimum, soa_ttl, dkim_selector, dkim_aktif
		 FROM dns_template_meta WHERE id=1`).
		Scan(&m.SOARefresh, &m.SOARetry, &m.SOAExpire, &m.SOAMinimum, &m.SOATTL, &m.DKIMSelector, &dk)
	m.DKIMAktif = dk == 1
	if strings.TrimSpace(m.DKIMSelector) == "" {
		m.DKIMSelector = "default"
	}
	return m
}

// subst: placeholder değişimi.
func subst(s, alanAdi, ipv4, selector, dkim string) string {
	s = strings.ReplaceAll(s, "{DOMAIN}", alanAdi)
	s = strings.ReplaceAll(s, "{IP}", ipv4)
	s = strings.ReplaceAll(s, "{SELECTOR}", selector)
	s = strings.ReplaceAll(s, "{DKIM}", dkim)
	return s
}

// EnsureDKIM: domain için DKIM anahtar çifti üretir (varsa yeniden kullanır),
// public key'i DKIM TXT değeri olarak döner (v=DKIM1; k=rsa; p=...).
// Private key mail sunucusuyla (OpenDKIM) eşlensin diye /etc/opendkim varsa oraya da yazılır.
func EnsureDKIM(ctx context.Context, db *sql.DB, domainID int64, alanAdi, selector string) (string, error) {
	var priv, pub string
	err := db.QueryRowContext(ctx,
		`SELECT private_key, public_key FROM dkim_keys WHERE domain_id=? AND selector=?`,
		domainID, selector).Scan(&priv, &pub)
	if err == nil && pub != "" {
		syncOpenDKIM(alanAdi, selector, priv, pub)
		return dkimTXT(pub), nil
	}

	// yeni anahtar üret
	key, gerr := rsa.GenerateKey(rand.Reader, 2048)
	if gerr != nil {
		return "", gerr
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := "-----BEGIN RSA PRIVATE KEY-----\n" + chunk64(base64.StdEncoding.EncodeToString(privDER)) + "-----END RSA PRIVATE KEY-----\n"
	pubDER, perr := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if perr != nil {
		return "", perr
	}
	pubB64 := base64.StdEncoding.EncodeToString(pubDER)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO dkim_keys(domain_id, selector, private_key, public_key) VALUES(?,?,?,?)
		 ON DUPLICATE KEY UPDATE private_key=VALUES(private_key), public_key=VALUES(public_key)`,
		domainID, selector, privPEM, pubB64); err != nil {
		return "", err
	}
	syncOpenDKIM(alanAdi, selector, privPEM, pubB64)
	return dkimTXT(pubB64), nil
}

// dkimTXT: base64 public key'den DKIM TXT rdata üretir.
func dkimTXT(pubB64 string) string {
	return "v=DKIM1; k=rsa; p=" + pubB64
}

// chunk64: PEM gövdesini 64 karakterlik satırlara böler.
func chunk64(s string) string {
	var b strings.Builder
	for len(s) > 64 {
		b.WriteString(s[:64])
		b.WriteByte('\n')
		s = s[64:]
	}
	if s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}

// syncOpenDKIM: OpenDKIM kuruluysa private key'i standart yola yazar ve KeyTable/SigningTable'a ekler.
// Best-effort: OpenDKIM yoksa sessizce atlar (DNS TXT yayını yine de çalışır).
func syncOpenDKIM(alanAdi, selector, privPEM, pubB64 string) {
	base := "/etc/opendkim"
	if _, err := os.Stat(base); err != nil {
		return
	}
	keyDir := filepath.Join(base, "keys", alanAdi)
	if err := os.MkdirAll(keyDir, 0750); err != nil {
		return
	}
	privPath := filepath.Join(keyDir, selector+".private")
	_ = os.WriteFile(privPath, []byte(privPEM), 0600)
	txtPath := filepath.Join(keyDir, selector+".txt")
	_ = os.WriteFile(txtPath, []byte(selector+"._domainkey."+alanAdi+" IN TXT ( \""+dkimTXT(pubB64)+"\" )\n"), 0644)
	_, _ = exec.Command("chown", "-R", "opendkim:opendkim", keyDir).CombinedOutput()

	appendUnique(filepath.Join(base, "KeyTable"),
		selector+"._domainkey."+alanAdi+" "+alanAdi+":"+selector+":"+privPath)
	appendUnique(filepath.Join(base, "SigningTable"),
		"*@"+alanAdi+" "+selector+"._domainkey."+alanAdi)
	_ = exec.Command("systemctl", "reload", "opendkim").Run()
}

// appendUnique: satır dosyada yoksa ekler (idempotent).
func appendUnique(path, line string) {
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), strings.SplitN(line, " ", 2)[0]) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// GetTemplate: GET /dns-template (admin) — şablon satırları + meta.
func (h *Handlers) GetTemplate(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r)
	rows, err := LoadTemplateFor(r.Context(), h.DB, rid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kayitlar": rows,
		"meta":     LoadTemplateMeta(r.Context(), h.DB),
	})
}

// PutTemplate: PUT /dns-template (admin) — şablonu tümüyle değiştirir (replace-all) + meta güncelle.
func (h *Handlers) PutTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kayitlar []TemplateRow `json:"kayitlar"`
		Meta     TemplateMeta  `json:"meta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	rid := middleware.ResellerIDFrom(r)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM dns_template WHERE reseller_id=?`, rid); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i, t := range req.Kayitlar {
		t.Tip = strings.ToUpper(strings.TrimSpace(t.Tip))
		if t.Ad == "" || t.Tip == "" || strings.TrimSpace(t.Deger) == "" {
			continue
		}
		if !gecerliTip(t.Tip) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz DNS tipi: "+t.Tip)
			return
		}
		if t.TTL <= 0 {
			t.TTL = 3600
		}
		if t.Sira == 0 {
			t.Sira = (i + 1) * 10
		}
		t.Oncelik = oncelikNormalize(t.Tip, t.Oncelik)
		ak := 1
		if !t.Aktif {
			ak = 0
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO dns_template(ad,tip,deger,ttl,oncelik,sira,aktif,reseller_id) VALUES(?,?,?,?,?,?,?,?)`,
			t.Ad, t.Tip, t.Deger, t.TTL, t.Oncelik, t.Sira, ak, rid); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	m := req.Meta
	if strings.TrimSpace(m.DKIMSelector) == "" {
		m.DKIMSelector = "default"
	}
	if m.SOARefresh <= 0 {
		m.SOARefresh = 3600
	}
	if m.SOARetry <= 0 {
		m.SOARetry = 900
	}
	if m.SOAExpire <= 0 {
		m.SOAExpire = 1209600
	}
	if m.SOAMinimum <= 0 {
		m.SOAMinimum = 3600
	}
	if m.SOATTL <= 0 {
		m.SOATTL = 3600
	}
	dk := 1
	if !m.DKIMAktif {
		dk = 0
	}
	if _, err := tx.ExecContext(r.Context(),
		`INSERT INTO dns_template_meta(id, soa_refresh, soa_retry, soa_expire, soa_minimum, soa_ttl, dkim_selector, dkim_aktif)
		 VALUES(1,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE soa_refresh=VALUES(soa_refresh), soa_retry=VALUES(soa_retry),
		   soa_expire=VALUES(soa_expire), soa_minimum=VALUES(soa_minimum), soa_ttl=VALUES(soa_ttl),
		   dkim_selector=VALUES(dkim_selector), dkim_aktif=VALUES(dkim_aktif)`,
		m.SOARefresh, m.SOARetry, m.SOAExpire, m.SOAMinimum, m.SOATTL, m.DKIMSelector, dk); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
