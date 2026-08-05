// Eklenti (plugin) durum yardımcıları.
//
// Neden ayrı dosya: mail eklentisi etkinleştirildiğinde SOL MENÜ, domain Mail
// sekmesi ve plan posta ayarları YENİDEN DERLEME OLMADAN görünmelidir. Sayfalar
// zaten mount'ta /eklentiler çeker; menü ise kalıcı mount olduğu için bir olayla
// tazelenir (eklentiDegisti()).
//
// 🔴 HATA YUTULMAZ: istek başarısız olursa BİLİNEN SON DEĞER korunur; "kapalı"
// varsayıp menüyü gizlemek de, "açık" varsayıp göstermek de yanlış olur.
import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'

export type EklentiKayit = {
  ad: string
  etiket: string
  surum: string
  aktif: boolean
  ui: boolean
  saglik: string
}

const OLAY = 'gosp:eklenti-degisti'

/** Etkinleştirme/kaldırma sonrası çağrılır → dinleyen bileşenler tazelenir. */
export function eklentiDegisti() {
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(OLAY))
}

export async function eklentileriGetir(): Promise<EklentiKayit[]> {
  const { data } = await api.get<EklentiKayit[]>('/eklentiler')
  return Array.isArray(data) ? data : []
}

/**
 * Bir eklentinin etkin olup olmadığını izler.
 * `null` = henüz bilinmiyor (ilk yükleme) → çağıran hiçbir şey ÇİZMEMELİ.
 */
export function useEklentiAktif(ad: string, etkin = true): boolean | null {
  const [aktif, setAktif] = useState<boolean | null>(null)
  const sonRef = useRef<boolean | null>(null)

  const cek = useCallback(() => {
    if (!etkin) return
    eklentileriGetir()
      .then(liste => {
        const v = liste.some(e => e.ad === ad && e.aktif)
        sonRef.current = v
        setAktif(v)
      })
      .catch(() => {
        // Ağ/yetki hatası: KARAR VERME. Bilinen son değeri koru.
        setAktif(sonRef.current)
      })
  }, [ad, etkin])

  useEffect(() => {
    cek()
    const t = setInterval(cek, 60_000)
    const onOlay = () => cek()
    window.addEventListener(OLAY, onOlay)
    window.addEventListener('focus', onOlay)
    return () => {
      clearInterval(t)
      window.removeEventListener(OLAY, onOlay)
      window.removeEventListener('focus', onOlay)
    }
  }, [cek])

  return aktif
}
