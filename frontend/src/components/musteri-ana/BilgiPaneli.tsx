// Sağ sütun bilgi paneli: genel bilgi, kullanım istatistikleri, alan adları.
// Dar sayfada (kapsayıcı < 56rem) araçların üstünde "Hesap özeti" olarak katlanır.
// Veri: GET /domains/{id} (Domain), /domains/{id}/kaynak (KaynakOzet),
// /domains/{id}/subdomain, /domains/{id}/ek-alan-adlari, /domains/{id}/backups — hepsi müşteriye açık uçlar.
import { useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button, Card } from '@/components/ui'
import type { Color } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { tonSinifi } from '@/components/tonlar'
import type { Domain } from '@/components/domainTipi'
import type { KaynakOzet } from '@/components/alanpano/tipler'
import type { Uc } from '@/components/alanpano/useUc'
import { BaglantiDugme, Dugme, KartBasligi, KopyaDugmesi, TurRozeti, YenidenDene } from './ortak'
import { cevir, cevirF, cevirSayili, oran, tarihBicim } from './dil'
import { KULLANIM_ESIK, gecerliYedekler, kullanimSatirlari, sslDurumu, yuzde } from './hesap'
import { eklemeEylemleri } from './ekleme'
import GuvenlikKarti from './GuvenlikKarti'
import { useDisariKapat } from './yardimci'
import type { SslDurumu } from './hesap'
import type { AlanSatiri, EkAlanListesi, Yedek } from './tipler'

const siteAdresi = (ad: string, https: boolean) => `${https ? 'https' : 'http'}://${ad}`

// domains.durum ENUM('aktif','pasif') — bilinen değer çevrilir, bilinmeyen olduğu gibi.
const DURUM_AD: Record<string, string> = { aktif: 'Aktif', pasif: 'Pasif' }
const durumAdi = (d: string): string => (DURUM_AD[d] ? cevir(DURUM_AD[d]) : d)

function BilgiSatiri({ etiket, children, kopya, mono = false }: { etiket: string; children: ReactNode; kopya?: string; mono?: boolean }) {
  return (
    <div className="flex min-h-10 items-center justify-between gap-3 py-1.5">
      <dt className="shrink-0 text-xs text-gray-500 dark:text-dark-300">{etiket}</dt>
      <dd className={`flex min-w-0 items-center justify-end gap-0.5 text-end text-gray-800 dark:text-dark-50 ${mono ? 'font-mono text-xs' : 'text-sm'}`}>
        {/* Yalnız düz metin kırpılır; düğme içeren değerler sarmalanmaz (overflow düğmenin
            dokunma alanını ve odak halkasını kesmesin). */}
        {typeof children === 'string' ? (
          <span className="min-w-0 truncate" title={children}>
            {children}
          </span>
        ) : (
          children
        )}
        {kopya && <KopyaDugmesi deger={kopya} etiket={etiket} />}
      </dd>
    </div>
  )
}

function SslRozeti({ ssl }: { ssl: SslDurumu }) {
  const [renk, metin]: [Color, string] =
    ssl.tur === 'yok'
      ? ['error', cevir('Yok')]
      : ssl.tur === 'doldu'
        ? ['error', cevir('Süresi doldu')]
        : ssl.tur === 'bitiyor'
          ? ['warning', cevirSayili('{0} gün kaldı', ssl.gun ?? 0, ssl.gun ?? 0)]
          : ['success', ssl.gun !== null ? cevirSayili('Geçerli · {0} gün', ssl.gun, ssl.gun) : cevir('Geçerli')]
  return (
    <Badge color={renk} variant="soft" className="px-1.5 py-0.5 text-tiny-plus">
      {metin}
    </Badge>
  )
}

