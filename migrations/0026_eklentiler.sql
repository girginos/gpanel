-- Eklenti (plugin) kayıt defteri.
-- Eklentiler AYRI process olarak çalışır ve UNIX soketten konuşur; core sadece
-- kaydı tutar, sağlığı izler ve /api/v1/eklenti/{ad}/* isteklerini sokete proxy'ler.
-- Böylece core'da eklenti kodu BULUNMAZ ve eklenti çökse panel ayakta kalır.

CREATE TABLE IF NOT EXISTS cp_eklentiler (
  id          INT AUTO_INCREMENT PRIMARY KEY,
  ad          VARCHAR(64)  NOT NULL,                  -- makine adı: 'ai'
  etiket      VARCHAR(128) NOT NULL,                  -- UI adı: 'AI Asistan'
  surum       VARCHAR(32)  NOT NULL DEFAULT '',
  aktif       TINYINT(1)   NOT NULL DEFAULT 0,        -- paralı gate: 0 => 402
  soket       VARCHAR(255) NOT NULL DEFAULT '',       -- /run/girginospanel/eklenti-ai.sock
  ui          TINYINT(1)   NOT NULL DEFAULT 0,        -- frontend bundle sunuyor mu
  saglik      VARCHAR(16)  NOT NULL DEFAULT 'bilinmiyor', -- saglikli|saglksiz|bilinmiyor
  son_kontrol DATETIME     NULL,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_eklenti_ad (ad)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
