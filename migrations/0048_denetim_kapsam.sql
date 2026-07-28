-- 0048: Denetim kaydi KAPSAMI + oturum gecersizlestirme + aski kaynagi
--
-- audit_log.reseller_id : kaydin ait oldugu kiraci.
--     0  → kok (panel sahibi) islemi / global kaynak
--     >0 → ilgili bayinin users.id'si. Bayi YALNIZ kendi kapsamini gorur,
--          kok hepsini gorur. Kayit, islemi YAPANIN degil ETKILENEN hesabin
--          kapsamina yazilir: kok bir bayinin domainini askiya alirsa bayi bunu
--          kendi kaydinda gorur (seffaflik).
--
-- users.token_gecersiz_ts : bu zaman damgasindan ONCE uretilmis JWT'ler
--     reddedilir. Askiya alma / parola degisimi / silme bunu guncelleyerek
--     ACIK OTURUMLARI aninda dusurur (JWT omru 8 saat, beklemek kabul edilemez).
--
-- domains.askida_kaynak : askinin kaynagi. 'manuel' = tek tek askiya alindi,
--     'bayi' = bayisi askiya alindigi icin zincirleme askiya alindi. Bayi geri
--     acilinca YALNIZ 'bayi' kaynakli olanlar geri gelir (manuel aski korunur).
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0;
ALTER TABLE audit_log ADD INDEX IF NOT EXISTS ix_audit_kapsam (reseller_id, id);
ALTER TABLE users   ADD COLUMN IF NOT EXISTS token_gecersiz_ts BIGINT NOT NULL DEFAULT 0;
ALTER TABLE domains ADD COLUMN IF NOT EXISTS askida_kaynak VARCHAR(8) NOT NULL DEFAULT '';
-- 🔴 GERI-DOLDURMA YOK.
-- Ilk tasarimda gecmis kayitlar alan adi / kullanici adi eslestirilerek kiracilara
-- dagitiliyordu. Ikisi de ZAMAN ICINDE YENIDEN KULLANILABILIR kimlikler:
-- "ornek.com" A bayisinde acilip silinmis, sonra B bayisinde yeniden acilmissa,
-- A donemine ait kayitlar B'nin kapsamina yaziliyor ve B baskasinin gecmisini
-- okuyordu (ayni sey silinip ayni adla acilan bayi icin de gecerli).
-- Gecmis kayitlar reseller_id=0'da kalir: kok hepsini gorur, hicbir bayi
-- baskasinin gecmisini gormez. Yeni kayitlar zaten dogru kapsamla yaziliyor.
UPDATE domains SET askida_kaynak='manuel' WHERE askida=1 AND askida_kaynak='';
