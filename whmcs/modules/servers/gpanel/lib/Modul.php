<?php
/**
 * GirginOSPanel WHMCS modulu — is mantigi. gpanel.php'deki WHMCS islevleri buraya
 * devreder (test edilebilirlik icin sinif).
 *
 * Donus sozlesmesi: 'success' ya da hata dizgesi. Hicbir hata metni anahtar/parola
 * icermez (ApiIstemci hata metinlerini uretir, gunluk maskelenir).
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

use WHMCS\Database\Capsule;

final class ModulHatasi extends \RuntimeException
{
}

final class Modul
{
    public const MODUL_ADI = 'gpanel';

    // configoptionN siralamasi (gpanel_ConfigOptions anahtar sirasi ile AYNI olmali)
    public const SEC_TIP = 1;
    public const SEC_PLAN = 2;
    public const SEC_PHP = 3;
    public const SEC_PAKET = 4;
    public const SEC_SQL = 5;
    public const SEC_TLS_KAPALI = 6;
    public const SEC_PANEL_URL = 7;
    public const SEC_MEVCUT_BAGLA = 8;

    /** SSO ucunun izin grubu (ayri ajan; grup adi degisirse yalniz 403 mesajini etkiler). */
    public const SSO_IZIN_GRUBU = 'sso';

    /** UsageUpdate DB toplu guncelleme boyutu. */
    public const USAGE_PARCA = 200;

    // ── Secenekler / istemci ─────────────────────────────────────────────

    /**
     * @param array<string,mixed> $p
     * @return array{tip:string,plan:string,php:string,paket:string,sql:bool,tlsKapali:bool,panelUrl:string,mevcutBagla:bool}
     */
    public static function secenekler(array $p): array
    {
        $g = static function (int $n) use ($p): string {
            return trim((string) ($p['configoption' . $n] ?? ''));
        };
        $tip = strtolower($g(self::SEC_TIP));
        return [
            'tip' => $tip === 'bayi' ? Kayit::TUR_BAYI : Kayit::TUR_HOSTING,
            'plan' => $g(self::SEC_PLAN),
            'php' => $g(self::SEC_PHP),
            'paket' => $g(self::SEC_PAKET),
            'sql' => Yardimci::acik($g(self::SEC_SQL)),
            'tlsKapali' => Yardimci::acik($g(self::SEC_TLS_KAPALI)),
            'panelUrl' => $g(self::SEC_PANEL_URL),
            'mevcutBagla' => Yardimci::acik($g(self::SEC_MEVCUT_BAGLA)),
        ];
    }

    /** @param array<string,mixed> $p */
    public static function istemci(array $p, string $eylem): ApiIstemci
    {
        $o = self::secenekler($p);
        return ApiIstemci::paramsdan($p, $o['tlsKapali'])
            ->eylem($eylem)
            ->gunlukcu([self::class, 'gunluge']);
    }

    /**
     * logModuleCall — veri zaten maskeli; replaceVars BOS verilir (sir hic dolasmaz).
     *
     * @param array<string,mixed> $islenmis
     * @param string[] $gizliler
     */
    public static function gunluge(string $eylem, string $istek, string $yanit, array $islenmis, array $gizliler): void
    {
        if (function_exists('logModuleCall')) {
            \logModuleCall(self::MODUL_ADI, $eylem, $istek, $yanit, $islenmis, []);
        }
    }

    /** Ortak calistirici: ozel durumlari tek dizgeye cevirir. */
    private static function calistir(string $eylem, array $p, callable $f): string
    {
        try {
            $f();
            return 'success';
        } catch (ApiHatasi $e) {
            return $e->getMessage();
        } catch (ModulHatasi $e) {
            return $e->getMessage();
        } catch (\Throwable $e) {
            // Beklenmeyen: ayrinti gunluge (maskeli), kullaniciya kisa metin.
            self::gunluge($eylem, 'istisna', Yardimci::maskeleJson((string) json_encode(['hata' => get_class($e) . ': ' . $e->getMessage()])), ['http' => 0], []);
            return 'Modül hatası: ' . get_class($e) . ' (ayrıntı Modül Günlüğü’nde).';
        }
    }

    // ── Plan / paket eslemesi ────────────────────────────────────────────

    /** @param array<int,array<string,mixed>> $liste */
    private static function adIdCoz(array $liste, string $deger, string $nesne, string $secenekAdi): int
    {
        $deger = trim($deger);
        if ($deger === '') {
            throw new ModulHatasi("Ürün ayarında '" . $secenekAdi . "' seçilmemiş.");
        }
        if (ctype_digit($deger)) {
            foreach ($liste as $x) {
                if ((int) ($x['id'] ?? 0) === (int) $deger) {
                    return (int) $deger;
                }
            }
            throw new ModulHatasi($nesne . ' bulunamadı (id ' . $deger . "). Ürün ayarındaki '" . $secenekAdi . "' alanını kontrol edin.");
        }
        $es = [];
        foreach ($liste as $x) {
            if (mb_strtolower((string) ($x['ad'] ?? ''), 'UTF-8') === mb_strtolower($deger, 'UTF-8')) {
                $es[] = (int) $x['id'];
            }
        }
        if (count($es) === 1) {
            return $es[0];
        }
        if (count($es) > 1) {
            throw new ModulHatasi($nesne . " adı birden çok kayıtla eşleşiyor ('" . $deger . "'); id kullanın.");
        }
        $adlar = [];
        foreach ($liste as $x) {
            $adlar[] = (string) ($x['ad'] ?? '');
        }
        throw new ModulHatasi($nesne . " bulunamadı ('" . $deger . "'). Mevcut: " . implode(', ', array_slice($adlar, 0, 15)));
    }

    /** @param array<string,mixed> $o */
    private static function planId(ApiIstemci $c, array $o): int
    {
        return self::adIdCoz($c->planlar(), $o['plan'], 'Panel planı', 'Panel planı');
    }

    // ── Hesap kimligi ────────────────────────────────────────────────────

    /** Alan adini panel listesinde bulur. @return array<string,mixed>|null */
    private static function hesapBul(ApiIstemci $c, string $alan): ?array
    {
        $alan = strtolower(trim($alan));
        foreach ($c->hesaplar() as $h) {
            if (strtolower((string) ($h['alan_adi'] ?? '')) === $alan) {
                return $h;
            }
        }
        return null;
    }

    /** @return array<string,mixed>|null */
    private static function bayiBul(ApiIstemci $c, string $kullanici): ?array
    {
        $kullanici = strtolower(trim($kullanici));
        foreach ($c->bayiler() as $b) {
            if (strtolower((string) ($b['kullanici'] ?? '')) === $kullanici) {
                return $b;
            }
        }
        return null;
    }

    /**
     * Hizmetin panel hesap kimligi. Kayitli degilse ad uzerinden YENIDEN BULMA yalniz
     * (a) bu hizmet icin "olusturma niyeti" isareti (panel_id=0) varsa ya da
     * (b) 'mevcut hesabi bagla' secenegi aciksa yapilir; aksi halde baska musterinin
     * hesabina baglanmamak icin hata verilir.
     *
     * @param array<string,mixed> $p
     */
    private static function hesapId(array $p, ApiIstemci $c, string $tur): int
    {
        $sid = (int) ($p['serviceid'] ?? 0);
        $k = Kayit::oku($sid);
        if ($k !== null && $k['tur'] === $tur && $k['panel_id'] > 0) {
            return $k['panel_id'];
        }
        $o = self::secenekler($p);
        $niyet = $k !== null && $k['tur'] === $tur && $k['panel_id'] === 0;
        if (!$niyet && !$o['mevcutBagla']) {
            throw new ModulHatasi("Bu hizmet bir panel hesabına bağlı değil. Yönetici > Hizmet > 'Panel hesap kimliği' alanıyla bağlayın ya da ürün ayarında 'Mevcut hesabı bağla'yı açın.");
        }
        if ($tur === Kayit::TUR_HOSTING) {
            $ad = strtolower(trim((string) ($p['domain'] ?? '')));
            $h = $ad !== '' ? self::hesapBul($c, $ad) : null;
            $pid = $h ? (int) $h['id'] : 0;
        } else {
            $ad = strtolower(trim((string) ($p['username'] ?? '')));
            $h = $ad !== '' ? self::bayiBul($c, $ad) : null;
            $pid = $h ? (int) $h['id'] : 0;
        }
        if ($pid <= 0) {
            throw new ModulHatasi("Panelde '" . $ad . "' hesabı bulunamadı.");
        }
        if (Kayit::baskaHizmetteMi($pid, $tur, $sid)) {
            throw new ModulHatasi("Panel hesabı '" . $ad . "' başka bir WHMCS hizmetine bağlı.");
        }
        Kayit::yaz($sid, $pid, $tur, $ad);
        return $pid;
    }

    /**
     * WHMCS hizmetine panelden gelen kullanici adi / IP / parolayi yazar (localAPI).
     * Hata olursa olusturmayi bozmaz.
     *
     * @param array<string,mixed> $p
     */
    private static function hizmetiGuncelle(array $p, string $kullanici, string $ip, ?string $parola): void
    {
        if (!function_exists('localAPI')) {
            return;
        }
        $v = ['serviceid' => (int) $p['serviceid']];
        if ($kullanici !== '') {
            $v['serviceusername'] = $kullanici;
        }
        if ($ip !== '') {
            $v['dedicatedip'] = $ip;
        }
        if ($parola !== null && $parola !== '') {
            $v['servicepassword'] = $parola;
        }
        if (count($v) === 1) {
            return;
        }
        try {
            \localAPI('UpdateClientProduct', $v);
        } catch (\Throwable $e) {
            // sessiz: parola panelde dogru; yonetici elle esitleyebilir
        }
    }

    // ── CreateAccount ────────────────────────────────────────────────────

    /** @param array<string,mixed> $p */
    public static function olustur(array $p): string
    {
        return self::calistir('CreateAccount', $p, static function () use ($p) {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'CreateAccount');
            if ($o['tip'] === Kayit::TUR_BAYI) {
                self::bayiOlustur($p, $o, $c);
            } else {
                self::hostingOlustur($p, $o, $c);
            }
        });
    }

    /**
     * @param array<string,mixed> $p
     * @param array<string,mixed> $o
     */
    private static function hostingOlustur(array $p, array $o, ApiIstemci $c): void
    {
        $sid = (int) $p['serviceid'];
        $alan = strtolower(trim((string) ($p['domain'] ?? '')));
        if ($alan === '') {
            throw new ModulHatasi('Hizmette alan adı (domain) boş; hesap oluşturulamaz.');
        }
        $k = Kayit::oku($sid);
        if ($k !== null && $k['tur'] === Kayit::TUR_HOSTING && $k['panel_id'] > 0) {
            // Zaten bagli (yeniden deneme): idempotent.
            self::hizmetiBaglaVeEsitle($p, $c, $k['panel_id'], null);
            return;
        }
        $niyet = $k !== null && $k['tur'] === Kayit::TUR_HOSTING && $k['panel_id'] === 0;

        $mevcut = self::hesapBul($c, $alan);
        if ($mevcut !== null) {
            self::mevcutuBagla($p, $o, $c, $mevcut, $niyet);
            return;
        }

        $govde = ['alan_adi' => $alan];
        if ($o['plan'] !== '') { // bos: panelin varsayilan plani
            $govde['plan_id'] = self::planId($c, $o);
        }
        if ($o['php'] !== '') {
            $govde['php_surum'] = $o['php'];
        }
        if ($o['sql']) {
            $govde['sql_olustur'] = true;
        }

        Kayit::yaz($sid, 0, Kayit::TUR_HOSTING, $alan); // olusturma niyeti
        try {
            $y = $c->hesapOlustur($govde);
        } catch (ApiHatasi $e) {
            if ($e->durum === 409) {
                // Yaris: baska istek bu arada acmis olabilir -> yeniden bak.
                $m = self::hesapBul($c, $alan);
                if ($m !== null && ($niyet || $o['mevcutBagla'])) {
                    self::mevcutuBagla($p, $o, $c, $m, true);
                    return;
                }
            }
            if ($e->durum >= 400 && $e->durum < 500) {
                Kayit::sil($sid); // kesin ret: niyet isareti kalmasin
            }
            throw $e;
        }
        $id = (int) ($y['id'] ?? 0);
        if ($id <= 0) {
            throw new ModulHatasi('Panel hesap oluşturdu ama kimlik döndürmedi.');
        }
        Kayit::yaz($sid, $id, Kayit::TUR_HOSTING, $alan);
        $uretilen = (string) ($y['olusturulan_parolalar']['ftp'] ?? '');
        $c->gizliEkle($uretilen);
        self::parolaEsitle($p, $c, $id, $uretilen, (string) ($y['sistem_kullanici'] ?? ''), (string) ($y['ipv4'] ?? ''));
    }

    /**
     * @param array<string,mixed> $p
     * @param array<string,mixed> $o
     * @param array<string,mixed> $mevcut
     */
    private static function mevcutuBagla(array $p, array $o, ApiIstemci $c, array $mevcut, bool $niyet): void
    {
        $sid = (int) $p['serviceid'];
        if (!$niyet && !$o['mevcutBagla']) {
            throw new ModulHatasi("'" . ($mevcut['alan_adi'] ?? '') . "' alan adı panelde zaten var. Başka bir müşterinin hesabına bağlanmamak için otomatik bağlanmadı; ürün ayarında 'Mevcut hesabı bağla'yı açın ya da hizmette 'Panel hesap kimliği'ni elle girin.");
        }
        $id = (int) $mevcut['id'];
        if (Kayit::baskaHizmetteMi($id, Kayit::TUR_HOSTING, $sid)) {
            throw new ModulHatasi("Panelde '" . ($mevcut['alan_adi'] ?? '') . "' hesabı zaten başka bir WHMCS hizmetine bağlı.");
        }
        Kayit::yaz($sid, $id, Kayit::TUR_HOSTING, (string) $mevcut['alan_adi']);
        self::hizmetiBaglaVeEsitle($p, $c, $id, $mevcut);
    }

    /**
     * Baglanmis hesapta WHMCS parolasini panele yazmayi dener (en iyi caba).
     *
     * @param array<string,mixed> $p
     * @param array<string,mixed>|null $hesap
     */
    private static function hizmetiBaglaVeEsitle(array $p, ApiIstemci $c, int $id, ?array $hesap): void
    {
        if ($hesap === null) {
            try {
                $hesap = $c->hesap($id);
            } catch (ApiHatasi $e) {
                $hesap = [];
            }
        }
        $pw = (string) ($p['password'] ?? '');
        $son = null;
        if ($pw !== '' && Yardimci::parolaHatasi($pw) === null) {
            try {
                $c->ftpParola($id, $pw);
                $son = $pw;
            } catch (ApiHatasi $e) {
                $son = null; // izin yok vb.: mevcut parola degismeden kalir
            }
        }
        self::hizmetiGuncelle($p, (string) ($hesap['sistem_kullanici'] ?? ''), (string) ($hesap['ipv4'] ?? ''), $son);
    }

    /**
     * Yeni hesapta WHMCS parolasini panele uygular; olmazsa panelin urettigini WHMCS'e yazar.
     *
     * @param array<string,mixed> $p
     */
    private static function parolaEsitle(array $p, ApiIstemci $c, int $id, string $uretilen, string $sk, string $ip): void
    {
        $pw = (string) ($p['password'] ?? '');
        $son = $uretilen;
        if ($pw !== '' && Yardimci::parolaHatasi($pw) === null) {
            try {
                $c->ftpParola($id, $pw);
                $son = $pw;
            } catch (ApiHatasi $e) {
                $son = $uretilen; // 'ftp.parola' izni yok: panel parolasini WHMCS'e yaz
            }
        }
        self::hizmetiGuncelle($p, $sk, $ip, $son);
    }

    /**
     * @param array<string,mixed> $p
     * @param array<string,mixed> $o
     */
    private static function bayiOlustur(array $p, array $o, ApiIstemci $c): void
    {
        $sid = (int) $p['serviceid'];
        $kullanici = strtolower(trim((string) ($p['username'] ?? '')));
        if (!preg_match('/^[a-z0-9][a-z0-9._-]{2,31}$/', $kullanici) || $kullanici === 'root') {
            throw new ModulHatasi("Bayi kullanıcı adı geçersiz ('" . $kullanici . "'): 3-32 karakter, küçük harf/rakam/._- olmalı (Hizmet > Kullanıcı adı).");
        }
        $pw = (string) ($p['password'] ?? '');
        if (strlen($pw) < 8 || strlen($pw) > 128) {
            throw new ModulHatasi('Bayi parolası 8-128 karakter olmalı.');
        }
        $k = Kayit::oku($sid);
        if ($k !== null && $k['tur'] === Kayit::TUR_BAYI && $k['panel_id'] > 0) {
            return; // idempotent
        }
        $niyet = $k !== null && $k['tur'] === Kayit::TUR_BAYI && $k['panel_id'] === 0;
        $mevcut = self::bayiBul($c, $kullanici);
        if ($mevcut !== null) {
            if (!$niyet && !$o['mevcutBagla']) {
                throw new ModulHatasi("'" . $kullanici . "' bayisi panelde zaten var; otomatik bağlanmadı ('Mevcut hesabı bağla' seçeneği kapalı).");
            }
            if (Kayit::baskaHizmetteMi((int) $mevcut['id'], Kayit::TUR_BAYI, $sid)) {
                throw new ModulHatasi("Bayi '" . $kullanici . "' başka bir WHMCS hizmetine bağlı.");
            }
            Kayit::yaz($sid, (int) $mevcut['id'], Kayit::TUR_BAYI, $kullanici);
            return;
        }
        $paketId = self::adIdCoz($c->bayiPaketleri(), $o['paket'], 'Bayi paketi', 'Bayi paketi');
        $ad = trim((string) ($p['clientsdetails']['fullname'] ?? ''));
        if ($ad === '') {
            $ad = trim(((string) ($p['clientsdetails']['firstname'] ?? '')) . ' ' . ((string) ($p['clientsdetails']['lastname'] ?? '')));
        }
        Kayit::yaz($sid, 0, Kayit::TUR_BAYI, $kullanici);
        try {
            $y = $c->bayiOlustur(['paket_id' => $paketId, 'kullanici' => $kullanici, 'parola' => $pw, 'ad_soyad' => $ad]);
        } catch (ApiHatasi $e) {
            if ($e->durum >= 400 && $e->durum < 500) {
                Kayit::sil($sid);
            }
            throw $e;
        }
        $id = (int) ($y['id'] ?? 0);
        if ($id <= 0) {
            throw new ModulHatasi('Panel bayiyi oluşturdu ama kimlik döndürmedi.');
        }
        Kayit::yaz($sid, $id, Kayit::TUR_BAYI, $kullanici);
    }

    // ── Askiya al / ac / sil ─────────────────────────────────────────────

    /** @param array<string,mixed> $p */
    public static function askiyaAl(array $p): string
    {
        return self::calistir('SuspendAccount', $p, static function () use ($p) {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'SuspendAccount');
            $id = self::hesapId($p, $c, $o['tip']);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                $c->bayiGuncelle($id, ['durum' => 'suspended']);
            } else {
                $c->askiyaAl($id);
            }
        });
    }

    /** @param array<string,mixed> $p */
    public static function askidanAl(array $p): string
    {
        return self::calistir('UnsuspendAccount', $p, static function () use ($p) {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'UnsuspendAccount');
            $id = self::hesapId($p, $c, $o['tip']);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                $c->bayiGuncelle($id, ['durum' => 'active']);
            } else {
                $c->askidanAl($id);
            }
        });
    }

    /** @param array<string,mixed> $p */
    public static function sil(array $p): string
    {
        return self::calistir('TerminateAccount', $p, static function () use ($p) {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'TerminateAccount');
            $sid = (int) $p['serviceid'];
            $id = self::hesapId($p, $c, $o['tip']);
            try {
                if ($o['tip'] === Kayit::TUR_BAYI) {
                    $c->bayiSil($id);
                } else {
                    $k = Kayit::oku($sid);
                    $alan = strtolower(trim((string) (($k['ad'] ?? '') !== '' ? $k['ad'] : ($p['domain'] ?? ''))));
                    $c->hesapSil($id, $alan);
                }
            } catch (ApiHatasi $e) {
                if ($e->durum !== 404) {
                    throw $e;
                }
                // Zaten yok: silinmis say (idempotent).
            }
            Kayit::sil($sid);
        });
    }

    // ── Paket / parola ───────────────────────────────────────────────────

    /** @param array<string,mixed> $p */
    public static function paketDegistir(array $p): string
    {
        return self::calistir('ChangePackage', $p, static function () use ($p) {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'ChangePackage');
            $id = self::hesapId($p, $c, $o['tip']);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                $paket = self::adIdCoz($c->bayiPaketleri(), $o['paket'], 'Bayi paketi', 'Bayi paketi');
                $c->bayiGuncelle($id, ['paket_id' => $paket]);
            } else {
                // plan_id null panelde "plani kaldir" demek: bos secimde ASLA gonderme.
                $c->planDegistir($id, self::planId($c, $o));
            }
        });
    }

    /** @param array<string,mixed> $p */
    public static function parolaDegistir(array $p): string
    {
        return self::calistir('ChangePassword', $p, static function () use ($p) {
            $o = self::secenekler($p);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                throw new ModulHatasi('Bayi hesabının parolası panel API anahtarıyla değiştirilemez (panel kısıtı). Bayi parolayı panelden değiştirmeli.');
            }
            $pw = (string) ($p['password'] ?? '');
            $h = Yardimci::parolaHatasi($pw);
            if ($h !== null) {
                throw new ModulHatasi($h);
            }
            $c = self::istemci($p, 'ChangePassword');
            $c->ftpParola(self::hesapId($p, $c, Kayit::TUR_HOSTING), $pw);
        });
    }

    // ── Baglanti testi ───────────────────────────────────────────────────

    /**
     * @param array<string,mixed> $p
     * @return array{success:bool,error:string}
     */
    public static function baglantiTesti(array $p): array
    {
        try {
            $c = ApiIstemci::paramsdan($p, false)->eylem('TestConnection')->gunlukcu([self::class, 'gunluge']);
            $b = $c->ben();
            $izinler = isset($b['izinler']) && is_array($b['izinler']) ? $b['izinler'] : [];
            $eksik = [];
            if (!self::izinVar($izinler, 'hesap', 'yaz')) {
                $eksik[] = 'hesap:yaz';
            }
            if (!self::izinVar($izinler, 'plan', 'oku')) {
                $eksik[] = 'plan:oku';
            }
            if ($eksik) {
                return ['success' => false, 'error' => 'Bağlantı kuruldu ama API anahtarında izin eksik: ' . implode(', ', $eksik) . ' (isteğe bağlı: ftp.parola:yaz, sso, bayi).'];
            }
            return ['success' => true, 'error' => ''];
        } catch (ApiHatasi $e) {
            return ['success' => false, 'error' => $e->getMessage()];
        } catch (\Throwable $e) {
            return ['success' => false, 'error' => 'Modül hatası: ' . get_class($e)];
        }
    }

    /** @param string[] $izinler ["grup:erisim", ...] */
    public static function izinVar(array $izinler, string $grup, string $erisim): bool
    {
        foreach ($izinler as $iz) {
            $parca = explode(':', (string) $iz, 2);
            if (($parca[0] ?? '') === $grup && (($parca[1] ?? '') === 'yaz' || ($parca[1] ?? '') === $erisim)) {
                return true;
            }
        }
        return false;
    }

    // ── SSO ──────────────────────────────────────────────────────────────

    /**
     * @param array<string,mixed> $p
     * @return array{success:bool,redirectTo?:string,errorMsg?:string}
     */
    public static function tekTikGiris(array $p, bool $musteri): array
    {
        $eylem = $musteri ? 'ServiceSingleSignOn' : 'AdminSingleSignOn';
        try {
            $o = self::secenekler($p);
            $c = self::istemci($p, $eylem);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                // Bayi icin SSO ucu yok: duz panel baglantisi.
                return self::duzBaglanti($p, $o, $musteri, 'Bayi hesabı için tek tıkla giriş ucu yok.');
            }
            $id = self::hesapId($p, $c, Kayit::TUR_HOSTING);
            try {
                $y = $c->sso($id);
            } catch (ApiHatasi $e) {
                // Uc yok (404) ya da anahtarda izin yok (403): musteri alaninda duz baglantiya dus.
                $ucYok = $e->durum === 404 || ($e->durum === 403 && $e->kod === '');
                if ($musteri && $ucYok) {
                    return self::duzBaglanti($p, $o, true, $e->getMessage());
                }
                return ['success' => false, 'errorMsg' => $e->getMessage()];
            }
            $url = (string) ($y['url'] ?? '');
            $hata = self::ssoUrlHatasi($url, $p, $o);
            if ($hata !== null) {
                return ['success' => false, 'errorMsg' => $hata];
            }
            return ['success' => true, 'redirectTo' => $url];
        } catch (ApiHatasi $e) {
            return ['success' => false, 'errorMsg' => $e->getMessage()];
        } catch (ModulHatasi $e) {
            return ['success' => false, 'errorMsg' => $e->getMessage()];
        } catch (\Throwable $e) {
            return ['success' => false, 'errorMsg' => 'Modül hatası: ' . get_class($e)];
        }
    }

    /**
     * SSO adresi: https, kullanici bilgisi yok, ana bilgisayar sunucu ya da paneli adresiyle ayni.
     *
     * @param array<string,mixed> $p
     * @param array<string,mixed> $o
     */
    private static function ssoUrlHatasi(string $url, array $p, array $o): ?string
    {
        $u = parse_url($url);
        if ($url === '' || $u === false || ($u['scheme'] ?? '') !== 'https' || isset($u['user']) || isset($u['pass']) || empty($u['host'])) {
            return 'Panel geçerli bir giriş adresi döndürmedi.';
        }
        $izinli = [];
        foreach ([(string) ($p['serverhostname'] ?? ''), (string) ($p['serverip'] ?? '')] as $h) {
            if ($h !== '') {
                $izinli[] = strtolower(trim($h, '[]'));
            }
        }
        if ($o['panelUrl'] !== '') {
            $pu = parse_url($o['panelUrl']);
            if ($pu && !empty($pu['host'])) {
                $izinli[] = strtolower(trim($pu['host'], '[]'));
            }
        }
        if (!in_array(strtolower(trim($u['host'], '[]')), $izinli, true)) {
            return 'Panelin döndürdüğü giriş adresi beklenen ana bilgisayara ait değil (güvenlik nedeniyle reddedildi).';
        }
        return null;
    }

    /**
     * @param array<string,mixed> $p
     * @param array<string,mixed> $o
     * @return array{success:bool,redirectTo?:string,errorMsg?:string}
     */
    private static function duzBaglanti(array $p, array $o, bool $musteri, string $neden): array
    {
        $url = self::panelUrl($p, $o);
        if (!$musteri || $url === '') {
            return ['success' => false, 'errorMsg' => $neden];
        }
        return ['success' => true, 'redirectTo' => $url];
    }

    /** Duz panel adresi: urundeki 'Panel adresi' ya da sunucu host:port. @param array<string,mixed> $p @param array<string,mixed> $o */
    public static function panelUrl(array $p, array $o): string
    {
        if ($o['panelUrl'] !== '') {
            $u = parse_url($o['panelUrl']);
            if ($u && ($u['scheme'] ?? '') === 'https' && !empty($u['host']) && !isset($u['user']) && !isset($u['pass'])) {
                return $o['panelUrl'];
            }
            return '';
        }
        if ($o['tip'] === Kayit::TUR_BAYI) {
            return ''; // bayi kapisi ayri port: adres elle girilmeli
        }
        $host = trim((string) ($p['serverhostname'] ?? '')) ?: trim((string) ($p['serverip'] ?? ''));
        $port = (int) ($p['serverport'] ?? 0);
        return Yardimci::hostGecerli($host) && $port > 0 ? Yardimci::tabanUrl($host, $port) . '/' : '';
    }

    // ── Kullanim guncelleme ──────────────────────────────────────────────

    /**
     * Sunucudaki tum hizmetlerin disk/trafik kullanimini TEK liste cagrisiyla gunceller.
     *
     * @param array<string,mixed> $p
     */
    public static function kullanimGuncelle(array $p): string
    {
        try {
            $c = ApiIstemci::paramsdan($p, false)->eylem('UsageUpdate')->gunlukcu([self::class, 'gunluge']);
            // Urun duzeyi 'TLS kapali' secenegi sunucu duzeyinde yok: accesshash ile kapatilabilir.
            $satirlar = Capsule::table('tblhosting')
                ->where('server', (int) ($p['serverid'] ?? 0))
                ->whereIn('domainstatus', ['Active', 'Suspended'])
                ->orderBy('id')
                ->get(['id']);
            $idler = [];
            foreach ($satirlar as $s) {
                $idler[] = (int) (is_object($s) ? $s->id : $s['id']);
            }
            if (!$idler) {
                return 'success';
            }
            Kayit::hazirla();
            $kayitlar = [];
            foreach (array_chunk($idler, self::USAGE_PARCA) as $parca) {
                foreach (Capsule::table(Kayit::TABLO)->whereIn('serviceid', $parca)->get() as $k) {
                    $k = (array) $k;
                    if ((int) $k['panel_id'] > 0) {
                        $kayitlar[(int) $k['serviceid']] = [(int) $k['panel_id'], (string) $k['tur']];
                    }
                }
            }
            if (!$kayitlar) {
                return 'success';
            }
            $hesaplar = [];
            $bayiler = [];
            $hata = '';
            $hostingVar = false;
            $bayiVar = false;
            foreach ($kayitlar as $v) {
                if ($v[1] === Kayit::TUR_BAYI) {
                    $bayiVar = true;
                } else {
                    $hostingVar = true;
                }
            }
            if ($hostingVar) {
                foreach ($c->hesaplar() as $h) {
                    $hesaplar[(int) ($h['id'] ?? 0)] = $h;
                }
            }
            if ($bayiVar) {
                try {
                    foreach ($c->bayiler() as $b) {
                        $bayiler[(int) ($b['id'] ?? 0)] = $b;
                    }
                } catch (ApiHatasi $e) {
                    $hata = $e->getMessage(); // hosting guncellemesi bu yuzden durmaz
                }
            }
            $simdi = date('Y-m-d H:i:s');
            $basarisiz = 0;
            foreach (array_chunk(array_keys($kayitlar), self::USAGE_PARCA) as $parca) {
                foreach ($parca as $sid) {
                    [$pid, $tur] = $kayitlar[$sid];
                    $g = null;
                    if ($tur === Kayit::TUR_BAYI && isset($bayiler[$pid])) {
                        $b = $bayiler[$pid];
                        $g = [
                            'diskusage' => Yardimci::kbToMb($b['disk_kullanim_kb'] ?? 0),
                            'disklimit' => max(0, (int) ($b['max_disk_mb'] ?? 0)),
                            'bwlimit' => max(0, (int) ($b['max_trafik_mb'] ?? 0)),
                            'lastupdate' => $simdi,
                        ];
                    } elseif ($tur === Kayit::TUR_HOSTING && isset($hesaplar[$pid])) {
                        $g = self::hesaptanKullanim($hesaplar[$pid], $simdi);
                    }
                    if ($g === null) {
                        continue;
                    }
                    try {
                        Capsule::table('tblhosting')->where('id', $sid)->update($g);
                    } catch (\Throwable $e) {
                        $basarisiz++;
                    }
                }
            }
            if ($basarisiz > 0) {
                return $basarisiz . ' hizmetin kullanımı veritabanına yazılamadı.';
            }
            return $hata === '' ? 'success' : 'Hosting kullanımı güncellendi; bayi kullanımı alınamadı: ' . $hata;
        } catch (ApiHatasi $e) {
            return $e->getMessage();
        } catch (\Throwable $e) {
            return 'Modül hatası: ' . get_class($e);
        }
    }

    /**
     * Panel Domain nesnesi -> tblhosting kullanim alanlari (MB).
     *
     * @param array<string,mixed> $h
     * @return array<string,mixed>
     */
    public static function hesaptanKullanim(array $h, string $simdi): array
    {
        return [
            'diskusage' => Yardimci::kbToMb($h['boyut_kb'] ?? 0),
            'disklimit' => max(0, (int) ($h['disk_kota_mb'] ?? 0)),
            'bwusage' => Yardimci::kbToMb($h['trafik_kb'] ?? 0),
            'bwlimit' => max(0, (int) ($h['trafik_kota_mb'] ?? 0)),
            'lastupdate' => $simdi,
        ];
    }

    // ── Yonetici sekmesi ─────────────────────────────────────────────────

    /**
     * @param array<string,mixed> $p
     * @return array<string,string>
     */
    public static function yoneticiAlanlari(array $p): array
    {
        $k = Kayit::oku((int) $p['serviceid']);
        $deger = $k !== null && $k['panel_id'] > 0 ? (string) $k['panel_id'] : '';
        $durum = $k === null ? 'bağlı değil' : ($k['panel_id'] > 0 ? ($k['tur'] === Kayit::TUR_BAYI ? 'bayi' : 'hosting') . ' hesabı' : 'oluşturma bekliyor/yarım');
        return [
            'Panel hesap kimliği' => '<input type="text" name="gpanel_panel_id" value="' . Yardimci::esc($deger) . '" size="10" pattern="[0-9]*" />'
                . ' <small>Boş bırakmak bağı değiştirmez. Panelde var olan hesabı elle bağlamak için kimliği girin (doğrulanır).</small>',
            'Panel bağlantı durumu' => Yardimci::esc($durum),
        ];
    }

    /**
     * Kaydet: kimlik panelde dogrulanir (alan adi/kullanici uyusmali). Hata exception
     * FIRLATMAZ; Etkinlik Gunlugu'ne yazilir.
     *
     * @param array<string,mixed> $p
     */
    public static function yoneticiKaydet(array $p): void
    {
        $yeni = isset($_POST['gpanel_panel_id']) ? trim((string) $_POST['gpanel_panel_id']) : '';
        $sid = (int) $p['serviceid'];
        $mevcut = Kayit::oku($sid);
        if ($yeni === '' || ($mevcut !== null && (string) $mevcut['panel_id'] === $yeni)) {
            return;
        }
        $etkinlik = static function (string $m) use ($sid): void {
            if (function_exists('logActivity')) {
                \logActivity('GirginOSPanel: hizmet #' . $sid . ' bağlanamadı: ' . $m);
            }
        };
        if (!ctype_digit($yeni) || (int) $yeni <= 0) {
            $etkinlik('panel hesap kimliği sayı olmalı');
            return;
        }
        try {
            $o = self::secenekler($p);
            $c = self::istemci($p, 'AdminServicesTabFieldsSave');
            $id = (int) $yeni;
            if ($o['tip'] === Kayit::TUR_BAYI) {
                $ad = strtolower(trim((string) ($p['username'] ?? '')));
                $bulundu = null;
                foreach ($c->bayiler() as $b) {
                    if ((int) ($b['id'] ?? 0) === $id) {
                        $bulundu = $b;
                    }
                }
                if ($bulundu === null || strtolower((string) $bulundu['kullanici']) !== $ad) {
                    $etkinlik('bayi kimliği/kullanıcı adı panelle uyuşmuyor');
                    return;
                }
            } else {
                $ad = strtolower(trim((string) ($p['domain'] ?? '')));
                $h = $c->hesap($id);
                if (strtolower((string) ($h['alan_adi'] ?? '')) !== $ad) {
                    $etkinlik('panel hesabının alan adı hizmetteki alan adıyla uyuşmuyor');
                    return;
                }
            }
            if (Kayit::baskaHizmetteMi($id, $o['tip'], $sid)) {
                $etkinlik('panel hesabı başka bir hizmete bağlı');
                return;
            }
            Kayit::yaz($sid, $id, $o['tip'], $ad);
        } catch (\Throwable $e) {
            $etkinlik($e instanceof ApiHatasi ? $e->getMessage() : get_class($e));
        }
    }

    // ── Musteri alani ────────────────────────────────────────────────────

    /**
     * @param array<string,mixed> $p
     * @return array{templatefile:string,vars:array<string,mixed>}
     */
    public static function musteriAlani(array $p): array
    {
        $dil = (string) ($p['clientsdetails']['language'] ?? '');
        $o = self::secenekler($p);
        $vars = [
            'L' => Yardimci::dil($dil),
            'tur' => $o['tip'],
            'hata' => '',
            'hesap' => null,
            'disk' => null,
            'trafik' => null,
            'cubuklar' => [],
            'serviceid' => (int) ($p['serviceid'] ?? 0),
            'panel_url' => self::panelUrl($p, $o),
            'token' => function_exists('generate_token') ? (string) \generate_token('plain') : '',
        ];
        try {
            $c = self::istemci($p, 'ClientArea');
            $id = self::hesapId($p, $c, $o['tip']);
            if ($o['tip'] === Kayit::TUR_BAYI) {
                foreach ($c->bayiler() as $b) {
                    if ((int) ($b['id'] ?? 0) === $id) {
                        $vars['hesap'] = [
                            'ad' => (string) $b['kullanici'], 'durum' => (string) ($b['durum'] ?? ''),
                            'plan' => (string) ($b['paket_ad'] ?? ''),
                            'alan_sayisi' => (int) ($b['domain_sayisi'] ?? 0), 'max_domain' => (int) ($b['max_domain'] ?? 0),
                        ];
                        $vars['disk'] = self::cubuk(Yardimci::kbToMb($b['disk_kullanim_kb'] ?? 0), (int) ($b['max_disk_mb'] ?? 0));
                        $vars['trafik'] = self::cubuk(0, (int) ($b['max_trafik_mb'] ?? 0));
                    }
                }
            } else {
                $h = $c->hesap($id);
                $vars['hesap'] = [
                    'ad' => (string) ($h['alan_adi'] ?? ''), 'durum' => !empty($h['askida']) ? 'askida' : (string) ($h['durum'] ?? ''),
                    'plan' => (string) ($h['plan_ad'] ?? ''), 'php' => (string) ($h['php_surum'] ?? ''),
                    'ftp_host' => (string) ($h['ftp_host'] ?? ''), 'ftp_user' => (string) ($h['ftp_user'] ?? ''),
                    'ipv4' => (string) ($h['ipv4'] ?? ''),
                ];
                $vars['disk'] = self::cubuk(Yardimci::kbToMb($h['boyut_kb'] ?? 0), (int) ($h['disk_kota_mb'] ?? 0));
                $vars['trafik'] = self::cubuk(Yardimci::kbToMb($h['trafik_kb'] ?? 0), (int) ($h['trafik_kota_mb'] ?? 0));
            }
        } catch (ApiHatasi $e) {
            $vars['hata'] = $e->getMessage();
        } catch (ModulHatasi $e) {
            $vars['hata'] = $e->getMessage();
        } catch (\Throwable $e) {
            $vars['hata'] = 'Hesap bilgisi alınamadı.';
        }
        $vars['cubuklar'] = [];
        foreach (['disk', 'trafik'] as $ad) {
            if (is_array($vars[$ad])) {
                $vars['cubuklar'][] = ['ad' => $ad] + $vars[$ad];
            }
        }
        return ['templatefile' => 'templates/clientarea', 'vars' => $vars];
    }

    /** @return array{kullanim:int,limit:int,yuzde:int} */
    public static function cubuk(int $kullanim, int $limit): array
    {
        $y = $limit > 0 ? (int) min(100, round($kullanim * 100 / $limit)) : 0;
        return ['kullanim' => $kullanim, 'limit' => $limit, 'yuzde' => $y];
    }

    /**
     * Musteri formu (POST): yeni parola + tekrar. serviceid YALNIZ WHMCS'in verdigi $params'tan.
     *
     * @param array<string,mixed> $p
     */
    public static function musteriParolaDegistir(array $p): string
    {
        if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'POST') {
            return 'Geçersiz istek.';
        }
        if (function_exists('check_token')) {
            \check_token('WHMCS.default'); // CSRF: WHMCS jetonu (basarisizsa WHMCS keser)
        }
        $a = (string) ($_POST['yeni_parola'] ?? '');
        $b = (string) ($_POST['yeni_parola2'] ?? '');
        if ($a !== $b) {
            return 'Parolalar eşleşmiyor.';
        }
        $p['password'] = $a;
        $s = self::parolaDegistir($p);
        if ($s === 'success') {
            self::hizmetiGuncelle($p, '', '', $a);
        }
        return $s;
    }

    /**
     * Musteri: bu hizmetin kullanimini panelden yenile.
     *
     * @param array<string,mixed> $p
     */
    public static function musteriKullanimYenile(array $p): string
    {
        return self::calistir('ClientAreaUsage', $p, static function () use ($p) {
            $o = self::secenekler($p);
            if ($o['tip'] !== Kayit::TUR_HOSTING) {
                throw new ModulHatasi('Bu ürün tipi için kullanım yenileme yok.');
            }
            $c = self::istemci($p, 'ClientAreaUsage');
            $h = $c->hesap(self::hesapId($p, $c, Kayit::TUR_HOSTING));
            Capsule::table('tblhosting')->where('id', (int) $p['serviceid'])->update(self::hesaptanKullanim($h, date('Y-m-d H:i:s')));
        });
    }
}
