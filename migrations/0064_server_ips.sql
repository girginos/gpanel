-- Panel'in yönettiği IP adresleri.
--
-- Kayıt sadece PANEL'in EKLEDIĞI IP'ler için. Kullanıcının elle koyduğu
-- (ISP kurulumu, cloud-init, network-scripts) IP'ler burada DEĞİLDİR ve
-- silme yolu bunlara DOKUNMAZ.
--
-- 🔴 Sunucu üzerinde `ip addr add ... label "panel:<slug>"` şeklinde etiket
-- kullanıyoruz. Silme sırasında ip_kesif YALNIZ bu label'lı olanları
-- "silinebilir" olarak işaretler — kullanıcının kendi ISP-yerleşik IP'sini
-- yanlışlıkla silmek imkansız.
--
-- Kalıcılık: reboot sonrası `girginospanel-ip.service` (oneshot) bu tabloyu
-- okur ve `ip addr add` ile yeniden koyar.

CREATE TABLE IF NOT EXISTS cp_server_ips (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY,
  ip           VARCHAR(45)  NOT NULL,          -- v4 veya v6 metin gösterimi
  iface        VARCHAR(32)  NOT NULL,          -- pub0, eth0 vs.
  cidr         TINYINT      NOT NULL DEFAULT 32, -- v4:32, v6:128 varsayılan
  note         VARCHAR(255) NOT NULL DEFAULT '',
  created_by   BIGINT       NULL,
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_server_ip (ip),
  KEY idx_iface (iface)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
