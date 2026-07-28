-- 0050: Silinmis bayilere ait denetim kapsamlarini KOK'e tasi.
-- Bayi silme akisi artik audit_log.reseller_id'yi 0'a cekiyor, ama bu duzeltmeden
-- ONCE silinmis bayilerin kayitlari eski ID'de duruyordu. users.AUTO_INCREMENT
-- o araliga geri donerse yeni bayi, eskisinin gecmisini kendi kaydinda gorurdu.
UPDATE audit_log a LEFT JOIN users u ON u.id = a.reseller_id
   SET a.reseller_id = 0
 WHERE a.reseller_id > 0 AND u.id IS NULL;
