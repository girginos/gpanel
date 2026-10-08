-- 0217_silinen_yedek.sql
-- Hosting/bayi silme yedeklerden BAGIMSIZDIR: yedek dizini BackupRoot/<sk> yerinde kalir, uzak
-- kopyalara dokunulmaz. Silme yalniz burada best-effort bir iz birakir; saklama suresi dolunca
-- (ya da yonetici isteyince) temizlenir.
--
-- Idempotent: yalniz CREATE TABLE IF NOT EXISTS. Satir ici yorum YOK (goc kosucusu
-- yalniz basinda iki tire olan satirlari atar ve metni noktalı virgulden boler).
--
--   son_kullanma   NULL = otomatik silinmez (yonetici elle siler)
--   dizin          sk (BackupRoot/<sk>); dizine silme sirasinda dokunulmaz
--   dosyalar       JSON dizi: silme anindaki yedek dosya adlari (uzak kopyalari silmek icin)
--   hedef_*        domaine ozel FTP/SFTP hedefinin kopyasi (domains satiri gidince
--                  backup_destinations CASCADE ile gider; uzak temizlik icin saklanir).
--                  hedef_parola gizli.SaklaBagli ile sifreli kalir, API'de donmez.
--   durum          saklaniyor | temizlendi
--   son_hata       son temizlik denemesinin hatasi (ham metin yalniz yonetici ekraninda)
CREATE TABLE IF NOT EXISTS cp_silinen_yedek (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  alan_adi         VARCHAR(253)    NOT NULL,
  sistem_kullanici VARCHAR(64)     NOT NULL,
  eski_domain_id   BIGINT UNSIGNED NOT NULL,
  reseller_id      BIGINT UNSIGNED NOT NULL DEFAULT 0,
  silinme          TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  son_kullanma     TIMESTAMP       NULL DEFAULT NULL,
  dizin            VARCHAR(255)    NOT NULL DEFAULT '',
  dosyalar         MEDIUMTEXT      NOT NULL,
  dosya_sayisi     INT UNSIGNED    NOT NULL DEFAULT 0,
  boyut_b          BIGINT          NOT NULL DEFAULT 0,
  hedef_tip        VARCHAR(8)      NOT NULL DEFAULT '',
  hedef_host       VARCHAR(253)    NOT NULL DEFAULT '',
  hedef_port       INT             NOT NULL DEFAULT 0,
  hedef_kullanici  VARCHAR(128)    NOT NULL DEFAULT '',
  hedef_parola     VARCHAR(512)    NOT NULL DEFAULT '',
  hedef_dizin      VARCHAR(255)    NOT NULL DEFAULT '',
  durum            VARCHAR(12)     NOT NULL DEFAULT 'saklaniyor',
  son_hata         VARCHAR(1000)   NOT NULL DEFAULT '',
  temizlenme       TIMESTAMP       NULL DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_silinen_yedek_durum (durum, son_kullanma)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
