import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from '@/store/auth'
import LoginPage from '@/pages/LoginPage'
import DashboardLayout from '@/components/DashboardLayout'
import HomePage from '@/pages/HomePage'
import DomainsPage from '@/pages/DomainsPage'
import ResellerlarPage from '@/pages/ResellerlarPage'
import DenetimPage from '@/pages/DenetimPage'
import BildirimlerPage from '@/pages/BildirimlerPage'
import SaldiriZincirleriPage from '@/pages/SaldiriZincirleriPage'
import YasakliDomainPage from '@/pages/YasakliDomainPage'
import OturumAyarPage from '@/pages/OturumAyarPage'
import PanelHostPage from '@/pages/PanelHostPage'
import IPYonetimPage from '@/pages/IPYonetimPage'
import PortYonetimPage from '@/pages/PortYonetimPage'
import WebsiteSecurityPage from '@/pages/WebsiteSecurityPage'
import DomainSecurityPage from '@/pages/DomainSecurityPage'
import BayiPlaniDuzenlePage from "@/pages/BayiPlaniDuzenlePage"
import BayiPlanlariPage from '@/pages/BayiPlanlariPage'
import SubscriptionDetailPage from '@/pages/SubscriptionDetailPage'
import ServicePlansPage from '@/pages/ServicePlansPage'
import SettingsPage from '@/pages/SettingsPage'
import PlaceholderPage from '@/pages/PlaceholderPage'
import ToolPage from '@/pages/ToolPage'
import DomainFilesPage from '@/pages/DomainFilesPage'
import DomainSSLPage from '@/pages/DomainSSLPage'
import DomainMailAyarlarPage from '@/pages/DomainMailAyarlarPage'
import DomainMailKutularPage from '@/pages/DomainMailKutularPage'
import MailKutuDetayPage from "@/pages/MailKutuDetayPage"
import DomainMailTeslimatPage from '@/pages/DomainMailTeslimatPage'
import DomainMailAliasPage from '@/pages/DomainMailAliasPage'
import MailSunucuPage from "@/pages/MailSunucuPage"
import DomainSSHPage from '@/pages/DomainSSHPage'
import DomainStatsPage from '@/pages/DomainStatsPage'
import DomainPerformansPage from '@/pages/DomainPerformansPage'
import DomainComposerPage from '@/pages/DomainComposerPage'
import DomainSifreKorumaPage from '@/pages/DomainSifreKorumaPage'
import DomainAntivirusPage from '@/pages/DomainAntivirusPage'
import AntivirusPanel from '@/pages/AntivirusPanel'
import DomainKopyaPage from '@/pages/DomainKopyaPage'
import DomainCronPage from '@/pages/DomainCronPage'
import DomainLogsPage from '@/pages/DomainLogsPage'
import DomainDNSPage from '@/pages/DomainDNSPage'
import RedisPage from '@/pages/RedisPage'
import DomainConnectionPage from '@/pages/DomainConnectionPage'
import DomainDatabasesPage from '@/pages/DomainDatabasesPage'
import DomainDatabaseDetailPage from '@/pages/DomainDatabaseDetailPage'
import DomainFTPPage from '@/pages/DomainFTPPage'
import DomainPHPPage from '@/pages/DomainPHPPage'
import DomainPlanPage from '@/pages/DomainPlanPage'
import DomainBackupsPage from '@/pages/DomainBackupsPage'
import DomainGitPage from '@/pages/DomainGitPage'
import DomainWebSunucuPage from '@/pages/DomainWebSunucuPage'
import DomainLaravelPage from '@/pages/DomainLaravelPage'
import DomainWafPage from '@/pages/DomainWafPage'
import PHPModuleriPage from '@/pages/PHPModuleriPage'
import PHPSunucuSihirbaziPage from '@/pages/PHPSunucuSihirbaziPage'
import PaketlerPage from '@/pages/PaketlerPage'
import PaketDetayPage from '@/pages/PaketDetayPage'
import AraclarAyarlarPage from '@/pages/AraclarAyarlarPage'
import DNSSablonuPage from '@/pages/DNSSablonuPage'
import ServislerPage from '@/pages/ServislerPage'
import PanelGuncellemePage from '@/pages/PanelGuncellemePage'
import SunucuOptimizePage from '@/pages/SunucuOptimizePage'
import SiteTasimaPage from '@/pages/SiteTasimaPage'
import EklentilerPage from '@/pages/EklentilerPage'
import WordPressPage from '@/pages/WordPressPage'
import FirewallPage from '@/pages/FirewallPage'
import BackupYonetimiPage from '@/pages/BackupYonetimiPage'
import BackupJobDetayPage from '@/pages/BackupJobDetayPage'
import DomainWordPressPage from '@/pages/DomainWordPressPage'
import DomainSubdomainlerPage from '@/pages/DomainSubdomainlerPage'
import DomainSubdomainYonetPage from '@/pages/DomainSubdomainYonetPage'
import CPanelGirisPage from '@/pages/CPanelGirisPage'
import IstatistiklerPage from '@/pages/IstatistiklerPage'
import IzlemePage from '@/pages/IzlemePage'
import YakindaPage from '@/pages/YakindaPage'

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

export default function App() {
  return (
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
        <Route path="istatistikler" element={<IstatistiklerPage />} />
        <Route path="eklentiler" element={<EklentilerPage />} />
        <Route path="eklentiler/:slug" element={<EklentilerPage />} />
        <Route path="wordpress" element={<WordPressPage />} />
        <Route path="firewall" element={<FirewallPage />} />
        <Route path="backup-yonetimi" element={<BackupYonetimiPage />} />
        <Route path="backup-yonetimi/is/:jid" element={<BackupJobDetayPage />} />
        <Route path="izleme" element={<IzlemePage />} />
        <Route path="mail-sunucu" element={<MailSunucuPage />} />
        <Route path="mail-sunucu-ayarlari" element={<Navigate to="/mail-sunucu?sekme=genel" replace />} />
        <Route path="mail-ip-havuzu" element={<Navigate to="/mail-sunucu?sekme=ip-havuzu" replace />} />

        <Route path="profil"          element={<SettingsPage />} />
        <Route path="parola-degistir" element={<Navigate to="/profil" replace />} />
        <Route path="ayarlar"         element={<Navigate to="/profil" replace />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
