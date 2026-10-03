// Müşteri ADINA panele giriş (cPanel tarzı tek tık giriş) — ön yüz akışı.
//
//  1. Bayi/yönetici satıra tıklar → POST /domains/{id}/adina-oturum: 60 sn geçerli,
//     TEK KULLANIMLIK giriş kodu + (kapı modunda) müşteri kapısının adresi.
//  2. Sekme müşteri kapısındaki `/adina#kod=…&don=…` adresine gider. Kod URL PARÇASINDA
//     (#) taşınır: sunucuya, nginx günlüğüne ve Referer'a gitmez; AdinaGirisPage onu
//     hemen adres çubuğundan siler ve POST /musteri/adina-giris ile kısa ömürlü
//     (30 dk) bir token'a çevirir.
//  3. Oturum SEKMEYE ÖZELDİR (store/auth oturumDeposu → sessionStorage): bayinin kendi
//     oturumu ve öteki sekmeleri etkilenmez. "Çık" → sekme `don` adresine (bayi paneli) döner.
import { api } from '@/lib/api'
import { adinaBilgi, useAuth } from '@/store/auth'

type BaslatYanit = { kod: string; adres: string; alan_adi: string }

// guvenliDonus: dönüş adresi yalnız AYNI ana makinedeki http(s) adresi olabilir
// (açık yönlendirme yok). Değilse bu kökün ana sayfası.
export function guvenliDonus(don: string | null | undefined): string {
  if (!don) return '/'
  try {
    const u = new URL(don, window.location.href)
    if ((u.protocol === 'https:' || u.protocol === 'http:') && u.hostname === window.location.hostname && u.pathname.startsWith('/')) {
      return u.href
    }
  } catch { /* yoksay */ }
  return '/'
}

// adinaBaslat: kodu alır ve sekmeyi müşteri paneline götürür. Hata çağırana fırlatılır.
export async function adinaBaslat(domainID: number): Promise<void> {
  const r = await api.post<BaslatYanit>(`/domains/${domainID}/adina-oturum`)
  const { kod, adres } = r.data
  if (!kod) throw new Error('giriş kodu alınamadı')
  const kok = adres || window.location.origin
  const don = window.location.origin + window.location.pathname
  window.location.assign(`${kok}/adina#kod=${encodeURIComponent(kod)}&don=${encodeURIComponent(don)}`)
}

// adinaCik: adına-oturumu kapatır (denetime yazılır), sekme bayi paneline döner.
export function adinaCik(): void {
  const b = adinaBilgi()
  const tok = useAuth.getState().token
  if (tok) {
    // Yanıt beklenmez; keepalive sayfa değişse de isteği tamamlar.
    try {
      void fetch(`${(import.meta.env.VITE_API_BASE as string) || '/api/v1'}/musteri/adina-cikis`, {
        method: 'POST', keepalive: true, headers: { Authorization: `Bearer ${tok}` },
      }).catch(() => {})
    } catch { /* yoksay */ }
  }
  useAuth.getState().cikis()
  window.location.assign(guvenliDonus(b?.don))
}
