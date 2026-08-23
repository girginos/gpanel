import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'

// /system/usage GERÇEK şekli (iç içe) — IzlemePage ile aynı.
type Usage = {
  sistem?: { hostname?: string; ip?: string; os_adi?: string; kernel?: string; mimari?: string; cpu_modeli?: string; cpu_cekirdek?: number; panel_surum?: string }
  cpu?: { yuzde?: number; cekirdek?: number; yuk_1dk?: number; yuk_5dk?: number; yuk_15dk?: number }
  bellek?: { toplam_kb?: number; kullanilan_kb?: number; bos_kb?: number; yuzde?: number }
  swap?: { toplam_kb?: number; kullanilan_kb?: number; yuzde?: number }
  disk?: { toplam_byte?: number; kullanilan_byte?: number; bos_byte?: number; yuzde?: number; mount?: string }
}

type Sayilar = { domain: number; domain_aktif: number }


const ISTAT_EN: Record<string, string> = {
  "Alan adı istatistikleri alınamadı": "Failed to get domain statistics",
  "Panel sürümü": "Panel version",
  "Sunucu adı": "Server name",
  "Sunucu kaynak kullanımı, domain sayıları, sistem özeti.": "Server resource usage, domain counts, system summary.",
  "Yük (1dk)": "Load (1m)",
  "Çekirdek (kernel)": "Kernel",
  "İşlemci": "Processor",
  "İşletim sistemi": "Operating system",
  "● Canlı (10sn)": "● Live (10s)",
  "Anasayfa": "Home",
  "İstatistikler": "Statistics",
  "çekirdek": "cores",
  "5dk: {0} · 15dk: {1}": "5m: {0} · 15m: {1}",
  "Bellek": "Memory",
  "Sistem": "System",
  "Domainler": "Domains",
  "Toplam domain": "Total domains",
  "Aktif domain": "Active domains",
  "Pasif domain": "Inactive domains",
  "Daha detaylı izleme için": "For more detailed monitoring",
  "İzleme": "Monitoring",
  "sayfasını ziyaret edin.": "visit the page.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (ISTAT_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function fmt(b: number) {
  if (!b || b < 0) return '0 B'
  if (b < 1024) return b + ' B'
  if (b < 1024 ** 2) return (b / 1024).toFixed(1) + ' KB'
  if (b < 1024 ** 3) return (b / 1024 / 1024).toFixed(1) + ' MB'
  return (b / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}
// güvenli sayı: undefined/null/NaN → 0
function n(v: number | undefined | null): number {
  return typeof v === 'number' && isFinite(v) ? v : 0
}

export default function IstatistiklerPage() {
  useTranslation() // dil re-render aboneligi
  const [u, setU] = useState<Usage | null>(null)
  const [s, setS] = useState<Sayilar | null>(null)
  const [hata, setHata] = useState<string | null>(null)

  function yukle() {
    api.get<Usage>('/system/usage').then(r => setU(r.data)).catch(e => setHata(apiHata(e)))
    api.get('/domains').then(dr => {
      const domains = (dr.data as any[]) || []
      setS({
        domain: domains.length,
        domain_aktif: domains.filter((d: any) => d.durum === 'aktif').length,
      })
    }).catch(hataYakala(cevir("Alan adı istatistikleri alınamadı")))
  }
  useEffect(() => { yukle(); const t = setInterval(yukle, 10000); return () => clearInterval(t) }, [])

  const cpu = n(u?.cpu?.yuzde)
  const bellek = n(u?.bellek?.yuzde)
  const disk = n(u?.disk?.yuzde)
  const cekirdek = n(u?.cpu?.cekirdek) || n(u?.sistem?.cpu_cekirdek) || 1
  const yuk1 = n(u?.cpu?.yuk_1dk)

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("İstatistikler") },
      ]} />
      <div className="flex items-center justify-between gap-2 flex-wrap mb-1">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("İstatistikler")}</h1>
        <span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">{cevir("● Canlı (10sn)")}</span>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">{cevir("Sunucu kaynak kullanımı, domain sayıları, sistem özeti.")}</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Sistem metrik 4 kart */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
        <Metrik baslik="CPU" deger={u ? cpu.toFixed(1) + '%' : '–'}
          alt={u ? `${cekirdek} ${cevir("çekirdek")}` : ''} renk="indigo" oran={cpu} />
        <Metrik baslik={cevir(cevir("Bellek"))} deger={u ? bellek.toFixed(1) + '%' : '–'}
          alt={u ? `${fmt(n(u?.bellek?.kullanilan_kb) * 1024)} / ${fmt(n(u?.bellek?.toplam_kb) * 1024)}` : ''}
          renk="emerald" oran={bellek} />
        <Metrik baslik="Disk" deger={u ? disk.toFixed(1) + '%' : '–'}
          alt={u ? `${fmt(n(u?.disk?.kullanilan_byte))} / ${fmt(n(u?.disk?.toplam_byte))}` : ''}
          renk="violet" oran={disk} />
        <Metrik baslik={cevir("Yük (1dk)")} deger={u ? yuk1.toFixed(2) : '–'}
          alt={u ? cevirT(cevir("5dk: {0} · 15dk: {1}"), n(u?.cpu?.yuk_5dk).toFixed(2), n(u?.cpu?.yuk_15dk).toFixed(2)) : ''}
          renk="amber" oran={Math.min(100, (yuk1 / cekirdek) * 100)} />
      </div>

      {/* Sistem özeti + sayaçlar */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-5">
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Sistem")}</h3>
          <div className="space-y-1.5 text-sm">
            <Satir e={cevir("Sunucu adı")} d={u?.sistem?.hostname || '–'} />
            <Satir e={cevir("İşletim sistemi")} d={u?.sistem?.os_adi || '–'} />
            <Satir e={cevir("Çekirdek (kernel)")} d={u?.sistem?.kernel || '–'} />
            <Satir e={cevir("İşlemci")} d={u?.sistem?.cpu_modeli ? `${u.sistem.cpu_modeli} · ${cekirdek} ${cevir("çekirdek")}` : '–'} />
            <Satir e="Swap" d={u?.swap ? `${n(u.swap.yuzde).toFixed(1)}% · ${fmt(n(u.swap.kullanilan_kb) * 1024)} / ${fmt(n(u.swap.toplam_kb) * 1024)}` : '–'} />
            <Satir e={cevir("Panel sürümü")} d={u?.sistem?.panel_surum || '–'} />
          </div>
        </div>
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Domainler")}</h3>
          <div className="space-y-1.5 text-sm">
            <Satir e={cevir(cevir("Toplam domain"))} d={s ? String(s.domain) : '–'} />
            <Satir e={cevir(cevir("Aktif domain"))} d={
              <span className="text-emerald-700 dark:text-emerald-300 font-semibold">{s ? s.domain_aktif : 0}</span>
            } />
            <Satir e={cevir(cevir("Pasif domain"))} d={String(s ? s.domain - s.domain_aktif : 0)} />
          </div>
        </div>
      </div>

      <div className="text-xs text-slate-400 dark:text-slate-500 text-center mt-6">
        {cevir(cevir("Daha detaylı izleme için"))} <a href="/izleme" className="text-brand-600 dark:text-brand-400 hover:underline">{cevir("İzleme")}</a> {cevir("sayfasını ziyaret edin.")}
      </div>
    </div>
  )
}

