// Tailux Select — gPanel uyarlaması. Input ile aynı düzen (label / prefix / error) ve aynı
// form sınıfları; yalnız sağda açılır ok var.
//
// Neden yerel <select>: klavye (ok tuşları, harfle atlama, Alt+↓), ekran okuyucu rolü ve
// mobil seçici tarayıcıdan hazır gelir; özel liste kutusu bunları yeniden yazmak demek.
// Aranabilir uzun liste gerekiyorsa AramaliSecim kullanılır.
//
// Erişilebilirlik: `label` verilince <label htmlFor> ile bağlanır; verilmezse çağıran
// `aria-label` geçmelidir. Hata metni aria-describedby ile duyurulur, aria-invalid konur.
//
// Kullanım:
//   <Select label={cevir("Kaynak panel")} prefix={<Ikon d={I.sunucu} />} value={tip}
//     onChange={e => setTip(e.target.value)} data={[{ value: 'plesk', label: 'Plesk' }]} />
import { forwardRef, ReactNode, SelectHTMLAttributes, useId } from 'react'
import { I, Ikon } from '../Ikon'
import { cn } from '@/lib/utils'

export type SelectSecenek = { value: string | number; label: string; disabled?: boolean }

export type SelectProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'prefix' | 'children' | 'multiple'> & {
  label?: ReactNode
  prefix?: ReactNode
  data: SelectSecenek[]
  error?: boolean | ReactNode
  classNames?: { root?: string }
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { label, prefix, data, error, disabled, className, classNames = {}, id, 'aria-describedby': tarif, ...rest }, ref,
) {
  const gen = useId()
  const selectId = id || gen
  const hataMetni = typeof error !== 'boolean' && error ? error : null
  const hataId = `${selectId}-hata`
  const affix = cn(
    'pointer-events-none absolute top-0 flex h-full w-9 items-center justify-center transition-colors',
    error ? 'text-error dark:text-error-light' : 'peer-focus:text-primary-600 dark:text-dark-300 dark:peer-focus:text-primary-500 text-gray-400',
  )
  return (
    <div className={cn('input-root', classNames.root)}>
      {label && (
        <label htmlFor={selectId} className="input-label">{label}</label>
      )}
      <div className={cn('input-wrapper relative', !!label && 'mt-1.5')}>
        <select
          id={selectId}
          ref={ref}
          disabled={disabled}
          aria-invalid={error ? true : undefined}
          aria-describedby={[tarif, hataMetni ? hataId : ''].filter(Boolean).join(' ') || undefined}
          className={cn(
            'form-input-base form-input pr-9',
            !!prefix && 'pl-9',
            error ? 'border-error dark:border-error-lighter'
              : disabled ? 'bg-gray-150 dark:border-dark-500 dark:bg-dark-600 cursor-not-allowed border-gray-300 opacity-60'
                : 'peer cursor-pointer focus:border-primary-600 dark:border-dark-450 dark:hover:border-dark-400 dark:focus:border-primary-500 border-gray-300 hover:border-gray-400',
            className,
          )}
          {...rest}
        >
          {data.map((s) => (
            <option key={String(s.value)} value={s.value} disabled={s.disabled}>{s.label}</option>
          ))}
        </select>
        {prefix && <div className={cn('left-0', affix)}>{prefix}</div>}
        <div className={cn('right-0', affix)}><Ikon d={I.chevronAsagi} /></div>
      </div>
      {hataMetni ? (
        <span id={hataId} className="mt-1 text-xs text-error dark:text-error-light">{hataMetni}</span>
      ) : null}
    </div>
  )
})

export default Select
