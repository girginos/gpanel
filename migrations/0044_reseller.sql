-- 0044_reseller.sql — Reseller hosting sistemi Faz 1: kimlik + izolasyon anahtarlari
-- reseller = users satiri (role='reseller'); sahip oldugu kaynaklarin scope-anahtari = kendi users.id.
-- reseller_id=0 => admin/global (dogrudan admin-sahipli veya global sablon/plan).

-- Reseller genel kotalari (yalniz role='reseller' satirlari icin anlamli; 0 = limitsiz)
ALTER TABLE users ADD COLUMN IF NOT EXISTS max_domain INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS max_disk_mb BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS max_trafik_mb BIGINT NOT NULL DEFAULT 0;

-- Sahiplik/izolasyon anahtarlari
ALTER TABLE domains ADD COLUMN IF NOT EXISTS reseller_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE service_plans ADD COLUMN IF NOT EXISTS reseller_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE dns_template ADD COLUMN IF NOT EXISTS reseller_id BIGINT NOT NULL DEFAULT 0;

ALTER TABLE domains ADD INDEX IF NOT EXISTS ix_dom_reseller (reseller_id);
ALTER TABLE service_plans ADD INDEX IF NOT EXISTS ix_sp_reseller (reseller_id);
ALTER TABLE dns_template ADD INDEX IF NOT EXISTS ix_dt_reseller (reseller_id);
