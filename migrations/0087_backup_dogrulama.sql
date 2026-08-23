-- 0087 - Yedek bütünlük doğrulama.
-- sha256: her yedeğin oluşturma-anı checksum'ı (at-rest bit-rot / bozuk uzak-kopya taraması).
-- dogrulama: '' | 'ok' (oluşturuldu+gzip -t geçti) | 'bozuk' (at-rest tarama uyuşmazlık buldu).
-- Bozuk/başarısız/eksik-veri yedekte KRİTİK bildirim gönderilir (bkz. internal/backups).
ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS sha256 CHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS dogrulama VARCHAR(16) NOT NULL DEFAULT '';
