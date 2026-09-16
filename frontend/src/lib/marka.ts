/* Marka (whitelabel) — panelin görünen kimliği.
 *
 * Değerler AUTH DIŞI `/api/v1/marka` ucundan gelir; çünkü ilk gösteren yer
 * GİRİŞ SAYFASIDIR ve o sayfa kimlik doğrulamadan ÖNCE render edilir.
 *
 * 🔴 MARKA PANELİ BLOKLAMAZ. Uç yavaş/ölü olsa bile panel varsayılan markayla
 * açılır (kısa zaman aşımı + varsayılana düşme). Bir görünüm ayarının,
 * paneli erişilemez kılma yetkisi olamaz.
 *
 * 🔴 HİÇBİR DEĞER innerHTML/dangerouslySetInnerHTML İLE BASILMAZ. Alanlar
 * yönetici tarafından girilir ama bir whitelabel formu, panelin kendi
 * oturumunda çalışan XSS için en doğal taşıyıcıdır. Metinler React metin
 * düğümü ya da textContent olarak yazılır.
 */

import { useSyncExternalStore } from 'react'

export type Marka = {
  panel_adi: string
  baslik: string
  kisa_ad: string
  vurgu_renk: string
  logo: boolean
  logo_surum: number
  /** Tarayıcı sekmesi simgesi — logodan AYRI yuva. */
  favicon: boolean
  favicon_surum: number
  /** Panelin her sayfasında görünen alt bilgi. Boşsa alt bilgi çizilmez. */
  footer: string
  giris_baslik: string
  giris_alt: string
  giris_dipnot: string
  surum_goster: boolean
  destek_url: string
  banner: boolean
  banner_surum: number
  banner_metin: string
  banner_url: string
}

export const VARSAYILAN: Marka = {
  panel_adi: 'GirginOSPanel',
  baslik: 'GirginOSPanel',
  kisa_ad: 'gPanel',
  vurgu_renk: '#2563eb',
  logo: false,
  logo_surum: 0,
  favicon: false,
  favicon_surum: 0,
  footer: '',
  giris_baslik: 'Hoş geldiniz',
  giris_alt: '',
  giris_dipnot: '',
  surum_goster: true,
  destek_url: '',
  banner: false,
  banner_surum: 0,
  banner_metin: '',
  banner_url: '',
}

let mevcut: Marka = VARSAYILAN

// Özgün favicon — marka logosu kaldırıldığında geri dönülecek değer.
// Modül yüklenirken, herhangi bir marka uygulanmadan ÖNCE okunur.
const ozgunFavicon =
  typeof document !== 'undefined'
    ? document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.getAttribute('href') ?? null
    : null
const ozgunFaviconTip =
  typeof document !== 'undefined'
    ? document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.getAttribute('type') ?? null
    : null
const dinleyiciler = new Set<(m: Marka) => void>()

export function marka(): Marka {
  return mevcut
}

export function markaDinle(f: (m: Marka) => void): () => void {
  dinleyiciler.add(f)
  return () => {
    dinleyiciler.delete(f)
  }
}

/** useMarka — bilesenler icin. Eklentiden kaydedilince otomatik yeniden cizer. */
export function useMarka(): Marka {
  return useSyncExternalStore(markaDinle, marka, marka)
}

/** logoURL — yüklenmiş logo yoksa null. Sürüm parametresi tarayıcı önbelleğini kırar. */
export function logoURL(m: Marka = mevcut): string | null {
  return m.logo ? `/api/v1/marka/logo?v=${m.logo_surum}` : null
}

/**
 * faviconURL — sekme simgesinin adresi. Yüklenmemişse null.
 *
 * Logodan AYRI: yönetici favicon yüklemediyse null döner ve çağıran logoya
 * düşer (eski davranış korunur).
 */
export function faviconURL(m: Marka = mevcut): string | null {
  return m.favicon ? `/api/v1/marka/favicon?v=${m.favicon_surum}` : null
}