function GenelBilgi({ domainId, domain, kaynak, ssl, yedekler, phpEol }: { domainId: number; domain: Domain; kaynak: KaynakOzet | null; ssl: SslDurumu; yedekler: Uc<Yedek[]>; phpEol: boolean }) {
  const [baglantiAcik, setBaglantiAcik] = useState(false)
  const baglantiId = useId()
  const kok = `/abonelikler/${domainId}`
  const plan = domain.plan_ad || kaynak?.plan_adi || ''
  const sonYedek = (() => {
    if (!yedekler.veri) return yedekler.hata ? cevir('Okunamadı') : '…'
    const g = gecerliYedekler(yedekler.veri)
    return g.length ? tarihBicim(g[0].olusturma) : cevir('Hiç alınmadı')
  })()
  const https = ssl.tur === 'gecerli' || ssl.tur === 'bitiyor'
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi
        baslik={cevir('Genel bilgi')}
        sag={
          plan ? (
            <Badge color="primary" variant="soft" className="min-w-0 max-w-[55%] truncate px-1.5 py-0.5 text-tiny-plus" title={plan}>
              {plan}
            </Badge>
          ) : undefined
        }
      />
      <dl className="divide-y divide-gray-100 px-4 dark:divide-dark-600">
        <BilgiSatiri etiket={cevir('Ana alan adı')}>
          <a
            href={siteAdresi(domain.alan_adi, https)}
            target="_blank"
            rel="noreferrer"
            className="-my-1.5 inline-flex min-h-10 min-w-0 max-w-full items-center gap-1 font-medium text-primary-600 hover:underline dark:text-primary-400"
            title={cevirF('{0} — yeni sekmede açılır', domain.alan_adi)}
          >
            <span className="min-w-0 truncate">{domain.alan_adi}</span>
            <Ikon d={I.harici} className="size-3.5 shrink-0" />
            <span className="sr-only">({cevir('yeni sekmede açılır')})</span>
          </a>
        </BilgiSatiri>
        <BilgiSatiri etiket={cevir('Durum')}>
          {/* Askı burada çizilmez: askıdaki hesap paneli hiç görmez (HesapAskidaPage). */}
          <Badge color={domain.durum === 'aktif' ? 'success' : 'neutral'} variant="soft" className="px-1.5 py-0.5 text-tiny-plus">
            {durumAdi(domain.durum)}
          </Badge>
        </BilgiSatiri>
        <BilgiSatiri etiket="SSL">
          <SslRozeti ssl={ssl} />
        </BilgiSatiri>
        <BilgiSatiri etiket={cevir('PHP sürümü')}>
          <span className="flex items-center gap-1.5">
            <span>PHP {domain.php_surum}</span>
            {phpEol && (
              <Badge color="warning" variant="soft" className="px-1.5 py-0.5 text-tiny-plus" title={cevir('Bu sürüm artık güvenlik güncellemesi almıyor.')}>
                {cevir('desteği bitti')}
              </Badge>
            )}
          </span>
        </BilgiSatiri>
        {domain.sistem_kullanici && (
          <BilgiSatiri etiket={cevir('Sistem kullanıcısı')} kopya={domain.sistem_kullanici} mono>
            {domain.sistem_kullanici}
          </BilgiSatiri>
        )}
        {domain.ipv4 && (
          <BilgiSatiri etiket={cevir('IP adresi')} kopya={domain.ipv4} mono>
            {domain.ipv4}
          </BilgiSatiri>
        )}
        <BilgiSatiri etiket={cevir('Son yedek')}>{sonYedek}</BilgiSatiri>
        {domain.olusturulma && <BilgiSatiri etiket={cevir('Oluşturulma')}>{tarihBicim(domain.olusturulma)}</BilgiSatiri>}
      </dl>
      <div className="border-t border-gray-100 dark:border-dark-600">
        <button
          type="button"
          aria-expanded={baglantiAcik}
          aria-controls={baglantiId}
          onClick={() => setBaglantiAcik((v) => !v)}
          className={`flex min-h-10 w-full items-center justify-between gap-2 px-4 py-2.5 text-start text-xs font-medium text-gray-600 hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-600/50 ${baglantiAcik ? '' : 'rounded-b-lg'}`}
        >
          {cevir('Bağlantı bilgileri')}
          <Ikon d={I.chevronAsagi} className={`size-4 text-gray-400 transition-transform dark:text-dark-300 ${baglantiAcik ? 'rotate-180' : ''}`} />
        </button>
        <div id={baglantiId} className={baglantiAcik ? 'border-t border-gray-100 dark:border-dark-600' : 'hidden'}>
          <dl className="divide-y divide-gray-100 px-4 dark:divide-dark-600">
            {domain.ftp_host && (
              <BilgiSatiri etiket={cevir('FTP sunucusu')} kopya={domain.ftp_host} mono>
                {domain.ftp_host}
              </BilgiSatiri>
            )}
            {domain.ftp_user && (
              <BilgiSatiri etiket={cevir('FTP kullanıcısı')} kopya={domain.ftp_user} mono>
                {domain.ftp_user}
              </BilgiSatiri>
            )}
            {domain.db_host && (
              <BilgiSatiri etiket={cevir('MySQL sunucusu')} kopya={domain.db_host} mono>
                {domain.db_host}
              </BilgiSatiri>
            )}
            {domain.db_user && (
              <BilgiSatiri etiket={cevir('Veritabanı kullanıcısı')} kopya={domain.db_user} mono>
                {domain.db_user}
              </BilgiSatiri>
            )}
            <BilgiSatiri etiket="SSH">
              <span className="flex items-center gap-1.5">
                <Badge color={domain.ssh_erisim ? 'success' : 'neutral'} variant="soft" className="px-1.5 py-0.5 text-tiny-plus">
                  {domain.ssh_erisim ? cevir('Açık') : cevir('Kapalı')}
                </Badge>
                <Link to={`${kok}/ssh-erisim`} className="-my-1.5 inline-flex min-h-10 items-center text-xs font-medium text-primary-600 hover:underline dark:text-primary-400">
                  {cevir('Ayrıntılar')}
                </Link>
              </span>
            </BilgiSatiri>
          </dl>
          <div className="border-t border-gray-100 px-2 py-1.5 dark:border-dark-600">
            <BaglantiDugme to={`${kok}/baglanti`} variant="flat" color="primary" ikonSonda={I.chevronSag}>
              {cevir('Tüm bağlantı bilgileri')}
            </BaglantiDugme>
          </div>
        </div>
      </div>
    </Card>
  )
}

