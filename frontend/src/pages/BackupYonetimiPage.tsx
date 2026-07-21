import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'

type OzetSatir = { domain_id: number; alan_adi: string; sayi: number; toplam_b: number; son_yedek: string }
type Ozet = { domainler: OzetSatir[]; toplam_boyut_b: number; toplam_yedek: number; hedef_sayisi: number; zamanlama: string }

export default function BackupYonetimiPage() {
  const [o, setO] = useState<Ozet | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [yedekliyor, setYedekliyor] = useState(false)

  function yukle() {
    setYuk(true)
    api.get<Ozet>('/admin/backups/ozet')
      .then(r => setO(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  async function simdiYedekle() {
    setHata(null); setBasari(null); setYedekliyor(true)
    try {
      await api.post('/admin/backups/tick')
      setBasari('Planlı yedekleme tetiklendi — birkaç saniye içinde tamamlanır, sonra yenileyin.')
    } catch (e) { setHata(apiHata(e, 'Yedekleme tetiklenemedi')) }
    finally { setYedekliyor(false) }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Backup Yöneticisi' },
      ]} />
      <div className="flex items-center gap-3 mb-1">
        <span className="text-2xl">💾</span>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">Backup Yöneticisi</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">Tüm domainlerin yedeklerini tek yerden görün; günlük otomatik yedekleme çalışır, S3/SFTP hedefleri domain bazında ayarlanır.</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* KPI */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
        <Kpi et="Toplam Yedek Boyutu" v={o ? fmtByte(o.toplam_boyut_b) : '—'} renk="sky" ikon="💽" />
        <Kpi et="Toplam Yedek" v={o ? String(o.toplam_yedek) : '—'} renk="violet" ikon="📦" />
        <Kpi et="Domain Sayısı" v={o ? String(o.domainler.length) : '—'} renk="teal" ikon="🌐" />
        <Kpi et="Aktif Uzak Hedef" v={o ? String(o.hedef_sayisi) : '—'} renk="emerald" ikon="☁️" alt="S3 / SFTP" />
      </div>

      {/* Zamanlama + eylem */}
      <div className="mb-5 flex flex-wrap items-center gap-3 px-4 py-3 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
        <span className="text-sm text-slate-600 dark:text-slate-300">
          🕒 Otomatik yedekleme: <strong>{o?.zamanlama || 'Her gün 03:00'}</strong> · 7 günlük saklama
        </span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <button onClick={simdiYedekle} disabled={yedekliyor}
            className="px-3.5 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {yedekliyor ? 'Tetikleniyor…' : '⏱ Tüm Domainleri Şimdi Yedekle'}
          </button>
          <button onClick={yukle} disabled={yuk} className="px-3 py-2 text-sm border border-slate-200 dark:border-slate-700 rounded-lg text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">↻ Yenile</button>
        </div>
      </div>

      {/* Tablo */}
      {/* Kapsayıcı çerçeve yalnız masaüstünde; mobilde satırlar zaten kart olduğu için
          ikinci bir çerçeve iç içe görünüm yaratırdı. */}
      <div className="lg:bg-white dark:lg:bg-slate-800/60 lg:border lg:border-slate-200 dark:lg:border-slate-700/60 lg:rounded-2xl lg:overflow-hidden">
        <div className="py-3 lg:px-4 lg:border-b lg:border-slate-100 dark:lg:border-slate-700/60">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Domain Yedekleri</h3>
        </div>
        <div className="lg:overflow-x-auto">
          <table className={`${T.tablo} text-sm`}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900/50 border-b border-slate-200 dark:border-slate-700/60`}>
              <tr>
                <th className={T.baslik}>Domain</th>
                <th className={`${T.baslik} text-right`}>Yedek Sayısı</th>
                <th className={`${T.baslik} text-right`}>Toplam Boyut</th>
                <th className={T.baslik}>Son Yedek</th>
                <th className={`${T.baslik} text-right`}>İşlem</th>
              </tr>
            </thead>
            {/* Satır ayracını T.satir'in kendi `lg:border-b`si veriyor; mobilde
                kart kenarlığı zaten ayırdığı için ayrı bir divide sınıfı gerekmiyor. */}
            <tbody className={T.govde}>
              {yuk ? (
                <tr className={T.satir}><td colSpan={5} className={T.hucreDurum}>Yükleniyor…</td></tr>
              ) : !o || o.domainler.length === 0 ? (
                <tr className={T.satir}><td colSpan={5} className={T.hucreDurum}>Domain yok.</td></tr>
              ) : (
                o.domainler.map(d => (
                  <tr key={d.domain_id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/40`}>
                    <td className={T.hucreBaslik}>{d.alan_adi}</td>
                    <td className={`${T.hucre} lg:text-right`} data-etiket="Yedek Sayısı">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-300">{d.sayi}</span>
                    </td>
                    <td className={`${T.hucre} lg:text-right`} data-etiket="Toplam Boyut">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-300">{d.sayi ? fmtByte(d.toplam_b) : '—'}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Son Yedek">
                      <span className="text-xs font-mono text-slate-500 dark:text-slate-400">{d.son_yedek || <span className="text-slate-400">hiç</span>}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Link to={`/abonelikler/${d.domain_id}/yedekler`} className="text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-brand-600 dark:text-brand-400 hover:bg-slate-50 dark:hover:bg-slate-700">Yönet →</Link>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-xs text-slate-400 dark:text-slate-500 mt-3">
        ℹ️ Yedekler <span className="font-mono">/var/backups/girginospanel/&lt;domain&gt;/</span> altında tutulur. Domain silinse bile yedekleri korunur (yanlış silme kurtarma). Tekil yedek indirme/geri yükleme/hedef ayarı için "Yönet"e girin.
      </p>
    </div>
  )
}

function Kpi({ et, v, renk, ikon, alt }: { et: string; v: string; renk: string; ikon: string; alt?: string }) {
  const c: Record<string, string> = {
    sky: 'text-sky-600 dark:text-sky-400', violet: 'text-violet-600 dark:text-violet-400',
    teal: 'text-teal-600 dark:text-teal-400', emerald: 'text-emerald-600 dark:text-emerald-400',
  }
  return (
    <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 p-4">
      <div className="flex items-center gap-2 text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{ikon} {et}</div>
      <div className={`text-2xl font-semibold mt-1 ${c[renk] || 'text-slate-700 dark:text-slate-200'}`}>{v}</div>
      {alt && <div className="text-[11px] text-slate-400 mt-0.5">{alt}</div>}
    </div>
  )
}

function fmtByte(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}
