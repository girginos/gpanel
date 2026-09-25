// Canlı kiracı ölçümü (GET /domains/{id}/canli) — yalnız görünürken yoklanır.
//
// 🔴 KAPILAR:
//  - `etkin` false (Pano sekmesi açık değil) → hiç istek yok.
//  - document.hidden (tarayıcı sekmesi arka planda) → zamanlayıcı SİLİNİR, istek yok;
//    sekme öne gelince hemen bir örnek alınır ve 10 sn'lik düzen sürer.
//  - destek=false (kiracı cgroup'u yok) → yoklama DURUR (her 10 sn'de aynı "yok"u
//    sormanın anlamı yok); `yeniden()` bir kez daha sorar.
//  - Hiç örnek alınamadan hata ya da art arda 3 hata → yoklama durur, hata gösterilir
//    (uç yoksa/404 ise sonsuza dek 10 sn'de bir istek atılmaz).
// setInterval yerine zincirli setTimeout: yanıt gelmeden yeni istek gönderilmez.
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import type { CanliOrnek } from './tipler'

export const CANLI_ARALIK_MS = 10_000
// cpu null (ilk örnek ya da arka ucun 2 dk'dan eski tabanı attığı durum — delta yok) →
// bir kez kısa sürede ikinci örnek; art arda null'da düz 10 sn (sonsuz 3 sn döngüsü yok).
const HIZLI_TEKRAR_MS = 3_000
const AZAMI_NOKTA = 30 // 30 × 10 sn = son 5 dk
const AZAMI_ARDISIK_HATA = 3
const BOSLUK_SN = 35 // iki örnek arası bundan uzunsa grafikte boşluk (sekme arka plandaydı)

// cpu/bellekB null → grafikte boşluk (ölçüm yok ya da duraklatma aralığı).
export type CanliNokta = { t: number; cpu: number | null; bellekB: number | null }

export type CanliDurum = {
  durum: 'yukleniyor' | 'hazir' | 'destekyok' | 'hata'
  son: CanliOrnek | null
  noktalar: CanliNokta[]
  hata: string | null
}

export function useCanli(domainId: number, etkin: boolean) {
  const [st, setSt] = useState<CanliDurum>({ durum: 'yukleniyor', son: null, noktalar: [], hata: null })
  const [duraklatildi, setDuraklatildi] = useState(false)
  const [tur, setTur] = useState(0)

  useEffect(() => {
    if (!etkin) return
    let iptal = false
    let bitti = false
    let ucusta = false
    let zaman: number | undefined
    let ornekVar = false
    let ardisikHata = 0
    let sonHizli = false

    const temizle = () => { if (zaman !== undefined) window.clearTimeout(zaman); zaman = undefined }
    const planla = (ms: number) => {
      temizle()
      if (iptal || bitti || document.hidden) return
      zaman = window.setTimeout(() => { zaman = undefined; void cek() }, ms)
    }

    async function cek() {
      if (iptal || bitti || ucusta || document.hidden) return
      ucusta = true
      try {
        const { data } = await api.get<CanliOrnek>(`/domains/${domainId}/canli`)
        if (iptal) return
        ardisikHata = 0
        if (!data.destek) {
          bitti = true
          setSt((s) => ({ ...s, durum: 'destekyok', son: data, hata: null }))
          return
        }
        const hizli = data.cpu_yuzde == null && !sonHizli
        sonHizli = hizli
        ornekVar = true
        setSt((s) => {
          const onceki = s.noktalar[s.noktalar.length - 1]
          if (onceki && data.zaman <= onceki.t) return { ...s, durum: 'hazir', son: data, hata: null }
          const ek: CanliNokta[] = onceki && data.zaman - onceki.t > BOSLUK_SN
            ? [{ t: onceki.t + 1, cpu: null, bellekB: null }]
            : []
          const yeni: CanliNokta = { t: data.zaman, cpu: data.cpu_yuzde, bellekB: data.bellek_b }
          return { durum: 'hazir', son: data, hata: null, noktalar: [...s.noktalar, ...ek, yeni].slice(-AZAMI_NOKTA) }
        })
        planla(hizli ? HIZLI_TEKRAR_MS : CANLI_ARALIK_MS)
      } catch (e) {
        if (iptal) return
        ardisikHata++
        if (!ornekVar || ardisikHata >= AZAMI_ARDISIK_HATA) {
          bitti = true
          setSt((s) => ({ ...s, durum: 'hata', hata: apiHata(e) }))
          return
        }
        planla(CANLI_ARALIK_MS)
      } finally {
        ucusta = false
      }
    }

    const gorunurluk = () => {
      setDuraklatildi(document.hidden)
      if (document.hidden) temizle()
      else if (zaman === undefined) void cek()
    }
    document.addEventListener('visibilitychange', gorunurluk)
    setDuraklatildi(document.hidden)
    void cek()
    return () => {
      iptal = true
      temizle()
      document.removeEventListener('visibilitychange', gorunurluk)
    }
  }, [domainId, etkin, tur])

  const yeniden = () => {
    setSt((s) => ({ ...s, durum: s.noktalar.length ? 'hazir' : 'yukleniyor', hata: null }))
    setTur((t) => t + 1)
  }

  return { ...st, duraklatildi: duraklatildi || !etkin, yeniden }
}
