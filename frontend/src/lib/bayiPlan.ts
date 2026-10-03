// Bayinin kendi hosting planları — paket sınırı ipuçları + varsayılan plan seçimi.
//
// Sunucu: GET /plans/bayi-sinir (AdminVeyaReseller). Bayi için paket havuzu (disk /
// trafik), fazla satış, bugünkü taahhüt ve posta tavanları; herkes için bayinin
// YAZAMADIĞI kök alanları (tek kaynak: internal/efektiflimit Ozette=false). Kural
// sunucudadır (internal/plans/bayi_plan.go) — burası yalnız ipucu gösterir.
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

export type BayiSinir = {
  bayi: boolean
  kok_alanlar: string[]
  max_domain?: number
  max_disk_mb?: number
  max_trafik_mb?: number
  fazla_satis?: boolean
  hosting_sayisi?: number
  taahhut_disk_mb?: number
  taahhut_trafik_mb?: number
  mail_max_email?: number
  mail_saatlik_limit?: number
  mail_kutu_kota_mb?: number
}

/** GET /plans/bayi-sinir — hata/yükleniyor: null (form ipuçsuz çalışır, sunucu yine doğrular). */
export function useBayiSinir(yenile = 0): BayiSinir | null {
  const [s, setS] = useState<BayiSinir | null>(null)
  useEffect(() => {
    let iptal = false
    api.get<BayiSinir>('/plans/bayi-sinir')
      .then(r => { if (!iptal) setS(r.data) })
      .catch(() => { if (!iptal) setS(null) })
    return () => { iptal = true }
  }, [yenile])
  return s
}

/** Bayi bu alanı düzenleyebilir mi? (yönetici her alanı) */
export function alanAcik(s: BayiSinir | null, alan: string): boolean {
  if (!s || !s.bayi) return true
  return !s.kok_alanlar.includes(alan)
}

/**
 * Bayinin gönderdiği plan gövdesinden kökün iç alanlarını çıkarır (sunucu bayi
 * gövdesinde bu alanlardan birini görürse 403 döner — sessizce yok saymaz).
 * Yönetici için gövde aynen döner. Bayi oturumunda sınır bilgisi yüklenmemişse
 * null döner: çağıran kaydetmemeli (hangi alanların çıkarılacağı bilinmiyor).
 */
export function bayiPlanGovdesi<T extends object>(s: BayiSinir | null, bayiRol: boolean, govde: T): Partial<T> | null {
  if (!bayiRol && !s?.bayi) return govde
  if (!s) return null
  const out = { ...govde } as Record<string, unknown>
  for (const k of s.kok_alanlar) delete out[k]
  return out as Partial<T>
}

/** Havuz ipucu: havuz, taahhüt, kalan (MB). Havuz sınırsızsa null. */
export function havuzDurumu(s: BayiSinir | null, tur: 'disk' | 'trafik'): { havuz: number; taahhut: number; kalan: number; fazla: boolean } | null {
  if (!s || !s.bayi) return null
  const havuz = (tur === 'disk' ? s.max_disk_mb : s.max_trafik_mb) || 0
  if (havuz <= 0) return null
  const taahhut = (tur === 'disk' ? s.taahhut_disk_mb : s.taahhut_trafik_mb) || 0
  return { havuz, taahhut, kalan: Math.max(0, havuz - taahhut), fazla: !!s.fazla_satis }
}

/** MB → okunur (1024 MB = 1 GB). */
export function mbOkunur(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} GB`
  return `${mb.toLocaleString()} MB`
}

type SecilebilirPlan = { id: number; ad: string; varsayilan?: boolean; duzenlenebilir?: boolean }

/**
 * Yeni hosting formunun ön seçili planı: çağıranın KENDİ varsayılan planı (bayide
 * bayinin varsayılanı; kökte kökün varsayılanı) → herhangi bir varsayılan → "Başlangıç"
 * → ilk plan. Bayiye kökün paylaşılan planları özet döner (varsayılan işareti yok).
 */
export function varsayilanPlanSec<T extends SecilebilirPlan>(planlar: T[], baslangicAdi = 'Başlangıç'): T | undefined {
  return planlar.find(p => p.varsayilan && p.duzenlenebilir !== false)
    || planlar.find(p => p.varsayilan)
    || planlar.find(p => p.ad === baslangicAdi)
    || planlar[0]
}