/** bannerURL — giriş sayfası bandının GÖRSELİ. Yüklenmemişse null. */
export function bannerURL(m: Marka = mevcut): string | null {
  return m.banner ? `/api/v1/marka/banner?v=${m.banner_surum}` : null
}

/* ── uygulama ─────────────────────────────────────────────────────────── */

function uygula(m: Marka) {
  mevcut = m
  document.title = m.baslik || m.panel_adi

  // Vurgu rengi: Tailwind `brand-*` ölçeği CSS değişkenlerine bağlıdır
  // (tailwind.config.js). Tek bir hex'ten 11 basamaklı ölçek türetilir.
  //
  // 🔴 KOŞULSUZ UYGULANIR. Önceki sürüm varsayılan renkte atlıyordu ve
  // `uygula()` yalnız UYGULUYOR, hiç GERİ ALMIYORDU: yönetici moru seçip
  // sonra varsayılana dönünce `--brand-*` değişkenleri MOR KALIYORDU (ancak
  // tam sayfa yenilemesiyle düzeliyordu). Varsayılan renk zaten doğru
  // ölçeği üretiyor, atlamanın hiçbir kazancı yok.
  // 🔴 GELEN RENK OLDUĞU GİBİ UYGULANIR — DEĞERE BAKIP EŞLEME YAPILMAZ.
  // Önceki sürüm gelen `#ea580c`'yi "API'nin ESKİ turuncu varsayılanı" sayıp
  // Tailux mavisine çeviriyordu. Ama turuncu, eklentinin GEÇERLİ saydığı
  // (hatta doğrulama iletisinde ÖRNEK olarak verdiği, form alanının
  // placeholder'ı olan) bir seçimdir: yönetici onu bilerek kaydettiğinde
  // diske/API'ye `#ea580c` yazılıyor, form yeniden açıldığında turuncu
  // görünüyor, eklentinin ön izlemesi turuncu boyuyor — ama panel MAVİ
  // çiziliyordu ve hiçbir hata iletisi yoktu. Yöneticinin tek kaçışı komşu
  // bir tona (#ea580d) kaymaktı, yani "seçilen renk alınamıyor" demekti.
  // Bir renk DEĞERİNDEN niyet okunamaz; varsayılanı belirlemek sunucunun
  // işidir, istemci onu tahmin etmez.
  const _gelen = (m.vurgu_renk || '').toLowerCase()
  const _renk = !_gelen ? VARSAYILAN.vurgu_renk : m.vurgu_renk
  olcekUygula(_renk)

  // 🔴 theme-color DA markadan gelir. index.html'de sabit `#ea580c` yazıyor;
  // marka rengi değiştiğinde mobil tarayıcı çubuğu ESKİ turuncuda kalıyordu —
  // markalanmış panelde en görünür yerlerden biri.
  const tc = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  if (tc) tc.setAttribute('content', _renk)

  // Favicon. 🔴 GERİ ALMA DA GEREKLİ: logo kaldırıldığında `href` eski
  // (artık 404 dönen) adreste kalıyor ve sekmede kırık ikon görünüyordu.
  // Özgün değer modül yüklenirken saklanır.
  // 🔴 ÖNCELİK: ayrı yüklenmiş favicon > logo. Logo genelde yatay/geniştir ve
  // sekmede tanınmaz; yönetici ayrı bir simge yüklediyse ONU kullanırız.
  // Hiçbiri yoksa aşağıdaki dal panelin özgün favicon'una geri döner.
  const u = faviconURL(m) || logoURL(m)
  if (u) {
    let l = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!l) {
      l = document.createElement('link')
      l.rel = 'icon'
      document.head.appendChild(l)
    }
    // 🔴 type KALDIRILIR: mevcut etiket `image/svg+xml` diyor; PNG bir logo
    // yüklendiğinde tip yanlış kalırsa bazı tarayıcılar ikonu çizmez.
    l.removeAttribute('type')
    l.href = u
  } else if (ozgunFavicon !== null) {
    const l = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (l && l.getAttribute('href') !== ozgunFavicon) {
      l.setAttribute('href', ozgunFavicon)
      if (ozgunFaviconTip) l.setAttribute('type', ozgunFaviconTip)
    }
  }

  dinleyiciler.forEach((f) => {
    try {
      f(m)
    } catch {
      /* bir dinleyicinin hatası diğerlerini düşürmesin */
    }
  })
}

