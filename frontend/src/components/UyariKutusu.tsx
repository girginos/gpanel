// Tona göre boyanan ortak bildirim parçaları (bkz. tonlar.ts):
//   <TonRozeti ton="uyari" />                 — diyalog/modal başlığındaki ikon karesi
//   <UyariKutusu ton="uyari" baslik="…">…</UyariKutusu> — sayfa içi uyarı/bilgi kutusu
// Emoji (🔴 vb.) YERİNE bunlar kullanılır: her yerde aynı SVG ikon ve aynı renk dili.
import type { ReactNode } from 'react'
import { I, Ikon } from './Ikon'
import { TONLAR, tonSinifi, type Ton } from './tonlar'
import { Button } from './ui'

export function TonRozeti({ ton, kucuk = false, className = '' }: { ton: Ton; kucuk?: boolean; className?: string }) {
  return (
    <span className={`${tonSinifi(ton)} grid shrink-0 place-items-center rounded-lg bg-this/10 text-this dark:bg-this-lighter/15 dark:text-this-lighter ${kucuk ? 'size-8' : 'size-10'} ${className}`}>
      <Ikon d={TONLAR[ton].ikon} className={kucuk ? 'size-4' : 'size-5'} />
    </span>
  )
}

export function UyariKutusu({ ton = 'bilgi', baslik, children, className = '', opak = false, kucuk = false, onKapat, kapatEtiketi = 'Kapat' }: {
  ton?: Exclude<Ton, 'soru'>
  baslik?: ReactNode
  children?: ReactNode
  className?: string
  // opak — içeriğin üstünde yüzen katmanlarda (ör. sabit hata bandı) yarı saydam zemin
  // okunmaz: panel zemini + ton kenarı + ton şeridi.
  opak?: boolean
  // kucuk — yoğun yerlerde (kart içi, form altı) küçük yazı + küçük ikon.
  kucuk?: boolean
  // onKapat — verilirse sağ üstte kapatma düğmesi çizilir.
  onKapat?: () => void
  kapatEtiketi?: string
}) {
  const zemin = opak
    ? 'border-this/40 border-s-4 bg-white shadow-soft dark:border-this-lighter/40 dark:bg-dark-700 dark:shadow-none'
    : 'border-this/30 bg-this/[.06] dark:border-this-lighter/25 dark:bg-this-lighter/[.07]'
  return (
    <div
      role={ton === 'tehlike' ? 'alert' : undefined}
      className={`${tonSinifi(ton)} flex rounded-lg border ${kucuk ? 'gap-2 p-2.5 text-xs' : 'gap-3 p-3 text-sm'} ${zemin} ${className}`}
    >
      <Ikon d={TONLAR[ton].ikon} className={`shrink-0 text-this-darker dark:text-this-lighter ${kucuk ? 'size-4' : 'mt-0.5 size-5'}`} />
      <div className="min-w-0 flex-1">
        {baslik && <div className="font-semibold text-this-darker dark:text-this-lighter">{baslik}</div>}
        {children && <div className={`${baslik ? 'mt-1 ' : ''}text-gray-700 dark:text-dark-100`}>{children}</div>}
      </div>
      {onKapat && (
        <Button isIcon variant="flat" ikon={I.kapat} onClick={onKapat} aria-label={kapatEtiketi} title={kapatEtiketi}
          className="-me-1 -mt-1 size-7 shrink-0 rounded-full" />
      )}
    </div>
  )
}
