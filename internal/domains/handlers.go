package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/backups"
	destekiptal "girginospanel/internal/destek/iptal"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/dns"
	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/kilit"
	"girginospanel/internal/kota"
	"girginospanel/internal/middleware"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/plans"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/subdomain"

	"github.com/go-chi/chi/v5"
)

type Domain struct {
	ID              int64  `json:"id"`
	AlanAdi         string `json:"alan_adi"`
	PHPSurum        string `json:"php_surum"`
	SSL             bool   `json:"ssl"`
	SSLBitis        string `json:"ssl_bitis,omitempty"`
	Durum           string `json:"durum"`
	SistemKullanici string `json:"sistem_kullanici"`
	BoyutKB         int64  `json:"boyut_kb"`
	TrafikKB        int64  `json:"trafik_kb"`
	Olusturulma     string `json:"olusturulma"`
	IPv4            string `json:"ipv4"`
	FTPHost         string `json:"ftp_host"`
	FTPUser         string `json:"ftp_user"`
	DBHost          string `json:"db_host"`
	DBUser          string `json:"db_user"`
	DBAdi           string `json:"db_adi"`
	WebRoot         string `json:"web_root"`
	IsDemo          bool   `json:"is_demo"`
	Notlar          string `json:"notlar,omitempty"`
	PlanID          *int64 `json:"plan_id,omitempty"`
	PlanAd          string `json:"plan_ad,omitempty"`
	SshErisim       bool   `json:"ssh_erisim"`
	Askida          bool   `json:"askida"`
	// Sahip: domaini kimin actigi. reseller_id=0 -> panel sahibi (admin), >0 -> bayi.
	SahipAd    string `json:"sahip_ad,omitempty"`
	SahipTur   string `json:"sahip_tur,omitempty"` // "admin" | "bayi"
	ResellerID int64  `json:"reseller_id"`
	// DiskKotaMB: EFEKTİF disk kotası (plan ⊕ hostinge özel üst yazım; 0 = sınırsız /
	// plansız). Bayi listesi "disk %" gösterir.
	DiskKotaMB int64 `json:"disk_kota_mb"`
	// TrafikKotaMB: bağlı planın aylık trafik kotası (0 = sınırsız / plansız).
	// Bayi listesi trafik kolonunda "kullanılan / kota" gösterir.
	TrafikKotaMB int64 `json:"trafik_kota_mb"`
	// OzelLimit: hostinge özel limit (üst yazım) var — listede "<plan> + özel" rozeti.
	// Müşteriye gönderilmez (musteriGorunumu siler).
	OzelLimit bool `json:"ozel_limit,omitempty"`
}

type Handlers struct {
	DB   *sql.DB
	IPv4 string
}

var selectAll = `SELECT d.id, d.alan_adi, d.sistem_kullanici, d.php_surum, d.ssl_aktif,
  COALESCE(DATE_FORMAT(d.ssl_bitis,'%Y-%m-%d'),''), d.durum, d.ipv4, d.ftp_host, d.ftp_user,
  d.db_host, d.db_user, d.db_adi, d.web_root, d.boyut_kb, d.trafik_kb, d.is_demo,
  COALESCE(d.notlar,''), DATE_FORMAT(d.olusturulma,'%Y-%m-%d'),
  d.plan_id, COALESCE(p.ad,''), d.ssh_erisim, COALESCE(d.askida,0),
  CASE WHEN CHAR_LENGTH(TRIM(COALESCE(us.full_name,'')))>=2 THEN TRIM(us.full_name) ELSE COALESCE(NULLIF(us.username,''),'') END, COALESCE(d.reseller_id,0),
  ` + efektiflimit.IfadeVarsayilan("disk_kota_mb", 0) + `, ` + efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0) + `, ` + efektiflimit.OzelVarIfade + `
  FROM domains d` + efektiflimit.Join + `
  LEFT JOIN users us ON us.id = d.reseller_id`

// kokAdi: panel sahibinin gorunen adi (users'taki ilk admin). Onbellege alinir —
// domain listesinde satir basina sorgu atmanin anlami yok.
var (
	kokAdiOnce sync.Once
	kokAdiVal  string
	KokDB      *sql.DB // main() tarafindan set edilir
)

func kokAdi() string {
	kokAdiOnce.Do(func() {
		if KokDB == nil {
			return
		}
		_ = KokDB.QueryRow(
			`SELECT COALESCE(NULLIF(full_name,''), username, '') FROM users WHERE role='admin' ORDER BY id LIMIT 1`).
			Scan(&kokAdiVal)
	})
	return kokAdiVal
}

