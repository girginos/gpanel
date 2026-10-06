<?php

use GirginOSPanel\WHMCS\Kayit;
use WHMCS\Database\Capsule;

function tblhostingDoldur(int $n, int $sunucu = 5): void
{
    for ($i = 1; $i <= $n; $i++) {
        Capsule::$tablolar['tblhosting'][] = ['id' => $i, 'server' => $sunucu, 'domainstatus' => 'Active', 'domain' => "site$i.com",
            'diskusage' => 0, 'disklimit' => 0, 'bwusage' => 0, 'bwlimit' => 0, 'lastupdate' => ''];
    }
}

function hizmetSatiri(int $id): array
{
    foreach (Capsule::$tablolar['tblhosting'] as $s) {
        if ($s['id'] === $id) {
            return $s;
        }
    }
    return [];
}

test('usage: TEK liste istegi + sayfali (parcali) DB guncelleme, 450 hizmet', function () {
    $p = yeni();
    tblhostingDoldur(450);
    for ($i = 1; $i <= 450; $i++) {
        $d = $p->domainEkle("site$i.com", 1, ['boyut_kb' => $i * 1024 + 500, 'trafik_kb' => $i * 2048, 'disk_kota_mb' => 1024, 'trafik_kota_mb' => 0]);
        Kayit::yaz($i, $d['id'], 'hosting', "site$i.com");
    }
    Capsule::$sorgular = [];
    esit('success', gpanel_UsageUpdate(params(['serverid' => 5])));
    esit(1, $p->sayac('GET /domains'), 'tek GET');
    esit(0, $p->sayac('GET /domains/{id}'), 'tek tek cagri yok');
    $guncel = 0;
    foreach (Capsule::$sorgular as $q) {
        if ($q[0] === 'update' && $q[1] === 'tblhosting') {
            $guncel++;
        }
    }
    esit(450, $guncel);
    $s = hizmetSatiri(450);
    esit(450, $s['diskusage']);          // 450*1024+500 KB -> 450 MB (asagi)
    esit(900, $s['bwusage']);
    esit(1024, $s['disklimit']);
    esit(0, $s['bwlimit'], '0 = sinirsiz');
    dogru($s['lastupdate'] !== '');
    esit(1, hizmetSatiri(1)['diskusage']);
});

test('usage: baglanmamis, baska sunucudaki, panelde silinmis hizmetler atlanir; ad esleme YOK', function () {
    $p = yeni();
    tblhostingDoldur(3);
    Capsule::$tablolar['tblhosting'][] = ['id' => 9, 'server' => 6, 'domainstatus' => 'Active', 'domain' => 'baska.com', 'diskusage' => 0, 'disklimit' => 0, 'bwusage' => 0, 'bwlimit' => 0, 'lastupdate' => ''];
    $a = $p->domainEkle('site1.com', 1, ['boyut_kb' => 5120]);
    $p->domainEkle('site2.com', 1, ['boyut_kb' => 9999999]);   // 2 numarali hizmet bagli DEGIL: ad ayni olsa da dokunulmaz
    Kayit::yaz(1, $a['id'], 'hosting', 'site1.com');
    Kayit::yaz(3, 99999, 'hosting', 'site3.com');               // panelde yok
    Kayit::yaz(9, $a['id'] + 1, 'hosting', 'baska.com');
    esit('success', gpanel_UsageUpdate(params(['serverid' => 5])));
    esit(5, hizmetSatiri(1)['diskusage']);
    esit(0, hizmetSatiri(2)['diskusage']);
    esit(0, hizmetSatiri(3)['diskusage']);
    esit(0, hizmetSatiri(9)['diskusage']);
});

test('usage: panel hatasinda (401/5xx/ag) DB degismez ve hata dizgesi doner; bos sunucuda istek yok', function () {
    $p = yeni();
    esit('success', gpanel_UsageUpdate(params()));
    esit(0, count($p->istekler));
    tblhostingDoldur(2);
    $d = $p->domainEkle('site1.com', 1, ['boyut_kb' => 4096]);
    Kayit::yaz(1, $d['id'], 'hosting', 'site1.com');
    $p->zorla['GET /domains'] = [500, '{"hata":"veritabanı hatası"}'];
    $s = gpanel_UsageUpdate(params());
    icerir($s, 'HTTP 500');
    esit(0, hizmetSatiri(1)['diskusage']);
    unset($p->zorla['GET /domains']);
    $p->anahtar = 'x';
    icerir(gpanel_UsageUpdate(params()), 'API anahtarı geçersiz');
    $p->anahtar = FakePanel::ANAHTAR;
    $p->ag = true;
    icerir(gpanel_UsageUpdate(params()), 'Panele bağlanılamadı');
});

test('usage: bayi hizmetleri /resellers listesinden; bayi listesi hatasi hosting guncellemesini durdurmaz', function () {
    $p = yeni();
    tblhostingDoldur(2);
    $d = $p->domainEkle('site1.com', 1, ['boyut_kb' => 3072]);
    $b = $p->bayiEkle('bayi01', ['disk_kullanim_kb' => 7168, 'max_disk_mb' => 2048, 'max_trafik_mb' => 4096]);
    Kayit::yaz(1, $d['id'], 'hosting', 'site1.com');
    Kayit::yaz(2, $b['id'], 'bayi', 'bayi01');
    esit('success', gpanel_UsageUpdate(params()));
    esit(7, hizmetSatiri(2)['diskusage']);
    esit(2048, hizmetSatiri(2)['disklimit']);
    esit(4096, hizmetSatiri(2)['bwlimit']);
    esit(3, hizmetSatiri(1)['diskusage']);
    // bayi ucu izinsiz
    Capsule::$tablolar['tblhosting'][0]['diskusage'] = 0;
    $p->izinler = ['hesap:yaz'];
    $s = gpanel_UsageUpdate(params());
    icerir($s, 'bayi kullanımı alınamadı');
    esit(3, hizmetSatiri(1)['diskusage'], 'hosting yine guncellendi');
});

test('usage: askidaki hizmet de guncellenir; Terminated/Cancelled atlanir', function () {
    $p = yeni();
    tblhostingDoldur(3);
    Capsule::$tablolar['tblhosting'][1]['domainstatus'] = 'Suspended';
    Capsule::$tablolar['tblhosting'][2]['domainstatus'] = 'Terminated';
    foreach ([1, 2, 3] as $i) {
        $d = $p->domainEkle("site$i.com", 1, ['boyut_kb' => 2048]);
        Kayit::yaz($i, $d['id'], 'hosting', "site$i.com");
    }
    gpanel_UsageUpdate(params());
    esit(2, hizmetSatiri(1)['diskusage']);
    esit(2, hizmetSatiri(2)['diskusage']);
    esit(0, hizmetSatiri(3)['diskusage']);
});

test('usage: musteri "kullanimi yenile" yalniz kendi hizmetini gunceller', function () {
    $p = yeni();
    tblhostingDoldur(2);
    $d = $p->domainEkle('site1.com', 1, ['boyut_kb' => 6144]);
    Kayit::yaz(1, $d['id'], 'hosting', 'site1.com');
    esit('success', gpanel_kullanimYenile(params(['serviceid' => 1])));
    esit(6, hizmetSatiri(1)['diskusage']);
    esit(0, hizmetSatiri(2)['diskusage']);
});
