// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { useEklentiAktif } from '@/lib/eklenti'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import TopBar from './TopBar'
import AltNavBar from './AltNavBar'
import HataYuzeyi from './HataYuzeyi'

type NavItem = { to: string; etiket: string; ikon: string ; sayac?: string }
type NavGroup = { baslik?: string; items: NavItem[] }
// Sayac anahtarlari GET /nav-sayaclar yanitindan gelir (Plesk deseni:
// menu ogesinin sagindaki adet). Deger 0/yoksa rozet CIZILMEZ.
type Sayaclar = Record<string, number>

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
  wp:          'M12 2C6.486 2 2 6.486 2 12s4.486 10 10 10 10-4.486 10-10S17.514 2 12 2zM3.98 8.14a8 8 0 013.28-3.11L4.06 17.12A7.99 7.99 0 013.98 8.14zm14.08.03a8 8 0 01-3.19 8.87l3.19-8.87zM7.19 5.03h4.02l1.31 5.51-2.22 6.66L7.19 5.03zm5.24 6.74l2.15-5.97a8.04 8.04 0 014.34.01l-2.15 5.96h-4.34z',
  izleme:      'M3 12l3-3 3 6 4-9 3 6h5',
  profil:      'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z',
  kilit:       'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z',
  firewall:    'M9 12l2 2 4-4m3 2c0 6-8 10-8 10S4 18 4 12V5l8-3 8 3v7z',
  guncelleme:  'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15',
  denetim:         'M9 12h6m-6 4h6M7 8h10M5 4h14a2 2 0 012 2v14a2 2 0 01-2 2H5a2 2 0 01-2-2V6a2 2 0 012-2z',
  mail:         'M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z',
  tasima:         'M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4',
  websec:         'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z',
  imunify:     'M8 7a4 4 0 018 0M6 11h12M7 11v3a5 5 0 0010 0v-3M4 13h3m10 0h3M12 8v11',
  zincir:      'M13.19 8.69a4.5 4.5 0 011.24 7.24l-4.5 4.5a4.5 4.5 0 01-6.36-6.36l1.76-1.76m13.35-.62l1.76-1.76a4.5 4.5 0 00-6.36-6.36l-4.5 4.5a4.5 4.5 0 001.24 7.24',
  optimize:    'M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4',
}

const NAV: NavGroup[] = [
  { items: [{ to: '/', etiket: 'Anasayfa', ikon: ICONS.home }] },
  { baslik: 'Barındırma Hizmetleri', items: [
    { to: '/domainler',           etiket: 'Domainler',        ikon: ICONS.domain, sayac: 'domainler' },
    { to: '/bayiler',             etiket: 'Bayiler',          ikon: ICONS.profil, sayac: 'bayiler' },
  ]},
  { baslik: 'Planlar', items: [
    { to: '/hizmet-planlari', etiket: 'Hosting Planları', ikon: ICONS.plan, sayac: 'hizmet_planlari' },
    { to: '/bayi-planlari',   etiket: 'Bayi Planları',    ikon: ICONS.plan, sayac: 'bayi_planlari' },
  ]},
  { baslik: 'Sunucu Yönetimi', items: [
    { to: '/araclar-ayarlar',     etiket: 'Araçlar ve Ayarlar', ikon: ICONS.araclar },
    { to: '/araclar/optimize', etiket: 'Sunucu Optimize', ikon: ICONS.optimize },
    { to: '/araclar/tasima', etiket: 'Site Taşıma', ikon: ICONS.tasima },
    { to: '/istatistikler',       etiket: 'İstatistikler',      ikon: ICONS.istatistik },
    { to: '/eklentiler',          etiket: 'Eklentiler',         ikon: ICONS.eklenti },
    { to: '/wordpress',           etiket: 'WordPress',          ikon: ICONS.wp },
    { to: '/firewall',            etiket: 'Güvenlik Duvarı',    ikon: ICONS.firewall },
    { to: '/izleme',              etiket: 'İzleme',             ikon: ICONS.izleme },
    { to: '/mail-sunucu',         etiket: 'Mail Sunucu',        ikon: ICONS.mail },
    { to: '/website-security',     etiket: 'Website Security Monitor', ikon: ICONS.websec },
    { to: '/antivirus',           etiket: 'Antivirüs',          ikon: ICONS.imunify },
    { to: '/saldiri-zincirleri',  etiket: 'Saldırı Zincirleri',  ikon: ICONS.zincir },
    { to: '/denetim',             etiket: 'Denetim Kaydı',      ikon: ICONS.denetim },
  ]},
  { baslik: 'Profilim', items: [
    { to: '/profil',              etiket: 'Profil ve Tercihler', ikon: ICONS.profil },
  ]},
]

