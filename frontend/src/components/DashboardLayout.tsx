import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
// gosp-tailux-rail-v1  (iki katmanli sidebar: ikon rayi + etiketli panel)
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { useEklentiler } from '@/lib/eklenti'
import { useMarka, logoURL, faviconURL } from '@/lib/marka'
import { Suspense } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import TopBar from './TopBar'
import AltNavBar from './AltNavBar'
import HataYuzeyi from './HataYuzeyi'

type NavItem = { to: string; etiket: string; ikon: string ; sayac?: string; rozet?: string }
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
  marka:       'M7 21a4 4 0 01-4-4V5a2 2 0 012-2h4a2 2 0 012 2v12a4 4 0 01-4 4zm0 0h12a2 2 0 002-2v-4a2 2 0 00-2-2h-2.343M11 7.343l1.657-1.657a2 2 0 012.828 0l2.829 2.829a2 2 0 010 2.828L11 19',
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
  tehdit:      'M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6l8-4zM12 8v4M12 16h.01',
  zincir:      'M13.19 8.69a4.5 4.5 0 011.24 7.24l-4.5 4.5a4.5 4.5 0 01-6.36-6.36l1.76-1.76m13.35-.62l1.76-1.76a4.5 4.5 0 00-6.36-6.36l-4.5 4.5a4.5 4.5 0 001.24 7.24',
  optimize:    'M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4',
  cpu:         'M9 3v2M15 3v2M9 19v2M15 19v2M3 9h2M3 15h2M19 9h2M19 15h2M7 7h10v10H7V7zm3 3h4v4h-4v-4z',
  // Ray bolum ikonlari (Tailux tarzi ust seviye)
  barindirma:  'M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-13 2h.01M7 16h.01',
  sunucu:      'M4 5h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1V6a1 1 0 011-1zM4 13h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1zM7 8h.01M7 16h.01',
  bildirim:    'M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9',
  yedek:       'M4 7v10c0 1.105 3.582 2 8 2s8-.895 8-2V7M4 7c0 1.105 3.582 2 8 2s8-.895 8-2M4 7c0-1.105 3.582-2 8-2s8 .895 8 2m0 5c0 1.105-3.582 2-8 2s-8-.895-8-2',
}


// Menü/başlık çevirisi (TR anahtar → EN). Diğer diller şimdilik TR'ye düşer.
const MENU_EN: Record<string, string> = {
  'Anasayfa': 'Home', 'Domainler': 'Domains', 'Bayiler': 'Resellers',
  'Hosting Planları': 'Hosting Plans', 'Bayi Planları': 'Reseller Plans',
  'Araçlar ve Ayarlar': 'Tools & Settings', 'PHP & Sunucu Sihirbazı': 'PHP & Server Wizard',
  'Sunucu Optimize': 'Server Optimization', 'Site Taşıma': 'Site Migration',
  'İstatistikler': 'Statistics', 'Eklentiler': 'Plugins', 'Marka': 'Branding', 'WordPress': 'WordPress',
  'Güvenlik Duvarı': 'Firewall', 'İzleme': 'Monitoring',
  'Website Security Monitor': 'Website Security Monitor', 'Antivirüs': 'Antivirus', 'Yakında': 'Soon', 'Tehdit İstihbaratı': 'Threat Intelligence', 'Uygulama Çalıştırıcı': 'Application Runner',
  'Saldırı Zincirleri': 'Attack Chains', 'Denetim Kaydı': 'Audit Log',
  'Denetim Kaydım': 'My Audit Log', 'Mail Sunucu': 'Mail Server',
  'Dosya Yöneticisi': 'File Manager', 'Veritabanları': 'Databases',
  'FTP Hesapları': 'FTP Accounts', 'DNS Ayarları': 'DNS Settings',
  'DNS Şablonum': 'My DNS Template', 'SSL/TLS': 'SSL/TLS',
  'PHP Ayarları': 'PHP Settings', 'Yedekler': 'Backups', 'Günlükler': 'Logs',
  'Zamanlanmış Görevler': 'Cron Jobs', 'Git Deploy': 'Git Deploy', 'Laravel': 'Laravel',
  'Apache & nginx': 'Apache & nginx', 'Hosting Hesapları': 'Hosting Accounts',
  'Genel Bakış': 'Overview', 'Domainim': 'My Domain',
  'Hosting Planlarım': 'My Hosting Plans',
  'Profil ve Tercihler': 'Profile & Preferences',
  // Grup başlıkları
  'Barındırma Hizmetleri': 'Hosting Services', 'Planlar': 'Plans',
  'Sunucu Yönetimi': 'Server Management', 'Ayarlar': 'Settings',
  'Profilim': 'My Profile', 'Genel': 'General',
  'Bildirimler': 'Notifications', 'Yedek Yönetimi': 'Backup Management',
  'Menüyü kapat': 'Close menu', 'adet': 'items',
}

