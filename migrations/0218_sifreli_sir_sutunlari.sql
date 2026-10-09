-- 0218: at-rest AEAD ile saklanan iki sir kolonu, sifreli ciktiyi alamayacak kadar DARDI.
--
-- gizli.SaklaBagli ciktisi "gos1:" + base64(nonce 12 B + veri + etiket 16 B)'dir: duz degerden UZUNDUR.
--   users.totp_secret         32 karakterlik base32 secret -> 85 karakter  (kolon varchar(64), 0024)
--   git_repos.webhook_secret  40 karakterlik hex secret    -> 96 karakter  (kolon varchar(64), 0009)
-- Kolon dar olunca (STRICT_TRANS_TABLES, MariaDB varsayilani) yazim "Data too long for column" ile duser:
--   POST /me/2fa/enable                  -> 500 "kaydedilemedi"  (2FA HIC etkinlestirilemez),
--   POST /domains/{id}/git, /github/use  -> 500                  (git deploy baglanamaz).
-- Sifreleme 8045730 (2026-09-16) ile geldi; kolonlar genisletilmedi. Mevcut satirlar oldugu gibi kalir
-- (duz metin 64 karaktere sigiyordu); acilistaki sifre-heal onlari bu kolonlarda artik SIFRELEYEBILIR.
--
-- Her iki ifade de idempotenttir (ayni tanimi yeniden uygular).
ALTER TABLE users MODIFY COLUMN totp_secret VARCHAR(255) NOT NULL DEFAULT '';
-- webhook_secret UNIQUE indekslidir (uk_git_secret): 191 x utf8mb4 = 764 B, COMPACT/REDUNDANT satir bicimindeki
-- 767 B indeks anahtari sinirinin de altinda kalir (255 x 4 = 1020 B olurdu).
ALTER TABLE git_repos MODIFY COLUMN webhook_secret VARCHAR(191) NOT NULL;
