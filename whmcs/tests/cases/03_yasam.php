<?php

use GirginOSPanel\WHMCS\Kayit;

function bagliHosting(?FakePanel &$p = null, array $pe = []): array
{
    $p = yeni();
    $d = $p->domainEkle('musteri.com');
    Kayit::yaz(11, $d['id'], 'hosting', 'musteri.com');
    return $d;
}

function bagliBayi(?FakePanel &$p = null): array
{
    $p = yeni();
    $b = $p->bayiEkle('bayi01');
    Kayit::yaz(11, $b['id'], 'bayi', 'bayi01');
    return $b;
}

test('askiya al / ac: hosting — dogru uc, panel durumu degisir', function () {
    $d = bagliHosting($p);
    esit('success', gpanel_SuspendAccount(params()));
    esit(true, $p->domains[$d['id']]['askida']);
    esit(1, $p->sayac('POST /domains/{id}/askiya-al'));
    esit('success', gpanel_UnsuspendAccount(params()));
    esit(false, $p->domains[$d['id']]['askida']);
    // govde bos nesne "{}"
    esit('{}', $p->istekler[0]['ham']);
});

test('askiya al / ac: bayi — PUT /resellers/{id} durum active|suspended', function () {
    $b = bagliBayi($p);
    $pr = params(['configoption1' => 'bayi']);
    esit('success', gpanel_SuspendAccount($pr));
    esit('suspended', $p->bayiler[$b['id']]['durum']);
    esit('success', gpanel_UnsuspendAccount($pr));
    esit('active', $p->bayiler[$b['id']]['durum']);
});

test('askiya al: bagli degilse net hata, istek atilmaz; baglama acikken ad uzerinden bulur', function () {
    $p = yeni();
    $d = $p->domainEkle('musteri.com');
    $s = gpanel_SuspendAccount(params());
    icerir($s, 'bağlı değil');
    esit(0, count($p->istekler));
    esit('success', gpanel_SuspendAccount(params(['configoption8' => 'on'])));
    esit($d['id'], kayitVar()['panel_id']);
});

test('askiya al: baska hizmete bagli hesaba ad uzerinden baglanamaz', function () {
    $d = bagliHosting($p);
    $s = gpanel_SuspendAccount(params(['serviceid' => 12, 'configoption8' => 'on']));
    icerir($s, 'başka bir WHMCS hizmetine bağlı');
    esit(false, $p->domains[$d['id']]['askida']);
});

test('askiya al: 404 / 409 / 403(askida) panel mesajlariyla', function () {
    $d = bagliHosting($p);
    unset($p->domains[$d['id']]);
    icerir(gpanel_SuspendAccount(params()), 'bulunamadı');
    $d = bagliHosting($p);
    $p->zorla['POST /domains/{id}/askiya-al'] = [409, '{"hata":"bayi hesabı askıda — tek tek işlem yapılamaz"}'];
    $s = gpanel_SuspendAccount(params());
    icerir($s, 'çakışıyor');
    icerir($s, 'bayi hesabı askıda');
});

test('sil: DELETE ?onay=<alan adi>; kayit silinir; 404 = zaten yok = basari', function () {
    $d = bagliHosting($p);
    esit('success', gpanel_TerminateAccount(params()));
    $i = null;
    foreach ($p->istekler as $x) {
        if ($x['anahtar'] === 'DELETE /domains/{id}') {
            $i = $x;
        }
    }
    esit(['onay' => 'musteri.com'], $i['query']);
    dogru($i['timeout'] >= 120);
    esit(0, count($p->domains));
    esit(null, kayitVar());
    // tekrar (panelde yok): idempotent basari
    $p2 = yeni();
    Kayit::yaz(11, 777, 'hosting', 'musteri.com');
    esit('success', gpanel_TerminateAccount(params()));
    esit(null, kayitVar());
});

test('sil: baska hata (500/403) basari DEGIL ve kayit korunur', function () {
    $d = bagliHosting($p);
    $p->zorla['DELETE /domains/{id}'] = [500, '{"hata":"silme hatası: x"}'];
    $s = gpanel_TerminateAccount(params());
    icerir($s, 'silme hatası: x');
    dogru(kayitVar() !== null);
});