const RESELLER_NAV: NavGroup[] = [
  { items: [{ to: '/', etiket: 'Anasayfa', ikon: ICONS.home }] },
  { baslik: 'Barındırma Hizmetleri', items: [
    { to: '/domainler',       etiket: 'Hosting Hesapları', ikon: ICONS.domain, sayac: 'domainler' },
    { to: '/hizmet-planlari', etiket: 'Hosting Planlarım', ikon: ICONS.plan, sayac: 'hizmet_planlari' },
  ]},
  { baslik: 'Ayarlar', items: [
    { to: '/araclar/dns-sablonu', etiket: 'DNS Şablonum',        ikon: ICONS.domain },
    { to: '/denetim',             etiket: 'Denetim Kaydım',      ikon: ICONS.denetim },
    { to: '/profil',              etiket: 'Profil ve Tercihler', ikon: ICONS.profil },
  ]},
]

export default function DashboardLayout() {
  const isMusteri = typeof window !== 'undefined' && localStorage.getItem('girginospanel.musteri') === '1'
  let _rol = ''
  try { _rol = JSON.parse(localStorage.getItem('gosp.user') || '{}').rol || '' } catch { /* okunamadi */ }
  const isReseller = _rol === 'reseller'
  const musteriDomainID = typeof window !== 'undefined' ? localStorage.getItem('girginospanel.musteri.domain_id') || '' : ''

  const [sayac, setSayac] = useState<Sayaclar>({})
  useEffect(() => {
    if (isMusteri) return
    let iptal = false
    const cek = () => api.get<Sayaclar>('/nav-sayaclar')
      .then(r => { if (!iptal) setSayac(r.data || {}) })
      .catch(() => { /* sayac kritik degil, sessiz gec */ })
    cek()
    const t = setInterval(cek, 60000)          // menu sayilari 1 dk'da bir tazelenir
    return () => { iptal = true; clearInterval(t) }
  }, [isMusteri])

  const [acikGruplar, setAcikGruplar] = useState<Record<string, boolean>>({
    'Barındırma Hizmetleri': true,
    'Planlar': true,
    'Sunucu Yönetimi': true,
    'Profilim': true,
    'Domainim': true,
  })

  // Mobil kenar çubuğu (off-canvas). lg ve üstünde sidebar zaten sabit görünür,
  // bu durum yalnızca < lg genişliklerde anlam taşır.
  const [mobilAcik, setMobilAcik] = useState(false)
  const konum = useLocation()

  // Rota değişince çekmeceyi kapat (link tıklamasında da onClick kapatıyor;
  // bu, geri/ileri gezinmesini de kapsayan güvenli ağ).
  useEffect(() => { setMobilAcik(false) }, [konum.pathname])

  // Çekmece açıkken Esc ile kapat + arka plan kaydırmasını kilitle
  useEffect(() => {
    if (!mobilAcik) return
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') setMobilAcik(false) }
    window.addEventListener('keydown', onKey)
    const eskiOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = eskiOverflow
    }
  }, [mobilAcik])

  // Musteri navigasyonu — sadece kendi domain'i
  const MUSTERI_NAV: NavGroup[] = [
    { baslik: 'Domainim', items: [
      { to: `/abonelikler/${musteriDomainID}`, etiket: 'Genel Bakış', ikon: ICONS.home },
      { to: `/abonelikler/${musteriDomainID}/dosyalar`, etiket: 'Dosya Yöneticisi', ikon: ICONS.domain },
      { to: `/abonelikler/${musteriDomainID}/veritabanlari`, etiket: 'Veritabanları', ikon: ICONS.plan },
      { to: `/abonelikler/${musteriDomainID}/ftp`, etiket: 'FTP Hesapları', ikon: ICONS.bayi },
      { to: `/abonelikler/${musteriDomainID}/php`, etiket: 'PHP Ayarları', ikon: ICONS.araclar },
      { to: `/abonelikler/${musteriDomainID}/web-sunucu`, etiket: 'Apache & nginx', ikon: ICONS.araclar },
      { to: `/abonelikler/${musteriDomainID}/laravel`, etiket: 'Laravel', ikon: ICONS.eklenti },
      { to: `/abonelikler/${musteriDomainID}/dns`, etiket: 'DNS Ayarları', ikon: ICONS.domain },
      { to: `/abonelikler/${musteriDomainID}/ssl`, etiket: 'SSL/TLS', ikon: ICONS.kilit },
      { to: `/abonelikler/${musteriDomainID}/cron`, etiket: 'Zamanlanmış Görevler', ikon: ICONS.izleme },
      { to: `/abonelikler/${musteriDomainID}/git`, etiket: 'Git Deploy', ikon: ICONS.eklenti },
      { to: `/abonelikler/${musteriDomainID}/gunlukler`, etiket: 'Günlükler', ikon: ICONS.istatistik },
      { to: `/abonelikler/${musteriDomainID}/yedekler`, etiket: 'Yedekler', ikon: ICONS.araclar },
    ]},
  ]

  // 'Mail Sunucu' menusu YALNIZ mail eklentisi lisansli+etkinken cizilir.
  // Lisans kaldirilinca (cp_eklentiler.aktif=0) menu KENDILIGINDEN kaybolur;
  // yeniden derleme gerekmez. Deger bilinmiyorsa (null) oge GIZLENIR — 402
  // donecek bir menu ogesini gostermek yaniltici olurdu.
  const mailAktif = useEklentiAktif('mail', !isMusteri)
  const temelNav = isMusteri ? MUSTERI_NAV : isReseller ? RESELLER_NAV : NAV
  const aktifNav = mailAktif === true
    ? temelNav
    : temelNav.map(g => ({ ...g, items: g.items.filter(it => it.to !== '/mail-sunucu') }))

  function toggle(b: string) {
    setAcikGruplar((s) => ({ ...s, [b]: !s[b] }))
  }

  return (
    <div className="min-h-screen flex items-start bg-slate-50 dark:bg-slate-900">
      {/* Mobil perde — yalnız çekmece açıkken ve < lg genişlikte */}
      {mobilAcik && (
        <div
          className="fixed inset-0 z-40 bg-slate-900/50 lg:hidden"
          onClick={() => setMobilAcik(false)}
          aria-hidden
        />
      )}

      {/*
        < lg : ekran dışına kaydırılmış sabit çekmece (hamburger ile açılır)
        >= lg: eski davranış — akışta duran yapışkan kenar çubuğu
      */}
      <aside
        id="gosp-kenar-cubugu"
        className={`fixed inset-y-0 left-0 z-50 w-72 bg-white dark:bg-slate-950 border-r border-slate-200 dark:border-slate-800 flex flex-col flex-shrink-0 h-screen transform transition-transform duration-200 ease-out ${
          mobilAcik ? 'translate-x-0' : '-translate-x-full'
        } lg:sticky lg:top-0 lg:bottom-auto lg:left-auto lg:z-20 lg:w-fit lg:min-w-[17rem] lg:max-w-[21rem] lg:translate-x-0 lg:self-start`}
      >
        <div className="h-14 flex items-center px-5 border-b border-slate-200 dark:border-slate-800">
          <div className="w-8 h-8 rounded-md bg-brand-600 flex items-center justify-center mr-2.5 shadow-sm shadow-brand-600/40">
            <svg viewBox="0 0 32 32" className="w-4 h-4 text-white" fill="currentColor">
              <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
            </svg>
          </div>
          <span className="text-[17px] font-semibold text-slate-900 dark:text-slate-100">GirginOSPanel</span>
          <button
            onClick={() => setMobilAcik(false)}
            className="ml-auto -mr-2 p-2 text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 rounded-md transition lg:hidden"
            aria-label="Menüyü kapat"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <nav className="flex-1 px-2 py-3 overflow-y-auto">
          {aktifNav.map((grup, gi) => (
            <div key={gi} className="mb-2">
              {grup.baslik && (
                <button
                  onClick={() => toggle(grup.baslik!)}
                  className="w-full flex items-center justify-between px-3 py-1.5 mt-1 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 transition"
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
                        onClick={() => setMobilAcik(false)}
                        className={({ isActive }) =>
                          `group relative flex items-center px-3 py-2.5 lg:py-2 rounded-lg text-[15px] transition-all duration-150 ${
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
                            <svg className={`w-[18px] h-[18px] mr-3 flex-shrink-0 transition ${
                              isActive ? 'text-brand-600 dark:text-brand-400' : 'text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:group-hover:text-slate-300'
                            }`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                              <path strokeLinecap="round" strokeLinejoin="round" d={it.ikon} />
                            </svg>
                            <span className="whitespace-nowrap">{it.etiket}</span>
                            {it.sayac && sayac[it.sayac] ? (
                              <span
                                className={`ml-auto pl-2 shrink-0 text-[13px] tabular-nums transition ${
                                  isActive
                                    ? 'text-slate-700 dark:text-slate-200 font-medium'
                                    : 'text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:group-hover:text-slate-300'
                                }`}
                                aria-label={`${sayac[it.sayac]} adet`}
                              >
                                {sayac[it.sayac]}
                              </span>
                            ) : null}
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
        <TopBar onMenuAc={() => setMobilAcik(true)} menuAcik={mobilAcik} />
        <main className="flex-1 min-w-0 pb-[calc(4rem+env(safe-area-inset-bottom))] lg:pb-0">
          <Outlet />
        </main>
      </div>

      <AltNavBar onMenuAc={() => setMobilAcik(true)} />
      <HataYuzeyi />
    </div>
  )
}
