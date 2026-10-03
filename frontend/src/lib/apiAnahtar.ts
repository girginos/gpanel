// API anahtarları (D4) — tipler ve çağrılar. Sözleşme: internal/apianahtar/uc.
//
// 🔴 Tam anahtar (gosp_…) YALNIZ olustur/yenile yanıtında bir kez gelir; çağıran
// onu yalnız bileşen durumunda tutar — store'a, localStorage'a, URL'ye YAZILMAZ.
import { api } from '@/lib/api'

export type AnahtarDurum = 'etkin' | 'gecis' | 'suresi_doldu' | 'iptal'

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
  sahip_id: number
  sahip_rol: 'admin' | 'reseller'
  sahip_ad?: string
}

export interface IzinGrubu {
  ad: string
  etiket: string
  aciklama: string
  roller: string[]
  yalniz_oku: boolean
}

export interface Katalog {
  gruplar: IzinGrubu[]
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

export async function anahtarListe(bayiler = false): Promise<ApiAnahtari[]> {
  const r = await api.get<{ anahtarlar: ApiAnahtari[] }>('/api-anahtarlari', { params: bayiler ? { sahip: 'bayiler' } : undefined })
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

export async function anahtarIptal(id: number): Promise<void> {
  await api.delete(`/api-anahtarlari/${id}`)
}

// Sunucunun makine okunur hata kodu (ör. iki_fa_gerekli, parola_hatali).
export function hataKodu(e: unknown): string | undefined {
  const d = (e as { response?: { data?: { kod?: unknown } } })?.response?.data
  return typeof d?.kod === 'string' ? d.kod : undefined
}
