// Hosting kullanıcısı (rol 'user') araç kataloğu — TEK KAYNAK.
//
// Müşteri ana sayfası (MusteriAnaSayfa) kutucukları, üst çubuk araması (TopBar) ve
// sekme başlığı (DashboardLayout) buradan okur. Her araç MÜŞTERİNİN GERÇEKTEN
// ERİŞEBİLDİĞİ mevcut bir sayfaya gider: /abonelikler/{id}/<yol> rotası App.tsx'te
// tanımlı ve sayfanın çağırdığı uçlar sunucuda MusteriScope arkasında.
// 🔴 Sayfası olmayan araç buraya EKLENMEZ (ölü bağlantı yok). Bilerek dışarıda:
//   - plan (Hosting Planı): GET /plans ve PUT /domains/{id}/plan müşteriye 403.
//   - phpMyAdmin / Webmail: ayrı sayfası yok; veritabanı ve posta kutusu sayfalarının
//     içinden açılır — anahtar sözcük olarak o araçlara bağlı.
//   - ek (addon) alan adı: arka uçta henüz yok.
//
// Kapsam modeli: 'hesap' = sistem kullanıcısının tamamı (gri kare), 'alan' = alan adına
// özel (mavi kare + küre). Alan adına özel araç, alt alan adı seçiliyken `altYol`u varsa
// o alt alan adının sayfasında açılır; yoksa ana alan adında (ipucu bunu söyler).
// `altYol` YALNIZ alt alan adı sayfasında o aracın karşılığı gerçekten varsa verilir.
// '' = alt alan adı yönetim sayfası (DomainSubdomainYonetPage): PHP sürümü + php.ini
// (php), SSL (ssl) ve web sunucusu — backend, nginx önbellek, güvenlik başlıkları, ek
// direktifler (web-sunucu → SubWebAyarlari, GET/PUT /domains/{id}/subdomain/{sid}/web-sunucu,
// MusteriScope). DNS, WAF vb. için alt alan adı karşılığı yok → ana alan adında açılır.
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'

export type AracKapsami = 'hesap' | 'alan'
export type AracEklentisi = 'mail' | 'calistirici'

export type MusteriArac = {
  kod: string
  ad: string // TR (görünen metin aracCevir ile geçer)
  aciklama: string
  ikon: string // '@/components/Ikon' içindeki I anahtarı
  kapsam: AracKapsami
  kategori: KategoriKodu
  // /abonelikler/{id}/<yol>
  yol: string
  // Alt alan adı seçiliyken: /abonelikler/{id}/subdomainler/{sid}[/<altYol>] ('' = yönetim sayfası)
  altYol?: string
  anahtar: string[]
  // Eklentiye bağlı araç: eklenti müşteriye açık (GET /eklentiler listesinde) değilse gizli.
  eklenti?: AracEklentisi
}

export type KategoriKodu = 'dosyalar' | 'veritabanlari' | 'alanadlari' | 'eposta' | 'guvenlik' | 'yazilim' | 'gelismis'

export const KATEGORILER: Array<{ kod: KategoriKodu; ad: string; ikon: string }> = [
  { kod: 'dosyalar', ad: 'Dosyalar', ikon: 'klasor' },
  { kod: 'veritabanlari', ad: 'Veritabanları', ikon: 'db' },
  { kod: 'alanadlari', ad: 'Alan Adları', ikon: 'kure' },
  { kod: 'eposta', ad: 'E-posta', ikon: 'mail' },
  { kod: 'guvenlik', ad: 'Güvenlik', ikon: 'kalkan' },
  { kod: 'yazilim', ad: 'Yazılım', ikon: 'kutu' },
  { kod: 'gelismis', ad: 'Gelişmiş', ikon: 'ayar' },
]

