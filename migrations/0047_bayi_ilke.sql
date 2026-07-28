-- 0047: Bayi paketine İLKELER (Plesk "Fazla kullanım" + "Fazla satma" karşılığı)
--
--  asim_ilkesi   : kaynak aşımına ne olacak
--                  'yok'         → aşım YASAK (limit dolunca yeni hosting/yükseltme reddedilir)
--                  'disk_trafik' → yalnız disk + trafik aşımına izin (diğer limitler sert)
--                  'tumu'        → tüm kaynaklarda aşıma izin
--  asim_bildirim : aşım durumunda yöneticiye e-posta
--  fazla_satis   : bayi sahip olduğundan FAZLA kaynak satabilir mi
--                  0 → TAAHHÜT kontrolü açık (plan kotaları toplamı ≤ bayi limiti)
--                  1 → yalnız FİİLİ kullanım sayılır (Plesk "oversell allowed")
ALTER TABLE reseller_plans
  ADD COLUMN asim_ilkesi   ENUM('yok','disk_trafik','tumu') NOT NULL DEFAULT 'disk_trafik',
  ADD COLUMN asim_bildirim TINYINT(1) NOT NULL DEFAULT 1,
  ADD COLUMN fazla_satis   TINYINT(1) NOT NULL DEFAULT 0;

-- Bayiye atanan ilkeler users'a KOPYALANIR (limitlerle aynı anlık-görüntü deseni:
-- paket sonradan değişse mevcut bayinin ilkesi kendiliğinden değişmez).
ALTER TABLE users
  ADD COLUMN asim_ilkesi   ENUM('yok','disk_trafik','tumu') NOT NULL DEFAULT 'disk_trafik',
  ADD COLUMN asim_bildirim TINYINT(1) NOT NULL DEFAULT 1,
  ADD COLUMN fazla_satis   TINYINT(1) NOT NULL DEFAULT 0;
