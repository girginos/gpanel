# Değişiklik günlüğü — GirginOSPanel WHMCS modülü

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