/** markaYukle — açılışta bir kez. Zaman aşımında sessizce varsayılanda kalır. */
export async function markaYukle(): Promise<void> {
  try {
    const kes = new AbortController()
    const zaman = window.setTimeout(() => kes.abort(), 2000)
    const y = await fetch('/api/v1/marka', { signal: kes.signal, cache: 'no-store' })
    window.clearTimeout(zaman)
    if (!y.ok) return
    const g = (await y.json()) as Partial<Marka>
    uygula({ ...VARSAYILAN, ...g })
  } catch {
    /* ağ/zaman aşımı → varsayılan marka. Panel yine açılır. */
  }
}

/** markaTazele — eklenti kaydettikten sonra sayfayı yenilemeden güncellemek için. */
export async function markaTazele(): Promise<void> {
  await markaYukle()
}

// Eklenti bundle'i AYRI bir dosyadir; core'un modullerini import edemez. Kaydet
// sonrasi panelin (kenar cubugu, sekme basligi, vurgu rengi) SAYFA YENILEMEDEN
// guncellenebilmesi icin kopru window uzerinden kurulur.
//
// 🔴 Kopru yalniz TAZELEME yapar — deger ALMAZ. Eklentinin window uzerinden
// markayi dogrudan yazabilmesi, sayfadaki herhangi bir betigin paneli yeniden
// markalamasina izin vermek olurdu; tazeleme ise yalniz sunucudan okur.
declare global {
  interface Window {
    __gospMarkaTazele?: () => void
  }
}
if (typeof window !== 'undefined') {
  window.__gospMarkaTazele = () => {
    void markaYukle()
  }
}

/* ── renk ölçeği ──────────────────────────────────────────────────────── */

/* Özgün turuncu ölçeğin AÇIKLIK eğrisi. Kullanıcının verdiği tek renkten
 * tutarlı bir 11 basamak üretmek için ton/doygunluk ondan, açıklık bu
 * eğriden alınır — böylece `brand-50` her zaman çok açık, `brand-900` her
 * zaman çok koyu kalır ve panelin kontrast dengesi korunur. */
const ACIKLIK: Array<[string, number, number]> = [
  // [basamak, açıklık %, doygunluk çarpanı]
  ['50', 96.5, 0.55],
  ['100', 92.2, 0.75],
  ['200', 83.5, 0.9],
  ['300', 72.4, 0.97],
  ['400', 61.0, 1.0],
  ['500', 53.5, 1.0],
  ['600', 48.0, 1.0],
  ['700', 40.0, 0.95],
  ['800', 33.7, 0.85],
  ['900', 27.5, 0.78],
  ['950', 14.9, 0.72],
]

