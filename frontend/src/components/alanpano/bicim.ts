// AlanPano ortak yardımcıları: çeviri sözlüğü + sayı/boyut/zaman biçimleri + ton seçimi.
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import type { Ton } from '@/components/tonlar'

const ALANPANO_EN: Record<string, string> = {
  'Alan adı özeti': 'Domain overview',
  'Son güncelleme {0}': 'Last updated {0}',
  'Yenile': 'Refresh',
  'Yeniden dene': 'Retry',
  'Ayrıntı': 'Details',
  'Veri alınamadı': 'Could not load data',
  'Yenilenemedi — önceki veri gösteriliyor': 'Refresh failed — showing previous data',
  // Özet kartları
  'Disk': 'Disk',
  'Bu ay trafik': 'Traffic this month',
  'Veritabanı': 'Database',
  'Site sağlığı': 'Site health',
  'Son yedek': 'Last backup',
  'Sınırsız': 'Unlimited',
  '{0} kullanıldı': '{0} used',
  'Inode {0}': 'Inodes {0}',
  '{0} veritabanı': '{0} databases',
  '{0} / {1} veritabanı': '{0} / {1} databases',
  'Toplam boyut ölçülemedi': 'Total size not measured',
  'Erişilebilir': 'Reachable',
  'Erişilemiyor': 'Unreachable',
  'Hızlı': 'Fast',
  'Kabul edilebilir': 'Acceptable',
  'Yavaş': 'Slow',
  'Etkin': 'Active',
  'SSL yok': 'No SSL',
  'Site HTTPS ile korunmuyor': 'The site is not protected by HTTPS',
  'Süresi doldu': 'Expired',
  '{0} gün kaldı': '{0} days left',
  'Kendinden imzalı': 'Self-signed',
  'Bitiş {0}': 'Expires {0}',
  'SSL\'e git': 'Go to SSL',
  'Henüz yedek yok': 'No backups yet',
  'Yedeklere git': 'Go to backups',
  'Yerel · dosya mevcut': 'Local · file present',
  'Uzakta · doğrulanmadı': 'Remote · not verified',
  'Dosya bulunamadı': 'File not found',
  '7 günden eski': 'Older than 7 days',
  // Performans
  'Performans skoru': 'Performance score',
  'Mükemmel': 'Excellent',
  'İyi': 'Good',
  'Geliştirilebilir': 'Needs work',
  'Zayıf': 'Poor',
  '{0}/{1} hızlandırıcı açık': '{0}/{1} accelerators on',
  'Öneri yok — ayarlar yerinde.': 'No recommendations — settings look good.',
  'Performans skoru {0} / 100': 'Performance score {0} / 100',
  // Canlı
  'Canlı kaynak kullanımı': 'Live resource usage',
  'Canlı': 'Live',
  'Duraklatıldı': 'Paused',
  '10 sn\'de bir · son 5 dk': 'Every 10 s · last 5 min',
  'Sekme arka planda — yoklama durdu': 'Tab in background — polling paused',
  'Bellek': 'Memory',
  'Ölçülüyor…': 'Measuring…',
  'Sınır %{0}': 'Limit {0}%',
  'Sınır {0}': 'Limit {0}',
  'Disk okuma': 'Disk read',
  'Disk yazma': 'Disk write',
  'Süreç': 'Processes',
  'Bu sunucuda kiracı ölçümü yok': 'No tenant metrics on this server',
  'Kiracının kaynak grubu (cgroup) bulunamadı; canlı CPU/bellek gösterilemiyor.': 'The tenant resource group (cgroup) was not found; live CPU/memory cannot be shown.',
  'Canlı veri alınamadı': 'Could not load live data',
  'Canlı veri kesildi': 'Live data interrupted',
  'Yükleniyor…': 'Loading…',
  'CPU kullanımı, son {0} örnek: en düşük %{1}, en yüksek %{2}, şu an %{3}': 'CPU usage, last {0} samples: min {1}%, max {2}%, now {3}%',
  'Bellek kullanımı, son {0} örnek: en düşük {1}, en yüksek {2}, şu an {3}': 'Memory usage, last {0} samples: min {1}, max {2}, now {3}',
  // İstatistik
  'Günlük istekler': 'Daily requests',
  'Erişim günlüğü · son 7 gün': 'Access log · last 7 days',
  'Toplam istek': 'Total requests',
  'Bant': 'Bandwidth',
  'Tekil IP': 'Unique IPs',
  'Bot oranı': 'Bot ratio',
  'istek': 'requests',
  'Henüz erişim günlüğü verisi yok': 'No access log data yet',
  'Site trafik almaya başladığında burada görünür.': 'It will appear here once the site starts receiving traffic.',
  'Günlük istek sayısı, {0} gün: toplam {1}, en yüksek {2} ({3})': 'Daily request count, {0} days: total {1}, peak {2} ({3})',
  // Durum kodları
  'Durum kodları': 'Status codes',
  'HTTP yanıt dağılımı': 'HTTP response distribution',
  'Başarılı': 'Success',
  'Yönlendirme': 'Redirect',
  'İstemci hatası': 'Client error',
  'Sunucu hatası': 'Server error',
  'Durum kodu dağılımı: {0}': 'Status code distribution: {0}',
  // Aylık trafik
  'Aylık trafik': 'Monthly traffic',
  'Son 12 ay · gelen + giden': 'Last 12 months · in + out',
  'Son 12 ay · aktarılan (giden) veri': 'Last 12 months · transferred (outbound) data',
  'Aktarılan veri': 'Transferred data',
  'Gelen trafik ölçülmüyor': 'Inbound traffic is not measured',
  'Gelen': 'In',
  'Giden': 'Out',
  'Toplam': 'Total',
  'Kota': 'Quota',
  'Henüz trafik kaydı yok': 'No traffic recorded yet',
  'Aylık kovalar site trafik aldıkça dolar.': 'Monthly buckets fill as the site receives traffic.',
  'Aylık trafik, {0} ay: toplam {1}, en yüksek {2} ({3})': 'Monthly traffic, {0} months: total {1}, peak {2} ({3})',
  // Bildirimler
  'Bildirimler': 'Notifications',
  'Bu alan adına ait son kayıtlar': 'Recent entries for this domain',
  'Bu alan adı için bildirim yok': 'No notifications for this domain',
  'Tümü': 'All',
  'Okunmadı': 'Unread',
  'az önce': 'just now',
  '{0} dk önce': '{0} min ago',
  '{0} sa önce': '{0} h ago',
  '{0} gün önce': '{0} days ago',
}

