import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// Gorunur hata yuzeyi — ekranin sag-altinda kapatilabilir uyari yigini.
// Sessiz `.catch(() => {})` yerine gecen lib/hata.ts kayitlarini render eder.
// DashboardLayout'ta bir kez mount edilir.
import { useEffect, useState } from 'react'
import { hataAbone, hataKapat, type HataKaydi } from '@/lib/hata'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { TONLAR, tonSinifi } from '@/components/tonlar'


const CMP_EN: Record<string, string> = {
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function HataYuzeyi() {
  useTranslation() // dil re-render aboneligi
  const [kayitlar, setKayitlar] = useState<HataKaydi[]>([])
  useEffect(() => hataAbone(setKayitlar), [])

  if (!kayitlar.length) return null

  return (
    <div
      className="fixed z-[60] bottom-4 right-4 flex flex-col gap-2 max-w-[min(24rem,calc(100vw-2rem))]"
      role="alert"
      aria-live="polite"
    >
      {kayitlar.map((k) => (
        <div
          key={k.id}
          className={`${tonSinifi('tehlike')} flex items-start gap-2 rounded-lg border border-this/30 bg-white py-2 pe-1.5 ps-3 shadow-lg dark:border-this-lighter/25 dark:bg-dark-700`}
        >
          <Ikon d={TONLAR.tehlike.ikon} className="mt-0.5 size-4 shrink-0 text-this dark:text-this-lighter" />
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium text-this-darker dark:text-this-lighter">{k.baglam}</div>
            <div className="break-words text-xs text-gray-700 dark:text-dark-100">{k.mesaj}</div>
          </div>
          <Button
            isIcon
            variant="flat"
            ikon={I.kapat}
            onClick={() => hataKapat(k.id)}
            className="-my-1 size-8 rounded-full"
            aria-label={cevir("Kapat")}
            title={cevir("Kapat")}
          />
        </div>
      ))}
    </div>
  )
}
