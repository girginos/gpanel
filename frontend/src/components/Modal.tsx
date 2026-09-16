// Tailux tarzı modal — bulanık backdrop + fade/scale giriş, dolu-zemin panel.
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
    const eski = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { window.removeEventListener('keydown', onKey); document.body.style.overflow = eski }
  }, [acik, onKapat])

  if (!acik) return null
  const w = genislik === 'sm' ? 'max-w-sm' : genislik === 'lg' ? 'max-w-2xl' : 'max-w-md'

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4" role="dialog" aria-modal="true">
      <div className="gosp-fade absolute inset-0 bg-gray-900/50 backdrop-blur-sm dark:bg-black/50" onClick={onKapat} aria-hidden />
      <div className={`gosp-modal-in relative flex max-h-[90vh] w-full ${w} flex-col overflow-hidden rounded-lg bg-white shadow-soft dark:bg-dark-700 dark:shadow-none`}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-500">
          <h3 className="truncate text-base font-medium text-gray-800 dark:text-dark-50">{baslik}</h3>
          <button
            onClick={onKapat}
            aria-label="Kapat"
            className="btn-base -mr-1.5 size-8 shrink-0 rounded-full text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-600 dark:hover:text-dark-50"
          >
            <svg className="h-[18px] w-[18px]" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto p-5">{children}</div>
      </div>
    </div>
  )
}
