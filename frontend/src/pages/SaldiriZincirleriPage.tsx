import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Olay = { kaynak: string; asama: string; asama_ad: string; seviye: string; ozet: string; tarih: string }
type Zincir = {
  id: number; domain_id: number | null; alan_adi: string
  asamalar: string[]; asama_ad: string[]; guven: number; seviye: string; tarih: string; olaylar: Olay[]
}

// Aşama → ikon (kill-chain).
const ASAMA_IKON: Record<string, string> = {
  giris: 'M15 7a2 2 0 012 2m4 0a6 6 0 01-7.7 5.7l-2.9 2.9a1 1 0 01-.7.3H8v1a1 1 0 01-1 1H6v1a1 1 0 01-1 1H4a1 1 0 01-1-1v-2.6a1 1 0 01.3-.7l6-6A6 6 0 1121 9z',
  dosya_yazma: 'M9 13h6m-3-3v6m5 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.6a1 1 0 01.7.3l4.4 4.4a1 1 0 01.3.7V19a2 2 0 01-2 2z',
  calistirma: 'M13 10V3L4 14h7v7l9-11h-7z',
  c2: 'M8.1 12a4 4 0 015.7-5.7m2.1 2.1a4 4 0 01-5.7 5.7M3 12c2.4-4 5.6-6 9-6s6.6 2 9 6c-2.4 4-5.6 6-9 6s-6.6-2-9-6z',
  persistence: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z',
}


