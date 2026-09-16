import { useEffect, useRef, useState } from 'react'
import { useAuth } from '@/store/auth'
import { useToast, type ToastAPI } from '@/components/Toast'
import Breadcrumb from '@/components/Breadcrumb'
import LisansAskida from '@/components/LisansAskida'
import { useEklentiAktif } from '@/lib/eklenti'

// Eklenti ekranı — GENEL yükleyici.
//
// 🔴 NEDEN GENEL: her yeni eklenti için core'a elden bir sayfa yazmak, "eklenti
// core'dan bağımsız yaşasın" kararını her seferinde biraz daha aşındırıyordu
// (ETİS böyle geldi, mail hâlâ bekliyordu). Burada tek bir yükleyici var;
// eklentiler kendilerini ÇALIŞMA ZAMANINDA kaydeder, core adlarını bilmez.
//
// Sözleşme (yeni):
//   window.GospEklenti.kaydet('<ad>', { mount(el, ctx) { …; return temizle } })
//
// Sözleşme (eski, GERİYE UYUMLU): window.__gosp<Ad>Mount(el, ctx)
// ETİS bu biçimde yazılmıştı; kırmamak için ikisi de destekleniyor.
//
// ctx: { apiBase, token, dil, toast }
//   apiBase — eklentinin core proxy'si (/api/v1/eklenti/<ad>)
//   token   — bundle <script src> ile auth-DIŞI yüklenir (script etiketi JWT
//             taşıyamaz); API çağrıları için token buradan geçirilir.
//   toast   — panelin bildirim yüzeyi. Eklentiler kendi satır içi kutularını
//             çiziyordu; o kutu ekranın ÜSTÜNDE, Kaydet düğmesi ise yapışkan
//             çubukta ALTTA olduğu için başarı bildirimi görüş alanının
//             dışında kalıyor ve kullanıcı "kaydedilmedi" sanıyordu. Toast
//             React context'inde yaşadığından eklentiden erişilemezdi;
//             köprü burada. ESKİ CORE'DA YOKTUR — eklenti varlığını
//             kontrol etmeli (bkz. app.js: mesaj()).

// Ctx — eklentiye geçilen bağlam. `domainID`/`alanAdi` YALNIZ ekran bir
// domainin içine gömüldüğünde doldurulur; eklenti o zaman kendini o alan adına
// daraltır (liste süzülür, oluşturma formunda domain sabitlenir).
type Ctx = {
  apiBase: string
  token: string
  dil: string
  toast: ToastAPI
  domainID?: number
  alanAdi?: string
}
type Kayit = { mount: (el: HTMLElement, ctx: Ctx) => (() => void) | void }

declare global {
  interface Window {
    GospEklenti?: {
      kayitlar: Record<string, Kayit>
      kaydet: (ad: string, k: Kayit) => void
    }
  }
}

// 🔴 `[key: string]: unknown` BİLEREK YOK. Window'a index signature eklemek,
// `declare global` tüm projeye yayıldığı için HER dosyada `window.yanlisYazim`
// hatasını sessizce geçerli kılardı — tek bir bileşen dosyası, projenin
// tamamındaki yazım denetimini kapatırdı. Eski sözleşmeyi okumak için yerel
// daraltma yapılır (aşağıda).

// Kayıt defterini ERKEN kur: bundle yüklenirken hazır olmalı, yoksa eklenti
// kendini kaydedemez ve sessizce boş ekran çıkar.
function defteriKur() {
  if (!window.GospEklenti) {
    window.GospEklenti = {
      kayitlar: {},
      kaydet(ad: string, k: Kayit) {
        this.kayitlar[ad] = k
      },
    }
  }
  return window.GospEklenti
}

// eskiSozlesme — __gospTehditMount gibi eski global'i arar.
function eskiSozlesme(ad: string): Kayit | null {
  const anahtar = '__gosp' + ad.charAt(0).toUpperCase() + ad.slice(1) + 'Mount'
  const fn = (window as unknown as Record<string, unknown>)[anahtar]
  if (typeof fn === 'function') {
    return { mount: fn as Kayit['mount'] }
  }
  return null
}

