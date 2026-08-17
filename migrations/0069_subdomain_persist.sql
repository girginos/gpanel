-- 0069: subdomain_ad kolon — orphan cert cleanup için persist
-- MV#3+#7 borcu: NginxProxyKaldir katalog scan yerine DB'den subdomain oku.
ALTER TABLE cp_host_uygulamalar
    ADD COLUMN subdomain_ad VARCHAR(255) NOT NULL DEFAULT '' AFTER systemd_unit;