function Kullanim({ kaynak }: { kaynak: Uc<KaynakOzet> }) {
  const k = kaynak.veri
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi
        baslik={cevir('Kullanım istatistikleri')}
        sag={
          <Button
            isIcon
            variant="flat"
            ikon={I.yenile}
            yukleniyor={kaynak.yukleniyor}
            onClick={() => void kaynak.yenile()}
            className="-me-2 size-10 rounded-full @2xl/sayfa:size-8"
            aria-label={cevir('Kullanımı yenile')}
            title={cevir('Kullanımı yenile')}
          />
        }
      />
      {!k ? (
        kaynak.yukleniyor ? (
          <div className="space-y-4 px-4 py-4" aria-hidden>
            {[1, 2, 3, 4].map((i) => (
              <div key={i}>
                <div className="h-3 w-2/3 animate-pulse rounded bg-gray-150 dark:bg-dark-600" />
                <div className="mt-2 h-1.5 animate-pulse rounded-full bg-gray-150 dark:bg-dark-600" />
              </div>
            ))}
          </div>
        ) : (
          <div className="px-4 py-4 text-sm text-gray-600 dark:text-dark-200">
            <p>{cevir('Kullanım bilgisi alınamadı.')}</p>
            <YenidenDene uc={kaynak} className="mt-3" />
          </div>
        )
      ) : (
        <>
          {kaynak.hata && <p className="px-4 pt-3 text-xs text-gray-500 dark:text-dark-300">{cevir('Son yenileme başarısız; değerler eski olabilir.')}</p>}
          <ul className="space-y-3.5 px-4 py-4">
            {kullanimSatirlari(k).map((s) => {
              const sinirsiz = s.x.limit <= 0
              const y = yuzde(s.x)
              const dolu = !sinirsiz && y >= 100
              const yakin = !sinirsiz && y >= KULLANIM_ESIK
              const deger = `${s.bicim(s.x.kullanim)} / ${sinirsiz ? cevir('Sınırsız') : s.bicim(s.x.limit)}`
              // Açık temada turuncu/kırmızı küçük yazı düşük kontrastlı: yazı koyu gri + kalın
              // kalır, uyarı rengini çubuk ve ikon taşır. Koyu temada yazı da ton rengini alır.
              return (
                <li key={s.ad}>
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="truncate text-xs-plus text-gray-700 dark:text-dark-100">{cevir(s.ad)}</span>
                    <span
                      className={`shrink-0 text-xs tabular-nums ${
                        dolu ? 'font-semibold text-gray-900 dark:text-error-lighter' : yakin ? 'font-semibold text-gray-900 dark:text-warning-lighter' : 'text-gray-600 dark:text-dark-200'
                      }`}
                    >
                      {deger}
                    </span>
                  </div>
                  {!sinirsiz && (
                    <div
                      role="meter"
                      aria-label={cevir(s.ad)}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={y}
                      aria-valuetext={`${cevirF('{0}: {1} / {2}', cevir(s.ad), s.bicim(s.x.kullanim), s.bicim(s.x.limit))} (${oran(y)})`}
                      className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-gray-150 dark:bg-dark-500"
                    >
                      <div
                        className={`h-full rounded-full transition-[width] duration-500 ${dolu ? 'bg-error' : yakin ? 'bg-warning' : 'bg-primary-500 dark:bg-primary-400'}`}
                        style={{ width: `${Math.max(y, 2)}%` }}
                      />
                    </div>
                  )}
                  {yakin && (
                    <p className={`${tonSinifi(dolu ? 'tehlike' : 'uyari')} mt-1 flex items-center gap-1 text-tiny-plus font-medium text-gray-800 dark:text-this-lighter`}>
                      <Ikon d={dolu ? I.tehlike : I.uyari} className="size-3.5 text-this-darker dark:text-this-lighter" />
                      {cevir(dolu ? 'Limit doldu' : 'Limite yaklaşıldı')} · {oran(y)}
                    </p>
                  )}
                </li>
              )
            })}
          </ul>
        </>
      )}
    </Card>
  )
}