test('sil: bayi — 409 (hosting hesaplari var) hata; bos bayi silinir', function () {
    $b = bagliBayi($p);
    $p->bayiler[$b['id']]['domain_sayisi'] = 3;
    $pr = params(['configoption1' => 'bayi']);
    $s = gpanel_TerminateAccount($pr);
    icerir($s, 'hosting hesaplarını taşıyın/silin');
    dogru(kayitVar() !== null);
    $p->bayiler[$b['id']]['domain_sayisi'] = 0;
    esit('success', gpanel_TerminateAccount($pr));
    esit(0, count($p->bayiler));
});

test('sil: baglanmamis hizmet sessizce basari sayilmaz', function () {
    $p = yeni();
    icerir(gpanel_TerminateAccount(params()), 'bağlı değil');
});

test('paket degistir: PUT /domains/{id}/plan {plan_id}; yanlis plan hata; bos plan null GONDERMEZ', function () {
    $d = bagliHosting($p);
    esit('success', gpanel_ChangePackage(params(['configoption2' => '2'])));
    esit(2, $p->domains[$d['id']]['plan_id']);
    $n = $p->sayac('PUT /domains/{id}/plan');
    icerir(gpanel_ChangePackage(params(['configoption2' => '555'])), 'Panel planı bulunamadı');
    icerir(gpanel_ChangePackage(params(['configoption2' => ''])), 'seçilmemiş');
    esit($n, $p->sayac('PUT /domains/{id}/plan'), 'yanlis/bos planda istek atilmamali');
    esit(2, $p->domains[$d['id']]['plan_id']);
});

test('paket degistir: bayi — PUT /resellers/{id} {paket_id}', function () {
    $b = bagliBayi($p);
    $p->paketler[] = ['id' => 9, 'ad' => 'Altın', 'max_domain' => 50, 'max_disk_mb' => 1, 'max_trafik_mb' => 1];
    esit('success', gpanel_ChangePackage(params(['configoption1' => 'bayi', 'configoption4' => 'Altın'])));
    esit(9, $p->bayiler[$b['id']]['paket_id']);
});

test('parola degistir: PUT ftp/password {"password"}; basari', function () {
    $d = bagliHosting($p);
    esit('success', gpanel_ChangePassword(params(['password' => PW_YENI])));
    esit(PW_YENI, $p->domains[$d['id']]['_ftp']);
    $i = end($p->istekler);
    esit(['password' => PW_YENI], $i['govde']);
});

test('parola degistir: zayif parola panele GITMEDEN reddedilir', function () {
    $d = bagliHosting($p);
    foreach (['kisa1', 'sadeceharfuzunparola', '123456789012345', ''] as $pw) {
        $s = gpanel_ChangePassword(params(['password' => $pw]));
        dogru($s !== 'success', $pw);
    }
    esit(0, $p->sayac('PUT /domains/{id}/ftp/password'));
});

test('parola degistir: ftp.parola izni yoksa 403 mesaji izin grubunu soyler; bayi urununde acik hata', function () {
    $d = bagliHosting($p);
    $p->izinler = ['hesap:yaz'];
    $s = gpanel_ChangePassword(params(['password' => PW_YENI]));
    icerir($s, "'ftp.parola' izni");
    $b = bagliBayi($p2);
    $s = gpanel_ChangePassword(params(['configoption1' => 'bayi', 'password' => PW_YENI]));
    icerir($s, 'Bayi hesabının parolası');
    esit(0, $p2->sayac('PUT /resellers/{id}'));
});

test('parola degistir: panel 400 (parola kurali) mesaji aynen', function () {
    $d = bagliHosting($p);
    $p->zorla['PUT /domains/{id}/ftp/password'] = [400, '{"hata":"parola harf ve rakam içermeli (karışık)"}'];
    $s = gpanel_ChangePassword(params(['password' => PW_YENI]));
    icerir($s, 'parola harf ve rakam içermeli (karışık)');
});
