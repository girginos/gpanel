// Tailux Card — gPanel uyarlaması. skin: bordered (varsayılan) | shadow | none.
import { forwardRef, HTMLAttributes, ReactNode } from 'react'

export type CardSkin = 'none' | 'bordered' | 'shadow'
function cx(...a: Array<string | false | null | undefined>) { return a.filter(Boolean).join(' ') }

export type CardProps = HTMLAttributes<HTMLDivElement> & { skin?: CardSkin; children?: ReactNode }

export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { className, children, skin = 'bordered', ...rest }, ref,
) {
  return (
    <div
      ref={ref}
      className={cx(
        'card rounded-lg',
        skin === 'bordered' && 'border border-gray-200 dark:border-dark-600 print:border-0',
        skin === 'shadow' && 'shadow-soft dark:bg-dark-700 bg-white dark:shadow-none print:shadow-none',
        className,
      )}
      {...rest}
    >
      {children}
    </div>
  )
})

export default Card
