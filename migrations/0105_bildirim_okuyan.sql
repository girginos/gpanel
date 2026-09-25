-- cp_bildirim_okundu: bildirimin "okundu" durumu artık GÖRÜNTÜLEYENE göre.
--
-- 🔴 NEDEN: bir domain bildirimini hem müşteri hem sahip (bayi ya da yönetici)
-- görür, ama okundu cp_bildirim.okundu adlı TEK bir sütundu. Müşteri "tümünü
-- okundu" deyince bayinin/yöneticinin "Yedek başarısız" ve "Saldırı zinciri"
-- rozetleri de düşüyordu (ele geçirilmiş bir müşteri hesabı sahibin kritik
-- uyarısını susturabiliyordu). Artık her görüntüleyenin kendi kaydı var.
--   okuyan_tur: admin | bayi | musteri
--   okuyan_id : admin → users.id, bayi → users.id, musteri → domains.id
-- cp_bildirim.okundu KALIR, anlamı "en az bir görüntüleyen okudu" olur (AV
-- toplama gibi "satır kapandı mı" diye bakan yazıcılar için). bkz. internal/bildirim.
--
-- Idempotent: her ifade yeniden koşulabilir (defter devre dışı kalırsa göçler
-- her açılışta yeniden koşar).
CREATE TABLE IF NOT EXISTS cp_bildirim_okundu (
  bildirim_id BIGINT UNSIGNED NOT NULL,
  okuyan_tur  VARCHAR(8)      NOT NULL,
  okuyan_id   BIGINT UNSIGNED NOT NULL,
  okundu_at   TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (bildirim_id, okuyan_tur, okuyan_id),
  KEY idx_bildirim_okuyan (okuyan_tur, okuyan_id),
  CONSTRAINT fk_bildirim_okundu_bildirim FOREIGN KEY (bildirim_id)
    REFERENCES cp_bildirim (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Alan adı "Ana hesap (admin)"e devredilince eski kod açık bildirimlere
-- reseller_id=0 yazıyordu. Yöneticinin kapsamı NULL olduğu için bu satırlar
-- yöneticiden de eski bayiden de kayboluyordu. 0 = ana hesap → NULL.
UPDATE cp_bildirim SET reseller_id = NULL WHERE reseller_id = 0;

-- Mevcut okundu=1 satırlar, o satırı BUGÜN gören HERKES için okundu sayılır:
-- yönetici (reseller_id NULL, uid=1 — yönetici jetonu hep uid=1 ile üretilir),
-- sahip bayi (reseller_id>0) ve domainin müşterisi (domain_id dolu). Kimin
-- okuduğu bilinmiyor; böylece göç anında hiçbir rolün ekranı değişmez (eski
-- bildirimler kimsede yeniden "okunmamış" belirmez). Ayrışma bundan sonraki
-- okumalarla başlar. okundu=0 satırlar herkes için okunmamış kalır.
--
-- 🔴 YALNIZ tablo boşken (ilk geçiş): yeni kod okundu sütununu "biri okudu"
-- anlamında yazmaya devam eder. Göç yeniden koşarsa bu bayrağı tüm
-- görüntüleyenlere YAYMAMALI (yoksa hata geri gelir). Tek INSERT: koşul bir
-- kez değerlendirilir.
INSERT IGNORE INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id)
SELECT g.bildirim_id, g.okuyan_tur, g.okuyan_id FROM (
    SELECT b.id AS bildirim_id, 'admin' AS okuyan_tur, 1 AS okuyan_id
      FROM cp_bildirim b WHERE b.okundu = 1 AND b.reseller_id IS NULL
  UNION ALL
    SELECT b.id, 'bayi', b.reseller_id
      FROM cp_bildirim b WHERE b.okundu = 1 AND b.reseller_id > 0
  UNION ALL
    SELECT b.id, 'musteri', b.domain_id
      FROM cp_bildirim b WHERE b.okundu = 1 AND b.domain_id IS NOT NULL
) g
WHERE NOT EXISTS (SELECT 1 FROM cp_bildirim_okundu k);
