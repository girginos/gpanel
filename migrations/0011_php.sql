-- 0011 - PHP per-domain ayarlari (Plesk benzeri)
CREATE TABLE IF NOT EXISTS php_settings (
  domain_id BIGINT UNSIGNED PRIMARY KEY,

  -- Performance & Security
  memory_limit VARCHAR(16) NOT NULL DEFAULT '256M',
  max_execution_time INT NOT NULL DEFAULT 30,
  max_input_time INT NOT NULL DEFAULT 60,
  post_max_size VARCHAR(16) NOT NULL DEFAULT '64M',
  upload_max_filesize VARCHAR(16) NOT NULL DEFAULT '32M',
  opcache_enable TINYINT(1) NOT NULL DEFAULT 1,
  disable_functions VARCHAR(1024) NOT NULL DEFAULT 'exec,passthru,shell_exec,system,proc_open,popen',

  -- Common
  display_errors TINYINT(1) NOT NULL DEFAULT 0,
  log_errors TINYINT(1) NOT NULL DEFAULT 1,
  allow_url_fopen TINYINT(1) NOT NULL DEFAULT 1,
  file_uploads TINYINT(1) NOT NULL DEFAULT 1,
  short_open_tag TINYINT(1) NOT NULL DEFAULT 0,
  error_reporting VARCHAR(255) NOT NULL DEFAULT 'E_ALL & ~E_DEPRECATED & ~E_STRICT',
  include_path VARCHAR(512) NOT NULL DEFAULT '.:/usr/share/php',
  open_basedir VARCHAR(512) NOT NULL DEFAULT '',
  session_save_path VARCHAR(255) NOT NULL DEFAULT '',
  mail_force_extra_parameters VARCHAR(255) NOT NULL DEFAULT '',

  -- PHP-FPM
  pm_strategy VARCHAR(16) NOT NULL DEFAULT 'ondemand',
  pm_max_children INT NOT NULL DEFAULT 8,
  pm_max_requests INT NOT NULL DEFAULT 500,
  pm_start_servers INT NOT NULL DEFAULT 2,
  pm_min_spare_servers INT NOT NULL DEFAULT 1,
  pm_max_spare_servers INT NOT NULL DEFAULT 3,

  -- Additional
  ek_direktifler TEXT NOT NULL,

  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_phpset_dom FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
