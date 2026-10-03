// Bayi paneli (Seçenek 1) ortak yardımcıları: tip, sınıflar, tarih/boyut biçimleri.
// Görsel parçalar (çubuk, halka grafik) Grafik.tsx'te.
import i18n from '@/lib/i18n'

export const cx = (...a: Array<string | false | null | undefined>) => a.filter(Boolean).join(' ')
export const ODAK = 'outline-none focus-visible:ring-2 focus-visible:ring-primary-600/70 dark:focus-visible:ring-primary-400/70'
export const KART = 'rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-700'

export type BayiDomain = {
  id: number
  alan_adi: string
  plan_id?: number
  plan_ad?: string
  boyut_kb: number
  disk_kota_mb?: number
  trafik_kb?: number
  trafik_kota_mb?: number
  ipv4?: string
  askida?: boolean
  olusturulma?: string
  ssl?: boolean
  ssl_bitis?: string
  sahip_ad?: string
}

export const yuzde = (k: number, l: number) => (l > 0 ? Math.min(100, Math.round((k / l) * 100)) : 0)
// Disk yüzdesi: kota 0 (sınırsız/plansız) → null (yüzde gösterilmez).
export const diskYuzde = (d: Pick<BayiDomain, 'boyut_kb' | 'disk_kota_mb'>): number | null =>
  d.disk_kota_mb && d.disk_kota_mb > 0 ? yuzde(d.boyut_kb / 1024, d.disk_kota_mb) : null
export const mbYaz = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toLocaleString(dilKodu(), { maximumFractionDigits: 1 })} GB` : `${Math.round(mb)} MB`)
const dilKodu = () => (i18n.language === 'en' ? 'en-US' : 'tr-TR')
// "2026-03-14" → TR "14 Mar 2026", EN "Mar 14, 2026"
export const tarihYaz = (iso?: string) => {
  if (!iso) return '—'
  const d = new Date(`${iso.slice(0, 10)}T12:00:00`)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(dilKodu(), { day: 'numeric', month: 'short', year: 'numeric' })
}
// SSL alınamadı: SSL açık ama sertifikanın süresi DOLMUŞ (acme otomatik yenilemesi
// başarısız). Yenilenen sertifika bitmez; SSL sütunu bu yüzden yok, yalnız hata uyarısı.
export const sslHatali = (d: Pick<BayiDomain, 'ssl' | 'ssl_bitis' | 'askida'>) =>
  !!d.ssl && !d.askida && !!d.ssl_bitis && d.ssl_bitis.slice(0, 10) < new Date().toISOString().slice(0, 10)
