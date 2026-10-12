package provisioner

import "database/sql"

// HavuzDegerleri — bir PHP-FPM havuzunun planla sınırlanan değerleri.
type HavuzDegerleri struct {
	PMStrategy        string
	MaxChildren       int
	StartServers      int
	MinSpareServers   int
	MaxSpareServers   int
	UploadMaxFilesize string
	PostMaxSize       string
	MaxExecutionTime  int
	MaxInputTime      int
}

// PlanHavuzTavani — per-tenant havuzun plan tavanlarının TEK uygulayıcısı;
// paylaşılan (geri dönüş) havuz ve alt alan havuzları da AYNI tavanı alır.
//
// 🔴 NEDEN: renderTenantPool pm.max_children'ı plandan türetiyor, boyut/süre
// ayarlarını plana göre kırpıyordu; ama paylaşılan havuz (php.RenderPool) ve alt
// alan havuzu (php.renderSubPool) php_settings değerlerini AYNEN yazıyordu. Bu iki
// havuz paylaşılan php-fpm master'ında, hesabın girginos-<sk>.slice'ı DIŞINDA koşar:
// müşteri PUT /php-settings ile pm_max_children=500 yazınca planın işçi sayısı ve
// cgroup RAM/CPU/süreç sınırı aşılıyordu. Artık:
//   - pm.max_children = plan (efektif limit; 0 → max(4, ram_mb/64); plan yok → 8)
//   - start/min_spare/max_spare max_children'dan türetilir (per-tenant ile aynı)
//   - pm stratejisi yalnız static|dynamic|ondemand (bilinmeyen → ondemand)
//   - upload/post disk kotasına, max_execution/max_input sabit tavana kırpılır
//
// memory_limit BİLEREK kapsam dışıdır (2026-09-11 operatör kararı; renderTenantPool).
func PlanHavuzTavani(db *sql.DB, domainID int64, sk string, v HavuzDegerleri) HavuzDegerleri {
	switch v.PMStrategy {
	case "static", "dynamic", "ondemand":
	default:
		v.PMStrategy = "ondemand"
	}
	v.MaxChildren = tenantPMMaxChildren(db, domainID)
	v.StartServers, v.MinSpareServers, v.MaxSpareServers = havuzYedekleri(v.MaxChildren)
	_, diskMB := tenantPlanTavanlari(db, domainID)
	v.UploadMaxFilesize = tenantBoyutTavani(sk, "upload_max_filesize", v.UploadMaxFilesize,
		tenantOranTavani(diskMB, tenantUploadYuzde, tenantUploadTabanMB))
	v.PostMaxSize = tenantBoyutTavani(sk, "post_max_size", v.PostMaxSize,
		tenantOranTavani(diskMB, tenantPostYuzde, tenantPostTabanMB))
	v.PostMaxSize = tenantPostEnAzUpload(sk, v.PostMaxSize, v.UploadMaxFilesize)
	v.MaxExecutionTime = tenantSureTavani(sk, "max_execution_time", v.MaxExecutionTime, tenantMaxExecTavanSn)
	v.MaxInputTime = tenantSureTavani(sk, "max_input_time", v.MaxInputTime, tenantMaxInputTavanSn)
	return v
}

// İstek duvar saati sınırı (request_terminate_timeout) tabanı/tavanı, saniye.
const (
	istekZamanAsimiTabanSn = 600
	istekZamanAsimiTavanSn = 3600
)

// IstekZamanAsimiSn — kafesli tenant havuzunun request_terminate_timeout'u (sn).
//
// 🔴 NEDEN: max_execution_time Linux'ta YALNIZ CPU süresini sayar (ITIMER_PROF);
// G/Ç'de bekleyen işçi (takılı sendmail/postdrop, yanıt vermeyen uzak API, kilitli
// DB) onu hiç aşmaz ve havuzdan sonsuza dek düşer. nginx 60 sn'de istemciye hata
// verir ama PHP arka planda sürer; pm.max_children dolunca SİTE ÇÖKER. Ölçüldü:
// 2026-10-12 .180'de WHMCS ayar kaydı mail()→postdrop'ta asılı kaldı, işçi 7+ dk
// serbest kalmadı. Master bu süreyi aşan işçiyi sonlandırır ve tenant günlüğüne
// "execution timed out" yazar (Günlükler'de görünür).
//
// Değer: max(600, 2×max_execution_time), en çok 3600; max_execution_time ≤ 0
// (sınırsız) → 3600. Meşru uzun işler CLI/cron'da koşmalı (FPM değil).
func IstekZamanAsimiSn(maxExec int) int {
	if maxExec <= 0 {
		return istekZamanAsimiTavanSn
	}
	s := 2 * maxExec
	if s < istekZamanAsimiTabanSn {
		s = istekZamanAsimiTabanSn
	}
	if s > istekZamanAsimiTavanSn {
		s = istekZamanAsimiTavanSn
	}
	return s
}

// havuzYedekleri — dynamic havuzun başlangıç/yedek işçi sayıları (max_children'dan).
func havuzYedekleri(maxCh int) (start, minSpare, maxSpare int) {
	start = maxCh / 4
	if start < 1 {
		start = 1
	}
	minSpare = 1
	maxSpare = maxCh / 2
	if maxSpare < minSpare {
		maxSpare = minSpare
	}
	return start, minSpare, maxSpare
}

// DisableFunctionsTabanli — kullanıcının disable_functions'ına ASLA düşmeyen zorunlu
// tabanı (LPE/izolasyon kaçışı primitifleri) ekler. Per-tenant havuzla aynı kural
// paylaşılan ve alt alan havuzlarında da geçerli olsun diye dışa açık.
func DisableFunctionsTabanli(df string) string { return dfBirlestir(df) }

// HavuzYedekleri — havuzYedekleri'nin dışa açık biçimi (pm.start/min_spare/max_spare,
// max_children'dan): hesap bazlı tavanla kısılan alt alan havuzu yedeklerini yeniden türetir.
func HavuzYedekleri(maxCh int) (start, minSpare, maxSpare int) { return havuzYedekleri(maxCh) }

// PlanPMMaxChildren — domain'in efektif planından pm.max_children (tenantPMMaxChildren:
// plan/özel üst yazım → yoksa max(4, ram_mb/64) → plan yoksa 8). Hesap bazlı alt alan
// havuzu bütçesinin kaynağı.
func PlanPMMaxChildren(db *sql.DB, domainID int64) int { return tenantPMMaxChildren(db, domainID) }
