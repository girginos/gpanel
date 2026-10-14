<?php

use GirginOSPanel\WHMCS\Kayit;

test('sso: admin ve musteri — POST /domains/{id}/sso, redirectTo panel adresi', function () {
    $d = bagliHosting($p);
    foreach (['gpanel_ServiceSingleSignOn', 'gpanel_AdminSingleSignOn'] as $f) {
        $r = $f(params());
        esit(true, $r['success'], $f);
        esit('https://panel.example.test:8443/adina#kod=SSOKOD-ONCE-1234567890', $r['redirectTo']);
    }
    esit(2, $p->sayac('POST /domains/{id}/sso'));
    esit('/domains/' . $d['id'] . '/sso', $p->istekler[0]['yol']);
});

test('sso: uc yok (404) ya da izinsiz (403) — musteri duz panel baglantisina duser, admin net hata alir', function () {
    foreach (['404', '403'] as $mod) {
        $d = bagliHosting($p);
        $p->ssoModu = $mod;
        $r = gpanel_ServiceSingleSignOn(params());
        esit(true, $r['success'], $mod);
        esit('https://panel.example.test:8443/', $r['redirectTo'], 'duz baglanti, kod YOK');
        $r = gpanel_AdminSingleSignOn(params());
        esit(false, $r['success'], $mod);
        dogru($r['errorMsg'] !== '');
    }
});

test('sso: anahtarda sso izni yoksa 403 + izin grubu mesaji (admin), musteri geri duser', function () {
    $d = bagliHosting($p);
    $p->izinler = ['hesap:yaz'];
    $r = gpanel_AdminSingleSignOn(params());
    esit(false, $r['success']);
    icerir($r['errorMsg'], "'sso' izni");
    $r = gpanel_ServiceSingleSignOn(params());
    esit(true, $r['success']);
    icermez($r['redirectTo'], '#');
});

test('sso: panel adresi urun ayarindan (musteri kapisi) geri dusme adresi olur', function () {
    $d = bagliHosting($p);
    $p->ssoModu = '404';
    $r = gpanel_ServiceSingleSignOn(params(['configoption7' => 'https://musteri.example.test:9443/']));
    esit('https://musteri.example.test:9443/', $r['redirectTo']);
    $r = gpanel_ServiceSingleSignOn(params(['configoption7' => 'javascript:alert(1)']));
    esit(false, $r['success'], 'https olmayan adres reddedilir');
});

test('sso: askida (409) ve hiz siniri (429) geri DUSMEZ, hata gosterir', function () {
    foreach (['409', '429'] as $mod) {
        $d = bagliHosting($p);
        $p->ssoModu = $mod;
        $r = gpanel_ServiceSingleSignOn(params());
        esit(false, $r['success'], $mod);
        dogru(!isset($r['redirectTo']));
    }
    $d = bagliHosting($p);
    $r = gpanel_ServiceSingleSignOn(params());
    esit(true, $r['success']);
    $p->domains[$d['id']]['askida'] = true;
    $r = gpanel_ServiceSingleSignOn(params());
    esit(false, $r['success']);
    icerir($r['errorMsg'], 'hesap askıda');
});

test('sso: panelin dondurdugu adres dogrulanir (http, baska host, userinfo reddedilir)', function () {
    foreach (['url_http', 'url_baska', 'url_user'] as $mod) {
        $d = bagliHosting($p);
        $p->ssoModu = $mod;
        $r = gpanel_ServiceSingleSignOn(params());
        esit(false, $r['success'], $mod);
        dogru(!isset($r['redirectTo']), $mod);
        icermez(json_encode($r), 'SSOKOD');
    }
});

test('sso: Panel adresi girilmisse yonlendirme HER ZAMAN oraya + #kod (panelin bildirdigi ad yok sayilir)', function () {
    foreach (['', 'url_baska', 'url_alt'] as $mod) {
        $d = bagliHosting($p);
        $p->ssoModu = $mod;
        foreach (['gpanel_ServiceSingleSignOn', 'gpanel_AdminSingleSignOn'] as $f) {
            $r = $f(params(['configoption7' => 'https://musteri.example.test:9443/']));
            esit(true, $r['success'], "$mod $f");
            esit('https://musteri.example.test:9443/adina#kod=SSOKOD-ONCE-1234567890', $r['redirectTo'], "$mod $f");
        }
    }
});

