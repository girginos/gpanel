import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import Breadcrumb from '@/components/Breadcrumb'
import PanelGuncelleme from '@/components/PanelGuncelleme'
import SunucuOptimize from '@/components/SunucuOptimize'

/*
 * Araçlar ve Ayarlar — sunucu geneli yönetim merkezi.
 * Tasarım: panelin monokrom "premium açık SaaS" dili. Nötr yüzey + tek brand-aksan,
 * çizgi (stroke) SVG ikon, tutarlı rounded-2xl, canlı arama. Emoji/gökkuşağı YOK.
 */

type Arac = {
  baslik: string
  aciklama: string
  href: string
  ikon: string
  rozet?: string
  anahtar?: string
}

type Grup = { ad: string; ikon: string; araclar: Arac[] }

const I = {
  chip:      'M8.25 3v1.5M4.5 8.25H3m18 0h-1.5M4.5 12H3m18 0h-1.5m-15 3.75H3m18 0h-1.5M8.25 19.5V21M12 3v1.5m0 15V21m3.75-18v1.5m0 15V21m-9-1.5h10.5a2.25 2.25 0 0 0 2.25-2.25V6.75a2.25 2.25 0 0 0-2.25-2.25H6.75A2.25 2.25 0 0 0 4.5 6.75v10.5a2.25 2.25 0 0 0 2.25 2.25Zm.75-12h9v9h-9v-9Z',
  puzzle:    'M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 0 1-.657.643 48.39 48.39 0 0 1-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 0 1-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 0 0-1.003-.349c-1.036 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 0 1-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 0 0 .657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 0 1-.349-1.003c0-1.035 1.008-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.4.604-.4.959v0c0 .333.277.599.61.58a48.1 48.1 0 0 0 5.427-.63 48.05 48.05 0 0 0 .582-4.717.532.532 0 0 0-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.96.401v0a.656.656 0 0 0 .658-.663 48.422 48.422 0 0 0-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 0 1-.61-.58v0Z',
  cube:      'm21 7.5-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9',
  clipboard: 'M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25Z',
  refresh:   'M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99',
  globe:     'M12 21a9.004 9.004 0 0 0 8.716-6.747M12 21a9.004 9.004 0 0 1-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 0 1 7.843 4.582M12 3a8.997 8.997 0 0 0-7.843 4.582m15.686 0A11.953 11.953 0 0 1 12 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0 1 21 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0 1 12 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 0 1 3 12c0-1.605.42-3.113 1.157-4.418',
  link:      'M13.19 8.688a4.5 4.5 0 0 1 1.242 7.244l-4.5 4.5a4.5 4.5 0 0 1-6.364-6.364l1.757-1.757m13.35-.622 1.757-1.757a4.5 4.5 0 0 0-6.364-6.364l-4.5 4.5a4.5 4.5 0 0 0 1.242 7.244',
  shield:    'M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z',
  server:    'M5.25 14.25h13.5m-13.5 0a3 3 0 0 1-3-3m3 3a3 3 0 1 0 0 6h13.5a3 3 0 1 0 0-6m-16.5-3a3 3 0 0 1 3-3h13.5a3 3 0 0 1 3 3m-19.5 0a4.5 4.5 0 0 1 .9-2.7L5.737 5.1a3.375 3.375 0 0 1 2.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 0 1 .9 2.7m0 0a3 3 0 0 1-3 3m0 3h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Zm-3 6h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Z',
  chart:     'M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z',
  wrench:    'M11.42 15.17 17.25 21A2.652 2.652 0 0 0 21 17.25l-5.877-5.877M11.42 15.17l2.496-3.03c.317-.384.74-.626 1.208-.766M11.42 15.17l-4.655 5.653a2.548 2.548 0 1 1-3.586-3.586l6.837-5.63m5.108-.233c.55-.164 1.163-.188 1.743-.14a4.5 4.5 0 0 0 4.486-6.336l-3.276 3.277a3.004 3.004 0 0 1-2.25-2.25l3.276-3.276a4.5 4.5 0 0 0-6.336 4.486c.091 1.076-.071 2.264-.904 2.95l-.102.085m-1.745 1.437L5.909 7.5H4.5L2.25 3.75l1.5-1.5L7.5 4.5v1.409l4.26 4.26m-1.745 1.437 1.745-1.437m6.615 8.206L15.75 15.75M4.867 19.125h.008v.008h-.008v-.008Z',
  tune:      'M10.5 6h9.75M10.5 6a1.5 1.5 0 1 1-3 0m3 0a1.5 1.5 0 1 0-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-9.75 0h9.75',
  search:    'm21 21-4.34-4.34M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z',
  chevron:   'M9 5l7 7-7 7',
}

