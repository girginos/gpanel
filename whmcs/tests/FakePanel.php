<?php
/**
 * Sahte panel: ApiIstemci'ye enjekte edilen tasiyici. Yanit govdeleri internal/ Go
 * kodundaki JSON etiketlerinden birebir alinmistir (domains.Domain, createResp,
 * plans.Plan, reseller.Reseller/Paket, httpx.ErrorBody). Uydurma alan YOK.
 *
 * Yetkilendirme izin.go Rotalar tablosunun ilgili satirlarini yansitir (Karar).
 */

final class FakePanel
{
    public const ANAHTAR = 'gosp_ABCDEFGHJKLM_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';
    public const KULLANICI = 'root';

    /** izin.go Rotalar (yalniz modulun kullandiklari + sso). yol deseni => [grup, erisim] */
    public static $rotalar = [
        'GET /domains' => ['hesap', 'oku'],
        'GET /domains/{id}' => ['hesap', 'oku'],
        'POST /domains' => ['hesap', 'yaz'],
        'DELETE /domains/{id}' => ['hesap', 'yaz'],
        'PUT /domains/{id}/plan' => ['hesap', 'yaz'],
        'POST /domains/{id}/askiya-al' => ['hesap', 'yaz'],
        'POST /domains/{id}/askidan-al' => ['hesap', 'yaz'],
        'PUT /domains/{id}/ftp/password' => ['ftp.parola', 'yaz'],
        'GET /plans' => ['plan', 'oku'],
        'GET /resellers' => ['bayi', 'oku'],
        'GET /reseller-plans' => ['bayi', 'oku'],
        'POST /resellers' => ['bayi', 'yaz'],
        'PUT /resellers/{id}' => ['bayi', 'yaz'],
        'DELETE /resellers/{id}' => ['bayi', 'yaz'],
        'POST /domains/{id}/sso' => ['sso', 'yaz'],
        'GET /bayi-kaynak/{id}' => ['bayi', 'oku'],
        // isleyici ayrica bayi:yaz ister (internal/bayisso)
        'POST /resellers/{id}/sso' => ['sso', 'yaz'],
        'PUT /resellers/{id}/password' => ['bayi.parola', 'yaz'],
    ];
    public static $herZaman = ['GET /api-anahtari/ben', 'GET /kapi/adres', 'GET /kapi/portlar'];
    /** /kapi/portlar yaniti: null = uc yok (eski panel, 404); dizi = ['musteri' => P, 'bayi' => P] */
    public $kapiPortlar = null;
    /** /bayi-kaynak/{id} yaniti: null = uc yok (eski panel, 404) */
    public $bayiKaynak = null;
    /** /kapi/adres yaniti: bos = yeni kapidayiz; ['adres' => 'https://h:P'] = eski kapi */
    public $kapiAdres = [];

    public $izinler = ['hesap:yaz', 'plan:yaz', 'ftp.parola:yaz', 'bayi:yaz', 'sso:yaz', 'bayi.parola:yaz'];
    /** bayi parola ucu: 'var' | '404' (eski panel, uc yok) */
    public $bayiParolaModu = 'var';
    public $istekler = [];
    public $domains = [];
    public $plans = [];
    public $bayiler = [];
    public $paketler = [];
    public $sonId = 100;
    public $anahtar = self::ANAHTAR;
    /** 'var' | '404' | '403' | '409' | '429' | 'url_http' | 'url_baska' | 'url_user' */
    public $ssoModu = 'var';
    /** bayi SSO: 'var' | '404' (eski panel) | 'url_baska' */
    public $bayiSsoModu = 'var';
    /** "METOD /yol" => [durum, govde] zorla */
    public $zorla = [];
    public $ag = false; // true: aga ulasilamadi
    public $ftpCikti = 'GenFtpPass#7788Zz';
    public $olusturmaGecikmeHatasi = false; // POST /domains: hesap olussun ama 504 don

