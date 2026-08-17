-- 0072: cp_eklenti_lisans uyari kolonu — kod lisans.go:106 bunu bekliyor
ALTER TABLE cp_eklenti_lisans
    ADD COLUMN IF NOT EXISTS uyari VARCHAR(255) NOT NULL DEFAULT '' AFTER son_hata;
