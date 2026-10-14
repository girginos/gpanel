-- 0221_av_haric_varsayilan.sql — 0078'in eski varsayılan hariç tutma listesini günceller.
--
-- 0078_av_ayarlar.sql 2026-09-16'da varsayılan listeye `.git/`, `node_modules/` ve WordPress önbellek
-- dizinlerini ekledi. 0078'i bu tarihten ÖNCE uygulamış sunucularda göç yeniden uygulanmaz: satır eski
-- varsayılanda kalır, antivirüs bu dizinleri boşuna tarar ve doğrulama her güncellemede 0078 için
-- "HASH MISMATCH" uyarısı verir.
--
-- Yalnız satır HÂLÂ eski varsayılansa (yönetici listeyi değiştirmemişse) yeni varsayılana çekilir;
-- özelleştirilmiş listeye dokunulmaz. İdempotent: ikinci koşuda eşleşen satır kalmaz.
UPDATE av_ayarlar
   SET haric_yollar = '/proc\n/sys\n/dev\n/run\n/var/lib/mysql\n/var/lib/containers\n/var/cache\n/var/backups\n/opt/girginospanel/frontend-dist\n.git/\nnode_modules/\n/wp-content/cache/\n/wp-content/uploads/cache/'
 WHERE id = 1
   AND haric_yollar = '/proc\n/sys\n/dev\n/run\n/var/lib/mysql\n/var/lib/containers\n/var/cache\n/var/backups\n/opt/girginospanel/frontend-dist';
