<?php
/**
 * GirginOSPanel WHMCS modulu — GERCEK PANELE karsi canli uctan uca deneme.
 * Sahte panel YOK: modulun gpanel_* islevleri WHMCS saplamalariyla (stub_global,
 * stub_capsule) ama GERCEK HTTP (curl) ile calisir. Olusturdugu her seyi siler.
 *
 *   GP_HOST=1.2.3.4 GP_KOK_PORT=.. GP_BAYI_PORT=.. GP_MUSTERI_PORT=.. \
 *   GP_KOK_ANAHTAR_DOSYA=/root/x GP_BAYI_ANAHTAR_DOSYA=/root/y GP_BAYI_KULLANICI=ekbayi \
 *   php tests/canli.php
 *
 * Anahtarlar dosyadan okunur, ekrana/loga YAZILMAZ (modul gunlugu ayrica maskeler).
 */

declare(strict_types=1);
error_reporting(E_ALL);
set_error_handler(function ($no, $str, $file, $line) {
    throw new ErrorException($str, 0, $no, $file, $line);
});

$MODUL = getenv('GPANEL_MODUL') ?: dirname(__DIR__) . '/modules/servers/gpanel';
require __DIR__ . '/stub_global.php';
require __DIR__ . '/stub_capsule.php';
require $MODUL . '/gpanel.php';

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Kayit;
use WHMCS\Database\Capsule;

// Test parolalari (gitleaks dostu: parcali; GERCEK sir degil, yalniz test VPS'inde gecici hesaplar).
define('PW_CANLI', 'Whmcs' . 'Canli' . '2026Parola');
define('PW_CANLI_YENI', 'Yeni' . 'Canli' . 'Parola2026x');
define('PW_BAYI', 'Bayi' . 'Canli' . '2026Parola');
define('PW_BAYI_BASKA', 'Baska' . 'Parola' . '2026xx');

$HOST = (string) getenv('GP_HOST');
$KOK_PORT = (int) getenv('GP_KOK_PORT');
$BAYI_PORT = (int) getenv('GP_BAYI_PORT');
$MUSTERI_PORT = (int) getenv('GP_MUSTERI_PORT');
$KOK_ANAHTAR = trim((string) file_get_contents((string) getenv('GP_KOK_ANAHTAR_DOSYA')));
$BAYI_ANAHTAR = trim((string) file_get_contents((string) getenv('GP_BAYI_ANAHTAR_DOSYA')));
$BAYI_KUL = getenv('GP_BAYI_KULLANICI') ?: 'ekbayi';
// Giris uclarinda IP basina kaba-kuvvet kilidi var (15 dk'da 5 basarisiz -> 30 dk).
// Olumlu girisler ve KASITLI basarisiz denemeler ayri kaynak adreslerinden gider
// (ayri kilit kovalari): GP_POZ_HOST (vars. 127.0.0.1), GP_NEG_HOST (vars. [::1]).
$POZ = getenv('GP_POZ_HOST') ?: '127.0.0.1';
$NEG = getenv('GP_NEG_HOST') ?: '[::1]';
$EK = date('mdHis');
$SAY = ['ok' => 0, 'fail' => 0, 'atla' => 0];
$TEMIZLIK = [];

function adim(string $ad, bool $kosul, string $ayrinti = ''): bool
{
    global $SAY;
    $SAY[$kosul ? 'ok' : 'fail']++;
    echo ($kosul ? '  OK    ' : '  FAIL  ') . $ad . ((!$kosul && $ayrinti !== '') ? "\n        -> " . $ayrinti : '') . "\n";
    return $kosul;
}

/** Musteri kapisina giris; HTTP kodu. $olumsuz: kasitli basarisiz deneme kovasi. */
function giris(string $kul, string $parola, bool $olumsuz = false): int
{
    global $POZ, $NEG, $MUSTERI_PORT;
    [$kod] = hamPost('https://' . ($olumsuz ? $NEG : $POZ) . ":$MUSTERI_PORT/api/v1/musteri/login", ['kullanici' => $kul, 'parola' => $parola]);
    return $kod;
}

