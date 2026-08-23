-- 0089: Sahiplik devri güvenliği.
-- Müşteri token'ı (ftp_accounts tabanlı) şimdiye dek SUNUCU TARAFINDA iptal
-- edilemiyordu: imza geçerliyse 24 saat boyunca kabul ediliyordu. Devir,
-- askıya alma ve parola değişimi bu yüzden mevcut oturumları düşürmüyordu.
ALTER TABLE ftp_accounts ADD COLUMN IF NOT EXISTS token_gecersiz_ts BIGINT NOT NULL DEFAULT 0;
