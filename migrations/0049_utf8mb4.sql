-- 0049: utf8mb4 — emoji/4-bayt karakterler SESSIZCE kayboluyordu.
-- Baglanti zaten utf8mb4 (PANEL_DB_DSN) ama tablolar/kolonlar eski charset'te
-- kaldigi icin MariaDB '??' yazip HATA DONDURMUYORDU (sessiz veri kaybi).
-- Kullanici metni tutan tablolar utf8mb4_unicode_ci'ye cevrilir.
ALTER TABLE users      CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE domains    CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE audit_log  CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE service_plans  CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE reseller_plans CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