    public function __construct()
    {
        $this->plans = [
            ['id' => 1, 'ad' => 'Başlangıç', 'disk_kota_mb' => 1024, 'trafik_kota_mb' => 10240, 'php_surum' => '8.3', 'varsayilan' => true],
            ['id' => 2, 'ad' => 'Pro', 'disk_kota_mb' => 5120, 'trafik_kota_mb' => 51200, 'php_surum' => '8.3', 'varsayilan' => false],
            ['id' => 3, 'ad' => 'pro', 'disk_kota_mb' => 1, 'trafik_kota_mb' => 1, 'php_surum' => '8.2', 'varsayilan' => false],
        ];
        $this->paketler = [
            ['id' => 7, 'ad' => 'Bayi Gümüş', 'max_domain' => 10, 'max_disk_mb' => 10240, 'max_trafik_mb' => 102400],
        ];
    }

    public function domainEkle(string $alan, int $planId = 1, array $ek = []): array
    {
        $id = ++$this->sonId;
        $d = $ek + [
            'id' => $id, 'alan_adi' => $alan, 'php_surum' => '8.3', 'ssl' => false, 'durum' => 'aktif',
            'sistem_kullanici' => 'c_' . preg_replace('/[^a-z0-9]/', '_', $alan), 'boyut_kb' => 0, 'trafik_kb' => 0,
            'olusturulma' => '2026-10-06', 'ipv4' => '203.0.113.10', 'ftp_host' => '203.0.113.10',
            'ftp_user' => 'c_' . preg_replace('/[^a-z0-9]/', '_', $alan), 'db_host' => 'localhost', 'db_user' => '', 'db_adi' => '',
            'web_root' => '/home/x/public_html', 'is_demo' => false, 'plan_id' => $planId, 'plan_ad' => 'Başlangıç',
            'ssh_erisim' => false, 'askida' => false, 'sahip_ad' => 'Panel Sahibi', 'sahip_tur' => 'admin', 'reseller_id' => 0,
            'disk_kota_mb' => 1024, 'trafik_kota_mb' => 10240,
        ];
        $this->domains[$id] = $d;
        return $d;
    }

    public function bayiEkle(string $kullanici, array $ek = []): array
    {
        $id = ++$this->sonId;
        $b = $ek + [
            'id' => $id, 'kullanici' => $kullanici, 'ad_soyad' => '', 'durum' => 'active', 'max_domain' => 10,
            'max_disk_mb' => 10240, 'max_trafik_mb' => 102400, 'domain_sayisi' => 0, 'disk_kullanim_kb' => 0,
            'paket_id' => 7, 'paket_ad' => 'Bayi Gümüş', 'son_giris' => '', 'olusturulma' => '2026-10-06',
        ];
        $this->bayiler[$id] = $b;
        return $b;
    }

    /** Tasiyici callable. */
    public function tasiyici(): callable
    {
        return function (array $istek): array {
            return $this->isle($istek);
        };
    }

    public function sayac(string $metodYol): int
    {
        $n = 0;
        foreach ($this->istekler as $i) {
            if ($i['anahtar'] === $metodYol) {
                $n++;
            }
        }
        return $n;
    }

    /** Test ici ('_' oneki) alanlar gercek panel yanitinda OLMAZ. */
    private function temizle($g)
    {
        if (!is_array($g)) {
            return $g;
        }
        $o = [];
        foreach ($g as $k => $v) {
            if (is_string($k) && $k !== '' && $k[0] === '_') {
                continue;
            }
            $o[$k] = $this->temizle($v);
        }
        return $o;
    }

    private function yanit(int $d, $g): array
    {
        $g = $this->temizle($g);
        return ['durum' => $d, 'govde' => is_string($g) ? $g : json_encode($g, JSON_UNESCAPED_UNICODE), 'hata' => null];
    }

    private function hata(int $d, string $m, string $kod = ''): array
    {
        return $this->yanit($d, $kod !== '' ? ['hata' => $m, 'kod' => $kod] : ['hata' => $m]);
    }