type Isaret = { ton: 'basari' | 'uyari' | 'tehlike'; ikon: string; metin: string }

function sslIsareti(ssl: SslDurumu): Isaret {
  if (ssl.tur === 'yok') return { ton: 'tehlike', ikon: I.kilitAcik, metin: cevir('SSL yok') }
  if (ssl.tur === 'doldu') return { ton: 'tehlike', ikon: I.kilitAcik, metin: cevir('SSL süresi doldu') }
  if (ssl.tur === 'bitiyor') return { ton: 'uyari', ikon: I.uyari, metin: cevirSayili('SSL {0} gün içinde bitiyor', ssl.gun ?? 0, ssl.gun ?? 0) }
  return { ton: 'basari', ikon: I.kilit, metin: cevir('SSL geçerli') }
}

function durumIsareti(satir: AlanSatiri, ssl: SslDurumu): Isaret | null {
  // Ana alan adı: SSL durumu. Ek alan adı: askıda ise askı, değilse kendi SSL'i (liste uçundan).
  // Alt alan adı: yalnız bilinen durum (desteği biten PHP) — alt alan adının SSL'i listede
  // ölçülmüyor, olmayanı göstermeyiz.
  if (satir.tur === 'ana') return sslIsareti(ssl)
  if (satir.tur === 'ek') {
    if (satir.ekAskida) return { ton: 'uyari', ikon: I.uyari, metin: cevir('Askıda') }
    if (satir.ekSsl) return sslIsareti(sslDurumu({ ssl: satir.ekSsl.aktif, ssl_bitis: satir.ekSsl.bitis }, new Date()))
  }
  if (satir.phpEol) return { ton: 'uyari', ikon: I.uyari, metin: cevirF('PHP {0} — desteği bitti', satir.php) }
  return null
}

function AlanDurumIsareti({ satir, ssl }: { satir: AlanSatiri; ssl: SslDurumu }) {
  const d = durumIsareti(satir, ssl)
  if (!d) {
    return (
      <span className="shrink-0 text-gray-400 dark:text-dark-300" aria-hidden>
        <Ikon d={I.link} className="size-4" />
      </span>
    )
  }
  return (
    <span className={`${tonSinifi(d.ton)} shrink-0 text-this-darker dark:text-this-lighter`} title={d.metin}>
      <Ikon d={d.ikon} className="size-4" />
      <span className="sr-only">{d.metin}</span>
    </span>
  )
}

