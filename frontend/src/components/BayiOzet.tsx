import { cevirT } from '@/lib/cevirT'
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'

const BAYIOZET_EN: Record<string, string> = {
  "Türkçe": "English",
  "Taahhüt": "Committed",
  "Bayi özeti yükleniyor": "Loading reseller summary",
  "Bayi özeti": "Reseller summary",
  "Bayi Özeti": "Reseller Summary",
  "Hosting hesabı": "Hosting account",
  "⚠ Limit doldu — yeni hesap açılamaz": "⚠ Limit reached — no new accounts allowed",
  "Tümü aktif": "All active",
  "Disk havuzu": "Disk pool",
  "Trafik havuzu": "Traffic pool",
  "Sınırsız": "Unlimited",
  "Kendi planlarım": "My plans",
  "Planları yönet": "Manage plans",
  "DNS şablonum ✓": "My DNS template ✓",
  "DNS şablonu yok": "No DNS template",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BAYIOZET_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Ozet = {
  paket_ad: string
  hosting_adet: number; hosting_limit: number; askida_adet: number
  disk_kullanim_kb: number; disk_taahhut_mb: number; disk_limit_mb: number
  trafik_kullanim_kb: number; trafik_taahhut_mb: number; trafik_limit_mb: number
  plan_adet: number; dns_sablon_ozel: boolean
}

// Limit bicimi: 0 = sinirsiz (∞). Kullanim bicimi: 0 = gercekten 0 MB.
const mb = (v: number) => (v <= 0 ? '∞' : v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`)
const mbKullanim = (v: number) => (v <= 0 ? '0 MB' : v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`)

// Doluluk çubuğu: limit 0 (sınırsız) ise çubuk gösterilmez, yalnız değer yazılır.
function Olcum({ etiket, deger, taahhut, limit, ipucu }: {
  etiket: string; deger: string; taahhut?: number; limit: number; ipucu?: string
}) {
  useTranslation()
  const oran = limit > 0 && taahhut !== undefined ? Math.min(100, Math.round((taahhut / limit) * 100)) : 0
  const kritik = oran >= 90
  const uyari = oran >= 75 && !kritik
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-3.5">
      <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">{etiket}</div>
      <div className="mt-1 text-lg font-semibold text-slate-900 dark:text-slate-100 tabular-nums">{deger}</div>
      {limit > 0 && taahhut !== undefined && (
        <>
          <div className="mt-2 h-1.5 rounded-full bg-slate-100 dark:bg-slate-700 overflow-hidden">
            <div className={`h-full rounded-full ${kritik ? 'bg-red-500' : uyari ? 'bg-amber-500' : 'bg-brand-500'}`} style={{ width: `${oran}%` }} />
          </div>
          <div className={`mt-1 text-xs tabular-nums ${kritik ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-500 dark:text-slate-400'}`}>
            {kritik ? '⚠ ' : ''}{cevir("Taahhüt")} {mb(taahhut)} / {mb(limit)} (%{oran})
          </div>
        </>
      )}
      {ipucu && <div className="mt-1 text-xs text-slate-400 dark:text-slate-500">{ipucu}</div>}
    </div>
  )
}

/** Bayi panosu — yalnız rol=reseller kullanıcılara gösterilir. Kota havuzunun
 *  ne kadarının taahhüt edildiğini (plan kotaları toplamı) ve fiili kullanımı
 *  ayrı ayrı gösterir: yeni hosting açma hakkı TAAHHÜDE göre kısıtlanır. */
export default function BayiOzet() {
  useTranslation()
  const [o, setO] = useState<Ozet | null>(null)
  const [yuk, setYuk] = useState(true)

  useEffect(() => {
    api.get<Ozet>('/reseller/ozet')
      .then(r => setO(r.data))
      .catch(() => setO(null))
      .finally(() => setYuk(false))
  }, [])

  if (yuk) return <div className="mb-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4" aria-busy="true" aria-label={cevir("Bayi özeti yükleniyor")}>
    {Array.from({ length: 4 }).map((_, i) => <div key={i} className="h-24 rounded-lg bg-slate-100 dark:bg-slate-800 animate-pulse" />)}
  </div>
  if (!o) return null

  const hostingDolu = o.hosting_limit > 0 && o.hosting_adet >= o.hosting_limit

  return (
    <section className="mb-5" aria-label={cevir("Bayi özeti")}>
      <div className="mb-2 flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Bayi Özeti")}</h2>
        {o.paket_ad && <span className="text-[11px] uppercase tracking-wider font-semibold bg-brand-100 dark:bg-brand-900/40 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded">{o.paket_ad}</span>}
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-3.5">
          <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">{cevir("Hosting hesabı")}</div>
          <div className="mt-1 text-lg font-semibold text-slate-900 dark:text-slate-100 tabular-nums">
            {o.hosting_adet}{o.hosting_limit > 0 ? ` / ${o.hosting_limit}` : ''}
          </div>
          <div className={`mt-1 text-xs ${hostingDolu ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-500 dark:text-slate-400'}`}>
            {hostingDolu ? cevir('⚠ Limit doldu — yeni hesap açılamaz') : o.askida_adet > 0 ? cevirT("{0} hesap askıda", o.askida_adet) : cevir('Tümü aktif')}
          </div>
        </div>
        <Olcum etiket={cevir("Disk havuzu")} deger={mbKullanim(Math.round(o.disk_kullanim_kb / 1024))} taahhut={o.disk_taahhut_mb} limit={o.disk_limit_mb} ipucu={o.disk_limit_mb <= 0 ? cevir('Sınırsız') : undefined} />
        <Olcum etiket={cevir("Trafik havuzu")} deger={mbKullanim(Math.round(o.trafik_kullanim_kb / 1024))} taahhut={o.trafik_taahhut_mb} limit={o.trafik_limit_mb} ipucu={o.trafik_limit_mb <= 0 ? cevir('Sınırsız') : undefined} />
        <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-3.5">
          <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">{cevir("Kendi planlarım")}</div>
          <div className="mt-1 text-lg font-semibold text-slate-900 dark:text-slate-100 tabular-nums">{o.plan_adet}</div>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs">
            <Link to="/hizmet-planlari" className="text-brand-700 dark:text-brand-300 hover:underline">{cevir("Planları yönet")}</Link>
            <Link to="/araclar/dns-sablonu" className={o.dns_sablon_ozel ? 'text-emerald-600 dark:text-emerald-400 hover:underline' : 'text-slate-500 dark:text-slate-400 hover:underline'}>
              {o.dns_sablon_ozel ? cevir('DNS şablonum ✓') : cevir('DNS şablonu yok')}
            </Link>
          </div>
        </div>
      </div>
    </section>
  )
}
