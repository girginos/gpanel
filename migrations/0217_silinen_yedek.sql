-- 0217_silinen_yedek.sql
-- Hosting/bayi silme yedeklerden BAGIMSIZDIR: backups satirlari domain silinince KALIR.
-- Eskiden backups.domain_id -> domains(id) FK'si ON DELETE CASCADE idi: domain silinince yedegin
-- DB satiri da gidiyor, yedek sayfasi o satirlara bagli oldugu icin dosya yetim kaliyordu.
-- Simdi FK kalkar. Silme aninda satirlara alan_adi, sistem_kullanici, reseller_id, iz_id, silinme ve
-- son_kullanma yazilir (bkz. internal/backups/silinen.go) ve yedek YEDEK SAYFASINDAN geri yuklenir.
--
-- Yetim yedek = domain_id 0 ve silinme IS NOT NULL.
--   domain_id        silme aninda 0 yapilir (domains'te 0 id'li satir olamaz: tum JOIN domains
--                    sorgulari yetimleri kendiliginden eler, domain id'si yeniden kullanilsa bile
--                    baska musterinin yedegi sizmaz)
--   eski_domain_id   silinen domainin id'si. YALNIZ BILGIDIR (anahtar degil): id yeniden kullanilabilir
--   iz_id            silmeye ozgu rastgele kimlik (32 hex). Yetim satirlar ve uzak hedef anlik goruntusu
--                    bu kimlikle baglanir. Canli satirlarda bos
--   alan_adi, sistem_kullanici, reseller_id   SILME ANINDA domains tablosundan yazilir. Canli satirlarda
--                    bos kalir (toplu dolgu YOK: 500 bin satirda 29 sn surup yazmalari bloke ediyordu)
--   silinme          NULL = yasayan hesabin yedegi
--   son_kullanma     NULL = otomatik silinmez (yonetici elle siler)
-- silinme ve son_kullanma DATETIME'dir. TIMESTAMP 2038-01-19'da biter: 3650 gunluk ayar 2028'den sonra
-- iz yazimini bozar, strict olmayan modda 0000-00-00 yazip yedekleri HEMEN sildirirdi.
--
-- backup_yetim_hedefler: domaine ozel FTP/SFTP hedefi domain silinince CASCADE ile gider
-- (fk_bdest_domain). Yetim yedeklerin uzak kopyalari yine de silinebilsin diye silme aninda hedefin
-- SIFRELI kaydi buraya kopyalanir. Satir iz_id ile bulunur (UNIQUE). Bir iz'in tum yetimleri bitince
-- (silindi ya da baglandi) satir silinir. Hedef bilgisi hicbir domaine geri yuklenmez.
--
-- Bu dosyanin uc surumu gorulmustur ve hepsi bu dosyayla yeni sekle gelir:
--   V1 (ilk, yayinlanmamis)  domain_id>0 + silinme dolu + domain yok. eski_domain_id, iz_id, tablo yok.
--                            Yetimler domain_id 0 ve eski_domain_id sekline donusturulur
--   V2 (ikinci)              eski_domain_id var, iz_id yok. backup_yetim_hedefler eski_domain_id UNIQUE.
--                            Mevcut anlik goruntulere iz_id olarak e + eski_domain_id verilir ve ayni
--                            eski_domain_id'li yetim satirlar ona baglanir
--   V3 (bu surum)            her ifade idempotenttir: ikinci ve ucuncu kosu veri kaybettirmez
-- backup_yetim_hedefler tablosu hicbir kosuda silinip yeniden olusturulmaz (anlik goruntu ve parolalar korunur).
--
-- Kilit ve sure etkisi (her ifade ayri calisir, tablo kopyalayan ifade YALNIZ eski gelistirme
-- veritabaninda vardir):
--   ADD COLUMN: sabit varsayilanli kolonlar tek ALTER'de eklenir. MariaDB 10.3 ve ustunde bu islem
--     ANLIKTIR (tablo yeniden yazilmaz, yazmalar yalniz kisa bir metadata kilidini bekler).
--   MODIFY COLUMN DATETIME: kolon zaten DATETIME ise bir sey yapmaz. Yalniz V1 ve V2'den gelen
--     gelistirme veritabaninda TIMESTAMP kalmistir, orada tabloyu bir kez yeniden yazar.
--   ADD KEY ... ALGORITHM=INPLACE, LOCK=NONE: indeksler yazmalar surerken CEVRIMICI kurulur. Sure satir
--     sayisiyla dogrusaldir (500 bin satirda saniyeler), yazmalar bloke olmaz. Desteklenmezse ifade
--     sessiz kopya yerine HATA verir.
--   Veri donusumu UPDATE'leri yalniz ilgili satirlara (yetimlere ve anlik goruntulu yetimlere) dokunur,
--     indeks uzerinden gider ve canli yedek satirlarini tarayip yazmaz.
-- Goc kosucusu: yalniz basi iki tireli satirlari atar ve metni noktali virgulden boler. Bu yuzden
-- satir ici yorum YOK ve yorumda noktali virgul YOK.
--
-- Bu dosyanin ilk surumu cp_silinen_yedek tablosunu olusturuyordu, artik yok.
DROP TABLE IF EXISTS cp_silinen_yedek;

