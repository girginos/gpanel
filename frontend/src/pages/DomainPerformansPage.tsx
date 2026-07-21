import { useEffect, useState } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Oge = { ad: string; aktif: boolean; deger: string; ayar: string; aciklama: string }
type Oneri = { metin: string; onem: string; ayar: string }
type Ozet = { alan_adi: string; php_surum: string; skor: number; ogeler: Oge[]; oneriler: Oneri[] }

export default function DomainPerformansPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [o, setO] = useState<Ozet | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    setYuk(true)
    api.get<Ozet>(`/domains/${id}/performans`)
      .then(r => setO(r.data)).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }, [id])

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">Yükleniyor…</div>
  if (!o) return <div className="px-4 py-4 sm:px-6 sm:py-5"><div className="text-sm text-red-600">{hata || 'Bulunamadı'}</div></div>

  const skorRenk = o.skor >= 80 ? 'emerald' : o.skor >= 60 ? 'amber' : 'rose'
  const skorHex: Record<string, string> = { emerald: '#10b981', amber: '#f59e0b', rose: '#f43f5e' }
  const git = (slug: string) => navigate(`/abonelikler/${id}/${slug}`)
  const onemRenk: Record<string, string> = { yuksek: 'text-rose-500', orta: 'text-amber-500', bilgi: 'text-emerald-500' }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-4xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: o.alan_adi, href: `/abonelikler/${id}` },
          { etiket: 'Performans' },
        ]} />
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Performans ve Hızlandırıcılar</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-5"><span className="font-mono">{o.alan_adi}</span> — mevcut hızlandırıcı durumu ve öneriler.</p>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-4">
          {/* Skor halkası */}
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm flex flex-col items-center justify-center">
            <div className="relative w-28 h-28">
              <svg viewBox="0 0 36 36" className="w-full h-full -rotate-90">
                <circle cx="18" cy="18" r="15.9" fill="none" className="stroke-slate-100 dark:stroke-slate-700" strokeWidth="3" />
                <circle cx="18" cy="18" r="15.9" fill="none" stroke={skorHex[skorRenk]} strokeWidth="3" strokeLinecap="round"
                  strokeDasharray={`${o.skor} 100`} />
              </svg>
              <div className="absolute inset-0 flex flex-col items-center justify-center">
                <span className="text-2xl font-bold text-slate-800 dark:text-slate-100">{o.skor}</span>
                <span className="text-[10px] text-slate-400">/ 100</span>
              </div>
            </div>
            <div className="mt-2 text-sm font-medium text-slate-600 dark:text-slate-300">Performans Skoru</div>
          </div>

          {/* Hızlandırıcı durumları */}
          <div className="sm:col-span-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Hızlandırıcılar</h3>
            <div className="space-y-2">
              {o.ogeler.map(og => (
                <div key={og.ad} className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-50 dark:border-slate-800 last:border-0">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`w-2 h-2 rounded-full ${og.aktif ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
                      <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{og.ad}</span>
                      <span className="text-xs font-mono text-slate-400">{og.deger}</span>
                    </div>
                    <p className="text-[11px] text-slate-400 ml-4 truncate">{og.aciklama}</p>
                  </div>
                  {og.ayar && <button onClick={() => git(og.ayar)} className="shrink-0 text-xs text-brand-600 dark:text-brand-400 hover:underline">Ayarla →</button>}
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Öneriler */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Öneriler</h3>
          <ul className="space-y-2">
            {o.oneriler.map((n, i) => (
              <li key={i} className="flex items-start gap-2 text-sm">
                <span className={`mt-0.5 ${onemRenk[n.onem] || 'text-slate-400'}`}>●</span>
                <span className="text-slate-600 dark:text-slate-300 flex-1">{n.metin}</span>
                {n.ayar && <button onClick={() => git(n.ayar)} className="shrink-0 text-xs text-brand-600 dark:text-brand-400 hover:underline">Git →</button>}
              </li>
            ))}
          </ul>
        </div>

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}