func scan(rs interface{ Scan(...any) error }) (Domain, error) {
	var d Domain
	var ssl, demo, sshE, askida, ozel int
	var planID sql.NullInt64
	err := rs.Scan(&d.ID, &d.AlanAdi, &d.SistemKullanici, &d.PHPSurum, &ssl,
		&d.SSLBitis, &d.Durum, &d.IPv4, &d.FTPHost, &d.FTPUser,
		&d.DBHost, &d.DBUser, &d.DBAdi, &d.WebRoot, &d.BoyutKB, &d.TrafikKB, &demo,
		&d.Notlar, &d.Olusturulma,
		&planID, &d.PlanAd, &sshE, &askida,
		&d.SahipAd, &d.ResellerID, &d.DiskKotaMB, &d.TrafikKotaMB, &ozel)
	d.OzelLimit = ozel == 1
	// Kok (reseller_id=0): users tablosunda admin satiri OLMAYABILIR (kurulum
	// varyanti) — sahip adini sorguya baglamak yerine burada cozuyoruz.
	d.SahipTur = "admin"
	if d.ResellerID > 0 {
		d.SahipTur = "bayi"
		if d.SahipAd == "" {
			d.SahipAd = "#" + strconv.FormatInt(d.ResellerID, 10) // silinmis bayi
		}
	} else if d.SahipAd = kokAdi(); d.SahipAd == "" {
		d.SahipAd = "Panel Sahibi"
	}
	d.SSL = ssl == 1
	d.IsDemo = demo == 1
	d.SshErisim = sshE == 1
	d.Askida = askida == 1
	if planID.Valid {
		v := planID.Int64
		d.PlanID = &v
	}
	return d, err
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	// Reseller kapsami: yalniz kendi hosting hesaplari. Admin: hepsi.
	sorgu := selectAll + " ORDER BY d.id DESC"
	args := []any{}
	if rid := middleware.ResellerIDFrom(r); rid > 0 {
		sorgu = selectAll + " WHERE d.reseller_id=? ORDER BY d.id DESC"
		args = append(args, rid)
	}
	rows, err := h.DB.QueryContext(r.Context(), sorgu, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı hatası: "+err.Error())
		return
	}
	defer rows.Close()
	out := make([]Domain, 0)
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
			return
		}
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE d.id=?", id)
	d, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if middleware.MusteriClaimsFrom(r) != nil {
		httpx.WriteJSON(w, http.StatusOK, musteriGorunumu(d))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

// musteriDomain — müşteri oturumunda GET /domains/{id} yanıtı: Domain'in aynısı,
// yalnız sahiplik alanları (sahip_ad, sahip_tur, reseller_id) JSON'da HİÇ yok.
// Aynı JSON adlı sığ alanlar gömülü Domain'dekileri gölgeler; nil + omitempty →
// anahtar yazılmaz.
//
// 🔴 NEDEN (B8): müşteri, yöneticinin ya da bayisinin adını/giriş adını (kokAdi,
// users.full_name/username) ve bayi kimliğini öğreniyordu. Müşteri ön yüzü bu
// alanları kullanmıyor (yalnız yönetici/bayi DomainsPage'i, o da /domains
// listesinden). Yönetici ve bayi yanıtı değişmez.
type musteriDomain struct {
	Domain
	SahipAd    *struct{} `json:"sahip_ad,omitempty"`
	SahipTur   *struct{} `json:"sahip_tur,omitempty"`
	ResellerID *struct{} `json:"reseller_id,omitempty"`
}

func musteriGorunumu(d Domain) musteriDomain {
	// Gölgelemeye ek olarak değerler de silinir: etiketler ileride değişse bile sızmaz.
	d.SahipAd, d.SahipTur, d.ResellerID = "", "", 0
	d.OzelLimit = false // müşteri yalnız efektif değerleri görür (rozet yok)
	return musteriDomain{Domain: d}
}

type createReq struct {
	AlanAdi    string `json:"alan_adi"`
	PHPSurum   string `json:"php_surum"`
	CustomerID *int64 `json:"customer_id,omitempty"`
	PlanID     *int64 `json:"plan_id,omitempty"`
	SqlOlustur bool   `json:"sql_olustur,omitempty"` // varsayılan false: DB otomatik açılmaz
	// YedekleriDevral: AÇIK bayrak (varsayılan false). true ise hosting başarıyla oluştuktan sonra aynı alan adı +
	// aynı sahip + aynı sistem kullanıcısıyla silinmiş bir hesabın yetim yedekleri bu hostinge bağlanır
	// (backups.YetimleriDevral). Bu uca yalnız yönetici/bayi/API anahtarı erişir; müşteri akışı yedek devralmaz.
	YedekleriDevral bool `json:"yedekleri_devral,omitempty"`
	// BayiID: yalnız KÖK — hostingi bu bayi ADINA aç (WHMCS bayi ürünü: bayinin kendi alan adı).
	// Hosting bayiye ait olur; bayinin kotası, plan kapsamı ve ad-alanı kuralları bayi kendisi
	// açıyormuş gibi uygulanır.
	BayiID *int64 `json:"bayi_id,omitempty"`
}

type createResp struct {
	Domain
	OluşturulanParolalar struct {
		FTP string `json:"ftp"`
		DB  string `json:"db"`
	} `json:"olusturulan_parolalar"`
	// DevralinanYedek: devralınan yetim yedek sayısı. YALNIZ istekte `yedekleri_devral:true` varsa yanıtta bulunur
	// (devralma hata verirse 0); bayrak yoksa alan hiç yazılmaz.
	DevralinanYedek *int64 `json:"devralinan_yedek,omitempty"`
}

// resellerKotaKontrol: reseller'in toplam kotalarini (max_domain / max_disk_mb)
// yeni hosting olusturmadan ONCE dogrular. 0 = limitsiz.
// Disk: mevcut hosting'lerin PLAN disk kotalari toplami + yeni planin kotasi
// (taahhut-bazli; asiri-satisi onler).
// bayiKilitleri: kota kontrolu ile INSERT arasindaki TOCTOU yarisini kapatir.
// Iki paralel istek ayni bayide COUNT okuyup ikisi de gecerse limit asiliyordu
// (max_domain=1 iken 2 hosting acildi). Panel tek-node oldugu icin surec-ici
// kilit yeterli; kilit bayi BAZINDA, farkli bayiler birbirini beklemez.
// bayiKilidi: ortak kilit paketine devreder (aski zinciri/plan degisimi/silme
// ayni kilidi kullanir → islemler birbirini beklemek zorunda).
func bayiKilidi(rid int64) *sync.Mutex { return kilit.Bayi(rid) }

func (h *Handlers) resellerKotaKontrol(ctx context.Context, rid int64, planID *int64) error {
	var maxDomain int
	var maxDiskMB int64
	var durum string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_domain, max_disk_mb, status FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDomain, &maxDiskMB, &durum); err != nil {
		return fmt.Errorf("bayi kaydı bulunamadı")
	}
	// 🔴 Askiya alinmis bayi hosting ACAMAZ. Aksi halde aski islemiyle es zamanli
	// gelen istek, askidan MUAF (askida=0) canli bir site dogurup kaliyordu.
	if durum != "active" {
		return fmt.Errorf("bayi hesabı askıda — yeni hosting açılamaz")
	}
	// Negatif limit "sinirsiz" sayilmasin (eski kayitlar/elle mudahale).
	if maxDomain < 0 {
		maxDomain = 0
	}
	if maxDiskMB < 0 {
		maxDiskMB = 0
	}
	if maxDomain > 0 {
		var adet int
		_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE reseller_id=?`, rid).Scan(&adet)
		if adet >= maxDomain {
			return fmt.Errorf("hosting hesabı limitiniz dolu (%d/%d)", adet, maxDomain)
		}
	}
	// 🔴 Fazla satma ilkesi: bayi "fazla satisa izinli" ise TAAHHUT (plan kotalari
	// toplami) kontrolu ATLANIR — Plesk "oversell allowed" davranisi. Fiili kullanim
	// kapisi (asagida) her durumda calisir; boylece sunucu yine korunur.
	var fazlaSatis int
	_ = h.DB.QueryRowContext(ctx, `SELECT fazla_satis FROM users WHERE id=? AND role='reseller'`, rid).Scan(&fazlaSatis)

	// Trafik havuzu (taahhut): mevcut planlarin trafik kotalari + yeni plan
	if planID != nil && fazlaSatis == 0 {
		var maxTrafikMB int64
		_ = h.DB.QueryRowContext(ctx, `SELECT max_trafik_mb FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxTrafikMB)
		if maxTrafikMB > 0 {
			var yeniTr, mevcutTr int64
			_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(trafik_kota_mb,0) FROM service_plans WHERE id=?`, *planID).Scan(&yeniTr)
			_ = h.DB.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0)+`),0) FROM domains d`+
					efektiflimit.Join+`WHERE d.reseller_id=?`, rid).Scan(&mevcutTr)
			if mevcutTr+yeniTr >= maxTrafikMB && yeniTr > 0 {
				return fmt.Errorf("trafik kotanız yetersiz (taahhüt %d MB + yeni %d MB > limit %d MB)", mevcutTr, yeniTr, maxTrafikMB)
			}
		}
	}
	// Gercek kullanim: bayinin hosting hesaplarinin FIILI disk toplami limiti astiysa
	// yeni hesap acilamaz (taahhut kontrolunden bagimsiz ikinci kapi).
	if maxDiskMB > 0 {
		var kullanimKB int64
		_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=?`, rid).Scan(&kullanimKB)
		if kullanimKB/1024 >= maxDiskMB { // sinira ULASMAK doludur (tum kapilarda ayni kural)
			return fmt.Errorf("disk kullanımınız limitte (%d MB / %d MB)", kullanimKB/1024, maxDiskMB)
		}
	}
	if maxDiskMB > 0 && planID != nil && fazlaSatis == 0 {
		var yeniKota int64
		_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(disk_kota_mb,0) FROM service_plans WHERE id=?`, *planID).Scan(&yeniKota)
		var mevcut int64
		_ = h.DB.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+`),0) FROM domains d`+
				efektiflimit.Join+`WHERE d.reseller_id=?`, rid).Scan(&mevcut)
		if mevcut+yeniKota >= maxDiskMB && yeniKota > 0 {
			return fmt.Errorf("disk kotanız yetersiz (taahhüt %d MB + yeni %d MB > limit %d MB)", mevcut, yeniKota, maxDiskMB)
		}
	}
	return nil
}

// saglama / saglamaGeriAl: sistem hesabı + vhost kurulumu (testler değiştirir —
// ret yollarının useradd/nginx yan etkisine HİÇ ulaşmadığını kanıtlamak için).
var (
	saglama       = provisioner.Provision
	saglamaGeriAl = provisioner.Deprovision

	// Sağlama SONRASI dış etkili adımlar (testler sahteyle değiştirir: gerçek
	// systemd/BIND/pure-ftpd'ye ulaşılmadan bağlamın iptal edilmediği kanıtlanır).
	olusturKaynakUygula  = kaynaklimit.UygulaHepsi
	olusturYedekDevral   = backups.YetimleriDevral
	olusturKalintiAyir   = backups.YetimKalintilariniAyir
	olusturFTP           = hesaplar.FTPCreate
	olusturSertifikaSart = provisioner.SertifikaSartMi
	olusturDNSTohumla    = dns.SeedDefaults
	olusturZoneYaz       = dns.WriteZone
)

// olusturSuresi — hosting oluşturmanın istekten KOPUK üst süresi. Ölçülen olağan
// süre ~10-35 sn (SELinux fcontext + php-fpm/nginx doğrulama + reload); üst sınır
// yalnız takılmayı keser.
const olusturSuresi = 10 * time.Minute

// kopukSure — öteki istekten kopuk yan etki zincirlerinin (PHP/backend/SSL) üst süresi.
const kopukSure = 5 * time.Minute

// Vhost render + SUNUCU GENELİ `nginx -t` + reload yapan dış etkili adımlar
// (testler sahteyle değiştirir: hata metninin kime ne döndüğü ana makinenin
// nginx'ine dokunmadan sınanır). Hataları istemciye nginxset.SunucuHatasi ile
// döner: `nginx -t` çıktısı komşu kiracıların sertifika/vhost yollarını taşır;
// yönetici ayrıntıyı aynen görür, öteki roller genel mesaj (ayrıntı günlükte).
var (
	phpSurumUygula  = provisioner.SetPHPVersion
	tenantFPMAktif  = provisioner.TenantFPMActive // per-tenant php-fpm kurulu mu
	tenantFPMUygula = provisioner.EnableTenantFPM // per-tenant birimi (yeni sürümle) yeniden kurar
	vhostUygulaAlan = provisioner.ApplyVhostForDomain
	vhostYenidenCiz = provisioner.RerenderVhost
	webKokDogrula   = provisioner.WebRootMutlak
	sslKapat        = provisioner.DisableSSL
)

// adAlaniYaz: ad-alanı denetiminin sonucunu HTTP'ye çevirir. Bayiye giden metin
// başka kiracının adını/sahibini içermez; ayrıntı yalnız yöneticiye.
func adAlaniYaz(w http.ResponseWriter, r *http.Request, err error) {
	var ah *AdAlaniHatasi
	if errors.As(err, &ah) {
		httpx.WriteError(w, http.StatusConflict, ah.Mesaj(middleware.RolFrom(r) == "admin" && middleware.ResellerIDFrom(r) == 0))
		return
	}
	log.Printf("domains.Create: ad alanı denetlenemedi: %v", err)
	httpx.WriteError(w, http.StatusInternalServerError, "alan adı denetlenemedi")
}

func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r) // >0 ise reseller olusturuyor
	// Kimliksiz bayi jetonu (rol reseller, anahtar 0) KÖK adına hosting açmasın:
	// aşağıdaki kapsam kapıları ve ad-alanı kuralı rid>0 ile bayiyi tanır.
	if middleware.RolFrom(r) == "reseller" && rid <= 0 {
		httpx.WriteError(w, http.StatusForbidden, "yetkiniz yok")
		return
	}
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	req.AlanAdi = strings.ToLower(strings.TrimSpace(req.AlanAdi))
	// Bayi ADINA hosting (yalnız kök): bundan sonra her kural rid = bayi ile işler (varsayılan plan,
	// plan kapsamı, kota + askı, ad alanı, sahiplik). Bayi oturumu/anahtarı bu alanı kullanamaz:
	// kendi kapsamı zaten kendisidir, başka bayi adına açmak IDOR olurdu.
	if req.BayiID != nil {
		if rid > 0 || middleware.RolFrom(r) != "admin" {
			httpx.WriteError(w, http.StatusForbidden, "bayi_id yalnız kök kullanıcıyla kullanılabilir")
			return
		}
		// API anahtarında genel hesap izni yetmez: bayinin kotasını/kapsamını kullanan işlem bayi
		// (yazma) izni de ister (WHMCS bayi ürünü anahtarında zaten vardır).
		if k := middleware.AnahtarFrom(r); k != nil && !izinVar(k.Izinler, "bayi:yaz") {
			httpx.WriteError(w, http.StatusForbidden, "bayi_id için anahtarda bayi (yazma) izni de gerekir")
			return
		}
		var bir int
		err := sql.ErrNoRows
		if *req.BayiID > 0 {
			err = h.DB.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id=? AND role='reseller'`, *req.BayiID).Scan(&bir)
		}
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusBadRequest, "bayi bulunamadı")
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "bayi okunamadı")
			return
		}
		rid = *req.BayiID
	}
	// 🔴 php_surum: İSTEMCİNİN yazdığı değer panelin yönettiği listede olmalı. Eskiden
	// geçersiz değer sağlamada sessizce 8.3 havuzuna düşüyor, domains.php_surum'a ise
	// ham değer yazılıyordu (havuz 8.3, kayıt "8.9"). Boş değer: aşağıdaki varsayılan
	// (plan / 8.3) davranışı AYNEN korunur. Yan etkisiz ret: plan, kilit ve DB'den önce.
	req.PHPSurum = strings.TrimSpace(req.PHPSurum)
	if req.PHPSurum != "" {
		s, verr := provisioner.PHPSurumDogrula(req.PHPSurum)
		if verr != nil {
			httpx.WriteError(w, http.StatusBadRequest, verr.Error())
			return
		}
		req.PHPSurum = s
	}
	// Plan seçilmediyse varsayılan planı ata — kaynak limitleri HER domaine uygulanır
	// (plan-driven default). Varsayılan yoksa plansız devam eder (limit uygulanmaz).
	if req.PlanID == nil {
		var defID int64
		// Reseller ise ONCE kendi varsayilan plani, yoksa global (reseller_id=0).
		// Yalnız KATALOG planı (domain_id IS NULL): bir hostinge özel plan
		// "varsayılan" işaretli olsa bile yeni hostinge otomatik bağlanmaz.
		if e := h.DB.QueryRowContext(r.Context(),
			`SELECT id FROM service_plans WHERE varsayilan=1 AND domain_id IS NULL AND reseller_id IN (?,0) ORDER BY reseller_id DESC, id LIMIT 1`, rid).Scan(&defID); e == nil && defID > 0 {
			req.PlanID = &defID
		}
	}
	// Yönetici: açıkça verilen plan VAR olmalı ve bir hostinge ÖZEL plan olmamalı (bayi yolu
	// plans.BaglanabilirMi ile aynı kuralı aşağıda uygular). Eskiden kök anahtarla plan_id=99999
	// kotasız hosting açıyor, DB'ye sarkık plan_id yazıyordu (gerçek VPS 2026-10-13).
	if rid <= 0 && req.PlanID != nil {
		var planDomain sql.NullInt64
		if err := h.DB.QueryRowContext(r.Context(), `SELECT domain_id FROM service_plans WHERE id=?`, *req.PlanID).Scan(&planDomain); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				httpx.WriteError(w, http.StatusInternalServerError, "plan okunamadı")
				return
			}
			httpx.WriteError(w, http.StatusBadRequest, "plan bulunamadı")
			return
		}
		if planDomain.Valid {
			httpx.WriteError(w, http.StatusBadRequest, plans.ErrOzelPlanBaskaHosting.Error())
			return
		}
	}
	// Reseller kapsam denetimi: kota + plan sahipligi + musteri atamasi yasak.
	if rid > 0 {
		req.CustomerID = nil // reseller musteri atayamaz (Faz 2 kapsami)
		if req.PlanID != nil {
			// Plan kapsamı TEK kaynaktan (plans.BaglanabilirMi — SetPlan ile aynı): bayi
			// yalnız görebildiği KATALOG planını yeni hostinge bağlar; kökün ya da başka
			// kiracının hostinge ÖZEL planı (alan adı + tüm limitler) seçilemez. Var olmayan
			// plan da aynı 403 — plan kimliklerinin varlığı sızdırılmaz.
			if err := plans.BaglanabilirMi(r.Context(), h.DB, rid, *req.PlanID, 0); err != nil {
				httpx.WriteError(w, http.StatusForbidden, err.Error())
				return
			}
		}
		// 🔴 Kilit BURADA alinir ve istek bitene kadar tutulur: kota okumasi ile
		// domains INSERT'i arasinda baska bir istek araya giremesin (TOCTOU).
		kilit := bayiKilidi(rid)
		kilit.Lock()
		defer kilit.Unlock()
		if err := h.resellerKotaKontrol(r.Context(), rid, req.PlanID); err != nil {
			httpx.WriteError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	if req.PHPSurum == "" {
		req.PHPSurum = "8.3"
		// Plan seçildiyse PHP sürümünü plandan miras al
		if req.PlanID != nil {
			var pv string
			if e := h.DB.QueryRowContext(r.Context(), `SELECT php_surum FROM service_plans WHERE id=?`, *req.PlanID).Scan(&pv); e == nil && strings.TrimSpace(pv) != "" {
				// Planın değeri de listeden geçmeli: bozuk plan değeri hostinge ham yazılmaz (8.3 kalır).
				if s, verr := provisioner.PHPSurumDogrula(pv); verr == nil {
					req.PHPSurum = s
				} else {
					log.Printf("domains.Create: planın php_surum değeri geçersiz (plan=%d) — 8.3 kullanıldı", *req.PlanID)
				}
			}
		}
	}
	if err := provisioner.ValidateDomain(req.AlanAdi); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 🔴 Ad-alanı (GL-02 / YK-01): birebir kayıt + başka kiracının alt/üst adı +
	// alt alan adı + panel adları. Sağlamadan ÖNCE (yan etkisiz ret); INSERT anında
	// kilit altında yeniden denetlenir (AdAlaniEkle).
	if err := AdAlaniDenetle(r.Context(), h.DB, req.AlanAdi, rid); err != nil {
		adAlaniYaz(w, r, err)
		return
	}

	// 1) Linux user + nginx + PHP pool
	if err := kota.CheckDomainEklenebilir(r.Context(), h.DB, nil); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// 🔴🔴 YAN ETKİ BURADA BAŞLAR — bundan sonra istek bağlamı KULLANILMAZ.
	//
	// 181 (2026-10-03): sağlama ~30 sn sürdü, tarayıcının 30 sn axios sınırı
	// bağlantıyı kapattı (nginx 499), r.Context() iptal oldu; kullanıcı/vhost/FPM
	// kurulmuşken DNS tohumu + SOA + zone "context canceled" ile düştü → zone'suz,
	// YARIM alan adı. İş artık istemciden bağımsız ya TAMAMLANIR ya da (INSERT
	// düşerse) saglamaGeriAl ile geri alınır. (Silme/askı/PHP ile aynı desen.)
	ctx, iptal := httpx.KopukBaglam(r, olusturSuresi)
	defer iptal()

	pr, err := saglama(req.AlanAdi, req.PHPSurum)
	if errors.Is(err, provisioner.ErrPHPKuruluDegil) {
		// Sağlama hiçbir şeye dokunmadan reddetti (PUT /domains/{id}/php ile aynı yanıt).
		httpx.WriteError(w, http.StatusConflict, "PHP "+req.PHPSurum+" bu sunucuda kurulu değil")
		return
	}
	if err != nil {
		log.Printf("provision %q başarısız: %v", req.AlanAdi, err)
		// Sağlama SUNUCU GENELİ `nginx -t` / `php-fpm -t` / reload çalıştırır;
		// çıktısı komşu kiracıların vhost/havuz yollarını taşır (bayi bu uca
		// erişir). Yönetici metni aynen; öteki roller genel mesaj.
		httpx.WriteError(w, http.StatusInternalServerError, "sağlama başarısız: "+nginxset.SunucuHatasi(r, "domains.olustur", 0, err))
		return
	}

	// DB adi/kullanici cPanel gibi: c_ ONEKSIZ temiz slug (c_girgin -> girgin_). Bunlar
	// TEMEL adlardir; komsunun ad-alanina dusuyorsa saglamaDBKur kendi ad-alaninda secer.
	dbPrefix := strings.TrimPrefix(pr.SistemKullanici, "c_")
	dbUser := dbPrefix + "_db"
	dbName := dbPrefix + "_main"
	// SQL istenmediyse domains satirina HAYALET db adi/kullanici yazma: kullanici
	// detayda var-olmayan bir DB gorup baglanmaya calisirdi. db_accounts zaten
	// kaynak-of-truth; bu alanlar yalniz DB gercekten olusturulunca dolar.
	if !req.SqlOlustur {
		dbUser, dbName = "", ""
	}

	// 2) domains satırı — ad-alanı denetimiyle AYNI kilit altında (sağlama sürerken
	// başka kiracı üst/alt adı açmış olabilir).
	var res sql.Result
	if err := AdAlaniEkle(ctx, h.DB, req.AlanAdi, rid, func() error {
		var e error
		res, e = h.DB.ExecContext(ctx,
			`INSERT INTO domains(alan_adi, sistem_kullanici, php_surum, ssl_aktif, durum, ipv4,
			   ftp_host, ftp_user, db_host, db_user, db_adi, web_root, is_demo, reseller_id)
			 VALUES(?,?,?,0,'aktif',?,?,?, 'localhost',?,?,?, 0, ?)`,
			req.AlanAdi, pr.SistemKullanici, req.PHPSurum, h.IPv4,
			h.IPv4, pr.SistemKullanici, dbUser, dbName, pr.WebRoot, rid)
		return e
	}); err != nil {
		_ = saglamaGeriAl(req.AlanAdi, pr.SistemKullanici)
		var ah *AdAlaniHatasi
		if errors.As(err, &ah) {
			adAlaniYaz(w, r, err)
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "DB kayıt başarısız: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	// A2: bu kimliği daha önce taşımış silinmiş bir hostingin kalıntı yedek satırı varsa (silme izi yazılamamış ve
	// kimlik yeniden kullanılmış) yetim işaretlenir; yeni müşteri eski müşterinin yedeklerini görmez.
	olusturKalintiAyir(ctx, h.DB, id)

	if req.CustomerID != nil || req.PlanID != nil {
		if _, err := h.DB.ExecContext(ctx,
			`UPDATE domains SET customer_id=?, plan_id=? WHERE id=?`,
			req.CustomerID, req.PlanID, id); err != nil {
			log.Printf("domains.Create: plan/müşteri bağlanamadı (domain=%d): %v", id, err)
		}
	}
	// Plan seçildiyse nginx web-sunucusu varsayılanlarını domain'e tohumla + vhost yenile
	if req.PlanID != nil {
		h.applyPlanNginxDefaults(ctx, id, *req.PlanID, pr.SistemKullanici, req.PHPSurum)
	}
	// 🔴🔴 A: IZOLASYON HERKESE — plan SECILMESE DE calisir.
	//
	// ESKI HAL (kapatilan delik): bu blok yukaridaki `if req.PlanID != nil` govdesinin
	// ICINDEYDI. Plansiz olusturulan YENI bir domain UygulaHepsi'yi HIC cagirmiyordu →
	// per-tenant CageFS FPM'e HIC gecmiyor, paylasilan master'da (izolasyonsuz) kaliyordu.
	// Yani "izolasyon" sessizce bir plan ozelligi haline gelmisti.
	//
	// YENI HAL: kosulsuz cagrilir. UygulaHepsi plansiz tenant'ta yalnizca KAYNAK
	// LIMITLERINI atlar (slice yazmaz, governor uygulamaz); izolasyonu (EnableTenantFPM)
	// her durumda kurar. Boylece yeni domain unit'i dogrudan guncel sertlestirilmis
	// sablonla (renderTenantUnit + damga) olusur.
	//
	// Arka plan + kendi 5dk context'i ZORUNLU: r.Context() HTTP istegi bitince iptal olur,
	// cutover yarida kalirdi. SetPlan ile ayni desen.
	// Seam istek goroutine'inde BİR KEZ okunur: arka plan goroutine'i paket değişkenini
	// okusaydı, testin Cleanup'ta geri yüklediği değişkenle yarışırdı (-race).
	kaynakUygula := olusturKaynakUygula
	go func(did int64, sk string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// Silmeyle seri (HostingYik aynı hesap kilidini tutar): hosting oluşturulur oluşturulmaz
		// silinirse bu uygulama silinen hesabın vhost'unu/birimini yeniden yazıp yetim
		// bırakıyordu (VM doğrulaması 2026-10-13). Silindiyse uygulanacak hesap yok.
		hk := kilit.Hesap(sk)
		hk.Lock()
		defer hk.Unlock()
		var vardir int
		if err := h.DB.QueryRowContext(ctx, `SELECT 1 FROM domains WHERE id=?`, did).Scan(&vardir); errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err := kaynakUygula(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply (create) domain=%d: %v", did, err)
		}
	}(id, pr.SistemKullanici)

	// 3) FTP hesap (random parola)
	ftpPass := hesaplar.RandomParola(20)
	uidN, gidN := uidGidOf(pr.SistemKullanici)
	if err := olusturFTP(h.DB, id, pr.SistemKullanici, ftpPass, uidN, gidN); err != nil {
		log.Printf("FTP create %q hata: %v", pr.SistemKullanici, err)
	}

	// 4) MySQL veritabanı — YALNIZCA istenirse (sql_olustur). Çoğu kurulum önce
	// dosya yükler, DB'yi sonra Databases sekmesinden açar → varsayılan KAPALI.
	dbPass := ""
	if req.SqlOlustur {
		dbPass = h.saglamaDBKur(ctx, id, dbName, dbUser)
	}

	// 4.5) HTTPS-ZORUNLU uzanti (.app/.dev/.page...) → sertifikayi HEMEN dene.
	// Tarayici bu TLD'lerde HSTS preload nedeniyle sertifika hatasini atlatmaya
	// IZIN VERMEZ; sertifika olmadan site hic acilmaz. DNS henuz yayilmadiysa
	// sessiz basarisiz olur (fail-safe self-signed 443'u ayakta tutar).
	// Sertifikasiz kalirsa site TAMAMEN erisilemez olacaksa (HTTPS-zorunlu uzanti
	// ya da CDN/proxy arkasi) sertifikayi kurulum aninda dene.
	if gerek, _ := olusturSertifikaSart(req.AlanAdi); gerek {
		go func(alan, sk, php, backend string, did int64) {
			crt, key, gercek := provisioner.OtoSSLDene(alan, sk, php, backend)
			if !gercek {
				return // DNS yayilmamis olabilir; panelden elle kurulabilir
			}
			// Gercek CA cert'i kuruldu → panel "SSL var" gostersin.
			if _, err := h.DB.Exec(`UPDATE domains SET ssl_aktif=1, ssl_kaynak='letsencrypt',
			  cert_path=?, key_path=?, ssl_bitis=DATE_ADD(NOW(), INTERVAL 90 DAY) WHERE id=?`,
				crt, key, did); err != nil {
				log.Printf("oto-SSL: domain %d ssl_aktif yazılamadı: %v — cert kuruldu ama panel 'SSL yok' gösterir", did, err)
			}
		}(req.AlanAdi, pr.SistemKullanici, req.PHPSurum, "php-fpm", id)
	}

	// 5) DNS şablonu otomatik tohumla + BIND zone yaz + reload
	// (Yine de düşerse açılış onarımı dns.EksikZoneOnar tohumlar.)
	if _, err := olusturDNSTohumla(ctx, h.DB, id, req.AlanAdi, h.IPv4); err != nil {
		log.Printf("DNS SeedDefaults %q hata: %v", req.AlanAdi, err)
	}
	if err := olusturZoneYaz(ctx, h.DB, id); err != nil {
		log.Printf("DNS WriteZone %q hata: %v", req.AlanAdi, err)
	}

	// 6) Silinmiş hesabın yetim yedeklerini devral — YALNIZ açık bayrakla (Y1). Hosting artık tamamen kuruldu:
	// devralma başarısız olursa hosting oluşturma BAŞARISIZ SAYILMAZ (loglanır, devralinan_yedek=0, denetime düşer).
	var devralinan int64
	var devralHata error
	if req.YedekleriDevral {
		devralinan, devralHata = olusturYedekDevral(ctx, h.DB, id)
		if devralHata != nil {
			devralinan = 0
			log.Printf("domains.Create: yetim yedekler devralınamadı (domain=%d %s): %v", id, req.AlanAdi, devralHata)
		}
	}

	row := h.DB.QueryRowContext(ctx, selectAll+" WHERE d.id=?", id)
	d, _ := scan(row)

	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "hosting.olustur", req.AlanAdi, fmt.Sprintf("plan=%d", planNoLog(req.PlanID)), rid, true)
	if req.YedekleriDevral && (devralinan > 0 || devralHata != nil) {
		not := fmt.Sprintf("%d yedek devralındı", devralinan)
		if devralHata != nil {
			not = "yedekler devralınamadı (hosting oluşturuldu)"
		}
		httpx.Denetim(h.DB, r, uid, kul, "hosting.yedek-devral", req.AlanAdi, not, rid, devralHata == nil)
	}
	resp := createResp{Domain: d}
	resp.OluşturulanParolalar.FTP = ftpPass
	resp.OluşturulanParolalar.DB = dbPass
	if req.YedekleriDevral {
		resp.DevralinanYedek = &devralinan
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

// saglamaDBKur: saglamadaki varsayilan veritabani (<onek>_main) + kullanici (<onek>_db).
// Donus: olusan kullanicinin parolasi; DB olusmadiysa "" (domains.db_user/db_adi
// temizlenir).
//
// 🔴 ONEK CAKISMASI (DoS): en uzun kok kurali shop.com'un (onek "shop") shop_db'sini
// shop-db.com'a (c'siz koku shop_db) verir. Eskiden ad sabitti: shop-db.com adi onceden
// acinca kurbanin varsayilan DB'si yalniz-yeni MySQLCreateDB'de reddediliyor, hosting
// DB'siz kaliyordu. Adlar artik kurbanin KENDI ad-alaninda (ve MariaDB/panelde serbest)
// secilir (shop_db2 …); secilen ad domains satirina yazilir.
//
// 🔴 HAYALET AD: MySQLCreateDB yalniz-yeni; ad baskasinca tutulmussa (yaris, eski
// kalinti) ya da DB olusamadiysa domains.db_user/db_adi'da var olmayan (ya da BASKASINA
// ait) bir ad yazili kalir, parola da yanitta doner. Kullanici o adla baglanmaya calisir;
// panel baska yerlerde bu alanlari "domainin DB'si" diye okur. Alanlar temizlenir,
// parola yanitta donmez.
func (h *Handlers) saglamaDBKur(ctx context.Context, id int64, dbName, dbUser string) string {
	serbest := func(ad string) (bool, error) { return hesaplar.AdSerbestMi(ctx, h.DB, ad) }
	kul, err := hesaplar.KendiAdAlanindaAdSec(ctx, h.DB, id, dbUser, serbest)
	ad := ""
	if err == nil {
		ad, err = hesaplar.KendiAdAlanindaAdSec(ctx, h.DB, id, dbName, serbest)
	}
	if err == nil && (kul != dbUser || ad != dbName) {
		_, err = h.DB.ExecContext(ctx, `UPDATE domains SET db_user=?, db_adi=? WHERE id=?`, kul, ad, id)
	}
	parola := ""
	if err == nil {
		if parola = hesaplar.RandomParola(24); parola == "" {
			err = errors.New("güvenli parola üretilemedi")
		}
	}
	if err == nil {
		err = hesaplar.MySQLCreateDB(h.DB, id, ad, kul, parola)
	}
	if err != nil {
		log.Printf("MySQL create %q/%q hata: %v", dbName, dbUser, err)
		if _, uerr := h.DB.ExecContext(ctx,
			`UPDATE domains SET db_user='', db_adi='' WHERE id=?`, id); uerr != nil {
			log.Printf("MySQL create %q: domains db alanlari temizlenemedi: %v", dbName, uerr)
		}
		return ""
	}
	return parola
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// 🔴 Silme ile aski zinciri ayni kilitte: zincir, Deprovision nginx conf'unu
	// sildikten SONRA RerenderVhost ile geri yaziyordu → DB'de olmayan domain
	// icin YETIM vhost kaliyordu (nginx silinmis alan adini dinlemeye devam).
	if kap := httpx.DomainKapsam(h.DB, id); true {
		k := kilit.Bayi(kap)
		k.Lock()
		defer k.Unlock()
	}
	var alanAdi, sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	// Onay metni (geri alınamaz işlem): ?onay=<alan adı>. Bayi için ZORUNLU — toplu
	// silmede yanlış kimlikli/kör bir istek başka bir hostingi silemesin; yönetici
	// verirse doğrulanır, vermezse eski davranış (yönetici araçları/E2E).
	if kod, mesaj := silmeOnayi(r, alanAdi); kod != 0 {
		httpx.WriteError(w, kod, mesaj)
		return
	}

	// 🔴 SİLME YEDEKLERDEN BAĞIMSIZDIR (kullanıcı kararı, 2026-10-08): yedek dizinine ve uzak
	// kopyalara dokunulmaz, yedek/uzak depo durumu silmeyi engellemez. Yedekler olduğu yerde kalır;
	// HostingYik domains satırı gitmeden önce silme izini hazırlar (backups.SilinenIzHazirla: alan adı, sk, bayi,
	// slot, uzak hedefin şifreli anlık görüntüsü), satır silinince TEK best-effort UPDATE ile yedek satırlarını
	// yetim işaretler (iz.Yaz: domain_id=0, benzersiz iz_id); yazılamazsa açılış/04:00 sahipsiz işaretleme
	// turu (SilinenSahipsizIsaretle) yakalar. Süre dolunca (varsayılan: hostingin yedek slotu kadar gün)
	// günlük süpürme temizler; yönetici elle silebilir ya da yeni bir hostinge bağlayabilir.

	// 🔴 YIKIM BASLADIKTAN SONRA ISTEK BAGLAMI KULLANILMAZ.
	//
	// Deprovision (userdel -r, acme kaldirma, nginx reload) ve yedek dizini
	// silme ctx'e bagli DEGIL; ama sonrasindaki `DELETE FROM domains` ve
	// orphan temizlikleri `r.Context()` kullaniyordu. Istemci koparsa
	// (tarayici 30 sn'de zaman asimina ugruyor) yikim TAMAMLANIR, kayit
	// silme IPTAL EDILIR: panel domaini "aktif" gosterir ama vhost'u, PHP
	// havuzu ve yedekleri gitmistir. Olculdu — 200.000 dosyalik bir yedek
	// dizini silindi, domain satiri kaldi.
	//
	// Ironik olan: bu, hayalet kayitlari gorunur kilma isinin tam da
	// engellemeye calistigi durumu URETIYORDU.
	silCtx := context.WithoutCancel(r.Context())

	kapsamSil, yikErr := HostingYik(silCtx, h.DB, id, alanAdi, sk, isDemo)
	uidSil, kulSil := middleware.Aktor(r)
	if yikErr != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silme hatası: "+yikErr.Error())
		return
	}
	httpx.Denetim(h.DB, r, uidSil, kulSil, "hosting.sil", alanAdi, "sistem_kullanici="+sk, kapsamSil, true)
	destekiptal.Domain(r.Context(), h.DB, id, protokol.IptalAlanSilme, uidSil, kulSil)

	HostingYikSonrasi(r.Context(), h.DB, alanAdi, isDemo)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"silinen": map[string]string{"alan_adi": alanAdi, "sistem_kullanici": sk},
	})
}

func uidGidOf(u string) (int, int) {
	uu, err := user.Lookup(u)
	if err != nil {
		return 0, 0
	}
	uid, _ := strconv.Atoi(uu.Uid)
	gid, _ := strconv.Atoi(uu.Gid)
	return uid, gid
}

type setPHPReq struct {
	PHPSurum string `json:"php_surum"`
}

func (h *Handlers) SetPHP(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setPHPReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	req.PHPSurum = strings.TrimSpace(req.PHPSurum)
	if req.PHPSurum == "" {
		httpx.WriteError(w, http.StatusBadRequest, "php_surum zorunlu")
		return
	}
	// 🔴 Yan etkiden ÖNCE doğrula: desteklenmeyen değer sessizce 8.3 havuzuna düşüp DB'ye
	// ham yazılıyordu. Boş değer yukarıda "zorunlu" (varsayılan YOK).
	surum, verr := provisioner.PHPSurumDogrula(req.PHPSurum)
	if verr != nil {
		httpx.WriteError(w, http.StatusBadRequest, verr.Error())
		return
	}
	var alanAdi, sk, backend, certPath, keyPath, sslKaynak string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(web_backend,'php-fpm'), COALESCE(cert_path,''), COALESCE(key_path,''), COALESCE(ssl_kaynak,'') FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo, &backend, &certPath, &keyPath, &sslKaynak)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin PHP sürümü değiştirilemez")
		return
	}
	// Denetim: önceki sürüm (best-effort; okunamazsa detayda yer almaz).
	var eskiSurum string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(php_surum,'') FROM domains WHERE id=?`, id).Scan(&eskiSurum)
	detay := "yeni=" + surum
	if es := strings.TrimSpace(eskiSurum); es != "" {
		detay = "eski=" + es + " " + detay
	}
	uid, kul := middleware.Aktor(r)

	// Havuz/vhost değişince DB de yazılmalı — istemci kopsa da (kopuk bağlam).
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	var socket string
	if tenantFPMAktif(sk) {
		// 🔴 Per-tenant FPM (Seçenek A) kurulu hesapta sürüm PER-TENANT BİRİMİNDEDİR:
		// SetPHPVersion yalnız paylaşılan havuzu yazar (sahipsiz paylaşılan pool +
		// DB "yeni sürüm" derken birim ESKİ php-fpm'i çalıştırmaya devam ederdi).
		// UI yolu (PUT /php-settings) ile aynı: birim yeni sürümle yeniden kurulur,
		// vhost per-tenant sokete render edilir.
		socket, err = tenantFPMUygula(h.DB, id, sk, surum)
	} else {
		socket, err = phpSurumUygula(alanAdi, sk, surum, certPath, keyPath, sslKaynak, backend)
	}
	if errors.Is(err, hesaplar.ErrHesapAskida) {
		// Hiçbir şeye dokunulmadı (havuz askıda başlatılmaz).
		httpx.WriteError(w, http.StatusConflict, "hesap askıda — PHP sürümü askıdan alındıktan sonra değiştirilebilir")
		return
	}
	if errors.Is(err, provisioner.ErrPHPKuruluDegil) {
		// Birim kurulmadan önce denetlenir: hiçbir şeye dokunulmadı.
		httpx.WriteError(w, http.StatusConflict, "PHP "+surum+" bu sunucuda kurulu değil")
		return
	}
	if err != nil {
		httpx.DenetimDomain(h.DB, r, uid, kul, "hosting.php_surum", alanAdi, detay, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "değişim başarısız: "+nginxset.SunucuHatasi(r, "domains.php", id, err))
		return
	}
	if _, err := h.DB.ExecContext(kctx,
		`UPDATE domains SET php_surum=? WHERE id=?`, surum, id); err != nil {
		httpx.DenetimDomain(h.DB, r, uid, kul, "hosting.php_surum", alanAdi, detay, id, false)
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	// Per-tenant FPM'de alt alanlar hesabın TEK havuzunu kullanır: kayıtları gerçekte çalışan
	// sürüme eşitle (arayüz alt alanda eski sürümü göstermesin).
	if tenantFPMAktif(sk) {
		if _, err := h.DB.ExecContext(kctx, `UPDATE subdomanlar SET php_surum=? WHERE domain_id=?`, surum, id); err != nil {
			log.Printf("php sürümü sonrası alt alan kayıtları (domain=%d): %v", id, err)
		}
	}
	// Ek alan adları hesabın PHP havuzunu kullanır: sürüm/soket değişti → vhost'ları yeniden kur.
	if err := subdomain.EkAlanVhostlariniYenidenKur(kctx, h.DB, id); err != nil {
		log.Printf("php sürümü sonrası ek alan vhost'ları (domain=%d): %v", id, err)
	}
	httpx.DenetimDomain(h.DB, r, uid, kul, "hosting.php_surum", alanAdi, detay, id, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "php_surum": surum, "socket": socket,
	})
}

// Web backend seçici — "php-fpm" | "apache" | "static"
type setBackendReq struct {
	Backend string `json:"backend"`
}

// gecerliBackendler — API'nin kabul ettiği backend'ler.
//
// 🔴 "proxy" bilerek `mevcutlar` listesinde DEĞİL: kullanıcı arayüzünden elle
// seçilebilir olsaydı, uygulaması olmayan bir domain proxy'ye alınıp location /
// hiç tanımlanmadan kalır ve site 404'e düşerdi. Bu modu Uygulama Çalıştırıcı
// eklentisi, uygulamayı kurduktan SONRA ayarlar.
var gecerliBackendler = map[string]bool{"php-fpm": true, "apache": true, "static": true, "proxy": true}

func (h *Handlers) GetWebBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var backend string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).Scan(&backend)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"backend":   backend,
		"mevcutlar": []string{"php-fpm", "apache", "static"},
	})
}

// uygulamaKokSnippetiVar — kiracının kök dizinde yayınlanmış bir uygulaması
// (yani `location /` getiren bir snippet'i) var mı.
//
// Core eklentiyi TANIMAZ; yalnız uzatma noktasının dolu olup olmadığına bakar.
func uygulamaKokSnippetiVar(sk string) bool {
	for _, c := range sk {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	if sk == "" {
		return false
	}
	ents, err := os.ReadDir("/etc/nginx/gosp-app/" + sk + "/kok")
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
			return true
		}
	}
	return false
}

func (h *Handlers) SetWebBackend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setBackendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if !gecerliBackendler[req.Backend] {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz backend (php-fpm|apache|static|proxy)")
		return
	}
	var alanAdi, sk, phpSurum string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alan_adi, sistem_kullanici, php_surum, is_demo FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &phpSurum, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin backend'i değiştirilemez")
		return
	}
	// 🔴 UYGULAMASIZ PROXY MODU REDDEDİLİR. proxy dalı bilerek `location /`
	// BASMAZ — onu uygulama snippet'i getirir. Snippet yokken bu moda geçen bir
	// domainin TAMAMI 404 döner, üstelik arayüzde proxy diye bir kart olmadığı
	// için hiçbir seçenek seçili görünmez ve kullanıcı sitesinin neden öldüğünü
	// panelden ANLAYAMAZ. Kapı bunu baştan engeller.
	if req.Backend == "proxy" && !uygulamaKokSnippetiVar(sk) {
		httpx.WriteError(w, http.StatusBadRequest,
			"bu alan adında kök dizinde yayınlanmış bir uygulama yok — proxy moduna geçilemez")
		return
	}
	_ = alanAdi
	// Yan etki burada başlar (DB → vhost → gerekirse DB geri alma): istemci kopsa
	// da zincir tamamlanır, DB ile disk ayrışmaz (kopuk bağlam).
	kctx, iptal := httpx.KopukBaglam(r, kopukSure)
	defer iptal()
	// Eski değeri SAKLA: render düşerse DB'yi geri almak için gerekir.
	var eskiBackend string
	_ = h.DB.QueryRowContext(kctx,
		`SELECT COALESCE(web_backend,'php-fpm') FROM domains WHERE id=?`, id).Scan(&eskiBackend)

	// 1) DB güncelle
	if _, err := h.DB.ExecContext(kctx,
		`UPDATE domains SET web_backend=? WHERE id=?`, req.Backend, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	// 2) Vhost'u yeniden uygula (nginx + apache yöneticisi web_backend'i DB'den okur)
	socket, _ := provisioner.PHPSocketFor(sk, phpSurum)
	if err := vhostUygulaAlan(h.DB, id, socket, phpSurum); err != nil {
		hata := nginxset.SunucuHatasi(r, "domains.web-backend", id, err)
		// 🔴 DB'Yİ GERİ AL. Aksi halde DB "php-fpm" derken disk proxy vhost'unu
		// servis eder; daha kötüsü, bundan sonraki HER arka plan render'ı (SSL
		// yenilemesi dahil) DB'den yeni değeri okuyup aynı hataya düşer ve o
		// kiracının yenilemesi KALICI olarak bozulur — kimse fark etmeden.
		if eskiBackend != "" {
			if _, gerr := h.DB.ExecContext(kctx,
				`UPDATE domains SET web_backend=? WHERE id=?`, eskiBackend, id); gerr != nil {
				httpx.WriteError(w, http.StatusInternalServerError,
					"vhost render: "+hata+" — DB geri alınamadı: "+gerr.Error())
				return
			}
		}
		httpx.WriteError(w, http.StatusInternalServerError,
			"vhost render: "+hata+" (backend değişikliği geri alındı)")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "backend": req.Backend,
	})
}

// ── Belge koku (web_root) — Laravel vb. icin dinamik public_html/<alt> ──
type setWebRootReq struct {
	AltDizin string `json:"alt_dizin"`
}

// GET /domains/{id}/web-root → mevcut alt dizin + aday dizinler (dinamik secim)
func (h *Handlers) GetWebRoot(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sk, webRoot string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(web_root,'') FROM domains WHERE id=?`, id).Scan(&sk, &webRoot)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"alt_dizin": provisioner.AltDizinCoz(sk, webRoot),
		"adaylar":   provisioner.AdayKokler(sk),
	})
}

