// Telegram destek botu — panel uçları (internal/destek/ajan/handlers.go).
//
// 🔴 Eşleştirme kodu YALNIZ üretim yanıtında gelir; bileşen durumunda tutulur —
// store'a, localStorage'a, URL'ye YAZILMAZ (Telegram bağlantısı t.me/<bot>?start=<kod>
// yalnız kullanıcının tıklaması için çizilir).
import { api } from '@/lib/api'

export type Duzey = 'kapali' | 'okuma' | 'onarim' | 'tam'
export type DestekRol = 'musteri' | 'bayi' | 'kok'
export type BaglantiDurumu = 'bekliyor' | 'aktif' | 'iptal'

export interface Aydinlatma {
  surum: string
  taslak: boolean
  tr: string
  en: string
}

export interface MerkezDurumu {
  durum: 'kapali' | 'yapilandirma_eksik' | 'baglaniyor' | 'bagli' | 'hata'
  tasima: string
  son_hata: string
  son_baglanti: number
  sunucu_kimligi: string
  cihaz_kimligi: string
}

export interface DestekAyar {
  modul_duzey?: Duzey
  bayi_duzey?: Duzey
  musteri_duzey?: Duzey
}

export interface DestekDurum {
  rol: DestekRol
  adina: boolean
  modul_acik: boolean
  tavan: Duzey
  bot_kullanici_adi: string
  aydinlatma: Aydinlatma
  sure_secenekleri: number[]
  en_uzun_gun: number
  varsayilan_gun: number
  merkez_bagli: boolean
  kod_omru_sn: number
  ayar?: DestekAyar
  baglanti?: MerkezDurumu
  musteri_ust_sinir?: Duzey
}

export interface EslesmeKodu {
  kod: string
  gorunen: string
  bitis: number
  gecerlilik: number
  merkez_bagli: boolean
  baglanti_url: string
  qr: string
}

export interface DestekBaglantisi {
  kimlik: string
  rol: DestekRol
  tg_kullanici_adi: string
  tg_kisa: string
  durum: BaglantiDurumu
  duzey: Duzey
  etkin_duzey: Duzey
  sure_gun: number
  bitis: number
  hedef: string
  olusturan: string
  onaylayan: string
  onay_at: number
  aydinlatma_surum: string
  son_kullanim: number
  iptal_nedeni: string
  iptal_eden: string
  iptal_at: number
  olusturma_at: number
  benim: boolean
}

export async function destekDurum(): Promise<DestekDurum> {
  return (await api.get<DestekDurum>('/destek/durum')).data
}

export async function destekAyarKaydet(ayar: DestekAyar): Promise<void> {
  await api.put('/destek/ayar', ayar)
}

export async function destekEslestir(duzey: Duzey, sureGun: number): Promise<EslesmeKodu> {
  return (await api.post<EslesmeKodu>('/destek/eslestir', { duzey, sure_gun: sureGun })).data
}

export async function destekBaglantilar(): Promise<DestekBaglantisi[]> {
  return (await api.get<{ baglantilar: DestekBaglantisi[] }>('/destek/baglantilar')).data.baglantilar || []
}

export async function destekOnayla(kimlik: string, aydinlatmaSurum: string): Promise<void> {
  await api.post(`/destek/baglantilar/${encodeURIComponent(kimlik)}/onayla`, { aydinlatma_surum: aydinlatmaSurum, kabul: true })
}

export async function destekIptal(kimlik: string): Promise<void> {
  await api.post(`/destek/baglantilar/${encodeURIComponent(kimlik)}/iptal`)
}
