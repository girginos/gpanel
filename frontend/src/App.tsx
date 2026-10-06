import { Suspense, lazy, useEffect } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth, depoBaskaOturumda, adinaDonusAl } from '@/store/auth'
import { guvenliDonus } from '@/lib/adina'
import { useHesapAski } from '@/store/hesapAski'
import HesapAskidaPage from '@/pages/HesapAskidaPage'
import { bayiAbonelikID, bayiYoluIzinli, bayiYonlendirme, girisYolu, musteriDomainID, musteriYoluIzinli } from '@/lib/rol'
import { useBayiAbonelikSahibi } from '@/lib/bayiAbonelik'
const LoginPage = lazy(() => import('@/pages/LoginPage'))
const AdinaGirisPage = lazy(() => import('@/pages/AdinaGirisPage'))
import DashboardLayout from '@/components/DashboardLayout'
import KapiGiris from '@/components/KapiGiris'
import SayfaKabugu, { KabukDisi, SayfaCercevesi } from '@/components/SayfaKabugu'
const HomePage = lazy(() => import('@/pages/HomePage'))
const DomainsPage = lazy(() => import('@/pages/DomainsPage'))
const ResellerlarPage = lazy(() => import('@/pages/ResellerlarPage'))
const DenetimPage = lazy(() => import('@/pages/DenetimPage'))
const ApiAnahtarlariPage = lazy(() => import('@/pages/ApiAnahtarlariPage'))
const BildirimlerPage = lazy(() => import('@/pages/BildirimlerPage'))
const SaldiriZincirleriPage = lazy(() => import('@/pages/SaldiriZincirleriPage'))
const YasakliDomainPage = lazy(() => import('@/pages/YasakliDomainPage'))
const OturumAyarPage = lazy(() => import('@/pages/OturumAyarPage'))
const PanelHostPage = lazy(() => import('@/pages/PanelHostPage'))
const IPYonetimPage = lazy(() => import('@/pages/IPYonetimPage'))
const PortYonetimPage = lazy(() => import('@/pages/PortYonetimPage'))
const WebsiteSecurityPage = lazy(() => import('@/pages/WebsiteSecurityPage'))
const DomainSecurityPage = lazy(() => import('@/pages/DomainSecurityPage'))
const MusteriGuvenlikPage = lazy(() => import('@/pages/MusteriGuvenlikPage'))
const BayiPlaniDuzenlePage = lazy(() => import('@/pages/BayiPlaniDuzenlePage'))
const BayiPlanlariPage = lazy(() => import('@/pages/BayiPlanlariPage'))
const SubscriptionDetailPage = lazy(() => import('@/pages/SubscriptionDetailPage'))
const MusteriAnaSayfa = lazy(() => import('@/pages/MusteriAnaSayfa'))
const DomainUygulamalarPage = lazy(() => import('@/pages/DomainUygulamalarPage'))
const DomainGelistiriciPage = lazy(() => import('@/pages/DomainGelistiriciPage'))
const ServicePlansPage = lazy(() => import('@/pages/ServicePlansPage'))
const SettingsPage = lazy(() => import('@/pages/SettingsPage'))
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
const EkAlanEklePage = lazy(() => import('@/pages/EkAlanEklePage'))
const CPanelGirisPage = lazy(() => import('@/pages/CPanelGirisPage'))
const IzlemePage = lazy(() => import('@/pages/IzlemePage'))
const TehditPage = lazy(() => import('@/pages/TehditPage'))
const MarkaPage = lazy(() => import('@/pages/MarkaPage'))
function GuardedRoute({ children }: { children: React.ReactNode }) {
  const token = useAuth((s) => s.token)
  const loc = useLocation()
  if (!token) {
    // Müşteri ADINA oturumun (bayi "panele gir") süresi doldu / kapandı: bu sekme giriş
    // formuna değil geldiği bayi paneline döner (bkz. store/auth KEY_ADINA_DON).
    const don = adinaDonusAl()
    if (don) {
      window.location.replace(guvenliDonus(don))
      return null
    }
    // Gelmek istenen yolu koru; giriş sonrası oraya DÖN (login'de bırakma).
    const hedef = loc.pathname + loc.search
    const next = hedef && hedef !== '/' ? `?next=${encodeURIComponent(hedef)}` : ''
    // Son oturum müşteriyse (çıkış, süre dolması, 401) ve hedef müşteriye ait
    // olabilirse müşteri giriş formu; yönetici/bayi yolları her zaman /giris.
    return <Navigate to={`${girisYolu(loc.pathname)}${next}`} replace />
  }
  return <>{children}</>
}