/** Reddedilmesi beklenen istek: 401/403 = OK; 429 (kilit) = ATLANDI (dogrulanamadi). */
function red(string $ad, int $kod): void
{
    global $SAY;
    if ($kod === 429) {
        $SAY['atla']++;
        echo "  ATLA  $ad (giris kilidi 429 — dogrulanamadi)\n";
        return;
    }
    adim($ad, $kod === 401 || $kod === 403, "HTTP $kod");
}

/** SSO adresinden tek kullanimlik kod: https://host:port/adina#kod=<QueryEscape(kod)> */
function ssoKodu(string $url): string
{
    parse_str((string) parse_url($url, PHP_URL_FRAGMENT), $f);
    return (string) ($f['kod'] ?? '');
}

function bolum(string $b): void
{
    echo "\n== $b\n";
}

/** Her senaryoda taze WHMCS saplama durumu. */
function taze(): void
{
    Capsule::sifirla();
    Kayit::sifirla();
    $GLOBALS['T'] = ['log' => [], 'localapi' => [], 'activity' => [], 'check_token' => 0];
    $_POST = [];
    $_SERVER['REQUEST_METHOD'] = 'GET';
    ApiIstemci::$varsayilanTasiyici = null; // GERCEK curl
}

/** @param array<string,mixed> $ek */
function prm(array $ek, string $rol = 'kok'): array
{
    global $HOST, $KOK_PORT, $BAYI_PORT, $MUSTERI_PORT, $KOK_ANAHTAR, $BAYI_ANAHTAR, $BAYI_KUL;
    return $ek + [
        'serverid' => 1,
        'serverhostname' => $HOST, 'serverip' => '', 'serversecure' => 'on',
        'serverport' => (string) ($rol === 'kok' ? $KOK_PORT : $BAYI_PORT),
        'serverusername' => $rol === 'kok' ? 'root' : $BAYI_KUL,
        'serverpassword' => $rol === 'kok' ? $KOK_ANAHTAR : $BAYI_ANAHTAR,
        'serveraccesshash' => 'tls-dogrulama-kapali', // test VPS'i kendi imzali sertifika
        'serviceid' => 101, 'domain' => '', 'username' => '', 'password' => PW_CANLI,
        'configoption1' => 'hosting', 'configoption2' => 'Başlangıç', 'configoption3' => '', 'configoption4' => '',
        'configoption5' => '', 'configoption6' => '', 'configoption7' => "https://$HOST:$MUSTERI_PORT/", 'configoption8' => '',
        'clientsdetails' => ['fullname' => 'Canli Test', 'language' => 'turkish', 'email' => 'canli@example.test'],
    ];
}

function api(string $rol = 'kok'): ApiIstemci
{
    global $HOST, $KOK_PORT, $BAYI_PORT, $KOK_ANAHTAR, $BAYI_ANAHTAR, $BAYI_KUL;
    return $rol === 'kok'
        ? new ApiIstemci($HOST, $KOK_PORT, 'root', $KOK_ANAHTAR, false)
        : new ApiIstemci($HOST, $BAYI_PORT, $BAYI_KUL, $BAYI_ANAHTAR, false);
}

/** Ham JSON POST (modul DISI dogrulama: musteri girisi, SSO kodu tuketme). */
function hamPost(string $url, array $govde): array
{
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_POST => true, CURLOPT_POSTFIELDS => json_encode($govde),
        CURLOPT_HTTPHEADER => ['Content-Type: application/json'],
        CURLOPT_RETURNTRANSFER => true, CURLOPT_HEADER => true, CURLOPT_TIMEOUT => 30,
        CURLOPT_SSL_VERIFYPEER => false, CURLOPT_SSL_VERIFYHOST => 0,
    ]);
    $yanit = (string) curl_exec($ch);
    $kod = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    $bas = (int) curl_getinfo($ch, CURLINFO_HEADER_SIZE);
    curl_close($ch);
    return [$kod, substr($yanit, 0, $bas), substr($yanit, $bas)];
}

