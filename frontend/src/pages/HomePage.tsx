import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { useAuth } from '@/store/auth'
import LoadHistoryChart from '@/components/LoadHistoryChart'

type SistemInfo = {
  hostname: string; ip: string; os_adi: string; kernel: string
  mimari: string; cpu_modeli: string; cpu_cekirdek: number; panel_surum: string
}
type CPU = { yuzde: number; cekirdek: number; yuk_1dk: number; yuk_5dk: number; yuk_15dk: number }
type Bellek = { toplam_kb: number; kullanilan_kb: number; bos_kb: number; yuzde: number }
type Swap = { toplam_kb: number; kullanilan_kb: number; yuzde: number }
type Disk = { toplam_byte: number; kullanilan_byte: number; bos_byte: number; yuzde: number; mount: string; fs?: string }
type Ag = { arayuz: string; rx_bytes_sn: number; tx_bytes_sn: number; rx_toplam_byte: number; tx_toplam_byte: number }
type Servis = { ad: string; etiket: string; aktif: boolean }
type Sistem = {
  sistem: SistemInfo; cpu: CPU; bellek: Bellek; swap: Swap
  disk: Disk; diskler: Disk[]; ag: Ag; servisler: Servis[]; uptime_sn: number
}
type Domain = { id: number; alan_adi: string; ssl: boolean; durum: string }

