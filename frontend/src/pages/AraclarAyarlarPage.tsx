// gosp-dark-swept
// gosp-dark-swept-v2
import { Link } from 'react-router-dom'
import Breadcrumb from '@/components/Breadcrumb'
import PanelGuncelleme from '@/components/PanelGuncelleme'
import SunucuOptimize from '@/components/SunucuOptimize'

type Arac = {
  baslik: string
  aciklama: string
  href: string
  ikon: string
  renk: string
  hazir: boolean
  rozet?: string
}

const GRUPLAR: { ad: string; aciklar: Arac[] }[] = [
  {
    ad: 'PHP Yönetimi',
    aciklar: [
      { baslik: 'PHP Sürümleri',  aciklama: '5.6 → 8.5 sürümlerini sunucuya ekleyin / kaldırın. Her domain bağımsız seçer.',
        href: '/araclar/php-surumler', ikon: '🐘', renk: 'indigo', hazir: true, rozet: 'Dinamik' },
      { baslik: 'PHP Modülleri',  aciklama: 'Sunucu genelinde extension toggle. PECL paket arama + derleme.',
        href: '/sistem/php-modulleri', ikon: '🧩', renk: 'violet', hazir: true },
    ],
  },
  {
    ad: 'Sunucu',
    aciklar: [
      { baslik: 'Paket Yöneticisi · Derleyiciler',
        aciklama: 'DNF üzerinden gcc/python/node/go/podman gibi sistem paketleri. Hızlı kurulum grupları.',
        href: '/araclar/paketler', ikon: '📦', renk: 'orange', hazir: true },
      { baslik: 'Hizmet Planları', aciklama: 'Barındırma paketleri, disk + DB + FTP kotaları.',
        href: '/hizmet-planlari', ikon: '📋', renk: 'sky', hazir: true },
      { baslik: 'Servisler', aciklama: 'Nginx / Apache / MariaDB / DNS / PHP-FPM servislerini yeniden başlat.',
        href: '/araclar/servisler', ikon: '🔁', renk: 'rose', hazir: true },
      { baslik: 'DNS Şablonu', aciklama: 'Yeni domainlere uygulanan merkezi DNS kayıtları (A/MX/SPF/DMARC/DKIM) + SOA. Düzenlenebilir.',
        href: '/araclar/dns-sablonu', ikon: '🌍', renk: 'emerald', hazir: true, rozet: 'Merkezi' },
    ],
  },
  {
    ad: 'Barındırma',
    aciklar: [
      { baslik: 'Domainler',   aciklama: 'Tüm domain listesi, arama, hızlı erişim.',
        href: '/domainler', ikon: '🌐', renk: 'teal', hazir: true },
    ],
  },
  {
    ad: 'Güvenlik ve Yedekleme',
    aciklar: [
      { baslik: 'Güvenlik Duvarı', aciklama: 'IP/port yasağı, beyaz liste, port kapatma (nftables). Kritik portlar korumalı.',
        href: '/firewall', ikon: '🛡️', renk: 'rose', hazir: true },
      { baslik: 'Backup Yöneticisi', aciklama: 'Tüm domainlerin yedekleri + boyut, tek-tık yedekle. S3/SFTP hedefler.',
        href: '/backup-yonetimi', ikon: '💾', renk: 'sky', hazir: true },
      { baslik: 'İzleme ve Loglar', aciklama: 'CPU/RAM/disk grafikleri + sunucu günlükleri (journald: panel/nginx/SSH…).',
        href: '/izleme', ikon: '📊', renk: 'emerald', hazir: true },
    ],
  },
]

