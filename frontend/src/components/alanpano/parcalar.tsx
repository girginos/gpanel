// AlanPano ortak görsel parçaları — Tailux Card/Button/Badge + tonlar.ts renk dili.
// Her kart üç durumu AYRI çizer: yükleniyor (iskelet), boş (açıklamalı), hata
// (mesaj + 'Yeniden dene'). Bekleyiş arızaya, arıza boş veriye benzemez.
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { TooltipContentProps } from 'recharts'
import { Button, Card, butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { TONLAR, type Ton } from '@/components/tonlar'
import { UyariKutusu } from '@/components/UyariKutusu'
import { cevir } from './bicim'
import type { Uc } from './useUc'

// Kart/ikon rengi: ton (durum anlamı) ya da nötr vurgu (primary).
export type Vurgu = Ton | 'vurgu'
const renkSinifi = (v: Vurgu): string => `this:${v === 'vurgu' ? 'primary' : TONLAR[v].renk}`

export function IkonKaro({ ikon, vurgu = 'vurgu', kucuk = false }: { ikon: string; vurgu?: Vurgu; kucuk?: boolean }) {
  return (
    <span className={`${renkSinifi(vurgu)} grid shrink-0 place-items-center rounded-lg bg-this/10 text-this dark:bg-this-lighter/15 dark:text-this-lighter ${kucuk ? 'size-8' : 'size-9'}`}>
      <Ikon d={ikon} className={kucuk ? 'size-4' : 'size-[18px]'} />
    </span>
  )
}

export function AyrintiLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to} className={butonSinifi({ color: 'primary', variant: 'flat', boyut: 'sm', className: 'shrink-0' })}>
      {children}
      <Ikon d={I.ileri} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
    </Link>
  )
}

export function PanoKart({ baslik, alt, ikon, vurgu = 'vurgu', sag, children, className = '' }: {
  baslik: string; alt?: ReactNode; ikon: string; vurgu?: Vurgu; sag?: ReactNode; children: ReactNode; className?: string
}) {
  return (
    <Card className={`flex min-w-0 flex-col bg-white p-4 dark:bg-dark-700 sm:p-5 ${className}`}>
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <IkonKaro ikon={ikon} vurgu={vurgu} />
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold text-gray-800 dark:text-dark-50">{baslik}</h3>
            {alt && <div className="truncate text-xs text-gray-500 dark:text-dark-300">{alt}</div>}
          </div>
        </div>
        {sag}
      </div>
      <div className="flex min-w-0 flex-1 flex-col">{children}</div>
    </Card>
  )
}

export function Iskelet({ yukseklik = 'h-40', satir = 0 }: { yukseklik?: string; satir?: number }) {
  return (
    <div className="flex flex-1 flex-col gap-2.5" aria-busy="true" aria-label={cevir('Yükleniyor…')}>
      {Array.from({ length: satir }).map((_, i) => (
        <div key={i} className={`h-3 animate-pulse rounded bg-gray-150 dark:bg-dark-500 ${i % 2 ? 'w-2/3' : 'w-full'}`} />
      ))}
      {yukseklik && <div className={`${yukseklik} w-full animate-pulse rounded-lg bg-gray-100 dark:bg-dark-600`} />}
    </div>
  )
}

export function BosDurum({ ikon, baslik, aciklama, yukseklik = 'min-h-40' }: { ikon: string; baslik: string; aciklama?: string; yukseklik?: string }) {
  return (
    <div className={`flex flex-1 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-gray-200 px-4 py-6 text-center dark:border-dark-500 ${yukseklik}`}>
      <Ikon d={ikon} className="size-7 text-gray-300 dark:text-dark-400" />
      <div className="text-sm font-medium text-gray-600 dark:text-dark-200">{baslik}</div>
      {aciklama && <div className="max-w-xs text-xs text-gray-500 dark:text-dark-300">{aciklama}</div>}
    </div>
  )
}

export function HataDurum({ mesaj, onYeniden, yukleniyor = false, baslik }: { mesaj: string; onYeniden?: () => void; yukleniyor?: boolean; baslik?: string }) {
  return (
    <div className="flex flex-1 flex-col justify-center gap-2">
      <UyariKutusu ton="tehlike" kucuk baslik={baslik || cevir('Veri alınamadı')}>{mesaj}</UyariKutusu>
      {onYeniden && (
        <Button variant="outlined" boyut="sm" ikon={I.yenile} yukleniyor={yukleniyor} onClick={onYeniden} className="self-start">
          {cevir('Yeniden dene')}
        </Button>
      )}
    </div>
  )
}

// Bir ucun durumuna göre içerik: ilk yükleme → iskelet; veri yokken hata → hata
// kutusu; veri varken yenileme hatası → veri + "önceki veri gösteriliyor" notu.
export function Durumlu<T>({ uc, iskelet, children }: { uc: Uc<T>; iskelet?: ReactNode; children: (veri: T) => ReactNode }) {
  if (uc.veri === null) {
    if (uc.hata) return <HataDurum mesaj={uc.hata} onYeniden={() => void uc.yenile()} yukleniyor={uc.yukleniyor} />
    return <>{iskelet ?? <Iskelet />}</>
  }
  return (
    <>
      {children(uc.veri)}
      {uc.hata && !uc.yukleniyor && (
        <UyariKutusu ton="uyari" kucuk className="mt-3">{cevir('Yenilenemedi — önceki veri gösteriliyor')}: {uc.hata}</UyariKutusu>
      )}
    </>
  )
}

