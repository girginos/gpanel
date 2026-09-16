import { Suspense, lazy } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from '@/store/auth'
const LoginPage = lazy(() => import('@/pages/LoginPage'))
import DashboardLayout from '@/components/DashboardLayout'
const HomePage = lazy(() => import('@/pages/HomePage'))
const DomainsPage = lazy(() => import('@/pages/DomainsPage'))
const ResellerlarPage = lazy(() => import('@/pages/ResellerlarPage'))
const DenetimPage = lazy(() => import('@/pages/DenetimPage'))
const BildirimlerPage = lazy(() => import('@/pages/BildirimlerPage'))
const SaldiriZincirleriPage = lazy(() => import('@/pages/SaldiriZincirleriPage'))
const YasakliDomainPage = lazy(() => import('@/pages/YasakliDomainPage'))
const OturumAyarPage = lazy(() => import('@/pages/OturumAyarPage'))
const PanelHostPage = lazy(() => import('@/pages/PanelHostPage'))
const IPYonetimPage = lazy(() => import('@/pages/IPYonetimPage'))
const PortYonetimPage = lazy(() => import('@/pages/PortYonetimPage'))
const WebsiteSecurityPage = lazy(() => import('@/pages/WebsiteSecurityPage'))
const DomainSecurityPage = lazy(() => import('@/pages/DomainSecurityPage'))
const BayiPlaniDuzenlePage = lazy(() => import('@/pages/BayiPlaniDuzenlePage'))
const BayiPlanlariPage = lazy(() => import('@/pages/BayiPlanlariPage'))
const SubscriptionDetailPage = lazy(() => import('@/pages/SubscriptionDetailPage'))
const ServicePlansPage = lazy(() => import('@/pages/ServicePlansPage'))
const SettingsPage = lazy(() => import('@/pages/SettingsPage'))
const PlaceholderPage = lazy(() => import('@/pages/PlaceholderPage'))
const ToolPage = lazy(() => import('@/pages/ToolPage'))
const DomainFilesPage = lazy(() => import('@/pages/DomainFilesPage'))
const DomainSSLPage = lazy(() => import('@/pages/DomainSSLPage'))
const DomainMailAyarlarPage = lazy(() => import('@/pages/DomainMailAyarlarPage'))
const DomainMailKutularPage = lazy(() => import('@/pages/DomainMailKutularPage'))
const MailKutuDetayPage = lazy(() => import('@/pages/MailKutuDetayPage'))
const DomainMailTeslimatPage = lazy(() => import('@/pages/DomainMailTeslimatPage'))
const DomainMailAliasPage = lazy(() => import('@/pages/DomainMailAliasPage'))
const MailSunucuPage = lazy(() => import('@/pages/MailSunucuPage'))
const DomainSSHPage = lazy(() => import('@/pages/DomainSSHPage'))
const DomainStatsPage = lazy(() => import('@/pages/DomainStatsPage'))
const DomainPerformansPage = lazy(() => import('@/pages/DomainPerformansPage'))
const DomainComposerPage = lazy(() => import('@/pages/DomainComposerPage'))
const DomainSifreKorumaPage = lazy(() => import('@/pages/DomainSifreKorumaPage'))
const DomainAntivirusPage = lazy(() => import('@/pages/DomainAntivirusPage'))
const AntivirusPanel = lazy(() => import('@/pages/AntivirusPanel'))
const DomainKopyaPage = lazy(() => import('@/pages/DomainKopyaPage'))
const DomainCronPage = lazy(() => import('@/pages/DomainCronPage'))
const DomainLogsPage = lazy(() => import('@/pages/DomainLogsPage'))
const DomainDNSPage = lazy(() => import('@/pages/DomainDNSPage'))
const RedisPage = lazy(() => import('@/pages/RedisPage'))
const DomainConnectionPage = lazy(() => import('@/pages/DomainConnectionPage'))
const DomainDatabasesPage = lazy(() => import('@/pages/DomainDatabasesPage'))
const DomainDatabaseDetailPage = lazy(() => import('@/pages/DomainDatabaseDetailPage'))
const DomainFTPPage = lazy(() => import('@/pages/DomainFTPPage'))
const DomainPHPPage = lazy(() => import('@/pages/DomainPHPPage'))
const DomainPlanPage = lazy(() => import('@/pages/DomainPlanPage'))
const DomainBackupsPage = lazy(() => import('@/pages/DomainBackupsPage'))
const DomainGitPage = lazy(() => import('@/pages/DomainGitPage'))
const DomainWebSunucuPage = lazy(() => import('@/pages/DomainWebSunucuPage'))
const DomainLaravelPage = lazy(() => import('@/pages/DomainLaravelPage'))
const DomainWafPage = lazy(() => import('@/pages/DomainWafPage'))
const DomainErisimPage = lazy(() => import('@/pages/DomainErisimPage'))
const PHPModuleriPage = lazy(() => import('@/pages/PHPModuleriPage'))
const PHPSunucuSihirbaziPage = lazy(() => import('@/pages/PHPSunucuSihirbaziPage'))
const PaketlerPage = lazy(() => import('@/pages/PaketlerPage'))
const PaketDetayPage = lazy(() => import('@/pages/PaketDetayPage'))
const AraclarAyarlarPage = lazy(() => import('@/pages/AraclarAyarlarPage'))
const DNSSablonuPage = lazy(() => import('@/pages/DNSSablonuPage'))
const ServislerPage = lazy(() => import('@/pages/ServislerPage'))
const IstatistiklerPage = lazy(() => import('@/pages/IstatistiklerPage'))
const PanelGuncellemePage = lazy(() => import('@/pages/PanelGuncellemePage'))
const SunucuOptimizePage = lazy(() => import('@/pages/SunucuOptimizePage'))
const SiteTasimaPage = lazy(() => import('@/pages/SiteTasimaPage'))
const EklentilerPage = lazy(() => import('@/pages/EklentilerPage'))
const WordPressPage = lazy(() => import('@/pages/WordPressPage'))
const FirewallPage = lazy(() => import('@/pages/FirewallPage'))
const BackupYonetimiPage = lazy(() => import('@/pages/BackupYonetimiPage'))
const BackupJobDetayPage = lazy(() => import('@/pages/BackupJobDetayPage'))
const DomainWordPressPage = lazy(() => import('@/pages/DomainWordPressPage'))
const DomainSubdomainlerPage = lazy(() => import('@/pages/DomainSubdomainlerPage'))
const DomainSubdomainYonetPage = lazy(() => import('@/pages/DomainSubdomainYonetPage'))
const CPanelGirisPage = lazy(() => import('@/pages/CPanelGirisPage'))
const IzlemePage = lazy(() => import('@/pages/IzlemePage'))
const YakindaPage = lazy(() => import('@/pages/YakindaPage'))
const TehditPage = lazy(() => import('@/pages/TehditPage'))
const MarkaPage = lazy(() => import('@/pages/MarkaPage'))
function GuardedRoute({ children }: { children: React.ReactNode }) {
  const token = useAuth((s) => s.token)
  const loc = useLocation()
  if (!token) {
    // Gelmek istenen yolu koru; giriş sonrası oraya DÖN (login'de bırakma).
    const hedef = loc.pathname + loc.search
    const next = hedef && hedef !== '/' ? `?next=${encodeURIComponent(hedef)}` : ''
    return <Navigate to={`/giris${next}`} replace />
  }
  return <>{children}</>
}

