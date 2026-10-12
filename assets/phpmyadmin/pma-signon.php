<?php
/**
 * phpMyAdmin signon endpoint
 * Panel kısa-ömürlü token üretir, kullanıcı /pma-signon.php?t=<token> URL'ine yönlendirilir,
 * bu script token'i panel API'sine sorar (X-Internal-Auth header ile), DB credentials döner,
 * session'a yazar ve phpMyAdmin'e yönlendirir.
 */
declare(strict_types=1);
session_name('pma_signon');
ini_set('session.cookie_path', '/');
// 🔴 Oturum sertlestirme (session fixation + cerez hirsizligi savunmasi):
//  - use_strict_mode: PHP, deposunda OLMAYAN (saldirganin uydurdugu) oturum
//    ID'lerini reddeder -> fixation'in birinci kapisi burada kapanir.
//  - httponly: XSS cerezi okuyamaz; samesite=Lax: cross-site istek cerez
//    tasimaz (panelden gelen top-level redirect calismaya devam eder).
//  - secure: yalniz HTTPS'te acilir (duz-HTTP kurulum kirilmasin diye kosullu).
ini_set('session.use_strict_mode', '1');
ini_set('session.cookie_httponly', '1');
ini_set('session.cookie_samesite', 'Lax');
if (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') {
    ini_set('session.cookie_secure', '1');
}
session_start();

if (empty($_GET['t'])) {
    // phpMyAdmin kimlik dogrulamasi BASARISIZ olunca SignonURL'e (buraya) TOKENSIZ
    // doner ve nedenini bu oturuma PMA_single_signon_error_message olarak yazar.
    // Eskiden burada her durumda "Token eksik" yaziyordu: gercek neden (or. "MySQL
    // sunucusuna oturum acilamiyor" = panel kaydindaki parola MariaDB'dekiyle
    // uyusmuyor) kullaniciya hic ulasmiyordu (2026-10-12, .180).
    $pmaHata = isset($_SESSION['PMA_single_signon_error_message'])
        ? trim((string)$_SESSION['PMA_single_signon_error_message']) : '';
    if ($pmaHata !== '') {
        // Basarisiz kimlik bilgileri (parola dahil) oturumda BEKLETILMEZ.
        $_SESSION = [];
        session_destroy();
        http_response_code(401);
        header('Content-Type: text/html; charset=utf-8');
        header('Cache-Control: no-store');
        echo '<!doctype html><html lang="tr"><head><meta charset="utf-8"><title>phpMyAdmin</title></head><body>'
            . '<h3>phpMyAdmin veritaban&#305;na giri&#351; yapamad&#305;</h3>'
            . '<p>Sunucu yan&#305;t&#305;: <b>' . htmlspecialchars($pmaHata, ENT_QUOTES, 'UTF-8') . '</b></p>'
            . '<p>Panelde kay&#305;tl&#305; veritaban&#305; parolas&#305; MySQL\'deki parolayla uyu&#351;muyor olabilir '
            . '(parola panel d&#305;&#351;&#305;ndan de&#287;i&#351;tirilmi&#351;). Panelde veritaban&#305; sayfas&#305;na d&#246;n&#252;p '
            . 'phpMyAdmin\'i yeniden a&#231;&#305;n: panel nedeni ve &#231;&#246;z&#252;m&#252; g&#246;sterir.</p>'
            . '</body></html>';
        exit;
    }
    http_response_code(400);
    die('Token eksik. Panel uzerinden gecis yapin.');
}
$token = (string)$_GET['t'];
if (!preg_match('/^[a-f0-9]{16,128}$/', $token)) {
    http_response_code(400);
    die('Token formati gecersiz.');
}

$internalToken = trim((string)@file_get_contents('/etc/girginospanel/pma-internal.token'));
if ($internalToken === '') {
    http_response_code(500);
    die('PMA internal token sunucuda yok.');
}

$ch = curl_init('http://127.0.0.1:8080/api/v1/internal/pma-redeem');
curl_setopt_array($ch, [
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_POST           => true,
    CURLOPT_POSTFIELDS     => json_encode(['token' => $token]),
    CURLOPT_HTTPHEADER     => [
        'Content-Type: application/json',
        'X-Internal-Auth: ' . $internalToken,
    ],
    CURLOPT_CONNECTTIMEOUT => 3,
    CURLOPT_TIMEOUT        => 5,
]);
$resp = curl_exec($ch);
$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
curl_close($ch);

if ($code !== 200 || !$resp) {
    http_response_code(401);
    die('Token bozulamadi (kod ' . (int)$code . '). Panel uzerinden yeniden deneyin.');
}
$data = json_decode($resp, true);
if (!is_array($data) || empty($data['kullanici'])) {
    http_response_code(500);
    die('Sunucudan beklenmedik yanit.');
}

// 🔴 SESSION FIXATION savunmasinin ikinci kapisi: kimlik dogrulama
// BASARILI -- kimlik bilgileri yazilmadan ONCE oturum ID'si yenilenir, eski
// oturum dosyasi SILINIR (true). Giris oncesi kurbana cerez sabitleyen bir
// saldirgan, bildigi eski ID ile DB kullanici/parola tasiyan oturumu alamaz.
session_regenerate_id(true);

$_SESSION['PMA_single_signon_user']     = $data['kullanici'];
$_SESSION['PMA_single_signon_password'] = $data['parola'];
$_SESSION['PMA_single_signon_host'] = 'localhost';
$_SESSION['PMA_single_signon_only_db']  = [$data['db']];
session_write_close();

// 🔴 Kullaniciyi SUNUCU KOKUNE degil, kendi VERITABANINA birak.
// Eskiden '/pma/' ile kok sayfaya dusuluyordu: phpMyAdmin orada SUNUCU
// kapsaminda acilir, "Disa aktar" sekmesi de sunucu kapsamli calisir ve dosyayi
// $cfg['Export']['file_template_server'] = '@SERVER@' ile adlandirir -> her
// export "localhost.sql" olur, hangi veritabani oldugu belli olmaz.
// Veritabani kapsaminda acilinca sablon '@DATABASE@' olur -> "<db_adi>.sql".
// (only_db zaten tek DB'ye kilitliyordu; sorun yetkide degil, ACILIS KAPSAMINDA.)
$hedef = '/pma/';
if (!empty($data['db'])) {
    // phpMyAdmin 5.x rota bicimi.
    $hedef = '/pma/index.php?route=/database/structure&db=' . rawurlencode($data['db']);
}
header('Location: ' . $hedef, true, 302);
exit;
