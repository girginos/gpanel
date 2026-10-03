-- 0126: pano gecmis verisi + kenar-tetikli esik durumu (D3b, pano widget'lari + bildirim).
--
-- pano_gunluk: GUNLUK anlik goruntu. kapsam_id = bayi users.id, 0 = tum sunucu (kok).
-- Ornekleyici (internal/pano) her turda BUGUNUN satirini gunceller (gunun son olcumu
-- kalir). Sahte/geriye donuk dolum YOK: gecmis yayindan itibaren birikir, arayuz yeterli
-- nokta yokken "veri toplaniyor" der. Saklama 400 gun (ornekleyici temizler). FK yok:
-- silinen bayinin gecmisi temizlige kadar kalir, kimseye gosterilmez (kapsam jetondan).
--
-- cp_esik_durum: kenar-tetikli durum. Esik bildirimi YALNIZ yukari gecisle uretilir,
-- okundu durumuna bakilmaz (cp_bildirim tekillestirmesi okunduktan sonra her turda yeni
-- satir acardi). konu_tur domain / bayi / is / imlec. seviye kova (yuzde ya da SSL
-- seviyesi) ya da imlec degeri. sahip_id olcum anindaki sahip: devirde durum sifirlanir.
--
-- Idempotent. Satir ici yorum YOK (gocu ; ile bolen kosucu, plan A.12).
CREATE TABLE IF NOT EXISTS pano_gunluk (
  tarih     DATE            NOT NULL,
  kapsam_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'bayi users.id veya 0 tum sunucu',
  hesap     INT             NOT NULL DEFAULT 0,
  askida    INT             NOT NULL DEFAULT 0,
  yeni      INT             NOT NULL DEFAULT 0 COMMENT 'o gun olusturulan hesap',
  disk_kb   BIGINT          NOT NULL DEFAULT 0,
  trafik_kb BIGINT          NOT NULL DEFAULT 0 COMMENT 'ay ici kumulatif trafik',
  olcum_at  TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (kapsam_id, tarih)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS cp_esik_durum (
  konu_tur   VARCHAR(8)      NOT NULL,
  konu_id    BIGINT UNSIGNED NOT NULL,
  metrik     VARCHAR(16)     NOT NULL,
  seviye     BIGINT          NOT NULL DEFAULT 0 COMMENT 'kova veya imlec',
  deger      DECIMAL(12,1)   NOT NULL DEFAULT 0,
  sahip_id   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  olcum_at   TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  degisim_at TIMESTAMP       NULL DEFAULT NULL,
  PRIMARY KEY (konu_tur, konu_id, metrik),
  KEY ix_esik_sahip (sahip_id, seviye)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
