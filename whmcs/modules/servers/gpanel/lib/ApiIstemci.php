<?php
/**
 * GirginOSPanel WHMCS modulu — panel REST API istemcisi.
 *
 * Kimlik: "Authorization: gosp <kullanici>:<gosp_onek_gizli>"  (internal/apianahtar/kimlik.go)
 * Taban : https://<host>:<yonetici ya da bayi kapisi portu>/api/v1
 * Hata  : {"hata":"...","kod":"..."}  (internal/httpx)
 *
 * Tasiyici (transport) enjekte edilebilir: callable(array $istek): array
 *   $istek = [method,url,headers[],body(string|null),timeout,dogrula(bool)]
 *   donus  = ['durum'=>int, 'govde'=>string, 'hata'=>string|null]
 */

declare(strict_types=1);

namespace GirginOSPanel\WHMCS;

final class ApiIstemci
{
    public const YOL = '/api/v1';
    public const KISA_ZAMAN_ASIMI = 30;
    /** Hesap olusturma/silme sunucuda ~30 sn+ surebilir. */
    public const UZUN_ZAMAN_ASIMI = 180;

    /** @var callable|null Testlerin varsayilan tasiyiciyi degistirmesi icin. */
    public static $varsayilanTasiyici = null;

    /** @var string */
    private $host;
    /** @var int */
    private $port;
    /** @var string */
    private $kullanici;
    /** @var string */
    private $anahtar;
    /** @var bool */
    private $dogrula;
    /** @var callable */
    private $tasiyici;
    /** @var callable|null  fn(string $eylem, string $istek, string $yanit, array $islenmis, array $gizliler) */
    private $gunluk = null;
    /** @var string */
    private $eylem = 'API';
    /** @var string[] ek gizli degerler (maskeleme) */
    private $ekGizli = [];

    public function __construct(string $host, int $port, string $kullanici, string $anahtar, bool $dogrula = true, ?callable $tasiyici = null)
    {
        $this->host = $host;
        $this->port = $port;
        $this->kullanici = $kullanici !== '' ? $kullanici : 'root';
        $this->anahtar = $anahtar;
        $this->dogrula = $dogrula;
        $this->tasiyici = $tasiyici ?? self::$varsayilanTasiyici ?? [self::class, 'curlTasiyici'];
    }

    /**
     * WHMCS $params dizisinden istemci uretir.
     *   serverhostname|serverip, serverport, serversecure, serverusername, serverpassword (= API anahtari)
     *   serveraccesshash = "tls-dogrulama-kapali" -> TLS dogrulamasi KAPALI (acik secim)
     *   $dogrulamaKapali: urun duzeyindeki "TLS dogrulamasini kapat" secenegi
     *
     * @param array<string,mixed> $params
     * @throws ApiHatasi
     */
    public static function paramsdan(array $params, bool $dogrulamaKapali = false): self
    {
        $host = trim((string) ($params['serverhostname'] ?? ''));
        if ($host === '') {
            $host = trim((string) ($params['serverip'] ?? ''));
        }
        if (!Yardimci::hostGecerli($host)) {
            throw new ApiHatasi(0, 'Sunucu ayarında geçerli bir ana bilgisayar adı/IP yok.');
        }
        $port = (int) ($params['serverport'] ?? 0);
        if ($port < 1 || $port > 65535) {
            throw new ApiHatasi(0, 'Sunucu ayarındaki Port (yönetici kapısı portu) geçersiz.');
        }
        if (!Yardimci::acik($params['serversecure'] ?? '')) {
            // Panel yalniz TLS konusur; API anahtarini duz metin gondermeyiz.
            throw new ApiHatasi(0, "Sunucu ayarında 'Güvenli (SSL)' kutusu işaretli olmalı; API anahtarı düz HTTP ile gönderilmez.");
        }
        $anahtar = (string) ($params['serverpassword'] ?? '');
        if ($anahtar === '') {
            throw new ApiHatasi(0, "Sunucu ayarındaki 'Parola' alanına panel API anahtarı (gosp_...) girilmeli.");
        }
        $dogrula = !($dogrulamaKapali || trim((string) ($params['serveraccesshash'] ?? '')) === 'tls-dogrulama-kapali');
        return new self($host, $port, (string) ($params['serverusername'] ?? ''), $anahtar, $dogrula);
    }

