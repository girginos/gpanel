// Ortak anahtar (switch). Tüm panelde tutarlı, küçük boyut:
//   md  → track h-5 w-9,  thumb size-4  (varsayılan)
//   sm  → track h-4 w-7,  thumb size-3  (tablo / dar satır)
// Erişilebilirlik: gerçek <button role="switch" aria-checked>; etiket `aria-label` ya da
// `aria-labelledby` ile bağlanır; klavye (Space/Enter) yerel; odak halkası focus-visible.
// `durumEtiketi` verilirse anahtarın hemen SOLUNDA küçük (text-xs) durum yazısı gösterilir.
import { ButtonHTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export type ToggleProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange' | 'role' | 'type'> & {
  acik: boolean
  onDegis?: (yeni: boolean) => void
  boyut?: 'sm' | 'md'
  renk?: 'success' | 'brand' | 'warning'
  durumEtiketi?: ReactNode
}

const RENK = {
  success: 'bg-emerald-500',
  brand: 'bg-brand-600 dark:bg-brand-500',
  warning: 'bg-amber-500',
} as const

export function Toggle({ acik, onDegis, boyut = 'md', renk = 'success', durumEtiketi, className, onClick, disabled, ...rest }: ToggleProps) {
  const dugme = (
    <button
      type="button" role="switch" aria-checked={acik} disabled={disabled}
      onClick={(e) => { onClick?.(e); onDegis?.(!acik) }}
      className={cn(
        'relative inline-flex shrink-0 cursor-pointer items-center rounded-full transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 focus-visible:ring-offset-1 dark:focus-visible:ring-offset-dark-700',
        'disabled:cursor-not-allowed disabled:opacity-60',
        boyut === 'sm' ? 'h-4 w-7' : 'h-5 w-9',
        acik ? RENK[renk] : 'bg-slate-300 dark:bg-slate-600',
        className,
      )}
      {...rest}
    >
      <span aria-hidden="true" className={cn(
        'inline-block transform rounded-full bg-white shadow transition-transform',
        boyut === 'sm' ? 'size-3' : 'size-4',
        boyut === 'sm' ? (acik ? 'translate-x-[14px]' : 'translate-x-0.5') : (acik ? 'translate-x-[18px]' : 'translate-x-0.5'),
      )} />
    </button>
  )
  if (!durumEtiketi) return dugme
  return (
    <span className="inline-flex shrink-0 items-center gap-2">
      <span className={cn('text-xs font-medium', acik ? 'text-emerald-700 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400')}>{durumEtiketi}</span>
      {dugme}
    </span>
  )
}

export default Toggle
