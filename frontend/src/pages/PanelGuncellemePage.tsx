import Breadcrumb from '@/components/Breadcrumb'
import PanelGuncelleme from '@/components/PanelGuncelleme'

/*
 * Panel Güncellemesi — özel sayfa.
 * Güncelleme arka planda (systemd-run transient unit) koşar: bu sayfa/tarayıcı
 * kapansa, hatta panel kendini yeniden başlatsa bile işlem devam eder.
 * PanelGuncelleme bileşeni durumu sunucudan (systemctl is-active) okur ve
 * sayfa yeniden açıldığında canlı ilerlemeyi tekrar yakalar.
 */
export default function PanelGuncellemePage() {
  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Panel Güncellemesi' },
      ]} />

      <div className="mb-5 max-w-3xl">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Panel Güncellemesi</h1>
        <p className="mt-1 text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          Paneli GitHub'daki son sürüme günceller. Ortam değişkenleri, veritabanı ve siteler korunur;
          yeni sürüm sağlıklı başlamazsa otomatik geri alınır. İşlem arka planda çalışır —
          bu sayfayı kapatabilirsiniz, güncelleme kesintisiz devam eder.
        </p>
      </div>

      <div className="max-w-3xl">
        <PanelGuncelleme />
      </div>
    </div>
  )
}
