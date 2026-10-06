# Arşiv tabanlı site taşıma (cPanel cpmove / DirectAdmin yedeği)

Durum: **faz 1 (tasarım + güvenli okuyucu)**. Hesap oluşturma, geri yükleme, uç ve arayüz = faz 2.
Kod: `internal/tasima/arsiv/` (saf: DB, ağ, root yok; `tasima`'yı import etmez).

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

## 6. Faz 2 için açık kararlar

1. Kayıt: `arsiv-cpanel`/`arsiv-directadmin` ayrı kaynak kodları mı, mevcut `cpanel`/`directadmin` koduna "yöntem=arşiv" mi?
2. Bayiye açılma: varsayılan kapalı + `tasima_kaynak_ayar` anahtarı (öneri: canlı SSH deneysel olgunlaşana dek kapalı).
3. Yükleme tavanı ve geçici disk (bölüm 2) — yükleme ucu mu, sunucuda hazır dosya yolu mu (yalnız kök)?
4. Ad çakışması/yeniden adlandırma: arşivdeki kullanıcı/db adı hedefte doluysa otomatik sonek mi, ret mi?
5. DEFINER süzme + `--sandbox` yoksa davranış (bayi ret, kök uyarı) — mevcut `dumpFiltre` ile birleştirme.
6. `UstVeriBayt` (4 MiB) çok büyük passwd/mysql.sql'de arşivi reddeder; iki geçişli okuma gerekir mi?
7. DA yerleşimi ve cPanel `mail/` bölgeleri gerçek yedekle doğrulanmalı (fikstürler belge bilgisine dayanıyor).
8. Cron: yalnız rapor mu, kullanıcı onayıyla `cron` ekleme mi?
