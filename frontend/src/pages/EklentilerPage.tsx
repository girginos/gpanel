import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// Eklenti pazaryeri — DİNAMİK katalog.
//
// Katalog /eklenti-katalog uçundan gelir (yerel tanım + kurulum durumu + lisans
// durumu birleşik). Kart durumuna göre eylem değişir:
//   lisanssız → "Lisansı Gir" / "Ücretsiz 1 Ay Dene"
//   kuruluyor → adım adım ilerleme (2,5 sn'de bir tazelenir)
//   lisanslı  → durum rozeti + bitiş tarihi + "Lisansı Kaldır"
//
// 🔴 SAHTE TEMİZLİK YOK: yükleme / boş / hata durumları GERÇEKTİR. Katalog
// çekilemezse hata kutusu çizilir; asla "eklenti yok" gibi gösterilmez.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { eklentiDegisti } from '@/lib/eklenti'
import { useDialog } from '@/components/Dialog'

type Adim = { ad: string; etiket: string; durum: string; mesaj: string; sure: string }
type KurulumDurum = {
  eklenti: string
  durum: string // calisiyor | tamam | hata | yok
  hata: string
  adimlar: Adim[]
  basladi: string
  bitti: string
}
type LisansDurum = {
  var: boolean
  durum: string
  maskeli: string
  expires_at: string
  son_dogrulama: string
  son_hata: string
  // Lisans GECERLI ama dikkat gerektiren durum (kopya tespiti gibi).
  // son_hata'dan ayridir: o "calismiyor", bu "calisiyor ama".
  uyari: string
  deneme: boolean
}
type Kart = {
  slug: string
  eklenti_ad: string
  ad: string
  kategori: string
  ikon: string
  kisa: string
  uzun: string
  ozellikler: string[]
  fiyat: string
  deneme: boolean
  uzak: boolean
  kurulu: boolean
  aktif: boolean
  saglik: string
  surum: string
  kurulur: boolean
  lisans: LisansDurum
  kurulum?: KurulumDurum
}
type KatalogYanit = { sunucu: string; parmakizi: string; uyari?: string; urunler: Kart[] }


const EKL_EN: Record<string, string> = {
  "Bitiş tarihi:": "Expiry date:",
  "Böyle bir eklenti bulunamadı.": "No such add-on found.",
  "Deneme lisansı alınamadı": "Failed to get trial license",
  "Deneme lisansı alındı, kurulum başladı.": "Trial license obtained, installation started.",
  "Deneme sürümü etkin": "Trial version active",
  "Doğrula ve Kur": "Verify and Install",
  "Eklenti kataloğu alınamadı": "Failed to get add-on catalog",
  "Eklenti kataloğu yüklenemedi": "Failed to load add-on catalog",
  "Elle kopyalayın": "Copy manually",
  "Kurulu, lisanssız": "Installed, unlicensed",
  "Kurulum adımları": "Installation steps",
  "Kurulum günlüğü": "Installation log",
  "Kurulum sürüyor…": "Installation in progress…",
  "Lisans doğrulanamadı": "Failed to verify license",
  "Lisans doğrulanamadı.": "Failed to verify license.",
  "Lisans doğrulandı, kurulum başladı.": "License verified, installation started.",
  "Lisans geçerli.": "License is valid.",
  "Lisans kaldırılamadı": "Failed to remove license",
  "Lisans kaldırıldı.": "License removed.",
  "Lisans var, eklenti kapalı": "Licensed, add-on off",
  "Lisanslı ve etkin": "Licensed and active",
  "Lisanssız": "Unlicensed",
  "Lisansı Doğrula": "Verify License",
  "Lisansı Gir": "Enter License",
  "Lisansı Kaldır": "Remove License",
  "her şey yolunda": "all is well",
  "ikili düz metin paketten kuruldu (şifreli paket yok)": "binary installed from plaintext package (no encrypted package)",
  "Ücretsiz 1 Ay Dene": "Try Free for 1 Month",
  "Şu anda kurulabilecek bir eklenti bulunmuyor.": "No add-on is currently available to install.",
  "← Eklentilere dön": "← Back to add-ons",
  "Emin misiniz?": "Are you sure?",
  "\"{0}\" lisansı kaldırılacak ve eklenti kapatılacak.": "The \"{0}\" license will be removed and the add-on turned off.",
  "Posta kutularınız, alan adlarınız ve ayarlarınız SİLİNMEZ — lisansı tekrar girdiğinizde kaldığı yerden devam eder.": "Your mailboxes, domains and settings are NOT deleted — when you enter the license again it resumes where it left off.",
  "Devam edilsin mi?": "Continue?",
  "Anasayfa": "Homepage",
  "Eklentiler": "Add-ons",
  "Tekrar dene": "Try again",
  "Sunucu Kodu": "Server Code",
  "Lisans alabilmek için bu sunucunun hesabınıza kayıtlı olması gerekir.": "To obtain a license, this server must be registered to your account.",
  "Kopyalandı ✓": "Copied ✓",
  "Kopyala": "Copy",
  "Lisans sunucusu:": "License server:",
  "Doğrulanıyor…": "Verifying…",
  "Kuruluyor…": "Installing…",
  "İşleniyor…": "Processing…",
  "Anahtar:": "Key:",
  " (deneme)": " (trial)",
  "Son doğrulama:": "Last verification:",
  "Not:": "Note:",
  "adım · tamamlandı": "steps · completed",
  "Kurulum tamamlanamadı, eklenti ETKİNLEŞTİRİLMEDİ. Hatayı giderdikten sonra lisansı tekrar girip yeniden deneyin.": "Installation could not be completed, the add-on was NOT activated. After fixing the error, enter the license again and retry.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (EKL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function tarih(s: string): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleDateString('tr-TR', { day: '2-digit', month: 'long', year: 'numeric' })
}

