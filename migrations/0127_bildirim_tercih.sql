-- 0127: bildirim tercihleri (D3b). Goruntuleyen basina, olay turune (kategori) gore
-- panel ici bildirim ac/kapat. okuyan_tur/okuyan_id sozlugu cp_bildirim_okundu ile AYNI
-- (admin=users.id, bayi=users.id, musteri=domains.id). Satir yoksa kategori ACIKTIR.
-- Kapali kategori listede/sayacta gizlenir (cp_bildirim satiri paylasimlidir, silinmez).
--
-- E-posta kanali YOK: panelde giden posta altyapisi yok (tasarim 1.2). Kanal eklenince
-- ayri goc ile eposta sutunu eklenecek.
--
-- Idempotent. Satir ici yorum YOK.
CREATE TABLE IF NOT EXISTS cp_bildirim_tercih (
  okuyan_tur VARCHAR(8)      NOT NULL,
  okuyan_id  BIGINT UNSIGNED NOT NULL,
  kategori   VARCHAR(32)     NOT NULL,
  panel      TINYINT(1)      NOT NULL DEFAULT 1,
  guncelleme TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (okuyan_tur, okuyan_id, kategori)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
