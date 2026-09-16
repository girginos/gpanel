-- FTP parolasi artik $6$ SHA-512 crypt hash'i (~106 karakter); eski VARCHAR(64)
-- SIGMAZ (ERROR 1406). At-rest cleartext -> $6$ gecisinin sartidir.
ALTER TABLE ftp_accounts MODIFY password_md5 VARCHAR(255) NOT NULL;
