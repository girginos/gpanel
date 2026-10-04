# Telegram destek ajanı — panelden AYRI hizmet (Faz 3)

Amaç: **panel çökse, durdurulsa ya da güncellenirken bile** destek botu sunucuyu teşhis edebilsin.
Ajan eskiden panel sürecinin içinde (`cmd/server`) çalışıyordu; panel düşünce destek de düşüyordu.

## Panelde Telegram Destek ÖZELLİĞİ YOKTUR (ürün kararı, 2026-10-04)

Telegram Destek **yalnız müşteri portalında** (app.girginos.io) vardır: eşleştirme, açma/kapama, bağlantı
listesi ve iptal orada yapılır. Sunucu panellerinde (kök / bayi / müşteri) **menü, sayfa, HTTP ucu
(`/destek/*`), API anahtarı izni ve panel şalteri YOKTUR — bilinçli olarak**. Ajan panelde görünmez bir
teşhis hizmetidir.

* Ajan, merkez açık anahtarı yapılandırılmışsa (ldflags `GomuluMerkezAnahtari` ya da
  `/etc/girginospanel/destek.json`) **her zaman merkeze bağlanır**. Kayıt yalnız teknik veri taşır
  (cihaz açık anahtarı, lisans parmak izi/bileti, panel sürümü, araç kataloğu özeti); kişisel veri yok.
  Sahip portalda eşleştirmeyi onaylamadıkça sunucuda **hiçbir şey yapılamaz**.
* `destek_ayar` (kök şalteri / rol tavanları) artık **okunmaz**; tablo göçlerde kalır, satırı olmasa da
  ajan bağlanır. Bir bağlantının etkin erişim düzeyi, sahibinin portalda seçtiği ve merkezin
  `EslesmeOnayla` v2 içinde **imzaladığı** düzeydir (`okuma|onarim|tam`); üst sınırı araç kataloğu koyar
  (bugün tüm araçlar R0 = salt okuma, bu yüzden `onarim`/`tam` seçilse de yazma aracı yoktur). Bağlantı
  başına süre (`sure_gun`), iptal ve oturum/bütçe sınırları aynen sürer.

### YEREL ACİL KAPATMA (yalnız root)

Panelde anahtar olmadığı için sunucu yöneticisinin tek yerel freni budur:

| Eylem | Komut | Etki |
|---|---|---|
| Kapat | `touch /etc/girginospanel/destek-kapali` | Ajan merkeze **bağlanmaz**; açık bağlantı **≤3 sn** içinde koparılır; gelen iş emirleri `destek_kapali` ile reddedilir. Nabızda (`destek_ajan_nabiz.durum`) `yerel_kapali` raporlanır. Mevcut bağlantı kayıtları silinmez. |
| Aç | `rm /etc/girginospanel/destek-kapali` | Ajan ≤3 sn içinde yeniden bağlanır. |
| Hizmeti durdur | `systemctl disable --now girginospanel-destek-ajan` | Ajan hiç çalışmaz (panel etkilenmez). Geri: `systemctl enable --now …`. |

Dosyanın **varlığı** yeterlidir (içerik/tür önemsiz; dizin ya da kırık sembolik bağ da kapatır; varlığı
denetlenemezse de kapalı sayılır). Dosyayı yalnız root oluşturabilir (`/etc/girginospanel` root'a aittir).
Güncellemeler dosyaya dokunmaz. Ajan ve panel çalışırken ayrıca yeniden başlatma gerekmez.

## Ne nerede çalışır

| Bileşen | Süreç | Not |
|---|---|---|
| Merkeze (bot.girginos.io) bağlanma, imza doğrulama, iş emri yürütme, süpürge (süre dolumu), DB yoklaması, nabız | `girginospanel-destek-ajan.service` (`/usr/local/bin/girginospanel-destek-ajan`, kaynak `cmd/destek-ajan`) | `girginospanel.service`'e **bağımlı değil** |
| Kimlik olayı iptal kancası (parola/2FA/hosting silme → bağlantı iptali + denetim) | `girginospanel.service` | DB'ye yazar; merkeze bildirimi ajan yapar |
| Panel-bağımlı araçların GET çağrıları köprüsü | `girginospanel.service` (`/run/girginospanel/destek-kopru.sock`) | yalnız uid 0 |

Panel-bağımsız araçlar (panel durmuşken de çalışır): `servis_durum`, `nginx_test`, `dogrula_ozet`,
`onarim_onizle`, `site_yokla`, `son_degisiklikler`, `log_oku` (php_fpm / laravel / cron).
Panel-bağımlı araçlar (köprü gerekir): `hesap_ozet`, `ssl_durum`, `cron_liste`, `php_ayar_oku`,
`yedek_liste`, `wp_durum`, `disk_kota`, `dns_kontrol`, `log_oku` (nginx_*, php_debug), `sunucu_log_oku`.
Köprü yoksa bunlar `panel_yanit_vermiyor` döner (özette; modele servis_durum/dogrula_ozet önerilir).

## Panel ↔ ajan bağı (yalnız veritabanı + özel köprü)

* **İptal**: kimlik olayları (parola değişimi, 2FA kapatma, kullanıcı/hosting silme-askı) bağlantıyı DB'de `iptal` yapar
  (panelde bunun için HTTP ucu yoktur; portal iptali merkez üzerinden ajana gelir),
  `destek_baglantilari.hub_bildirildi_at` NULL kalır. Ajan ≤3 sn'de görür → `baglanti_iptal` gönderir,
  izi temizler, damgalar. Merkeze bağlı değilse damgalanmaz; yeniden bağlanınca bildirilir.
* **Nabız**: ajan ≤10 sn'de `destek_ajan_nabiz` (id=1) satırını yazar (`durum`: `bagli`, `hata`,
  `yerel_kapali`, `durdu`…). Panelde bunu gösteren arayüz YOKTUR; ops/test okur (`ajan.NabizOku`).
