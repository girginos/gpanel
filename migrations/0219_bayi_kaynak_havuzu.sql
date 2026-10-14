-- 0219_bayi_kaynak_havuzu.sql — Bayi (reseller) basina VPS benzeri kaynak havuzu.
--
-- Goc kosucusu yalniz "--" ile BASLAYAN satirlari atar ve govdeyi noktalu virgulden boler:
-- bu dosyada satir ici yorum ve yorum metni icinde noktalu virgul YOKTUR. Her ifade iki kez
-- koşunca ayni sonucu verir.
--
-- 1) Havuz degerleri bayi kaydinda (users) ve pakette (reseller_plans). Hepsinde 0 = SINIRSIZ:
--    mevcut bayilerin satirlari 0 ile gelir, yani bir deger verilene kadar HICBIR bayi etkilenmez.
--    Disk havuzu icin yeni kolon YOKTUR: mevcut max_disk_mb havuzdur. Sert sinir (XFS proje kotasi)
--    yalniz yonetici kernel'e pquota ekleyip sunucuyu yeniden baslatinca devreye girer.
--      kaynak_cpu_cekirdek  ondalikli cekirdek sayisi (1.5 = 150 yuzde CPUQuota)
--      kaynak_ram_mb        fiziksel RAM tavani (MemoryMax, takas yok)
--      kaynak_io_agirlik    G/C agirligi (IOWeight, 1-10000)
ALTER TABLE users ADD COLUMN IF NOT EXISTS kaynak_cpu_cekirdek DECIMAL(7,2) NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kaynak_ram_mb BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kaynak_io_agirlik INT NOT NULL DEFAULT 0;
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS kaynak_cpu_cekirdek DECIMAL(7,2) NOT NULL DEFAULT 0;
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS kaynak_ram_mb BIGINT NOT NULL DEFAULT 0;
ALTER TABLE reseller_plans ADD COLUMN IF NOT EXISTS kaynak_io_agirlik INT NOT NULL DEFAULT 0;

-- 2) Kullanim zaman serisi: bayi basina 5 dakikalik ornek, 30 gun saklanir (panel budar).
--    ts UTC. NULL = olculemedi (ilk ornek, sayac sifirlandi, havuz dilimi yok).
--      cpu_milli     ortalama CPU (1000 = 1 cekirdek)    cpu_sinir_milli  havuz CPU tavani
--      kisitlama_ms  CPU kisma suresi (ms)               ram_mb/ram_sinir_mb anlik fiziksel RAM
--      oom_olay      OOM oldurme sayisi (aralikta)       disk_mb/disk_sinir_mb disk kullanimi
--      io_oku_kbs, io_yaz_kbs  ortalama disk G/C         kaynak  olcum kaynagi
CREATE TABLE IF NOT EXISTS bayi_kaynak_kullanim (
  bayi_id         BIGINT UNSIGNED NOT NULL,
  ts              DATETIME NOT NULL,
  cpu_milli       INT NULL,
  cpu_sinir_milli INT NOT NULL DEFAULT 0,
  kisitlama_ms    INT NULL,
  ram_mb          INT NULL,
  ram_sinir_mb    BIGINT NOT NULL DEFAULT 0,
  oom_olay        INT NULL,
  disk_mb         BIGINT NULL,
  disk_sinir_mb   BIGINT NOT NULL DEFAULT 0,
  io_oku_kbs      INT NULL,
  io_yaz_kbs      INT NULL,
  kaynak          VARCHAR(24) NOT NULL DEFAULT '',
  PRIMARY KEY (bayi_id, ts),
  KEY ix_bayi_kaynak_ts (ts),
  CONSTRAINT fk_bayi_kaynak_bayi FOREIGN KEY (bayi_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