    private function isle(array $i): array
    {
        $u = parse_url($i['url']);
        $yol = substr($u['path'], strlen('/api/v1'));
        parse_str($u['query'] ?? '', $q);
        $desen = preg_replace('#/\d+#', '/{id}', $yol);
        $anahtar = $i['method'] . ' ' . $desen;
        $govde = $i['body'] !== null ? json_decode($i['body'], true) : null;
        $this->istekler[] = ['anahtar' => $anahtar, 'metod' => $i['method'], 'yol' => $yol, 'query' => $q, 'govde' => $govde, 'basliklar' => $i['headers'], 'url' => $i['url'], 'dogrula' => $i['dogrula'], 'timeout' => $i['timeout'], 'ham' => $i['body']];

        if ($this->ag) {
            return ['durum' => 0, 'govde' => '', 'hata' => 'Could not resolve host'];
        }
        if (isset($this->zorla[$anahtar])) {
            return $this->yanit($this->zorla[$anahtar][0], $this->zorla[$anahtar][1]);
        }
        // kimlik
        $beklenen = 'Authorization: gosp ' . self::KULLANICI . ':' . $this->anahtar;
        if (!in_array($beklenen, $i['headers'], true) || $this->anahtar !== self::ANAHTAR) {
            return $this->hata(401, 'geçersiz oturum');
        }
        // izin (Karar)
        if (!in_array($anahtar, self::$herZaman, true)) {
            if (!isset(self::$rotalar[$anahtar])) {
                return $this->hata(403, 'bu uç API anahtarıyla kullanılamaz');
            }
            [$grup, $erisim] = self::$rotalar[$anahtar];
            if ($anahtar === 'PUT /resellers/{id}/password' && $this->bayiParolaModu === '404') {
                return $this->hata(404, '404 page not found');
            }
            if ($anahtar === 'POST /resellers/{id}/sso' && $this->bayiSsoModu === '404') {
                return $this->hata(404, '404 page not found');
            }
            if ($anahtar === 'POST /domains/{id}/sso') {
                if ($this->ssoModu === '404') {
                    return $this->hata(404, '404 page not found');
                }
                if ($this->ssoModu === '403') {
                    return $this->hata(403, 'bu uç API anahtarıyla kullanılamaz');
                }
            }
            if (!$this->izinVar($grup, $erisim)) {
                return $this->hata(403, 'anahtarın bu işlem için izni yok');
            }
        }
        return $this->yonlendir($i['method'], $desen, $yol, $q, $govde);
    }

    private function izinVar(string $grup, string $erisim): bool
    {
        foreach ($this->izinler as $iz) {
            [$g, $e] = explode(':', $iz);
            if ($g === $grup && ($e === 'yaz' || $e === $erisim)) {
                return true;
            }
        }
        return false;
    }

    private function idAl(string $yol): int
    {
        preg_match('#/(\d+)#', $yol, $m);
        return (int) ($m[1] ?? 0);
    }

