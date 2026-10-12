<?php

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Ceviri;
use GirginOSPanel\WHMCS\Kayit;
use GirginOSPanel\WHMCS\Yardimci;
use WHMCS\Database\Capsule;

/**
 * Dil ortami kurar (yeni()'den SONRA cagrilir: yeni() oturumu/tablolari sifirlar).
 *   $oturum: $_SESSION; $sistem: $CONFIG['Language'] (null = tanimsiz)
 *   $yonetici/$musteri: tbladmins/tblclients satirlari [id => language]
 */
function dilOrtami(array $oturum, ?string $sistem, array $yonetici = [], array $musteri = []): void
{
    $_SESSION = $oturum;
    if ($sistem === null) {
        unset($GLOBALS['CONFIG']['Language']);
    } else {
        $GLOBALS['CONFIG']['Language'] = $sistem;
    }
    Capsule::$tablolar['tbladmins'] = [];
    foreach ($yonetici as $id => $dil) {
        Capsule::$tablolar['tbladmins'][] = ['id' => $id, 'language' => $dil];
    }
    Capsule::$tablolar['tblclients'] = [];
    foreach ($musteri as $id => $dil) {
        Capsule::$tablolar['tblclients'][] = ['id' => $id, 'language' => $dil];
    }
    Ceviri::sifirla();
}

/** Ingilizce yonetici oturumu (sistem varsayilani Turkce kalir: yonetici dili kazanmali). */
function ingilizceYonetici(): void
{
    dilOrtami(['adminid' => 1], 'turkish', [1 => 'english']);
}

/** Varsayilan test ortamina (Turkce sistem, oturum yok) don. */
function dilGeriAl(): void
{
    $_SESSION = [];
    $GLOBALS['CONFIG']['Language'] = 'turkish';
    Ceviri::sifirla();
}

test('dil: cozum sirasi — yonetici > musteri > sistem varsayilani > en', function () {
    try {
        $durumlar = [
            // [oturum, sistem, tbladmins, tblclients, beklenen, aciklama]
            [['adminid' => 1], 'turkish', [1 => 'english'], [], 'en', 'yonetici Ingilizce, sistem Turkce'],
            [['adminid' => 1], 'english', [1 => 'turkish'], [], 'tr', 'yonetici Turkce, sistem Ingilizce'],
            [['adminid' => 1, 'uid' => 7], 'turkish', [1 => 'english'], [7 => 'turkish'], 'en', 'yonetici musteriden once'],
            [['adminid' => 1], 'turkish', [1 => ''], [], 'tr', 'yonetici dili bos -> sistem'],
            [['adminid' => 2], 'english', [1 => 'turkish'], [], 'en', 'yonetici kaydi yok -> sistem'],
            [['uid' => 7], 'turkish', [], [7 => 'english'], 'en', 'musteri Ingilizce, sistem Turkce'],
            [['uid' => 7], 'english', [], [7 => 'turkish'], 'tr', 'musteri Turkce, sistem Ingilizce'],
            [['uid' => 7], 'turkish', [], [7 => ''], 'tr', 'musteri dili bos -> sistem (tr)'],
            [['uid' => 7], 'english', [], [7 => ''], 'en', 'musteri dili bos -> sistem (en)'],
            [[], 'english', [1 => 'turkish'], [7 => 'turkish'], 'en', 'oturum yok (cron) -> sistem'],
            [[], ' Turkish ', [], [], 'tr', 'sistem buyuk harf/bosluk'],
            [[], 'german', [], [], 'en', 'Turkce disi her dil -> en'],
            [[], null, [], [], 'en', 'hicbiri yok -> en'],
            [['adminid' => 0, 'uid' => 0], null, [], [], 'en', 'sifir kimlik -> en'],
        ];
        foreach ($durumlar as [$oturum, $sistem, $yon, $mus, $beklenen, $neden]) {
            yeni();
            dilOrtami($oturum, $sistem, $yon, $mus);
            esit($beklenen, Ceviri::dil(), $neden);
        }
        foreach (['turkish' => 'tr', 'TR' => 'tr', 'tr_TR' => 'tr', 'tr-tr' => 'tr', 'english' => 'en', 'trk' => 'en', '' => 'en'] as $ad => $kod) {
            esit($kod, Ceviri::kodla((string) $ad), 'kodla ' . $ad);
        }
    } finally {
        dilGeriAl();
    }
});

