// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect } from 'react'

export default function Modal({
  acik, baslik, onKapat, children, genislik = 'md',
}: {
  acik: boolean
  baslik: string
  onKapat: () => void
  children: React.ReactNode
  genislik?: 'sm' | 'md' | 'lg'
}) {
  useEffect(() => {
    if (!acik) return
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onKapat() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [acik, onKapat])

  if (!acik) return null
  const w = genislik === 'sm' ? 'max-w-sm' : genislik === 'lg' ? 'max-w-2xl' : 'max-w-md'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
      <div className="absolute inset-0 bg-slate-900/40" onClick={onKapat} />
      <div className={`relative bg-white dark:bg-slate-800 rounded-2xl shadow-2xl w-full ${w} max-h-[90vh] overflow-auto`}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-200 dark:border-slate-700">
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
          <button onClick={onKapat} className="p-1 text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 rounded-md transition">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}