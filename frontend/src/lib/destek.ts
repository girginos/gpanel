// Telegram destek botu — panel uçları (internal/destek/ajan/handlers.go).
//
// 🔴 EŞLEŞTİRME BU PANELDE YAPILMAZ: sunucu sahibi müşteri portalında (app.girginos.io)
// sunucuyu seçer, aydınlatmayı kabul eder ve kodu oradan alır (panel kapalıyken de çalışsın
// diye). Panelde yalnız durum, kök şalteri, bağlantı listesi ve kaldırma vardır; hepsi YALNIZ kök.
import { api } from '@/lib/api'

export type Duzey = 'kapali' | 'okuma' | 'onarim' | 'tam'
export type DestekRol = 'musteri' | 'bayi' | 'kok'
export type BaglantiDurumu = 'bekliyor' | 'aktif' | 'iptal'

export interface MerkezDurumu {
  durum: 'kapali' | 'yapilandirma_eksik' | 'baglaniyor' | 'bagli' | 'hata' | 'ajan_yok'
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

// Destek ajanı panelden AYRI hizmettir (girginospanel-destek-ajan); canlılığı DB nabzından gelir.
export interface AjanDurumu {
  canli: boolean
  yas_sn: number
  pid: number
  surum: string
  kopru: '' | 'tamam' | 'yok'
}

export interface DestekDurum {
  rol: DestekRol
  modul_acik: boolean
  tavan: Duzey
  merkez_bagli: boolean
  ayar?: DestekAyar
  baglanti?: MerkezDurumu
  ajan?: AjanDurumu
  eslestirme_url: string
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
}

export async function destekDurum(): Promise<DestekDurum> {
  return (await api.get<DestekDurum>('/destek/durum')).data
}

export async function destekAyarKaydet(ayar: DestekAyar): Promise<void> {
  await api.put('/destek/ayar', ayar)
}

export async function destekBaglantilar(): Promise<DestekBaglantisi[]> {
  return (await api.get<{ baglantilar: DestekBaglantisi[] }>('/destek/baglantilar')).data.baglantilar || []
}

export async function destekIptal(kimlik: string): Promise<void> {
  await api.post(`/destek/baglantilar/${encodeURIComponent(kimlik)}/iptal`)
}
