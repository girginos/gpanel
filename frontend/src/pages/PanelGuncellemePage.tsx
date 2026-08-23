import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import Breadcrumb from '@/components/Breadcrumb'
import PanelGuncelleme from '@/components/PanelGuncelleme'

/*
 * Panel Güncellemesi — özel sayfa.
 * Güncelleme arka planda (systemd-run transient unit) koşar: bu sayfa/tarayıcı
 * kapansa, hatta panel kendini yeniden başlatsa bile işlem devam eder.
 * PanelGuncelleme bileşeni durumu sunucudan (systemctl is-active) okur ve
 * sayfa yeniden açıldığında canlı ilerlemeyi tekrar yakalar.
 */

const PGUNC_EN: Record<string, string> = {
  "Panel Güncellemesi": "Panel Update",
  "Araçlar ve Ayarlar": "Tools and Settings",
  "Anasayfa": "Home",
  "Paneli GitHub'daki son sürüme günceller. Ortam değişkenleri, veritabanı ve siteler korunur;": "Updates the panel to the latest version on GitHub. Environment variables, database and sites are preserved;",
  "yeni sürüm sağlıklı başlamazsa otomatik geri alınır. İşlem arka planda çalışır —": "if the new version does not start healthy it is automatically rolled back. The process runs in the background —",
  "bu sayfayı kapatabilirsiniz, güncelleme kesintisiz devam eder.": "you can close this page, the update continues uninterrupted.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PGUNC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PanelGuncellemePage() {
  useTranslation() // dil re-render aboneligi
  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' },
        { etiket: cevir("Panel Güncellemesi") },
      ]} />

      <div className="mb-5 max-w-3xl">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir("Panel Güncellemesi")}</h1>
        <p className="mt-1 text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          {cevir("Paneli GitHub'daki son sürüme günceller. Ortam değişkenleri, veritabanı ve siteler korunur;")}
          {' '}{cevir("yeni sürüm sağlıklı başlamazsa otomatik geri alınır. İşlem arka planda çalışır —")}
          {cevir(cevir("bu sayfayı kapatabilirsiniz, güncelleme kesintisiz devam eder."))}
        </p>
      </div>

      <div className="max-w-3xl">
        <PanelGuncelleme />
      </div>
    </div>
  )
}
