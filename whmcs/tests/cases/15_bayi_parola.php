<?php

use GirginOSPanel\WHMCS\Yardimci;

test('bayi parola: yonetici ChangePassword bayi panel parolasini sifirlar (PUT /resellers/{id}/password)', function () {
    $b = bagliBayi($p);
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => PW_YENI]));
    esit('success', $s);
    esit(PW_YENI, $p->bayiler[$b['id']]['_parola']);
    esit(1, $p->sayac('PUT /resellers/{id}/password'));
    $son = array_values(array_filter($p->istekler, fn($i) => $i['anahtar'] === 'PUT /resellers/{id}/password'))[0];
    esit('/resellers/' . $b['id'] . '/password', $son['yol']);
    esit(['parola' => PW_YENI], $son['govde']);
    esit(0, $p->sayac('PUT /resellers/{id}'), 'genel bayi guncelleme ucu kullanilmaz');
    icermez(json_encode($GLOBALS['T']['log']), PW_YENI);
});

test('bayi parola: zayif parola panele gitmez; eski panel (uc yok) acik hata; panel 400 aynen', function () {
    $b = bagliBayi($p);
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => 'kisa1']));
    dogru($s !== 'success');
    esit(0, $p->sayac('PUT /resellers/{id}/password'));
    $p->bayiParolaModu = '404';
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => PW_YENI]));
    icerir($s, 'bayi şifre sıfırlamayı desteklemiyor');
    $p->bayiParolaModu = 'var';
    $p->zorla['PUT /resellers/{id}/password'] = [400, '{"hata":"parola 8-128 karakter olmalı"}'];
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => PW_YENI]));
    icerir($s, 'parola 8-128 karakter olmalı');
    // Kayit silinmis bayi: panel 404 (JSON mesajli) "desteklemiyor" DEGIL, kayit bulunamadi.
    unset($p->zorla['PUT /resellers/{id}/password']);
    unset($p->bayiler[$b['id']]);
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => PW_YENI]));
    icermez($s, 'desteklemiyor');
    icerir($s, 'reseller bulunamadı');
});

test('bayi parola: musteri alani formu bayi parolasini sifirlar ve WHMCS parolasini gunceller', function () {
    $b = bagliBayi($p);
    $_SERVER['REQUEST_METHOD'] = 'POST';
    $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_YENI];
    esit('success', gpanel_parolaDegistir(params(['configoption1' => 'bayi'])));
    esit(PW_YENI, $p->bayiler[$b['id']]['_parola']);
    $guncel = array_values(array_filter($GLOBALS['T']['localapi'], fn($c) => $c[0] === 'UpdateClientProduct'));
    dogru(count($guncel) === 1 && ($guncel[0][1]['servicepassword'] ?? '') === PW_YENI, json_encode(array_column($guncel, 0)));
    // Eslesmeyen tekrar: panele gidilmez.
    $_POST = ['yeni_parola' => PW_YENI, 'yeni_parola2' => PW_YENI . 'x'];
    icerir(gpanel_parolaDegistir(params(['configoption1' => 'bayi'])), 'eşleşmiyor');
    esit(1, $p->sayac('PUT /resellers/{id}/password'));
});

test('bayi parola: sablon — bayide de form (bagli hesapta), bayi kapsam metni, Profil baglantisi ikincil', function () {
    $t = file_get_contents($GLOBALS['MODUL'] . '/templates/clientarea.tpl');
    icerir($t, "{if \$tur neq 'bayi' || \$hesap}\n    <div class=\"gpca-kart\">\n      <details>");
    icerir($t, "{if \$tur eq 'bayi'}{\$L.bayi_parola_kapsam|escape}{else}{\$L.parola_kapsam|escape}{/if}");
    icerir(Yardimci::dil('turkish')['bayi_parola_kapsam'], 'API anahtarlarınız iptal');
    icerir(Yardimci::dil('english')['bayi_parola_kapsam'], 'revokes your reseller API keys');
});
