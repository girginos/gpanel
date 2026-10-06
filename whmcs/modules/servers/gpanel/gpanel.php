<?php
/**
 * GirginOSPanel — WHMCS sağlama (provisioning) modülü.
 *
 * WHMCS 8.x / PHP 8.1+. Sunucu kaydı: Hostname, Port = YÖNETİCİ (ya da bayi) KAPISI
 * portu, Kullanıcı adı = anahtar sahibinin panel kullanıcı adı ("root"), Parola = API
 * anahtarı (gosp_...), SSL = işaretli. Ayrıntı: whmcs/README.md.
 *
 * İş mantığı lib/ altındadır; bu dosya yalnızca WHMCS sözleşmesini karşılar.
 */

if (!defined('WHMCS')) {
    die('This file cannot be accessed directly');
}

require_once __DIR__ . '/lib/ApiHatasi.php';
require_once __DIR__ . '/lib/Yardimci.php';
require_once __DIR__ . '/lib/ApiIstemci.php';
require_once __DIR__ . '/lib/Kayit.php';
require_once __DIR__ . '/lib/Modul.php';

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Modul;
use GirginOSPanel\WHMCS\Yardimci;

/**
 * @return array<string,mixed>
 */
function gpanel_MetaData()
{
    return [
        'DisplayName' => 'GirginOSPanel',
        'APIVersion' => '1.1',
        'RequiresServer' => true,
        'DefaultNonSSLPort' => '8080',
        'DefaultSSLPort' => '8443',
        'ServiceSingleSignOnLabel' => 'Panele Giriş Yap',
        'AdminSingleSignOnLabel' => 'Panele Giriş (Yönetici)',
    ];
}

/**
 * Ürün seçenekleri. SIRA configoption1..8 ile birebir (Modul::SEC_*).
 *
 * Plan/paket alanları 'Loader' ile panelden doldurulur (sunucu seçiliyse); Loader
 * desteklenmeyen sürümde alan düz metindir: plan ID'sini ya da tam adını yazın.
 *
 * @return array<string,array<string,mixed>>
 */
function gpanel_ConfigOptions(array $params = [])
{
    $yukle = static function (array $params, string $tur): array {
        try {
            $c = ApiIstemci::paramsdan($params, false)->eylem('ConfigOptions');
            $out = [];
            if ($tur === 'plan') {
                foreach ($c->planlar() as $x) {
                    $out[(string) $x['id']] = (string) ($x['ad'] ?? $x['id']);
                }
            } else {
                foreach ($c->bayiPaketleri() as $x) {
                    $out[(string) $x['id']] = (string) ($x['ad'] ?? $x['id']);
                }
            }
            return $out;
        } catch (\Throwable $e) {
            return [];
        }
    };

    return [
        'urun_tipi' => [
            'FriendlyName' => 'Ürün tipi',
            'Type' => 'dropdown',
            'Options' => ['hosting' => 'Hosting hesabı', 'bayi' => 'Bayi (reseller) hesabı'],
            'Default' => 'hosting',
            'Description' => 'Bayi ürünü için API anahtarı KÖK kullanıcıya ait olmalı ve "bayi" iznine sahip olmalı.',
        ],
        'plan' => [
            'FriendlyName' => 'Panel planı (hosting)',
            'Type' => 'text',
            'Size' => '25',
            'Loader' => static function (array $params) use ($yukle) {
                return $yukle($params, 'plan');
            },
            'SimpleMode' => true,
            'Description' => 'Plan ID ya da tam adı. Boşsa panelin varsayılan planı.',
        ],
        'php' => [
            'FriendlyName' => 'PHP sürümü',
            'Type' => 'text',
            'Size' => '8',
            'Description' => 'Örn. 8.3. Boşsa plandan/panelden miras.',
        ],
        'bayi_paketi' => [
            'FriendlyName' => 'Bayi paketi (bayi ürünü)',
            'Type' => 'text',
            'Size' => '25',
            'Loader' => static function (array $params) use ($yukle) {
                return $yukle($params, 'paket');
            },
            'SimpleMode' => true,
            'Description' => 'Bayi paketi ID ya da tam adı (yalnız Ürün tipi = Bayi).',
        ],
        'sql_olustur' => [
            'FriendlyName' => 'Varsayılan veritabanı aç',
            'Type' => 'yesno',
            'Description' => 'Hesapla birlikte bir MariaDB veritabanı oluştur.',
        ],
        'tls_dogrulama_kapali' => [
            'FriendlyName' => 'TLS doğrulamasını KAPAT',
            'Type' => 'yesno',
            'Description' => 'DİKKAT: kendi imzalı sertifikada araya girme (MITM) saldırısına açar; API anahtarı ele geçirilebilir. Yalnız güvenilir ağda ve geçici olarak kullanın. Sunucu ayarında Access Hash = tls-dogrulama-kapali de aynı işi yapar.',
        ],
        'panel_adresi' => [
            'FriendlyName' => 'Panel adresi (isteğe bağlı)',
            'Type' => 'text',
            'Size' => '40',
            'Description' => 'https://panel.ornek.com:PORT/ — müşteri/bayi kapısı. SSO yokken düz bağlantı olarak kullanılır; bayi ürününde zorunludur.',
        ],
        'mevcut_bagla' => [
            'FriendlyName' => 'Mevcut hesabı bağla',
            'Type' => 'yesno',
            'Description' => 'Panelde zaten var olan alan adı/bayi bulunursa hizmete BAĞLA. Kapalıyken (önerilen) başka müşterinin hesabını devralma engellenir.',
        ],
    ];
}

