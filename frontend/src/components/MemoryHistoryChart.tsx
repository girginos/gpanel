import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '@/lib/api'

type Nokta = { ts: string; yuk1: number; yuk5: number; yuk15: number; bellek: number }
type Yanit = { saat: number; cekirdek: number; noktalar: Nokta[] }

const ARALIKLAR = [
  { et: '1sa', saat: 1 },
  { et: '6sa', saat: 6 },
  { et: '24sa', saat: 24 },
  { et: '7g', saat: 168 },
]

// SVG düzlemi — uniform ölçek
const W0 = 1000, H = 300, ML = 40, MR = 16, MT = 16, MB = 28
const RENK = '#0ea5e9' // sky — CPU grafiğinden görsel olarak ayrışsın

// Bellek kullanımı geçmişi (%). /system/load-history noktalarındaki `bellek` alanını çizer.
// LoadHistoryChart ile aynı görsel dil: alan + gradyan + yumuşatılmış çizgi + ızgara + hover.
export default function MemoryHistoryChart() {
  const [saat, setSaat] = useState(24)
  const [d, setD] = useState<Yanit | null>(null)
  const [yetkiYok, setYetkiYok] = useState(false)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    let on = true
    async function tick() {
      if (typeof document !== 'undefined' && document.hidden) return
      try {
        const r = await api.get<Yanit>(`/system/load-history?saat=${saat}`)
        if (on) { setD(r.data); setYetkiYok(false) }
      } catch (e: any) {
        if (on && e?.response?.status === 403) setYetkiYok(true)
      }
    }
    tick()
    const t = setInterval(tick, 30000)
    return () => { on = false; clearInterval(t) }
  }, [saat])

  const pts = d?.noktalar || []
  const son = pts[pts.length - 1]
  // Kart genisligini olc: viewBox'i 1:1 yaparak yukseklik W'ye kilitli kalmaz,
  // sabit H (px) yuksekliginde net (bozulmayan) grafik + okunur zaman ekseni.
  const wrapRef = useRef<HTMLDivElement>(null)
  const [Wpx, setWpx] = useState(0)
  useEffect(() => {
    const el = wrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((es) => { const w = es[0]?.contentRect.width; if (w && w > 0) setWpx(Math.round(w)) })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const W = Wpx > 40 ? Wpx - 40 : W0 // p-5 (kart ic bosluk) = 2x20px

  const cw = W - ML - MR, ch = H - MT - MB

  const g = useMemo(() => {
    if (!pts.length) return null
    const yMax = 100
    const yAt = (v: number) => MT + (1 - Math.max(0, Math.min(v, yMax)) / yMax) * ch
    const xAt = (i: number) => ML + (pts.length <= 1 ? cw / 2 : (i / (pts.length - 1)) * cw)
    const line = smoothPath(pts, xAt, (p) => yAt(p.bellek))
    const area = pts.length > 1
      ? `${line} L${xAt(pts.length - 1).toFixed(1)},${(H - MB).toFixed(1)} L${xAt(0).toFixed(1)},${(H - MB).toFixed(1)} Z`
      : ''
    const yTicks = [0, 25, 50, 75, 100]
    const xLabel = (ts: string) => saat > 24 ? ts.slice(5, 10).replace('-', '/') : ts.slice(11, 16)
    const xTickIdx = pts.length > 1 ? [0, Math.floor(pts.length / 3), Math.floor((2 * pts.length) / 3), pts.length - 1] : [0]
    return { yAt, xAt, line, area, yTicks, xLabel, xTickIdx }
  }, [pts, saat, cw, ch])

  if (yetkiYok) return null

  const hp = hover != null ? pts[hover] : null
  const guncelDeger = hp ? hp.bellek : son ? son.bellek : null

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    if (pts.length < 2) return
    const rect = e.currentTarget.getBoundingClientRect()
    const px = ((e.clientX - rect.left) / rect.width) * W
    const df = (px - ML) / cw
    setHover(Math.max(0, Math.min(pts.length - 1, Math.round(df * (pts.length - 1)))))
  }

  const asiri = guncelDeger != null && guncelDeger >= 85
  const dikkat = guncelDeger != null && guncelDeger >= 70

  return (
    <div ref={wrapRef} className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900/60">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round"
            className="h-4 w-4 text-slate-400 dark:text-slate-500">
            <path d="M3 7h18v10H3zM7 7v10m5-10v10m5-10v10" />
          </svg>
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Bellek kullanımı</h3>
            <p className="text-[11px] text-slate-400 dark:text-slate-500">Kullanılan RAM yüzdesi · 0–100%</p>
          </div>
        </div>
        <div className="flex items-center gap-0.5 rounded-xl border border-slate-200 bg-slate-100 p-0.5 dark:border-slate-800 dark:bg-slate-800/60">
          {ARALIKLAR.map(a => (
            <button key={a.saat} onClick={() => { setSaat(a.saat); setHover(null) }}
              className={`rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${saat === a.saat
                ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-slate-100'
                : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}`}>
              {a.et}
            </button>
          ))}
        </div>
      </div>

      {/* canlı / hover değeri */}
      <div className="mb-3 flex flex-wrap items-center gap-x-5 gap-y-1.5">
        <div className="flex items-center gap-2 text-xs">
          <span className="h-2.5 w-2.5 rounded-full" style={{ background: RENK }} />
          <span className="text-slate-500 dark:text-slate-400">Bellek</span>
          <span className={`font-mono font-semibold tabular-nums ${asiri ? 'text-red-600 dark:text-red-400' : dikkat ? 'text-amber-600 dark:text-amber-400' : 'text-slate-900 dark:text-slate-100'}`}>
            {guncelDeger == null ? '—' : `%${guncelDeger.toFixed(1)}`}
          </span>
        </div>
        {hp && <span className="ml-auto font-mono text-[11px] text-slate-400 dark:text-slate-500">{hp.ts.slice(5, 16).replace('-', '/').replace('T', ' ')}</span>}
      </div>

      {!g ? (
        <div className="flex h-[200px] flex-col items-center justify-center text-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} className="h-9 w-9 text-slate-300 dark:text-slate-600">
            <path strokeLinecap="round" strokeLinejoin="round" d="M3 13.125 8 8l3 6 4-9 3 6h4" />
          </svg>
          <p className="mt-3 text-sm text-slate-500 dark:text-slate-400">Henüz veri toplanmadı</p>
          <p className="mt-0.5 text-xs text-slate-400 dark:text-slate-500">Örnekler her dakika kaydedilir; grafik birkaç dakikada dolar.</p>
        </div>
      ) : (
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full select-none" onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
          <defs>
            <linearGradient id="mem-area" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={RENK} stopOpacity="0.28" />
              <stop offset="70%" stopColor={RENK} stopOpacity="0.06" />
              <stop offset="100%" stopColor={RENK} stopOpacity="0" />
            </linearGradient>
          </defs>

          {/* yatay ızgara + Y etiketleri */}
          {g.yTicks.map((v, i) => (
            <g key={i}>
              <line x1={ML} y1={g.yAt(v)} x2={W - MR} y2={g.yAt(v)} className="stroke-slate-100 dark:stroke-slate-800" strokeWidth="1" />
              <text x={ML - 8} y={g.yAt(v) + 3.5} textAnchor="end" className="fill-slate-400 dark:fill-slate-500" fontSize="11" fontFamily="ui-monospace,monospace">{v}</text>
            </g>
          ))}

          {/* %85 kritik referans çizgisi */}
          <g>
            <line x1={ML} y1={g.yAt(85)} x2={W - MR} y2={g.yAt(85)} stroke="#ef4444" strokeWidth="1" strokeDasharray="5 5" opacity="0.5" />
            <text x={W - MR} y={g.yAt(85) - 5} textAnchor="end" fill="#ef4444" fontSize="10" opacity="0.8">%85 kritik</text>
          </g>

          {/* X etiketleri */}
          {g.xTickIdx.map((idx, i) => (
            <text key={i} x={g.xAt(idx)} y={H - 9} textAnchor={i === 0 ? 'start' : i === g.xTickIdx.length - 1 ? 'end' : 'middle'}
              className="fill-slate-400 dark:fill-slate-500" fontSize="11" fontFamily="ui-monospace,monospace">{g.xLabel(pts[idx].ts)}</text>
          ))}

          {/* alan + çizgi */}
          <path d={g.area} fill="url(#mem-area)" />
          <path d={g.line} fill="none" stroke={RENK} strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

          {/* son nokta nabzı */}
          {pts.length > 0 && (
            <g>
              <circle cx={g.xAt(pts.length - 1)} cy={g.yAt(pts[pts.length - 1].bellek)} r="7" fill={RENK} opacity="0.18">
                <animate attributeName="r" values="4;9;4" dur="2.4s" repeatCount="indefinite" />
                <animate attributeName="opacity" values="0.28;0;0.28" dur="2.4s" repeatCount="indefinite" />
              </circle>
              <circle cx={g.xAt(pts.length - 1)} cy={g.yAt(pts[pts.length - 1].bellek)} r="3" fill={RENK} />
            </g>
          )}

          {/* hover crosshair + nokta */}
          {hp && hover != null && (
            <g>
              <line x1={g.xAt(hover)} y1={MT} x2={g.xAt(hover)} y2={H - MB} className="stroke-slate-300 dark:stroke-slate-600" strokeWidth="1" strokeDasharray="3 3" />
              <circle cx={g.xAt(hover)} cy={g.yAt(hp.bellek)} r="3.5" fill={RENK} stroke="#0d1524" strokeWidth="1.5" />
            </g>
          )}
        </svg>
      )}
    </div>
  )
}

