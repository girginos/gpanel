-- Yedek/geri-yükleme işleri (Plesk tarzı job listesi + çoklu-domain restore + ilerleme).
CREATE TABLE IF NOT EXISTS backup_jobs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  tur VARCHAR(16) NOT NULL DEFAULT 'manuel',
  islem VARCHAR(16) NOT NULL DEFAULT 'yedek',
  durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor',
  toplam INT NOT NULL DEFAULT 0,
  tamamlanan INT NOT NULL DEFAULT 0,
  basari INT NOT NULL DEFAULT 0,
  hata INT NOT NULL DEFAULT 0,
  boyut_b BIGINT NOT NULL DEFAULT 0,
  aktif_domain VARCHAR(255) NOT NULL DEFAULT '',
  geri_mod VARCHAR(16) NOT NULL DEFAULT '',
  baslatan VARCHAR(64) NOT NULL DEFAULT '',
  detay MEDIUMTEXT,
  baslangic TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  bitis TIMESTAMP NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE backups ADD COLUMN IF NOT EXISTS job_id BIGINT NULL;
CREATE INDEX idx_backups_job ON backups (job_id);
CREATE INDEX idx_backup_jobs_durum ON backup_jobs (islem, durum);
