import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import YedekGenelAyar from '@/components/YedekGenelAyar'

const BACKUP_EN: Record<string, string> = {
  'Anasayfa': 'Home',
  'Backup Yöneticisi': 'Backup Manager',
  'Yedekleme ve geri yükleme işleri Plesk tarzı listelenir. Bir işe tıklayarak kendi sayfasında domainleri seçip tam / SQL / dosya bazında geri yükleyin.': 'Backup and restore jobs are listed Plesk-style. Click a job to open its own page and restore selected domains fully / by SQL / by file.',
  'Toplam Yedek Boyutu': 'Total Backup Size',
  'Toplam Yedek': 'Total Backups',
  'Domain Sayısı': 'Domain Count',
  'Aktif Uzak Hedef': 'Active Remote Target',
  'Otomatik yedekleme:': 'Automatic backup:',
  'Her gün 03:00': 'Every day 03:00',
  'Başlatılıyor…': 'Starting…',
  'Bir iş sürüyor…': 'A job is running…',
  '⏱ Tüm Domainleri Şimdi Yedekle': '⏱ Back Up All Domains Now',
  'Yedekleme / Geri Yükleme İşleri': 'Backup / Restore Jobs',
  'çalışıyor': 'running',
  'otomatik yenilenir': 'auto-refreshes',
  'Henüz iş yok. “Tüm Domainleri Şimdi Yedekle” ile başlayın.': 'No jobs yet. Start with “Back Up All Domains Now”.',
  'Domain bazlı yedekler (granüler dosya/DB geri yükleme)': 'Per-domain backups (granular file/DB restore)',
  'yedek': 'backups',
  'Yönet →': 'Manage →',
  'İş': 'Job',
  'şu an:': 'now:',
  'geri': 'restore',
  'Yedek': 'Backup',
  'otomatik': 'automatic',
  'Durdur': 'Stop',
  'Durduruluyor…': 'Stopping…',
  'İşi durdurmak istediğinize emin misiniz? Süren yedek yarıda kesilir, yarım dosya silinir. Tamamlanan yedekler korunur.': 'Are you sure you want to stop the job? The running backup is aborted and its partial file removed. Completed backups are kept.',
  'İş durduruldu.': 'Job stopped.',
  'İş durdurulamadı': 'Could not stop the job',
  'iptal': 'cancelled',
  'manuel': 'manual',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (BACKUP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type OzetSatir = { domain_id: number; alan_adi: string; sayi: number; toplam_b: number; son_yedek: string }
type Ozet = { domainler: OzetSatir[]; toplam_boyut_b: number; toplam_yedek: number; hedef_sayisi: number; zamanlama: string }
export type Job = {
  id: number; tur: string; islem: string; durum: string
  toplam: number; tamamlanan: number; basari: number; hata: number
  boyut_b: number; aktif_domain: string; mod: string; baslatan: string; baslangic: string; bitis: string
}

export default function BackupYonetimiPage() {
  useTranslation() // dil re-render aboneligi
  const [o, setO] = useState<Ozet | null>(null)
  const [jobs, setJobs] = useState<Job[]>([])
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [yedekliyor, setYedekliyor] = useState(false)
  const timer = useRef<number | null>(null)

  function ozetYukle() { api.get<Ozet>('/admin/backups/ozet').then(r => setO(r.data)).catch(hataYakala(cevir('Yedek özeti alınamadı'))) }
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
      setBasari(cevirT("Yedekleme başladı — iş #{0} ({1} domain).", data.job_id, data.toplam))
      jobYukle()
    } catch (e) { setHata(apiHata(e, cevir('Yedekleme başlatılamadı'))) }
    finally { setYedekliyor(false) }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: cevir('Araçlar ve Ayarlar'), href: '/araclar-ayarlar' },
        { etiket: cevir('Backup Yöneticisi') },
      ]} />
      <div className="flex items-center gap-3 mb-1">
        <Ikon d={I.disket} className="h-6 w-6" />
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{cevir('Backup Yöneticisi')}</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{cevir('Yedekleme ve geri yükleme işleri Plesk tarzı listelenir. Bir işe tıklayarak kendi sayfasında domainleri seçip tam / SQL / dosya bazında geri yükleyin.')}</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
        <Kpi et={cevir("Toplam Yedek Boyutu")} v={o ? fmtByte(o.toplam_boyut_b) : '—'} renk="sky" ikon="💽" />
        <Kpi et={cevir("Toplam Yedek")} v={o ? String(o.toplam_yedek) : '—'} renk="violet" ikon="📦" />
        <Kpi et={cevir("Domain Sayısı")} v={o ? String(o.domainler.length) : '—'} renk="teal" ikon="🌐" />
        <Kpi et={cevir("Aktif Uzak Hedef")} v={o ? String(o.hedef_sayisi) : '—'} renk="emerald" ikon="☁️" alt="S3 / SFTP" />
      </div>

      <YedekGenelAyar />

      <div className="mb-5 flex flex-wrap items-center gap-3 px-4 py-3 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
        <span className="text-sm text-slate-600 dark:text-slate-300">🕒 {cevir("Otomatik yedekleme:")} <strong>{o?.zamanlama || cevir('Her gün 03:00')}</strong></span>
        <div className="ml-auto flex items-center gap-2">
          <button onClick={tumunuYedekle} disabled={yedekliyor || calisan}
            className="px-3.5 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {yedekliyor ? cevir('Başlatılıyor…') : calisan ? cevir('Bir iş sürüyor…') : cevir('⏱ Tüm Domainleri Şimdi Yedekle')}
          </button>
        </div>
      </div>

      <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 overflow-hidden">
        <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 flex items-center gap-2">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Yedekleme / Geri Yükleme İşleri")}</h3>
          {calisan && <span className="inline-flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400"><span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" /> {cevir("çalışıyor")}</span>}
          <span className="ml-auto text-[11px] text-slate-400">{cevir("otomatik yenilenir")}</span>
        </div>
        <div className="divide-y divide-slate-100 dark:divide-slate-700/60">
          {jobs.length === 0 && <div className="px-4 py-8 text-center text-sm text-slate-400">{cevir("Henüz iş yok. “Tüm Domainleri Şimdi Yedekle” ile başlayın.")}</div>}
          {jobs.map(j => <JobSatir key={j.id} j={j} onDurdur={() => { setBasari(cevir('İş durduruldu.')); jobYukle(); ozetYukle() }} />)}
        </div>
      </div>

      {o && o.domainler.length > 0 && (
        <details className="mt-4 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
          <summary className="px-4 py-3 text-sm font-semibold text-slate-700 dark:text-slate-200 cursor-pointer select-none">{cevir("Domain bazlı yedekler (granüler dosya/DB geri yükleme)")}</summary>
          <div className="border-t border-slate-100 dark:border-slate-700/60 divide-y divide-slate-100 dark:divide-slate-700/60">
            {o.domainler.map(d => (
              <div key={d.domain_id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                <span className="font-medium text-slate-800 dark:text-slate-200">{d.alan_adi}</span>
                <span className="font-mono text-xs text-slate-400">{d.sayi} {cevir("yedek")} · {d.sayi ? fmtByte(d.toplam_b) : '—'}</span>
                <Link to={`/abonelikler/${d.domain_id}/yedekler`} className="ml-auto text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-brand-600 dark:text-brand-400 hover:bg-slate-50 dark:hover:bg-slate-700">{cevir("Yönet →")}</Link>
              </div>
            ))}
          </div>
        </details>
      )}
    </div>
  )
}

