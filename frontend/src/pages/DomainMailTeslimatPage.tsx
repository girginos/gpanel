import { useEffect, useState, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'

type Kayit = {
  zaman: string; qid: string; gonderen: string; alici: string
  durum: string; relay: string; detay: string
}

const DURUM: Record<string, { renk: string; etiket: string }> = {
  sent: { renk: 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300', etiket: 'Teslim' },
  bounced: { renk: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300', etiket: 'Reddedildi' },
  deferred: { renk: 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300', etiket: 'Ertelendi' },
}
function rozet(durum: string) {
  return DURUM[durum] || { renk: 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300', etiket: durum }
}

export default function DomainMailTeslimatPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [kayitlar, setKayitlar] = useState<Kayit[] | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [yenileniyor, setYenileniyor] = useState(false)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, 'Domain yüklenemedi')))
  }, [id])

  const yukle = useCallback((d: Domain) => {
    setYenileniyor(true)
    api.get<{ kayitlar: Kayit[] }>(`/eklenti/mail/teslimat?domain=${encodeURIComponent(d.alan_adi)}&limit=150`)
      .then(r => setKayitlar(r.data.kayitlar || []))
      .catch(e => { setHata(apiHata(e, 'Teslimat kayıtları alınamadı')); setKayitlar([]) })
      .finally(() => setYenileniyor(false))
  }, [])

  useEffect(() => { if (domain) yukle(domain) }, [domain, yukle])

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
    </div>
  )

  const sayac = (d: string) => (kayitlar || []).filter(k => k.durum === d).length

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: 'E-Posta Teslimat Takibi' },
      ]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300">E-Posta Teslimat Takibi</h1>
        <button onClick={() => yukle(domain)} disabled={yenileniyor}
          className="inline-flex items-center gap-1.5 text-sm px-3 py-1.5 rounded-lg border border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-700/50 text-slate-700 dark:text-slate-200 disabled:opacity-60">
          <svg className={`w-4 h-4 ${yenileniyor ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
          Yenile
        </button>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">{domain.alan_adi} gönderilen/alınan son e-postaların teslimat durumu (sunucu günlüğünden).</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {kayitlar && kayitlar.length > 0 && (
        <div className="flex items-center gap-2 mb-3 text-xs">
          <span className={`px-2 py-0.5 rounded ${DURUM.sent.renk}`}>{sayac('sent')} Teslim</span>
          <span className={`px-2 py-0.5 rounded ${DURUM.deferred.renk}`}>{sayac('deferred')} Ertelendi</span>
          <span className={`px-2 py-0.5 rounded ${DURUM.bounced.renk}`}>{sayac('bounced')} Reddedildi</span>
        </div>
      )}

      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
        {kayitlar === null && <div className="py-10 text-center text-sm text-slate-400">Yükleniyor…</div>}
        {kayitlar?.length === 0 && <div className="py-10 text-center text-sm text-slate-400">Bu domain için teslimat kaydı bulunamadı.</div>}
        {kayitlar && kayitlar.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wider text-slate-500 dark:text-slate-400 border-b border-slate-100 dark:border-slate-700">
                  <th className="px-4 py-2.5 font-semibold">Zaman</th>
                  <th className="px-4 py-2.5 font-semibold">Gönderen</th>
                  <th className="px-4 py-2.5 font-semibold">Alıcı</th>
                  <th className="px-4 py-2.5 font-semibold">Durum</th>
                  <th className="px-4 py-2.5 font-semibold">Ayrıntı</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-50 dark:divide-slate-700/50">
                {kayitlar.map((k, i) => {
                  const r = rozet(k.durum)
                  return (
                    <tr key={i} className="hover:bg-slate-50 dark:hover:bg-slate-700/30">
                      <td className="px-4 py-2 whitespace-nowrap text-slate-500 dark:text-slate-400 font-mono text-xs">{k.zaman}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-700 dark:text-slate-300 max-w-[160px] truncate" title={k.gonderen}>{k.gonderen || '—'}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-700 dark:text-slate-300 max-w-[180px] truncate" title={k.alici}>{k.alici}</td>
                      <td className="px-4 py-2"><span className={`text-[10px] font-bold px-2 py-0.5 rounded uppercase ${r.renk}`}>{r.etiket}</span></td>
                      <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400 max-w-[240px] truncate" title={k.detay}>{k.detay}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
