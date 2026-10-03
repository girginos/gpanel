-- 0109_tasima_kaynak_kapsam.sql — Site Taşıma: kaynak kayıt defteri + bayi kapsamı şeması.
--
-- Göç koşucusu (cmd/server runMigrations) yalnız "--" ile BAŞLAYAN satırları atar ve
-- gövdeyi noktalı virgülden böler. Bu yüzden bu dosyada satır içi yorum YOKTUR.
-- Her ifade iki kez koşunca aynı sonucu verir (MODIFY aynı tipe, IF NOT EXISTS, INSERT IGNORE).
--
-- 1) kaynak_tip ENUM('cpanel','plesk','directadmin') gpanel kaynağını reddediyordu:
--    katı kipte keşif oturumu ve iş kaydı INSERT'i düşüyordu. Kaynak kodu uygulamada
--    doğrulanır, sütun serbest metin olur. Mevcut değerler aynen korunur.
ALTER TABLE tasima_isleri MODIFY COLUMN kaynak_tip VARCHAR(32) NOT NULL;

-- 2) Kuyruk durumu. Sona eklenir, mevcut değerlerin sıra numarası değişmez.
--    Kod durum sırasına dayanmaz (ORDER BY durum yok).
ALTER TABLE tasima_isleri
  MODIFY COLUMN durum ENUM('bekliyor','kesif','oturum','calisiyor','tamam','hata','iptal','kesildi','kuyrukta')
    NOT NULL DEFAULT 'bekliyor';

-- 3) Kapsam. sahip_rid 0 = kök, 0'dan büyükse bayinin users.id değeri. Göç öncesi
--    satırlar yalnız AdminOnly uçlarla açıldı, bu yüzden varsayılan 0 doğrudur ve
--    UPDATE gerekmez. FK yok: 0 bir kullanıcı değil. Silinen bayinin artakalan
--    satırlarının temizliği koda aittir. baslatan_uid işi başlatan gerçek kullanıcıdır.
ALTER TABLE tasima_isleri
  ADD COLUMN IF NOT EXISTS sahip_rid    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS baslatan_uid BIGINT UNSIGNED NULL;

ALTER TABLE tasima_isleri ADD INDEX IF NOT EXISTS ix_tasima_kapsam (sahip_rid, durum, id);

-- 4) Kuyruktaki iş panel yeniden başlasa da devam edebilsin diye seçilen hesabın
--    tamamı (arşiv envanteri dahil) sunucuda saklanır, istemciden yeniden alınmaz.
ALTER TABLE tasima_kalemleri ADD COLUMN IF NOT EXISTS hesap_json MEDIUMTEXT NULL;

-- 5) Kaynak başına kökün anahtarı. Kaynaklar kodda tanımlıdır, burada yalnız durum
--    tutulur. Satır yoksa varsayılanı kod belirler, tohum yok.
CREATE TABLE IF NOT EXISTS tasima_kaynak_ayar (
  kod             VARCHAR(32)     NOT NULL,
  etkin           TINYINT(1)      NOT NULL,
  guncelleyen_uid BIGINT UNSIGNED NULL,
  guncelleme      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (kod)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 6) Taşıma bayide VARSAYILAN KAPALI (kullanıcı kararı). Kök tek anahtarla açar.
--    INSERT IGNORE: yeniden koşu kökün verdiği kararı ezmez. Satır hiç yoksa da
--    kapalı sayılmalıdır.
INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('tasima_bayi_acik', '0');
