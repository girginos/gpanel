<?php
/**
 * GirginOSPanel WHMCS modulu — API hata nesnesi.
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

final class ApiHatasi extends \RuntimeException
{
    /** @var int HTTP durumu (0 = ag/TLS hatasi, yanit yok) */
    public int $durum;
    /** @var string panelin "hata" alani (ya da ag hatasi metni), aynen */
    public string $panelMesaji;
    /** @var string panel hata kodu ("hesap_askida", "bayi_askida" ...) */
    public string $kod;
    /** @var string istegin gerektirdigi izin grubu (403 mesaji icin) */
    public string $izinGrubu;

    public function __construct(int $durum, string $mesaj, string $panelMesaji = '', string $kod = '', string $izinGrubu = '')
    {
        parent::__construct($mesaj, $durum);
        $this->durum = $durum;
        $this->panelMesaji = $panelMesaji;
        $this->kod = $kod;
        $this->izinGrubu = $izinGrubu;
    }
}
