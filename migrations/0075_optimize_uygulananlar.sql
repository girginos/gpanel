-- Sunucu Optimize: bir yedege ait UYGULANAN parametreler (param=deger, ...).
-- 0073 defterde kayitli oldugu icin duzenlenemez; kolon ayri gocte eklenir.
ALTER TABLE cp_optimize_yedekler
  ADD COLUMN IF NOT EXISTS uygulananlar TEXT NULL AFTER aciklama;
