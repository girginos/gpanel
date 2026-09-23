import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useKapiTipi } from '@/lib/kapi'

// Giriş formunu bulunulan kapıya göre seçer (ADR-GP-001). Müşteri kapısında
// yönetici girişi, yönetim kapısında müşteri girişi YOKTUR — sunucu o uçları 404'ler.
// Kullanıcıya çalışmayacak bir form göstermek yerine doğru forma yönlendirir.
// Karma (geçiş kapısı / kapı modu kapalı) → istenen form olduğu gibi.
export default function KapiGiris({ form, children }: { form: 'yonetim' | 'musteri'; children: ReactNode }) {
  const tip = useKapiTipi()
  const loc = useLocation()
  if (tip === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50 dark:bg-dark-900" aria-busy="true">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600 dark:border-dark-600 dark:border-t-brand-400" />
      </div>
    )
  }
  if (form === 'yonetim' && tip === 'musteri') return <Navigate to={'/cp/giris' + loc.search} replace />
  if (form === 'musteri' && tip === 'yonetim') return <Navigate to={'/giris' + loc.search} replace />
  return <>{children}</>
}
