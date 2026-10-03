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
