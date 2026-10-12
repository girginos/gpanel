<?php

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Kayit;
use GirginOSPanel\WHMCS\Yardimci;

/** Tum yasam dongusunu kosturur; tum gunluk/hata/localAPI ciktisini tek dizgede toplar. */
function tamAkis(): array
{
    $p = yeni();
    $cikti = [];
    $pr = params(['password' => PW_WHMCS2]);
    $cikti[] = gpanel_CreateAccount($pr);
    $cikti[] = gpanel_ChangePassword(params(['password' => PW_YENI2]));
    $cikti[] = gpanel_SuspendAccount($pr);
    $cikti[] = gpanel_UnsuspendAccount($pr);
    // SSO donusu redirectTo'yu (kod dahil) WHMCS'e VERMEK zorunda; yalniz log denetlenir.
    gpanel_ServiceSingleSignOn($pr);
    gpanel_AdminSingleSignOn($pr);
    $cikti[] = json_encode(gpanel_TestConnection($pr));
    $cikti[] = gpanel_ChangePackage(params(['configoption2' => '2']));
    $cikti[] = json_encode(gpanel_ClientArea($pr)['vars']['hesap']);
    $p->zorla['POST /domains/{id}/askiya-al'] = [500, '{"hata":"hata"}'];
    $cikti[] = gpanel_SuspendAccount($pr);
    $cikti[] = gpanel_TerminateAccount($pr);
    return [$p, implode("\n", $cikti)];
}

test('maskeleme: log ciktisinda API anahtari, parolalar, panelin urettigi parola ve SSO kodu YOK', function () {
    [$p, $kullaniciCiktisi] = tamAkis();
    $log = tumGunluk();
    dogru(count($GLOBALS['T']['log']) >= 10, 'gunluk bos olmamali (test anlamli olsun)');
    foreach ([FakePanel::ANAHTAR, '0123456789abcdefghijklmnopqrstuvwxyzABCDEFG', PW_WHMCS2, PW_YENI2, 'GenFtpPass#7788Zz', 'DbPass#99887766xx', 'SSOKOD-ONCE-1234567890'] as $sir) {
        icermez($log, $sir, 'gunluk');
        icermez($kullaniciCiktisi, $sir, 'kullaniciya donen metin');
    }
    // replaceVars (WHMCS'in yapacagi ek maskeleme) BOS: sir WHMCS'e bile gitmez
    foreach ($GLOBALS['T']['log'] as $l) {
        esit([], $l['replace']);
    }
    // gunlukte maskeli yer tutucu goruluyor
    icerir($log, '***');
});

test('maskeleme: gizli alan adlari ve gomulu degerler (Yardimci::maskele)', function () {
    $m = Yardimci::maskele([
        'password' => PW_X, 'url' => 'https://h:8443/#kod-12345', 'x' => 'icinde gizli123456 var',
        'olusturulan_parolalar' => ['ftp' => 'F1', 'db' => 'D1'], 'ic' => ['Authorization' => 'gosp root:gosp_ABCDEFGHJKLM_xxxxxxxxxxxxxxxx'],
    ], ['gizli123456']);
    $j = json_encode($m, JSON_UNESCAPED_SLASHES);
    foreach ([PW_X, 'kod-12345', 'gizli123456', '"F1"', '"D1"', 'xxxxxxxxxxxxxxxx'] as $s) {
        icermez($j, $s);
    }
    icerir($j, 'https://h:8443/#***');
    $b = Yardimci::maskele('Authorization: gosp root:gosp_ABCDEFGHJKLM_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG');
    icermez($b, 'abcdefghijklmnop');
    icerir($b, 'gosp root:***');
});

test('maskeleme: hata mesajlarinda ag hatasi metnindeki anahtar temizlenir', function () {
    $p = yeni();
    ApiIstemci::$varsayilanTasiyici = function () {
        return ['durum' => 0, 'govde' => '', 'hata' => 'connect failed Authorization: gosp root:' . FakePanel::ANAHTAR];
    };
    $r = gpanel_TestConnection(params());
    icermez($r['error'], FakePanel::ANAHTAR);
});

test('guvenlik: serviceid/sahiplik istekten OKUNMAZ (kaynakta $_REQUEST/$_GET/$_COOKIE yok; $_POST yalniz parola alanlari)', function () {
    global $MODUL;
    $kaynak = '';
    foreach (array_merge([$MODUL . '/gpanel.php'], glob($MODUL . '/lib/*.php')) as $f) {
        $kaynak .= file_get_contents($f);
    }
    icermez($kaynak, '$_REQUEST');
    icermez($kaynak, '$_GET');
    icermez($kaynak, '$_COOKIE');
    preg_match_all('/\$_POST\[\'([a-z_0-9]+)\'\]/', $kaynak, $m);
    $izinli = ['yeni_parola', 'yeni_parola2', 'gpanel_panel_id'];
    foreach (array_unique($m[1]) as $alan) {
        dogru(in_array($alan, $izinli, true), '$_POST[' . $alan . ']');
    }
    icermez($kaynak, "\$_POST['serviceid']");
});

