// Toast.tsx — SAĞ ÜST bildirim sistemi (kaydetme / hata / bilgi).
//
// Tasarım: renkli dolu kart + ikon karesi + kalın başlık + alt satır + chevron,
// hover'da sağ üstte kapatma (X). Üst üste yığılır, kendiliğinden kaybolur,
// ekranı bloklamaz (useDialog modal'ının aksine).
//
// API (geriye dönük uyumlu):
//   const t = useToast()
//   t.basari('Kaydedildi')                       // yalnız başlık
//   t.basari('Kaydedildi', 'DNS kaydı güncellendi')  // başlık + alt satır
//   t.hata('Kaydedilemedi', apiHata(err))
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'

const TOAST_EN: Record<string, string> = {
  'Kapat': 'Close',
  'Bildirimi kapat': 'Dismiss notification',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (TOAST_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type ToastTip = 'basari' | 'hata' | 'bilgi' | 'uyari'

type ToastKaydi = {
  id: number
  tip: ToastTip
  baslik: string
  mesaj?: string
  cikiyor?: boolean
}

export type ToastAPI = {
  basari: (baslik: string, mesaj?: string) => void
  hata: (baslik: string, mesaj?: string) => void
  bilgi: (baslik: string, mesaj?: string) => void
  uyari: (baslik: string, mesaj?: string) => void
}

const Ctx = createContext<ToastAPI | null>(null)

export function useToast(): ToastAPI {
  const api = useContext(Ctx)
  if (!api) {
    // Sağlayıcı yoksa panel çökmesin: bildirim sessizce yutulur.
    const yut = () => { }
    return { basari: yut, hata: yut, bilgi: yut, uyari: yut }
  }
  return api
}

const SURE: Record<ToastTip, number> = {
  basari: 3600,
  bilgi: 4200,
  uyari: 5200,
  hata: 6800, // hata okunacak kadar dursun
}

const RENK: Record<ToastTip, string> = {
  basari: 'bg-emerald-600',
  hata: 'bg-rose-600',
  uyari: 'bg-amber-500',
  bilgi: 'bg-blue-600',
}

export function ToastSaglayici({ children }: { children: ReactNode }) {
  const [kayitlar, setKayitlar] = useState<ToastKaydi[]>([])
  const sayacRef = useRef(0)
  const zamanlayicilar = useRef<number[]>([])

  useEffect(() => () => { zamanlayicilar.current.forEach((t) => window.clearTimeout(t)) }, [])

  const kapat = useCallback((id: number) => {
    setKayitlar((l) => l.map((k) => (k.id === id ? { ...k, cikiyor: true } : k)))
    const t = window.setTimeout(() => setKayitlar((l) => l.filter((k) => k.id !== id)), 220)
    zamanlayicilar.current.push(t)
  }, [])

  const ekle = useCallback((tip: ToastTip, baslik: string, mesaj?: string) => {
    const id = ++sayacRef.current
    // En fazla 4 bildirim: toplu işlemde ekran dolmasın (en eskisi düşer).
    setKayitlar((l) => [...l.slice(-3), { id, tip, baslik, mesaj }])
    const t = window.setTimeout(() => kapat(id), SURE[tip])
    zamanlayicilar.current.push(t)
  }, [kapat])

  const api = useMemo<ToastAPI>(() => ({
    basari: (b, m) => ekle('basari', b, m),
    hata: (b, m) => ekle('hata', b, m),
    bilgi: (b, m) => ekle('bilgi', b, m),
    uyari: (b, m) => ekle('uyari', b, m),
  }), [ekle])

  return (
    <Ctx.Provider value={api}>
      {children}
      {/* SAĞ ÜST — topbar'ın altına denk gelsin diye top-16 */}
      <div
        className="pointer-events-none fixed right-4 top-16 z-[140] flex max-w-[calc(100vw-2rem)] flex-col gap-2 sm:max-w-md"
        aria-live="polite" aria-atomic="false"
      >
        {kayitlar.map((k) => <ToastSatir key={k.id} kayit={k} kapat={() => kapat(k.id)} />)}
      </div>
    </Ctx.Provider>
  )
}

function ToastSatir({ kayit, kapat }: { kayit: ToastKaydi; kapat: () => void }) {
  useTranslation() // dil re-render aboneligi
  return (
    <div
      role={kayit.tip === 'hata' ? 'alert' : 'status'}
      onClick={kapat}
      className={`group pointer-events-auto relative flex cursor-pointer items-center gap-3 rounded-xl ${RENK[kayit.tip]} p-2.5 pr-8 text-white shadow-lg shadow-black/20 transition-all duration-200 motion-reduce:transition-none ${
        kayit.cikiyor ? 'translate-x-2 opacity-0' : 'translate-x-0 opacity-100'
      }`}
    >
      <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-white/15">
        <Ikon tip={kayit.tip} />
      </span>

      {/* 🔴 Metin KIRPILMAZ, SARAR: başlık `truncate` idi ve tek satıra sığmayan
          her bildirim "Lisans doğrulandı. Kurmak istediğini…" gibi yarıda
          kesiliyordu — kullanıcı asıl talimatı göremiyordu. Artık sarıyor;
          yükseklik patlamasın diye makul bir satır sınırı var. `break-words`
          uzun anahtar/yol gibi boşluksuz metinleri de taşırmadan böler. */}
      <div className="min-w-0 flex-1">
        <h3 className="line-clamp-3 break-words text-sm font-semibold leading-snug">{kayit.baslik}</h3>
        {kayit.mesaj && <p className="mt-0.5 line-clamp-4 break-words text-xs text-white/90">{kayit.mesaj}</p>}
      </div>

      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
        className="h-4 w-4 shrink-0 opacity-80" aria-hidden="true"><path d="M9 18l6-6-6-6" /></svg>

      <button
        type="button"
        onClick={(e) => { e.stopPropagation(); kapat() }}
        aria-label={cevir('Bildirimi kapat')}
        title={cevir('Kapat')}
        className="absolute -right-1.5 -top-1.5 grid h-[18px] w-[18px] place-items-center rounded-full bg-slate-900/90 text-white opacity-0 ring-1 ring-white/25 transition-opacity sm:group-hover:opacity-100"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.6} strokeLinecap="round"
          className="h-2.5 w-2.5" aria-hidden="true"><path d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </div>
  )
}

function Ikon({ tip }: { tip: ToastTip }) {
  const p = tip === 'basari' ? 'M20 6L9 17l-5-5'
    : tip === 'hata' ? 'M12 8v5M12 16.5h.01M10.3 3.6L2.3 17.6A1.5 1.5 0 003.6 20h16.8a1.5 1.5 0 001.3-2.4L13.6 3.6a1.5 1.5 0 00-2.6 0z'
      : tip === 'uyari' ? 'M12 9v4m0 4h.01M10.3 3.6L2.3 17.6A1.5 1.5 0 003.6 20h16.8a1.5 1.5 0 001.3-2.4L13.6 3.6a1.5 1.5 0 00-2.6 0z'
        : 'M12 16v-5M12 8h.01M12 21a9 9 0 100-18 9 9 0 000 18z'
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={tip === 'basari' ? 2.8 : 1.9}
      strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5" aria-hidden="true"><path d={p} /></svg>
  )
}