// PUT /domains/{id}/web-root {"alt_dizin":"public"} → public_html/<alt>'i belge koku yap.
// Bos alt_dizin = public_html koku. Dizin VAR OLMALI + public_html icinde OLMALI.
func (h *Handlers) SetWebRoot(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setWebRootReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "okuma hatası: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin belge kökü değiştirilemez")
		return
	}
	// doğrula + mutlak yol (traversal/cross-tenant/symlink kapalı, dizin var olmalı)
	abs, verr := webKokDogrula(sk, req.AltDizin)
	if verr != nil {
		httpx.WriteError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE domains SET web_root=? WHERE id=?`, abs, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB güncelleme: "+err.Error())
		return
	}
	// vhost yeniden render (nginx -t + rollback İÇERİDE — bozuk config diske kalmaz)
	if err := vhostYenidenCiz(h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "vhost render: "+nginxset.SunucuHatasi(r, "domains.web-root", id, err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"alt_dizin": provisioner.AltDizinCoz(sk, abs),
	})
}

// FTP parola değiştir
type setFTPPwReq struct {
	Parola string `json:"parola"`
}

func (h *Handlers) SetFTPPassword(w http.ResponseWriter, r *http.Request) {
	// API anahtarı (ftp.parola izni): otomasyon sözleşmesi — "password" alanı, boşsa
	// üretim, panel parola kuralı, denetim kaydı (api_parola.go). Arayüz yolu aşağıda
	// DEĞİŞMEDEN kalır.
	if middleware.AnahtarFrom(r) != nil {
		h.apiFTPParola(w, r, "")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req setFTPPwReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	if req.Parola == "" {
		httpx.WriteError(w, http.StatusBadRequest, "yeni parola gerekli")
		return
	}
	if !hesaplar.ParolaGecerli(req.Parola) {
		httpx.WriteError(w, http.StatusBadRequest, "parola geçersiz karakter (satır sonu) içeriyor")
		return
	}
	var sk, alan string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, alan_adi, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &alan, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin FTP parolası değiştirilemez")
		return
	}
	if err := hesaplar.FTPUpdatePassword(h.DB, sk, req.Parola); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "FTP parola güncelleme: "+err.Error())
		return
	}
	// Parola yazıldı: ana hesabın AÇIK FTP oturumları düşer (hata parolayı bozmaz).
	uid, aktor := middleware.Aktor(r)
	h.ftpOturumlariDusur(r, uid, aktor, alan, sk, id)
	// SSH açıksa sistem (SSH) parolasını da FTP ile senkronla
	var sshOn int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT ssh_erisim FROM domains WHERE id=?`, id).Scan(&sshOn)
	if sshOn == 1 {
		_ = hesaplar.SyncSSHPassword(h.DB, sk)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "username": sk,
	})
}