function JobSatir({ j, onDurdur }: { j: Job; onDurdur: (id: number) => void }) {
  const pct = j.toplam ? Math.round((j.tamamlanan / j.toplam) * 100) : (j.durum === 'tamam' ? 100 : 0)
  const [durduruluyor, setDurduruluyor] = useState(false)
  async function durdur(e: React.MouseEvent) {
    // Satirin tamami <Link> — tiklamayi yut, yoksa is detayina gider.
    e.preventDefault(); e.stopPropagation()
    if (!window.confirm(cevir('İşi durdurmak istediğinize emin misiniz? Süren yedek yarıda kesilir, yarım dosya silinir. Tamamlanan yedekler korunur.'))) return
    setDurduruluyor(true)
    try { await api.post(`/admin/backups/jobs/${j.id}/durdur`); onDurdur(j.id) }
    finally { setDurduruluyor(false) }
  }
  return (
    <Link to={`/backup-yonetimi/is/${j.id}`} className="w-full flex items-center gap-3 px-4 py-3 hover:bg-slate-50 dark:hover:bg-slate-800/40">
      <DurumIkon durum={j.durum} />
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-slate-800 dark:text-slate-200">{cevir("İş")} #{j.id}</span>
          <IslemRozet islem={j.islem} mod={j.mod} />
          <TurRozet tur={j.tur} />
        </div>
        <div className="text-xs text-slate-400 mt-0.5 font-mono">{j.baslangic}{j.baslatan ? ' · ' + j.baslatan : ''}{j.durum === 'calisiyor' && j.aktif_domain ? ' · ' + cevir("şu an:") + ' ' + j.aktif_domain : ''}</div>
      </div>
      <div className="ml-auto flex items-center gap-4 shrink-0">
        {j.boyut_b > 0 && <span className="hidden sm:inline font-mono text-xs text-slate-500 dark:text-slate-400">{fmtByte(j.boyut_b)}</span>}
        <div className="w-28 sm:w-40">
          <div className="flex justify-between text-[11px] text-slate-400 mb-0.5"><span>{j.tamamlanan}/{j.toplam}</span><span>{pct}%</span></div>
          <div className="h-1.5 rounded-full bg-slate-100 dark:bg-slate-700 overflow-hidden">
            <div className={`h-full rounded-full transition-all duration-500 ${barRenk(j.durum)} ${j.durum === 'calisiyor' ? 'animate-pulse' : ''}`} style={{ width: `${pct}%` }} />
          </div>
        </div>
        {j.durum === 'calisiyor' && (
          <button
            type="button"
            onClick={durdur}
            disabled={durduruluyor}
            title={cevir('Durdur')}
            className="px-2.5 py-1 text-xs font-medium rounded-lg border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50"
          >
            {durduruluyor ? cevir('Durduruluyor…') : cevir('Durdur')}
          </button>
        )}
        <span className="text-slate-300 dark:text-slate-600 text-xs">→</span>
      </div>
    </Link>
  )
}

