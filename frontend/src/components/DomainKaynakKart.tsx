// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

type Limit = { kullanim: number; limit: number }
export type Ozet = {
  alan_adi: string; sk: string; plan_adi: string; php_surum: string
  ipv4: string; ssl_aktif: boolean; ssl_bitis?: string
  disk_mb: Limit; trafik_mb: Limit
  db_sayisi: Limit; ftp_sayisi: Limit; eposta_sayi: Limit; domain_sayi: Limit
  dns_kayit: number; cron_is: number
  yedek_sayisi: number; yedek_mb: number
}

export default function DomainKaynakKart({ domainId }: { domainId: number | string }) {
  const [ozet, setOzet] = useState<Ozet | null>(null)
  const [yuk, setYuk] = useState(true)

  function yukle() {
    setYuk(true)
    api.get<Ozet>(`/domains/${domainId}/kaynak`)
      .then(r => setOzet(r.data))
      .catch(() => setOzet(null))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [domainId])

  if (yuk) {
    return (
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="h-5 bg-slate-100 dark:bg-slate-800 rounded w-32 mb-3 animate-pulse" />
        <div className="space-y-3">
          {[1, 2, 3, 4].map(i => (
            <div key={i} className="h-3 bg-slate-100 dark:bg-slate-800 rounded animate-pulse" />
          ))}
        </div>
      </div>
    )
  }
  if (!ozet) return null

  return (
    <div className="space-y-3">
      {/* Plan + Özet */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Paket ve Kaynaklar</h3>
          <button onClick={yukle} className="text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300" title="Yenile">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </button>
        </div>

        <div className="mb-3 pb-3 border-b border-slate-100 dark:border-slate-800">
          <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-0.5">Hizmet Planı</div>
          <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{ozet.plan_adi}</div>
        </div>

        <Bar etiket="Disk" k={ozet.disk_mb.kullanim} l={ozet.disk_mb.limit} birim="MB" renk="indigo" />
        <Bar etiket="Trafik (aylık)" k={ozet.trafik_mb.kullanim} l={ozet.trafik_mb.limit} birim="MB" renk="sky" />
        <Bar etiket="Veritabanı" k={ozet.db_sayisi.kullanim} l={ozet.db_sayisi.limit} birim="DB" renk="emerald" />
        <Bar etiket="FTP Hesabı" k={ozet.ftp_sayisi.kullanim} l={ozet.ftp_sayisi.limit} birim="hesap" renk="amber" />
        <Bar etiket="E-posta Kutusu" k={ozet.eposta_sayi.kullanim} l={ozet.eposta_sayi.limit} birim="kutu" renk="rose" />
        <Bar etiket="Subdomain" k={ozet.domain_sayi.kullanim} l={ozet.domain_sayi.limit} birim="domain" renk="violet" />
      </div>

      {/* Yapılandırma Özeti */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Yapılandırma</h3>
        <Sat e="IP Adresi" d={ozet.ipv4 || '—'} mono />
        <Sat e="Sistem Kullanıcısı" d={ozet.sk} mono />
        <Sat e="PHP Sürümü"
          d={<span><span className="font-mono font-medium text-slate-800 dark:text-slate-200">PHP {ozet.php_surum}</span></span>}
        />
        <Sat e="SSL/TLS"
          d={
            ozet.ssl_aktif
              ? <span className="flex items-center gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                  <span className="text-emerald-700 dark:text-emerald-300 text-xs font-medium">Aktif</span>
                  {ozet.ssl_bitis && <span className="text-slate-400 dark:text-slate-500 text-[10px]">→ {ozet.ssl_bitis}</span>}
                </span>
              : <span className="flex items-center gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-full bg-slate-300" />
                  <span className="text-slate-500 dark:text-slate-500 text-xs">Yok</span>
                </span>
          }
        />
      </div>

      {/* İlave Sayaclar */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Sayaçlar</h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-y-2 gap-x-3">
          <Mini etiket="DNS kayıt" deger={ozet.dns_kayit} />
          <Mini etiket="Cron işi" deger={ozet.cron_is} />
          <Mini etiket="Yedek" deger={ozet.yedek_sayisi} />
          <Mini etiket="Yedek boyutu" deger={`${ozet.yedek_mb} MB`} />
        </div>
      </div>
    </div>
  )
}

// ----- helpers -----
// Canlı gradient bar'lar. Sınırsız (limit=0) durumda soluk 8% stub yerine
// sağa doğru sönümlenen tam-genişlik gradient → "sınırsız/akan" hissi (canlı).
function Bar({ etiket, k, l, birim, renk }: { etiket: string; k: number; l: number; birim: string; renk: string }) {
  const sinirsiz = l === 0
  const oran = sinirsiz ? 0 : Math.min(100, Math.round((k / l) * 100))
  const grad: Record<string, string> = {
    indigo:  'from-indigo-400 to-indigo-600',
    sky:     'from-sky-400 to-sky-600',
    emerald: 'from-emerald-400 to-emerald-600',
    amber:   'from-amber-400 to-amber-600',
    rose:    'from-rose-400 to-rose-600',
    violet:  'from-violet-400 to-violet-600',
  }
  const fill = oran >= 90 ? 'from-red-400 to-red-600' : (oran >= 75 ? 'from-amber-400 to-amber-600' : (grad[renk] || 'from-slate-400 to-slate-600'))
  const fade = 'linear-gradient(to right, black 0%, black 35%, transparent 96%)'
  return (
    <div className="mb-3 last:mb-0">
      <div className="flex items-baseline justify-between mb-1">
        <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{etiket}</span>
        <span className="text-[11px] font-mono text-slate-500 dark:text-slate-400">
          {sinirsiz
            ? <><span className="text-slate-700 dark:text-slate-200 font-semibold">{fmt(k)}</span> {birim} · <span className="text-emerald-500 font-bold">∞</span></>
            : <><span className="text-slate-700 dark:text-slate-200 font-semibold">{fmt(k)}</span> / {fmt(l)} {birim}</>
          }
        </span>
      </div>
      <div className="h-2 rounded-full bg-slate-100 dark:bg-slate-700/50 overflow-hidden">
        {sinirsiz ? (
          <div
            className={`h-full rounded-full bg-gradient-to-r ${grad[renk] || 'from-slate-400 to-slate-600'}`}
            style={{ width: '100%', maskImage: fade, WebkitMaskImage: fade }}
          />
        ) : (
          <div className={`h-full rounded-full bg-gradient-to-r ${fill}`} style={{ width: Math.max(oran, 3) + '%' }} />
        )}
      </div>
    </div>
  )
}
function fmt(n: number) {
  if (n >= 1024) return (n / 1024).toFixed(1) + 'k'
  return String(n)
}
function Sat({ e, d, mono }: { e: string; d: any; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between py-1.5 border-b border-slate-50 dark:border-slate-800 last:border-0">
      <span className="text-xs text-slate-500 dark:text-slate-500">{e}</span>
      <span className={`text-xs text-slate-700 dark:text-slate-300 text-right ${mono ? 'font-mono' : ''} max-w-[60%] truncate`} title={typeof d === 'string' ? d : undefined}>{d}</span>
    </div>
  )
}
function Mini({ etiket, deger }: { etiket: string; deger: number | string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500">{etiket}</div>
      <div className="text-sm font-mono font-medium text-slate-800 dark:text-slate-200">{deger}</div>
    </div>
  )
}
