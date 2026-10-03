// Bildirim ortak yardımcıları (D3b): tip, kategori etiketi, bilinen başlıkların
// EN karşılığı, göreli zaman ve TIKLAMA HEDEFİ (tek kaynak — zil, bildirim sayfası).
//
// Başlıklar sunucuda sabit TR metindir (üreticiler: internal/pano, hesapaski,
// backups…); burada bilinenler EN'e çevrilir, bilinmeyen başlık olduğu gibi kalır.
// Mesaj gövdesi (alan adı + sayılar) çevrilmez.
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import type { Rol } from '@/lib/rol'

export type Bildirim = {
  id: number
  seviye: string
  kategori: string
  baslik: string
  mesaj: string
  domain_id: number | null
  ref_tur?: string
  ref_id?: number
  okundu: boolean
  tarih: string
}

const EN: Record<string, string> = {
  // Kategoriler
  'Kota': 'Quota', 'SSL': 'SSL', 'Askı': 'Suspension', 'Yedek': 'Backup', 'Website Security': 'Website Security',
  'Antivirüs': 'Antivirus', 'Saldırı zinciri': 'Attack chain', 'Taşıma': 'Migration', 'Toplu işlem': 'Bulk operation',
  'Sistem': 'System', 'Genel': 'General',
  // Bilinen başlıklar
  "Disk kotası %85'i aştı": 'Disk quota exceeded 85%', 'Disk kotası doldu': 'Disk quota is full',
  "Aylık trafik kotası %85'i aştı": 'Monthly traffic quota exceeded 85%', 'Aylık trafik kotası doldu': 'Monthly traffic quota is full',
  "Kaynak havuzu: Disk kotası %85'i aştı": 'Resource pool: disk exceeded 85%', 'Kaynak havuzu: Disk kotası doldu': 'Resource pool: disk is full',
  "Kaynak havuzu: Aylık trafik kotası %85'i aştı": 'Resource pool: monthly traffic exceeded 85%', 'Kaynak havuzu: Aylık trafik kotası doldu': 'Resource pool: monthly traffic is full',
  "Kaynak havuzu: Hesap sayısı %85'i aştı": 'Resource pool: account count exceeded 85%', 'Kaynak havuzu: Hesap sayısı doldu': 'Resource pool: account limit reached',
  'Bayi kaynak havuzu doldu': 'Reseller resource pool is full',
  'SSL sertifikası yenilenemedi': 'SSL certificate could not be renewed', 'SSL sertifikasının süresi doluyor': 'SSL certificate is about to expire',
  'SSL sertifikasının süresi doldu': 'SSL certificate has expired',
  'Hesap askıya alındı': 'Account suspended', 'Hesap yeniden etkinleştirildi': 'Account reactivated',
  'Website Security: yeni yüksek önemli bulgu': 'Website Security: new high-severity finding',
  'Taşıma tamamlandı': 'Migration completed', 'Taşıma hatayla bitti': 'Migration failed', 'Taşıma yarıda kesildi': 'Migration interrupted',
  'Taşıma kısmen tamamlandı': 'Migration partially completed',
  'Toplu işlem tamamlandı': 'Bulk operation completed', 'Toplu işlem başarısız oldu': 'Bulk operation failed',
  'Toplu işlem kısmen tamamlandı': 'Bulk operation partially completed',
  'Yedek başarısız': 'Backup failed', 'Swap alani yok': 'No swap space',
  // Zaman
  'az önce': 'just now', '{0} dk önce': '{0} min ago', '{0} sa önce': '{0} h ago', '{0} gün önce': '{0} d ago',
}

export const bildirimCevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

const KATEGORI: Record<string, string> = {
  kota: 'Kota', ssl: 'SSL', aski: 'Askı', yedek: 'Yedek', websec: 'Website Security', antivirus: 'Antivirüs',
  zincir: 'Saldırı zinciri', tasima: 'Taşıma', toplu: 'Toplu işlem', sistem: 'Sistem',
}
export const kategoriAd = (k: string): string => bildirimCevir(KATEGORI[k] || (k ? k[0].toUpperCase() + k.slice(1) : 'Genel'))
export const baslikAd = (b: Pick<Bildirim, 'baslik'>): string => bildirimCevir(b.baslik)

// Eski kayıtlardaki tam sunucu yolunu (/home/<kullanıcı>/) gizle — göreli kalır.
// (Yeni kayıtlar sunucuda bildirim.Guvenli'den geçer; bu eski satırlar için.)
export const yolGizle = (m: string) => m.replace(/\/home\/[^/\s]+\//g, '')

// Sunucu zaman damgası UTC ama bölgesiz ("2026-08-22 14:14:45"): 'Z' eklenmezse
// yerel sanılır ve UTC+3'te 3 saat kayar.
export function goreliZaman(t: string): string {
  const d = new Date(t.replace(' ', 'T') + (t.includes(' ') ? 'Z' : ''))
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (!isFinite(s) || s < 0) return t.slice(0, 16)
  const f = (k: string, n: number) => bildirimCevir(k).replace('{0}', String(n))
  if (s < 60) return bildirimCevir('az önce')
  if (s < 3600) return f('{0} dk önce', Math.floor(s / 60))
  if (s < 86400) return f('{0} sa önce', Math.floor(s / 3600))
  if (s < 604800) return f('{0} gün önce', Math.floor(s / 86400))
  return t.slice(0, 10)
}

// hedef — bildirime tıklayınca gidilecek sayfa (null = yalnız okundu işaretlenir).
// Müşteriye yalnız kendi aboneliğinin altı ve /bildirimler; yönetici sayfaları
// (antivirüs merkezi, taşıma, toplu) yalnız ilgili rolde.
export function hedef(b: Bildirim, rol: Rol | null | undefined): string | null {
  const d = b.domain_id
  const musteri = rol === 'user'
  if (d) {
    const kok = `/abonelikler/${d}`
    switch (b.kategori) {
      case 'yedek': return `${kok}/yedekler`
      case 'ssl': return `${kok}/ssl`
      case 'antivirus': case 'zincir': return `${kok}/imunify`
      case 'websec': return musteri ? kok : `/website-security/domain/${d}`
      default: return kok // kota, askı, diğerleri: aboneliğin ana sayfası
    }
  }
  if (musteri) return null
  switch (b.kategori) {
    case 'tasima': return '/araclar/tasima'
    case 'kota': return '/' // bayi havuzu: pano
    case 'toplu': return rol === 'admin' ? '/domainler' : null
    case 'antivirus': return rol === 'admin' ? '/antivirus' : null
    case 'zincir': return '/saldiri-zincirleri'
    case 'yedek': return rol === 'admin' ? '/backup-yonetimi' : null
    default: return null
  }
}

// Seviye tonu (tema: Tailux semantik renkleri).
export const seviyeSinif = (s: string) =>
  s === 'kritik' ? 'bg-error/10 text-error dark:bg-error/15'
    : s === 'uyari' ? 'bg-warning/10 text-warning dark:bg-warning/15'
      : 'bg-info/10 text-info dark:bg-info/15'
