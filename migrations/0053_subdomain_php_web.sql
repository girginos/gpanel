-- 0053: Subdomain PHP ayarları (ayrı FPM havuzu) + web-sunucu ayarları paritesi.
-- Alt alanlar artık domainlerle aynı özelleştirmeye sahip: kendi php_settings satırı
-- (ayrı FPM havuzuna render edilir) + kendi nginx/web-backend ayarları.

-- Alt alan web backend (php-fpm | apache | static) — domains.web_backend paritesi.
ALTER TABLE subdomanlar ADD COLUMN IF NOT EXISTS web_backend VARCHAR(32) NOT NULL DEFAULT 'php-fpm';

-- Alt alan PHP ayarları — php_settings şemasının aynısı, subdomain_id ile anahtarlı.
CREATE TABLE IF NOT EXISTS subdomain_php_settings (
  subdomain_id INT PRIMARY KEY,
  memory_limit VARCHAR(16) NOT NULL DEFAULT '256M',
  max_execution_time INT NOT NULL DEFAULT 30,
  max_input_time INT NOT NULL DEFAULT 60,
  post_max_size VARCHAR(16) NOT NULL DEFAULT '64M',
  upload_max_filesize VARCHAR(16) NOT NULL DEFAULT '32M',
  opcache_enable TINYINT(1) NOT NULL DEFAULT 1,
  disable_functions TEXT,
  display_errors TINYINT(1) NOT NULL DEFAULT 0,
  log_errors TINYINT(1) NOT NULL DEFAULT 1,
  allow_url_fopen TINYINT(1) NOT NULL DEFAULT 1,
  file_uploads TINYINT(1) NOT NULL DEFAULT 1,
  short_open_tag TINYINT(1) NOT NULL DEFAULT 0,
  error_reporting VARCHAR(128) NOT NULL DEFAULT 'E_ALL & ~E_DEPRECATED & ~E_STRICT',
  include_path VARCHAR(255) NOT NULL DEFAULT '.:/usr/share/php',
  open_basedir VARCHAR(255) NOT NULL DEFAULT '',
  session_save_path VARCHAR(255) NOT NULL DEFAULT '',
  mail_force_extra_parameters VARCHAR(255) NOT NULL DEFAULT '',
  pm_strategy VARCHAR(16) NOT NULL DEFAULT 'ondemand',
  pm_max_children INT NOT NULL DEFAULT 8,
  pm_max_requests INT NOT NULL DEFAULT 500,
  pm_start_servers INT NOT NULL DEFAULT 2,
  pm_min_spare_servers INT NOT NULL DEFAULT 1,
  pm_max_spare_servers INT NOT NULL DEFAULT 3,
  ek_direktifler TEXT,
  debug_mode TINYINT(1) NOT NULL DEFAULT 0,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_sub_php FOREIGN KEY (subdomain_id) REFERENCES subdomanlar(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Alt alan nginx ayarları — nginx_settings paritesi + client_max_body dedike kolonu.
CREATE TABLE IF NOT EXISTS subdomain_nginx_settings (
  subdomain_id INT PRIMARY KEY,
  hdr_x_content_type TINYINT(1) NOT NULL DEFAULT 1,
  hdr_x_xss TINYINT(1) NOT NULL DEFAULT 1,
  hdr_referrer TINYINT(1) NOT NULL DEFAULT 0,
  hdr_permissions TINYINT(1) NOT NULL DEFAULT 0,
  hdr_csp_upgrade TINYINT(1) NOT NULL DEFAULT 0,
  hdr_hsts TINYINT(1) NOT NULL DEFAULT 0,
  hsts_max_age INT NOT NULL DEFAULT 31536000,
  hsts_subdomains TINYINT(1) NOT NULL DEFAULT 0,
  hsts_preload TINYINT(1) NOT NULL DEFAULT 0,
  fastcgi_cache TINYINT(1) NOT NULL DEFAULT 0,
  fastcgi_cache_dakika INT NOT NULL DEFAULT 60,
  browser_cache TINYINT(1) NOT NULL DEFAULT 1,
  browser_cache_gun INT NOT NULL DEFAULT 30,
  client_max_body_mb INT NOT NULL DEFAULT 64,
  ek_direktifler TEXT,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_sub_nginx FOREIGN KEY (subdomain_id) REFERENCES subdomanlar(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
