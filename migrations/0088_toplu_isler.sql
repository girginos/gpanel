-- 0088 - Domain toplu islemleri (async is + ilerleme). Sayfa bloklamadan (process bar)
-- calisan toplu DNS reset / SSL kurulumu / sahiplik / plan degisimi icin.
CREATE TABLE IF NOT EXISTS cp_toplu_isler (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  tip VARCHAR(24) NOT NULL,                       -- dns_reset | ssl_kur | sahip | plan
  durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor', -- calisiyor | tamam | kismi | hata | iptal
  toplam INT NOT NULL DEFAULT 0,
  tamamlanan INT NOT NULL DEFAULT 0,
  basari INT NOT NULL DEFAULT 0,
  hata INT NOT NULL DEFAULT 0,
  aktif_domain VARCHAR(255) NOT NULL DEFAULT '',
  parametre TEXT,
  detay MEDIUMTEXT,
  baslatan VARCHAR(64) NOT NULL DEFAULT '',
  baslangic TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  bitis TIMESTAMP NULL,
  KEY ix_toplu_durum (durum, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
