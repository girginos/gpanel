-- 0039 - per-domain PHP Debug Modu (saglam fatal-gorunurluk)
--
-- php_settings tablosuna debug_mode ekler. Amac: musteri "PHP Debug Modu"nu actiginda
-- FATAL hatalar (E_ERROR/E_PARSE/... register_shutdown_function + error_get_last ile)
-- guvenilir sekilde yakalanip hem ekrana hem per-domain debug log dosyasina yazilsin.
--
-- KRITIK: app runtime'da error_reporting sifirlarsa pool'daki php_admin_value ile
-- verilen display_errors/error_reporting BUNU EZMEZ. Fatal'i gorunur kilmanin tek
-- guvenilir yolu auto_prepend ile yuklenen shutdown-handler shim'idir; debug_mode=1
-- oldugunda renderTenantPool bu shim'i (auto_prepend_file) devreye alir.
--
-- SEMANTIK (php_settings, per-domain):
--   debug_mode 0 = kapali (prod: display_errors=off, error_reporting = kullanici ayari)
--   debug_mode 1 = acik  (display_errors=on + error_reporting=E_ALL + auto_prepend shim)
--
-- Idempotent: MariaDB 10.5+ ADD COLUMN IF NOT EXISTS destekler; her acilista tekrar-guvenli.

ALTER TABLE php_settings
  ADD COLUMN IF NOT EXISTS debug_mode TINYINT(1) NOT NULL DEFAULT 0;
