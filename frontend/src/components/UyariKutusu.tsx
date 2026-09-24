// Tona göre boyanan ortak bildirim parçaları (bkz. tonlar.ts):
//   <TonRozeti ton="uyari" />                 — diyalog/modal başlığındaki ikon karesi
//   <UyariKutusu ton="uyari" baslik="…">…</UyariKutusu> — sayfa içi uyarı/bilgi kutusu
// Emoji (🔴 vb.) YERİNE bunlar kullanılır: her yerde aynı SVG ikon ve aynı renk dili.
import type { ReactNode } from 'react'
import { Ikon } from './Ikon'
import { TONLAR, tonSinifi, type Ton } from './tonlar'

export function TonRozeti({ ton, kucuk = false, className = '' }: { ton: Ton; kucuk?: boolean; className?: string }) {
  return (
    <span className={`${tonSinifi(ton)} grid shrink-0 place-items-center rounded-lg bg-this/10 text-this dark:bg-this-lighter/15 dark:text-this-lighter ${kucuk ? 'size-8' : 'size-10'} ${className}`}>
      <Ikon d={TONLAR[ton].ikon} className={kucuk ? 'size-4' : 'size-5'} />
    </span>
  )
}

export function UyariKutusu({ ton = 'bilgi', baslik, children, className = '' }: {
  ton?: Exclude<Ton, 'soru'>
  baslik?: ReactNode
  children?: ReactNode
  className?: string
}) {
  return (
    <div
      role={ton === 'tehlike' ? 'alert' : undefined}
      className={`${tonSinifi(ton)} flex gap-3 rounded-lg border border-this/30 bg-this/[.06] p-3 text-sm dark:border-this-lighter/25 dark:bg-this-lighter/[.07] ${className}`}
    >
      <Ikon d={TONLAR[ton].ikon} className="mt-0.5 size-5 shrink-0 text-this-darker dark:text-this-lighter" />
      <div className="min-w-0 flex-1">
        {baslik && <div className="font-semibold text-this-darker dark:text-this-lighter">{baslik}</div>}
        {children && <div className={`${baslik ? 'mt-1 ' : ''}text-gray-700 dark:text-dark-100`}>{children}</div>}
      </div>
    </div>
  )
}
