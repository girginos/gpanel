# GirginOSPanel — WHMCS sağlama modülü

WHMCS 8.x (PHP 8.1+) için sunucu (provisioning) modülü. Hosting hesabı ve bayi (reseller)
ürünlerini panelin API anahtarı ile otomatik yönetir.

## Kurulum

1. `bash whmcs/build.sh` → `whmcs/dist/gpanel-whmcs-<sürüm>.zip` (ya da `modules/servers/gpanel`
   dizinini elle kopyalayın).
2. Zip'i WHMCS kök dizinine açın: sonuç `<WHMCS>/modules/servers/gpanel/gpanel.php`.
3. **Sunucu ekle** (Yönetim > Sistem Ayarları > Ürünler/Hizmetler > Sunucular):

| WHMCS alanı | Değer |
|---|---|
| Hostname | Panelin alan adı ya da IP'si |
| **Port** | **Yönetici kapısı portu** (panele tarayıcıyla girdiğiniz yönetici portu; rastgele port). Bayi anahtarı kullanacaksanız bayi kapısı portu |
| Kullanıcı adı | Anahtarın **sahibinin** panel kullanıcı adı (`root` ya da bayi kullanıcı adı) |
| **Parola** | API anahtarı (`gosp_…`) |
| Güvenli (SSL) | **İşaretli olmalı** (işaretsizse modül anahtarı göndermeyi reddeder) |
| Modül | GirginOSPanel |
| Access Hash | Boş. (`tls-dogrulama-kapali` yazmak TLS doğrulamasını kapatır — aşağıya bakın) |

Kimlik biçimi panelde `Authorization: gosp <kullanıcı adı>:<anahtar>`; kullanıcı adı anahtarın
sahibiyle eşleşmezse 401 alırsınız.

4. "Bağlantıyı Sına" (TestConnection): `GET /api/v1/api-anahtari/ben` çağırır ve anahtarda
   en az `hesap:yaz` ve `plan:oku` izni arar.

## API anahtarı izinleri

Panel > Ayarlar > API anahtarları. Varsayılan-reddet: seçilmeyen uç çalışmaz.

| İzin grubu | Gerekli olduğu işlevler |
|---|---|
| `hesap` (yaz) | oluştur, askı al/aç, sil, paket değiştir, kullanım güncelleme, müşteri alanı |
| `plan` (oku) | plan listesi / ad→ID eşleme, TestConnection |
| `ftp.parola` (yaz) | ChangePassword, oluşturma sonrası WHMCS parolasını panele uygulama |
| `sso` (yaz)* | tek tıkla giriş (`POST /api/v1/domains/{id}/sso`) |
| `bayi` (yaz) | **bayi ürünü** (yalnız KÖK anahtarı): bayi oluştur/askı/sil/paket/kullanım |

\* SSO ucu ayrı bir değişiklikle panele ekleniyor; izin grubunun adı oradaki karara göre
değişirse yalnızca hata mesajındaki grup adı etkilenir.

Anahtara IP kilidi tanımlıysa WHMCS sunucusunun çıkış IP'sini ekleyin.

## Ürün yapılandırması (Modül Ayarları sekmesi)

