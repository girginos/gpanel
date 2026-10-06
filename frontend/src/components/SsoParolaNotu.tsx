// API anahtarıyla (WHMCS tek tık girişi) açılmış müşteri oturumunda müşteri GİRİŞ
// parolası bu panelden değiştirilemez (sunucu 403: AdinaYasak) — parola yönetimi
// faturalama sistemindedir. Parola formu yerine bu kısa açıklama gösterilir.
import { useBayiCevir } from '@/lib/bayiCeviri'

export default function SsoParolaNotu({ className = '' }: { className?: string }) {
  const t = useBayiCevir()
  return (
    <p data-sso-parola-notu className={`text-sm text-slate-600 dark:text-dark-200 ${className}`}>
      {t('Parolanızı müşteri alanınızdan değiştirin')}
    </p>
  )
}