// Ray'de kendi ikonunu URETMEYEN bolum: adanmis ev dugmesi onu temsil eder.
const GENEL = 'Genel'

const NAV: NavGroup[] = [
  // 🔴 'Genel' bir BASLIK tasir. Basliksiz olsaydi `bolumler` filtresine
  // girmez, Anasayfa'dayken panel son secili bolumu (or. Sunucu Yonetimi)
  // gostermeye devam ederdi -- ev dugmesi mavi, yanindaki liste alakasiz.
  // Ray'de ayrica ikon URETMEZ: ustteki adanmis ev dugmesi bu bolumu temsil eder.
  { baslik: 'Genel', items: [
    { to: '/',                etiket: 'Anasayfa',       ikon: ICONS.home },
    { to: '/bildirimler',     etiket: 'Bildirimler',    ikon: ICONS.bildirim },
    { to: '/backup-yonetimi', etiket: 'Yedek Yönetimi', ikon: ICONS.yedek },
  ]},
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
    { to: '/php-sunucu-sihirbazi', etiket: 'PHP & Sunucu Sihirbazı', ikon: ICONS.cpu },
    { to: '/araclar/optimize', etiket: 'Sunucu Optimize', ikon: ICONS.optimize },
    { to: '/araclar/tasima', etiket: 'Site Taşıma', ikon: ICONS.tasima },
    { to: '/eklentiler',          etiket: 'Eklentiler',         ikon: ICONS.eklenti },
    { to: '/marka',               etiket: 'Marka',              ikon: ICONS.marka },
    { to: '/wordpress',           etiket: 'WordPress',          ikon: ICONS.wp },
    { to: '/firewall',            etiket: 'Güvenlik Duvarı',    ikon: ICONS.firewall },
    { to: '/izleme',              etiket: 'İzleme',             ikon: ICONS.izleme },
    { to: '/mail-sunucu',         etiket: 'Mail Sunucu',        ikon: ICONS.mail },
    { to: '/website-security',     etiket: 'Website Security Monitor', ikon: ICONS.websec },
    { to: '/antivirus',           etiket: 'Antivirüs',          ikon: ICONS.imunify, rozet: 'Yakında' },
    { to: '/saldiri-zincirleri',  etiket: 'Saldırı Zincirleri',  ikon: ICONS.zincir },
    { to: '/denetim',             etiket: 'Denetim Kaydı',      ikon: ICONS.denetim },
  ]},
  { baslik: 'Profilim', items: [
    { to: '/profil',              etiket: 'Profil ve Tercihler', ikon: ICONS.profil },
  ]},
]

const RESELLER_NAV: NavGroup[] = [
  // Bayi tarafinda da ayni yetim-panel sorunu vardi; icerik eklenmiyor,
  // yalnizca baslik veriliyor ki panel Anasayfa'yi takip etsin.
  { baslik: GENEL, items: [{ to: '/', etiket: 'Anasayfa', ikon: ICONS.home }] },
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

// Ray (ikon seviyesi) icin bolum basligi → ikon eslemesi.
const BOLUM_IKON: Record<string, string> = {
  'Barındırma Hizmetleri': ICONS.barindirma,
  'Planlar': ICONS.plan,
  'Sunucu Yönetimi': ICONS.sunucu,
  'Profilim': ICONS.profil,
  'Ayarlar': ICONS.araclar,
  'Domainim': ICONS.domain,
}

// Rota parcasi inerken icerik alaninda gosterilir (kabuk ayakta kalir).
function IcerikYukleniyor() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600 dark:border-dark-600 dark:border-t-brand-400" />
    </div>
  )
}

