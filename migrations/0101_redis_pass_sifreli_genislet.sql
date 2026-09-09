-- redis_pass at-rest sifreleme: gizli.SaklaBagli ciktisi ("gos1:" + base64)
-- 36 karakterlik parola icin ~91 karakter -- 0022'deki VARCHAR(64)'e SIGMAZ
-- (strict modda Error 1406 => Redis etkinlestirme komple patlar).
-- MODIFY idempotent: ayni tipe tekrar uygulanmasi zararsiz.
ALTER TABLE cp_domain_redis MODIFY redis_pass VARCHAR(255) NOT NULL;
