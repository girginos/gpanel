import { create } from 'zustand'

export type Kullanici = {
  id: number
  adi: string
  rol: 'admin' | 'reseller' | 'user'
  ad_soyad?: string
}

type AuthState = {
  token: string | null
  kullanici: Kullanici | null
  bitis: number | null
  giris: (token: string, kullanici: Kullanici, bitis: number) => void
  girisMusteri: (token: string, bitis: number, domainID: number, alanAdi: string, kullaniciAdi: string) => void
  guncelleAd: (adSoyad: string) => void
  cikis: () => void
  hidrate: () => void
}

const KEY_TOKEN = 'gosp.token'
const KEY_USER  = 'gosp.user'
const KEY_EXP   = 'gosp.exp'

const KEY_MUSTERI      = 'girginospanel.musteri'
export const KEY_MUSTERI_DOM  = 'girginospanel.musteri.domain_id'
const KEY_MUSTERI_ALAN = 'girginospanel.musteri.alan_adi'

// 🔴 SON OTURUM TİPİ — cikis()/musteriBayrakSil() bunu SİLMEZ. Oturum düşünce
// (süre doldu → ilkDurum, ya da 401 → cikis) müşteri bayrakları GuardedRoute
// çalışmadan önce siliniyor; müşteriyi yönetici formuna (/giris) değil kendi giriş
// formuna (/cp/giris) göndermek için tek iz budur. Her girişte üzerine yazılır.
const KEY_SON_OTURUM = 'girginospanel.son_oturum'
type OturumTipi = 'musteri' | 'yonetim'

function sonOturumYaz(tip: OturumTipi) {
  try { localStorage.setItem(KEY_SON_OTURUM, tip) } catch { /* yoksay */ }
}

// Oturum bitince gidilecek giriş formu. Kapı modu açıkken KapiGiris zaten doğru
// forma çevirir; bu, kapı modu kapalı (karma) kurulumda müşterinin yönetici
// formunu görmemesi içindir.
export function girisYolu(): '/giris' | '/cp/giris' {
  try {
    return localStorage.getItem(KEY_SON_OTURUM) === 'musteri' ? '/cp/giris' : '/giris'
  } catch {
    return '/giris'
  }
}

function musteriBayrakSil() {
  // Bu sürümden ÖNCE açılmış müşteri oturumunda son-oturum izi yok: bayrak
  // silinmeden önce ize çevrilir (süre dolunca yine /cp/giris'e gidilsin).
  if (localStorage.getItem(KEY_MUSTERI) === '1') sonOturumYaz('musteri')
  localStorage.removeItem(KEY_MUSTERI)
  localStorage.removeItem(KEY_MUSTERI_DOM)
  localStorage.removeItem(KEY_MUSTERI_ALAN)
}

function ilkDurum() {
  if (typeof window === 'undefined') {
    return { token: null as string | null, kullanici: null as Kullanici | null, bitis: null as number | null }
  }
  const t = localStorage.getItem(KEY_TOKEN)
  const u = localStorage.getItem(KEY_USER)
  const e = localStorage.getItem(KEY_EXP)
  if (!t || !u || !e) {
    musteriBayrakSil()
    return { token: null, kullanici: null, bitis: null }
  }
  const exp = Number(e)
  if (!Number.isFinite(exp) || exp * 1000 < Date.now()) {
    localStorage.removeItem(KEY_TOKEN)
    localStorage.removeItem(KEY_USER)
    localStorage.removeItem(KEY_EXP)
    musteriBayrakSil()
    return { token: null, kullanici: null, bitis: null }
  }
  try {
    return { token: t, kullanici: JSON.parse(u) as Kullanici, bitis: exp }
  } catch {
    return { token: null, kullanici: null, bitis: null }
  }
}

export const useAuth = create<AuthState>((set, get) => ({
  ...ilkDurum(),
  giris: (token, kullanici, bitis) => {
    localStorage.setItem(KEY_TOKEN, token)
    localStorage.setItem(KEY_USER, JSON.stringify(kullanici))
    localStorage.setItem(KEY_EXP, String(bitis))
    musteriBayrakSil()
    sonOturumYaz('yonetim')
    set({ token, kullanici, bitis })
  },
  girisMusteri: (token, bitis, domainID, alanAdi, kullaniciAdi) => {
    const synth: Kullanici = { id: 0, adi: kullaniciAdi, rol: 'user', ad_soyad: alanAdi }
    localStorage.setItem(KEY_TOKEN, token)
    localStorage.setItem(KEY_USER, JSON.stringify(synth))
    localStorage.setItem(KEY_EXP, String(bitis))
    localStorage.setItem(KEY_MUSTERI, '1')
    localStorage.setItem(KEY_MUSTERI_DOM, String(domainID))
    localStorage.setItem(KEY_MUSTERI_ALAN, alanAdi)
    sonOturumYaz('musteri')
    set({ token, kullanici: synth, bitis })
  },
  guncelleAd: (adSoyad) => set((s) => {
    if (!s.kullanici) return s
    const k = { ...s.kullanici, ad_soyad: adSoyad }
    try { localStorage.setItem(KEY_USER, JSON.stringify(k)) } catch { /* yoksay */ }
    return { kullanici: k }
  }),
  cikis: () => {
    if (get().kullanici?.rol === 'user') sonOturumYaz('musteri')
    localStorage.removeItem(KEY_TOKEN)
    localStorage.removeItem(KEY_USER)
    localStorage.removeItem(KEY_EXP)
    musteriBayrakSil()
    set({ token: null, kullanici: null, bitis: null })
  },
  hidrate: () => {
    /* ilkDurum() ilk render'da yapıyor */
  },
}))
