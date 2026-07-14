// Panel dark/light/system tema yönetimi.
// - localStorage anahtarı: gosp.theme (light|dark|system)
// - system: prefers-color-scheme: dark medya sorgusuna göre
// - Sınıf: <html class="dark"> Tailwind darkMode: 'class' ile eşleşir.

export type Theme = 'light' | 'dark' | 'system'

const KEY = 'gosp.theme'

export function getTheme(): Theme {
  if (typeof window === 'undefined') return 'light'
  const v = localStorage.getItem(KEY) as Theme | null
  // Default: light — kullanıcı butondan seçmedikçe açık tema aç.
  return v === 'dark' || v === 'light' || v === 'system' ? v : 'light'
}

export function effectiveTheme(t: Theme): 'light' | 'dark' {
  if (t === 'system') {
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return t
}

export function applyTheme(t: Theme) {
  const eff = effectiveTheme(t)
  const html = document.documentElement
  if (eff === 'dark') html.classList.add('dark')
  else html.classList.remove('dark')
}

export function setTheme(t: Theme) {
  localStorage.setItem(KEY, t)
  applyTheme(t)
  window.dispatchEvent(new CustomEvent('gosp:theme-change', { detail: t }))
}

// Boot-time: ilk render öncesi çağır (main.tsx içinden).
// FOUC'u engellemek için main.tsx bunu import'tan sonra hemen çağırmalı.
export function bootTheme() {
  applyTheme(getTheme())
  // system tema seçiliyse OS tercihi değiştiğinde otomatik güncelle
  if (window.matchMedia) {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener?.('change', () => {
      if (getTheme() === 'system') applyTheme('system')
    })
  }
}
