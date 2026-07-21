-- Kullanıcıya özel anasayfa (dashboard) widget düzeni — sürükle-bırak sırası (JSON metni).
-- İdempotent: her açılışta güvenle çalışır.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS dashboard_duzen TEXT NULL;
