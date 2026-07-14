-- Antivirüs / zararlı yazılım tarama sonuçları
CREATE TABLE IF NOT EXISTS av_taramalar (
  id INT AUTO_INCREMENT PRIMARY KEY,
  domain_id INT NOT NULL,
  baslangic TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  bitis TIMESTAMP NULL,
  durum VARCHAR(16) NOT NULL DEFAULT 'calisiyor',
  taranan INT NOT NULL DEFAULT 0,
  enfekte INT NOT NULL DEFAULT 0,
  motor VARCHAR(48) NOT NULL DEFAULT '',
  KEY ix_avt_dom (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS av_bulgular (
  id INT AUTO_INCREMENT PRIMARY KEY,
  tarama_id INT NOT NULL,
  domain_id INT NOT NULL,
  dosya VARCHAR(512) NOT NULL,
  imza VARCHAR(255) NOT NULL,
  motor VARCHAR(32) NOT NULL DEFAULT '',
  karantina TINYINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY ix_avb_tarama (tarama_id),
  KEY ix_avb_dom (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
