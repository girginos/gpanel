-- Lisans sunucusu adresini lic-eu'ya taşı (mevcut kurulumların onarımı).
--
-- NEDEN: 0055 `lisans_sunucu` ayarını 'https://app.girginos.io' ile tohumluyordu.
-- O vhost Cloudflare ARKASINDADIR ve panele bakan uçları (katalog/activate/
-- heartbeat) sunmaz — panel Eklentiler sayfasında "Uzak katalog alınamadı
-- (HTTP 404)" uyarısı bundan çıkıyordu. Adres çözümü env > cp_ayarlar > kod
-- sabiti önceliğiyle yapıldığı için, DB'deki bu satır koddaki DOĞRU varsayılanı
-- (https://lic-eu.girginos.io) eziyordu.
--
-- 🔴 Yalnızca ESKİ VARSAYILAN değeri günceller. Operatör bilerek kendi lisans
-- sunucusunu yazdıysa (özel/on-prem kurulum) o değere DOKUNULMAZ.
UPDATE cp_ayarlar
   SET deger = 'https://lic-eu.girginos.io'
 WHERE anahtar = 'lisans_sunucu'
   AND deger IN ('https://app.girginos.io', 'https://app.girginos.io/');

-- Satır hiç yoksa (0055'ten önceki kurulumlar) ekle.
INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('lisans_sunucu', 'https://lic-eu.girginos.io');
