-- 0056_lisans_uyari.sql — Lisans GEÇERLİ ama dikkat gerektiren durum.
--
-- 🔴 son_hata'dan AYRI tutulur. son_hata "çalışmıyor, sebebi bu" demektir ve
-- başarılı nabızda temizlenir. uyari ise "çalışıyor AMA şu sorun var" demektir
-- (bugünkü tek örneği: bu lisansın bir kopyası başka bir yerde çalışıyor).
-- Aynı kolona koysaydık ya uyarı başarıda silinir ya da müşteri çalışan bir
-- kurulumu arızalı sanardı.
ALTER TABLE cp_eklenti_lisans ADD COLUMN uyari TEXT NULL;
