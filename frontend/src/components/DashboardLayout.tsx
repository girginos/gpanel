// gosp-dark-swept
// gosp-dark-swept-v2
import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import TopBar from './TopBar'

type NavItem = { to: string; etiket: string; ikon: string }
type NavGroup = { baslik?: string; items: NavItem[] }

const ICONS = {
  home:        'M3 12l2-2 7-7 7 7 2 2v8a2 2 0 01-2 2h-3v-7H10v7H7a2 2 0 01-2-2v-8z',
  musteri:     'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z',
  bayi:        'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z',
  domain:      'M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
  abonelik:    'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2',
  plan:        'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  araclar:     'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.827 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.99.601 2.295.247 2.572-1.065zM15 12a3 3 0 11-6 0 3 3 0 016 0z',
  istatistik:  'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z',
  eklenti:     'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4',
  wp:          'M12 2C6.477 2 2 6.477 2 12s4.477 10 10 10 10-4.477 10-10S17.523 2 12 2zm0 18a8 8 0 110-16 8 8 0 010 16z',
  izleme:      'M3 12l3-3 3 6 4-9 3 6h5',
  profil:      'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z',
  kilit:       'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z',
  firewall:    'M9 12l2 2 4-4m3 2c0 6-8 10-8 10S4 18 4 12V5l8-3 8 3v7z',
}

const NAV: NavGroup[] = [
  { items: [{ to: '/', etiket: 'Anasayfa', ikon: ICONS.home }] },
  { baslik: 'Barındırma Hizmetleri', items: [
    { to: '/domainler',           etiket: 'Domainler',        ikon: ICONS.domain },
    { to: '/hizmet-planlari',     etiket: 'Hizmet Planları',  ikon: ICONS.plan },
  ]},
  { baslik: 'Sunucu Yönetimi', items: [
    { to: '/araclar-ayarlar',     etiket: 'Araçlar ve Ayarlar', ikon: ICONS.araclar },
    { to: '/istatistikler',       etiket: 'İstatistikler',      ikon: ICONS.istatistik },
    { to: '/eklentiler',          etiket: 'Eklentiler',         ikon: ICONS.eklenti },
    { to: '/wordpress',           etiket: 'WordPress',          ikon: ICONS.wp },
    { to: '/firewall',            etiket: 'Güvenlik Duvarı',    ikon: ICONS.firewall },
    { to: '/izleme',              etiket: 'İzleme',             ikon: ICONS.izleme },
  ]},
  { baslik: 'Profilim', items: [
    { to: '/profil',              etiket: 'Profil ve Tercihler', ikon: ICONS.profil },
  ]},
]

