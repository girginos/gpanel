// Ön yüz rol yardımcıları (Seçenek A — müşteri paneli düzeltmeleri).
//
// 🔴 TEK KAYNAK: rol kararı YALNIZ useAuth `kullanici.rol` değerinden verilir.
// 'user' = hosting müşterisi; bu değeri yalnız girisMusteri yazar (store/auth.ts).
// /me'nin müşteriye döndürdüğü sentetik 'musteri' değeri bu kararla KARIŞTIRILMAZ.
// `girginospanel.musteri` bayrağı bugün eşdeğer ama yeni kapılar ona bağlanmaz:
// users.role ENUM'u 'user' içeriyor, giriş yolu genişlerse eşdeğerlik bozulur.
//
// Yalnız 'user' rolünü etkiler — yönetici ve bayi davranışı değişmez.
import { useAuth, KEY_MUSTERI_DOM, type Kullanici } from '@/store/auth'

export type Rol = Kullanici['rol']

export const musteriMi = (rol: Rol | null | undefined): boolean => rol === 'user'

export function useRol(): Rol | undefined {
  return useAuth((s) => s.kullanici?.rol)
}

// Müşterinin (tek) aboneliğinin kimliği. Boş/bozuksa null — çağıran yönlendirme
// yerine oturumu kapatır (aksi halde /abonelikler/ → kapı → /abonelikler/ döngüsü).
export function musteriDomainID(): number | null {
  let v: string | null = null
  try { v = localStorage.getItem(KEY_MUSTERI_DOM) } catch { return null }
  if (!v || !/^[1-9][0-9]{0,14}$/.test(v)) return null
  return Number(v)
}

// Rolün ana sayfası: müşteri → kendi aboneliği, diğerleri → '/'.
// Hook DEĞİL: HataSiniri React ağacının dışında (sınıf bileşeni, hook yok) da
// çağırır; rol verilmezse store'dan okunur (store ilk değerini localStorage'dan alır).
// Müşterinin domain kimliği bozuksa '/' döner — oradaki rol kapısı oturumu kapatır.
export function rolAnaSayfa(rol: Rol | null | undefined = useAuth.getState().kullanici?.rol): string {
  if (!musteriMi(rol)) return '/'
  const id = musteriDomainID()
  return id === null ? '/' : `/abonelikler/${id}`
}

// Müşteri izin listesi: /abonelikler/<kendi id>/*, /profil (ve eski takma adları),
// /bildirimler. Geri kalan her yol (bilinmeyenler dahil) müşteri için yasak.
const MUSTERI_SERBEST = new Set(['/profil', '/bildirimler', '/parola-degistir', '/ayarlar'])

export function musteriYoluIzinli(yol: string, domainID: number): boolean {
  const y = yol.length > 1 ? yol.replace(/\/+$/, '') : yol
  if (MUSTERI_SERBEST.has(y)) return true
  const kok = `/abonelikler/${domainID}`
  return y === kok || y.startsWith(kok + '/')
}

// Kırıntı (Breadcrumb) merkezi eşlemesi — 60+ sayfaya tek tek dokunmadan:
//  - müşteri: '/' → kendi aboneliği; '/domainler' (yönetici listesi) atılır
//  - bayi ve müşteri: '/araclar-ayarlar' (yönetici-yalnız merkez) atılır
export function kirintiEsle<T extends { href?: string }>(items: T[], rol: Rol | null | undefined): T[] {
  if (musteriMi(rol)) {
    const ana = rolAnaSayfa(rol)
    return items
      .filter((it) => it.href !== '/domainler' && it.href !== '/araclar-ayarlar')
      .map((it) => (it.href === '/' ? { ...it, href: ana } : it))
  }
  if (rol === 'reseller') return items.filter((it) => it.href !== '/araclar-ayarlar')
  return items
}