// Parca yuklenirken gosterilen ara ekran. Sade tutuldu: kabuk zaten ayakta,
// yalnizca icerik alani icin kisa bir bekleme.
function SayfaYukleniyor() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600 dark:border-dark-600 dark:border-t-brand-400" />
    </div>
  )
}

export default function App() {
  return (
    <Suspense fallback={<SayfaYukleniyor />}>
    <Routes>
      <Route path="/giris" element={<LoginPage />} />
        <Route path="/cp/giris" element={<CPanelGirisPage />} />
        <Route path="/cp" element={<CPanelGirisPage />} />
      <Route
        path="/"
        element={
          <GuardedRoute>
            <DashboardLayout />
          </GuardedRoute>
        }
      >
        <Route index                       element={<HomePage />} />
        <Route path="domainler"            element={<DomainsPage />} />
        <Route path="bayiler"              element={<ResellerlarPage />} />
        <Route path="denetim"              element={<DenetimPage />} />
        <Route path="bildirimler"         element={<BildirimlerPage />} />
        <Route path="saldiri-zincirleri"  element={<SaldiriZincirleriPage />} />
        <Route path="abonelikler"          element={<Navigate to="/domainler" replace />} />
        <Route path="abonelikler/:id"      element={<SubscriptionDetailPage />} />
        <Route path="abonelikler/:id/baglanti"      element={<DomainConnectionPage />} />
        <Route path="abonelikler/:id/dosyalar"      element={<DomainFilesPage />} />
        <Route path="abonelikler/:id/veritabanlari" element={<DomainDatabasesPage />} />
        <Route path="abonelikler/:id/veritabanlari/:dbid" element={<DomainDatabaseDetailPage />} />
        <Route path="abonelikler/:id/ftp"           element={<DomainFTPPage />} />
        <Route path="abonelikler/:id/php"           element={<DomainPHPPage />} />
        <Route path="abonelikler/:id/plan"          element={<DomainPlanPage />} />
        <Route path="abonelikler/:id/ssl"           element={<DomainSSLPage />} />
        <Route path="abonelikler/:id/mail/ayarlar"  element={<DomainMailAyarlarPage />} />
        <Route path="abonelikler/:id/mail/kutular"  element={<DomainMailKutularPage />} />
        <Route path="abonelikler/:id/mail/kutular/:kutuId" element={<MailKutuDetayPage />} />
        <Route path="abonelikler/:id/mail/teslimat" element={<DomainMailTeslimatPage />} />
        <Route path="abonelikler/:id/mail/takmaadlar" element={<DomainMailAliasPage />} />
        <Route path="abonelikler/:id/ssh-erisim"    element={<DomainSSHPage />} />
        <Route path="abonelikler/:id/istatistik"    element={<DomainStatsPage />} />
        <Route path="abonelikler/:id/performans"    element={<DomainPerformansPage />} />
        <Route path="abonelikler/:id/composer"      element={<DomainComposerPage />} />
        <Route path="abonelikler/:id/sifre-koruma"  element={<DomainSifreKorumaPage />} />
        <Route path="abonelikler/:id/imunify"       element={<DomainAntivirusPage />} />
        <Route path="abonelikler/:id/kopyala"       element={<DomainKopyaPage />} />
        <Route path="abonelikler/:id/wordpress"     element={<DomainWordPressPage />} />
        <Route path="abonelikler/:id/subdomainler"  element={<DomainSubdomainlerPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid" element={<DomainSubdomainYonetPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/wordpress" element={<DomainWordPressPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/composer" element={<DomainComposerPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/gunlukler" element={<DomainLogsPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/erisim" element={<DomainErisimPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/dosyalar" element={<DomainFilesPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/istatistik" element={<DomainStatsPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid/sifre-koruma" element={<DomainSifreKorumaPage />} />
        <Route path="abonelikler/:id/cron"          element={<DomainCronPage />} />
        <Route path="abonelikler/:id/gunlukler"     element={<DomainLogsPage />} />
        <Route path="abonelikler/:id/dns"           element={<DomainDNSPage />} />
        <Route path="abonelikler/:id/redis"         element={<RedisPage />} />
        <Route path="abonelikler/:id/yedekler"      element={<DomainBackupsPage />} />
        <Route path="abonelikler/:id/git"           element={<DomainGitPage />} />
        <Route path="abonelikler/:id/web-sunucu"    element={<DomainWebSunucuPage />} />
        <Route path="abonelikler/:id/laravel" element={<DomainLaravelPage />} />
        <Route path="abonelikler/:id/waf"           element={<DomainWafPage />} />
        <Route path="abonelikler/:id/erisim"        element={<DomainErisimPage />} />
        <Route path="php-sunucu-sihirbazi"           element={<PHPSunucuSihirbaziPage />} />
        {/* Eski dağınık sayfalar tek sihirbaza yönlendirir (kullanıcı isteği). */}
        <Route path="sistem/php-modulleri"           element={<Navigate to="/php-sunucu-sihirbazi" replace />} />
        <Route path="_eski/php-modulleri"            element={<PHPModuleriPage />} />
        <Route path="araclar/paketler"               element={<PaketlerPage />} />
        <Route path="araclar/paketler/:id"           element={<PaketDetayPage />} />
        <Route path="araclar/php-surumler"           element={<Navigate to="/php-sunucu-sihirbazi" replace />} />
        <Route path="araclar/servisler"              element={<ServislerPage />} />
        <Route path="araclar/dns-sablonu"            element={<DNSSablonuPage />} />
        <Route path="araclar/guncelleme" element={<PanelGuncellemePage />} />
        <Route path="araclar/optimize" element={<SunucuOptimizePage />} />
        <Route path="araclar/tasima" element={<SiteTasimaPage />} />
        <Route path="araclar/yasakli-domain" element={<YasakliDomainPage />} />
        <Route path="araclar/oturum-guvenligi" element={<OturumAyarPage />} />
        <Route path="araclar/panel-hostname" element={<PanelHostPage />} />
        <Route path="araclar/ip-yonetimi" element={<IPYonetimPage />} />
        <Route path="araclar/port-degistirme" element={<PortYonetimPage />} />
        <Route path="website-security" element={<WebsiteSecurityPage />} />
        <Route path="website-security/domain/:id" element={<DomainSecurityPage />} />
        <Route path="antivirus" element={<AntivirusPanel />} />
        <Route path="abonelikler/:id/:slug" element={<ToolPage />} />
        <Route path="hizmet-planlari"      element={<ServicePlansPage />} />
        <Route path="bayi-planlari"        element={<BayiPlanlariPage />} />
        <Route path="bayi-planlari/yeni"   element={<BayiPlaniDuzenlePage />} />
        <Route path="bayi-planlari/:id"    element={<BayiPlaniDuzenlePage />} />

        <Route path="araclar-ayarlar" element={<AraclarAyarlarPage />} />
        <Route path="eklentiler" element={<EklentilerPage />} />
        <Route path="tehdit" element={<TehditPage />} />
        <Route path="marka" element={<MarkaPage />} />
        <Route path="eklentiler/:slug" element={<EklentilerPage />} />
        <Route path="wordpress" element={<WordPressPage />} />
        <Route path="firewall" element={<FirewallPage />} />
        <Route path="backup-yonetimi" element={<BackupYonetimiPage />} />
        <Route path="backup-yonetimi/is/:jid" element={<BackupJobDetayPage />} />
        <Route path="izleme" element={<IzlemePage />} />
        <Route path="istatistikler" element={<IstatistiklerPage />} />
        <Route path="mail-sunucu" element={<MailSunucuPage />} />
        <Route path="mail-sunucu-ayarlari" element={<Navigate to="/mail-sunucu?sekme=genel" replace />} />
        <Route path="mail-ip-havuzu" element={<Navigate to="/mail-sunucu?sekme=ip-havuzu" replace />} />

        <Route path="profil"          element={<SettingsPage />} />
        <Route path="parola-degistir" element={<Navigate to="/profil" replace />} />
        <Route path="ayarlar"         element={<Navigate to="/profil" replace />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
    </Suspense>
  )
}
