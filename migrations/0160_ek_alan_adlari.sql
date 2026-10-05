-- 0160_ek_alan_adlari.sql
-- Ek (addon) alan adi: bir hosting hesabi (tek sistem kullanicisi) altinda
-- birden fazla alan adi (cPanel modeli). Her ek alan adi kendi docroot, nginx
-- vhost, SSL ve DNS bolgesine sahiptir. E-posta v1 kapsami disindadir.
--
-- ek_alan_adlari.alan_adi GLOBAL benzersiz (domains.alan_adi ve subdomanlar.tam_ad
-- ile capraz denetim uygulama katmaninda: domains.AdAlaniDenetle). Kucuk harf
-- normalize edilmis yazilir. Hesap silinince satirlar CASCADE gider.
--
-- ek_dns_kayitlari: ek alan adinin bolge kayitlari. dns_records ile KARISMAZ
-- (ana bolge etkilenmez). Satir ici yorum yok ve metinde noktali virgul yok:
-- goc kosucusu yalniz basinda iki tire olan satirlari atar ve metni noktali
-- virgulden boler.
CREATE TABLE IF NOT EXISTS ek_alan_adlari (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  alan_adi VARCHAR(253) NOT NULL,
  php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
  ssl_aktif TINYINT(1) NOT NULL DEFAULT 0,
  ssl_bitis DATE NULL DEFAULT NULL,
  created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_ek_alan_adi (alan_adi),
  KEY ix_ek_alan_domain (domain_id),
  CONSTRAINT fk_ek_alan_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ek_dns_kayitlari (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  ek_alan_id BIGINT UNSIGNED NOT NULL,
  ad VARCHAR(253) NOT NULL,
  tip VARCHAR(16) NOT NULL,
  deger TEXT NOT NULL,
  ttl INT NOT NULL DEFAULT 3600,
  oncelik INT NOT NULL DEFAULT 0,
  aktif TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
  KEY ix_ek_dns_alan (ek_alan_id),
  CONSTRAINT fk_ek_dns_alan FOREIGN KEY (ek_alan_id) REFERENCES ek_alan_adlari(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
