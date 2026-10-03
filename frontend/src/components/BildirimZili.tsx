// Üst çubuk bildirim zili (D3b). Rozet GET /bildirimler/sayac ile 60 sn'de bir
// yoklanır — sekme gizliyken YOKLANMAZ (görünür olunca hemen tazelenir). Liste
// yalnız menü açılınca çekilir (son 15). Tıklama hedefi tek kaynaktan
// (lib/bildirim hedef): yedek → yedekler, SSL → SSL, taşıma → taşıma…
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { api } from '@/lib/api'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useAuth } from '@/store/auth'
import { type Bildirim, baslikAd, goreliZaman, hedef, seviyeSinif, yolGizle } from '@/lib/bildirim'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'

const EN: Record<string, string> = {
  'Bildirimler': 'Notifications',
  'yeni': 'new',
  'kritik': 'critical',
  'Tümünü okundu': 'Mark all read',
  'Bildirim yok': 'No notifications',
  'Bildirimler alınamadı': 'Notifications could not be loaded',
  'Tümünü göster': 'Show all',
  'Tercihler': 'Preferences',
  'Yükleniyor…': 'Loading…',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

const KAT_IKON: Record<string, string> = {
  antivirus: I.kalkan, zincir: I.kalkan, websec: I.kalkanTara, ssl: I.kilit, kota: I.db, yedek: I.disket,
  aski: I.durakla, tasima: I.tasi, toplu: I.parca, sistem: I.sunucu,
}

const YOKLAMA_MS = 60_000

export default function BildirimZili() {
  useTranslation() // dil re-render aboneliği
  const rol = useAuth((s) => s.kullanici?.rol)
  const navigate = useNavigate()
  const [okunmamis, setOkunmamis] = useState(0)
  const [kritik, setKritik] = useState(0)
  const [acik, setAcik] = useState(false)
  const [liste, setListe] = useState<Bildirim[] | null>(null)
  const [hata, setHata] = useState(false)
  const [okunduYapiliyor, setOkunduYapiliyor] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const sayacYukle = useCallback(() => {
    api.get<{ okunmamis: number; kritik: number }>('/bildirimler/sayac')
      .then((r) => { setOkunmamis(r.data.okunmamis || 0); setKritik(r.data.kritik || 0) })
      .catch(() => { /* sessiz — rozet kritik değil */ })
  }, [])

  useEffect(() => {
    sayacYukle()
    const t = setInterval(() => { if (!document.hidden) sayacYukle() }, YOKLAMA_MS)
    const g = () => { if (!document.hidden) sayacYukle() }
    document.addEventListener('visibilitychange', g)
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', g) }
  }, [sayacYukle])

  useEffect(() => {
    if (!acik) return
    const h = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setAcik(false) }
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') setAcik(false) }
    document.addEventListener('mousedown', h)
    document.addEventListener('keydown', k)
    return () => { document.removeEventListener('mousedown', h); document.removeEventListener('keydown', k) }
  }, [acik])

  function ac() {
    const yeni = !acik
    setAcik(yeni)
    if (!yeni) return
    setHata(false)
    api.get<{ bildirimler: Bildirim[]; okunmamis: number }>('/bildirimler?limit=15')
      .then((r) => { setListe(r.data.bildirimler || []); setOkunmamis(r.data.okunmamis || 0) })
      .catch(() => setHata(true))
    sayacYukle()
  }

  async function tumOkundu() {
    setOkunduYapiliyor(true)
    try {
      await api.post('/bildirimler/0/okundu', {})
      setListe((l) => (l || []).map((b) => ({ ...b, okundu: true })))
      setOkunmamis(0); setKritik(0)
    } catch { /* sessiz */ } finally { setOkunduYapiliyor(false) }
  }

  function git(b: Bildirim) {
    setAcik(false)
    if (!b.okundu) {
      api.post(`/bildirimler/${b.id}/okundu`, {}).catch(() => { })
      setListe((l) => (l || []).map((x) => (x.id === b.id ? { ...x, okundu: true } : x)))
      setOkunmamis((n) => Math.max(0, n - 1))
      if (b.seviye === 'kritik') setKritik((n) => Math.max(0, n - 1))
    }
    const h = hedef(b, rol)
    if (h) navigate(h)
  }

  const etiket = okunmamis > 0 ? `${cevir('Bildirimler')} (${okunmamis} ${cevir('yeni')})` : cevir('Bildirimler')
  return (
    <div className="relative" ref={ref}>
      <Button isIcon variant="flat" onClick={ac} className="relative size-9 rounded-full" aria-label={etiket} title={etiket}
        aria-haspopup="true" aria-expanded={acik}>
        {okunmamis > 0 && (
          <span className={`absolute top-1 right-1 min-w-[16px] h-4 px-1 flex items-center justify-center text-[10px] font-bold text-white rounded-full ${kritik > 0 ? 'bg-error' : 'bg-primary-600'}`}>
            {okunmamis > 99 ? '99+' : okunmamis}
          </span>
        )}
        <Ikon d={I.zil} kalinlik={1.8} className="w-5 h-5" />
      </Button>
      {acik && (
        <div role="dialog" aria-label={cevir('Bildirimler')}
          className="absolute right-0 mt-2 w-[27rem] max-w-[calc(100vw-1.5rem)] max-h-[70vh] overflow-y-auto bg-white dark:bg-dark-700 border border-gray-200 dark:border-dark-600 rounded-lg shadow-lg z-50">
          <div className="px-4 py-2.5 border-b border-gray-100 dark:border-dark-600 flex items-center justify-between gap-2 sticky top-0 bg-white dark:bg-dark-700 z-10">
            <span className="text-sm font-semibold text-gray-800 dark:text-dark-100">
              {cevir('Bildirimler')}
              {okunmamis > 0 && <span className="ms-1.5 text-xs font-normal text-gray-500 dark:text-dark-300">{okunmamis} {cevir('yeni')}{kritik > 0 && <> · <span className="text-error">{kritik} {cevir('kritik')}</span></>}</span>}
            </span>
            {okunmamis > 0 && (
              <Button boyut="sm" variant="flat" color="primary" ikon={I.onay} onClick={tumOkundu} yukleniyor={okunduYapiliyor} className="-me-2">{cevir('Tümünü okundu')}</Button>
            )}
          </div>
          {hata ? (
            <p className="px-4 py-8 text-center text-sm text-error">{cevir('Bildirimler alınamadı')}</p>
          ) : liste === null ? (
            <p className="px-4 py-8 text-center text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
          ) : liste.length === 0 ? (
            <div className="px-4 py-10 text-center">
              <Ikon d={I.zil} kalinlik={1.5} className="w-9 h-9 mx-auto mb-2 text-gray-300 dark:text-dark-400" />
              <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Bildirim yok')}</p>
            </div>
          ) : liste.map((b) => (
            <button key={b.id} onClick={() => git(b)}
              className={`w-full text-start px-3.5 py-3 border-b border-gray-100 dark:border-dark-600/50 hover:bg-gray-50 dark:hover:bg-dark-600/50 transition flex gap-3 outline-none focus-visible:bg-gray-50 dark:focus-visible:bg-dark-600/50 ${!b.okundu ? 'bg-primary-600/[.04] dark:bg-primary-400/[.06]' : ''}`}>
              <span className={`mt-0.5 w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0 ${seviyeSinif(b.seviye)}`}>
                <Ikon d={KAT_IKON[b.kategori] || I.zil} kalinlik={1.9} className="w-4 h-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex items-start gap-1.5">
                  <span className={`block text-sm ${!b.okundu ? 'font-semibold text-gray-900 dark:text-dark-50' : 'font-medium text-gray-700 dark:text-dark-100'}`}>{baslikAd(b)}</span>
                  {!b.okundu && <span className="mt-1.5 w-1.5 h-1.5 rounded-full bg-primary-600 dark:bg-primary-400 flex-shrink-0" aria-hidden="true" />}
                </span>
                <span className="block text-xs text-gray-500 dark:text-dark-300 line-clamp-2 mt-0.5 break-words">{yolGizle(b.mesaj)}</span>
                <span className="block text-[11px] text-gray-400 dark:text-dark-300 mt-1">{goreliZaman(b.tarih)}</span>
              </span>
            </button>
          ))}
          <div className="flex sticky bottom-0 bg-white dark:bg-dark-700 border-t border-gray-100 dark:border-dark-600">
            <button onClick={() => { setAcik(false); navigate('/bildirimler') }}
              className="flex-1 inline-flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-primary-600 dark:text-primary-400 hover:bg-gray-50 dark:hover:bg-dark-600/50">
              {cevir('Tümünü göster')}
              <Ikon d={I.ileri} className="size-3.5 shrink-0" />
            </button>
            <button onClick={() => { setAcik(false); navigate('/bildirimler#tercihler') }}
              className="inline-flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-gray-600 dark:text-dark-200 hover:bg-gray-50 dark:hover:bg-dark-600/50 border-s border-gray-100 dark:border-dark-600">
              <Ikon d={I.ayar} className="size-3.5 shrink-0" />
              {cevir('Tercihler')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
