-- 0200_trafik_imlec_dosya.sql
-- Ek alan adi ve alt alan access log'lari da hesabin aylik trafigine sayilir.
-- Her log dosyasinin okuma imleci (ofset+boyut) ayri satirdir: anahtar (domain_id, log_ad).
-- Ana alan adi imleci eski tabloda (domain_trafik_imlec) kalir. Hesap silinince CASCADE.
CREATE TABLE IF NOT EXISTS domain_trafik_imlec_dosya (
  domain_id BIGINT UNSIGNED NOT NULL,
  log_ad VARCHAR(253) NOT NULL,
  ofset BIGINT NOT NULL DEFAULT 0,
  boyut BIGINT NOT NULL DEFAULT 0,
  guncelleme TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (domain_id, log_ad),
  CONSTRAINT fk_trafik_imlec_dosya_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
