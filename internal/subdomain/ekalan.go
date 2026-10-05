package subdomain

// ekalan.go — EK (addon) ALAN ADI: bir hosting hesabı (tek sistem kullanıcısı)
// altında birden fazla alan adı (cPanel modeli). Kullanıcı kararı: model = TEK
// SİSTEM KULLANICISI + EK DOMAIN; müşteri hesabı modeli DEĞİL.
//
// Her ek alan adı: kendi docroot'u (/home/<sk>/domains/<alan>/public_html), kendi
// nginx vhost'u (ek_<sk>_<alan>.conf; hesabın PHP havuzu), kendi SSL'i ve kendi DNS
// bölgesi (ek_dns_kayitlari) vardır. Alt alan adı (subdomanlar) modelinden bağımsızdır:
// ek alan adı kendi bölgesinin ÜST adıdır. vhost üreticisi alt alanla ORTAKTIR
// (renderSubVhost): ACME challenge konumu, güvenlik başlıkları, PHP bloğu tek yerde.
//
// KAPSAM (v1): e-posta ek alan adı için KAPALI (DNS şablonundan MX/SPF/DKIM alınmaz,
// posta ana makineleri yoktur); alt alan adı oluşturma yalnız ana alan adı altındadır;
// ek alan adının PHP sürümü hesabınkidir (ayrı havuz yok).
//
// LİMİT: plan max_domain = hesapta toplam alan adı (ana + ek); 0 = sınırsız. Kapı
// sunucudadır (Olustur): sayım + ekleme domains satırı FOR UPDATE kilidiyle tek
// işlemde, ad-alanı denetimiyle de süreç genelinde sıralanır (yarış güvenli).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"girginospanel/internal/dns"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/provisioner"

	"github.com/go-chi/chi/v5"
)

// AdAlaniEkle — ad-alanı (takeover) denetimi + ekleme, süreç genelinde kilitli.
// Üretimde domains.AdAlaniEkle bağlanır (cmd/server/ek_alan_kablo.go): subdomain,
// domains'i import edemez (domains → subdomain). nil = YAPILANDIRILMAMIŞ → ekleme
// REDDEDİLİR (fail-closed).
var AdAlaniEkle func(ctx context.Context, db *sql.DB, ad string, kiraci int64, ekle func() error) error

// Dış etkili adımlar (testler sahteyle değiştirir).
var (
	ekZoneTohum = dns.EkSeed
	ekZoneYaz   = dns.WriteEkZone
	ekZoneSil   = dns.DeleteEkZone
)

// EkAlan — API gösterimi.
type EkAlan struct {
	ID        int64  `json:"id"`
	AlanAdi   string `json:"alan_adi"`
	DocRoot   string `json:"docroot"`
	PHPSurum  string `json:"php_surum"`
	SSLAktif  bool   `json:"ssl_aktif"`
	SSLBitis  string `json:"ssl_bitis"`
	Durum     string `json:"durum"` // aktif | askida
	CreatedAt string `json:"created_at"`
}

type ekLimit struct {
	AlanAdi     int    `json:"alan_adi"` // paket limiti (ana dahil toplam), 0 = sınırsız
	Kullanim    int    `json:"kullanim"` // 1 + ek alan adı sayısı
	Eklenebilir bool   `json:"eklenebilir"`
	Neden       string `json:"neden"`
}

type ekListeYanit struct {
	Ekler    []EkAlan `json:"ekler"`
	Limit    ekLimit  `json:"limit"`
	AltLimit struct {
		Limit    int `json:"limit"` // alt alan adı için ayrı paket limiti YOK → 0 (sınırsız)
		Kullanim int `json:"kullanim"`
	} `json:"alt_limit"`
	IP string `json:"ip"`
}

// ekLimitDurumu — saf: paket limiti + mevcut ek sayısından ekleme hakkı. limit 0 =
// sınırsız; ana alan adı 1 sayılır → limit 1 ise ek alan adı EKLENEMEZ.
func ekLimitDurumu(limit, ekSayisi int, demo bool) ekLimit {
	l := ekLimit{AlanAdi: limit, Kullanim: 1 + ekSayisi, Eklenebilir: true}
	switch {
	case demo:
		l.Eklenebilir, l.Neden = false, "demo aboneliğinde kullanılamaz"
	case limit > 0 && l.Kullanim >= limit:
		l.Eklenebilir = false
		l.Neden = fmt.Sprintf("Paketinizde alan adı hakkı kalmadı (%d/%d kullanılıyor)", l.Kullanim, limit)
	}
	return l
}

