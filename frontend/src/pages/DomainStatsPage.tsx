import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type KV = { ad: string; sayi: number }
type Gun = { tarih: string; istek: number }
type Ozet = {
  alan_adi: string; log_var: boolean
  toplam_istek: number; toplam_bant_mb: number; tekil_ip: number; bot_orani: number
  durum_grup: Record<string, number>
  top_yollar: KV[]; top_ip: KV[]; top_durum: KV[]; gunluk: Gun[]; son_istekler: string[]
}

export default function DomainStatsPage() {
  const { id } = useParams()
  const [o, setO] = useState<Ozet | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Ozet>(`/domains/${id}/istatistik`)
      .then(r => setO(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  if (yuk) return <div className="px-6 py-5 text-slate-400">Yükleniyor…</div>
  if (!o) return <div className="px-6 py-5"><div className="text-sm text-red-600">{hata || 'Bulunamadı'}</div></div>

  const maxGun = Math.max(1, ...o.gunluk.map(g => g.istek))
  const durumBar: Record<string, string> = { '2xx': 'bg-emerald-500', '3xx': 'bg-sky-500', '4xx': 'bg-amber-500', '5xx': 'bg-rose-500' }

  return (
    <div className="px-6 py-5">
      <div className="max-w-5xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: o.alan_adi, href: `/abonelikler/${id}` },
          { etiket: 'İstatistikler' },
        ]} />

        <div className="flex items-center justify-between mb-4">
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Trafik İstatistikleri</h1>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-1"><span className="font-mono">{o.alan_adi}</span> — nginx erişim günlüğü analizi.</p>
          </div>
          <button onClick={yukle} className="text-sm px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-800">↻ Yenile</button>
        </div>

        {!o.log_var || o.toplam_istek === 0 ? (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-10 text-center text-sm text-slate-400">
            Henüz erişim günlüğü verisi yok. Site trafik almaya başladığında burada görünür.
          </div>
        ) : (
          <>
            {/* KPI kartları */}
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-4">
              <KPI etiket="Toplam İstek" deger={o.toplam_istek.toLocaleString('tr-TR')} renk="indigo" />
              <KPI etiket="Bant Kullanımı" deger={`${o.toplam_bant_mb.toFixed(1)} MB`} renk="sky" />
              <KPI etiket="Tekil IP" deger={o.tekil_ip.toLocaleString('tr-TR')} renk="emerald" />
              <KPI etiket="Bot Oranı" deger={`%${o.bot_orani}`} renk={o.bot_orani >= 50 ? 'rose' : 'violet'} />
            </div>

            {/* Durum dağılımı */}
            <Kart baslik="HTTP Durum Dağılımı">
              <div className="space-y-2">
                {(['2xx', '3xx', '4xx', '5xx'] as const).map(g => {
                  const v = o.durum_grup[g] || 0
                  const oran = o.toplam_istek ? Math.round(v / o.toplam_istek * 100) : 0
                  return (
                    <div key={g} className="flex items-center gap-3">
                      <span className="w-10 text-xs font-mono text-slate-500">{g}</span>
                      <div className="flex-1 h-3 rounded-full bg-slate-100 dark:bg-slate-700/50 overflow-hidden">
                        <div className={`h-full rounded-full ${durumBar[g]}`} style={{ width: Math.max(oran, v > 0 ? 2 : 0) + '%' }} />
                      </div>
                      <span className="w-24 text-right text-xs font-mono text-slate-600 dark:text-slate-300">{v.toLocaleString('tr-TR')} <span className="text-slate-400">%{oran}</span></span>
                    </div>
                  )
                })}
              </div>
            </Kart>

            {/* Günlük istek (7 gün) */}
            {o.gunluk.length > 0 && (
              <Kart baslik="Günlük İstek (son 7 gün)">
                <div className="flex items-end gap-2 h-32">
                  {o.gunluk.map(g => (
                    <div key={g.tarih} className="flex-1 flex flex-col items-center gap-1">
                      <div className="w-full flex items-end justify-center" style={{ height: '100px' }}>
                        <div className="w-full max-w-[36px] rounded-t bg-gradient-to-t from-brand-600 to-brand-400" style={{ height: Math.max(4, g.istek / maxGun * 100) + '%' }} title={`${g.istek} istek`} />
                      </div>
                      <span className="text-[10px] text-slate-400 font-mono">{g.tarih.split('/')[0]}</span>
                      <span className="text-[10px] text-slate-600 dark:text-slate-300 font-mono">{g.istek}</span>
                    </div>
                  ))}
                </div>
              </Kart>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <Kart baslik="En Çok İstenen Yollar">
                <Tablo rows={o.top_yollar} birim="istek" mono />
              </Kart>
              <Kart baslik="En Aktif IP'ler">
                <Tablo rows={o.top_ip} birim="istek" mono />
              </Kart>
            </div>

            <Kart baslik="Son İstekler">
              <div className="font-mono text-xs space-y-1 max-h-64 overflow-y-auto">
                {o.son_istekler.map((s, i) => {
                  const kod = s.slice(0, 3)
                  const renk = kod[0] === '5' ? 'text-rose-500' : kod[0] === '4' ? 'text-amber-500' : kod[0] === '3' ? 'text-sky-500' : 'text-emerald-500'
                  return <div key={i} className="truncate"><span className={renk}>{kod}</span><span className="text-slate-500 dark:text-slate-400">{s.slice(3)}</span></div>
                })}
              </div>
            </Kart>
          </>
        )}

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}

function KPI({ etiket, deger, renk }: { etiket: string; deger: string; renk: string }) {
  const map: Record<string, string> = { indigo: 'text-indigo-500', sky: 'text-sky-500', emerald: 'text-emerald-500', violet: 'text-violet-500', rose: 'text-rose-500' }
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm">
      <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-400">{etiket}</div>
      <div className={`text-2xl font-bold font-mono mt-1 ${map[renk] || 'text-slate-700'}`}>{deger}</div>
    </div>
  )
}
function Kart({ baslik, children }: { baslik: string; children: React.ReactNode }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
      <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{baslik}</h3>
      {children}
    </div>
  )
}
function Tablo({ rows, birim, mono }: { rows: KV[]; birim: string; mono?: boolean }) {
  const max = Math.max(1, ...rows.map(r => r.sayi))
  if (!rows.length) return <div className="text-sm text-slate-400 py-3">Veri yok.</div>
  return (
    <div className="space-y-1.5">
      {rows.map((r, i) => (
        <div key={i} className="relative flex items-center justify-between text-xs py-1 px-2 rounded overflow-hidden">
          <div className="absolute inset-0 bg-brand-500/10 dark:bg-brand-500/15" style={{ width: (r.sayi / max * 100) + '%' }} />
          <span className={`relative truncate ${mono ? 'font-mono' : ''} text-slate-700 dark:text-slate-200`} title={r.ad}>{r.ad}</span>
          <span className="relative shrink-0 ml-2 font-mono text-slate-500 dark:text-slate-400">{r.sayi.toLocaleString('tr-TR')} {birim}</span>
        </div>
      ))}
    </div>
  )
}
