// API anahtarları (D4) — tipler ve çağrılar. Sözleşme: internal/apianahtar/uc.
//
// 🔴 Tam anahtar (gosp_…) YALNIZ olustur/yenile yanıtında bir kez gelir; çağıran
// onu yalnız bileşen durumunda tutar — store'a, localStorage'a, URL'ye YAZILMAZ.
import { api } from '@/lib/api'

// askida: anahtar geçerli ama sahibi bayi askıda (kayıt değişmez; askıdan alınınca
// kendiliğinden yeniden çalışır).
export type AnahtarDurum = 'etkin' | 'gecis' | 'askida' | 'suresi_doldu' | 'iptal'

export interface ApiAnahtari {
  id: number
  ad: string
  gosterim: string
  onek: string
  izinler: string[]
  izinli_ipler: string[]
  hiz_dk: number
  son_kullanma_at: number
  olusturma_at: number
  son_kullanim_at: number
  son_kullanim_ip: string
  iptal_at: number
  durum: AnahtarDurum
  iptal_neden: string
  iptal_yonetim: boolean
  sahip_askida: boolean
}

export interface IzinGrubu {
  ad: string
  etiket: string
  aciklama: string
  roller: string[]
  yalniz_oku: boolean
  // Tek düzeyli izin (yalnız yazma; parola değiştirme): arayüz Yok / İzin ver gösterir.
  yalniz_yaz?: boolean
}

export interface Katalog {
  gruplar: IzinGrubu[]
  // Authorization: gosp <kullanici_adi>:<anahtar> — anahtar yalnız bu adla geçer.
  kullanici_adi?: string
  kimlik_sema?: string
  ip_zorunlu: boolean
  varsayilan_gun: number
  maks_gun: number
  varsayilan_hiz: number
  min_hiz: number
  maks_hiz: number
  maks_ip: number
}

export interface OlusturGovde {
  ad: string
  izinler: string[]
  izinli_ipler: string[]
  son_kullanma_gun: number
  hiz_dk: number
  parola: string
  kod?: string
}

export interface TekKezYanit {
  anahtar: string
  anahtar_kaydi: ApiAnahtari
}

// Yalnız çağıranın kendi anahtarları (sunucu oturumdan süzer; sahip parametresi yok).
export async function anahtarListe(): Promise<ApiAnahtari[]> {
  const r = await api.get<{ anahtarlar: ApiAnahtari[] }>('/api-anahtarlari')
  return r.data.anahtarlar || []
}

export async function anahtarKatalog(): Promise<Katalog> {
  return (await api.get<Katalog>('/api-anahtarlari/izin-gruplari')).data
}

export async function anahtarOlustur(g: OlusturGovde): Promise<TekKezYanit> {
  return (await api.post<TekKezYanit>('/api-anahtarlari', g)).data
}

export async function anahtarYenile(id: number, parola: string, kod: string, gecis_saat: number): Promise<TekKezYanit> {
  return (await api.post<TekKezYanit>(`/api-anahtarlari/${id}/yenile`, { parola, kod, gecis_saat })).data
}

// İptal: yumuşak (satır ve iz kalır).
export async function anahtarIptal(id: number): Promise<void> {
  await api.post(`/api-anahtarlari/${id}/iptal`)
}

// Kalıcı silme: satır gider, denetim geçmişi kalır. Etkin anahtarda onayAd =
// anahtarın adı zorunlu ("iptal et ve sil"); iptal edilmiş/süresi dolmuşta gerekmez.
export async function anahtarSil(id: number, onayAd?: string): Promise<void> {
  await api.delete(`/api-anahtarlari/${id}`, onayAd ? { data: { onay_ad: onayAd } } : undefined)
}

// Sunucunun makine okunur hata kodu (ör. iki_fa_gerekli, parola_hatali).
export function hataKodu(e: unknown): string | undefined {
  const d = (e as { response?: { data?: { kod?: unknown } } })?.response?.data
  return typeof d?.kod === 'string' ? d.kod : undefined
}
