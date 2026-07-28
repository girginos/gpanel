-- 0046: Domaine-ÖZEL planları katalogdan ayır.
--
-- 🔴 Sorun: "tek tıkla özel plan" her hosting için yeni bir service_plans satırı
-- üretiyordu ve bunlar "Hizmet Planları" kataloğunda listeleniyordu → 100 domain
-- özelleştirilirse katalogda 100 çöp plan.
--
-- Çözüm: domain_id ile işaretle. domain_id IS NOT NULL olanlar KATALOGDA GÖRÜNMEZ;
-- yalnız sahibi olan hostingin plan sayfasında görünür ve o hosting başka plana
-- geçtiğinde / silindiğinde otomatik temizlenir.
ALTER TABLE service_plans
  ADD COLUMN domain_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT 'dolu ise: yalnız bu domaine ait özel plan (katalogda listelenmez)';

CREATE INDEX ix_service_plans_domain ON service_plans (domain_id);

-- Geriye dönük: mevcut "<alan_adı> — Özel…" planlarını sahibi domaine bağla.
UPDATE service_plans p
  JOIN domains d ON p.ad LIKE CONCAT(d.alan_adi, ' — Özel%')
  SET p.domain_id = d.id
  WHERE p.domain_id IS NULL;
