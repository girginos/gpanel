// Tema düğmesi — açık → koyu → sistem döngüsü. Üst çubuk ve tam ekran sayfalar (askı)
// aynı düğmeyi kullanır; başka yerde seçilen tema 'gosp:theme-change' ile yansır.
import { useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { getTheme, setTheme, type Theme } from '@/lib/theme'
import { Button } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'

const TEMA_EN: Record<string, string> = { 'Açık': 'Light', 'Koyu': 'Dark', 'Sistem': 'System' }
const cevir = (tr: string): string => (i18n.language === 'en' ? (TEMA_EN[tr] || ORTAK_EN[tr] || tr) : tr)
const TEMA_ADI: Record<Theme, string> = { light: 'Açık', dark: 'Koyu', system: 'Sistem' }

export default function TemaDugmesi() {
  useTranslation() // dil re-render aboneligi
  const [tema, setTema] = useState<Theme>(getTheme())

  useEffect(() => {
    const h = (e: Event) => setTema((e as CustomEvent<Theme>).detail)
    window.addEventListener('gosp:theme-change', h)
    return () => window.removeEventListener('gosp:theme-change', h)
  }, [])

  function degistir() {
    const siradaki: Theme = tema === 'light' ? 'dark' : tema === 'dark' ? 'system' : 'light'
    setTheme(siradaki)
    setTema(siradaki)
  }

  const etiket = cevirT(cevir('Tema: {0} — tıkla değiştir'), cevir(TEMA_ADI[tema]))
  return (
    <Button isIcon variant="flat" onClick={degistir} className="size-9 rounded-full" aria-label={etiket} title={etiket}>
      <Ikon d={tema === 'dark' ? I.ay : tema === 'light' ? I.gunes : I.monitor} kalinlik={1.8} className="w-5 h-5" />
    </Button>
  )
}
