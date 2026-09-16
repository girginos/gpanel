-- 0061_tasima_oturum.sql — Taşıma OTURUM kalıcılığı.
--
-- Amaç: kullanıcı her sayfa yenilemesinde kaynak sunucu bilgilerini (host/kullanıcı/
-- parola) ve keşif sonuçlarını YENİDEN girmesin. Keşif anında bir 'oturum' satırı
-- yazılır; bilgiler (kimlik AES-256-GCM şifreli, host'a bağlı) TTL boyunca saklanır,
-- sayfa yenilenince liste/geri-yükle uçlarıyla geri gelir. Başlatma bu şifreli
-- kimliği SUNUCU TARAFINDA çözer — parola tarayıcıya asla geri dönmez.
--
-- 🔴 Yeni 'oturum' durumu: 'kesif'ten AYRI, çünkü AcilistaTemizle 'kesif'i öldürür
-- (yarım keşif işi). 'oturum' panel yeniden başlasa da (TTL dolana dek) yaşar.

ALTER TABLE tasima_isleri
  MODIFY COLUMN durum ENUM('bekliyor','kesif','oturum','calisiyor','tamam','hata','iptal','kesildi')
    NOT NULL DEFAULT 'bekliyor';

ALTER TABLE tasima_isleri
  ADD COLUMN IF NOT EXISTS kesif_json   MEDIUMTEXT NULL,   -- keşfedilen hesap listesi (JSON)
  ADD COLUMN IF NOT EXISTS secim_json   TEXT NULL,         -- seçilen alan adları (JSON)
  ADD COLUMN IF NOT EXISTS son_kullanim DATETIME NULL,     -- son dokunma
  ADD COLUMN IF NOT EXISTS gecerlilik   DATETIME NULL;     -- kimlik/oturum sona erme (TTL)
