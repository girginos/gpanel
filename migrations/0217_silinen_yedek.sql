-- 0217_silinen_yedek.sql
-- Hosting/bayi silme yedeklerden BAGIMSIZDIR: backups satirlari domain silinince KALIR.
-- Eskiden backups.domain_id -> domains(id) FK'si ON DELETE CASCADE idi: domain silinince yedegin
-- DB satiri da gidiyor, yedek sayfasi o satirlara bagli oldugu icin dosya yetim kaliyordu.
-- Simdi FK kalkar. Silme aninda satirlara alan_adi, sistem_kullanici, reseller_id, silinme ve
-- son_kullanma yazilir (bkz. internal/backups/silinen.go) ve yedek YEDEK SAYFASINDAN geri yuklenir.
--
-- Yetim yedek = silinme IS NOT NULL ve domains'te domain_id karsiligi yok.
--   alan_adi, sistem_kullanici, reseller_id   silme aninda domains tablosundan kopyalanir
--   silinme        NULL = yasayan hesabin yedegi
--   son_kullanma   NULL = otomatik silinmez (yonetici elle siler)
--
-- Idempotent: DROP FOREIGN KEY IF EXISTS, ADD COLUMN IF NOT EXISTS ve yalniz bos kayitlari dolduran UPDATE.
-- Satir ici yorum YOK ve yorumda noktali virgul YOK (goc kosucusu yalniz basinda iki tire olan
-- satirlari atar ve metni noktali virgulden boler).
--
-- Bu dosyanin ilk (yayinlanmamis) surumu cp_silinen_yedek tablosunu olusturuyordu, artik yok.
DROP TABLE IF EXISTS cp_silinen_yedek;

ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain;

ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS silinme TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS son_kullanma TIMESTAMP NULL DEFAULT NULL,
  ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme);

UPDATE backups b JOIN domains d ON d.id = b.domain_id
  SET b.alan_adi = d.alan_adi, b.sistem_kullanici = d.sistem_kullanici, b.reseller_id = COALESCE(d.reseller_id, 0)
  WHERE b.sistem_kullanici = '';
