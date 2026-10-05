-- 0180_websec_ek_alan.sql
-- Website Security Monitor: ek (addon) alan adlari da taranir. Bulgu ve envanter
-- satirlari hangi ek alana ait oldugunu ek_alan_id ile tasir. NULL = hesabin ana
-- alan adi (mevcut satirlarin tamami), dolu = ek_alan_adlari.id. Ek alan silinince
-- (ya da hesap silinip ek alanlari CASCADE giderse) bu satirlar da CASCADE ile gider.
--
-- Benzersiz anahtarlar DEGISMEZ: ek alanin docroot yolu (install_path) ana docroottan
-- farkli oldugu icin ayni (domain_id, install_path, ...) cakismaz.
--
-- cp_websec_ek_tarama: cp_websec_domain_tarama ile ayni mantik, ek alan BASINA son
-- tarama damgasi (uygulamasi olmayan ek alan "tarandi, uygulama yok" ile "henuz
-- taranmadi" ayrimi icin).
--
-- Idempotent: ADD COLUMN IF NOT EXISTS, FOREIGN KEY IF NOT EXISTS, CREATE TABLE IF
-- NOT EXISTS. Satir ici yorum yok ve metinde noktali virgul yok: goc kosucusu yalniz
-- basinda iki tire olan satirlari atar ve metni noktali virgulden boler.
ALTER TABLE cp_websec_apps ADD COLUMN IF NOT EXISTS ek_alan_id BIGINT UNSIGNED NULL DEFAULT NULL;

ALTER TABLE cp_websec_apps ADD CONSTRAINT fk_websec_apps_ek_alan FOREIGN KEY IF NOT EXISTS (ek_alan_id) REFERENCES ek_alan_adlari(id) ON DELETE CASCADE;

ALTER TABLE cp_websec_findings ADD COLUMN IF NOT EXISTS ek_alan_id BIGINT UNSIGNED NULL DEFAULT NULL;

ALTER TABLE cp_websec_findings ADD CONSTRAINT fk_websec_findings_ek_alan FOREIGN KEY IF NOT EXISTS (ek_alan_id) REFERENCES ek_alan_adlari(id) ON DELETE CASCADE;

CREATE TABLE IF NOT EXISTS cp_websec_ek_tarama (
  ek_alan_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  son_tarama DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_websec_ek_tarama_ek_alan
    FOREIGN KEY (ek_alan_id) REFERENCES ek_alan_adlari(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
