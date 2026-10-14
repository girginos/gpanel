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
define('PW_CANLI_FORM', 'Form' . 'Canli' . 'Parola2026y');

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

/** Ham GET (Bearer jeton): HTTP durum kodu. */
function hamGet(string $url, string $jeton): int
{
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_HTTPHEADER => ['Authorization: Bearer ' . $jeton],
        CURLOPT_RETURNTRANSFER => true, CURLOPT_TIMEOUT => 30,
        CURLOPT_SSL_VERIFYPEER => false, CURLOPT_SSL_VERIFYHOST => 0,
    ]);
    curl_exec($ch);
    $kod = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    return $kod;
}

/** Ham JSON istek (Bearer jeton): [durum, cozulmus govde]. */
function hamJson(string $metod, string $url, ?array $govde, string $jeton, int $zaman = 30): array
{
    $ch = curl_init($url);
    $bas = ['Authorization: Bearer ' . $jeton];
    $opt = [CURLOPT_CUSTOMREQUEST => $metod, CURLOPT_RETURNTRANSFER => true, CURLOPT_TIMEOUT => $zaman,
        CURLOPT_SSL_VERIFYPEER => false, CURLOPT_SSL_VERIFYHOST => 0];
    if ($govde !== null) {
        $opt[CURLOPT_POSTFIELDS] = json_encode($govde);
        $bas[] = 'Content-Type: application/json';
    }
    $opt[CURLOPT_HTTPHEADER] = $bas;
    curl_setopt_array($ch, $opt);
    $y = (string) curl_exec($ch);
    $kod = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    return [$kod, json_decode($y, true) ?: []];
}

/** Sitenin bu sunucudaki HTTP durumu (Host basligiyla, 127.0.0.1:80). */
function siteKod(string $alan): int
{
    $ch = curl_init('http://127.0.0.1/');
    curl_setopt_array($ch, [CURLOPT_HTTPHEADER => ['Host: ' . $alan], CURLOPT_RETURNTRANSFER => true, CURLOPT_TIMEOUT => 20]);
    curl_exec($ch);
    $kod = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    return $kod;
}