export function DurumIkon({ durum, kucuk }: { durum: string; kucuk?: boolean }) {
  const s = kucuk ? 'w-4 h-4 text-xs' : 'w-6 h-6 text-sm'
  if (durum === 'calisiyor') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-400 animate-pulse`}>◔</span>
  if (durum === 'iptal') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400`}>■</span>
  if (durum === 'tamam') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900/40 text-emerald-600 dark:text-emerald-400`}>✓</span>
  if (durum === 'kismi') return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-400`}>!</span>
  return <span className={`${s} shrink-0 inline-flex items-center justify-center rounded-full bg-red-100 dark:bg-red-900/40 text-red-600 dark:text-red-400`}>✕</span>
}
export function IslemRozet({ islem, mod }: { islem: string; mod: string }) {
  const geri = islem === 'geri'
  return <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold ${geri ? 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300' : 'bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300'}`}>{geri ? cevir('geri') + (mod ? ' · ' + mod : '') : cevir('Yedek')}</span>
}
export function TurRozet({ tur }: { tur: string }) {
  return <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400">{tur === 'otomatik' ? cevir('otomatik') : cevir('manuel')}</span>
}
export function barRenk(durum: string): string {
  if (durum === 'calisiyor') return 'bg-amber-400'
  if (durum === 'tamam') return 'bg-emerald-500'
  if (durum === 'kismi') return 'bg-amber-500'
  if (durum === 'iptal') return 'bg-slate-400'
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
