-- FAZ 2 Attack Chain: aşama-sınıflı olay akışı + korelasyonla üretilen zincirler.
CREATE TABLE IF NOT EXISTS av_olay (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id   BIGINT UNSIGNED NULL,
  reseller_id BIGINT UNSIGNED NULL,
  kaynak      VARCHAR(16)  NOT NULL,                    -- dosya | surec | api | ag
  asama       VARCHAR(24)  NOT NULL,                    -- giris|dosya_yazma|calistirma|c2|persistence
  seviye      VARCHAR(16)  NOT NULL DEFAULT 'uyari',
  ozet        VARCHAR(255) NOT NULL DEFAULT '',
  ref_tur     VARCHAR(24)  NOT NULL DEFAULT '',
  ref_id      BIGINT       NOT NULL DEFAULT 0,
  created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_olay_dom_time (domain_id, created_at),
  KEY idx_olay_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS av_zincir (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id   BIGINT UNSIGNED NULL,
  reseller_id BIGINT UNSIGNED NULL,
  asamalar    VARCHAR(128) NOT NULL,                    -- "dosya_yazma>calistirma>c2"
  guven       INT          NOT NULL DEFAULT 0,          -- 0-100
  olay_sayisi INT          NOT NULL DEFAULT 0,
  imza        VARCHAR(64)  NOT NULL,                    -- dedup: domain+aşamalar
  created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_zincir_dom (domain_id),
  KEY idx_zincir_imza (imza),
  KEY idx_zincir_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
