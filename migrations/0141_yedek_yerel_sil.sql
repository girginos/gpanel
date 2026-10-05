-- 0141_yedek_yerel_sil.sql
-- Drive uzak kopyası doğrulanınca YEREL yedek dosyasını silme seçeneği. 0140 tabloları AYNEN
-- kullanılır, bu göç yalnız iki ayar kolonu ve bir işaret kolonu ekler.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS, DROP CONSTRAINT IF EXISTS. Satır içi yorum YOK ve
-- metinde noktalı virgül YOK: göç koşucusu yalnız başında iki tire olan satırları atar ve
-- metni noktalı virgülden böler.
--
-- backup_uzak_kopya_ayar
--   yerel_sil   0 = yerel kopya hiç silinmez (VARSAYILAN, mevcut davranış değişmez)
--   yerel_tut   her alan adı için yerelde tutulacak EN YENİ kök yedek sayısı (0-10, varsayılan 1)
--
-- backups
--   yerel_yok   yerel dosya Drive kopyası doğrulandığı için silindi. NULL = silinmedi.
--               Kayıt SİLİNMEZ: yedek Drive kopyasına bağlı olarak listede kalır.
ALTER TABLE backup_uzak_kopya_ayar
  ADD COLUMN IF NOT EXISTS yerel_sil TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS yerel_tut INT UNSIGNED NOT NULL DEFAULT 1;

ALTER TABLE backup_uzak_kopya_ayar DROP CONSTRAINT IF EXISTS ck_uzak_ayar_yerel_tut;

ALTER TABLE backup_uzak_kopya_ayar ADD CONSTRAINT ck_uzak_ayar_yerel_tut CHECK (yerel_tut <= 10);

ALTER TABLE backups ADD COLUMN IF NOT EXISTS yerel_yok TIMESTAMP NULL DEFAULT NULL;
