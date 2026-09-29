-- 0113_yedek_uzak_hedefler.sql
-- Yedek altyapısı: kökün tanımladığı UZAK hedefler (S3 ve uyumlular, WebDAV, SMB,
-- Dropbox, Google Drive), her yedeğin uzak kopyalarının kalıcı kaydı/kuyruğu ve
-- age şifreleme anahtarları. Mevcut backup_destinations (domain FTP/SFTP) ve
-- backup_genel_ayar (sistem geneli FTP/SFTP) AYNEN kalır.
--
-- Idempotent: yalnız CREATE TABLE IF NOT EXISTS (defter devre dışı kalırsa göç
-- her açılışta yeniden koşar). Satır içi yorum YOK: göç koşucusu yalnız başında
-- iki tire olan satırları atar ve metni noktalı virgülden böler.
--
-- backup_hedefler
--   tur           kayıt defterindeki hedef türü (s3, webdav, smb, dropbox, drive)
--   ayar          SIRSIZ JSON (uç nokta, kova, yol, kullanıcı adı ...)
--   sirlar        gizli.SaklaBagli ile şifreli JSON (parola, gizli anahtar, yenileme
--                 jetonu). Hiçbir API yanıtında dönmez.
--   sahip_tur/id  v1 yalnız kok/0 (sunucu geneli, tüm domainlerin yedekleri)
--   sifreleme     age (uzak yedekler zorunlu şifreli)
--   bant_kbps     yükleme hız sınırı KiB/sn, 0 = sınırsız
--   ic_ag_izni    1 = kökün AÇIK onayıyla özel ağ adresleri (metadata her durumda red)
--   son_durum, hata_kategori  yalnız kategori (ham sağlayıcı metni DB'ye yazılmaz)
CREATE TABLE IF NOT EXISTS backup_hedefler (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ad             VARCHAR(64)     NOT NULL,
  tur            VARCHAR(16)     NOT NULL,
  ayar           TEXT            NOT NULL,
  sirlar         TEXT            NOT NULL,
  sahip_tur      VARCHAR(8)      NOT NULL DEFAULT 'kok',
  sahip_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
  aktif          TINYINT(1)      NOT NULL DEFAULT 1,
  sifreleme      VARCHAR(8)      NOT NULL DEFAULT 'age',
  bant_kbps      INT UNSIGNED    NOT NULL DEFAULT 0,
  ic_ag_izni     TINYINT(1)      NOT NULL DEFAULT 0,
  son_durum      VARCHAR(16)     NOT NULL DEFAULT '',
  hata_kategori  VARCHAR(16)     NOT NULL DEFAULT '',
  son_deneme     TIMESTAMP       NULL DEFAULT NULL,
  son_basari     TIMESTAMP       NULL DEFAULT NULL,
  created_at     TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     TIMESTAMP       NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_backup_hedef_ad (sahip_tur, sahip_id, ad),
  CONSTRAINT ck_backup_hedef_ayar CHECK (JSON_VALID(ayar)),
  CONSTRAINT ck_backup_hedef_sahip CHECK (sahip_tur IN ('kok', 'bayi', 'musteri'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- backup_uzak_kopyalar
--   kaynak          genel (backup_genel_ayar) | domain (backup_destinations) | hedef (backup_hedefler)
--   hedef_id        yalnız kaynak hedef iken dolu. Hedef silinince kopya kayıtları da silinir
--                   (uzaktaki dosyalar kökün deposunda kalır).
--   hedef_anahtar   COALESCE(hedef_id, 0): NULL değerler UNIQUE'te birbirine eşit
--                   sayılmadığı için genel/domain kaynaklı kopyanın tekilliği bununla sağlanır.
--   uzak_ad         hedef tabanına göre göreli ad (hedef.AdDogrula, en çok 512 bayt)
--   durum           bekliyor | yukleniyor | tamam | hata | siliniyor
--   deneme, sonraki_deneme  kalıcı kuyruk (yeniden deneme zamanı)
--   sha256_sifreli, anahtar_parmak  şifreli dosyanın özeti ve şifreleyen anahtarın parmak izi
--   son_dogrulama   haftalık rastgele uzak kopya doğrulamasının son zamanı
CREATE TABLE IF NOT EXISTS backup_uzak_kopyalar (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  backup_id       BIGINT UNSIGNED NOT NULL,
  kaynak          VARCHAR(8)      NOT NULL,
  hedef_id        BIGINT UNSIGNED NULL DEFAULT NULL,
  hedef_anahtar   BIGINT UNSIGNED AS (COALESCE(hedef_id, 0)) PERSISTENT,
  uzak_ad         VARCHAR(512)    NOT NULL DEFAULT '',
  durum           VARCHAR(16)     NOT NULL DEFAULT 'bekliyor',
  deneme          INT UNSIGNED    NOT NULL DEFAULT 0,
  sonraki_deneme  TIMESTAMP       NULL DEFAULT NULL,
  boyut_b         BIGINT          NOT NULL DEFAULT 0,
  sha256_sifreli  CHAR(64)        NOT NULL DEFAULT '',
  anahtar_parmak  VARCHAR(64)     NOT NULL DEFAULT '',
  hata_kategori   VARCHAR(16)     NOT NULL DEFAULT '',
  son_dogrulama   TIMESTAMP       NULL DEFAULT NULL,
  created_at      TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      TIMESTAMP       NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_uzak_kopya (backup_id, kaynak, hedef_anahtar),
  KEY idx_uzak_kopya_kuyruk (durum, sonraki_deneme),
  KEY idx_uzak_kopya_hedef (hedef_id),
  CONSTRAINT fk_uzak_kopya_backup FOREIGN KEY (backup_id) REFERENCES backups (id) ON DELETE CASCADE,
  CONSTRAINT fk_uzak_kopya_hedef FOREIGN KEY (hedef_id) REFERENCES backup_hedefler (id) ON DELETE CASCADE,
  CONSTRAINT ck_uzak_kopya_kaynak CHECK (kaynak IN ('genel', 'domain', 'hedef')),
  CONSTRAINT ck_uzak_kopya_hedef CHECK ((kaynak = 'hedef') = (hedef_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- backup_sifre_anahtarlari
--   parmak          age alıcısının (genel anahtar) SHA-256 parmak izi, küçük harf hex
--   alici           age genel anahtarı (age1...), gizli değil
--   gizli_anahtar   gizli.SaklaBagli(AGE-SECRET-KEY-1..., parmak) ile parmağa bağlı şifreli
--   durum           bekliyor (kurtarma dosyası onaylanmadı, şifreleme ETKİN DEĞİL) | etkin | emekli
--                   emekli anahtar yalnız eski yedekleri çözmek için tutulur
--   etkin_tekil     yalnız durum etkin iken 1, diğerlerinde NULL: UNIQUE ile aynı anda
--                   en çok BİR etkin anahtar
--   kurtarma_indirildi, kurtarma_onay  kök kurtarma dosyasını indirdi ve "kaydettim" dedi
CREATE TABLE IF NOT EXISTS backup_sifre_anahtarlari (
  id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  parmak              VARCHAR(64)     NOT NULL,
  alici               VARCHAR(128)    NOT NULL,
  gizli_anahtar       TEXT            NOT NULL,
  durum               VARCHAR(16)     NOT NULL DEFAULT 'bekliyor',
  etkin_tekil         TINYINT UNSIGNED AS (IF(durum = 'etkin', 1, NULL)) PERSISTENT,
  kurtarma_indirildi  TIMESTAMP       NULL DEFAULT NULL,
  kurtarma_onay       TIMESTAMP       NULL DEFAULT NULL,
  created_at          TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_sifre_anahtar_parmak (parmak),
  UNIQUE KEY uk_sifre_anahtar_etkin (etkin_tekil),
  KEY idx_sifre_anahtar_durum (durum),
  CONSTRAINT ck_sifre_anahtar_durum CHECK (durum IN ('bekliyor', 'etkin', 'emekli'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