* **Köprü sözleşmesi** (`internal/destek/kopru`): Unix soket `/run/girginospanel/destek-kopru.sock`
  (dizin 0700 root, soket 0600 root; TCP yok). Her bağlantıda `SO_PEERCRED` uid==0 denetlenir (başkası
  hiçbir bayt okunmadan kapatılır). İstek tek JSON (≤8 KB): `{kimlik{rol,kullanici_id,kullanici,domain_id,
  alan_adi,tg_kisa}, yol, sorgu}`. Sunucu yolu `middleware.DestekYolIzinli` ile (varsayılan red, yalnız GET,
  yalnız `DestekRotalari` kalıpları, `{id}` yalnız rakam) yeniden doğrular, sonra süreç içi çağrıyı
  `DestekBagla` ile yapar (ikinci kapı: `destekIstek` RoutePattern). Yanıt ≤8 MB, çağrı sınırı 25 sn.
  Kimlik işareti **yalnız panel sürecinde** konur; HTTP ile taklit edilemez.

## Cihaz kimliği sürekliliği (güncellemede yeni cihaz OLMAZ)

Cihaz Ed25519 anahtarı eskiden olduğu gibi `/etc/girginospanel/destek-cihaz.key`'de: içerik
`gizli.SaklaBagli(tohum, "destek-cihaz-v1")` ile `/etc/girginospanel/db.key`'e bağlı mühürlü, 0600 root.
Ajan **aynı kodu** (`cihazAnahtariYukle`) aynı dosyada çalıştırır → kimlik değişmez; yükleme dosyayı
yeniden yazmaz/kenara almaz. Tuzaklar kapatıldı:

* `gizli` paketi okunamayan `db.key`'i yenisiyle ÜRETİP üstüne yazar. Ajan açılışta
  `gizli.AnahtarHazir()` ile db.key'in var ve ≥32 bayt olduğunu denetler; değilse **başlamaz**
  (ortam sorunu yüzünden kimlik/sırlar sessizce değişmesin). Ajan db.key'i hiçbir koşulda üretmez.
* Test: `TestCihazKimligiAyrilmadaDegismez` (eski panel biçimindeki dosya → aynı kimlik, dosya bayt-bayt aynı).

## Kurulum / güncelleme sırası

Yeni kurulum (`girginospanel-install.sh`): ikili `/usr/local/bin`'e (adım 6), birim `/etc/systemd/system`'e
(adım 10, **etkinleştirilmez**), panel başlatılıp ayağa kalktıktan SONRA adım 12'de `enable` + `restart`.
Güncelleme (`girginospanel-update`): ikili adım 6'da atomik yazılır; **panel yeni ikiliyle SAĞLIKLI
açıldıktan sonra** (adım 11a) birim kurulur, `daemon-reload`, `enable`, `restart`. Sağlıksız yeni
sürümde (geri alma dalı) ajan başlatılmaz. Ajanın başlamaması güncellemeyi başarısız saymaz.
Sır yenileme (`girginospanel-server sir yenile`) ajanı da `try-restart` eder (kendi LoadCredential
kopyası bayatlar).

Eski (gömülü ajanlı) panel ile yeni hizmet aynı cihaz anahtarıyla merkeze AYNI ANDA bağlanmasın diye:
yeni panel ajanı hiç başlatmaz; hizmet panel swap'ından sonra başlar; hizmet ayrıca
`/run/girginospanel/destek-ajan.lock` üzerinde `flock` ile **tek örnek** garantisi verir
(ikinci örnek `başka bir destek ajanı örneği çalışıyor` ile çıkar).

## Ayrıcalıklar (neden kök?)

