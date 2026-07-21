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

const SERI = [
  { key: 'yuk1' as const, et: '1 dk', renk: '#f97316' },  // brand
  { key: 'yuk5' as const, et: '5 dk', renk: '#0ea5e9' },  // sky
  { key: 'yuk15' as const, et: '15 dk', renk: '#8b5cf6' }, // violet
]

// SVG düzlemi — uniform ölçek (yazı/çizgi bozulmaz)
const W0 = 1000, H = 320, ML = 46, MR = 16, MT = 16, MB = 30

export default function LoadHistoryChart() {
  const [saat, setSaat] = useState(24)
  const [d, setD] = useState<Yanit | null>(null)
  const [yetkiYok, setYetkiYok] = useState(false)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    let on = true
    async function tick() {
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
  const cek = d?.cekirdek || 0
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
    const tumVal = pts.flatMap(p => [p.yuk1, p.yuk5, p.yuk15])
    const maxVal = Math.max(0.5, cek, ...tumVal)
    const yMax = maxVal * 1.08
    // √ ölçek: düşük yüklerde detay + tepe noktaları da görünür
    const sq = Math.sqrt(yMax)
    const yAt = (v: number) => MT + (1 - Math.sqrt(Math.max(0, Math.min(v, yMax))) / sq) * ch
    const xAt = (i: number) => ML + (pts.length <= 1 ? cw / 2 : (i / (pts.length - 1)) * cw)

    // monoton-kübik yumuşatma (taban altına taşmaz)
    const smooth = (key: keyof Nokta) => {
      const xs = pts.map((_, i) => xAt(i))
      const ys = pts.map(p => yAt(p[key] as number))
      const n = xs.length
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
    const line1 = smooth('yuk1')
    const area1 = pts.length > 1 ? `${line1} L${xAt(pts.length - 1).toFixed(1)},${(H - MB).toFixed(1)} L${xAt(0).toFixed(1)},${(H - MB).toFixed(1)} Z` : ''

    // nice tick (√-konumlu, düşük uçta yoğun)
    const nice = (x: number) => { if (x <= 0) return 0; const p = Math.pow(10, Math.floor(Math.log10(x))); const n = x / p; return (n < 1.5 ? 1 : n < 3 ? 2 : n < 7 ? 5 : 10) * p }
    const rawTicks = [0, nice(yMax * 0.06), nice(yMax * 0.22), nice(yMax * 0.5), Math.round(yMax * 10) / 10]
    const yTicks = [...new Set(rawTicks)].filter(v => v <= yMax).sort((a, b) => a - b)

    const xLabel = (ts: string) => saat > 24 ? ts.slice(5, 10).replace('-', '/') : ts.slice(11, 16)
    const xTickIdx = pts.length > 1 ? [0, Math.floor(pts.length / 3), Math.floor((2 * pts.length) / 3), pts.length - 1] : [0]

    return { yAt, xAt, line1, area1, yTicks, xLabel, xTickIdx, yMax }
  }, [pts, cek, saat, cw, ch])

  if (yetkiYok) return null

  const hp = hover != null ? pts[hover] : null

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    if (pts.length < 2) return
    const rect = e.currentTarget.getBoundingClientRect()
    const px = ((e.clientX - rect.left) / rect.width) * W
    const df = (px - ML) / cw
    setHover(Math.max(0, Math.min(pts.length - 1, Math.round(df * (pts.length - 1)))))
  }

  return (
    <div ref={wrapRef} className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900/60">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Sistem Yükü Geçmişi</h3>
          <p className="text-[11px] text-slate-400 dark:text-slate-500">
            Load average (1 / 5 / 15 dk){cek ? ` · ${cek} çekirdek` : ''} · √ ölçek
          </p>
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

      {/* canlı değerler / hover değerleri */}
      <div className="mb-3 flex flex-wrap items-center gap-x-5 gap-y-1.5">
        {SERI.map(s => {
          const v = hp ? (hp[s.key] as number) : son ? (son[s.key] as number) : null
          const asiri = cek > 0 && v != null && v > cek
          return (
            <div key={s.key} className="flex items-center gap-2 text-xs">
              <span className="h-2.5 w-2.5 rounded-full" style={{ background: s.renk }} />
              <span className="text-slate-500 dark:text-slate-400">{s.et}</span>
              <span className={`font-mono font-semibold tabular-nums ${asiri ? 'text-red-600 dark:text-red-400' : 'text-slate-900 dark:text-slate-100'}`}>
                {v == null ? '—' : v.toFixed(2)}
              </span>
            </div>
          )
        })}
        {hp && <span className="ml-auto font-mono text-[11px] text-slate-400 dark:text-slate-500">{hp.ts.slice(5, 16).replace('-', '/').replace('T', ' ')}</span>}
      </div>

      {!g ? (
        <div className="flex h-[240px] flex-col items-center justify-center text-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} className="h-9 w-9 text-slate-300 dark:text-slate-600">
            <path strokeLinecap="round" strokeLinejoin="round" d="M3 13.125 8 8l3 6 4-9 3 6h4" />
          </svg>
          <p className="mt-3 text-sm text-slate-500 dark:text-slate-400">Henüz veri toplanmadı</p>
          <p className="mt-0.5 text-xs text-slate-400 dark:text-slate-500">Örnekler her dakika kaydedilir; grafik birkaç dakikada dolar.</p>
        </div>
      ) : (
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full select-none" onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
          <defs>
            <linearGradient id="lh-area" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#f97316" stopOpacity="0.30" />
              <stop offset="70%" stopColor="#f97316" stopOpacity="0.06" />
              <stop offset="100%" stopColor="#f97316" stopOpacity="0" />
            </linearGradient>
          </defs>

          {/* yatay ızgara + Y etiketleri */}
          {g.yTicks.map((v, i) => (
            <g key={i}>
              <line x1={ML} y1={g.yAt(v)} x2={W - MR} y2={g.yAt(v)} className="stroke-slate-100 dark:stroke-slate-800" strokeWidth="1" />
              <text x={ML - 8} y={g.yAt(v) + 3.5} textAnchor="end" className="fill-slate-400 dark:fill-slate-500" fontSize="11" fontFamily="ui-monospace,monospace">{v.toFixed(v < 10 ? 1 : 0)}</text>
            </g>
          ))}

          {/* çekirdek referans (=%100 kapasite) */}
          {cek > 0 && cek <= g.yMax && (
            <g>
              <line x1={ML} y1={g.yAt(cek)} x2={W - MR} y2={g.yAt(cek)} stroke="#ef4444" strokeWidth="1" strokeDasharray="5 5" opacity="0.55" />
              <text x={W - MR} y={g.yAt(cek) - 5} textAnchor="end" fill="#ef4444" fontSize="10" opacity="0.85">{cek} çekirdek · %100</text>
            </g>
          )}

          {/* X etiketleri */}
          {g.xTickIdx.map((idx, i) => (
            <text key={i} x={g.xAt(idx)} y={H - 9} textAnchor={i === 0 ? 'start' : i === g.xTickIdx.length - 1 ? 'end' : 'middle'}
              className="fill-slate-400 dark:fill-slate-500" fontSize="11" fontFamily="ui-monospace,monospace">{g.xLabel(pts[idx].ts)}</text>
          ))}

          {/* alan + seriler (15→5→1 üstte) */}
          <path d={g.area1} fill="url(#lh-area)" />
          <SmoothLine gg={g} data={pts} k="yuk15" renk="#8b5cf6" w={1.5} op={0.75} />
          <SmoothLine gg={g} data={pts} k="yuk5" renk="#0ea5e9" w={1.8} op={0.9} />
          <path d={g.line1} fill="none" stroke="#f97316" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

          {/* son nokta nabzı */}
          {pts.length > 0 && (
            <g>
              <circle cx={g.xAt(pts.length - 1)} cy={g.yAt(pts[pts.length - 1].yuk1)} r="7" fill="#f97316" opacity="0.18">
                <animate attributeName="r" values="4;9;4" dur="2.4s" repeatCount="indefinite" />
                <animate attributeName="opacity" values="0.28;0;0.28" dur="2.4s" repeatCount="indefinite" />
              </circle>
              <circle cx={g.xAt(pts.length - 1)} cy={g.yAt(pts[pts.length - 1].yuk1)} r="3" fill="#f97316" />
            </g>
          )}

          {/* hover crosshair + noktalar + tooltip */}
          {hp && hover != null && (
            <g>
              <line x1={g.xAt(hover)} y1={MT} x2={g.xAt(hover)} y2={H - MB} className="stroke-slate-300 dark:stroke-slate-600" strokeWidth="1" strokeDasharray="3 3" />
              {SERI.map(s => (
                <circle key={s.key} cx={g.xAt(hover)} cy={g.yAt(hp[s.key] as number)} r="3.5" fill={s.renk} stroke="#0d1524" strokeWidth="1.5" />
              ))}
            </g>
          )}
        </svg>
      )}
    </div>
  )
}

function SmoothLine({ gg, data, k, renk, w, op }: { gg: any; data: Nokta[]; k: keyof Nokta; renk: string; w: number; op: number }) {
  // gg.line1 is precomputed for yuk1; for others we recompute via same smoother
  const path = smoothOf(gg, data, k)
  return <path d={path} fill="none" stroke={renk} strokeWidth={w} strokeLinejoin="round" strokeLinecap="round" opacity={op} />
}

function smoothOf(gg: any, data: Nokta[], key: keyof Nokta): string {
  const xs = data.map((_, i) => gg.xAt(i))
  const ys = data.map(p => gg.yAt(p[key] as number))
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
