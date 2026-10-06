<?php
/**
 * GirginOSPanel WHMCS modulu — saf PHP test kosucusu (PHPUnit gerekmez).
 *   php tests/run.php            (modul: ../modules/servers/gpanel)
 *   GPANEL_MODUL=/yol php tests/run.php   (mutasyon testi: degistirilmis kopya)
 */

declare(strict_types=1);

error_reporting(E_ALL);
set_error_handler(function ($no, $str, $file, $line) {
    throw new ErrorException($str, 0, $no, $file, $line);
});

define('PW_YENI', 'Yeni' . 'Parola' . '12345');
define('PW_YENI2', 'Yeni' . 'Gizli' . 'Parola99');
define('PW_WHMCS', 'Whmcs' . 'Pass' . '2026xx');
define('PW_WHMCS2', 'Whmcs' . 'Parola' . '2026Zz');
define('PW_FARKLI', 'Farkli' . '1234567890');
define('PW_X', 'abcd' . '1234efgh');
$MODUL = getenv('GPANEL_MODUL') ?: dirname(__DIR__) . '/modules/servers/gpanel';
$TESTLER = [];
$HATALAR = [];
$SAYI = 0;

require __DIR__ . '/stub_global.php';
require __DIR__ . '/stub_capsule.php';
require __DIR__ . '/FakePanel.php';
require $MODUL . '/gpanel.php';

use GirginOSPanel\WHMCS\ApiHatasi;
use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Kayit;
use GirginOSPanel\WHMCS\Yardimci;
use WHMCS\Database\Capsule;

function test(string $ad, callable $f): void
{
    $GLOBALS['TESTLER'][$ad] = $f;
}

function dogru($v, string $m = ''): void
{
    $GLOBALS['SAYI']++;
    if (!$v) {
        throw new RuntimeException('dogru bekleniyordu. ' . $m);
    }
}

function esit($beklenen, $gercek, string $m = ''): void
{
    $GLOBALS['SAYI']++;
    if ($beklenen !== $gercek) {
        throw new RuntimeException('beklenen=' . var_export($beklenen, true) . ' gercek=' . var_export($gercek, true) . ' ' . $m);
    }
}

function icerir(string $kume, string $parca, string $m = ''): void
{
    $GLOBALS['SAYI']++;
    if (strpos($kume, $parca) === false) {
        throw new RuntimeException("'" . $parca . "' bulunamadi: " . substr($kume, 0, 300) . ' ' . $m);
    }
}

function icermez(string $kume, string $parca, string $m = ''): void
{
    $GLOBALS['SAYI']++;
    if ($parca !== '' && strpos($kume, $parca) !== false) {
        throw new RuntimeException("'" . $parca . "' SIZDI: " . substr($kume, max(0, strpos($kume, $parca) - 60), 200) . ' ' . $m);
    }
}

/** Her testte taze ortam. */
function yeni(): FakePanel
{
    Capsule::sifirla();
    Kayit::sifirla();
    $GLOBALS['T'] = ['log' => [], 'localapi' => [], 'activity' => [], 'check_token' => 0];
    $_POST = [];
    $_SERVER['REQUEST_METHOD'] = 'GET';
    $p = new FakePanel();
    ApiIstemci::$varsayilanTasiyici = $p->tasiyici();
    return $p;
}

function params(array $ek = []): array
{
    return $ek + [
        'serverid' => 5,
        'serverhostname' => 'panel.example.test', 'serverip' => '', 'serverport' => '8443', 'serversecure' => 'on',
        'serverusername' => 'root', 'serverpassword' => FakePanel::ANAHTAR, 'serveraccesshash' => '',
        'serviceid' => 11, 'domain' => 'musteri.com', 'username' => 'musteri', 'password' => PW_WHMCS,
        'configoption1' => 'hosting', 'configoption2' => 'Başlangıç', 'configoption3' => '', 'configoption4' => 'Bayi Gümüş',
        'configoption5' => '', 'configoption6' => '', 'configoption7' => '', 'configoption8' => '',
        'clientsdetails' => ['fullname' => 'Ali Veli', 'language' => 'turkish'],
    ];
}

function kayitVar(int $sid = 11): ?array
{
    return Kayit::oku($sid);
}

function tumGunluk(): string
{
    return json_encode($GLOBALS['T']['log'], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES) . json_encode($GLOBALS['T']['activity']);
}

foreach (glob(__DIR__ . '/cases/*.php') as $dosya) {
    require $dosya;
}

$basarisiz = 0;
$filtre = $argv[1] ?? '';
foreach ($TESTLER as $ad => $f) {
    if ($filtre !== '' && strpos($ad, $filtre) === false) {
        continue;
    }
    try {
        $f();
        echo "  ok    $ad\n";
    } catch (Throwable $e) {
        $basarisiz++;
        echo "  FAIL  $ad\n        " . $e->getMessage() . "\n        (" . basename($e->getFile()) . ':' . $e->getLine() . ")\n";
    }
}
echo "\n" . count($TESTLER) . ' test, ' . $SAYI . ' dogrulama, ' . $basarisiz . " basarisiz\n";
exit($basarisiz > 0 ? 1 : 0);