// ekLimitHatasi — sunucu limit kapısı (409).
type ekLimitHatasi struct{ mesaj string }

func (e *ekLimitHatasi) Error() string { return e.mesaj }

// ekSatir — bir ek alan adı + sahibi hesap.
type ekSatir struct {
	ID, DomainID, Kiraci   int64
	Ad, PHP, SK, ParentPHP string
	IPv4                   string
	Demo                   bool
	SSLBitisDB             sql.NullString
	Olusturma              string
}

func (h *Handlers) ekParentSorgu(ctx context.Context, id int64) (ekSatir, error) {
	var s ekSatir
	var demo int
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, sistem_kullanici, COALESCE(php_surum,'8.3'), COALESCE(is_demo,0), COALESCE(reseller_id,0), COALESCE(ipv4,'')
		   FROM domains WHERE id=?`, id).Scan(&s.DomainID, &s.SK, &s.ParentPHP, &demo, &s.Kiraci, &s.IPv4)
	s.Demo = demo == 1
	s.PHP = s.ParentPHP
	return s, err
}

// ekYukle — URL'deki {id}/{eid}: ek alan adı VE {id} hesabına ait olmalı (IDOR kapısı:
// MusteriScope yalnız {id}'yi doğrular).
func (h *Handlers) ekYukle(ctx context.Context, did, eid int64) (ekSatir, error) {
	var s ekSatir
	var demo int
	err := h.DB.QueryRowContext(ctx,
		`SELECT e.id, e.domain_id, e.alan_adi, COALESCE(d.php_surum,'8.3'), d.sistem_kullanici, COALESCE(d.php_surum,'8.3'),
		        COALESCE(d.is_demo,0), COALESCE(d.reseller_id,0), COALESCE(d.ipv4,''),
		        DATE_FORMAT(e.ssl_bitis,'%Y-%m-%d'), DATE_FORMAT(e.created_at,'%Y-%m-%d %H:%i')
		   FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id
		  WHERE e.id=? AND e.domain_id=?`, eid, did).
		Scan(&s.ID, &s.DomainID, &s.Ad, &s.PHP, &s.SK, &s.ParentPHP, &demo, &s.Kiraci, &s.IPv4, &s.SSLBitisDB, &s.Olusturma)
	s.Demo = demo == 1
	return s, err
}

func urlID(r *http.Request, ad string) int64 {
	v, _ := strconv.ParseInt(chi.URLParam(r, ad), 10, 64)
	return v
}

func (h *Handlers) ekGoster(ctx context.Context, s ekSatir, askida bool) EkAlan {
	crt, key := certYolu(s.SK, s.Ad)
	aktif := dosyaVar(crt) && dosyaVar(key)
	bitis := ""
	if aktif {
		bitis = certBitis(crt)
	}
	if bitis == "" && aktif && s.SSLBitisDB.Valid {
		bitis = s.SSLBitisDB.String
	}
	durum := "aktif"
	if askida {
		durum = "askida"
	}
	return EkAlan{ID: s.ID, AlanAdi: s.Ad, DocRoot: ekDocroot(s.SK, s.Ad), PHPSurum: s.ParentPHP,
		SSLAktif: aktif, SSLBitis: bitis, Durum: durum, CreatedAt: s.Olusturma}
}

func (h *Handlers) ekIP(s ekSatir) string {
	if s.IPv4 != "" {
		return s.IPv4
	}
	return h.IPv4
}

// efektifMaxDomain — hostingin efektif max_domain'i (plan ⊕ üst yazım; 0 = sınırsız).
func efektifMaxDomain(ctx context.Context, q efektiflimit.Sorgulayici, did int64) (int, error) {
	el, err := efektiflimit.Oku(ctx, q, did)
	if err != nil {
		return 0, err
	}
	return el.Efektif("max_domain", 0), nil
}

// GET /domains/{id}/ek-alan-adlari
func (h *Handlers) EkListe(w http.ResponseWriter, r *http.Request) {
	id := urlID(r, "id")
	p, err := h.ekParentSorgu(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, alan_adi, DATE_FORMAT(ssl_bitis,'%Y-%m-%d'), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		   FROM ek_alan_adlari WHERE domain_id=? ORDER BY alan_adi`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	askida, _ := hesaplar.HesapAskida(r.Context(), h.DB, p.SK)
	out := ekListeYanit{Ekler: []EkAlan{}}
	for rows.Next() {
		s := p
		if err := rows.Scan(&s.ID, &s.Ad, &s.SSLBitisDB, &s.Olusturma); err == nil {
			out.Ekler = append(out.Ekler, h.ekGoster(r.Context(), s, askida))
		}
	}
	if err := rows.Err(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	limit, lerr := efektifMaxDomain(r.Context(), h.DB, id)
	if lerr != nil {
		log.Printf("ek alan: efektif limit okunamadı (domain=%d): %v", id, lerr)
		httpx.WriteError(w, http.StatusInternalServerError, "paket limiti okunamadı")
		return
	}
	out.Limit = ekLimitDurumu(limit, len(out.Ekler), p.Demo)
	var altN int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subdomanlar WHERE domain_id=?`, id).Scan(&altN)
	out.AltLimit.Kullanim = altN
	out.IP = h.ekIP(p)
	httpx.WriteJSON(w, http.StatusOK, out)
}

// GET /domains/{id}/ek-alan-adlari/{eid}
func (h *Handlers) EkDetay(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	askida, _ := hesaplar.HesapAskida(r.Context(), h.DB, s.SK)
	g := h.ekGoster(r.Context(), s, askida)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": g.ID, "alan_adi": g.AlanAdi, "docroot": g.DocRoot, "php_surum": g.PHPSurum,
		"ssl_aktif": g.SSLAktif, "ssl_bitis": g.SSLBitis, "durum": g.Durum, "created_at": g.CreatedAt, "ip": h.ekIP(s),
	})
}

// kaydi sınıflayan arayüz: domains.AdAlaniHatasi (subdomain import edemez).
type mesajliHata interface{ Mesaj(yonetici bool) string }

type ekOlusturReq struct {
	AlanAdi  string `json:"alan_adi"`
	PHPSurum string `json:"php_surum"`
	SSL      string `json:"ssl"` // "" | self-signed | letsencrypt
}

// POST /domains/{id}/ek-alan-adlari
func (h *Handlers) EkOlustur(w http.ResponseWriter, r *http.Request) {
	id := urlID(r, "id")
	p, err := h.ekParentSorgu(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if p.Demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if !reEkSK.MatchString(p.SK) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı")
		return
	}
	var req ekOlusturReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	ad, err := ekAlanAdiDogrula(req.AlanAdi)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	sslTip := strings.ToLower(strings.TrimSpace(req.SSL))
	if sslTip == "le" {
		sslTip = "letsencrypt"
	}
	if sslTip != "" && sslTip != "self-signed" && sslTip != "letsencrypt" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz ssl tipi (self-signed|letsencrypt)")
		return
	}
	// PHP sürümü hesabınkidir (ayrı havuz yok): başka sürüm istenirse açıkça reddedilir.
	if v := strings.TrimSpace(req.PHPSurum); v != "" && v != p.ParentPHP {
		httpx.WriteError(w, http.StatusBadRequest, "ek alan adı hesabın PHP sürümünü ("+p.ParentPHP+") kullanır")
		return
	}
	if _, err := provisioner.PHPSocketFor(p.SK, p.ParentPHP); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PHP sürümü sunucuda kurulu değil: "+p.ParentPHP)
		return
	}
	if AdAlaniEkle == nil {
		log.Printf("ek alan: ad-alanı denetimi bağlanmamış — ekleme reddedildi")
		httpx.WriteError(w, http.StatusInternalServerError, "alan adı denetimi yapılandırılmamış")
		return
	}

	// Yan etki zinciri istemci kopsa da tamamlanır (yarım ek alan adı kalmaz).
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()

	// 1) Ad-alanı denetimi + LİMİT + satır ekleme: süreç genelinde sıralı, tek işlem.
	var eid int64
	err = AdAlaniEkle(kctx, h.DB, ad, p.Kiraci, func() error {
		// HESAP düzeyi ad-alanı denetimi (bayi düzeyindeki denetimi sıkılaştırır).
		if e := ekHesapAdAlaniDenetle(kctx, h.DB, id, ad); e != nil {
			return e
		}
		var e error
		eid, e = h.ekSatirEkle(kctx, id, ad, p.ParentPHP)
		return e
	})
	if err != nil {
		var lh *ekLimitHatasi
		var mh mesajliHata
		switch {
		case errors.As(err, &lh):
			httpx.WriteError(w, http.StatusConflict, lh.mesaj)
		case errors.As(err, &mh):
			httpx.WriteError(w, http.StatusConflict, mh.Mesaj(middleware.RolFrom(r) == "admin"))
		default:
			log.Printf("ek alan ekleme (domain=%d ad=%s): %v", id, ad, err)
			httpx.WriteError(w, http.StatusInternalServerError, "ek alan adı eklenemedi")
		}
		return
	}

	// 2) docroot + vhost (nginx -t başarısızsa TÜM değişiklik geri alınır).
	docroot, yeni, derr := ekDocrootHazirla(p.SK, ad)
	if derr != nil {
		h.ekGeriAl(kctx, eid, p.SK, ad, false)
		log.Printf("ek alan docroot (%s): %v", ad, derr)
		httpx.WriteError(w, http.StatusInternalServerError, "docroot oluşturulamadı")
		return
	}
	if err := ekVhostYenidenKur(kctx, h.DB, eid); err != nil {
		h.ekGeriAl(kctx, eid, p.SK, ad, yeni)
		httpx.WriteError(w, http.StatusInternalServerError, "vhost yazılamadı: "+nginxset.SunucuHatasi(r, "ekalan.olustur", id, err))
		return
	}

	// 3) DNS bölgesi: hata ölümcül DEĞİL (site çalışır; açılış onarımı yeniden dener).
	uyarilar := []string{}
	if _, err := ekZoneTohum(kctx, h.DB, eid, ad, h.ekIP(p), p.Kiraci); err != nil {
		log.Printf("ek alan %s: DNS kayıtları üretilemedi: %v", ad, err)
		uyarilar = append(uyarilar, "DNS kayıtları oluşturulamadı")
	} else if err := ekZoneYaz(kctx, h.DB, eid); err != nil {
		log.Printf("ek alan %s: bölge dosyası yazılamadı: %v", ad, err)
		uyarilar = append(uyarilar, "DNS bölgesi yazılamadı (sunucu yeniden başlayınca yeniden denenir)")
	}

	// 4) İsteğe bağlı SSL (DNS yayılmadan Let's Encrypt başarısız olur: ölümcül değil).
	resp := map[string]any{"ok": true, "id": eid, "alan_adi": ad, "docroot": docroot}
	if sslTip != "" {
		hata, _ := h.ekSSLUygula(kctx, r, ekSatir{ID: eid, DomainID: id, Ad: ad, SK: p.SK, ParentPHP: p.ParentPHP, Kiraci: p.Kiraci}, sslTip)
		s := map[string]any{"tip": sslTip, "ok": hata == ""}
		if hata != "" {
			s["hata"] = hata
		}
		resp["ssl"] = s
	}
	if len(uyarilar) > 0 {
		resp["uyarilar"] = uyarilar
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "ek_alan.ekle", ad, "hesap="+p.SK, p.Kiraci, true)
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ekSatirEkle — limit kapısı + INSERT, domains satırı kilidi altında tek işlemde.
func (h *Handlers) ekSatirEkle(ctx context.Context, did int64, ad, php string) (int64, error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// Aynı hesaba eşzamanlı eklemeleri sıralar (limit yarışı).
	var kilit int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM domains WHERE id=? FOR UPDATE`, did).Scan(&kilit); err != nil {
		return 0, err
	}
	limit, err := efektifMaxDomain(ctx, tx, did)
	if err != nil {
		return 0, fmt.Errorf("paket limiti okunamadı: %w", err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ek_alan_adlari WHERE domain_id=?`, did).Scan(&n); err != nil {
		return 0, err
	}
	if d := ekLimitDurumu(limit, n, false); !d.Eklenebilir {
		return 0, &ekLimitHatasi{d.Neden}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO ek_alan_adlari (domain_id, alan_adi, php_surum) VALUES (?,?,?)`, did, ad, php)
	if err != nil {
		return 0, err
	}
	eid, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eid, nil
}

// ekGeriAl — oluşturma yarıda kalınca: satır (+DNS CASCADE), vhost ve (bu istekte
// oluşturulduysa) docroot kaldırılır.
func (h *Handlers) ekGeriAl(ctx context.Context, eid int64, sk, ad string, docrootYeni bool) {
	_ = os.Remove(ekConfPath(sk, ad))
	_, _ = komut("systemctl", "reload", "nginx")
	_, _ = h.DB.ExecContext(context.WithoutCancel(ctx), `DELETE FROM ek_alan_adlari WHERE id=?`, eid)
	if docrootYeni {
		ekAlanDizininiSil(sk, ad)
	}
}

// ekDocrootHazirla — docroot'u güvenle oluşturur. yeni = bu çağrıda oluşturuldu.
// Kiracının sembolik bağlantısıyla hesap dışına çıkış reddedilir (kök işlemleri:
// MkdirAll/chown/chcon yalnız doğrulanmış yolda).
func ekDocrootHazirla(sk, ad string) (docroot string, yeni bool, err error) {
	if !reEkSK.MatchString(sk) || !ekAdGuvenli(ad) {
		return "", false, errors.New("geçersiz ad")
	}
	taban := ekDocrootTaban(sk)
	if fi, e := os.Lstat(taban); e == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", false, errors.New(ekAlanKlasoru + " dizini sembolik bağlantı")
	}
	docroot = ekDocroot(sk, ad)
	if err := ekYolGuvenli(sk, docroot); err != nil {
		return "", false, err
	}
	_, statErr := os.Stat(docroot)
	yeni = statErr != nil
	if err := os.MkdirAll(docroot, 0o755); err != nil {
		return "", false, err
	}
	if err := ekYolGuvenli(sk, docroot); err != nil { // MkdirAll sonrası yeniden
		return "", false, err
	}
	if _, e := os.Stat(docroot + "/index.html"); e != nil {
		_ = os.WriteFile(docroot+"/index.html", []byte(provisioner.WelcomeHTML(ad)), 0o644)
	}
	// -h: sembolik bağlantının KENDİSİNİ chown'la, hedefini değil.
	_, _ = komut("chown", "-R", "-h", sk+":"+sk, taban)
	_, _ = komut("chcon", "-R", "-t", "httpd_sys_content_t", ekDocrootTaban(sk)+"/"+ad)
	return docroot, yeni, nil
}