function Metrik({ baslik, deger, alt, renk, oran }: { baslik: string; deger: string; alt: string; renk: string; oran: number }) {
  const renkMap: Record<string, string> = {
    indigo: 'bg-indigo-500', emerald: 'bg-emerald-500',
    violet: 'bg-violet-500', amber: 'bg-amber-500',
  }
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
      <div className="text-xs text-slate-500 dark:text-slate-500 uppercase tracking-wider">{baslik}</div>
      <div className="text-2xl font-bold text-slate-900 dark:text-slate-100 mt-1">{deger}</div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-0.5 truncate">{alt}</div>
      <div className="mt-2 h-1.5 bg-slate-100 dark:bg-slate-700 rounded overflow-hidden">
        <div className={`h-full ${renkMap[renk]} transition-all`} style={{ width: Math.min(100, Math.max(0, oran)) + '%' }} />
      </div>
    </div>
  )
}

function Satir({ e, d }: { e: string; d: any }) {
  return (
    <div className="flex items-center justify-between gap-3 py-1 border-b border-slate-50 dark:border-slate-700/40 last:border-0">
      <span className="text-xs text-slate-500 dark:text-slate-500 shrink-0">{e}</span>
      <span className="text-xs font-mono text-slate-800 dark:text-slate-200 text-right truncate">{d}</span>
    </div>
  )
}
