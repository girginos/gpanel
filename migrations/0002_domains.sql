-- 0002 — domains tablosu (kalici, test verileri seed olarak yuklenir)
-- NOT: ssl yerine ssl_aktif (MariaDB reserved word)

CREATE TABLE IF NOT EXISTS domains (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  alan_adi         VARCHAR(253) NOT NULL UNIQUE,
  sistem_kullanici VARCHAR(64)  NOT NULL,
  php_surum        VARCHAR(8)   NOT NULL DEFAULT '8.3',
  ssl_aktif        TINYINT(1)   NOT NULL DEFAULT 0,
  ssl_bitis        DATE         NULL,
  durum            ENUM('aktif','pasif') NOT NULL DEFAULT 'aktif',
  ipv4             VARCHAR(45)  NOT NULL DEFAULT '',
  ftp_host         VARCHAR(253) NOT NULL DEFAULT '',
  ftp_user         VARCHAR(64)  NOT NULL DEFAULT '',
  db_host          VARCHAR(64)  NOT NULL DEFAULT 'localhost',
  db_user          VARCHAR(64)  NOT NULL DEFAULT '',
  db_adi           VARCHAR(64)  NOT NULL DEFAULT '',
  web_root         VARCHAR(255) NOT NULL DEFAULT '',
  boyut_kb         BIGINT       NOT NULL DEFAULT 0,
  trafik_kb        BIGINT       NOT NULL DEFAULT 0,
  is_demo          TINYINT(1)   NOT NULL DEFAULT 0,
  notlar           TEXT         NULL,
  olusturulma      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
  KEY ix_durum (durum),
  KEY ix_sistem_kullanici (sistem_kullanici)
) ENGINE=InnoDB;