| Seçenek | Açıklama |
|---|---|
| Ürün tipi | `Hosting hesabı` ya da `Bayi hesabı` |
| Panel planı | Plan ID'si ya da **tam adı** (sunucu seçiliyse listeden de gelir). Boş = panelin varsayılan planı. Bulunamazsa hesap AÇILMAZ, net hata verilir |
| PHP sürümü | Örn. `8.3`; boş = plan/panel varsayılanı |
| Bayi paketi | Yalnız bayi ürünü; ID ya da ad |
| Varsayılan veritabanı aç | Hesapla birlikte MariaDB oluşturur |
| TLS doğrulamasını KAPAT | **Tehlikeli**: sertifika doğrulanmaz, araya girme (MITM) ile API anahtarı çalınabilir. Önce panele geçerli sertifika (Let's Encrypt) kurun. Varsayılan KAPALI (doğrula) |
| Panel adresi | `https://…:PORT/` müşteri/bayi kapısı; SSO yokken düz bağlantı + bayi ürününde giriş adresi |
| Mevcut hesabı bağla | Aşağıya bakın (güvenlik) |

Hizmetin **alan adı** panel hesabının alan adıdır; WHMCS "Kullanıcı adı" alanı oluşturma sonrası
panelin sistem kullanıcısıyla (FTP kullanıcısı), "Parola" WHMCS parolasıyla güncellenir
(`ftp.parola` izni yoksa panelin ürettiği parolayla).

**Bayi ürünü:** WHMCS "Kullanıcı adı" bayinin panel kullanıcı adı olur (3-32 karakter, küçük
harf/rakam/`._-`); parola 8-128 karakter. Bayi parolası API ile **değiştirilemez** (panel kısıtı).

## Hesap eşleme ve güvenlik

* WHMCS hizmeti ↔ panel hesap kimliği `mod_gpanel_hesap` tablosunda tutulur (ilk kullanımda otomatik
  oluşur). Yönetici > Hizmet > "Panel hesap kimliği" alanından elle bağlanabilir (kimlik panelde
  doğrulanır: alan adı/kullanıcı adı uyuşmalı, başka hizmete bağlı olmamalı).
* **Hesap devralma engeli:** panelde zaten var olan bir alan adı, "Mevcut hesabı bağla" kapalıyken
  yeni siparişe BAĞLANMAZ (aksi halde bir müşteri başkasının sitesini yönetebilirdi). Yarım kalmış
  bir oluşturmanın yeniden denenmesi ("olusturma niyeti" işareti) güvenle devam eder.
* `CreateAccount` idempotent: tekrar çalıştırmak ikinci hesap açmaz.
* Günlük (Modül Günlüğü): API anahtarı, parolalar, panelin ürettiği parolalar ve SSO tek kullanımlık
  kodu **maskelenir**.
* Müşteri alanı işlevleri `serviceid`'yi yalnız WHMCS'in verdiği `$params`'tan alır; parola formu
  WHMCS CSRF jetonuyla (`token`) korunur ve yalnız POST kabul eder.

## Tek tıkla giriş (SSO)

`ServiceSingleSignOn` (müşteri) ve `AdminSingleSignOn` (yönetici) panelin
`POST /api/v1/domains/{id}/sso` ucunu çağırır (`{"ok":true,"url":"https://host:port/#kod","gecerlilik_sn":60}`)
ve tarayıcıyı o adrese yönlendirir. Adres https olmalı, kullanıcı bilgisi içermemeli ve ana bilgisayarı
sunucu ayarındaki hostname/IP (ya da "Panel adresi") ile aynı olmalıdır; aksi halde reddedilir.
Uç yoksa (404) ya da anahtarda izin yoksa (403): **müşteri** düz panel bağlantısına yönlendirilir,
**yönetici** nedeni görür. Hesap askıdaysa (409) giriş yapılmaz.

## Kullanım güncelleme

`UsageUpdate` her sunucu için **tek** `GET /api/v1/domains` çağrısı yapar (panel listeyi sayfalamaz),
yalnız panel hesabına bağlı Active/Suspended hizmetlerin `tblhosting` disk/trafik/limit alanlarını
günceller (200'lük parçalarla). Panel hata verirse veritabanına dokunulmaz.

## Sorun giderme

| Mesaj | Anlamı |
|---|---|
| "API anahtarı geçersiz" (401) | Anahtar yanlış/iptal/süresi dolmuş ya da Kullanıcı adı anahtar sahibiyle eşleşmiyor ya da yanlış kapı portu |
| "API anahtarında 'X' izni eksik" (403) | Anahtara ilgili izin grubunu ekleyin; bayi anahtarı kök uçlarına (bayi ürünü) giremez |
| "Panele bağlanılamadı …" | Ağ/DNS/TLS: sertifika geçerli mi? Port yönetici kapısı mı? Güvenlik duvarı/IP kilidi |
| "… bağlı değil" | Hizmet bir panel hesabına bağlanmamış: Yönetici > Hizmet > Panel hesap kimliği |
| "alan adı panelde zaten var" | Bkz. hesap devralma engeli |
| "Panel planı bulunamadı" | Ürün ayarındaki plan ID/ad yanlış |
| 422/400 doğrulama | Panelin mesajı aynen gösterilir (ör. parola kuralı: ≥12 karakter, harf+rakam) |

Modül Günlüğü: Yönetim > Sistem Günlükleri > Modül Günlüğü (Modül Günlüğünü Etkinleştir).

## Geliştirme / test

```
php whmcs/tests/run.php        # sahte panel + WHMCS saplamaları, PHPUnit gerekmez
bash whmcs/tests/mutasyon.sh   # elle mutasyon testi (18 mutant)
bash whmcs/build.sh            # test + zip
```
Testteki sahte panel yanıtları `internal/` Go kodundaki JSON etiketlerinden alınmıştır; izin tablosu
`cmd/server/testdata/api_anahtar_uclari.golden` ile karşılaştırılır.

## WHMCS işlevi → panel ucu → izin

| WHMCS işlevi | Panel ucu | İzin |
|---|---|---|
| TestConnection | `GET /api-anahtari/ben` | (her zaman) |
| CreateAccount (hosting) | `GET /domains` (var mı), `GET /plans`, `POST /domains`, `PUT /domains/{id}/ftp/password` | hesap:yaz, plan:oku, ftp.parola:yaz |
| CreateAccount (bayi) | `GET /resellers`, `GET /reseller-plans`, `POST /resellers` | bayi:yaz |
| SuspendAccount / UnsuspendAccount | `POST /domains/{id}/askiya-al` · `askidan-al` (bayi: `PUT /resellers/{id}` `durum`) | hesap:yaz (bayi:yaz) |
| TerminateAccount | `DELETE /domains/{id}?onay=<alan>` (bayi: `DELETE /resellers/{id}`) | hesap:yaz (bayi:yaz) |
| ChangePackage | `GET /plans` + `PUT /domains/{id}/plan` (bayi: `PUT /resellers/{id}` `paket_id`) | hesap:yaz, plan:oku (bayi:yaz) |
| ChangePassword | `PUT /domains/{id}/ftp/password` | ftp.parola:yaz |
| UsageUpdate | `GET /domains` (+ bayi için `GET /resellers`) | hesap:oku (bayi:oku) |
| Service/AdminSingleSignOn | `POST /domains/{id}/sso` | sso |
| ClientArea / kullanım yenile | `GET /domains/{id}` | hesap:oku |