// 🔴 ROL KAPISI ('user' = hosting müşterisi, 'reseller' = bayi). Karar ROTA düzeyinde:
// sayfa bileşeni hiç mount edilmez (HomePage içinde <Navigate> dönmek yetmezdi — efekti
// yine koşar, 4 yönetici ucuna 403 attırırdı). Müşteride izin listesi dışındaki her yol
// (/, bilinmeyenler, /domainler, /abonelikler, başka abonelik…) kendi aboneliğine gider;
// bayide bkz. BayiKapisi. Yönetici için kapı şeffaftır — davranış değişmez.
function RolKapisi() {
  const rol = useAuth((s) => s.kullanici?.rol)
  const { pathname } = useLocation()
  if (rol === 'reseller') return <BayiKapisi yol={pathname} />
  if (rol !== 'user') return <Outlet />
  const id = musteriDomainID()
  // domain_id boş/bozuk: yönlendirme döngü kurardı (/abonelikler/ → kapı → …) —
  // oturum kapatılır, GuardedRoute müşteri giriş formuna götürür.
  if (id === null) return <BozukMusteriOturumu />
  if (!musteriYoluIzinli(pathname, id)) return <Navigate to={`/abonelikler/${id}`} replace />
  return <Outlet />
}

// /abonelikler/:id — hosting müşterisinin (rol 'user') ana sayfası "Modern klasik"
// (MusteriAnaSayfa); kök ve bayi aynı rotada abonelik sayfasını görür (değişmedi).
// rolAnaSayfa('user') zaten bu rotayı verdiği için mevcut bağlantılar (Breadcrumb
// 'Anasayfa', logo, rol kapısı yönlendirmesi) müşteride kendiliğinden yeni sayfaya çıkar.
function AbonelikSayfasi() {
  const musteri = useAuth((s) => s.kullanici?.rol === 'user')
  // Rota KabukDisi bloğunda (müşteri ana sayfasının tam genişlik uyarı bandı);
  // yönetici/bayi görünümü ise her panel sayfası gibi tek kabukta çizilir.
  return musteri ? <MusteriAnaSayfa /> : <SayfaCercevesi><SubscriptionDetailPage /></SayfaCercevesi>
}

// 🔴 BAYİ KAPISI. İzin listesi (lib/rol bayiYoluIzinli) dışındaki yol — AdminOnly uçlara
// dayanan yönetici sayfaları ve bilinmeyenler — bayinin en yakın sayfasına gider (sayfa
// mount edilmez → yönetici uçlarına istek yok). Abonelik yolları yalnız bayinin KENDİ
// aboneliğinde açılır; başka bayinin/kökün (ya da var olmayan) kimliği → /domainler.
function BayiKapisi({ yol }: { yol: string }) {
  const izinli = bayiYoluIzinli(yol)
  const abonelik = izinli ? bayiAbonelikID(yol) : null
  const sahip = useBayiAbonelikSahibi(abonelik)
  if (!izinli) return <Navigate to={bayiYonlendirme(yol)} replace />
  if (abonelik === null) return <Outlet />
  if (sahip === null) return <SayfaYukleniyor />
  return sahip ? <Outlet /> : <Navigate to="/domainler" replace />
}

