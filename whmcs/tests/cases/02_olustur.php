<?php

use GirginOSPanel\WHMCS\Kayit;

function sonLocalApi(string $komut = 'UpdateClientProduct'): ?array
{
    $son = null;
    foreach ($GLOBALS['T']['localapi'] as [$k, $v]) {
        if ($k === $komut) {
            $son = $v;
        }
    }
    return $son;
}

function postDomains(FakePanel $p): array
{
    $out = [];
    foreach ($p->istekler as $x) {
        if ($x['anahtar'] === 'POST /domains') {
            $out[] = $x;
        }
    }
    return $out;
}

test('olustur: hosting basari — govde panel createReq ile birebir, kayit + WHMCS guncellemesi', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(params()));
    $pd = postDomains($p);
    esit(1, count($pd));
    esit(['alan_adi' => 'musteri.com', 'plan_id' => 1], $pd[0]['govde']);
    dogru($pd[0]['timeout'] >= 120, 'uzun zaman asimi');
    $k = kayitVar();
    esit('hosting', $k['tur']);
    dogru($k['panel_id'] > 0);
    esit(1, $p->sayac('PUT /domains/{id}/ftp/password'));
    $l = sonLocalApi();
    esit(11, $l['serviceid']);
    esit('c_musteri_com', $l['serviceusername']);
    esit('203.0.113.10', $l['dedicatedip']);
    esit(PW_WHMCS, $l['servicepassword']);
});

test('olustur: php/sql govdeye girer; bos plan = panel varsayilani (plan_id yok)', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(params(['configoption2' => '', 'configoption3' => '8.2', 'configoption5' => 'on'])));
    esit(['alan_adi' => 'musteri.com', 'php_surum' => '8.2', 'sql_olustur' => true], postDomains($p)[0]['govde']);
});

test('olustur: ftp.parola izni yoksa panelin urettigi parola WHMCS hizmetine yazilir', function () {
    $p = yeni();
    $p->izinler = ['hesap:yaz', 'plan:oku'];
    esit('success', gpanel_CreateAccount(params()));
    esit('GenFtpPass#7788Zz', sonLocalApi()['servicepassword']);
});

test('olustur: WHMCS parolasi panel kuralina uymuyorsa panelin urettigi kullanilir', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(params(['password' => 'kisa'])));
    esit(0, $p->sayac('PUT /domains/{id}/ftp/password'));
    esit('GenFtpPass#7788Zz', sonLocalApi()['servicepassword']);
});

test('olustur: idempotent — ikinci cagri yeni hesap acmaz, hata vermez', function () {
    $p = yeni();
    esit('success', gpanel_CreateAccount(params()));
    esit('success', gpanel_CreateAccount(params()));
    esit(1, count(postDomains($p)));
    esit(1, count($p->domains));
});

test('olustur: kayit kaybolmus + panelde var (olusturma niyeti isareti ile) — yeniden baglar, cift hesap acmaz', function () {
    // POST basarili ama yanit WHMCS'e ulasmadi (504): hesap panelde var, kayit "niyet" durumunda.
    $p = yeni();
    $p->olusturmaGecikmeHatasi = true;
    $s = gpanel_CreateAccount(params());
    icerir($s, 'HTTP 504');
    esit(0, kayitVar()['panel_id'], 'niyet isareti kalmali');
    esit(1, count($p->domains), 'hesap panelde olustu');
    $p->olusturmaGecikmeHatasi = false;
    esit('success', gpanel_CreateAccount(params()));
    esit(1, count(postDomains($p)), 'ikinci POST olmamali');
    dogru(kayitVar()['panel_id'] > 0);
});

test('olustur: panelde baskasinin hesabi varsa ve baglama kapaliysa BAGLANMAZ (hesap devralma engeli)', function () {
    $p = yeni();
    $p->domainEkle('musteri.com');
    $s = gpanel_CreateAccount(params());
    icerir($s, 'zaten var');
    icerir($s, 'Mevcut hesabı bağla');
    esit(null, kayitVar());
    esit(0, count(postDomains($p)));
    esit(0, $p->sayac('PUT /domains/{id}/ftp/password'), 'baskasinin parolasi degistirilmemeli');
});

test('olustur: mevcut hesabi bagla secenegi acikken baglar; baska hizmete bagliysa reddeder', function () {
    $p = yeni();
    $d = $p->domainEkle('musteri.com');
    esit('success', gpanel_CreateAccount(params(['configoption8' => 'on'])));
    esit($d['id'], kayitVar()['panel_id']);
    esit(0, count(postDomains($p)));
    // baska hizmet ayni hesabi isteyemez
    $s = gpanel_CreateAccount(params(['serviceid' => 12, 'configoption8' => 'on']));
    icerir($s, 'başka bir WHMCS hizmetine bağlı');
    esit(null, Kayit::oku(12));
});