    public function dogrulaAcik(): bool
    {
        return $this->dogrula;
    }

    public function tabanUrl(): string
    {
        return Yardimci::tabanUrl($this->host, $this->port);
    }

    public function gunlukcu(?callable $g): self
    {
        $this->gunluk = $g;
        return $this;
    }

    public function eylem(string $e): self
    {
        $this->eylem = $e;
        return $this;
    }

    public function gizliEkle(string $s): self
    {
        if ($s !== '') {
            $this->ekGizli[] = $s;
        }
        return $this;
    }

    // ── Yuksek duzey uclar ────────────────────────────────────────────────

    /** @return array<string,mixed> */
    public function ben(): array
    {
        return $this->istek('GET', '/api-anahtari/ben');
    }

    /** @return array<int,array<string,mixed>> */
    public function planlar(): array
    {
        return $this->liste($this->istek('GET', '/plans', null, [], 'plan'));
    }

    /** @return array<int,array<string,mixed>> */
    public function hesaplar(): array
    {
        return $this->liste($this->istek('GET', '/domains', null, [], 'hesap'));
    }

    /** @return array<string,mixed> */
    public function hesap(int $id): array
    {
        return $this->istek('GET', '/domains/' . $id, null, [], 'hesap');
    }

    /**
     * @param array<string,mixed> $govde alan_adi, php_surum?, plan_id?, sql_olustur?
     * @return array<string,mixed> (createResp: Domain alanlari + olusturulan_parolalar{ftp,db})
     */
    public function hesapOlustur(array $govde): array
    {
        return $this->istek('POST', '/domains', $govde, [], 'hesap', self::UZUN_ZAMAN_ASIMI);
    }

    /** @return array<string,mixed> */
    public function askiyaAl(int $id): array
    {
        return $this->istek('POST', '/domains/' . $id . '/askiya-al', [], [], 'hesap');
    }

    /** @return array<string,mixed> */
    public function askidanAl(int $id): array
    {
        return $this->istek('POST', '/domains/' . $id . '/askidan-al', [], [], 'hesap');
    }

    /** Silme onayi: ?onay=<alan adi> (bayi icin zorunlu, kok icin dogrulanir). */
    public function hesapSil(int $id, string $alanAdi): array
    {
        return $this->istek('DELETE', '/domains/' . $id, null, ['onay' => $alanAdi], 'hesap', self::UZUN_ZAMAN_ASIMI);
    }

    public function planDegistir(int $id, ?int $planId): array
    {
        return $this->istek('PUT', '/domains/' . $id . '/plan', ['plan_id' => $planId], [], 'hesap');
    }

    public function ftpParola(int $id, string $parola): array
    {
        return $this->gizliEkle($parola)->istek('PUT', '/domains/' . $id . '/ftp/password', ['password' => $parola], [], 'ftp.parola');
    }

    /** POST /domains/{id}/sso -> {ok,url,gecerlilik_sn}. Sozlesme SABIT (ayri ajan). */
    public function sso(int $id): array
    {
        return $this->istek('POST', '/domains/' . $id . '/sso', [], [], 'sso');
    }

    /** @return array<int,array<string,mixed>> */
    public function bayiler(): array
    {
        return $this->liste($this->istek('GET', '/resellers', null, [], 'bayi'));
    }

    /** @return array<int,array<string,mixed>> */
    public function bayiPaketleri(): array
    {
        return $this->liste($this->istek('GET', '/reseller-plans', null, [], 'bayi'));
    }

    /** @param array<string,mixed> $govde paket_id, kullanici, parola, ad_soyad */
    public function bayiOlustur(array $govde): array
    {
        return $this->gizliEkle((string) ($govde['parola'] ?? ''))->istek('POST', '/resellers', $govde, [], 'bayi');
    }

    /** @param array<string,mixed> $govde durum ("active"|"suspended"), paket_id ... */
    public function bayiGuncelle(int $id, array $govde): array
    {
        return $this->istek('PUT', '/resellers/' . $id, $govde, [], 'bayi');
    }