test('guvenlik: istemci formu baska hizmete bagli id ile calisamaz (POST govdesindeki id/serviceid yok sayilir)', function () {
    $d = bagliHosting($p);
    $d2 = $p->domainEkle('baskasi.com');
    Kayit::yaz(12, $d2['id'], 'hosting', 'baskasi.com');
    $_SERVER['REQUEST_METHOD'] = 'POST';
    $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_YENI, 'id' => 12, 'serviceid' => 12];
    esit('success', gpanel_parolaDegistir(params(['serviceid' => 11])));
    esit(PW_YENI, $p->domains[$d['id']]['_ftp']);
    dogru(!isset($p->domains[$d2['id']]['_ftp']), 'baska hizmetin parolasi degismemeli');
});

test('musteri parola formu: GET reddedilir, token kontrolu cagrilir, eslesmeyen/zayif reddedilir, WHMCS parolasi guncellenir', function () {
    $d = bagliHosting($p);
    $_SERVER['REQUEST_METHOD'] = 'GET';
    icerir(gpanel_parolaDegistir(params()), 'Geçersiz istek');
    esit(0, $GLOBALS['T']['check_token']);
    $_SERVER['REQUEST_METHOD'] = 'POST';
    $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_FARKLI];
    icerir(gpanel_parolaDegistir(params()), 'eşleşmiyor');
    $_POST = ['yeni_parola' => 'kisa1', 'yeni_parola2' => 'kisa1'];
    dogru(gpanel_parolaDegistir(params()) !== 'success');
    esit(0, $p->sayac('PUT /domains/{id}/ftp/password'));
    $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_YENI];
    esit('success', gpanel_parolaDegistir(params()));
    dogru($GLOBALS['T']['check_token'] >= 1);
    esit(PW_YENI, sonLocalApi()['servicepassword']);
    dogru(!isset(sonLocalApi()['serviceusername']), 'username degismemeli');
});

test('sablon: her cikti |escape ile kacislanir, token alani + modop=custom formu var', function () {
    global $MODUL;
    $t = file_get_contents($MODUL . '/templates/clientarea.tpl');
    $govde = preg_replace('/\{\*.*?\*\}/s', '', $t);
    preg_match_all('/\{\$([^}]+)\}/', $govde, $m);
    foreach ($m[1] as $ifade) {
        dogru(strpos($ifade, '|escape') !== false, 'kacissiz cikti: {$' . $ifade . '}');
    }
    icerir($t, 'name="token"');
    icerir($t, 'name="modop" value="custom"');
    icerir($t, 'name="a" value="parolaDegistir"');
    icerir($t, 'dosinglesignon=1');
});

test('musteri alani: bilgi ve kullanim cubuklari; panel hatasi istisna firlatmaz', function () {
    $d = bagliHosting($p);
    $p->domains[$d['id']] = array_merge($p->domains[$d['id']], ['boyut_kb' => 512 * 1024, 'disk_kota_mb' => 1024, 'trafik_kb' => 0, 'trafik_kota_mb' => 0, 'askida' => false]);
    $r = gpanel_ClientArea(params());
    esit('templates/clientarea', $r['templatefile']);
    $v = $r['vars'];
    esit('', $v['hata']);
    esit('musteri.com', $v['hesap']['ad']);
    esit(['ad' => 'disk', 'kullanim' => 512, 'limit' => 1024, 'yuzde' => 50], $v['cubuklar'][0]);
    esit(0, $v['cubuklar'][1]['yuzde']);
    esit('TOKEN123', $v['token']);
    esit(11, $v['serviceid']);
    esit('Hosting hesabı', $v['L']['baslik'], 'turkce metin');
    $v = gpanel_ClientArea(params(['clientsdetails' => ['language' => 'english']]))['vars'];
    esit('Hosting account', $v['L']['baslik']);
    $p->ag = true;
    $v = gpanel_ClientArea(params())['vars'];
    icerir($v['hata'], 'Panele bağlanılamadı');
    esit(null, $v['hesap']);
    icermez(json_encode($v), FakePanel::ANAHTAR);
});

test('musteri alani: bagli olmayan hizmet panele sormaz, hata gosterir', function () {
    $p = yeni();
    $v = gpanel_ClientArea(params())['vars'];
    icerir($v['hata'], 'bağlı değil');
    esit(0, count($p->istekler));
});

test('dil: iki dil dosyasi ayni anahtarlari tasir', function () {
    global $MODUL;
    $tr = include $MODUL . '/lang/turkish.php';
    $en = include $MODUL . '/lang/english.php';
    esit(array_keys($tr), array_keys($en));
});