const GRUPLAR: Grup[] = [
  {
    ad: 'PHP',
    ikon: I.chip,
    araclar: [
      { baslik: 'PHP Sürümleri', href: '/araclar/php-surumler', ikon: I.chip, rozet: 'Dinamik',
        anahtar: 'remi fpm versiyon 7.4 8.0 8.1 8.2 8.3 8.4 8.5 8.6',
        aciklama: '7.4 → 8.6 sürümlerini ekleyin / kaldırın. Her domain kendi sürümünü seçer.' },
      { baslik: 'PHP Modülleri', href: '/sistem/php-modulleri', ikon: I.puzzle,
        anahtar: 'extension pecl derleme',
        aciklama: 'Sunucu genelinde eklenti aç/kapat. PECL paket arama ve derleme.' },
    ],
  },
  {
    ad: 'Sistem ve Servisler',
    ikon: I.server,
    araclar: [
      { baslik: 'Paket Yöneticisi', href: '/araclar/paketler', ikon: I.cube,
        anahtar: 'dnf gcc python node go podman derleyici',
        aciklama: 'DNF paketleri — derleyiciler ve çalışma ortamları. Hazır kurulum grupları.' },
      { baslik: 'Servisler', href: '/araclar/servisler', ikon: I.refresh,
        anahtar: 'nginx apache mariadb dns php-fpm restart',
        aciklama: 'Nginx / Apache / MariaDB / DNS / PHP-FPM durumu ve yeniden başlatma.' },
      { baslik: 'Hizmet Planları', href: '/hizmet-planlari', ikon: I.clipboard,
        anahtar: 'paket kota disk ftp veritabani',
        aciklama: 'Barındırma paketleri; disk, veritabanı ve FTP kotaları.' },
    ],
  },
  {
    ad: 'Ağ ve DNS',
    ikon: I.globe,
    araclar: [
      { baslik: 'DNS Şablonu', href: '/araclar/dns-sablonu', ikon: I.globe, rozet: 'Merkezi',
        anahtar: 'a mx spf dmarc dkim soa kayit',
        aciklama: 'Yeni domainlere uygulanan merkezi DNS kayıtları (A/MX/SPF/DMARC/DKIM) + SOA.' },
      { baslik: 'Domainler', href: '/domainler', ikon: I.link,
        anahtar: 'site abonelik liste',
        aciklama: 'Tüm domain listesi, arama ve hızlı erişim.' },
    ],
  },
  {
    ad: 'Güvenlik ve Yedekleme',
    ikon: I.shield,
    araclar: [
      { baslik: 'Güvenlik Duvarı', href: '/firewall', ikon: I.shield,
        anahtar: 'nftables ip port yasak beyaz liste',
        aciklama: 'IP/port yasağı, beyaz liste, port kapatma. Kritik portlar korumalı.' },
      { baslik: 'Backup Yöneticisi', href: '/backup-yonetimi', ikon: I.server,
        anahtar: 'yedek s3 sftp boyut',
        aciklama: 'Tüm domainlerin yedekleri + boyut, tek tıkla yedekle. S3/SFTP hedefler.' },
      { baslik: 'İzleme ve Loglar', href: '/izleme', ikon: I.chart,
        anahtar: 'cpu ram disk grafik journald gunluk log',
        aciklama: 'CPU/RAM/disk grafikleri ve sunucu günlükleri (panel/nginx/SSH…).' },
    ],
  },
]

function Ikon({ d, className = '' }: { d: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6}
      aria-hidden="true" className={className}>
      <path strokeLinecap="round" strokeLinejoin="round" d={d} />
    </svg>
  )
}

