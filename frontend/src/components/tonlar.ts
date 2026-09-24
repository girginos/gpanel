// Tek ton haritası — diyalog, modal başlığı, uyarı kutusu ve toast AYNI renk/ikon dilini
// konuşur. Bir bildirimin rengi ekrana göre değil, TONUNA göre belirlenir.
//
//   soru    → onay isteyen nötr soru (varsayılan onay/sor)     — primary
//   bilgi   → bilgilendirme                                    — info
//   basari  → işlem tamam                                      — success
//   uyari   → kısmen oldu / dikkat / geri alınabilir risk       — warning
//   tehlike → yıkıcı eylem ya da hata                          — error
//
// Renkler Tailux anlamsal token'larından gelir (`this:<renk>` köprüsü, styles.css);
// ham emerald/rose/amber sınıfı KULLANILMAZ.
import { I } from './Ikon'
import type { Color } from './ui'

export type Ton = 'soru' | 'bilgi' | 'basari' | 'uyari' | 'tehlike'

type TonStili = {
  // renk: `this:<renk>` sınıfının adı (bg-this, text-this… bununla boyanır).
  renk: Exclude<Color, 'neutral' | 'secondary'>
  ikon: string
  // buton: bu tondaki diyaloğun ana (onay) butonunun rengi.
  buton: Color
}

export const TONLAR: Record<Ton, TonStili> = {
  soru: { renk: 'primary', ikon: I.soru, buton: 'primary' },
  bilgi: { renk: 'info', ikon: I.bilgi, buton: 'primary' },
  basari: { renk: 'success', ikon: I.onayDaire, buton: 'primary' },
  uyari: { renk: 'warning', ikon: I.uyari, buton: 'warning' },
  tehlike: { renk: 'error', ikon: I.tehlike, buton: 'error' },
}

// `this:<renk>` sınıfı — styles.css'te tanımlı düz sınıflar (Tailwind taraması gerekmez).
export const tonSinifi = (ton: Ton): string => `this:${TONLAR[ton].renk}`
