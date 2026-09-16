-- Eklenti nav (sol menu) metadata'si — plugin sekmeleri artik DashboardLayout'ta
-- HARDCODED degil: plugin kendi nav bilgisini cp_eklentiler'e yazar, DashboardLayout
-- dinamik kurar (Asama 2). nav_yol='' => top-level sekme yok (per-domain / UI'siz).
ALTER TABLE cp_eklentiler ADD COLUMN IF NOT EXISTS nav_yol  VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE cp_eklentiler ADD COLUMN IF NOT EXISTS nav_grup VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE cp_eklentiler ADD COLUMN IF NOT EXISTS nav_ikon VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE cp_eklentiler ADD COLUMN IF NOT EXISTS nav_sira INT NOT NULL DEFAULT 100;

-- Mevcut kurulumlarda bilinen top-level nav'li eklentileri doldur (yalniz bossa).
UPDATE cp_eklentiler SET nav_yol='/mail-sunucu', nav_grup='Sunucu Yonetimi', nav_ikon='mail',  nav_sira=60 WHERE ad='mail'       AND nav_yol='';
UPDATE cp_eklentiler SET nav_yol='/marka',       nav_grup='Sunucu Yonetimi', nav_ikon='marka', nav_sira=20 WHERE ad='whitelabel' AND nav_yol='';
