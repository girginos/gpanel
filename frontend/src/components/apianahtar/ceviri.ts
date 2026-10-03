// API anahtarları sayfası metinleri — TR anahtar → EN (panelin sayfa sözlüğü deseni).
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'

const API_EN: Record<string, string> = {
  'API Anahtarları': 'API Keys',
  'Faturalama otomasyonu (WHMCS, Blesta) ve betikler için kalıcı erişim anahtarları. Anahtar yalnız seçtiğiniz izinlerle ve kendi kapsamınızda çalışır.': 'Persistent access keys for billing automation (WHMCS, Blesta) and scripts. A key works only with the permissions you choose and within your own scope.',
  'Yeni anahtar': 'New key',
  'Anahtarlarım': 'My keys',
  'Bayi anahtarları': 'Reseller keys',
  'Yükleniyor…': 'Loading…',
  'Henüz API anahtarı yok.': 'No API keys yet.',
  'Ad': 'Name',
  'Anahtar': 'Key',
  'İzinler': 'Permissions',
  'İzinli IP': 'Allowed IPs',
  'her IP': 'any IP',
  'Son kullanım': 'Last used',
  'hiç': 'never',
  'Bitiş': 'Expires',
  'Durum': 'Status',
  'Bayi': 'Reseller',
  'Etkin': 'Active',
  'Geçiş süresinde': 'In grace period',
  'Süresi doldu': 'Expired',
  'İptal edildi': 'Revoked',
  'İptal edildi (yönetim)': 'Revoked (administrator)',
  'Döndür': 'Rotate',
  'İptal et': 'Revoke',
  'Anahtar iptal edilsin mi?': 'Revoke this key?',
  '"{0}" anahtarı hemen geçersiz olur; onu kullanan entegrasyonlar çalışmayı bırakır. Bu işlem geri alınamaz.': 'The key "{0}" becomes invalid immediately; integrations using it stop working. This cannot be undone.',
  'Vazgeç': 'Cancel',
  'Anahtar iptal edildi': 'Key revoked',
  'İşlem başarısız': 'Operation failed',
  'Liste okunamadı': 'Could not load the list',
  'okuma': 'read',
  'okuma + yazma': 'read + write',
  'Yeni API anahtarı': 'New API key',
  'Anahtar adı': 'Key name',
  'ör. WHMCS üretim': 'e.g. WHMCS production',
  'Yok': 'None',
  'Okuma': 'Read',
  'Okuma + yazma': 'Read + write',
  'İzinli IP adresleri': 'Allowed IP addresses',
  'Her satıra bir IP ya da CIDR (ör. 203.0.113.10 ya da 203.0.113.0/24).': 'One IP or CIDR per line (e.g. 203.0.113.10 or 203.0.113.0/24).',
  'Kök anahtarında zorunludur.': 'Required for the root key.',
  'Boş bırakılırsa her IP\'den kullanılabilir (önerilmez).': 'If left empty, it can be used from any IP (not recommended).',
  'Geçerlilik süresi': 'Validity period',
  '{0} gün': '{0} days',
  'Hız sınırı (istek/dk)': 'Rate limit (requests/min)',
  'Kimliğinizi doğrulayın': 'Confirm your identity',
  'Parolanız': 'Your password',
  '2FA kodu': '2FA code',
  'Oluştur': 'Create',
  'Anahtar adı zorunlu.': 'Key name is required.',
  'En az bir izin seçin.': 'Select at least one permission.',
  'Kök anahtarında en az bir izinli IP zorunludur.': 'At least one allowed IP is required for the root key.',
  'Parolanızı girin.': 'Enter your password.',
  'İki aşamalı doğrulama kodunu girin.': 'Enter your two-factor authentication code.',
  'Anahtar oluşturulamadı': 'Could not create the key',
  'Anahtarınız hazır': 'Your key is ready',
  'Bu anahtar bir daha gösterilmeyecek. Şimdi kopyalayıp güvenli bir yerde (parola yöneticisi, faturalama sisteminin ayarları) saklayın.': 'This key will not be shown again. Copy it now and store it somewhere safe (a password manager, your billing system settings).',
  'Kopyala': 'Copy',
  'Kopyalandı': 'Copied',
  'Panoya kopyalanamadı; anahtarı elle seçip kopyalayın.': 'Could not copy to clipboard; select and copy the key manually.',
  'Anahtarı güvenli bir yere kopyaladım': 'I have copied the key to a safe place',
  'Kapat': 'Close',
  'Anahtarı döndür': 'Rotate key',
  '"{0}" için aynı izinlerle yeni bir anahtar üretilir. Eski anahtar seçtiğiniz geçiş süresi sonunda geçersiz olur.': 'A new key with the same permissions is generated for "{0}". The old key becomes invalid at the end of the grace period you choose.',
  'Eski anahtarın geçiş süresi': 'Grace period for the old key',
  'Hemen geçersiz olsun': 'Invalidate immediately',
  '{0} saat': '{0} hours',
  'Yeni anahtar üret': 'Generate new key',
  'Anahtar döndürülemedi': 'Could not rotate the key',
  'Kullanım': 'Usage',
  'Anahtarı her istekte Authorization başlığında gönderin. Anahtar yalnız kendi kapınızdan (kök anahtarı yönetici portundan, bayi anahtarı bayi portundan) ve yalnız sunucudan sunucuya çağrılarda çalışır; tarayıcıdan (Origin başlığıyla) gelen istekler reddedilir.': 'Send the key in the Authorization header with every request. A key works only through its own gate (root key via the admin port, reseller key via the reseller port) and only for server-to-server calls; requests from a browser (with an Origin header) are rejected.',
  'Bağlantıyı test et': 'Test the connection',
  'Hosting hesaplarını listele': 'List hosting accounts',
  'Anahtar giriş, parola/2FA değişikliği, müşteri adına oturum, phpMyAdmin oturumu ve anahtar yönetimi gibi oturum açan ya da kimlik değiştiren uçlara hiçbir izinle erişemez.': 'A key can never access endpoints that open sessions or change identity — login, password/2FA changes, sign-in on behalf of a customer, phpMyAdmin sessions and key management — whatever its permissions.',
  'Bayi parolası sıfırlandığında, bayi askıya alındığında ya da silindiğinde o bayinin tüm anahtarları iptal edilir.': 'When a reseller\'s password is reset, or the reseller is suspended or deleted, all of that reseller\'s keys are revoked.',
  'Oluşturulma': 'Created',
  'Hata oluştu': 'An error occurred',
}

export const cevir = (tr: string): string => (i18n.language === 'en' ? (API_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export function cevirT(sablon: string, ...args: unknown[]): string {
  let s = cevir(sablon)
  args.forEach((a, i) => { s = s.split('{' + i + '}').join(String(a)) })
  return s
}

export function tarih(unix: number): string {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString(i18n.language === 'en' ? 'en-GB' : 'tr-TR', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  })
}