    private function yonlendir(string $m, string $desen, string $yol, array $q, $g): array
    {
        $k = $m . ' ' . $desen;
        switch ($k) {
            case 'GET /bayi-kaynak/{id}':
                return $this->bayiKaynak === null ? $this->hata(404, '404 page not found') : $this->yanit(200, $this->bayiKaynak);
            case 'GET /kapi/portlar':
                return $this->kapiPortlar === null ? $this->hata(404, '404 page not found') : $this->yanit(200, $this->kapiPortlar);
            case 'GET /kapi/adres':
                return $this->yanit(200, $this->kapiAdres === [] ? new \stdClass() : $this->kapiAdres);
            case 'GET /api-anahtari/ben':
                return $this->yanit(200, ['anahtar_id' => 1, 'ad' => 'whmcs', 'gosterim' => 'gosp_ABCDEFGHJKLM…EFG', 'rol' => 'admin', 'izinler' => $this->izinler, 'son_kullanma_at' => null]);
            case 'GET /plans':
                return $this->yanit(200, $this->plans);
            case 'GET /domains':
                return $this->yanit(200, array_values($this->domains));
            case 'GET /domains/{id}':
                $d = $this->domains[$this->idAl($yol)] ?? null;
                return $d ? $this->yanit(200, $d) : $this->hata(404, 'domain bulunamadı');
            case 'POST /domains':
                return $this->domainOlustur($g);
            case 'POST /domains/{id}/askiya-al':
            case 'POST /domains/{id}/askidan-al':
                $id = $this->idAl($yol);
                if (!isset($this->domains[$id])) {
                    return $this->hata(404, 'domain bulunamadı');
                }
                $askida = $m === 'POST' && strpos($yol, 'askiya-al') !== false;
                $this->domains[$id]['askida'] = $askida;
                return $this->yanit(200, ['ok' => true, 'id' => $id, 'alan_adi' => $this->domains[$id]['alan_adi'], 'askida' => $askida]);
            case 'DELETE /domains/{id}':
                $id = $this->idAl($yol);
                $d = $this->domains[$id] ?? null;
                if (!$d) {
                    return $this->hata(404, 'domain bulunamadı');
                }
                if (strtolower($q['onay'] ?? '') !== $d['alan_adi']) {
                    return $this->hata(400, 'onay metni alan adıyla eşleşmiyor');
                }
                unset($this->domains[$id]);
                return $this->yanit(200, ['ok' => true, 'silinen' => ['alan_adi' => $d['alan_adi'], 'sistem_kullanici' => $d['sistem_kullanici']]]);
            case 'PUT /domains/{id}/plan':
                $id = $this->idAl($yol);
                if (!isset($this->domains[$id])) {
                    return $this->hata(404, 'domain bulunamadı');
                }
                $this->domains[$id]['plan_id'] = $g['plan_id'];
                return $this->yanit(200, ['ok' => true, 'plan_id' => $g['plan_id']]);
            case 'PUT /domains/{id}/ftp/password':
                $id = $this->idAl($yol);
                if (!isset($this->domains[$id])) {
                    return $this->hata(404, 'domain bulunamadı');
                }
                $pw = $g['password'] ?? '';
                if (strlen($pw) < 12) {
                    return $this->hata(400, 'parola en az 12 karakter olmalı');
                }
                $this->domains[$id]['_ftp'] = $pw;
                return $this->yanit(200, ['id' => $id, 'alan_adi' => $this->domains[$id]['alan_adi'], 'kullanici' => '', 'username' => '', 'ok' => true, 'uretildi' => false]);
            case 'POST /domains/{id}/sso':
                $id = $this->idAl($yol);
                if (!isset($this->domains[$id])) {
                    return $this->hata(404, 'domain bulunamadı');
                }
                if ($this->ssoModu === '409' || !empty($this->domains[$id]['askida'])) {
                    return $this->hata(409, 'hesap askıda', 'hesap_askida');
                }
                if ($this->ssoModu === '429') {
                    return $this->hata(429, 'API anahtarı hız sınırı aşıldı');
                }
                $url = 'https://panel.example.test:8443/adina#kod=SSOKOD-ONCE-1234567890';
                if ($this->ssoModu === 'url_http') {
                    $url = 'http://panel.example.test:8443/adina#kod=SSOKOD-ONCE-1234567890';
                } elseif ($this->ssoModu === 'url_baska') {
                    $url = 'https://evil.example.org:8443/adina#kod=SSOKOD-ONCE-1234567890';
                } elseif ($this->ssoModu === 'url_user') {
                    $url = 'https://panel.example.test@evil.example.org/adina#kod=SSOKOD-ONCE-1234567890';
                } elseif ($this->ssoModu === 'url_alt') {
                    // 0.3.0-20261009: ana bilgisayar adi ayarlanmamis panel (server_name _)
                    $url = 'https://_:8443/adina#kod=SSOKOD-ONCE-1234567890';
                } elseif ($this->ssoModu === 'url_kodsuz') {
                    $url = 'https://panel.example.test:8443/adina';
                }
                return $this->yanit(200, ['ok' => true, 'url' => $url, 'gecerlilik_sn' => 60]);
            case 'PUT /resellers/{id}/password':
                $id = $this->idAl($yol);
                if (!isset($this->bayiler[$id])) {
                    return $this->hata(404, 'reseller bulunamadı');
                }
                $pw = (string) ($g['parola'] ?? '');
                if (strlen($pw) < 8 || strlen($pw) > 128) {
                    return $this->hata(400, 'parola 8-128 karakter olmalı');
                }
                $this->bayiler[$id]['_parola'] = $pw;
                return $this->yanit(200, ['ok' => true, 'id' => $id, 'kullanici' => $this->bayiler[$id]['kullanici']]);
            case 'POST /resellers/{id}/sso':
                if (!$this->izinVar('bayi', 'yaz')) {
                    return $this->hata(403, 'bayi paneline giriş için anahtarda "bayi" (yazma) izni de gerekir');
                }
                $id = $this->idAl($yol);
                if (!isset($this->bayiler[$id])) {
                    return $this->hata(404, 'bayi bulunamadı');
                }
                if (($this->bayiler[$id]['durum'] ?? '') !== 'active') {
                    return $this->hata(409, 'bayi hesabı askıda — askıdaki bayiye giriş yapılamaz');
                }
                $h = $this->bayiSsoModu === 'url_baska' ? 'evil.example.org' : 'panel.example.test';
                return $this->yanit(200, ['ok' => true, 'url' => 'https://' . $h . ':9444/sso#kod=BAYIKOD-ONCE-1234567890', 'gecerlilik_sn' => 60]);
            case 'GET /resellers':
                return $this->yanit(200, array_values($this->bayiler));
            case 'GET /reseller-plans':
                return $this->yanit(200, $this->paketler);
            case 'POST /resellers':
                foreach ($this->bayiler as $b) {
                    if ($b['kullanici'] === ($g['kullanici'] ?? '')) {
                        return $this->hata(409, 'bu kullanıcı adı zaten var');
                    }
                }
                if (strlen($g['parola'] ?? '') < 8) {
                    return $this->hata(400, 'parola 8-128 karakter olmalı');
                }
                $b = $this->bayiEkle($g['kullanici'], ['paket_id' => $g['paket_id'], 'ad_soyad' => $g['ad_soyad'] ?? '']);
                return $this->yanit(201, ['id' => $b['id'], 'kullanici' => $b['kullanici']]);
            case 'PUT /resellers/{id}':
                $id = $this->idAl($yol);
                if (!isset($this->bayiler[$id])) {
                    return $this->hata(404, 'reseller bulunamadı');
                }
                if (isset($g['durum']) && !in_array($g['durum'], ['active', 'suspended'], true)) {
                    return $this->hata(400, 'geçersiz durum');
                }
                $this->bayiler[$id] = array_merge($this->bayiler[$id], array_intersect_key($g, ['durum' => 1, 'paket_id' => 1]));
                return $this->yanit(200, ['ok' => true]);
            case 'DELETE /resellers/{id}':
                $id = $this->idAl($yol);
                if (!isset($this->bayiler[$id])) {
                    return $this->hata(404, 'bayi bulunamadı');
                }
                if ($this->bayiler[$id]['domain_sayisi'] > 0) {
                    return $this->hata(409, "önce bu reseller'ın hosting hesaplarını taşıyın/silin");
                }
                unset($this->bayiler[$id]);
                return $this->yanit(200, ['ok' => true]);
        }
        return $this->hata(404, 'rota yok: ' . $k);
    }

