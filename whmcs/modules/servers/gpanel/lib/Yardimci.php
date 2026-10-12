<?php
/**
 * GirginOSPanel WHMCS modulu — yardimci islevler (maskeleme, parola, adres, dil).
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

final class Yardimci
{
    public const SURUM = '1.0.3';

    /** Degeri HER ZAMAN maskelenen alan adlari (kucuk harf). */
    private const GIZLI_ANAHTARLAR = [
        'password', 'parola', 'authorization', 'anahtar', 'apikey', 'api_key',
        'serverpassword', 'secret', 'token', 'servicepassword', 'yeni_parola',
        'olusturulan_parolalar', 'accesshash', 'serveraccesshash',
    ];

    /**
     * Veri icindeki gizli degerleri toplar (gizli anahtar adli alanlarin metin
     * degerleri). Dizgede degeri her yerde aramak icin kullanilir.
     *
     * @param mixed $veri
     * @return string[]
     */
    public static function gizliDegerleriTopla($veri, bool $icerde = false): array
    {
        $out = [];
        if (is_array($veri)) {
            foreach ($veri as $k => $v) {
                $gizli = $icerde || (is_string($k) && in_array(strtolower($k), self::GIZLI_ANAHTARLAR, true));
                if ($gizli && is_scalar($v) && (string) $v !== '') {
                    $out[] = (string) $v;
                } elseif (is_array($v)) {
                    $out = array_merge($out, self::gizliDegerleriTopla($v, $gizli));
                }
            }
        }
        return $out;
    }

    /**
     * Anahtar/parola/jeton maskeleme. $gizliler: ayrica her yerde aranip
     * degistirilecek duz metin sirlar (API anahtari, WHMCS parolasi ...).
     *
     * @param mixed $veri
     * @param string[] $gizliler
     * @return mixed
     */
    public static function maskele($veri, array $gizliler = [])
    {
        $gizliler = array_values(array_filter(
            array_merge($gizliler, self::gizliDegerleriTopla($veri)),
            static function ($s) {
                return is_string($s) && strlen($s) >= 4;
            }
        ));
        // Uzun olan once (kismi eslesme artigi kalmasin).
        usort($gizliler, static function ($a, $b) {
            return strlen($b) <=> strlen($a);
        });
        return self::maskeleIc($veri, $gizliler, false);
    }

    /**
     * @param mixed $veri
     * @param string[] $gizliler
     * @return mixed
     */
    private static function maskeleIc($veri, array $gizliler, bool $gizliAlan)
    {
        if (is_array($veri)) {
            $out = [];
            foreach ($veri as $k => $v) {
                if (is_string($k) && in_array(strtolower($k), self::GIZLI_ANAHTARLAR, true)) {
                    $out[$k] = '***';
                    continue;
                }
                $out[$k] = self::maskeleIc($v, $gizliler, false);
            }
            return $out;
        }
        if (is_string($veri)) {
            foreach ($gizliler as $s) {
                $veri = str_replace($s, '***', $veri);
            }
            // SSO tek kullanimlik kodu URL parcasinda (#kod).
            $veri = preg_replace('/(https?:\/\/[^\s"\'#]*)#[^\s"\']+/i', '$1#***', $veri) ?? $veri;
            // "gosp kullanici:anahtar" basligi.
            $veri = preg_replace('/\bgosp\s+([^\s:"]+):\S+/i', 'gosp $1:***', $veri) ?? $veri;
            $veri = preg_replace('/\bgosp_[A-Za-z0-9]{6,}_[A-Za-z0-9_-]{10,}/', 'gosp_***', $veri) ?? $veri;
            return $veri;
        }
        return $veri;
    }

    /** JSON dizgesini maskeler (cozulemezse dizge olarak maskeler). */
    public static function maskeleJson(string $json, array $gizliler = []): string
    {
        $d = json_decode($json, true);
        if (is_array($d)) {
            $m = self::maskele($d, $gizliler);
            return (string) json_encode($m, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
        }
        return (string) self::maskele($json, $gizliler);
    }

    /**
     * Panelin parola kurali (internal/hesaplar ParolaGucluMu + api_parola.go):
     * en az 12, en cok 128 karakter, en az bir harf ve bir rakam, satir sonu yok.
     * Gecerliyse null, degilse neden (Ceviri: yonetici/sistem dilinde).
     */
    public static function parolaHatasi(string $pw): ?string
    {
        if ($pw === '' || preg_match('/[\r\n\x00]/', $pw)) {
            return Ceviri::t('Parola boş olamaz ve satır sonu/kontrol karakteri içeremez.');
        }
        $uz = function_exists('mb_strlen') ? mb_strlen($pw, 'UTF-8') : strlen($pw);
        if ($uz < 12) {
            return Ceviri::t('Parola en az 12 karakter olmalı.');
        }
        if ($uz > 128) {
            return Ceviri::t('Parola en çok 128 karakter olmalı.');
        }
        if (!preg_match('/[A-Za-z]/', $pw) || !preg_match('/[0-9]/', $pw)) {
            return Ceviri::t('Parola harf ve rakam içermeli.');
        }
        return null;
    }

    /** Modulun varsayilan portu (MetaData DefaultSSLPort): basit "Add Server" ekraninda Port alani yok. */
    public const VARSAYILAN_PORT = 8443;

    /**
     * Access Hash secenekleri (satir / virgul / noktali virgul / bosluk ile ayrilir):
     *   port=30409            panel yonetici kapisi portu
     *   tls-dogrulama-kapali  TLS dogrulamasini kapat (tehlikeli)
     * Bilinmeyen parcalar YOK SAYILIR ve hicbir yere yazilmaz (alan gizli sayilir).
     *
     * @return array{port:int,tlsKapali:bool}
     */
    public static function erisimSecenekleri(string $ah): array
    {
        $out = ['port' => 0, 'tlsKapali' => false];
        foreach (preg_split('/[\s,;]+/', trim($ah)) ?: [] as $t) {
            if (strtolower($t) === 'tls-dogrulama-kapali') {
                $out['tlsKapali'] = true;
            } elseif (preg_match('/^port=(\d{1,5})$/i', $t, $m) && (int) $m[1] >= 1 && (int) $m[1] <= 65535) {
                $out['port'] = (int) $m[1];
            }
        }
        return $out;
    }

    /**
     * Sunucu adresi (host + port + portun nereden geldigi).
     *
     * NEDEN: WHMCS'in basit "Add Server" ekraninda Port alani yok, modulun varsayilan
     * portu (8443) kullanilir; gPanel'in rol kapisi portlari ise rastgeledir (ör. 30409)
     * ve guvenlik geregi anonim olarak ogrenilemez. Port su sirayla belirlenir:
     *   1. Hostname alani: "panel.ornek.com:30409" ya da "https://panel.ornek.com:30409/"
     *   2. Access Hash: "port=30409"
     *   3. WHMCS Port alani (Gelismis mod) — bos ise modul varsayilani
     *
     * @param array<string,mixed> $p
     * @return array{host:string,port:int,kaynak:string}  kaynak: hostname|accesshash|whmcs|varsayilan
     */
    public static function sunucuAdresi(array $p): array
    {
        [$host, $port] = self::hostPortAyir((string) ($p['serverhostname'] ?? ''));
        if ($host === '') {
            [$host] = self::hostPortAyir((string) ($p['serverip'] ?? ''));
        }
        $kaynak = 'hostname';
        if ($port <= 0) {
            $port = self::erisimSecenekleri((string) ($p['serveraccesshash'] ?? ''))['port'];
            $kaynak = 'accesshash';
        }
        if ($port <= 0) {
            // Port alani DOLUYSA (gecersiz olsa bile: 0, 70000) o kullanilir ve istemci
            // reddeder; yalniz BOS alan varsayilana duser.
            $ham = trim((string) ($p['serverport'] ?? ''));
            if ($ham !== '') {
                $port = (int) $ham;
                $kaynak = 'whmcs';
            } else {
                $port = self::VARSAYILAN_PORT;
                $kaynak = 'varsayilan';
            }
        }
        return ['host' => $host, 'port' => $port, 'kaynak' => $kaynak];
    }

    /**
     * "alan", "alan:PORT", "[v6]:PORT", "v6" (koseli parantezsiz, portsuz), "https://alan:PORT/yol"
     * -> [host, port]; port yoksa 0. Kullanici bilgili URL ya da gecersiz port -> ['', 0].
     *
     * @return array{0:string,1:int}
     */
    public static function hostPortAyir(string $s): array
    {
        $s = trim($s);
        if ($s === '') {
            return ['', 0];
        }
        if (preg_match('#^https?://#i', $s)) {
            $u = parse_url($s);
            if ($u === false || empty($u['host']) || isset($u['user']) || isset($u['pass'])) {
                return ['', 0];
            }
            return [trim((string) $u['host'], '[]'), (int) ($u['port'] ?? 0)];
        }
        $s = rtrim($s, '/');
        if (preg_match('/^\[([0-9A-Fa-f:.]+)\](?::(\d{1,5}))?$/', $s, $m)) {
            $pt = isset($m[2]) ? (int) $m[2] : 0;
            return $pt > 65535 ? ['', 0] : [$m[1], $pt];
        }
        if (substr_count($s, ':') === 1) {
            [$h, $pt] = explode(':', $s, 2);
            if (preg_match('/^\d{1,5}$/', $pt) && (int) $pt >= 1 && (int) $pt <= 65535) {
                return [$h, (int) $pt];
            }
            return ['', 0];
        }
        return [$s, 0];
    }

    /** Ana bilgisayar adi/IP dogrulamasi (URL numarasi yok: @ / ? # bosluk). */
    public static function hostGecerli(string $host): bool
    {
        $host = trim($host, '[]');
        if ($host === '' || strlen($host) > 253) {
            return false;
        }
        if (filter_var($host, FILTER_VALIDATE_IP)) {
            return true;
        }
        return (bool) preg_match('/^(?=.{1,253}$)([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/', $host);
    }

    /** host + port -> https taban adresi (IPv6 koseli). */
    public static function tabanUrl(string $host, int $port): string
    {
        $host = trim($host, '[]');
        if (strpos($host, ':') !== false) {
            $host = '[' . $host . ']';
        }
        return 'https://' . $host . ($port > 0 && $port !== 443 ? ':' . $port : '');
    }

    /** Bool benzeri WHMCS degeri (on/yes/1/true). */
    public static function acik($v): bool
    {
        if (is_bool($v)) {
            return $v;
        }
        return in_array(strtolower(trim((string) $v)), ['on', 'yes', '1', 'true'], true);
    }

    public static function esc($s): string
    {
        return htmlspecialchars((string) $s, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
    }

    /** MB -> okunakli metin: 1023 MB, 1 GB, 1.5 GB, 50 GB (GB'de en cok bir ondalik). */
    public static function boyutMetin(int $mb): string
    {
        $mb = max(0, $mb);
        if ($mb < 1024) {
            return $mb . ' MB';
        }
        $gb = round($mb / 1024, 1);
        return (fmod($gb, 1.0) === 0.0 ? (string) (int) $gb : number_format($gb, 1, '.', '')) . ' GB';
    }

    /** KB -> MB (asagi). */
    public static function kbToMb($kb): int
    {
        return (int) floor(max(0, (int) $kb) / 1024);
    }

    /**
     * Dil dosyasini yukler. $dil: "turkish"/"english"/bos. Bilinmeyen -> english.
     *
     * @return array<string,string>
     */
    public static function dil(string $dil = ''): array
    {
        $dil = strtolower(trim($dil));
        if ($dil === '' && isset($GLOBALS['CONFIG']['Language'])) {
            $dil = strtolower((string) $GLOBALS['CONFIG']['Language']);
        }
        $ad = ($dil === 'turkish') ? 'turkish' : 'english';
        $yol = dirname(__DIR__) . '/lang/' . $ad . '.php';
        $_ADDONLANG = [];
        $L = [];
        if (is_file($yol)) {
            $r = include $yol;
            if (is_array($r)) {
                $L = $r;
            }
        }
        return $L;
    }
}
