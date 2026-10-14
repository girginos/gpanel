<?php

use GirginOSPanel\WHMCS\Kayit;

function bayiSiparisi(array $ek = []): array
{
    return params($ek + ['configoption1' => 'bayi', 'configoption2' => '', 'configoption4' => 'Bayi Gümüş', 'domain' => 'bayisite.com',
        'username' => 'bayi01', 'password' => PW_WHMCS]);
}

test('bayi hosting: domainli bayi siparisi bayiyi VE bayiye ait hostingi acar, parola WHMCS parolasi', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    esit(1, $p->sayac('POST /resellers'));
    esit(1, $p->sayac('POST /domains'));
    $k = Kayit::oku(11);
    esit('bayi', $k['tur'], 'hizmet BAYİYE bagli kalir');
    $post = array_values(array_filter($p->istekler, fn($i) => $i['anahtar'] === 'POST /domains'))[0];
    esit(['alan_adi' => 'bayisite.com', 'bayi_id' => $k['panel_id']], $post['govde']);
    $d = array_values(array_filter($p->domains, fn($d) => $d['alan_adi'] === 'bayisite.com'))[0];
    esit($k['panel_id'], $d['reseller_id'], 'hosting bayiye ait');
    esit(PW_WHMCS, $d['_ftp'], 'hosting parolasi WHMCS parolasi');
    $guncel = array_values(array_filter($GLOBALS['T']['localapi'], fn($c) => $c[0] === 'UpdateClientProduct'));
    dogru(!array_filter($guncel, fn($c) => isset($c[1]['serviceusername'])), 'WHMCS kullanici adi (bayi adi) DEGISMEZ');
});

test('bayi hosting: alan adi bossa yalniz bayi; yeniden denemede ikinci hosting yok; eksik hosting tamamlanir', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(bayiSiparisi(['domain' => ''])));
    esit(0, $p->sayac('POST /domains'));
    $p = yeni();
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    esit(1, $p->sayac('POST /resellers'));
    esit(1, $p->sayac('POST /domains'), 'ikinci hosting acilmaz');
    // Ilk denemede hosting dusmus olsun: yeniden deneme yalniz hostingi acar.
    $p = yeni();
    $p->zorla['POST /domains'] = [503, '{"hata":"gecici"}'];
    dogru(gpanel_CreateAccount(bayiSiparisi()) !== 'success');
    unset($p->zorla['POST /domains']);
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    esit(1, $p->sayac('POST /resellers'), 'bayi tekrar acilmaz');
    esit(2, $p->sayac('POST /domains'));
});

test('bayi hosting: alan adi baska hesaba aitse acik hata; bayi:yaz yoksa panel reddi', function () {
    $p = yeni();
    $p->domainEkle('bayisite.com');
    $s = gpanel_CreateAccount(bayiSiparisi());
    icerir($s, 'başka bir hesaba ait');
    $p = yeni();
    $p->izinler = ['hesap:yaz', 'plan:yaz', 'bayi:oku', 'sso:yaz'];
    dogru(gpanel_CreateAccount(bayiSiparisi()) !== 'success');
});

test('bayi hosting: musteri alani iki giris dugmesi; hosting SSO musteri kapisina (gp_hedef=hosting)', function () {
    $p = yeni();
    $p->kapiPortlar = ['musteri' => 9443, 'bayi' => 9444];
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    $v = gpanel_ClientArea(bayiSiparisi())['vars'];
    esit('bayisite.com', $v['bayi_hosting']['alan']);
    $t = file_get_contents($GLOBALS['MODUL'] . '/templates/clientarea.tpl');
    icerir($t, '&amp;dosinglesignon=1&amp;gp_hedef=hosting');
    icerir($t, '{$L.hosting_giris|escape}');
    try {
        $_GET['gp_hedef'] = 'hosting';
        // Urunde Panel adresi BAYI kapisi olsa da hosting girisi musteri kapisinin adresine gider.
        $r = gpanel_ServiceSingleSignOn(bayiSiparisi(['configoption7' => 'https://panel.example.test:9444/']));
        esit(true, $r['success'], json_encode($r));
        esit('https://panel.example.test:8443/adina#kod=SSOKOD-ONCE-1234567890', $r['redirectTo']);
        esit(1, $p->sayac('POST /domains/{id}/sso'));
        esit(0, $p->sayac('POST /resellers/{id}/sso'));
    } finally {
        unset($_GET['gp_hedef']);
    }
    // Hostingi olmayan bayi hizmeti: acik hata.
    $p = yeni();
    esit('success', gpanel_CreateAccount(bayiSiparisi(['domain' => ''])));
    try {
        $_GET['gp_hedef'] = 'hosting';
        $r = gpanel_ServiceSingleSignOn(bayiSiparisi(['domain' => '']));
        esit(false, $r['success']);
        icerir($r['errorMsg'], 'hosting hesabı yok');
    } finally {
        unset($_GET['gp_hedef']);
    }
});

test('bayi hosting: sifre degisikligi bayi + hosting; paket degisiminde urun plani hostinge', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(bayiSiparisi()));
    $bid = Kayit::oku(11)['panel_id'];
    esit('success', gpanel_ChangePassword(bayiSiparisi(['password' => PW_YENI])));
    esit(PW_YENI, $p->bayiler[$bid]['_parola']);
    $d = array_values(array_filter($p->domains, fn($d) => $d['alan_adi'] === 'bayisite.com'))[0];
    esit(PW_YENI, $d['_ftp'], 'hosting parolasi da degisti');
    // ftp.parola izni yoksa: bayi degisir, hosting icin acik hata
    $p->izinler = ['hesap:yaz', 'plan:yaz', 'bayi:yaz', 'sso:yaz', 'bayi.parola:yaz'];
    icerir(gpanel_ChangePassword(bayiSiparisi(['password' => PW_YENI])), 'hosting parolası değiştirilemedi');
    $p->izinler[] = 'ftp.parola:yaz';
    esit('success', gpanel_ChangePackage(bayiSiparisi(['configoption2' => '2'])));
    $d = array_values(array_filter($p->domains, fn($d) => $d['alan_adi'] === 'bayisite.com'))[0];
    esit(2, (int) $d['plan_id'], 'urunun plani bayinin hostingine uygulandi');
    esit('success', gpanel_ChangePackage(bayiSiparisi(['configoption2' => ''])));
});
