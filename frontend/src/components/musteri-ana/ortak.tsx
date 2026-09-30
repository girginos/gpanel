// Müşteri ana sayfası — küçük ortak parçalar (tema çekirdeği üstüne).
import { forwardRef, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button, butonSinifi } from '@/components/ui'
import type { ButtonProps, Color, Variant } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { useToast } from '@/components/Toast'
import { cevir, cevirF } from './dil'
import { panoyaKopyala } from './yardimci'
import type { AlanTuru } from './tipler'

// Dokunma hedefi: dar sayfada (kapsayıcı < 42rem) etkileşimli öğeler en az 40 px;
// geniş sayfada Tailux'un sıkı ölçüleri geri gelir.
export const DOKUN = 'min-h-10 @2xl/sayfa:min-h-0'
export const DOKUN_IKON = 'size-10 @2xl/sayfa:size-8'

// Tema çekirdeği Button'u — yalnız boyut="sm" iken dar sayfada 40 px yüksekliğe çıkar.
export const Dugme = forwardRef<HTMLButtonElement, ButtonProps>(function Dugme({ boyut, className = '', ...rest }, ref) {
  return <Button ref={ref} boyut={boyut} className={boyut === 'sm' ? `${DOKUN} ${className}` : className} {...rest} />
})

// Başarısız bir uç çekimi için "Yeniden dene" — sayfanın her yerinde aynı ton
// (neutral outlined + yenile ikonu, yenilerken döner) ve dar sayfada 40 px hedef.
export function YenidenDene({ uc, className = '' }: { uc: { yukleniyor: boolean; yenile: () => Promise<void> }; className?: string }) {
  return (
    <Dugme variant="outlined" boyut="sm" ikon={I.yenile} yukleniyor={uc.yukleniyor} className={className} onClick={() => void uc.yenile()}>
      {cevir('Yeniden dene')}
    </Dugme>
  )
}

// Buton görünümlü iç bağlantı (gezinme bir eylem değil, bağlantıdır: yeni sekmede
// açılabilir). İkon ölçüsü/boşluğu Button ile birebir aynı.
export function BaglantiDugme({
  to,
  ikon,
  ikonSonda,
  color = 'neutral',
  variant = 'outlined',
  className = '',
  children,
  ...rest
}: {
  to: string
  ikon?: string
  ikonSonda?: string
  color?: Color
  variant?: Variant
  className?: string
  children: ReactNode
  'aria-label'?: string
  title?: string
}) {
  return (
    <Link to={to} className={butonSinifi({ color, variant, boyut: 'sm', className: `${DOKUN} ${className}` })} {...rest}>
      {ikon && <Ikon d={ikon} className="-ms-0.5 me-1.5 size-3.5 shrink-0" />}
      {children}
      {ikonSonda && <Ikon d={ikonSonda} className="-me-0.5 ms-1.5 size-3.5 shrink-0" />}
    </Link>
  )
}

// Kapsamın görsel dili: hesap geneli = gri kare · alan adına özel = mavi kare + küre rozeti.
export function KapsamKaresi({ alan, ikon }: { alan: boolean; ikon: string }) {
  return (
    <span className="relative shrink-0" aria-hidden>
      <span
        className={`grid size-9 place-items-center rounded-lg ${
          alan ? 'bg-primary-50 text-primary-600 dark:bg-primary-400/10 dark:text-primary-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-dark-100'
        }`}
      >
        <Ikon d={ikon} className="size-[18px]" />
      </span>
      {alan && (
        <span className="absolute -bottom-1 -end-1 grid size-4 place-items-center rounded-full bg-white text-primary-600 ring-1 ring-primary-200 dark:bg-dark-700 dark:text-primary-300 dark:ring-primary-400/40">
          <Ikon d={I.kure} className="size-2.5" kalinlik={2.2} />
        </span>
      )}
    </span>
  )
}

const TUR_AD: Record<AlanTuru, string> = { ana: 'Ana', alt: 'Alt' }
const TUR_ACIKLAMA: Record<AlanTuru, string> = {
  ana: 'Ana alan adı — hesabın birincil sitesi',
  alt: 'Alt alan adı — ana alan adının altında',
}

export function TurRozeti({ tur }: { tur: AlanTuru }) {
  // Alt: nötr "soft" zemin açık temada neredeyse görünmüyor → kendi zeminini çizer
  // (unstyled: Badge'in renk sınıflarıyla çakışmasın).
  const alt = tur === 'alt'
  return (
    <Badge
      color={alt ? 'neutral' : 'primary'}
      variant="soft"
      unstyled={alt}
      title={cevir(TUR_ACIKLAMA[tur])}
      className={`shrink-0 px-1.5 py-0.5 text-tiny-plus ${alt ? 'badge bg-gray-150 text-gray-700 dark:bg-dark-500 dark:text-dark-100' : ''}`}
    >
      {cevir(TUR_AD[tur])}
    </Badge>
  )
}

export function KopyaDugmesi({ deger, etiket }: { deger: string; etiket: string }) {
  const toast = useToast()
  const [oldu, setOldu] = useState(false)
  const zaman = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(zaman.current), [])
  return (
    <Button
      isIcon
      variant="flat"
      ikon={oldu ? I.onay : I.kopya}
      className="-my-1.5 size-10 shrink-0 rounded-lg"
      aria-label={cevirF('{0} kopyala', etiket)}
      title={cevirF('{0} kopyala', etiket)}
      onClick={async () => {
        if (await panoyaKopyala(deger)) {
          toast.basari(cevir('Kopyalandı'), `${etiket}: ${deger}`)
          setOldu(true)
          window.clearTimeout(zaman.current)
          zaman.current = window.setTimeout(() => setOldu(false), 1600)
        } else {
          toast.hata(cevir('Kopyalanamadı'), cevir('Tarayıcı panoya erişime izin vermedi.'))
        }
      }}
    />
  )
}

// Başlıkla birlikte kart üst şeridi.
export function KartBasligi({ baslik, sag, id }: { baslik: string; sag?: ReactNode; id?: string }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-2.5 dark:border-dark-600">
      <h2 id={id} className="min-w-0 truncate text-sm font-semibold text-gray-800 dark:text-dark-50">
        {baslik}
      </h2>
      {sag}
    </div>
  )
}
