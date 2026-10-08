-- 0217_silinen_yedek.sql
-- Hosting/bayi silme yedeklerden BAGIMSIZDIR: backups satirlari domain silinince KALIR.
-- Eskiden backups.domain_id -> domains(id) FK'si ON DELETE CASCADE idi: domain silinince yedegin
-- DB satiri da gidiyor, yedek sayfasi o satirlara bagli oldugu icin dosya yetim kaliyordu.
-- Simdi FK kalkar. Silme aninda satirlara alan_adi, sistem_kullanici, reseller_id, silinme ve
-- son_kullanma yazilir (bkz. internal/backups/silinen.go) ve yedek YEDEK SAYFASINDAN geri yuklenir.
--
-- Yetim yedek = domain_id 0 ve silinme IS NOT NULL.
--   domain_id        silme aninda 0 yapilir (domains'te 0 id'li satir olamaz: tum JOIN domains
--                    sorgulari yetimleri kendiliginden eler, domain id'si yeniden kullanilsa bile
--                    baska musterinin yedegi sizmaz)
--   eski_domain_id   silinen domainin id'si (yetimin uzak hedef anlik goruntusunu bulmak icin)
--   alan_adi, sistem_kullanici, reseller_id   silme aninda domains tablosundan kopyalanir
--   silinme          NULL = yasayan hesabin yedegi
--   son_kullanma     NULL = otomatik silinmez (yonetici elle siler)
--
-- backup_yetim_hedefler: domaine ozel FTP/SFTP hedefi domain silinince CASCADE ile gider
-- (fk_bdest_domain). Yetim yedeklerin uzak kopyalari yine de silinebilsin diye silme aninda
-- hedefin SIFRELI ham kaydi buraya kopyalanir. Bir eski_domain_id'nin tum yetimleri bitince
-- (silindi ya da baglandi) satir silinir.
--
-- Idempotent: DROP FOREIGN KEY IF EXISTS, ADD COLUMN IF NOT EXISTS, CREATE TABLE IF NOT EXISTS ve
-- yalniz eski bicimli satirlara dokunan UPDATE.
-- Kilit etkisi: sabit varsayilanli kolonlar ayri bir ALTER'de metadata duzeyinde eklenir
-- (MariaDB 10.3 ve ustu). Indeksler ayri ALTER'lerde yerinde ve cevrimici olusur. Her ifade
-- ayri oldugundan tablo kopyalanmaz ve yazmalar bloke olmaz. Son UPDATE yalniz silinme dolu
-- satirlari hedefler (ilk calismada bile az satir).
-- Satir ici yorum YOK ve yorumda noktali virgul YOK (goc kosucusu yalniz basinda iki tire olan
-- satirlari atar ve metni noktali virgulden boler).
--
-- Bu dosyanin ilk (yayinlanmamis) surumu cp_silinen_yedek tablosunu olusturuyordu, artik yok.
DROP TABLE IF EXISTS cp_silinen_yedek;

ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain;

ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS silinme TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS son_kullanma TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0;

ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme);

ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_eski_domain (eski_domain_id);

CREATE TABLE IF NOT EXISTS backup_yetim_hedefler (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  eski_domain_id BIGINT UNSIGNED NOT NULL,
  tip            VARCHAR(8)   NOT NULL DEFAULT 'sftp',
  host           VARCHAR(253) NOT NULL,
  port           INT          NOT NULL DEFAULT 22,
  kullanici      VARCHAR(128) NOT NULL,
  parola         VARCHAR(255) NOT NULL,
  uzak_dizin     VARCHAR(255) NOT NULL DEFAULT '/',
  olusturma      TIMESTAMP    NOT NULL DEFAULT current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_yetim_hedef_domain (eski_domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

UPDATE backups b JOIN domains d ON d.id = b.domain_id
  SET b.alan_adi = d.alan_adi, b.sistem_kullanici = d.sistem_kullanici, b.reseller_id = COALESCE(d.reseller_id, 0)
  WHERE b.sistem_kullanici = '';

UPDATE backups
  SET eski_domain_id = domain_id, domain_id = 0
  WHERE domain_id > 0 AND silinme IS NOT NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id = backups.domain_id);
