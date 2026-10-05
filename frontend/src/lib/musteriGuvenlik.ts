// Hosting müşterisi "Website Security" — tipler, metinler ve saf yardımcılar (React'siz).
// Uçlar: GET /domains/{id}/guvenlik[/ozet|/bulgular], POST /domains/{id}/guvenlik/tara
// (internal/websec/musteri.go). Sunucu yanıtı yalnız müşterinin KENDİ hesabıdır; yol,
// kaynak, ham feed hatası ve kuyruk toplamı yanıtta yoktur.
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'

export type Onem = 'critical' | 'high' | 'medium' | 'low'
export const ONEMLER: Onem[] = ['critical', 'high', 'medium', 'low']
export type OnemSayac = Record<Onem, number>

export type GuvSatir = {
  domain_id: number
  ek_alan_id?: number // >0: ek alan adı satırı
  alan_adi: string
  ek: boolean
  app_type: string
  app_version: string
  uygulama_sayisi: number
  paket_sayisi: number
  bulgu_sayisi: number
  onem: OnemSayac
  domain_son_tarama: string
  son_sonuc?: { uygulama_sayisi: number; bulgu_sayisi: number }
  durum: string // taraniyor | sirada | acik | temiz | desteklenmiyor | beklemede
}

export type GuvOzet = OnemSayac & {
  toplam: number
  son_tarama: string
  taraniyor: boolean
  hedef_sayisi: number
  durum: 'acik' | 'temiz' | 'desteklenmiyor' | 'beklemede'
  feed_uyari: boolean
}

export type GuvListe = {
  ozet: GuvOzet
  tara: { soguma_dk: number; gunluk_sinir: number; gunluk_kalan: number }
  items: GuvSatir[]
  feed_uyari: boolean
}

export type GuvBulgu = {
  id: number
  ek_alan_id?: number
  alan_adi: string
  ek: boolean
  app_type: string
  paket: string
  surum: string
  cve: string
  onem: string
  cvss?: number
  baslik: string
  duzeltme_surumu: string
  oneri_kod: string
  oneri: string
  baglanti?: string
  son_gorulme: string
}

export type GuvBulguSayfa = { toplam: number; sayfa: number; sayfa_boyut: number; items: GuvBulgu[] }

export const GUV_EN: Record<string, string> = {
  'Website Security': 'Website Security',
  'Güvenlik': 'Security',
  'Sitenizdeki bilinen güvenlik açıkları': 'Known security vulnerabilities on your site',
  'Hesabınızdaki alan adlarını (ana ve ek alan adları) WordPress eklentileri, temalar ve Node.js / PHP Composer paketleri açısından bilinen güvenlik açıklarına karşı tarar.':
    'Checks the domains in your account (primary and add-on domains) for known vulnerabilities in WordPress plugins, themes and Node.js / PHP Composer packages.',
  'Güvenlik özeti': 'Security summary',
  'Kritik': 'Critical',
  'Yüksek': 'High',
  'Orta': 'Medium',
  'Düşük': 'Low',
  'Son tarama': 'Last scan',
  'Alan adı': 'Domain',
  'Tür': 'Type',
  'Paket': 'Packages',
  'Durum': 'Status',
  'İşlem': 'Action',
  'Ek': 'Add-on',
  'Ek alan adı': 'Add-on domain',
  'Tara': 'Scan',
  'Taranıyor…': 'Scanning…',
  'Sırada': 'Queued',
  'Temiz': 'Clean',
  'açık': 'open',
  'Uygulama yok': 'No app',
  'Beklemede': 'Pending',
  'Bu alan adında taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.':
    'No scannable application was found on this domain (WordPress, Node.js, PHP Composer). Scan again if you installed one.',
  'Henüz tarama yapılmadı': 'No scan has run yet',
  'Alan adlarınız ilk kez taranacak. Sonuçlar tarama bitince burada görünür.': 'Your domains will be scanned for the first time. Results appear here when the scan finishes.',
  'İlk taramayı başlat': 'Start the first scan',
  'Her alan adı en az {0} dakikada bir, hesabınız günde en çok {1} kez taranabilir. Bugün kalan hak: {2}.':
    'Each domain can be scanned at most once every {0} minutes, and your account up to {1} times a day. Remaining today: {2}.',
  'Alan adı satırına tıklayarak o alan adının açıklarını görün.': 'Click a domain row to see its vulnerabilities.',
  'Bu alan adının açıklarını göster': 'Show vulnerabilities for this domain',
  'Bu alan adının açıklarını gizle': 'Hide vulnerabilities for this domain',
  'Filtre:': 'Filter:',
  'Hepsi': 'All',
  'Kritik {0}': 'Critical {0}',
  'Yüksek {0}': 'High {0}',
  'Orta {0}': 'Medium {0}',
  'Düşük {0}': 'Low {0}',
  'Bu filtreyle açık yok.': 'No vulnerabilities for this filter.',
  'Bu alan adında bilinen açık bulunmadı.': 'No known vulnerabilities were found on this domain.',
  'Daha fazla göster': 'Show more',
  'Kurulu sürüm': 'Installed version',
  'Düzeltme sürümü': 'Fixed version',
  'Öneri': 'Recommendation',
  'Güvenlik bildirimi ({0}) — yeni sekmede açılır': 'Security advisory ({0}) — opens in a new tab',
  'Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.': 'Security data sources are currently unreachable; results may be out of date.',
  'Yükleniyor…': 'Loading…',
  'Güvenlik bilgileri alınamadı': 'Could not load security information',
  'Açıklar alınamadı': 'Could not load vulnerabilities',
  'Tekrar dene': 'Retry',
  'Bu sayfa hosting hesabı oturumuna özeldir. Hesapları görmek için Website Security Monitor sayfasını kullanın.': 'This page is for hosting account sessions. Use Website Security Monitor to see accounts.',
  'Alan adı bulunamadı.': 'Domain not found.',
  'Tarama sıraya alındı': 'Scan queued',
  '{0} sıradaki tarama.': '{0} in the scan queue.',
  'Zaten sırada ya da taranıyor': 'Already queued or scanning',
  'Bu alan adı için tarama zaten sırada ya da sürüyor.': 'A scan for this domain is already queued or in progress.',
  'Tarama sınırı': 'Scan limit',
  'Başlatılamadı': 'Could not start',
  'Tarama hatası': 'Scan error',
  'Tarama tamamlandı': 'Scan completed',
  '{0} uygulama tarandı, {1} bulgu.': '{0} applications scanned, {1} findings.',
  'Taranacak uygulama bulunamadı (WordPress, Node.js veya PHP Composer yok).': 'No application to scan was found (no WordPress, Node.js or PHP Composer).',
  'Tarama beklenenden uzun sürüyor': 'Scan is taking longer than expected',
  'Sonuç gelince bu sayfada görünür; sayfayı yenileyebilirsiniz.': 'The result will appear on this page; you can refresh it.',
  '{0} alan adı tarama sırasına alındı.': '{0} domains were queued for scanning.',
  'Tarama başlatılamadı. Lütfen biraz sonra tekrar deneyin.': 'Could not start the scan. Please try again shortly.',
  'Henüz taranmadı': 'Not scanned yet',
  'Tüm alan adlarını gör': 'View all domains',
  'Ayrıntılar': 'Details',
  '{0} açık': '{0} open',
  // Öneriler (oneri_kod)
  '{0} eklentisini {1} sürümüne ya da daha yenisine güncelleyin.': 'Update the {0} plugin to version {1} or later.',
  '{0} temasını {1} sürümüne ya da daha yenisine güncelleyin.': 'Update the {0} theme to version {1} or later.',
  '{0} paketini {1} sürümüne ya da daha yenisine yükseltin.': 'Upgrade the {0} package to version {1} or later.',
  'Henüz yama yok; eklentiyi devre dışı bırakmayı ya da başka bir eklentiyle değiştirmeyi düşünün.': 'No fix is available yet; consider deactivating the plugin or replacing it with another one.',
  'Henüz yama yok; temayı değiştirmeyi düşünün.': 'No fix is available yet; consider switching to another theme.',
  'Henüz yama yok; paketi kaldırmayı ya da güvenli bir alternatifle değiştirmeyi düşünün.': 'No fix is available yet; consider removing the package or replacing it with a safe alternative.',
}

