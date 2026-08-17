-- websec bulgulari silinen domain ile birlikte CASCADE silinsin.
--
-- 🔴 FK neden kurulamiyordu: `domains.id` bigint UNSIGNED, `domain_id` ise
-- SIGNED idi. InnoDB FK'de iki tarafin tipi BIREBIR ayni olmali (errno 150).
-- Bu yuzden once kolon tipi hizalanir, sonra kisit eklenir.
ALTER TABLE cp_websec_findings MODIFY COLUMN domain_id BIGINT UNSIGNED NOT NULL;

-- Yetim satirlar kalmissa FK eklenemez — once onlari temizle.
DELETE f FROM cp_websec_findings f
  LEFT JOIN domains d ON d.id = f.domain_id
  WHERE d.id IS NULL;

ALTER TABLE cp_websec_findings
  ADD CONSTRAINT fk_websec_findings_domain
  FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE;