test('dil: istek basina onbellek (tek DB sorgusu); sifirla yeniden cozer; $_SESSION yoksa hata yok', function () {
    try {
        yeni();
        ingilizceYonetici();
        Capsule::$sorgular = [];
        esit('Panel account ID', Ceviri::t('Panel hesap kimliği'));
        esit('not linked', Ceviri::t('bağlı değil'));
        esit('en', Ceviri::dil());
        esit(1, count(array_filter(Capsule::$sorgular, function ($q) {
            return $q[1] === 'tbladmins';
        })), 'yonetici dili istek basina bir kez okunmali');
        Capsule::$tablolar['tbladmins'] = [['id' => 1, 'language' => 'turkish']];
        esit('en', Ceviri::dil(), 'onbellek: ayni istekte degismez');
        Ceviri::sifirla();
        esit('tr', Ceviri::dil(), 'sifirla sonrasi yeniden cozulur');
        // CLI/cron: oturum hic baslamamis
        unset($_SESSION);
        $GLOBALS['CONFIG']['Language'] = 'english';
        Ceviri::sifirla();
        esit('en', Ceviri::dil());
    } finally {
        dilGeriAl();
    }
});

test('dil: t() yer tutuculari; sozlukte olmayan metin Turkce doner; deger icindeki {n} yeniden islenmez', function () {
    try {
        yeni();
        esit("'x.com' alan adı", substr(Ceviri::t("'{0}' alan adı panelde zaten var. Başka bir müşterinin hesabına bağlanmamak için otomatik bağlanmadı; ürün ayarında 'Mevcut hesabı bağla'yı açın ya da hizmette 'Panel hesap kimliği'ni elle girin.", ['x.com']), 0, strlen("'x.com' alan adı")));
        ingilizceYonetici();
        esit('Panel server error (HTTP 503).', Ceviri::t('Panel sunucu hatası (HTTP {0}).', [503]));
        esit('Sözlükte yok {0}', Ceviri::t('Sözlükte yok {0}'), 'bilinmeyen metin Turkce kalir');
        esit("Reseller '{1}' is linked to another WHMCS service.", Ceviri::t("Bayi '{0}' başka bir WHMCS hizmetine bağlı.", ['{1}']), 'enjeksiyon yok');
    } finally {
        dilGeriAl();
    }
});

test('dil: yonetici sekmesi Ingilizce — etiketler, aciklama, durum, etkinlik gunlugu; kacislama korunur', function () {
    try {
        $d = bagliHosting($p);
        ingilizceYonetici();
        $f = gpanel_AdminServicesTabFields(params());
        esit(['Panel account ID', 'Panel link status'], array_keys($f));
        esit('hosting account', $f['Panel link status']);
        icerir($f['Panel account ID'], 'value="' . $d['id'] . '"');
        icerir($f['Panel account ID'], 'Leaving it empty keeps the current link. Enter an existing panel account ID to link it manually (it is verified).');
        icermez($f['Panel account ID'], 'Boş bırakmak');
        // yanlis alan adi -> etkinlik gunlugu Ingilizce, kayit degismez
        $x = $p->domainEkle('baskasi.com');
        $_POST = ['gpanel_panel_id' => (string) $x['id']];
        gpanel_AdminServicesTabFieldsSave(params());
        esit($d['id'], kayitVar()['panel_id']);
        esit("GirginOSPanel: service #11 could not be linked: the panel account's domain does not match the service domain", end($GLOBALS['T']['activity']));
        $_POST = ['gpanel_panel_id' => '12abc'];
        gpanel_AdminServicesTabFieldsSave(params());
        esit('GirginOSPanel: service #11 could not be linked: the panel account ID must be a number', end($GLOBALS['T']['activity']));
        // bagli degil / yarim / bayi
        Kayit::sil(11);
        esit('not linked', gpanel_AdminServicesTabFields(params())['Panel link status']);
        Kayit::yaz(11, 0, 'hosting', 'musteri.com');
        esit('pending creation/incomplete', gpanel_AdminServicesTabFields(params())['Panel link status']);
        Kayit::yaz(11, 4, 'bayi', 'bayi01');
        esit('reseller account', gpanel_AdminServicesTabFields(params())['Panel link status']);
        // XSS: kimlik alani Ingilizcede de kacisli
        Kayit::yaz(11, 5, 'hosting', '"><script>x</script>');
        icermez(implode('', gpanel_AdminServicesTabFields(params())), '<script>');
    } finally {
        dilGeriAl();
    }
});

