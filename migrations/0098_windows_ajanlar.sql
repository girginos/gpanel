-- Windows ajan kayitlari (gPanel Windows temeli).
-- jeton_sifreli: gizli.SaklaBagli(jeton, adres) — DUZ METIN DEGIL; baglam=adres
-- oldugu icin bir satirin ciphertext'i baska satira tasinip cozulemez.
-- adres UNIQUE: ayni ajani iki kez kaydetmek iki ayri "gercek" yaratirdi.
CREATE TABLE IF NOT EXISTS windows_ajanlar (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  ad VARCHAR(64) NOT NULL,
  adres VARCHAR(255) NOT NULL UNIQUE,
  jeton_sifreli TEXT NOT NULL,
  surum VARCHAR(32) NOT NULL DEFAULT '',
  kanal VARCHAR(32) NOT NULL DEFAULT '',
  yetenekler INT UNSIGNED NOT NULL DEFAULT 0,
  durum VARCHAR(16) NOT NULL DEFAULT 'bilinmiyor',
  son_gorulme TIMESTAMP NULL DEFAULT NULL,
  olusturma TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