// ekAlanDizininiSil — <ev>/<sk>/domains/<alan> dizinini, yol hesap dizini altında
// kaldığı doğrulanırsa siler.
func ekAlanDizininiSil(sk, ad string) {
	if !reEkSK.MatchString(sk) || !ekAdGuvenli(ad) {
		return
	}
	dizin := ekDocrootTaban(sk) + "/" + ad
	if err := ekYolGuvenli(sk, dizin); err != nil {
		log.Printf("ek alan sil: docroot atlandı (%s): %v", ad, err)
		return
	}
	_ = os.RemoveAll(dizin)
}

// ekAcmeSil — acme.sh kayıtlarını ve sertifika dosyalarını kaldırır.
func ekAcmeSil(sk, ad string) {
	if !ekAdGuvenli(ad) {
		return
	}
	_, _ = komut("/root/.acme.sh/acme.sh", "--remove", "--ecc", "--config-home", provisioner.AcmeConfigHome(), "-d", ad)
	crt, key := certYolu(sk, ad)
	_ = os.Remove(crt)
	_ = os.Remove(key)
	for _, b := range []string{provisioner.AcmeConfigHome(), "/root/.acme.sh"} {
		if b == "" {
			continue
		}
		_ = os.RemoveAll(b + "/" + ad + "_ecc")
		_ = os.RemoveAll(b + "/" + ad)
	}
}