// İki eylem de açıkken tek "Ekle" düğmesi → açılır menü (aria-haspopup/expanded, Esc ve dışarı tıklamayla kapanır).
function EkleMenusu({ kok }: { kok: string }) {
  const [acik, setAcik] = useState(false)
  const kokRef = useRef<HTMLDivElement>(null)
  const dugmeRef = useRef<HTMLButtonElement>(null)
  const menuId = useId()
  useDisariKapat(acik, kokRef, () => {
    const icerde = kokRef.current?.contains(document.activeElement)
    setAcik(false)
    if (icerde) dugmeRef.current?.focus()
  })
  const oge = 'flex min-h-10 items-center gap-2 rounded-md px-2.5 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-dark-100 dark:hover:bg-dark-600'
  return (
    <div ref={kokRef} className="relative shrink-0">
      <Dugme ref={dugmeRef} variant="soft" color="primary" boyut="sm" ikon={I.arti} aria-haspopup="menu" aria-expanded={acik} aria-controls={menuId} onClick={() => setAcik((v) => !v)}>
        {cevir('Ekle')}
        <Ikon d={I.chevronAsagi} className={`-me-0.5 ms-1 size-3.5 transition-transform ${acik ? 'rotate-180' : ''}`} />
      </Dugme>
      {acik && (
        <div id={menuId} role="menu" aria-label={cevir('Ekle')} className="absolute end-0 z-20 mt-1 w-52 rounded-lg border border-gray-200 bg-white p-1 shadow-soft dark:border-dark-500 dark:bg-dark-700 dark:shadow-none">
          <Link role="menuitem" to={`${kok}/ek-alanlar/yeni`} className={oge} onClick={() => setAcik(false)}>
            <Ikon d={I.kure} className="size-4 shrink-0 text-primary-600 dark:text-primary-400" />
            {cevir('Alan adı ekle')}
          </Link>
          <Link role="menuitem" to={`${kok}/subdomainler`} className={oge} onClick={() => setAcik(false)}>
            <Ikon d={I.arti} className="size-4 shrink-0 text-gray-500 dark:text-dark-300" />
            {cevir('Alt alan adı ekle')}
          </Link>
        </div>
      )}
    </div>
  )
}

