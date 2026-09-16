// Tailux Input — gPanel uyarlaması (label / prefix / suffix / error), birebir sınıflar.
import { forwardRef, InputHTMLAttributes, ReactNode, useId } from 'react'

function cx(...a: Array<string | false | null | undefined>) { return a.filter(Boolean).join(' ') }

export type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'prefix'> & {
  label?: ReactNode
  prefix?: ReactNode
  suffix?: ReactNode
  error?: boolean | ReactNode
  unstyled?: boolean
  classNames?: { root?: string; input?: string; wrapper?: string }
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, prefix, suffix, error, unstyled = false, disabled, type = 'text', className, classNames = {}, id, ...rest }, ref,
) {
  const gen = useId()
  const inputId = id || gen
  const affix = cx(
    'absolute top-0 flex h-full w-9 items-center justify-center transition-colors',
    error ? 'text-error dark:text-error-light' : 'peer-focus:text-primary-600 dark:text-dark-300 dark:peer-focus:text-primary-500 text-gray-400',
  )
  return (
    <div className={cx('input-root', classNames.root)}>
      {label && (
        <label htmlFor={inputId} className="input-label">{label}</label>
      )}
      <div className={cx('input-wrapper relative', !!label && 'mt-1.5', classNames.wrapper)}>
        <input
          id={inputId}
          ref={ref}
          type={type}
          disabled={disabled}
          className={cx(
            'form-input-base',
            !!suffix && 'pr-9',
            !!prefix && 'pl-9',
            !unstyled && cx(
              'form-input',
              error ? 'border-error dark:border-error-lighter'
                : disabled ? 'bg-gray-150 dark:border-dark-500 dark:bg-dark-600 cursor-not-allowed border-gray-300 opacity-60'
                  : 'peer focus:border-primary-600 dark:border-dark-450 dark:hover:border-dark-400 dark:focus:border-primary-500 border-gray-300 hover:border-gray-400',
            ),
            className, classNames.input,
          )}
          {...rest}
        />
        {prefix && <div className={cx('prefix left-0', affix)}>{prefix}</div>}
        {suffix && <div className={cx('suffix right-0', affix)}>{suffix}</div>}
      </div>
      {typeof error !== 'boolean' && error ? (
        <span className="mt-1 text-xs text-error dark:text-error-light">{error}</span>
      ) : null}
    </div>
  )
})

export default Input
