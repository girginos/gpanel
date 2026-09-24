import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import type { Color } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

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

// Şiddet → Tailux Badge. Kritik dolu (en güçlü vurgu), diğerleri soft; şiddet
// adı rozet metninde her zaman yazılı (renk tek başına anlam taşımaz).
const SEV_ROZET: Record<string, { color: Color; variant: 'filled' | 'soft' }> = {
  critical: { color: 'error',   variant: 'filled' },
  high:     { color: 'error',   variant: 'soft' },
  medium:   { color: 'warning', variant: 'soft' },
  low:      { color: 'neutral', variant: 'soft' },
}
const APP_META: Record<string, { ad: string; renk: Color }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'info' },
  'nodejs':       { ad: 'Node.js',      renk: 'success' },
  'php-composer': { ad: 'PHP Composer', renk: 'secondary' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? (t || '—')
const appRenk = (t: string): Color => APP_META[t]?.renk ?? 'neutral'


const SEC_EN: Record<string, string> = {
  "Türkçe": "English",
  "paket": "packages",
  "Bu Domaini Tara": "Scan This Domain",
  "Filtre:": "Filter:",
  "Hepsi": "All",
  "Kritik {0}": "Critical {0}",
  "Yüksek {0}": "High {0}",
  "Orta {0}": "Medium {0}",
  "Düşük {0}": "Low {0}",
  "Tekrar dene": "Retry",
  "Paket": "Package",
  "Kurulu / Fix": "Installed / Fix",
  "Başlatılamadı": "Failed to start",
  "Başlatılıyor…": "Starting…",
  "Bu domainde bilinen açık bulunmadı.": "No known vulnerabilities found on this domain.",
  "Bu filtreyle açık yok.": "No vulnerabilities with this filter.",
  "Tarama hatası": "Scan error",
  "İşlem başarısız": "Operation failed",
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
  const toast = useToast()
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
      const m = apiHata(e, cevir("Yüklenemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
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
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Tarama hatası")), ton: 'tehlike' })
    } finally { setTaranıyor(false) }
  }

  const alanAdi = uyg[0]?.alan_adi || sayfa.items[0]?.alan_adi || `#${domainID}`
  const taraniyorMu = uyg.some((u) => u.durum === 'taraniyor')
  const sev = useMemo(() => {
    const c = { critical: 0, high: 0, medium: 0, low: 0 }
    for (const b of sayfa.items) if (b.severity in c) (c as Record<string, number>)[b.severity]++
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
              <Badge key={u.app_type} color={appRenk(u.app_type)} variant="soft" className="font-normal">
                {appAd(u.app_type)}{u.app_version ? ' ' + u.app_version : ''} · {u.paket_sayisi} {cevir("paket")}
              </Badge>
            ))}
            {taraniyorMu && (
              <Badge color="info" variant="soft" className="gap-1.5 text-[11px]">
                <span className="size-1.5 animate-pulse rounded-full bg-current" aria-hidden="true" /> {cevir("Taranıyor…")}
              </Badge>
            )}
          </div>
        </div>
        <Button color="primary" ikon={I.oynat} onClick={domainTara} yukleniyor={taranıyor || taraniyorMu}>
          {taraniyorMu ? cevir("Taranıyor…") : (taranıyor ? cevir("Başlatılıyor…") : cevir("Bu Domaini Tara"))}
        </Button>
      </div>

      {/* Şiddet filtresi */}
      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <span className="text-slate-500 mr-1">{cevir("Filtre:")}</span>
        {([['', cevir("Hepsi")], ['critical', cevirT(cevir("Kritik {0}"), sev.critical)], ['high', cevirT(cevir("Yüksek {0}"), sev.high)], ['medium', cevirT(cevir("Orta {0}"), sev.medium)], ['low', cevirT(cevir("Düşük {0}"), sev.low)]] as [string, string][]).map(([v, ad]) => (
          <button key={v} onClick={() => { setFiltre(v); setAktifSayfa(1) }}
            className={`rounded-md border px-2 py-1 ${filtre === v ? 'border-dark-700 bg-dark-800 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-dark-600 dark:text-slate-400 dark:hover:bg-dark-700'}`}>
            {ad}
          </button>
        ))}
      </div>

      {yukleniyor && !ilk.current ? (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <UyariKutusu ton="tehlike">
          {hata}
          <div className="mt-2">
            <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={yukle} yukleniyor={yukleniyor}>{cevir("Tekrar dene")}</Button>
          </div>
        </UyariKutusu>
      ) : sayfa.items.length === 0 ? (
        <UyariKutusu ton="basari">
          {taraniyorMu ? cevir("Taranıyor…") : (filtre ? cevir("Bu filtreyle açık yok.") : cevir("Bu domainde bilinen açık bulunmadı."))}
        </UyariKutusu>
      ) : (
        <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-dark-600">
          <div className="overflow-x-auto">
            <table className="min-w-[860px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                <tr>
                  <th className="w-32 px-3 py-3 font-semibold">{cevir("Şiddet")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Paket")}</th>
                  <th className="w-40 px-3 py-3 font-semibold">{cevir("Kurulu / Fix")}</th>
                  <th className="w-40 px-3 py-3 font-semibold">CVE</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Başlık")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-dark-600">
                {sayfa.items.map((b) => (
                  <tr key={b.id} className="bg-white dark:bg-dark-900">
                    <td className="px-3 py-2.5">
                      <Badge color={(SEV_ROZET[b.severity] || SEV_ROZET.low).color} variant={(SEV_ROZET[b.severity] || SEV_ROZET.low).variant} className="whitespace-nowrap text-[11px]">
                        {b.severity || '—'}{b.cvss > 0 ? ' · ' + b.cvss.toFixed(1) : ''}
                      </Badge>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="font-mono text-[13px] break-all">{b.package_name}</div>
                      <Badge color={appRenk(b.app_type)} variant="soft" className="mt-0.5 whitespace-nowrap px-1.5 py-0.5 text-[10px]">{appAd(b.app_type)}</Badge>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[13px] whitespace-nowrap">
                      <span className="text-slate-900 dark:text-slate-100">{b.installed_version}</span>
                      {b.fixed_in && <span className="ml-1 inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400"><Ikon d={I.ileri} className="size-3 shrink-0" />{b.fixed_in}</span>}
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
