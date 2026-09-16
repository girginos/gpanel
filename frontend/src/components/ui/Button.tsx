// Tailux Button — gPanel uyarlaması. Sınıf stringleri Tailux ile BİREBİR
// (components/ui/Button + styles: .btn/.btn-base + this:<renk> variantları).
// Kullanım: <Button color="primary">Kaydet</Button>, variant filled|soft|outlined|flat.
import { forwardRef, ButtonHTMLAttributes, ReactNode } from 'react'

export type Variant = 'filled' | 'outlined' | 'soft' | 'flat'
export type Color = 'neutral' | 'primary' | 'secondary' | 'info' | 'success' | 'warning' | 'error'

const THIS: Record<Exclude<Color, 'neutral'>, string> = {
  primary: 'this:primary',
  secondary: 'this:secondary',
  info: 'this:info',
  success: 'this:success',
  warning: 'this:warning',
  error: 'this:error',
}

const variants: Record<Variant, string> = {
  filled:
    'bg-this text-white hover:bg-this-darker focus:bg-this-darker active:bg-this-darker/90 disabled:bg-this-light dark:disabled:bg-this-darker',
  soft: 'text-this-darker bg-this-darker/[.08] hover:bg-this-darker/[.15] focus:bg-this-darker/[.15] active:focus:bg-this-darker/20 dark:bg-this-lighter/10 dark:text-this-lighter dark:hover:bg-this-lighter/20 dark:focus:bg-this-lighter/20 dark:active:bg-this-lighter/25',
  outlined:
    'text-this-darker border border-this-darker hover:bg-this-darker/[.05] focus:bg-this-darker/[.05] active:bg-this-darker/10 dark:border-this-lighter dark:text-this-lighter dark:hover:bg-this-lighter/[.05] dark:focus:bg-this-lighter/[.05] dark:active:bg-this-lighter/10',
  flat: 'text-this-darker hover:bg-this-darker/[.08] focus:bg-this-darker/[.08] active:bg-this-darker/[.15] dark:text-this-lighter dark:hover:bg-this-lighter/10 dark:focus:bg-this-lighter/10 dark:active:bg-this-lighter/[.15]',
}

const neutralVariants: Record<Variant, string> = {
  filled:
    'bg-gray-150 text-gray-900 hover:bg-gray-200 focus:bg-gray-200 active:bg-gray-200/80 dark:bg-surface-2 dark:text-dark-50 dark:hover:bg-surface-1 dark:focus:bg-surface-1 dark:active:bg-surface-1/90',
  soft: 'bg-gray-150/30 text-gray-900 hover:bg-gray-200/[.15] focus:bg-gray-200/[.15] active:bg-gray-200/20 dark:bg-dark-500/30 dark:text-dark-50 dark:hover:bg-dark-450/[.15] dark:focus:bg-dark-450/[.15] dark:active:bg-dark-450/20',
  outlined:
    'border border-gray-300 hover:bg-gray-300/20 focus:bg-gray-300/20 text-gray-900 active:bg-gray-300/25 dark:text-dark-50 dark:hover:bg-dark-300/20 dark:focus:bg-dark-300/20 dark:active:bg-dark-300/25 dark:border-dark-450',
  flat: 'hover:bg-gray-300/20 focus:bg-gray-300/20 text-gray-700 active:bg-gray-300/25 dark:text-dark-200 dark:hover:bg-dark-300/10 dark:focus:bg-dark-300/10 dark:active:bg-dark-300/20',
}

function cx(...a: Array<string | false | null | undefined>): string {
  return a.filter(Boolean).join(' ')
}

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  color?: Color
  variant?: Variant
  isIcon?: boolean
  isGlow?: boolean
  unstyled?: boolean
  // yukleniyor — işlem sürerken buton içinde spinner gösterir + butonu devre
  // dışı bırakır (çift tıklama/çift işlem YASAK). Tüm buton yapılarında ortak.
  yukleniyor?: boolean
  children?: ReactNode
}

// Yukleyici — buton içi dönen spinner (Tailux akım rengini currentColor'dan alır).
function Yukleyici() {
  return (
    <svg className="animate-spin size-4 shrink-0 -ms-0.5 me-1.5" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3.5" />
      <path className="opacity-90" fill="currentColor" d="M4 12a8 8 0 0 1 8-8V0C5.373 0 0 5.373 0 12h4z" />
    </svg>
  )
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, children, color = 'neutral', variant = 'filled', isIcon = false, isGlow = false, unstyled = false, yukleniyor = false, disabled, type, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type || 'button'}
      disabled={disabled || yukleniyor}
      aria-busy={yukleniyor || undefined}
      className={cx(
        'btn-base',
        !unstyled && 'btn',
        !unstyled && isIcon && 'shrink-0 p-0',
        !unstyled && (color === 'neutral'
          ? cx(neutralVariants[variant], isGlow && 'dark:shadow-dark-450/5 shadow-lg shadow-gray-200/50')
          : cx(THIS[color], variants[variant], isGlow && 'shadow-soft shadow-this/50 dark:shadow-this/50 dark:shadow-lg')),
        unstyled && color !== 'neutral' && THIS[color],
        className,
      )}
      {...rest}
    >
      {yukleniyor && <Yukleyici />}
      {yukleniyor && isIcon ? null : children}
    </button>
  )
})

export default Button
