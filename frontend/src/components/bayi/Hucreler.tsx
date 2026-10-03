// Bayi listelerinin (Domainler + Pano "son 10") ortak kullanım hücresi: kullanılan
// miktar, kota varsa "x / y" + ince çubuk; sınırsızsa yalnız kullanılan.
import { Cubuk } from './Grafik'
import { mbYaz, yuzde } from './ortak'

export function KullanimHucre({ kullanilanMB, kotaMB, etiket }: { kullanilanMB: number; kotaMB?: number; etiket: string }) {
  const kota = kotaMB && kotaMB > 0 ? kotaMB : 0
  return (
    <div className="w-full max-w-[9.5rem]">
      <div className="text-xs tabular-nums text-gray-700 dark:text-dark-200">
        {mbYaz(kullanilanMB)}{kota > 0 && <span className="text-gray-400 dark:text-dark-300"> / {mbYaz(kota)}</span>}
      </div>
      {kota > 0 && <div className="mt-1"><Cubuk deger={yuzde(kullanilanMB, kota)} etiket={etiket} /></div>}
    </div>
  )
}