const RENK_MAP: Record<string, { bg: string; icon: string; rozet: string }> = {
  indigo:  { bg: 'bg-indigo-50 dark:bg-indigo-900/15 hover:bg-indigo-100 dark:hover:bg-indigo-900/25 border-indigo-200 dark:border-indigo-800/50', icon: 'bg-indigo-100 dark:bg-indigo-900/40', rozet: 'bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300' },
  violet:  { bg: 'bg-violet-50 dark:bg-violet-900/15 hover:bg-violet-100 dark:hover:bg-violet-900/25 border-violet-200 dark:border-violet-800/50', icon: 'bg-violet-100 dark:bg-violet-900/40', rozet: 'bg-violet-100 dark:bg-violet-900/40 text-violet-700 dark:text-violet-300' },
  orange:  { bg: 'bg-orange-50 dark:bg-orange-900/15 hover:bg-orange-100 dark:hover:bg-orange-900/25 border-orange-200 dark:border-orange-800/50', icon: 'bg-orange-100 dark:bg-orange-900/40', rozet: 'bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300' },
  sky:     { bg: 'bg-sky-50 dark:bg-sky-900/15 hover:bg-sky-100 dark:hover:bg-sky-900/25 border-sky-200 dark:border-sky-800/50', icon: 'bg-sky-100 dark:bg-sky-900/40', rozet: 'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300' },
  emerald: { bg: 'bg-emerald-50 dark:bg-emerald-900/15 hover:bg-emerald-100 dark:hover:bg-emerald-900/25 border-emerald-200 dark:border-emerald-800/50', icon: 'bg-emerald-100 dark:bg-emerald-900/40', rozet: 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300' },
  amber:   { bg: 'bg-amber-50 dark:bg-amber-900/15 hover:bg-amber-100 dark:hover:bg-amber-900/25 border-amber-200 dark:border-amber-800/50', icon: 'bg-amber-100 dark:bg-amber-900/40', rozet: 'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300' },
  teal:    { bg: 'bg-teal-50 dark:bg-teal-900/15 hover:bg-teal-100 dark:hover:bg-teal-900/25 border-teal-200 dark:border-teal-800/50', icon: 'bg-teal-100 dark:bg-teal-900/40', rozet: 'bg-teal-100 dark:bg-teal-900/40 text-teal-700 dark:text-teal-300' },
  slate:   { bg: 'bg-slate-50 dark:bg-slate-800/40 hover:bg-slate-100 dark:hover:bg-slate-800 border-slate-200 dark:border-slate-700', icon: 'bg-slate-100 dark:bg-slate-700', rozet: 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300' },
  rose:    { bg: 'bg-rose-50 dark:bg-rose-900/15 hover:bg-rose-100 dark:hover:bg-rose-900/25 border-rose-200 dark:border-rose-800/50', icon: 'bg-rose-100 dark:bg-rose-900/40', rozet: 'bg-rose-100 dark:bg-rose-900/40 text-rose-700 dark:text-rose-300' },
}

export default function AraclarAyarlarPage() {
  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Araçlar ve Ayarlar</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
        Sunucu genelinde yönetim araçları. Sistem paketleri, PHP sürümleri, güvenlik ve bakım araçları.
      </p>

      <PanelGuncelleme />
      <SunucuOptimize />

      {GRUPLAR.map(g => (
        <div key={g.ad} className="mb-7">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-3">{g.ad}</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {g.aciklar.map(a => {
              const renk = RENK_MAP[a.renk]
              const Component = a.hazir ? Link : 'div'
              return (
                <Component
                  key={a.baslik}
                  to={a.href}
                  className={`relative flex items-start gap-3 p-4 border rounded-2xl transition ${renk.bg} ${a.hazir ? 'cursor-pointer' : 'cursor-not-allowed opacity-70'}`}
                >
                  <div className={`w-10 h-10 rounded-lg flex items-center justify-center text-xl flex-shrink-0 ${renk.icon}`}>
                    {a.ikon}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2">
                      <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{a.baslik}</span>
                      {a.rozet && (
                        <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium ${renk.rozet}`}>
                          {a.rozet}
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">{a.aciklama}</div>
                  </div>
                  {a.hazir && (
                    <svg className="w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0 mt-1" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
                    </svg>
                  )}
                </Component>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
