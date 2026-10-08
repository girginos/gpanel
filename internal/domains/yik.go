package domains

import (
	"context"
	"database/sql"
	"log"

	"girginospanel/internal/backups"
	"girginospanel/internal/dns"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/redis"
	"girginospanel/internal/subdomain"
)

// HostingYik — bir hosting hesabını (vhost, PHP havuzu, Linux kullanıcısı, DB'ler,
// orphan tablolar ve domains satırı) YOK eder. Tekil silme (Delete) ile bayi silme
// zinciri AYNI yolu kullanır: iki yol ayrışırsa biri kalıntı bırakır.
// 🔴 YEDEKLER SİLME İLE İLGİLENMEZ: yedek dizini ve uzak kopyalar olduğu yerde kalır; silme
// yalnız best-effort iz bırakır (backups.SilinenIsaretle), süre dolunca günlük tur temizler.
// Çağıran: kilit.Bayi kilidini tutar ve denetim kaydını yazar.
// Dönen kapsam, satır gitmeden ÖNCE çözülen domains.reseller_id'dir (denetim için).
// İstemci kopsa da yıkım yarım kalmasın diye çağıran context.WithoutCancel vermelidir.
func HostingYik(ctx context.Context, db *sql.DB, id int64, alanAdi, sk string, isDemo int) (kapsam int64, err error) {
	if isDemo == 0 {
		// Yedekler silme akışından BAĞIMSIZDIR: dizine/uzak kopyalara dokunulmaz, yedek yüzünden
		// silme başarısız olmaz. Yalnız best-effort DB izi (hata vermez), satır gitmeden önce.
		backups.SilinenIsaretle(ctx, db, id, alanAdi, sk)
		// MariaDB'deki gerçek DB'leri kaldır (CASCADE FK sadece panel DB metadata'sını siler)
		_ = hesaplar.MySQLDropAllForDomain(db, id)
		// Ek (addon) alan adları: vhost + sertifika + günlük + bölge + satır; Deprovision'ın
		// nginx reload'undan ÖNCE.
		subdomain.EkAlanlariTemizle(ctx, db, id, sk)
		if e := provisioner.Deprovision(alanAdi, sk); e != nil {
			log.Printf("deprovision warn (%s): %v", alanAdi, e)
		}
		_ = kaynaklimit.SystemdSliceSil(sk)
		redis.KapatDomain(db, id, sk)
	}

	// Orphan temizliği: bu tablolarda FK cascade yok (mevcut kurulumlar için).
	_, _ = db.ExecContext(ctx, `DELETE FROM service_plans WHERE domain_id=?`, id)
	_, _ = db.ExecContext(ctx, `DELETE FROM hosting_limit_ust WHERE domain_id=?`, id)
	_, _ = db.ExecContext(ctx, `DELETE FROM domain_trafik WHERE domain_id=?`, id)
	_, _ = db.ExecContext(ctx, `DELETE FROM domain_trafik_imlec WHERE domain_id=?`, id)
	_, _ = db.ExecContext(ctx, `DELETE FROM wp_bakim WHERE domain_id=?`, id)
	// domains(id)'e FK'si OLMAYAN ve hosting silinince KALAN hesap-kapsamlı tablolar:
	//   - subdomanlar: tam_ad UNIQUE + ad-alanı kuralı (AdAlaniDenetle) yetim alt alan
	//     adını sonsuza dek "dolu" tutar → o ad yeniden hosting/alt alan olarak açılamaz.
	//     (alt tabloları — erişim kısıtı, PHP/web ayarı — subdomanlar(id) FK CASCADE ile gider)
	//   - korumali_dizinler: dizin parola korumasının kullanıcı/htpasswd kayıtları
	//   - cp_uygulama_kurulumlar: uygulama sihirbazı kurulum kayıtları
	//   - cp_wp_kurulum: WordPress yönetici kullanıcısı + (şifreli) parolası
	for _, t := range []string{"subdomanlar", "korumali_dizinler", "cp_uygulama_kurulumlar", "cp_wp_kurulum"} {
		_, _ = db.ExecContext(ctx, `DELETE FROM `+t+` WHERE domain_id=?`, id) //nolint:gosec // G202: t SABİT dilimden gelir; kullanıcı girdisi yok, tablo adı ? ile parametrelenemez.
	}

	kapsam = httpx.DomainKapsam(db, id)
	if _, err = db.ExecContext(ctx, `DELETE FROM domains WHERE id=?`, id); err != nil {
		return kapsam, err
	}
	return kapsam, nil
}

// HostingYikSonrasi — satır gittikten SONRA BIND zone temizliği (updateZoneIncludes
// zones.conf'u domains tablosundan üretir; satır duruyorken silinen zone'un include'u
// geri yazılırdı).
func HostingYikSonrasi(ctx context.Context, db *sql.DB, alanAdi string, isDemo int) {
	if isDemo != 0 {
		return
	}
	if err := dns.DeleteZone(ctx, db, alanAdi); err != nil {
		log.Printf("DNS DeleteZone warn (%s): %v", alanAdi, err)
	}
}
