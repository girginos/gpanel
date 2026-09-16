-- Kullanılmayan alt alan adı erişim tablolarını kaldır.
--
-- 0093 bu iki tabloyu, alt alan adı desteğini de kapsayacağı varsayımıyla
-- oluşturmuştu. Karar: alt alan adı kapsam DIŞI — ihtiyaç duyan kullanıcı
-- alt alanı ayrı bir domain olarak ekleyip tam kısıtlama alabiliyor.
--
-- 🔴 Boş tabloları bırakmak zararsız değil: şemaya bakan bir geliştirici
-- alt alanların kapsandığını sanır. Kod incelemesi bunu ayrıca işaretledi
-- ("tabloların varlığı 'kapsanıyor' izlenimi veriyor — yanlış güvence").
-- Kapsanmayan bir şeyin şemada yeri olmamalı.
--
-- Güvenli: kod tabanında bu tablolara SIFIR referans var (doğrulandı) ve
-- hiçbir zaman veri yazılmadı. İleride alt alan desteği eklenirse yeni bir
-- göç dosyasıyla yeniden oluşturulur.

DROP TABLE IF EXISTS subdomain_erisim_kurallari;
DROP TABLE IF EXISTS subdomain_erisim_kisit;
