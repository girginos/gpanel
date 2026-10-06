<?php

use GirginOSPanel\WHMCS\ApiHatasi;
use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Kayit;
use GirginOSPanel\WHMCS\Yardimci;

test('meta: MetaData ve ConfigOptions sozlesmesi', function () {
    $m = gpanel_MetaData();
    esit('GirginOSPanel', $m['DisplayName']);
    esit(true, $m['RequiresServer']);
    dogru(isset($m['ServiceSingleSignOnLabel'], $m['AdminSingleSignOnLabel']));
    $o = gpanel_ConfigOptions();
    esit(['urun_tipi', 'plan', 'php', 'bayi_paketi', 'sql_olustur', 'tls_dogrulama_kapali', 'panel_adresi', 'mevcut_bagla'], array_keys($o));
    esit(['hosting', 'bayi'], array_keys($o['urun_tipi']['Options']));
    icerir($o['tls_dogrulama_kapali']['Description'], 'MITM');
    dogru(is_callable($o['plan']['Loader']));
});

test('meta: tum WHMCS islevleri tanimli', function () {
    foreach (['MetaData', 'ConfigOptions', 'TestConnection', 'CreateAccount', 'SuspendAccount', 'UnsuspendAccount', 'TerminateAccount',
        'ChangePackage', 'ChangePassword', 'UsageUpdate', 'AdminLink', 'ServiceSingleSignOn', 'AdminSingleSignOn', 'ClientArea',
        'AdminServicesTabFields', 'AdminServicesTabFieldsSave', 'ClientAreaCustomButtonArray', 'ClientAreaAllowedFunctions'] as $f) {
        dogru(function_exists('gpanel_' . $f), $f);
    }
});

test('meta: ConfigOptions Loader panelden plan listesi doldurur, hata verirse bos doner', function () {
    $p = yeni();
    $o = gpanel_ConfigOptions();
    $liste = ($o['plan']['Loader'])(params());
    esit('Başlangıç', $liste['1']);
    $paket = ($o['bayi_paketi']['Loader'])(params());
    esit(['7' => 'Bayi Gümüş'], $paket);
    $p->ag = true;
    esit([], ($o['plan']['Loader'])(params()));
});

test('istemci: kimlik basligi gosp <kullanici>:<anahtar>, https + kapi portu', function () {
    $p = yeni();
    gpanel_TestConnection(params());
    $i = $p->istekler[0];
    dogru(in_array('Authorization: gosp root:' . FakePanel::ANAHTAR, $i['basliklar'], true));
    esit('https://panel.example.test:8443/api/v1/api-anahtari/ben', $i['url']);
    esit(true, $i['dogrula'], 'TLS dogrulamasi varsayilan ACIK olmali');
});

test('istemci: serversecure kapaliysa anahtar duz HTTP ile gonderilmez', function () {
    $p = yeni();
    $r = gpanel_TestConnection(params(['serversecure' => '']));
    esit(false, $r['success']);
    icerir($r['error'], 'SSL');
    esit(0, count($p->istekler));
});

test('istemci: gecersiz host/port/anahtar reddedilir, istek atilmaz', function () {
    $p = yeni();
    foreach ([['serverhostname' => 'a.com/../x'], ['serverhostname' => 'evil.com@x'], ['serverport' => '0'], ['serverport' => '70000'], ['serverpassword' => '']] as $ek) {
        $r = gpanel_TestConnection(params($ek + ['serverip' => '']));
        esit(false, $r['success'], json_encode($ek));
    }
    esit(0, count($p->istekler));
});

test('istemci: TLS dogrulamasi yalniz ACIK secimle kapanir (urun secenegi ya da access hash)', function () {
    $p = yeni();
    gpanel_SuspendAccount(params(['serviceid' => 1]));  // bagli degil; istek yok ama istemci kurulur
    gpanel_TestConnection(params(['serveraccesshash' => 'tls-dogrulama-kapali']));
    esit(false, $p->istekler[0]['dogrula']);
    $p2 = yeni();
    Kayit::yaz(11, 101, 'hosting', 'musteri.com');
    $p2->domainEkle('musteri.com');
    gpanel_SuspendAccount(params(['configoption6' => 'on']));
    esit(false, $p2->istekler[0]['dogrula']);
    $p3 = yeni();
    $p3->domainEkle('musteri.com');
    Kayit::yaz(11, 101, 'hosting', 'musteri.com');
    gpanel_SuspendAccount(params(['configoption6' => '']));
    esit(true, $p3->istekler[0]['dogrula']);
});

