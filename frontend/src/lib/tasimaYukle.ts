// Site Taşıma — arşiv yükleme için SAF yardımcılar (parça planı, yeniden deneme/devam kararı,
// boyut/uzantı doğrulama, bayt/hız biçimleme, sunucu yanıtı ayıklama). Ağ/DOM yok: yan etkisiz.
// Sunucu sözleşmesi: lib/tasima.ts başı ve POST/GET/PUT /tasima/arsiv/yukle*.
// Not: projede birim test koşucusu (vitest/jest) yok; bu yüzden saf ve ayrı tutuldu.

export const ARSIV_UZANTILARI = ['.tar.gz', '.tgz', '.tar.zst', '.tar.xz', '.tar.bz2', '.tar'] as const
export const YENIDEN_DENEME_TAVAN = 5

/** Uzantı yalnız İPUCU (kesin doğrulama sunucuda). */
export function uzantiBilinen(ad: string): boolean {
  const k = ad.toLowerCase()
  return ARSIV_UZANTILARI.some(u => k.endsWith(u))
}

export type YuklemeRed = '' | 'bos' | 'tavan' | 'tavanYok'

/** Dosya yüklenebilir mi? tavan 0/geçersiz → tavan bilinmiyor, reddedilir (güvenli-kapalı). */
export function yuklemeDogrula(boyut: number, tavan: number): YuklemeRed {
  if (!Number.isFinite(boyut) || boyut <= 0) return 'bos'
  if (!Number.isFinite(tavan) || tavan <= 0) return 'tavanYok'
  return boyut > tavan ? 'tavan' : ''
}

/** Parça sayısı. */
export function parcaSayisi(boyut: number, parca: number): number {
  if (!(boyut > 0) || !(parca > 0)) return 0
  return Math.ceil(boyut / parca)
}

/** `konum`dan başlayan parçanın [bas, son) aralığı; konum >= boyut ise null. */
export function parcaAraligi(konum: number, boyut: number, parca: number): { bas: number; son: number } | null {
  if (!(parca > 0) || !(konum >= 0) || konum >= boyut) return null
  return { bas: konum, son: Math.min(konum + parca, boyut) }
}

export type HataKarari = { eylem: 'esitle'; alinan: number } | { eylem: 'yeniden' } | { eylem: 'vazgec' }

/**
 * PUT parçası başarısız olunca ne yapılır?
 *  - 409 + gövdede geçerli `alinan` → sunucudaki konuma eşitle ve devam et;
 *  - yanıt yok (ağ/zaman aşımı), 408, 429, 5xx → yeniden dene (çağıran GET ile alinan'ı okur);
 *  - diğer 4xx (403/413/404/422 …) → vazgeç.
 */
export function hataKarari(durum: number | undefined, govdeAlinan: unknown): HataKarari {
  if (durum === 409 && typeof govdeAlinan === 'number' && Number.isFinite(govdeAlinan) && govdeAlinan >= 0) {
    return { eylem: 'esitle', alinan: govdeAlinan }
  }
  if (durum === undefined || durum === 408 || durum === 429 || durum >= 500) return { eylem: 'yeniden' }
  return { eylem: 'vazgec' }
}

/** Artan bekleme (ms): 1sn, 2sn, 4sn, 8sn, 15sn tavan. deneme 1'den başlar. */
export function beklemeMs(deneme: number): number {
  const n = Math.max(1, Math.floor(deneme))
  return Math.min(1000 * 2 ** (n - 1), 15_000)
}

export function denemeHakkiVar(deneme: number): boolean {
  return deneme < YENIDEN_DENEME_TAVAN
}

/** Yarım yükleme aynı dosya mı (ad + boyut uyuşmalı)? */
export function ayniDosya(y: { ad: string; boyut: number }, d: { name: string; size: number }): boolean {
  return y.ad === d.name && y.boyut === d.size
}

const BIRIMLER = ['B', 'KB', 'MB', 'GB', 'TB']
/** Bayt → insan okur (1024 tabanlı). */
export function baytBicim(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  let i = 0
  let d = n
  while (d >= 1024 && i < BIRIMLER.length - 1) { d /= 1024; i++ }
  return `${i === 0 ? d : d.toFixed(d >= 100 ? 0 : d >= 10 ? 1 : 2)} ${BIRIMLER[i]}`
}

