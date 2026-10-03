-- 0128: kok ve bayi API anahtarlari (D4).
-- Gizli deger SAKLANMAZ, yalniz SHA-256 ozeti (ozet). onek herkese acik arama kimligidir.
-- sahip_id = users.id (kok=1, bayi=kendi id). sahip_rol istek aninda users.role ile eslesmeli.
-- izinler = kanonik grup:erisim listesi (virgullu, sirali), ornek hesap:yaz,plan:oku
-- izinli_ipler = virgullu IP/CIDR, bos = her IP (yalniz bayide, kokte zorunlu)
-- iptal_at gelecekte olabilir (yenileme gecis suresi). Satir silinmez, iz kalir.
-- FK YOK (elestiri 11: tip uyusmazligi tabloyu hic olusturmayabilir). Bayi silinince
-- anahtarlari reseller.Sil icinde iptal edilir, JOIN users bos doner ve dogrulama reddeder.
CREATE TABLE IF NOT EXISTS api_anahtarlari (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  sahip_id        BIGINT UNSIGNED NOT NULL,
  sahip_rol       ENUM('admin','reseller') NOT NULL,
  ad              VARCHAR(64)   NOT NULL,
  onek            CHAR(12)      NOT NULL,
  ozet            CHAR(64)      NOT NULL,
  son4            CHAR(4)       NOT NULL,
  izinler         VARCHAR(512)  NOT NULL DEFAULT '',
  izinli_ipler    VARCHAR(2048) NOT NULL DEFAULT '',
  hiz_dk          INT UNSIGNED  NOT NULL DEFAULT 120,
  son_kullanma_at TIMESTAMP NULL DEFAULT NULL,
  olusturma_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  son_kullanim_at TIMESTAMP NULL DEFAULT NULL,
  son_kullanim_ip VARCHAR(45)   NOT NULL DEFAULT '',
  iptal_at        TIMESTAMP NULL DEFAULT NULL,
  iptal_eden_id   BIGINT UNSIGNED NULL DEFAULT NULL,
  iptal_neden     VARCHAR(32)   NOT NULL DEFAULT '',
  UNIQUE KEY uq_api_onek (onek),
  KEY ix_api_sahip (sahip_id, iptal_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