test('baglanti testi: basari / 401 / 403 / izin eksik / ag hatasi', function () {
    $p = yeni();
    esit(['success' => true, 'error' => ''], gpanel_TestConnection(params()));
    $p->anahtar = 'gosp_YANLISYANLIS_x';
    $r = gpanel_TestConnection(params());
    esit(false, $r['success']);
    icerir($r['error'], 'API anahtarı geçersiz');
    $p = yeni();
    $p->izinler = ['dns:yaz'];
    $r = gpanel_TestConnection(params());
    esit(false, $r['success']);
    icerir($r['error'], 'hesap:yaz');
    icerir($r['error'], 'plan:oku');
    $p = yeni();
    $p->izinler = ['hesap:yaz', 'plan:oku'];
    esit(true, gpanel_TestConnection(params())['success']);
    $p = yeni();
    $p->ag = true;
    $r = gpanel_TestConnection(params());
    icerir($r['error'], 'Panele bağlanılamadı');
    icerir($r['error'], 'Could not resolve host');
});

test('hata esleme: her HTTP kodu eyleme donuk Turkce mesaj + panel mesaji aynen', function () {
    $tablo = [
        [400, 'alan adı geçersiz: x', '', '', ['Panel isteği doğrulamadı:', 'alan adı geçersiz: x']],
        [422, 'plan seçilemedi', '', '', ['Panel isteği doğrulamadı:', 'plan seçilemedi']],
        [401, 'geçersiz oturum', '', '', ['API anahtarı geçersiz', 'kullanıcı adı']],
        [403, 'anahtarın bu işlem için izni yok', '', 'ftp.parola', ["'ftp.parola' izni eksik", 'anahtarın bu işlem için izni yok']],
        [403, 'hesabınız askıya alınmış', 'hesap_askida', 'hesap', ['askıda']],
        [403, 'bayi hesabı askıya alınmış', 'bayi_askida', 'hesap', ['askıda']],
        [404, 'domain bulunamadı', '', '', ['bulunamadı', 'domain bulunamadı']],
        [409, 'bayi hesabı askıda', '', '', ['çakışıyor', 'bayi hesabı askıda']],
        [429, 'hız sınırı', '', '', ['hız sınırı']],
        [500, 'iç hata', '', '', ['sunucu hatası (HTTP 500)', 'iç hata']],
        [503, 'doğrulanamıyor', '', '', ['sunucu hatası (HTTP 503)']],
        [418, 'x', '', '', ['Beklenmeyen panel yanıtı (HTTP 418)']],
    ];
    foreach ($tablo as [$d, $pm, $kod, $grup, $beklenen]) {
        $m = ApiIstemci::hataMesaji($d, $pm, $kod, $grup);
        foreach ($beklenen as $b) {
            icerir($m, $b, "HTTP $d");
        }
    }
});

test('hata esleme: gercek istek yolu ApiHatasi alanlarini doldurur (durum, kod, panel mesaji)', function () {
    $p = yeni();
    $p->zorla['POST /domains/{id}/askiya-al'] = [403, '{"hata":"hesabınız askıya alınmış","kod":"hesap_askida"}'];
    $c = ApiIstemci::paramsdan(params());
    try {
        $c->askiyaAl(5);
        dogru(false);
    } catch (ApiHatasi $e) {
        esit(403, $e->durum);
        esit('hesap_askida', $e->kod);
        esit('hesabınız askıya alınmış', $e->panelMesaji);
        esit('hesap', $e->izinGrubu);
    }
    $p->zorla['GET /domains'] = [200, '<html>proxy</html>'];
    try {
        $c->hesaplar();
        dogru(false);
    } catch (ApiHatasi $e) {
        icerir($e->getMessage(), 'çözümlenemedi');
    }
    $p->zorla['GET /plans'] = [200, '{"a":1}'];
    try {
        $c->planlar();
        dogru(false);
    } catch (ApiHatasi $e) {
        icerir($e->getMessage(), 'liste biçiminde değil');
    }
});

test('islem seviyesinde esleme: 403/404/409/422/429/5xx dizgeleri', function () {
    foreach ([[403, 'izin yok'], [404, 'yok'], [409, 'askıda'], [422, 'plan hatası'], [429, 'yavaş'], [500, 'hata'], [503, 'hata']] as [$d, $msg]) {
        $p = yeni();
        $p->domainEkle('musteri.com');
        Kayit::yaz(11, 101, 'hosting', 'musteri.com');
        $p->zorla['POST /domains/{id}/askiya-al'] = [$d, json_encode(['hata' => $msg])];
        $s = gpanel_SuspendAccount(params());
        dogru($s !== 'success', "HTTP $d");
        icerir($s, $msg, "HTTP $d");
    }
});

test('yardimci: parola kurali panelle ayni (12+, harf+rakam, satir sonu yok)', function () {
    esit(null, Yardimci::parolaHatasi('Abcdefghij12'));
    dogru(Yardimci::parolaHatasi('kisa1') !== null);
    dogru(Yardimci::parolaHatasi('sadeceharflerburada') !== null);
    dogru(Yardimci::parolaHatasi('1234567890123456') !== null);
    dogru(Yardimci::parolaHatasi("Abcdefghij12\nx") !== null);
    dogru(Yardimci::parolaHatasi(str_repeat('a1', 65)) !== null);
    dogru(Yardimci::parolaHatasi('') !== null);
});