export const cevir = (tr: string): string => (i18n.language === 'en' ? (ALANPANO_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// Şablon: cevirT(cevir(...)) deseni — önce yerel sözlükten şablon, sonra yer tutucular.
export function sablon(tr: string, ...args: unknown[]): string {
  let s = cevir(tr)
  args.forEach((a, i) => { s = s.split(`{${i}}`).join(String(a)) })
  return s
}

export const yerel = (): string => (i18n.language === 'en' ? 'en-US' : 'tr-TR')

export function sayi(n: number, basamak = 0): string {
  return (Number.isFinite(n) ? n : 0).toLocaleString(yerel(), { maximumFractionDigits: basamak })
}

export function mb(v: number): string {
  const x = Math.max(0, v || 0)
  if (x >= 1024 * 1024) return `${sayi(x / 1024 / 1024, 2)} TB`
  if (x >= 1024) return `${sayi(x / 1024, 1)} GB`
  return `${sayi(x, x < 1 ? 2 : x < 10 ? 1 : 0)} MB`
}

export function bayt(v: number): string {
  const x = Math.max(0, v || 0)
  if (x < 1024) return `${sayi(x)} B`
  if (x < 1024 ** 2) return `${sayi(x / 1024, 0)} KB`
  if (x < 1024 ** 3) return `${sayi(x / 1024 ** 2, 1)} MB`
  if (x < 1024 ** 4) return `${sayi(x / 1024 ** 3, 1)} GB`
  return `${sayi(x / 1024 ** 4, 2)} TB`
}

export const hiz = (bps: number): string => `${bayt(bps)}/s`

export const yuzde = (v: number, basamak = 0): string =>
  i18n.language === 'en' ? `${sayi(v, basamak)}%` : `%${sayi(v, basamak)}`

// CPU yüzdesi: boşta kiracı 0.02 gibi değerler verir — "%0" yerine "<%0,1" (sıfır değil).
export const cpuYuzde = (v: number): string =>
  v > 0 && v < 0.1 ? '<' + yuzde(0.1, 1) : yuzde(v, v < 10 ? 1 : 0)

// Arka uç zaman damgaları UTC ama dilim etiketi yok ("2026-09-24 08:15:02").
// Z eklenmezse UTC+3 kullanıcıda 3 saat kayar (bkz. TopBar goreliZaman).
export function sunucuZamani(t: string): Date | null {
  if (!t) return null
  const d = new Date(t.replace(' ', 'T') + (t.includes(' ') && !/[zZ]|[+-]\d\d:?\d\d$/.test(t) ? 'Z' : ''))
  return Number.isNaN(d.getTime()) ? null : d
}

export function goreli(d: Date | null, ham = ''): string {
  if (!d) return ham
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (!Number.isFinite(s) || s < 0) return tarihSaat(d)
  if (s < 60) return cevir('az önce')
  if (s < 3600) return sablon('{0} dk önce', Math.floor(s / 60))
  if (s < 86400) return sablon('{0} sa önce', Math.floor(s / 3600))
  if (s < 30 * 86400) return sablon('{0} gün önce', Math.floor(s / 86400))
  return tarihSaat(d)
}

export const tarih = (d: Date): string => d.toLocaleDateString(yerel(), { day: 'numeric', month: 'short', year: 'numeric' })
export const tarihSaat = (d: Date): string =>
  d.toLocaleString(yerel(), { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
export const saat = (d: Date, sn = false): string =>
  d.toLocaleTimeString(yerel(), sn ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } : { hour: '2-digit', minute: '2-digit' })

// nginx günlük tarihi "25/Sep/2026" → Date (yerel gece yarısı). Ayrıştırılamazsa null.
const AYLAR = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec']
export function nginxGunu(s: string): Date | null {
  const m = /^(\d{1,2})\/([A-Za-z]{3})\/(\d{4})$/.exec(s.trim())
  if (!m) return null
  const ay = AYLAR.indexOf(m[2].toLowerCase())
  if (ay < 0) return null
  return new Date(Number(m[3]), ay, Number(m[1]))
}

// Bayt ekseni için okunur tikler: 0'dan başlayan eşit adımlar (1/2/2.5/5 × 10ⁿ birim) —
// recharts'ın bayt üzerinde seçtiği 5,1 GB / 10,2 GB gibi tikler yerine 0 / 5 / 10 / 15 GB.
export function baytTikleri(max: number): number[] {
  if (!(max > 0)) return [0, 1]
  const birim = [1, 1024, 1024 ** 2, 1024 ** 3, 1024 ** 4].reduce((b, u) => (max / u >= 1 ? u : b), 1)
  const ham = max / birim / 3
  const us = 10 ** Math.floor(Math.log10(ham))
  const n = ham / us
  const adim = (n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10) * us * birim
  const t = [0]
  while (t[t.length - 1] < max) t.push(t[t.length - 1] + adim)
  return t
}

// "2026-09" → Date (ayın ilk günü)
export function ayTarihi(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})$/.exec(s)
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, 1) : null
}

