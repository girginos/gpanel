// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Plan = {
  id: number; ad: string; aciklama: string
  disk_kota_mb: number; trafik_kota_mb: number
  max_db: number; max_ftp: number; max_email: number
  cpu_yuzde: number; ram_mb: number; php_surum: string
  varsayilan: boolean
}
type Domain = { id: number; alan_adi: string; plan_id?: number; plan_ad?: string }

// Plan "ağırlığı": yükseltme mi düşürme mi olduğunu göstermek için kaba bir puan.
// Sınırsız (0) alanlar en yükseğe sayılır — aksi halde "sınırsız" düşürme görünür.
function puan(p: Plan) {
  const s = (v: number) => (v <= 0 ? 1_000_000 : v)
  return s(p.disk_kota_mb) + s(p.trafik_kota_mb) / 10 + s(p.ram_mb) * 2 + s(p.cpu_yuzde) * 4
}
const mb = (v: number) => (v <= 0 ? 'sınırsız' : v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`)
const adet = (v: number) => (v <= 0 ? 'sınırsız' : String(v))

export default function DomainPlanPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState<number | 'ozel' | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    Promise.all([
      api.get<Domain>(`/domains/${id}`),
      api.get<Plan[]>('/plans'),
    ])
      .then(([d, p]) => { setDomain(d.data); setPlanlar(p.data || []) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function planUygula(p: Plan) {
    if (!id) return
    setIsleniyor(p.id); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/plan`, { plan_id: p.id })
      setBasari(`✓ "${p.ad}" planı uygulandı. Kaynak limitleri arka planda güncelleniyor.`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Plan değiştirilemedi'))
    } finally { setIsleniyor(null) }
  }

  async function ozelPlan() {
    if (!id) return
    setIsleniyor('ozel'); setHata(null); setBasari(null)
    try {
      const { data } = await api.post<{ plan_id: number; ad: string }>(`/domains/${id}/ozel-plan`, {})
      setBasari(`✓ "${data.ad}" oluşturuldu ve bu hostinge atandı. Limitleri plan sayfasından düzenleyebilirsiniz.`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Özel plan oluşturulamadı'))
    } finally { setIsleniyor(null) }
  }

  const mevcut = planlar.find(p => p.id === domain?.plan_id) || null
  const mevcutPuan = mevcut ? puan(mevcut) : -1

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '…', href: `/abonelikler/${id}` },
        { etiket: 'Hosting Planı' },
      ]} />

      <div className="mb-4">
        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">Hosting Planı</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Bu hostingin paketini yükseltin, düşürün veya tek tıkla bu hostinge özel bir plan oluşturun.
        </p>
      </div>

      {hata && <div role="alert" className="mb-3 text-sm rounded-md border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-950/30 text-red-700 dark:text-red-300 px-3 py-2">{hata}</div>}
      {basari && <div role="status" className="mb-3 text-sm rounded-md border border-emerald-200 dark:border-emerald-900/50 bg-emerald-50 dark:bg-emerald-950/30 text-emerald-700 dark:text-emerald-300 px-3 py-2">{basari}</div>}

      {yuk ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Planlar yükleniyor">
          {Array.from({ length: 3 }).map((_, i) => <div key={i} className="h-44 rounded-lg bg-slate-100 dark:bg-slate-800 animate-pulse" />)}
        </div>
      ) : (
        <>
          {/* Mevcut plan özeti */}
          <div className="mb-5 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">Mevcut plan</div>
                <div className="text-base font-semibold text-slate-900 dark:text-slate-100">{mevcut?.ad || domain?.plan_ad || 'Plan atanmamış'}</div>
                {mevcut && (
                  <dl className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-sm text-slate-600 dark:text-slate-300 tabular-nums">
                    <div><dt className="inline text-slate-400 dark:text-slate-500">Disk </dt><dd className="inline font-medium">{mb(mevcut.disk_kota_mb)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">Trafik </dt><dd className="inline font-medium">{mb(mevcut.trafik_kota_mb)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">RAM </dt><dd className="inline font-medium">{mb(mevcut.ram_mb)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">CPU </dt><dd className="inline font-medium">{mevcut.cpu_yuzde <= 0 ? 'sınırsız' : `%${mevcut.cpu_yuzde}`}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">PHP </dt><dd className="inline font-medium">{mevcut.php_surum}</dd></div>
                  </dl>
                )}
              </div>
              <div className="flex flex-wrap gap-2">
                {mevcut && (
                  <Link to={`/araclar/paketler/${mevcut.id}`}
                        className="inline-flex items-center rounded-md border border-slate-300 dark:border-slate-600 px-3 py-1.5 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                    Planı düzenle
                  </Link>
                )}
                <button type="button" onClick={ozelPlan} disabled={isleniyor !== null}
                        className="inline-flex items-center rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                  {isleniyor === 'ozel' ? 'Oluşturuluyor…' : 'Tek tıkla özel plan'}
                </button>
              </div>
            </div>
            <p className="mt-2 text-xs text-slate-400 dark:text-slate-500">
              Özel plan: mevcut planın bir kopyası bu hostinge özel olarak oluşturulur; limitlerini
              diğer hostingleri etkilemeden serbestçe değiştirebilirsiniz.
            </p>
          </div>

          {planlar.length === 0 ? (
            <div role="status" className="text-center py-12 rounded-lg border border-dashed border-slate-300 dark:border-slate-700">
              <h3 className="text-sm font-medium text-slate-700 dark:text-slate-200">Tanımlı plan yok</h3>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">Önce bir hosting planı oluşturun.</p>
              <Link to="/araclar/paketler" className="mt-4 inline-flex items-center rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700">Planlara git</Link>
            </div>
          ) : (
            <ul role="list" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {[...planlar].sort((a, b) => puan(a) - puan(b)).map(p => {
                const bu = p.id === domain?.plan_id
                const fark = puan(p) - mevcutPuan
                const yon = !mevcut ? 'geç' : fark > 0 ? 'yükselt' : fark < 0 ? 'düşür' : 'geç'
                return (
                  <li key={p.id} className={`rounded-lg border p-4 flex flex-col gap-3 ${bu ? 'border-brand-500 dark:border-brand-500 bg-brand-50/40 dark:bg-brand-950/20' : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900'}`}>
                    <div>
                      <div className="flex items-start justify-between gap-2">
                        <h3 className="font-semibold text-slate-900 dark:text-slate-100">{p.ad}</h3>
                        {bu && <span className="shrink-0 text-[10px] uppercase font-semibold tracking-wider bg-brand-100 dark:bg-brand-900/40 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded">Aktif</span>}
                      </div>
                      <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400 line-clamp-2">{p.aciklama || 'Açıklama yok'}</p>
                    </div>
                    <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-sm text-slate-600 dark:text-slate-300 tabular-nums">
                      <div><dt className="inline text-slate-400 dark:text-slate-500">Disk </dt><dd className="inline font-medium">{mb(p.disk_kota_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">Trafik </dt><dd className="inline font-medium">{mb(p.trafik_kota_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">RAM </dt><dd className="inline font-medium">{mb(p.ram_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">CPU </dt><dd className="inline font-medium">{p.cpu_yuzde <= 0 ? '∞' : `%${p.cpu_yuzde}`}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">DB </dt><dd className="inline font-medium">{adet(p.max_db)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">FTP </dt><dd className="inline font-medium">{adet(p.max_ftp)}</dd></div>
                    </dl>
                    <div className="mt-auto">
                      {bu ? (
                        <button type="button" disabled className="w-full rounded-md border border-slate-200 dark:border-slate-700 px-3 py-1.5 text-sm text-slate-400 dark:text-slate-500 cursor-default">Kullanımda</button>
                      ) : (
                        <button type="button" onClick={() => planUygula(p)} disabled={isleniyor !== null}
                                className={`w-full rounded-md px-3 py-1.5 text-sm font-medium text-white disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-1 dark:focus-visible:ring-offset-slate-900 ${
                                  yon === 'düşür'
                                    ? 'bg-slate-600 hover:bg-slate-700 focus-visible:ring-slate-500'
                                    : 'bg-brand-600 hover:bg-brand-700 focus-visible:ring-brand-500'}`}>
                          {isleniyor === p.id ? 'Uygulanıyor…' : yon === 'yükselt' ? '↑ Yükselt' : yon === 'düşür' ? '↓ Düşür' : 'Bu plana geç'}
                        </button>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
