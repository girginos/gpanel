import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// AramaliSecim.tsx — filtrelenebilir (aranabilir) seçim kutusu.
//
// Neden: native <select> uzun listede (çok bayi/müşteri/plan) kullanışsız —
// arama yok, klavyeyle harf harf atlama yetersiz. Bu bileşen açılınca bir arama
// kutusu gösterir ve seçenekleri anlık filtreler. Tema uyumlu (Dialog stiliyle).
//
// Açılır liste body'ye PORTAL ile, `position: fixed` olarak çizilir: Modal gövdesi
// (overflow-y-auto) ya da kaydırılan bir kap listeyi KIRPMAZ. Konum tetikleyiciden
// hesaplanır; kaydırma/yeniden boyutlanmada izlenir; altta yer yoksa yukarı açılır;
// tetikleyici kabının görünür alanından çıkarsa liste kapanır.
// Esc YALNIZ listeyi kapatır (olay durdurulur — içinde bulunduğu Modal kapanmaz).

import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent as TusOlayi } from 'react'
import { createPortal } from 'react-dom'
import { Ikon, I } from '@/components/Ikon'

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
  "Seçiniz…": "Select…",
  "Ara…": "Search…",
  "Kayıt yok": "No records",
  "Yükleniyor…": "Loading…",
  "Eşleşen yok": "No matches",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

const BOSLUK = 4  // tetikleyici ile liste arası (px)
const KENAR = 8   // ekran kenarına en az uzaklık (px)
const EN_AZ = 120 // yer çok darsa bile listenin alabileceği en az yükseklik (px)

// Tetikleyici, kaydırılan/kırpan atalarının (Modal gövdesi vb.) ve ekranın görünür
// alanında mı? Değilse liste boşlukta asılı kalmasın diye kapatılır.
function gorunurMu(el: HTMLElement): boolean {
  const r = el.getBoundingClientRect()
  if (r.bottom <= 0 || r.top >= window.innerHeight) return false
  for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
    const st = getComputedStyle(p)
    if (/(auto|scroll|hidden|clip)/.test(st.overflowY)) {
      const pr = p.getBoundingClientRect()
      if (r.bottom <= pr.top || r.top >= pr.bottom) return false
    }
  }
  return true
}

