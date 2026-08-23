import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Bildirim = { id: number; seviye: string; kategori: string; baslik: string; mesaj: string; domain_id: number | null; okundu: boolean; tarih: string }

const KAT_ETIKET: Record<string, string> = { antivirus: 'Antivirüs', ssl: 'SSL', yedek: 'Yedek', kota: 'Kota', sistem: 'Sistem' }
const katAd = (k: string) => KAT_ETIKET[k] || (k ? k[0].toUpperCase() + k.slice(1) : 'Genel')

const goreliZaman = (t: string) => {
  // UTC timezone-suz zaman damgasi (bkz. TopBar). Z eklemeden 3 saat kayar.
  const d = new Date(t.replace(' ', 'T') + (t.includes(' ') ? 'Z' : ''))
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (!isFinite(s) || s < 0) return t.slice(0, 16)
  if (s < 60) return cevir("az önce")
  if (s < 3600) return `${Math.floor(s / 60)} ${cevir("dk önce")}`
  if (s < 86400) return `${Math.floor(s / 3600)} ${cevir("sa önce")}`
  if (s < 604800) return `${Math.floor(s / 86400)} ${cevir("gün önce")}`
  return t.slice(0, 10)
}
// Eski kayıtlardaki tam sunucu yolunu (/home/<kullanıcı>/) gizle — göreli kalır.
const yolGizle = (m: string) => m.replace(/\/home\/[^/\s]+\//g, '')
const sevBg = (s: string) =>
  s === 'kritik' ? 'bg-red-100 dark:bg-red-900/30 text-red-600 dark:text-red-400'
    : s === 'uyari' ? 'bg-amber-100 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400'
      : 'bg-sky-100 dark:bg-sky-900/30 text-sky-600 dark:text-sky-400'
const KatIkon = ({ kategori }: { kategori: string }) => (
  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.9}>
    <path strokeLinecap="round" strokeLinejoin="round" d={
      kategori === 'antivirus'
        ? 'M9 12l2 2 4-4m5.6-4A12 12 0 0112 2.9 12 12 0 013.4 6 12 12 0 003 9c0 5.6 3.8 10.3 9 11.6 5.2-1.3 9-6 9-11.6 0-1-.1-2-.4-3z'
        : 'M15 17h5l-1.4-1.4A2 2 0 0118 14.2V11a6 6 0 00-4-5.7V5a2 2 0 10-4 0v.3C7.7 6.2 6 8.4 6 11v3.2c0 .5-.2 1-.6 1.4L4 17h5m6 0v1a3 3 0 11-6 0v-1'
    } />
  </svg>
)


