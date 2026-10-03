package efektiflimit

// TabloDDL — göç 0120'deki hosting_limit_ust CREATE ifadesinin BİREBİR kopyası
// (FK ayrı ALTER ifadesindedir). Yalnız testler kullanır: DB'li testler kendi
// geçici şemalarına bunu koşar. ddl_test.go göç dosyasıyla eşitliğini doğrular —
// sütun eklenince ikisi birlikte değişmek zorunda.
const TabloDDL = `CREATE TABLE IF NOT EXISTS hosting_limit_ust (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
