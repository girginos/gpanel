-- 0091_backup_genel_ayar.sql
-- Sistem geneli yedek ayarlari (tekil satir id=1):
--   * ana salter (aktif=0 -> hicbir otomatik yedek alinmaz)
--   * disk korumasi (min_bos_gb / max_depo_gb esikleri)
--   * SISTEM GENELI uzak hedef (backup_destinations SADECE domain bazliydi:
--     domain_id NOT NULL + UNIQUE uk_domain -> global hedef semasal olarak imkansizdi)
CREATE TABLE IF NOT EXISTS backup_genel_ayar (
  id             tinyint(3) unsigned NOT NULL DEFAULT 1,
  aktif          tinyint(4)   NOT NULL DEFAULT 1,
  min_bos_gb     int(11)      NOT NULL DEFAULT 10,
  max_depo_gb    int(11)      NOT NULL DEFAULT 0,
  uzak_aktif     tinyint(4)   NOT NULL DEFAULT 0,
  uzak_tip       varchar(8)   NOT NULL DEFAULT "sftp",
  uzak_host      varchar(253) NOT NULL DEFAULT "",
  uzak_port      int(11)      NOT NULL DEFAULT 22,
  uzak_kullanici varchar(128) NOT NULL DEFAULT "",
  uzak_parola    varchar(255) NOT NULL DEFAULT "",
  uzak_dizin     varchar(255) NOT NULL DEFAULT "/",
  uzak_yerel_sil tinyint(4)   NOT NULL DEFAULT 0,
  son_yukleme    timestamp    NULL DEFAULT NULL,
  son_durum      varchar(32)  NOT NULL DEFAULT "",
  son_hata       varchar(512) NOT NULL DEFAULT "",
  updated_at     timestamp    NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO backup_genel_ayar (id) VALUES (1);
