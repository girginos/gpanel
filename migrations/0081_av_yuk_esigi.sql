-- Dinamik yük throttling eşiği (çekirdek yüzdesi; 0=kapalı).
ALTER TABLE av_ayarlar ADD COLUMN IF NOT EXISTS yuk_esigi INT NOT NULL DEFAULT 0;
