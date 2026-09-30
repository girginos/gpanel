// Müşteri ana sayfası — dil yardımcıları. Görünen her metin TR anahtardır; EN'de önce
// bu sayfanın haritası, sonra araç kataloğu (ARAC_EN), sonra ortak sözlük (ORTAK_EN)
// aranır; bulunamazsa TR kalır. Yer tutucular {0},{1}… (EN'de kelime sırası değişebilir).
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { ARAC_EN } from './araclar'

export const MUSTERI_ANA_EN: Record<string, string> = {
  // Sayfa / genel
  'Yükleniyor…': 'Loading…',
  'Hesap bilgileri': 'Account information',
  'Hesap bilgileri alınamadı': 'Could not load account information',
  'Kaynak bilgisi alınamadı': 'Could not load resource information',
  'Yeniden dene': 'Try again',
  'Ana sayfa': 'Home',
  '{0} — ana sayfa': '{0} — home',
  // Araç ızgarası
  'Araçlar': 'Tools',
  '{0} araç · {1} kategori': '{0} tools · {1} categories',
  'Araç ara… (ör. ssl, yedek, dns)': 'Search tools… (e.g. ssl, backup, dns)',
  'Araç ya da alan adı ara': 'Search tools or domains',
  'Aramayı temizle': 'Clear search',
  'Kapsama göre süz': 'Filter by scope',
  'Tümü': 'All',
  'Hesap geneli': 'Account-wide',
  'Alan adı başına': 'Per domain',
  'Tümünü aç': 'Expand all',
  'Tümünü daralt': 'Collapse all',
  '{0} sistem kullanıcısına ait; tüm alan adlarında ortak.': 'Owned by system user {0}; shared by all domains.',
  'Şu alan adında açılır:': 'Opens for this domain:',
  'Seçili alan adı': 'Selected domain',
  'Alt alan adı sayfası olmayan araçlar (ör. DNS, WAF) ana alan adında ({0}) açılır.': 'Tools without a subdomain page (e.g. DNS, WAF) open for the primary domain ({0}).',
  'Alan adı başına araçlar artık {0} için açılır.': 'Per-domain tools now open for {0}.',
  '“{0}” için {1} sonuç': '{1} results for “{0}”',
  'Enter ilk sonucu açar': 'Enter opens the first result',
  'Araçları bu alan adında aç': 'Open tools for this domain',
  '“{0}” ile eşleşen araç yok': 'No tools match “{0}”',
  'Başka bir sözcük deneyin ya da kapsam süzgecini kaldırın.': 'Try another word or remove the scope filter.',
  'ssl, yedek, dns, php, veritabanı': 'ssl, backup, dns, php, database',
  'Süzgeci kaldır': 'Remove filter',
  '{0} — {1} için açılır': '{0} — opens for {1}',
  '{0} — hesap geneli': '{0} — account-wide',
  '{0} için açılır': 'opens for {0}',
  'E-posta hizmetinin lisansı askıda; e-posta araçları lisans yenilenene kadar kullanılamaz. Lütfen hizmet sağlayıcınızla iletişime geçin.':
    'The email service licence is suspended; email tools are unavailable until it is renewed. Please contact your hosting provider.',
  // Tür rozetleri
  'Ana': 'Primary',
  'Alt': 'Sub',
  'Ana alan adı — hesabın birincil sitesi': 'Primary domain — the account’s main site',
  'Alt alan adı — ana alan adının altında': 'Subdomain — lives under the primary domain',
  // Kopyalama
  '{0} kopyala': 'Copy {0}',
  'Kopyalandı': 'Copied',
  'Kopyalanamadı': 'Could not copy',
  'Tarayıcı panoya erişime izin vermedi.': 'The browser blocked clipboard access.',
  // Bilgi paneli
  'Hesap özeti': 'Account summary',
  'Disk': 'Disk',
  '{0} alan adı': '{0} domains',
  'Limite yaklaşan kullanım var': 'Some usage is near its limit',
  'Genel bilgi': 'General information',
  'Ana alan adı': 'Primary domain',
  '{0} — yeni sekmede açılır': '{0} — opens in a new tab',
  'yeni sekmede açılır': 'opens in a new tab',
  'Durum': 'Status',
  'Aktif': 'Active',
  'Pasif': 'Inactive',
  'Yok': 'None',
  'Süresi doldu': 'Expired',
  '{0} gün kaldı': '{0} days left',
  'Geçerli': 'Valid',
  'Geçerli · {0} gün': 'Valid · {0} days',
  'PHP sürümü': 'PHP version',
  'desteği bitti': 'end of life',
  'Bu sürüm artık güvenlik güncellemesi almıyor.': 'This version no longer gets security updates.',
  'Sistem kullanıcısı': 'System user',
  'IP adresi': 'IP address',
  'Son yedek': 'Last backup',
  'Hiç alınmadı': 'Never',
  'Okunamadı': 'Unavailable',
  'Oluşturulma': 'Created',
  'Bağlantı bilgileri': 'Connection details',
  'FTP sunucusu': 'FTP server',
  'FTP kullanıcısı': 'FTP user',
  'MySQL sunucusu': 'MySQL server',
  'Veritabanı kullanıcısı': 'Database user',
  'Açık': 'On',
  'Kapalı': 'Off',
  'Ayrıntılar': 'Details',
  'Tüm bağlantı bilgileri': 'All connection details',
  'Kullanım istatistikleri': 'Usage statistics',
  'Kullanımı yenile': 'Refresh usage',
  'Kullanım bilgisi alınamadı.': 'Could not load usage information.',
  'Son yenileme başarısız; değerler eski olabilir.': 'The last refresh failed; values may be out of date.',
  'Sınırsız': 'Unlimited',
  'Disk alanı': 'Disk space',
  'Dosya sayısı (inode)': 'Files (inodes)',
  'Aylık trafik': 'Monthly traffic',
  'FTP hesapları': 'FTP accounts',
  'E-posta hesapları': 'Email accounts',
  'Limite yaklaşıldı': 'Approaching limit',
  'Limit doldu': 'Limit reached',
  '{0}: {1} / {2}': '{0}: {1} of {2}',
  'Alan adları': 'Domains',
  'Ekle': 'Add',
  'Alt alan adı ekle': 'Add subdomain',
  'Alan adına özel araçları {0} için aç': 'Open per-domain tools for {0}',
  '{0} alt alan adını yönet': 'Manage subdomain {0}',
  'Alt alan adları alınamadı.': 'Could not load subdomains.',
  'Alt alan adları yükleniyor…': 'Loading subdomains…',
  '1 ana alan adı · {0} alt alan adı': '1 primary domain · {0} subdomains',
  'SSL yok': 'No SSL',
  'SSL süresi doldu': 'SSL expired',
  'SSL {0} gün içinde bitiyor': 'SSL expires in {0} days',
  'SSL geçerli': 'SSL valid',
  'PHP {0} — desteği bitti': 'PHP {0} — end of life',
  // Uyarı bandı
  'Uyarılar': 'Alerts',
  'Uyarılar ({0})': 'Alerts ({0})',
  'Tümü ({0})': 'All ({0})',
  'Daralt': 'Collapse',
  'Gizle': 'Hide',
  'Uyarıyı gizle: {0}': 'Hide alert: {0}',
  '{0} HTTPS ile korunmuyor': '{0} is not protected by HTTPS',
  'Ziyaretçiler tarayıcıda "güvenli değil" uyarısı görür.': 'Visitors see a “Not secure” warning in their browser.',
  'SSL kur': 'Install SSL',
  '{0} SSL sertifikasının süresi doldu': 'The SSL certificate for {0} has expired',
  'Ziyaretçiler tarayıcıda güvenlik uyarısı görür; sertifikayı yenileyin.': 'Visitors see a security warning in their browser; renew the certificate.',
  'SSL sayfasını aç': 'Open SSL page',
  '{0} SSL sertifikası {1} gün içinde bitiyor': 'SSL certificate for {0} expires in {1} days',
  'Otomatik yenileme gecikmiş olabilir; durumu SSL sayfasından kontrol edin.': 'Automatic renewal may be delayed; check its status on the SSL page.',
  '{0} limiti doldu': '{0} limit reached',
  '{0} {1} dolu': '{0} is {1} full',
  '{0} / {1} kullanılıyor.': '{0} of {1} in use.',
  'Yer açmak için gereksiz dosyaları ve eski yedekleri temizleyin.': 'Free up space by removing unneeded files and old backups.',
  'Önbellek ve geçici dosyaları temizleyerek dosya sayısını azaltın.': 'Reduce the file count by clearing cache and temporary files.',
  'Limit dolarsa siteye erişim kısıtlanabilir.': 'Access to the site may be restricted once the limit is reached.',
  'Geçerli bir yedeğiniz yok': 'You have no valid backup',
  'Kayıtlı yedeklerin dosyaları bulunamadı; yeni bir yedek alın.': 'The files of your recorded backups could not be found; take a new backup.',
  'Hesabınızın henüz yedeği alınmamış; bir yedek alın ya da otomatik yedeklemeyi açın.': 'Your account has not been backed up yet; take a backup or turn on automatic backups.',
  'Yedek al': 'Back up now',
  'Son yedek {0} gün önce alındı': 'Last backup was {0} days ago',
  'Düzenli yedek için otomatik yedeklemeyi açabilirsiniz.': 'Turn on automatic backups for regular backups.',
  '{0} PHP {1} kullanıyor': '{0} runs PHP {1}',
  'Bu sürüm artık güvenlik güncellemesi almıyor; desteklenen bir sürüme geçin.': 'This version no longer gets security updates; switch to a supported version.',
  'PHP sürümünü değiştir': 'Change PHP version',
  '{0} alt alan adı desteği biten PHP sürümü kullanıyor': '{0} subdomains run an end-of-life PHP version',
  '{0} okunmamış önemli bildirim': '{0} unread important notifications',
  'En yenisi: {0}': 'Latest: {0}',
  'Bildirimleri aç': 'Open notifications',
}