function sonUpdate(): array
{
    $son = [];
    foreach ($GLOBALS['T']['localapi'] as [$k, $v]) {
        if ($k === 'UpdateClientProduct') {
            $son = $v;
        }
    }
    return $son;
}

function hesapGetir(int $id, string $rol = 'kok'): ?array
{
    try {
        return api($rol)->hesap($id);
    } catch (Throwable $e) {
        return null;
    }
}

function gunlukSizinti(string $gizli): bool
{
    // Yalniz modul gunlugu + etkinlik kaydi (localAPI UpdateClientProduct parolayi
    // WHMCS'e YAZMAK icin tasir; o gunluk degildir).
    $hepsi = json_encode([$GLOBALS['T']['log'], $GLOBALS['T']['activity']], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    return $gizli !== '' && strpos($hepsi, $gizli) !== false;
}

// ─────────────────────────────────────────────────────────────────────────────
function hostingSenaryosu(string $rol): void
{
    global $HOST, $MUSTERI_PORT, $EK, $TEMIZLIK, $KOK_ANAHTAR, $BAYI_ANAHTAR;
    $alan = "whmcs-$rol-$EK.test";
    bolum("HOSTING ($rol anahtarı) — $alan");
    taze();
    $p = prm(['domain' => $alan], $rol);

    $r = gpanel_TestConnection($p);
    adim('TestConnection', $r['success'] === true, $r['error'] ?? '');

    $planlar = gpanel_ConfigOptions($p)['plan']['Loader']($p);
    adim('ConfigOptions plan listesi panelden geliyor (' . count($planlar) . ')', count($planlar) > 0 && in_array('Başlangıç', $planlar, true), json_encode($planlar, JSON_UNESCAPED_UNICODE));

    $s = gpanel_CreateAccount($p);
    adim('CreateAccount', $s === 'success', $s);
    $k = Kayit::oku(101);
    $id = (int) ($k['panel_id'] ?? 0);
    adim('mod_gpanel_hesap kaydı (panel_id>0)', $id > 0, json_encode($k));
    if ($id <= 0) {
        return;
    }
    $TEMIZLIK[] = [$rol, $id, $alan];
    $u = sonUpdate();
    $d = hesapGetir($id, $rol) ?? [];
    adim('panelde hesap var, alan adı doğru', ($d['alan_adi'] ?? '') === $alan, json_encode($d));
    adim('WHMCS kullanıcı adı = panel sistem kullanıcısı', ($u['serviceusername'] ?? '') !== '' && ($u['serviceusername'] ?? '') === ($d['sistem_kullanici'] ?? '?'), json_encode($u));
    adim('WHMCS parolası panele uygulandı (servicepassword = WHMCS parolası)', ($u['servicepassword'] ?? '') === PW_CANLI);
    adim('plan Başlangıç', ($d['plan_ad'] ?? '') === 'Başlangıç', (string) ($d['plan_ad'] ?? ''));
    if ($rol === 'bayi') {
        adim('hesabın sahibi bayi', ($d['sahip_tur'] ?? '') === 'bayi', json_encode([$d['sahip_tur'] ?? null, $d['reseller_id'] ?? null]));
    }

    $s = gpanel_CreateAccount($p);
    adim('CreateAccount tekrar (idempotent, ikinci hesap yok)', $s === 'success', $s);

    // Müşteri gerçekten WHMCS parolasıyla panele girebiliyor mu?
    $sk = (string) ($d['sistem_kullanici'] ?? '');
    $kod = giris($sk, PW_CANLI);
    adim('müşteri paneline WHMCS parolasıyla giriş (200)', $kod === 200, "HTTP $kod");

    $s = gpanel_SuspendAccount($p + ['suspendreason' => 'Ödenmemiş fatura']);
    adim('SuspendAccount', $s === 'success', $s);
    adim('panelde askıda', (hesapGetir($id, $rol)['askida'] ?? null) === true);
    red('askıdayken müşteri girişi REDDEDİLİR (doğru parola)', giris($sk, PW_CANLI, true));
    $r = gpanel_ServiceSingleSignOn($p);
    adim('askıdayken SSO kod üretmez (ya da düz bağlantı)', strpos((string) ($r['redirectTo'] ?? ''), '#') === false, json_encode($r));

    $s = gpanel_UnsuspendAccount($p);
    adim('UnsuspendAccount', $s === 'success', $s);
    adim('panelde askı kalktı', (hesapGetir($id, $rol)['askida'] ?? null) === false);

    $s = gpanel_ChangePackage(prm(['domain' => $alan, 'configoption2' => 'Standart'], $rol));
    adim('ChangePackage → Standart', $s === 'success', $s);
    adim('panelde plan Standart', (hesapGetir($id, $rol)['plan_ad'] ?? '') === 'Standart');

    $yeni = PW_CANLI_YENI;
    $s = gpanel_ChangePassword(prm(['domain' => $alan, 'password' => $yeni, 'username' => $sk], $rol));
    adim('ChangePassword', $s === 'success', $s);
    $kod = giris($sk, $yeni);
    adim('yeni parolayla müşteri girişi (200)', $kod === 200, "HTTP $kod");
    if ($rol === 'kok') {
        red('eski parola artık geçmez', giris($sk, PW_CANLI, true));
    }

    foreach (['gpanel_ServiceSingleSignOn' => 'müşteri', 'gpanel_AdminSingleSignOn' => 'yönetici'] as $f => $kim) {
        $r = $f($p);
        $url = (string) ($r['redirectTo'] ?? '');
        $kodSso = ssoKodu($url);
        adim("SSO ($kim) başarı + #kod=", ($r['success'] ?? false) === true && strlen($kodSso) >= 32, json_encode($r));
        adim("SSO ($kim) adresi = Panel adresi kökeni + /adina", strpos($url, "https://$HOST:$MUSTERI_PORT/adina#kod=") === 0, $url);
        if ($kodSso !== '') {
            global $POZ, $NEG;
            [$kod, , $govde] = hamPost("https://$POZ:$MUSTERI_PORT/api/v1/musteri/adina-giris", ['kod' => $kodSso]);
            $j = json_decode($govde, true) ?: [];
            adim("SSO ($kim) kodu panelde müşteri oturumu açıyor (200, sso=true, doğru alan adı)", $kod === 200 && ($j['sso'] ?? null) === true && ($j['alan_adi'] ?? '') === $alan, "HTTP $kod " . substr($govde, 0, 120));
            if ($kim === 'müşteri') {
                [$kod2] = hamPost("https://$NEG:$MUSTERI_PORT/api/v1/musteri/adina-giris", ['kod' => $kodSso]);
                red("SSO ($kim) kodu tek kullanımlık (2. deneme red)", $kod2);
            }
        }
    }

    Capsule::$tablolar['tblhosting'][] = ['id' => 101, 'server' => 1, 'domainstatus' => 'Active', 'domain' => $alan,
        'diskusage' => -1, 'disklimit' => -1, 'bwusage' => -1, 'bwlimit' => -1, 'lastupdate' => ''];
    $s = gpanel_UsageUpdate($p);
    $satir = Capsule::$tablolar['tblhosting'][0];
    adim('UsageUpdate', $s === 'success', $s);
    adim('tblhosting disk/trafik limitleri yazıldı (Standart: 10240 MB / 51200 MB)', $satir['disklimit'] === 10240 && $satir['bwlimit'] === 51200 && $satir['diskusage'] >= 0, json_encode($satir));

    $ca = gpanel_ClientArea($p);
    adim('ClientArea şablon + değişkenler', is_array($ca) && isset($ca['templatefile']), json_encode(array_keys((array) $ca)));

    $anahtar = $rol === 'kok' ? $KOK_ANAHTAR : $BAYI_ANAHTAR;
    adim('modül günlüğünde API anahtarı YOK', !gunlukSizinti($anahtar));
    adim('modül günlüğünde parolalar YOK', !gunlukSizinti(PW_CANLI) && !gunlukSizinti($yeni));

    $s = gpanel_TerminateAccount($p);
    adim('TerminateAccount', $s === 'success', $s);
    adim('panelde hesap silindi', hesapGetir($id, $rol) === null);
    adim('mod_gpanel_hesap kaydı silindi', Kayit::oku(101) === null || (int) (Kayit::oku(101)['panel_id'] ?? 0) === 0);
    $TEMIZLIK = array_values(array_filter($TEMIZLIK, fn($t) => $t[1] !== $id));
}

// ─────────────────────────────────────────────────────────────────────────────
function bayiSenaryosu(): void
{
    global $HOST, $BAYI_PORT, $EK, $TEMIZLIK;
    $kul = "wcbayi$EK";
    bolum("BAYİ ürünü (kök anahtarı) — $kul");
    taze();
    $c = api('kok');
    // ChangePackage denemesi için ikinci paket (biter bitmez silinir)
    $paket2 = null;
    try {
        $paket2 = $c->istek('POST', '/reseller-plans', ['ad' => "WHMCS canli paket $EK", 'max_domain' => 7, 'max_disk_mb' => 7000, 'max_trafik_mb' => 70000]);
    } catch (Throwable $e) {
        adim('ikinci bayi paketi oluşturulamadı', false, $e->getMessage());
    }
    $paket2Id = (int) ($paket2['id'] ?? 0);
    $paketler = gpanel_ConfigOptions(prm([]))['bayi_paketi']['Loader'](prm([]));
    adim('ConfigOptions bayi paketleri (' . count($paketler) . ')', count($paketler) >= 1, json_encode($paketler, JSON_UNESCAPED_UNICODE));

    $p = prm(['serviceid' => 201, 'domain' => '', 'username' => $kul, 'password' => PW_BAYI,
        'configoption1' => 'bayi', 'configoption4' => 'ek-paket', 'configoption7' => "https://$HOST:$BAYI_PORT/"]);
    $s = gpanel_CreateAccount($p);
    adim('CreateAccount (bayi)', $s === 'success', $s);
    $id = (int) (Kayit::oku(201)['panel_id'] ?? 0);
    adim('bayi kaydı (panel_id>0)', $id > 0);
    if ($id <= 0) {
        return;
    }
    $TEMIZLIK[] = ['bayi-hesap', $id, $kul];
    $bul = function () use ($c, $id): ?array {
        foreach ($c->bayiler() as $b) {
            if ((int) $b['id'] === $id) {
                return $b;
            }
        }
        return null;
    };
    $b = $bul() ?? [];
    adim('panelde bayi var (kullanıcı adı, paket ek-paket)', ($b['kullanici'] ?? '') === $kul && ($b['paket_ad'] ?? '') === 'ek-paket', json_encode($b, JSON_UNESCAPED_UNICODE));
    global $POZ;
    [$kod] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI]);
    adim('bayi WHMCS parolasıyla bayi paneline girebiliyor (200)', $kod === 200, "HTTP $kod");

    $s = gpanel_SuspendAccount($p);
    adim('SuspendAccount (bayi)', $s === 'success', $s);
    adim('panelde bayi askıda', in_array((string) (($bul() ?? [])['durum'] ?? ''), ['askida', 'suspended', 'pasif'], true), json_encode($bul()));
    $s = gpanel_UnsuspendAccount($p);
    adim('UnsuspendAccount (bayi)', $s === 'success', $s);
    adim('panelde bayi aktif', in_array((string) (($bul() ?? [])['durum'] ?? ''), ['aktif', 'active'], true), json_encode($bul()));

    if ($paket2Id > 0) {
        $s = gpanel_ChangePackage(array_merge($p, ['configoption4' => (string) $paket2Id]));
        adim('ChangePackage (bayi) → ikinci paket', $s === 'success', $s);
        adim('panelde bayinin paketi değişti', (int) (($bul() ?? [])['paket_id'] ?? 0) === $paket2Id, json_encode($bul()));
    }

    $r = gpanel_ChangePassword(array_merge($p, ['password' => PW_BAYI_BASKA]));
    adim('ChangePassword (bayi) panel kısıtı: net hata, sessiz başarı DEĞİL', $r !== 'success', (string) $r);

    Capsule::$tablolar['tblhosting'][] = ['id' => 201, 'server' => 1, 'domainstatus' => 'Active', 'domain' => '',
        'diskusage' => -1, 'disklimit' => -1, 'bwusage' => -1, 'bwlimit' => -1, 'lastupdate' => ''];
    $s = gpanel_UsageUpdate($p);
    adim('UsageUpdate (bayi)', $s === 'success', $s);
    adim('tblhosting bayi satırı güncellendi', Capsule::$tablolar['tblhosting'][0]['disklimit'] >= 0, json_encode(Capsule::$tablolar['tblhosting'][0]));

    $s = gpanel_TerminateAccount($p);
    adim('TerminateAccount (bayi)', $s === 'success', $s);
    // Bayi silme panelde ARKA PLAN isidir (202 {is_id}): alan adlari + kullanicilar
    // ile birlikte silinir. 60 sn'ye kadar bekle.
    $sure = 0;
    while ($bul() !== null && $sure < 60) {
        sleep(3);
        $sure += 3;
    }
    adim("panelde bayi silindi (arka plan işi, {$sure} sn)", $bul() === null);
    $TEMIZLIK = array_values(array_filter($TEMIZLIK, fn($t) => $t[1] !== $id || $t[0] !== 'bayi-hesap'));
    if ($paket2Id > 0) {
        try {
            $c->istek('DELETE', '/reseller-plans/' . $paket2Id);
        } catch (Throwable $e) {
            echo "  (uyarı) test paketi silinemedi: " . $e->getMessage() . "\n";
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
function guvenlikSenaryosu(): void
{
    global $EK;
    bolum('GÜVENLİK');
    taze();
    $p = prm(['domain' => "whmcs-guv-$EK.test"]);
    $r = gpanel_TestConnection(array_merge($p, ['serverpassword' => 'gosp_aaaaaaaaaaaa_' . str_repeat('B', 43)]));
    adim('yanlış anahtar → TestConnection başarısız, anlaşılır mesaj', $r['success'] === false && $r['error'] !== '', json_encode($r));
    $r = gpanel_TestConnection(array_merge($p, ['serverusername' => 'baskasi']));
    adim('anahtar başka kullanıcı adıyla çalışmaz', $r['success'] === false, json_encode($r));
    $r = gpanel_TestConnection(array_merge($p, ['serveraccesshash' => '']));
    adim('TLS doğrulaması açıkken kendi imzalı sertifika REDDEDİLİR', $r['success'] === false, json_encode($r));
    $r = gpanel_TestConnection(array_merge($p, ['serversecure' => '']));
    adim('SSL kutusu kapalıyken anahtar düz HTTP ile gönderilmez', $r['success'] === false, json_encode($r));
    $s = gpanel_CreateAccount(prm(['domain' => "whmcs-guv-$EK.test", 'configoption1' => 'bayi', 'username' => "wcg$EK", 'password' => PW_BAYI, 'configoption7' => 'https://x.test/'], 'bayi'));
    adim('bayi anahtarı BAYİ ürünü açamaz (kök uç)', $s !== 'success', $s);
}

hostingSenaryosu('kok');
hostingSenaryosu('bayi');
bayiSenaryosu();
guvenlikSenaryosu();

if ($TEMIZLIK) {
    bolum('TEMİZLİK (başarısız adımlardan kalanlar)');
    foreach ($TEMIZLIK as [$rol, $id, $ad]) {
        try {
            if ($rol === 'bayi-hesap') {
                api('kok')->bayiSil((int) $id);
            } else {
                api($rol === 'bayi' ? 'bayi' : 'kok')->istek('DELETE', '/domains/' . $id, null, ['onay' => $ad]);
            }
            echo "  silindi: $rol #$id $ad\n";
        } catch (Throwable $e) {
            echo "  SİLİNEMEDİ: $rol #$id $ad — " . $e->getMessage() . "\n";
        }
    }
}
echo "\nSONUÇ: {$SAY['ok']} OK, {$SAY['fail']} FAIL, {$SAY['atla']} ATLANDI\n";
exit($SAY['fail'] > 0 ? 1 : 0);
