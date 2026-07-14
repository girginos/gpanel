// gosp-dark-swept
// gosp-dark-swept-v2
import { useNavigate } from 'react-router-dom'

type Renk = 'amber' | 'violet' | 'sky' | 'indigo' | 'emerald' | 'teal' | 'slate' | 'orange' | 'rose'

const BG: Record<Renk, string> = {
  amber:   'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300',
  violet:  'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300',
  sky:     'bg-sky-100 text-sky-700',
  indigo:  'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300',
  emerald: 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300',
  teal:    'bg-teal-100 text-teal-700',
  slate:   'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
  orange:  'bg-orange-100 text-orange-700',
  rose:    'bg-rose-100 text-rose-700',
}

export default function ToolCard({
  etiket, aciklama, ikon, renk = 'slate', faz, uyari, to, onClick,
}: {
  etiket: string
  aciklama?: string
  ikon: string
  renk?: Renk
  faz?: string
  uyari?: string
  to?: string
  onClick?: () => void
}) {
  const navigate = useNavigate()
  const tikla = () => {
    if (to) navigate(to)
    else if (onClick) onClick()
  }
  const govde = (
    <>
      <div className={`w-10 h-10 rounded-2xl flex items-center justify-center flex-shrink-0 ${BG[renk]}`}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} className="w-5 h-5">
          <path strokeLinecap="round" strokeLinejoin="round" d={ikon} />
        </svg>
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate flex items-center gap-1.5">
          <span className="truncate">{etiket}</span>
          {faz && (
            <span className="text-[9px] font-semibold uppercase tracking-wider text-amber-700 dark:text-amber-300 bg-amber-100 dark:bg-amber-900/30 px-1 py-0.5 rounded">
              {faz}
            </span>
          )}
        </div>
        {aciklama && <div className="text-xs text-slate-500 dark:text-slate-500 truncate mt-0.5">{aciklama}</div>}
        {uyari && <div className="text-[11px] text-red-600 dark:text-red-400 truncate mt-0.5">{uyari}</div>}
      </div>
    </>
  )
  const klass = 'group flex items-start gap-3 p-3 rounded-2xl border border-slate-200 dark:border-slate-700 hover:border-slate-300 dark:hover:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800/50 hover:shadow-sm transition text-left w-full cursor-pointer'

  return <button type="button" onClick={tikla} className={klass}>{govde}</button>
}