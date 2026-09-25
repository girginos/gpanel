// Özet kartları: Disk, Bu ay trafik, Veritabanı, Site sağlığı, SSL, Son yedek.
// Renk DURUMDAN gelir (tonlar.ts): doluluk %75+ uyarı, %90+ tehlike; SSL/yedek/sağlık
// durumu kendi tonunu seçer. Veri yoksa iskelet, hata varsa kart içinde hata + yeniden dene.
import { Badge } from '@/components/ui'
import { I } from '@/components/Ikon'
import { TONLAR, type Ton } from '@/components/tonlar'
import type { KaynakOzet, Saglik, SSLDurum, Yedek } from './tipler'
import type { Uc } from './useUc'
import { AyrintiLink, OzetKart, type Vurgu } from './parcalar'
import { bayt, cevir, dolulukTonu, goreli, mb, sablon, sayi, sunucuZamani, tarih, tarihSaat, yuzde } from './bicim'

type KaynakUc = Uc<KaynakOzet>

const durumu = <T,>(u: Uc<T>) => ({
  yukleniyor: u.yukleniyor, hata: u.hata, bos: u.veri === null, onYeniden: () => void u.yenile(),
})

function TonRozet({ ton, children }: { ton: Exclude<Ton, 'soru'>; children: string }) {
  return <Badge color={TONLAR[ton].renk} variant="soft" className="shrink-0">{children}</Badge>
}

// Kullanım/limit metni: "%9 kullanıldı · Sınır 20 GB" ya da "Sınırsız".
function kotaAlt(kullanim: number, limit: number): string {
  if (!limit) return cevir('Sınırsız')
  return `${sablon('{0} kullanıldı', yuzde((kullanim / limit) * 100))} · ${sablon('Sınır {0}', mb(limit))}`
}

export function DiskKart({ kaynak }: { kaynak: KaynakUc }) {
  const k = kaynak.veri
  const d = k?.disk_mb
  const oran = d && d.limit ? (d.kullanim / d.limit) * 100 : 0
  const inode = k && k.inode_limit > 0 ? ` · ${sablon('Inode {0}', yuzde((k.inode_kullanim / k.inode_limit) * 100))}` : ''
  return (
    <OzetKart baslik={cevir('Disk')} ikon={I.sunucu} durum={durumu(kaynak)}
      vurgu={(d && dolulukTonu(d.kullanim, d.limit)) || 'vurgu'}
      deger={d ? mb(d.kullanim) : undefined}
      cubuk={d && d.limit ? { oran, etiket: `${cevir('Disk')} ${yuzde(oran)}` } : undefined}
      alt={d ? kotaAlt(d.kullanim, d.limit) + inode : undefined} />
  )
}

export function TrafikKart({ kaynak }: { kaynak: KaynakUc }) {
  const t = kaynak.veri?.trafik_mb
  const oran = t && t.limit ? (t.kullanim / t.limit) * 100 : 0
  return (
    <OzetKart baslik={cevir('Bu ay trafik')} ikon={I.kure} durum={durumu(kaynak)}
      vurgu={(t && dolulukTonu(t.kullanim, t.limit)) || 'vurgu'}
      deger={t ? mb(t.kullanim) : undefined}
      cubuk={t && t.limit ? { oran, etiket: `${cevir('Bu ay trafik')} ${yuzde(oran)}` } : undefined}
      alt={t ? kotaAlt(t.kullanim, t.limit) : undefined} />
  )
}

export function VeritabaniKart({ kaynak }: { kaynak: KaynakUc }) {
  const k = kaynak.veri
  const n = k?.db_sayisi.kullanim ?? 0
  const lim = k?.db_sayisi.limit ?? 0
  const boyutVar = typeof k?.db_mb === 'number'
  const adet = lim ? sablon('{0} / {1} veritabanı', n, lim) : sablon('{0} veritabanı', n)
  return (
    <OzetKart baslik={cevir('Veritabanı')} ikon={I.db} durum={durumu(kaynak)}
      vurgu={(k && dolulukTonu(n, lim)) || 'vurgu'}
      deger={k ? (boyutVar ? mb(k.db_mb as number) : adet) : undefined}
      alt={k ? (boyutVar ? adet : cevir('Toplam boyut ölçülemedi')) : undefined} />
  )
}

function hizEtiketi(ms: number): string {
  return ms < 300 ? cevir('Hızlı') : ms < 1000 ? cevir('Kabul edilebilir') : cevir('Yavaş')
}

