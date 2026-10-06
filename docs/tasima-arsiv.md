# Arşiv tabanlı site taşıma (cPanel cpmove / DirectAdmin yedeği)

Durum: **faz 2 (taşıma akışına bağlandı; olgunluk `deneysel`, gerçek cPanel/DA yedeğiyle DOĞRULANMADI)**.
Kod: `internal/tasima/arsiv/` (saf okuyucu; `tasima`'yı import etmez) + `internal/tasima/arsiv_*.go` (kaynak sürücüleri,
uçlar, adımlar) + `internal/tasima/arsiv_cron.go` + `internal/cron/ekle.go` + arayüz `frontend/src/pages/SiteTasimaPage.tsx`.
Göç: `migrations/0216_tasima_arsiv.sql`. Kullanıcı kararları (2026-10-06) ve uygulama özeti: bölüm 7.

## 1. Mimari

```
yükleme ──► [arsiv.Ac]  tek geçiş, envanter, hiçbir şey diske yazılmaz   (arsiv.go, akis.go)   VAR (A2)
               │
               ├─► [arsiv.Oku]  tür tespiti + ayrıştırıcı + SQL tarama → Manifest   (manifest.go, cpanel.go,
               │                                                       directadmin.go, sql.go)   BU TUR
               └─► [Arsiv.Cikar] kiracı kimliğinde, süzülmüş akışla çıkarma        VAR (A2), faz 2 çağırır
```

Mevcut `internal/tasima` mimarisine takılış (yeni paralel yol YOK): arşiv, `kaynak_kayit.go` defterine
**yeni bir kaynak sürücüsü** (`arsiv-cpanel`, `arsiv-directadmin`; olgunluk `deneysel`, varsayılan kapalı)
olarak faz 2'de kaydolur. `kesif` = `arsiv.Oku` (SSH yerine yerel dosya), `Hesap` listesi manifestten kurulur;
sahip eşlemesi, `Kapsam{RID}` (bayi), kota (`domains.TasimaKotaKontrol`), iş günlüğü, `kuyruk.go` yuvası ve
`aktar.go` adımları (dosya/DB/DNS/posta) aynen kullanılır; yalnız "uzak çalıştır/rsync" yerine
"arşivden çıkar / dökümü akıt" gelir. `arsiv` → `tasima` import yönü hiç tersine dönmez.

## 2. Yükleme yolu (faz 2 kararları)

- Tek yol: panel yükleme ucu, **parçalı + sürdürülebilir** değil, düz `multipart` yerine disk-akışı; bellekte tutulmaz.
- Boyut tavanı: **kiracı disk kotası kalanı** ve mutlak tavan (varsayılan 50 GiB); aşım → 413, kısmi dosya silinir.
- Geçici dizin: `/var/lib/girginospanel/tasima/arsiv/<rid>/<is_id>.tar.*`, `0700 root` (bayi kimliği dizin adında;
  kiracılar birbirinin arşivini ne görür ne okur). Çıkarma ev dizinine, kiracı kimliğinde yapılır; yedek iş
  bitince/iptalde/panel açılışında artakalanlar silinir (`kuyruk.go` artakalan temizliği ile aynı yer).
- Kota: yükleme baytı + çıkarma sonrası toplam (`Hedef.Butce`, `Sinirlar.DiskPayi`) kiracı kotasına sayılır;
  `Oku` manifestindeki `ToplamBayt/DosyaSayisi` yükleme sonrası ön kota denetimi için kullanılır.
- Sıkıştırma biçimleri: gzip/bzip2 süreç içi (Go), xz/zstd `nobody` alt sürecinde (içerikten tanınır, uzantıdan değil).

## 3. Yedek yerleşimleri

