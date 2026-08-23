// Toast.tsx — kısa süreli işlem bildirimleri (yükleme/silme/taşıma/arşiv...).
//
// Neden ayrı bir sistem: useDialog() modal'ı kullanıcıyı durdurur ve "Tamam"
// beklediği için ardışık işlemlerde (10 dosya sil → 10 modal) engel olur.
// Toast ekranı bloklamaz, kendiliğinden kaybolur, üst üste yığılır.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'

const TOAST_EN: Record<string, string> = {
  "Kapatmak için tıklayın": "Click to close",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (TOAST_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type ToastTip = 'basari' | 'hata' | 'bilgi'

type ToastKaydi = {
  id: number
  tip: ToastTip
  mesaj: string
  cikiyor?: boolean
}

export type ToastAPI = {
  basari: (mesaj: string) => void
  hata: (mesaj: string) => void
  bilgi: (mesaj: string) => void
}

const Ctx = createContext<ToastAPI | null>(null)

export function useToast(): ToastAPI {
  const api = useContext(Ctx)
  if (!api) {
    // Saglayici yoksa panel çökmesin: bildirim sessizce yutulur.
    return { basari: () => {}, hata: () => {}, bilgi: () => {} }
  }
  return api
}

const SURE: Record<ToastTip, number> = {
  basari: 3200,
  bilgi: 4000,
  hata: 6500, // hata mesajı okunacak kadar dursun
}

export function ToastSaglayici({ children }: { children: ReactNode }) {
  const [kayitlar, setKayitlar] = useState<ToastKaydi[]>([])
  const sayacRef = useRef(0)
  const zamanlayicilar = useRef<number[]>([])

  useEffect(() => () => { zamanlayicilar.current.forEach(t => window.clearTimeout(t)) }, [])

  const kapat = useCallback((id: number) => {
    setKayitlar(l => l.map(k => (k.id === id ? { ...k, cikiyor: true } : k)))
    const t = window.setTimeout(() => setKayitlar(l => l.filter(k => k.id !== id)), 220)
    zamanlayicilar.current.push(t)
  }, [])

  const ekle = useCallback((tip: ToastTip, mesaj: string) => {
    const id = ++sayacRef.current
    // En fazla 4 bildirim: toplu işlemde ekran dolmasın (en eskisi düşer).
    setKayitlar(l => [...l.slice(-3), { id, tip, mesaj }])
    const t = window.setTimeout(() => kapat(id), SURE[tip])
    zamanlayicilar.current.push(t)
  }, [kapat])

  // useMemo: value referansı sabit kalsın, yoksa her toast tüm ağacı yeniden render eder.
  const api = useMemo<ToastAPI>(() => ({
    basari: m => ekle('basari', m),
    hata: m => ekle('hata', m),
    bilgi: m => ekle('bilgi', m),
  }), [ekle])

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="fixed bottom-4 right-4 z-[120] flex flex-col gap-2 pointer-events-none max-w-[calc(100vw-2rem)] sm:max-w-sm"
        aria-live="polite" aria-atomic="false">
        {kayitlar.map(k => <ToastSatir key={k.id} kayit={k} kapat={() => kapat(k.id)} />)}
      </div>
    </Ctx.Provider>
  )
}

function ToastSatir({ kayit, kapat }: { kayit: ToastKaydi; kapat: () => void }) {
  useTranslation() // dil re-render aboneligi
  const renk =
    kayit.tip === 'basari'
      ? 'border-emerald-200 dark:border-emerald-800/60 bg-white dark:bg-slate-800'
      : kayit.tip === 'hata'
        ? 'border-red-200 dark:border-red-800/60 bg-white dark:bg-slate-800'
        : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800'
  const ikonRenk =
    kayit.tip === 'basari'
      ? 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400'
      : kayit.tip === 'hata'
        ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400'
        : 'bg-brand-50 dark:bg-brand-900/30 text-brand-600 dark:text-brand-300'

  return (
    <div
      role={kayit.tip === 'hata' ? 'alert' : 'status'}
      onClick={kapat}
      className={`pointer-events-auto cursor-pointer flex items-start gap-2.5 rounded-xl border ${renk} shadow-lg shadow-slate-900/5 px-3.5 py-2.5 transition-all duration-200 motion-reduce:transition-none ${
        kayit.cikiyor ? 'opacity-0 translate-x-2' : 'opacity-100 translate-x-0'
      }`}
      title={cevir("Kapatmak için tıklayın")}
    >
      <span className={`w-6 h-6 shrink-0 rounded-lg flex items-center justify-center ${ikonRenk}`}>
        {kayit.tip === 'basari' ? <IkonTik /> : kayit.tip === 'hata' ? <IkonUnlem /> : <IkonBilgi />}
      </span>
      <span className="text-sm text-slate-700 dark:text-slate-200 leading-snug break-words min-w-0">{kayit.mesaj}</span>
    </div>
  )
}

function IkonTik() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5" /></svg>
  )
}
function IkonUnlem() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"
      strokeLinecap="round" aria-hidden="true"><path d="M12 8v5" /><path d="M12 16.5h.01" /></svg>
  )
}
function IkonBilgi() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"
      strokeLinecap="round" aria-hidden="true"><path d="M12 11v5" /><path d="M12 7.5h.01" /></svg>
  )
}
