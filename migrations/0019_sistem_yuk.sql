-- Sistem yük (load average) geçmişi — dashboard grafiği için periyodik örnekleme
CREATE TABLE IF NOT EXISTS sistem_yuk (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  yuk1 FLOAT NOT NULL DEFAULT 0,
  yuk5 FLOAT NOT NULL DEFAULT 0,
  yuk15 FLOAT NOT NULL DEFAULT 0,
  bellek_yuzde FLOAT NOT NULL DEFAULT 0,
  KEY ix_sy_ts (ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