export const cevir = (tr: string): string => (i18n.language === 'en' ? (GUV_EN[tr] || ORTAK_EN[tr] || tr) : tr)
export const sablon = (tr: string, ...args: unknown[]): string => {
  let s = cevir(tr)
  args.forEach((a, i) => { s = s.split('{' + i + '}').join(String(a)) })
  return s
}

// Sunucu zamanları MariaDB UTC "YYYY-AA-GG SS:DD:ss": lib/websecTarama.zamanYaz ile aynı
// biçimleyici kullanılır (sayfa import eder).

const ONERI_SABLON: Record<string, string> = {
  guncelle_eklenti: '{0} eklentisini {1} sürümüne ya da daha yenisine güncelleyin.',
  guncelle_tema: '{0} temasını {1} sürümüne ya da daha yenisine güncelleyin.',
  guncelle_paket: '{0} paketini {1} sürümüne ya da daha yenisine yükseltin.',
  yama_yok_eklenti: 'Henüz yama yok; eklentiyi devre dışı bırakmayı ya da başka bir eklentiyle değiştirmeyi düşünün.',
  yama_yok_tema: 'Henüz yama yok; temayı değiştirmeyi düşünün.',
  yama_yok_paket: 'Henüz yama yok; paketi kaldırmayı ya da güvenli bir alternatifle değiştirmeyi düşünün.',
}

// Bulgunun öneri metni, arayüz dilinde. Bilinmeyen kod → sunucunun Türkçe metni.
export function oneriMetni(b: Pick<GuvBulgu, 'oneri_kod' | 'oneri' | 'paket' | 'duzeltme_surumu'>): string {
  const s = ONERI_SABLON[b.oneri_kod]
  return s ? sablon(s, b.paket, b.duzeltme_surumu) : b.oneri
}

export const APP_AD: Record<string, string> = { wordpress: 'WordPress', nodejs: 'Node.js', 'php-composer': 'PHP Composer' }
export const appAd = (t: string): string => APP_AD[t] ?? (t || '—')

// Ana sayfa özet kartı: durum → metin + ton.
export function ozetGorunumu(o: Pick<GuvOzet, 'durum' | 'toplam'>): { metin: string; ton: 'error' | 'success' | 'warning' | 'neutral' } {
  switch (o.durum) {
    case 'acik': return { metin: sablon('{0} açık', o.toplam), ton: 'error' }
    case 'temiz': return { metin: cevir('Temiz'), ton: 'success' }
    case 'desteklenmiyor': return { metin: cevir('Uygulama yok'), ton: 'neutral' }
    default: return { metin: cevir('Henüz taranmadı'), ton: 'warning' }
  }
}
