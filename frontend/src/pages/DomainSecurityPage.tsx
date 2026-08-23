import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Domaine ÖZEL güvenlik sayfası — bir domainin bilinen açıklarını listeler.
 * /website-security/domain/:id · Website Security Monitor'den satıra tıklanınca gelinir.
 */

type Bulgu = {
  id: number
  domain_id: number
  alan_adi: string
  app_type: string
  package_name: string
  installed_version: string
  cve_id: string
  severity: string
  cvss: number
  title: string
  fixed_in: string
  source: string
  last_seen: string
}

type Sayfa = { toplam: number; sayfa: number; sayfa_boyut: number; items: Bulgu[] }

type Uygulama = {
  domain_id: number; alan_adi: string; app_type: string; install_path: string
  app_version: string; paket_sayisi: number; bulgu_sayisi: number; son_tarama: string; durum: string
}
type EnvanterYanit = { toplam: number; items: Uygulama[]; desteklenmeyen: number }

const SEV_RENK: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300',
  high:     'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
  medium:   'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  low:      'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
}
const APP_META: Record<string, { ad: string; renk: string }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'bg-blue-100 text-blue-800 dark:bg-blue-950/40 dark:text-blue-300' },
  'nodejs':       { ad: 'Node.js',      renk: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' },
  'php-composer': { ad: 'PHP Composer', renk: 'bg-violet-100 text-violet-800 dark:bg-violet-950/40 dark:text-violet-300' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? (t || '—')
const appRenk = (t: string) => APP_META[t]?.renk ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'


const SEC_EN: Record<string, string> = {
  "Başlatılamadı": "Failed to start",
  "Başlatılıyor…": "Starting…",
  "Bu domainde bilinen açık bulunmadı.": "No known vulnerabilities found on this domain.",
  "Bu filtreyle açık yok.": "No vulnerabilities with this filter.",
  "Tarama hatası": "Scan error",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SEC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainSecurityPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams<{ id: string }>()
  const domainID = Number(id)
  const [sayfa, setSayfa] = useState<Sayfa>({ toplam: 0, sayfa: 1, sayfa_boyut: 50, items: [] })
  const [uyg, setUyg] = useState<Uygulama[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [filtre, setFiltre] = useState('')
  const [aktifSayfa, setAktifSayfa] = useState(1)
  const [taranıyor, setTaranıyor] = useState(false)
  const dialog = useDialog()
  const ilk = useRef(false)

  const yukle = async () => {
    if (!ilk.current) setYukleniyor(true)
    setHata(null)
    try {
      const params = new URLSearchParams({ domain_id: String(domainID), page: String(aktifSayfa), page_size: '100' })
      if (filtre) params.set('severity', filtre)
      const [p, env] = await Promise.all([
        api.get<Sayfa>('/websec/findings?' + params.toString()),
        api.get<EnvanterYanit>('/websec/apps'),
      ])
      setSayfa({
        toplam: p.data.toplam ?? 0, sayfa: p.data.sayfa ?? 1,
        sayfa_boyut: p.data.sayfa_boyut ?? 100, items: Array.isArray(p.data.items) ? p.data.items : [],
      })
      setUyg((env.data.items ?? []).filter((u) => u.domain_id === domainID))
      ilk.current = true
    } catch (e) {
      setHata(apiHata(e, cevir("Yüklenemedi")))
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { if (domainID > 0) void yukle() }, [domainID, filtre, aktifSayfa])

  // Taranıyorsa canlı tazele
  useEffect(() => {
    if (!uyg.some((u) => u.durum === 'taraniyor')) return
    const t = setInterval(yukle, 5000)
    return () => clearInterval(t)
  }, [uyg])

  const domainTara = async () => {
    setTaranıyor(true)
    try {
      await api.post('/websec/rescan?domain_id=' + domainID, {})
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Tarama hatası")) })
    } finally { setTaranıyor(false) }
  }

  const alanAdi = uyg[0]?.alan_adi || sayfa.items[0]?.alan_adi || `#${domainID}`
  const taraniyorMu = uyg.some((u) => u.durum === 'taraniyor')
  const sev = useMemo(() => {
    const c = { critical: 0, high: 0, medium: 0, low: 0 }
    for (const b of sayfa.items) if (b.severity in c) (c as any)[b.severity]++
    return c
  }, [sayfa.items])

  return (
    <div className="w-full px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { etiket: 'Website Security Monitor', href: '/website-security' },
        { etiket: alanAdi },
      ]} />

      <div className="mt-4 mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{alanAdi}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            {uyg.map((u) => (
              <span key={u.app_type} className={`rounded px-2 py-0.5 text-xs ${appRenk(u.app_type)}`}>
                {appAd(u.app_type)}{u.app_version ? ' ' + u.app_version : ''} · {u.paket_sayisi} paket
              </span>
            ))}
            {taraniyorMu && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-100 px-2 py-0.5 text-[11px] font-medium text-blue-800 dark:bg-blue-950/40 dark:text-blue-300">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" /> {cevir("Taranıyor…")}
              </span>
            )}
          </div>
        </div>
        <button onClick={domainTara} disabled={taranıyor || taraniyorMu}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">
          {taraniyorMu ? 'Taranıyor…' : (taranıyor ? 'Başlatılıyor…' : 'Bu Domaini Tara')}
        </button>
      </div>

      {/* Şiddet filtresi */}
      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <span className="text-slate-500 mr-1">Filtre:</span>
        {([['', 'Hepsi'], ['critical', `Kritik ${sev.critical}`], ['high', cevirT(cevir("Yüksek {0}"), sev.high)], ['medium', `Orta ${sev.medium}`], ['low', cevirT(cevir("Düşük {0}"), sev.low)]] as [string, string][]).map(([v, ad]) => (
          <button key={v} onClick={() => { setFiltre(v); setAktifSayfa(1) }}
            className={`rounded-md border px-2 py-1 ${filtre === v ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800'}`}>
            {ad}
          </button>
        ))}
      </div>

      {yukleniyor && !ilk.current ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata} <button onClick={yukle} className="underline">Tekrar dene</button></div>
      ) : sayfa.items.length === 0 ? (
        <div className="rounded-2xl border border-emerald-200 bg-emerald-50 py-10 text-center text-sm text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300">
          {taraniyorMu ? 'Taranıyor…' : (filtre ? 'Bu filtreyle açık yok.' : cevir("Bu domainde bilinen açık bulunmadı."))}
        </div>
      ) : (
        <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
          <div className="overflow-x-auto">
            <table className="min-w-[860px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th className="w-32 px-3 py-3 font-semibold">{cevir("Şiddet")}</th>
                  <th className="px-3 py-3 font-semibold">Paket</th>
                  <th className="w-40 px-3 py-3 font-semibold">Kurulu / Fix</th>
                  <th className="w-40 px-3 py-3 font-semibold">CVE</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Başlık")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {sayfa.items.map((b) => (
                  <tr key={b.id} className="bg-white dark:bg-slate-950">
                    <td className="px-3 py-2.5">
                      <span className={`inline-block whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium ${SEV_RENK[b.severity] || SEV_RENK.low}`}>
                        {b.severity || '—'}{b.cvss > 0 ? ' · ' + b.cvss.toFixed(1) : ''}
                      </span>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="font-mono text-[13px] break-all">{b.package_name}</div>
                      <span className={`mt-0.5 inline-block whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium ${appRenk(b.app_type)}`}>{appAd(b.app_type)}</span>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[13px] whitespace-nowrap">
                      <span className="text-slate-900 dark:text-slate-100">{b.installed_version}</span>
                      {b.fixed_in && <span className="ml-1 text-emerald-600 dark:text-emerald-400">→ {b.fixed_in}</span>}
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12px] text-slate-700 dark:text-slate-300 whitespace-nowrap">{b.cve_id}</td>
                    <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">{b.title}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
