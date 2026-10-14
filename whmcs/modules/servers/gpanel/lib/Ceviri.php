<?php
/**
 * GirginOSPanel WHMCS modulu — yonetici/sistem metinleri icin ceviri (tr/en).
 *
 * Kaynak metin Turkce'dir ve kodda aynen durur: Ceviri::t('Turkce metin', [$a, $b]).
 * Dil Turkce degilse EN sozlugundeki karsiligi doner; sozlukte yoksa Turkce metin doner
 * (test, koddaki her Ceviri::t('...') metninin sozlukte oldugunu denetler).
 * Yer tutucular: {0}, {1}, ... (strtr: deger icindeki "{1}" yeniden islenmez).
 *
 * Musteri alani sablonu (templates/clientarea.tpl) bu sinifi KULLANMAZ; o lang/*.php
 * dosyalarini kullanir (Yardimci::dil). Panelden gelen ham mesajlar CEVRILMEZ.
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

use WHMCS\Database\Capsule;

final class Ceviri
{
    /** @var string|null istek basina onbellek: 'tr'|'en' */
    private static $dil = null;

    /**
     * Gecerli dil (istek basina bir kez cozulur):
     *   1. Yonetici oturumu ($_SESSION['adminid']) -> tbladmins.language
     *   2. Musteri oturumu ($_SESSION['uid'])      -> tblclients.language
     *   3. Bos/yoksa (cron, otomasyon)             -> $CONFIG['Language'] (sistem varsayilani)
     *   4. Hicbiri yoksa                            -> en
     * Turkce disindaki her dil Ingilizce'ye duser.
     *
     * @return string 'tr'|'en'
     */
    public static function dil(): string
    {
        if (self::$dil === null) {
            self::$dil = self::coz();
        }
        return self::$dil;
    }

    /** Test/uzun omurlu surec dikisi: onbellegi bosaltir, sonraki dil() yeniden cozer. */
    public static function sifirla(): void
    {
        self::$dil = null;
    }

    private static function coz(): string
    {
        $ad = '';
        $oturum = isset($_SESSION) && is_array($_SESSION) ? $_SESSION : [];
        $yonetici = (int) ($oturum['adminid'] ?? 0);
        $musteri = (int) ($oturum['uid'] ?? 0);
        if ($yonetici > 0) {
            $ad = self::tablodanDil('tbladmins', $yonetici);
        } elseif ($musteri > 0) {
            $ad = self::tablodanDil('tblclients', $musteri);
        }
        if ($ad === '' && isset($GLOBALS['CONFIG']['Language'])) {
            $ad = (string) $GLOBALS['CONFIG']['Language'];
        }
        return self::kodla($ad);
    }

    /** tbladmins/tblclients satirinin language alani; hata ya da kayit yoksa ''. */
    private static function tablodanDil(string $tablo, int $id): string
    {
        try {
            if (!class_exists(Capsule::class)) {
                return '';
            }
            $s = Capsule::table($tablo)->where('id', $id)->first();
            if (is_object($s)) {
                return trim((string) ($s->language ?? ''));
            }
            if (is_array($s)) {
                return trim((string) ($s['language'] ?? ''));
            }
        } catch (\Throwable $e) {
            // DB erisilemedi: sistem varsayilanina dus
        }
        return '';
    }

    /** WHMCS dil adi ("turkish", "english", ...) -> 'tr'|'en'. Bos ya da bilinmeyen -> 'en'. */
    public static function kodla(string $ad): string
    {
        $ad = strtolower(trim($ad));
        if ($ad === 'turkish' || $ad === 'tr' || strncmp($ad, 'tr_', 3) === 0 || strncmp($ad, 'tr-', 3) === 0) {
            return 'tr';
        }
        return 'en';
    }

    /**
     * @param array<int,scalar|null> $args {0},{1},... sirasiyla
     */
    public static function t(string $tr, array $args = []): string
    {
        $m = $tr;
        if (self::dil() === 'en' && isset(self::EN[$tr])) {
            $m = self::EN[$tr];
        }
        if ($args !== []) {
            $r = [];
            $i = 0;
            foreach ($args as $v) {
                $r['{' . $i . '}'] = (string) $v;
                $i++;
            }
            $m = strtr($m, $r);
        }
        return $m;
    }

    /** @return array<string,string> Turkce kaynak => Ingilizce */
    public static function sozluk(): array
    {
        return self::EN;
    }

    private const EN = [
        // ── gpanel.php: MetaData / ConfigOptions ──────────────────────────
        'Panele Giriş Yap' => 'Log in to Panel',
        'Panele Giriş (Yönetici)' => 'Log in to Panel (Admin)',
        'Ürün tipi' => 'Product type',
        'Hosting hesabı' => 'Hosting account',
        'Bayi (reseller) hesabı' => 'Reseller account',
        'WHMCS ürün türü "Reseller Account" ise bu alan ne olursa olsun bayi hesabı açılır. Bayi ürünü için API anahtarı KÖK kullanıcıya ait olmalı ve "bayi" iznine sahip olmalı.'
            => 'If the WHMCS product type is "Reseller Account", a reseller account is created regardless of this field. For reseller products the API key must belong to the ROOT user and have the "bayi" (reseller) permission.',
        'Panel planı (hosting)' => 'Panel plan (hosting)',
        'Plan ID ya da tam adı. Boşsa panelin varsayılan planı.' => 'Plan ID or exact name. Empty uses the panel\'s default plan.',
        'PHP sürümü' => 'PHP version',
        'Seçilmezse plandan/panelden miras.' => 'If not selected, inherits from the plan/panel.',
        'Plandan / panelden' => 'From plan / panel',
        'Bayi paketi (bayi ürünü)' => 'Reseller package (reseller product)',
        'Bayi paketi ID ya da tam adı (yalnız Ürün tipi = Bayi).' => 'Reseller package ID or exact name (only when Product type = Reseller).',
        'Varsayılan veritabanı aç' => 'Create default database',
        'Hesapla birlikte bir MariaDB veritabanı oluştur.' => 'Create a MariaDB database together with the account.',
        'TLS doğrulamasını KAPAT' => 'DISABLE TLS verification',
        'DİKKAT: kendi imzalı sertifikada araya girme (MITM) saldırısına açar; API anahtarı ele geçirilebilir. Yalnız güvenilir ağda ve geçici olarak kullanın. Sunucu ayarında Access Hash = tls-dogrulama-kapali de aynı işi yapar.'
            => 'WARNING: with a self-signed certificate this allows man-in-the-middle (MITM) attacks; the API key can be stolen. Use only on a trusted network and only temporarily. Access Hash = tls-dogrulama-kapali in the server settings does the same.',
        'Panel adresi (isteğe bağlı)' => 'Panel URL (optional)',
        'Boş bırakılabilir: adres panelden alınır — hosting ürününde müşteri kapısı, bayi ürününde bayi kapısı (gPanel 0.3.0-20261013+). Başka bir ad/port için https://panel.ornek.com:PORT/ girin.'
            => 'Can be left empty: the address is taken from the panel — the client gate for a hosting product, the reseller gate for a reseller product (gPanel 0.3.0-20261013+). Enter https://panel.example.com:PORT/ to use a different name/port.',
        'Mevcut hesabı bağla' => 'Link existing account',
        'Panelde zaten var olan alan adı/bayi bulunursa hizmete BAĞLA. Kapalıyken (önerilen) başka müşterinin hesabını devralma engellenir.'
            => 'If the domain/reseller already exists on the panel, LINK it to the service. When off (recommended), taking over another client\'s account is prevented.',

        // ── ApiIstemci ─────────────────────────────────────────────────────
        'Sunucu ayarında geçerli bir ana bilgisayar adı/IP yok (port yazacaksanız biçim: panel.ornek.com:30409).'
            => 'The server settings have no valid hostname/IP (to include a port use the format panel.example.com:30409).',
        'Sunucu ayarındaki Port (yönetici kapısı portu) geçersiz.' => 'The Port in the server settings (admin gate port) is invalid.',
        "Sunucu ayarında 'Güvenli (SSL)' kutusu işaretli olmalı; API anahtarı düz HTTP ile gönderilmez."
            => "The 'Secure' (SSL) box must be ticked in the server settings; the API key is never sent over plain HTTP.",
        "Sunucu ayarındaki 'Parola' alanına panel API anahtarı (gosp_...) girilmeli."
            => "Enter the panel API key (gosp_...) in the 'Password' field of the server settings.",
        'Panel yanıtı beklenen liste biçiminde değil.' => 'The panel response is not in the expected list format.',
        'İstek gövdesi JSON’a çevrilemedi.' => 'Could not encode the request body as JSON.',
        'Panele bağlanılamadı ({0}:{1}): {2}' => 'Could not connect to the panel ({0}:{1}): {2}',
        'yanıt yok' => 'no response',
        'Panel yanıtı çözümlenemedi (JSON değil).' => 'Could not parse the panel response (not JSON).',
        'Port {0} kullanıldı. gPanel yönetici kapısı portunu (panele tarayıcıda girdiğiniz adresteki port) Hostname alanına "{1}:PORT" biçiminde ya da Access Hash alanına "port=PORT" olarak yazın (Gelişmiş modda Port alanı da kullanılabilir).'
            => 'Port {0} was used. Enter the gPanel admin gate port (the port in the address you open the panel with in a browser) in the Hostname field as "{1}:PORT" or in the Access Hash field as "port=PORT" (in Advanced mode the Port field can also be used).',
        'Panel isteği doğrulamadı:' => 'Panel validation failed:',
        'geçersiz istek.' => 'invalid request.',
        'API anahtarı geçersiz (ya da sunucu ayarındaki kullanıcı adı anahtarın sahibiyle eşleşmiyor).'
            => 'Invalid API key (or the username in the server settings does not match the key owner).',
        'Hesap/bayi askıda olduğu için işlem reddedildi.' => 'Rejected because the account/reseller is suspended.',
        "API anahtarında '{0}' izni eksik ya da bu uç anahtara kapalı." => "The API key lacks the '{0}' permission, or this endpoint is closed to the key.",
        'Erişim reddedildi (yetki ya da kapsam yetersiz).' => 'Access denied (insufficient permission or scope).',
        'Panelde kayıt bulunamadı.' => 'Record not found on the panel.',
        'İşlem mevcut durumla çakışıyor (örn. hesap/bayi askıda ya da kayıt zaten var).'
            => 'The operation conflicts with the current state (e.g. the account/reseller is suspended or the record already exists).',
        'Panel istek hız sınırına takıldı; biraz sonra yeniden deneyin.' => 'Panel rate limit reached; try again shortly.',
        'Panel sunucu hatası (HTTP {0}).' => 'Panel server error (HTTP {0}).',
        'Beklenmeyen panel yanıtı (HTTP {0}).' => 'Unexpected panel response (HTTP {0}).',
        'PHP curl eklentisi yok' => 'PHP curl extension is missing',

        // ── Yardimci: parola kurali ────────────────────────────────────────
        'Parola boş olamaz ve satır sonu/kontrol karakteri içeremez.' => 'The password cannot be empty or contain line breaks/control characters.',
        'Parola en az 12 karakter olmalı.' => 'The password must be at least 12 characters.',
        'Parola en çok 128 karakter olmalı.' => 'The password must be at most 128 characters.',
        'Parola harf ve rakam içermeli.' => 'The password must contain both letters and digits.',

        // ── Modul ──────────────────────────────────────────────────────────
        'Modül hatası: {0} (ayrıntı Modül Günlüğü’nde).' => 'Module error: {0} (details in the Module Log).',
        'Modül hatası: {0}' => 'Module error: {0}',
        "Ürün ayarında '{0}' seçilmemiş." => "'{0}' is not set in the product settings.",
        "{0} bulunamadı (id {1}). Ürün ayarındaki '{2}' alanını kontrol edin." => "{0} not found (ID {1}). Check the '{2}' field in the product settings.",
        "{0} adı birden çok kayıtla eşleşiyor ('{1}'); id kullanın." => "{0} name matches more than one record ('{1}'); use the ID.",
        "{0} bulunamadı ('{1}'). Mevcut: {2}" => "{0} not found ('{1}'). Available: {2}",
        'Panel planı' => 'Panel plan',
        'Bayi paketi' => 'Reseller package',
        "Bu hizmet bir panel hesabına bağlı değil. Yönetici > Hizmet > 'Panel hesap kimliği' alanıyla bağlayın ya da ürün ayarında 'Mevcut hesabı bağla'yı açın."
            => "This service is not linked to a panel account. Link it with the 'Panel account ID' field on the service page (admin area) or enable 'Link existing account' in the product settings.",
        "Panelde '{0}' hesabı bulunamadı." => "Account '{0}' was not found on the panel.",
        "Panel hesabı '{0}' başka bir WHMCS hizmetine bağlı." => "Panel account '{0}' is linked to another WHMCS service.",
        'Hizmette alan adı (domain) boş; hesap oluşturulamaz.' => 'The service has no domain; the account cannot be created.',
        'Panel hesap oluşturdu ama kimlik döndürmedi.' => 'The panel created the account but returned no ID.',
        "'{0}' alan adı panelde zaten var. Başka bir müşterinin hesabına bağlanmamak için otomatik bağlanmadı; ürün ayarında 'Mevcut hesabı bağla'yı açın ya da hizmette 'Panel hesap kimliği'ni elle girin."
            => "Domain '{0}' already exists on the panel. It was not linked automatically to avoid linking another client's account; enable 'Link existing account' in the product settings or enter the 'Panel account ID' on the service manually.",
        "Panelde '{0}' hesabı zaten başka bir WHMCS hizmetine bağlı." => "Account '{0}' on the panel is already linked to another WHMCS service.",
        "Bayi kullanıcı adı geçersiz ('{0}'): 3-32 karakter, küçük harf/rakam/._- olmalı (Hizmet > Kullanıcı adı)."
            => "Invalid reseller username ('{0}'): must be 3-32 characters of lowercase letters/digits/._- (Service > Username).",
        'Bayi parolası 8-128 karakter olmalı.' => 'The reseller password must be 8-128 characters.',
        "'{0}' bayisi panelde zaten var; otomatik bağlanmadı ('Mevcut hesabı bağla' seçeneği kapalı)."
            => "Reseller '{0}' already exists on the panel; it was not linked automatically ('Link existing account' is off).",
        "Bayi '{0}' başka bir WHMCS hizmetine bağlı." => "Reseller '{0}' is linked to another WHMCS service.",
        'Panel bayiyi oluşturdu ama kimlik döndürmedi.' => 'The panel created the reseller but returned no ID.',
        "Bayi açıldı ama '{0}' alan adı panelde başka bir hesaba ait; bayinin hostingi açılamadı."
            => "The reseller was created, but the domain '{0}' belongs to another account on the panel; the reseller's hosting account could not be created.",
        'Bu bayi hizmetinin hosting hesabı yok.' => 'This reseller service has no hosting account.',
        'Bayi parolası değişti ama hosting parolası değiştirilemedi: {0}'
            => 'The reseller password was changed, but the hosting password could not be changed: {0}',
        'Bu gPanel sürümü bayi şifre sıfırlamayı desteklemiyor; paneli güncelleyin. Bayi şifresini panelde Profil sayfasından değiştirebilir.'
            => 'This gPanel version does not support resetting reseller passwords; update the panel. The reseller can change the password on the Profile page in the panel.',
        'Bağlantı kuruldu ama API anahtarında izin eksik: {0} (isteğe bağlı: ftp.parola:yaz, sso, bayi, bayi.parola).'
            => 'Connected, but the API key is missing permissions: {0} (optional: ftp.parola:yaz, sso, bayi, bayi.parola).',
        "Bağlantı kuruldu ama bu port gPanel'in ESKİ (geçiş dönemi) kapısı; yeni yönetici kapısı portu: {0}. Eski kapı kapanınca bağlantı kesilir — sunucu ayarındaki portu {0} yapın."
            => "Connected, but this port is gPanel's OLD (transition period) gate; the new admin gate port is {0}. The connection will break when the old gate closes — set the port in the server settings to {0}.",
        'Panel geçerli bir giriş adresi döndürmedi.' => 'The panel did not return a valid login URL.',
        "Ürün ayarındaki 'Panel adresi' geçersiz (https://alan:PORT/ biçiminde olmalı)." => "The 'Panel URL' product setting is invalid (must be https://domain:PORT/).",
        "Panelin döndürdüğü giriş adresi beklenen ana bilgisayara ait değil (güvenlik nedeniyle reddedildi). Ürün ayarlarında 'Panel adresi' alanına müşteri kapısını (https://alan:PORT/) girin."
            => "The login URL returned by the panel does not belong to the expected host (rejected for security). Enter the client gate (https://domain:PORT/) in the 'Panel URL' product setting.",
        '{0} hizmetin kullanımı veritabanına yazılamadı.' => 'Usage could not be written to the database for {0} service(s).',
        'Hosting kullanımı güncellendi; bayi kullanımı alınamadı: {0}' => 'Hosting usage updated; reseller usage could not be retrieved: {0}',
        'bağlı değil' => 'not linked',
        'bayi hesabı' => 'reseller account',
        'hosting hesabı' => 'hosting account',
        'oluşturma bekliyor/yarım' => 'pending creation/incomplete',
        'Panel hesap kimliği' => 'Panel account ID',
        'Boş bırakmak bağı değiştirmez. Panelde var olan hesabı elle bağlamak için kimliği girin (doğrulanır).'
            => 'Leaving it empty keeps the current link. Enter an existing panel account ID to link it manually (it is verified).',
        'Panel bağlantı durumu' => 'Panel link status',
        'GirginOSPanel: hizmet #{0} bağlanamadı: {1}' => 'GirginOSPanel: service #{0} could not be linked: {1}',
        'panel hesap kimliği sayı olmalı' => 'the panel account ID must be a number',
        'bayi kimliği/kullanıcı adı panelle uyuşmuyor' => 'the reseller ID/username does not match the panel',
        'panel hesabının alan adı hizmetteki alan adıyla uyuşmuyor' => 'the panel account\'s domain does not match the service domain',
        'panel hesabı başka bir hizmete bağlı' => 'the panel account is linked to another service',
        'Hesap bilgisi alınamadı.' => 'Could not retrieve account information.',
        'Geçersiz istek.' => 'Invalid request.',
        'Parolalar eşleşmiyor.' => 'Passwords do not match.',
        'Bu ürün tipi için kullanım yenileme yok.' => 'Usage refresh is not available for this product type.',
    ];
}