**cPanel** `cpmove-<k>.tar.gz` (tek kök dizin): `cp/<k>` (KEY=VALUE hesap kaydı), `userdata/main` (dar YAML:
main_domain, addon_domains, sub_domains, parked_domains), `userdata/<alan>` (documentroot), `mysql/<db>.sql`,
`mysql.sql` (GRANT dökümü, yalnız adlar okunur), `dnszones/<alan>.db`, `homedir/` (site + `mail/<alan>/<kutu>/`
maildir + `etc/<alan>/passwd`), `cron/<k>`.
**DirectAdmin** (kök yok): `backup/user.conf`, `backup/<db>.sql|.conf`, `backup/<alan>/email/passwd`,
`backup/crontab.conf`, `domains/<alan>/public_html`, `imap/<alan>/<kutu>/`.
Her iki yerleşim de gerçek yedekle **doğrulanmadı** (deneysel); bilinmeyen yapı uyarıyla atlanır.

## 4. Neyin taşınacağı / taşınmayacağı

| Taşınır | Not |
|---|---|
| Site dosyaları (`homedir/`, `domains/`) | kiracı kimliğinde, süzülmüş akışla |
| Veritabanları + DB kullanıcı **adları** | döküm tarayıcıdan geçer; kullanıcı/parola panelde yeniden üretilir |
| Alan adları (ana/ek/alt/park) + belge kökleri | söz dizimi `AlanAdiGecerli`; sahiplik faz 2'de `domains.AdAlaniDenetle` |
| E-posta hesapları (kutu adı + maildir) | parola özeti taşınmaz → yeni parola |
| DNS bölgeleri (cPanel) | yalnız hesabın kendi alanları; kayıtlar faz 2'de ayrıştırılır |
| Cron satırları | yalnız **rapor** (kabuk komutu; otomatik kurulmaz) |

Taşınmayanlar `Manifest.Tasinmayacak` ve `Manifest.Uyarilar` ile kullanıcıya raporlanır: parolalar/özetler,
GRANT/CREATE USER, e-posta yönlendirici/otomatik yanıt/liste, SSL, Apache/PHP/ModSecurity ayarları, kota/plan/bayi
bilgisi, günlükler, aygıt/FIFO düğümleri, kök dışı symlink, setuid/setgid bitleri; DA için DNS ve alt alan listesi.

## 5. Güvenlik kuralları (kaynak = güvenilmez girdi)

