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
import { Ikon } from './Ikon'
import { TONLAR, tonSinifi, type Ton } from './tonlar'
import { useHesapAski } from '@/store/hesapAski'

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

// Renk + ikon diyalog/modal/uyarı kutusuyla AYNI ton haritasından (tonlar.ts). Beyaz metin
// okunsun diye uyarı ve hata tonun koyu adımında.
const TON: Record<ToastTip, Ton> = { basari: 'basari', hata: 'tehlike', uyari: 'uyari', bilgi: 'bilgi' }
const RENK: Record<ToastTip, string> = {
  basari: `${tonSinifi('basari')} bg-this`,
  hata: `${tonSinifi('tehlike')} bg-this-darker`,
  uyari: `${tonSinifi('uyari')} bg-this-darker`,
  bilgi: `${tonSinifi('bilgi')} bg-this`,
}

export function ToastSaglayici({ children }: { children: ReactNode }) {
  const [kayitlar, setKayitlar] = useState<ToastKaydi[]>([])
  const sayacRef = useRef(0)
  const zamanlayicilar = useRef<number[]>([])

  useEffect(() => () => { zamanlayicilar.current.forEach((t) => window.clearTimeout(t)) }, [])

  // Askıya geçişte ekrandaki bildirimler de temizlenir: panelden kalan toast'lar tam ekran
  // askı sayfasının üstünde asılı kalmasın (yenileri aşağıda ekle() içinde engellenir).
  const askida = useHesapAski((s) => s.askida)
  useEffect(() => {
    if (!askida) return
    zamanlayicilar.current.forEach((t) => window.clearTimeout(t))
    zamanlayicilar.current = []
    setKayitlar([])
  }, [askida])

  const kapat = useCallback((id: number) => {
    setKayitlar((l) => l.map((k) => (k.id === id ? { ...k, cikiyor: true } : k)))
    const t = window.setTimeout(() => setKayitlar((l) => l.filter((k) => k.id !== id)), 220)
    zamanlayicilar.current.push(t)
  }, [])

  const ekle = useCallback((tip: ToastTip, baslik: string, mesaj?: string) => {
    // Askı sayfası açıkken panelden geriye kalan (uçuştaki) isteklerin bildirimleri
    // tam ekran sayfanın üstüne düşmesin.
    if (useHesapAski.getState().askida) return
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
        <Ikon d={TONLAR[TON[kayit.tip]].ikon} className="h-5 w-5" />
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
        className="absolute -right-1.5 -top-1.5 grid h-[18px] w-[18px] place-items-center rounded-full bg-dark-900/90 text-white opacity-0 ring-1 ring-white/25 transition-opacity sm:group-hover:opacity-100"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.6} strokeLinecap="round"
          className="h-2.5 w-2.5" aria-hidden="true"><path d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </div>
  )
}

