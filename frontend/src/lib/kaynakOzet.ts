import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'

// kaynakOzet — bayi/paket liste hücresi için kaynak havuzu özeti ("2 çekirdek · 4 GB").
// CPU ve RAM'in ikisi de 0 ise "sınırsız" (havuz tanımlı değil).

const KO_EN: Record<string, string> = {
  "çekirdek": "cores",
  "sınırsız": "unlimited",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (KO_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export function kaynakOzet(cpu?: number, ramMB?: number): string {
  const p: string[] = []
  if (cpu && cpu > 0) p.push(`${cpu} ${cevir('çekirdek')}`)
  if (ramMB && ramMB > 0) p.push(ramMB >= 1024 ? `${(ramMB / 1024).toFixed(ramMB % 1024 === 0 ? 0 : 1)} GB` : `${ramMB} MB`)
  return p.length ? p.join(' · ') : cevir('sınırsız')
}
