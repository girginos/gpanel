-- 0014 - service_plans: varsayılan PHP sürümü (yeni domainler miras alır)
ALTER TABLE service_plans
  ADD COLUMN IF NOT EXISTS php_surum VARCHAR(8) NOT NULL DEFAULT '8.3';
