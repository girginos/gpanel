-- git webhook_secret artik at-rest sifreli (AEAD, gösterim için) saklanir; gelen
-- webhook eslemesi ise SHA-256 hash sutunuyla yapilir (AEAD non-deterministik →
-- WHERE ile aranamaz). Boylece parola/secret DUZ METIN durmaz ama URL akisi bozulmaz.
ALTER TABLE git_repos ADD COLUMN IF NOT EXISTS webhook_secret_hash VARCHAR(64) NOT NULL DEFAULT '';
