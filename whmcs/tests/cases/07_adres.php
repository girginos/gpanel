<?php

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Yardimci;

test('adres: hostPortAyir — alan, alan:PORT, [v6]:PORT, URL; gecersizler reddedilir', function () {
    $tablo = [
        ['panel.example.test', ['panel.example.test', 0]],
        ['panel.example.test:30409', ['panel.example.test', 30409]],
        [' panel.example.test:30409/ ', ['panel.example.test', 30409]],
        ['https://panel.example.test:30409/', ['panel.example.test', 30409]],
        ['https://panel.example.test/', ['panel.example.test', 0]],
        ['203.0.113.10:9443', ['203.0.113.10', 9443]],
        ['[2001:db8::5]:9443', ['2001:db8::5', 9443]],
        ['[2001:db8::5]', ['2001:db8::5', 0]],
        ['2001:db8::5', ['2001:db8::5', 0]],
        ['panel.example.test:0', ['', 0]],
        ['panel.example.test:99999', ['', 0]],
        ['panel.example.test:abc', ['', 0]],
        ['https://user:pw@panel.example.test:30409/', ['', 0]],
        ['', ['', 0]],
    ];
    foreach ($tablo as [$girdi, $bek]) {
        esit($bek, Yardimci::hostPortAyir($girdi), $girdi);
    }
});

test('adres: erisimSecenekleri — port=N ve tls-dogrulama-kapali, ayiraclar, gecersiz port yok sayilir', function () {
    esit(['port' => 30409, 'tlsKapali' => false], Yardimci::erisimSecenekleri('port=30409'));
    esit(['port' => 30409, 'tlsKapali' => true], Yardimci::erisimSecenekleri("tls-dogrulama-kapali\nPORT=30409"));
    esit(['port' => 9443, 'tlsKapali' => true], Yardimci::erisimSecenekleri('port=9443, tls-dogrulama-kapali'));
    esit(['port' => 0, 'tlsKapali' => false], Yardimci::erisimSecenekleri('port=99999'));
    esit(['port' => 0, 'tlsKapali' => false], Yardimci::erisimSecenekleri(''));
    esit(['port' => 0, 'tlsKapali' => true], Yardimci::erisimSecenekleri('tls-dogrulama-kapali'));
});

test('adres: oncelik — Hostname portu > Access Hash portu > WHMCS Port > 8443', function () {
    $b = ['serverhostname' => 'panel.example.test', 'serverip' => '', 'serverport' => '', 'serveraccesshash' => ''];
    esit(['host' => 'panel.example.test', 'port' => 8443, 'kaynak' => 'varsayilan'], Yardimci::sunucuAdresi($b));
    esit(30000, Yardimci::sunucuAdresi(['serverport' => '30000'] + $b)['port']);
    esit('whmcs', Yardimci::sunucuAdresi(['serverport' => '30000'] + $b)['kaynak']);
    esit(['host' => 'panel.example.test', 'port' => 31000, 'kaynak' => 'accesshash'],
        Yardimci::sunucuAdresi(['serveraccesshash' => 'port=31000', 'serverport' => '30000'] + $b));
    esit(['host' => 'panel.example.test', 'port' => 32000, 'kaynak' => 'hostname'],
        Yardimci::sunucuAdresi(['serverhostname' => 'panel.example.test:32000', 'serveraccesshash' => 'port=31000', 'serverport' => '30000'] + $b));
    esit('203.0.113.10', Yardimci::sunucuAdresi(['serverhostname' => '', 'serverip' => '203.0.113.10'] + $b)['host']);
});

test('adres: basit ekran — Hostname "alan:PORT" ile istekler o porta gider; SSO/duz baglanti/AdminLink ayni host', function () {
    $p = yeni();
    $pr = params(['serverhostname' => 'panel.example.test:9443', 'serverport' => '8443']);
    $r = gpanel_TestConnection($pr);
    esit(true, $r['success'], (string) $r['error']);
    icerir($p->istekler[0]['url'], 'https://panel.example.test:9443/api/v1/');
    // SSO: panelin dondurdugu adres host'u (panel.example.test) Hostname'deki portlu degerle eslesmeli
    $d = bagliHosting($p);
    $s = gpanel_ServiceSingleSignOn($pr);
    esit(true, $s['success'], json_encode($s));
    icerir(gpanel_AdminLink($pr), 'https://panel.example.test:9443/');
});

test('adres: Access Hash "port=N" ve TLS bayragi birlikte', function () {
    $p = yeni();
    $r = gpanel_TestConnection(params(['serveraccesshash' => "tls-dogrulama-kapali\nport=9555", 'serverport' => '8443']));
    esit(true, $r['success'], (string) $r['error']);
    icerir($p->istekler[0]['url'], 'https://panel.example.test:9555/api/v1/');
    esit(false, $p->istekler[0]['dogrula'], 'TLS dogrulamasi kapali olmali');
    // yalniz port: TLS dogrulamasi ACIK kalir
    $p = yeni();
    gpanel_TestConnection(params(['serveraccesshash' => 'port=9555']));
    esit(true, $p->istekler[0]['dogrula']);
});

test('adres: varsayilan portta baglanti hatasi portun nereye yazilacagini soyler; acik portta ipucu yok', function () {
    $p = yeni();
    $p->ag = true;
    $r = gpanel_TestConnection(params(['serverport' => '']));
    esit(false, $r['success']);
    icerir($r['error'], 'panel.example.test:8443');
    icerir($r['error'], 'panel.example.test:PORT');
    icerir($r['error'], 'port=PORT');
    $p = yeni();
    $p->ag = true;
    $r = gpanel_TestConnection(params(['serverhostname' => 'panel.example.test:9443']));
    icermez($r['error'], 'port=PORT');
});

test('adres: eski kapi — /kapi/adres yeni port donerse baglanti testi uyarir; bos ya da 404 ise basarili', function () {
    $p = yeni();
    $p->kapiAdres = ['adres' => 'https://panel.example.test:30409', 'bitis' => 1790000000];
    $r = gpanel_TestConnection(params());
    esit(false, $r['success']);
    icerir($r['error'], '30409');
    icerir($r['error'], 'ESKİ');
    // ayni port (zaten yeni kapidayiz) -> basarili
    $p = yeni();
    $p->kapiAdres = ['adres' => 'https://panel.example.test:8443'];
    esit(true, gpanel_TestConnection(params())['success']);
    // uc yok (eski panel) -> basarili
    $p = yeni();
    $p->zorla['GET /kapi/adres'] = [404, ['hata' => 'bulunamadı']];
    esit(true, gpanel_TestConnection(params())['success']);
});
