import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from '@/store/auth'
import LoginPage from '@/pages/LoginPage'
import DashboardLayout from '@/components/DashboardLayout'
import HomePage from '@/pages/HomePage'
import DomainsPage from '@/pages/DomainsPage'
import SubscriptionDetailPage from '@/pages/SubscriptionDetailPage'
import ServicePlansPage from '@/pages/ServicePlansPage'
import SettingsPage from '@/pages/SettingsPage'
import PlaceholderPage from '@/pages/PlaceholderPage'
import ToolPage from '@/pages/ToolPage'
import DomainFilesPage from '@/pages/DomainFilesPage'
import DomainSSLPage from '@/pages/DomainSSLPage'
import DomainSSHPage from '@/pages/DomainSSHPage'
import DomainStatsPage from '@/pages/DomainStatsPage'
import DomainPerformansPage from '@/pages/DomainPerformansPage'
import DomainComposerPage from '@/pages/DomainComposerPage'
import DomainSifreKorumaPage from '@/pages/DomainSifreKorumaPage'
import DomainAntivirusPage from '@/pages/DomainAntivirusPage'
import DomainKopyaPage from '@/pages/DomainKopyaPage'
import DomainCronPage from '@/pages/DomainCronPage'
import DomainLogsPage from '@/pages/DomainLogsPage'
import DomainDNSPage from '@/pages/DomainDNSPage'
import RedisPage from '@/pages/RedisPage'
import DomainConnectionPage from '@/pages/DomainConnectionPage'
import DomainDatabasesPage from '@/pages/DomainDatabasesPage'
import DomainFTPPage from '@/pages/DomainFTPPage'
import DomainPHPPage from '@/pages/DomainPHPPage'
import DomainBackupsPage from '@/pages/DomainBackupsPage'
import DomainGitPage from '@/pages/DomainGitPage'
import DomainWebSunucuPage from '@/pages/DomainWebSunucuPage'
import PHPModuleriPage from '@/pages/PHPModuleriPage'
import PaketlerPage from '@/pages/PaketlerPage'
import PaketDetayPage from '@/pages/PaketDetayPage'
import PHPSurumleriPage from '@/pages/PHPSurumleriPage'
import AraclarAyarlarPage from '@/pages/AraclarAyarlarPage'
import ServislerPage from '@/pages/ServislerPage'
import WordPressPage from '@/pages/WordPressPage'
import FirewallPage from '@/pages/FirewallPage'
import BackupYonetimiPage from '@/pages/BackupYonetimiPage'
import DomainWordPressPage from '@/pages/DomainWordPressPage'
import DomainSubdomainlerPage from '@/pages/DomainSubdomainlerPage'
import CPanelGirisPage from '@/pages/CPanelGirisPage'
import IstatistiklerPage from '@/pages/IstatistiklerPage'
import IzlemePage from '@/pages/IzlemePage'
import YakindaPage from '@/pages/YakindaPage'

function GuardedRoute({ children }: { children: React.ReactNode }) {
  const token = useAuth((s) => s.token)
  if (!token) return <Navigate to="/giris" replace />
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
        <Route path="abonelikler"          element={<Navigate to="/domainler" replace />} />
        <Route path="abonelikler/:id"      element={<SubscriptionDetailPage />} />
        <Route path="abonelikler/:id/baglanti"      element={<DomainConnectionPage />} />
        <Route path="abonelikler/:id/dosyalar"      element={<DomainFilesPage />} />
        <Route path="abonelikler/:id/veritabanlari" element={<DomainDatabasesPage />} />
        <Route path="abonelikler/:id/ftp"           element={<DomainFTPPage />} />
        <Route path="abonelikler/:id/php"           element={<DomainPHPPage />} />
        <Route path="abonelikler/:id/ssl"           element={<DomainSSLPage />} />
        <Route path="abonelikler/:id/ssh-erisim"    element={<DomainSSHPage />} />
        <Route path="abonelikler/:id/istatistik"    element={<DomainStatsPage />} />
        <Route path="abonelikler/:id/performans"    element={<DomainPerformansPage />} />
        <Route path="abonelikler/:id/composer"      element={<DomainComposerPage />} />
        <Route path="abonelikler/:id/sifre-koruma"  element={<DomainSifreKorumaPage />} />
        <Route path="abonelikler/:id/imunify"       element={<DomainAntivirusPage />} />
        <Route path="abonelikler/:id/kopyala"       element={<DomainKopyaPage />} />
        <Route path="abonelikler/:id/wordpress"     element={<DomainWordPressPage />} />
        <Route path="abonelikler/:id/subdomainler"  element={<DomainSubdomainlerPage />} />
        <Route path="abonelikler/:id/cron"          element={<DomainCronPage />} />
        <Route path="abonelikler/:id/gunlukler"     element={<DomainLogsPage />} />
        <Route path="abonelikler/:id/dns"           element={<DomainDNSPage />} />
        <Route path="abonelikler/:id/redis"         element={<RedisPage />} />
        <Route path="abonelikler/:id/yedekler"      element={<DomainBackupsPage />} />
        <Route path="abonelikler/:id/git"           element={<DomainGitPage />} />
        <Route path="abonelikler/:id/web-sunucu"    element={<DomainWebSunucuPage />} />
        <Route path="sistem/php-modulleri"           element={<PHPModuleriPage />} />
        <Route path="araclar/paketler"               element={<PaketlerPage />} />
        <Route path="araclar/paketler/:id"           element={<PaketDetayPage />} />
        <Route path="araclar/php-surumler"           element={<PHPSurumleriPage />} />
        <Route path="araclar/servisler"              element={<ServislerPage />} />
        <Route path="abonelikler/:id/:slug" element={<ToolPage />} />
        <Route path="hizmet-planlari"      element={<ServicePlansPage />} />

        <Route path="araclar-ayarlar" element={<AraclarAyarlarPage />} />
        <Route path="istatistikler" element={<IstatistiklerPage />} />
        <Route path="eklentiler" element={<YakindaPage baslik="Eklentiler" ikon="🧩" aciklama="Panel için 3. parti eklenti yönetimi" ozellikler={["Marketplace gezinme","Tek tıkla kur/kaldır","Sürüm güncelleme","API entegrasyonu","Geliştirici SDK"]} />} />
        <Route path="wordpress" element={<WordPressPage />} />
        <Route path="firewall" element={<FirewallPage />} />
        <Route path="backup-yonetimi" element={<BackupYonetimiPage />} />
        <Route path="izleme" element={<IzlemePage />} />

        <Route path="profil"          element={<SettingsPage />} />
        <Route path="parola-degistir" element={<Navigate to="/profil" replace />} />
        <Route path="ayarlar"         element={<Navigate to="/profil" replace />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
