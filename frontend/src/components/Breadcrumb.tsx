// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
import { Link, useNavigate } from 'react-router-dom'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { kirintiEsle, rolAnaSayfa, useRol } from '@/lib/rol'

const BREADCRUMB_EN: Record<string, string> = {
  "Sayfa yolu": "Page path",
  "Geri dön": "Go back",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BREADCRUMB_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export type BreadcrumbItem = { etiket: string; href?: string }

export default function Breadcrumb({ items }: { items: BreadcrumbItem[] }) {
  useTranslation() // dil re-render aboneligi
  const navigate = useNavigate()
  const rol = useRol()
  // Rol eşlemesi MERKEZDE (60+ sayfa aynı kalıbı kullanıyor): müşteride 'Anasayfa'
  // kendi aboneliğine gider, yönetici listesi 'Domainler' atılır; bayi ve müşteride
  // yönetici-yalnız 'Araçlar ve Ayarlar' atılır. Bkz. lib/rol.ts kirintiEsle.
  const ogeler = kirintiEsle(items, rol)

  // Mobilde tarayıcı geri tuşu her zaman elde değil (tam ekran/PWA, tek elle
  // kullanım). Kırıntı yolunun başına açık bir geri düğmesi koyuyoruz.
  // Geçmiş boşsa (doğrudan link ile girilmişse) rolün ana sayfasına düşer.
  function geri() {
    if (window.history.length > 1) navigate(-1)
    else navigate(rolAnaSayfa(rol))
  }

  // İz uzun olduğunda mobilde son parçaya kaydır: kullanıcı nerede olduğunu
  // görsün, geriye doğru kaydırarak üst seviyelere ulaşsın.
  return (
    <nav
      className="flex items-center gap-1 text-sm mb-3 text-slate-500 dark:text-slate-500 overflow-x-auto [&>*]:shrink-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      aria-label={cevir("Sayfa yolu")}
    >
      <Button
        isIcon
        variant="flat"
        ikon={I.geri}
        onClick={geri}
        className="lg:hidden -ml-1.5 mr-0.5 size-8"
        aria-label={cevir("Geri dön")}
        title={cevir("Geri dön")}
      />

      {ogeler.map((it, i) => {
        const son = i === ogeler.length - 1
        return (
          <div key={i} className="flex items-center">
            {it.href && !son ? (
              // py-1: mobilde parmakla basılabilir hedef yüksekliği
              <Link
                to={it.href}
                className="py-1 whitespace-nowrap hover:text-brand-600 dark:hover:text-brand-400 transition"
              >
                {it.etiket}
              </Link>
            ) : (
              <span className={`whitespace-nowrap ${son ? 'text-slate-700 dark:text-slate-300 font-medium' : ''}`}>
                {it.etiket}
              </span>
            )}
            {!son && (
              <Ikon d={I.chevronSag} kalinlik={2} className="w-4 h-4 mx-1.5 text-slate-300 dark:text-slate-600 shrink-0" />
            )}
          </div>
        )
      })}
    </nav>
  )
}
