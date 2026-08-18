-- PHP per-domain varsayilanlarini YUKSELT (operator talebi).
--
-- 🔴 NEDEN AYRI GOC: 0011 ve 0053'teki `DEFAULT` degerlerini duzenlemek
-- MEVCUT sunuculari ETKILEMEZ — o gocler coktan uygulandi ve MariaDB kolon
-- varsayilanini yalnizca ALTER ile degistirir. Migration dosyasini duzenlemek
-- ayrica sha256 capasini bozar ve dogrulamada "HASH FARKLI" uyarisi uretir.
--
-- ESKI -> YENI
--   memory_limit         256M -> 2048M
--   max_execution_time     30 -> 3000
--   max_input_time         60 -> 6000
--   post_max_size         64M -> 8000M
--   upload_max_filesize   32M -> 2000M
--
-- KAPSAM: yalnizca VARSAYILAN. Mevcut domainlerin kayitli degerleri KORUNUR
-- (operator panelden bilerek degistirmis olabilir).

ALTER TABLE php_settings
  MODIFY COLUMN memory_limit        VARCHAR(16) NOT NULL DEFAULT '2048M',
  MODIFY COLUMN max_execution_time  INT         NOT NULL DEFAULT 3000,
  MODIFY COLUMN max_input_time      INT         NOT NULL DEFAULT 6000,
  MODIFY COLUMN post_max_size       VARCHAR(16) NOT NULL DEFAULT '8000M',
  MODIFY COLUMN upload_max_filesize VARCHAR(16) NOT NULL DEFAULT '2000M';

ALTER TABLE subdomain_php_settings
  MODIFY COLUMN memory_limit        VARCHAR(16) NOT NULL DEFAULT '2048M',
  MODIFY COLUMN max_execution_time  INT         NOT NULL DEFAULT 3000,
  MODIFY COLUMN max_input_time      INT         NOT NULL DEFAULT 6000,
  MODIFY COLUMN post_max_size       VARCHAR(16) NOT NULL DEFAULT '8000M',
  MODIFY COLUMN upload_max_filesize VARCHAR(16) NOT NULL DEFAULT '2000M';