export const cevir = (tr: string): string => (i18n.language === 'en' ? (MUSTERI_ANA_EN[tr] || ARAC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

const doldur = (kalip: string, d: Array<string | number>): string => d.reduce<string>((s, v, i) => s.split(`{${i}}`).join(String(v)), kalip)

export const cevirF = (tr: string, ...d: Array<string | number>): string => doldur(cevir(tr), d)

// EN tekil kalıplar: sayı 1 iken (Intl 'one') MUSTERI_ANA_EN'deki çoğul kalıp yerine
// bunlar kullanılır ("1 domains" değil "1 domain"). TR'de sayıyla ad tekil kalır.
const EN_TEKIL: Record<string, string> = {
  '{0} alan adı': '{0} domain',
  '“{0}” için {1} sonuç': '{1} result for “{0}”',
  '1 ana alan adı · {0} alt alan adı': '1 primary domain · {0} subdomain',
  '{0} gün kaldı': '{0} day left',
  'Geçerli · {0} gün': 'Valid · {0} day',
  'SSL {0} gün içinde bitiyor': 'SSL expires in {0} day',
  '{0} SSL sertifikası {1} gün içinde bitiyor': 'SSL certificate for {0} expires in {1} day',
  'Son yedek {0} gün önce alındı': 'Last backup was {0} day ago',
}

// Sayıya göre tekil/çoğul seçen cevirF: `n` kalıbı seçer, `d` yer tutucuları doldurur.
export const cevirSayili = (tr: string, n: number, ...d: Array<string | number>): string =>
  doldur(i18n.language === 'en' && EN_TEKIL[tr] && new Intl.PluralRules('en').select(n) === 'one' ? EN_TEKIL[tr] : cevir(tr), d)

const yerel = (): string => (i18n.language === 'en' ? 'en-US' : 'tr-TR')

export const sayi = (n: number): string => (Number.isFinite(n) ? n : 0).toLocaleString(yerel())

export const oran = (n: number): string => (i18n.language === 'en' ? `${n}%` : `%${n}`)

// Sunucunun duvar saati tarihleri ('YYYY-MM-DD' ya da 'YYYY-MM-DD HH:MM') → yerel biçim.
// Saat dilimi dönüşümü YAPILMAZ (değer olduğu gibi okunur, yalnız biçimlenir); okunamayan
// değer olduğu gibi döner.
export function tarihBicim(t: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2}))?/.exec(t)
  if (!m) return t
  const saatli = m[4] !== undefined
  const d = new Date(+m[1], +m[2] - 1, +m[3], saatli ? +m[4] : 0, saatli ? +m[5] : 0)
  if (!Number.isFinite(d.getTime())) return t
  return d.toLocaleString(yerel(), saatli ? { dateStyle: 'medium', timeStyle: 'short' } : { dateStyle: 'medium' })
}
