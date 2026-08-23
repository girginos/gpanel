import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import Breadcrumb from '@/components/Breadcrumb'

interface Props {
  baslik: string
  aciklama: string
  ikon: string
  ozellikler: string[]
}


const YAKINDA_EN: Record<string, string> = {
  "Planlanan Özellikler": "Planned Features",
  "Yakında": "Coming Soon",
  "Yol Haritası": "Roadmap",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (YAKINDA_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function YakindaPage({ baslik, aciklama, ikon, ozellikler }: Props) {
  useTranslation() // dil re-render aboneligi
  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: baslik },
      ]} />

      <div className="flex items-center gap-3 mb-2">
        <span className="text-3xl">{ikon}</span>
        <div>
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{baslik}</h1>
          <p className="text-sm text-slate-500 dark:text-slate-500">{aciklama}</p>
        </div>
      </div>

      <div className="bg-gradient-to-br from-brand-50/40 to-indigo-50/40 border-2 border-dashed border-brand-200 dark:border-brand-800 rounded-2xl p-8 mt-6">
        <div className="flex items-center gap-2 mb-4">
          <span className="text-[10px] uppercase tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-2 py-0.5 rounded font-bold">{cevir("Yakında")}</span>
          <span className="text-xs text-slate-500 dark:text-slate-500">{cevir("Yol Haritası")}</span>
        </div>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Planlanan Özellikler")}</h3>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
          {ozellikler.map(o => (
            <div key={o} className="flex items-start gap-2 px-3 py-2 bg-white dark:bg-slate-800/80 rounded border border-slate-100 dark:border-slate-800">
              <span className="text-emerald-500 flex-shrink-0">○</span>
              <span className="text-sm text-slate-700 dark:text-slate-300">{o}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}