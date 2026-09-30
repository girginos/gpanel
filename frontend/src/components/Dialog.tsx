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
//   if (!(await onay({ baslik: 'Silinsin mi?', mesaj: '…', ton: 'tehlike', onayIkonu: I.cop }))) return
//   const p = await sor({ baslik: 'Yeni parola', tur: 'password' })  // iptal → null
//   await bilgi({ baslik: 'Bitti', mesaj: '…', ton: 'basari' })
//
// RENK TONDAN GELİR (tonlar.ts): onay/sor varsayılanı 'soru', bilgi varsayılanı 'bilgi'.
// Kısmi başarı/geri alınabilir risk 'uyari', yıkıcı eylem ya da hata 'tehlike'.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
import { Button } from './ui'
import { I } from './Ikon'
import { TONLAR, type Ton } from './tonlar'
import { TonRozeti } from './UyariKutusu'
import { useHesapAski } from '@/store/hesapAski'

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
  ton?: Ton
  // onayIkonu — onay butonunun ikonu (vars. onay işareti; silmede I.cop vb.).
  onayIkonu?: string
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

  // Hesap askıya alınınca (tam ekran askı sayfası, bkz. store/hesapAski) açık diyalog
  // sayfanın üstünde kalmasın: iptal edilmiş sayılıp kapanır (bekleyen söz çözülür).
  // Askıdayken açılmaya çalışan diyalog da aynı yoldan hemen iptal olur.
  const askida = useHesapAski((s) => s.askida)
  useEffect(() => {
    if (askida && istek) {
      istek.cevapla(istek.tur === 'sor' ? null : false)
      setIstek(null)
    }
  }, [askida, istek])

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
  const ton: Ton = s?.ton ?? (istek?.tur === 'bilgi' ? 'bilgi' : 'soru')

  return (
    <Ctx.Provider value={api}>
      {children}
      {istek && s && !askida && (
        <div className="gosp-fade fixed inset-0 z-[100] flex items-center justify-center bg-gray-900/50 p-4 backdrop-blur-sm dark:bg-black/50"
          role="dialog" aria-modal="true" aria-label={s.baslik} onClick={iptal}>
          <div className="gosp-modal-in w-full max-w-md rounded-lg bg-white p-6 shadow-soft dark:bg-dark-700 dark:shadow-none"
            onClick={e => e.stopPropagation()}>
            <div className="flex items-start gap-3">
              <TonRozeti ton={ton} />
              <div className="min-w-0 flex-1">
                <h3 className="text-base font-semibold text-gray-800 dark:text-dark-50">{s.baslik}</h3>
                {s.mesaj && <div className="mt-1.5 break-words text-sm leading-relaxed text-gray-600 dark:text-dark-200">{s.mesaj}</div>}
              </div>
            </div>

            {istek.tur === 'sor' && (
              <input
                ref={girdiRef}
                type={s.tur === 'password' ? 'password' : 'text'}
                value={deger}
                onChange={e => setDeger(e.target.value)}
                placeholder={s.yerTutucu}
                className="mt-4 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-800 placeholder:text-gray-400 focus:border-primary-600 focus:outline-hidden dark:border-dark-450 dark:bg-dark-800 dark:text-dark-50 dark:focus:border-primary-500"
              />
            )}

            {/* Ortak modal alt çubuğu: mobilde ana eylem üstte, geniş ekranda sağa yaslı. */}
            <div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              {istek.tur !== 'bilgi' && (
                <Button variant="outlined" onClick={iptal} ikon={I.kapat}>
                  {s.iptalEtiketi || cevir('Vazgeç')}
                </Button>
              )}
              <Button ref={onayRef} color={TONLAR[ton].buton} onClick={tamam} ikon={s.onayIkonu ?? I.onay}>
                {s.onayEtiketi || (istek.tur === 'bilgi' ? cevir('Tamam') : cevir('Onayla'))}
              </Button>
            </div>
          </div>
        </div>
      )}
    </Ctx.Provider>
  )
}
