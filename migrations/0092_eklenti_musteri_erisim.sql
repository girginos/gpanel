-- Eklenti bazinda MUSTERI erisimi.
--
-- Eklenti proxy'si bugune kadar yalniz admin/reseller'a aciktri. Bazi eklentiler
-- (ornegin Uygulama Calistirici) dogrudan hosting sahibinin kullanacagi bir
-- yuzeydir; bazilari (mail sunucu yonetimi) yonetici yuzeyidir.
--
-- Blanket bir aciklama TUM eklentileri musteriye acardi. Bu bayrak, her
-- eklentinin kendi kararini tasir ve VARSAYILAN KAPALIDIR (mevcut eklentiler
-- oldugu gibi kalir).
ALTER TABLE cp_eklentiler
  ADD COLUMN musteri_erisim TINYINT(1) NOT NULL DEFAULT 0
  COMMENT 'musteri rolu bu eklentiye erisebilir mi (0=yalniz admin/reseller)';