/** Bayt/sn (ms içinde aktarılan bayttan). */
export function hizHesapla(bayt: number, ms: number): number {
  return ms > 0 && bayt > 0 ? (bayt * 1000) / ms : 0
}

/** Kalan saniye; hız 0 ise 0 (bilinmiyor). */
export function kalanSn(kalanBayt: number, hiz: number): number {
  return hiz > 0 && kalanBayt > 0 ? kalanBayt / hiz : 0
}

export function yuzdeHesapla(alinan: number, boyut: number): number {
  if (!(boyut > 0)) return 0
  return Math.max(0, Math.min(100, Math.floor((alinan / boyut) * 100)))
}

/** Taşıma için gereken boş alan ≈ arşiv × 2,5 (sunucu `onkontrol` göndermezse yedek). */
export function gerekliBosAlan(arsivBayt: number): number {
  return Math.ceil(arsivBayt * 2.5)
}

/** Uzun listede ilk n + kalan sayısı. */
export function listeKes<T>(l: T[], n = 10): { gorunen: T[]; kalan: number } {
  return l.length > n ? { gorunen: l.slice(0, n), kalan: l.length - n } : { gorunen: l, kalan: 0 }
}

// ---- sunucu yanıtı ayıklama (güvenilmez girdi) ----
function nesne(v: unknown): Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
}
function sayi(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : 0
}

export type ArsivYukleme = { id: number; ad: string; boyut: number; alinan: number; durum: 'yukleniyor' | 'hazir'; olusturma: string }

export function yuklemeAyikla(ham: unknown): ArsivYukleme | null {
  const g = nesne(ham)
  const id = sayi(g.id)
  if (!id || !Number.isInteger(id)) return null
  return {
    id,
    ad: typeof g.ad === 'string' ? g.ad : '',
    boyut: sayi(g.boyut),
    alinan: sayi(g.alinan),
    durum: g.durum === 'hazir' ? 'hazir' : 'yukleniyor',
    olusturma: typeof g.olusturma === 'string' ? g.olusturma : '',
  }
}

export function yuklemeListeAyikla(ham: unknown): ArsivYukleme[] {
  const l = nesne(ham).yuklemeler
  if (!Array.isArray(l)) return []
  const s: ArsivYukleme[] = []
  for (const x of l) { const y = yuklemeAyikla(x); if (y) s.push(y) }
  return s
}

export type ArsivOzet = {
  arsiv_bayt: number; uye_sayisi: number; dosya_sayisi: number; toplam_bayt: number
  db_sayisi: number; posta_sayisi: number; cron_sayisi: number; dns_sayisi: number
}
export type ArsivOnkontrol = { arsiv_bayt: number; gerekli_bos_bayt: number }
export type ArsivKesifBilgi = { ozet: ArsivOzet; onkontrol: ArsivOnkontrol; uyarilar: string[]; tasinmayacak: string[] }

function metinListe(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

export function kesifBilgiAyikla(ham: unknown): ArsivKesifBilgi {
  const g = nesne(ham)
  const o = nesne(g.ozet)
  const k = nesne(g.onkontrol)
  const ozet: ArsivOzet = {
    arsiv_bayt: sayi(o.arsiv_bayt), uye_sayisi: sayi(o.uye_sayisi), dosya_sayisi: sayi(o.dosya_sayisi),
    toplam_bayt: sayi(o.toplam_bayt), db_sayisi: sayi(o.db_sayisi), posta_sayisi: sayi(o.posta_sayisi),
    cron_sayisi: sayi(o.cron_sayisi), dns_sayisi: sayi(o.dns_sayisi),
  }
  const arsivBayt = sayi(k.arsiv_bayt) || ozet.arsiv_bayt
  return {
    ozet,
    onkontrol: { arsiv_bayt: arsivBayt, gerekli_bos_bayt: sayi(k.gerekli_bos_bayt) || gerekliBosAlan(arsivBayt) },
    uyarilar: metinListe(g.uyarilar),
    tasinmayacak: metinListe(g.tasinmayacak),
  }
}
