-- Windows ajanlarina sertifika parmak izi (TOFU igneleme).
-- Ajan artik TLS konusuyor; panel ilk kayitta ajanin self-signed sertifikasinin
-- SHA256 parmak izini (64 hex karakter) ogrenip burada saklar ve sonraki her
-- cagrida zorlar. Sertifika degisirse cagri kesilir, durum
-- 'parmak-izi-uyusmazligi' olur; igne ancak operatorun yeniden kaydiyla tazelenir.
-- Bos deger ('') = 0099 oncesi eski kayit: ilk Sina cagrisi mevcut sertifikayi
-- TOFU ile ogrenip bu kolona yazar (winajan.Sina).
ALTER TABLE windows_ajanlar ADD COLUMN parmak_izi VARCHAR(64) NOT NULL DEFAULT '';

-- 'parmak-izi-uyusmazligi' (22 karakter) 0098'deki VARCHAR(16)'ya sigmazdi;
-- kirpilmis/reddedilen durum yazmamak icin kolon genisletilir. Veri kaybi yok.
ALTER TABLE windows_ajanlar MODIFY durum VARCHAR(32) NOT NULL DEFAULT 'bilinmiyor';
