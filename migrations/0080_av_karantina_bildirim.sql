-- Antivirüs: bulgu detayı + karantina geri-yükleme + bildirim.
--
-- 🔴 NEDEN: (1) ajan bulguları DB'ye yazmıyordu → panel görmüyordu; şema
-- seviye/puan taşımıyordu. (2) Karantina orijinal yolu KAYBEDİYORDU → yanlış
-- pozitifte dosya geri yüklenemiyordu. (3) Bildirim altyapısı hiç yoktu →
-- müşteriye/panele hiçbir uyarı akmıyordu.

-- ── av_bulgular: seviye/puan + karantina geri-yükleme yolları ──
ALTER TABLE av_bulgular
  ADD COLUMN seviye        VARCHAR(16)  NOT NULL DEFAULT ''  AFTER motor,
  ADD COLUMN puan          INT          NOT NULL DEFAULT 0   AFTER seviye,
  -- 🔴 Geri yükleme için ŞART: orijinal_yol karantinaya alınmadan önceki tam
  -- yol, karantina_yol .karantina içindeki hedef. İkisi olmadan yanlış pozitif
  -- kalıcı veri kaybıdır.
  ADD COLUMN orijinal_yol  VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN karantina_yol VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN durum         VARCHAR(16)  NOT NULL DEFAULT 'aktif';
  -- durum: aktif | karantina | geri_yuklendi | silindi | temizlendi

-- ── av_taramalar: kaynak (panel/ajan) + kapsam ──
ALTER TABLE av_taramalar
  ADD COLUMN kaynak VARCHAR(16) NOT NULL DEFAULT 'panel',  -- panel | ajan-zamanli | ajan-izle
  ADD COLUMN kapsam VARCHAR(16) NOT NULL DEFAULT 'domain'; -- domain | host | sunucu

-- ── cp_bildirim: panel/müşteri bildirim akışı ──
-- 🔴 Genel amaçlı: yalnız antivirüs değil, kategori ile her modül kullanabilir.
CREATE TABLE IF NOT EXISTS cp_bildirim (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  seviye     VARCHAR(16)  NOT NULL DEFAULT 'bilgi',   -- bilgi | uyari | kritik
  kategori   VARCHAR(32)  NOT NULL DEFAULT '',        -- antivirus | ssl | ...
  baslik     VARCHAR(200) NOT NULL,
  mesaj      TEXT         NOT NULL,
  -- Hedef: domain_id doluysa o domainin müşterisine, boşsa panel-geneli (admin).
  domain_id  BIGINT UNSIGNED NULL,
  -- İlişkili kayıt (ör. av_bulgular.id) — panelde "detaya git".
  ref_tur    VARCHAR(24)  NOT NULL DEFAULT '',
  ref_id     BIGINT       NOT NULL DEFAULT 0,
  okundu     TINYINT(1)   NOT NULL DEFAULT 0,
  created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_bildirim_okundu (okundu),
  KEY idx_bildirim_kategori (kategori),
  KEY idx_bildirim_domain (domain_id),
  KEY idx_bildirim_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
