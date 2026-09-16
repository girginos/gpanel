-- OPS paneli icin metrik kapsaminin genisletilmesi.
--
-- 0019 yalnizca yuk (1/5/15) + bellek yuzdesi tutuyordu; CPU, disk, swap ve ag
-- gecmisi HIC yazilmiyordu. Zaman araligi secici bu metriklerde duz cizgi
-- gosterecekti. Kolonlar EKLEMELI (ADD COLUMN) — mevcut 7 gunluk yuk/bellek
-- gecmisi korunur, eski satirlarda yeni kolonlar 0 kalir.
--
-- net_rx_bps/net_tx_bps: anlik BAYT/SANIYE hizi (sayac degil). Toplayici iki
-- ornek arasindaki farki sureye bolerek yazar; sayac saklansaydi arayuz her
-- noktada delta almak zorunda kalir, sayac sifirlanmalarinda (arayuz reset,
-- yeniden baslatma) negatif sicramalar cizerdi.
ALTER TABLE sistem_yuk
  ADD COLUMN cpu_yuzde  FLOAT  NOT NULL DEFAULT 0,
  ADD COLUMN disk_yuzde FLOAT  NOT NULL DEFAULT 0,
  ADD COLUMN swap_yuzde FLOAT  NOT NULL DEFAULT 0,
  ADD COLUMN net_rx_bps BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN net_tx_bps BIGINT NOT NULL DEFAULT 0;