    public function bayiSil(int $id): array
    {
        return $this->istek('DELETE', '/resellers/' . $id, null, [], 'bayi');
    }

    /**
     * @param mixed $d
     * @return array<int,array<string,mixed>>
     * @throws ApiHatasi
     */
    private function liste($d): array
    {
        // Panel duz dizi doner (handlers.List, plans.List, reseller.Liste).
        if (!is_array($d) || ($d !== [] && array_keys($d) !== range(0, count($d) - 1))) {
            throw new ApiHatasi(502, 'Panel yanıtı beklenen liste biçiminde değil.');
        }
        return $d;
    }

    // ── Cekirdek ──────────────────────────────────────────────────────────

    /**
     * @param array<string,mixed>|null $govde
     * @param array<string,string> $sorgu
     * @return array<mixed>
     * @throws ApiHatasi
     */
    public function istek(string $metod, string $yol, ?array $govde = null, array $sorgu = [], string $izinGrubu = '', int $zamanAsimi = self::KISA_ZAMAN_ASIMI): array
    {
        $url = $this->tabanUrl() . self::YOL . $yol;
        if ($sorgu) {
            $url .= '?' . http_build_query($sorgu, '', '&', PHP_QUERY_RFC3986);
        }
        $govdeMetin = null;
        if ($govde !== null && $metod !== 'GET' && $metod !== 'DELETE') {
            $govdeMetin = json_encode($govde === [] ? new \stdClass() : $govde, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
            if ($govdeMetin === false) {
                throw new ApiHatasi(0, 'İstek gövdesi JSON’a çevrilemedi.');
            }
        }
        $basliklar = [
            'Authorization: gosp ' . $this->kullanici . ':' . $this->anahtar,
            'Accept: application/json',
            'User-Agent: GirginOSPanel-WHMCS/' . Yardimci::SURUM,
        ];
        if ($govdeMetin !== null) {
            $basliklar[] = 'Content-Type: application/json';
        }
        $istek = [
            'method' => $metod, 'url' => $url, 'headers' => $basliklar,
            'body' => $govdeMetin, 'timeout' => $zamanAsimi, 'dogrula' => $this->dogrula,
        ];

        try {
            $yanit = ($this->tasiyici)($istek);
        } catch (\Throwable $e) {
            $yanit = ['durum' => 0, 'govde' => '', 'hata' => $e->getMessage()];
        }
        $durum = (int) ($yanit['durum'] ?? 0);
        $ham = (string) ($yanit['govde'] ?? '');
        $ag = isset($yanit['hata']) ? (string) $yanit['hata'] : '';
        $cozulen = json_decode($ham, true);

        $this->gunlukle($metod, $url, $govde, $durum, $ham, $cozulen, $ag);

        if ($durum === 0) {
            throw new ApiHatasi(0, 'Panele bağlanılamadı: ' . ($ag !== '' ? $this->temizle($ag) : 'yanıt yok'), $ag);
        }
        if ($durum >= 200 && $durum < 300) {
            if (!is_array($cozulen)) {
                throw new ApiHatasi($durum, 'Panel yanıtı çözümlenemedi (JSON değil).');
            }
            return $cozulen;
        }
        $pm = is_array($cozulen) && isset($cozulen['hata']) && is_string($cozulen['hata']) ? $cozulen['hata'] : '';
        $kod = is_array($cozulen) && isset($cozulen['kod']) && is_string($cozulen['kod']) ? $cozulen['kod'] : '';
        throw new ApiHatasi($durum, self::hataMesaji($durum, $pm, $kod, $izinGrubu), $pm, $kod, $izinGrubu);
    }

    /** HTTP durumu -> Turkce, eyleme donuk hata metni. Panel mesaji aynen eklenir. */
    public static function hataMesaji(int $durum, string $panel, string $kod, string $izinGrubu): string
    {
        $ek = $panel !== '' ? ' Panel: ' . $panel : '';
        switch (true) {
            case $durum === 400 || $durum === 422:
                return 'Panel isteği doğrulamadı:' . ($panel !== '' ? ' ' . $panel : ' geçersiz istek.');
            case $durum === 401:
                return 'API anahtarı geçersiz (ya da sunucu ayarındaki kullanıcı adı anahtarın sahibiyle eşleşmiyor).' . $ek;
            case $durum === 403:
                if ($kod === 'hesap_askida' || $kod === 'bayi_askida') {
                    return 'Hesap/bayi askıda olduğu için işlem reddedildi.' . $ek;
                }
                if ($izinGrubu !== '') {
                    return "API anahtarında '" . $izinGrubu . "' izni eksik ya da bu uç anahtara kapalı." . $ek;
                }
                return 'Erişim reddedildi (yetki ya da kapsam yetersiz).' . $ek;
            case $durum === 404:
                return 'Panelde kayıt bulunamadı.' . $ek;
            case $durum === 409:
                return 'İşlem mevcut durumla çakışıyor (örn. hesap/bayi askıda ya da kayıt zaten var).' . $ek;
            case $durum === 429:
                return 'Panel istek hız sınırına takıldı; biraz sonra yeniden deneyin.' . $ek;
            case $durum >= 500:
                return 'Panel sunucu hatası (HTTP ' . $durum . ').' . $ek;
            default:
                return 'Beklenmeyen panel yanıtı (HTTP ' . $durum . ').' . $ek;
        }
    }

    private function temizle(string $s): string
    {
        $s = str_replace($this->anahtar, '***', $s);
        return substr($s, 0, 300);
    }

    /**
     * @param array<string,mixed>|null $govde
     * @param mixed $cozulen
     */
    private function gunlukle(string $metod, string $url, ?array $govde, int $durum, string $ham, $cozulen, string $ag): void
    {
        if ($this->gunluk === null) {
            return;
        }
        $gizliler = array_merge([$this->anahtar], $this->ekGizli);
        $istekMetni = Yardimci::maskeleJson((string) json_encode(
            ['method' => $metod, 'url' => $url, 'body' => $govde],
            JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES
        ), $gizliler);
        $yanitMetni = $ham !== '' ? Yardimci::maskeleJson($ham, $gizliler) : '';
        $isl = ['http' => $durum];
        if ($ag !== '') {
            $isl['ag_hatasi'] = Yardimci::maskele($ag, $gizliler);
        }
        // Gunluk geri cagrisina da sirlar verilir ama BIZ zaten maskeledik.
        ($this->gunluk)($this->eylem, $istekMetni, $yanitMetni, $isl, $gizliler);
    }

    /** @return array{durum:int,govde:string,hata:?string} */
    public static function curlTasiyici(array $istek): array
    {
        if (!function_exists('curl_init')) {
            return ['durum' => 0, 'govde' => '', 'hata' => 'PHP curl eklentisi yok'];
        }
        $ch = curl_init($istek['url']);
        $opt = [
            CURLOPT_CUSTOMREQUEST => $istek['method'],
            CURLOPT_HTTPHEADER => $istek['headers'],
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_FOLLOWLOCATION => false, // yonlendirmede Authorization sizmasin
            CURLOPT_CONNECTTIMEOUT => 10,
            CURLOPT_TIMEOUT => (int) $istek['timeout'],
            CURLOPT_SSL_VERIFYPEER => (bool) $istek['dogrula'],
            CURLOPT_SSL_VERIFYHOST => $istek['dogrula'] ? 2 : 0,
        ];
        if (defined('CURLOPT_PROTOCOLS') && defined('CURLPROTO_HTTPS')) {
            $opt[CURLOPT_PROTOCOLS] = CURLPROTO_HTTPS;
        }
        if ($istek['body'] !== null) {
            $opt[CURLOPT_POSTFIELDS] = $istek['body'];
        }
        curl_setopt_array($ch, $opt);
        $govde = curl_exec($ch);
        $durum = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        $hata = $govde === false ? curl_error($ch) : null;
        curl_close($ch);
        return ['durum' => $govde === false ? 0 : $durum, 'govde' => $govde === false ? '' : (string) $govde, 'hata' => $hata];
    }
}
