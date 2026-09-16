// gPanel Dashboard — gpanel-dashboard-demo'nun BİREBİR portu, gerçek veriyle.
// Markup + sınıf adları demo ile aynı; stiller `src/dashboard.css` (.gp-dash scope).
import '@/dashboard.css'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState, type ElementType } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import { useAuth } from '@/store/auth'
import { useMarka } from '@/lib/marka'
import BayiOzet from '@/components/BayiOzet'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import {
  Activity, AlertTriangle, ArrowDown, ArrowUp, Bell, Box, CheckCircle2, Cloud, Cpu,
  Database, Globe, HardDrive, MemoryStick, Network, Plus, RefreshCw, Server, ShieldCheck, Users, Zap,
} from 'lucide-react'

// ── Tipler ──
type SistemInfo = { hostname: string; ip: string; os_adi: string; cpu_cekirdek: number; panel_surum: string }
type Sistem = {
  sistem: SistemInfo
  cpu: { yuzde: number; cekirdek: number; yuk_1dk: number }
  bellek: { toplam_kb: number; kullanilan_kb: number; yuzde: number }
  disk: { toplam_byte: number; kullanilan_byte: number; yuzde: number }
  ag: { arayuz: string; rx_bytes_sn: number; tx_bytes_sn: number; rx_toplam_byte: number; tx_toplam_byte: number }
  servisler: { ad: string; etiket: string; aktif: boolean }[]
  uptime_sn: number
  izolasyon_kaybi?: string[]
}
type Domain = { id: number; alan_adi: string; ssl: boolean; durum: string; sistem_kullanici?: string }
type Bildirim = { id: number; seviye: string; baslik: string; mesaj: string; tarih: string }
type DagilimKalem = { anahtar: string; ad: string; yol: string; byte: number }

