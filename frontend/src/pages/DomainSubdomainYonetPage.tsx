import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import SubPHPAyarlari from '@/components/SubPHPAyarlari'
import SubWebAyarlari from '@/components/SubWebAyarlari'

type Detay = {
  id: number; alt_ad: string; tam_ad: string; php_surum: string
  docroot: string; created_at: string; parent_ad: string; parent_id: number
  disk_kb: number; ipv4: string
}
type PHPVer = { surum: string; aciklama?: string }

function fmtKB(kb: number) {
  if (!kb) return '0 KB'
  if (kb < 1024) return kb + ' KB'
  if (kb < 1024 * 1024) return (kb / 1024).toFixed(1) + ' MB'
  return (kb / 1024 / 1024).toFixed(2) + ' GB'
}

export default function DomainSubdomainYonetPage() {
  const { id, sid } = useParams()
  const navigate = useNavigate()
  const [d, setD] = useState<Detay | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)

  const [phpler, setPhpler] = useState<PHPVer[]>([])
  const [yeniPHP, setYeniPHP] = useState('')
  const [phpKaydet, setPhpKaydet] = useState(false)

  const [sslAktif, setSslAktif] = useState<boolean | null>(null)
  const [sslMesgul, setSslMesgul] = useState(false)

  function yukle() {
    if (!id || !sid) return
    setYuk(true)
    api.get<Detay>(`/domains/${id}/subdomain/${sid}`)
      .then(r => { setD(r.data); setYeniPHP(r.data.php_surum) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
    api.get<{ aktif: boolean }>(`/domains/${id}/subdomain/${sid}/ssl`)
      .then(r => setSslAktif(r.data.aktif)).catch(() => setSslAktif(null))
  }
  useEffect(yukle, [id, sid])
  useEffect(() => {
    api.get<PHPVer[]>('/php/versions').then(r => setPhpler(r.data || [])).catch(() => setPhpler([]))
  }, [])

  async function phpDegistir() {
    if (!d || yeniPHP === d.php_surum) return
    setHata(null); setOk(null); setPhpKaydet(true)
    try {
      await api.put(`/domains/${id}/subdomain/${sid}/php`, { php_surum: yeniPHP })
      setOk(`PHP surumu ${yeniPHP} olarak guncellendi.`)
      setD({ ...d, php_surum: yeniPHP })
    } catch (e) { setHata(apiHata(e, 'PHP degistirilemedi')) }
    finally { setPhpKaydet(false) }
  }

  async function sslKur(tip: 'letsencrypt' | 'self-signed') {
    setHata(null); setOk(null); setSslMesgul(true)
    try {
      await api.post(`/domains/${id}/subdomain/${sid}/ssl`, { tip })
      setOk(`SSL kuruldu (${tip === 'letsencrypt' ? "Let's Encrypt" : 'oz-imzali'}). Artik https:// ile erisilebilir.`)
      setSslAktif(true)
    } catch (e) { setHata(apiHata(e, 'SSL kurulamadi')) }
    finally { setSslMesgul(false) }
  }
  async function sslKaldir() {
    if (!confirm('SSL sertifikasi kaldirilsin mi? Site sadece http:// ile erisilebilir olur.')) return
    setHata(null); setOk(null); setSslMesgul(true)
    try {
      await api.delete(`/domains/${id}/subdomain/${sid}/ssl`)
      setOk('SSL kaldirildi.'); setSslAktif(false)
    } catch (e) { setHata(apiHata(e, 'SSL kaldirilamadi')) }
    finally { setSslMesgul(false) }
  }

  async function sil() {
    if (!d) return
    if (!confirm(`${d.tam_ad} subdomaini silinsin mi?\nvhost + dosyalari (docroot) + DNS kaydi kaldirilir. Geri alinamaz.`)) return
    try {
      await api.delete(`/domains/${id}/subdomain/${sid}`)
      navigate(`/abonelikler/${id}/subdomainler`)
    } catch (e) { setHata(apiHata(e, 'Silinemedi')) }
  }

  const kart = 'rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-5'
  const etiket = 'text-[11px] uppercase tracking-wider text-slate-400 dark:text-slate-500'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-5xl">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: d?.parent_ad || '...', href: `/abonelikler/${id}` },
        { etiket: 'Subdomainler', href: `/abonelikler/${id}/subdomainler` },
        { etiket: d?.tam_ad || '...' },
      ]} />

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {ok && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{ok}</div>}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">Yukleniyor...</div>
      ) : !d ? (
        <div className="py-12 text-center text-sm text-slate-400">Subdomain bulunamadi.</div>
      ) : (
        <>
          <div className="flex items-start justify-between gap-3 mb-5 flex-wrap">
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{d.tam_ad}</h1>
                <span className="text-[10px] uppercase tracking-wider bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300 px-1.5 py-0.5 rounded">alt alan</span>
              </div>
              <p className="text-sm text-slate-500 mt-1">{d.parent_ad} alan adinin alt alani &middot; kendi yonetim paneli</p>
            </div>
            <div className="flex items-center gap-2">
              <a href={`${sslAktif ? 'https' : 'http'}://${d.tam_ad}`} target="_blank" rel="noreferrer"
                className="text-sm px-3 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700">
                Ziyaret Et &#8599;
              </a>
              <button onClick={sil}
                className="text-sm px-3 py-1.5 rounded-md bg-red-600 hover:bg-red-700 text-white font-medium">
                Sil
              </button>
            </div>
          </div>

          {/* Uygulamalar */}
          <div className="mb-4">
            <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200 mb-2">Araclar</h2>
            <div className="grid gap-2 sm:grid-cols-3">
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/wordpress`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-sky-100 dark:bg-sky-900/30 text-sky-600 dark:text-sky-400 flex items-center justify-center text-lg font-bold">W</div>
              <div className="flex-1">
                <div className="text-sm font-semibold text-slate-800 dark:text-slate-200">WordPress</div>
                <div className="text-xs text-slate-500">1-tikla kurulum &middot; eklenti/tema &middot; yonetim &mdash; bu alt alana kurulur</div>
              </div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/composer`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-amber-100 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400 flex items-center justify-center text-lg font-bold">C</div>
              <div className="flex-1"><div className="text-sm font-semibold text-slate-800 dark:text-slate-200">Composer</div><div className="text-xs text-slate-500">Paket yoneticisi</div></div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/gunlukler`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300 flex items-center justify-center text-lg font-bold">L</div>
              <div className="flex-1"><div className="text-sm font-semibold text-slate-800 dark:text-slate-200">Gunlukler</div><div className="text-xs text-slate-500">access &middot; error</div></div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/dosyalar`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-emerald-100 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400 flex items-center justify-center text-lg font-bold">F</div>
              <div className="flex-1"><div className="text-sm font-semibold text-slate-800 dark:text-slate-200">Dosyalar</div><div className="text-xs text-slate-500">Dosya yoneticisi</div></div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/istatistik`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center text-lg font-bold">%</div>
              <div className="flex-1"><div className="text-sm font-semibold text-slate-800 dark:text-slate-200">Istatistik</div><div className="text-xs text-slate-500">Trafik analizi</div></div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            <Link to={`/abonelikler/${id}/subdomainler/${sid}/sifre-koruma`}
              className="flex items-center gap-3 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 hover:border-brand-400 dark:hover:border-brand-500 transition group">
              <div className="w-10 h-10 rounded-xl bg-rose-100 dark:bg-rose-900/30 text-rose-600 dark:text-rose-400 flex items-center justify-center text-lg">&#128274;</div>
              <div className="flex-1"><div className="text-sm font-semibold text-slate-800 dark:text-slate-200">Sifre Koruma</div><div className="text-xs text-slate-500">.htpasswd dizin kilidi</div></div>
              <span className="text-brand-500 group-hover:translate-x-0.5 transition">&rarr;</span>
            </Link>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className={kart}>
              <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200 mb-3">Genel Bilgi</h2>
              <dl className="space-y-2.5 text-sm">
                <div className="flex justify-between gap-3"><dt className={etiket}>Docroot</dt><dd className="font-mono text-xs text-slate-600 dark:text-slate-400 text-right break-all">{d.docroot.replace(/^\/home\/[^/]+/, '~')}</dd></div>
                <div className="flex justify-between gap-3"><dt className={etiket}>Disk</dt><dd className="font-mono text-slate-700 dark:text-slate-300">{fmtKB(d.disk_kb)}</dd></div>
                <div className="flex justify-between gap-3"><dt className={etiket}>DNS (A)</dt><dd className="font-mono text-xs text-slate-600 dark:text-slate-400">{d.alt_ad} &rarr; {d.ipv4 || '-'}</dd></div>
                <div className="flex justify-between gap-3"><dt className={etiket}>Olusturulma</dt><dd className="font-mono text-xs text-slate-600 dark:text-slate-400">{d.created_at || '-'}</dd></div>
              </dl>
            </div>

            <div className={kart}>
              <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200 mb-3">PHP Surumu</h2>
              <p className="text-xs text-slate-500 mb-3">Bu alt alanin PHP-FPM havuzunu bagimsiz secebilirsiniz.</p>
              <div className="flex items-center gap-2">
                <select value={yeniPHP} onChange={e => setYeniPHP(e.target.value)}
                  className="flex-1 px-3 py-2 rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-sm">
                  {phpler.map(p => <option key={p.surum} value={p.surum}>PHP {p.surum}{/default|appstream/i.test(p.aciklama || '') ? ' (varsayılan)' : ''}</option>)}
                  {phpler.length === 0 && <option value={d.php_surum}>PHP {d.php_surum}</option>}
                </select>
                <button onClick={phpDegistir} disabled={phpKaydet || yeniPHP === d.php_surum}
                  className="px-3 py-2 rounded-md bg-slate-900 dark:bg-slate-700 text-white text-sm font-medium disabled:opacity-40">
                  {phpKaydet ? '...' : 'Kaydet'}
                </button>
              </div>
              <p className="text-[11px] text-slate-400 mt-2">Aktif: PHP {d.php_surum}</p>
            </div>

            <SubPHPAyarlari domainId={String(id)} sid={String(sid)} />

            <div className={`${kart} md:col-span-2`}>
              <div className="flex items-center justify-between mb-3">
                <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200">SSL / TLS Sertifikasi</h2>
                <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${sslAktif ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400'}`}>
                  {sslAktif == null ? '...' : sslAktif ? 'Aktif' : 'Yok'}
                </span>
              </div>
              <div className="flex items-center gap-2 flex-wrap">
                <button onClick={() => sslKur('letsencrypt')} disabled={sslMesgul}
                  className="text-sm px-3 py-1.5 rounded-md bg-emerald-600 hover:bg-emerald-700 text-white font-medium disabled:opacity-40">
                  Let's Encrypt Kur
                </button>
                <button onClick={() => sslKur('self-signed')} disabled={sslMesgul}
                  className="text-sm px-3 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-40">
                  Oz-imzali
                </button>
                {sslAktif && (
                  <button onClick={sslKaldir} disabled={sslMesgul}
                    className="text-sm px-3 py-1.5 rounded-md border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-40 ml-auto">
                    Kaldir
                  </button>
                )}
              </div>
              <p className="text-[11px] text-slate-400 mt-2">Let's Encrypt icin {d.tam_ad} A kaydinin bu sunucuya ({d.ipv4}) cozumlenmesi gerekir.</p>
            </div>

            <SubWebAyarlari domainId={String(id)} sid={String(sid)} />
          </div>
        </>
      )}
    </div>
  )
}
