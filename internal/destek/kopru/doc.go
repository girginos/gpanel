// Package kopru — destek ajanı (ayrı süreç, girginospanel-destek-ajan) ile panel
// süreci arasındaki ÖZEL, yalnız-kök Unix soket köprüsü.
//
// NEDEN: panel-bağımlı araçlar (hesap_ozet, ssl_durum, …) panelin salt-okunur GET
// uçlarını süreç İÇİ çağırır ve kimliği middleware.DestekBagla context işaretiyle
// kurar; bu işaret HTTP üzerinden taklit edilemez. Ajan panelden ayrı bir sürece
// taşınınca işaret hâlâ YALNIZ panel sürecinde konmalıdır: bu yüzden ajan HTTP
// başlığı/jetonu değil, bu köprüyü kullanır ve köprü TEK giriş kapısıdır.
//
// Sözleşme (bire bir):
//
//   - Soket: /run/girginospanel/destek-kopru.sock — dizin 0700 root, soket 0600 root.
//     TCP/ağ dinleyicisi YOK.
//   - Her bağlantıda SO_PEERCRED ile eş süreç uid'si denetlenir: yalnız uid 0.
//     Başka uid'ler hiçbir bayt okunmadan kapatılır.
//   - İstek: tek JSON nesnesi (≤ IstekSiniri bayt), ardından yazma yarı-kapatılır.
//     {kimlik:{rol,kullanici_id,kullanici,domain_id,alan_adi,tg_kisa}, yol, sorgu}
//     (ya da {ping:true}: panelin köprüyü işlediğini yoklar, hiçbir uç çağrılmaz).
//   - Sunucu isteği middleware.DestekYolIzinli ile (VARSAYILAN RED, yalnız GET, yalnız
//     DestekRotalari kalıbı) yeniden doğrular, sonra mevcut süreç içi yönlendirici
//     çağrısını DestekBagla ile yapar (ikinci kapı: destekIstek RoutePattern denetimi).
//   - Yanıt: tek JSON nesnesi {kod, govde(base64), asildi}; govde ≤ YanitSiniri.
//
// Köprü yoksa/reddederse/zaman aşımına uğrarsa istemci ErrYok döner; araç katmanı
// bunu "panel_yanit_vermiyor" olarak raporlar.
package kopru
