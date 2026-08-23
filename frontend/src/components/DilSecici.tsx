// DilSecici.tsx — müşteri panelinde dil değiştirme açılır menüsü.
// Seçim localStorage'a yazılır (i18n detection cache); sayfa yenilenmeden uygulanır.

import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DILLER } from '@/lib/i18n'

// Bayrak — inline SVG bayrak. 🔴 EMOJI bayrak (🇹🇷/🇬🇧) Windows Chrome/Edge'de
// render OLMAZ (regional-indicator glyph yok → "TR"/"GB" harfleri görünür).
// Inline SVG her platformda çizilir.
function Bayrak({ kod, className = '' }: { kod: string; className?: string }) {
  const ort = `inline-block rounded-[2px] ring-1 ring-black/10 dark:ring-white/15 ${className}`
  if (kod === 'tr') {
    return (
      <svg viewBox="0 0 24 16" width={21} height={14} className={ort} aria-hidden="true">
        <rect width="24" height="16" fill="#E30A17" />
        <circle cx="9" cy="8" r="4.2" fill="#fff" />
        <circle cx="10.6" cy="8" r="3.4" fill="#E30A17" />
        <polygon points="15.5,5 16.205,7.029 18.353,7.073 16.641,8.371 17.263,10.427 15.5,9.2 13.737,10.427 14.359,8.371 12.647,7.073 14.795,7.029" fill="#fff" />
      </svg>
    )
  }
  // en → Birleşik Krallık bayrağı (Union Jack)
  return (
    <svg viewBox="0 0 60 30" width={28} height={14} className={ort} aria-hidden="true">
      <clipPath id="gb-ukt"><path d="M30,15 h30 v15 z v15 h-30 z h-30 v-15 z v-15 h30 z" /></clipPath>
      <rect width="60" height="30" fill="#012169" />
      <path d="M0,0 L60,30 M60,0 L0,30" stroke="#fff" strokeWidth="6" />
      <path d="M0,0 L60,30 M60,0 L0,30" clipPath="url(#gb-ukt)" stroke="#C8102E" strokeWidth="4" />
      <path d="M30,0 V30 M0,15 H60" stroke="#fff" strokeWidth="10" />
      <path d="M30,0 V30 M0,15 H60" stroke="#C8102E" strokeWidth="6" />
    </svg>
  )
}

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
        <Bayrak kod={aktif.kod} />
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
              <Bayrak kod={d.kod} />
              <span>{d.ad}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
