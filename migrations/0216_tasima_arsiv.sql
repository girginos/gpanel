-- 0216_tasima_arsiv.sql — Site Taşıma: arşiv kaynakları (cPanel cpmove / DirectAdmin yedeği) faz 2.
--
-- Göç koşucusu yalnız "--" ile BAŞLAYAN satırları atar ve gövdeyi noktalı virgülden böler:
-- bu dosyada satır içi yorum ve metin içinde noktalı virgül YOKTUR. Her ifade iki kez
-- koşunca aynı sonucu verir.
--
-- 1) Kaynak başına ikinci anahtar: bayiye açılma. Arşiv kaynakları bayide VARSAYILAN KAPALI
--    (kullanıcı kararı): satır yoksa ya da bayi_etkin 0 ise bayi göremez ve kullanamaz.
--    Mevcut satırlar (canlı SSH kaynakları) bu sütunu kullanmaz, davranışları değişmez.
ALTER TABLE tasima_kaynak_ayar ADD COLUMN IF NOT EXISTS bayi_etkin TINYINT(1) NOT NULL DEFAULT 0;

-- 2) İş, bir yüklemeye bağlıdır: yükleme iş sürerken silinemez, iş bitince silinir.
ALTER TABLE tasima_isleri ADD COLUMN IF NOT EXISTS arsiv_yukleme_id BIGINT UNSIGNED NULL;

-- 3) Parçalı ve devam edebilir arşiv yüklemesi (geçici dizin + kota için kayıt).
--    sahip_rid 0 = kök, 0'dan büyükse bayinin users.id değeri. yol sunucuda hesaplanır,
--    istemciden gelmez. durum: yukleniyor, hazir, silindi.
CREATE TABLE IF NOT EXISTS tasima_yuklemeleri (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  sahip_rid   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  yukleyen_uid BIGINT UNSIGNED NULL,
  ad          VARCHAR(190) NOT NULL,
  boyut       BIGINT UNSIGNED NOT NULL,
  alinan      BIGINT UNSIGNED NOT NULL DEFAULT 0,
  durum       VARCHAR(16) NOT NULL DEFAULT 'yukleniyor',
  olusturma   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  guncelleme  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY ix_yukleme_kapsam (sahip_rid, durum, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
