-- 0129: Telegram destek botu (Faz 1) — eşleştirme bağlantıları, iş kayıtları, ayarlar.
-- Numara: 0128 (api_anahtarlari) en yüksek göç; 0116-0119/0121/0124-0125 boşlukları
-- tarihsel atlamalar (hiçbir dalda kullanılmıyor) — sıra korunsun diye 0129.
-- FK YOK (0128 gerekçesi): domain/bayi silinince bağlantılar internal/destek/iptal ile
-- iptal edilir; ajan her kullanımda kapsamı ve durum izini yeniden doğrular.

-- destek_baglantilari: Telegram kullanıcısı ↔ panel kapsamı.
--   kimlik      = protokoldeki baglanti_id (22 karakter base64url, tahmin edilemez)
--   rol         = musteri (domain_id + ftp_hesap_id), bayi (panel_user_id = reseller_id), kok
--   reseller_id = bayi: kendi id; müşteri: domainin bayisi (0 = kök müşterisi)
--   durum       = bekliyor (merkez onayı geldi, panelde ikinci onay yok) | aktif | iptal
--   durum_izi   = onay anındaki kimlik izinin sha256'sı (müşteri: FTP parola özeti;
--                 bayi/kök: users.password_hash); değişirse bağlantı kendiliğinden iptal
--   iki_fa      = onay anında 2FA açık mıydı (kapatılırsa iptal)
CREATE TABLE IF NOT EXISTS destek_baglantilari (
  id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  kimlik            CHAR(22)       NOT NULL,
  kod_ozet          CHAR(64)       NOT NULL DEFAULT '',
  tg_user_id        BIGINT         NOT NULL,
  tg_kullanici_adi  VARCHAR(32)    NOT NULL DEFAULT '',
  rol               ENUM('musteri','bayi','kok') NOT NULL,
  domain_id         BIGINT UNSIGNED NULL DEFAULT NULL,
  ftp_hesap_id      BIGINT UNSIGNED NULL DEFAULT NULL,
  reseller_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
  panel_user_id     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  duzey             ENUM('okuma','onarim','tam') NOT NULL DEFAULT 'okuma',
  sure_gun          SMALLINT UNSIGNED NOT NULL DEFAULT 7,
  bitis             TIMESTAMP NULL DEFAULT NULL,
  durum             ENUM('bekliyor','aktif','iptal') NOT NULL DEFAULT 'bekliyor',
  olusturan         VARCHAR(64)    NOT NULL DEFAULT '',
  onaylayan         VARCHAR(64)    NOT NULL DEFAULT '',
  onay_at           TIMESTAMP NULL DEFAULT NULL,
  aydinlatma_surum  VARCHAR(32)    NOT NULL DEFAULT '',
  aydinlatma_zamani TIMESTAMP NULL DEFAULT NULL,
  durum_izi         CHAR(64)       NOT NULL DEFAULT '',
  iki_fa            TINYINT(1)     NOT NULL DEFAULT 0,
  son_kullanim      TIMESTAMP NULL DEFAULT NULL,
  iptal_nedeni      VARCHAR(32)    NOT NULL DEFAULT '',
  iptal_eden        VARCHAR(64)    NOT NULL DEFAULT '',
  iptal_at          TIMESTAMP NULL DEFAULT NULL,
  olusturma_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  guncelleme_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_destek_bag_kimlik (kimlik),
  KEY ix_destek_bag_durum (durum, iptal_at),
  KEY ix_destek_bag_domain (domain_id),
  KEY ix_destek_bag_bayi (reseller_id),
  KEY ix_destek_bag_kullanici (panel_user_id),
  KEY ix_destek_bag_tg (tg_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- destek_islemleri: merkezden gelen her iş emri (Faz 1: yalnız R0). Ham çıktı
-- SAKLANMAZ; param maskeli ve kesik. is_kodu tekildir (aynı iş iki kez yürütülmez).
-- denetim_ozet: R0 kayıtları audit_log'a saatlik özet olarak geçince 1 olur.
CREATE TABLE IF NOT EXISTS destek_islemleri (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  is_kodu       CHAR(8)        NOT NULL,
  baglanti_id   BIGINT UNSIGNED NOT NULL,
  arac          VARCHAR(40)    NOT NULL,
  risk          ENUM('R0','R1','R2') NOT NULL DEFAULT 'R0',
  param_maskeli VARCHAR(1024)  NOT NULL DEFAULT '',
  sonuc         VARCHAR(32)    NOT NULL DEFAULT 'yurutuluyor',
  sure_ms       INT UNSIGNED   NOT NULL DEFAULT 0,
  denetim_ozet  TINYINT(1)     NOT NULL DEFAULT 0,
  zaman         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_destek_is_kodu (is_kodu),
  KEY ix_destek_is_bag (baglanti_id, zaman),
  KEY ix_destek_is_ozet (denetim_ozet, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- destek_ayar: reseller_id=0 kök satırı (modül şalteri + bayi ve tüm müşteriler için
-- üst sınır), reseller_id=<bayi> satırı ("müşterilerim kullanabilir" üst sınırı).
-- Satır yoksa her şey 'kapali' (varsayılan KAPALI).
-- Etkin düzey = min(modul_duzey, rol üst sınırları, bağlantı düzeyi) — ajan/ayar.go.
CREATE TABLE IF NOT EXISTS destek_ayar (
  reseller_id   BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  modul_duzey   ENUM('kapali','okuma','onarim','tam') NOT NULL DEFAULT 'kapali',
  bayi_duzey    ENUM('kapali','okuma','onarim','tam') NOT NULL DEFAULT 'kapali',
  musteri_duzey ENUM('kapali','okuma','onarim','tam') NOT NULL DEFAULT 'kapali',
  guncelleyen   VARCHAR(64)    NOT NULL DEFAULT '',
  guncelleme_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