function BozukMusteriOturumu() {
  const cikis = useAuth((s) => s.cikis)
  const hidrate = useAuth((s) => s.hidrate)
  useEffect(() => {
    // Yeniden koşuda (StrictMode, yeniden çizim) karar zaten verilmişse dokunma.
    if (useAuth.getState().kullanici?.rol !== 'user' || musteriDomainID() !== null) return
    // Depo başka sekmenin oturumundaysa (aynı kökende sonradan giriş yapıp müşteri
    // bayraklarını sildi) bu sekmenin müşteri oturumu bozuk değil BAYATTIR: depoya
    // dokunulmaz, sekme depodaki oturumu benimser (MFE-2). Gerçekten bozuksa çıkış.
    if (depoBaskaOturumda()) hidrate()
    else cikis()
  }, [cikis, hidrate])
  return null
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
  // Askıdaki hesap (giriş formu ya da açık oturumdaki ilk askı yanıtı, bkz. lib/api):
  // panelin hiçbir rotası/kabuğu çizilmez — yalnız tam ekran askı sayfası.
  const askida = useHesapAski((s) => s.askida)
  if (askida) return <HesapAskidaPage />
  return (
    <Suspense fallback={<SayfaYukleniyor />}>
    <Routes>
      {/* Rol kapıları (ADR-GP-001): her kapı yalnız kendi giriş formunu gösterir. */}
      <Route path="/giris" element={<KapiGiris form="yonetim"><LoginPage /></KapiGiris>} />
      {/* Müşteri adına giriş kodu değişimi (kimliksiz; kod URL parçasında). */}
      <Route path="/adina" element={<AdinaGirisPage />} />
        <Route path="/cp/giris" element={<KapiGiris form="musteri"><CPanelGirisPage /></KapiGiris>} />
        <Route path="/cp" element={<KapiGiris form="musteri"><CPanelGirisPage /></KapiGiris>} />
      <Route
        path="/"
        element={
          <GuardedRoute>
            <DashboardLayout />
          </GuardedRoute>
        }
      >
        {/* Rol kapısı: altındaki TÜM rotalar müşteride ve bayide izin listesinden geçer (yeni rota = varsayılan yasak). */}
        <Route element={<RolKapisi />}>
        {/* 🔴 KABUK DIŞI (açık istisnalar — sessiz kaçış yok). Tam genişlik ZEMİN
            kullanan sayfalar; içerik genişliği yine --container-sayfa'dır:
            • index (HomePage): koyu pano adası (.gp-dash) içerik alanını baştan
              sona kaplar; içerik dashboard.css'te aynı genişlikte ortalanır.
              Bayi dalı (bayi/BayiPano) koyu pano değildir: HomePage onu SayfaCercevesi
              ile her panel sayfası gibi kabukta çizer.
            • abonelikler/:id müşteri dalı (MusteriAnaSayfa): üst çubuğa yapışık tam
              genişlik uyarı bandı (bant içeriği max-w-sayfa + kabukla aynı dolgu);
              sayfa gövdesi, iskelet ve hata durumu SayfaCercevesi ile kabukta çizilir.
              Yönetici/bayi dalı AbonelikSayfasi içinde SayfaCercevesi ile sarılır.
            Panel dışı tam ekranlar (giriş, cPanel giriş, askı) zaten DashboardLayout'a
            hiç girmez. */}
        <Route element={<KabukDisi />}>
        <Route index                      element={<HomePage />} />
        <Route path="abonelikler/:id"      element={<AbonelikSayfasi />} />
        </Route>
        {/* TEK SAYFA KABUĞU (components/SayfaKabugu): yeni rota BURAYA eklenir ve
            ortalanmış sabit genişliği kendiliğinden alır. */}
        <Route element={<SayfaKabugu />}>
        <Route path="domainler"            element={<DomainsPage />} />
        <Route path="bayiler"              element={<ResellerlarPage />} />
        <Route path="denetim"              element={<DenetimPage />} />
        <Route path="api-anahtarlari"      element={<ApiAnahtarlariPage />} />
        <Route path="bildirimler"         element={<BildirimlerPage />} />
        <Route path="saldiri-zincirleri"  element={<SaldiriZincirleriPage />} />
        <Route path="abonelikler"          element={<Navigate to="/domainler" replace />} />
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
        <Route path="abonelikler/:id/wordpress"     element={<DomainWordPressPage />} />
        <Route path="abonelikler/:id/subdomainler"  element={<DomainSubdomainlerPage />} />
        <Route path="abonelikler/:id/subdomainler/:sid" element={<DomainSubdomainYonetPage />} />
        <Route path="abonelikler/:id/ek-alanlar/yeni" element={<EkAlanEklePage />} />
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
        <Route path="abonelikler/:id/uygulamalar"   element={<DomainUygulamalarPage />} />
        <Route path="abonelikler/:id/gelistirici"   element={<DomainGelistiriciPage />} />
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
        <Route path="abonelikler/:id/guvenlik" element={<MusteriGuvenlikPage />} />
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
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
    </Suspense>
  )
}
