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

require_once __DIR__ . '/lib/Ceviri.php';
require_once __DIR__ . '/lib/ApiHatasi.php';
require_once __DIR__ . '/lib/Yardimci.php';
require_once __DIR__ . '/lib/ApiIstemci.php';
require_once __DIR__ . '/lib/Kayit.php';
require_once __DIR__ . '/lib/Modul.php';

use GirginOSPanel\WHMCS\ApiIstemci;
use GirginOSPanel\WHMCS\Ceviri;
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
        'ServiceSingleSignOnLabel' => Ceviri::t('Panele Giriş Yap'),
        'AdminSingleSignOnLabel' => Ceviri::t('Panele Giriş (Yönetici)'),
    ];
}

/**
 * Ürün seçenekleri. SIRA configoption1..8 ile birebir (Modul::SEC_*).
 *
 * Hepsi SimpleMode: WHMCS 8+ "basit modda" yalnız SimpleMode alanlarını gösterir ve KAYDEDER;
 * 1.0.4'e kadar yalnız plan + bayi paketi işaretliydi → Ürün tipi gizli kalıyor, bayi ürünü
 * hosting olarak açılıyordu (müşteri sunucusunda görüldü, 2026-10-13).
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
            'FriendlyName' => Ceviri::t('Ürün tipi'),
            'Type' => 'dropdown',
            'SimpleMode' => true,
            'Options' => ['hosting' => Ceviri::t('Hosting hesabı'), 'bayi' => Ceviri::t('Bayi (reseller) hesabı')],
            'Default' => 'hosting',
            'Description' => Ceviri::t('WHMCS ürün türü "Reseller Account" ise bu alan ne olursa olsun bayi hesabı açılır. Bayi ürünü için API anahtarı KÖK kullanıcıya ait olmalı ve "bayi" iznine sahip olmalı.'),
        ],
        'plan' => [
            'FriendlyName' => Ceviri::t('Panel planı (hosting)'),
            'Type' => 'text',
            'SimpleMode' => true,
            'Size' => '25',
            'Loader' => static function (array $params) use ($yukle) {
                return $yukle($params, 'plan');
            },
            'Description' => Ceviri::t('Plan ID ya da tam adı. Boşsa panelin varsayılan planı.'),
        ],
        'php' => [
            'FriendlyName' => Ceviri::t('PHP sürümü'),
            'Type' => 'dropdown',
            'SimpleMode' => true,
            'Options' => ['' => Ceviri::t('Plandan / panelden')]
                + array_combine(Yardimci::PHP_SURUMLERI, Yardimci::PHP_SURUMLERI),
            'Default' => '',
            'Description' => Ceviri::t('Seçilmezse plandan/panelden miras.'),
        ],
        'bayi_paketi' => [
            'FriendlyName' => Ceviri::t('Bayi paketi (bayi ürünü)'),
            'Type' => 'text',
            'SimpleMode' => true,
            'Size' => '25',
            'Loader' => static function (array $params) use ($yukle) {
                return $yukle($params, 'paket');
            },
            'Description' => Ceviri::t('Bayi paketi ID ya da tam adı (yalnız Ürün tipi = Bayi).'),
        ],
        'sql_olustur' => [
            'FriendlyName' => Ceviri::t('Varsayılan veritabanı aç'),
            'Type' => 'yesno',
            'SimpleMode' => true,
            'Description' => Ceviri::t('Hesapla birlikte bir MariaDB veritabanı oluştur.'),
        ],
        'tls_dogrulama_kapali' => [
            'FriendlyName' => Ceviri::t('TLS doğrulamasını KAPAT'),
            'Type' => 'yesno',
            'SimpleMode' => true,
            'Description' => Ceviri::t('DİKKAT: kendi imzalı sertifikada araya girme (MITM) saldırısına açar; API anahtarı ele geçirilebilir. Yalnız güvenilir ağda ve geçici olarak kullanın. Sunucu ayarında Access Hash = tls-dogrulama-kapali de aynı işi yapar.'),
        ],
        'panel_adresi' => [
            'FriendlyName' => Ceviri::t('Panel adresi (isteğe bağlı)'),
            'Type' => 'text',
            'SimpleMode' => true,
            'Size' => '40',
            'Description' => Ceviri::t('Boş bırakılabilir: adres panelden alınır — hosting ürününde müşteri kapısı, bayi ürününde bayi kapısı (gPanel 0.3.0-20261013+). Başka bir ad/port için https://panel.ornek.com:PORT/ girin.'),
        ],
        'mevcut_bagla' => [
            'FriendlyName' => Ceviri::t('Mevcut hesabı bağla'),
            'Type' => 'yesno',
            'SimpleMode' => true,
            'Description' => Ceviri::t('Panelde zaten var olan alan adı/bayi bulunursa hizmete BAĞLA. Kapalıyken (önerilen) başka müşterinin hesabını devralma engellenir.'),
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
    $a = Yardimci::sunucuAdresi($params);
    if (!Yardimci::hostGecerli($a['host']) || $a['port'] < 1) {
        return '';
    }
    $url = Yardimci::tabanUrl($a['host'], $a['port']) . '/';
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
