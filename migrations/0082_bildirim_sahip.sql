-- cp_bildirim.reseller_id: bildirim görünürlüğünü DOMAIN SAHİBİNE kilitler.
-- 🔴 İZOLASYON: kiracı zararlı-dosya bildirimi yalnız sahibine görünür; admin
-- (reseller_id IS NULL) ve başka resellerlar GÖRMEZ. bkz. internal/bildirim.kapsam
ALTER TABLE cp_bildirim
  ADD COLUMN reseller_id BIGINT UNSIGNED NULL AFTER domain_id,
  ADD KEY idx_bildirim_reseller (reseller_id);

-- Mevcut satırları sahibinden backfill et. reseller_id=0 (admin-sahipli) → NULL
-- kalır (admin görmeye devam eder). domain_id boş (panel-geneli) → NULL.
UPDATE cp_bildirim b
  JOIN domains d ON d.id = b.domain_id
  SET b.reseller_id = NULLIF(d.reseller_id, 0)
  WHERE b.domain_id IS NOT NULL;
