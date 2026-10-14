# Değişiklik günlüğü — GirginOSPanel WHMCS modülü

## 1.0.9
- **Bayi siparişi bayinin kendi hostingini de açar** (cPanel/WHM gibi) — hizmette alan adı varsa bayi
  açıldıktan sonra o alan adı için BAYİYE AİT hosting açılır (gPanel `POST /domains` `bayi_id`; ürünün
  hosting planı, PHP ve veritabanı ayarları kullanılır). Hosting parolası WHMCS hizmet parolasıdır.
  Yeniden denemede ikinci hosting açılmaz; alan adı başka hesaba aitse açık hata. Askı ve silme bayiyle
  birlikte hostingi de kapsar (panel).
- **Müşteri alanında iki giriş düğmesi:** bayi ürününde "Bayi paneline giriş yap" + "Hosting paneline giriş
  yap" (`?gp_hedef=hosting`, müşteri kapısı); hosting ürününde tek "Panele giriş yap". Düz panel bağlantısı
  düğmesi kalktı (tek tık giriş kullanılamazsa WHMCS zaten düz adrese yönlendirir).
- **Tek şifre formu:** bayi ürününde form bayi paneli parolasını VE bayinin hosting parolasını birlikte
  değiştirir (yönetici "Şifre değiştir" de aynı). "Bayi panelinde şifreyi değiştir" bağlantısı kalktı.
- Paket değişiminde üründe hosting planı seçiliyse bayinin hostingine de uygulanır.
- Bayi ürününde "Kaynak kullanımı" bölümünden "Toplam alan adı" ölçeri kalktı: aynı bilgi "Hesap
  bilgileri"nde ("n / limit") zaten gösteriliyor.

## 1.0.8
- **Bayi ürününde tek tıkla bayi paneline giriş** — "Bayi paneline giriş yap" düğmesi bayiyi parola
  sormadan bayi paneline alır (gPanel `POST /resellers/{id}/sso`; kök API anahtarında `sso` ve
  `bayi` (yazma) izni gerekir). "Bayi panelinde şifreyi değiştir" aynı yolla oturum açıp Profil
  sayfasını açar (`?gp_hedef=profil`, yalnız beyaz liste). Uç yoksa (eski panel) ya da anahtarda
  izin yoksa müşteri alanı düz bayi paneli bağlantısına düşer. Askıdaki bayide düğme pasif.
- **Bayi şifresi WHMCS'ten sıfırlanır** — yönetici "Şifre değiştir" (ChangePassword) ve müşteri
  alanındaki şifre formu bayi panel parolasını sıfırlar (gPanel `PUT /resellers/{id}/password`; kök
  anahtarında AYRI `bayi.parola` izni gerekir, varsayılan kapalı). Panel sıfırlamada bayinin açık
  oturumlarını kapatır ve bayi API anahtarlarını iptal eder. Eski panelde açık hata (uç yok); 1.0.7'ye
  kadar bayi şifre değişikliği her zaman hata veriyordu.
- **CPU ve RAM göstergeleri bayi sayfasında her zaman** — havuz limiti yoksa "Sınırsız" yazar
  (kullanım ölçülmüyorsa "—"); 1.0.6'da limit tanımlı değilse hiç görünmüyordu.

## 1.0.7
- **WHMCS ürün türü "Reseller Account" ise bayi hesabı açılır** — "Ürün tipi" boş ya da varsayılan
  (hosting) kalmış olsa da. WHMCS'in `producttype` parametresi, yoksa ürün kaydı (`tblproducts.type`)
  okunur. 1.0.6'ya kadar yalnız modülün kendi "Ürün tipi" alanına bakılıyordu: türü Reseller Account
  olan ürün HOSTING hesabı açıyordu (bir müşteri sunucusunda görüldü, 2026-10-13).

