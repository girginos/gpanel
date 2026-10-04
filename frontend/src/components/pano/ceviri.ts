// Pano widget metinleri — TR anahtar → EN (bileşenin yerel haritası; plan A.8).
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'

const EN: Record<string, string> = {
  'İstatistikler': 'Statistics',
  'Tümünü gör': 'View all',
  'Pano istatistikleri': 'Dashboard statistics',
  'Yenile': 'Refresh',
  'Yeniden dene': 'Retry',
  'Veri alınamadı': 'Could not load data',
  // Eğilim
  'Disk ve trafik eğilimi': 'Disk and traffic trend',
  'Son 30 gün': 'Last 30 days',
  'Toplam disk': 'Total disk',
  'Günlük trafik': 'Daily traffic',
  'Veri toplanıyor': 'Collecting data',
  'Geçmiş, bu sürümden itibaren günde bir birikir; eğilim için en az iki gün gerekir.': 'History accumulates daily from this version on; a trend needs at least two days.',
  '{0} gündür toplanıyor': 'collecting for {0} day(s)',
  'son {0} günde': 'in the last {0} days',
  'değişim yok': 'no change',
  'Son disk ölçümü': 'Last disk measurement',
  'Disk kotası ölçümü kapalı; değerler son elle ölçümden.': 'Disk quota accounting is off; values come from the last manual measurement.',
  // Kota
  'Yakında dolacak hesaplar': 'Accounts nearing their limits',
  'Kotasının %{0} kadarını aşan': 'Using more than {0}% of their quota',
  'Kotasının %85\'ini aşan hesap yok': 'No account is above 85% of its quota',
  'Disk': 'Disk',
  'Trafik': 'Traffic',
  've {0} hesap daha': 'and {0} more account(s)',
  // SSL
  'SSL sertifikaları': 'SSL certificates',
  'Süresi doldu': 'Expired',
  '14 gün içinde': 'Within 14 days',
  "SSL'siz aktif": 'Active without SSL',
  '30 gün içinde dolacak sertifika yok': 'No certificate expires within 30 days',
  '{0} gün önce doldu': 'expired {0} day(s) ago',
  'bugün doluyor': 'expires today',
  '{0} gün kaldı': '{0} day(s) left',
  'Son tarama': 'Last scan',
  // Askı
  'Son askıya alınanlar': 'Recently suspended',
  'Askıda hesap yok': 'No suspended accounts',
  'Yönetici': 'Administrator',
  'Bayi': 'Reseller',
  'Bayi hesabı askıda': 'Reseller account suspended',
  'Toplam {0}': 'Total {0}',
  // Websec
  'Website Security bulguları': 'Website Security findings',
  'Kritik': 'Critical', 'Yüksek': 'High', 'Orta': 'Medium', 'Düşük': 'Low',
  'Kritik/yüksek önemli bulgu yok': 'No critical/high findings',
  'Henüz tarama yapılmadı': 'No scan yet',
  'Düzeltilmiş sürüm {0}': 'Fixed in {0}',
}

export function pt(tr: string, ...args: unknown[]): string {
  let s = i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr
  args.forEach((a, i) => { s = s.split('{' + i + '}').join(String(a)) })
  return s
}
