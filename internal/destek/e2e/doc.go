// Package e2e — Telegram destek botu Faz 1 MERKEZ ↔ AJAN uçtan uca testleri.
//
// Gerçek merkez (internal/destek/merkez: webhook, WSS ve uzun yoklama sunucusu,
// imzalı meydan okuma/eşleştirme/iş emri, Claude araç döngüsü, çıkış süzgeci,
// dosya deposu) gerçek ajana (internal/destek/ajan: panel uçları, eşleştirme,
// imza doğrulama, iş emri denetim zinciri, iptal) ve gerçek araç yürütücüsüne
// (internal/destek/arac — yalnız dış dünya dikişleri sahte) karşı SÜREÇ İÇİNDE
// koşar. Dış dünya: sahte Claude istemcisi + sahte Telegram Bot API sunucusu
// (httptest) + geçici MariaDB (GOSP_TEST_GECICI_DSN yoksa testler atlanır).
// Gerçek Telegram ya da Anthropic çağrısı YAPILMAZ.
//
// Paket yalnız test içerir; üretim kodu bu paketi içe aktarmaz.
package e2e