export const MUSTERI_ARACLAR: MusteriArac[] = [
  // Dosyalar
  { kod: 'dosyalar', ad: 'Dosya Yöneticisi', aciklama: 'Dosyaları yükle, düzenle, sıkıştır', ikon: 'klasor', kapsam: 'hesap', kategori: 'dosyalar', yol: 'dosyalar', anahtar: ['dosya', 'file', 'upload', 'yükle', 'public_html', 'zip'] },
  { kod: 'ftp', ad: 'FTP Hesapları', aciklama: 'FTP erişimi ve parolası', ikon: 'yukle', kapsam: 'hesap', kategori: 'dosyalar', yol: 'ftp', anahtar: ['ftp', 'sftp', 'parola', 'şifre', 'password'] },
  { kod: 'yedekler', ad: 'Yedekler', aciklama: 'Yedek al, geri yükle, indir', ikon: 'bulut', kapsam: 'hesap', kategori: 'dosyalar', yol: 'yedekler', anahtar: ['yedek', 'backup', 'geri yükle', 'restore'] },
  { kod: 'baglanti', ad: 'Bağlantı Bilgileri', aciklama: 'FTP ve veritabanı bağlantı ayrıntıları', ikon: 'link', kapsam: 'hesap', kategori: 'dosyalar', yol: 'baglanti', anahtar: ['bağlantı', 'connection', 'host', 'sunucu', 'kullanıcı adı'] },
  // Veritabanları
  { kod: 'veritabanlari', ad: 'Veritabanları', aciklama: 'MySQL veritabanları, kullanıcılar, phpMyAdmin', ikon: 'db', kapsam: 'hesap', kategori: 'veritabanlari', yol: 'veritabanlari', anahtar: ['mysql', 'mariadb', 'db', 'database', 'sql', 'phpmyadmin'] },
  { kod: 'redis', ad: 'Redis Önbelleği', aciklama: 'İzole nesne önbelleği', ikon: 'simsek', kapsam: 'hesap', kategori: 'veritabanlari', yol: 'redis', anahtar: ['redis', 'cache', 'önbellek', 'object cache'] },
  // Alan adları
  { kod: 'altalan', ad: 'Alt Alan Adları', aciklama: 'Alt alan adı ekle ve yönet', ikon: 'kure', kapsam: 'hesap', kategori: 'alanadlari', yol: 'subdomainler', anahtar: ['alt alan', 'subdomain', 'alan adı', 'domain'] },
  { kod: 'dns', ad: 'DNS Ayarları', aciklama: 'A, MX, TXT, CNAME kayıtları', ikon: 'anten', kapsam: 'alan', kategori: 'alanadlari', yol: 'dns', anahtar: ['dns', 'kayıt', 'mx', 'txt', 'cname', 'zone', 'nameserver'] },
  // E-posta (mail eklentisi)
  { kod: 'mail-kutular', ad: 'E-posta Hesapları', aciklama: 'Posta kutuları, tek tıkla webmail', ikon: 'mail', kapsam: 'alan', kategori: 'eposta', yol: 'mail/kutular', anahtar: ['mail', 'e-posta', 'eposta', 'email', 'posta kutusu', 'webmail', 'roundcube'], eklenti: 'mail' },
  { kod: 'mail-takma', ad: 'Takma Adlar ve Yönlendirme', aciklama: 'Yönlendirme, catch-all', ikon: 'ilet', kapsam: 'alan', kategori: 'eposta', yol: 'mail/takmaadlar', anahtar: ['alias', 'takma ad', 'yönlendirme', 'forward', 'catch-all'], eklenti: 'mail' },
  { kod: 'mail-ayar', ad: 'Posta Ayarları', aciklama: 'Sağlık analizi, bağlantı bilgileri', ikon: 'ayar', kapsam: 'alan', kategori: 'eposta', yol: 'mail/ayarlar', anahtar: ['imap', 'smtp', 'pop', 'spf', 'dkim', 'dmarc', 'mail ayar'], eklenti: 'mail' },
  { kod: 'mail-teslimat', ad: 'Teslimat Takibi', aciklama: 'Gönderim ve teslimat günlüğü', ikon: 'gonder', kapsam: 'alan', kategori: 'eposta', yol: 'mail/teslimat', anahtar: ['teslimat', 'delivery', 'gönderim', 'bounce', 'mail log'], eklenti: 'mail' },
  // Güvenlik
  { kod: 'ssl', ad: 'SSL/TLS', aciklama: "Let's Encrypt sertifikaları", ikon: 'kilit', kapsam: 'alan', kategori: 'guvenlik', yol: 'ssl', altYol: '', anahtar: ['ssl', 'tls', 'https', 'sertifika', 'certificate', 'lets encrypt'] },
  { kod: 'waf', ad: 'Güvenlik Duvarı (WAF)', aciklama: 'ModSecurity + OWASP CRS', ikon: 'kalkan', kapsam: 'alan', kategori: 'guvenlik', yol: 'waf', anahtar: ['waf', 'modsecurity', 'owasp', 'güvenlik duvarı', 'firewall'] },
  { kod: 'erisim', ad: 'Erişim Kısıtlama', aciklama: 'IP izin ve engel kuralları', ikon: 'ban', kapsam: 'alan', kategori: 'guvenlik', yol: 'erisim', altYol: 'erisim', anahtar: ['ip', 'engel', 'izin', 'block', 'allow', 'erişim'] },
  { kod: 'sifre-koruma', ad: 'Şifre Korumalı Dizinler', aciklama: '.htpasswd ile dizin koruma', ikon: 'anahtar', kapsam: 'alan', kategori: 'guvenlik', yol: 'sifre-koruma', altYol: 'sifre-koruma', anahtar: ['htpasswd', 'parola koruma', 'şifre', 'dizin koruma', 'password protect'] },
  { kod: 'guvenlik-tarama', ad: 'Website Security', aciklama: 'Sitenizdeki bilinen güvenlik açıkları', ikon: 'kalkanTara', kapsam: 'hesap', kategori: 'guvenlik', yol: 'guvenlik', anahtar: ['website security', 'güvenlik açığı', 'açık', 'zafiyet', 'cve', 'vulnerability', 'eklenti güncelle', 'tema güncelle', 'tarama', 'wordpress güvenlik'] },
  { kod: 'antivirus', ad: 'Antivirüs', aciklama: 'Zararlı yazılım taraması, karantina', ikon: 'onayDaire', kapsam: 'hesap', kategori: 'guvenlik', yol: 'imunify', anahtar: ['antivirüs', 'antivirus', 'virüs', 'malware', 'zararlı', 'karantina', 'g-av', 'tarama'] },
  { kod: 'ssh', ad: 'SSH Erişimi', aciklama: 'Terminal erişimi ve anahtarlar', ikon: 'terminal', kapsam: 'hesap', kategori: 'guvenlik', yol: 'ssh-erisim', anahtar: ['ssh', 'terminal', 'anahtar', 'key', 'shell'] },
  // Yazılım
  { kod: 'php', ad: 'PHP Ayarları', aciklama: 'Sürüm ve php.ini', ikon: 'parca', kapsam: 'alan', kategori: 'yazilim', yol: 'php', altYol: '', anahtar: ['php', 'sürüm', 'version', 'php.ini', 'memory_limit', 'fpm'] },
  { kod: 'web-sunucu', ad: 'Apache ve nginx', aciklama: 'Güvenlik başlıkları, ek direktifler', ikon: 'sunucu', kapsam: 'alan', kategori: 'yazilim', yol: 'web-sunucu', altYol: '', anahtar: ['apache', 'nginx', 'htaccess', 'yönlendirme', 'redirect', 'başlık', 'header'] },
  { kod: 'wordpress', ad: 'WordPress', aciklama: 'Tek tıkla kurulum ve yönetim', ikon: 'pano', kapsam: 'alan', kategori: 'yazilim', yol: 'wordpress', altYol: 'wordpress', anahtar: ['wordpress', 'wp', 'blog', 'eklenti', 'tema'] },
  { kod: 'uygulamalar', ad: 'Uygulama Kurulumu', aciklama: 'Hazır uygulama kataloğu', ikon: 'indir', kapsam: 'alan', kategori: 'yazilim', yol: 'uygulamalar', anahtar: ['uygulama', 'kur', 'install', 'katalog', 'app'] },
  { kod: 'laravel', ad: 'Laravel Toolkit', aciklama: 'Kurulum, Artisan, dağıtım, kuyruk', ikon: 'roket', kapsam: 'alan', kategori: 'yazilim', yol: 'laravel', anahtar: ['laravel', 'artisan', 'queue', 'kuyruk', 'deploy'] },
  { kod: 'composer', ad: 'PHP Composer', aciklama: 'PHP paket yöneticisi', ikon: 'kutu', kapsam: 'alan', kategori: 'yazilim', yol: 'composer', altYol: 'composer', anahtar: ['composer', 'paket', 'package', 'vendor'] },
  { kod: 'git', ad: 'Git Dağıtımı', aciklama: 'Depodan otomatik dağıtım', ikon: 'git', kapsam: 'alan', kategori: 'yazilim', yol: 'git', anahtar: ['git', 'github', 'deploy', 'depo', 'repo', 'dağıtım'] },
  { kod: 'performans', ad: 'Performans', aciklama: 'OPcache, gzip gibi hızlandırıcılar', ikon: 'yukselt', kapsam: 'alan', kategori: 'yazilim', yol: 'performans', anahtar: ['performans', 'hız', 'opcache', 'gzip', 'cache', 'speed'] },
  { kod: 'calistirici', ad: 'Uygulama Çalıştırıcı', aciklama: 'Uygulama dağıtımı, süreçler, günlükler', ikon: 'oynat', kapsam: 'alan', kategori: 'yazilim', yol: 'gelistirici', anahtar: ['node', 'python', 'çalıştırıcı', 'runner', 'geliştirici', 'uygulama'], eklenti: 'calistirici' },
  // Gelişmiş
  { kod: 'cron', ad: 'Zamanlanmış Görevler', aciklama: 'Cron işleri', ikon: 'saat', kapsam: 'hesap', kategori: 'gelismis', yol: 'cron', anahtar: ['cron', 'zamanla', 'görev', 'schedule', 'crontab'] },
  { kod: 'gunlukler', ad: 'Günlükler', aciklama: 'Erişim ve hata günlükleri', ikon: 'dosya', kapsam: 'alan', kategori: 'gelismis', yol: 'gunlukler', altYol: 'gunlukler', anahtar: ['log', 'günlük', 'hata', 'error', 'access', 'erişim'] },
  { kod: 'istatistik', ad: 'İstatistikler', aciklama: 'Ziyaret ve trafik', ikon: 'grafik', kapsam: 'alan', kategori: 'gelismis', yol: 'istatistik', altYol: 'istatistik', anahtar: ['istatistik', 'ziyaret', 'trafik', 'stats', 'analytics'] },
]

