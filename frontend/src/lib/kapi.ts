import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

// Rol kapıları (ADR-GP-001): panel yönetici / bayi / müşteri için ayrı portlarda
// dinleyebilir. Bu port hangi kapı? Sunucu söyler (GET /kapi — kimlik istemez);
// sayfa başına tek istek, modül önbelleği. Uç yoksa ya da ulaşılamazsa "karma" =
// bugünkü davranış: her giriş formu gösterilir, yetki kararını sunucu verir.
// Yönetici ve bayi kapıları AYNI yanıtı alır ("yonetim") — hangisinin yönetici
// portu olduğu buradan öğrenilemez.
export type KapiTip = 'musteri' | 'yonetim' | 'karma'

let bekleyen: Promise<KapiTip> | null = null

export function kapiTipi(): Promise<KapiTip> {
  if (!bekleyen) {
    bekleyen = api
      .get<{ tip?: string }>('/kapi', { timeout: 10_000 })
      .then((r): KapiTip => (r.data?.tip === 'musteri' || r.data?.tip === 'yonetim' ? r.data.tip : 'karma'))
      .catch((): KapiTip => 'karma')
  }
  return bekleyen
}

// useKapiTipi: yanıt gelene dek null.
export function useKapiTipi(): KapiTip | null {
  const [tip, setTip] = useState<KapiTip | null>(null)
  useEffect(() => {
    let iptal = false
    void kapiTipi().then((t) => { if (!iptal) setTip(t) })
    return () => { iptal = true }
  }, [])
  return tip
}