export default function DashboardLayout() {
  const marka = useMarka()
  const { i18n } = useTranslation()
  const cevir = (tr: string) => (i18n.language === 'en' ? (MENU_EN[tr] || tr) : tr)

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

  // Mobil kenar çubuğu (off-canvas). lg ve üstünde sidebar zaten sabit görünür,
  // bu durum yalnızca < lg genişliklerde anlam taşır.
  const [mobilAcik, setMobilAcik] = useState(false)
  const konum = useLocation()
  const yonlendir = useNavigate()

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
  // 🔴 GÖRÜNÜRLÜK: 'aktif' DEĞİL 'kurulu'. Askıya alınmış (kurulu ama
  // aktif=0) bir eklentinin sekmesi GİZLENMEZ — açık kalır ki sayfası 'lisans
  // askıda' landing'ini çizebilsin. Gerçek yönetim engeli SERVER-SIDE'dır
  // (eklenti proxy'si aktif=0 => 402); bu yalnız sekmenin kaybolmayıp
  // landing göstermesi içindir. Kurulu DEĞİLse (listede yok) sekme yine gizli.
  const eklentiler = useEklentiler(!isMusteri)
  const kuruluMu = (ad: string): boolean | null =>
    eklentiler === null ? null : eklentiler.some((e) => e.ad === ad)
  const eklentiKapilari: Array<{ yol: string; goster: boolean | null }> = [
    { yol: '/mail-sunucu', goster: kuruluMu('mail') },
    { yol: '/marka', goster: isReseller ? false : kuruluMu('whitelabel') },
  ]
  const gizli = new Set(eklentiKapilari.filter((k) => k.goster !== true).map((k) => k.yol))
  // 🔴 EKLENTİ KAPILARI GEÇ ÇÖZÜLÜR — aşağıdaki efektler buna BAĞLIDIR.
  // useEklentiAktif ilk render'da `null` döner ("henüz bilinmiyor"), o anda
  // eklenti rotaları `gizli`dedir ve menüden süzülür. /marka üzerinde
  // yenileme yapıldığında aktif bölüm efekti tam o anda koşuyor, rotayı
  // içeren bölümü bulamıyor ve panel 'Genel'e düşüyordu; liste gelince öğe
  // menüye giriyor ama efektin tek bağımlılığı pathname olduğu için BİR DAHA
  // KOŞMUYORDU. Bu anahtar, kapılar çözülünce efektleri yeniden tetikler.
  const gizliAnahtar = [...gizli].sort().join(',')
  const temelNav = isMusteri ? MUSTERI_NAV : isReseller ? RESELLER_NAV : NAV
  const aktifNav = gizli.size === 0
    ? temelNav
    : temelNav.map(g => ({ ...g, items: g.items.filter(it => !gizli.has(it.to)) }))

  // ── Iki katmanli sidebar ────────────────────────────────────────────────
  // Ust seviye (ray): baslikli gruplar. Ungrouped 'Anasayfa' rayda ayri link.
  // Ray'in ustundeki adanmis ev dugmesi 'Genel' bolumunun ILK ogesidir.
  // (Eskiden basliksiz gruptan geliyordu; Genel artik baslikli.)
  const homeItem = (aktifNav.find(g => g.baslik === GENEL) ?? aktifNav.find(g => !g.baslik))?.items[0]
  const bolumler = aktifNav.filter(g => !!g.baslik)

  // Panelde gosterilen aktif bolum. Rota degisince o rotayi iceren bolume gecer.
  const [aktifBolum, setAktifBolum] = useState<string>('')
  useEffect(() => {
    const m = aktifNav
      .filter(g => !!g.baslik)
      .find(g => g.items.some(it => konum.pathname === it.to || konum.pathname.startsWith(it.to + '/')))
    if (m?.baslik) setAktifBolum(m.baslik)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [konum.pathname, gizliAnahtar])

  // Sekme başlığı SAYFAYA ÖZEL. marka.uygula() yalnız marka adını yazıyordu,
  // dolayısıyla her sekmede aynı başlık görünüyordu. Burada aktif menü öğesinin
  // etiketini markanın önüne ekleriz ("Eklentiler · GirginOSPanel"). Marka/dil
  // değişince de çalışır — aksi halde marka geç yüklenince başlığı geri ezerdi.
  // En uzun eşleşen 'to' seçilir ki alt sayfalar en özgül üst öğeyi göstersin.
  useEffect(() => {
    const markaAd = marka.baslik || marka.panel_adi
    const aktif = aktifNav
      .flatMap(g => g.items)
      .filter(it => konum.pathname === it.to || konum.pathname.startsWith(it.to + '/'))
      .sort((a, b) => b.to.length - a.to.length)[0]
    document.title = aktif ? `${cevir(aktif.etiket)} · ${markaAd}` : markaAd
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [konum.pathname, marka, i18n.language, gizliAnahtar])

  const gosterilenBaslik = aktifBolum || bolumler[0]?.baslik || ''
  const gosterilenBolum = bolumler.find(g => g.baslik === gosterilenBaslik)
  const homeAktif = !!homeItem && (konum.pathname === '/' )

  const RayIkon = ({ d, className = 'w-[22px] h-[22px]' }: { d: string; className?: string }) => (
    <svg className={className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7} aria-hidden>
      <path strokeLinecap="round" strokeLinejoin="round" d={d} />
    </svg>
  )

  return (
    <div className="min-h-screen flex items-start bg-gray-50 dark:bg-dark-900">
      {/* Mobil perde — yalnız çekmece açıkken ve < lg genişlikte */}
      {mobilAcik && (
        <div
          className="fixed inset-0 z-40 bg-dark-800/50 lg:hidden"
          onClick={() => setMobilAcik(false)}
          aria-hidden
        />
      )}

      {/*
        Iki katman: [ikon rayi 72px] + [etiketli panel 248px].
        < lg : ekran disina kaydirilmis cekmece (hamburger ile acilir)
        >= lg: yapiskan
      */}
      <div
        id="gosp-kenar-cubugu"
        className={`fixed inset-y-0 left-0 z-50 flex h-screen transform transition-transform duration-200 ease-out ${
          mobilAcik ? 'translate-x-0' : '-translate-x-full'
        } lg:sticky lg:top-0 lg:translate-x-0 lg:self-start`}
      >
        {/* ── IKON RAYI ── */}
        <aside className="w-[68px] shrink-0 h-screen flex flex-col items-center bg-white dark:bg-dark-900 border-r border-slate-200 dark:border-dark-600 py-3 gap-1">
          {/* Ray isareti — KARE bir simge.
               Burada LOGO KULLANILMAZ. Marka logosu cogunlukla YATAYdir;
              onu 36x36'lik raya sikistirmak taninmaz bir lekeye ceviriyordu.
              Kare olmasi beklenen yuva favicon'dur; logo ise yandaki genis
              baslikta kendi en-boy oraniyla durur. Favicon yuklenmemisse
              panelin kendi isareti kalir. */}
          <div className="h-[46px] flex items-center justify-center mb-1">
            {faviconURL(marka) ? (
              <img src={faviconURL(marka) as string} alt="" aria-hidden className="w-9 h-9 rounded-lg object-contain" />
            ) : (
              <div className="w-9 h-9 rounded-lg bg-brand-600 flex items-center justify-center shadow-sm shadow-brand-600/40">
                <svg viewBox="0 0 32 32" className="w-[18px] h-[18px] text-white" fill="currentColor">
                  <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
                </svg>
              </div>
            )}
          </div>

          {/* Anasayfa — dogrudan link */}
          {homeItem && (
            <NavLink
              to={homeItem.to}
              end
              onClick={() => { setAktifBolum(GENEL); setMobilAcik(false) }}
              title={cevir('Anasayfa')}
              aria-label={cevir('Anasayfa')}
              className={`w-11 h-11 flex items-center justify-center rounded-lg transition ${
                homeAktif ? 'bg-brand-600 text-white shadow-sm shadow-brand-600/30'
                  : 'text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-white/5 hover:text-slate-800 dark:hover:text-slate-200'
              }`}
            >
              <RayIkon d={homeItem.ikon} />
            </NavLink>
          )}

          {/* Bolum ikonlari */}
          <div className="flex-1 flex flex-col items-center gap-1 mt-1 overflow-y-auto no-scrollbar">
            {bolumler.filter(g => g.baslik !== GENEL).map(g => {
              const secili = g.baslik === gosterilenBaslik
              return (
                <button
                  key={g.baslik}
                  type="button"
                  onClick={() => {
                    // Ray ikonu = o bölüme GİR: panel açılır + sayfa gider + ray
                    // vurgusu senkronlaşır (rota useEffect'i aktifBolum'ü zaten
                    // rotaya çeker). Zaten o bölümdeysen sayfayı DEĞİŞTİRME (mevcut
                    // alt sayfada kal), yalnız paneli aç/koru.
                    setAktifBolum(g.baslik!)
                    const ilk = g.items[0]
                    const zatenBurada = g.items.some(it => konum.pathname === it.to || konum.pathname.startsWith(it.to + '/'))
                    if (ilk && !zatenBurada) yonlendir(ilk.to)
                    setMobilAcik(false)
                  }}
                  title={cevir(g.baslik!)}
                  aria-label={cevir(g.baslik!)}
                  aria-current={secili ? 'true' : undefined}
                  className={`relative w-11 h-11 flex items-center justify-center rounded-lg transition ${
                    secili ? 'bg-brand-50 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300'
                      : 'text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-white/5 hover:text-slate-800 dark:hover:text-slate-200'
                  }`}
                >
                  {secili && <span className="absolute left-0 top-2 bottom-2 w-0.5 rounded-r bg-brand-600 dark:bg-brand-400" aria-hidden />}
                  <RayIkon d={BOLUM_IKON[g.baslik!] || ICONS.araclar} />
                </button>
              )
            })}
          </div>
        </aside>

        {/* ── ETIKETLI PANEL ── */}
        <div className="w-[248px] shrink-0 h-screen flex flex-col bg-white dark:bg-dark-800 border-r border-slate-200 dark:border-dark-600">
          {/* Baslik: panel adi + kapat (mobil) */}
          <div className="h-[60px] flex items-center px-5 border-b border-slate-200 dark:border-dark-600">
            {/* MARKA LOGOSU BURADA — genis basligin yeri.
                Logo kendi en-boy oraniyla yatay durur; yuksekligi baslik
                seridine sigacak sekilde sinirlanir. Logo yoksa panel adi
                yazilir ki markasiz kurulumda serit bos kalmasin. */}
            {/* Marka alani ANASAYFA BAGLANTISIDIR.
                Onceki hali olu bir gorseldi; "logoya tikla -> anasayfa" neredeyse
                evrensel bir beklenti ve karsilanmayinca kullanici tiklayip hicbir
                sey olmamasiyla karsilasiyordu.
                Erisilebilir adi LINK tasir (aria-label); logo gorseli dekoratif
                sayilir (alt="") -- aksi halde ekran okuyucu ayni adi iki kez
                okurdu. Odak halkasi gorunur. */}
            <NavLink
              to="/"
              end
              onClick={() => setMobilAcik(false)}
              aria-label={marka.panel_adi}
              className="flex min-w-0 items-center rounded-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-transparent focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800"
            >
              {logoURL(marka) ? (
                <img
                  src={logoURL(marka) as string}
                  alt=""
                  aria-hidden
                  className="max-h-9 w-auto max-w-[168px] object-contain"
                />
              ) : (
                <span className="truncate text-[17px] font-semibold text-slate-900 dark:text-slate-100">
                  {marka.panel_adi}
                </span>
              )}
            </NavLink>
            <button
              onClick={() => setMobilAcik(false)}
              className="ml-auto -mr-2 p-2 text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 rounded-md transition lg:hidden"
              aria-label={cevir('Menüyü kapat')}
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          {/* Aktif bolumun etiketli ogeleri */}
          <nav className="flex-1 px-2.5 py-3 overflow-y-auto">
            <div className="px-2.5 pb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
              {cevir(gosterilenBaslik)}
            </div>
            <ul className="space-y-0.5">
              {gosterilenBolum?.items.map(it => {
                const ustPath = gosterilenBolum.items.some(
                  it2 => it2.to !== it.to && it2.to.startsWith(it.to + '/')
                )
                return (
                  <li key={it.to}>
                    <NavLink
                      to={it.to}
                      end={it.to === '/' || ustPath}
                      onClick={() => setMobilAcik(false)}
                      className={({ isActive }) =>
                        `group relative flex items-center px-3 py-2 rounded-lg text-[14px] transition-all duration-150 ${
                          isActive
                            ? 'bg-brand-50 dark:bg-brand-500/10 text-brand-700 dark:text-brand-300 font-medium'
                            : 'text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-white/5 hover:text-slate-900 dark:hover:text-slate-100'
                        }`
                      }
                    >
                      {({ isActive }) => (
                        <>
                          {isActive && (
                            <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-r bg-brand-600 dark:bg-brand-400" aria-hidden />
                          )}
                          <svg className={`w-[18px] h-[18px] mr-3 shrink-0 transition ${
                            isActive ? 'text-brand-600 dark:text-brand-400' : 'text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:group-hover:text-slate-300'
                          }`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                            <path strokeLinecap="round" strokeLinejoin="round" d={it.ikon} />
                          </svg>
                          <span className="whitespace-nowrap truncate">{cevir(it.etiket)}</span>
                          {it.sayac && sayac[it.sayac] ? (
                            <span
                              className={`ml-auto pl-2 shrink-0 text-[13px] tabular-nums transition ${
                                isActive
                                  ? 'text-brand-700 dark:text-brand-300 font-medium'
                                  : 'text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:group-hover:text-slate-300'
                              }`}
                              aria-label={`${sayac[it.sayac]} ${cevir('adet')}`}
                            >
                              {sayac[it.sayac]}
                            </span>
                          ) : null}
                          {it.rozet ? (
                            <span className="ml-auto shrink-0 text-[10px] font-semibold uppercase tracking-wide px-1.5 py-0.5 rounded bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">{cevir(it.rozet)}</span>
                          ) : null}
                        </>
                      )}
                    </NavLink>
                  </li>
                )
              })}
            </ul>
          </nav>
        </div>
      </div>

      <div className="flex-1 flex flex-col min-w-0">
        <TopBar onMenuAc={() => setMobilAcik(true)} menuAcik={mobilAcik} />
        {/* Alt gezinme çubuğu (AltNavBar) mobilde ekranın dibine sabitlenir;
            altında kalan boşluğu akışın SON ögesi ayırır. Alt bilgi
            çiziliyorsa o son öge footer'dır — ikisi birden ayırırsa boşluk
            iki katına çıkar. */}
        <main className={`flex-1 min-w-0 lg:pb-0 ${marka.footer ? '' : 'pb-[calc(4rem+env(safe-area-inset-bottom))]'}`}>
          {/* 🔴 IC SUSPENSE SINIRI. React en YAKIN siniri kullanir; bu olmasaydi
              rota degisimlerinde App icindeki dis sinir devreye girer ve KABUK
              (sidebar + topbar) da sokulup yeniden cizilirdi. Boylece yalnizca
              icerik alani bekler. */}
          <Suspense fallback={<IcerikYukleniyor />}>
            <Outlet />
          </Suspense>
        </main>
        {/* MARKA ALT BİLGİSİ (whitelabel). Boşsa HİÇ çizilmez: boş bir şerit,
            markası ayarlanmamış panelde gereksiz gürültü olurdu. Giriş
            sayfasındaki `giris_dipnot`tan ayrıdır — bu her sayfada görünür. */}
        {marka.footer && (
          <footer className="border-t border-slate-200 px-4 py-3 pb-[calc(0.75rem+4rem+env(safe-area-inset-bottom))] text-center text-xs text-slate-500 dark:border-dark-600 dark:text-slate-400 lg:pb-3">
            {marka.footer}
          </footer>
        )}
      </div>

      <AltNavBar onMenuAc={() => setMobilAcik(true)} />
      <HataYuzeyi />
    </div>
  )
}
