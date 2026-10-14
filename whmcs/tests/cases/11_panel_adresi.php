<?php

use GirginOSPanel\WHMCS\Kayit;

test('panel adresi: bos ise panelden — bayi urunu BAYI kapisina, hosting MUSTERI kapisina', function () {
    // bayi: SSO ucu yok (eski panel), duz baglanti bayi kapisina
    $p = yeni();
    $p->kapiPortlar = ['musteri' => 9443, 'bayi' => 9444];
    $p->bayiSsoModu = '404';
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi', 'configoption7' => '']));
    dogru($r['success'], json_encode($r));
    esit('https://panel.example.test:9444/', $r['redirectTo'], 'bayi -> bayi kapisi');
    esit('https://panel.example.test:9444/', gpanel_ClientArea(params(['configoption1' => 'bayi', 'configoption7' => '']))['vars']['panel_url']);

    // hosting: SSO ucu yokken duz baglanti MUSTERI kapisina (yonetici 8443 DEGIL)
    $d = bagliHosting($p);
    $p->kapiPortlar = ['musteri' => 9443, 'bayi' => 9444];
    $p->ssoModu = '404';
    $r = gpanel_ServiceSingleSignOn(params(['configoption7' => '']));
    dogru($r['success'], json_encode($r));
    esit('https://panel.example.test:9443/', $r['redirectTo'], 'hosting -> musteri kapisi');
    esit('https://panel.example.test:9443/', gpanel_ClientArea(params(['configoption7' => '']))['vars']['panel_url']);
});

test('panel adresi: urunde acikca girilen adres panelin portlarindan once gelir', function () {
    $p = yeni();
    $p->kapiPortlar = ['musteri' => 9443, 'bayi' => 9444];
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    esit('https://bayi.ornek.test:7000/', gpanel_ClientArea(params(['configoption1' => 'bayi', 'configoption7' => 'https://bayi.ornek.test:7000/']))['vars']['panel_url']);
});

test('panel adresi: eski panel (uc yok) — bayi bos (adres girilmeli), hosting eski davranis', function () {
    $p = yeni();
    $p->bayiSsoModu = '404';
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi', 'configoption7' => '']));
    dogru(!$r['success'] && !isset($r['redirectTo']), json_encode($r));
    esit('', gpanel_ClientArea(params(['configoption1' => 'bayi', 'configoption7' => '']))['vars']['panel_url']);
    $d = bagliHosting($p);
    esit('https://panel.example.test:8443/', gpanel_ClientArea(params(['configoption7' => '']))['vars']['panel_url']);
});
