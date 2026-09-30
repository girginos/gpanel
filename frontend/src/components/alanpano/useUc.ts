// Tek uç çekimi: açılışta BİR kez, sonra yalnız elle `yenile()`.
//
// 🔴 Döngü YOK. Panonun uçlarının çoğu pahalı: /kaynak kota kapalıyken `du -sm`
// (0.7–2 sn), /istatistik 200 bin satıra kadar günlük tarar, /health 8 sn zaman
// aşımlı dış yoklama yapar. Bunları aralıkla çağırmak sunucuyu kiracı başına
// sürekli meşgul ederdi (bkz. musteri-bulgular MP-09).
//
// Davranış:
//  - Uçuştaki istek varken yeniden istenirse AYNI söz döner (çift tıklama, üç ayrı
//    'Yenile' düğmesi aynı anda basılsa bile tek istek gider).
//  - Yenileme sırasında eski veri korunur (yukleniyor + veri); hata olursa eski veri
//    kalır ve `hata` dolar — ekrandaki bayat veri "yeni" diye sunulmaz, uyarı çizilir.
//  - url değişirse (başka abonelik) eski yanıt yeni sayfaya yazılmaz (oturum iptali).
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { hataBildir } from '@/lib/hata'
import type { UcDurumu } from './tipler'

type Oturum = { iptal: boolean; istek: Promise<void> | null }

export type Uc<T> = UcDurumu<T> & { yenile: () => Promise<void> }

export function useUc<T>(url: string | null, hataBaglam?: string): Uc<T> {
  const [durum, setDurum] = useState<UcDurumu<T>>({ veri: null, yukleniyor: url != null, hata: null })
  const oturum = useRef<Oturum | null>(null)
  // Hata bağlamı (çevrilmiş metin) ref'te: dil değişince yenile() kimliği değişip uç
  // YENİDEN çekilmesin.
  const baglam = useRef(hataBaglam)
  useEffect(() => { baglam.current = hataBaglam })

  const yenile = useCallback((): Promise<void> => {
    const o = oturum.current
    if (!o || o.iptal || !url) return Promise.resolve()
    if (o.istek) return o.istek
    setDurum((d) => (d.yukleniyor ? d : { ...d, yukleniyor: true }))
    const p = api.get<T>(url)
      .then((r) => { if (!o.iptal) setDurum({ veri: r.data, yukleniyor: false, hata: null }) })
      .catch((e: unknown) => {
        if (o.iptal) return
        setDurum((d) => ({ veri: d.veri, yukleniyor: false, hata: apiHata(e) }))
        // Askı 403'ünü hataBildir kendisi yutar (uygulama zaten askı sayfasına geçti).
        if (baglam.current) hataBildir(baglam.current, e)
      })
      .finally(() => { if (o.istek === p) o.istek = null })
    o.istek = p
    return p
  }, [url])

  useEffect(() => {
    const o: Oturum = { iptal: false, istek: null }
    oturum.current = o
    if (url) {
      setDurum((d) => (d.veri === null && d.yukleniyor && d.hata === null ? d : { veri: null, yukleniyor: true, hata: null }))
      void yenile()
    }
    return () => { o.iptal = true }
  }, [url, yenile])

  return { ...durum, yenile }
}
