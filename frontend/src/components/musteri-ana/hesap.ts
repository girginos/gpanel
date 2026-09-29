// Müşteri ana sayfası — GERÇEK veriden türetilen durumlar (saf fonksiyonlar).
// Uyarı bandı yalnız burada türetilen maddeleri gösterir; veri yoksa madde yok, madde
// yoksa bant hiç çizilmez. Eşikler tek yerde:
import type { Domain } from '@/components/domainTipi'
import type { KaynakOzet, Limit } from '@/components/alanpano/tipler'
import { I } from '@/components/Ikon'
import { mb } from '@/components/alanpano/bicim'
import { cevir, cevirF, oran, sayi } from './dil'
import type { AltAlan, AlanSatiri, BildirimYaniti, PhpSurum, Uyari, UyariTonu, Yedek } from './tipler'

export const SSL_UYARI_GUN = 14 // bu kadar gün (ve altı) kalan sertifika uyarı verir
export const YEDEK_ESKI_GUN = 7 // son geçerli yedek bundan eskiyse bilgi verir
export const KULLANIM_ESIK = 80 // yüzde; üstü uyarı rengi, 100 tehlike

export const yuzde = (x: Limit): number => (x.limit > 0 ? Math.min(100, Math.round((x.kullanim / x.limit) * 100)) : 0)

// 'YYYY-MM-DD' → bugünden kaç gün sonra (bugün 0, geçmiş negatif). Okunamazsa null.
export function kalanGun(tarih: string | undefined, simdi: Date): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(tarih ?? '')
  if (!m) return null
  const hedef = Date.UTC(+m[1], +m[2] - 1, +m[3])
  const bugun = Date.UTC(simdi.getFullYear(), simdi.getMonth(), simdi.getDate())
  return Math.round((hedef - bugun) / 86_400_000)
}

// 'YYYY-MM-DD HH:MM' → kaç tam gün önce. Okunamazsa null.
export function gecenGun(tarih: string, simdi: Date): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(tarih)
  if (!m) return null
  const t = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]).getTime()
  return Number.isFinite(t) ? Math.max(0, Math.floor((simdi.getTime() - t) / 86_400_000)) : null
}

// Dosyası bulunamayan kayıt (mevcut 'yok') geçerli yedek sayılmaz. Liste yeniden eskiye.
export const gecerliYedekler = (l: Yedek[]): Yedek[] => l.filter((y) => y.mevcut !== 'yok')

export const eolSurumleri = (l: PhpSurum[] | null): Set<string> => new Set((l ?? []).filter((s) => s.eol).map((s) => s.surum))

// Kullanım paneli satırları (/kaynak). Limit 0 = sınırsız (çubuk çizilmez).
export type KullanimSatiri = { ad: string; x: Limit; bicim: (n: number) => string }

export function kullanimSatirlari(k: KaynakOzet): KullanimSatiri[] {
  const l: KullanimSatiri[] = [{ ad: 'Disk alanı', x: k.disk_mb, bicim: mb }]
  // Inode kotası yalnız XFS kota aktifken ölçülür (limit 0 → ölçülmüyor, satır yok).
  if (k.inode_limit > 0) l.push({ ad: 'Dosya sayısı (inode)', x: { kullanim: k.inode_kullanim, limit: k.inode_limit }, bicim: sayi })
  l.push({ ad: 'Aylık trafik', x: k.trafik_mb, bicim: mb })
  l.push({ ad: 'Veritabanları', x: k.db_sayisi, bicim: sayi })
  l.push({ ad: 'FTP hesapları', x: k.ftp_sayisi, bicim: sayi })
  // E-posta sayacının gerçek kaynağı yoksa (eposta_olculmuyor) hep 0 yanıltır — gizli.
  if (!k.eposta_olculmuyor) l.push({ ad: 'E-posta hesapları', x: k.eposta_sayi, bicim: sayi })
  return l
}

export type SslDurumu = { tur: 'yok' | 'doldu' | 'bitiyor' | 'gecerli'; gun: number | null }

export function sslDurumu(d: Pick<Domain, 'ssl' | 'ssl_bitis'>, simdi: Date): SslDurumu {
  if (!d.ssl) return { tur: 'yok', gun: null }
  const gun = kalanGun(d.ssl_bitis, simdi)
  if (gun === null) return { tur: 'gecerli', gun: null }
  if (gun <= 0) return { tur: 'doldu', gun }
  if (gun <= SSL_UYARI_GUN) return { tur: 'bitiyor', gun }
  return { tur: 'gecerli', gun }
}

