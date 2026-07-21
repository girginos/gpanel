// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import DomainList, { type Domain } from '@/components/DomainList'
import DomainPano from '@/components/DomainPano'
import ResourceCard from '@/components/ResourceCard'
import { useAuth } from '@/store/auth'

export default function DashboardPage() {
  const kullanici = useAuth((s) => s.kullanici)
  const [params, setParams] = useSearchParams()
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)

  useEffect(() => {
    setYukleniyor(true)
    api.get<Domain[]>('/domains')
      .then((r) => {
        setDomainler(r.data)
        if (!params.get('domain') && r.data.length > 0) {
          setParams({ domain: String(r.data[0].id) }, { replace: true })
        }
      })
      .catch((e) => setHata(apiHata(e)))
      .finally(() => setYukleniyor(false))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const seciliId = Number(params.get('domain')) || domainler[0]?.id
  const secili = domainler.find((d) => d.id === seciliId) || domainler[0]

  function secimYap(id: number) {
    setParams({ domain: String(id) })
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1600px]">
      <div className="mb-5 flex items-baseline justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Pano</h1>
          <p className="text-sm text-slate-500 dark:text-slate-500 mt-0.5">
            Hoş geldiniz, <span className="text-slate-700 dark:text-slate-300 font-medium">{kullanici?.ad_soyad || kullanici?.adi}</span>
          </p>
        </div>
        {secili && (
          <div className="text-right text-xs text-slate-500 dark:text-slate-500">
            <span className="block">Seçili domain</span>
            <span className="text-brand-700 dark:text-brand-300 font-mono font-semibold text-sm">{secili.alan_adi}</span>
          </div>
        )}
      </div>

      {hata && (
        <div className="mb-4 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">
          {hata}
        </div>
      )}

      <div className="grid grid-cols-12 gap-5">
        <aside className="col-span-12 lg:col-span-3">
          <DomainList items={domainler} seciliId={secili?.id} onSec={secimYap} yukleniyor={yukleniyor} />
        </aside>

        <section className="col-span-12 lg:col-span-6">
          {secili ? (
            <DomainPano domain={secili} />
          ) : (
            <div className="bg-white dark:bg-slate-800 border-2 border-dashed border-slate-200 dark:border-slate-700 rounded-2xl p-12 text-center text-slate-500 dark:text-slate-500">
              {yukleniyor ? 'Yükleniyor…' : 'Henüz domain yok. Sol panelden ekleyin.'}
            </div>
          )}
        </section>

        <aside className="col-span-12 lg:col-span-3">
          <ResourceCard />
        </aside>
      </div>
    </div>
  )
}