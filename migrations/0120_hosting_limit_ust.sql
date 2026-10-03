-- 0120_hosting_limit_ust.sql
-- Özel plan = ÜST YAZIM (D1). Hosting her zaman bir KATALOG planına bağlı kalır.
-- Hostinge özel farklı limitler bu tabloda durur: her hosting için en fazla bir
-- satır, her limit sütununda NULL "plandan al" demektir. Efektif değer
-- internal/efektiflimit paketinden TEK yerden çözülür (üst yazım, sonra
-- disk/inode için domains.disk_kota_mb/inode_kota mirası, sonra plan).
--
-- kok_alanlar: yöneticinin (kök) bir bayi hostingine yazdığı alanlar, virgüllü.
-- Bayi bu alanları göremez (GL-06 sınıfı alanlarda) ve değiştiremez.
--
-- service_plans_ozel_arsiv: eski "<alan> — Özel" kopya planları dönüşümde
-- buraya satır JSON'u olarak taşınır (açık sütun listesi değil: service_plans
-- ileride sütun kazansa da arşiv kırılmaz). Arşiv bir sonraki sürüme kadar silinmez.
--
-- Satır içi yorum YOK: göç koşucusu yalnız başında iki tire olan satırları atar
-- ve metni noktalı virgülden böler. Idempotent.
CREATE TABLE IF NOT EXISTS hosting_limit_ust (
  domain_id               BIGINT UNSIGNED NOT NULL,
  disk_kota_mb            INT NULL,
  trafik_kota_mb          INT NULL,
  inode_kota              INT NULL,
  cpu_yuzde               INT NULL,
  ram_mb                  INT NULL,
  max_process             INT NULL,
  io_agirlik              INT NULL,
  pm_max_children         INT NULL,
  io_read_mbps            INT NULL,
  io_write_mbps           INT NULL,
  io_read_iops            INT NULL,
  io_write_iops           INT NULL,
  mysql_max_baglanti      INT NULL,
  db_max_queries_per_hour INT NULL,
  db_max_updates_per_hour INT NULL,
  db_max_query_seconds    INT NULL,
  max_domain              INT NULL,
  max_db                  INT NULL,
  max_ftp                 INT NULL,
  max_email               INT NULL,
  saatlik_mail_limiti     INT NULL,
  mail_kutu_kota_mb       INT NULL,
  kok_alanlar             VARCHAR(1024) NOT NULL DEFAULT '' COMMENT 'kokun kilitledigi alanlar (virgullu)',
  kaynak_ozel_plan_id     BIGINT UNSIGNED NULL COMMENT 'donusumde kaynak eski ozel plan',
  guncelleyen_user_id     BIGINT UNSIGNED NULL,
  guncellendi             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE hosting_limit_ust
  ADD CONSTRAINT fk_hlu_domain FOREIGN KEY IF NOT EXISTS (domain_id) REFERENCES domains (id) ON DELETE CASCADE;

CREATE TABLE IF NOT EXISTS service_plans_ozel_arsiv (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  kaynak_plan_id BIGINT UNSIGNED NOT NULL,
  domain_id      BIGINT UNSIGNED NULL,
  taban_plan_id  BIGINT UNSIGNED NULL,
  satir_json     LONGTEXT NOT NULL,
  rapor          VARCHAR(1024) NOT NULL DEFAULT '',
  arsivlendi     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_ozel_arsiv_kaynak (kaynak_plan_id),
  KEY ix_ozel_arsiv_domain (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
