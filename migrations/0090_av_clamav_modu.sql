-- Antivirüs motorunu GEÇİCİ olarak clamav'a al.
-- Kendi kural motorumuz (avmotor) meşru framework dosyalarında (Compiler.php,
-- StorageServiceProvider.php, ActionScheduler_Abstract_ListTable.php, UrlChannel.php)
-- yanlış-pozitif fırtınası üretiyordu. kural_motoru=0 iken avajan clamscan imzalarını
-- kullanır ("sunucu motoru == clamav"). Geri dönüş: panelden "Kural motoru" tekrar açılır.
UPDATE av_ayarlar SET kural_motoru = 0 WHERE id = 1;

-- Gerçek zamanlı korumayı da kapat. Backend (avayar.Oku/Yaz) zaten zorla kapatıyor;
-- DB satırını da hizala ki panel durumu tutarlı olsun ve izleyici servisi kalkmasın.
UPDATE av_ayarlar SET gercek_zamanli = 0 WHERE id = 1;
