import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// cn — shadcn deseninin standart yardimcisi. Projede YOKTU, `@/lib/utils`ten
// import eden bilesenler icin eklendi.
//
// clsx: kosullu/dizi/nesne sinif birlestirme.
// twMerge: CAKISAN Tailwind siniflarinda SONUNCUYU kazandirir — `cn('p-2','p-4')`
// duz birlestirmede ikisini de birakir ve hangisinin kazandigi CSS sirasina
// kalirdi; twMerge `p-4` dondurur.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
