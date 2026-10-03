// Package protokol, GirginOSPanel Telegram destek botunun SÖZLEŞME paketidir.
// Panel içi ajan (internal/destek), merkez (cmd/destek-hub, bot.girginos.io)
// ve Claude araç tanımları YALNIZ bu paketteki tiplere ve kurallara uyar;
// mesaj, imza, araç şeması ya da hata kodu başka yerde yeniden tanımlanmaz.
//
// # Taraflar ve taşıma
//
// Ajan (müşteri sunucusundaki girginospanel) merkeze DIŞA bağlanır:
// WSS [WSSYolu] (alt protokol [WSAltProtokol]). WSS kurulamazsa aynı mesajlar
// uzun yoklama uçlarından ([YoklamaKayitYolu], [YoklamaYanitYolu],
// [YoklamaAlYolu], [YoklamaGonderYolu]) taşınır. Her çerçeve tek bir [Zarf]
// JSON nesnesidir; en çok [CerceveSiniri] bayt.
//
// # Akış
//
//  1. ajan → kayit: cihaz Ed25519 açık anahtarı, lisans parmak izi + ispatı,
//     panel sürümü, [KatalogOzeti].
//  2. merkez → meydan_okuma: 32 bayt rastgele, MERKEZ ANAHTARIYLA İMZALI.
//     Merkez Cloudflare arkasında olduğundan TLS merkezi kanıtlamaz; ajan
//     merkezi panele gömülü (sabitlenmiş) açık anahtarla doğrular.
//  3. ajan → meydan_yanit: cihaz anahtarıyla imza ([MeydanYanitla]).
//  4. merkez → kayit_sonuc.
//  5. kalp_atisi iki yönde [KalpAraligi] ile; [KalpZamanAsimi] sessizlikte
//     bağlantı kapatılır.
//
// Eşleştirme: ajan kod_ozet (tek kullanımlık kodun SHA-256 özeti) bildirir →
// müşteri Telegram'da /start <kod> → merkez eslesme_onayla (İMZALI) → ajan
// kodu tüketir, panelde ikinci onayı bekler → baglanti_durum (hesap sıra
// numaraları + etiketleri, rol, etkin düzey). baglanti_iptal iki yönde
// gönderilebilir; iptal yalnız yetkiyi düşürür.
//
// İş: merkez → is_emri (İMZALI) → ajan [Dogrulayici.Dogrula] +
// [AracGirdisiCoz] (YEREL oturum bağlamıyla; merkezin iddiası kapsamı
// değiştirmez) → yürütür → is_sonucu (maskeli, kesik satırlar).
//
// # Katı çözümleme
//
// Zarf ve tüm gövdeler katı çözülür: bilinmeyen alan, yalnız büyük/küçük harfi
// farklı alan, yinelenen anahtar, nesne sonrası ek veri → ret. surum alanı
// [Surum] değilse surum_desteklenmiyor. Her mesaj tipinin bir yönü vardır
// ([Tip.Yonu]); ters yönden gelen tip reddedilir.
//
// # İmza
//
// Ed25519. İmza JSON üzerinden DEĞİL, [KanonikBaytlar] ile üretilen açık
// tanımlı bayt dizisi üzerinden atılır (biçim imza.go'da). Her imzalı mesaj
// nonce + verilme + son_gecerlilik taşır; son_gecerlilik − verilme ≤
// [EnUzunGecerlilik] (60 sn). Doğrulama sırası: biçim → anahtar → imza →
// zaman → tekrar önbelleği (önbelleğe yalnız imzası geçerli mesaj yazılır).
//
// # Araçlar
//
// [Katalog] tek kaynaktır. [ClaudeAraclari] Messages API "tools" dizisini
// (strict:true, additionalProperties:false) üretir; [AracGirdisiCoz] modelden
// ya da iş emrinden gelen girdiyi aynı tanıma göre katı doğrular. Claude'un
// strict şemasının desteklemediği kısıtlar (uzunluk, desen) şemaya yazılmaz,
// açıklamaya eklenir ve ayrıştırıcıda zorlanır. "hesap" parametresi oturumdaki
// sıra numarasıdır; enum'u çalışma zamanında doldurulur, modelden alan adı ya
// da kimlik alınmaz.
package protokol
