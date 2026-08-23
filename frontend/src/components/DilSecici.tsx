// DilSecici.tsx — müşteri panelinde dil değiştirme açılır menüsü.
// Seçim localStorage'a yazılır (i18n detection cache); sayfa yenilenmeden uygulanır.

import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DILLER } from '@/lib/i18n'

export default function DilSecici() {
  const { i18n, t } = useTranslation()
  const [acik, setAcik] = useState(false)
  const kokRef = useRef<HTMLDivElement>(null)

  const aktif = DILLER.find(d => d.kod === i18n.resolvedLanguage) || DILLER[0]

  useEffect(() => {
    if (!acik) return
    function tikla(e: MouseEvent) {
      if (kokRef.current && !kokRef.current.contains(e.target as Node)) setAcik(false)
    }
    document.addEventListener('mousedown', tikla)
    return () => document.removeEventListener('mousedown', tikla)
  }, [acik])

  function sec(kod: string) {
    i18n.changeLanguage(kod)
    document.documentElement.lang = kod
    setAcik(false)
  }

  return (
    <div className="relative" ref={kokRef}>
      <button
        type="button"
        onClick={() => setAcik(a => !a)}
        aria-label={t('dil.degistir')}
        title={t('dil.degistir')}
        className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors"
      >
        <span className="text-base leading-none">{aktif.bayrak}</span>
        <span className="hidden sm:inline">{aktif.kod.toUpperCase()}</span>
        <svg className={`w-3.5 h-3.5 text-slate-400 transition-transform ${acik ? 'rotate-180' : ''}`}
          fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden="true">
          <path strokeLinecap="round" strokeLinejoin="round" d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {acik && (
        <div className="absolute right-0 z-[80] mt-1 w-44 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 shadow-xl py-1">
          {DILLER.map(d => (
            <button
              key={d.kod}
              type="button"
              onClick={() => sec(d.kod)}
              className={`flex w-full items-center gap-2.5 px-3 py-2 text-sm transition-colors ${
                d.kod === aktif.kod
                  ? 'bg-brand-50 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 font-medium'
                  : 'text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700'
              }`}
            >
              <span className="text-base leading-none">{d.bayrak}</span>
              <span>{d.ad}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