Birim `User=root` ama `CapabilityBoundingSet` asgari: `CAP_SETUID CAP_SETGID` (kiracı kimliğine
`runuser`), `CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE` (`/home/<sk>`, `/var/log`, journal okuma; `nginx -t`
geçici dizin), `CAP_CHOWN CAP_FOWNER` (`nginx -t`), `CAP_KILL` (zaman aşımında kiracı komut çocuğuna
sinyal), `CAP_AUDIT_WRITE CAP_SYS_RESOURCE` (runuser PAM oturumu). `NoNewPrivileges`,
`ProtectSystem=strict`, `ProtectHome=read-only`, `PrivateTmp`, `PrivateDevices`, çekirdek/cgroup/saat
korumaları, `SystemCallFilter=@system-service @setuid`, `MemoryMax=512M`, `TasksMax=256`.
`ReadWritePaths`: `/run/girginospanel` (kilit), `/etc/girginospanel` (YALNIZ cihaz anahtarının ilk
üretimi; `destek-kapali` dosyası yalnız OKUNUR), `/var/log/nginx` + `/var/lib/nginx` + `/var/cache/nginx` (`nginx -t`, doğrula). Sırlar `LoadCredential=panel-env` ile
(ortam değişkeni YOK); ajan DB DSN'i ve araç çıktısı maskesi için kullanır — JWT sırrı vb. yalnız
bellekte maske listesinde durur.

**DOĞRULANMADI (yayın öncesi dev sunucuda zorunlu):** kısıtların altında her araç çalışıyor mu —
`systemd-analyze security girginospanel-destek-ajan`, ardından bot üzerinden her panel-bağımsız araç
(özellikle `nginx_test`, `dogrula_ozet`, `onarim_onizle`, `log_oku laravel`, `disk_kota` kiracı `du`).
Bir araç `EPERM`/`EROFS` verirse eksik izin birime **gerekçesiyle** eklenir (gevşetme kararı insana ait).

## "Panel durdu → bot yine teşhis ediyor" doğrulaması

1. `systemctl stop girginospanel` (panel kapalı; `systemctl is-active girginospanel-destek-ajan` = active).
2. Telegram'dan sor ("sitem açılmıyor"): bot `servis_durum` / `site_yokla` / `log_oku php_fpm`
   sonuçlarıyla yanıt verir; `hesap_ozet` gibi panel-bağımlı araçlar `panel_yanit_vermiyor` döner
   ve bot panel hizmetinin yeniden başlatılması gerekebileceğini söyler.
3. `systemctl start girginospanel` → **ajanı yeniden başlatmadan** birkaç saniye içinde panel-bağımlı
   araçlar yeniden çalışır (köprü istemcisi her çağrıda soketi yeniden dener).
4. Ajan durumu: `mysql -e "SELECT durum,merkez_bagli,nabiz_at FROM destek_ajan_nabiz"` (panel arayüzü yok).
Otomatik karşılığı: `internal/destek/e2e` `TestPanelYokAjanTeshisiSurer`.

## Uyumluluk notu (merkez/hub)

`panel_yanit_vermiyor` protokolde yeni bir hata kodu DEĞİLDİR: eski merkez sürümleri bilinmeyen
`hata_kodu`'nu reddederdi. Telde `mesgul` gider; ayırt edici ad `ozet` metnindedir
(`panel_yanit_vermiyor: …`). Merkez sistem istemine kural 10 eklendi (prompt-cache bir kez sıfırlanır;
merkez dağıtımıyla gelir, ajanlar için zorunlu değil).

## Lisans bileti izni (güvenlik düzeltmesi)

`/var/lib/girginospanel-mail/bilet` eskiden 0644'tü: yerel herhangi bir kullanıcı biletteki gerçek
parmak izini okuyup gerçek cihaz çevrimdışıyken merkezde o parmak izini kaydedebilirdi. Okuyanlar
taranarak belirlendi: mail eklentisi (root) + webmail kapısı (`gosp_lisans.php`, PHP-FPM `apache`
kullanıcısı) + bu ajan (root); `/var/lib/girginospanel-calistirici/bilet` yalnız çalıştırıcı eklentisi (root).
Artık: mail kopyası **0640 root:apache** (apache grubu yoksa 0600), çalıştırıcı kopyası **0600 root**.
Panel açılışı mevcut dosyaları düzeltir (`lisans.BiletIzinleriniDuzelt`). Kod `internal/lisans`
altındadır (depoda izlenmez, `gpanel-temiz` ile paylaşılır).

## Geri alma

* Ajanı kapat: `systemctl disable --now girginospanel-destek-ajan` (panel etkilenmez; destek durur) ya da
  hizmeti çalışır bırakıp `touch /etc/girginospanel/destek-kapali` (bkz. "Yerel acil kapatma").
* **Eski (gömülü ajanlı) panel sürümüne dönmeden ÖNCE** mutlaka yukarıdaki komutu çalıştır — aksi halde
  eski panel ile hizmet aynı cihaz anahtarıyla merkeze çift bağlanır (kilit yalnız yeni ikilileri korur).
* DB: göç 0130 yalnız ekleme yapar (`hub_bildirildi_at`, `destek_ayar.surum`, `destek_ajan_nabiz`);
  eski ikili bunları yok sayar, geri almada silinmesi gerekmez.
* Cihaz kimliği dosyası geri alma ile değişmez.
