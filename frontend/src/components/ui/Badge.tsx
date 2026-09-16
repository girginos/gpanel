// Tailux Badge — gPanel uyarlaması, birebir sınıflar.
import { forwardRef, HTMLAttributes, ReactNode } from 'react'
import type { Color } from './Button'

type Variant = 'filled' | 'outlined' | 'soft'

const THIS: Record<Exclude<Color, 'neutral'>, string> = {
  primary: 'this:primary', secondary: 'this:secondary', info: 'this:info',
  success: 'this:success', warning: 'this:warning', error: 'this:error',
}
const variants: Record<Variant, string> = {
  filled: 'text-white bg-this',
  outlined: 'border border-this/30 text-this dark:border-this-lighter/30 dark:text-this-lighter',
  soft: 'text-this-darker bg-this-darker/[0.07] dark:text-this-lighter dark:bg-this-lighter/10',
}
const neutralVariants: Record<Variant, string> = {
  filled: 'bg-gray-200 text-gray-900 dark:bg-surface-2 dark:text-dark-50',
  outlined: 'border border-gray-300 text-gray-900 dark:border-surface-1 dark:text-dark-50',
  soft: 'bg-gray-200/30 text-gray-900 dark:bg-dark-500/30 dark:text-dark-50',
}
function cx(...a: Array<string | false | null | undefined>) { return a.filter(Boolean).join(' ') }

export type BadgeProps = HTMLAttributes<HTMLDivElement> & {
  color?: Color; variant?: Variant; unstyled?: boolean; children?: ReactNode
}

export const Badge = forwardRef<HTMLDivElement, BadgeProps>(function Badge(
  { className, children, color = 'neutral', variant = 'filled', unstyled = false, ...rest }, ref,
) {
  return (
    <div
      ref={ref}
      className={cx(
        'badge-base',
        !unstyled && cx('badge', color === 'neutral' ? neutralVariants[variant] : cx(THIS[color], variants[variant])),
        className,
      )}
      {...rest}
    >
      {children}
    </div>
  )
})

export default Badge
