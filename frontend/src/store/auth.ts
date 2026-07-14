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
const KEY_MUSTERI_DOM  = 'girginospanel.musteri.domain_id'
const KEY_MUSTERI_ALAN = 'girginospanel.musteri.alan_adi'

function musteriBayrakSil() {
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

export const useAuth = create<AuthState>((set) => ({
  ...ilkDurum(),
  giris: (token, kullanici, bitis) => {
    localStorage.setItem(KEY_TOKEN, token)
    localStorage.setItem(KEY_USER, JSON.stringify(kullanici))
    localStorage.setItem(KEY_EXP, String(bitis))
    musteriBayrakSil()
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
    set({ token, kullanici: synth, bitis })
  },
  guncelleAd: (adSoyad) => set((s) => {
    if (!s.kullanici) return s
    const k = { ...s.kullanici, ad_soyad: adSoyad }
    try { localStorage.setItem(KEY_USER, JSON.stringify(k)) } catch { /* yoksay */ }
    return { kullanici: k }
  }),
  cikis: () => {
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