// monoton-kübik yumuşatma (taban altına taşmaz) — LoadHistoryChart ile aynı yöntem
function smoothPath(data: Nokta[], xAt: (i: number) => number, yAt: (p: Nokta) => number): string {
  const xs = data.map((_, i) => xAt(i))
  const ys = data.map(p => yAt(p))
  const n = xs.length
  if (n === 0) return ''
  if (n === 1) return `M${xs[0].toFixed(1)},${ys[0].toFixed(1)}`
  const dx: number[] = [], m: number[] = []
  for (let i = 0; i < n - 1; i++) { dx[i] = xs[i + 1] - xs[i]; m[i] = (ys[i + 1] - ys[i]) / (dx[i] || 1) }
  const t: number[] = new Array(n)
  t[0] = m[0]; t[n - 1] = m[n - 2]
  for (let i = 1; i < n - 1; i++) t[i] = m[i - 1] * m[i] <= 0 ? 0 : (m[i - 1] + m[i]) / 2
  for (let i = 0; i < n - 1; i++) {
    if (m[i] === 0) { t[i] = 0; t[i + 1] = 0; continue }
    const a = t[i] / m[i], b = t[i + 1] / m[i], h = Math.hypot(a, b)
    if (h > 3) { const s = 3 / h; t[i] = s * a * m[i]; t[i + 1] = s * b * m[i] }
  }
  let p = `M${xs[0].toFixed(1)},${ys[0].toFixed(1)}`
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i]
    p += ` C${(xs[i] + h / 3).toFixed(1)},${(ys[i] + t[i] * h / 3).toFixed(1)} ${(xs[i + 1] - h / 3).toFixed(1)},${(ys[i + 1] - t[i + 1] * h / 3).toFixed(1)} ${xs[i + 1].toFixed(1)},${ys[i + 1].toFixed(1)}`
  }
  return p
}
