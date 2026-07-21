import Breadcrumb from '@/components/Breadcrumb'
import SunucuOptimize from '@/components/SunucuOptimize'

/*
 * Sunucu Optimize — özel sayfa (sol menüde de bağlı).
 * İş systemd-run transient unit altında arka planda koşar; sayfa/tarayıcı
 * kapansa bile devam eder, durum sunucudan okunur (resume-on-reopen).
 * İleride servis-bazlı optimizasyon (MariaDB / Nginx / PHP-FPM / Redis …) buraya eklenecek.
 */
export default function SunucuOptimizePage() {
  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Sunucu Optimize' },
      ]} />

      <div className="mb-5 max-w-3xl">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Sunucu Optimize</h1>
        <p className="mt-1 text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          Sistem paketlerini günceller ve MariaDB / Nginx / PHP ayarlarını sunucu kaynağına göre
          yeniden ayarlar. İşlem arka planda çalışır — bu sayfayı kapatabilirsiniz, uzun sürebilir
          ve servisleri kısa süre etkileyebilir.
        </p>
      </div>

      <div className="max-w-3xl">
        <SunucuOptimize />
      </div>
    </div>
  )
}
