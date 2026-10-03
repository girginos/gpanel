// Package ajan — GirginOSPanel Telegram destek botunun PANEL İÇİ ajanı (Faz 1).
//
// Görevler:
//   - Cihaz kimliği: Ed25519 anahtar (ilk açılışta üretilir, 0600 + gizli.SaklaBagli).
//   - Bağlantı döngüsü: modül açıksa merkeze (bot.girginos.io) DIŞA bağlanır — WSS
//     (protokol.WSSYolu + alt protokol), kurulamazsa uzun yoklama; üstel geri çekilme
//   - titreşim; panik panel sürecini düşürmez. Merkez meydan okuması panelde
//     SABİTLENMİŞ merkez açık anahtarıyla doğrulanır (TLS Cloudflare'de sonlanır).
//   - Eşleştirme: panel uçları kod üretir (10 dk, tek kullanımlık, bellekte yalnız
//     SHA-256 özeti); merkez imzalı eslesme_onayla → bekleyen kayıt; panelde İKİNCİ
//     ONAY (her rol) → aktif. İptal: panelden, Telegram'dan (/unutbeni) ve otomatik
//     (internal/destek/iptal + kimlik izi denetimi).
//   - İş emri: imza/zaman/tekrar → yerel bağlantı kaydı → etkin düzey (min kuralı)
//     → protokol.AracGirdisiCoz (YEREL kapsamla) → Yurutucu → maskeli IsSonucu.
//     Merkezin iddiası kapsamı DEĞİŞTİRMEZ (yerel otorite).
//
// Araç yürütücüsü ayrı pakettedir (internal/destek/arac); burada yalnız arayüzü
// (Yurutucu) tanımlıdır ve main.go bağlar. Bağlı değilse iş emri "arac_yok" ile reddedilir.
package ajan
