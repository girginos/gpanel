// Tailux tarzı modal — bulanık backdrop + fade/scale giriş, dolu-zemin panel.
// Paneldeki TÜM modallar bunu kullanır (aynı arka plan, animasyon, z-sırası, kapatma,
// alt çubuk): sayfalarda elle `fixed inset-0` modal YAZILMAZ.
//
//   <Modal acik={a} baslik="Parola üretildi" ton="basari" onKapat={kapat}
//     alt={<Button color="primary" ikon={I.onay} onClick={kapat}>Tamam</Button>}>
//     …içerik…
//   </Modal>
import { useEffect, useLayoutEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import type { Ton } from './tonlar'
import { TonRozeti } from './UyariKutusu'

const ODAKLANABILIR = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export default function Modal({
  acik, baslik, onKapat, children, genislik = 'md', ton, alt, kapatilamaz = false,
}: {
  acik: boolean
  baslik: ReactNode
  onKapat: () => void
  children: ReactNode
  genislik?: 'sm' | 'md' | 'lg' | 'xl'
  // ton — başlığın solunda tona göre ikon (bkz. tonlar.ts); sonuç/uyarı modallarında ver.
  ton?: Ton
  // alt — sabit alt çubuk (eylem butonları): sağa yaslı, mobilde ana eylem üstte.
  alt?: ReactNode
  // kapatilamaz — işlem sürerken Esc/arka plan/X kapatmasın (ör. kaydediliyor).
  kapatilamaz?: boolean
}) {
  const kokRef = useRef<HTMLDivElement | null>(null)
  const panelRef = useRef<HTMLDivElement | null>(null)
  // onKapat her çizimde yeni fonksiyon olabilir: etkiyi yeniden kurmasın diye ref'te tut.
  const kapatRef = useRef(onKapat)
  const kilitRef = useRef(kapatilamaz)
  useLayoutEffect(() => { kapatRef.current = onKapat; kilitRef.current = kapatilamaz })

  useEffect(() => {
    if (!acik) return
    const onceki = document.activeElement as HTMLElement | null
    const eski = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    // Açılışta odak panelin içine: ilk giriş alanı, yoksa panelin kendisi.
    const t = window.setTimeout(() => {
      const p = panelRef.current
      if (!p || p.contains(document.activeElement)) return
      const ilkGirdi = p.querySelector<HTMLElement>('input:not([disabled]), select:not([disabled]), textarea:not([disabled])')
      ;(ilkGirdi || p).focus()
    }, 30)
    function tus(e: KeyboardEvent) {
      // Üstte başka bir modal/diyalog açıksa (ör. bu modaldan açılan onay) tuşlar onundur.
      const ustte = Array.from(document.querySelectorAll('[aria-modal="true"]')).pop()
      if (ustte && ustte !== kokRef.current) return
      if (e.key === 'Escape') {
        if (!kilitRef.current) kapatRef.current()
        return
      }
      if (e.key !== 'Tab' || !panelRef.current) return
      // Odak tuzağı: Tab/Shift+Tab panelin dışına çıkmaz.
      const l = Array.from(panelRef.current.querySelectorAll<HTMLElement>(ODAKLANABILIR)).filter((el) => el.offsetParent !== null)
      if (l.length === 0) { e.preventDefault(); return }
      const ilk = l[0], son = l[l.length - 1]
      if (e.shiftKey && (document.activeElement === ilk || document.activeElement === panelRef.current)) { e.preventDefault(); son.focus() }
      else if (!e.shiftKey && document.activeElement === son) { e.preventDefault(); ilk.focus() }
    }
    window.addEventListener('keydown', tus)
    return () => {
      window.clearTimeout(t)
      window.removeEventListener('keydown', tus)
      document.body.style.overflow = eski
      onceki?.focus?.()
    }
  }, [acik])

  if (!acik) return null
  const w = genislik === 'sm' ? 'max-w-sm' : genislik === 'lg' ? 'max-w-2xl' : genislik === 'xl' ? 'max-w-4xl' : 'max-w-md'
  const kapat = () => { if (!kapatilamaz) onKapat() }

  return (
    <div ref={kokRef} className="fixed inset-0 z-[100] flex items-center justify-center p-4" role="dialog" aria-modal="true"
      aria-label={typeof baslik === 'string' ? baslik : undefined}>
      <div className="gosp-fade absolute inset-0 bg-gray-900/50 backdrop-blur-sm dark:bg-black/50" onClick={kapat} aria-hidden />
      <div ref={panelRef} tabIndex={-1}
        className={`gosp-modal-in relative flex max-h-[90vh] w-full ${w} flex-col overflow-hidden rounded-lg bg-white shadow-soft outline-hidden dark:bg-dark-700 dark:shadow-none`}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-500">
          <div className="flex min-w-0 items-center gap-3">
            {ton && <TonRozeti ton={ton} kucuk />}
            <h3 className="truncate text-base font-medium text-gray-800 dark:text-dark-50">{baslik}</h3>
          </div>
          <button
            type="button"
            onClick={kapat}
            disabled={kapatilamaz}
            aria-label="Kapat"
            className="btn-base -mr-1.5 size-8 shrink-0 rounded-full text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:text-dark-300 dark:hover:bg-dark-600 dark:hover:text-dark-50"
          >
            <svg className="h-[18px] w-[18px]" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto p-5">{children}</div>
        {alt && (
          <div className="flex flex-col-reverse gap-2 border-t border-gray-200 px-5 py-3 sm:flex-row sm:justify-end dark:border-dark-500">
            {alt}
          </div>
        )}
      </div>
    </div>
  )
}