// Araç adları, açıklamaları ve kategori adları (TR → EN).
export const ARAC_EN: Record<string, string> = {
  'Dosyalar': 'Files',
  'Veritabanları': 'Databases',
  'Alan Adları': 'Domains',
  'E-posta': 'Email',
  'Güvenlik': 'Security',
  'Yazılım': 'Software',
  'Gelişmiş': 'Advanced',
  'Ana sayfa': 'Home',
  'Dosya Yöneticisi': 'File Manager',
  'Dosyaları yükle, düzenle, sıkıştır': 'Upload, edit and compress files',
  'FTP Hesapları': 'FTP Accounts',
  'FTP erişimi ve parolası': 'FTP access and password',
  'Yedekler': 'Backups',
  'Yedek al, geri yükle, indir': 'Back up, restore, download',
  'public_html içeriğinin kopyasını oluştur': 'Create a copy of your public_html contents',
  'Bağlantı Bilgileri': 'Connection Details',
  'FTP ve veritabanı bağlantı ayrıntıları': 'FTP and database connection details',
  'MySQL veritabanları, kullanıcılar, phpMyAdmin': 'MySQL databases, users, phpMyAdmin',
  'Redis Önbelleği': 'Redis Cache',
  'İzole nesne önbelleği': 'Isolated object cache',
  'Alt Alan Adları': 'Subdomains',
  'Alt alan adı ekle ve yönet': 'Add and manage subdomains',
  'DNS Ayarları': 'DNS Settings',
  'A, MX, TXT, CNAME kayıtları': 'A, MX, TXT and CNAME records',
  'E-posta Hesapları': 'Email Accounts',
  'Posta kutuları, tek tıkla webmail': 'Mailboxes and one-click webmail',
  'Takma Adlar ve Yönlendirme': 'Aliases & Forwarding',
  'Yönlendirme, catch-all': 'Forwarding, catch-all',
  'Posta Ayarları': 'Mail Settings',
  'Sağlık analizi, bağlantı bilgileri': 'Health check, connection details',
  'Teslimat Takibi': 'Delivery Tracking',
  'Gönderim ve teslimat günlüğü': 'Sending and delivery log',
  'SSL/TLS': 'SSL/TLS',
  "Let's Encrypt sertifikaları": "Let's Encrypt certificates",
  'Güvenlik Duvarı (WAF)': 'Web Application Firewall',
  'ModSecurity + OWASP CRS': 'ModSecurity + OWASP CRS',
  'Erişim Kısıtlama': 'Access Restrictions',
  'IP izin ve engel kuralları': 'IP allow and deny rules',
  'Şifre Korumalı Dizinler': 'Password-Protected Directories',
  '.htpasswd ile dizin koruma': 'Protect directories with .htpasswd',
  'Antivirüs': 'Antivirus',
  'Website Security': 'Website Security',
  'Sitenizdeki bilinen güvenlik açıkları': 'Known security vulnerabilities on your site',
  'Zararlı yazılım taraması, karantina': 'Malware scanning and quarantine',
  'SSH Erişimi': 'SSH Access',
  'Terminal erişimi ve anahtarlar': 'Terminal access and keys',
  'PHP Ayarları': 'PHP Settings',
  'Sürüm ve php.ini': 'Version and php.ini',
  'Apache ve nginx': 'Apache and nginx',
  'Güvenlik başlıkları, ek direktifler': 'Security headers, extra directives',
  'WordPress': 'WordPress',
  'Tek tıkla kurulum ve yönetim': 'One-click install and management',
  'Uygulama Kurulumu': 'App Installer',
  'Hazır uygulama kataloğu': 'Ready-made application catalog',
  'Laravel Toolkit': 'Laravel Toolkit',
  'Kurulum, Artisan, dağıtım, kuyruk': 'Install, Artisan, deploy, queue',
  'PHP Composer': 'PHP Composer',
  'PHP paket yöneticisi': 'PHP package manager',
  'Git Dağıtımı': 'Git Deployment',
  'Depodan otomatik dağıtım': 'Deploy automatically from a repository',
  'Performans': 'Performance',
  'OPcache, gzip gibi hızlandırıcılar': 'Accelerators such as OPcache and gzip',
  'Uygulama Çalıştırıcı': 'Application Runner',
  'Uygulama dağıtımı, süreçler, günlükler': 'App deployment, processes and logs',
  'Zamanlanmış Görevler': 'Cron Jobs',
  'Cron işleri': 'Scheduled commands',
  'Günlükler': 'Logs',
  'Erişim ve hata günlükleri': 'Access and error logs',
  'İstatistikler': 'Statistics',
  'Ziyaret ve trafik': 'Visits and traffic',
}

