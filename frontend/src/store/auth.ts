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
  girisAdina: (token: string, bitis: number, domainID: number, alanAdi: string, kullaniciAdi: string, bilgi: AdinaBilgi) => void
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

// ── MÜŞTERİ ADINA OTURUM (bayi/yönetici "panele gir", bkz. lib/adina.ts) ─────────
// 🔴 SEKMEYE ÖZEL: adına-oturum sessionStorage'da tutulur. Kapı modu kapalıyken bayi
// paneli ile müşteri paneli AYNI kökendedir; oturum localStorage'a yazılsaydı bayinin
// kendi oturumunu (ve öteki sekmelerini) ezerdi. KEY_ADINA varken bu sekmenin tüm
// oturum anahtarları (token/kullanıcı/bitiş/müşteri bayrakları) sessionStorage'dan
// okunur/yazılır; localStorage'daki bayi oturumuna DOKUNULMAZ. Çıkışta sekme
// bilgi.don adresine (bayi paneli) döner ve oradaki oturum kendiliğinden geri gelir.
export const KEY_ADINA = 'gosp.adina'
// Dönüş adresi ayrı tutulur: oturum süresi dolup anahtarlar silinse de sekme
// giriş formuna değil bayi paneline dönebilsin (GuardedRoute).
const KEY_ADINA_DON = 'gosp.adina.don'

export type AdinaBilgi = {
  aktor: string
  rol: 'admin' | 'reseller'
  alan: string
  domainID: number
  don: string
}

function adinaModu(): boolean {
  try { return sessionStorage.getItem(KEY_ADINA) !== null } catch { return false }
}

// Bu sekmenin oturum deposu: adına-oturumda sessionStorage, aksi hâlde localStorage.
export function oturumDeposu(): Storage {
  return adinaModu() ? sessionStorage : localStorage
}

export function adinaBilgi(): AdinaBilgi | null {
  try {
    const v = sessionStorage.getItem(KEY_ADINA)
    if (!v) return null
    const b = JSON.parse(v) as AdinaBilgi
    return b && typeof b.alan === 'string' && typeof b.don === 'string' ? b : null
  } catch {
    return null
  }
}

// Süresi dolmuş/kapatılmış adına-oturumdan kalan dönüş adresi (tek kullanımlık).
export function adinaDonusAl(): string | null {
  try {
    const d = sessionStorage.getItem(KEY_ADINA_DON)
    sessionStorage.removeItem(KEY_ADINA_DON)
    return d
  } catch {
    return null
  }
}

function adinaTemizle(donusuSakla: boolean) {
  try {
    const b = adinaBilgi()
    for (const k of [KEY_TOKEN, KEY_USER, KEY_EXP, KEY_MUSTERI, KEY_MUSTERI_DOM, KEY_MUSTERI_ALAN, KEY_ADINA]) sessionStorage.removeItem(k)
    if (donusuSakla && b?.don) sessionStorage.setItem(KEY_ADINA_DON, b.don)
    else sessionStorage.removeItem(KEY_ADINA_DON)
  } catch { /* yoksay */ }
}

// 🔴 SON OTURUM TİPİ — cikis()/musteriBayrakSil() bunu SİLMEZ. Oturum düşünce
// (süre doldu → ilkDurum, ya da 401 → cikis) müşteri bayrakları GuardedRoute
// çalışmadan önce siliniyor; müşteriyi yönetici formuna (/giris) değil kendi giriş
// formuna (/cp/giris) göndermek için tek iz budur. Her girişte üzerine yazılır.
const KEY_SON_OTURUM = 'girginospanel.son_oturum'
type OturumTipi = 'musteri' | 'yonetim'

function sonOturumYaz(tip: OturumTipi) {
  if (adinaModu()) return
  try { localStorage.setItem(KEY_SON_OTURUM, tip) } catch { /* yoksay */ }
}

// Bu tarayıcıda son açılan oturum müşteri oturumu muydu? Tek başına giriş formunu
// SEÇMEZ: karar hedef yolla birlikte verilir (lib/rol.ts girisYolu) — yoksa aynı
// tarayıcıdaki yönetici/bayi, çıkışı olmayan müşteri formuna kilitlenirdi (MFE-1).
export function sonOturumMusteri(): boolean {
  try {
    return localStorage.getItem(KEY_SON_OTURUM) === 'musteri'
  } catch {
    return false
  }
}

// Depodaki (localStorage) oturum bu sekmenin bellekteki oturumundan FARKLI mı? Aynı
// kökende başka bir sekme bu sekmeden sonra giriş yaptıysa depo artık onundur.
export function depoBaskaOturumda(): boolean {
  let t: string | null = null
  try { t = oturumDeposu().getItem(KEY_TOKEN) } catch { return false }
  return t !== null && t !== useAuth.getState().token
}

