-- AV hariç listesinden TEHLİKELİ göreli segmentleri çıkar.
--
-- 🔴 NEDEN: node_modules/ · .git/ · cache/ göreli segment olarak hariç
-- tutulunca, saldırgan shell'i `uploads/node_modules/shell.php` gibi bir alt
-- klasöre koyarak taramadan TAMAMEN kaçıyordu (adversaryel denetimde kanıtlandı).
-- Bu dizinlerde meşru .php nadir; ajanın `ilgiliUzanti` geçidi zaten .js/.map/.gz
-- gürültüsünü elediği için bu göreli hariçler GEREKSİZ ve saf kaçış vektörü.
-- Yalnız GERÇEK sistem/veri yolları (mutlak) hariç kalır.
--
-- Operatör kendi eklediği yolları KORUR: yalnız bizim koyduğumuz tehlikeli
-- göreli girdiler temizlenir (tam satır eşleşmesiyle).
UPDATE av_ayarlar SET haric_yollar = TRIM(BOTH '\n' FROM REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
  CONCAT('\n', haric_yollar, '\n'),
  '\n.git/\n', '\n'),
  '\nnode_modules/\n', '\n'),
  '\n/wp-content/cache/\n', '\n'),
  '\n/wp-content/uploads/cache/\n', '\n'),
  '\n\n', '\n'))
WHERE id = 1;
