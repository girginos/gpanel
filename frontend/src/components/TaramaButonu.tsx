// TaramaButonu — Website Security "Tara" eyleminin TEK görünümü (liste satırı + domain
// detay başlığı). Var olan ui/Button üzerine kurulu; yeni buton stili YOK.
//
// Durumlar (sunucunun domain durumundan türetilir, uydurma veri yok):
//   hazir        → primary, kalkan+büyüteç ikonu, etiket ("Tara" / "Bu Domaini Tara")
//   baslatiliyor → primary + dönen gösterge, "Başlatılıyor…", kilitli (istek sürüyor)
//   taraniyor    → info + dönen gösterge, "Taranıyor…", kilitli
//   sirada       → info + saat ikonu, "Sırada", kilitli
// acikSayisi > 0 (son taramada açık bulundu) ve hazir → sağ üst köşede kırmızı nokta;
// anlamı title + ekran okuyucu metniyle verilir (renk tek başına anlam taşımaz).
// Mobilde dokunma hedefi ≥ 40px (max-sm:min-h-10).
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import type { MouseEvent } from 'react'
import { Button } from '@/components/ui'
import type { Boyut, Variant } from '@/components/ui'
import { I } from '@/components/Ikon'

export type TaramaDurumu = 'hazir' | 'baslatiliyor' | 'taraniyor' | 'sirada'

const EN: Record<string, string> = {
  "Tara": "Scan",
  "Başlatılıyor…": "Starting…",
  "Taranıyor…": "Scanning…",
  "Sırada": "Queued",
  "Son taramada {0} açık bulundu — yeniden tara": "{0} vulnerabilities found in the last scan — scan again",
  "Tarama sürüyor": "Scan in progress",
  "Tarama sırasında bekliyor": "Waiting in the scan queue",
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || tr) : tr)

type Props = {
  durum: TaramaDurumu
  onClick: (e: MouseEvent<HTMLButtonElement>) => void
  // hazir durumundaki etiket (çevrilmiş); vars. "Tara"
  etiket?: string
  // sm: tablo satırı (soft) · md: sayfa başlığı (filled)
  boyut?: Boyut
  // son taramadaki açık sayısı (domain durumu 'acik' iken bulgu_sayisi)
  acikSayisi?: number
  // tarama hazır olsa da başka bir nedenle kapalı (ör. yönetici: sunucu geneli tarama sürüyor)
  kilitli?: boolean
  kilitNedeni?: string
  // ekran okuyucu bağlamı (ör. alan adı) → "alan: Tara"
  baglam?: string
  className?: string
}

export function TaramaButonu({ durum, onClick, etiket, boyut = 'sm', acikSayisi = 0, kilitli = false, kilitNedeni, baglam, className }: Props) {
  useTranslation() // dil re-render aboneliği
  const variant: Variant = boyut === 'sm' ? 'soft' : 'filled'
  const mesgul = durum === 'baslatiliyor' || durum === 'taraniyor'
  const metin = durum === 'baslatiliyor' ? cevir("Başlatılıyor…")
    : durum === 'taraniyor' ? cevir("Taranıyor…")
    : durum === 'sirada' ? cevir("Sırada")
    : (etiket ?? cevir("Tara"))
  const riskli = durum === 'hazir' && acikSayisi > 0
  const riskMetni = riskli ? cevir("Son taramada {0} açık bulundu — yeniden tara").replace('{0}', String(acikSayisi)) : ''
  const ipucu = durum === 'taraniyor' ? cevir("Tarama sürüyor")
    : durum === 'sirada' ? cevir("Tarama sırasında bekliyor")
    : (kilitli && kilitNedeni) ? kilitNedeni
    : riskMetni || undefined

  return (
    <span className={`relative inline-flex shrink-0 ${className ?? ''}`} title={ipucu}>
      <Button
        boyut={boyut}
        variant={variant}
        color={durum === 'taraniyor' || durum === 'sirada' ? 'info' : 'primary'}
        ikon={durum === 'sirada' ? I.saat : I.kalkanTara}
        yukleniyor={mesgul}
        disabled={kilitli || durum === 'sirada'}
        onClick={onClick}
        aria-label={baglam ? `${baglam}: ${metin}${riskMetni ? ` (${riskMetni})` : ''}` : undefined}
        className={`max-sm:min-h-10 whitespace-nowrap font-semibold ${boyut === 'sm' ? 'min-w-[5.5rem]' : ''}`}
      >
        {metin}
        {riskli && !baglam && <span className="sr-only"> ({riskMetni})</span>}
      </Button>
      {riskli && (
        <span aria-hidden="true"
          className="pointer-events-none absolute -end-1 -top-1 size-2.5 rounded-full bg-error ring-2 ring-white dark:ring-dark-900" />
      )}
    </span>
  )
}

export default TaramaButonu