export default function DashboardLayout() {
  const isMusteri = typeof window !== 'undefined' && localStorage.getItem('girginospanel.musteri') === '1'
  const musteriDomainID = typeof window !== 'undefined' ? localStorage.getItem('girginospanel.musteri.domain_id') || '' : ''

  const [acikGruplar, setAcikGruplar] = useState<Record<string, boolean>>({
    'Barındırma Hizmetleri': true,
    'Sunucu Yönetimi': true,
    'Profilim': true,
    'Domainim': true,
  })

  // Musteri navigasyonu — sadece kendi domain'i
  const MUSTERI_NAV: NavGroup[] = [
    { baslik: 'Domainim', items: [
      { to: `/abonelikler/${musteriDomainID}`, etiket: 'Genel Bakış', ikon: ICONS.home },
      { to: `/abonelikler/${musteriDomainID}/dosyalar`, etiket: 'Dosya Yöneticisi', ikon: ICONS.domain },
      { to: `/abonelikler/${musteriDomainID}/veritabanlari`, etiket: 'Veritabanları', ikon: ICONS.plan },
      { to: `/abonelikler/${musteriDomainID}/ftp`, etiket: 'FTP Hesapları', ikon: ICONS.bayi },
      { to: `/abonelikler/${musteriDomainID}/php`, etiket: 'PHP Ayarları', ikon: ICONS.araclar },
      { to: `/abonelikler/${musteriDomainID}/web-sunucu`, etiket: 'Apache & nginx', ikon: ICONS.araclar },
      { to: `/abonelikler/${musteriDomainID}/dns`, etiket: 'DNS Ayarları', ikon: ICONS.domain },
      { to: `/abonelikler/${musteriDomainID}/ssl`, etiket: 'SSL/TLS', ikon: ICONS.kilit },
      { to: `/abonelikler/${musteriDomainID}/cron`, etiket: 'Zamanlanmış Görevler', ikon: ICONS.izleme },
      { to: `/abonelikler/${musteriDomainID}/git`, etiket: 'Git Deploy', ikon: ICONS.eklenti },
      { to: `/abonelikler/${musteriDomainID}/gunlukler`, etiket: 'Günlükler', ikon: ICONS.istatistik },
      { to: `/abonelikler/${musteriDomainID}/yedekler`, etiket: 'Yedekler', ikon: ICONS.araclar },
    ]},
  ]

  const aktifNav = isMusteri ? MUSTERI_NAV : NAV

  function toggle(b: string) {
    setAcikGruplar((s) => ({ ...s, [b]: !s[b] }))
  }

  return (
    <div className="min-h-screen flex items-start bg-slate-50 dark:bg-slate-900">
      <aside className="w-56 bg-white dark:bg-slate-950 border-r border-slate-200 dark:border-slate-800 flex flex-col flex-shrink-0 sticky top-0 h-screen z-20 self-start">
        <div className="h-14 flex items-center px-5 border-b border-slate-200 dark:border-slate-800">
          <div className="w-8 h-8 rounded-md bg-brand-600 flex items-center justify-center mr-2.5 shadow-sm shadow-brand-600/40">
            <svg viewBox="0 0 32 32" className="w-4 h-4 text-white" fill="currentColor">
              <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
            </svg>
          </div>
          <span className="text-base font-semibold text-slate-900 dark:text-slate-100">GirginOSPanel</span>
        </div>

        <nav className="flex-1 px-2 py-3 overflow-y-auto">
          {aktifNav.map((grup, gi) => (
            <div key={gi} className="mb-2">
              {grup.baslik && (
                <button
                  onClick={() => toggle(grup.baslik!)}
                  className="w-full flex items-center justify-between px-3 py-1.5 mt-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 transition"
                >
                  <span>{grup.baslik}</span>
                  <svg
                    className={`w-3 h-3 transition-transform ${acikGruplar[grup.baslik!] ? '' : '-rotate-90'}`}
                    fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
                  </svg>
                </button>
              )}
              {(!grup.baslik || acikGruplar[grup.baslik]) && (
                <ul className="space-y-0.5">
                  {grup.items.map((it) => {
                    const ustPath = grup.items.some(
                      (it2) => it2.to !== it.to && it2.to.startsWith(it.to + '/')
                    )
                    return (
                    <li key={it.to}>
                      <NavLink
                        to={it.to}
                        end={it.to === '/' || ustPath}
                        className={({ isActive }) =>
                          `group relative flex items-center px-3 py-1.5 rounded-lg text-sm transition-all duration-150 ${
                            isActive
                              ? 'bg-slate-100 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-medium shadow-sm dark:shadow-none'
                              : 'text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/60 hover:text-slate-900 dark:hover:text-slate-100'
                          }`
                        }
                      >
                        {({ isActive }) => (
                          <>
                            {isActive && (
                              <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-r bg-slate-900 dark:bg-white" aria-hidden />
                            )}
                            <svg className={`w-4 h-4 mr-2.5 flex-shrink-0 transition ${
                              isActive ? 'text-brand-600 dark:text-brand-400' : 'text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:group-hover:text-slate-300'
                            }`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                              <path strokeLinecap="round" strokeLinejoin="round" d={it.ikon} />
                            </svg>
                            <span className="truncate">{it.etiket}</span>
                          </>
                        )}
                      </NavLink>
                    </li>
                  )})}
                </ul>
              )}
            </div>
          ))}
        </nav>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <TopBar />
        <main className="flex-1 min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  )
}