Arşiv düzeyi (`arsiv.go`, `akis.go`, `cikar.go`; A2): ad `..`/mutlak/ters bölü/4096 B/64 bileşen → ret;
çift giriş → ret; symlink yalnız seçimde kalan, bağ zincirinden geçmeyen göreli hedef, hardlink yalnız önceden
yazılmış düzenli dosya, aygıt/FIFO atlanır, setuid/gid/sticky silinir, sahiplik/xattr yok sayılır; üye/boyut/ad
baytı/bağ sayısı/üst veri sınırı, sıkıştırma ve seyrek-üye oran sınırı, kiracı bütçesi, boş disk+inode denetimi.
Bu tur eklenenler: **SQL tarayıcı** (`sql.go`, allowlist: yalnız SET/CREATE/DROP/INSERT/REPLACE/LOCK/UNLOCK/ALTER/
START/BEGIN/COMMIT/ROLLBACK/DELIMITER; GRANT/REVOKE/PREPARE/LOAD_FILE/SONAME/OUTFILE/SLEEP yasak; CREATE/DROP/ALTER
penceresinde USER/ROLE/DATABASE/SCHEMA; satır başı istemci komutları ve kod bölgesindeki `\`; SET'te
GLOBAL/PERSIST/PASSWORD/işlev/onaltılık/NO_BACKSLASH_ESCAPES-ANSI) — dizgi/yorum/sürümlü yorum farkındalıklı tek
geçiş, sabit bellek; **manifest kuralları**: sahip/plan/IP iddiası okunmaz, parola özeti hiç saklanmaz, posta/DNS yalnız
hesabın kendi alanları için, kutu adı `PostaYerelGecerli` (`..`/ayraç yok), belge kökü ev dizinine bağlı ve `..`
yok, YAML'da çapa/takma ad/akış stili ret, sistem veritabanı adı atlanır.
Kural → test eşlemesi: bu turun raporunda.

**Kalan sınır (kabul edilen risk):** DEFINER sayılır, faz 2 içe aktarmada süzer; çapraz-veritabanı nitelikli adlar
(`` `baska`.`t` ``) sözcüksel engellenmez (mysqldump görünümlerde kaynak db adını yazar) → koruma: içe aktaran
MySQL kullanıcısı yalnız hedef veritabanında yetkili + `mysql --sandbox` (yoksa bayi taşıması kapalı:
`errBayiSandboxYok`). **Root'ta sandboxsız dökümü çalıştırma YASAK.**

## 6. Faz 2 kararları (kullanıcı kararı → uygulama)

| # | Karar | Uygulama |
|---|---|---|
| 1 | Ayrı kodlar `arsiv-cpanel` / `arsiv-directadmin` | `arsiv_kaynak.go` kayıt defterine kaydolur (`Arsiv:true`, deneysel, varsayılan KAPALI); canlı SSH `cpanel`/`directadmin` koduna dokunulmaz; her biri `PUT /tasima/kaynaklar/{kod}` ile ayrı kapatılır (kapatınca sıradaki işler iptal) |
| 2a | Kök: sunucudaki hazır dosya yolu | `arsiv_yol.go`: mutlak+temiz, izinli dizin kökü altında (`cp_ayarlar.tasima_arsiv_dizinleri`; varsayılan /root /srv /mnt /backup /backups /var/backups), panel/kiracı/sistem dizinlerinde değil, yolda sembolik bağ yok, düzenli dosya, **root sahipli ve grup/diğer yazamaz (dosya + izinli kökten itibaren tüm üst dizinler)** → doğrulama ile okuma arasında kökten başkası dosyayı değiştiremez |
| 2b | Bayi: parçalı + devam edebilir yükleme | `arsiv_yukle.go`: `POST/PUT/GET/DELETE /tasima/arsiv/yukle…` + `/bitir`; sunucu yolu `<arsivYukleKok>/<rid>/<id>.arsiv` (0700 root, ad yola girmez), kapsam başına 50 GiB toplam kota + en çok 3 kayıt, bayi disk havuzu ve boş alan önkontrolü, biçim imzası (gzip/bzip2/xz/zstd/tar) `bitir`de, 24 sa (yarım) / 72 sa (hazır) sonra silinir, işe bağlıyken silinemez |
| 3 | Tavanlar | yol **500 GiB**, yükleme **50 GiB** (`arsiv.YolTavan/YuklemeTavan`); açılmış toplam = tavan × 4, üye sayısı yol 1,5 M / yükleme 600 k, döküm 64/32 GiB; **bomba oranı (500×, 512 MiB eşik), üst veri 4 MiB, bağ sınırları aynen kalır**. Önkontrol: hedefte boş alan < arşiv × 2,5 ise taşıma BAŞLAMADAN reddedilir (keşif `onkontrol`, başlat 507, iş başı yeniden); bayi disk havuzu (açılmış boyut) ve kiracı bütçesi = plan disk kotası ∧ havuz kalanı (`arsivButce`; aşım → kalem `atlandi`, hesap geri alınır) |
| 4 | Ad çakışması: otomatik sonek | **sk**: `provisioner.BenzersizKullanici` `c_ad`, `c_ad2`, `c_ad3`… — bu işte **üst sınır 9999 + `kiraci.GecerliSK`** eklendi, tükenince hesap açılmaz; **DB adı**: `benzersizHedefDB` `<sk>_<ad>`, `_2`… (50 deneme, sonra kendi ad-alanında kompakt ad, 64 B sınırı, `GecerliDBKimlik`); **DB kullanıcısı**: `<sk>_db`, `_db2`… (`tasimaKullanicisiSec`). Hepsi başka kiracının ad-alanına düşmeyi (`DBAdiBaskaKiracininMi`) reddeder |
| 5a | Bayiye açılma varsayılan KAPALI | `tasima_kaynak_ayar.bayi_etkin` (0216, varsayılan 0): bayi kapısı = genel `tasima_bayi_acik` ∧ kaynak `etkin` ∧ `bayi_etkin`; kapatınca bayinin sıradaki işleri iptal |
| 5b | **Cron TAM taşınır** (önceki "yalnız rapor" varsayılanı GEÇERSİZ) | `arsiv_cron.go` + `cron.Ekle`: bkz. bölüm 8 |
| 5c | DEFINER aktarımda süzülür | `dbIceAktar` (canlı SSH ile ortak) → `dumpFiltre`; arşiv dökümü ayrıca iki geçişli taranır (bölüm 7) |
| 5d | `--sandbox` yoksa bayide ret, kökte uyarı | mevcut `veritabanlariniAktar` (`errBayiSandboxYok`) aynen; arşiv yolu da aynı fonksiyondan geçer |
| 5e | `domains.AdAlaniDenetle` hesap oluşturulmadan önce, bayi `Kapsam{RID}` | `HesapAktar` aynen (arşiv kaynağı da onun içinden geçer; `TestHesapAktar_ArsivKaynagi_AdAlaniSaglamadanOnce`) |
| 6 | Mevcut altyapı yeniden kullanılır | iş kuyruğu (`tasima_isleri`/`tasima_kalemleri`/`dagit`/`calistir`), ilerleme, geri alma (`HesapAktar` defer), denetim kodları (`tasima_baslat`, `tasima_arsiv_kesif`, `tasima_arsiv_yukle_*`, `tasima_kaynak_ayar`) |

## 7. Akış ve güvenlik özeti (faz 2)

```
POST /tasima/arsiv/yukle … (bayi/kök)  ┐
kök: sunucudaki yol                    ┴─► POST /tasima/arsiv/kesif → 202 {oturum_id}   (asenkron; 2 saat TTL oturum)
                                            GET  /tasima/arsiv/kesif/{id}  → durum kesif|hazir|hata + hesaplar/özet/önkontrol
                                            POST /tasima/arsiv/baslat {oturum_id,…} → kuyruğa yazar (kimlik YOK)
kuyruk dağıtıcı → isYurut → arsivIsHazirla (kapsam+parmak izi+disk+envanter) → calistir → HesapAktar (adımlar arşivden)
```

- **Rol/kapsam:** tüm uçlar `AdminVeyaReseller`; bayi ek kapısı handler içinde (`arsivKapi`); kaynak anahtarı `AdminOnly`.
  Yükleme/oturum/iş kayıtları `sahip_rid` ile süzülür, kapsam dışı ve olmayan kayıt AYNI 404; bayi `yol` veremez (403).
- **Başlatma sunucu kopyasından:** istemcinin `web_root`/`dbler`/`ssl`/`sahip`/`yol` alanları yok sayılır; seçim oturumdaki
  sunucu kopyasıyla kesişir; bayi için sahip/üzerine yazma/ssl zorlanır, plan zorunlu+kapsamlı.
- **Parmak izi:** oturum + iş kaydı arşiv dosyasının (boyut, mtime) damgasını taşır; keşifte, başlatmada, iş başında ve arşiv
  okunduktan sonra yeniden doğrulanır (değiştiyse ret).
- **Adımlar** (`arsiv_aktar.go`): dosya = `Arsiv.Cikar` (kiracı kimliğinde, süzülmüş akış, bütçeli; docroot-dışı bağımlılık
  `../<dizin>/` için posta/etc/ssl/logs dizinleri hariç); DB = **tara (SQL allowlist) → içe aktar (dumpFiltre)** iki geçiş
  (manifest taramasından sonra içerik değişse yakalanır); DNS = cPanel bölgesi (hesabın kendi alanı) → mevcut `dnsAktar`
  süzgeci, DA'da varsayılan şablon; SSL taşınmaz (Let's Encrypt); posta = kutular eklentide yeni parolayla açılır, maildir
  `vmail` kimliğinde `Arsiv.CikarDizin` ile (kutu allowlist'i, dovecot/sieve hariç, **bağ ve sabit bağ yok**), alan başına tek geçiş;
  cron = bölüm 8.
- **Yükleme temizliği:** tam başarılı işte silinir; hatalı/iptal/kısmi işte 72 sa kalır (yeniden deneme).

## 8. Cron taşıma (arsiv_cron.go, internal/cron/ekle.go)

Manifest `Cron` artık HAM satırları (ortam satırları dahil, sırayla) taşır. `cronAta` satırları alan adına dağıtır
(`/home/<eski>/<belge kökü>` en uzun önek; yol yoksa ana alan; birden çok alana başvuran satır `taşınamadı`), her kalemde `cronKur`
= `cronCevir` + `cron.Ekle`: zaman 5 alan ya da `@hourly/@daily/@midnight/@weekly/@monthly/@yearly/@annually` (→ 5 alan;
`@reboot` ve bilinmeyen `@x` red), `/home/<eski>` → `/home/<sk>`, başka hesabın `/home/…` yolu red, MAILTO="" → bildirim `yok`
(dolu MAILTO not), güvenli ortam değişkenleri `export AD='…';` öneki (LD_PRELOAD, BASH_ENV, IFS, HOME… red), tehlikeli içerik
(panel/kiracı yolları, `crontab`, sudo/su, systemctl, nsenter, docker, mkfifo, /dev/tcp, curl|sh, bash -i …) red; komut ≤ 1024 B,
satır ≤ 4096 B, hesap başına ≤ 100 görev; **yinelenen satır eklenmez** (idempotent); hesap askıdaysa yazılmaz ve nedeniyle
raporlanır. Çevrilemeyen HER satır kalem uyarısında `Cron taşınamadı: "<satır>" — <neden>` olarak görünür (sessiz atma yok).
Görevler mevcut `internal/cron` altyapısında çalışır: depo `/var/lib/girginospanel/cron/<sk>`, `/etc/cron.d` satırı yalnız
`panel cron-calistir <sk> <kimlik>` içerir, görev kiracı kimliğinde ve `girginos-<sk>.slice` içinde, panel sırları ortamsız.
**Dürüst sınır:** tehlikeli-içerik listesi yalnız ikinci katmandır (değişken birleştirme/kodlama ile atlatılabilir); asıl yalıtım
kiracı uid + kaynak dilimi + temiz ortamdır. `/home/<eski>` altında docroot DIŞINDA kalan betikler taşınmaz (dosya adımı yalnız
belge kökü + docroot-dışı uygulama dizinlerini çıkarır): satır yine eklenir, betik yoksa cron hata verir.

## 9. Hâlâ açık / kullanıcı kararı bekleyenler

1. `UstVeriBayt` (4 MiB) tavanı çok büyük passwd/mysql.sql'de arşivi reddeder (yalnız iki geçişli okuma çözer).
2. DirectAdmin ve cPanel `mail/` bölgeleri, DA `imap/<kutu>[/Maildir]` yerleşimi ve DNS (DA'da yok) gerçek yedekle doğrulanmadı.
3. Tek arşivde N alan = N tam geçiş (dosya) + 2×DB + alan başına posta geçişi: 500 GiB yedekte saatler sürer (kalem zaman aşımı arşivde
   12 sa, `arsivHesapTimeout`; canlı SSH 3 sa). Çıkarma/DB geçişleri aynı arşivi yeniden okuduğundan çok alanlı büyük yedek yavaştır.
4. Bayi tarafında yükleme sırasında nginx/CDN gövde sınırı (parça 8 MiB) ve oturum zaman aşımı gerçek ortamda denenmedi.
5. Keşif asenkron bellek: yol 1,5 M üye ≈ birkaç yüz MiB (arsiv paketi sınırı); eşzamanlı en çok 2 inceleme.