/**
 * @param array<string,mixed> $params
 * @return array{success:bool,error:string}
 */
function gpanel_TestConnection(array $params)
{
    return Modul::baglantiTesti($params);
}

function gpanel_CreateAccount(array $params)
{
    return Modul::olustur($params);
}

function gpanel_SuspendAccount(array $params)
{
    return Modul::askiyaAl($params);
}

function gpanel_UnsuspendAccount(array $params)
{
    return Modul::askidanAl($params);
}

function gpanel_TerminateAccount(array $params)
{
    return Modul::sil($params);
}

function gpanel_ChangePackage(array $params)
{
    return Modul::paketDegistir($params);
}

function gpanel_ChangePassword(array $params)
{
    return Modul::parolaDegistir($params);
}

function gpanel_UsageUpdate(array $params)
{
    return Modul::kullanimGuncelle($params);
}

/** Sunucular sayfasındaki "Panele git" düğmesi. */
function gpanel_AdminLink(array $params)
{
    $host = trim((string) ($params['serverhostname'] ?? '')) ?: trim((string) ($params['serverip'] ?? ''));
    $port = (int) ($params['serverport'] ?? 0);
    if (!Yardimci::hostGecerli($host) || $port < 1) {
        return '';
    }
    $url = Yardimci::tabanUrl($host, $port) . '/';
    return '<form action="' . Yardimci::esc($url) . '" method="get" target="_blank" rel="noopener">'
        . '<input type="submit" class="btn btn-default btn-sm" value="GirginOSPanel" /></form>';
}

/** @return array{success:bool,redirectTo?:string,errorMsg?:string} */
function gpanel_AdminSingleSignOn(array $params)
{
    return Modul::tekTikGiris($params, false);
}

/** @return array{success:bool,redirectTo?:string,errorMsg?:string} */
function gpanel_ServiceSingleSignOn(array $params)
{
    return Modul::tekTikGiris($params, true);
}

/** @return array<string,string> */
function gpanel_AdminServicesTabFields(array $params)
{
    return Modul::yoneticiAlanlari($params);
}

function gpanel_AdminServicesTabFieldsSave(array $params)
{
    Modul::yoneticiKaydet($params);
}

/** @return array<string,mixed> */
function gpanel_ClientArea(array $params)
{
    return Modul::musteriAlani($params);
}

/** @return array<string,string> etiket => islev (gpanel_ oneksiz) */
function gpanel_ClientAreaCustomButtonArray()
{
    $L = Yardimci::dil();
    return [($L['btn_kullanim_yenile'] ?? 'Refresh usage') => 'kullanimYenile'];
}

/** @return string[] */
function gpanel_ClientAreaAllowedFunctions()
{
    return ['parolaDegistir', 'kullanimYenile'];
}

function gpanel_kullanimYenile(array $params)
{
    return Modul::musteriKullanimYenile($params);
}

function gpanel_parolaDegistir(array $params)
{
    return Modul::musteriParolaDegistir($params);
}