test('sso: Panel adresi yolu (/alt/) kokene indirgenir; IPv6 koken; panelin yolu korunur', function () {
    $d = bagliHosting($p);
    $r = gpanel_ServiceSingleSignOn(params(['configoption7' => 'https://musteri.example.test:9443/alt/yol?x=1']));
    esit('https://musteri.example.test:9443/adina#kod=SSOKOD-ONCE-1234567890', $r['redirectTo']);
    $r = gpanel_ServiceSingleSignOn(params(['configoption7' => 'https://[2001:db8::5]:9443/']));
    esit('https://[2001:db8::5]:9443/adina#kod=SSOKOD-ONCE-1234567890', $r['redirectTo']);
});

test('sso: Panel adresi girilmis olsa da panelin http/userinfo/kodsuz adresi reddedilir; gecersiz Panel adresi hata', function () {
    foreach (['url_http', 'url_user', 'url_kodsuz'] as $mod) {
        $d = bagliHosting($p);
        $p->ssoModu = $mod;
        $r = gpanel_ServiceSingleSignOn(params(['configoption7' => 'https://musteri.example.test:9443/']));
        esit(false, $r['success'], $mod);
        dogru(!isset($r['redirectTo']), $mod);
        icermez(json_encode($r), 'SSOKOD');
        $r = gpanel_AdminSingleSignOn(params(['configoption7' => 'https://musteri.example.test:9443/']));
        icerir($r['errorMsg'] ?? '', 'geçerli bir giriş adresi', $mod);
    }
    $d = bagliHosting($p);
    $r = gpanel_AdminSingleSignOn(params(['configoption7' => 'http://musteri.example.test:9443/']));
    esit(false, $r['success']);
    icerir($r['errorMsg'], 'Panel adresi');
    icermez(json_encode($r), 'SSOKOD');
});

test('sso: ana bilgisayar adi "_" donen panel + Panel adresi YOK -> reddedilir, mesaj Panel adresi ayarini onerir', function () {
    $d = bagliHosting($p);
    $p->ssoModu = 'url_alt';
    $r = gpanel_AdminSingleSignOn(params());
    esit(false, $r['success']);
    icerir($r['errorMsg'], "'Panel adresi'");
    icermez(json_encode($r), 'SSOKOD');
});

test('sso: bagli olmayan hizmet hata; ESKI panelde (bayi SSO ucu yok) bayi musterisi duz baglanti (panel adresi varsa)', function () {
    $p = yeni();
    $r = gpanel_ServiceSingleSignOn(params());
    esit(false, $r['success']);
    icerir($r['errorMsg'], 'bağlı değil');
    $b = bagliBayi($p);
    $p->bayiSsoModu = '404';
    $pr = params(['configoption1' => 'bayi']);
    $r = gpanel_ServiceSingleSignOn($pr);
    esit(false, $r['success'], 'bayi icin adres girilmemis');
    $r = gpanel_ServiceSingleSignOn($pr + ['configoption7' => 'https://bayi.example.test:9444/']);
    $pr['configoption7'] = 'https://bayi.example.test:9444/';
    $r = gpanel_ServiceSingleSignOn($pr);
    esit(true, $r['success']);
    esit('https://bayi.example.test:9444/', $r['redirectTo']);
    esit(0, $p->sayac('POST /domains/{id}/sso'));
    dogru($p->sayac('POST /resellers/{id}/sso') >= 1, 'once bayi SSO ucu denenir');
});

test('admin link: Sunucular sayfasi formu HTML kacisli', function () {
    $h = gpanel_AdminLink(params(['serverhostname' => 'panel.example.test']));
    icerir($h, 'https://panel.example.test:8443/');
    esit('', gpanel_AdminLink(params(['serverhostname' => '"><script>'])));
});