// Veritabanı listele (domain'e ait)
type DBAccount struct {
	ID          int64  `json:"id"`
	DomainID    int64  `json:"domain_id"`
	DBAdi       string `json:"db_adi"`
	DBKullanici string `json:"db_kullanici"`
	DBHost      string `json:"db_host"`
	DBParola    string `json:"db_parola"`
	Olusturulma string `json:"olusturulma"`
	Boyut       int64  `json:"boyut"`
}

func (h *Handlers) ListDatabases(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, domain_id, db_name, db_user, db_host, db_pass_plain, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		 FROM db_accounts WHERE domain_id=? ORDER BY id`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB sorgu: "+err.Error())
		return
	}
	defer rows.Close()
	out := make([]DBAccount, 0)
	for rows.Next() {
		var d DBAccount
		if err := rows.Scan(&d.ID, &d.DomainID, &d.DBAdi, &d.DBKullanici, &d.DBHost, &d.DBParola, &d.Olusturulma); err != nil {
			continue
		}
		d.DBParola = "" // 🔴 CWE-200: parola LISTEDE donmez; sahip /databases/{dbid}/parola ucundan alir
		out = append(out, d)
	}
	// Boyut (data+index): panel MySQL kullanicisi tenant DB'lerini information_schema'da
	// GOREMEZ (yetki kapsami) -> ROOT socket exec ile sorgula.
	if len(out) > 0 {
		var inl strings.Builder
		for i := range out {
			if i > 0 {
				inl.WriteString(",")
			}
			inl.WriteString("'" + strings.ReplaceAll(out[i].DBAdi, "'", "") + "'")
		}
		q := "SELECT table_schema, COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema IN (" + inl.String() + ") GROUP BY table_schema"
		if bo, be := exec.CommandContext(r.Context(), "mysql", "-N", "-B", "-e", q).Output(); be == nil {
			f := strings.Fields(string(bo))
			boyutlar := map[string]int64{}
			for i := 0; i+1 < len(f); i += 2 {
				nv, _ := strconv.ParseInt(f[i+1], 10, 64)
				boyutlar[f[i]] = nv
			}
			for i := range out {
				out[i].Boyut = boyutlar[out[i].DBAdi]
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// createDBReq: "Yeni Veritabanı" istegi.
//
// Otomatik=true (veya hicbir alan verilmezse) → DB adi/kullanici/parola OTOMATIK uretilir
// (eski davranis, geriye uyumlu). Aksi halde musteri OZELLESTIRIR:
//   - DBSonek: DB adi soneki → panel `<sk>_` onekini ZORUNLU ekler (cakisma-guvenli).
//   - KullaniciTipi "yeni": KullaniciSonek gir (onek eklenir); "mevcut": MevcutKullanici sec.
//   - Parola: musteri girer (guclu olmali) VEYA bos → panel guclu rastgele uretir.
type createDBReq struct {
	Otomatik        bool   `json:"otomatik"`
	DBSonek         string `json:"db_sonek"`
	KullaniciTipi   string `json:"kullanici_tipi"` // "yeni" | "mevcut"
	KullaniciSonek  string `json:"kullanici_sonek"`
	MevcutKullanici string `json:"mevcut_kullanici"`
	Parola          string `json:"parola"`
}

func (h *Handlers) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req createDBReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz istek gövdesi")
		return
	}
	var sk string
	var isDemo int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).
		Scan(&sk, &isDemo)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "domain sorgu: "+err.Error())
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe veritabanı eklenemez")
		return
	}
	// 🔴 Sayı kapısı + oluşturma TEK kilit altında: eşzamanlı istekler aynı COUNT'u okuyup
	// max_db sınırını aşmasın (kilit: hosting başına; bkz. kilit.DBOlustur).
	dbKilit := kilit.DBOlustur(id)
	dbKilit.Lock()
	defer dbKilit.Unlock()
	if err := kota.CheckDBEklenebilir(r.Context(), h.DB, id); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// Geriye uyumlu: gövde boş / Otomatik=true → hepsini otomatik üret (eski davranış).
	otomatik := req.Otomatik ||
		(req.DBSonek == "" && req.KullaniciSonek == "" && req.MevcutKullanici == "" && req.Parola == "")

	dbPrefix := strings.TrimPrefix(sk, "c_") // cPanel gibi c_ oneksiz DB prefix (girgin_)
	var dbAdi, dbKullanici, parola string
	mevcutKullaniciModu := false

	if otomatik {
		dbAdi = dbPrefix + "_ek" + strconv.FormatInt(id, 10)
		dbKullanici = dbAdi
		parola = hesaplar.RandomParola(24)
	} else {
		// --- DB adı: müşteri SONEK verir, panel `<sk>_` önekini ZORUNLU ekler ---
		if req.DBSonek == "" {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı adı soneki gerekli")
			return
		}
		if !hesaplar.GecerliDBSonek(req.DBSonek) {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz veritabanı soneki (yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter)")
			return
		}
		dbAdi = dbPrefix + "_" + req.DBSonek
		if !hesaplar.GecerliDBKimlik(dbAdi) {
			httpx.WriteError(w, http.StatusBadRequest, "veritabanı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
			return
		}

		// --- Kullanıcı: yeni (sonek) VEYA mevcut (bu domaine ait) ---
		switch req.KullaniciTipi {
		case "mevcut":
			if req.MevcutKullanici == "" || !hesaplar.GecerliDBKimlik(req.MevcutKullanici) {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz mevcut kullanıcı")
				return
			}
			// Sahiplik: seçilen kullanıcı GERÇEKTEN bu domaine ait olmalı (önek garantisi).
			// BINARY: MariaDB hesap adı harf duyarlı; kendi "GIRGIN_X_DB" satırı komşunun
			// "girgin_x_db"sini seçtirmemeli.
			var n int
			_ = h.DB.QueryRowContext(r.Context(),
				`SELECT COUNT(*) FROM db_accounts WHERE domain_id=? AND BINARY db_user=?`, id, req.MevcutKullanici).Scan(&n)
			if n == 0 {
				httpx.WriteError(w, http.StatusBadRequest, "seçilen kullanıcı bu domaine ait değil")
				return
			}
			dbKullanici = req.MevcutKullanici
			mevcutKullaniciModu = true
		default: // "yeni"
			if req.KullaniciSonek == "" {
				httpx.WriteError(w, http.StatusBadRequest, "kullanıcı adı soneki gerekli")
				return
			}
			if !hesaplar.GecerliDBSonek(req.KullaniciSonek) {
				httpx.WriteError(w, http.StatusBadRequest, "geçersiz kullanıcı soneki (yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter)")
				return
			}
			dbKullanici = dbPrefix + "_" + req.KullaniciSonek
			if !hesaplar.GecerliDBKimlik(dbKullanici) {
				httpx.WriteError(w, http.StatusBadRequest, "kullanıcı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
				return
			}
			// Yeni kullanıcı için parola: müşteri girer (güçlü) VEYA boş → panel üretir.
			if req.Parola == "" {
				parola = hesaplar.RandomParola(24)
			} else {
				if ok, neden := hesaplar.ParolaGucluMu(req.Parola); !ok {
					httpx.WriteError(w, http.StatusBadRequest, neden)
					return
				}
				parola = req.Parola
			}
		}
	}

	// İsim çakışması → net 409 (duplicate-key 500 yerine).
	var cakisma int
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM db_accounts WHERE db_name=?`, dbAdi).Scan(&cakisma)
	if cakisma > 0 {
		httpx.WriteError(w, http.StatusConflict, "bu isimde bir veritabanı zaten var: "+dbAdi)
		return
	}

	// 🔴 DB KULLANICI ÖNEKİ ÇAKIŞMASI (kiracılar arası): zorunlu "<önek>_" SAHİPLİK
	// KANITI DEĞİL. Tire alt çizgiye döndüğü ve sonekte '_' serbest olduğu için
	// ad-alanları iç içe geçer: girgin.com'un ("girgin") "x_db" soneki, girgin-x.com'un
	// ("girgin_x") varsayılan kullanıcısı "girgin_x_db"dir. Eskiden bu uç yalnız DB ADI
	// çakışmasına bakıyor, var olan kullanıcıyı MySQLCreateDB'ye bırakıyordu; o da
	// komşunun parolasını değiştirip GRANT ekliyordu. Artık ad (DB ve yeni kullanıcı)
	// başka kiracının ad-alanına düşüyorsa ya da panelde/MariaDB'de zaten varsa 409.
	// Yanıt komşunun adını/varlığını ayrıca sızdırmaz (genel metin).
	if baska, err := hesaplar.DBAdiBaskaKiracininMi(r.Context(), h.DB, id, dbAdi); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "veritabanı adı denetlenemedi")
		return
	} else if baska {
		httpx.WriteError(w, http.StatusConflict, "bu veritabanı adı kullanılamaz — farklı bir sonek seçin")
		return
	}
	// Panelde kayıtlı olmayan (ör. yedekten dönen / elle açılmış) bir DB'ye GRANT verilmez.
	if hesaplar.DBVarMi(dbAdi) {
		httpx.WriteError(w, http.StatusConflict, "bu isimde bir veritabanı zaten var: "+dbAdi)
		return
	}
	if !mevcutKullaniciModu {
		if baska, err := hesaplar.DBAdiBaskaKiracininMi(r.Context(), h.DB, id, dbKullanici); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kullanıcı adı denetlenemedi")
			return
		} else if baska {
			httpx.WriteError(w, http.StatusConflict, "bu kullanıcı adı kullanılamaz — farklı bir sonek seçin")
			return
		}
		// "Yeni kullanıcı" YENİ olmalı: panelde (herhangi bir domainde) ya da MariaDB'de
		// var olan bir hesap bu yoldan devralınamaz — kendi var olan kullanıcısını
		// isteyen "mevcut" modunu (domain_id sahiplik denetimli) kullanır.
		var kulCakisma int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM db_accounts WHERE db_user=?`, dbKullanici).Scan(&kulCakisma); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "kullanıcı adı denetlenemedi")
			return
		}
		if kulCakisma > 0 || hesaplar.DBKullanicisiVarMi(dbKullanici) {
			httpx.WriteError(w, http.StatusConflict, "bu kullanıcı adı zaten var: "+dbKullanici)
			return
		}
	}

	if mevcutKullaniciModu {
		if err := hesaplar.MySQLCreateDBForUser(h.DB, id, dbAdi, dbKullanici); err != nil {
			kod := http.StatusInternalServerError
			if errors.Is(err, hesaplar.ErrDBKimlikMevcut) {
				kod = http.StatusConflict // denetimle oluşturma arasında yarış
			}
			httpx.WriteError(w, kod, "DB oluşturma: "+err.Error())
			return
		}
		// Mevcut kullanıcının parolasını yanıtta göster (müşteri zaten sahibi).
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT db_pass_plain FROM db_accounts WHERE domain_id=? AND BINARY db_user=? LIMIT 1`, id, dbKullanici).Scan(&parola)
		parola = gizli.CozBagli(parola, dbKullanici) // yanitta ACIK deger doner (musteri baglanacak)
	} else {
		// MySQLCreateDB YALNIZ-YENİ: kullanıcı/DB denetimden sonra (yarışla) oluştuysa
		// hiçbir şeye dokunmadan ErrDBKimlikMevcut döner.
		if err := hesaplar.MySQLCreateDB(h.DB, id, dbAdi, dbKullanici, parola); err != nil {
			kod := http.StatusInternalServerError
			if errors.Is(err, hesaplar.ErrDBKimlikMevcut) {
				kod = http.StatusConflict
			}
			httpx.WriteError(w, kod, "DB oluşturma: "+err.Error())
			return
		}
	}

	// Governor/limit: yeni DB-kullanıcısına plan limitlerini uygula (arka planda, best-effort).
	go func(did int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := kaynaklimit.UygulaHepsi(ctx, h.DB, did); err != nil {
			log.Printf("kaynaklimit apply (db-create) domain=%d: %v", did, err)
		}
	}(id)

	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.olustur", dbAdi, "kullanici="+dbKullanici, id, true)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "domain_id": id, "db_adi": dbAdi, "db_kullanici": dbKullanici, "db_parola": parola,
	})
}

