import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// AramaliSecim.tsx — filtrelenebilir (aranabilir) seçim kutusu.
//
// Neden: native <select> uzun listede (çok bayi/müşteri/plan) kullanışsız —
// arama yok, klavyeyle harf harf atlama yetersiz. Bu bileşen açılınca bir arama
// kutusu gösterir ve seçenekleri anlık filtreler. Tema uyumlu (Dialog stiliyle).

import { useEffect, useMemo, useRef, useState } from 'react'

export type Secenek = {
  id: number | string
  etiket: string
  altEtiket?: string // ikincil satır (ör. kullanıcı adı, plan limiti)
}

type Props = {
  secenekler: Secenek[]
  deger: number | string | null
  onDegis: (id: number | string) => void
  yerTutucu?: string
  aramaYerTutucu?: string
  bosMetin?: string      // seçenek yokken
  yukleniyor?: boolean
}


const CMP_EN: Record<string, string> = {
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function AramaliSecim({
  secenekler, deger, onDegis,
  yerTutucu = cevir("Seçiniz…"),
  aramaYerTutucu = 'Ara…',
  bosMetin = cevir("Kayıt yok"),
  yukleniyor = false,
}: Props) {
  useTranslation() // dil re-render aboneligi
  const [acik, setAcik] = useState(false)
  const [q, setQ] = useState('')
  const [vurgu, setVurgu] = useState(0) // klavye ile gezinme
  const kokRef = useRef<HTMLDivElement>(null)
  const aramaRef = useRef<HTMLInputElement>(null)

  const secili = secenekler.find(s => s.id === deger) || null

  const filtreli = useMemo(() => {
    const t = q.trim().toLocaleLowerCase('tr')
    if (!t) return secenekler
    return secenekler.filter(s =>
      s.etiket.toLocaleLowerCase('tr').includes(t) ||
      (s.altEtiket || '').toLocaleLowerCase('tr').includes(t))
  }, [secenekler, q])

  // Dışarı tıklama → kapat.
  useEffect(() => {
    if (!acik) return
    function tikla(e: MouseEvent) {
      if (kokRef.current && !kokRef.current.contains(e.target as Node)) setAcik(false)
    }
    document.addEventListener('mousedown', tikla)
    return () => document.removeEventListener('mousedown', tikla)
  }, [acik])

  // Açılınca aramaya odaklan + filtreyi sıfırla.
  useEffect(() => {
    if (acik) {
      setQ(''); setVurgu(0)
      const t = setTimeout(() => aramaRef.current?.focus(), 10)
      return () => clearTimeout(t)
    }
  }, [acik])

  function sec(id: number | string) {
    onDegis(id)
    setAcik(false)
  }

  function klavye(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') { e.preventDefault(); setVurgu(v => Math.min(v + 1, filtreli.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setVurgu(v => Math.max(v - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (filtreli[vurgu]) sec(filtreli[vurgu].id) }
    else if (e.key === 'Escape') { setAcik(false) }
  }

  return (
    <div className="relative" ref={kokRef}>
      <button
        type="button"
        onClick={() => setAcik(a => !a)}
        className="flex w-full items-center justify-between gap-2 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm text-left text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400"
      >
        <span className={`truncate ${secili ? '' : 'text-slate-400 dark:text-slate-500'}`}>
          {secili ? secili.etiket : yerTutucu}
        </span>
        <svg className={`w-4 h-4 shrink-0 text-slate-400 transition-transform ${acik ? 'rotate-180' : ''}`}
          fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden="true">
          <path strokeLinecap="round" strokeLinejoin="round" d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {acik && (
        <div className="absolute z-[60] mt-1 w-full rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 shadow-xl">
          <div className="p-2 border-b border-slate-100 dark:border-slate-700">
            <input
              ref={aramaRef}
              type="text"
              value={q}
              onChange={e => { setQ(e.target.value); setVurgu(0) }}
              onKeyDown={klavye}
              placeholder={aramaYerTutucu}
              className="w-full rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
            />
          </div>
          <ul className="max-h-56 overflow-auto py-1" role="listbox">
            {yukleniyor ? (
              <li className="px-3 py-2 text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</li>
            ) : filtreli.length === 0 ? (
              <li className="px-3 py-2 text-sm text-slate-400 dark:text-slate-500">
                {secenekler.length === 0 ? bosMetin : cevir("Eşleşen yok")}
              </li>
            ) : (
              filtreli.map((s, i) => (
                <li key={s.id}>
                  <button
                    type="button"
                    onClick={() => sec(s.id)}
                    onMouseEnter={() => setVurgu(i)}
                    className={`flex w-full flex-col items-start px-3 py-2 text-left text-sm transition-colors ${
                      i === vurgu ? 'bg-brand-50 dark:bg-brand-900/30' : ''
                    } ${s.id === deger ? 'text-brand-700 dark:text-brand-300 font-medium' : 'text-slate-700 dark:text-slate-200'}`}
                  >
                    <span className="truncate w-full">{s.etiket}</span>
                    {s.altEtiket && <span className="text-[11px] text-slate-400 dark:text-slate-500 truncate w-full">{s.altEtiket}</span>}
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>
      )}
    </div>
  )
}