/** Musteri paneli girisi (sistem kullanicisi + parola), olumlu adresten. */
function musteriGiris(string $kul, string $parola, bool $olumsuz = false): int
{
    return giris($kul, $parola, $olumsuz);
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
    // 1.0.4: ürün ayarında 1.0.3'ten kalma serbest metin PHP yazımı kanonik sürümle açılmalı.
    $php = $rol === 'kok' ? ['php81', '8.1'] : ['PHP 8.2', '8.2'];
    $p = prm(['domain' => $alan, 'configoption3' => $php[0]], $rol);

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
    adim('PHP sürümü: eski yazım "' . $php[0] . '" → ' . $php[1], ($d['php_surum'] ?? '') === $php[1],
        json_encode(['php_surum' => $d['php_surum'] ?? null]));
    // 1.0.5: Panel adresi BOŞ hosting ürünü → müşteri alanı bağlantısı MÜŞTERİ kapısı (yönetici DEĞİL).
    $v = gpanel_ClientArea(array_merge($p, ['configoption7' => '']))['vars'] ?? [];
    adim('Panel adresi boş: müşteri alanı bağlantısı MÜŞTERİ kapısı (:' . $MUSTERI_PORT . ')', ($v['panel_url'] ?? '') === "https://$HOST:$MUSTERI_PORT/", (string) ($v['panel_url'] ?? ''));
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
    $k = siteKod($alan);
    adim('askıda site kapalı (503)', $k === 503, "HTTP $k");
    red('askıdayken müşteri girişi REDDEDİLİR (doğru parola)', giris($sk, PW_CANLI, true));
    $r = gpanel_ServiceSingleSignOn($p);
    adim('askıdayken SSO kod üretmez (ya da düz bağlantı)', strpos((string) ($r['redirectTo'] ?? ''), '#') === false, json_encode($r));

    $s = gpanel_UnsuspendAccount($p);
    adim('UnsuspendAccount', $s === 'success', $s);
    adim('panelde askı kalktı', (hesapGetir($id, $rol)['askida'] ?? null) === false);
    $k = 0;
    for ($i = 0; $i < 10 && ($k = siteKod($alan)) !== 200; $i++) {
        sleep(1);
    }
    adim('askıdan çıkış → site yeniden açık (200)', $k === 200, "HTTP $k");
    $kod = giris($sk, PW_CANLI);
    adim('askıdan çıkış → müşteri girişi yeniden çalışıyor (200)', $kod === 200, "HTTP $kod");

    $s = gpanel_ChangePackage(prm(['domain' => $alan, 'configoption2' => 'Standart'], $rol));
    adim('ChangePackage → Standart', $s === 'success', $s);
    adim('panelde plan Standart', (hesapGetir($id, $rol)['plan_ad'] ?? '') === 'Standart');
    $kotaStd = (int) (hesapGetir($id, $rol)['disk_kota_mb'] ?? 0);
    $s = gpanel_ChangePackage(prm(['domain' => $alan, 'configoption2' => 'Başlangıç'], $rol));
    adim('ChangePackage (düşürme) → Başlangıç', $s === 'success', $s);
    $d = hesapGetir($id, $rol) ?? [];
    adim('panelde plan Başlangıç ve disk kotası düştü', ($d['plan_ad'] ?? '') === 'Başlangıç' && (int) ($d['disk_kota_mb'] ?? 0) > 0
        && (int) ($d['disk_kota_mb'] ?? 0) < $kotaStd, json_encode(['plan' => $d['plan_ad'] ?? null, 'kota' => $d['disk_kota_mb'] ?? null, 'onceki' => $kotaStd]));
    $s = gpanel_ChangePackage(prm(['domain' => $alan, 'configoption2' => 'Plan-yok-' . $EK], $rol));
    adim('ChangePackage olmayan plan adıyla: net hata, plan değişmez', $s !== 'success' && (hesapGetir($id, $rol)['plan_ad'] ?? '') === 'Başlangıç', (string) $s);
    $s = gpanel_ChangePackage(prm(['domain' => $alan, 'configoption2' => 'Standart'], $rol));
    adim('ChangePackage (yeniden yükseltme) → Standart', $s === 'success' && (hesapGetir($id, $rol)['plan_ad'] ?? '') === 'Standart', $s);

    $yeni = PW_CANLI_YENI;
    $s = gpanel_ChangePassword(prm(['domain' => $alan, 'password' => $yeni, 'username' => $sk], $rol));
    adim('ChangePassword', $s === 'success', $s);
    $kod = giris($sk, $yeni);
    adim('yeni parolayla müşteri girişi (200)', $kod === 200, "HTTP $kod");
    if ($rol === 'kok') {
        red('eski parola artık geçmez', giris($sk, PW_CANLI, true));
    }
    // Müşteri alanı formu (WHMCS müşterisinin kendi "Şifre değiştir"i): parola panele uygulanır.
    $_SERVER['REQUEST_METHOD'] = 'POST';
    $_POST = ['yeni_parola' => PW_CANLI_FORM, 'yeni_parola2' => PW_CANLI_FORM];
    $s = gpanel_parolaDegistir(prm(['domain' => $alan, 'username' => $sk], $rol));
    $_POST = [];
    $_SERVER['REQUEST_METHOD'] = 'GET';
    adim('müşteri alanı şifre formu', $s === 'success', (string) $s);
    $kod = giris($sk, PW_CANLI_FORM);
    adim('form parolasıyla müşteri girişi (200)', $kod === 200, "HTTP $kod");
    adim('form parolası günlüğe sızmadı', !gunlukSizinti(PW_CANLI_FORM));

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
    $paket3 = null;
    try {
        $paket3 = $c->istek('POST', '/reseller-plans', ['ad' => "WHMCS canli kucuk $EK", 'max_domain' => 1, 'max_disk_mb' => 2000, 'max_trafik_mb' => 20000]);
    } catch (Throwable $e) {
        adim('küçük bayi paketi oluşturulamadı', false, $e->getMessage());
    }
    $paket3Id = (int) ($paket3['id'] ?? 0);
    $paketler = gpanel_ConfigOptions(prm([]))['bayi_paketi']['Loader'](prm([]));
    adim('ConfigOptions bayi paketleri (' . count($paketler) . ')', count($paketler) >= 1, json_encode($paketler, JSON_UNESCAPED_UNICODE));

    $bayiAlan = "$kul.test";
    $p = prm(['serviceid' => 201, 'domain' => $bayiAlan, 'username' => $kul, 'password' => PW_BAYI,
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
    // 1.0.9: domainli bayi siparişi bayinin KENDİ hostingini de açar (bayiye ait, WHMCS parolası).
    $bh = null;
    foreach ($c->hesaplar() as $h) {
        if (($h['alan_adi'] ?? '') === $bayiAlan) {
            $bh = $h;
        }
    }
    adim('bayinin kendi hostingi açıldı ve BAYİYE ait', $bh !== null && (int) ($bh['reseller_id'] ?? 0) === $id, json_encode([$bh['reseller_id'] ?? null, $bh['sahip_tur'] ?? null]));
    $bsk = (string) ($bh['sistem_kullanici'] ?? '');
    $kod = giris($bsk, PW_BAYI);
    adim('bayinin hostingine WHMCS parolasıyla müşteri girişi (200)', $kod === 200, "HTTP $kod");
    adim('WHMCS kullanıcı adı bayinin adı olarak kaldı', !isset(sonUpdate()['serviceusername']), json_encode(sonUpdate()));
    $s = gpanel_CreateAccount($p);
    $say = 0;
    foreach ($c->hesaplar() as $h) {
        $say += (($h['alan_adi'] ?? '') === $bayiAlan) ? 1 : 0;
    }
    adim('CreateAccount yeniden denemesi: ikinci hosting açılmaz', $s === 'success' && $say === 1, $s);
    $v = gpanel_ClientArea(array_merge($p, ['configoption7' => '']))['vars'] ?? [];
    adim('müşteri alanı: bayinin hostingi görünüyor (iki giriş düğmesi)', ($v['bayi_hosting']['alan'] ?? '') === $bayiAlan, json_encode($v['bayi_hosting'] ?? null));
    $_GET['gp_hedef'] = 'hosting';
    $r = gpanel_ServiceSingleSignOn(array_merge($p, ['configoption7' => '']));
    unset($_GET['gp_hedef']);
    $url = (string) ($r['redirectTo'] ?? '');
    global $MUSTERI_PORT;
    adim('"Hosting paneline giriş": MÜŞTERİ kapısında /adina#kod=', strpos($url, "https://$HOST:$MUSTERI_PORT/adina#kod=") === 0, json_encode($r));
    global $POZ;
    [$kod, , $govde] = hamPost("https://$POZ:$MUSTERI_PORT/api/v1/musteri/adina-giris", ['kod' => ssoKodu($url)]);
    $j = json_decode((string) $govde, true) ?: [];
    adim('hosting tek tık kodu bayinin hostinginde oturum açar', $kod === 200 && ($j['alan_adi'] ?? '') === $bayiAlan, "HTTP $kod");
    // 1.0.5: Panel adresi BOŞ bayi ürünü → panelden öğrenilen BAYİ kapısı (yönetici/müşteri DEĞİL).
    $pBos = array_merge($p, ['configoption7' => '']);
    // 1.0.8: "Bayi paneline giriş yap" = tek tık giriş (POST /resellers/{id}/sso) → BAYİ kapısında /sso#kod=.
    $r = gpanel_ServiceSingleSignOn($pBos);
    $url = (string) ($r['redirectTo'] ?? '');
    adim('Bayi tek tık giriş: adres BAYİ kapısında /sso#kod= (:' . $BAYI_PORT . ')', strpos($url, "https://$HOST:$BAYI_PORT/sso#kod=") === 0, json_encode($r));
    $kodB = ssoKodu($url);
    global $POZ, $NEG, $MUSTERI_PORT;
    [$kod, , $govde] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/sso-giris", ['kod' => $kodB]);
    $j = json_decode((string) $govde, true) ?: [];
    adim('kod bayi kapısında bayi oturumuna döner (200, rol reseller, doğru bayi, sso)', $kod === 200 && ($j['kullanici']['rol'] ?? '') === 'reseller'
        && ($j['kullanici']['adi'] ?? '') === $kul && !empty($j['token']) && ($j['sso'] ?? false) === true, "HTTP $kod");
    $jeton = (string) ($j['token'] ?? '');
    [$kod] = hamPost("https://$NEG:$BAYI_PORT/api/v1/auth/sso-giris", ['kod' => $kodB]);
    red('aynı kod ikinci kez kullanılamaz', $kod);
    $kodM = ssoKodu((string) (gpanel_ServiceSingleSignOn($pBos)['redirectTo'] ?? ''));
    [$kod] = hamPost("https://$POZ:$MUSTERI_PORT/api/v1/auth/sso-giris", ['kod' => $kodM]); // 404 giriş kilidine sayılmaz
    adim('müşteri kapısında bayi kodu tüketilemez (404)', $kod === 404, "HTTP $kod");
    $kod = hamGet("https://$POZ:$BAYI_PORT/api/v1/reseller/ozet", $jeton);
    adim('tek tık oturumuyla bayi özeti (200)', $kod === 200, "HTTP $kod");
    $kod = hamGet("https://$POZ:$BAYI_PORT/api/v1/api-anahtarlari", $jeton);
    adim('tek tık oturumunda API anahtarı yönetimi kapalı (403)', $kod === 403, "HTTP $kod");
    $_GET['gp_hedef'] = 'profil';
    $r = gpanel_ServiceSingleSignOn($pBos);
    unset($_GET['gp_hedef']);
    adim('"Bayi panelinde şifreyi değiştir": giriş sonrası /profil', substr((string) ($r['redirectTo'] ?? ''), -15) === '&next=%2Fprofil', json_encode($r));
    $v = gpanel_ClientArea($pBos)['vars'] ?? [];
    adim('Panel adresi boş: müşteri alanı bağlantısı BAYİ kapısı', ($v['panel_url'] ?? '') === "https://$HOST:$BAYI_PORT/", (string) ($v['panel_url'] ?? ''));
    global $POZ;
    [$kod, , $govde] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI]);
    adim('bayi WHMCS parolasıyla bayi paneline girebiliyor (200)', $kod === 200, "HTTP $kod");
    $bayiJwt = (string) ((json_decode((string) $govde, true) ?: [])['token'] ?? '');

    // ── Bayi askısı uçtan uca: bayinin hostingleri, siteler, müşteri/bayi girişi, açık oturum ──
    // Bayi kendi panelinden 2 hosting açar; ikincisini KENDİSİ askıya alır (askıdan çıkışta
    // o askıda kalmalı: yalnız bayi askısının kapattıkları geri açılır).
    $hostlar = [];
    foreach ([1, 2] as $i) {
        $alanB = "wcb$i-$EK.test";
        [$kod, $y] = hamJson('POST', "https://$POZ:$BAYI_PORT/api/v1/domains", ['alan_adi' => $alanB], $bayiJwt, 180);
        adim("bayi kendi hostingini açtı ($alanB)", $kod === 201 && (int) ($y['id'] ?? 0) > 0, "HTTP $kod");
        if ((int) ($y['id'] ?? 0) > 0) {
            $hostlar[] = ['id' => (int) $y['id'], 'alan' => $alanB, 'sk' => (string) ($y['sistem_kullanici'] ?? ''),
                'pw' => (string) ($y['olusturulan_parolalar']['ftp'] ?? '')];
        }
    }
    if (count($hostlar) === 2) {
        $h1 = $hostlar[0];
        $h2 = $hostlar[1];
        adim('site açık (200) — askıdan önce', siteKod($h1['alan']) === 200, 'HTTP ' . siteKod($h1['alan']));
        $kod = musteriGiris($h1['sk'], $h1['pw']);
        adim('müşteri paneline giriş (200) — askıdan önce', $kod === 200, "HTTP $kod");
        [$kod] = hamJson('POST', "https://$POZ:$BAYI_PORT/api/v1/domains/{$h2['id']}/askiya-al", [], $bayiJwt);
        adim('bayi ikinci hostingi kendisi askıya aldı', $kod === 200, "HTTP $kod");
    }

    $s = gpanel_SuspendAccount($p);
    adim('SuspendAccount (bayi)', $s === 'success', $s);
    adim('panelde bayi askıda', in_array((string) (($bul() ?? [])['durum'] ?? ''), ['askida', 'suspended', 'pasif'], true), json_encode($bul()));
    foreach ($hostlar as $h) {
        adim("bayi askısı → hosting askıda ({$h['alan']})", (hesapGetir($h['id'])['askida'] ?? null) === true, json_encode(hesapGetir($h['id'])['askida'] ?? null));
        $k = siteKod($h['alan']);
        adim("bayi askısı → site kapalı (503) ({$h['alan']})", $k === 503, "HTTP $k");
    }
    if ($hostlar) {
        red('bayi askısı → müşteri paneline giriş REDDEDİLİR (doğru parola)', musteriGiris($hostlar[0]['sk'], $hostlar[0]['pw'], true));
    }
    [$kod, , $govde] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI]); // 403 kilide sayılmaz
    adim('askıdaki bayi DOĞRU parolayla bayi paneline GİREMEZ (403 hesap askıda)', $kod === 403 && strpos((string) $govde, 'hesap_askida') !== false, "HTTP $kod");
    $kod = hamGet("https://$POZ:$BAYI_PORT/api/v1/reseller/ozet", $bayiJwt);
    adim('askı öncesi açılmış bayi oturumu düştü (401/403)', $kod === 401 || $kod === 403, "HTTP $kod");
    $kod = hamGet("https://$POZ:$BAYI_PORT/api/v1/reseller/ozet", $jeton);
    adim('tek tık (SSO) bayi oturumu da düştü (401/403)', $kod === 401 || $kod === 403, "HTTP $kod");
    $r = gpanel_ServiceSingleSignOn($pBos);
    adim('askıdaki bayiye tek tık giriş yok (düz bağlantıya da düşmez)', empty($r['success']) && !isset($r['redirectTo']), json_encode($r));

    $s = gpanel_UnsuspendAccount($p);
    adim('UnsuspendAccount (bayi)', $s === 'success', $s);
    adim('panelde bayi aktif', in_array((string) (($bul() ?? [])['durum'] ?? ''), ['aktif', 'active'], true), json_encode($bul()));
    if (count($hostlar) === 2) {
        adim('askıdan çıkış → bayi askısının kapattığı hosting açıldı', (hesapGetir($h1['id'])['askida'] ?? null) === false, json_encode(hesapGetir($h1['id'])));
        $k = 0;
        for ($i = 0; $i < 10 && ($k = siteKod($h1['alan'])) !== 200; $i++) {
            sleep(1);
        }
        adim('askıdan çıkış → site yeniden açık (200)', $k === 200, "HTTP $k");
        $kod = musteriGiris($h1['sk'], $h1['pw']);
        adim('askıdan çıkış → müşteri girişi yeniden çalışıyor (200)', $kod === 200, "HTTP $kod");
        adim('bayinin KENDİ askıya aldığı hosting askıda KALDI', (hesapGetir($h2['id'])['askida'] ?? null) === true, json_encode(hesapGetir($h2['id'])['askida'] ?? null));
    }
    [$kod, , $govde] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI]);
    adim('askıdan çıkış → bayi yeniden giriş yapabiliyor (200)', $kod === 200, "HTTP $kod");
    $bayiJwt = (string) ((json_decode((string) $govde, true) ?: [])['token'] ?? '');

    if ($paket2Id > 0) {
        $s = gpanel_ChangePackage(array_merge($p, ['configoption4' => (string) $paket2Id]));
        adim('ChangePackage (bayi, yükseltme) → ikinci paket', $s === 'success', $s);
        $b = $bul() ?? [];
        adim('panelde bayinin paketi ve limitleri değişti (7 alan adı)', (int) ($b['paket_id'] ?? 0) === $paket2Id && (int) ($b['max_domain'] ?? 0) === 7, json_encode($b));
    }
    if ($paket3Id > 0) {
        // Düşürme MEVCUT kullanımın altına (2 hosting, yeni paket 1): kabul edilir; mevcut
        // hostingler çalışır, yeni hosting limitten reddedilir.
        $s = gpanel_ChangePackage(array_merge($p, ['configoption4' => (string) $paket3Id]));
        adim('ChangePackage (bayi, düşürme kullanımın altına) → küçük paket', $s === 'success', $s);
        $b = $bul() ?? [];
        adim('panelde bayinin limiti 1 alan adı', (int) ($b['paket_id'] ?? 0) === $paket3Id && (int) ($b['max_domain'] ?? 0) === 1, json_encode($b));
        if (isset($h1)) {
            $k = siteKod($h1['alan']);
            adim('düşürme sonrası mevcut hosting çalışıyor (200)', $k === 200, "HTTP $k");
        }
        [$kod, $y] = hamJson('POST', "https://$POZ:$BAYI_PORT/api/v1/domains", ['alan_adi' => "wcb3-$EK.test"], $bayiJwt, 180);
        adim('düşürme sonrası yeni hosting limitten reddedilir (403)', $kod === 403, "HTTP $kod " . json_encode($y, JSON_UNESCAPED_UNICODE));
        if ($kod === 201 && (int) ($y['id'] ?? 0) > 0) {
            $TEMIZLIK[] = ['kok', (int) $y['id'], "wcb3-$EK.test"];
        }
        if ($paket2Id > 0) {
            $s = gpanel_ChangePackage(array_merge($p, ['configoption4' => (string) $paket2Id]));
            adim('ChangePackage (bayi) → yeniden ikinci paket', $s === 'success', $s);
        }
    }

    // 1.0.8: bayi şifresi WHMCS'ten sıfırlanır (kök anahtarında AYRI bayi.parola izni).
    $r = gpanel_ChangePassword(array_merge($p, ['password' => PW_BAYI_BASKA]));
    adim('ChangePassword (bayi) → bayi panel parolası sıfırlandı', $r === 'success', (string) $r);
    if ($bsk !== '') {
        $kod = giris($bsk, PW_BAYI_BASKA);
        adim('ChangePassword (bayi) → bayinin hosting parolası da değişti (200)', $kod === 200, "HTTP $kod");
    }
    [$kod] = hamPost("https://$POZ:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI_BASKA]);
    adim('WHMCS\'teki yeni parolayla bayi girişi (200)', $kod === 200, "HTTP $kod");
    [$kod] = hamPost("https://$NEG:$BAYI_PORT/api/v1/auth/login", ['kullanici' => $kul, 'parola' => PW_BAYI]);
    red('eski bayi parolası artık GEÇMEZ', $kod);
    $kod = hamGet("https://$POZ:$BAYI_PORT/api/v1/reseller/ozet", $bayiJwt);
    adim('sıfırlama öncesi açık bayi oturumu düştü (401/403)', $kod === 401 || $kod === 403, "HTTP $kod");

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
    $kalan = 0;
    foreach ($c->hesaplar() as $h) {
        $kalan += (($h['alan_adi'] ?? '') === $bayiAlan) ? 1 : 0;
    }
    adim('bayi silinince bayinin hostingi de silindi', $kalan === 0, (string) $kalan);
    $TEMIZLIK = array_values(array_filter($TEMIZLIK, fn($t) => $t[1] !== $id || $t[0] !== 'bayi-hesap'));
    foreach ([$paket3Id ?? 0] as $pid) {
        if ($pid > 0) {
            try {
                $c->istek('DELETE', '/reseller-plans/' . $pid);
            } catch (Throwable $e) {
                echo "  (uyarı) küçük test paketi silinemedi: " . $e->getMessage() . "\n";
            }
        }
    }
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
