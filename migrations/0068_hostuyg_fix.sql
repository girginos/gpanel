-- 0068: cp_host_uygulamalar UNIQUE(kod) -> UNIQUE(kod, ornek_ad)
-- Ayni recipe'in birden fazla instance ile kurulmasina izin ver.
--
-- IF EXISTS SART: bu goc deftere YAZILAMADIGI icin (ilk denemede
-- "Can't DROP INDEX kod" hatasi verip yarim kaliyordu) her acilista yeniden
-- kosuyor ve ayni hatayi tekrar uretiyordu. Idempotent hale getirildi.
ALTER TABLE cp_host_uygulamalar DROP INDEX IF EXISTS kod;
ALTER TABLE cp_host_uygulamalar ADD UNIQUE KEY IF NOT EXISTS unq_kod_ornek (kod, ornek_ad);
