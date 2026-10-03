// Müşteri ADINA panele giriş (cPanel tarzı tek tık giriş) — ön yüz akışı.
//
//  1. Bayi/yönetici satıra tıklar → POST /domains/{id}/adina-oturum: 60 sn geçerli,
//     TEK KULLANIMLIK giriş kodu + (kapı modunda) müşteri kapısının adresi.
//  2. Sekme müşteri kapısındaki `/adina#kod=…&don=…` adresine gider. Kod URL PARÇASINDA
//     (#) taşınır: sunucuya, nginx günlüğüne ve Referer'a gitmez; AdinaGirisPage onu
//     hemen adres çubuğundan siler ve POST /musteri/adina-giris ile kısa ömürlü
//     (30 dk) bir token'a çevirir.
//  3. Oturum SEKMEYE ÖZELDİR (store/auth oturumDeposu → sessionStorage): bayinin kendi
//     oturumu ve öteki sekmeleri etkilenmez. Müşteri paneli YENİ SEKMEDE açılır (bayi paneli
//     yerinde kalır); "dön" o sekmeyi kapatır. Açılır pencere engellenirse aynı sekmede gidilir,
//     "dön" sekmeyi `don` adresine (bayi paneli) götürür.
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

// adinaBaslat: kodu alır ve müşteri panelini YENİ SEKMEDE açar. Hata çağırana fırlatılır.
// Sekme, tıklama anında (await'ten ÖNCE) açılır: tarayıcılar kullanıcı hareketi dışındaki
// window.open'ı engeller. Engellenirse aynı sekmede gidilir.
export async function adinaBaslat(domainID: number): Promise<void> {
  const w = window.open('', '_blank')
  if (w) {
    try { w.opener = null } catch { /* yoksay */ }
  }
  try {
    const r = await api.post<BaslatYanit>(`/domains/${domainID}/adina-oturum`)
    const { kod, adres } = r.data
    if (!kod) throw new Error('giriş kodu alınamadı')
    const kok = adres || window.location.origin
    const don = window.location.origin + window.location.pathname
    const hedef = `${kok}/adina#kod=${encodeURIComponent(kod)}&don=${encodeURIComponent(don)}`
    if (w && !w.closed) w.location.replace(hedef + '&sekme=1')
    else window.location.assign(hedef)
  } catch (e) {
    if (w && !w.closed) w.close()
    throw e
  }
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
  if (b?.sekme) {
    // Script'in açtığı sekme kapatılabilir; tarayıcı izin vermezse bayi paneline gidilir.
    window.close()
    setTimeout(() => { if (!window.closed) window.location.assign(guvenliDonus(b?.don)) }, 300)
    return
  }
  window.location.assign(guvenliDonus(b?.don))
}
