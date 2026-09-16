-- WP kurulumda üretilen admin parolası artık kurulum YANITINDA dönmez (CWE-200);
-- şifreli saklanır (gizli.SaklaBagli) ve sahip "iste-göster" ucundan bir kez alır.
-- hedef = mutlak kurulum dizini (kurulum kimliği; alt-dizin/alt-alan kurulumları ayrışır).
CREATE TABLE IF NOT EXISTS cp_wp_kurulum (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id       BIGINT UNSIGNED NOT NULL,
  hedef           VARCHAR(500) NOT NULL,
  admin_kullanici VARCHAR(191) NOT NULL,
  admin_parola    VARCHAR(255) NOT NULL,
  olusturulma     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_domain_hedef (domain_id, hedef),
  KEY k_domain (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
