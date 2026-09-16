// OPS paneli — Grafana benzeri operasyon gorunumu.
//
// BAGIMSIZ MODUL: kendi CSS'i (`ops.css`), kendi veri cekimi, kendi durumu.
// Pano (`HomePage`/`dashboard.css`) ile hicbir sey paylasmaz.
//
// Veri kaynagi GET /system/load-history?saat=N — 60 sn'de bir ornekleyen
// kalici seri (7 gun saklama). Kova basina hem ORTALAMA hem TEPE doner;
// ortalama cizgi, tepe soluk bant olarak cizilir.
import '@/ops.css'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api } from '@/lib/api'
import {
  Area, AreaChart, CartesianGrid, Line, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { Activity, Cpu, Gauge, HardDrive, MemoryStick, Network, RefreshCw } from 'lucide-react'

const OPS_EN: Record<string, string> = {
  'Zaman aralığı': 'Time range', 'Yenileme': 'Refresh', 'Kapalı': 'Off', 'Canlı': 'Live', 'Duruk': 'Paused',
  'Şimdi yenile': 'Refresh now', 'CPU': 'CPU', 'Bellek': 'Memory', 'Disk': 'Disk', 'Swap': 'Swap',
  'Yük (1dk)': 'Load (1m)', 'Ağ': 'Network', 'çekirdek': 'cores', 'toplam': 'total',
  'CPU Kullanımı': 'CPU Usage', 'Bellek ve Swap': 'Memory & Swap', 'Yük Ortalaması': 'Load Average',
  'Ağ Trafiği': 'Network Traffic', 'Disk Kullanımı': 'Disk Usage',
  'ortalama': 'avg', 'tepe': 'peak', 'Gelen': 'In', 'Giden': 'Out',
  'Seri': 'Series', 'En az': 'Min', 'En çok': 'Max', 'Ort': 'Avg', 'Son': 'Last',
  'Bu aralıkta veri yok.': 'No data in this range.',
  'Veri toplanıyor — ilk örnekler 60 saniyede bir yazılıyor.': 'Collecting data — samples are written every 60 seconds.',
  'Geçmiş alınamadı': 'Failed to load history',
  'çekirdek sınırı': 'core limit',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (OPS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Nokta = {
  ts: string
  yuk1: number; yuk5: number; yuk15: number
  bellek: number; cpu: number; cpu_max: number
  disk: number; swap: number
  rx_bps: number; rx_max: number; tx_bps: number; tx_max: number
}
type Yanit = { saat: number; cekirdek: number; noktalar: Nokta[] }

const ARALIKLAR = [
  { saat: 1, et: '1sa' }, { saat: 6, et: '6sa' }, { saat: 12, et: '12sa' },
  { saat: 24, et: '24sa' }, { saat: 72, et: '3g' }, { saat: 168, et: '7g' },
]
const YENILEMELER = [
  { sn: 0, et: 'Kapalı' }, { sn: 10, et: '10sn' }, { sn: 30, et: '30sn' }, { sn: 60, et: '1dk' },
]

const RENK = {
  cpu: '#3b82f6', cpuMax: '#1d4ed8',
  bellek: '#8b5cf6', swap: '#f59e0b',
  y1: '#10b981', y5: '#38bdf8', y15: '#a78bfa',
  rx: '#3b82f6', tx: '#10b981',
  disk: '#f97316',
}

// ── biçimlendiriciler ──
const yuzde = (v: number) => `%${(v ?? 0).toFixed(1)}`
function hiz(b: number): string {
  const v = Math.abs(b || 0)
  if (v < 1024) return `${v.toFixed(0)} B/s`
  if (v < 1024 ** 2) return `${(v / 1024).toFixed(1)} KB/s`
  if (v < 1024 ** 3) return `${(v / 1024 ** 2).toFixed(1)} MB/s`
  return `${(v / 1024 ** 3).toFixed(2)} GB/s`
}
// MySQL "2026-09-03 02:31:33" -> yerel saat. Aralik uzunlugu etiketi belirler:
// 24 saatin altinda sadece saat, ustunde gun de gerekir.
function etiketle(ts: string, saat: number): string {
  const d = new Date(ts.replace(' ', 'T'))
  if (isNaN(d.getTime())) return ts.slice(11, 16)
  const ss = d.toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })
  if (saat <= 24) return ss
  return `${d.getDate().toString().padStart(2, '0')}.${(d.getMonth() + 1).toString().padStart(2, '0')} ${ss}`
}

// Esik rengi — tek renk seridi + ayni renkte deger metni.
function esik(v: number, uyari = 70, kritik = 90): string {
  return v >= kritik ? 'var(--ops-crit)' : v >= uyari ? 'var(--ops-warn)' : 'var(--ops-ok)'
}

// Seri istatistigi: Grafana lejantindaki min/max/ort/son sutunlari.
function istat(dizi: number[]) {
  if (!dizi.length) return { min: 0, max: 0, ort: 0, son: 0 }
  let min = Infinity, max = -Infinity, top = 0
  for (const v of dizi) { if (v < min) min = v; if (v > max) max = v; top += v }
  return { min, max, ort: top / dizi.length, son: dizi[dizi.length - 1] }
}

type SeriTanim = { ad: string; renk: string; degerler: number[]; bicim: (v: number) => string }

function Lejant({ seriler }: { seriler: SeriTanim[] }) {
  return (
    <div className="ops-legend">
      <table>
        <thead>
          <tr>
            <th>{cevir('Seri')}</th>
            <th>{cevir('En az')}</th>
            <th>{cevir('En çok')}</th>
            <th>{cevir('Ort')}</th>
            <th>{cevir('Son')}</th>
          </tr>
        </thead>
        <tbody>
          {seriler.map((s) => {
            const k = istat(s.degerler)
            return (
              <tr key={s.ad}>
                <td><i style={{ background: s.renk }} />{s.ad}</td>
                <td>{s.bicim(k.min)}</td>
                <td>{s.bicim(k.max)}</td>
                <td>{s.bicim(k.ort)}</td>
                <td>{s.bicim(k.son)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function Ipucu({ active, payload, label, bicim }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="ops-tip">
      <strong>{label}</strong>
      {payload.map((p: any) => (
        <div key={p.dataKey}>
          <i style={{ background: p.color || p.stroke }} />
          <span>{p.name}</span>
          <b>{bicim(p.value)}</b>
        </div>
      ))}
    </div>
  )
}

function Panel({ baslik, ikon: Ikon, sag, genis, children }: {
  baslik: string; ikon: any; sag?: string; genis?: boolean; children: React.ReactNode
}) {
  return (
    <section className={`ops-panel${genis ? ' genis' : ''}`}>
      <div className="ops-head">
        <h3><Ikon />{baslik}</h3>
        {sag ? <span>{sag}</span> : null}
      </div>
      {children}
    </section>
  )
}

export default function OpsPanel() {
  useTranslation()
  const [saat, setSaat] = useState(6)
  const [yenileSn, setYenileSn] = useState(30)
  const [veri, setVeri] = useState<Yanit | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [sonCekim, setSonCekim] = useState<Date | null>(null)
  const mesgul = useRef(false)

  const cek = useCallback(() => {
    // 🔴 Ust uste binmeyi ONLE: 7 gunluk sorgu yavas donebilir; 10 sn'lik
    // yenilemede istekler kuyruga girip sunucuyu bosuna yorardi.
    //
    // 🔴 BURADA `document.hidden` KONTROLU YOK — bilerek. Gizli sekme kontrolu
    // yalnizca ARALIKLI yenilemeye aittir (asagida). Ilk yuklemeye de konsaydi,
    // arka planda acilan sekme veriyi HIC cekmezdi; yenileme "Kapali" ise
    // sayfa kalici olarak "Veri toplaniyor" ekraninda kalirdi.
    if (mesgul.current) return
    mesgul.current = true
    api.get<Yanit>(`/system/load-history?saat=${saat}`)
      .then((r) => { setVeri(r.data); setHata(null); setSonCekim(new Date()) })
      .catch(() => setHata(cevir('Geçmiş alınamadı')))
      .finally(() => { mesgul.current = false })
  }, [saat])

  useEffect(() => { cek() }, [cek])
  useEffect(() => {
    if (!yenileSn) return
    // Gizli sekmede yoklama YAPMA — kullanici bakmiyorken sunucuyu yormanin
    // anlami yok. Sekme geri gorunur olunca bir sonraki tik veriyi tazeler.
    const t = setInterval(() => {
      if (typeof document !== 'undefined' && document.hidden) return
      cek()
    }, yenileSn * 1000)
    return () => clearInterval(t)
  }, [yenileSn, cek])

  const n = veri?.noktalar || []
  const cekirdek = veri?.cekirdek || 0

  // Grafik verisi tek seferde hazirlanir; her panel ayni diziyi kullanir.
  // 🔴 Giden trafik NEGATIF yaziliyor: aynalanmis (yukari gelen / asagi giden)
  // gorunum, iki yonu ust uste bindirmeden karsilastirmayi mumkun kilar.
  const d = useMemo(() => n.map((p) => ({
    t: etiketle(p.ts, saat),
    cpu: p.cpu, cpuMax: p.cpu_max,
    bellek: p.bellek, swap: p.swap, disk: p.disk,
    y1: p.yuk1, y5: p.yuk5, y15: p.yuk15,
    rx: p.rx_bps, rxMax: p.rx_max,
    txNeg: -Math.abs(p.tx_bps), txMaxNeg: -Math.abs(p.tx_max), tx: p.tx_bps,
  })), [n, saat])

  const son = n.length ? n[n.length - 1] : null
  const bosMu = n.length === 0

  const kutular = son ? [
    { ad: 'CPU', ikon: Cpu, deger: yuzde(son.cpu), alt: `${cekirdek} ${cevir('çekirdek')}`, renk: esik(son.cpu) },
    { ad: cevir('Bellek'), ikon: MemoryStick, deger: yuzde(son.bellek), alt: `${cevir('tepe')} ${yuzde(istat(n.map(x => x.bellek)).max)}`, renk: esik(son.bellek) },
    { ad: cevir('Disk'), ikon: HardDrive, deger: yuzde(son.disk), alt: '/', renk: esik(son.disk, 80, 92) },
    { ad: cevir('Swap'), ikon: Gauge, deger: yuzde(son.swap), alt: `${cevir('tepe')} ${yuzde(istat(n.map(x => x.swap)).max)}`, renk: esik(son.swap, 25, 60) },
    { ad: cevir('Yük (1dk)'), ikon: Activity, deger: son.yuk1.toFixed(2), alt: `${cevir('çekirdek sınırı')} ${cekirdek}`, renk: cekirdek ? esik((son.yuk1 / cekirdek) * 100, 80, 110) : 'var(--ops-dim)' },
    { ad: cevir('Ağ'), ikon: Network, deger: hiz(son.rx_bps + son.tx_bps), alt: `↓${hiz(son.rx_bps)} ↑${hiz(son.tx_bps)}`, renk: 'var(--ops-ok)' },
  ] : []

  const eksenStil = { fill: '#6b7c92', fontSize: 11 }
  const izgara = <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,.05)" vertical={false} />
  const xEksen = <XAxis dataKey="t" axisLine={false} tickLine={false} tick={eksenStil} interval="preserveStartEnd" minTickGap={44} />

  return (
    <div className="gp-ops">
      {/* araç çubuğu */}
      <div className="ops-bar">
        <span className="ops-label">{cevir('Zaman aralığı')}</span>
        <div className="ops-pills">
          {ARALIKLAR.map((a) => (
            <button key={a.saat} aria-pressed={saat === a.saat} onClick={() => setSaat(a.saat)}>{a.et}</button>
          ))}
        </div>
        <div className="ops-bar-sep" />
        <span className="ops-label">{cevir('Yenileme')}</span>
        <select className="ops-select" value={yenileSn} onChange={(e) => setYenileSn(Number(e.target.value))}>
          {YENILEMELER.map((y) => <option key={y.sn} value={y.sn}>{cevir(y.et)}</option>)}
        </select>
        <span className={`ops-canli${yenileSn ? '' : ' duruk'}`}>
          <i />{yenileSn ? cevir('Canlı') : cevir('Duruk')}
          {sonCekim ? ` · ${sonCekim.toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}` : ''}
        </span>
        <button className="ops-select" onClick={cek} title={cevir('Şimdi yenile')} aria-label={cevir('Şimdi yenile')}>
          <RefreshCw size={13} />
        </button>
      </div>

      {/* tek-değer kutuları */}
      {kutular.length > 0 && (
        <div className="ops-stats">
          {kutular.map((k) => (
            <div className="ops-stat" key={k.ad} style={{ ['--d' as any]: k.renk }}>
              <h4><k.ikon />{k.ad}</h4>
              <b>{k.deger}</b>
              <small>{k.alt}</small>
            </div>
          ))}
        </div>
      )}

      {bosMu ? (
        <section className="ops-panel">
          <div className="ops-bos">
            {hata || cevir('Veri toplanıyor — ilk örnekler 60 saniyede bir yazılıyor.')}
          </div>
        </section>
      ) : (
        <div className="ops-grid">
          {/* CPU — ortalama alan + tepe çizgisi */}
          <Panel baslik={cevir('CPU Kullanımı')} ikon={Cpu} sag={`${cevir('ortalama')} + ${cevir('tepe')}`}>
            <div className="ops-chart">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={d} margin={{ top: 4, right: 10, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="opsCpu" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={RENK.cpu} stopOpacity={0.35} />
                      <stop offset="100%" stopColor={RENK.cpu} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  {izgara}{xEksen}
                  <YAxis axisLine={false} tickLine={false} tick={eksenStil} domain={[0, 100]} width={38} tickFormatter={(v: any) => `%${v}`} />
                  <ReferenceLine y={90} stroke="rgba(244,63,94,.35)" strokeDasharray="4 4" />
                  <Tooltip content={<Ipucu bicim={yuzde} />} />
                  <Area type="monotone" dataKey="cpuMax" name={cevir('tepe')} stroke={RENK.cpuMax} strokeWidth={1} strokeDasharray="3 3" fill="none" dot={false} isAnimationActive={false} />
                  <Area type="monotone" dataKey="cpu" name={cevir('ortalama')} stroke={RENK.cpu} strokeWidth={2} fill="url(#opsCpu)" dot={false} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <Lejant seriler={[
              { ad: cevir('ortalama'), renk: RENK.cpu, degerler: d.map(x => x.cpu), bicim: yuzde },
              { ad: cevir('tepe'), renk: RENK.cpuMax, degerler: d.map(x => x.cpuMax), bicim: yuzde },
            ]} />
          </Panel>

          {/* Bellek + Swap */}
          <Panel baslik={cevir('Bellek ve Swap')} ikon={MemoryStick}>
            <div className="ops-chart">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={d} margin={{ top: 4, right: 10, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="opsMem" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={RENK.bellek} stopOpacity={0.35} />
                      <stop offset="100%" stopColor={RENK.bellek} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  {izgara}{xEksen}
                  <YAxis axisLine={false} tickLine={false} tick={eksenStil} domain={[0, 100]} width={38} tickFormatter={(v: any) => `%${v}`} />
                  <Tooltip content={<Ipucu bicim={yuzde} />} />
                  <Area type="monotone" dataKey="bellek" name={cevir('Bellek')} stroke={RENK.bellek} strokeWidth={2} fill="url(#opsMem)" dot={false} isAnimationActive={false} />
                  <Area type="monotone" dataKey="swap" name={cevir('Swap')} stroke={RENK.swap} strokeWidth={1.6} fill="none" dot={false} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <Lejant seriler={[
              { ad: cevir('Bellek'), renk: RENK.bellek, degerler: d.map(x => x.bellek), bicim: yuzde },
              { ad: cevir('Swap'), renk: RENK.swap, degerler: d.map(x => x.swap), bicim: yuzde },
            ]} />
          </Panel>

          {/* Yük ortalaması — çekirdek sayısı referans çizgisi */}
          <Panel baslik={cevir('Yük Ortalaması')} ikon={Activity} sag={cekirdek ? `${cevir('çekirdek sınırı')}: ${cekirdek}` : undefined}>
            <div className="ops-chart">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={d} margin={{ top: 4, right: 10, left: 0, bottom: 0 }}>
                  {izgara}{xEksen}
                  <YAxis axisLine={false} tickLine={false} tick={eksenStil} width={38} />
                  {/* 🔴 Cekirdek sayisi = doygunluk esigi. Yuk bu cizgiyi asiyorsa
                      calisabilir surecler CPU bekliyor demektir. */}
                  {cekirdek > 0 && <ReferenceLine y={cekirdek} stroke="rgba(244,63,94,.45)" strokeDasharray="4 4" />}
                  <Tooltip content={<Ipucu bicim={(v: number) => (v ?? 0).toFixed(2)} />} />
                  <Area type="monotone" dataKey="y1" name="1dk" stroke={RENK.y1} strokeWidth={2} fill="none" dot={false} isAnimationActive={false} />
                  <Area type="monotone" dataKey="y5" name="5dk" stroke={RENK.y5} strokeWidth={1.5} fill="none" dot={false} isAnimationActive={false} />
                  <Area type="monotone" dataKey="y15" name="15dk" stroke={RENK.y15} strokeWidth={1.5} strokeDasharray="3 3" fill="none" dot={false} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <Lejant seriler={[
              { ad: '1dk', renk: RENK.y1, degerler: d.map(x => x.y1), bicim: (v) => v.toFixed(2) },
              { ad: '5dk', renk: RENK.y5, degerler: d.map(x => x.y5), bicim: (v) => v.toFixed(2) },
              { ad: '15dk', renk: RENK.y15, degerler: d.map(x => x.y15), bicim: (v) => v.toFixed(2) },
            ]} />
          </Panel>

          {/* Disk */}
          <Panel baslik={cevir('Disk Kullanımı')} ikon={HardDrive} sag="/">
            <div className="ops-chart">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={d} margin={{ top: 4, right: 10, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="opsDisk" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={RENK.disk} stopOpacity={0.3} />
                      <stop offset="100%" stopColor={RENK.disk} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  {izgara}{xEksen}
                  <YAxis axisLine={false} tickLine={false} tick={eksenStil} domain={[0, 100]} width={38} tickFormatter={(v: any) => `%${v}`} />
                  <ReferenceLine y={90} stroke="rgba(244,63,94,.35)" strokeDasharray="4 4" />
                  <Tooltip content={<Ipucu bicim={yuzde} />} />
                  <Area type="monotone" dataKey="disk" name={cevir('Disk')} stroke={RENK.disk} strokeWidth={2} fill="url(#opsDisk)" dot={false} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <Lejant seriler={[{ ad: cevir('Disk'), renk: RENK.disk, degerler: d.map(x => x.disk), bicim: yuzde }]} />
          </Panel>

          {/* Ağ — aynalanmış (gelen yukarı / giden aşağı) */}
          <Panel baslik={cevir('Ağ Trafiği')} ikon={Network} genis sag={`↓ ${cevir('Gelen')} · ↑ ${cevir('Giden')}`}>
            <div className="ops-chart">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={d} margin={{ top: 4, right: 10, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="opsRx" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={RENK.rx} stopOpacity={0.32} />
                      <stop offset="100%" stopColor={RENK.rx} stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="opsTx" x1="0" y1="1" x2="0" y2="0">
                      <stop offset="0%" stopColor={RENK.tx} stopOpacity={0.32} />
                      <stop offset="100%" stopColor={RENK.tx} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  {izgara}{xEksen}
                  <YAxis axisLine={false} tickLine={false} tick={eksenStil} width={68} tickFormatter={(v: any) => hiz(v)} />
                  <ReferenceLine y={0} stroke="rgba(255,255,255,.16)" />
                  <Tooltip content={<Ipucu bicim={hiz} />} />
                  <Area type="monotone" dataKey="rx" name={cevir('Gelen')} stroke={RENK.rx} strokeWidth={2} fill="url(#opsRx)" dot={false} isAnimationActive={false} />
                  <Area type="monotone" dataKey="txNeg" name={cevir('Giden')} stroke={RENK.tx} strokeWidth={2} fill="url(#opsTx)" dot={false} isAnimationActive={false} />
                  <Line type="monotone" dataKey="rxMax" name={`${cevir('Gelen')} ${cevir('tepe')}`} stroke={RENK.rx} strokeWidth={1} strokeDasharray="3 3" dot={false} isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
            <Lejant seriler={[
              { ad: cevir('Gelen'), renk: RENK.rx, degerler: d.map(x => x.rx), bicim: hiz },
              { ad: cevir('Giden'), renk: RENK.tx, degerler: d.map(x => x.tx), bicim: hiz },
            ]} />
          </Panel>
        </div>
      )}
    </div>
  )
}
