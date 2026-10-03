// useWidget — GET /pano/w/{id} (D3b). Widget başına ayrı istek (bir widget'ın
// hatası öbürlerini düşürmez). Kısa ömürlü modül önbelleği: aynı widget'ı isteyen
// iki bileşen (ör. KPI şeridi + eğilim kartı) tek istek paylaşır.
import { useCallback, useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'

const TTL_MS = 30_000
const onbellek = new Map<string, { t: number; p: Promise<unknown> }>()

// Önbellek anahtarı OTURUM SAHİBİNİ içerir: aynı sekmede çıkış → başka hesapla giriş
// (ör. yönetici → bayi) eski kapsamın verisini 30 sn boyunca göstermez.
function getir<T>(sahip: string, id: string, zorla: boolean): Promise<T> {
  const anahtar = `${sahip}|${id}`
  const k = onbellek.get(anahtar)
  if (!zorla && k && Date.now() - k.t < TTL_MS) return k.p as Promise<T>
  const p = api.get<{ id: string; veri: T }>(`/pano/w/${encodeURIComponent(id)}`).then((r) => r.data.veri)
  onbellek.set(anahtar, { t: Date.now(), p })
  p.catch(() => onbellek.delete(anahtar))
  return p
}

export type WidgetDurum<T> = { veri: T | null; yuk: boolean; hata: string; yenile: () => void }

export function useWidget<T>(id: string): WidgetDurum<T> {
  const sahip = useAuth((s) => (s.kullanici ? `${s.kullanici.rol}:${s.kullanici.id}` : ''))
  const [veri, setVeri] = useState<T | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState('')
  const [tur, setTur] = useState(0)

  useEffect(() => {
    let iptal = false
    setYuk(true); setHata('')
    getir<T>(sahip, id, tur > 0)
      .then((v) => { if (!iptal) setVeri(v) })
      .catch((e) => { if (!iptal) setHata(apiHata(e)) })
      .finally(() => { if (!iptal) setYuk(false) })
    return () => { iptal = true }
  }, [sahip, id, tur])

  const yenile = useCallback(() => setTur((n) => n + 1), [])
  return { veri, yuk, hata, yenile }
}
