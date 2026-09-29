-- cp_websec_domain_tarama: domain BAŞINA son websec taraması.
--
-- NEDEN: uygulama envanteri "desteklenmiyor" (tarandı, uygulama yok) ile "beklemede"
-- (henüz taranmadı) ayrımını sunucu geneli cp_websec_status.last_success'ten yapıyordu.
-- Herhangi bir tarama bir kez bittiyse o tarihten sonra eklenen domainler de
-- "desteklenmiyor" görünüyordu ve bayi görünümünde sunucu geneli bir damga kiracıya
-- sızıyordu. Artık her domainin kendi kaydı var. Tarama bitince yazılır, domain
-- silinince CASCADE ile düşer. Devirde kayıt domainle birlikte gider (bayi kapsamı
-- domains.reseller_id üzerinden canlı JOIN ile çözülür, burada saklanmaz).
--
-- Idempotent: CREATE TABLE IF NOT EXISTS yeniden koşulabilir.
CREATE TABLE IF NOT EXISTS cp_websec_domain_tarama (
  domain_id  BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  son_tarama DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_websec_domain_tarama_domain
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