test('dil: baglanti hatasi + hata eslemesi + urun ayarlari Ingilizce; panel mesaji CEVRILMEZ; sirlar yine yok', function () {
    try {
        $p = yeni();
        $p->ag = true;
        ingilizceYonetici();
        $r = gpanel_TestConnection(params(['serverport' => '']));
        esit(false, $r['success']);
        icerir($r['error'], 'Could not connect to the panel (panel.example.test:8443): Could not resolve host');
        icerir($r['error'], 'Port 8443 was used.');
        icerir($r['error'], '"panel.example.test:PORT"');
        icerir($r['error'], '"port=PORT"');
        icermez($r['error'], 'bağlanılamadı');
        icermez($r['error'], 'kullanıldı');
        icermez($r['error'], FakePanel::ANAHTAR);
        // 401 / izin eksik / eski kapi
        $p = yeni();
        ingilizceYonetici();
        $p->anahtar = 'gosp_YANLISYANLIS_x';
        $r = gpanel_TestConnection(params());
        esit('Invalid API key (or the username in the server settings does not match the key owner). Panel: geçersiz oturum', $r['error'], 'panel mesaji aynen');
        $p = yeni();
        ingilizceYonetici();
        $p->izinler = ['dns:yaz'];
        icerir(gpanel_TestConnection(params())['error'], 'Connected, but the API key is missing permissions: hesap:yaz, plan:oku');
        $p = yeni();
        ingilizceYonetici();
        $p->kapiAdres = ['adres' => 'https://panel.example.test:30409'];
        icerir(gpanel_TestConnection(params())['error'], "gPanel's OLD (transition period) gate; the new admin gate port is 30409.");
        // hata eslemesi
        esit("The API key lacks the 'ftp.parola' permission, or this endpoint is closed to the key. Panel: anahtarın bu işlem için izni yok",
            ApiIstemci::hataMesaji(403, 'anahtarın bu işlem için izni yok', '', 'ftp.parola'));
        esit('Panel validation failed: invalid request.', ApiIstemci::hataMesaji(422, '', '', ''));
        esit('Panel validation failed: alan adı geçersiz: x', ApiIstemci::hataMesaji(400, 'alan adı geçersiz: x', '', ''));
        esit('Unexpected panel response (HTTP 418).', ApiIstemci::hataMesaji(418, '', '', ''));
        // islem duzeyi: bagli olmayan hizmet, plan bulunamadi, SSL zorunlu
        $p = yeni();
        ingilizceYonetici();
        icerir(gpanel_SuspendAccount(params()), 'This service is not linked to a panel account.');
        icerir(gpanel_CreateAccount(params(['configoption2' => '999'])), "Panel plan not found (ID 999). Check the 'Panel plan' field");
        icerir(gpanel_TestConnection(params(['serversecure' => '']))['error'], "The 'Secure' (SSL) box must be ticked");
        // urun ayarlari + MetaData
        $o = gpanel_ConfigOptions();
        esit('Product type', $o['urun_tipi']['FriendlyName']);
        esit(['hosting' => 'Hosting account', 'bayi' => 'Reseller account'], $o['urun_tipi']['Options']);
        icerir($o['tls_dogrulama_kapali']['Description'], 'MITM');
        esit('Link existing account', $o['mevcut_bagla']['FriendlyName']);
        $m = gpanel_MetaData();
        esit('GirginOSPanel', $m['DisplayName'], 'DisplayName cevrilmez');
        esit('Log in to Panel (Admin)', $m['AdminSingleSignOnLabel']);
        esit('The password must be at least 12 characters.', Yardimci::parolaHatasi('Kisa1'));
    } finally {
        dilGeriAl();
    }
});

test('dil: musteri oturumu (uid) Ingilizce; cron (oturum yok) sistem dilini kullanir', function () {
    try {
        $p = yeni();
        dilOrtami(['uid' => 7], 'turkish', [], [7 => 'english']);
        $_SERVER['REQUEST_METHOD'] = 'POST';
        $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_YENI2];
        esit('Passwords do not match.', gpanel_parolaDegistir(params()));
        $_SERVER['REQUEST_METHOD'] = 'GET';
        esit('Invalid request.', gpanel_parolaDegistir(params()));
        // cron: oturum yok, sistem Ingilizce
        $p = yeni();
        dilOrtami([], 'english');
        $p->anahtar = 'gosp_YANLISYANLIS_x';
        Capsule::$tablolar['tblhosting'] = [['id' => 11, 'server' => 5, 'domainstatus' => 'Active']];
        Kayit::yaz(11, 101, 'hosting', 'musteri.com');
        icerir(gpanel_UsageUpdate(params()), 'Invalid API key');
        // ayni ortam Turkce sistemde Turkce
        dilOrtami([], 'turkish');
        icerir(gpanel_UsageUpdate(params()), 'API anahtarı geçersiz');
    } finally {
        dilGeriAl();
    }
});