const SALDIRI_EN: Record<string, string> = {
  "Türkçe": "English",
  "Etkin saldırı": "Active attack",
  "Etkin saldırı zinciri yok.": "No active attack chains.",
  "Saldırı Zincirleri": "Attack Chains",
  "Tespit motorları (dosya + süreç) izliyor; zincir oluşursa burada görünür.": "Detection engines (file + process) are monitoring; if a chain forms it appears here.",
  "Şüpheli zincir": "Suspicious chain",
  "Yükleniyor…": "Loading…",
  "Dosya, süreç ve API olayları kiracı + zaman penceresinde": "File, process and API events, within the tenant + time window, are strung",
  "tek saldırı zincirine": "into a single attack chain",
  "dizilir. Nedensel bağ (aynı dosya/süreç) güveni yükseltir.": ". A causal link (same file/process) raises confidence.",
  "etkin kritik saldırı zinciri": "active critical attack chains",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SALDIRI_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function SaldiriZincirleriPage() {
  useTranslation() // dil re-render aboneligi
  const [zincirler, setZincirler] = useState<Zincir[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  function yukle() {
    api.get<{ zincirler: Zincir[] }>('/antivirus/zincirler')
      .then(r => { setZincirler(r.data.zincirler || []); setHata(null) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(() => {
    yukle()
    pollRef.current = setInterval(yukle, 15000) // canlı: 15sn'de bir tazele
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [])

  const aktifKritik = zincirler.filter(z => z.seviye === 'kritik').length

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-5xl mx-auto">
        <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Saldırı Zincirleri") }]} />
        <div className="flex items-start gap-3 mb-1">
          <span className={`mt-1 w-2.5 h-2.5 rounded-full flex-shrink-0 ${aktifKritik > 0 ? 'bg-red-500 animate-pulse' : 'bg-emerald-500'}`} />
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 leading-tight">{cevir("Saldırı Zincirleri")}</h1>
            <p className="text-sm text-slate-500 dark:text-slate-400">
              {cevir("Dosya, süreç ve API olayları kiracı + zaman penceresinde")} <b>{cevir("tek saldırı zincirine")}</b> {cevir("dizilir. Nedensel bağ (aynı dosya/süreç) güveni yükseltir.")}
            </p>
          </div>
        </div>

        {aktifKritik > 0 && (
          <div className="mt-3 mb-4 px-4 py-2.5 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-sm font-medium text-red-700 dark:text-red-300 flex items-center gap-2">
            <span className="font-mono">● {aktifKritik} {cevir("etkin kritik saldırı zinciri")}</span>
          </div>
        )}

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        {yuk ? (
          <div className="py-16 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
        ) : zincirler.length === 0 ? (
          <div className="py-16 text-center">
            <svg className="w-12 h-12 mx-auto mb-3 text-emerald-400 dark:text-emerald-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}><path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.6-4A12 12 0 0112 2.9 12 12 0 013.4 6 12 12 0 003 9c0 5.6 3.8 10.3 9 11.6 5.2-1.3 9-6 9-11.6 0-1-.1-2-.4-3z" /></svg>
            <p className="text-sm text-emerald-600 dark:text-emerald-400 font-medium">{cevir("Etkin saldırı zinciri yok.")}</p>
            <p className="text-xs text-slate-400 mt-1">{cevir("Tespit motorları (dosya + süreç) izliyor; zincir oluşursa burada görünür.")}</p>
          </div>
        ) : (
          <div className="space-y-4">
            {zincirler.map(z => {
              const kritik = z.seviye === 'kritik'
              return (
                <div key={z.id} className={`rounded-2xl border overflow-hidden shadow-sm ${kritik ? 'border-red-300 dark:border-red-800' : 'border-amber-200 dark:border-amber-800/60'}`}>
                  {/* Başlık çubuğu */}
                  <div className={`px-4 py-3 flex flex-wrap items-center justify-between gap-2 ${kritik ? 'bg-red-50 dark:bg-red-900/20' : 'bg-amber-50 dark:bg-amber-900/15'}`}>
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`w-2 h-2 rounded-full flex-shrink-0 ${kritik ? 'bg-red-500 animate-pulse' : 'bg-amber-500'}`} />
                      <span className={`font-mono text-xs font-semibold uppercase tracking-wide ${kritik ? 'text-red-700 dark:text-red-300' : 'text-amber-700 dark:text-amber-300'}`}>
                        {kritik ? cevir("Etkin saldırı") : cevir("Şüpheli zincir")}
                      </span>
                      <span className="text-sm font-medium text-slate-700 dark:text-slate-200 truncate">· {z.alan_adi || '—'}</span>
                    </div>
                    <div className="flex items-center gap-3 flex-shrink-0">
                      <span className="text-xs text-slate-400 font-mono">{z.tarih}</span>
                      <span className={`text-sm font-mono font-bold ${kritik ? 'text-red-600 dark:text-red-400' : 'text-amber-600 dark:text-amber-400'}`}>Risk %{z.guven}</span>
                    </div>
                  </div>

                  {/* Aşama zaman çizelgesi */}
                  <div className="px-4 py-3 bg-white dark:bg-slate-800">
                    <div className="flex items-center gap-1 flex-wrap">
                      {z.asamalar.map((a, i) => (
                        <span key={a} className="flex items-center gap-1">
                          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-slate-100 dark:bg-slate-700/60 text-xs font-medium text-slate-700 dark:text-slate-200">
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d={ASAMA_IKON[a] || ''} /></svg>
                            {z.asama_ad[i] || a}
                          </span>
                          {i < z.asamalar.length - 1 && <span className={`text-sm ${kritik ? 'text-red-400' : 'text-amber-400'}`}>→</span>}
                        </span>
                      ))}
                    </div>

                    {/* Olay dökümü */}
                    {z.olaylar?.length > 0 && (
                      <div className="mt-3 border-t border-slate-100 dark:border-slate-700 pt-2 space-y-1">
                        {z.olaylar.map((o, i) => (
                          <div key={i} className="flex items-start gap-2 text-xs font-mono">
                            <span className="text-slate-400 whitespace-nowrap">{o.tarih?.slice(11)}</span>
                            <span className={`px-1.5 rounded ${o.kaynak === 'dosya' ? 'bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300' : o.kaynak === 'surec' ? 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500'}`}>{o.kaynak}</span>
                            <span className="text-slate-400">{o.asama_ad}</span>
                            <span className="text-slate-600 dark:text-slate-300 break-all">{o.ozet}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
