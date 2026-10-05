-- 0140_yedek_uzak_kopya.sql
-- Yedek uzak kopya orkestratörü (Y1). 0113 tabloları AYNEN kullanılır:
-- backup_uzak_kopyalar = iş kuyruğu + manifest kaydı (kaynak='hedef'),
-- backup_sifre_anahtarlari = age alıcıları. Bu göç yalnız eksikleri ekler.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS, DROP CONSTRAINT IF EXISTS, CREATE TABLE
-- IF NOT EXISTS, INSERT IGNORE. Satır içi yorum YOK ve metinde noktalı virgül
-- YOK: göç koşucusu yalnız başında iki tire olan satırları atar ve metni
-- noktalı virgülden böler.
--
-- backup_uzak_kopya_ayar (tek satır, id=1) VARSAYILAN KAPALI
--   aktif        0 = uzak kopya hiç gönderilmez (mevcut yedek davranışı değişmez)
--   hedef_id     seçili uzak hedef. Hedef silinirse NULL olur ve gönderim durur
--   klasor       hedef içinde ön ek klasörü (orkestratör YALNIZ bunun altına yazar)
--   saklama_n    domain başına tutulan son kopya sayısı (en az 1)
--   saklama_gun  0 = gün sınırı yok. Doluysa daha eski kopyalar silinir
--                (her domainin en yeni kopyası her durumda korunur)
--   bant_kbps    yükleme hız sınırı KiB/sn, 0 = sınırsız
--
-- backup_uzak_kopyalar yeni kolonlar (iş durumu, sade hata, boyut ve özetler)
--   boyut_b          ŞİFRELİ dosya boyutu (0113'te vardı)
--   boyut_duz        düz yedek boyutu
--   sha256_duz       düz yedeğin özeti. Şifreli özet sha256_sifreli kolonunda
--   sifreleme        x25519 | hibrit
--   manifest_ad      uzaktaki manifest dosyasının adı (boş = henüz yazılmadı)
--   son_hata         SADE ve sırsız hata metni (ham sağlayıcı metni yazılmaz)
--   yuklenen_b       yüklemede aktarılan bayt (ilerleme)
--   baslangic_at, bitis_at   son denemenin zamanları
--   yedek_dosya      yerel yedek dosya adı (yerel kayıt silinse de kalır)
ALTER TABLE backup_uzak_kopyalar
  ADD COLUMN IF NOT EXISTS boyut_duz BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS sha256_duz CHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sifreleme VARCHAR(8) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS manifest_ad VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS son_hata VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS yuklenen_b BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS baslangic_at TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS bitis_at TIMESTAMP NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS yedek_dosya VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE backup_uzak_kopyalar DROP CONSTRAINT IF EXISTS ck_uzak_kopya_durum;

ALTER TABLE backup_uzak_kopyalar ADD CONSTRAINT ck_uzak_kopya_durum CHECK (durum IN ('bekliyor', 'sifreleniyor', 'yukleniyor', 'dogrulaniyor', 'tamam', 'hata', 'iptal', 'siliniyor', 'silindi', 'sahipsiz'));

CREATE TABLE IF NOT EXISTS backup_uzak_kopya_ayar (
  id           TINYINT UNSIGNED NOT NULL DEFAULT 1,
  aktif        TINYINT(1)       NOT NULL DEFAULT 0,
  hedef_id     BIGINT UNSIGNED  NULL DEFAULT NULL,
  klasor       VARCHAR(128)     NOT NULL DEFAULT 'girginospanel-yedek',
  saklama_n    INT UNSIGNED     NOT NULL DEFAULT 7,
  saklama_gun  INT UNSIGNED     NOT NULL DEFAULT 0,
  bant_kbps    INT UNSIGNED     NOT NULL DEFAULT 0,
  updated_at   TIMESTAMP        NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  CONSTRAINT ck_uzak_ayar_tek CHECK (id = 1),
  CONSTRAINT ck_uzak_ayar_n CHECK (saklama_n >= 1),
  CONSTRAINT fk_uzak_ayar_hedef FOREIGN KEY (hedef_id) REFERENCES backup_hedefler (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO backup_uzak_kopya_ayar (id) VALUES (1);
