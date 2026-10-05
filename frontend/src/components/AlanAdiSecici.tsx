// AlanAdiSecici — hesabın alan adlarından (ana / ek / alt) birini seçtiren ORTAK seçici.
// Tür rozetli (Ana / Ek / Alt), pasif seçenek + ipucu destekler; yerel <select> (klavye +
// ekran okuyucu hazır), sürüklenmez. SSL, WordPress ve günlük sayfaları aynı bileşeni
// kullanır (birleştirmede tek kopya).
//
// Kullanım:
//   <AlanAdiSecici secenekler={[{ deger: 'ana', alanAdi: 'a.com', tur: 'ana' },
//                               { deger: 'ek:3', alanAdi: 'b.com', tur: 'ek', pasif: true, ipucu: 'Hesap askıda' }]}
//     deger={sec} onDegis={setSec} tumuEtiketi="Tüm alan adları" />
import i18n from '@/lib/i18n'
import { Badge, Select } from '@/components/ui'

export type AlanAdiTuru = 'ana' | 'ek' | 'alt'
export type AlanAdiSecenek = {
  deger: string // benzersiz anahtar (ör. 'ana', 'ek:3', 'alt:7')
  alanAdi: string
  tur: AlanAdiTuru
  pasif?: boolean
  ipucu?: string // pasif seçeneğin nedeni; seçenek metnine eklenir
}

const TUR_TR: Record<AlanAdiTuru, string> = { ana: 'Ana', ek: 'Ek', alt: 'Alt' }
const TUR_EN: Record<AlanAdiTuru, string> = { ana: 'Primary', ek: 'Addon', alt: 'Sub' }
const TUR_RENK: Record<AlanAdiTuru, 'primary' | 'info' | 'neutral'> = { ana: 'primary', ek: 'info', alt: 'neutral' }

export function turEtiketi(tur: AlanAdiTuru): string {
  return i18n.language === 'en' ? TUR_EN[tur] : TUR_TR[tur]
}

// TurRozeti — tablo/liste satırlarında da kullanılan küçük tür rozeti.
export function TurRozeti({ tur, className = '' }: { tur: AlanAdiTuru; className?: string }) {
  return (
    <Badge color={TUR_RENK[tur]} variant="soft" className={`shrink-0 px-1.5 py-0.5 text-tiny-plus font-semibold ${className}`}>
      {turEtiketi(tur)}
    </Badge>
  )
}

export type AlanAdiSeciciProps = {
  secenekler: AlanAdiSecenek[]
  deger: string
  onDegis: (deger: string) => void
  // verilirse listenin başına deger='' olan "tümü" seçeneği eklenir
  tumuEtiketi?: string
  etiket?: string
  ariaEtiket?: string
  disabled?: boolean
  className?: string
}

export default function AlanAdiSecici({ secenekler, deger, onDegis, tumuEtiketi, etiket, ariaEtiket, disabled, className = '' }: AlanAdiSeciciProps) {
  const data = [
    ...(tumuEtiketi ? [{ value: '', label: tumuEtiketi }] : []),
    ...secenekler.map(s => ({
      value: s.deger,
      label: `${s.alanAdi} · ${turEtiketi(s.tur)}${s.pasif && s.ipucu ? ` — ${s.ipucu}` : ''}`,
      disabled: !!s.pasif,
    })),
  ]
  const secili = secenekler.find(s => s.deger === deger)
  return (
    <div className={`flex min-w-0 items-end gap-2 ${className}`} draggable={false} onDragStart={e => e.preventDefault()}>
      <Select
        label={etiket}
        aria-label={etiket ? undefined : ariaEtiket}
        value={deger}
        onChange={e => onDegis(e.target.value)}
        data={data}
        disabled={disabled}
        classNames={{ root: 'min-w-0 flex-1' }}
      />
      {secili && <TurRozeti tur={secili.tur} className="mb-2" />}
    </div>
  )
}
