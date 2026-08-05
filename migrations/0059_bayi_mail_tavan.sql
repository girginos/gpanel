-- Bayi paketine POSTA TAVANLARI (domain başına üst sınır). 0 = sınırsız.
--
-- 🔴 ANLAM: bu üç kolon HAVUZ DEĞİL TAVANDIR. `max_domain`/`max_disk_mb`
-- bayinin toplam kaynağıdır (0045'te users.max_* alanlarına kopyalanır);
-- aşağıdakiler ise bayinin TEK BİR domaine atayabileceği en yüksek değerdir.
-- Bu yüzden users tablosuna KOPYALANMAZLAR — doğrulama anında reseller_plans
-- üzerinden okunur (bkz. internal/plans/plans.go bayiTavanKontrol).
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS mail_max_email INT NOT NULL DEFAULT 0;
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS mail_saatlik_limit INT NOT NULL DEFAULT 0;
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS mail_kutu_kota_mb INT NOT NULL DEFAULT 0;