func (h *Handlers) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbid, _ := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	var dbName, dbUser string
	var isDemo int
	var domainID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT db.db_name, db.db_user, d.is_demo, d.id
		 FROM db_accounts db JOIN domains d ON d.id=db.domain_id
		 WHERE db.id=?`, dbid).Scan(&dbName, &dbUser, &isDemo, &domainID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "DB kaydı bulunamadı")
		return
	}
	// Rota seviyesinde {id} domain param'i yok → sahiplik BURADA da dogrulanir
	// (rota: DBSahipligiMusteri). Kok her DB'de, bayi kendi hosting hesaplarinda,
	// hosting sahibi (musteri) KENDI domaininin DB'sinde.
	if !middleware.DomainSahibiMi(r, domainID) {
		httpx.WriteError(w, http.StatusForbidden, "bu veritabanına erişiminiz yok")
		return
	}
	if isDemo == 1 {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğin DB'si silinemez")
		return
	}
	// Kullanıcı DROP USER ile YALNIZ güvenle bu domainin hesabıysa düşer; başka DB'lerde de
	// kullanılıyorsa (mevcut-kullanıcı modu), başka domaine kayıtlıysa, sistem/servis hesabıysa
	// ya da yetkileri bu domainin dışına taşıyorsa yalnız DB (ve o ada bağlı yetkiler) düşer.
	// 🔴 Eskiden "başka satırda yok" (paylasim=0) yetiyordu: yedekten kurtarılan wp-config ile
	// satıra yazdırılmış "roundcube"/"pma"/"pureftpd" ya da komşunun kayıtsız kullanıcısı
	// DROP USER ile düşüyordu (sunucu geneli webmail/phpMyAdmin/FTP kesintisi).
	if err := dbSilSahipli(h.DB, domainID, dbName, dbUser); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "DB silme: "+err.Error())
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, kul, "db.sil", dbName, "kullanici="+dbUser, domainID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silinen": dbName})
}

// TopluSahip KALDIRILDI (rota da): sahiplik devri artik yalniz
// POST /domains/toplu/is (tip=sahip) uzerinden, internal/toplu/devir.go'daki
// tam devir zinciriyle yapilir — kimlik rotasyonu, oturum dusurme, webhook
// secret yenileme ve denetim kaydi dahil. Bu handler yalnizca customer_id
// yaziyordu; duran ikinci bir yol, sertlestirmenin atlandigi arka kapidir.
type topluDurumReq struct {
	IDs   []int64 `json:"ids"`
	Durum string  `json:"durum"` // aktif | pasif
}

func (h *Handlers) TopluDurum(w http.ResponseWriter, r *http.Request) {
	var req topluDurumReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "boş ids")
		return
	}
	if req.Durum != "aktif" && req.Durum != "pasif" {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz durum")
		return
	}
	// 🔴 "Pasif" ARTIK GERCEKTEN KAPATIR: eskiden yalnizca durum kolonu degisiyor,
	// site yayinda kaliyordu (kullanici "pasif yaptim ama bir sey olmadi" diyordu).
	// Artik tekil askiya-alma ile AYNI makineden gecer.
	askida := req.Durum == "pasif"
	rid := middleware.ResellerIDFrom(r)
	uid, kul := middleware.Aktor(r)
	kaynak := elleAskiKaynagi(r) // bayi kökün askısını açamaz → atlanan
	// Başlatılan toplu işlem istemci kopsa / HTTP zaman aşımı dolsa da sonuna
	// kadar yürür (yarıda kesilen döngü hangi hesabın işlendiğini belirsiz bırakırdı).
	ctx := context.WithoutCancel(r.Context())

	basarili, atlanan := 0, 0
	for _, id := range req.IDs {
		// Kapsam: bayi yalniz KENDI hosting hesabina dokunabilir.
		if rid > 0 {
			var sahip int64
			if err := h.DB.QueryRowContext(ctx,
				`SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, id).Scan(&sahip); err != nil || sahip != rid {
				atlanan++
				continue
			}
		}
		alanAdi, err := h.AskiUygula(ctx, id, askida, kaynak)
		if err != nil {
			log.Printf("toplu durum: domain %d: %v", id, err)
			atlanan++
			continue
		}
		eylem := "hosting.askidan_al"
		if askida {
			eylem = "hosting.askiya_al"
		}
		httpx.DenetimDomain(h.DB, r, uid, kul, eylem, alanAdi, "toplu işlem", id, true)
		basarili++
	}
	httpx.Denetim(h.DB, r, uid, kul, "hosting.toplu_durum", req.Durum,
		fmt.Sprintf("istenen=%d uygulanan=%d atlanan=%d", len(req.IDs), basarili, atlanan), rid, atlanan == 0)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": atlanan == 0, "guncellenen": basarili, "atlanan": atlanan,
	})
}

