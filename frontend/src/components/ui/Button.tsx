// Tailux Button — gPanel uyarlaması. Sınıf stringleri Tailux ile BİREBİR
// (components/ui/Button + styles: .btn/.btn-base + this:<renk> variantları).
// Kullanım: <Button color="primary" ikon={I.disket}>Kaydet</Button>, variant filled|soft|outlined|flat.
//
// TEK BUTON DİLİ (tüm panel):
//   - ana eylem → color="primary" (filled) · ikincil/iptal → neutral outlined · yıkıcı → error
//   - ikon: eylem ikonu METNİN SOLUNDA (I.* — components/Ikon); ikonSonda: yön/dış bağlantı oku
//   - yukleniyor: ikon yerinde dönen spinner (yenile ikonu kendisi döner) + buton kilitli
//   - boyut="sm": tablo satırı/araç çubuğu; varsayılan md
import { forwardRef, ButtonHTMLAttributes, ReactNode } from 'react'
import { I, Ikon } from '../Ikon'

export type Variant = 'filled' | 'outlined' | 'soft' | 'flat'
export type Boyut = 'sm' | 'md'
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

// Renkli butonlarda klavye odak halkası butonun kendi rengini alır (.btn-base
// varsayılanı primary — nötr butonlar onu kullanır). Yalnız :focus-visible.
const odakHalkasi = 'focus-visible:ring-this/60 dark:focus-visible:ring-this-lighter/60'

function cx(...a: Array<string | false | null | undefined>): string {
  return a.filter(Boolean).join(' ')
}

export type ButtonGorunum = {
  color?: Color
  variant?: Variant
  isIcon?: boolean
  isGlow?: boolean
  unstyled?: boolean
  boyut?: Boyut
  className?: string
}

// sm: yoğun yerler (tablo satırı, araç çubuğu). isIcon'da boyutu className verir (size-8 vb.).
const BOYUT_SM = 'rounded-md px-3 py-1.5 text-xs-plus'
// md (varsayılan): 14px. Boyut verilmeyen butonlar sayfanın 16px'lik taban yazısını miras alıp
// fazla iri duruyordu; sm (13px) ile arasındaki ölçekte kalır. isIcon'da metin yok.
const BOYUT_MD = 'text-sm'

// butonSinifi — Button görünümünün TEK kaynağı. Buton gibi görünmesi gereken
// <a>/<Link> öğeleri de bunu kullanır: <a className={butonSinifi({ color: 'primary' })}>.
// eslint-disable-next-line react-refresh/only-export-components
export function butonSinifi({ color = 'neutral', variant = 'filled', isIcon = false, isGlow = false, unstyled = false, boyut = 'md', className }: ButtonGorunum = {}): string {
  return cx(
    'btn-base',
    !unstyled && 'btn',
    !unstyled && isIcon && 'shrink-0 p-0',
    !unstyled && !isIcon && boyut === 'sm' && BOYUT_SM,
    !unstyled && !isIcon && boyut === 'md' && BOYUT_MD,
    !unstyled && (color === 'neutral'
      ? cx(neutralVariants[variant], isGlow && 'dark:shadow-dark-450/5 shadow-lg shadow-gray-200/50')
      : cx(THIS[color], variants[variant], odakHalkasi, isGlow && 'shadow-soft shadow-this/50 dark:shadow-this/50 dark:shadow-lg')),
    unstyled && color !== 'neutral' && THIS[color],
    className,
  )
}

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  color?: Color
  variant?: Variant
  isIcon?: boolean
  isGlow?: boolean
  unstyled?: boolean
  boyut?: Boyut
  // ikon — metnin solundaki eylem ikonu (I.* yolu, bkz. components/Ikon). isIcon ile
  // birlikte verilirse butonun TEK içeriğidir (children gerekmez; aria-label ver).
  ikon?: string
  // ikonSonda — metnin sağında yön/dış bağlantı ikonu (I.ileri, I.harici …).
  ikonSonda?: string
  // yukleniyor — işlem sürerken buton içinde spinner gösterir + butonu devre
  // dışı bırakır (çift tıklama/çift işlem YASAK). Tüm buton yapılarında ortak.
  yukleniyor?: boolean
  children?: ReactNode
}

// Yukleyici — buton içi dönen spinner (Tailux akım rengini currentColor'dan alır).
function Yukleyici({ className }: { className: string }) {
  return (
    <svg className={cx('animate-spin shrink-0 motion-reduce:animate-[spin_1.6s_linear_infinite]', className)} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3.5" />
      <path className="opacity-90" fill="currentColor" d="M4 12a8 8 0 0 1 8-8V0C5.373 0 0 5.373 0 12h4z" />
    </svg>
  )
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, children, color = 'neutral', variant = 'filled', isIcon = false, isGlow = false, unstyled = false, boyut = 'md', ikon, ikonSonda, yukleniyor = false, disabled, type, ...rest },
  ref,
) {
  const olcu = boyut === 'sm' ? 'size-3.5' : 'size-4'
  // Metinli butonda ikon/spinner metinle arasında boşluk bırakır; yalnız-ikon butonda ortalanır.
  const bosluk = isIcon ? '' : boyut === 'sm' ? '-ms-0.5 me-1.5' : '-ms-1 me-2'
  // Yenile ikonu yüklenirken kendisi döner (ayrı spinner yerine) — aynı anlam, daha az sıçrama.
  const onIkon = yukleniyor
    ? (ikon === I.yenile
      ? <Ikon d={ikon} className={cx('animate-spin shrink-0', olcu, bosluk)} />
      : <Yukleyici className={cx(olcu, bosluk)} />)
    : ikon ? <Ikon d={ikon} className={cx('shrink-0', olcu, bosluk)} /> : null
  return (
    <button
      ref={ref}
      type={type || 'button'}
      disabled={disabled || yukleniyor}
      aria-busy={yukleniyor || undefined}
      className={butonSinifi({ color, variant, isIcon, isGlow, unstyled, boyut, className })}
      {...rest}
    >
      {onIkon}
      {yukleniyor && isIcon ? null : isIcon && ikon ? null : children}
      {ikonSonda && !isIcon && <Ikon d={ikonSonda} className={cx('shrink-0', olcu, boyut === 'sm' ? 'ms-1.5 -me-0.5' : 'ms-2 -me-1')} />}
    </button>
  )
})

export default Button
