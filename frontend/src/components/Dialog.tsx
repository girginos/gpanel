// Tema uyumlu diyalog sistemi — tarayıcının native confirm/prompt/alert
// kutularının yerine geçer.
//
// 🔴 NEDEN: native kutular panelin temasına uymuyor (işletim sistemi görünümü,
// "148.251.169.181:8443 diyor ki…" başlığı, karanlık temada okunmuyor) ve
// markasız görünüyor. Ayrıca bazı tarayıcılar arka arkaya açılan native
// kutuları "bu sayfa daha fazla iletişim kutusu göstermesin" ile SUSTURUR →
// kullanıcı onay veremediği için işlem sessizce iptal olur.
//
// KULLANIM (hook, söz-tabanlı — native API ile birebir aynı akış):
//   const { onay, sor, bilgi } = useDialog()
//   if (!(await onay({ baslik: 'Silinsin mi?', mesaj: '…', tehlike: true }))) return
//   const p = await sor({ baslik: 'Yeni parola', tur: 'password' })  // iptal → null
//   await bilgi({ baslik: 'Bitti', mesaj: '…' })
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'

const DIALOG_EN: Record<string, string> = {
  "Vazgeç": "Cancel",
  "Tamam": "OK",
  "Onayla": "Confirm",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DIALOG_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Tur = 'onay' | 'sor' | 'bilgi'

interface OnaySecenek {
  baslik: string
  mesaj?: ReactNode
  onayEtiketi?: string
  iptalEtiketi?: string
  tehlike?: boolean
}
interface SorSecenek extends OnaySecenek {
  varsayilan?: string
  yerTutucu?: string
  tur?: 'text' | 'password'
}

interface Istek {
  tur: Tur
  secenek: SorSecenek
  cevapla: (v: boolean | string | null) => void
}

interface DialogAPI {
  onay: (s: OnaySecenek) => Promise<boolean>
  sor: (s: SorSecenek) => Promise<string | null>
  bilgi: (s: OnaySecenek) => Promise<void>
}

const Ctx = createContext<DialogAPI | null>(null)

// useDialog — sağlayıcı yoksa native kutulara düşer (sayfa hiçbir zaman
// onay alamadan kilitlenmesin; sessiz başarısızlık YASAK).
export function useDialog(): DialogAPI {
  const api = useContext(Ctx)
  if (api) return api
  return {
    onay: async (s) => window.confirm([s.baslik, typeof s.mesaj === 'string' ? s.mesaj : ''].filter(Boolean).join('\n\n')),
    sor: async (s) => window.prompt(s.baslik, s.varsayilan || ''),
    bilgi: async (s) => { window.alert([s.baslik, typeof s.mesaj === 'string' ? s.mesaj : ''].filter(Boolean).join('\n\n')) },
  }
}

const IkonUyari = () => (
  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
    <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01M5.07 19h13.86c1.54 0 2.5-1.67 1.73-3L13.73 4a2 2 0 00-3.46 0L3.34 16c-.77 1.33.19 3 1.73 3z" />
  </svg>
)
const IkonSoru = () => (
  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
    <path strokeLinecap="round" strokeLinejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
)

export function DialogSaglayici({ children }: { children: ReactNode }) {
  useTranslation() // dil re-render aboneligi
  const [istek, setIstek] = useState<Istek | null>(null)
  const [deger, setDeger] = useState('')
  const girdiRef = useRef<HTMLInputElement | null>(null)
  const onayRef = useRef<HTMLButtonElement | null>(null)

  const ac = useCallback((tur: Tur, secenek: SorSecenek) => {
    return new Promise<boolean | string | null>((cevapla) => {
      setDeger(secenek.varsayilan || '')
      setIstek({ tur, secenek, cevapla })
    })
  }, [])

  const api = useRef<DialogAPI>({
    onay: async (s) => (await ac('onay', s)) as boolean,
    sor: async (s) => (await ac('sor', s)) as string | null,
    bilgi: async (s) => { await ac('bilgi', s) },
  }).current

  function kapat(sonuc: boolean | string | null) {
    istek?.cevapla(sonuc)
    setIstek(null)
  }
  function iptal() { kapat(istek?.tur === 'sor' ? null : false) }
  function tamam() {
    if (!istek) return
    if (istek.tur === 'sor') { kapat(deger) } else { kapat(true) }
  }

  // Esc = iptal, Enter = onayla (metin alanında da geçerli).
  useEffect(() => {
    if (!istek) return
    function tus(e: KeyboardEvent) {
      if (e.key === 'Escape') { e.preventDefault(); iptal() }
      else if (e.key === 'Enter') { e.preventDefault(); tamam() }
    }
    window.addEventListener('keydown', tus)
    // Açılışta odak: soru ise girdiye, değilse onay butonuna.
    const t = window.setTimeout(() => {
      if (istek.tur === 'sor') girdiRef.current?.focus()
      else onayRef.current?.focus()
    }, 30)
    return () => { window.removeEventListener('keydown', tus); window.clearTimeout(t) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [istek, deger])

  const s = istek?.secenek
  const tehlike = !!s?.tehlike

  return (
    <Ctx.Provider value={api}>
      {children}
      {istek && s && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4"
          role="dialog" aria-modal="true" aria-label={s.baslik} onClick={iptal}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl p-6 max-w-md w-full shadow-2xl border border-slate-200 dark:border-slate-700"
            onClick={e => e.stopPropagation()}>
            <div className="flex items-start gap-3">
              <div className={`w-10 h-10 shrink-0 rounded-xl flex items-center justify-center ${
                tehlike
                  ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400'
                  : 'bg-brand-50 dark:bg-brand-900/30 text-brand-600 dark:text-brand-300'}`}>
                {tehlike ? <IkonUyari /> : <IkonSoru />}
              </div>
              <div className="min-w-0 flex-1">
                <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{s.baslik}</h3>
                {s.mesaj && <div className="mt-1.5 text-sm leading-relaxed text-slate-500 dark:text-slate-400 break-words">{s.mesaj}</div>}
              </div>
            </div>

            {istek.tur === 'sor' && (
              <input
                ref={girdiRef}
                type={s.tur === 'password' ? 'password' : 'text'}
                value={deger}
                onChange={e => setDeger(e.target.value)}
                placeholder={s.yerTutucu}
                className="mt-4 w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400"
              />
            )}

            <div className="flex gap-2 mt-5">
              {istek.tur !== 'bilgi' && (
                <button onClick={iptal}
                  className="flex-1 text-sm font-medium px-4 py-2.5 rounded-xl border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">
                  {s.iptalEtiketi || cevir('Vazgeç')}
                </button>
              )}
              <button ref={onayRef} onClick={tamam}
                className={`flex-1 text-sm font-medium px-4 py-2.5 rounded-xl text-white shadow-sm transition-colors ${
                  tehlike
                    ? 'bg-red-600 hover:bg-red-700 shadow-red-600/20'
                    : 'bg-brand-600 hover:bg-brand-700 shadow-brand-600/20'}`}>
                {s.onayEtiketi || (istek.tur === 'bilgi' ? cevir('Tamam') : cevir('Onayla'))}
              </button>
            </div>
          </div>
        </div>
      )}
    </Ctx.Provider>
  )
}