export default function HomePage() {
  const kullanici = useAuth((s) => s.kullanici)
  const [s, setS] = useState<Sistem | null>(null)
  const [domainler, setDomainler] = useState<Domain[]>([])

  useEffect(() => {
    const cek = () => api.get<Sistem>('/system/usage').then((r) => setS(r.data)).catch(() => {})
    cek()
    api.get<Domain[]>('/domains').then((r) => setDomainler(r.data)).catch(() => {})
    const id = setInterval(cek, 5000)
    return () => clearInterval(id)
  }, [])

  const aktif = domainler.filter((d) => d.durum === 'aktif').length
  const sslli = domainler.filter((d) => d.ssl).length
  const diskList = s ? (s.diskler?.length ? s.diskler : [s.disk]) : []
  const anaDisk = s ? (diskList[0] || s.disk) : null

  return (
    <div className="px-6 py-5 max-w-[1400px] mx-auto">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100 leading-tight">Sistem Panosu</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Hoş geldiniz, <span className="text-slate-700 dark:text-slate-300 font-medium">{kullanici?.ad_soyad || kullanici?.adi}</span>
          </p>
        </div>
        <div className="flex items-center gap-1.5 text-[11px] text-slate-400">
          <span className={`w-1.5 h-1.5 rounded-full ${s ? 'bg-emerald-500 animate-pulse' : 'bg-slate-300'}`} />
          Canlı
        </div>
      </div>

      {/* KPI — kompakt ring gauge'lar */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-3">
        <KpiRing etiket="CPU" yuzde={s?.cpu.yuzde ?? 0} alt={s ? `${s.cpu.cekirdek} çekirdek` : '…'} renk="brand" hazir={!!s} />
        <KpiRing etiket="Bellek" yuzde={s?.bellek.yuzde ?? 0} alt={s ? `${fmtGB(s.bellek.kullanilan_kb)} / ${fmtGB(s.bellek.toplam_kb)}` : '…'} renk="emerald" hazir={!!s} />
        <KpiRing etiket="Disk" yuzde={anaDisk?.yuzde ?? 0} alt={anaDisk ? `${fmtByteGB(anaDisk.kullanilan_byte)} / ${fmtByteGB(anaDisk.toplam_byte)}` : '…'} renk="violet" hazir={!!s} />
        <YukKart cpu={s?.cpu} />
      </div>

      {/* Sistem bilgi şeridi — satır-içi çipler */}
      <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl px-4 py-2.5 mb-3">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5">
          <Bilgi etiket="Sunucu" val={s?.sistem.hostname} mono />
          <Bilgi etiket="IP" val={s?.sistem.ip} mono />
          <Bilgi etiket="OS" val={s?.sistem.os_adi} />
          <Bilgi etiket="Çekirdek" val={s?.sistem.kernel} mono kisa />
          <Bilgi etiket="Çalışma" val={s ? formatUptime(s.uptime_sn) : undefined} />
          {s && s.swap.toplam_kb > 0 && <Bilgi etiket="Swap" val={`%${s.swap.yuzde.toFixed(0)}`} />}
          <Bilgi etiket="Sürüm" val={s?.sistem.panel_surum} mono />
        </div>
      </div>

      {/* Yük geçmişi grafiği */}
      <div className="mb-3">
        <LoadHistoryChart />
      </div>

      {/* 3'lü grid: Disk · Ağ · Domainler */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 mb-3">
        <Kart baslik="Disk Kullanımı">
          {!s ? <Yukleniyor /> : (
            <div className="space-y-2.5">
              {diskList.map((d, i) => <DiskSatir key={i} d={d} />)}
            </div>
          )}
        </Kart>

        <Kart baslik="Ağ Trafiği">
          {!s ? <Yukleniyor /> : !s.ag.arayuz ? (
            <div className="text-xs text-slate-400 dark:text-slate-500 py-2 italic">Arayüz bulunamadı</div>
          ) : (
            <>
              <div className="text-[11px] text-slate-400 dark:text-slate-500 font-mono mb-2">{s.ag.arayuz}</div>
              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-lg bg-emerald-50 dark:bg-emerald-900/15 border border-emerald-100 dark:border-emerald-800/40 p-2.5">
                  <div className="text-[10px] uppercase tracking-wide text-emerald-600 dark:text-emerald-400 font-semibold">↓ İndirme</div>
                  <div className="text-base font-bold font-mono text-emerald-700 dark:text-emerald-300 mt-0.5">{fmtRate(s.ag.rx_bytes_sn)}</div>
                  <div className="text-[10px] text-slate-400 dark:text-slate-500 mt-0.5">Σ {fmtByteGB(s.ag.rx_toplam_byte)}</div>
                </div>
                <div className="rounded-lg bg-sky-50 dark:bg-sky-900/15 border border-sky-100 dark:border-sky-800/40 p-2.5">
                  <div className="text-[10px] uppercase tracking-wide text-sky-600 dark:text-sky-400 font-semibold">↑ Yükleme</div>
                  <div className="text-base font-bold font-mono text-sky-700 dark:text-sky-300 mt-0.5">{fmtRate(s.ag.tx_bytes_sn)}</div>
                  <div className="text-[10px] text-slate-400 dark:text-slate-500 mt-0.5">Σ {fmtByteGB(s.ag.tx_toplam_byte)}</div>
                </div>
              </div>
            </>
          )}
        </Kart>

        <Kart baslik="Domainler">
          <div className="grid grid-cols-3 gap-2 mb-3">
            <MiniStat deger={domainler.length} etiket="Toplam" renk="slate" />
            <MiniStat deger={aktif} etiket="Aktif" renk="emerald" />
            <MiniStat deger={sslli} etiket="SSL" renk="violet" />
          </div>
          <Link to="/domainler" className="block text-xs text-brand-600 dark:text-brand-400 hover:underline font-medium">
            Tüm domainleri yönet →
          </Link>
          <div className="mt-3 pt-3 border-t border-slate-100 dark:border-slate-700/60 grid grid-cols-2 gap-2">
            <KisaLink to="/firewall" etiket="Güvenlik Duvarı" />
            <KisaLink to="/izleme" etiket="İzleme" />
            <KisaLink to="/araclar-ayarlar" etiket="Ayarlar" />
          </div>
        </Kart>
      </div>

      {/* Servisler — kompakt çipler */}
      <Kart baslik="Servisler" className="mb-3">
        {!s ? <Yukleniyor /> : (
          <div className="flex flex-wrap gap-1.5">
            {s.servisler.map((sv) => (
              <span key={sv.ad} title={sv.ad}
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs border ${
                  sv.aktif
                    ? 'border-emerald-200 dark:border-emerald-800/60 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300'
                    : 'border-red-200 dark:border-red-800/60 bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'
                }`}>
                <span className={`w-1.5 h-1.5 rounded-full ${sv.aktif ? 'bg-emerald-500' : 'bg-red-500'}`} />
                {sv.etiket}
              </span>
            ))}
          </div>
        )}
      </Kart>

      {/* Son domainler — kompakt tablo */}
      <Kart baslik="Son Domainler">
        {domainler.length === 0 ? (
          <div className="py-5 text-center text-xs text-slate-400">Henüz domain yok</div>
        ) : (
          <div className="overflow-x-auto -mx-1">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-[11px] uppercase tracking-wide text-slate-400 border-b border-slate-100 dark:border-slate-700/60">
                  <th className="text-left font-medium py-1.5 px-1">Domain</th>
                  <th className="text-left font-medium py-1.5 px-1">Durum</th>
                  <th className="text-left font-medium py-1.5 px-1">SSL</th>
                  <th className="text-right font-medium py-1.5 px-1"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-50 dark:divide-slate-800">
                {domainler.slice(0, 6).map((d) => (
                  <tr key={d.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <td className="py-2 px-1">
                      <Link to={`/abonelikler/${d.id}`} className="text-brand-600 dark:text-brand-400 hover:underline font-medium">{d.alan_adi}</Link>
                    </td>
                    <td className="px-1">
                      <span className={`text-[10px] px-1.5 py-0.5 rounded uppercase font-semibold tracking-wide ${
                        d.durum === 'aktif' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-400'
                      }`}>{d.durum}</span>
                    </td>
                    <td className="px-1 text-xs">
                      {d.ssl ? <span className="text-emerald-600 dark:text-emerald-400">● Korumalı</span> : <span className="text-amber-600 dark:text-amber-400">○ Yok</span>}
                    </td>
                    <td className="px-1 text-right">
                      <Link to={`/abonelikler/${d.id}`} className="text-xs text-slate-400 hover:text-brand-600 dark:hover:text-brand-400">Pano →</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Kart>
    </div>
  )
}

/* ---------- bileşenler ---------- */

const RENK: Record<string, string> = { brand: '#f97316', emerald: '#10b981', violet: '#8b5cf6', sky: '#0ea5e9' }
function esikRenk(y: number, taban: string) {
  if (y >= 85) return '#ef4444'
  if (y >= 70) return '#f59e0b'
  return RENK[taban] || '#64748b'
}

function KpiRing({ etiket, yuzde, alt, renk, hazir }: { etiket: string; yuzde: number; alt: string; renk: string; hazir: boolean }) {
  const r = 26, c = 2 * Math.PI * r
  const val = Math.min(100, Math.max(0, yuzde))
  const off = c * (1 - val / 100)
  const stroke = esikRenk(yuzde, renk)
  return (
    <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-3.5 flex items-center gap-3">
      <div className="relative shrink-0 w-16 h-16">
        <svg viewBox="0 0 64 64" className="w-16 h-16 -rotate-90">
          <circle cx="32" cy="32" r={r} fill="none" className="stroke-slate-100 dark:stroke-slate-700" strokeWidth="5" />
          <circle cx="32" cy="32" r={r} fill="none" stroke={stroke} strokeWidth="5" strokeLinecap="round"
            strokeDasharray={c} strokeDashoffset={hazir ? off : c} className="transition-all duration-700" />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-sm font-bold text-slate-800 dark:text-slate-100">{hazir ? `%${yuzde.toFixed(0)}` : '—'}</span>
        </div>
      </div>
      <div className="min-w-0">
        <div className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 font-semibold">{etiket}</div>
        <div className="text-xs text-slate-500 dark:text-slate-400 truncate mt-0.5" title={alt}>{alt}</div>
      </div>
    </div>
  )
}

function YukKart({ cpu }: { cpu?: CPU }) {
  const cek = cpu?.cekirdek || 1
  const renk = (y: number) => y >= cek ? 'text-red-500' : y >= cek * 0.7 ? 'text-amber-500' : 'text-slate-800 dark:text-slate-100'
  return (
    <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-3.5">
      <div className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 font-semibold mb-1.5">Yük Ortalaması</div>
      {!cpu ? (
        <div className="text-slate-300 dark:text-slate-600 text-xl font-mono">—</div>
      ) : (
        <>
          <div className="flex items-baseline gap-2">
            <span className={`text-2xl font-bold font-mono ${renk(cpu.yuk_1dk)}`}>{cpu.yuk_1dk.toFixed(2)}</span>
            <span className="text-[11px] text-slate-400">1dk</span>
          </div>
          <div className="text-xs text-slate-500 dark:text-slate-400 font-mono mt-1">
            {cpu.yuk_5dk.toFixed(2)} · {cpu.yuk_15dk.toFixed(2)} <span className="text-slate-400">/ {cek} çek.</span>
          </div>
        </>
      )}
    </div>
  )
}

function Bilgi({ etiket, val, mono, kisa }: { etiket: string; val?: string; mono?: boolean; kisa?: boolean }) {
  return (
    <div className="flex items-baseline gap-1.5 min-w-0">
      <span className="text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 font-semibold shrink-0">{etiket}</span>
      <span className={`text-xs text-slate-700 dark:text-slate-200 truncate ${mono ? 'font-mono' : 'font-medium'} ${kisa ? 'max-w-[160px]' : 'max-w-[220px]'}`} title={val || ''}>
        {val || '—'}
      </span>
    </div>
  )
}

function Kart({ baslik, children, className }: { baslik: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={`bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-4 ${className || ''}`}>
      <h3 className="text-[11px] uppercase tracking-wide font-semibold text-slate-400 dark:text-slate-500 mb-3">{baslik}</h3>
      {children}
    </div>
  )
}

function MiniStat({ deger, etiket, renk }: { deger: number; etiket: string; renk: string }) {
  const r: Record<string, string> = { slate: 'text-slate-700 dark:text-slate-200', emerald: 'text-emerald-600 dark:text-emerald-400', violet: 'text-violet-600 dark:text-violet-400' }
  return (
    <div className="text-center py-2 rounded-lg bg-slate-50 dark:bg-slate-900/50">
      <div className={`text-xl font-bold ${r[renk]}`}>{deger}</div>
      <div className="text-[10px] uppercase tracking-wide text-slate-400 mt-0.5">{etiket}</div>
    </div>
  )
}

function KisaLink({ to, etiket }: { to: string; etiket: string }) {
  return (
    <Link to={to} className="px-2.5 py-1.5 text-xs text-center rounded-md bg-slate-50 dark:bg-slate-900/50 text-slate-600 dark:text-slate-300 hover:bg-brand-50 dark:hover:bg-brand-900/20 hover:text-brand-700 dark:hover:text-brand-300 transition font-medium">
      {etiket}
    </Link>
  )
}

function DiskSatir({ d }: { d: Disk }) {
  const teh = d.yuzde >= 85 ? 'bg-red-500' : d.yuzde >= 70 ? 'bg-amber-500' : 'bg-sky-500'
  return (
    <div>
      <div className="flex justify-between items-baseline text-xs mb-1">
        <span className="font-mono font-medium text-slate-700 dark:text-slate-200 truncate">{d.mount}</span>
        <span className="text-slate-400 shrink-0 ml-2">
          {fmtByteGB(d.kullanilan_byte)} / {fmtByteGB(d.toplam_byte)}
          <span className="ml-2 font-mono font-semibold text-slate-600 dark:text-slate-300">%{d.yuzde.toFixed(1)}</span>
        </span>
      </div>
      <div className="h-1.5 bg-slate-100 dark:bg-slate-700/60 rounded-full overflow-hidden">
        <div className={`h-full transition-all duration-700 ${teh}`} style={{ width: `${Math.min(100, Math.max(1, d.yuzde))}%` }} />
      </div>
    </div>
  )
}

function Yukleniyor() { return <div className="py-4 text-center text-xs text-slate-400">Yükleniyor…</div> }

function formatUptime(sn: number): string {
  const g = Math.floor(sn / 86400), sa = Math.floor((sn % 86400) / 3600), dk = Math.floor((sn % 3600) / 60)
  if (g > 0) return `${g}g ${sa}sa`
  if (sa > 0) return `${sa}sa ${dk}dk`
  return `${dk}dk`
}
function fmtGB(kb: number): string {
  const mb = kb / 1024
  return mb < 1024 ? `${mb.toFixed(0)} MB` : `${(mb / 1024).toFixed(1)} GB`
}
function fmtByteGB(b: number): string {
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(0)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(1)} GB`
}
function fmtRate(bps: number): string {
  if (bps < 1024) return `${bps.toFixed(0)} B/s`
  if (bps < 1024 * 1024) return `${(bps / 1024).toFixed(0)} KB/s`
  return `${(bps / 1024 / 1024).toFixed(1)} MB/s`
}
