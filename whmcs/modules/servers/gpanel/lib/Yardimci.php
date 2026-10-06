<?php
/**
 * GirginOSPanel WHMCS modulu — yardimci islevler (maskeleme, parola, adres, dil).
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

final class Yardimci
{
    public const SURUM = '1.0.0';

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
     * Gecerliyse null, degilse Turkce neden.
     */
    public static function parolaHatasi(string $pw): ?string
    {
        if ($pw === '' || preg_match('/[\r\n\x00]/', $pw)) {
            return 'Parola boş olamaz ve satır sonu/kontrol karakteri içeremez.';
        }
        $uz = function_exists('mb_strlen') ? mb_strlen($pw, 'UTF-8') : strlen($pw);
        if ($uz < 12) {
            return 'Parola en az 12 karakter olmalı.';
        }
        if ($uz > 128) {
            return 'Parola en çok 128 karakter olmalı.';
        }
        if (!preg_match('/[A-Za-z]/', $pw) || !preg_match('/[0-9]/', $pw)) {
            return 'Parola harf ve rakam içermeli.';
        }
        return null;
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
