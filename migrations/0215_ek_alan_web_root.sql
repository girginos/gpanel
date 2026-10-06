-- 0215_ek_alan_web_root.sql
-- Ek (addon) alan adi belge koku: ana alan adindaki domains.web_root ile ayni model.
-- Bos (varsayilan) = ek alanin kendi public_html koku (<ev>/<sk>/domains/<alan>/public_html),
-- yani mevcut tum ek alanlarin davranisi DEGISMEZ. Dolu = o kokun ICINDEKI dogrulanmis
-- MUTLAK alt dizin (orn. Laravel public). Dogrulama uygulama katmaninda
-- (provisioner.WebRootMutlakTaban) yapilir, render de GuvenliWebRootTaban ile savunur.
-- Idempotent. Satir ici yorum yok ve metinde noktali virgul yok.
ALTER TABLE ek_alan_adlari ADD COLUMN IF NOT EXISTS web_root VARCHAR(255) NOT NULL DEFAULT '';