// applyPlanNginxDefaults, yeni domain bir plana bağlandığında planın nginx
// varsayılanlarını (FastCGI cache + client_max_body + ek direktifler) domain'in
// nginx_settings satırına yazar ve vhost'u bu ayarlarla yeniden render eder.
// Best-effort: hata olursa domain yine de oluşturulmuş kalır (yalnızca loglanır).
func (h *Handlers) applyPlanNginxDefaults(ctx context.Context, domainID, planID int64, sk, php string) {
	var fc, cmb int
	var ekPlan string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT fastcgi_cache, client_max_body_mb, COALESCE(nginx_ek_direktifler,'')
		   FROM service_plans WHERE id=?`, planID).Scan(&fc, &cmb, &ekPlan); err != nil {
		log.Printf("plan nginx defaults oku (plan=%d): %v", planID, err)
		return
	}
	ek := ""
	if cmb > 0 {
		ek = "client_max_body_size " + strconv.Itoa(cmb) + "m;\n"
	}
	if strings.TrimSpace(ekPlan) != "" {
		ek += ekPlan
	}
	if _, err := h.DB.ExecContext(ctx,
		`INSERT INTO nginx_settings(domain_id, fastcgi_cache, ek_direktifler)
		 VALUES(?,?,?)
		 ON DUPLICATE KEY UPDATE fastcgi_cache=VALUES(fastcgi_cache), ek_direktifler=VALUES(ek_direktifler)`,
		domainID, fc, ek); err != nil {
		log.Printf("nginx_settings tohumla (domain=%d): %v", domainID, err)
		return
	}
	socket, err := provisioner.PHPSocketFor(sk, php)
	if err != nil {
		log.Printf("php socket (domain=%d): %v", domainID, err)
		return
	}
	if err := provisioner.ApplyVhostForDomain(h.DB, domainID, socket, php); err != nil {
		log.Printf("plan vhost yeniden render (domain=%d): %v", domainID, err)
	}
}

// planNoLog: denetim kaydinda plan kimligini yazdirmak icin — nil ise 0.
func planNoLog(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// silmeOnayi: DELETE /domains/{id} onay metni kararı. (0,"") = geçer.
func silmeOnayi(r *http.Request, alanAdi string) (int, string) {
	onay := strings.TrimSpace(r.URL.Query().Get("onay"))
	if onay == "" {
		if middleware.RolFrom(r) == "admin" {
			return 0, ""
		}
		return http.StatusBadRequest, "silme onayı gerekli: alan adını yazın"
	}
	if !strings.EqualFold(onay, alanAdi) {
		return http.StatusBadRequest, "onay metni alan adıyla eşleşmiyor"
	}
	return 0, ""
}

// izinVar: anahtar izin listesinde birebir grup:düzey var mı.
func izinVar(izinler []string, aranan string) bool {
	for _, iz := range izinler {
		if iz == aranan {
			return true
		}
	}
	return false
}
