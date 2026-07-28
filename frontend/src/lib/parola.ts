// Panel genelinde ortak parola üreteci.
//
// 🔴 Math.random() KULLANILMAZ — kriptografik olarak zayıf; üretilen parola
// tahmin edilebilir olur. crypto.getRandomValues + reddetme örneklemesi
// (rejection sampling) ile modulo yanlılığı da engellenir.

// Karıştırılması kolay karakterler (0/O, 1/l/I) DIŞARIDA: parola çoğu zaman
// panelden okunup elle iletiliyor.
const KUCUK = 'abcdefghijkmnopqrstuvwxyz'
const BUYUK = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
const RAKAM = '23456789'
const SEMBOL = '!@#$%*?-_+='
const TUMU = KUCUK + BUYUK + RAKAM + SEMBOL

/** rastgeleSayi: [0, ust) aralığında yansız kripto-rastgele tamsayı. */
function rastgeleSayi(ust: number): number {
  const max = 256 - (256 % ust) // modulo yanlılığını kes
  const buf = new Uint8Array(1)
  for (;;) {
    crypto.getRandomValues(buf)
    if (buf[0] < max) return buf[0] % ust
  }
}

function rastgeleKarakter(alfabe: string): string {
  return alfabe[rastgeleSayi(alfabe.length)]
}

/** parolaUret: her sınıftan en az bir karakter içeren güçlü parola üretir. */
export function parolaUret(uzunluk = 16): string {
  const n = Math.max(12, Math.min(64, uzunluk))
  const zorunlu = [rastgeleKarakter(KUCUK), rastgeleKarakter(BUYUK), rastgeleKarakter(RAKAM), rastgeleKarakter(SEMBOL)]
  const geri = Array.from({ length: n - zorunlu.length }, () => rastgeleKarakter(TUMU))
  const hepsi = [...zorunlu, ...geri]
  // Fisher-Yates (kripto rastgelelikle) — zorunlu karakterler hep başta olmasın.
  for (let i = hepsi.length - 1; i > 0; i--) {
    const j = rastgeleSayi(i + 1)
    ;[hepsi[i], hepsi[j]] = [hepsi[j], hepsi[i]]
  }
  return hepsi.join('')
}

/** panoyaKopyala: HTTPS/localhost'ta Clipboard API, değilse eski yönteme düşer. */
export async function panoyaKopyala(metin: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(metin)
      return true
    }
  } catch { /* alttaki yedege dus */ }
  try {
    const ta = document.createElement('textarea')
    ta.value = metin
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
