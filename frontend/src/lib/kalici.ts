// Süreli yerel saklama — kullanıcı sekmeyi/sayfayı kapatsa bile form verisi ve
// açık sekme belli bir süre korunur, sonra kendiliğinden düşer.
//
// 🔴 PAROLA SAKLANMAZ: localStorage kalıcıdır, aynı origin'deki her script (ve
// bir XSS) okuyabilir. Parolayı buraya yazmak, kullanıcı işini bitirdikten çok
// sonra bile diskte duran bir sır bırakırdı. Çağıran taraf parola alanlarını
// AYIKLAYARAK verir; bu dosya da anahtar adında parola geçen alanları son bir
// savunma olarak temizler.

const ONEK = 'gosp.kalici.'

interface Paket<T> {
  v: T
  bitis: number // epoch ms
}

// parolaAyikla — 'parola'/'password'/'sifre' içeren alanları derinlemesine siler.
function parolaAyikla<T>(veri: T): T {
  if (veri === null || typeof veri !== 'object') return veri
  if (Array.isArray(veri)) return veri.map(parolaAyikla) as unknown as T
  const cikti: Record<string, unknown> = {}
  for (const [k, d] of Object.entries(veri as Record<string, unknown>)) {
    if (/parola|password|sifre|secret|token/i.test(k)) continue
    cikti[k] = parolaAyikla(d)
  }
  return cikti as T
}

export function kaydet<T>(anahtar: string, veri: T, ttlMs: number): void {
  try {
    const p: Paket<T> = { v: parolaAyikla(veri), bitis: Date.now() + ttlMs }
    localStorage.setItem(ONEK + anahtar, JSON.stringify(p))
  } catch {
    /* kota dolu / özel mod — saklama kritik değil, sessiz geç */
  }
}

export function oku<T>(anahtar: string): T | null {
  try {
    const ham = localStorage.getItem(ONEK + anahtar)
    if (!ham) return null
    const p = JSON.parse(ham) as Paket<T>
    if (!p || typeof p.bitis !== 'number' || Date.now() > p.bitis) {
      localStorage.removeItem(ONEK + anahtar)
      return null
    }
    return p.v
  } catch {
    return null
  }
}

export function sil(anahtar: string): void {
  try { localStorage.removeItem(ONEK + anahtar) } catch { /* yoksay */ }
}

// suresiGecenleriTemizle — açılışta biriken eski kayıtları süpürür (kota şişmesin).
export function suresiGecenleriTemizle(): void {
  try {
    const simdi = Date.now()
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const k = localStorage.key(i)
      if (!k || !k.startsWith(ONEK)) continue
      try {
        const p = JSON.parse(localStorage.getItem(k) || '{}') as Paket<unknown>
        if (typeof p.bitis !== 'number' || simdi > p.bitis) localStorage.removeItem(k)
      } catch { localStorage.removeItem(k) }
    }
  } catch { /* yoksay */ }
}

// Varsayılan saklama süresi — 24 saat.
export const GUN = 24 * 60 * 60 * 1000
