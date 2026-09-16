-- Alt alan adı bazlı erişim kısıtlama.
--
-- 0093 bu tabloları oluşturmuş, 0094 (kapsam dışı kararı) düşürmüştü. Karar
-- değişti: müşterinin asıl senaryosu zaten `api.site.com` — alt alanı ayrı bir
-- domain gibi açmak işe yarar ama kullanıcıdan ekstra iş ister ve alt alan
-- üst domainin planı/kaynakları altında kalmaz.
--
-- 0093'ün şemasından iki fark:
--   1) YABANCI ANAHTAR var. 0093'te yoktu; kod incelemesi işaretledi:
--      `subdomanlar.id` AUTO_INCREMENT ve MariaDB 10.11 sayacı restart'ta
--      korumaz → id yeniden kullanılırsa yetim kurallar YENİ alt alana
--      devrolurdu. ON DELETE CASCADE bunu imkânsız kılar.
--   2) `sira` üzerinde tekil dizin yerine (domain_id, sira, id) sıralama
--      dizini — ana domain tarafıyla aynı erişim deseni.

CREATE TABLE IF NOT EXISTS subdomain_erisim_kisit (
  subdomain_id INT NOT NULL PRIMARY KEY,
  aktif        TINYINT(1) NOT NULL DEFAULT 0,
  -- 'izin' | 'red' — listede eşleşme olmazsa ne yapılacağı.
  -- 🔴 Karşılaştırma kodda FAIL-CLOSED: yalnız açıkça 'izin' açar, tanınmayan
  -- her değer kapatır. Kolon utf8mb4_unicode_ci (harf duyarsız) olduğu için
  -- 'RED' gibi bir değer DB'ye girebilir; Go tarafı bunu kaçırmamalı.
  varsayilan   VARCHAR(8) NOT NULL DEFAULT 'izin',
  updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_serisimkisit_sub FOREIGN KEY (subdomain_id)
    REFERENCES subdomanlar(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS subdomain_erisim_kurallari (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  subdomain_id INT NOT NULL,
  tip          VARCHAR(8) NOT NULL,
  -- Kanonik ağ biçiminde saklanır: "1.2.3.4/0" -> "0.0.0.0/0".
  -- nginx host bitlerini yok sayar; kanonikleştirilmezse panel "sadece 1 adres"
  -- gösterirken nginx tüm interneti açar (ana domainde ölçüldü).
  cidr         VARCHAR(64) NOT NULL,
  aciklama     VARCHAR(190) NOT NULL DEFAULT '',
  sira         INT NOT NULL DEFAULT 0,
  olusturma    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_serisim_sub_cidr (subdomain_id, cidr),
  KEY ix_serisim_sub_sira (subdomain_id, sira, id),
  CONSTRAINT fk_serisimkural_sub FOREIGN KEY (subdomain_id)
    REFERENCES subdomanlar(id) ON DELETE CASCADE
) ENGINE=InnoDB;
