// Alan adı kaynak kartı (DomainKaynakKart) biçim yardımcısı.
// Not: alan adı özet panosu (AlanPano) 2026-09-29'da alan adı sayfasından kaldırıldı
// (kullanıcı kararı: kafa karıştırıyordu; bu tür widget'lar bayi panosuna taşınacak).
// Grafik/özet kartlarının kaynağı git geçmişinde: 777d409:frontend/src/components/alanpano/.
import i18n from '@/lib/i18n'

const yerel = (): string => (i18n.language === 'en' ? 'en-US' : 'tr-TR')

function sayi(n: number, basamak = 0): string {
  return (Number.isFinite(n) ? n : 0).toLocaleString(yerel(), { maximumFractionDigits: basamak })
}

export function mb(v: number): string {
  const x = Math.max(0, v || 0)
  if (x >= 1024 * 1024) return `${sayi(x / 1024 / 1024, 2)} TB`
  if (x >= 1024) return `${sayi(x / 1024, 1)} GB`
  return `${sayi(x, x < 1 ? 2 : x < 10 ? 1 : 0)} MB`
}