export default function EklentiEkrani(props: {
  ad: string
  baslik: string
  aciklama?: string
  /** Sade mod: başlık ve breadcrumb çizilmez — ekran bir sekmenin içindedir. */
  sade?: boolean
  /** Eklentiye geçilecek ek bağlam (ör. içinde bulunulan domain). */
  ekBaglam?: Partial<Ctx>
}) {
  const { ad, baslik, aciklama, sade, ekBaglam } = props
  const kok = useRef<HTMLDivElement>(null)
  const token = useAuth((s) => s.token)
  // 🔴 Eklenti aktif mi (lisans askida DEGIL mi). false => YONETIM YERINE
  // landing goster + bundle'i MOUNT ETME (yonetim arayuzu hic yuklenmez → kullanici
  // yonetemez). null => henuz bilinmiyor. Gercek engel zaten server-side 402'dir.
  const eklentiAktif = useEklentiAktif(ad)

  // 🔴 KİMLİĞİ SABİT bir köprü. ctx nesnesi mount efektine giriyor ve efekt
  // yalnız [ad, token, domainID] değişince yeniden koşmalı; toast API'si
  // referansı değişirse eklenti sürekli yeniden monte olur ve kullanıcının
  // doldurduğu form ile kaydırma konumu kaybolur (aynı tuzak ekBaglam
  // yorumunda da anlatılıyor). Sarmalayıcı bir kez üretilir, güncel API'yi
  // her çağrıda ref'ten okur.
  const toast = useToast()
  const toastRef = useRef<ToastAPI>(toast)
  toastRef.current = toast
  const toastKopru = useRef<ToastAPI>({
    basari: (b, m) => toastRef.current.basari(b, m),
    hata: (b, m) => toastRef.current.hata(b, m),
    bilgi: (b, m) => toastRef.current.bilgi(b, m),
    uyari: (b, m) => toastRef.current.uyari(b, m),
  }).current
  const [hata, setHata] = useState<string | null>(null)

  useEffect(() => {
    // Askida/yukleniyorken bundle YUKLENMEZ ve MOUNT EDILMEZ.
    if (eklentiAktif !== true) return
    const defter = defteriKur()
    let temizle: (() => void) | void
    let iptal = false

    const monte = () => {
      if (iptal || !kok.current) return
      const kayit = defter.kayitlar[ad] || eskiSozlesme(ad)
      if (!kayit) {
        // Bundle yüklendi ama kendini kaydetmedi: sessiz boş ekran yerine
        // NE OLDUĞUNU söyle — aksi halde "sayfa açılmıyor" olarak gelir.
        setHata('Eklenti yüklendi ancak arayüzünü kaydetmedi (sürüm uyumsuz olabilir).')
        return
      }
      setHata(null)
      temizle = kayit.mount(kok.current, {
        apiBase: `/api/v1/eklenti/${ad}`,
        token: token || '',
        dil: document.documentElement.lang || 'tr',
        toast: toastKopru,
        ...(ekBaglam || {}),
      })
    }

    if (defter.kayitlar[ad] || eskiSozlesme(ad)) {
      monte()
    } else {
      const id = `gosp-eklenti-${ad}`
      let s = document.getElementById(id) as HTMLScriptElement | null
      if (!s) {
        s = document.createElement('script')
        s.id = id
        s.src = `/api/v1/eklenti-bundle/${ad}/app.js`
        s.addEventListener('load', () => {
          s!.dataset.gospYuklendi = '1'
          monte()
        }, { once: true })
        s.addEventListener('error', () =>
          setHata('Eklenti yüklenemedi — kurulu veya etkin değil.'), { once: true })
        document.body.appendChild(s)
      } else if (s.dataset.gospYuklendi === '1') {
        // 🔴 ZATEN YÜKLENMİŞ script'te `load` BİR DAHA ATEŞLENMEZ. Eskiden bu
        // dalda yalnız dinleyici ekleniyordu; bundle yüklenmiş ama kendini
        // kaydetmemişse (sürüm uyumsuz, ad eşleşmiyor) monte hiç çağrılmaz,
        // dolayısıyla uyarı da GÖSTERİLMEZDİ: kullanıcı kalıcı boş ekran görür
        // ve sayfayı yenilemek de düzeltmez (script yine DOM'dadır).
        monte()
      } else {
        // Yükleme sürüyor: aynı effect birden çok kez koşarsa dinleyici
        // birikmesin diye { once: true }.
        s.addEventListener('load', monte, { once: true })
      }
    }

    return () => {
      iptal = true
      if (typeof temizle === 'function') temizle()
    }
    // 🔴 ekBaglam bağımlılığa GİRMEZ: çağıran her render'da yeni bir nesne
    // ürettiği için eklenti sürekli yeniden monte olur ve kullanıcının açtığı
    // form/kaydırma durumu kaybolurdu. İçeriği domain değişmedikçe sabittir.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ad, token, ekBaglam?.domainID, eklentiAktif])

  // 🔴 SAYFA DOLGUSU BURADA. Panelin <main>'i yatay dolgu vermez; her sayfa
  // kendi dolgusunu koyar (bkz. EklentilerPage: "px-4 py-4 sm:px-6 sm:py-5
  // max-w-6xl mx-auto"). Bu kabuk onu koymuyordu, dolayısıyla HER eklenti
  // ekranı — başlığıyla birlikte — pencerenin sağ ve sol kenarına yapışık
  // çiziliyordu. Ölçüt panelin kendi sayfaları; aynı değerler kullanılır ki
  // eklenti ekranı diğer sayfalardan farklı hizalanmasın. Genişlik sınırı
  // ayrıca geniş ekranda tek satırlık metin kutularının 1200 piksele
  // yayılmasını da engeller.
  // `sade` gömülü kullanımdır (örn. abonelik sayfası içinde); orada dolguyu
  // saran sayfa verir, burada vermek çift dolgu olurdu.
  const kapsayici = sade ? '' : 'px-4 py-4 sm:px-6 sm:py-5 max-w-6xl mx-auto'

  return (
    <div className={kapsayici}>
      {!sade && (
        <>
          <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: baslik }]} />
          <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{baslik}</h1>
          {aciklama && (
            <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">{aciklama}</p>
          )}
        </>
      )}
      {hata && (
        <div className="rounded-lg border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 px-4 py-3 text-sm text-amber-800 dark:text-amber-300 mb-4">
          {hata}
        </div>
      )}
      {eklentiAktif === false ? (
        <LisansAskida baslik={baslik} />
      ) : (
        <div ref={kok} />
      )}
    </div>
  )
}
