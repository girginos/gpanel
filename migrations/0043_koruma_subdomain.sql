-- 0043_koruma_subdomain.sql — subdomain kapsamli sifre-koruma (.htpasswd)
-- subdomain_id=0 → domain-seviyesi; >0 → ilgili subdomain. UNIQUE key subdomain_id icerir
-- ki ayni yol/kullanici hem domain hem subdomain icin cakismasin.
ALTER TABLE korumali_dizinler ADD COLUMN IF NOT EXISTS subdomain_id INT NOT NULL DEFAULT 0;
ALTER TABLE korumali_dizinler DROP INDEX IF EXISTS uq_kd;
ALTER TABLE korumali_dizinler ADD UNIQUE KEY IF NOT EXISTS uq_kd_sub (domain_id, subdomain_id, yol, kullanici);
ALTER TABLE korumali_dizinler ADD INDEX IF NOT EXISTS ix_kd_sub (subdomain_id);
