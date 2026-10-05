-- 0170: bayi ozel panel adresi TEK Uygula eylemi (cp_marka_alanadi adim izleme).
-- adim: durum makinesinin su anki adimi (dogrulama, dns_kontrol, dns_eklendi, dns_bekleniyor, dns_cakisma, dns_red, dns_zamanasimi, ssl).
-- Bos adim eski elle akisin satiri demektir ve eski dogrula yolu ona aynen uygulanir.
-- adim_mesaj: bayiye gosterilen sade metin. dns_kaynak: panel (bolge bu sunucuda) ya da harici (TXT sartli).
-- yoklama_bitis: arka plan DNS yoklamasinin bitis zamani (UTC). adim_zaman: son adim guncellemesi (UTC).
-- Idempotent: ADD COLUMN IF NOT EXISTS. Satir ici yorum yok ve metinde noktali virgul yok.
ALTER TABLE cp_marka_alanadi
  ADD COLUMN IF NOT EXISTS adim VARCHAR(24) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS adim_mesaj VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS dns_kaynak VARCHAR(8) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS yoklama_bitis DATETIME NULL DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS adim_zaman DATETIME NULL DEFAULT NULL
