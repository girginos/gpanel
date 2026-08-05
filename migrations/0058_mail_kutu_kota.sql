-- Plan bazlı posta kutusu DEPOLAMA kotası (kutu başına MB). 0 = sınırsız.
--
-- `saatlik_mail_limiti` (0054) giden mail ADEDİNİ sınırlar; bu kolon ise her
-- posta kutusunun DİSK kullanımını sınırlar. İkisi farklı şeydir.
--
-- Enforcement mail eklentisindedir (Dovecot quota eklentisi + virtual_users.
-- quota_bytes). Panel yalnız plan değerini tutar ve eklentiye senkronlar —
-- 🔴 yalnız arayüzde sayı göstermek kota UYGULAMAK değildir.
ALTER TABLE service_plans ADD COLUMN IF NOT EXISTS mail_kutu_kota_mb INT NOT NULL DEFAULT 0;