ALTER TABLE backups DROP FOREIGN KEY IF EXISTS fk_backup_domain;

ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS alan_adi VARCHAR(253) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sistem_kullanici VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS reseller_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS silinme DATETIME NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS son_kullanma DATETIME NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS iz_id VARCHAR(32) NOT NULL DEFAULT '';

ALTER TABLE backups MODIFY COLUMN silinme DATETIME NULL DEFAULT NULL;

ALTER TABLE backups MODIFY COLUMN son_kullanma DATETIME NULL DEFAULT NULL;

ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_silinen (sistem_kullanici, silinme), ALGORITHM=INPLACE, LOCK=NONE;

ALTER TABLE backups ADD KEY IF NOT EXISTS ix_backups_iz (iz_id), ALGORITHM=INPLACE, LOCK=NONE;

ALTER TABLE backups DROP KEY IF EXISTS ix_backups_eski_domain;

UPDATE backups
  SET eski_domain_id = domain_id, domain_id = 0
  WHERE sistem_kullanici > '' AND silinme IS NOT NULL AND domain_id > 0
    AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id = backups.domain_id);

CREATE TABLE IF NOT EXISTS backup_yetim_hedefler (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  iz_id          VARCHAR(32)  NOT NULL DEFAULT '',
  eski_domain_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  tip            VARCHAR(8)   NOT NULL DEFAULT 'sftp',
  host           VARCHAR(253) NOT NULL,
  port           INT          NOT NULL DEFAULT 22,
  kullanici      VARCHAR(128) NOT NULL,
  parola         VARCHAR(255) NOT NULL,
  uzak_dizin     VARCHAR(255) NOT NULL DEFAULT '/',
  olusturma      DATETIME     NOT NULL DEFAULT current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_yetim_hedef_iz (iz_id),
  KEY ix_yetim_hedef_eski (eski_domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE backup_yetim_hedefler ADD COLUMN IF NOT EXISTS iz_id VARCHAR(32) NOT NULL DEFAULT '' AFTER id;

UPDATE backup_yetim_hedefler SET iz_id = CONCAT('e', eski_domain_id) WHERE iz_id = '';

UPDATE backups SET iz_id = CONCAT('e', eski_domain_id)
  WHERE domain_id = 0 AND silinme IS NOT NULL AND iz_id = '' AND eski_domain_id > 0
    AND EXISTS (SELECT 1 FROM backup_yetim_hedefler h WHERE h.iz_id = CONCAT('e', backups.eski_domain_id));

ALTER TABLE backup_yetim_hedefler DROP KEY IF EXISTS uk_yetim_hedef_domain;

ALTER TABLE backup_yetim_hedefler ADD UNIQUE KEY IF NOT EXISTS uk_yetim_hedef_iz (iz_id);

ALTER TABLE backup_yetim_hedefler ADD KEY IF NOT EXISTS ix_yetim_hedef_eski (eski_domain_id);

ALTER TABLE backup_yetim_hedefler MODIFY COLUMN olusturma DATETIME NOT NULL DEFAULT current_timestamp();
