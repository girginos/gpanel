// Ön yüz rol yardımcıları (Seçenek A — müşteri paneli düzeltmeleri).
//
// 🔴 TEK KAYNAK: rol kararı YALNIZ useAuth `kullanici.rol` değerinden verilir.
// 'user' = hosting müşterisi; bu değeri yalnız girisMusteri yazar (store/auth.ts).
// /me'nin müşteriye döndürdüğü sentetik 'musteri' değeri bu kararla KARIŞTIRILMAZ.
// `girginospanel.musteri` bayrağı bugün eşdeğer ama yeni kapılar ona bağlanmaz:
// users.role ENUM'u 'user' içeriyor, giriş yolu genişlerse eşdeğerlik bozulur.
//
// Müşteri kapıları yalnız 'user' rolünü, bayi rota kapısı yalnız 'reseller' rolünü etkiler.
// Yönetici-yalnız (AdminOnly) uca giden arayüz öğeleri yoneticiMi ile çizilir: bayi ve
// müşteri onları görmez (bayi kapsam kuralı).
import { useAuth, KEY_MUSTERI_DOM, oturumDeposu, sonOturumMusteri, type Kullanici } from '@/store/auth'

export type Rol = Kullanici['rol']

export const musteriMi = (rol: Rol | null | undefined): boolean => rol === 'user'

// Panel sahibi (yönetici) mi? Sunucu tarafında AdminOnly olan uçlara giden düğme,
// bağlantı ve yoklamalar YALNIZ bunun true olduğu oturumda çizilir/çağrılır.
// Bilinmeyen/boş rol yönetici SAYILMAZ (kapalı varsayılan).
export const yoneticiMi = (rol: Rol | null | undefined): boolean => rol === 'admin'

export function useRol(): Rol | undefined {
  return useAuth((s) => s.kullanici?.rol)
}

// Müşterinin (tek) aboneliğinin kimliği. Boş/bozuksa null — çağıran yönlendirme
// yerine oturumu kapatır (aksi halde /abonelikler/ → kapı → /abonelikler/ döngüsü).
export function musteriDomainID(): number | null {
  let v: string | null = null
  // Adına-oturumda (bayi "panele gir") sekmeye özel depo — bkz. store/auth oturumDeposu.
  try { v = oturumDeposu().getItem(KEY_MUSTERI_DOM) } catch { return null }
  if (!v || !/^[1-9][0-9]{0,14}$/.test(v)) return null
  return Number(v)
}

// Rolün ana sayfası: müşteri → kendi aboneliği (/abonelikler/{id}; bu rotada müşteriye
// MusteriAnaSayfa çizilir, bkz. App.tsx AbonelikSayfasi), diğerleri → '/'.
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

// ── Bayi (rol 'reseller') rota izin listesi — App.tsx RolKapisi ─────────────────────
// KANIT (tahmin değil): (1) bayi menüsü — DashboardLayout RESELLER_NAV, AltNavBar
// bayiSekmeler (+ 'Yeni' → /domainler?yeni=1), TopBar HERKES araması (domain/alt alan
// sonuçları → /abonelikler/…), bildirim zili, hesap menüsü; (2) bu sayfaların uçları
// main.go'da bayiye AÇIK (AdminVeyaReseller / SahipYonetim / MusteriScope — kapsam handler
// içinde); (3) derin bağlantılar: abonelik sayfaları (sahipliği arka uç MusteriScope denetler,
// kapı ayrıca bayinin kendi listesine bakar) ve plan düzenleyici /araclar/paketler/<id>
// (ServicePlansPage, DomainPlanPage); Website Security alanı (/website-security/domain/<id>). Geri kalan her yol — AdminOnly uçlara dayanan yönetici
// sayfaları (sunucu araçları, bayiler, bayi planları, eklentiler, antivirüs paneli, izleme…)
// ve BİLİNMEYENLER — bayiye yasak: yeni rota eklenince bayi için varsayılan KAPALI.
const BAYI_SERBEST = new Set([
  '/', '/domainler', '/abonelikler', '/hizmet-planlari', '/araclar/dns-sablonu',
  '/denetim', '/bildirimler', '/profil', '/parola-degistir', '/ayarlar',
  // RESELLER_NAV'a sonradan giren kapılar (uçlar bayiye açık, kapsam handler'da):
  '/marka', '/api-anahtarlari', '/website-security', '/araclar/tasima',
])
const BAYI_ABONELIK = /^\/abonelikler\/([1-9][0-9]{0,14})(\/|$)/
const BAYI_PLAN = /^\/araclar\/paketler\/[1-9][0-9]{0,14}$/
const BAYI_WEBSEC = /^\/website-security\/domain\/[1-9][0-9]{0,14}$/

// React Router yolları büyük/küçük harf duyarsız ve sondaki '/' yok sayılarak eşler;
// kapı da aynı biçimde karar verir (yoksa izinli sayfa '/Domainler' ile yasak görünürdü).
function bayiYolu(yol: string): string {
  return (yol.length > 1 ? yol.replace(/\/+$/, '') : yol).toLowerCase()
}

export function bayiYoluIzinli(yol: string): boolean {
  const y = bayiYolu(yol)
  return BAYI_SERBEST.has(y) || BAYI_ABONELIK.test(y) || BAYI_PLAN.test(y) || BAYI_WEBSEC.test(y)
}

// Yasak yoldan bayinin en yakın sayfası: plan yolları → planları; abonelik yolları (bozuk ya
// da bayiye ait olmayan kimlik) → hosting listesi; antivirüs paneli → bildirimler (bayinin
// antivirüs uyarıları orada; domainsiz bildirim bağlantısı bu yola düşer); diğerleri → '/'.
export function bayiYonlendirme(yol: string): string {
  const y = bayiYolu(yol)
  if (y === '/araclar/paketler' || y.startsWith('/araclar/paketler/')) return '/hizmet-planlari'
  if (y.startsWith('/abonelikler/')) return '/domainler'
  if (y === '/antivirus') return '/bildirimler'
  return '/'
}

// Yol bir abonelik (hosting) yoluysa kimliği; değilse null.
export function bayiAbonelikID(yol: string): number | null {
  const m = BAYI_ABONELIK.exec(bayiYolu(yol))
  return m ? Number(m[1]) : null
}

// Oturumu olmayan ziyaretçinin gideceği giriş formu (GuardedRoute). Kapı modu açıkken
// KapiGiris zaten doğru forma çevirir; bu, kapı modu kapalı (karma) kurulum içindir.
// Son oturum müşteriyse YALNIZ müşteriye ait olabilecek hedefler (/, bir abonelik yolu,
// /profil, /bildirimler …) müşteri formuna gider; yönetici/bayi yolları (/araclar/…,
// /domainler …) HER ZAMAN /giris'e — aynı tarayıcıdaki yönetici müşteri izine kilitlenmez
// (MFE-1). '/' için müşteri formunda "Yönetim girişi" bağlantısı var (CPanelGirisPage).
// Oturum açılınca domain kimliği silindiğinden abonelik yolu yalnız BİÇİMİYLE tanınır.
export function girisYolu(hedef: string): '/giris' | '/cp/giris' {
  return sonOturumMusteri() && musteriYoluOlabilir(hedef) ? '/cp/giris' : '/giris'
}

export function musteriYoluOlabilir(yol: string): boolean {
  const y = yol.length > 1 ? yol.replace(/\/+$/, '') : yol
  return y === '' || y === '/' || MUSTERI_SERBEST.has(y) || /^\/abonelikler\/[1-9][0-9]{0,14}(\/|$)/.test(y)
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