## 1.0.6
- **Bayi ürünü sayfası:** kaynak havuzu göstergeleri — işlemci (vCPU) ve bellek (RAM) limiti ve
  anlık kullanım (`GET /bayi-kaynak/{id}`, gPanel 0.3.0-20261013+, izin `bayi:oku`); toplam alan adı
  ölçeri ("Toplam alan adı"); "Bayi panelinde şifreyi değiştir" bağlantısı bayi panelinin Profil
  sayfasına gider (bayi parolası API ile değiştirilemez — kimlik devralma koruması; oturum yoksa
  panel girişten sonra Profil'e döner). Havuz tanımlı değilse ya da eski panelde yalnız paket
  ölçerleri görünür.

## 1.0.5
- **Tüm ürün ayarları "basit modda" görünür.** WHMCS 8+ basit modda yalnız `SimpleMode` alanlarını
  gösterir ve kaydeder; 1.0.4'e kadar yalnız plan ve bayi paketi işaretliydi. Ürün tipi, PHP
  sürümü, veritabanı, panel adresi gizli kalıyordu: tipi seçilemeyen bayi ürünü HOSTING olarak
  açılıyordu.
- **Panel adresi otomatik:** alan boşsa adres panelden alınır (`GET /kapi/portlar`, gPanel
  0.3.0-20261013+) — hosting ürünü müşteri paneline, bayi ürünü bayi paneline gider ("Panele
  giriş" ve müşteri alanındaki panel bağlantısı). Sunucu ayarındaki ad korunur, yalnız port
  seçilir. Eski panelde bayi ürünü için bayi kapısı yine elle girilmeli; hosting eski davranış.
- Testler: tüm seçenekler SimpleMode, otomatik müşteri/bayi adresi, açık "Panel adresi" önceliği,
  eski panelde geri düşüş.

## 1.0.4
- **PHP sürümü ayarı açılır liste oldu** (7.4–8.5 ve "Plandan / panelden"). 1.0.3'e kadar serbest
  metindi: `php81`, `PHP 8.1`, `8.1.2` gibi yazımlar panele ham gidiyordu ve panel tanımadığı değeri
  SESSİZCE 8.3 yapıyordu (8.1 isteyen 8.3 alıyordu). Modül artık eski kayıtlı değerleri de kanonik
  sürüme çevirerek gönderir (`php81` → `8.1`); tanınmayan değer olduğu gibi gider ve panel açık
  iletiyle reddeder. Panel 20261013'ten itibaren aynı kuralı kendisi de uygular.
- Liste dışı eski bir değer kayıtlıysa ürün ayarı ilk açılışta "Plandan / panelden" gösterir;
  ürünü kaydetmeden önce doğru sürümü seçin.
- Testler: kanonikleştirme tablosu, ayarın açılır liste sözleşmesi, eski serbest metin değerinin
  panele kanonik gitmesi; sözdizimi denetimi `php -n -l` (ini/eklenti yüklemeden).

## 1.0.3
- **Müşteri alanı yeniden tasarlandı.** Üst kart (alan adı + durum/plan/PHP/SSL rozetleri), "Panele
  giriş" (SSO) ve panel adresi YENİ SEKMEDE açılır; disk/trafik ölçerleri MB/GB + yüzde, %75 uyarı /
  %90 kritik renk; hesap bilgileri iki sütun (FTP/IP tek tıkla seçilir); parola formu katlanır bölümde;
  askıdaki hesapta uyarı bandı + pasif giriş düğmesi. Bayi ürününde trafik ölçeri yok (panel bayi
  trafiğini ölçmez) → hosting hesabı sayısı ölçeri. Six (BS3) ve Twenty-One (BS4) uyumlu, kapsamlı
  `.gpca` CSS (`{literal}` içinde), mobilde tek sütun. Yeni dil anahtarları (tr/en).
- **Yönetici/sistem metinleri çok dilli (tr/en).** Yönetici dili Türkçe ise Türkçe, değilse
  İngilizce: ürün ayarları (Modül Ayarları) etiket/açıklamaları ve seçenekleri, SSO düğme etiketleri,
  yönetici hizmet sekmesi ("Panel account ID" / "Panel link status"), tüm modül hata metinleri
  (bağlantı testi, oluşturma/askı/silme/paket/parola, SSO, kullanım güncelleme, parola kuralı) ve
  Etkinlik Günlüğü kayıtları. Yeni `lib/Ceviri.php`: kaynak metin kodda Türkçe kalır, İngilizce
  sözlük (91 metin), `{0}` yer tutucuları.
- **Dil çözümü** (istek başına bir kez): yönetici oturumu → `tbladmins.language`; müşteri oturumu →
  `tblclients.language` (boşsa sistem varsayılanı); oturum yoksa (cron/otomasyon) `$CONFIG['Language']`;
  hiçbiri yoksa İngilizce. Türkçe dışındaki her dil İngilizce'ye düşer.
- Panelden gelen ham mesajlar (API `hata` alanı) ÇEVRİLMEZ, aynen eklenir. Müşteri alanı metinleri
  eskisi gibi `lang/*.php` ile gelir. Davranış/güvenlik aynı (maskeleme, CSRF, SSO adres kontrolleri,
  hata sınıfları, yönetici sekmesi kaçışlama).
- Testler: 7 yeni test (dil çözüm sırası, önbellek, yer tutucu, yönetici sekmesi + bağlantı hatası +
  ürün ayarları İngilizce, müşteri/cron yolu, sözlük kapsaması: koddaki her `Ceviri::t('…')` sözlükte,
  kullanılmayan anahtar yok, yer tutucular tutarlı, `Ceviri::t` ile sarılmamış Türkçe metin yok).
  Test ortamında sistem dili `turkish` (mevcut testler aynı). 2 yeni mutant (26/26).

## 1.0.2
- **Port basit "Add Server" ekranından girilebilir.** WHMCS'in basit ekranında Port alanı yok (modül
  varsayılanı 8443 kullanılır); gPanel rol kapısı portları rastgeledir. Öncelik: Hostname alanında
  `panel.ornek.com:30409` (ya da `https://panel.ornek.com:30409/`) > Access Hash'te `port=30409` >
  WHMCS Port alanı (Gelişmiş mod) > 8443. Access Hash birden çok satır alır (`port=…`,
  `tls-dogrulama-kapali`). SSO adres kontrolü, düz panel bağlantısı ve "Panele git" aynı adresi kullanır.
- Bağlantı hatası port açıkça verilmemişse portun nereye yazılacağını söyler (host:port ile).
- **Eski kapı tespiti:** panel kapı portları değiştirildiyse ve WHMCS geçiş dönemindeki eski porttan
  bağlanıyorsa "Bağlantıyı Sına" yeni yönetici portunu söyler (`GET /api/v1/kapi/adres`, her anahtara açık).
- Testler: 7 yeni test (adres ayrıştırma, öncelik, basit ekran, ipucu, eski kapı), 3 yeni mutant (24/24).

## 1.0.1
Gerçek panele (0.3.0-20261009, test VPS) karşı ilk uçtan uca deneme sonrası.

- SSO: üründe **Panel adresi** girilmişse müşteri/yönetici her zaman o adresin kökenine
  (şema+host+port) yönlendirilir; panelin yolu ve tek kullanımlık kodu (`/adina#kod=…`) aynen
  korunur. Neden: ana bilgisayar adı ayarlanmamış panel adresi `https://_:PORT/` döndürüyordu;
  IP ile tanımlı WHMCS sunucusunda panel kendi ana bilgisayar adını döndürür → eşleşme reddi.
- SSO: kodsuz (`#kod` yok) adres reddedilir; eşleşmeyen ana bilgisayar hatası artık
  "Panel adresi" ayarını önerir.
- Testler: sahte panel gerçek SSO adres biçimine (`/adina#kod=`) getirildi; 3 yeni test,
  3 yeni mutant (21/21 öldürülüyor). Yeni `tests/canli.php`: gerçek panele karşı uçtan uca
  deneme (hosting kök+bayi anahtarı, bayi ürünü, güvenlik; oluşturduğunu siler).

## 1.0.0
İlk sürüm (WHMCS 8.x, PHP 8.1+).

- Sağlama: CreateAccount (idempotent), SuspendAccount, UnsuspendAccount, TerminateAccount,
  ChangePackage, ChangePassword, UsageUpdate, TestConnection.
- Tek tıkla panele giriş: ServiceSingleSignOn + AdminSingleSignOn (`POST /api/v1/domains/{id}/sso`),
  uç yoksa/izinsizse müşteri alanında düz panel bağlantısına geri düşme.
- Bayi (reseller) ürünü: bayi hesabı oluştur/askı/aç/sil, paket değiştir, kullanım.
- Müşteri alanı: hesap bilgisi, disk/trafik çubukları, parola değiştirme formu, kullanım yenileme.
- Yönetici hizmet sekmesi: panel hesap kimliği (doğrulamalı elle bağlama).
- Güvenlik: varsayılan TLS doğrulaması, anahtar/parola/SSO kodu log maskeleme, başka müşterinin
  hesabını devralmaya karşı "mevcut hesabı bağla" kapısı, istekten serviceid okunmaz.
- Testler: `php whmcs/tests/run.php` (sahte panel + WHMCS saplamaları), `bash whmcs/tests/mutasyon.sh`.