export const aracCevir = (tr: string): string => (i18n.language === 'en' ? (ARAC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// Arama için: küçük harf + Türkçe harfleri sadeleştir ("sifre" → "şifre" de bulunsun).
const SADE: Record<string, string> = { ç: 'c', ğ: 'g', ı: 'i', ö: 'o', ş: 's', ü: 'u', â: 'a', î: 'i', û: 'u' }
export const sade = (s: string): string => s.toLocaleLowerCase('tr').replace(/[çğıöşüâîû]/g, (c) => SADE[c] ?? c)

const kategoriAdi = (kod: KategoriKodu): string => KATEGORILER.find((k) => k.kod === kod)?.ad ?? ''

// Aracın aranabilir metni (TR + EN ad/açıklama, anahtar sözcükler, kategori adı).
export function aracSamanligi(a: MusteriArac): string {
  const kat = kategoriAdi(a.kategori)
  return sade([a.ad, a.aciklama, ARAC_EN[a.ad] ?? '', ARAC_EN[a.aciklama] ?? '', ...a.anahtar, kat, ARAC_EN[kat] ?? ''].join(' '))
}

// Sözcüklerin hepsi geçiyorsa eşleşir (boş arama = hepsi).
export function aracEslesir(a: MusteriArac, sozcukler: string[]): boolean {
  if (!sozcukler.length) return true
  const s = aracSamanligi(a)
  return sozcukler.every((w) => s.includes(w))
}

export const aramaSozcukleri = (q: string): string[] => sade(q.trim()).split(/\s+/).filter(Boolean)

// Eklenti kapısı — ana sayfa ve üst çubuk araması AYNI süzgeci kullanır (GET /eklentiler,
// müşteriye yalnız açık olanlar). Liste yoksa (henüz gelmedi / alınamadı) eklenti aracı
// önerilmez. Mail: yalnız aktifken araçlar; çalıştırıcı: kuruluysa (askıyı sayfası söyler).
export function eklentiKapisi(liste: ReadonlyArray<{ ad: string; aktif: boolean }> | null): (a: MusteriArac) => boolean {
  const l = liste ?? []
  const mailAktif = l.some((e) => e.ad === 'mail' && e.aktif)
  const calistiriciKurulu = l.some((e) => e.ad === 'calistirici')
  return (a) => !a.eklenti || (a.eklenti === 'mail' ? mailAktif : calistiriciKurulu)
}

// Aracın açılacağı yol. Alt alan adı seçiliyse ve aracın alt alan sayfası varsa oraya.
export function aracHedefi(a: MusteriArac, domainId: number | string, altId?: number | null): { yol: string; altAlanda: boolean } {
  const kok = `/abonelikler/${domainId}`
  if (a.kapsam === 'alan' && altId != null && a.altYol !== undefined) {
    return { yol: `${kok}/subdomainler/${altId}${a.altYol ? `/${a.altYol}` : ''}`, altAlanda: true }
  }
  return { yol: a.yol ? `${kok}/${a.yol}` : kok, altAlanda: false }
}

// Bir müşteri yolunun aracı (sekme başlığı için): /abonelikler/{id}/<yol>[/...] → en uzun eşleşen araç.
export function yolunAraci(pathname: string): MusteriArac | null {
  const m = /^\/abonelikler\/[0-9]+\/(.+?)\/?$/.exec(pathname)
  if (!m) return null
  const kalan = m[1]
  let enIyi: MusteriArac | null = null
  for (const a of MUSTERI_ARACLAR) {
    if (!a.yol) continue
    if ((kalan === a.yol || kalan.startsWith(`${a.yol}/`)) && (!enIyi || a.yol.length > enIyi.yol.length)) enIyi = a
  }
  return enIyi
}
