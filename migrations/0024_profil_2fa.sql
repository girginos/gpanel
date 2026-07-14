ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret  varchar(64) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled tinyint(1)  NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS tercih_tema  varchar(8)  NOT NULL DEFAULT 'system';
ALTER TABLE users ADD COLUMN IF NOT EXISTS tercih_dil   varchar(8)  NOT NULL DEFAULT 'tr';
UPDATE users SET email='', full_name='' WHERE username='root' AND email='admin@local' AND full_name='Sistem Yöneticisi';
