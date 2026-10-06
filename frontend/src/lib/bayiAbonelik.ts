// Bayinin abonelik (hosting) sahipliği — yalnız rota kapısı içindir (App.tsx RolKapisi).
//
// 🔴 Güvenlik sınırı ARKA UÇTUR: MusteriScope (resellerDomainSahibi) bayiye ait olmayan ya
// da var olmayan kimliğe 403 döner. Bu yardımcı yalnız arayüzün başka bir bayinin/kökün
// aboneliğinin sayfasını HİÇ açmaması (her uca 403 yağdırmaması) içindir. Karar bayinin
// KENDİ kapsamlı listesinden verilir (GET /domains — handler reseller_id ile süzer):
// yabancı ile var olmayan kimlik AYIRT EDİLEMEZ, dolayısıyla varlık bilgisi sızmaz.
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { useAuth } from '@/store/auth'

// Oturum (jeton) başına bilinen SAHİP kimlikler. Iskada liste yeniden çekilir (az önce
// açılan hosting önbellekte yoktur); olumsuz sonuç önbelleğe ALINMAZ.
let bilinen: { jeton: string; kimlikler: Set<number> } | null = null

function sahipBiliniyor(jeton: string | null, id: number): boolean {
  return jeton !== null && bilinen !== null && bilinen.jeton === jeton && bilinen.kimlikler.has(id)
}

// true = bayinin kendi aboneliği, false = değil, null = karar bekleniyor (liste yükleniyor)
// ya da yol abonelik yolu değil. Liste alınamazsa (ağ/sunucu hatası) true: arayüz bayiyi
// kendi aboneliğinden atmaz, yetki kararını arka uç verir.
export function useBayiAbonelikSahibi(id: number | null): boolean | null {
  const jeton = useAuth((s) => s.token)
  const hazir = id !== null && sahipBiliniyor(jeton, id)
  const [sonuc, setSonuc] = useState<{ jeton: string | null; id: number; sahip: boolean } | null>(null)
  useEffect(() => {
    if (id === null || hazir) return
    let iptal = false
    api.get<{ id: number }[]>('/domains')
      .then((r) => {
        const kimlikler = new Set((Array.isArray(r.data) ? r.data : []).map((d) => Number(d.id)))
        if (jeton !== null) bilinen = { jeton, kimlikler }
        if (!iptal) setSonuc({ jeton, id, sahip: kimlikler.has(id) })
      })
      .catch(() => { if (!iptal) setSonuc({ jeton, id, sahip: true }) })
    return () => { iptal = true }
  }, [id, hazir, jeton])
  if (id === null) return null
  if (hazir) return true
  return sonuc !== null && sonuc.id === id && sonuc.jeton === jeton ? sonuc.sahip : null
}