// Ana alan adı + alt alan adları (alfabetik) — panelin ve kapsam seçicinin satırları.
export function alanSatirlari(d: Domain, altlar: AltAlan[] | null, eol: Set<string>): AlanSatiri[] {
  const ana: AlanSatiri = { anahtar: 'ana', ad: d.alan_adi, tur: 'ana', altId: null, php: d.php_surum, phpEol: eol.has(d.php_surum) }
  const alt = [...(altlar ?? [])]
    .sort((x, y) => x.tam_ad.localeCompare(y.tam_ad))
    .map<AlanSatiri>((s) => ({ anahtar: `alt-${s.id}`, ad: s.tam_ad, tur: 'alt', altId: s.id, php: s.php_surum, phpEol: eol.has(s.php_surum) }))
  return [ana, ...alt]
}

const TON_SIRA: Record<UyariTonu, number> = { tehlike: 0, uyari: 1, bilgi: 2 }

// Eski kayıtlardaki tam sunucu yolunu (/home/<kullanıcı>/) gizle (TopBar ile aynı).
const yolGizle = (m: string): string => m.replace(/\/home\/[^/\s]+\//g, '')

type Girdi = {
  domainId: number
  domain: Domain | null
  kaynak: KaynakOzet | null
  altlar: AltAlan[] | null
  yedekler: Yedek[] | null
  surumler: PhpSurum[] | null
  bildirim: BildirimYaniti | null
  simdi: Date
}

export function uyarilariTuret(g: Girdi): Uyari[] {
  const { domain: d, kaynak: k, simdi } = g
  const kok = `/abonelikler/${g.domainId}`
  const l: Uyari[] = []
  if (d) {
    if (d.askida) {
      l.push({
        id: 'askida',
        ton: 'tehlike',
        baslik: cevir('Hesabınız askıya alındı'),
        aciklama: cevir('Siteniz ziyaretçilere bakım sayfası gösteriyor. Ayrıntı için hizmet sağlayıcınızla iletişime geçin.'),
      })
    }
    const ssl = sslDurumu(d, simdi)
    const sslEylem = { ikon: I.kilit, yol: `${kok}/ssl` }
    if (ssl.tur === 'yok') {
      l.push({
        id: 'ssl-yok',
        ton: 'tehlike',
        baslik: cevirF('{0} HTTPS ile korunmuyor', d.alan_adi),
        aciklama: cevir('Ziyaretçiler tarayıcıda "güvenli değil" uyarısı görür.'),
        eylem: { ...sslEylem, etiket: cevir('SSL kur') },
      })
    } else if (ssl.tur === 'doldu') {
      l.push({
        id: `ssl-doldu-${d.ssl_bitis}`,
        ton: 'tehlike',
        baslik: cevirF('{0} SSL sertifikasının süresi doldu', d.alan_adi),
        aciklama: cevir('Ziyaretçiler tarayıcıda güvenlik uyarısı görür; sertifikayı yenileyin.'),
        eylem: { ...sslEylem, etiket: cevir('SSL sayfasını aç') },
      })
    } else if (ssl.tur === 'bitiyor') {
      l.push({
        id: `ssl-bitiyor-${d.ssl_bitis}`,
        ton: 'uyari',
        baslik: cevirF('{0} SSL sertifikası {1} gün içinde bitiyor', d.alan_adi, ssl.gun ?? 0),
        aciklama: cevir('Otomatik yenileme gecikmiş olabilir; durumu SSL sayfasından kontrol edin.'),
        eylem: { ...sslEylem, etiket: cevir('SSL sayfasını aç') },
      })
    }
  }

  if (k) {
    const kotalar: Array<{ kod: string; ad: string; x: Limit; bicim: (n: number) => string; aciklama: string; eylem: Uyari['eylem'] }> = [
      { kod: 'disk', ad: 'Disk alanı', x: k.disk_mb, bicim: mb, aciklama: 'Yer açmak için gereksiz dosyaları ve eski yedekleri temizleyin.', eylem: { etiket: cevir('Dosya Yöneticisi'), ikon: I.klasor, yol: `${kok}/dosyalar` } },
      { kod: 'inode', ad: 'Dosya sayısı (inode)', x: { kullanim: k.inode_kullanim, limit: k.inode_limit }, bicim: sayi, aciklama: 'Önbellek ve geçici dosyaları temizleyerek dosya sayısını azaltın.', eylem: { etiket: cevir('Dosya Yöneticisi'), ikon: I.klasor, yol: `${kok}/dosyalar` } },
      { kod: 'trafik', ad: 'Aylık trafik', x: k.trafik_mb, bicim: mb, aciklama: 'Limit dolarsa siteye erişim kısıtlanabilir.', eylem: { etiket: cevir('İstatistikler'), ikon: I.grafik, yol: `${kok}/istatistik` } },
    ]
    for (const q of kotalar) {
      if (q.x.limit <= 0) continue
      const y = yuzde(q.x)
      if (y < KULLANIM_ESIK) continue
      const dolu = y >= 100
      l.push({
        id: `kota-${q.kod}-${dolu ? 100 : KULLANIM_ESIK}`,
        ton: dolu ? 'tehlike' : 'uyari',
        baslik: dolu ? cevirF('{0} limiti doldu', cevir(q.ad)) : cevirF('{0} {1} dolu', cevir(q.ad), oran(y)),
        aciklama: `${cevirF('{0} / {1} kullanılıyor.', q.bicim(q.x.kullanim), q.bicim(q.x.limit))} ${cevir(q.aciklama)}`,
        eylem: q.eylem,
      })
    }
  }

  if (g.yedekler) {
    const gecerli = gecerliYedekler(g.yedekler)
    const yedekEylem = { ikon: I.bulut, yol: `${kok}/yedekler` }
    if (!gecerli.length) {
      l.push({
        id: `yedek-yok-${g.yedekler.length}`,
        ton: 'uyari',
        baslik: cevir('Geçerli bir yedeğiniz yok'),
        aciklama: g.yedekler.length
          ? cevir('Kayıtlı yedeklerin dosyaları bulunamadı; yeni bir yedek alın.')
          : cevir('Hesabınızın henüz yedeği alınmamış; bir yedek alın ya da otomatik yedeklemeyi açın.'),
        eylem: { ...yedekEylem, etiket: cevir('Yedek al') },
      })
    } else {
      const gun = gecenGun(gecerli[0].olusturma, simdi)
      if (gun !== null && gun >= YEDEK_ESKI_GUN) {
        l.push({
          id: `yedek-eski-${gecerli[0].id}`,
          ton: 'bilgi',
          baslik: cevirF('Son yedek {0} gün önce alındı', gun),
          aciklama: cevir('Düzenli yedek için otomatik yedeklemeyi açabilirsiniz.'),
          eylem: { ...yedekEylem, etiket: cevir('Yedekler') },
        })
      }
    }
  }

  if (d && g.surumler) {
    const eol = eolSurumleri(g.surumler)
    if (eol.has(d.php_surum)) {
      l.push({
        id: `php-${d.php_surum}`,
        ton: 'uyari',
        baslik: cevirF('{0} PHP {1} kullanıyor', d.alan_adi, d.php_surum),
        aciklama: cevir('Bu sürüm artık güvenlik güncellemesi almıyor; desteklenen bir sürüme geçin.'),
        eylem: { etiket: cevir('PHP sürümünü değiştir'), ikon: I.parca, yol: `${kok}/php` },
      })
    }
    const eskiAltlar = (g.altlar ?? []).filter((s) => eol.has(s.php_surum))
    if (eskiAltlar.length === 1) {
      const s = eskiAltlar[0]
      l.push({
        id: `php-alt-${s.id}-${s.php_surum}`,
        ton: 'uyari',
        baslik: cevirF('{0} PHP {1} kullanıyor', s.tam_ad, s.php_surum),
        aciklama: cevir('Bu sürüm artık güvenlik güncellemesi almıyor; desteklenen bir sürüme geçin.'),
        eylem: { etiket: cevir('PHP sürümünü değiştir'), ikon: I.parca, yol: `${kok}/subdomainler/${s.id}` },
      })
    } else if (eskiAltlar.length > 1) {
      l.push({
        id: `php-alt-${eskiAltlar.map((s) => `${s.id}:${s.php_surum}`).join(',')}`,
        ton: 'uyari',
        baslik: cevirF('{0} alt alan adı desteği biten PHP sürümü kullanıyor', eskiAltlar.length),
        aciklama: eskiAltlar.map((s) => `${s.tam_ad} (PHP ${s.php_surum})`).join(', '),
        eylem: { etiket: cevir('Alt Alan Adları'), ikon: I.kure, yol: `${kok}/subdomainler` },
      })
    }
  }

  if (g.bildirim) {
    const onemli = g.bildirim.bildirimler.filter((b) => !b.okundu && (b.seviye === 'kritik' || b.seviye === 'uyari'))
    if (onemli.length) {
      const ton: UyariTonu = onemli.some((b) => b.seviye === 'kritik') ? 'tehlike' : 'uyari'
      const ilk = onemli[0]
      l.push({
        id: `bildirim-${onemli.map((b) => b.id).join(',')}`,
        ton,
        baslik: onemli.length === 1 ? ilk.baslik : cevirF('{0} okunmamış önemli bildirim', onemli.length),
        aciklama: onemli.length === 1 ? yolGizle(ilk.mesaj) : cevirF('En yenisi: {0}', ilk.baslik),
        eylem: { etiket: cevir('Bildirimleri aç'), ikon: I.zil, yol: '/bildirimler' },
      })
    }
  }

  return l.sort((x, y) => TON_SIRA[x.ton] - TON_SIRA[y.ton])
}