const BILDIRIM_EN: Record<string, string> = {
  "Okunmamış bildirim yok.": "No unread notifications.",
  "az önce": "just now",
  "○ Yalnız okunmamış": "○ Unread only",
  "● Yalnız okunmamış": "● Unread only",
  "dk önce": "min ago",
  "sa önce": "h ago",
  "gün önce": "d ago",
  "Anasayfa": "Homepage",
  "Bildirimler": "Notifications",
  "Antivirüs": "Antivirus",
  "Yedek": "Backup",
  "Kota": "Quota",
  "Sistem": "System",
  "Genel": "General",
  "Tümünü okundu işaretle": "Mark all as read",
  "Bildirim yok.": "No notifications.",
  "Kritik": "Critical",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BILDIRIM_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function BildirimlerPage() {
  useTranslation() // dil re-render aboneligi
  const navigate = useNavigate()
  const [liste, setListe] = useState<Bildirim[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [sadeceOkunmamis, setSadeceOkunmamis] = useState(false)
  const [kat, setKat] = useState('') // '' = tümü (istemci tarafı süzülür)

  function yukle() {
    setHata(null)
    const q = sadeceOkunmamis ? '?sadece_okunmamis=1' : ''
    api.get<{ bildirimler: Bildirim[] }>(`/bildirimler${q}`)
      .then(r => setListe(r.data.bildirimler || []))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(() => { setYuk(true); yukle() }, [sadeceOkunmamis])

  async function tumOkundu() {
    try { await api.post('/bildirimler/0/okundu', {}); setListe(l => l.map(b => ({ ...b, okundu: true }))) } catch { /* sessiz */ }
  }
  function git(b: Bildirim) {
    if (!b.okundu) {
      api.post(`/bildirimler/${b.id}/okundu`, {}).catch(() => { })
      setListe(l => l.map(x => x.id === b.id ? { ...x, okundu: true } : x))
    }
    if (b.domain_id) { navigate(`/abonelikler/${b.domain_id}/imunify`); return }
    if (b.kategori === 'antivirus') { navigate('/antivirus'); return }
  }

  // Kategori çipleri — YÜKLENEN kümeden türetilir (yalnız var olanlar görünür).
  const katlar = Array.from(new Set(liste.map(b => b.kategori).filter(Boolean)))
  const goster = kat ? liste.filter(b => b.kategori === kat) : liste
  const okunmamisSayi = liste.filter(b => !b.okundu).length

  const cip = (deger: string, etiket: string) => (
    <button key={deger || 'hepsi'} onClick={() => setKat(deger)}
      className={`px-3 py-1.5 text-xs font-medium rounded-full border transition ${
        kat === deger
          ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300'
          : 'border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800'
      }`}>{etiket}</button>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-4xl mx-auto">
        <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir('Bildirimler') }]} />
        <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Bildirimler")}</h1>
          {okunmamisSayi > 0 && (
            <button onClick={tumOkundu}
              className="px-3 py-2 text-sm font-medium border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-200">
              {cevir("Tümünü okundu işaretle")} ({okunmamisSayi})
            </button>
          )}
        </div>

        {/* Filtreler */}
        <div className="flex flex-wrap items-center gap-2 mb-4">
          <button onClick={() => setSadeceOkunmamis(v => !v)}
            className={`px-3 py-1.5 text-xs font-medium rounded-full border transition ${
              sadeceOkunmamis
                ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300'
                : 'border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800'
            }`}>
            {sadeceOkunmamis ? cevir("● Yalnız okunmamış") : cevir("○ Yalnız okunmamış")}
          </button>
          <span className="w-px h-5 bg-slate-200 dark:bg-slate-700 mx-1" />
          {cip('', cevir("Tümü"))}
          {katlar.map(k => cip(k, cevir(katAd(k))))}
        </div>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl shadow-sm overflow-hidden">
          {yuk ? (
            <div className="px-4 py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
          ) : goster.length === 0 ? (
            <div className="px-4 py-16 text-center">
              <svg className="w-10 h-10 mx-auto mb-3 text-slate-300 dark:text-slate-600" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}><path strokeLinecap="round" strokeLinejoin="round" d="M15 17h5l-1.4-1.4A2 2 0 0118 14.2V11a6 6 0 00-4-5.7V5a2 2 0 10-4 0v.3C7.7 6.2 6 8.4 6 11v3.2c0 .5-.2 1-.6 1.4L4 17h5m6 0v1a3 3 0 11-6 0v-1" /></svg>
              <p className="text-sm text-slate-400 dark:text-slate-500">{sadeceOkunmamis ? cevir('Okunmamış bildirim yok.') : cevir('Bildirim yok.')}</p>
            </div>
          ) : (
            goster.map(b => (
              <button key={b.id} onClick={() => git(b)}
                className={`w-full text-left px-4 py-3.5 border-b border-slate-100 dark:border-slate-700/50 last:border-b-0 hover:bg-slate-50 dark:hover:bg-slate-700/40 transition flex gap-3 ${!b.okundu ? 'bg-brand-50/40 dark:bg-brand-900/10' : ''}`}>
                <span className={`mt-0.5 w-9 h-9 rounded-lg flex items-center justify-center flex-shrink-0 ${sevBg(b.seviye)}`}>
                  <KatIkon kategori={b.kategori} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-start justify-between gap-2">
                    <span className={`text-sm ${!b.okundu ? 'font-semibold text-slate-900 dark:text-slate-50' : 'font-medium text-slate-700 dark:text-slate-200'}`}>{b.baslik}</span>
                    <span className="flex items-center gap-2 flex-shrink-0">
                      <span className="text-[11px] text-slate-400 dark:text-slate-500 whitespace-nowrap">{goreliZaman(b.tarih)}</span>
                      {!b.okundu && <span className="w-2 h-2 rounded-full bg-brand-500" />}
                    </span>
                  </span>
                  <span className="block text-xs text-slate-500 dark:text-slate-400 mt-1 break-words">{yolGizle(b.mesaj)}</span>
                  <span className="inline-flex items-center gap-1 mt-2 text-[10px] font-medium uppercase tracking-wider text-slate-400 dark:text-slate-500">
                    <span className="px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700/60">{cevir(katAd(b.kategori))}</span>
                    {b.seviye === 'kritik' && <span className="px-1.5 py-0.5 rounded bg-red-100 dark:bg-red-900/30 text-red-600 dark:text-red-400">{cevir("Kritik")}</span>}
                  </span>
                </span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
