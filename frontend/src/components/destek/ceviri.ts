// Telegram Destek sayfası metinleri — TR anahtar → EN (panelin sayfa sözlüğü deseni).
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'

const DESTEK_EN: Record<string, string> = {
  'Telegram Destek': 'Telegram Support',
  'Hesabınızı Telegram destek botuna bağlayın: bot sorunu teşhis etmek için yalnız sizin seçtiğiniz kapsamda ve düzeyde günlükleri ve ayarları okur. Parolalar ve gizli değerler sunucudan çıkmadan maskelenir.': 'Connect your account to the Telegram support bot: to diagnose problems it reads logs and settings only within the scope and level you choose. Passwords and secret values are masked before they leave the server.',
  'Yükleniyor…': 'Loading…',
  'Durum okunamadı': 'Could not load status',
  'Liste okunamadı': 'Could not load the list',
  'İşlem başarısız': 'Operation failed',
  'Vazgeç': 'Cancel',
  'Kapalı': 'Off',
  'Yalnız okuma': 'Read only',
  'Okuma + güvenli onarım': 'Read + safe repair',
  'Tam (kritik işlemler dahil)': 'Full (including critical operations)',
  'Modül ayarları': 'Module settings',
  'Telegram destek modülü': 'Telegram support module',
  'Bayiler için üst sınır': 'Limit for resellers',
  'Müşteriler için üst sınır': 'Limit for customers',
  'Modül varsayılan olarak kapalıdır. Açıldığında panel destek merkezine dışa doğru bağlanır; hiçbir bağlantı bu sınırların üzerinde yetki alamaz.': 'The module is off by default. When it is on, the panel connects outward to the support hub; no connection can get permissions above these limits.',
  'Kaydet': 'Save',
  'Ayarlar kaydedildi': 'Settings saved',
  'Merkez bağlantısı': 'Hub connection',
  'Bağlı': 'Connected',
  'Bağlanıyor': 'Connecting',
  'Hata': 'Error',
  'Yapılandırma eksik': 'Configuration missing',
  'Cihaz kimliği': 'Device ID',
  'Son hata': 'Last error',
  'Müşterilerim kullanabilir': 'My customers can use it',
  'Müşterilerinizin bota bağlanabileceği en yüksek düzey. Yöneticinin izin verdiği üst sınır: {0}.': 'The highest level your customers can connect the bot with. Limit allowed by the administrator: {0}.',
  'Eşleştir': 'Pair',
  'Telegram destek bu hesap için kapalı. Sunucu yöneticiniz (ya da bayiniz) açtığında buradan eşleştirebilirsiniz.': 'Telegram support is off for this account. You can pair here once your server administrator (or reseller) turns it on.',
  'Müşteri adına açılan oturumda Telegram bağlantısı kurulamaz ve onaylanamaz; müşterinin kendisi yapmalıdır.': 'A Telegram connection cannot be created or approved in a session opened on behalf of a customer; the customer must do it.',
  'Yetki düzeyi': 'Permission level',
  'Süre': 'Duration',
  '{0} gün': '{0} days',
  '1 gün': '1 day',
  'Bu sürümde bot yalnız okuma araçlarını kullanır; daha yüksek düzey ileride açılacak onarım araçları içindir. Etkin düzey her zaman yönetici ve bayi sınırlarıyla kısılır.': 'In this version the bot uses only read tools; higher levels are for repair tools that will be enabled later. The effective level is always capped by administrator and reseller limits.',
  'Eşleştirme kodu oluştur': 'Create pairing code',
  'Kod oluşturulamadı': 'Could not create the code',
  'Eşleştirme kodu': 'Pairing code',
  'Telegram\'da aç': 'Open in Telegram',
  'QR kodu telefonunuzla okutun ya da bağlantıyı açın. Bot kullanıcı adı yapılandırılmadıysa Telegram\'da bota şunu yazın:': 'Scan the QR code with your phone or open the link. If no bot username is configured, send this to the bot in Telegram:',
  'Kod {0} içinde geçersiz olur ve yalnız bir kez kullanılabilir.': 'The code expires in {0} and can be used only once.',
  'Kodun süresi doldu.': 'The code has expired.',
  'Panel şu an destek merkezine bağlı değil; bağlantı kurulunca kod merkeze iletilir.': 'The panel is not connected to the support hub right now; the code will be sent once the connection is established.',
  'Kodu kopyala': 'Copy code',
  'Kopyalandı': 'Copied',
  'Onay bekleyen bağlantı': 'Connection awaiting approval',
  'Telegram hesabı {0} bu panele bağlanmak istiyor. Siz onaylamadan bot hiçbir şey okuyamaz.': 'Telegram account {0} wants to connect to this panel. The bot cannot read anything until you approve.',
  'Aydınlatma metnini okudum ve kabul ediyorum (sürüm {0}).': 'I have read and accept the privacy notice (version {0}).',
  'Onayla': 'Approve',
  'Reddet': 'Reject',
  'Bağlantı onaylandı': 'Connection approved',
  'Bağlantı reddedildi': 'Connection rejected',
  'Bağlantılar': 'Connections',
  'Henüz Telegram bağlantısı yok.': 'No Telegram connections yet.',
  'Telegram': 'Telegram',
  'Kapsam': 'Scope',
  'Durum': 'Status',
  'Düzey': 'Level',
  'Bitiş': 'Expires',
  'Son kullanım': 'Last used',
  'hiç': 'never',
  'Onay bekliyor': 'Awaiting approval',
  'Etkin': 'Active',
  'Kaldırıldı': 'Removed',
  'Kaldır': 'Remove',
  'Bağlantı kaldırılsın mı?': 'Remove this connection?',
  '{0} Telegram hesabının bu paneldeki erişimi hemen sona erer. Yeniden bağlanmak için yeni bir eşleştirme gerekir.': 'The access of Telegram account {0} to this panel ends immediately. A new pairing is required to connect again.',
  'Bağlantı kaldırıldı': 'Connection removed',
  'Kök': 'Root',
  'Bayi': 'Reseller',
  'Müşteri': 'Customer',
  'Aydınlatma metni': 'Privacy notice',
  'TASLAK — hukuki onaydan önce gerçek müşterilere açılmaz': 'DRAFT — not offered to real customers before legal approval',
  'Sürüm {0}': 'Version {0}',
  'Metni göster': 'Show text',
  'Metni gizle': 'Hide text',
  'Kabul edildi: {0}': 'Accepted: {0}',
  'Yenile': 'Refresh',
  'Onaylayan': 'Approved by',
  'Kaldıran': 'Removed by',
  'İptal nedeni': 'Reason',
  'panelden kaldırıldı': 'removed from the panel',
  'Telegram\'dan kaldırıldı (/unutbeni)': 'removed from Telegram (/unutbeni)',
  'FTP parolası değişti': 'FTP password changed',
  'hosting silindi ya da askıya alındı': 'hosting deleted or suspended',
  'bayi askıya alındı, silindi ya da parolası sıfırlandı': 'reseller suspended, deleted or password reset',
  'iki aşamalı doğrulama kapatıldı': 'two-factor authentication turned off',
  'süresi doldu': 'expired',
  'destek merkezi kaldırdı': 'removed by the support hub',
  'Bağlantı her zaman süreli (en çok {0} gün); parolanız değişirse, hesabınız askıya alınırsa ya da 2FA kapatılırsa kendiliğinden kaldırılır.': 'A connection is always time-limited (at most {0} days); it is removed automatically if your password changes, your account is suspended or 2FA is turned off.',
}

export const cevir = (tr: string): string => (i18n.language === 'en' ? (DESTEK_EN[tr] || ORTAK_EN[tr] || tr) : tr)

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

export const DUZEY_ETIKET: Record<string, string> = {
  kapali: 'Kapalı',
  okuma: 'Yalnız okuma',
  onarim: 'Okuma + güvenli onarım',
  tam: 'Tam (kritik işlemler dahil)',
}

export const IPTAL_ETIKET: Record<string, string> = {
  kullanici: 'panelden kaldırıldı',
  unutbeni: "Telegram'dan kaldırıldı (/unutbeni)",
  ftp_parola: 'FTP parolası değişti',
  alan_silme: 'hosting silindi ya da askıya alındı',
  bayi: 'bayi askıya alındı, silindi ya da parolası sıfırlandı',
  iki_fa: 'iki aşamalı doğrulama kapatıldı',
  suresi_doldu: 'süresi doldu',
  merkez: 'destek merkezi kaldırdı',
}
