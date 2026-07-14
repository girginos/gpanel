// gosp-dark-swept
// gosp-dark-swept-v2
import { Link } from 'react-router-dom'

export type BreadcrumbItem = { etiket: string; href?: string }

export default function Breadcrumb({ items }: { items: BreadcrumbItem[] }) {
  return (
    <nav className="flex items-center text-sm mb-3 text-slate-500 dark:text-slate-500" aria-label="Breadcrumb">
      {items.map((it, i) => {
        const son = i === items.length - 1
        return (
          <div key={i} className="flex items-center">
            {it.href && !son ? (
              <Link to={it.href} className="hover:text-brand-600 dark:text-brand-400 transition">{it.etiket}</Link>
            ) : (
              <span className={son ? 'text-slate-700 dark:text-slate-300 font-medium' : ''}>{it.etiket}</span>
            )}
            {!son && (
              <svg className="w-4 h-4 mx-1.5 text-slate-300" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
              </svg>
            )}
          </div>
        )
      })}
    </nav>
  )
}