function AracKart({ a }: { a: Arac }) {
  return (
    <Link
      to={a.href}
      className="group relative flex items-start gap-3.5 rounded-2xl border border-slate-200 bg-white p-4
                 transition-all hover:border-brand-300 hover:shadow-sm
                 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/50
                 dark:border-slate-800 dark:bg-slate-900/40 dark:hover:border-brand-700/60 dark:hover:bg-slate-900"
    >
      <span
        className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl
                   bg-slate-100 text-slate-500 transition-colors
                   group-hover:bg-brand-50 group-hover:text-brand-600
                   dark:bg-slate-800 dark:text-slate-400 dark:group-hover:bg-brand-900/30 dark:group-hover:text-brand-400"
      >
        <Ikon d={a.ikon} className="h-5 w-5" />
      </span>

      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{a.baslik}</span>
          {a.rozet && (
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide
                             text-slate-500 dark:bg-slate-800 dark:text-slate-400">
              {a.rozet}
            </span>
          )}
        </span>
        <span className="mt-1 block text-xs leading-relaxed text-slate-500 dark:text-slate-400">{a.aciklama}</span>
      </span>

      <Ikon d={I.chevron}
        className="mt-0.5 h-4 w-4 flex-shrink-0 text-slate-300 transition-all
                   group-hover:translate-x-0.5 group-hover:text-brand-500 dark:text-slate-600" />
    </Link>
  )
}

export default function AraclarAyarlarPage() {
  const [q, setQ] = useState('')

  const gruplar = useMemo(() => {
    const t = q.trim().toLowerCase()
    if (!t) return GRUPLAR
    return GRUPLAR
      .map(g => ({
        ...g,
        araclar: g.araclar.filter(a =>
          (a.baslik + ' ' + a.aciklama + ' ' + (a.anahtar ?? '') + ' ' + g.ad).toLowerCase().includes(t)),
      }))
      .filter(g => g.araclar.length > 0)
  }, [q])

  const toplam = GRUPLAR.reduce((n, g) => n + g.araclar.length, 0)

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Araçlar ve Ayarlar' }]} />

      {/* Başlık + arama */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Araçlar ve Ayarlar</h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Sunucu geneli yönetim — PHP, sistem paketleri, ağ, güvenlik ve bakım.
          </p>
        </div>
        <div className="relative w-full sm:w-72">
          <Ikon d={I.search}
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
          <input
            type="search"
            value={q}
            onChange={e => setQ(e.target.value)}
            placeholder="Araç ara…"
            aria-label="Araç ara"
            className="w-full rounded-xl border border-slate-200 bg-white py-2 pl-9 pr-3 text-sm text-slate-900
                       placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/30
                       dark:border-slate-800 dark:bg-slate-900/60 dark:text-slate-100"
          />
        </div>
      </div>

      {/* Sunucu bakımı — güncelleme + optimize */}
      <section aria-labelledby="bakim-baslik" className="mb-8">
        <div className="mb-3 flex items-center gap-2">
          <Ikon d={I.wrench} className="h-4 w-4 text-slate-400" />
          <h2 id="bakim-baslik" className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Sunucu Bakımı
          </h2>
        </div>
        <div className="space-y-3">
          <PanelGuncelleme />
          <SunucuOptimize />
        </div>
      </section>

      {/* Araç grupları */}
      {gruplar.length === 0 ? (
        <div role="status" className="rounded-2xl border border-dashed border-slate-200 py-14 text-center dark:border-slate-800">
          <Ikon d={I.tune} className="mx-auto h-9 w-9 text-slate-300 dark:text-slate-600" />
          <p className="mt-3 text-sm font-medium text-slate-700 dark:text-slate-300">"{q}" için araç bulunamadı</p>
          <p className="mt-1 text-xs text-slate-500">Arama terimini değiştirin veya temizleyin.</p>
        </div>
      ) : (
        gruplar.map(g => (
          <section key={g.ad} aria-labelledby={`grup-${g.ad}`} className="mb-8">
            <div className="mb-3 flex items-center gap-2">
              <Ikon d={g.ikon} className="h-4 w-4 text-slate-400" />
              <h2 id={`grup-${g.ad}`} className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
                {g.ad}
              </h2>
              <span className="text-xs text-slate-300 dark:text-slate-600">·</span>
              <span className="text-xs text-slate-400 dark:text-slate-500">{g.araclar.length}</span>
            </div>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
              {g.araclar.map(a => <AracKart key={a.baslik} a={a} />)}
            </div>
          </section>
        ))
      )}

      {!q && (
        <p className="pt-2 text-xs text-slate-400 dark:text-slate-600">{toplam} araç · sunucu geneli</p>
      )}
    </div>
  )
}
