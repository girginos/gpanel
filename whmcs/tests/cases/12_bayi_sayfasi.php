<?php

use GirginOSPanel\WHMCS\Kayit;

function bayiSayfasi(?array $kaynak, ?array $portlar = ['musteri' => 9443, 'bayi' => 9444]): array
{
    $p = yeni();
    $p->kapiPortlar = $portlar;
    $p->bayiKaynak = $kaynak;
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    $p->bayiler[$b['id']] = array_merge($p->bayiler[$b['id']], ['domain_sayisi' => 12, 'max_domain' => 50, 'disk_kullanim_kb' => 1024 * 1024, 'max_disk_mb' => 20480]);
    return gpanel_ClientArea(params(['configoption1' => 'bayi', 'configoption7' => '']))['vars'];
}

test('bayi sayfasi: havuz ölçerleri (CPU vCPU, RAM); toplam alan adı ölçer değil hesap satırı', function () {
    $v = bayiSayfasi(['havuz' => ['cpu_cekirdek' => 2, 'ram_mb' => 1024, 'disk_mb' => 20480], 'anlik' => ['cpu_milli' => 350, 'ram_mb' => 512]]);
    esit('', $v['hata']);
    esit(['disk', 'cpu', 'ram'], array_column($v['olcerler'], 'ad'));
    $o = array_column($v['olcerler'], null, 'ad');
    esit(['ad' => 'cpu', 'kullanim_metin' => '0.35 vCPU', 'limit_metin' => '2 vCPU', 'yuzde' => 18, 'limitli' => true, 'sinif' => ''], $o['cpu']);
    esit(['ad' => 'ram', 'kullanim_metin' => '512 MB', 'limit_metin' => '1 GB', 'yuzde' => 50, 'limitli' => true, 'sinif' => ''], $o['ram']);
    esit([12, 50], [$v['hesap']['alan_sayisi'], $v['hesap']['max_domain']], 'toplam alan adı hesap bilgilerinde');
    esit(null, $v['bayi_hosting'], 'hizmette alan adı panelde yok → bayinin hostingi yok');
});

test('bayi sayfasi: havuz yok / eski panel / anlık örnek yok — sayfa bozulmaz', function () {
    $v = bayiSayfasi(['havuz' => ['cpu_cekirdek' => 0, 'ram_mb' => 0, 'disk_mb' => 0]]);
    esit(['disk', 'cpu', 'ram'], array_column($v['olcerler'], 'ad'), 'havuz tanımlı değil: CPU/RAM yine görünür');
    $o = array_column($v['olcerler'], null, 'ad');
    esit(['ad' => 'cpu', 'kullanim_metin' => '—', 'limit_metin' => '', 'yuzde' => 0, 'limitli' => false, 'sinif' => ''], $o['cpu']);
    esit(['ad' => 'ram', 'kullanim_metin' => '—', 'limit_metin' => '', 'yuzde' => 0, 'limitli' => false, 'sinif' => ''], $o['ram']);
    $v = bayiSayfasi(['havuz' => ['cpu_cekirdek' => 0, 'ram_mb' => 0], 'anlik' => ['cpu_milli' => 1250, 'ram_mb' => 3072]]);
    $o = array_column($v['olcerler'], null, 'ad');
    esit(['1.25 vCPU', '', false], [$o['cpu']['kullanim_metin'], $o['cpu']['limit_metin'], $o['cpu']['limitli']], 'sınırsız + ölçülmüş');
    esit(['3 GB', '', false], [$o['ram']['kullanim_metin'], $o['ram']['limit_metin'], $o['ram']['limitli']]);
    $v = bayiSayfasi(null);
    esit('', $v['hata']);
    esit(['disk'], array_column($v['olcerler'], 'ad'), 'eski panel (uç yok)');
    $v = bayiSayfasi(['havuz' => ['cpu_cekirdek' => 1.5, 'ram_mb' => 2048]]);
    $o = array_column($v['olcerler'], null, 'ad');
    esit('0 vCPU', $o['cpu']['kullanim_metin'], 'anlık yok → 0');
    esit('1.5 vCPU', $o['cpu']['limit_metin']);
    $v = bayiSayfasi(null, null);
    // Bağlı olmayan bayi hizmeti: şifre bağlantısı yok.
    yeni();
    $v = gpanel_ClientArea(params(['configoption1' => 'bayi']))['vars'];
    esit(null, $v['bayi_hosting']);
});

test('bayi sayfasi: hosting ürününde havuz ölçeri ve bayi şifre bağlantısı YOK', function () {
    $p = yeni();
    $d = bagliHosting($p);
    $p->kapiPortlar = ['musteri' => 9443, 'bayi' => 9444];
    $p->bayiKaynak = ['havuz' => ['cpu_cekirdek' => 2, 'ram_mb' => 1024]];
    $v = gpanel_ClientArea(params(['configoption7' => '']))['vars'];
    esit(null, $v['bayi_hosting']);
    dogru(!in_array('cpu', array_column($v['olcerler'], 'ad'), true));
    esit(0, $p->sayac('GET /bayi-kaynak/{id}'), 'hosting sayfası bayi kaynağını sormaz');
});

test('bayi sayfasi: şablonda şifre bağlantısı yeni sekmede, noopener', function () {
    $t = file_get_contents($GLOBALS['MODUL'] . '/templates/clientarea.tpl');
    icermez($t, 'bayi_parola_url');
    icermez($t, '{$panel_url|escape}', 'düz panel bağlantısı düğmesi kalktı');
});
