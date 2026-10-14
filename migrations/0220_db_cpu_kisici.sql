-- 0220_db_cpu_kisici.sql
-- MariaDB CPU kisicisi (internal/dbgovernor): DB kullanicisi / hosting / bayi bazinda CPU
-- suresi muhasebesi (information_schema.USER_STATISTICS) ve kademeli kisma.
--
-- Zamanlar UNIX saniyesidir (BIGINT): kisici hem Go tarafinda hem SQL tarafinda ayni zamani
-- yorumlar, saat dilimi/DST/TIMESTAMP-2038 sorunu dogmaz.
--
-- db_cpu_olcum   5 dakikalik dilimler (dilim = UNIX saniyesi, 300'un kati) basina DB kullanicisinin
--                harcadigi CPU (milisaniye). "En cok CPU harcayan veritabanlari" (son 1 saat / 24 saat)
--                bundan okunur. db_user harf DUYARLI (utf8mb4_bin): MariaDB hesap adlari harf duyarlidir. Yalniz CPU harcayan kullanici yazilir; 2 gunden eskisi kisici tarafindan
--                silinir. domain_id: panelde TEK hostinge bagli DB kullanicisi (ayni kullanici iki
--                hostingte kayitliysa sahibi belirsizdir, olculmez ve kisilmaz). domains'e FK YOK: hosting
--                silinince satirlar sahipsiz kalir ve sorgularin domains JOIN'i onlari eler.
-- db_cpu_kisit   Uygulanan (ya da gozlem modunda UYGULANACAK olan) kisitlar. acik=1: kisit suruyor;
--                kapaninca acik NULL olur (UNIQUE(db_user, acik) => bir kullanicinin en cok BIR acik
--                kisiti olur; NULL'lar benzersizlik disindadir). Panel cokup/yeniden baslarsa acik
--                satirlar acilista bulunur ve onceki_baglanti geri yazilir (kalici kisit kalmaz).
--   seviye            1 = MAX_USER_CONNECTIONS dusuruldu; 2 = ek olarak uzun okuma sorgulari KILL
--   onceki_baglanti   kisittan once mysql.user.max_user_connections (0 = sinirsiz)
--   kisit_baglanti    uygulanan deger
--   esik_milli        asilan esik (milli-cekirdek: 1000 = 1 cekirdek)
--   tepe_milli        kisit suresince olculen en yuksek CPU (milli-cekirdek)
--   neden             hosting | bayi | hosting+bayi  (hangi esik asildi)
--   kesilen           3. seviyede KILL CONNECTION ile kesilen baglanti sayisi
--   seviye            1 baglanti siniri; 2 + uzun okuma KILL; 3 + baglantilar kesildi
--   uygulandi         0 = gozlem modu (hicbir sey degistirilmedi)
--   bitis_neden       soguma | acilis | kapali | askida | silindi | mod (gozlem kaydi uygula'ya gecince)

CREATE TABLE IF NOT EXISTS db_cpu_olcum (
  dilim      BIGINT UNSIGNED NOT NULL,
  db_user    VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  domain_id  BIGINT UNSIGNED NOT NULL,
  cpu_ms     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (dilim, db_user),
  KEY ix_dco_domain (domain_id, dilim)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS db_cpu_kisit (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  db_user         VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  domain_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
  reseller_id     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  alan_adi        VARCHAR(253) NOT NULL DEFAULT '',
  neden           VARCHAR(16) NOT NULL DEFAULT 'hosting',
  seviye          TINYINT UNSIGNED NOT NULL DEFAULT 1,
  onceki_baglanti INT UNSIGNED NOT NULL DEFAULT 0,
  kisit_baglanti  INT UNSIGNED NOT NULL DEFAULT 0,
  esik_milli      INT UNSIGNED NOT NULL DEFAULT 0,
  tepe_milli      INT UNSIGNED NOT NULL DEFAULT 0,
  oldurulen       INT UNSIGNED NOT NULL DEFAULT 0,
  kesilen         INT UNSIGNED NOT NULL DEFAULT 0,
  uygulandi       TINYINT(1) NOT NULL DEFAULT 1,
  baslangic_ts    BIGINT UNSIGNED NOT NULL,
  son_asim_ts     BIGINT UNSIGNED NOT NULL,
  bitis_ts        BIGINT UNSIGNED NULL,
  bitis_neden     VARCHAR(16) NULL,
  acik            TINYINT(1) NULL DEFAULT 1,
  UNIQUE KEY uq_dck_acik (db_user, acik),
  KEY ix_dck_domain (domain_id, baslangic_ts),
  KEY ix_dck_bayi (reseller_id, baslangic_ts),
  KEY ix_dck_baslangic (baslangic_ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
