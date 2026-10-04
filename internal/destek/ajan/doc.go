// Package ajan — GirginOSPanel Telegram destek botunun ajanı (Faz 1; Faz 3'te panelden AYRI süreç).
//
// 🔴 Panel ve ajan AYRI süreçlerdir: ajan cmd/destek-ajan (girginospanel-destek-ajan.service)
// içinde çalışır (Baslat), panel yalnız arayüz uçlarını (Rotalar, DB), denetim-yalnız iptal
// kancasını (PanelKancasiKur) kullanır ve ajanı ÇALIŞTIRMAZ. Aralarındaki tek bağ veritabanıdır
// (izleme.go: ayar/iptal yoklaması ≤3 sn, nabız ≤10 sn) + panel-bağımlı araçlar için özel köprü
// (internal/destek/kopru).
//
// Görevler (ajan süreci):
//   - Cihaz kimliği: Ed25519 anahtar (ilk açılışta üretilir, 0600 + gizli.SaklaBagli).
//   - Bağlantı döngüsü: modül açıksa merkeze (bot.girginos.io) DIŞA bağlanır — WSS
//     (protokol.WSSYolu + alt protokol), kurulamazsa uzun yoklama; üstel geri çekilme
//   - titreşim; panik panel sürecini düşürmez. Merkez meydan okuması panelde
//     SABİTLENMİŞ merkez açık anahtarıyla doğrulanır (TLS Cloudflare'de sonlanır).
//   - Eşleştirme (v2, 2026-10-04): panelde YAPILMAZ — sunucu sahibi müşteri portalında
//     (app.girginos.io) kod alır; merkez imzalı eslesme_onayla_v2 (düzey, süre, portal onayı,
//     portal kullanıcısı imzayla kapsanır) gelince ajan bağlantıyı DOĞRUDAN etkin ve rol=kök
//     açar (yerel kod ve panelde ikinci onay yok). Panel uçları yalnız KÖK içindir: durum,
//     modül şalteri, liste, iptal. İptal: panelden, portaldan, Telegram'dan (/unutbeni) ve
//     otomatik (internal/destek/iptal + kimlik izi denetimi). Kayıttaki lisans bileti merkezde
//     doğrulanır; uzun oturum bileti tazelemek için BiletYenilemeAraligi'nda yeniden kurulur.
//   - İş emri: imza/zaman/tekrar → yerel bağlantı kaydı → etkin düzey (min kuralı)
//     → protokol.AracGirdisiCoz (YEREL kapsamla) → Yurutucu → maskeli IsSonucu.
//     Merkezin iddiası kapsamı DEĞİŞTİRMEZ (yerel otorite).
//
// Araç yürütücüsü ayrı pakettedir (internal/destek/arac); burada yalnız arayüzü
// (Yurutucu) tanımlıdır ve cmd/destek-ajan bağlar. Bağlı değilse iş emri "arac_yok" ile reddedilir.
package ajan
