// Bayi kaynak havuzu kullanım grafiği (CPU / RAM / disk, sınır çizgisiyle).
// Veri: GET <yol>?aralik=24s|7g|30g → { adim_sn, noktalar: Nokta[] } (bayikaynak.Seri).
// Bayi kendi havuzunu (/reseller/kaynak/kullanim), yönetici bir bayiyi
// (/bayi-kaynak/{id}/kullanim) görür. SAHTE VERİ YOK: iki noktadan az → "Veri toplanıyor".
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api, apiHata } from '@/lib/api'
import { UyariKutusu } from '@/components/UyariKutusu'

const KG_EN: Record<string, string> = {
  'Kaynak kullanımı': 'Resource usage',
  'Son 24 saat': 'Last 24 hours',
  'Son 7 gün': 'Last 7 days',
  'Son 30 gün': 'Last 30 days',
  'Veri toplanıyor — ölçümler 5 dakikada bir alınır.': 'Collecting data — measurements are taken every 5 minutes.',
  'Kullanım verisi alınamadı': 'Failed to load usage data',
  'Sınır': 'Limit',
  'çekirdek': 'cores',
  'Sınırsız': 'Unlimited',
  'Bellek aşımı': 'Out-of-memory events',
  'Zaman aralığı': 'Time range',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (KG_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Nokta = {
  t: string
  cpu_milli: number | null; cpu_sinir_milli: number
  ram_mb: number | null; ram_sinir_mb: number
  disk_mb: number | null; disk_sinir_mb: number
  oom_olay: number | null
}
type Yanit = { adim_sn: number; noktalar: Nokta[] }
type Aralik = '24s' | '7g' | '30g'

const ARALIKLAR: { k: Aralik; ad: string }[] = [
  { k: '24s', ad: 'Son 24 saat' }, { k: '7g', ad: 'Son 7 gün' }, { k: '30g', ad: 'Son 30 gün' },
]
const DEPO = 'gosp.bayiKaynakAralik'
const RENK = { cpu: '#3b82f6', ram: '#8b5cf6', disk: '#f97316', sinir: '#ef4444' }

function aralikOku(): Aralik {
  try {
    const v = localStorage.getItem(DEPO)
    if (v === '24s' || v === '7g' || v === '30g') return v
  } catch { /* depolama kapalı */ }
  return '24s'
}

function mbYaz(mb: number): string {
  return mb >= 1024 ? `${(mb / 1024).toFixed(mb >= 10240 ? 0 : 1)} GB` : `${Math.round(mb)} MB`
}

function zamanYaz(iso: string, aralik: Aralik): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return aralik === '24s'
    ? d.toLocaleTimeString(i18n.language === 'en' ? 'en-GB' : 'tr-TR', { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleDateString(i18n.language === 'en' ? 'en-GB' : 'tr-TR', { day: '2-digit', month: '2-digit' })
}

function Grafik({ baslik, veri, anahtar, sinir, renk, bicim, alt }: {
  baslik: string; veri: Record<string, number | string | null>[]; anahtar: string
  sinir: number; renk: string; bicim: (n: number) => string; alt?: string
}) {
  const id = `kg-${anahtar}`
  const degerler = veri.map((x) => x[anahtar]).filter((v): v is number => typeof v === 'number')
  const son = degerler.length ? degerler[degerler.length - 1] : null
  const tepe = Math.max(sinir, ...degerler, 0)
  return (
    <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-xs font-semibold text-gray-700 dark:text-dark-100">{baslik}</h3>
        <span className="truncate text-xs tabular-nums text-gray-500 dark:text-dark-300">
          {son !== null ? bicim(son) : '—'} / {sinir > 0 ? bicim(sinir) : cevir('Sınırsız')}
        </span>
      </div>
      <div className="mt-2 h-36">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={veri} margin={{ top: 4, right: 6, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={renk} stopOpacity={0.35} />
                <stop offset="100%" stopColor={renk} stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-gray-200 dark:text-dark-600" vertical={false} />
            <XAxis dataKey="etiket" tickLine={false} axisLine={false} tick={{ fontSize: 10, fill: '#94a3b8' }} minTickGap={28} />
            <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 10, fill: '#94a3b8' }} width={52}
              domain={[0, Math.ceil(tepe * 1.1) || 1]} tickFormatter={(v: number) => bicim(v)} />
            {sinir > 0 && <ReferenceLine y={sinir} stroke={RENK.sinir} strokeDasharray="4 4" label={{ value: cevir('Sınır'), position: 'insideTopRight', fontSize: 10, fill: RENK.sinir }} />}
            <Tooltip formatter={(v) => (typeof v === 'number' ? bicim(v) : String(v ?? '—'))} labelFormatter={(l) => String(l)}
              contentStyle={{ fontSize: 12 }} />
            <Area type="monotone" dataKey={anahtar} name={baslik} stroke={renk} strokeWidth={2} fill={`url(#${id})`}
              dot={false} isAnimationActive={false} connectNulls={false} />
          </AreaChart>
        </ResponsiveContainer>
      </div>
      {alt && <p className="mt-1 text-[11px] text-gray-500 dark:text-dark-300">{alt}</p>}
    </div>
  )
}

export default function KaynakGrafik({ yol }: { yol: string }) {
  useTranslation()
  const [aralik, setAralik] = useState<Aralik>(aralikOku)
  const [veri, setVeri] = useState<Yanit | null>(null)
  const [hata, setHata] = useState('')
  const [yuk, setYuk] = useState(true)

  useEffect(() => {
    let iptal = false
    setYuk(true); setHata('')
    api.get<Yanit>(yol, { params: { aralik } })
      .then((r) => { if (!iptal) setVeri(r.data) })
      .catch((e) => { if (!iptal) { setVeri(null); setHata(apiHata(e)) } })
      .finally(() => { if (!iptal) setYuk(false) })
    return () => { iptal = true }
  }, [yol, aralik])

  function sec(a: Aralik) {
    setAralik(a)
    try { localStorage.setItem(DEPO, a) } catch { /* depolama kapalı */ }
  }

  const noktalar = veri?.noktalar ?? []
  const son = noktalar.length ? noktalar[noktalar.length - 1] : null
  const satirlar = noktalar.map((n) => ({
    etiket: zamanYaz(n.t, aralik),
    cpu: n.cpu_milli === null ? null : n.cpu_milli / 1000,
    ram: n.ram_mb,
    disk: n.disk_mb,
  }))
  const oomToplam = noktalar.reduce((s, n) => s + (n.oom_olay ?? 0), 0)
  const cekirdek = (v: number) => `${v.toFixed(v < 10 ? 2 : 1)} ${cevir('çekirdek')}`

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold text-gray-900 dark:text-dark-50">{cevir('Kaynak kullanımı')}</h2>
        <div role="tablist" aria-label={cevir('Zaman aralığı')} className="inline-flex rounded-lg border border-gray-200 p-0.5 dark:border-dark-600">
          {ARALIKLAR.map((a) => (
            <button key={a.k} type="button" role="tab" aria-selected={aralik === a.k} onClick={() => sec(a.k)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium ${aralik === a.k ? 'bg-primary-600 text-white dark:bg-primary-500' : 'text-gray-600 hover:bg-gray-100 dark:text-dark-200 dark:hover:bg-dark-600'}`}>
              {cevir(a.ad)}
            </button>
          ))}
        </div>
      </div>
      {hata && <div className="mt-3"><UyariKutusu ton="tehlike" baslik={cevir('Kullanım verisi alınamadı')}>{hata}</UyariKutusu></div>}
      {!hata && !yuk && noktalar.length < 2 && (
        <p className="mt-3 text-sm text-gray-500 dark:text-dark-300">{cevir('Veri toplanıyor — ölçümler 5 dakikada bir alınır.')}</p>
      )}
      {!hata && noktalar.length >= 2 && (
        <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-3">
          <Grafik baslik="CPU" veri={satirlar} anahtar="cpu" sinir={(son?.cpu_sinir_milli ?? 0) / 1000} renk={RENK.cpu} bicim={cekirdek} />
          <Grafik baslik="RAM" veri={satirlar} anahtar="ram" sinir={son?.ram_sinir_mb ?? 0} renk={RENK.ram} bicim={mbYaz}
            alt={oomToplam > 0 ? `${cevir('Bellek aşımı')}: ${oomToplam}` : undefined} />
          <Grafik baslik="Disk" veri={satirlar} anahtar="disk" sinir={son?.disk_sinir_mb ?? 0} renk={RENK.disk} bicim={mbYaz} />
        </div>
      )}
    </div>
  )
}
