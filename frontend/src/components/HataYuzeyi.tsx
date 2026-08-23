import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// Gorunur hata yuzeyi — ekranin sag-altinda kapatilabilir uyari yigini.
// Sessiz `.catch(() => {})` yerine gecen lib/hata.ts kayitlarini render eder.
// DashboardLayout'ta bir kez mount edilir.
import { useEffect, useState } from 'react'
import { hataAbone, hataKapat, type HataKaydi } from '@/lib/hata'


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
          className="flex items-start gap-2 rounded-lg border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-900/25 px-3 py-2 shadow-lg"
        >
          <svg className="w-4 h-4 mt-0.5 shrink-0 text-red-500" viewBox="0 0 20 20" fill="currentColor" aria-hidden>
            <path
              fillRule="evenodd"
              d="M18 10A8 8 0 11 2 10a8 8 0 0116 0zm-8-5a1 1 0 00-1 1v4a1 1 0 002 0V6a1 1 0 00-1-1zm0 9a1 1 0 100-2 1 1 0 000 2z"
              clipRule="evenodd"
            />
          </svg>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium text-red-800 dark:text-red-200">{k.baglam}</div>
            <div className="text-xs text-red-700 dark:text-red-300 break-words">{k.mesaj}</div>
          </div>
          <button
            onClick={() => hataKapat(k.id)}
            className="shrink-0 text-red-400 hover:text-red-600 dark:hover:text-red-200"
            aria-label={cevir("Kapat")}
          >
            <svg className="w-4 h-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden>
              <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  )
}