test('yonetici sekmesi: alanlar kacisli; kaydet dogrular (alan adi uyusmali), baska hizmete bagli reddeder', function () {
    $d = bagliHosting($p);
    $f = gpanel_AdminServicesTabFields(params());
    icerir($f['Panel hesap kimliği'], 'value="' . $d['id'] . '"');
    // yeni, yanlis kimlik: baska alan adi
    $x = $p->domainEkle('baskasi.com');
    $_POST = ['gpanel_panel_id' => (string) $x['id']];
    gpanel_AdminServicesTabFieldsSave(params());
    esit($d['id'], kayitVar()['panel_id'], 'alan adi uyusmadigi icin degismemeli');
    icerir(implode(' ', $GLOBALS['T']['activity']), 'uyuşmuyor');
    // dogru kimlik: ayni alan adli yeni kayit (ornek: panelde yeniden olusturulmus)
    $y = $p->domainEkle('musteri.com');   // ikinci "musteri.com" (test icin)
    $_POST = ['gpanel_panel_id' => (string) $y['id']];
    gpanel_AdminServicesTabFieldsSave(params());
    esit($y['id'], kayitVar()['panel_id']);
    // rakam olmayan
    $_POST = ['gpanel_panel_id' => '12abc'];
    gpanel_AdminServicesTabFieldsSave(params());
    esit($y['id'], kayitVar()['panel_id']);
    // baska hizmete bagli
    Kayit::yaz(12, $d['id'], 'hosting', 'musteri.com');
    $_POST = ['gpanel_panel_id' => (string) $d['id']];
    gpanel_AdminServicesTabFieldsSave(params());
    esit($y['id'], kayitVar()['panel_id'], 'baska hizmete bagli id reddedilmeli');
});

test('yonetici sekmesi: XSS — panel_id alani kacislanir', function () {
    $p = yeni();
    Kayit::yaz(11, 5, 'hosting', '"><script>x</script>');
    $f = gpanel_AdminServicesTabFields(params());
    icermez(implode('', $f), '<script>');
});

test('sozlesme: modulun cagirdigi tum uclar izin.go Rotalar altin dosyasinda (SSO dahil)', function () {
    $golden = dirname(__DIR__, 3) . '/cmd/server/testdata/api_anahtar_uclari.golden';
    if (!is_file($golden)) {
        echo "        (golden yok, atlandi)\n";
        return;
    }
    $satirlar = [];
    $herZaman = [];
    foreach (file($golden, FILE_IGNORE_NEW_LINES) as $s) {
        if (preg_match('#^(\S+) /api/v1(\S+)\s+\(her zaman\)$#', $s, $m)) {
            $herZaman[$m[1] . ' ' . $m[2]] = true;
        }
        // "GET /api/v1/domains  hesap:oku"
        if (preg_match('#^(\S+) /api/v1(\S+)\s+(\S+)$#', $s, $m)) {
            $satirlar[$m[1] . ' ' . preg_replace('/\{[a-z]+\}/', '{id}', $m[2])] = $m[3];
        }
    }
    dogru(count($satirlar) > 20, 'golden okunamadi');
    // Tam yasam dongusu + bayi + sso
    tamAkis();
    $p = yeni();
    $d = $p->domainEkle('musteri.com');
    Kayit::yaz(11, $d['id'], 'hosting', 'musteri.com');
    gpanel_TestConnection(params());
    gpanel_UsageUpdate(params());
    gpanel_TerminateAccount(params());
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    $pb = params(['configoption1' => 'bayi']);
    gpanel_SuspendAccount($pb);
    gpanel_UnsuspendAccount($pb);
    gpanel_ChangePackage($pb);
    gpanel_TerminateAccount($pb);
    $p2 = yeni();
    gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi09']));
    $kullanilan = [];
    foreach ([$p, $p2] as $pp) {
        foreach ($pp->istekler as $i) {
            $kullanilan[$i['anahtar']] = true;
        }
    }
    foreach (array_keys(FakePanel::$rotalar) as $r) {
        $kullanilan[$r] = true;
    }
    // FakePanel.rotalar ile altin dosya birebir ayni grup/erisim
    foreach (FakePanel::$rotalar as $k => [$g, $e]) {
        dogru(isset($satirlar[$k]), "altin dosyada yok: $k");
        esit($g . ':' . $e, $satirlar[$k], $k);
    }
    // FakePanel'in "her zaman acik" uclari altin dosyada da her zaman acik
    foreach (FakePanel::$herZaman as $k) {
        dogru(isset($herZaman[$k]), "altin dosyada (her zaman) degil: $k");
    }
    // modulun gercekten attigi her uc altin dosyada (benzeri) var
    foreach (array_keys($kullanilan) as $k) {
        if ($k === 'POST /domains/{id}/sso' || isset($herZaman[$k])) {
            continue;
        }
        dogru(isset($satirlar[$k]), "modul altin dosyada olmayan uc cagiriyor: $k");
    }
});

test('sozdizimi: tum PHP dosyalari php -l temiz', function () {
    global $MODUL;
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator(dirname($MODUL, 3), FilesystemIterator::SKIP_DOTS));
    $n = 0;
    foreach ($it as $f) {
        if (substr((string) $f, -4) === '.php' && strpos((string) $f, '/tests/') === false) {
            exec('php -l ' . escapeshellarg((string) $f) . ' 2>&1', $o, $rc);
            esit(0, $rc, (string) $f);
            $n++;
        }
    }
    dogru($n >= 8, "php dosya sayisi $n");
});