test('olustur: yanlis plan eslemesi — POST atilmadan net hata', function () {
    $p = yeni();
    $s = gpanel_CreateAccount(params(['configoption2' => '999']));
    icerir($s, 'Panel planı bulunamadı (id 999)');
    esit(0, count(postDomains($p)));
    esit(null, kayitVar());
    $s = gpanel_CreateAccount(params(['configoption2' => 'Yok Boyle Plan']));
    icerir($s, 'Mevcut: Başlangıç');
    esit(0, count(postDomains($p)));
});

test('olustur: plan adi buyuk/kucuk harf duyarsiz eslenir; birden cok eslesme belirsizse hata', function () {
    $p = yeni();
    $s = gpanel_CreateAccount(params(['configoption2' => 'başlangıç']));
    esit('success', $s);
    esit(1, postDomains($p)[0]['govde']['plan_id']);
    $p = yeni();
    $s = gpanel_CreateAccount(params(['configoption2' => 'PRO']));  // "Pro" ve "pro" iki kayit
    icerir($s, 'birden çok kayıtla eşleşiyor');
    esit(0, count(postDomains($p)));
    $p = yeni();
    esit('success', gpanel_CreateAccount(params(['configoption2' => '2'])));
    esit(2, postDomains($p)[0]['govde']['plan_id']);
});

test('olustur: bos alan adi / panel dogrulama hatasi (400) aynen yansir ve niyet isareti silinir', function () {
    $p = yeni();
    icerir(gpanel_CreateAccount(params(['domain' => ''])), 'alan adı');
    esit(0, count($p->istekler));
    $s = gpanel_CreateAccount(params(['domain' => 'gecersiz alan']));
    icerir($s, 'geçersiz alan adı');
    esit(null, kayitVar(), 'kesin ret: niyet isareti kalmamali');
});

test('olustur: 5xx sonrasi niyet isareti kalir (yeniden denenebilir)', function () {
    $p = yeni();
    $p->zorla['POST /domains'] = [500, '{"hata":"sağlama başarısız: x"}'];
    $s = gpanel_CreateAccount(params());
    icerir($s, 'sağlama başarısız: x');
    esit(0, kayitVar()['panel_id']);
});

test('olustur: izin eksikse (hesap:yaz yok) 403 mesaji izin grubunu soyler', function () {
    $p = yeni();
    $p->izinler = ['hesap:oku', 'plan:oku'];
    $s = gpanel_CreateAccount(params());
    icerir($s, "'hesap' izni");
});

test('olustur: bayi urunu — govde, kayit, idempotent, kullanici adi/parola/paket kontrolleri', function () {
    $p = yeni();
    $pr = params(['configoption1' => 'bayi', 'username' => 'bayi01']);
    esit('success', gpanel_CreateAccount($pr));
    $r = null;
    foreach ($p->istekler as $x) {
        if ($x['anahtar'] === 'POST /resellers') {
            $r = $x;
        }
    }
    esit(['paket_id' => 7, 'kullanici' => 'bayi01', 'parola' => PW_WHMCS, 'ad_soyad' => 'Ali Veli'], $r['govde']);
    esit('bayi', kayitVar()['tur']);
    esit('success', gpanel_CreateAccount($pr));
    esit(1, $p->sayac('POST /resellers'));
    // hatalar
    yeni();
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'Ab'])), 'kullanıcı adı geçersiz');
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'root'])), 'kullanıcı adı geçersiz');
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi02', 'password' => 'kisa'])), '8-128');
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi02', 'configoption4' => 'Yok'])), 'Bayi paketi bulunamadı');
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi02', 'configoption4' => ''])), "seçilmemiş");
});

test('olustur: bayi — panelde var olan kullanici adi baglama kapaliyken reddedilir', function () {
    $p = yeni();
    $p->bayiEkle('bayi03');
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi03'])), 'zaten var');
    esit(0, $p->sayac('POST /resellers'));
    esit('success', gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi03', 'configoption8' => 'on'])));
});

test('olustur: bayi 409 (kullanici var — yaris) mesaji panelden aynen', function () {
    $p = yeni();
    $p->zorla['POST /resellers'] = [409, '{"hata":"bu kullanıcı adı zaten var"}'];
    icerir(gpanel_CreateAccount(params(['configoption1' => 'bayi', 'username' => 'bayi04'])), 'bu kullanıcı adı zaten var');
});

test('olustur: istisna (beklenmeyen) kullaniciya kisa metin, sir icermez', function () {
    $p = yeni();
    \GirginOSPanel\WHMCS\ApiIstemci::$varsayilanTasiyici = function () {
        throw new RuntimeException('curl hata ' . FakePanel::ANAHTAR);
    };
    $s = gpanel_CreateAccount(params());
    icerir($s, 'Panele bağlanılamadı');
    icermez($s, FakePanel::ANAHTAR);
});
