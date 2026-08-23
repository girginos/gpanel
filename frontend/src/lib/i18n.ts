// i18n.ts — Müşteri paneli çoklu dil altyapısı.
//
// Kapsam: ADMIN paneli Türkçe kalır; yalnız MÜŞTERİ-yüzü sayfalar (dosya, DB,
// mail, DNS, PHP, SSL, backup, WP...) ve giriş çevrilir. Diller: TR/EN/ES/FR/
// DE/NL/HI/RU. Şimdilik TR + EN dolu; diğer 6 dil dosyaları iskelet (EN'e
// düşer) — sonraki adımda otomatik çeviriyle doldurulacak.
//
// Metin bulunamazsa TR'ye (fallback) düşer — hiçbir zaman boş anahtar görünmez.

import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import tr from '@/locales/tr.json'
import en from '@/locales/en.json'

// Desteklenen diller — dil seçici bunları listeler.
export const DILLER = [
  { kod: 'tr', ad: 'Türkçe', bayrak: '🇹🇷' },
  { kod: 'en', ad: 'English', bayrak: '🇬🇧' },
] as const

export type DilKodu = (typeof DILLER)[number]['kod']

// Henüz çevrilmemiş diller EN kaynağını kullanır (boş görünmez); TR nihai fallback.
const resources = {
  tr: { translation: tr },
  en: { translation: en },
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'tr', // metin yoksa Türkçe göster
    supportedLngs: DILLER.map(d => d.kod),
    interpolation: { escapeValue: false }, // React zaten XSS-güvenli
    detection: {
      // Sıra: localStorage seçimi → tarayıcı dili. Panel host'a yazılmaz.
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'gosp.dil',
      caches: ['localStorage'],
    },
  })


// <html lang> senkron: ilk yukleme + her dil degisiminde (CSS uppercase locale dogru olsun; TR-locale I sorunu)
if (typeof document !== 'undefined') {
  document.documentElement.lang = i18n.language || 'tr'
  i18n.on('languageChanged', (lng) => { document.documentElement.lang = lng })
}

export default i18n
