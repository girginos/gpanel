// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Plan = {
  id: number; ad: string; aciklama: string
  disk_kota_mb: number; trafik_kota_mb: number
  max_db: number; max_ftp: number; max_email: number; saatlik_mail_limiti: number; mail_kutu_kota_mb: number
  cpu_yuzde: number; ram_mb: number; inode_kota: number; php_surum: string
  varsayilan: boolean
  [k: string]: unknown // PUT'ta bilinmeyen alanlar KAYBOLMASIN (waf, io, nginx…)
}
type Domain = { id: number; alan_adi: string; plan_id?: number; plan_ad?: string }
type Surum = { surum: string; aciklama?: string }

// Plan "ağırlığı": yükseltme mi düşürme mi olduğunu göstermek için kaba bir puan.
// Sınırsız (0) alanlar en yükseğe sayılır — aksi halde "sınırsız" düşürme görünür.
function puan(p: Plan) {
  const s = (v: number) => (v <= 0 ? 1_000_000 : v)
  return s(p.disk_kota_mb) + s(p.trafik_kota_mb) / 10 + s(p.ram_mb) * 2 + s(p.cpu_yuzde) * 4
}
const mb = (v: number) => (v <= 0 ? 'sınırsız' : v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`)
const adet = (v: number) => (v <= 0 ? 'sınırsız' : String(v))

const inp = 'w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 tabular-nums'

export default function DomainPlanPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState<number | 'ozel' | 'kaydet' | null>(null)
  // Satır-içi özelleştirme: yalnız bu hostinge ÖZEL plan düzenlenebilir.
  const [taslak, setTaslak] = useState<Plan | null>(null)
  // Mail eklentisi aktif+ödemeli ise posta limiti alanları görünür.
  const [mailAktif, setMailAktif] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    Promise.all([
      api.get<Domain>(`/domains/${id}`),
      // ?domain= → katalog planlari + YALNIZ bu hostingin ozel plani
      api.get<Plan[]>(`/plans?domain=${id}`),
      api.get<Surum[]>('/php/versions').catch(() => ({ data: [] as Surum[] })),
    ])
      .then(([d, p, s]) => { setDomain(d.data); setPlanlar(p.data || []); setSurumler(s.data || []) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
    // Mail eklentisi aktif mi? (posta limiti alanlarının gate'i)
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
  }
  useEffect(yukle, [id])

  const mevcut = planlar.find(p => p.id === domain?.plan_id) || null
  const mevcutPuan = mevcut ? puan(mevcut) : -1
  // Bu plan yalnız bu hostinge mi ait? (adı "<alan_adı> — Özel" ile başlıyorsa)
  const ozelMi = !!(mevcut && domain && mevcut.ad.startsWith(`${domain.alan_adi} — Özel`))

  async function planUygula(p: Plan) {
    if (!id) return
    setIsleniyor(p.id); setHata(null); setBasari(null); setTaslak(null)
    try {
      await api.put(`/domains/${id}/plan`, { plan_id: p.id })
      setBasari(`✓ "${p.ad}" planı uygulandı. Kaynak limitleri arka planda güncelleniyor.`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Plan değiştirilemedi'))
    } finally { setIsleniyor(null) }
  }

  // Tek tıkla özel plan: mevcut planın kopyasını bu hostinge özel oluşturur ve
  // hemen düzenleme panelini açar (kullanıcı sayfadan çıkmadan limit değiştirsin).
  async function ozelPlan() {
    if (!id) return
    setIsleniyor('ozel'); setHata(null); setBasari(null)
    try {
      const { data } = await api.post<{ plan_id: number; ad: string; zaten_ozel?: boolean }>(`/domains/${id}/ozel-plan`, {})
      setBasari(data.zaten_ozel
        ? `"${data.ad}" zaten bu hostinge özel — limitleri aşağıdan düzenleyebilirsiniz.`
        : `✓ "${data.ad}" oluşturuldu ve bu hostinge atandı. Limitleri aşağıdan düzenleyin.`)
      const { data: pl } = await api.get<{ plan: Plan }>(`/plans/${data.plan_id}`)
      setTaslak(pl.plan)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Özel plan oluşturulamadı'))
    } finally { setIsleniyor(null) }
  }

  async function duzenlemeyiAc() {
    if (!mevcut) return
    setHata(null); setBasari(null)
    try {
      const { data } = await api.get<{ plan: Plan }>(`/plans/${mevcut.id}`)
      setTaslak(data.plan)
    } catch (e) { setHata(apiHata(e, 'Plan okunamadı')) }
  }

  function T<K extends keyof Plan>(k: K, v: Plan[K]) {
    if (!taslak) return
    setTaslak({ ...taslak, [k]: v })
  }

  async function kaydet() {
    if (!taslak || !id) return
    setIsleniyor('kaydet'); setHata(null); setBasari(null)
    try {
      await api.put(`/plans/${taslak.id}`, taslak)
      // Plan kaydı limitleri hostinge KENDİLİĞİNDEN uygulamaz — yeniden atayarak uygula.
      await api.put(`/domains/${id}/plan`, { plan_id: taslak.id })
      setBasari('✓ Limitler kaydedildi ve bu hostinge uygulandı.')
      setTaslak(null)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Kaydedilemedi'))
    } finally { setIsleniyor(null) }
  }

  const phpSecenek = Array.from(new Set([...(surumler.map(s => s.surum)), taslak?.php_surum].filter(Boolean) as string[]))

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
          Bu hostingin paketini yükseltin, düşürün veya bu hostinge özel bir plan oluşturup limitlerini kendiniz belirleyin.
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
          {/* Mevcut plan + aksiyonlar */}
          <div className="mb-5 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">Mevcut plan</div>
                <div className="flex items-center gap-2">
                  <span className="text-base font-semibold text-slate-900 dark:text-slate-100">{mevcut?.ad || domain?.plan_ad || 'Plan atanmamış'}</span>
                  {ozelMi && <span className="text-[10px] uppercase font-semibold tracking-wider bg-brand-100 dark:bg-brand-900/40 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded">Bu hostinge özel</span>}
                </div>
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
                {ozelMi ? (
                  <button type="button" onClick={duzenlemeyiAc} disabled={isleniyor !== null || !!taslak}
                          className="inline-flex items-center rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                    Limitleri özelleştir
                  </button>
                ) : (
                  <button type="button" onClick={ozelPlan} disabled={isleniyor !== null}
                          className="inline-flex items-center rounded-md bg-brand-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                    {isleniyor === 'ozel' ? 'Oluşturuluyor…' : 'Tek tıkla özel plan'}
                  </button>
                )}
                {mevcut && (
                  <Link to={`/araclar/paketler/${mevcut.id}`}
                        className="inline-flex items-center rounded-md border border-slate-300 dark:border-slate-600 px-3 py-1.5 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                    Tüm ayarlar
                  </Link>
                )}
              </div>
            </div>
            <p className="mt-2 text-xs text-slate-400 dark:text-slate-500">
              {ozelMi
                ? 'Bu plan yalnız bu hostinge ait — limitlerini değiştirmek başka hiçbir hostingi etkilemez.'
                : 'Bu plan başka hostingler tarafından da kullanılıyor olabilir. Limitleri yalnız bu hosting için değiştirmek üzere önce “Tek tıkla özel plan” oluşturun.'}
            </p>
          </div>

          {/* Satır-içi özelleştirme paneli */}
          {taslak && (
            <div className="mb-5 rounded-lg border border-brand-300 dark:border-brand-800 bg-brand-50/40 dark:bg-brand-950/20 p-4">
              <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Kaynak limitleri — {taslak.ad}</h2>
                <span className="text-xs text-slate-500 dark:text-slate-400">0 = sınırsız</span>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <Alan etiket="Disk (MB)"><input type="number" min={0} className={inp} value={taslak.disk_kota_mb} onChange={e => T('disk_kota_mb', Number(e.target.value))} /></Alan>
                <Alan etiket="Trafik (MB/ay)"><input type="number" min={0} className={inp} value={taslak.trafik_kota_mb} onChange={e => T('trafik_kota_mb', Number(e.target.value))} /></Alan>
                <Alan etiket="RAM (MB)"><input type="number" min={0} className={inp} value={taslak.ram_mb} onChange={e => T('ram_mb', Number(e.target.value))} /></Alan>
                <Alan etiket="CPU (%)" ipucu="100 = 1 çekirdek"><input type="number" min={0} className={inp} value={taslak.cpu_yuzde} onChange={e => T('cpu_yuzde', Number(e.target.value))} /></Alan>
                <Alan etiket="Veritabanı"><input type="number" min={0} className={inp} value={taslak.max_db} onChange={e => T('max_db', Number(e.target.value))} /></Alan>
                <Alan etiket="FTP hesabı"><input type="number" min={0} className={inp} value={taslak.max_ftp} onChange={e => T('max_ftp', Number(e.target.value))} /></Alan>
                {mailAktif && (
                  <>
                    <Alan etiket="E-posta kutusu" ipucu="Mail eklentisi — posta kutusu sayısı"><input type="number" min={0} className={inp} value={taslak.max_email} onChange={e => T('max_email', Number(e.target.value))} /></Alan>
                    <Alan etiket="Saatlik gönderim" ipucu="Kutu başına giden mail/saat"><input type="number" min={0} className={inp} value={taslak.saatlik_mail_limiti} onChange={e => T('saatlik_mail_limiti', Number(e.target.value))} /></Alan>
                    <Alan etiket="Kutu depolama (MB)" ipucu="Posta kutusu başına disk kotası; 0 = sınırsız"><input type="number" min={0} className={inp} value={taslak.mail_kutu_kota_mb} onChange={e => T('mail_kutu_kota_mb', Number(e.target.value))} /></Alan>
                  </>
                )}
                <Alan etiket="Inode (dosya sayısı)"><input type="number" min={0} className={inp} value={taslak.inode_kota} onChange={e => T('inode_kota', Number(e.target.value))} /></Alan>
                <Alan etiket="PHP sürümü">
                  <select className={inp} value={taslak.php_surum} onChange={e => T('php_surum', e.target.value)}>
                    {phpSecenek.map(v => <option key={v} value={v}>PHP {v}</option>)}
                  </select>
                </Alan>
              </div>
              <div className="mt-4 flex flex-wrap gap-2">
                <button type="button" onClick={kaydet} disabled={isleniyor !== null}
                        className="inline-flex items-center rounded-md bg-brand-600 px-3.5 py-2 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
                  {isleniyor === 'kaydet' ? 'Kaydediliyor…' : 'Kaydet ve uygula'}
                </button>
                <button type="button" onClick={() => setTaslak(null)} disabled={isleniyor !== null}
                        className="inline-flex items-center rounded-md border border-slate-300 dark:border-slate-600 px-3.5 py-2 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-white dark:hover:bg-slate-800 disabled:opacity-60">
                  Vazgeç
                </button>
                <Link to={`/araclar/paketler/${taslak.id}`} className="inline-flex items-center px-1 py-2 text-sm text-brand-700 dark:text-brand-300 hover:underline">
                  Gelişmiş ayarlar (WAF, nginx, IO) →
                </Link>
              </div>
            </div>
          )}

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
                    <div className="mt-auto flex gap-2">
                      {bu ? (
                        <>
                          <span className="flex-1 rounded-md border border-slate-200 dark:border-slate-700 px-3 py-1.5 text-sm text-center text-slate-400 dark:text-slate-500">Kullanımda</span>
                          {ozelMi && (
                            <button type="button" onClick={duzenlemeyiAc} disabled={isleniyor !== null || !!taslak}
                                    className="shrink-0 rounded-md border border-brand-300 dark:border-brand-700 px-3 py-1.5 text-sm font-medium text-brand-700 dark:text-brand-300 hover:bg-brand-50 dark:hover:bg-brand-950/40 disabled:opacity-60">
                              Düzenle
                            </button>
                          )}
                        </>
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

function Alan({ etiket, ipucu, children }: { etiket: string; ipucu?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-slate-600 dark:text-slate-300 mb-1">{etiket}</span>
      {children}
      {ipucu && <span className="mt-1 block text-[11px] text-slate-400 dark:text-slate-500">{ipucu}</span>}
    </label>
  )
}
