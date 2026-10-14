<?php

use GirginOSPanel\WHMCS\Kayit;

use WHMCS\Database\Capsule;

test('urun turu: WHMCS "Reseller Account" -> bayi hesabi (Urun tipi bos ya da hosting olsa da)', function () {
    foreach (['', 'hosting'] as $tip) {
        $p = yeni();
        $s = gpanel_CreateAccount(params(['producttype' => 'reselleraccount', 'configoption1' => $tip, 'configoption4' => '7', 'username' => 'bayi01']));
        esit('success', $s, "tip='$tip'");
        esit(1, $p->sayac('POST /resellers'), "tip='$tip': bayi olusturulmali");
        // 1.0.9: hizmetin alan adı için açılan hosting BAYİYE aittir (bayi_id) — ayrı bir müşteri hostingi değil.
        $post = array_values(array_filter($p->istekler, fn($i) => $i['anahtar'] === 'POST /domains'));
        esit(1, count($post), "tip='$tip': bayinin kendi hostingi");
        dogru(isset($post[0]['govde']['bayi_id']), "tip='$tip': hosting bayi adına (bayi_id)");
        esit('bayi', Kayit::oku(11)['tur'], "tip='$tip': hizmet bayiye bağlı");
    }
});

test('urun turu: hostingaccount + bos tip -> hosting (eski davranis); acik bayi secimi -> bayi', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(params(['producttype' => 'hostingaccount', 'configoption1' => ''])));
    esit(1, $p->sayac('POST /domains'));
    esit(0, $p->sayac('POST /resellers'));
    $p = yeni();
    esit('success', gpanel_CreateAccount(params(['producttype' => 'hostingaccount', 'configoption1' => 'bayi', 'configoption4' => '7', 'username' => 'bayi02'])));
    esit(1, $p->sayac('POST /resellers'));
});

test('urun turu: producttype yoksa urun kaydindan (tblproducts.type) okunur', function () {
    $p = yeni();
    Capsule::table('tblproducts')->insert(['id' => 77, 'type' => 'reselleraccount']);
    esit('success', gpanel_CreateAccount(params(['pid' => 77, 'configoption1' => '', 'configoption4' => '7', 'username' => 'bayi03'])));
    esit(1, $p->sayac('POST /resellers'));
});
