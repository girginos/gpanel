# API: müşteri tek tık girişi (SSO)

WHMCS gibi bir faturalama sistemi, hosting müşterisini panele **tek tıkla** girdirebilir.
Uç, API anahtarıyla çağrılır ve kısa ömürlü, tek kullanımlık bir bağlantı döner.

## Uç

```
POST /api/v1/domains/{id}/sso
Authorization: gosp <kullanıcı adı>:<anahtar>
```

- Gövde: boş ya da `{}`.
- İzin: anahtarda **`sso`** izni ("Müşteri paneline tek tık giriş"; tek düzey, varsayılan KAPALI).
  Roller: yönetici ve bayi. Bayi anahtarı yalnız kendi alan adlarına girdirebilir.
- Başarı (200):

```json
{"ok": true, "url": "https://panel.example.com:25723/adina#kod=...", "gecerlilik_sn": 60}
```

`url` müşteriyi (tarayıcıda yönlendirerek) panele girdirir. Kod URL **parçasındadır** (`#`):
sunucuya, nginx günlüğüne ve Referer'a gitmez; panel sayfası kodu adres çubuğundan hemen siler.

| Kod | Anlam |
|-----|-------|
| 401 | anahtar/kullanıcı adı geçersiz, iptal ya da süresi dolmuş |
| 403 | anahtarın `sso` izni yok, IP/kapı uyumsuz, ya da JWT oturumuyla çağrıldı (yalnız API anahtarı) |
| 404 | alan adı yok **ya da** bayi kapsamı dışı (ikisi aynı yanıt; varlık sızdırılmaz) |
| 409 | hesap askıda (müşteri girişi yapılamaz) ya da hostingin panel kullanıcısı yok |
| 429 | hız sınırı (anahtar başına dakikada 30 SSO isteği; ayrıca genel API hız sınırı) |
| 503 | müşteri paneli adresi çözülemedi / hesap durumu doğrulanamadı |

## Örnek

```bash
curl -s -X POST https://panel.example.com:25723/api/v1/domains/42/sso \
  -H "Authorization: gosp bayi_adi:gosp_xxxxxxxxxxxx_..." 
# → {"ok":true,"url":"https://panel.example.com:25723/adina#kod=...","gecerlilik_sn":60}
```

WHMCS tarafı: `url` döner dönmez müşteriyi oraya yönlendirin (kodu saklamayın/loglamayın).

## URL'nin ana bilgisayarı ve portu

- Ana bilgisayar: isteğin geldiği Host; IP ya da `localhost` ise panel ana bilgisayar adı
  ayarı (panelhost) varsa o, yoksa isteğin host'u (sunucu IP'si).
- Port: rol kapıları açıksa **müşteri kapısı**, değilse panelin tek portu.

## Güvenlik

- Kod 32 bayt rastgele, **60 sn** geçerli, **tek kullanımlık**; bellekte yalnız SHA-256 özetiyle
  tutulur. Yalnız müşteri kapısındaki mevcut `POST /musteri/adina-giris` tüketicisiyle
  müşteri-adına oturuma çevrilir (yeni paralel mekanizma yok).
- Oturum bir "müşteri adına oturum"dur (30 dk, yenilenmez): **müşteri parolası ve kimlik işlemleri
  yapılamaz** (403, `AdinaYasak`); alan adı sahipliği ve aktör hesabı her istekte yeniden doğrulanır.
- **Askıdaki hesaba giriş yok**: üretimde 409; kod üretildikten sonra askıya alınırsa tüketimde ve
  açık oturumda reddedilir (olağan bayi "panele gir" akışından FARKI: orada bayi askıdaki hesabı inceleyebilir).
- Denetim: `musteri.sso_kod` ("API anahtarıyla müşteri girişi"; aktör `sahip (API anahtarı ad)`) ve
  tüketimde `musteri.adina_oturum` (ayrıntıda anahtar adı). **Kod hiçbir kayıtta/log'da yoktur.**
- Yanıt `Cache-Control: no-store`. Kod yalnız yanıt gövdesindeki URL'de bulunur.
- Anahtar kuralları aynen geçerlidir: IP izin listesi, kapı uyumu, tarayıcı (Origin) reddi.
- Yönetim arayüzünde (API anahtarları) `sso` izni ayrı bir satırdır; yeni anahtarlarda kapalı gelir.