// Doluluk oranına göre ton: %90+ tehlike, %75+ uyarı; sınırsızda (limit 0) ton yok.
export function dolulukTonu(kullanim: number, limit: number): Ton | null {
  if (!limit) return null
  const o = kullanim / limit
  return o >= 0.9 ? 'tehlike' : o >= 0.75 ? 'uyari' : null
}

// Bildirim seviyesi → ton (arka uç: kritik | uyari | bilgi; basari olası).
export function seviyeTonu(s: string): Exclude<Ton, 'soru'> {
  if (s === 'kritik' || s === 'hata') return 'tehlike'
  if (s === 'uyari') return 'uyari'
  if (s === 'basari') return 'basari'
  return 'bilgi'
}

// Grafik ızgarası/eksen yazısı temaya göre (recharts sınıflarını Tailwind ile boyar).
export const GRAFIK_SARGI =
  'text-xs [&_.recharts-cartesian-grid_line]:stroke-gray-200 dark:[&_.recharts-cartesian-grid_line]:stroke-dark-500 ' +
  '[&_.recharts-cartesian-axis-tick-value]:fill-gray-400 dark:[&_.recharts-cartesian-axis-tick-value]:fill-dark-300 ' +
  '[&_.recharts-reference-line_line]:stroke-gray-400 dark:[&_.recharts-reference-line_line]:stroke-dark-300 ' +
  '[&_.recharts-label]:fill-gray-500 dark:[&_.recharts-label]:fill-dark-200 ' +
  '[&_.recharts-tooltip-cursor]:fill-gray-150/60 dark:[&_.recharts-tooltip-cursor]:fill-dark-500/40'
