// cevirT: SOV→SVO template cevirisi. Sablon TR (degiskenler {0},{1}… placeholder);
// mega'da EN template (dogru kelime sirasi); args placeholder'a yerlesir.
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
export function cevirT(sablon: string, ...args: unknown[]): string {
  let s = i18n.language === 'en' ? (ORTAK_EN[sablon] || sablon) : sablon
  args.forEach((a, i) => { s = s.split('{' + i + '}').join(String(a)) })
  return s
}
