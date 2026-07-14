// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

type Usage = {
  cpu: { yuzde: number; cekirdek: number; yuk_1dk: number; yuk_5dk: number; yuk_15dk: number }
  bellek: { toplam_kb: number; kullanilan_kb: number; bos_kb: number; yuzde: number }
  disk: { toplam_byte: number; kullanilan_byte: number; bos_byte: number; yuzde: number; mount: string }
  uptime_sn: number
}

type Saglik = { durum: string; surum: string; zaman: string }

export default function ResourceCard() {
  const [u, setU] = useState<Usage | null>(null)
  const [s, setS] = useState<Saglik | null>(null)

  useEffect(() => {
    let on = true
    async function tick() {
      try {
        const r = await api.get<Usage>('/system/usage')
        if (on) setU(r.data)
      } catch { /* yoksay */ }
      try {
        const r = await fetch('/healthz', { cache: 'no-store' })
        if (r.ok && on) setS(await r.json())
      } catch { /* yoksay */ }
    }
    tick()
    const t = setInterval(tick, 4000)
    return () => { on = false; clearInterval(t) }
  }, [])

  return (
    <div className="space-y-4">
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4 flex items-center justify-between">
          Kaynak Kullanımı
          {u && <span className="text-[10px] font-normal text-slate-400 dark:text-slate-500 uppercase tracking-wider">canlı</span>}
        </h3>

        {!u ? (
          <div className="text-sm text-slate-400 dark:text-slate-500 py-6 text-center">Yükleniyor…</div>
        ) : (
          <>
            <Cubuk
              etiket="CPU"
              yuzde={u.cpu.yuzde}
              alt={`${u.cpu.cekirdek} çekirdek · yük ${u.cpu.yuk_1dk.toFixed(2)} / ${u.cpu.yuk_5dk.toFixed(2)} / ${u.cpu.yuk_15dk.toFixed(2)}`}
              renk="brand"
            />
            <Cubuk
              etiket="Bellek"
              yuzde={u.bellek.yuzde}
              alt={`${(u.bellek.kullanilan_kb / 1024).toFixed(0)} MB / ${(u.bellek.toplam_kb / 1024).toFixed(0)} MB`}
              renk="emerald"
            />
            <Cubuk
              etiket="Disk"
              yuzde={u.disk.yuzde}
              alt={`${(u.disk.kullanilan_byte / 1e9).toFixed(1)} GB / ${(u.disk.toplam_byte / 1e9).toFixed(1)} GB`}
              renk="violet"
            />
            <div className="mt-4 pt-3 border-t border-slate-100 dark:border-slate-800 text-xs text-slate-500 dark:text-slate-500 flex justify-between">
              <span>Çalışma süresi</span>
              <span className="font-mono text-slate-700 dark:text-slate-300">{formatUptime(u.uptime_sn)}</span>
            </div>
          </>
        )}
      </div>

      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Sistem Durumu</h3>
        {!s ? (
          <div className="text-sm text-slate-400 dark:text-slate-500">Bekleniyor…</div>
        ) : (
          <div className="space-y-2 text-sm">
            <Satir
              etiket="Backend"
              deger={s.durum === 'ayakta' ? 'Çalışıyor' : s.durum}
              ok={s.durum === 'ayakta'}
            />
            <Satir etiket="Sürüm" deger={s.surum} ok />
            <Satir etiket="Saat" deger={new Date(s.zaman).toLocaleTimeString('tr-TR')} ok />
          </div>
        )}
      </div>
    </div>
  )
}

function Cubuk({ etiket, yuzde, alt, renk }: { etiket: string; yuzde: number; alt: string; renk: string }) {
  const bg: Record<string, string> = {
    brand:   'bg-brand-500',
    emerald: 'bg-emerald-500',
    violet:  'bg-violet-500',
  }
  const teh = yuzde >= 85 ? 'bg-red-500' : yuzde >= 70 ? 'bg-amber-500' : bg[renk]
  return (
    <div className="mb-3 last:mb-0">
      <div className="flex justify-between text-sm">
        <span className="text-slate-700 dark:text-slate-300 font-medium">{etiket}</span>
        <span className="font-mono text-slate-900 dark:text-slate-100">%{yuzde.toFixed(1)}</span>
      </div>
      <div className="h-2 bg-slate-100 dark:bg-slate-800 rounded-full overflow-hidden my-1">
        <div className={`h-full transition-all duration-500 ${teh}`} style={{ width: `${Math.min(100, Math.max(2, yuzde))}%` }}></div>
      </div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 font-mono">{alt}</div>
    </div>
  )
}

function Satir({ etiket, deger, ok }: { etiket: string; deger: string; ok: boolean }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-slate-600 dark:text-slate-400 dark:text-slate-500">{etiket}</span>
      <span className={`font-medium flex items-center gap-1.5 ${ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}`}>
        <span className={`w-1.5 h-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-red-500'}`}></span>
        {deger}
      </span>
    </div>
  )
}

function formatUptime(sn: number): string {
  const gun = Math.floor(sn / 86400)
  const saat = Math.floor((sn % 86400) / 3600)
  const dk = Math.floor((sn % 3600) / 60)
  if (gun > 0) return `${gun}g ${saat}sa`
  if (saat > 0) return `${saat}sa ${dk}dk`
  return `${dk}dk`
}