function kurulumSuruyor(k: KatalogYanit | null): boolean {
  return !!k?.urunler?.some(u => u.kurulum?.durum === 'calisiyor')
}

export default function EklentilerPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const { slug } = useParams()
  // Panoya kopyalama izin/güvenli-bağlam nedeniyle başarısız olabilir — sessizce
  // cevir("kopyalandı") DEMEZ; butonda gerçek sonucu gösterir.
  const [kopyaDurum, setKopyaDurum] = useState<'bos' | 'ok' | 'hata'>('bos')
  const kodKopyala = useCallback(async (kod: string) => {
    try {
      await navigator.clipboard.writeText(kod)
      setKopyaDurum('ok')
    } catch {
      setKopyaDurum('hata')
    }
    window.setTimeout(() => setKopyaDurum('bos'), 2500)
  }, [])
  const [katalog, setKatalog] = useState<KatalogYanit | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)

  // İşlem geri bildirimi (modal/aksiyon sonuçları)
  const [islem, setIslem] = useState<string | null>(null)      // süren işlem: eklenti_ad
  const [islemHata, setIslemHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  // Lisans anahtarı modalı
  const [modalKart, setModalKart] = useState<Kart | null>(null)
  const [anahtar, setAnahtar] = useState('')

  const ilkRef = useRef(true)

  const cek = useCallback((sessiz = false) => {
    if (!sessiz) setYuk(true)
    return api.get<KatalogYanit>('/eklenti-katalog')
      .then(r => {
        setKatalog(r.data)
        setHata(null)
      })
      .catch(e => {
        // 🔴 Hata durumunda ESKİ veriyi "temiz" göstermeyiz; hata kutusu çıkar.
        setHata(apiHata(e, cevir("Eklenti kataloğu alınamadı")))
        if (ilkRef.current) setKatalog(null)
      })
      .finally(() => { setYuk(false); ilkRef.current = false })
  }, [])

  useEffect(() => { cek() }, [cek])

  // Kurulum sürerken hızlı yoklama.
  useEffect(() => {
    if (!kurulumSuruyor(katalog)) return
    const t = setInterval(() => { cek(true) }, 2500)
    return () => clearInterval(t)
  }, [katalog, cek])

  // Kurulum biter bitmez menü/sekme kapılarını tazele.
  const oncekiAktif = useRef<string>('')
  useEffect(() => {
    const imza = (katalog?.urunler || []).map(u => `${u.eklenti_ad}:${u.aktif ? 1 : 0}`).join(',')
    if (oncekiAktif.current && imza !== oncekiAktif.current) eklentiDegisti()
    oncekiAktif.current = imza
  }, [katalog])

  async function lisansGonder() {
    if (!modalKart) return
    const ad = modalKart.eklenti_ad
    setIslem(ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${ad}/lisans`, { anahtar })
      setBasari(data?.mesaj || cevir("Lisans doğrulandı, kurulum başladı."))
      setModalKart(null); setAnahtar('')
      await cek(true)
    } catch (e) {
      setIslemHata(apiHata(e, cevir("Lisans doğrulanamadı")))
    } finally {
      setIslem(null)
    }
  }

  async function denemeAl(k: Kart) {
    setIslem(k.eklenti_ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${k.eklenti_ad}/deneme`, {})
      setBasari(data?.mesaj || cevir("Deneme lisansı alındı, kurulum başladı."))
      await cek(true)
    } catch (e) {
      // trial_used mesajı sunucudan geldiği gibi gösterilir.
      setIslemHata(apiHata(e, cevir("Deneme lisansı alınamadı")))
    } finally {
      setIslem(null)
    }
  }

  async function lisansDogrula(k: Kart) {
    setIslem(k.eklenti_ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${k.eklenti_ad}/lisans-dogrula`, {})
      // 🔴 "belirsiz" (ağ hatası) BAŞARI DEĞİLDİR — ayrı gösterilir.
      if (data?.karar === 'gecerli') setBasari(data.mesaj || cevir("Lisans geçerli."))
      else setIslemHata(data?.mesaj || cevir("Lisans doğrulanamadı."))
      await cek(true)
      eklentiDegisti()
    } catch (e) {
      setIslemHata(apiHata(e, cevir("Lisans doğrulanamadı")))
    } finally {
      setIslem(null)
    }
  }

  async function lisansKaldir(k: Kart) {
    if (!(await onay({ baslik: cevir('Emin misiniz?'),
      mesaj: cevirT(cevir('"{0}" lisansı kaldırılacak ve eklenti kapatılacak.'), k.ad) + '\n\n' +
        cevir('Posta kutularınız, alan adlarınız ve ayarlarınız SİLİNMEZ — lisansı tekrar girdiğinizde kaldığı yerden devam eder.') + '\n\n' +
        cevir('Devam edilsin mi?'), tehlike: true }))) return
    setIslem(k.eklenti_ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.delete(`/eklenti/${k.eklenti_ad}/lisans`)
      setBasari(data?.mesaj || cevir("Lisans kaldırıldı."))
      await cek(true)
      eklentiDegisti()
    } catch (e) {
      setIslemHata(apiHata(e, cevir("Lisans kaldırılamadı")))
    } finally {
      setIslem(null)
    }
  }

  const urunler = katalog?.urunler || []
  const secili = slug ? urunler.find(u => u.slug === slug) : undefined

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-6xl mx-auto">
      <Breadcrumb items={
        secili
          ? [{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir('Eklentiler'), href: '/eklentiler' }, { etiket: secili.ad }]
          : [{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Sunucu Yönetimi") }, { etiket: cevir('Eklentiler') }]
      } />

      {!secili && (
        <>
          <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">{cevir("Eklentiler")}</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
            {cevir(cevir("Panelinize ek yetenekler kazandıran lisanslı modüller. Lisansınızı girin, kurulum otomatik yapılsın."))}
          </p>
        </>
      )}

      {katalog?.uyari && (
        <div className="mb-4 rounded-lg border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 px-4 py-3 text-sm text-amber-800 dark:text-amber-200">
          {katalog.uyari}
        </div>
      )}
      {basari && (
        <div className="mb-4 rounded-lg border border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20 px-4 py-3 text-sm text-emerald-800 dark:text-emerald-200">
          {basari}
        </div>
      )}
      {islemHata && (
        <div className="mb-4 rounded-lg border border-rose-200 dark:border-rose-800 bg-rose-50 dark:bg-rose-900/20 px-4 py-3 text-sm text-rose-800 dark:text-rose-200">
          {islemHata}
        </div>
      )}

      {/* ── Yükleniyor ── */}
      {yuk && !katalog && (
        <div className="grid gap-4 sm:grid-cols-2">
          {[0, 1].map(i => (
            <div key={i} className="rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-5 animate-pulse">
              <div className="h-10 w-10 rounded-xl bg-slate-200 dark:bg-slate-700 mb-4" />
              <div className="h-4 w-2/5 bg-slate-200 dark:bg-slate-700 rounded mb-2" />
              <div className="h-3 w-full bg-slate-200 dark:bg-slate-700 rounded mb-1.5" />
              <div className="h-3 w-4/5 bg-slate-200 dark:bg-slate-700 rounded" />
            </div>
          ))}
        </div>
      )}

      {/* ── Hata (asla sessizce boş göstermeyiz) ── */}
      {hata && (
        <div className="rounded-2xl border border-rose-200 dark:border-rose-800 bg-rose-50 dark:bg-rose-900/20 p-5">
          <p className="text-sm font-medium text-rose-800 dark:text-rose-200">{cevir("Eklenti kataloğu yüklenemedi")}</p>
          <p className="text-sm text-rose-700 dark:text-rose-300 mt-1">{hata}</p>
          <button onClick={() => cek()} className="mt-3 px-3 py-1.5 text-sm rounded-lg bg-rose-600 hover:bg-rose-700 text-white transition">
            {cevir("Tekrar dene")}
          </button>
        </div>
      )}

      {/* ── Boş ── */}
      {!yuk && !hata && urunler.length === 0 && (
        <div className="rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-10 text-center">
          <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Şu anda kurulabilecek bir eklenti bulunmuyor.")}</p>
        </div>
      )}

      {/* ── Detay ── */}
      {!hata && slug && !yuk && !secili && (
        <div className="rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-10 text-center">
          <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Böyle bir eklenti bulunamadı.")}</p>
          <Link to="/eklentiler" className="mt-3 inline-block text-sm text-brand-600 dark:text-brand-400 hover:underline">{cevir("← Eklentilere dön")}</Link>
        </div>
      )}
      {secili && (
        <Detay
          k={secili}
          mesgul={islem === secili.eklenti_ad}
          onLisans={() => { setModalKart(secili); setIslemHata(null) }}
          onDeneme={() => denemeAl(secili)}
          onKaldir={() => lisansKaldir(secili)}
          onDogrula={() => lisansDogrula(secili)}
        />
      )}

      {/* ── Kart ızgarası ── */}
      {!secili && !hata && urunler.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2">
          {urunler.map(k => (
            <KartGovde
              key={k.slug}
              k={k}
              mesgul={islem === k.eklenti_ad}
              onLisans={() => { setModalKart(k); setIslemHata(null) }}
              onDeneme={() => denemeAl(k)}
              onKaldir={() => lisansKaldir(k)}
              onDogrula={() => lisansDogrula(k)}
            />
          ))}
        </div>
      )}

      {/* Sunucu Kodu — lisans ancak HESABA KAYITLI sunucuya verilir. Kod
          panelin kurulum parmak izidir; müşteri bunu app.girginos.io →
          Sunucularım sayfasına ekler. Kesilmiş göstermek işe yaramaz — tamamı
          kopyalanabilir olmalı. */}
      {katalog && (
        <div className="mt-6 rounded-lg border border-slate-200 bg-slate-50 p-4 dark:border-slate-700 dark:bg-slate-800/50">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Sunucu Kodu")}</h3>
              <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                {cevir("Lisans alabilmek için bu sunucunun hesabınıza kayıtlı olması gerekir.")}{' '}
                <a
                  href="https://app.girginos.io/sunucularim"
                  target="_blank"
                  rel="noreferrer"
                  className="font-medium text-emerald-600 hover:underline dark:text-emerald-400"
                >
                  {cevir(cevir("app.girginos.io → Sunucularım"))}
                </a>{' '}
                {cevir(cevir("sayfasından aşağıdaki kodu ekleyin."))}
              </p>
              <code className="mt-2 block break-all rounded bg-white px-2 py-1.5 font-mono text-[11px] text-slate-600 dark:bg-slate-900 dark:text-slate-300">
                {katalog.parmakizi}
              </code>
            </div>
            <button
              type="button"
              onClick={() => kodKopyala(katalog.parmakizi)}
              className="shrink-0 rounded-md border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-white dark:border-slate-600 dark:text-slate-200 dark:hover:bg-slate-700"
            >
              {kopyaDurum === 'ok' ? cevir('Kopyalandı ✓') : kopyaDurum === 'hata' ? cevir("Elle kopyalayın") : cevir('Kopyala')}
            </button>
          </div>
          <p className="mt-3 text-[11px] text-slate-400 dark:text-slate-500">
            {cevir("Lisans sunucusu:")} <span className="font-mono">{katalog.sunucu}</span>
          </p>
        </div>
      )}

      {/* ── Lisans anahtarı modalı ── */}
      <Modal acik={!!modalKart} baslik={cevirT(cevir("{0} — Lisansı Gir"), modalKart?.ad || '')} onKapat={() => { setModalKart(null); setAnahtar('') }}>
        <form onSubmit={e => { e.preventDefault(); lisansGonder() }}>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-3">
            {cevir(cevir("Satın aldığınız lisans anahtarını girin. Anahtar doğrulandıktan sonra eklenti bu sunucuya otomatik kurulur."))}
          </p>
          <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1" htmlFor="lisans-anahtari">
            {cevir(cevir("Lisans anahtarı"))}
          </label>
          <input
            id="lisans-anahtari"
            autoFocus
            value={anahtar}
            onChange={e => setAnahtar(e.target.value)}
            placeholder="XXXX-XXXX-XXXX-XXXX"
            className="w-full px-3 py-2 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-sm font-mono text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
          />
          {islemHata && <p className="mt-2 text-sm text-rose-600 dark:text-rose-400">{islemHata}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button type="button" onClick={() => { setModalKart(null); setAnahtar('') }}
              className="px-3 py-2 text-sm rounded-lg border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 transition">
              {cevir("Vazgeç")}
            </button>
            <button type="submit" disabled={!anahtar.trim() || !!islem}
              className="px-4 py-2 text-sm rounded-lg bg-brand-600 hover:bg-brand-700 disabled:opacity-50 text-white transition">
              {islem ? cevir('Doğrulanıyor…') : cevir("Doğrula ve Kur")}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}

/* ── Kart ─────────────────────────────────────────────────────────────── */

function Rozet({ k }: { k: Kart }) {
  if (k.kurulum?.durum === 'calisiyor')
    return <Etiket renk="sky">{cevir("Kuruluyor…")}</Etiket>
  if (k.kurulum?.durum === 'hata')
    return <Etiket renk="rose">{cevir("Kurulum başarısız")}</Etiket>
  if (k.aktif && k.lisans.var)
    return <Etiket renk="emerald">{k.lisans.deneme ? cevir('Deneme sürümü etkin') : cevir("Lisanslı ve etkin")}</Etiket>
  if (k.lisans.var && !k.aktif)
    return <Etiket renk="amber">{cevir("Lisans var, eklenti kapalı")}</Etiket>
  if (k.kurulu && !k.aktif)
    return <Etiket renk="slate">{cevir("Kurulu, lisanssız")}</Etiket>
  return <Etiket renk="slate">{cevir("Lisanssız")}</Etiket>
}

function Etiket({ renk, children }: { renk: string; children: React.ReactNode }) {
  const m: Record<string, string> = {
    emerald: 'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-900/25 dark:text-emerald-300 dark:border-emerald-800',
    amber: 'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/25 dark:text-amber-300 dark:border-amber-800',
    rose: 'bg-rose-50 text-rose-700 border-rose-200 dark:bg-rose-900/25 dark:text-rose-300 dark:border-rose-800',
    sky: 'bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-900/25 dark:text-sky-300 dark:border-sky-800',
    slate: 'bg-slate-50 text-slate-600 border-slate-200 dark:bg-slate-700/40 dark:text-slate-300 dark:border-slate-600',
  }
  return <span className={`inline-flex items-center px-2 py-0.5 rounded-full border text-[11px] font-medium ${m[renk] || m.slate}`}>{children}</span>
}

type Eylemler = { mesgul: boolean; onLisans: () => void; onDeneme: () => void; onKaldir: () => void; onDogrula: () => void }

function Eylem({ k, mesgul, onLisans, onDeneme, onKaldir, onDogrula }: { k: Kart } & Eylemler) {
  if (k.kurulum?.durum === 'calisiyor') {
    return <span className="text-sm text-sky-600 dark:text-sky-400">{cevir("Kurulum sürüyor…")}</span>
  }
  if (k.lisans.var) {
    return (
      <div className="flex flex-wrap gap-2">
        <button onClick={onDogrula} disabled={mesgul}
          className="px-3 py-2 text-sm rounded-lg border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50 transition">
          {mesgul ? cevir('İşleniyor…') : cevir("Lisansı Doğrula")}
        </button>
        <button onClick={onKaldir} disabled={mesgul}
          className="px-3 py-2 text-sm rounded-lg border border-rose-300 dark:border-rose-700 text-rose-700 dark:text-rose-300 hover:bg-rose-50 dark:hover:bg-rose-900/25 disabled:opacity-50 transition">
          {cevir("Lisansı Kaldır")}
        </button>
      </div>
    )
  }
  return (
    <div className="flex flex-wrap gap-2">
      <button onClick={onLisans} disabled={mesgul}
        className="px-4 py-2 text-sm rounded-lg bg-brand-600 hover:bg-brand-700 text-white disabled:opacity-50 transition">
        {cevir("Lisansı Gir")}
      </button>
      {k.deneme && (
        <button onClick={onDeneme} disabled={mesgul}
          className="px-4 py-2 text-sm rounded-lg border border-brand-300 dark:border-brand-700 text-brand-700 dark:text-brand-300 hover:bg-brand-50 dark:hover:bg-brand-900/25 disabled:opacity-50 transition">
          {mesgul ? cevir('İşleniyor…') : cevir("Ücretsiz 1 Ay Dene")}
        </button>
      )}
    </div>
  )
}

function Ikon({ d }: { d: string }) {
  return (
    <div className="w-11 h-11 rounded-xl bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center flex-shrink-0">
      <svg className="w-6 h-6 text-brand-600 dark:text-brand-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.6}>
        <path strokeLinecap="round" strokeLinejoin="round" d={d || 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4'} />
      </svg>
    </div>
  )
}

function LisansOzet({ l }: { l: LisansDurum }) {
  if (!l.var) return null
  return (
    <div className="mt-3 rounded-lg bg-slate-50 dark:bg-slate-900/50 border border-slate-200 dark:border-slate-700 px-3 py-2 text-xs text-slate-600 dark:text-slate-300 space-y-0.5">
      <div>{cevir("Anahtar:")} <span className="font-mono">{l.maskeli}</span>{l.deneme && cevir(" (deneme)")}</div>
      {l.expires_at && <div>{cevir("Bitiş tarihi:")} <strong>{tarih(l.expires_at)}</strong></div>}
      {l.son_dogrulama && <div>{cevir("Son doğrulama:")} {tarih(l.son_dogrulama)}</div>}
      {l.son_hata && <div className="text-rose-600 dark:text-rose-400">{cevir("Not:")} {l.son_hata}</div>}
      {/*
        Kopya uyarisi. Kirmizi DEGIL kehribar: hizmet calisiyor, lisans gecerli.
        Kirmiziya boyasaydik musteri calisan kurulumunu arizali sanardi. Metin
        lisans sunucusundan gelir; burada yorumlanmaz.
      */}
      {l.uyari && (
        <div className="mt-1 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-amber-700 dark:text-amber-400">
          {l.uyari}
        </div>
      )}
    </div>
  )
}

function Adimlar({ ku }: { ku: KurulumDurum }) {
  const renk: Record<string, string> = {
    tamam: 'text-emerald-600 dark:text-emerald-400',
    calisiyor: 'text-sky-600 dark:text-sky-400',
    uyari: 'text-amber-600 dark:text-amber-400',
    hata: 'text-rose-600 dark:text-rose-400',
  }
  const isaret: Record<string, string> = { tamam: '✓', calisiyor: '…', uyari: '!', hata: '×' }

  const uyarilar = ku.adimlar.filter((a) => a.durum === 'uyari')
  const hatalar = ku.adimlar.filter((a) => a.durum === 'hata')
  const suruyor = ku.durum === 'calisiyor' || ku.adimlar.some((a) => a.durum === 'calisiyor')

  /*
    🔴 ÖLÇÜT "bitti mi" DEĞİL, "bitti VE hatasız mı".
    Kurulum başarıyla bittiğinde 15 satırlık adım listesi kalıcı olarak ekranda
    durmasın — kart sade özete düşer. Ama:
      • hata (×) varsa liste AÇIK kalır; kurulum ETKİNLEŞMEDİ, gizlemek yanıltır.
      • kurulum hâlâ sürüyorsa liste AÇIK kalır; ilerleme görünmeli.
    Yalnız bu iki durum da yoksa katlanır.
  */
  const katlanabilir = !suruyor && ku.durum === 'tamam' && hatalar.length === 0
  const [acik, setAcik] = useState(!katlanabilir)
  // Kurulum bittiğinde (katlanabilir true'ya döndüğünde) listeyi otomatik kapat;
  // hata/çalışma durumuna dönerse otomatik aç. Yalnız geçişte tetiklenir, bu yüzden
  // kullanıcının elle açtığı günlüğü her poll'de kapatmaz.
  useEffect(() => { setAcik(!katlanabilir) }, [katlanabilir])

  return (
    <div className="mt-3 rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/50 p-3">
      {katlanabilir ? (
        <button type="button" onClick={() => setAcik((v) => !v)}
          className="flex w-full items-center gap-1.5 text-left text-xs font-medium text-slate-500 hover:text-brand-600 dark:text-slate-400 dark:hover:text-brand-400 transition">
          <svg className={`h-3.5 w-3.5 shrink-0 transition-transform ${acik ? 'rotate-90' : ''}`}
            fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
          </svg>
          <span>{cevir("Kurulum günlüğü")}</span>
          <span className="text-slate-400 dark:text-slate-500">({ku.adimlar.length} {cevir("adım · tamamlandı")})</span>
        </button>
      ) : (
        <p className="text-xs font-medium text-slate-500 dark:text-slate-400 mb-2">{cevir("Kurulum adımları")}</p>
      )}

      {/*
        🔴 UYARILAR ASLA KAYBOLMAZ. Adım listesi katlandığında `!` satırları özetin
        İÇİNDE kalır — örn. "ikili düz metin paketten kuruldu (şifreli paket yok)".
        Bunları da gizleseydik müşteri kurulumundaki eksiği HİÇ öğrenemezdi; kart
        "her şey yolunda" der, gerçek başka olurdu.
      */}
      {katlanabilir && uyarilar.length > 0 && (
        <div className="mt-2 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 space-y-1">
          {uyarilar.map((a, i) => (
            <div key={i} className="text-xs text-amber-700 dark:text-amber-400">
              <span className="font-mono mr-2">!</span>
              <span className="font-medium">{a.etiket}</span>
              {a.mesaj && <div className="mt-0.5 ml-6 whitespace-pre-wrap break-words">{a.mesaj}</div>}
            </div>
          ))}
        </div>
      )}

      {acik && (
        <ul className={`space-y-1.5 ${katlanabilir ? 'mt-2' : ''}`}>
          {ku.adimlar.map((a, i) => (
            <li key={i} className="text-xs">
              <span className={`font-mono mr-2 ${renk[a.durum] || 'text-slate-400'}`}>{isaret[a.durum] || '·'}</span>
              <span className="text-slate-700 dark:text-slate-200">{a.etiket}</span>
              {a.sure && <span className="text-slate-400 dark:text-slate-500"> ({a.sure})</span>}
              {a.mesaj && (a.durum === 'hata' || a.durum === 'uyari') && (
                <div className={`mt-0.5 ml-6 whitespace-pre-wrap break-words ${renk[a.durum]}`}>{a.mesaj}</div>
              )}
            </li>
          ))}
        </ul>
      )}

      {ku.durum === 'hata' && (
        <p className="mt-2 text-xs text-rose-700 dark:text-rose-300">
          {cevir("Kurulum tamamlanamadı, eklenti ETKİNLEŞTİRİLMEDİ. Hatayı giderdikten sonra lisansı tekrar girip yeniden deneyin.")}
        </p>
      )}
    </div>
  )
}

function KartGovde({ k, ...e }: { k: Kart } & Eylemler) {
  return (
    <div className="rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-5 flex flex-col">
      <div className="flex items-start gap-3">
        <Ikon d={k.ikon} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <Link to={`/eklentiler/${k.slug}`} className="text-base font-semibold text-slate-900 dark:text-slate-100 hover:text-brand-600 dark:hover:text-brand-400 transition">
              {k.ad}
            </Link>
            <Rozet k={k} />
          </div>
          <p className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mt-0.5">{k.kategori}</p>
        </div>
      </div>

      <p className="mt-3 text-sm text-slate-600 dark:text-slate-300 flex-1">{k.kisa}</p>
      <LisansOzet l={k.lisans} />
      {k.kurulum && k.kurulum.durum !== 'yok' && k.kurulum.adimlar.length > 0 && <Adimlar ku={k.kurulum} />}

      <div className="mt-4 flex items-center justify-between gap-3">
        <Link to={`/eklentiler/${k.slug}`} className="text-sm text-brand-600 dark:text-brand-400 hover:underline">{cevir("Ayrıntılar")}</Link>
        <Eylem k={k} {...e} />
      </div>
    </div>
  )
}

function Detay({ k, ...e }: { k: Kart } & Eylemler) {
  return (
    <div className="rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 overflow-hidden">
      <div className="p-6 border-b border-slate-200 dark:border-slate-700">
        <div className="flex items-start gap-4">
          <Ikon d={k.ikon} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{k.ad}</h1>
              <Rozet k={k} />
            </div>
            <p className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mt-0.5">
              {k.kategori}{k.fiyat && ` · ${k.fiyat}`}{k.surum && cevirT(cevir(" · sürüm {0}"), k.surum)}
            </p>
          </div>
        </div>
        <p className="mt-4 text-sm text-slate-600 dark:text-slate-300">{k.kisa}</p>
        <LisansOzet l={k.lisans} />
        {k.kurulum && k.kurulum.durum !== 'yok' && k.kurulum.adimlar.length > 0 && <Adimlar ku={k.kurulum} />}
        <div className="mt-4"><Eylem k={k} {...e} /></div>
        {!k.kurulur && (
          <p className="mt-3 text-xs text-amber-600 dark:text-amber-400">
            {cevir(cevir("Not: bu panel sürümünde bu eklenti için otomatik kurulum yordamı bulunmuyor."))}
          </p>
        )}
      </div>

      <div className="p-6 grid gap-6 md:grid-cols-3">
        <div className="md:col-span-2">
          <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-2">{cevir("Açıklama")}</h2>
          <div className="text-sm text-slate-600 dark:text-slate-300 space-y-3">
            {(k.uzun || k.kisa).split('\n\n').map((p, i) => <p key={i}>{p}</p>)}
          </div>
        </div>
        <div>
          <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-2">{cevir("Özellikler")}</h2>
          <ul className="space-y-1.5">
            {(k.ozellikler || []).map((o, i) => (
              <li key={i} className="flex gap-2 text-sm text-slate-600 dark:text-slate-300">
                <svg className="w-4 h-4 mt-0.5 flex-shrink-0 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                </svg>
                <span>{o}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}