/**
 * Modul PHP dosyalarindaki Ceviri::t(<sabit dizge>...) cagrilari ve Turkce harfli diger
 * dizge sabitleri. lang/ (musteri sablonu) ve Ceviri.php (sozluk) haric.
 *
 * @return array{t:array<string,string[]>,literalDegil:string[],kacan:string[],dosyaSayisi:array<string,int>}
 */
function ceviriTara(string $modul): array
{
    $sonuc = ['t' => [], 'literalDegil' => [], 'kacan' => [], 'dosyaSayisi' => []];
    $dosyalar = array_merge([$modul . '/gpanel.php'], glob($modul . '/lib/*.php'));
    foreach ($dosyalar as $dosya) {
        if (basename($dosya) === 'Ceviri.php') {
            continue;
        }
        $ad = basename($dosya);
        $sonuc['dosyaSayisi'][$ad] = 0;
        $tk = array_values(array_filter(token_get_all((string) file_get_contents($dosya)), function ($x) {
            return !(is_array($x) && in_array($x[0], [T_WHITESPACE, T_COMMENT, T_DOC_COMMENT], true));
        }));
        $tLiteral = [];
        $n = count($tk);
        for ($i = 0; $i + 3 < $n; $i++) {
            if (is_array($tk[$i]) && $tk[$i][0] === T_STRING && $tk[$i][1] === 'Ceviri'
                && is_array($tk[$i + 1]) && $tk[$i + 1][0] === T_DOUBLE_COLON
                && is_array($tk[$i + 2]) && $tk[$i + 2][0] === T_STRING && $tk[$i + 2][1] === 't'
                && $tk[$i + 3] === '(') {
                $a = $tk[$i + 4] ?? null;
                $sonra = $tk[$i + 5] ?? null;
                if (!is_array($a) || $a[0] !== T_CONSTANT_ENCAPSED_STRING || !in_array($sonra, [',', ')'], true)) {
                    $sonuc['literalDegil'][] = $ad . ':' . $tk[$i][2];
                    continue;
                }
                $metin = eval('return ' . $a[1] . ';');
                $sonuc['t'][$metin][] = $ad . ':' . $a[2];
                $tLiteral[$i + 4] = true;
                $sonuc['dosyaSayisi'][$ad]++;
            }
        }
        foreach ($tk as $j => $x) {
            if (is_array($x) && $x[0] === T_CONSTANT_ENCAPSED_STRING && !isset($tLiteral[$j])
                && preg_match('/[çğıöşüÇĞİÖŞÜ]/u', $x[1])) {
                $sonuc['kacan'][] = $ad . ':' . $x[2] . ' ' . $x[1];
            }
        }
    }
    return $sonuc;
}

test('dil: sozluk kapsamasi — koddaki her Ceviri::t() metni sozlukte, yer tutucular tutarli, kacan Turkce metin yok', function () {
    global $MODUL;
    $s = ceviriTara($MODUL);
    $sozluk = Ceviri::sozluk();
    esit([], $s['literalDegil'], 'Ceviri::t ilk argumani tek sabit dizge olmali (sozluk denetlenebilsin)');
    esit([], $s['kacan'], 'Ceviri::t ile sarilmamis Turkce metin');
    foreach (['gpanel.php', 'ApiIstemci.php', 'Modul.php', 'Yardimci.php'] as $ad) {
        dogru(($s['dosyaSayisi'][$ad] ?? 0) > 0, $ad . ' icinde Ceviri::t bulunamadi (tarayici bozuk?)');
    }
    $eksik = array_values(array_diff(array_keys($s['t']), array_keys($sozluk)));
    esit([], $eksik, 'sozlukte olmayan metinler');
    $olu = array_values(array_diff(array_keys($sozluk), array_keys($s['t'])));
    esit([], $olu, 'kodda kullanilmayan sozluk anahtarlari');
    dogru(count($sozluk) >= 80, 'sozluk beklenenden kucuk: ' . count($sozluk));
    foreach ($sozluk as $tr => $en) {
        preg_match_all('/\{\d+\}/', $tr, $a);
        preg_match_all('/\{\d+\}/', $en, $b);
        $a = array_unique($a[0]);
        $b = array_unique($b[0]);
        sort($a);
        sort($b);
        esit($a, $b, 'yer tutucular: ' . $tr);
        dogru(trim($en) !== '' && $en !== $tr, 'bos/cevrilmemis: ' . $tr);
        dogru(!preg_match('/[çğıöşüÇĞİÖŞÜ]/u', $en), 'Ingilizce metinde Turkce harf: ' . $en);
    }
});