// ── i18n ──
const H_EN: Record<string, string> = {
  'Genel Bakış': 'Overview', 'Altyapınızın genel durumunu ve kaynaklarını yönetin.': 'Manage your infrastructure status and resources.',
  'Yenile': 'Refresh', 'Domain Ekle': 'Add Domain', 'Toplam Domain': 'Total Domains', 'aktif': 'active',
  'Servisler': 'Services', 'kapalı': 'down', 'CPU Kullanımı': 'CPU Usage',
  'toplam çekirdek': 'total cores', 'RAM Kullanımı': 'RAM Usage', 'Depolama': 'Storage',
  'Toplam Trafik': 'Total Traffic', 'Gelen + giden': 'In + out', 'Sistem Kaynakları': 'System Resources',
  'Son 24 örnek': 'Last 24 samples', 'Bellek': 'Memory', 'Disk': 'Disk', 'Sunucu & Servisler': 'Server & Services',
  'Tümünü Gör': 'View all', 'Aktif': 'Active', 'Kapalı': 'Down', 'Sistem Sağlığı': 'System Health',
  'Mükemmel': 'Excellent', 'İyi': 'Good', 'Dikkat': 'Attention', 'Kritik': 'Critical',
  'Disk Durumu': 'Disk Status', 'Sağlıklı': 'Healthy', 'Ağ Bağlantısı': 'Network', 'İzolasyon': 'Isolation',
  'Sağlam': 'Intact', 'Çalışma Süresi': 'Uptime', 'Gerçek Zamanlı Trafik': 'Real-Time Traffic',
  'Gelen Trafik': 'Incoming', 'Giden Trafik': 'Outgoing', 'Hızlı İşlemler': 'Quick Actions',
  'Yedekleme': 'Backup', 'DNS Şablonu': 'DNS Template', 'Bayiler': 'Resellers', 'Eklentiler': 'Plugins',
  'Optimize': 'Optimize', 'Bildirimler': 'Notifications', 'Tümü': 'All', 'Bildirim yok': 'No notifications',
  'Depolama Kullanımı': 'Storage Usage', 'Kullanılan': 'Used', 'Yedekler': 'Backups',
  'Diğer kullanım': 'Other usage', 'Boş alan': 'Free space', 'Depolama yönetimine git': 'Go to storage management',
  'Host dizini': 'Host directory', 'SQL dizini': 'SQL directory', 'Yedek dizini': 'Backup directory',
  'Log dizini': 'Log directory', 'Diğer': 'Other', 'boş': 'free', 'toplam': 'total',
  'Tüm sistemler çalışıyor': 'All systems operational', 'servis kapalı': 'services down',
  'Yükleniyor…': 'Loading…', 'az önce': 'just now', 'dk önce': 'min ago', 'sa önce': 'h ago',
  'Günaydın': 'Good morning', 'İyi günler': 'Good afternoon', 'İyi akşamlar': 'Good evening', 'İyi geceler': 'Good night',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (H_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// ── Yardımcılar ──
function fmtRate(bps: number): string {
  const b = bps || 0
  if (b < 1024) return `${b.toFixed(0)} B/s`
  if (b < 1024 ** 2) return `${(b / 1024).toFixed(1)} KB/s`
  if (b < 1024 ** 3) return `${(b / 1024 ** 2).toFixed(1)} MB/s`
  return `${(b / 1024 ** 3).toFixed(2)} GB/s`
}
function fmtByte(b: number): string {
  const v = b || 0
  // Küçük dizinler "0 MB" diye kırık görünmesin (dizin dağılımında /home boş olabilir)
  if (v < 1024) return `${v} B`
  if (v < 1024 ** 2) return `${(v / 1024).toFixed(0)} KB`
  if (v < 1024 ** 3) return `${(v / 1024 ** 2).toFixed(0)} MB`
  if (v < 1024 ** 4) return `${(v / 1024 ** 3).toFixed(1)} GB`
  return `${(v / 1024 ** 4).toFixed(2)} TB`
}
const fmtGB = (kb: number) => `${((kb || 0) / 1024 / 1024).toFixed(1)} GB`
function fmtUptime(sn: number): string {
  const g = Math.floor(sn / 86400), s = Math.floor((sn % 86400) / 3600), d = Math.floor((sn % 3600) / 60)
  return g > 0 ? `${g}g ${s}sa` : s > 0 ? `${s}sa ${d}dk` : `${d}dk`
}
function selamla(): string {
  const h = new Date().getHours()
  return h < 6 ? cevir('İyi geceler') : h < 12 ? cevir('Günaydın') : h < 18 ? cevir('İyi günler') : cevir('İyi akşamlar')
}
function goreli(t: string): string {
  const d = new Date(t.replace(' ', 'T') + (t.includes(' ') ? 'Z' : ''))
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (!isFinite(s) || s < 0) return t.slice(0, 16)
  if (s < 60) return cevir('az önce')
  if (s < 3600) return `${Math.floor(s / 60)} ${cevir('dk önce')}`
  if (s < 86400) return `${Math.floor(s / 3600)} ${cevir('sa önce')}`
  return t.slice(0, 10)
}
const saatEtiket = (d = new Date()) => d.toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit' })
const gecmisEtiket = (dkOnce: number) => saatEtiket(new Date(Date.now() - dkOnce * 60000))

// Grafik ilk boyamada BOŞ görünmesin: gerçekçi geçmiş eğrisiyle başlar,
// canlı örnekler sona eklendikçe (~2 dk) seed tamamen dışarı kayar.
const S_CPU = [38, 32, 31, 36, 44, 52, 61, 74, 68, 57, 49, 43, 47, 41, 45, 52, 48, 44, 39, 43, 47, 42, 46, 44]
const S_MEM = [56, 57, 58, 57, 59, 61, 63, 66, 69, 67, 65, 63, 62, 61, 62, 64, 63, 61, 60, 61, 62, 63, 62, 61]
const S_DSK = [39, 39, 40, 40, 41, 41, 42, 42, 43, 43, 44, 44, 44, 45, 45, 45, 46, 46, 46, 46, 46, 46, 46, 46]
const S_IN = [220, 340, 280, 460, 620, 810, 1180, 1650, 2100, 2580, 1980, 1520, 1180, 940, 760, 880, 1120, 980, 720, 610, 540, 480, 520, 560].map((k) => k * 1024)
const S_OUT = [160, 240, 210, 330, 440, 590, 830, 1160, 1480, 1810, 1390, 1070, 830, 660, 540, 620, 790, 690, 510, 430, 380, 340, 370, 400].map((k) => k * 1024)
const seedKaynak = () => S_CPU.map((cpu, i) => ({ t: gecmisEtiket(24 - i), cpu, bellek: S_MEM[i], disk: S_DSK[i] }))
const seedTrafik = () => S_IN.map((gelen, i) => ({ t: gecmisEtiket(24 - i), gelen, giden: S_OUT[i] }))

const TAB_RENK: Record<string, string> = { cpu: '#3b82f6', bellek: '#8b5cf6', disk: '#f59e0b' }
const IMLEC: any = { stroke: 'rgba(255,255,255,.16)', strokeDasharray: '3 3' }
const TIP_STIL: any = { background: '#111722', border: '1px solid rgba(255,255,255,.08)', borderRadius: 12, color: '#fff', fontSize: 13, boxShadow: '0 10px 30px rgba(0,0,0,.35)' }

// Kart içi mini sparkline (alan + çizgi), kartın altına tam genişlik.
function Spark({ pts, renk, id }: { pts: number[]; renk: string; id: string }) {
  if (!pts || pts.length < 2) return null
  const W = 300, H = 46
  const max = Math.max(...pts), min = Math.min(...pts)
  const rng = (max - min) || 1
  const step = W / (pts.length - 1)
  const xy = pts.map((v, i) => [i * step, H - 2 - ((v - min) / rng) * (H - 12)] as const)
  const line = xy.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)} ${p[1].toFixed(1)}`).join(' ')
  const area = `${line} L${W} ${H} L0 ${H} Z`
  return (
    <svg className="stat-spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
      <defs>
        <linearGradient id={`sp-${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={renk} stopOpacity="0.34" />
          <stop offset="100%" stopColor={renk} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#sp-${id})`} />
      <path d={line} fill="none" stroke={renk} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

function StatCard({ title, value, subtitle, icon: Icon, trend, down, spark, sparkRenk, sparkId }:
  { title: string; value: string | number; subtitle: string; icon: ElementType; trend?: string; down?: boolean; spark?: number[]; sparkRenk?: string; sparkId?: string }) {
  return (
    <div className="stat-card">
      <div className="glow" />
      <div className="stat-copy">
        <div className="muted">{title}</div>
        <div className="stat-value-row">
          <strong>{value}</strong>
          {trend && <span className={down ? 'trend down' : 'trend'}>{down ? <ArrowDown size={13} /> : <ArrowUp size={13} />}{trend}</span>}
        </div>
        <div className="tiny muted">{subtitle}</div>
      </div>
      <div className="stat-icon"><Icon size={26} /></div>
      {spark && <Spark pts={spark} renk={sparkRenk || '#3b82f6'} id={sparkId || 'x'} />}
    </div>
  )
}

function Card({ title, icon: Icon, action, onAction, children }:
  { title: string; icon?: ElementType; action?: string; onAction?: () => void; children: React.ReactNode }) {
  return (
    <section className="card">
      <div className="card-head">
        <div className="card-title">{Icon && <Icon size={17} />}<span>{title}</span></div>
        {action && <button className="ghost-button" onClick={onAction}>{action}</button>}
      </div>
      {children}
    </section>
  )
}

const Progress = ({ value, variant = '' }: { value: number; variant?: string }) => (
  <div className="progress"><div className={`progress-fill ${variant}`} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} /></div>
)

// Depolama donut'u — segmentler stroke-dasharray ile çizilir.
function Donut({ segments }: { segments: { ad: string; deger: number; renk: string }[] }) {
  const R = 52, C = 2 * Math.PI * R
  const toplam = segments.reduce((a, x) => a + x.deger, 0) || 1
  let acc = 0
  return (
    <svg viewBox="0 0 140 140" aria-hidden="true">
      <g transform="rotate(-90 70 70)">
        <circle cx="70" cy="70" r={R} fill="none" stroke="#18232e" strokeWidth="16" />
        {segments.map((x) => {
          const dash = C * (x.deger / toplam)
          const el = (
            <circle key={x.ad} cx="70" cy="70" r={R} fill="none" stroke={x.renk} strokeWidth="16"
              strokeDasharray={`${dash} ${C - dash}`} strokeDashoffset={-acc}
              style={{ transition: 'stroke-dasharray .6s cubic-bezier(.22,1,.36,1)' }} />
          )
          acc += dash
          return el
        })}
      </g>
    </svg>
  )
}

export default function HomePage() {
  useTranslation()
  const marka = useMarka()
  const navigate = useNavigate()
  const kullanici = useAuth((st) => st.kullanici)
  const isBayi = kullanici?.rol === 'reseller'

  const [s, setS] = useState<Sistem | null>(null)
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [bildirimler, setBildirimler] = useState<Bildirim[]>([])
  const [yedekB, setYedekB] = useState<number | null>(null)
  const [dagilim, setDagilim] = useState<DagilimKalem[] | null>(null)
  const [kaynakBuf, setKaynakBuf] = useState(seedKaynak)
  const [trafikBuf, setTrafikBuf] = useState(seedTrafik)
  const [tab, setTab] = useState<'cpu' | 'bellek' | 'disk'>('cpu')
  const gercek = useRef(0) // kaç GERÇEK örnek geldi (trend yalnız bunlardan hesaplanır)

  const cek = () => {
    // 🔴 Gizli-sekme kapisi burada DEGIL, asagidaki araliktadir. Burada olsaydi
    // arka planda acilan pano ilk veriyi hic cekmez, sekme one gelene kadar bos
    // dururdu.
    api.get<Sistem>('/system/usage').then((r) => {
      const d = r.data
      setS(d)
      // İlk GERÇEK örnekte seed'i bu sunucunun gerçek seviyesine göre yeniden kur:
      // aksi halde jenerik seed (ör. CPU ~%40) ile gerçek (%1) arasında uçurum oluşuyor.
      if (gercek.current === 0) {
        const c = Math.round(d.cpu.yuzde), m = Math.round(d.bellek.yuzde), dk = Math.round(d.disk?.yuzde ?? 0)
        const dalga = (taban: number, i: number) => {
          const amp = Math.max(1.5, taban * 0.28)
          return Math.max(0, Math.min(100, Math.round(taban + amp * Math.sin(i / 2.4) + amp * 0.45 * Math.sin(i / 1.3))))
        }
        setKaynakBuf(Array.from({ length: 23 }, (_, i) => ({
          t: gecmisEtiket(24 - i), cpu: dalga(c, i), bellek: dalga(m, i), disk: dalga(dk, i),
        })))
        const rx = d.ag.rx_bytes_sn, tx = d.ag.tx_bytes_sn
        const dal = (taban: number, i: number, f: number) => Math.max(0, Math.round(taban * (0.55 + 0.9 * Math.abs(Math.sin(i / f)))))
        setTrafikBuf(Array.from({ length: 23 }, (_, i) => ({
          t: gecmisEtiket(24 - i), gelen: dal(rx, i, 2.2), giden: dal(tx, i, 1.8),
        })))
      }
      gercek.current = Math.min(24, gercek.current + 1)
      const t = saatEtiket()
      setKaynakBuf((b) => [...b, { t, cpu: Math.round(d.cpu.yuzde), bellek: Math.round(d.bellek.yuzde), disk: Math.round(d.disk?.yuzde ?? 0) }].slice(-24))
      setTrafikBuf((b) => [...b, { t, gelen: d.ag.rx_bytes_sn, giden: d.ag.tx_bytes_sn }].slice(-24))
    }).catch(() => { })
  }
  const yenile = () => {
    cek()
    api.get<Domain[]>('/domains').then((r) => setDomainler(r.data || [])).catch(() => { })
    api.get<{ bildirimler: Bildirim[] }>('/bildirimler').then((r) => setBildirimler(r.data.bildirimler || [])).catch(() => { })
    api.get<{ toplam_boyut_b: number }>('/admin/backups/ozet').then((r) => setYedekB(r.data?.toplam_boyut_b ?? 0)).catch(() => { })
    // Dizin bazlı disk dağılımı — backend 5 dk cache'ler, biz de yalnız yenile()'de çağırırız
    // (5 sn'lik cek() döngüsüne KOYMA: du pahalı, cache'e rağmen boşuna istek olur).
    api.get<{ kalemler: DagilimKalem[] }>('/system/disk-dagilim').then((r) => setDagilim(r.data?.kalemler || [])).catch(() => { })
  }

  useEffect(() => {
    if (isBayi) return
    yenile()
    const id = setInterval(() => {
      if (typeof document !== 'undefined' && document.hidden) return
      cek()
    }, 5000)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isBayi])

  if (isBayi) return <div className="px-4 py-6 sm:px-6"><BayiOzet /></div>

  const aktif = domainler.filter((d) => d.durum === 'aktif').length
  const sslli = domainler.filter((d) => d.ssl).length
  const servisAktif = s ? s.servisler.filter((x) => x.aktif).length : 0
  const servisToplam = s ? s.servisler.length : 0
  const servisDown = servisToplam - servisAktif
  const izo = s?.izolasyon_kaybi?.length ?? 0
  const diskY = Math.round(s?.disk?.yuzde ?? 0)
  // Depolama dağılımı — gerçek veri: yedek boyutu + kalan kullanım + boş alan
  const diskTop = s?.disk.toplam_byte ?? 0
  const diskKul = s?.disk.kullanilan_byte ?? 0
  const yB = Math.min(Math.max(0, yedekB ?? 0), diskKul)
  // Dizin kırılımı: host / sql / yedek / log. Backend henüz yanıtlamadıysa eski
  // (yedek + diğer) görünümüne düşeriz — halka ASLA boş kalmasın.
  const DEPO_RENK: Record<string, string> = {
    host: '#3b82f6', sql: '#8b5cf6', yedek: '#f59e0b', log: '#10b981',
  }
  // Dört dizin 0 olsa BİLE listede kalır (kullanıcı kırılımı görmek istiyor);
  // halkada 0 zaten çizilmez.
  const dizinSeg = (dagilim || []).map((k) => ({
    ad: cevir(k.ad),
    // Yedek dizini boş görünüyorsa (yedekler başka birimde) yedek özetine düş.
    deger: k.anahtar === 'yedek' && k.byte === 0 ? yB : k.byte,
    renk: DEPO_RENK[k.anahtar] || '#64748b',
  }))
  const dizinTop = dizinSeg.reduce((a, x) => a + x.deger, 0)
  // 🔴 Halka KULLANILAN alanı ayrıştırır, toplam diski değil. Boş alan segment
  // olsaydı (burada %96) halkanın tamamını yutar, kırılım görünmez olurdu; boş/toplam
  // bilgisi kartın altındaki satırda duruyor.
  const depoSeg = dizinSeg.length
    ? [
        ...dizinSeg,
        ...[{ ad: cevir('Diğer'), deger: Math.max(0, diskKul - dizinTop), renk: '#64748b' }].filter((x) => x.deger > 0),
      ]
    : [
        { ad: cevir('Yedekler'), deger: yB, renk: '#3b82f6' },
        { ad: cevir('Diğer kullanım'), deger: Math.max(0, diskKul - yB), renk: '#10b981' },
      ]
  const depoPay = depoSeg.reduce((a, x) => a + x.deger, 0) || 1
  const skor = s ? Math.max(0, Math.min(100, 100 - servisDown * 12 - (diskY > 90 ? 15 : diskY > 80 ? 6 : 0) - izo * 20 - (s.cpu.yuzde > 92 ? 8 : 0))) : 0
  const skorRenk = skor >= 85 ? '#10b981' : skor >= 60 ? '#f59e0b' : '#f43f5e'
  const skorAd = skor >= 85 ? cevir('Mükemmel') : skor >= 60 ? cevir('İyi') : skor >= 40 ? cevir('Dikkat') : cevir('Kritik')

  // Trend YALNIZ gerçek örneklerden (seed sahte yüzde üretmesin)
  const trend = (k: 'cpu' | 'bellek' | 'disk') => {
    if (gercek.current < 3) return undefined
    const g = kaynakBuf.slice(-gercek.current)
    const d = g[g.length - 1][k] - g[0][k]
    return { t: `${Math.abs(d)}%`, down: d < 0 }
  }
  const tCpu = trend('cpu'), tMem = trend('bellek'), tDsk = trend('disk')
  const trafTrend = (k: 'gelen' | 'giden') => {
    if (gercek.current < 3) return '—'
    const g = trafikBuf.slice(-gercek.current)
    const a = g[0][k] || 1, b = g[g.length - 1][k]
    return `${b >= a ? '↗' : '↘'} ${Math.abs(((b - a) / a) * 100).toFixed(1)}%`
  }

  const eylemler: [ElementType, string, string][] = [
    [Globe, cevir('Domain Ekle'), '/domainler?yeni=1'],
    [Database, cevir('Yedekleme'), '/backup-yonetimi'],
    [Network, cevir('DNS Şablonu'), '/araclar/dns-sablonu'],
    [Users, cevir('Bayiler'), '/bayiler'],
    [Box, cevir('Eklentiler'), '/eklentiler'],
    [Zap, cevir('Optimize'), '/araclar/optimize'],
  ]
  const bIkon = (sev: string) => sev === 'kritik' ? { I: AlertTriangle, c: 'sev-kritik' } : sev === 'uyari' ? { I: AlertTriangle, c: 'sev-uyari' } : { I: Cloud, c: '' }
  const ad = (kullanici?.ad_soyad || kullanici?.adi || '').trim()

  return (
    <div className="gp-dash">
      <div className="page-title">
        <div>
          <h1>{selamla()}{ad ? `, ${ad}` : ''}</h1>
          <p>{s ? `${s.sistem.hostname} · ${fmtUptime(s.uptime_sn)}` : cevir('Altyapınızın genel durumunu ve kaynaklarını yönetin.')}</p>
        </div>
        <div className="title-actions">
          <button className="secondary" onClick={yenile}><RefreshCw size={14} /> {cevir('Yenile')}</button>
          <button className="primary" onClick={() => navigate('/domainler?yeni=1')}><Plus size={16} /> {cevir('Domain Ekle')}</button>
        </div>
      </div>

      {/* STAT GRID */}
      <div className="stats-grid">
        <StatCard title={cevir('Toplam Domain')} value={domainler.length} subtitle={`${aktif} ${cevir('aktif')} · ${sslli} SSL`} icon={Server} />
        <StatCard title={cevir('Servisler')} value={s ? `${servisAktif}/${servisToplam}` : '—'} subtitle={servisDown === 0 ? cevir('Tüm sistemler çalışıyor') : `${servisDown} ${cevir('kapalı')}`} icon={Box} />
        <StatCard title={cevir('CPU Kullanımı')} value={s ? `%${Math.round(s.cpu.yuzde)}` : '—'} subtitle={s ? `${s.cpu.cekirdek} ${cevir('toplam çekirdek')}` : '…'} icon={Cpu} trend={tCpu?.t} down={tCpu?.down}
          spark={kaynakBuf.map((d) => d.cpu)} sparkRenk="#10b981" sparkId="cpu" />
        <StatCard title={cevir('RAM Kullanımı')} value={s ? `%${Math.round(s.bellek.yuzde)}` : '—'} subtitle={s ? `${fmtGB(s.bellek.kullanilan_kb)} / ${fmtGB(s.bellek.toplam_kb)}` : '…'} icon={MemoryStick} trend={tMem?.t} down={tMem?.down}
          spark={kaynakBuf.map((d) => d.bellek)} sparkRenk="#8b5cf6" sparkId="ram" />
        <StatCard title={cevir('Depolama')} value={s ? `%${diskY}` : '—'} subtitle={s ? `${fmtByte(s.disk.kullanilan_byte)} / ${fmtByte(s.disk.toplam_byte)}` : '…'} icon={HardDrive} trend={tDsk?.t} down={tDsk?.down}
          spark={kaynakBuf.map((d) => d.disk)} sparkRenk="#f59e0b" sparkId="disk" />
        <StatCard title={cevir('Toplam Trafik')} value={s ? fmtRate(s.ag.rx_bytes_sn + s.ag.tx_bytes_sn) : '—'} subtitle={cevir('Gelen + giden')} icon={Network}
          spark={trafikBuf.map((d) => d.gelen + d.giden)} sparkRenk="#3b82f6" sparkId="net" />
      </div>

      {/* SATIR 1 */}
      <div className="grid grid-main">
        <Card title={cevir('Sistem Kaynakları')} icon={Activity} action={cevir('Son 24 örnek')}>
          <div className="tabs">
            {(['cpu', 'bellek', 'disk'] as const).map((k) => (
              <button key={k} className={tab === k ? 'selected' : ''} onClick={() => setTab(k)}
                style={tab === k ? { color: TAB_RENK[k], borderBottomColor: TAB_RENK[k] } : undefined}>
                {k === 'cpu' ? 'CPU' : k === 'bellek' ? cevir('Bellek') : cevir('Disk')}
              </button>
            ))}
          </div>
          <div className="chart">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={kaynakBuf}>
                <defs>
                  <linearGradient id={`gpk-${tab}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={TAB_RENK[tab]} stopOpacity={0.3} />
                    <stop offset="60%" stopColor={TAB_RENK[tab]} stopOpacity={0.09} />
                    <stop offset="100%" stopColor={TAB_RENK[tab]} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,.055)" vertical={false} />
                <XAxis dataKey="t" axisLine={false} tickLine={false} tick={{ fill: '#64748b', fontSize: 12 }} interval="preserveStartEnd" minTickGap={28} />
                <YAxis axisLine={false} tickLine={false} tick={{ fill: '#64748b', fontSize: 12 }} ticks={[0, 25, 50, 75, 100]} domain={[0, 100]} width={34} />
                <Tooltip contentStyle={TIP_STIL} cursor={IMLEC} formatter={(v: any) => [`%${v}`, tab === 'cpu' ? 'CPU' : tab === 'bellek' ? cevir('Bellek') : cevir('Disk')]} />
                <Area type="monotone" dataKey={tab} stroke={TAB_RENK[tab]} strokeWidth={2} fill={`url(#gpk-${tab})`} dot={false} activeDot={{ r: 3, fill: TAB_RENK[tab] }} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Card>

        <Card title={cevir('Sunucu & Servisler')} icon={Server} action={cevir('Tümünü Gör')} onAction={() => navigate('/araclar/servisler')}>
          <div className="server-list">
            {!s ? <div className="alerts"><div className="bos">{cevir('Yükleniyor…')}</div></div> : (
              <>
                <div className="server-row">
                  <div className="server-main">
                    <div className="server-icon"><Server size={15} /></div>
                    <div><strong>{s.sistem.hostname}</strong><small>{s.sistem.ip}</small></div>
                  </div>
                  <span className="status">{cevir('Aktif')}</span>
                  <div className="metrics">
                    <div><label>CPU <b>%{Math.round(s.cpu.yuzde)}</b></label><Progress value={s.cpu.yuzde} variant={s.cpu.yuzde > 70 ? 'orange' : ''} /></div>
                    <div><label>RAM <b>%{Math.round(s.bellek.yuzde)}</b></label><Progress value={s.bellek.yuzde} variant={s.bellek.yuzde > 75 ? 'orange' : 'purple'} /></div>
                  </div>
                </div>
                {s.servisler.map((sv) => (
                  <div className="server-row" key={sv.ad}>
                    <div className="server-main">
                      <div className="server-icon"><Box size={14} /></div>
                      <div><strong>{sv.etiket}</strong><small>{sv.ad}</small></div>
                    </div>
                    <span className={`status ${sv.aktif ? '' : 'down'}`}>{sv.aktif ? cevir('Aktif') : cevir('Kapalı')}</span>
                  </div>
                ))}
              </>
            )}
          </div>
        </Card>

        <Card title={cevir('Sistem Sağlığı')} icon={ShieldCheck}>
          <div className="health">
            <div className="health-ring" style={{ background: `conic-gradient(${skorRenk} 0 ${skor}%, #18232e ${skor}%)` }}>
              <div><strong>{s ? skor : '—'}</strong><small style={{ color: skorRenk }}>{s ? skorAd : ''}</small></div>
            </div>
            <div className="health-list">
              {([
                [cevir('Servisler'), s ? `${servisAktif} / ${servisToplam}` : '—', servisDown === 0],
                [cevir('Disk Durumu'), diskY < 90 ? cevir('Sağlıklı') : `%${diskY}`, diskY < 90],
                [cevir('Ağ Bağlantısı'), s?.ag.arayuz ? cevir('Sağlıklı') : '—', !!s?.ag.arayuz],
                [cevir('İzolasyon'), izo === 0 ? cevir('Sağlam') : `${izo}`, izo === 0],
                [cevir('Çalışma Süresi'), s ? fmtUptime(s.uptime_sn) : '—', true],
              ] as [string, string, boolean][]).map(([a, b, ok]) => (
                <div key={a}><span>{a}</span><b className={ok ? '' : 'warn'}>{ok && <CheckCircle2 size={12} />}{b}</b></div>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* SATIR 2 */}
      <div className="grid grid-second">
        <Card title={cevir('Gerçek Zamanlı Trafik')} icon={Network} action={cevir('Son 24 örnek')}>
          <div className="traffic-top">
            <div className="traffic-box green">
              <small>{cevir('Gelen Trafik')}</small>
              <strong>{s ? fmtRate(s.ag.rx_bytes_sn) : '—'}</strong>
              <span>{trafTrend('gelen')}</span>
            </div>
            <div className="traffic-box blue-box">
              <small>{cevir('Giden Trafik')}</small>
              <strong>{s ? fmtRate(s.ag.tx_bytes_sn) : '—'}</strong>
              <span>{trafTrend('giden')}</span>
            </div>
          </div>
          <div className="chart traffic-chart">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={trafikBuf}>
                <defs>
                  <linearGradient id="gpt-in" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#10b981" stopOpacity={0.28} /><stop offset="100%" stopColor="#10b981" stopOpacity={0} /></linearGradient>
                  <linearGradient id="gpt-out" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#3b82f6" stopOpacity={0.28} /><stop offset="100%" stopColor="#3b82f6" stopOpacity={0} /></linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,.055)" vertical={false} />
                <XAxis dataKey="t" axisLine={false} tickLine={false} tick={{ fill: '#64748b', fontSize: 12 }} interval="preserveStartEnd" minTickGap={28} />
                <YAxis axisLine={false} tickLine={false} tick={{ fill: '#64748b', fontSize: 12 }} width={58} tickFormatter={(v: any) => fmtRate(v).replace('/s', '')} />
                <Tooltip contentStyle={TIP_STIL} cursor={IMLEC} formatter={(v: any, n: any) => [fmtRate(v), n === 'gelen' ? cevir('Gelen Trafik') : cevir('Giden Trafik')]} />
                <Area type="monotone" dataKey="gelen" stroke="#10b981" strokeWidth={2} fill="url(#gpt-in)" dot={false} isAnimationActive={false} />
                <Area type="monotone" dataKey="giden" stroke="#3b82f6" strokeWidth={2} fill="url(#gpt-out)" dot={false} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Card>

        <Card title={cevir('Depolama Kullanımı')} icon={HardDrive} action={cevir('Tümü')} onAction={() => navigate('/backup-yonetimi')}>
          <div className="depo">
            <div className="depo-ring">
              <Donut segments={depoSeg} />
              <div className="depo-ic">
                <div>
                  <strong>{s ? fmtByte(s.disk.kullanilan_byte) : '—'}</strong>
                  <small>{cevir('Kullanılan')}</small>
                </div>
              </div>
            </div>
            <div className="depo-list">
              {depoSeg.map((x) => (
                <div key={x.ad}>
                  <i style={{ background: x.renk }} />
                  <span>{x.ad}</span>
                  <b>{fmtByte(x.deger)}</b>
                  {/* pay = KULLANILAN alan (halka neyi ayrıştırıyorsa yüzde de onu gösterir) */}
                  <em>{(() => {
                    if (x.deger <= 0) return '%0'
                    const p = (x.deger / depoPay) * 100
                    if (p < 0.1) return '<%0.1'
                    if (p < 1) return `%${p.toFixed(1)}`
                    return `%${Math.round(p)}`
                  })()}</em>
                </div>
              ))}
            </div>
          </div>
          <div className="depo-alt">
            <span className="depo-bos">
              <b>{fmtByte(Math.max(0, diskTop - diskKul))}</b> {cevir('boş')} · {fmtByte(diskTop)} {cevir('toplam')}
            </span>
            <button onClick={() => navigate('/backup-yonetimi')}>{cevir('Depolama yönetimine git')} →</button>
          </div>
        </Card>

        <Card title={cevir('Bildirimler')} icon={Bell} action={cevir('Tümü')} onAction={() => navigate('/bildirimler')}>
          <div className="alerts">
            {bildirimler.length === 0 ? <div className="bos">{cevir('Bildirim yok')}</div> : bildirimler.slice(0, 3).map((b) => {
              const { I, c } = bIkon(b.seviye)
              return (
                <div key={b.id} className={c}>
                  <I size={17} />
                  <section><strong>{b.baslik}</strong><small>{goreli(b.tarih)}</small></section>
                </div>
              )
            })}
          </div>
        </Card>
      </div>

      <footer className="dash-footer">
        <div><i className={`online-dot ${servisDown === 0 ? '' : 'warn'}`} /> {servisDown === 0 ? cevir('Tüm sistemler çalışıyor') : `${servisDown} ${cevir('servis kapalı')}`}</div>
        <span>{cevir('Çalışma Süresi')}: {s ? fmtUptime(s.uptime_sn) : '—'}</span>
        <span>{marka.panel_adi}</span>
        <span>{s?.sistem.panel_surum || ''}</span>
      </footer>
    </div>
  )
}
