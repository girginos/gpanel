<?php

use GirginOSPanel\WHMCS\Yardimci;

test('php surumu: acik yazimlar kanonik surume, taninmayan oldugu gibi (panel ile ayni tablo)', function () {
    $tablo = [
        '83' => '8.3', '81' => '8.1', '74' => '7.4', 'php83' => '8.3', 'php81' => '8.1', 'PHP81' => '8.1',
        'PHP 8.1' => '8.1', 'php-8.2' => '8.2', 'PHP_7.4' => '7.4', 'php8.4' => '8.4',
        '8.3.31' => '8.3', '8.1.2' => '8.1', 'php8.2.30' => '8.2', ' php 8.5 ' => '8.5', '8.2' => '8.2',
    ];
    foreach ($tablo as $girdi => $beklenen) {
        esit($beklenen, Yardimci::phpSurum((string) $girdi), (string) $girdi);
    }
    foreach (['8.30', 'v8.3', '830', '8', 'php', '-8.3', 'abc', '9.9'] as $v) {
        esit($v, Yardimci::phpSurum($v), "taninmayan deger degismemeli: $v");
    }
    esit('0.8', Yardimci::phpSurum('08.3'), 'bastaki sifir: panel phpMap disi oldugu icin reddeder');
});

test('php surumu: urun ayari acilir liste — bos = plandan, yalniz panel surumleri', function () {
    $o = gpanel_ConfigOptions();
    esit('dropdown', $o['php']['Type']);
    esit(array_merge([''], Yardimci::PHP_SURUMLERI), array_map('strval', array_keys($o['php']['Options'])));
    esit('', $o['php']['Default']);
});

test('php surumu: eski serbest metin degeri (php81) panele kanonik gider', function () {
    foreach (['php81' => '8.1', 'PHP 8.2' => '8.2', '8.4.12' => '8.4', '8.3' => '8.3'] as $kayitli => $beklenen) {
        $p = yeni();
        esit('success', gpanel_CreateAccount(params(['configoption2' => '', 'configoption3' => $kayitli])));
        esit(['alan_adi' => 'musteri.com', 'php_surum' => $beklenen], postDomains($p)[0]['govde'], $kayitli);
    }
    // Taninmayan deger OLDUGU GIBI gider (panel reddeder); modul tahmin etmez.
    $p = yeni();
    gpanel_CreateAccount(params(['configoption2' => '', 'configoption3' => '9.9']));
    esit('9.9', postDomains($p)[0]['govde']['php_surum']);
});