    private function domainOlustur($g): array
    {
        $alan = strtolower(trim($g['alan_adi'] ?? ''));
        if (!preg_match('/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/', $alan)) {
            return $this->hata(400, 'geçersiz alan adı');
        }
        foreach ($this->domains as $d) {
            if ($d['alan_adi'] === $alan) {
                return $this->hata(409, 'bu alan adı zaten kayıtlı');
            }
        }
        $plan = $g['plan_id'] ?? 1;
        $ek = ['php_surum' => $g['php_surum'] ?? '8.3'];
        if (isset($g['bayi_id'])) {
            // panel: yalniz kok + anahtarda bayi:yaz; olmayan bayi 400
            if (!$this->izinVar('bayi', 'yaz')) {
                return $this->hata(403, 'bayi_id için anahtarda bayi (yazma) izni de gerekir');
            }
            if (!isset($this->bayiler[(int) $g['bayi_id']])) {
                return $this->hata(400, 'bayi bulunamadı');
            }
            $ek += ['reseller_id' => (int) $g['bayi_id'], 'sahip_tur' => 'bayi', 'sahip_ad' => $this->bayiler[(int) $g['bayi_id']]['kullanici']];
            $this->bayiler[(int) $g['bayi_id']]['domain_sayisi']++;
        }
        $d = $this->domainEkle($alan, (int) $plan, $ek);
        if ($this->olusturmaGecikmeHatasi) {
            return $this->hata(504, 'gateway timeout');
        }
        $d['olusturulan_parolalar'] = ['ftp' => $this->ftpCikti, 'db' => !empty($g['sql_olustur']) ? 'DbPass#99887766xx' : ''];
        unset($d['_ftp']);
        return $this->yanit(201, $d);
    }
}
