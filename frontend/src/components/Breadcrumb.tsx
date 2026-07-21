// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
import { Link, useNavigate } from 'react-router-dom'

export type BreadcrumbItem = { etiket: string; href?: string }

export default function Breadcrumb({ items }: { items: BreadcrumbItem[] }) {
  const navigate = useNavigate()

  // Mobilde tarayıcı geri tuşu her zaman elde değil (tam ekran/PWA, tek elle
  // kullanım). Kırıntı yolunun başına açık bir geri düğmesi koyuyoruz.
  // Geçmiş boşsa (doğrudan link ile girilmişse) anasayfaya düşer.
  function geri() {
    if (window.history.length > 1) navigate(-1)
    else navigate('/')
  }

  // İz uzun olduğunda mobilde son parçaya kaydır: kullanıcı nerede olduğunu
  // görsün, geriye doğru kaydırarak üst seviyelere ulaşsın.
  return (
    <nav
      className="flex items-center gap-1 text-sm mb-3 text-slate-500 dark:text-slate-500 overflow-x-auto [&>*]:flex-shrink-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      aria-label="Sayfa yolu"
    >
      <button
        type="button"
        onClick={geri}
        className="lg:hidden -ml-1.5 mr-0.5 p-1.5 rounded-md text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 transition"
        aria-label="Geri dön"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
        </svg>
      </button>

      {items.map((it, i) => {
        const son = i === items.length - 1
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
              <svg className="w-4 h-4 mx-1.5 text-slate-300 dark:text-slate-600 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
              </svg>
            )}
          </div>
        )
      })}
    </nav>
  )
}
