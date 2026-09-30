// Sayfanın üstündeki ince uyarı bandı: ilk (en ağır) uyarı görünür, "Tümü (n)" hepsini
// açar. Maddeler gerçek durumdan türetilir (hesap.ts uyarilariTuret); madde yoksa
// bant HİÇ çizilmez. "Gizle" yalnız bu tarayıcıda, 24 saat için (lib/kalici).
import { useRef } from 'react'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { TONLAR, tonSinifi } from '@/components/tonlar'
import { BaglantiDugme, DOKUN_IKON, Dugme } from './ortak'
import { cevir, cevirF } from './dil'
import type { Uyari } from './tipler'

function EylemBaglantisi({ u }: { u: Uyari }) {
  if (!u.eylem) return null
  return (
    <BaglantiDugme to={u.eylem.yol} ikon={u.eylem.ikon} className="shrink-0 bg-white dark:bg-dark-700">
      {u.eylem.etiket}
    </BaglantiDugme>
  )
}

export default function UyariBandi({ liste, acik, setAcik, onGizle }: { liste: Uyari[]; acik: boolean; setAcik: (v: boolean) => void; onGizle: (id: string) => void }) {
  const bantRef = useRef<HTMLElement>(null)
  if (!liste.length) return null
  const ilk = liste[0]
  const tekli = liste.length === 1
  // "Tümü" ile "Daralt" yer değiştirir: odak kaybolmasın, yeni düğmeye taşınsın.
  const degistir = (v: boolean) => {
    setAcik(v)
    window.requestAnimationFrame(() => bantRef.current?.querySelector<HTMLElement>('[data-bant-dugme]')?.focus())
  }
  // Gizlenen uyarının düğmesi kaybolur: odak bandın kendisine döner.
  const gizle = (id: string) => {
    onGizle(id)
    window.requestAnimationFrame(() => bantRef.current?.focus({ preventScroll: true }))
  }
  const gizleDugmesi = (u: Uyari) => (
    <Button
      isIcon
      variant="flat"
      ikon={I.kapat}
      className={`${DOKUN_IKON} rounded-md`}
      aria-label={cevirF('Uyarıyı gizle: {0}', u.baslik)}
      title={cevir('Gizle')}
      onClick={() => gizle(u.id)}
    />
  )
  return (
    <section
      ref={bantRef}
      tabIndex={-1}
      aria-label={cevir('Uyarılar')}
      className={`outline-none ${
        acik && !tekli
          ? 'border-b border-gray-200 bg-gray-100 dark:border-dark-600 dark:bg-dark-800'
          : `${tonSinifi(ilk.ton)} border-b border-this/20 bg-this/[.06] dark:border-this-lighter/20 dark:bg-this-lighter/[.06]`
      }`}
    >
      <div className="mx-auto max-w-sayfa px-4 @2xl/sayfa:px-6">
        {!acik || tekli ? (
          // Dar sayfada metin tam satırı alır, düğmeler altına iner; geniş sayfada tek satır.
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 py-2 @2xl/sayfa:flex-nowrap">
            <Ikon d={TONLAR[ilk.ton].ikon} className="size-4 shrink-0 text-this-darker dark:text-this-lighter" />
            <p className="min-w-0 grow basis-[calc(100%-1.75rem)] text-sm leading-snug @2xl/sayfa:basis-0">
              <span className="line-clamp-2 font-medium text-gray-800 @2xl/sayfa:line-clamp-1 dark:text-dark-50">
                {ilk.baslik}
                <span className="hidden font-normal text-gray-600 @5xl/sayfa:inline dark:text-dark-200"> — {ilk.aciklama}</span>
              </span>
            </p>
            <div className="flex w-full flex-wrap items-center gap-2 ps-7 @2xl/sayfa:w-auto @2xl/sayfa:shrink-0 @2xl/sayfa:flex-nowrap @2xl/sayfa:gap-1 @2xl/sayfa:ps-0">
              <EylemBaglantisi u={ilk} />
              {tekli ? (
                gizleDugmesi(ilk)
              ) : (
                <Dugme data-bant-dugme variant="flat" boyut="sm" ikonSonda={I.chevronAsagi} aria-expanded={false} onClick={() => degistir(true)} className="shrink-0">
                  {cevirF('Tümü ({0})', liste.length)}
                </Dugme>
              )}
            </div>
          </div>
        ) : (
          <div className="py-2.5">
            <div className="flex items-center justify-between gap-3 pb-2">
              <h2 className="text-sm font-semibold text-gray-800 dark:text-dark-50">{cevirF('Uyarılar ({0})', liste.length)}</h2>
              <Dugme data-bant-dugme variant="flat" boyut="sm" ikonSonda={I.chevronYukari} aria-expanded onClick={() => degistir(false)}>
                {cevir('Daralt')}
              </Dugme>
            </div>
            <ul className="divide-y divide-gray-100 overflow-hidden rounded-lg border border-gray-200 bg-white dark:divide-dark-600 dark:border-dark-600 dark:bg-dark-700">
              {liste.map((u) => (
                <li key={u.id} className={`${tonSinifi(u.ton)} flex flex-col gap-2 px-3 py-2.5 @2xl/sayfa:flex-row @2xl/sayfa:items-center @2xl/sayfa:gap-4`}>
                  <div className="flex min-w-0 flex-1 items-start gap-2.5">
                    <Ikon d={TONLAR[u.ton].ikon} className="mt-0.5 size-4 shrink-0 text-this-darker dark:text-this-lighter" />
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-gray-800 dark:text-dark-50">{u.baslik}</p>
                      <p className="mt-0.5 break-words text-xs text-gray-600 dark:text-dark-200">{u.aciklama}</p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1 ps-6 @2xl/sayfa:ps-0">
                    <EylemBaglantisi u={u} />
                    {gizleDugmesi(u)}
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  )
}
