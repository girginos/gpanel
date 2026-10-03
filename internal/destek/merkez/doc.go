// Package merkez, GirginOSPanel Telegram destek botunun MERKEZ servisidir
// (cmd/destek-hub; bot.girginos.io arkasında, ayrı kullanıcı ve ikili).
//
// Sorumluluklar:
//
//   - Telegram webhook'u (gizli başlık doğrulamalı, yalnız özel sohbet) ve
//     komutlar (/start <kod>, /hesaplarim, /durum, /insan, /dil, /unutbeni).
//   - Ajan uçları: WSS ve uzun yoklama; kayıt → merkez imzalı meydan okuma →
//     cihaz imzalı yanıt; eşleştirme (kod_ozet ↔ /start), bağlantı durumu,
//     İMZALI iş emri → is_sonucu.
//   - Claude orkestrasyonu: Haiku triyaj → Sonnet araç döngüsü → gerekirse
//     Opus yükseltmesi → Haiku çıkış denetimi. Araç girdisi merkezde de
//     protokol.AracGirdisiCoz ile doğrulanır (1. hat; bağlayıcı olan ajandır).
//   - Maliyet: prompt cache (tools+system sabit önek + artan konuşma kırılım
//     noktası), kullanım defteri, oturum token tavanı, sunucu/genel bütçe,
//     acil kesici, cache isabet oranı izleme.
//   - Çıkış süzgeci: maske ikinci geçiş, izinli bağlantılar, kimlik avı reddi.
//
// Sırlar YALNIZ systemd LoadCredential dizininden (CREDENTIALS_DIRECTORY)
// dosya olarak okunur; ortam değişkeninden sır okunmaz, log'a yazılmaz.
//
// Sözleşme internal/destek/protokol'dedir; bu paket onu yeniden tanımlamaz.
package merkez
