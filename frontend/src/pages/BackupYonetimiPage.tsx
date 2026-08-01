import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type OzetSatir = { domain_id: number; alan_adi: string; sayi: number; toplam_b: number; son_yedek: string }
type Ozet = { domainler: OzetSatir[]; toplam_boyut_b: number; toplam_yedek: number; hedef_sayisi: number; zamanlama: string }
export type Job = {
  id: number; tur: string; islem: string; durum: string
  toplam: number; tamamlanan: number; basari: number; hata: number
  boyut_b: number; aktif_domain: string; mod: string; baslatan: string; baslangic: string; bitis: string
}

export default function BackupYonetimiPage() {
  const [o, setO] = useState<Ozet | null>(null)
  const [jobs, setJobs] = useState<Job[]>([])
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [yedekliyor, setYedekliyor] = useState(false)
  const timer = useRef<number | null>(null)

  function ozetYukle() { api.get<Ozet>('/admin/backups/ozet').then(r => setO(r.data)).catch(() => {}) }
  function jobYukle() { api.get<Job[]>('/admin/backups/jobs').then(r => setJobs(r.data)).catch(e => setHata(apiHata(e))) }

  useEffect(() => {
    ozetYukle(); jobYukle()
    timer.current = window.setInterval(jobYukle, 2500)
    return () => { if (timer.current) window.clearInterval(timer.current) }
  }, [])

  const calisan = jobs.some(j => j.durum === 'calisiyor')
  const oncekiCalisan = useRef(calisan)
  useEffect(() => {
    if (oncekiCalisan.current && !calisan) ozetYukle()
    oncekiCalisan.current = calisan
  }, [calisan])

  async function tumunuYedekle() {
    setHata(null); setBasari(null); setYedekliyor(true)
    try {
      const { data } = await api.post('/admin/backups/jobs', {})
      setBasari(`Yedekleme başladı — iş #${data.job_id} (${data.toplam} domain).`)
      jobYukle()
    } catch (e) { setHata(apiHata(e, 'Yedekleme başlatılamadı')) }
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
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">Yedekleme ve geri yükleme işleri Plesk tarzı listelenir. Bir işe tıklayarak kendi sayfasında domainleri seçip tam / SQL / dosya bazında geri yükleyin.</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
        <Kpi et="Toplam Yedek Boyutu" v={o ? fmtByte(o.toplam_boyut_b) : '—'} renk="sky" ikon="💽" />
        <Kpi et="Toplam Yedek" v={o ? String(o.toplam_yedek) : '—'} renk="violet" ikon="📦" />
        <Kpi et="Domain Sayısı" v={o ? String(o.domainler.length) : '—'} renk="teal" ikon="🌐" />
        <Kpi et="Aktif Uzak Hedef" v={o ? String(o.hedef_sayisi) : '—'} renk="emerald" ikon="☁️" alt="S3 / SFTP" />
      </div>

      <div className="mb-5 flex flex-wrap items-center gap-3 px-4 py-3 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
        <span className="text-sm text-slate-600 dark:text-slate-300">🕒 Otomatik yedekleme: <strong>{o?.zamanlama || 'Her gün 03:00'}</strong></span>
        <div className="ml-auto flex items-center gap-2">
          <button onClick={tumunuYedekle} disabled={yedekliyor || calisan}
            className="px-3.5 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {yedekliyor ? 'Başlatılıyor…' : calisan ? 'Bir iş sürüyor…' : '⏱ Tüm Domainleri Şimdi Yedekle'}
          </button>
        </div>
      </div>

      <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 overflow-hidden">
        <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 flex items-center gap-2">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Yedekleme / Geri Yükleme İşleri</h3>
          {calisan && <span className="inline-flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400"><span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" /> çalışıyor</span>}
          <span className="ml-auto text-[11px] text-slate-400">otomatik yenilenir</span>
        </div>
        <div className="divide-y divide-slate-100 dark:divide-slate-700/60">
          {jobs.length === 0 && <div className="px-4 py-8 text-center text-sm text-slate-400">Henüz iş yok. “Tüm Domainleri Şimdi Yedekle” ile başlayın.</div>}
          {jobs.map(j => <JobSatir key={j.id} j={j} />)}
        </div>
      </div>

      {o && o.domainler.length > 0 && (
        <details className="mt-4 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
          <summary className="px-4 py-3 text-sm font-semibold text-slate-700 dark:text-slate-200 cursor-pointer select-none">Domain bazlı yedekler (granüler dosya/DB geri yükleme)</summary>
          <div className="border-t border-slate-100 dark:border-slate-700/60 divide-y divide-slate-100 dark:divide-slate-700/60">
            {o.domainler.map(d => (
              <div key={d.domain_id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                <span className="font-medium text-slate-800 dark:text-slate-200">{d.alan_adi}</span>
                <span className="font-mono text-xs text-slate-400">{d.sayi} yedek · {d.sayi ? fmtByte(d.toplam_b) : '—'}</span>
                <Link to={`/abonelikler/${d.domain_id}/yedekler`} className="ml-auto text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-brand-600 dark:text-brand-400 hover:bg-slate-50 dark:hover:bg-slate-700">Yönet →</Link>
              </div>
            ))}
          </div>
        </details>
      )}
    </div>
  )
}

function JobSatir({ j }: { j: Job }) {
  const pct = j.toplam ? Math.round((j.tamamlanan / j.toplam) * 100) : (j.durum === 'tamam' ? 100 : 0)
  return (
    <Link to={`/backup-yonetimi/is/${j.id}`} className="w-full flex items-center gap-3 px-4 py-3 hover:bg-slate-50 dark:hover:bg-slate-800/40">
      <DurumIkon durum={j.durum} />
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-slate-800 dark:text-slate-200">İş #{j.id}</span>
          <IslemRozet islem={j.islem} mod={j.mod} />
          <TurRozet tur={j.tur} />
        </div>
        <div className="text-xs text-slate-400 mt-0.5 font-mono">{j.baslangic}{j.baslatan ? ' · ' + j.baslatan : ''}{j.durum === 'calisiyor' && j.aktif_domain ? ' · şu an: ' + j.aktif_domain : ''}</div>
      </div>
      <div className="ml-auto flex items-center gap-4 shrink-0">
        {j.boyut_b > 0 && <span className="hidden sm:inline font-mono text-xs text-slate-500 dark:text-slate-400">{fmtByte(j.boyut_b)}</span>}
        <div className="w-28 sm:w-40">
          <div className="flex justify-between text-[11px] text-slate-400 mb-0.5"><span>{j.tamamlanan}/{j.toplam}</span><span>{pct}%</span></div>
          <div className="h-1.5 rounded-full bg-slate-100 dark:bg-slate-700 overflow-hidden">
            <div className={`h-full rounded-full transition-all duration-500 ${barRenk(j.durum)} ${j.durum === 'calisiyor' ? 'animate-pulse' : ''}`} style={{ width: `${pct}%` }} />
          </div>
        </div>
        <span className="text-slate-300 dark:text-slate-600 text-xs">→</span>
      </div>
    </Link>
  )
}

export function DurumIkon({ durum, kucuk }: { durum: string; kucuk?: boolean }) {
  const s = kucuk ? 'w-4 h-4 text-xs' : 'w-6 h-6 text-sm'
  if (durum === 'calisiyor') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-400 animate-pulse`}>◔</span>
  if (durum === 'tamam') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900/40 text-emerald-600 dark:text-emerald-400`}>✓</span>
  if (durum === 'kismi') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-400`}>!</span>
  return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-red-100 dark:bg-red-900/40 text-red-600 dark:text-red-400`}>✕</span>
}
export function IslemRozet({ islem, mod }: { islem: string; mod: string }) {
  const geri = islem === 'geri'
  return <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold ${geri ? 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300' : 'bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300'}`}>{geri ? 'geri' + (mod ? ' · ' + mod : '') : 'yedek'}</span>
}
export function TurRozet({ tur }: { tur: string }) {
  return <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400">{tur === 'otomatik' ? 'otomatik' : 'manuel'}</span>
}
export function barRenk(durum: string): string {
  if (durum === 'calisiyor') return 'bg-amber-400'
  if (durum === 'tamam') return 'bg-emerald-500'
  if (durum === 'kismi') return 'bg-amber-500'
  return 'bg-red-500'
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

export function fmtByte(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}