export function Cubuk({ oran, vurgu = 'vurgu', etiket }: { oran: number; vurgu?: Vurgu; etiket: string }) {
  const o = Math.max(0, Math.min(100, oran))
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-gray-150 dark:bg-dark-500"
      role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(o)} aria-label={etiket}>
      <div className={`${renkSinifi(vurgu)} h-full rounded-full bg-this transition-[width] duration-500 dark:bg-this-lighter`} style={{ width: `${o > 0 ? Math.max(o, 2) : 0}%` }} />
    </div>
  )
}

// Özet kartı (StatCard deseni): başlık + büyük değer + alt satır + isteğe bağlı çubuk/rozet/eylem.
export function OzetKart({ baslik, ikon, vurgu = 'vurgu', deger, alt, cubuk, rozet, eylem, durum }: {
  baslik: string; ikon: string; vurgu?: Vurgu
  deger?: ReactNode; alt?: ReactNode; cubuk?: { oran: number; etiket: string }; rozet?: ReactNode; eylem?: ReactNode
  durum: { yukleniyor: boolean; hata: string | null; bos?: boolean; onYeniden?: () => void }
}) {
  const ilk = durum.bos && durum.yukleniyor
  const hata = durum.bos && !durum.yukleniyor && durum.hata
  return (
    <Card className="flex min-w-0 flex-col gap-3 bg-white p-4 dark:bg-dark-700">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-300">{baslik}</div>
          {ilk ? (
            <div className="mt-2 h-6 w-24 animate-pulse rounded bg-gray-150 dark:bg-dark-500" aria-busy="true" aria-label={cevir('Yükleniyor…')} />
          ) : hata ? (
            <div className="mt-1 flex items-center gap-1.5 text-sm font-medium text-this-darker this:error dark:text-this-lighter">
              <Ikon d={I.tehlike} className="size-4 shrink-0" />{cevir('Veri alınamadı')}
            </div>
          ) : (
            <div className="mt-1 truncate text-xl font-semibold tabular-nums text-gray-800 dark:text-dark-50">{deger}</div>
          )}
        </div>
        <IkonKaro ikon={ikon} vurgu={hata ? 'tehlike' : vurgu} />
      </div>
      {ilk ? (
        <div className="h-3 w-3/4 animate-pulse rounded bg-gray-150 dark:bg-dark-500" />
      ) : hata ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-xs text-gray-500 dark:text-dark-300" title={durum.hata || ''}>{durum.hata}</span>
          {durum.onYeniden && (
            <Button isIcon variant="flat" ikon={I.yenile} yukleniyor={durum.yukleniyor} onClick={durum.onYeniden}
              className="size-8 rounded-full" aria-label={cevir('Yeniden dene')} title={cevir('Yeniden dene')} />
          )}
        </div>
      ) : (
        <>
          {cubuk && <Cubuk oran={cubuk.oran} vurgu={vurgu} etiket={cubuk.etiket} />}
          {(alt || rozet) && (
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-dark-300">
              {rozet}
              {alt && <span className="min-w-0 truncate">{alt}</span>}
            </div>
          )}
          {eylem && <div className="-mb-1 -ms-2 mt-auto">{eylem}</div>}
        </>
      )}
    </Card>
  )
}

// Performans skoru halkası — renk tondan (this:*), iz rengi temaya göre.
export function Halka({ deger, vurgu, etiket, children }: { deger: number; vurgu: Vurgu; etiket: string; children?: ReactNode }) {
  const d = Math.max(0, Math.min(100, deger))
  const R = 15.9155 // çevre = 100
  return (
    <div className="relative size-32 shrink-0" role="img" aria-label={etiket}>
      <svg viewBox="0 0 36 36" className="size-full -rotate-90" aria-hidden="true">
        <circle cx="18" cy="18" r={R} fill="none" strokeWidth="3.2" className="stroke-gray-150 dark:stroke-dark-500" />
        <circle cx="18" cy="18" r={R} fill="none" strokeWidth="3.2" strokeLinecap="round"
          strokeDasharray={`${d} ${100 - d}`}
          className={`${renkSinifi(vurgu)} stroke-this transition-[stroke-dasharray] duration-700 dark:stroke-this-lighter`} />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">{children}</div>
    </div>
  )
}

// Grafik ipucu — temaya uyumlu kutu (recharts varsayılanı açık temada sabit beyaz).
export function GrafikIpucu({ p, bicim, baslik }: {
  p: TooltipContentProps
  bicim: (v: number) => string
  baslik?: (etiket: string | number | undefined) => string
}) {
  if (!p.active || !p.payload?.length) return null
  return (
    <div className="rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs shadow-soft dark:border-dark-500 dark:bg-dark-750 dark:shadow-soft-dark">
      <div className="mb-1 font-medium text-gray-800 dark:text-dark-50">{baslik ? baslik(p.label) : p.label}</div>
      {p.payload.map((x) => (
        <div key={String(x.dataKey)} className="flex items-center gap-2 text-gray-600 dark:text-dark-200">
          <span className="inline-block size-2 rounded-full" style={{ background: x.color || x.stroke || x.fill }} aria-hidden="true" />
          <span>{x.name}</span>
          <b className="ms-auto ps-3 font-semibold tabular-nums text-gray-800 dark:text-dark-50">
            {typeof x.value === 'number' ? bicim(x.value) : '—'}
          </b>
        </div>
      ))}
    </div>
  )
}
