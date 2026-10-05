// WordPress Toolkit — kurulum hedefi (ana / ek / alt alan adı) saf yardımcıları.
// Sayfa, süzgeç, kurulum seçici ve derin bağlantı (?hedef=) AYNI anahtar biçimini kullanır;
// API tabanı hedef türünden türetilir (ek alan adı ve alt alan adı kendi rotasından).
// Saf (React/DOM'suz) → esbuild + node ile birim testte gerçekten çalıştırılır.

export type WpTur = 'ana' | 'ek' | 'alt'
export type WpHedefKimlik = { tur?: WpTur; ek_alan_id?: number; alt_alan_id?: number }

// 'ana' | 'ek:<id>' | 'alt:<id>'
export const hedefAnahtar = (h: WpHedefKimlik): string =>
  h.tur === 'ek' ? `ek:${h.ek_alan_id}` : h.tur === 'alt' ? `alt:${h.alt_alan_id}` : 'ana'

const reHedef = /^(ana|ek:\d+|alt:\d+)$/

// Derin bağlantı: alt alan adı rotası :sid, aksi halde ?hedef=; biçim dışı değer yok sayılır.
export function derinBaglanti(sid: string | undefined, hedefParam: string | null | undefined): string {
  if (sid && /^\d+$/.test(sid)) return `alt:${sid}`
  const p = hedefParam || ''
  return reHedef.test(p) ? p : ''
}

// Hedefe göre API tabanı: ana /domains/{id}/wordpress; ek ve alt alan adı kendi rotası.
export const hedefBase = (id: string, h: WpHedefKimlik): string =>
  h.tur === 'ek' ? `/domains/${id}/ek-alan-adlari/${h.ek_alan_id}/wordpress`
    : h.tur === 'alt' ? `/domains/${id}/subdomain/${h.alt_alan_id}/wordpress`
      : `/domains/${id}/wordpress`

// SSL ekleme sayfası: ana alan adı SSL sayfası; ek ve alt alan adı kendi yönetim sayfası.
export const hedefSslYol = (id: string, h: WpHedefKimlik): string =>
  h.tur === 'ek' ? `/abonelikler/${id}/ek-alanlar/${h.ek_alan_id}`
    : h.tur === 'alt' ? `/abonelikler/${id}/subdomainler/${h.alt_alan_id}`
      : `/abonelikler/${id}/ssl`

// Site adresi önizlemesi: seçilen alan adının adresi + alt dizin (başındaki / atılır).
export const urlOnizleme = (taban: string, altDizin: string): string => {
  const d = altDizin.trim().replace(/^\/+/, '')
  return d ? `${taban}/${d}` : taban
}