function AlanListesi({
  domainId,
  satirlar,
  altlar,
  ekler,
  ssl,
  seciliAnahtar,
  onSec,
  className = '',
}: {
  domainId: number
  satirlar: AlanSatiri[]
  altlar: Uc<unknown[]>
  ekler: Uc<EkAlanListesi>
  ssl: SslDurumu
  seciliAnahtar: string
  onSec: (anahtar: string) => void
  className?: string
}) {
  const kok = `/abonelikler/${domainId}`
  const ekSayisi = satirlar.filter((s) => s.tur === 'ek').length
  const altSayisi = satirlar.filter((s) => s.tur === 'alt').length
  const https = ssl.tur === 'gecerli' || ssl.tur === 'bitiyor'
  // Eylem görünürlüğü TEK yerden (ekleme.ts): ek alan listesi gelmeden hiçbir düğme çizilmez.
  const el = ekler.veri
  const { alanEkle, altEkle, bilgi } = eklemeEylemleri({
    alanLimit: el?.limit.alan_adi ?? 0,
    alanKullanim: el?.limit.kullanim ?? 0,
    altLimit: el?.alt_limit.limit ?? 0,
    altKullanim: el?.alt_limit.kullanim ?? 0,
    yuklendi: el != null,
  })
  return (
    <Card className={`bg-white dark:bg-dark-700 ${className}`}>
      <div className="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-2.5 dark:border-dark-600">
        <h2 className="flex min-w-0 items-center gap-2 text-sm font-semibold text-gray-800 dark:text-dark-50">
          {cevir('Alan adları')}
          <span className="rounded-full bg-gray-100 px-1.5 py-0.5 text-tiny-plus font-medium tabular-nums text-gray-600 dark:bg-dark-600 dark:text-dark-200">{satirlar.length}</span>
        </h2>
        {alanEkle && altEkle ? (
          <EkleMenusu kok={kok} />
        ) : alanEkle ? (
          <BaglantiDugme to={`${kok}/ek-alanlar/yeni`} variant="soft" color="primary" ikon={I.arti} className="shrink-0">
            {cevir('Alan adı ekle')}
          </BaglantiDugme>
        ) : altEkle ? (
          <BaglantiDugme to={`${kok}/subdomainler`} variant="soft" color="primary" ikon={I.arti} className="shrink-0">
            {cevir('Alt alan adı ekle')}
          </BaglantiDugme>
        ) : null}
      </div>
      {bilgi && (
        <p className="flex items-center gap-2 border-b border-gray-100 px-4 py-2 text-xs text-gray-600 dark:border-dark-600 dark:text-dark-200">
          <Ikon d={I.bilgi} className="size-3.5 shrink-0 text-gray-400 dark:text-dark-300" />
          {bilgi}
        </p>
      )}
      <ul className="p-1.5" aria-label={cevir('Alan adları')}>
        {satirlar.map((s) => {
          const secili = s.anahtar === seciliAnahtar
          return (
            <li key={s.anahtar} className="relative flex items-center gap-0.5">
              {s.tur === 'alt' && <span aria-hidden className="absolute start-4 top-0 h-1/2 w-2.5 rounded-es border-b border-s border-gray-300 dark:border-dark-450" />}
              <button
                type="button"
                onClick={() => onSec(s.anahtar)}
                aria-pressed={secili}
                title={cevirF('Alan adına özel araçları {0} için aç', s.ad)}
                className={`group flex min-h-10 min-w-0 flex-1 items-center gap-2.5 rounded-md py-2 pe-2 text-start transition-colors ${s.tur === 'alt' ? 'ps-8' : 'ps-2.5'} ${
                  secili ? 'bg-primary-50 dark:bg-primary-400/10' : 'hover:bg-gray-50 dark:hover:bg-dark-600/60'
                }`}
              >
                <AlanDurumIsareti satir={s} ssl={ssl} />
                <span className={`min-w-0 flex-1 truncate text-sm ${secili ? 'font-medium text-primary-700 dark:text-primary-300' : 'text-gray-800 dark:text-dark-50'}`}>{s.ad}</span>
                <TurRozeti tur={s.tur} />
              </button>
              {s.tur === 'alt' && s.altId !== null ? (
                <Link
                  to={`${kok}/subdomainler/${s.altId}`}
                  className="grid size-10 shrink-0 place-items-center rounded-md text-gray-400 hover:bg-gray-50 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-600/60 dark:hover:text-dark-100"
                  aria-label={cevirF('{0} alt alan adını yönet', s.ad)}
                  title={cevirF('{0} alt alan adını yönet', s.ad)}
                >
                  <Ikon d={I.chevronSag} className="size-4" />
                </Link>
              ) : s.tur === 'ek' && s.ekId != null ? (
                <Link
                  to={`${kok}/ek-alanlar/${s.ekId}`}
                  className="grid size-10 shrink-0 place-items-center rounded-md text-gray-400 hover:bg-gray-50 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-600/60 dark:hover:text-dark-100"
                  aria-label={cevirF('{0} ek alan adını yönet', s.ad)}
                  title={cevirF('{0} ek alan adını yönet', s.ad)}
                >
                  <Ikon d={I.chevronSag} className="size-4" />
                </Link>
              ) : (
                <a
                  href={siteAdresi(s.ad, https)}
                  target="_blank"
                  rel="noreferrer"
                  className="grid size-10 shrink-0 place-items-center rounded-md text-gray-400 hover:bg-gray-50 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-600/60 dark:hover:text-dark-100"
                  aria-label={cevirF('{0} — yeni sekmede açılır', s.ad)}
                  title={cevirF('{0} — yeni sekmede açılır', s.ad)}
                >
                  <Ikon d={I.harici} className="size-4" />
                </a>
              )}
            </li>
          )
        })}
      </ul>
      {ekler.hata && !ekler.veri ? (
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 px-4 py-2.5 text-xs text-gray-600 dark:border-dark-600 dark:text-dark-200">
          <span>{cevir('Ek alan adları alınamadı.')}</span>
          <YenidenDene uc={ekler} />
        </div>
      ) : altlar.hata && !altlar.veri ? (
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 px-4 py-2.5 text-xs text-gray-600 dark:border-dark-600 dark:text-dark-200">
          <span>{cevir('Alt alan adları alınamadı.')}</span>
          <YenidenDene uc={altlar} />
        </div>
      ) : (
        <p className="border-t border-gray-100 px-4 py-2.5 text-xs tabular-nums text-gray-500 dark:border-dark-600 dark:text-dark-300">
          {altlar.veri && ekler.veri ? cevirSayili('1 ana · {0} ek · {1} alt alan adı', altSayisi, ekSayisi, altSayisi) : cevir('Alan adları yükleniyor…')}
        </p>
      )}
    </Card>
  )
}