function olcekUygula(hex: string) {
  const rgb = hexRGB(hex)
  if (!rgb) return
  const [h, s, l600] = rgbHSL(rgb)
  const kok = document.documentElement

  // 🔴 SECILEN RENK BIREBIR `brand-600` OLUR.
  // Onceki surumde aciklik egrisi sabitti (600 = %48) ve secilen #7c3aed
  // butonlarda #6014E0 olarak cikiyordu — yonetici kendi marka rengini
  // secip BASKA bir rengi goruyordu. Simdi tum egri, 600 basamagi tam
  // secilen rengin acikligina denk gelecek sekilde KAYDIRILIR; boylece
  // hem renk sadik kalir hem basamaklar arasindaki mesafe korunur.

  // 🔴 KIRPMA SINIRLARI SEÇİLEN RENGE GÖRE GENİŞLER. Sabit [4, 97] aralığı,
  // `600` basamağı kırpmadan geçmediği için uç renklerde SIRALAMAYI bozuyordu:
  // `#000000` seçildiğinde 600 en koyu olurken 900/950 ondan AÇIK kalıyor,
  // `#ffffff` seçildiğinde 600 en açık olurken 50/100/400 aynı renge çöküyordu.
  // Sınırları l600'e göre genişletmek hem monotonluğu korur hem de seçilen
  // rengin `600`'e BİREBİR oturmasını sürdürür.
  const altSinir = Math.min(4, l600)
  const ustSinir = Math.max(97, l600)

  // 🔴 KIRPMA DEĞİL, YENİDEN ÖLÇEKLEME.
  // Basamakları tek tek kırpmak, kaydırma büyük olduğunda üst uçtaki
  // basamakları AYNI değere çökertiyordu: mor markada brand-50 ile
  // brand-100 arasındaki kontrast 1,01:1 ölçüldü — yani aynı renk.
  // Bunun yerine eğrinin açık ve koyu yarısı, 600 basamağını sabit tutacak
  // şekilde ayrı ayrı [alt, l600] ve [l600, üst] aralıklarına yeniden
  // ölçeklenir; sıralama ve basamaklar arası mesafe korunur.
  const CAPA_L = 48.0
  const ACIK_UC = 96.5 // ACIKLIK'taki en açık basamak
  const KOYU_UC = 14.9 // ACIKLIK'taki en koyu basamak
  const yenidenOlcekle = (l: number): number => {
    if (l >= CAPA_L) {
      const oran = (l - CAPA_L) / (ACIK_UC - CAPA_L)
      return l600 + oran * (ustSinir - l600)
    }
    const oran = (CAPA_L - l) / (CAPA_L - KOYU_UC)
    return l600 - oran * (l600 - altSinir)
  }

  for (const [basamak, l, sc] of ACIKLIK) {
    if (basamak === '600') {
      kok.style.setProperty('--brand-600', `${rgb[0]} ${rgb[1]} ${rgb[2]}`)
      continue
    }
    const hedef = Math.max(altSinir, Math.min(ustSinir, yenidenOlcekle(l)))
    const [r, g, b] = hslRGB(h, Math.min(100, s * sc), hedef)
    // Tailwind `rgb(var(--brand-600) / <alpha-value>)` bekler → boşluklu üçlü.
    kok.style.setProperty(`--brand-${basamak}`, `${r} ${g} ${b}`)
  }
  const tema = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  if (tema) tema.content = hex
}

function hexRGB(hex: string): [number, number, number] | null {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return null
  return [
    parseInt(hex.slice(1, 3), 16),
    parseInt(hex.slice(3, 5), 16),
    parseInt(hex.slice(5, 7), 16),
  ]
}

function rgbHSL([r, g, b]: [number, number, number]): [number, number, number] {
  const R = r / 255,
    G = g / 255,
    B = b / 255
  const maks = Math.max(R, G, B),
    min = Math.min(R, G, B)
  const l = (maks + min) / 2
  if (maks === min) return [0, 0, l * 100]
  const d = maks - min
  const s = l > 0.5 ? d / (2 - maks - min) : d / (maks + min)
  let h = 0
  if (maks === R) h = ((G - B) / d + (G < B ? 6 : 0)) / 6
  else if (maks === G) h = ((B - R) / d + 2) / 6
  else h = ((R - G) / d + 4) / 6
  return [h * 360, s * 100, l * 100]
}

function hslRGB(h: number, s: number, l: number): [number, number, number] {
  const H = ((h % 360) + 360) % 360 / 360,
    S = s / 100,
    L = l / 100
  if (S === 0) {
    const v = Math.round(L * 255)
    return [v, v, v]
  }
  const q = L < 0.5 ? L * (1 + S) : L + S - L * S
  const p = 2 * L - q
  const kanal = (t: number) => {
    if (t < 0) t += 1
    if (t > 1) t -= 1
    if (t < 1 / 6) return p + (q - p) * 6 * t
    if (t < 1 / 2) return q
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6
    return p
  }
  return [
    Math.round(kanal(H + 1 / 3) * 255),
    Math.round(kanal(H) * 255),
    Math.round(kanal(H - 1 / 3) * 255),
  ]
}
