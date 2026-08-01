-- 0051_tasima.sql — cPanel / Plesk / DirectAdmin site tasima (migration) altyapisi.
--
-- Iki tablo:
--   tasima_isleri    : bir tasima oturumu (kaynak sunucu + kimlik + ayarlar + ilerleme)
--   tasima_kalemleri : o oturumdaki her bir hesap/domain (tekil veya toplu)
--
-- Kimlik bilgileri (parola / SSH anahtari) DUZ METIN TUTULMAZ — internal/gizli
-- (AES-256-GCM) ile sifrelenip saklanir; is bitince temizlenir (kimlik_temiz=1).

CREATE TABLE IF NOT EXISTS tasima_isleri (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  kaynak_tip       ENUM('cpanel','plesk','directadmin') NOT NULL,
  kaynak_host      VARCHAR(253) NOT NULL,
  kaynak_port      INT NOT NULL DEFAULT 22,
  kaynak_kullanici VARCHAR(64)  NOT NULL DEFAULT 'root',
  kaynak_parola    VARCHAR(1024) NULL,
  kaynak_anahtar   TEXT NULL,
  kimlik_temiz     TINYINT(1) NOT NULL DEFAULT 0,
  tasima_modu      ENUM('tekil','toplu') NOT NULL DEFAULT 'tekil',
  durum            ENUM('bekliyor','kesif','calisiyor','tamam','hata','iptal','kesildi')
                     NOT NULL DEFAULT 'bekliyor',
  toplam           INT NOT NULL DEFAULT 0,
  tamamlanan       INT NOT NULL DEFAULT 0,
  basarisiz        INT NOT NULL DEFAULT 0,
  ayarlar          TEXT NULL,
  hata             TEXT NULL,
  baslatan         VARCHAR(64) NULL,
  baslangic        DATETIME NULL,
  bitis            DATETIME NULL,
  olusturma        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tasima_durum (durum),
  KEY idx_tasima_olusturma (olusturma)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS tasima_kalemleri (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  is_id          BIGINT UNSIGNED NOT NULL,
  kaynak_hesap   VARCHAR(64)  NOT NULL,
  alan_adi       VARCHAR(253) NOT NULL,
  durum          ENUM('bekliyor','calisiyor','tamam','hata','atlandi')
                   NOT NULL DEFAULT 'bekliyor',
  domain_id      BIGINT UNSIGNED NULL,
  adim           VARCHAR(64) NULL,
  dosya_bayt     BIGINT NOT NULL DEFAULT 0,
  db_sayisi      INT NOT NULL DEFAULT 0,
  dns_sayisi     INT NOT NULL DEFAULT 0,
  hata           TEXT NULL,
  basladi        DATETIME NULL,
  bitti          DATETIME NULL,
  PRIMARY KEY (id),
  KEY idx_kalem_is (is_id),
  KEY idx_kalem_durum (durum),
  CONSTRAINT fk_kalem_is FOREIGN KEY (is_id)
    REFERENCES tasima_isleri(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