export default function BilgiPaneli({
  domainId,
  domain,
  kaynak,
  yedekler,
  altlar,
  ekler,
  satirlar,
  ssl,
  seciliAnahtar,
  onSec,
  acik,
  setAcik,
}: {
  domainId: number
  domain: Domain
  kaynak: Uc<KaynakOzet>
  yedekler: Uc<Yedek[]>
  altlar: Uc<unknown[]>
  ekler: Uc<EkAlanListesi>
  satirlar: AlanSatiri[]
  ssl: SslDurumu
  seciliAnahtar: string
  onSec: (anahtar: string) => void
  acik: boolean
  setAcik: (v: boolean) => void
}) {
  const bolumId = useId()
  const k = kaynak.veri
  const liste = k ? kullanimSatirlari(k) : []
  const yakin = liste.some((s) => s.x.limit > 0 && yuzde(s.x) >= KULLANIM_ESIK)
  const ozet = [domain.alan_adi, k && k.disk_mb.limit > 0 ? `${cevir('Disk')} ${oran(yuzde(k.disk_mb))}` : null, cevirSayili('{0} alan adı', satirlar.length, satirlar.length)].filter(Boolean).join(' · ')
  return (
    <div>
      <button
        type="button"
        aria-expanded={acik}
        aria-controls={bolumId}
        onClick={() => setAcik(!acik)}
        className="flex w-full items-center gap-3 rounded-lg border border-gray-200 bg-white px-4 py-3 text-start transition-colors hover:bg-gray-50 @4xl/sayfa:hidden dark:border-dark-600 dark:bg-dark-700 dark:hover:bg-dark-600/60"
      >
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-dark-100">
          <Ikon d={I.grafik} className="size-[18px]" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Hesap özeti')}</span>
          <span className="block truncate text-xs text-gray-500 dark:text-dark-300">{ozet}</span>
        </span>
        {yakin && (
          <span className={`${tonSinifi('uyari')} text-this-darker dark:text-this-lighter`} title={cevir('Limite yaklaşan kullanım var')}>
            <Ikon d={I.uyari} className="size-4" />
            <span className="sr-only">{cevir('Limite yaklaşan kullanım var')}</span>
          </span>
        )}
        <Ikon d={I.chevronAsagi} className={`size-4 shrink-0 text-gray-400 transition-transform dark:text-dark-300 ${acik ? 'rotate-180' : ''}`} />
      </button>
      <div id={bolumId} className={`${acik ? 'grid' : 'hidden'} mt-3 gap-4 @2xl/sayfa:grid-cols-2 @4xl/sayfa:mt-0 @4xl/sayfa:grid @4xl/sayfa:grid-cols-1`}>
        <GenelBilgi domainId={domainId} domain={domain} kaynak={k} ssl={ssl} yedekler={yedekler} phpEol={satirlar[0]?.phpEol ?? false} />
        <GuvenlikKarti domainId={domainId} />
        <Kullanim kaynak={kaynak} />
        <AlanListesi
          domainId={domainId}
          satirlar={satirlar}
          altlar={altlar}
          ekler={ekler}
          ssl={ssl}
          seciliAnahtar={seciliAnahtar}
          onSec={onSec}
          className="@2xl/sayfa:col-span-2 @4xl/sayfa:col-span-1"
        />
      </div>
    </div>
  )
}