// DELETE /domains/{id}/ek-alan-adlari/{eid}?onay=<alan_adi>
func (h *Handlers) EkSil(w http.ResponseWriter, r *http.Request) {
	id := urlID(r, "id")
	s, err := h.ekYukle(r.Context(), id, urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	if s.Demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	if kod, msg := ekSilmeOnayi(r, s.Ad); kod != 0 {
		httpx.WriteError(w, kod, msg)
		return
	}
	if !reEkSK.MatchString(s.SK) || !ekAdGuvenli(s.Ad) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz kayıt")
		return
	}
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	ekDosyalariniKaldir(kctx, s.SK, s.Ad)
	if _, err := h.DB.ExecContext(kctx, `DELETE FROM ek_alan_adlari WHERE id=? AND domain_id=?`, s.ID, id); err != nil {
		log.Printf("ek alan sil: DB satırı silinemedi (eid=%d domain=%d): %v — vhost/docroot GİTTİ ama kayıt kaldı", s.ID, id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "ek alan adı kaydı silinemedi")
		return
	}
	// Bölge dosyası satırlar silindikten SONRA (include DB'den üretilir).
	if err := ekZoneSil(kctx, h.DB, s.Ad); err != nil {
		log.Printf("ek alan sil: DNS bölgesi kaldırılamadı (%s): %v", s.Ad, err)
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "ek_alan.sil", s.Ad, "hesap="+s.SK, s.Kiraci, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ekSilmeOnayi — domains.silmeOnayi ile aynı kural: yönetici dışında alan adını
// yazarak onay ZORUNLU; verilirse herkes için eşleşmeli.
func ekSilmeOnayi(r *http.Request, ad string) (int, string) {
	onay := strings.TrimSpace(r.URL.Query().Get("onay"))
	if onay == "" {
		if middleware.RolFrom(r) == "admin" {
			return 0, ""
		}
		return http.StatusBadRequest, "silme onayı gerekli: alan adını yazın"
	}
	if !strings.EqualFold(onay, ad) {
		return http.StatusBadRequest, "onay metni alan adıyla eşleşmiyor"
	}
	return 0, ""
}

// ekDosyalariniKaldir — vhost (render kilidi altında), acme/sertifika, günlükler,
// docroot. DB satırına ve bölgeye dokunmaz.
func ekDosyalariniKaldir(ctx context.Context, sk, ad string) {
	func() {
		birak := provisioner.RenderKilidiAl()
		defer birak()
		_ = os.Remove(ekConfPath(sk, ad))
		_, _ = komut("systemctl", "reload", "nginx")
	}()
	ekAcmeSil(sk, ad)
	for _, tur := range []string{".access.log", ".error.log"} {
		for _, f := range ekGunlukler("/var/log/nginx", ad+tur) {
			_ = os.Remove(f)
		}
	}
	ekAlanDizininiSil(sk, ad)
}

// GET /domains/{id}/ek-alan-adlari/{eid}/dns — salt okunur.
func (h *Handlers) EkDNS(w http.ResponseWriter, r *http.Request) {
	s, err := h.ekYukle(r.Context(), urlID(r, "id"), urlID(r, "eid"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "ek alan adı bulunamadı")
		return
	}
	kayitlar, err := dns.EkKayitlari(r.Context(), h.DB, s.ID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DNS kayıtları okunamadı")
		return
	}
	type k struct {
		Ad      string `json:"ad"`
		Tip     string `json:"tip"`
		Deger   string `json:"deger"`
		TTL     int    `json:"ttl"`
		Oncelik int    `json:"oncelik"`
	}
	out := make([]k, 0, len(kayitlar))
	for _, x := range kayitlar {
		out = append(out, k{x.Ad, x.Tip, x.Deger, x.TTL, x.Oncelik})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// EkAlanlariTemizle — HESAP silinirken: hesabın tüm ek alan adlarının vhost, sertifika
// (acme kaydı dahil), günlük ve bölge kalıntılarını kaldırır ve satırları siler.
// (docroot'lar ev dizinidir: userdel -r siler.) domains.Delete, provisioner.Deprovision'dan
// ÖNCE çağırır (nginx reload orada).
func EkAlanlariTemizle(ctx context.Context, db *sql.DB, domainID int64, sk string) {
	adlar, err := ekAdlari(ctx, db, domainID)
	if err != nil || len(adlar) == 0 {
		if err != nil {
			log.Printf("ek alan temizlik (domain=%d): liste okunamadı: %v", domainID, err)
		}
		return
	}
	for _, ad := range adlar {
		ekDosyalariniKaldir(ctx, sk, ad)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM ek_alan_adlari WHERE domain_id=?`, domainID); err != nil {
		log.Printf("ek alan temizlik (domain=%d): satırlar silinemedi: %v", domainID, err)
	}
	for _, ad := range adlar {
		if err := ekZoneSil(ctx, db, ad); err != nil {
			log.Printf("ek alan temizlik: bölge kaldırılamadı (%s): %v", ad, err)
		}
	}
}

// ekAdlari — hesabın ek alan adları (tablo yoksa boş).
func ekAdlari(ctx context.Context, db *sql.DB, domainID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT alan_adi FROM ek_alan_adlari WHERE domain_id=? ORDER BY id`, domainID)
	if err != nil {
		if tabloYok(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if rows.Scan(&a) == nil {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

func tabloYok(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "1146") || strings.Contains(m, "doesn't exist")
}