export default function AramaliSecim({
  secenekler, deger, onDegis,
  yerTutucu = cevir("Seçiniz…"),
  aramaYerTutucu = cevir("Ara…"),
  bosMetin = cevir("Kayıt yok"),
  yukleniyor = false,
}: Props) {
  useTranslation() // dil re-render aboneligi
  const [acik, setAcik] = useState(false)
  const [q, setQ] = useState('')
  const [vurgu, setVurgu] = useState(0) // klavye ile gezinme
  const [konum, setKonum] = useState<CSSProperties | null>(null)
  const kokRef = useRef<HTMLDivElement>(null)
  const tetikRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const aramaRef = useRef<HTMLInputElement>(null)
  const yukariRef = useRef(false)
  const listeId = useId()

  const secili = secenekler.find(s => s.id === deger) || null

  const filtreli = useMemo(() => {
    const t = q.trim().toLocaleLowerCase('tr')
    if (!t) return secenekler
    return secenekler.filter(s =>
      s.etiket.toLocaleLowerCase('tr').includes(t) ||
      (s.altEtiket || '').toLocaleLowerCase('tr').includes(t))
  }, [secenekler, q])

  // Listeyi tetikleyiciye göre konumla. Yön (aşağı/yukarı) YALNIZ açılışta seçilir:
  // yazdıkça liste kısalıp uzarken yön zıplamasın. Yukarı açılan liste alttan
  // (tetikleyicinin üst kenarından) sabitlenir — kısalınca tetikleyiciye yapışık kalır.
  const konumla = useCallback((yonuSec: boolean) => {
    const t = tetikRef.current
    if (!t) return
    const r = t.getBoundingClientRect()
    const vh = window.innerHeight
    const altta = vh - r.bottom - BOSLUK - KENAR
    const ustte = r.top - BOSLUK - KENAR
    if (yonuSec) {
      const gereken = panelRef.current?.offsetHeight ?? 0
      yukariRef.current = gereken > altta && ustte > altta
    }
    const yukari = yukariRef.current
    setKonum({
      position: 'fixed',
      left: r.left,
      width: r.width,
      maxHeight: Math.max(EN_AZ, yukari ? ustte : altta),
      ...(yukari ? { bottom: vh - r.top + BOSLUK } : { top: r.bottom + BOSLUK }),
    })
  }, [])

  // Açılışta: panel önce görünmez çizilir (doğal yüksekliği ölçülsün), sonra boyamadan
  // ÖNCE yerine konur — titreme yok.
  useLayoutEffect(() => {
    if (acik) konumla(true)
    else setKonum(null)
  }, [acik, konumla])

  // Açıkken: kaydırma (herhangi bir kap — capture) ve pencere boyutu değişince izle;
  // tetikleyici görünür alandan çıkarsa kapat. Listenin kendi kaydırması yok sayılır.
  // (Tarayıcı scroll/resize olaylarını zaten kare başına bir kez verir — ek kısma yok.)
  useEffect(() => {
    if (!acik) return
    const izle = (e: Event) => {
      if (panelRef.current && e.target instanceof Node && panelRef.current.contains(e.target)) return
      const t = tetikRef.current
      if (!t || !gorunurMu(t)) { setAcik(false); return }
      konumla(false)
    }
    window.addEventListener('scroll', izle, true)
    window.addEventListener('resize', izle)
    return () => {
      window.removeEventListener('scroll', izle, true)
      window.removeEventListener('resize', izle)
    }
  }, [acik, konumla])

  // Dışarı tıklama → kapat (liste portalda: kök + panel birlikte "içeri" sayılır).
  useEffect(() => {
    if (!acik) return
    function tikla(e: MouseEvent) {
      const h = e.target as Node
      if (kokRef.current?.contains(h) || panelRef.current?.contains(h)) return
      setAcik(false)
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

  function kapat(odakGeri: boolean) {
    setAcik(false)
    if (odakGeri) tetikRef.current?.focus()
  }

  function sec(id: number | string) {
    onDegis(id)
    kapat(true)
  }

  // Klavyeyle gezinirken vurgulanan seçenek listenin görünür alanında kalsın
  // (öğeler aynı — yalnız vurgu değişir; kaydırma hemen yapılabilir).
  function vurgula(i: number) {
    setVurgu(i)
    panelRef.current?.querySelector(`[data-sira="${i}"]`)?.scrollIntoView({ block: 'nearest' })
  }

  function klavye(e: TusOlayi) {
    if (e.key === 'ArrowDown') { e.preventDefault(); vurgula(Math.min(vurgu + 1, filtreli.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); vurgula(Math.max(vurgu - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (filtreli[vurgu]) sec(filtreli[vurgu].id) }
  }

  // Esc listeyi kapatır ve olayı DURDURUR: Modal Esc'i window'da dinler; olay oraya
  // ulaşırsa liste ile birlikte Modal da kapanırdı. Tab: liste kapanır, odak
  // tetikleyiciye döner ve tarayıcının Tab'ı oradan sıradaki alana geçer (liste
  // body'de olduğundan aksi hâlde odak Modal'ın dışına kaçardı).
  function panelTus(e: TusOlayi) {
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); kapat(true) }
    else if (e.key === 'Tab') { kapat(true) }
  }

  function tetikTus(e: TusOlayi) {
    if (acik && e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); kapat(false) }
  }

  const aktifId = acik && filtreli[vurgu] ? `${listeId}-${vurgu}` : undefined

  return (
    <div className="relative" ref={kokRef}>
      <button
        ref={tetikRef}
        type="button"
        onClick={() => setAcik(a => !a)}
        onKeyDown={tetikTus}
        aria-haspopup="listbox"
        aria-expanded={acik}
        aria-controls={acik ? listeId : undefined}
        className="flex w-full items-center justify-between gap-2 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-3 py-2 text-sm text-left text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400"
      >
        <span className={`truncate ${secili ? '' : 'text-slate-400 dark:text-slate-500'}`}>
          {secili ? secili.etiket : yerTutucu}
        </span>
        <Ikon d={I.chevronAsagi} kalinlik={2} className={`size-4 shrink-0 text-slate-400 transition-transform ${acik ? 'rotate-180' : ''}`} />
      </button>

      {acik && createPortal(
        <div
          ref={panelRef}
          onKeyDown={panelTus}
          style={konum ?? { position: 'fixed', top: 0, left: 0, visibility: 'hidden' }}
          className="z-[110] flex flex-col overflow-hidden rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 shadow-xl"
        >
          <div className="shrink-0 p-2 border-b border-slate-100 dark:border-dark-600">
            <input
              ref={aramaRef}
              type="text"
              value={q}
              onChange={e => { setQ(e.target.value); setVurgu(0) }}
              onKeyDown={klavye}
              placeholder={aramaYerTutucu}
              aria-controls={listeId}
              aria-activedescendant={aktifId}
              className="w-full rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
            />
          </div>
          <ul id={listeId} className="max-h-56 min-h-0 overflow-auto overscroll-contain py-1" role="listbox">
            {yukleniyor ? (
              <li className="px-3 py-2 text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</li>
            ) : filtreli.length === 0 ? (
              <li className="px-3 py-2 text-sm text-slate-400 dark:text-slate-500">
                {secenekler.length === 0 ? bosMetin : cevir("Eşleşen yok")}
              </li>
            ) : (
              filtreli.map((s, i) => (
                <li key={s.id} id={`${listeId}-${i}`} role="option" aria-selected={s.id === deger} data-sira={i}>
                  <button
                    type="button"
                    tabIndex={-1}
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
        </div>,
        document.body,
      )}
    </div>
  )
}
