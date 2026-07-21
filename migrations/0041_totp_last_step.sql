-- 2FA (TOTP) replay koruması: en son başarıyla kullanılan 30sn zaman-adımı.
-- Aynı kodun tekrar kullanımını (replay) engellemek için login/enable bunu günceller.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS totp_last_step BIGINT NOT NULL DEFAULT 0;
