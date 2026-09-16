-- Domain bazlı erişim kısıtlama (Access Restrictions).
--
-- Neden nft değil nginx: tek sunucu IP'sinde onlarca alan adı var; paket
-- seviyesinde hangi isteğin hangi alan adına gittiği GÖRÜNMEZ. Alan adı ancak
-- TLS SNI / HTTP Host başlığından bilinir. Bu yüzden kısıtlama, panelin zaten
-- render ettiği per-domain nginx server bloğunda uygulanır.
--
-- Model: sıralı allow/deny listesi + varsayılan politika (ilk eşleşen kazanır,
-- nginx ngx_http_access_module semantiği).
--   varsayilan='red' → beyaz liste (sadece yazılanlar girer)  ← Azure "Allow" modu
--   varsayilan='izin' → kara liste (yazılanlar hariç herkes girer)

CREATE TABLE IF NOT EXISTS domain_erisim_kisit (
  domain_id  BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  aktif      TINYINT(1) NOT NULL DEFAULT 0,
  -- 'izin' | 'red' — listede eşleşme olmazsa ne yapılacağı
  varsayilan VARCHAR(8) NOT NULL DEFAULT 'izin',
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_erisimkisit_dom FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS domain_erisim_kurallari (
  id        BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  -- 'izin' | 'red'
  tip       VARCHAR(8) NOT NULL,
  -- Tek IP ya da CIDR (v4/v6). API'de net.ParseIP/ParseCIDR ile doğrulanır;
  -- render sırasında TEKRAR doğrulanır (config enjeksiyonuna karşı savunma).
  cidr      VARCHAR(64) NOT NULL,
  aciklama  VARCHAR(190) NOT NULL DEFAULT '',
  sira      INT NOT NULL DEFAULT 0,
  olusturma TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_erisim_dom_cidr (domain_id, cidr),
  KEY ix_erisim_dom_sira (domain_id, sira, id),
  CONSTRAINT fk_erisimkural_dom FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Alt alan adı paritesi (müşterinin asıl senaryosu: api.site.com).
CREATE TABLE IF NOT EXISTS subdomain_erisim_kisit (
  subdomain_id INT NOT NULL PRIMARY KEY,
  aktif        TINYINT(1) NOT NULL DEFAULT 0,
  varsayilan   VARCHAR(8) NOT NULL DEFAULT 'izin',
  updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS subdomain_erisim_kurallari (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  subdomain_id INT NOT NULL,
  tip          VARCHAR(8) NOT NULL,
  cidr         VARCHAR(64) NOT NULL,
  aciklama     VARCHAR(190) NOT NULL DEFAULT '',
  sira         INT NOT NULL DEFAULT 0,
  olusturma    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_serisim_sub_cidr (subdomain_id, cidr),
  KEY ix_serisim_sub_sira (subdomain_id, sira, id)
) ENGINE=InnoDB;

-- Güvenilir vekil (CDN/proxy) aralıkları — GLOBAL.
--
-- 🔴 Bu tablo olmadan erişim kısıtlaması CDN arkasında SESSİZCE YANLIŞ çalışır:
-- nginx'in gördüğü $remote_addr ziyaretçi değil, CDN kenar sunucusudur. O zaman
-- CDN aralığını izin verirsen HERKES girer, gerçek ziyaretçi IP'sini izin
-- verirsen HİÇ KİMSE giremez. Buraya yazılan aralıklar için
-- 00-gosp-realip.conf üretilir (set_real_ip_from + real_ip_header).
CREATE TABLE IF NOT EXISTS guvenilir_vekil (
  id        BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  cidr      VARCHAR(64) NOT NULL,
  aciklama  VARCHAR(190) NOT NULL DEFAULT '',
  olusturma TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_guvvekil_cidr (cidr)
) ENGINE=InnoDB;

-- Hangi başlıktan gerçek IP okunacağı (Cloudflare: CF-Connecting-IP,
-- genel proxy: X-Forwarded-For).
INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('realip_header', 'X-Forwarded-For');