export function SaglikKart({ saglik }: { saglik: Uc<Saglik> }) {
  const h = saglik.veri
  const ok = !!h && h.erisilebilir && h.durum_kodu >= 200 && h.durum_kodu < 400
  const kodTon: Exclude<Ton, 'soru'> = !h || h.durum_kodu === 0 || h.durum_kodu >= 500 ? 'tehlike'
    : h.durum_kodu >= 400 ? 'uyari' : h.durum_kodu >= 300 ? 'bilgi' : 'basari'
  const vurgu: Vurgu = !h ? 'vurgu' : !ok ? 'tehlike' : h.yanit_suresi_ms >= 1000 ? 'uyari' : 'basari'
  return (
    <OzetKart baslik={cevir('Site sağlığı')} ikon={I.monitor} vurgu={vurgu}
      durum={durumu(saglik)}
      deger={h ? (ok ? `${sayi(h.yanit_suresi_ms)} ms` : cevir('Erişilemiyor')) : undefined}
      rozet={h && h.durum_kodu > 0 ? <TonRozet ton={kodTon}>{`HTTP ${h.durum_kodu}`}</TonRozet> : undefined}
      alt={h ? (ok ? `${hizEtiketi(h.yanit_suresi_ms)} · ${(h.sema || '').toUpperCase()}` : (h.hata || cevir('Erişilemiyor'))) : undefined} />
  )
}

function sslKaynakAdi(k: string): string {
  if (k === 'letsencrypt') return "Let's Encrypt"
  if (k === 'self-signed') return cevir('Kendinden imzalı')
  return k
}

export function SSLKart({ ssl, domainId }: { ssl: Uc<SSLDurum>; domainId: number }) {
  const s = ssl.veri
  const bitis = s?.bitis_iso ? new Date(s.bitis_iso) : null
  const gecerliBitis = bitis && !Number.isNaN(bitis.getTime()) ? bitis : null
  const kalan = gecerliBitis ? Math.ceil((gecerliBitis.getTime() - Date.now()) / 86_400_000) : null
  const ozImzali = s?.kaynak === 'self-signed'
  let vurgu: Vurgu = 'vurgu'
  let deger: string | undefined
  let alt: string | undefined
  if (s) {
    if (!s.aktif) {
      vurgu = 'uyari'; deger = cevir('SSL yok'); alt = cevir('Site HTTPS ile korunmuyor')
    } else {
      vurgu = kalan !== null && kalan < 0 ? 'tehlike' : (kalan !== null && kalan < 15) || ozImzali ? 'uyari' : 'basari'
      deger = kalan === null ? cevir('Etkin') : kalan < 0 ? cevir('Süresi doldu') : sablon('{0} gün kaldı', kalan)
      alt = [gecerliBitis ? sablon('Bitiş {0}', tarih(gecerliBitis)) : '', sslKaynakAdi(s.kaynak)].filter(Boolean).join(' · ')
    }
  }
  return (
    <OzetKart baslik="SSL" ikon={I.kalkan} vurgu={vurgu} durum={durumu(ssl)} deger={deger} alt={alt}
      eylem={s && (vurgu === 'uyari' || vurgu === 'tehlike')
        ? <AyrintiLink to={`/abonelikler/${domainId}/ssl`}>{cevir("SSL'e git")}</AyrintiLink> : undefined} />
  )
}

const HAFTA_MS = 7 * 86_400_000

export function YedekKart({ yedekler, domainId }: { yedekler: Uc<Yedek[]>; domainId: number }) {
  const liste = yedekler.veri
  const son = liste && liste.length ? liste[0] : null // uç id DESC sıralı döner
  const d = son ? sunucuZamani(son.olusturma) : null
  const eski = !!d && Date.now() - d.getTime() > HAFTA_MS
  const mevcutTon: Exclude<Ton, 'soru'> = son?.mevcut === 'yerel' ? 'basari' : son?.mevcut === 'yok' ? 'tehlike' : 'bilgi'
  const mevcutMetin = son?.mevcut === 'yerel' ? cevir('Yerel · dosya mevcut')
    : son?.mevcut === 'yok' ? cevir('Dosya bulunamadı') : cevir('Uzakta · doğrulanmadı')
  const vurgu: Vurgu = !liste ? 'vurgu' : !son ? 'uyari' : son.mevcut === 'yok' ? 'tehlike' : eski ? 'uyari' : mevcutTon
  return (
    <OzetKart baslik={cevir('Son yedek')} ikon={I.bulut} vurgu={vurgu} durum={durumu(yedekler)}
      deger={liste ? (son ? <span title={d ? tarihSaat(d) : son.olusturma}>{goreli(d, son.olusturma)}</span> : cevir('Henüz yedek yok')) : undefined}
      rozet={son ? <TonRozet ton={mevcutTon}>{mevcutMetin}</TonRozet> : undefined}
      alt={son ? [bayt(son.boyut_b), eski ? cevir('7 günden eski') : ''].filter(Boolean).join(' · ') : undefined}
      eylem={liste && (!son || vurgu === 'uyari' || vurgu === 'tehlike')
        ? <AyrintiLink to={`/abonelikler/${domainId}/yedekler`}>{cevir('Yedeklere git')}</AyrintiLink> : undefined} />
  )
}
