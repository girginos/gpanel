<?php
/**
 * GirginOSPanel WHMCS modulu — WHMCS hizmeti <-> panel hesap kimligi eslemesi.
 *
 * Kalici + yeniden denenebilir: kayit mod_gpanel_hesap tablosunda (serviceid ->
 * panel_id). Tablo yoksa ilk kullanimda olusturulur. Kayit kaybolursa modul panelde
 * alan adindan (bayi icin kullanici adindan) yeniden bulup baglar.
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

use WHMCS\Database\Capsule;

final class Kayit
{
    public const TABLO = 'mod_gpanel_hesap';
    public const TUR_HOSTING = 'hosting';
    public const TUR_BAYI = 'bayi';

    private static $hazir = false;

    public static function hazirla(): void
    {
        if (self::$hazir) {
            return;
        }
        $sema = Capsule::schema();
        if (!$sema->hasTable(self::TABLO)) {
            $sema->create(self::TABLO, function ($t) {
                $t->integer('serviceid')->unique();
                $t->unsignedBigInteger('panel_id');
                $t->string('tur', 16);
                $t->string('ad', 255)->default('');
                $t->timestamp('updated_at')->nullable();
            });
        }
        self::$hazir = true;
    }

    /** Testler icin sifirlar. */
    public static function sifirla(): void
    {
        self::$hazir = false;
    }

    /** @return array{serviceid:int,panel_id:int,tur:string,ad:string}|null */
    public static function oku(int $serviceId): ?array
    {
        self::hazirla();
        $s = Capsule::table(self::TABLO)->where('serviceid', $serviceId)->first();
        if (!$s) {
            return null;
        }
        $s = (array) $s;
        return ['serviceid' => (int) $s['serviceid'], 'panel_id' => (int) $s['panel_id'], 'tur' => (string) $s['tur'], 'ad' => (string) ($s['ad'] ?? '')];
    }

    /** Ayni panel hesabina bagli BASKA hizmet var mi? (hesap gasbi / cift baglama onlemi) */
    public static function baskaHizmetteMi(int $panelId, string $tur, int $serviceId): bool
    {
        self::hazirla();
        $n = Capsule::table(self::TABLO)
            ->where('panel_id', $panelId)->where('tur', $tur)->where('serviceid', '!=', $serviceId)->first();
        return (bool) $n;
    }

    public static function yaz(int $serviceId, int $panelId, string $tur, string $ad): void
    {
        self::hazirla();
        $simdi = date('Y-m-d H:i:s');
        $var = Capsule::table(self::TABLO)->where('serviceid', $serviceId)->first();
        if ($var) {
            Capsule::table(self::TABLO)->where('serviceid', $serviceId)
                ->update(['panel_id' => $panelId, 'tur' => $tur, 'ad' => $ad, 'updated_at' => $simdi]);
        } else {
            Capsule::table(self::TABLO)
                ->insert(['serviceid' => $serviceId, 'panel_id' => $panelId, 'tur' => $tur, 'ad' => $ad, 'updated_at' => $simdi]);
        }
    }

    public static function sil(int $serviceId): void
    {
        self::hazirla();
        Capsule::table(self::TABLO)->where('serviceid', $serviceId)->delete();
    }
}
