<?php

use GirginOSPanel\WHMCS\Modul;
use GirginOSPanel\WHMCS\Yardimci;

test('musteri alani: boyutMetin — MB/GB okunakli', function () {
    foreach ([0 => '0 MB', 1 => '1 MB', 1023 => '1023 MB', 1024 => '1 GB', 1536 => '1.5 GB', 10240 => '10 GB', 51200 => '50 GB', 1100 => '1.1 GB', -5 => '0 MB'] as $mb => $bek) {
        esit($bek, Yardimci::boyutMetin($mb), (string) $mb);
    }
});

test('musteri alani: olcerler — metin, yuzde, sinirsiz, esik siniflari', function () {
    $d = bagliHosting($p);
    $p->domains[$d['id']] = array_merge($p->domains[$d['id']], ['boyut_kb' => 512 * 1024, 'disk_kota_mb' => 1024, 'trafik_kb' => 0, 'trafik_kota_mb' => 0, 'ssl' => true, 'ssl_bitis' => '2027-01-10']);
    $v = gpanel_ClientArea(params())['vars'];
    esit(['ad' => 'disk', 'kullanim_metin' => '512 MB', 'limit_metin' => '1 GB', 'yuzde' => 50, 'limitli' => true, 'sinif' => ''], $v['olcerler'][0]);
    esit(false, $v['olcerler'][1]['limitli'], 'trafik sinirsiz');
    esit(true, $v['hesap']['ssl']);
    esit('2027-01-10', $v['hesap']['ssl_bitis']);
    esit('uyari', Modul::olcer('disk', Modul::cubuk(75, 100))['sinif']);
    esit('kritik', Modul::olcer('disk', Modul::cubuk(95, 100))['sinif']);
    esit('', Modul::olcer('disk', Modul::cubuk(10, 0))['sinif'], 'limitsiz sinif almaz');
});

test('musteri alani: sablon — panel baglantilari YENI SEKMEDE, CSS literal icinde, iki dilde ayni anahtarlar', function () {
    global $MODUL;
    $t = file_get_contents($MODUL . '/templates/clientarea.tpl');
    dogru((bool) preg_match('#href="clientarea\.php\?action=productdetails&amp;id=\{\$serviceid\|escape\}&amp;dosinglesignon=1" target="_blank" rel="noopener"#', $t), 'SSO baglantisi yeni sekmede');
    // 1.0.9: düz panel bağlantısı düğmesi yok (tek tık giriş kullanılamazsa WHMCS düz adrese yönlendirir).
    icermez($t, 'href="{$panel_url|escape}"', 'düz panel bağlantısı düğmesi kalktı');
    dogru((bool) preg_match('#&amp;gp_hedef=hosting" target="_blank" rel="noopener"#', $t), 'bayinin hosting girişi yeni sekmede');
    esit(1, substr_count($t, '{literal}'));
    $stil = substr($t, strpos($t, '{literal}'), strpos($t, '{/literal}') - strpos($t, '{literal}'));
    dogru(strpos($stil, '<style>') !== false && strpos($stil, '</style>') !== false, 'stil literal icinde');
    preg_match_all('/\$L\.([a-z_]+)/', $t, $m);
    $tr = Yardimci::dil('turkish');
    $en = Yardimci::dil('english');
    foreach (array_unique($m[1]) as $k) {
        dogru(isset($tr[$k]) && isset($en[$k]), "dil anahtari eksik: $k");
    }
});

test('musteri alani: bayi — trafik olceri YOK (panel olcmez), alan sayisi olcer DEGIL hesap satiri', function () {
    $p = yeni();
    $b = $p->bayiEkle('bayi01');
    \GirginOSPanel\WHMCS\Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    $p->bayiler[$b['id']] = array_merge($p->bayiler[$b['id']], ['domain_sayisi' => 9, 'max_domain' => 10, 'disk_kullanim_kb' => 2048 * 1024, 'max_disk_mb' => 20480, 'max_trafik_mb' => 51200, 'durum' => 'suspended']);
    $v = gpanel_ClientArea(params(['configoption1' => 'bayi']))['vars'];
    $adlar = array_column($v['olcerler'], 'ad');
    esit(['disk'], $adlar, 'toplam alan adi kaynak olcerlerinde YOK (hesap bilgilerinde var)');
    esit([9, 10], [$v['hesap']['alan_sayisi'], $v['hesap']['max_domain']], 'hesap bilgileri satiri');
    esit('2 GB', $v['olcerler'][0]['kullanim_metin']);
    esit('askida', $v['hesap']['durum'], 'panel suspended -> sablon askida');
    esit(2, count($v['cubuklar']), 'eski cubuklar (geri uyum) degismedi');
});
