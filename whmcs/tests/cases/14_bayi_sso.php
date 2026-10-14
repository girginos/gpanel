<?php

use GirginOSPanel\WHMCS\Yardimci;

test('bayi sso: musteri ve yonetici tek tikla bayi paneline girer (POST /resellers/{id}/sso)', function () {
    $b = bagliBayi($p);
    $pr = params(['configoption1' => 'bayi']);
    $r = gpanel_ServiceSingleSignOn($pr);
    esit(true, $r['success'], json_encode($r));
    esit('https://panel.example.test:9444/sso#kod=BAYIKOD-ONCE-1234567890', $r['redirectTo']);
    esit(1, $p->sayac('POST /resellers/{id}/sso'));
    esit(0, $p->sayac('POST /domains/{id}/sso'), 'hosting SSO ucu cagrilmaz');
    esit('/resellers/' . $b['id'] . '/sso', $p->istekler[count($p->istekler) - 1]['yol']);
    $r = gpanel_AdminSingleSignOn($pr);
    esit(true, $r['success']);
    esit(2, $p->sayac('POST /resellers/{id}/sso'));
});

test('bayi sso: gp_hedef=profil -> girisden sonra Profil; baska hedef yok sayilir; yonetici hedefsiz', function () {
    bagliBayi($p);
    $pr = params(['configoption1' => 'bayi']);
    $kod = 'https://panel.example.test:9444/sso#kod=BAYIKOD-ONCE-1234567890';
    try {
        $_GET['gp_hedef'] = 'profil';
        $r = gpanel_ServiceSingleSignOn($pr);
        esit($kod . '&next=%2Fprofil', $r['redirectTo']);
        $r = gpanel_AdminSingleSignOn($pr);
        esit($kod, $r['redirectTo'], 'yonetici SSOsu hedef almaz');
        foreach (['https://evil.example.org/', '//evil', '/api-anahtarlari', 'Profil', ['profil']] as $kotu) {
            $_GET['gp_hedef'] = $kotu;
            $r = gpanel_ServiceSingleSignOn($pr);
            esit($kod, $r['redirectTo'], 'beyaz liste disi hedef: ' . json_encode($kotu));
        }
        // Eski panel: duz baglantiya dusulur, hedef yol olarak eklenir.
        $_GET['gp_hedef'] = 'profil';
        $p->bayiSsoModu = '404';
        $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi', 'configoption7' => 'https://bayi.example.test:9444/']));
        esit(true, $r['success']);
        esit('https://bayi.example.test:9444/profil', $r['redirectTo']);
    } finally {
        unset($_GET['gp_hedef']);
    }
});

test('bayi sso: izin yoksa musteri duz baglanti, yonetici hata; askida 409 hata; yabanci host reddedilir', function () {
    bagliBayi($p);
    $pr = params(['configoption1' => 'bayi', 'configoption7' => 'https://bayi.example.test:9444/']);
    $p->izinler = ['hesap:yaz', 'plan:yaz', 'bayi:oku', 'sso:yaz'];
    $r = gpanel_ServiceSingleSignOn($pr);
    esit(true, $r['success']);
    esit('https://bayi.example.test:9444/', $r['redirectTo'], 'bayi:yaz yok -> duz baglanti');
    $r = gpanel_AdminSingleSignOn($pr);
    esit(false, $r['success']);
    icerir($r['errorMsg'], 'bayi');

    $b = bagliBayi($p);
    $p->bayiler[$b['id']]['durum'] = 'suspended';
    $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi', 'configoption7' => 'https://bayi.example.test:9444/']));
    esit(false, $r['success'], 'askidaki bayi duz baglantiya da dusmez');
    icerir($r['errorMsg'], 'askıda');

    bagliBayi($p);
    $p->bayiSsoModu = 'url_baska';
    $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi']));
    esit(false, $r['success']);
    icermez(json_encode($r), 'BAYIKOD');
});

test('bayi sso: urunde Panel adresi varsa kod o kokene gider (yol ve parca korunur)', function () {
    bagliBayi($p);
    $r = gpanel_ServiceSingleSignOn(params(['configoption1' => 'bayi', 'configoption7' => 'https://bayi.example.net:2083/']));
    esit('https://bayi.example.net:2083/sso#kod=BAYIKOD-ONCE-1234567890', $r['redirectTo']);
});

test('bayi sso: sablon — bayi giris dugmesi tek tik SSO, sifre dugmesi askida gizli; dil anahtarlari', function () {
    $t = file_get_contents($GLOBALS['MODUL'] . '/templates/clientarea.tpl');
    icerir($t, '{if $tur neq \'bayi\' || $hesap}');
    icerir($t, '{if $tur eq \'bayi\'}{$L.bayi_giris|escape}{else}{$L.panele_giris|escape}{/if}');
    icerir($t, '{if $tur eq \'bayi\' && $bayi_hosting}');
    esit('Bayi paneline giriş yap', Yardimci::dil('turkish')['bayi_giris']);
    esit('Log in to the reseller panel', Yardimci::dil('english')['bayi_giris']);
});