function musteriBayrakSil() {
  // Bu sürümden ÖNCE açılmış müşteri oturumunda son-oturum izi yok: bayrak
  // silinmeden önce ize çevrilir (süre dolunca yine /cp/giris'e gidilsin).
  if (oturumDeposu().getItem(KEY_MUSTERI) === '1') sonOturumYaz('musteri')
  oturumDeposu().removeItem(KEY_MUSTERI)
  oturumDeposu().removeItem(KEY_MUSTERI_DOM)
  oturumDeposu().removeItem(KEY_MUSTERI_ALAN)
}

function ilkDurum() {
  if (typeof window === 'undefined') {
    return { token: null as string | null, kullanici: null as Kullanici | null, bitis: null as number | null }
  }
  const t = oturumDeposu().getItem(KEY_TOKEN)
  const u = oturumDeposu().getItem(KEY_USER)
  const e = oturumDeposu().getItem(KEY_EXP)
  if (!t || !u || !e) {
    musteriBayrakSil()
    if (adinaModu()) adinaTemizle(true)
    return { token: null, kullanici: null, bitis: null }
  }
  const exp = Number(e)
  if (!Number.isFinite(exp) || exp * 1000 < Date.now()) {
    oturumDeposu().removeItem(KEY_TOKEN)
    oturumDeposu().removeItem(KEY_USER)
    oturumDeposu().removeItem(KEY_EXP)
    musteriBayrakSil()
    // Adına-oturumun süresi doldu: sekme bayi paneline döner (GuardedRoute, KEY_ADINA_DON).
    if (adinaModu()) adinaTemizle(true)
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
    // Bu sekmede yeni (yönetici/bayi) giriş: varsa adına-modu kapat ki oturum
    // olağan depoya (localStorage) yazılsın.
    adinaTemizle(false)
    oturumDeposu().setItem(KEY_TOKEN, token)
    oturumDeposu().setItem(KEY_USER, JSON.stringify(kullanici))
    oturumDeposu().setItem(KEY_EXP, String(bitis))
    musteriBayrakSil()
    sonOturumYaz('yonetim')
    set({ token, kullanici, bitis })
  },
  girisMusteri: (token, bitis, domainID, alanAdi, kullaniciAdi) => {
    const synth: Kullanici = { id: 0, adi: kullaniciAdi, rol: 'user', ad_soyad: alanAdi }
    oturumDeposu().setItem(KEY_TOKEN, token)
    oturumDeposu().setItem(KEY_USER, JSON.stringify(synth))
    oturumDeposu().setItem(KEY_EXP, String(bitis))
    oturumDeposu().setItem(KEY_MUSTERI, '1')
    oturumDeposu().setItem(KEY_MUSTERI_DOM, String(domainID))
    oturumDeposu().setItem(KEY_MUSTERI_ALAN, alanAdi)
    sonOturumYaz('musteri')
    set({ token, kullanici: synth, bitis })
  },
  guncelleAd: (adSoyad) => set((s) => {
    if (!s.kullanici) return s
    const k = { ...s.kullanici, ad_soyad: adSoyad }
    try { oturumDeposu().setItem(KEY_USER, JSON.stringify(k)) } catch { /* yoksay */ }
    return { kullanici: k }
  }),
  girisAdina: (token, bitis, domainID, alanAdi, kullaniciAdi, bilgi) => {
    // Önce sekme adına-moda geçer: ardından girisMusteri'nin TÜM yazımları
    // sessionStorage'a gider (localStorage'daki bayi oturumu korunur).
    try { sessionStorage.setItem(KEY_ADINA, JSON.stringify(bilgi)); sessionStorage.removeItem(KEY_ADINA_DON) } catch { /* yoksay */ }
    get().girisMusteri(token, bitis, domainID, alanAdi, kullaniciAdi)
  },
  cikis: () => {
    // Adına-oturum: yalnız bu sekmenin sessionStorage kopyası silinir; bayi oturumuna
    // (localStorage) dokunulmaz. Dönüş (bayi paneli) çağıranın işidir (lib/adina.ts).
    if (adinaModu()) {
      adinaTemizle(true)
      set({ token: null, kullanici: null, bitis: null })
      return
    }
    // 🔴 Depo başka bir sekmenin oturumunu taşıyorsa ona DOKUNULMAZ: bu sekmenin çıkışı
    // yalnız kendi (bellekteki) oturumunu kapatır. Aksi hâlde bayat bir müşteri sekmesinin
    // çıkışı (401, rol kapısı, Çıkış) sonradan giriş yapan yöneticinin oturumunu siler ve
    // son-oturum izini 'musteri'ye çevirirdi (MFE-2).
    if (!depoBaskaOturumda()) {
      if (get().kullanici?.rol === 'user') sonOturumYaz('musteri')
      oturumDeposu().removeItem(KEY_TOKEN)
      oturumDeposu().removeItem(KEY_USER)
      oturumDeposu().removeItem(KEY_EXP)
      musteriBayrakSil()
    }
    set({ token: null, kullanici: null, bitis: null })
  },
  // Belleği depodaki oturumla yeniden eşitler (ilk durum açılışta ilkDurum() ile okunur).
  hidrate: () => set(ilkDurum()),
}))
