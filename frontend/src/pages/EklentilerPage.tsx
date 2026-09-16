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
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'

type Adim = { ad: string; etiket: string; durum: string; mesaj: string; sure: string }
type KurulumDurum = {
  eklenti: string
  durum: string // calisiyor | tamam | hata | yok
  hata: string
  adimlar: Adim[]
  basladi: string
  bitti: string
  // Kurucunun ÇALIŞTIRMAYI PLANLADIĞI adım sayısı (kurulum.go: Kurulum.Toplam).
  // Opsiyonel: eski panel ikilisi bu alanı göndermez, o zaman ilerleme çubuğu
  // yüzde yerine belirsiz kipe düşer — uydurma yüzde göstermez.
  toplam?: number
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
  // suite: bu lisans TEK LISANS ('*') kaydindan geliyor, yani eklentinin
  // kendi anahtari degil — tum eklentileri kapsayan anahtar.
  suite?: boolean
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
  /** Ücretsiz, panelle birlikte gelir; lisans sunucusunda ürünü yok. */
  cekirdek?: boolean
  /** Eklenti proxy'si yalnız yöneticiye açık. */
  sadece_admin?: boolean
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
  "Mail Sunucu": "Mail Server",
  "Uygulama Çalıştırıcı": "Application Runner",
  "Geliştirici": "Developer",
  "Node.js ve Python uygulamalarını panelden yayına alın: git'ten çekin, derleyin, atomik sürüm geçişiyle canlıya alın.": "Deploy Node.js and Python apps from the panel: pull from git, build, and go live with atomic version switching.",
  "Marka (Whitelabel)": "Branding (Whitelabel)",
  "Görünüm": "Appearance",
  "Paneli kendi markanızla sunun: ad, logo, tarayıcı sekmesi başlığı, vurgu rengi ve giriş sayfası metinleri.": "Present the panel under your own brand: name, logo, browser tab title, accent color and login-page text.",
  "Panelinize ek yetenekler kazandıran lisanslı modüller. Kurmak istediğinizde Kur'a, kullanmak istediğinizde Yapılandır'a basın.": "Licensed modules that add capabilities to your panel — press Install to add one, Configure to use it.",
  "E-POSTA": "E-MAIL",
  "Panelinize tam donanımlı e-posta sunucusu ekler: posta kutuları, takma adlar, spam filtresi ve giden IP havuzu.": "Adds a full-featured email server to your panel: mailboxes, aliases, spam filter and outgoing IP pool.",
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
  "Yapılandır": "Configure",
  "Lisansı Yenile": "Renew License",
  "Lisansınızı girin": "Enter your license",
  "Tek lisans, tüm eklentiler": "One license, all add-ons",
  "Lisans anahtarınızı girdiğinizde bu panelde yayınlanan tüm eklentiler açılır. Hangisini kuracağınıza siz karar verirsiniz — lisans girmek hiçbir şey kurmaz.": "Entering your license key unlocks every add-on published for this panel. You decide which ones to install — entering a license installs nothing.",
  "Lisans anahtarı": "License key",
  "Lisansı Kaydet": "Save License",
  "Kaydediliyor…": "Saving…",
  "Lisansınız yok mu?": "No license yet?",
  "Bu panelde şu eklentiler var:": "This panel offers:",
  "Kur": "Install",
  "Kuruluyor…": "Installing…",
  "Yeniden Etkinleştir": "Re-enable",
  "Devre Dışı Bırak": "Disable",
  "Ücretsiz — panelle birlikte gelir": "Free — included with the panel",
  "Etkin": "Enabled",
  "Kurulum başladı.": "Installation started.",
  "Kurulum başlatılamadı": "Installation could not be started",
  "Lisans kapsamında": "Covered by license",
  "Bu panel sürümünde kurulamıyor.": "Cannot be installed on this panel version.",
  "Lisansınız yayınlanan tüm eklentileri kapsıyor. Kurmak istediğiniz eklentide Kur'a basmanız yeterli.": "Your license covers every published add-on. Just press Install on the one you want.",
  "Lisansı Kaldır": "Remove License",
  "her şey yolunda": "all is well",
  "ikili düz metin paketten kuruldu (şifreli paket yok)": "binary installed from plaintext package (no encrypted package)",
  "Ücretsiz 1 Ay Dene": "Try Free for 1 Month",
  "Şu anda kurulabilecek bir eklenti bulunmuyor.": "No add-on is currently available to install.",
  "← Eklentilere dön": "← Back to add-ons",
  "Emin misiniz?": "Are you sure?",
  "\"{0}\" lisansı kaldırılacak ve eklenti kapatılacak.": "The \"{0}\" license will be removed and the add-on turned off.",
  "\"{0}\" eklentisi devre dışı bırakılacak.": "The \"{0}\" add-on will be disabled.",
  "Ayarlarınız SİLİNMEZ — aynı karttaki \"Yeniden Etkinleştir\" ile kaldığı yerden devam eder.": "Your settings are NOT deleted — use \"Re-enable\" on this same card to resume where you left off.",
  "Eklenti devre dışı bırakıldı. Ayarlarınız silinmedi.": "Add-on disabled. Your settings were not deleted.",
  "Posta kutularınız, alan adlarınız ve ayarlarınız SİLİNMEZ — lisansı tekrar girdiğinizde kaldığı yerden devam eder.": "Your mailboxes, domains and settings are NOT deleted — when you enter the license again it resumes where it left off.",
  "Devam edilsin mi?": "Continue?",
  "Anasayfa": "Homepage",
  "Eklentiler": "Add-ons",
  "Tekrar dene": "Try again",
  "Sunucu Kodu": "Server Code",
  "Lisans alabilmek için bu sunucunun hesabınıza kayıtlı olması gerekir.": "To obtain a license, this server must be registered to your account.",
  "Kopyalandı ✓": "Copied ✓",
  "Kopyala": "Copy",
  "Doğrulanıyor…": "Verifying…",
  "İşleniyor…": "Processing…",
  "Anahtar:": "Key:",
  " (deneme)": " (trial)",
  "Son doğrulama:": "Last verification:",
  "Not:": "Note:",
  "adım · tamamlandı": "steps · completed",
  "Kurulum ilerlemesi": "Installation progress",
  "{0}. adım": "Step {0}",
  "{0} / {1} adım": "{0} / {1} steps",
  "Küçült": "Minimise",
  "Büyüt": "Expand",
  "Kapat": "Close",
  "Kurulum tamamlandı": "Installation complete",
  "Kurulum sürüyor": "Installation running",
  "Kurulum başarısız": "Installation failed",
  "Ayrıntılı günlük": "Detailed log",
  "{0} kurulum sürüyor": "{0} installations running",
  "Kurulum tamamlanamadı, eklenti ETKİNLEŞTİRİLMEDİ. Hatayı giderdikten sonra lisansı tekrar girip yeniden deneyin.": "Installation could not be completed, the add-on was NOT activated. After fixing the error, enter the license again and retry.",
  "Adımlar tekrarlanabilir: yeniden denemek zaten yapılmış işi bozmaz.": "The steps are repeatable: retrying does not undo work already done.",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (EKL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// AYAR_YOLU — eklentinin core'daki yapılandırma ekranı.
//
// 🔴 Burada, backend'de DEĞİL: rota core'un kendi bilgisidir. Katalog
// ürününe "ayar yolu" alanı eklemek, backend'i frontend rotalarına bağımlı
// kılardı — rota değişince API sözleşmesi değişirdi.
//
// Yolu OLMAYAN eklenti "Yapılandır" göstermez (yalnız Ayrıntılar): olmayan
// bir sayfaya götüren düğme, çalışmayan bir düğmedir.
const AYAR_YOLU: Record<string, string> = {
  mail: '/mail-sunucu',
  whitelabel: '/marka',
  tehdit: '/tehdit',
}

function tarih(s: string): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleDateString(i18n.language === 'en' ? 'en-US' : 'tr-TR', { day: '2-digit', month: 'long', year: 'numeric' })
}

function kurulumSuruyor(k: KatalogYanit | null): boolean {
  return !!k?.urunler?.some(u => u.kurulum?.durum === 'calisiyor')
}

// Panelde gösterilecek kurulumlar. Aynı anda BİRDEN FAZLA olabilir: kilit
// eklenti başınadır (kurulum.go: KurulumSuruyorMu), farklı iki eklenti
// eşzamanlı kurulabilir — bu yüzden panel tek kurulum değil LİSTE gösterir.
function kurulumListesi(k: KatalogYanit | null): Array<{ kart: Kart; ku: KurulumDurum }> {
  return (k?.urunler || [])
    .filter(u => u.kurulum && u.kurulum.durum !== 'yok' && u.kurulum.adimlar.length > 0)
    .map(u => ({ kart: u, ku: u.kurulum as KurulumDurum }))
}

// İlerleme ölçüsü. `toplam` yoksa YÜZDE HESAPLANMAZ.
//
// 🔴 Adımlar append-only gelir: `adimlar.length` "şimdiye kadar başlamış adım"
// demektir, "toplam" değil. bitmis/adimlar.length ile hesaplasaydık çubuk
// kurulum boyunca hep ~%95-100 gösterirdi — yani yalan söylerdi. Toplam
// bilinmiyorsa belirsiz kipe düşülür ve yalnız sayaç gösterilir.
function ilerleme(ku: KurulumDurum) {
  const bitmis = ku.adimlar.filter(a => a.durum !== 'calisiyor').length
  const toplam = ku.toplam || 0
  // bitmis > toplam: kurucuya adım eklenmiş ama sayı güncellenmemiş demektir
  // (sunucu logu da uyarır). Yanlış yüzde göstermektense belirsize düş.
  const belirsiz = toplam <= 0 || bitmis > toplam
  const bitti = ku.durum === 'tamam'
  const yuzde = belirsiz ? 0 : bitti ? 100 : Math.min(99, Math.round((bitmis / toplam) * 100))
  const suren = ku.adimlar.find(a => a.durum === 'calisiyor')
  return { bitmis, toplam, belirsiz, yuzde, suren }
}

// 🔴 BİTMİŞ kurulumda süre `bitti - basladi`dır, `now - basladi` DEĞİL.
// Yoklama kurulum bitince duruyor, ama bileşen başka sebeplerle yeniden
// çizildiğinde sayaç işlemeye devam ediyordu: üç gün açık kalan bir sekmede
// "Kurulum tamamlandı · 4320d 0s" görünürdü.
function gecenSure(basladi: string, bitti?: string): string {
  if (!basladi) return ''
  const t0 = Date.parse(basladi)
  if (!Number.isFinite(t0)) return ''
  const t1 = bitti ? Date.parse(bitti) : NaN
  const son = Number.isFinite(t1) ? t1 : Date.now()
  const sn = Math.max(0, Math.round((son - t0) / 1000))
  if (sn < 60) return `${sn}s`
  const dk = Math.floor(sn / 60)
  if (dk < 60) return `${dk}d ${sn % 60}s`
  return `${Math.floor(dk / 60)}sa ${dk % 60}d`
}

// KurulumIzi — KARTTA kalan kalıcı iz.
//
// 🔴 Adım listesi karttan panele taşındı (kart her adımda uzayıp ızgarayı
// zıplatıyordu), ama UYARILAR VE HATA KARTTA KALIR. Panel kapatılabilir; iz
// yalnız panelde olsaydı müşteri "ikili düz metin paketten kuruldu" gibi bir
// eksiği ya da başarısızlığın SEBEBİNİ hiç öğrenemezdi. Yükseklik sınırlı:
// izgara yine zıplamasın.
function KurulumIzi({ ku }: { ku: KurulumDurum }) {
  const uyarilar = ku.adimlar.filter(a => a.durum === 'uyari')
  const hatali = ku.adimlar.find(a => a.durum === 'hata')
  if (ku.durum === 'calisiyor' || (uyarilar.length === 0 && ku.durum !== 'hata')) return null
  return (
    <div className="mt-3 space-y-1.5">
      {ku.durum === 'hata' && (
        <div className="rounded border border-rose-500/40 bg-rose-500/10 px-2 py-1.5 text-xs text-rose-700 dark:text-rose-300">
          <span className="font-medium">{cevir("Kurulum başarısız")}</span>
          {(hatali?.mesaj || ku.hata) && (
            <div className="mt-0.5 line-clamp-3 break-words">{hatali?.mesaj || ku.hata}</div>
          )}
        </div>
      )}
      {uyarilar.length > 0 && (
        <div className="rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
          {uyarilar.slice(0, 2).map((a, i) => (
            <div key={i} className="line-clamp-2 break-words">
              <span className="font-mono mr-1.5">!</span>{a.mesaj || a.etiket}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function IlerlemeCubugu({ ku }: { ku: KurulumDurum }) {
  const { belirsiz, yuzde } = ilerleme(ku)
  const hata = ku.durum === 'hata'
  const renk = hata ? 'bg-rose-500' : ku.durum === 'tamam' ? 'bg-emerald-500' : 'bg-brand-600'
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-dark-600"
      role={belirsiz ? undefined : 'progressbar'}
      aria-valuenow={belirsiz ? undefined : yuzde}
      aria-valuemin={belirsiz ? undefined : 0}
      aria-valuemax={belirsiz ? undefined : 100}>
      {/* Belirsiz kip: yüzde bilinmiyor. Dolu bir çubuk göstermek yalan olurdu;
          kayan dar bir şerit "çalışıyor ama ne kadar kaldığı bilinmiyor" der. */}
      {belirsiz && !hata && ku.durum !== 'tamam'
        ? <div className={`h-full w-1/3 animate-pulse rounded-full ${renk}`} />
        : <div className={`h-full rounded-full transition-[width] duration-500 ${renk}`}
            style={{ width: `${belirsiz ? 100 : yuzde}%` }} />}
    </div>
  )
}

// KurulumPaneli — sağ altta SABİT pencere.
//
// 🔴 NEDEN TAŞINDI: adım listesi kartın içindeydi ve kurulum boyunca 0'dan ~29
// satıra büyüyordu. Kartlar CSS Grid çocuğu olduğu için satırdaki DİĞER kartlar
// da uzuyor, üstelik 2,5 saniyelik yoklamayla bu sürekli tekrarlanıyordu —
// sayfa boyunca düzen kayması. Panel kartların dışında, kendi yüksekliği
// sınırlı bir katmanda yaşar; kart artık büyümez.
function KurulumPaneli({ kurulumlar, kucuk, setKucuk, onGizle, onYukseklik }: {
  kurulumlar: Array<{ kart: Kart; ku: KurulumDurum }>
  kucuk: boolean
  setKucuk: (v: boolean) => void
  onGizle: (ad: string) => void
  onYukseklik: (px: number) => void
}) {
  const [acikGunluk, setAcikGunluk] = useState<string | null>(null)
  // 🔴 SAYFA DOLGUSU ÖLÇÜLÜR, TAHMİN EDİLMEZ. Sabit bir pb-40 (160px)
  // verilmişti; panelin gövdesi ise max-h-[60vh] ve günlük açılınca daha da
  // büyüyor. İki kurulum listelendiğinde ya da günlük açıldığında ızgaranın
  // son satırındaki "Ayrıntılar"/"Kur" düğmeleri yine örtülüyordu — yani
  // düzeltilen şikâyet yer değiştirmiş oluyordu.
  const kutuRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const el = kutuRef.current
    if (!el) { onYukseklik(0); return }
    if (typeof ResizeObserver === 'undefined') { onYukseklik(el.offsetHeight); return }
    const ro = new ResizeObserver(() => onYukseklik(el.offsetHeight))
    ro.observe(el)
    onYukseklik(el.offsetHeight)
    return () => ro.disconnect()
  })
  useEffect(() => () => onYukseklik(0), [onYukseklik])
  if (kurulumlar.length === 0) return null
  const suren = kurulumlar.filter(x => x.ku.durum === 'calisiyor').length

  return (
    // z-[90]: Modal z-[100] ve Toast z-[140] ALTINDA kalmalı — lisans penceresi
    // açıkken onun üstünü örtmesin.
    <div ref={kutuRef} className="fixed bottom-4 right-4 z-[90] w-[22rem] max-w-[calc(100vw-2rem)]
                    rounded-xl border border-slate-200 bg-white shadow-lg
                    dark:border-dark-600 dark:bg-dark-800"
      /* 🔴 CANLI BÖLGE KÖKE KONMAZ. aria-live pencerenin tamamındaydı; içinde
         2,5 saniyede bir değişen saniye sayacı ve açıkken 29 satırlık günlük
         vardı — ekran okuyucu kurulum boyunca sürekli kesilip her turda tüm
         pencereyi yeniden okuyordu. Canlı olan tek şey DURUM METNİdir. */
      role="region" aria-label={cevir("Kurulum ilerlemesi")}>
      <div className="flex items-center gap-2 border-b border-slate-200 px-3 py-2 dark:border-dark-600">
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-slate-900 dark:text-slate-100">
          {cevir("Kurulum ilerlemesi")}
          {suren > 0 && <span className="ml-1 text-slate-400">({suren})</span>}
        </span>
        <button type="button" onClick={() => setKucuk(!kucuk)}
          aria-label={kucuk ? cevir("Büyüt") : cevir("Küçült")}
          className="rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600
                     focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500
                     dark:hover:bg-dark-600 dark:hover:text-slate-200">
          <svg className={`h-4 w-4 transition-transform ${kucuk ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden>
            <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>

      {!kucuk && (
        <div className="max-h-[60vh] space-y-3 overflow-y-auto p-3">
          {kurulumlar.map(({ kart, ku }) => {
            const { bitmis, toplam, belirsiz, suren: adim } = ilerleme(ku)
            const bitti = ku.durum !== 'calisiyor'
            return (
              <div key={kart.eklenti_ad} className="space-y-1.5">
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-sm text-slate-800 dark:text-slate-100">{cevir(kart.ad)}</span>
                  <span className="shrink-0 text-xs tabular-nums text-slate-400">
                    {belirsiz
                      ? cevirT(cevir("{0}. adım"), String(bitmis + (adim ? 1 : 0)))
                      : cevirT(cevir("{0} / {1} adım"), String(bitmis), String(toplam))}
                  </span>
                  {/* Bitmiş kurulum kapatılabilir; SÜREN kurulum kapatılamaz —
                      kullanıcı ilerlemeyi kaybetmesin. */}
                  {bitti && (
                    <button type="button" onClick={() => onGizle(kart.eklenti_ad)}
                      aria-label={cevir("Kapat")}
                      className="shrink-0 rounded p-0.5 text-slate-400 hover:text-slate-600
                                 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500
                                 dark:hover:text-slate-200">
                      <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  )}
                </div>
                <IlerlemeCubugu ku={ku} />
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate text-xs text-slate-500 dark:text-slate-400"
                    aria-live="polite">
                    {ku.durum === 'hata' ? cevir("Kurulum başarısız")
                      : ku.durum === 'tamam' ? cevir("Kurulum tamamlandı")
                        : (adim?.etiket || cevir("Kurulum sürüyor"))}
                  </span>
                  {/* Sayaç canlı bölgenin DIŞINDA: duyurulacak bilgi değil. */}
                  <span className="shrink-0 text-xs tabular-nums text-slate-400" aria-hidden>{gecenSure(ku.basladi, ku.bitti)}</span>
                </div>

                <button type="button"
                  onClick={() => setAcikGunluk(acikGunluk === kart.eklenti_ad ? null : kart.eklenti_ad)}
                  className="text-xs text-brand-600 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500 dark:text-brand-400">
                  {cevir("Ayrıntılı günlük")}
                </button>
                {acikGunluk === kart.eklenti_ad && (
                  <ul className="max-h-52 space-y-1 overflow-y-auto rounded bg-slate-50 p-2 dark:bg-dark-900">
                    {ku.adimlar.map((a, i) => (
                      <li key={i} className="text-xs">
                        <span className={`mr-1.5 font-mono ${
                          a.durum === 'tamam' ? 'text-emerald-600 dark:text-emerald-400'
                            : a.durum === 'calisiyor' ? 'text-sky-600 dark:text-sky-400'
                              : a.durum === 'uyari' ? 'text-amber-600 dark:text-amber-400'
                                : 'text-rose-600 dark:text-rose-400'}`}>
                          {a.durum === 'tamam' ? '✓' : a.durum === 'calisiyor' ? '…' : a.durum === 'uyari' ? '!' : '×'}
                        </span>
                        <span className="text-slate-700 dark:text-slate-200">{a.etiket}</span>
                        {a.sure && <span className="text-slate-400"> ({a.sure})</span>}
                        {a.mesaj && (a.durum === 'hata' || a.durum === 'uyari') && (
                          <div className="ml-5 mt-0.5 whitespace-pre-wrap break-words text-slate-500 dark:text-slate-400">{a.mesaj}</div>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
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

  // İşlem geri bildirimi (modal/aksiyon sonuçları) — sağ üst toast ile gösterilir.
  const toast = useToast()
  const [islem, setIslem] = useState<string | null>(null)      // süren işlem: eklenti_ad
  const [islemHata, setIslemHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  // Kurulum sürerken 2,5 sn'de bir yoklanır: aynı hata tekrar tekrar toast'lanmasın.
  const sonHataRef = useRef<string | null>(null)

  // Lisans anahtarı modalı
  const [modalKart, setModalKart] = useState<Kart | null>(null)
  // Sağ alt kurulum paneli. Kurulum durumu için AYRI state açılmaz — kaynak
  // katalog yanıtıdır; buradakiler yalnız kullanıcının panel tercihleridir.
  const [panelKucuk, setPanelKucuk] = useState(false)
  // Sabit panelin ölçülen yüksekliği; sayfanın alt dolgusu buna bağlanır.
  const [panelYuksek, setPanelYuksek] = useState(0)
  // Kullanıcının kapattığı BİTMİŞ kurulumlar. Süren kurulum gizlenemez.
  const [gizlenen, setGizlenen] = useState<string[]>([])

  // Panelde gösterilecekler.
  //
  // 🔴 GİZLEME LİSTESİ TEMİZLENİR. Filtre süren kurulumu zaten gösteriyordu
  // ama ad listeden hiç çıkmıyordu: kullanıcı başarısız bir kurulumu kapatıp
  // yeniden başlattığında, kurulum bittiği ANDA sonuç satırı ekrandan
  // kayboluyordu — yani sonucu göremiyordu. Süren kurulum görülünce ad düşer.
  useEffect(() => {
    const surenler = kurulumListesi(katalog)
      .filter(x => x.ku.durum === 'calisiyor')
      .map(x => x.kart.eklenti_ad)
    if (surenler.length === 0) return
    setGizlenen(g => g.some(a => surenler.includes(a)) ? g.filter(a => !surenler.includes(a)) : g)
  }, [katalog])

  const panelKurulumlari = kurulumListesi(katalog)
    .filter(x => x.ku.durum === 'calisiyor' || !gizlenen.includes(x.kart.eklenti_ad))
    // 🔴 BAŞARILI ve ESKİ kurulum panelden düşer. Sunucu tarafındaki oturum
    // kaydı panel yeniden başlayana kadar duruyor; kullanıcının "Kapat"ı ise
    // yalnız bileşen state'inde. Aksi halde saatler önce biten bir kurulumun
    // penceresi her sayfa açılışında geri geliyordu. HATA hiç yaşlanmaz:
    // etkinleşmemiş bir eklenti kendiliğinden gözden kaybolmamalı.
    .filter(x => {
      if (x.ku.durum !== 'tamam' || !x.ku.bitti) return true
      const t = Date.parse(x.ku.bitti)
      return !Number.isFinite(t) || Date.now() - t < 30 * 60 * 1000
    })
  const [anahtar, setAnahtar] = useState('')

  // Lisans giriş ekranı (eklentiden BAĞIMSIZ tek kutu)
  const [girisAnahtar, setGirisAnahtar] = useState('')
  const [girisMesgul, setGirisMesgul] = useState(false)
  const [girisHata, setGirisHata] = useState<string | null>(null)

  const ilkRef = useRef(true)

  const cek = useCallback((sessiz = false) => {
    if (!sessiz) setYuk(true)
    return api.get<KatalogYanit>('/eklenti-katalog')
      .then(r => {
        setKatalog(r.data)
        setHata(null)
        sonHataRef.current = null
      })
      .catch(e => {
        // 🔴 Hata durumunda ESKİ veriyi "temiz" göstermeyiz; hata kutusu çıkar.
        const m = apiHata(e, cevir("Eklenti kataloğu alınamadı"))
        setHata(m)
        if (sonHataRef.current !== m) { sonHataRef.current = m; toast.hata(cevir("İşlem başarısız"), m) }
        if (ilkRef.current) setKatalog(null)
      })
      .finally(() => { setYuk(false); ilkRef.current = false })
  }, [])

  // lisansKaydet — anahtarı KAYDEDER, hiçbir şey KURMAZ.
  //
  // 🔴 Kurulum tetiklemez: kullanıcı bu noktada hangi eklentiyi istediğini
  // henüz seçmemiştir. Eski akış lisans girer girmez o kartın eklentisini
  // kuruyordu; istemediği bir kurulumu iptal etmenin yolu yoktu.
  const lisansKaydet = useCallback(async () => {
    const a = girisAnahtar.trim()
    if (!a) { setGirisHata(cevir("Lisans anahtarı boş olamaz.")); return }
    setGirisMesgul(true); setGirisHata(null); setBasari(null)
    try {
      const { data } = await api.post('/lisans', { anahtar: a })
      setGirisAnahtar('')
      const m = data?.mesaj || cevir("Lisans doğrulandı.")
      setBasari(m)
      toast.basari(m)
      await cek(true)
    } catch (e) {
      setGirisHata(apiHata(e, cevir("Lisans doğrulanamadı")))
    } finally {
      setGirisMesgul(false)
    }
  }, [girisAnahtar, cek])

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

  // 🔴 Anahtar GONDERILMEZ. Panel kayitli tek-lisans anahtarini kullanir;
  // kullaniciya ayni anahtari ikinci kez yazdirmak anlamsiz olurdu.
  async function eklentiKur(k: Kart) {
    const ad = k.eklenti_ad
    setIslem(ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${ad}/lisans`, {})
      const m = data?.mesaj || cevir("Kurulum başladı.")
      setBasari(m)
      toast.basari(m)
      await cek(true)
    } catch (e) {
      const m = apiHata(e, cevir("Kurulum başlatılamadı"))
      setIslemHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIslem(null)
    }
  }

  async function lisansGonder() {
    if (!modalKart) return
    const ad = modalKart.eklenti_ad
    setIslem(ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${ad}/lisans`, { anahtar })
      const m = data?.mesaj || cevir("Lisans doğrulandı, kurulum başladı.")
      setBasari(m)
      toast.basari(m)
      setModalKart(null); setAnahtar('')
      await cek(true)
    } catch (e) {
      const m = apiHata(e, cevir("Lisans doğrulanamadı"))
      setIslemHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIslem(null)
    }
  }
  async function lisansDogrula(k: Kart) {
    setIslem(k.eklenti_ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/eklenti/${k.eklenti_ad}/lisans-dogrula`, {})
      // 🔴 "belirsiz" (ağ hatası) BAŞARI DEĞİLDİR — ayrı gösterilir.
      if (data?.karar === 'gecerli') {
        const m = data.mesaj || cevir("Lisans geçerli.")
        setBasari(m)
        toast.basari(m)
      } else {
        const m = data?.mesaj || cevir("Lisans doğrulanamadı.")
        setIslemHata(m)
        toast.hata(cevir("İşlem başarısız"), m)
      }
      await cek(true)
      eklentiDegisti()
    } catch (e) {
      const m = apiHata(e, cevir("Lisans doğrulanamadı"))
      setIslemHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIslem(null)
    }
  }

  async function lisansKaldir(k: Kart) {
    // 🔴 ÇEKİRDEK üründe (katalog.go: Cekirdek=true) kaldırılacak bir LİSANS
    // YOKTUR — cp_eklenti_lisans'ta kendi satırı bile olmaz — ve posta kutusu
    // / alan adı diye nesneleri de yoktur; bunlar mail eklentisinin
    // kavramlarıdır. Kartta çıkan düğme "Devre Dışı Bırak", geri dönüş yolu da
    // lisans girmek DEĞİL aynı kartta beliren "Yeniden Etkinleştir"dir.
    // Mail'e özel metni burada göstermek kullanıcıyı olmayan bir lisans
    // ekranına yönlendiriyordu; onay metnini bu yüzden dala ayırıyoruz.
    const govde = k.cekirdek
      ? cevirT(cevir('"{0}" eklentisi devre dışı bırakılacak.'), k.ad) + '\n\n' +
        cevir('Ayarlarınız SİLİNMEZ — aynı karttaki "Yeniden Etkinleştir" ile kaldığı yerden devam eder.')
      : cevirT(cevir('"{0}" lisansı kaldırılacak ve eklenti kapatılacak.'), k.ad) + '\n\n' +
        cevir('Posta kutularınız, alan adlarınız ve ayarlarınız SİLİNMEZ — lisansı tekrar girdiğinizde kaldığı yerden devam eder.')
    if (!(await onay({ baslik: cevir('Emin misiniz?'),
      mesaj: govde + '\n\n' + cevir('Devam edilsin mi?'), tehlike: true }))) return
    setIslem(k.eklenti_ad); setIslemHata(null); setBasari(null)
    try {
      const { data } = await api.delete(`/eklenti/${k.eklenti_ad}/lisans`)
      // Sunucunun başarı metni de mail'e özeldir (handlers.go:589-590 — "Posta
      // kutularınız ve ayarlarınız silinmedi; lisansı tekrar girdiğinizde…").
      // Çekirdek üründe onu olduğu gibi basmak, onay kutusunda düzelttiğimiz
      // yanlışı toast'ta tekrar etmek olurdu; bu yüzden kendi metnimizi
      // kullanırız. Lisanslı eklentilerde davranış aynen korunur.
      const m = k.cekirdek
        ? cevir("Eklenti devre dışı bırakıldı. Ayarlarınız silinmedi.")
        : (data?.mesaj || cevir("Lisans kaldırıldı."))
      setBasari(m)
      toast.basari(m)
      await cek(true)
      eklentiDegisti()
    } catch (e) {
      const m = apiHata(e, cevir("Lisans kaldırılamadı"))
      setIslemHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIslem(null)
    }
  }

  const urunler = katalog?.urunler || []
  const secili = slug ? urunler.find(u => u.slug === slug) : undefined

  // 🔴 "Lisans var mı" = HERHANGİ bir ürünün lisansı var mı.
  //
  // Tek lisans modelinde suite satırı tüm kartlara yayıldığı için tek bir
  // kartın `lisans.var` olması yeterlidir. Katalog HENÜZ YÜKLENMEDİYSE
  // (`katalog === null`) giriş ekranını göstermeyiz: lisansı olan bir
  // kullanıcıya bir anlığına "lisansınızı girin" demek, olmayan bir sorunu
  // varmış gibi gösterir.
  const lisansVar = urunler.some(k => k.lisans.var)
  const girisEkrani = !!katalog && !secili && !hata && !lisansVar

  // 🔴 ÇEKİRDEK ürünler GİRİŞ EKRANINDA DA ızgarada gösterilir.
  //
  // Çekirdek ürün ücretsizdir ve backend onun için `cp_eklenti_lisans` satırı
  // AÇMAZ (handlers.go çekirdek kısa devresi). Dolayısıyla hiç lisans girilmemiş
  // taze bir panelde HER kartta `lisans.var === false` olur, `girisEkrani`
  // true'ya düşer ve kart ızgarası hiç render edilmezdi. "Kur" düğmesi ile
  // ayrıntı bağlantısı YALNIZCA kartın içinde olduğundan, ücretsiz Marka
  // (whitelabel) eklentisi ancak ücretli bir anahtar girilerek kurulabiliyordu;
  // kartın içindeki çekirdek muafiyeti (`kapsamda`) ölü koda dönüşmüştü.
  //
  // Giriş ekranında ızgarayı ÇEKİRDEKLE SINIRLARIZ: ücretli kartları burada da
  // göstermek "tek kutu, tek karar" akışını geri bozardı.
  //
  // 🔴 GÜNCELLEME: Çekirdek (Marka) artık main.go cekirdekEklentiGuvence ile
  // panel açılışında OTOMATİK kurulur → taze panelde `kurulu === true` gelir.
  // Kurulu çekirdeği giriş ekranında lisans kutusunun ALTINDA ayrı bir kart
  // olarak GÖSTERMEYİZ; mail/app-runner gibi yalnız kendi NAV yolundan (/marka)
  // ve panel lisanslandıktan sonra ızgarada yönetilir. Kart YALNIZ oto-kurulum
  // BAŞARISIZ olduysa (kurulu === false) fallback olarak çıkar — böylece panel
  // yine de kurulumsuz kilitlenmez.
  const izgaraUrunler = girisEkrani ? urunler.filter(u => u.cekirdek && !u.kurulu) : urunler

  return (
    // 🔴 Panel açıkken ALT DOLGU. Sabit pencere sağ altta duruyor; dolgu
    // olmasaydı ızgaranın son satırındaki kartın "Ayrıntılar"/"Kur" düğmelerini
    // örterdi — düzen şikâyeti yer değiştirmiş olurdu.
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-6xl mx-auto"
      style={panelYuksek > 0 ? { paddingBottom: panelYuksek + 32 } : undefined}>
      <Breadcrumb items={
        secili
          ? [{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir('Eklentiler'), href: '/eklentiler' }, { etiket: secili.ad }]
          : [{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Sunucu Yönetimi") }, { etiket: cevir('Eklentiler') }]
      } />

      {!secili && (
        <>
          <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">{cevir("Eklentiler")}</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
            {girisEkrani
              ? cevir("Tek lisans, tüm eklentiler")
              : cevir("Panelinize ek yetenekler kazandıran lisanslı modüller. Kurmak istediğinizde Kur'a, kullanmak istediğinizde Yapılandır'a basın.")}
          </p>
        </>
      )}

      {/* Sunucu Kodu (kullanıcı isteğiyle kaldırıldı — lisans ekranı sade). */}
      {katalog?.uyari && (
        <div className="mb-4 rounded-lg border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 px-4 py-3 text-sm text-amber-800 dark:text-amber-200">
          {katalog.uyari}
        </div>
      )}
      {/* ── Yükleniyor ── */}
      {yuk && !katalog && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1].map(i => (
            <div key={i} className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 p-5 animate-pulse">
              <div className="h-10 w-10 rounded-lg bg-slate-200 dark:bg-dark-600 mb-4" />
              <div className="h-4 w-2/5 bg-slate-200 dark:bg-dark-600 rounded mb-2" />
              <div className="h-3 w-full bg-slate-200 dark:bg-dark-600 rounded mb-1.5" />
              <div className="h-3 w-4/5 bg-slate-200 dark:bg-dark-600 rounded" />
            </div>
          ))}
        </div>
      )}

      {/* ── Hata (asla sessizce boş göstermeyiz) ── */}
      {hata && (
        <div className="rounded-lg border border-rose-200 dark:border-rose-800 bg-rose-50 dark:bg-rose-900/20 p-5">
          <p className="text-sm font-medium text-rose-800 dark:text-rose-200">{cevir("Eklenti kataloğu yüklenemedi")}</p>
          <p className="text-sm text-rose-700 dark:text-rose-300 mt-1">{hata}</p>
          <Button color="error" onClick={() => cek()} className="mt-3 px-3 py-1.5 text-sm">
            {cevir("Tekrar dene")}
          </Button>
        </div>
      )}

      {/* ── Boş ── */}
      {!yuk && !hata && urunler.length === 0 && (
        <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 p-10 text-center">
          <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Şu anda kurulabilecek bir eklenti bulunmuyor.")}</p>
        </div>
      )}

      {/* ── Detay ── */}
      {!hata && slug && !yuk && !secili && (
        <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 p-10 text-center">
          <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Böyle bir eklenti bulunamadı.")}</p>
          <Link to="/eklentiler" className="mt-3 inline-block text-sm text-brand-600 dark:text-brand-400 hover:underline">{cevir("← Eklentilere dön")}</Link>
        </div>
      )}
      {secili && (
        <Detay
          k={secili}
          mesgul={islem === secili.eklenti_ad}
          onLisans={() => { setModalKart(secili); setIslemHata(null) }}
          onKaldir={() => lisansKaldir(secili)}
          onDogrula={() => lisansDogrula(secili)}
          onKur={() => eklentiKur(secili)}
        />
      )}

      {/* ── LİSANS GİRİŞ EKRANI ──────────────────────────────────────────
          Lisans yokken ÜCRETLİ kart ızgarası GÖSTERİLMEZ (ücretsiz çekirdek
          ürünler hariç — bkz. `izgaraUrunler`). Eskiden her kartta ayrı bir
          "Lisansı Gir" vardı; kullanıcı aynı anahtarı her eklenti için tekrar
          giriyor ve girer girmez o eklenti kuruluyordu. Tek kutu, tek karar. */}
      {girisEkrani && (
        <div className="rounded-lg border border-slate-200 bg-white p-6 sm:p-8 dark:border-dark-600 dark:bg-dark-700">
          <h2 className="text-lg font-semibold text-slate-800 dark:text-slate-100">
            {cevir("Lisansınızı girin")}
          </h2>
          <p className="mt-2 text-sm text-slate-600 dark:text-slate-300 max-w-2xl">
            {cevir("Lisans anahtarınızı girdiğinizde bu panelde yayınlanan tüm eklentiler açılır. Hangisini kuracağınıza siz karar verirsiniz — lisans girmek hiçbir şey kurmaz.")}
          </p>

          <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:items-start">
            <div className="flex-1">
              <label htmlFor="lisans-anahtar" className="sr-only">{cevir("Lisans anahtarı")}</label>
              <input
                id="lisans-anahtar"
                value={girisAnahtar}
                onChange={e => { setGirisAnahtar(e.target.value); setGirisHata(null) }}
                onKeyDown={e => { if (e.key === 'Enter' && !girisMesgul) lisansKaydet() }}
                placeholder="GOSP-XXXX-XXXX-XXXX-XXXX"
                autoComplete="off"
                spellCheck={false}
                className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2.5 font-mono text-sm text-slate-800 placeholder:text-slate-400 focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-500/30 dark:border-slate-600 dark:bg-dark-800 dark:text-slate-100"
              />
              {girisHata && (
                <p role="alert" className="mt-2 text-sm text-rose-600 dark:text-rose-400">{girisHata}</p>
              )}
            </div>
            <Button
              color="primary"
              onClick={lisansKaydet}
              disabled={girisMesgul || !girisAnahtar.trim()}
              className="shrink-0 px-5 py-2.5 text-sm"
            >
              {girisMesgul ? cevir("Kaydediliyor…") : cevir("Lisansı Kaydet")}
            </Button>
          </div>

          {/* Lisansın neyi açtığını GÖSTER: boş bir kutuya anahtar girmek
              istemek için önce karşılığını görmek gerekir. */}
          {urunler.length > 0 && (
            <div className="mt-7 border-t border-slate-200 pt-5 dark:border-dark-600">
              <p className="text-xs font-medium uppercase tracking-wide text-slate-500 dark:text-slate-400">
                {cevir("Bu panelde şu eklentiler var:")}
              </p>
              <ul className="mt-3 grid gap-3 sm:grid-cols-2">
                {urunler.map(u => (
                  <li key={u.slug} className="flex gap-3">
                    <span className="mt-0.5 shrink-0 text-brand-600 dark:text-brand-400"><Ikon d={u.ikon} /></span>
                    <span>
                      <span className="block text-sm font-medium text-slate-800 dark:text-slate-100">{u.ad}</span>
                      <span className="block text-xs text-slate-500 dark:text-slate-400">{u.kisa}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Ürün başına "Ücretsiz 1 Ay Dene" bağlantıları KALDIRILDI: ürün
              modeli tek anahtarlı suite'e geçti (bir anahtar tüm eklentileri
              açar), dolayısıyla eklenti eklenti deneme başlatmak akışa aykırı
              ve kafa karıştırıcıydı. Deneme kanalı ürün kartının kendi
              ayrıntı görünümünde duruyor. */}
        </div>
      )}

      {/* ── Kart ızgarası ── */}
      {!secili && !hata && izgaraUrunler.length > 0 && (
        <div className={`grid gap-4 sm:grid-cols-2 lg:grid-cols-3${girisEkrani ? ' mt-6' : ''}`}>
          {izgaraUrunler.map(k => (
            <KartGovde
              key={k.slug}
              k={k}
              mesgul={islem === k.eklenti_ad}
              onLisans={() => { setModalKart(k); setIslemHata(null) }}
              onKaldir={() => lisansKaldir(k)}
              onDogrula={() => lisansDogrula(k)}
              onKur={() => eklentiKur(k)}
            />
          ))}
        </div>
      )}

      {/* ── Kurulum ilerlemesi — sağ alt sabit pencere ── */}
      <KurulumPaneli
        kurulumlar={panelKurulumlari}
        kucuk={panelKucuk}
        setKucuk={setPanelKucuk}
        onGizle={(ad) => setGizlenen(g => g.includes(ad) ? g : [...g, ad])}
        onYukseklik={setPanelYuksek}
      />

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
            className="w-full px-3 py-2 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-sm font-mono text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
          />
          {islemHata && <p className="mt-2 text-sm text-rose-600 dark:text-rose-400">{islemHata}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <Button type="button" variant="outlined" onClick={() => { setModalKart(null); setAnahtar('') }}
              className="px-3 py-2 text-sm">
              {cevir("Vazgeç")}
            </Button>
            <Button type="submit" color="primary" disabled={!anahtar.trim() || !!islem}
              className="px-4 py-2 text-sm">
              {islem ? cevir('Doğrulanıyor…') : cevir("Doğrula ve Kur")}
            </Button>
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
  // 🔴 ÇEKİRDEK ÜRÜN ücretsizdir; suite lisansı onu da "kapsıyor" görünse bile
  // "Lisanslı ve etkin" ya da "Lisans kapsamında" demek kataloğun kendi
  // vaadiyle ("Ücretsiz — panelle birlikte gelir") çelişir ve kullanıcıyı
  // satın alınacak bir şey olduğu izlenimine sokar. Bu yüzden lisans
  // dallarından ÖNCE gelir.
  if (k.cekirdek) {
    return k.aktif
      ? <Etiket renk="emerald">{cevir("Etkin")}</Etiket>
      : <Etiket renk="slate">{cevir("Ücretsiz — panelle birlikte gelir")}</Etiket>
  }
  // 🔴 Lisans kapsaminda ama HENUZ KURULU DEGIL — "etkin" DEMEK YANLIS olur.
  // Bu dal `aktif` kontrolunden ONCE gelir: kurulmamis eklenti aktif olamaz,
  // dolayisiyla asagidaki dala hic ulasamaz ve rozetsiz kalirdi.
  if (k.lisans.var && k.lisans.suite && !k.kurulu) {
    return <Etiket renk="sky">{cevir("Lisans kapsamında")}</Etiket>
  }
  if (k.aktif && k.lisans.var) {
    return <Etiket renk="emerald">{k.lisans.deneme ? cevir('Deneme sürümü etkin') : cevir("Lisanslı ve etkin")}</Etiket>
  }
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
    slate: 'bg-slate-50 text-slate-600 border-slate-200 dark:bg-dark-600/40 dark:text-slate-300 dark:border-slate-600',
  }
  return <span className={`inline-flex items-center px-2 py-0.5 rounded-full border text-[11px] font-medium ${m[renk] || m.slate}`}>{children}</span>
}

type Eylemler = { mesgul: boolean; onLisans: () => void; onKaldir: () => void; onDogrula: () => void; onKur: () => void }

function Eylem({ k, mesgul, onLisans, onKaldir, onDogrula, onKur }: { k: Kart } & Eylemler) {
  if (k.kurulum?.durum === 'calisiyor') {
    return <span className="text-sm text-sky-600 dark:text-sky-400">{cevir("Kurulum sürüyor…")}</span>
  }
  // 🔴 TEK LISANS: kapsamda ama HENUZ KURULU DEGIL → "Kur".
  //
  // Backend artik suite lisansini tum eklentiler icin dolduruyor. Bunu tek
  // basina birakirsak kurulmamis bir eklentide "Lisansı Doğrula / Lisansı
  // Kaldır" cikardi — kullanicinin kurmadigi bir seyi kaldirmasi teklif
  // edilirdi. Kurulu olup olmamasi belirleyici.
  // `k.kurulur` SART: bu panel surumunde kurucu yordami olmayan bir eklenti
  // icin "Kur" gostermek, basildiginda "kurulum yordamı yok" hatasi veren bir
  // dugme sunmak demektir. Lisans kapsamda oldugu rozetten zaten anlasilir.
  // 🔴 `suite` şartı KALDIRILDI. Lisans hangi yoldan gelirse gelsin (suite
  // satırı ya da ürünün kendi satırı) anahtar zaten kayıtlıdır; kullanıcıdan
  // tekrar istemenin bir sebebi yok. Ayırt edici olan KURULU OLUP OLMAMASI.
  // 🔴 ÇEKİRDEK ÜRÜNLERİN lisans satırı YOKTUR ama kurulabilirler; kapsam
  // ölçütü bu yüzden "lisans var VEYA çekirdek".
  const kapsamda = k.lisans.var || !!k.cekirdek

  // Kurulabilir = kurucusu var VE (hiç kurulu değil YA DA kurulu ama pasif ve
  // çekirdek).
  //
  // İkinci koşul "KURULU AMA aktif=0" ÇIKMAZINI açar: aktif=0 üç yoldan gelir
  // (devre dışı bırakma, bütünlük zorlaması, nabız) ve satır durduğu için
  // kurulu=true kalır. Bu koşul olmadan kart ne "Kur" dalını (şart: !kurulu)
  // ne de çalışan "Yapılandır"ı gösterir; kullanıcının hiçbir geri dönüş yolu
  // kalmaz. Bilinçli olarak ÇEKİRDEK ile sınırlandırıldı: aynı kapıyı mail'e
  // açmak, onun kurulumunun yeniden çalıştırılabilir olduğunu KANITSIZ
  // varsaymak olurdu.
  const kurulabilir = k.kurulur && (!k.kurulu || (!k.aktif && !!k.cekirdek))

  if (kapsamda && kurulabilir) {
    return (
      <div className="flex flex-wrap gap-2">
        <Button color="primary" onClick={onKur} disabled={mesgul}
          className="px-3 py-1.5 text-[13px]">
          {mesgul ? cevir('Kuruluyor…') : (k.kurulu ? cevir("Yeniden Etkinleştir") : cevir("Kur"))}
        </Button>
      </div>
    )
  }
  if (kapsamda && !k.kurulu) {
    return (
      <span className="text-sm text-slate-500 dark:text-slate-400">
        {cevir("Bu panel sürümünde kurulamıyor.")}
      </span>
    )
  }
  // KURULU + ETKİN + kendi ekranı var → YAPILANDIR. `k.aktif` şarttır: pasif
  // eklentinin arayüz paketi zaten 404 döner, "Yapılandır" çalışmayan bir
  // düğme olurdu.
  if (kapsamda && k.kurulu && k.aktif && AYAR_YOLU[k.eklenti_ad]) {
    return (
      <div className="flex flex-wrap gap-2">
        <Link to={AYAR_YOLU[k.eklenti_ad]}
          className="px-3 py-1.5 text-[13px] rounded-lg bg-brand-600 hover:bg-brand-700 text-white transition">
          {cevir("Yapılandır")}
        </Link>
        {/* Çekirdek üründe doğrulanacak bir lisans yoktur; onun yerine geri
            alma yolu sunulur (bugün whitelabel kartında hiç yoktu). */}
        {k.cekirdek ? (
          <Button color="error" variant="outlined" onClick={onKaldir} disabled={mesgul}
            className="px-3 py-1.5 text-[13px]">
            {cevir("Devre Dışı Bırak")}
          </Button>
        ) : (
          <Button variant="outlined" onClick={onDogrula} disabled={mesgul}
            className="px-3 py-1.5 text-[13px]">
            {mesgul ? cevir('İşleniyor…') : cevir("Lisansı Doğrula")}
          </Button>
        )}
      </div>
    )
  }
  if (k.lisans.var) {
    return (
      <div className="flex flex-wrap gap-2">
        <Button variant="outlined" onClick={onDogrula} disabled={mesgul}
          className="px-3 py-1.5 text-[13px]">
          {mesgul ? cevir('İşleniyor…') : cevir("Lisansı Doğrula")}
        </Button>
        {/* Suite lisansinda tek eklentinin "lisansini kaldirmak" anlamsiz:
            anahtar hepsini kapsiyor. Bunun yerine eklenti kaldirilir. */}
        {!k.lisans.suite && (
          <Button color="error" variant="outlined" onClick={onKaldir} disabled={mesgul}
            className="px-3 py-1.5 text-[13px]">
            {cevir("Lisansı Kaldır")}
          </Button>
        )}
      </div>
    )
  }
  // 🔴 Buraya normalde HİÇ GELİNMEZ: lisans yokken kart ızgarası değil
  // lisans giriş ekranı gösterilir. Yine de sessiz kalmak yerine kullanıcıyı
  // doğru yere yönlendiririz (ör. lisans süresi dolmuş bir kart).
  return (
    <div className="flex flex-wrap gap-2">
      <Button color="primary" onClick={onLisans} disabled={mesgul}
        className="px-3 py-1.5 text-[13px]">
        {cevir("Lisansı Yenile")}
      </Button>
    </div>
  )
}

function Ikon({ d }: { d: string }) {
  return (
    <div className="w-11 h-11 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center shrink-0">
      <svg className="w-6 h-6 text-brand-600 dark:text-brand-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.6}>
        <path strokeLinecap="round" strokeLinejoin="round" d={d || 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4'} />
      </svg>
    </div>
  )
}

// 🔴 Rutin lisans satirlari (Anahtar / Bitis tarihi / Son dogrulama) KALDIRILDI:
// tek-anahtarli suite modelinde ayni anahtar ve ayni bitis tarihi HER kartta
// tekrar ediyordu; bitis zaten panelde tek yerde gorunuyor. Kartlar bilgi
// tekrariyla doluyordu.
//
// Ama SORUN sinyalleri KALIR: son_hata (dogrulama hatasi) ve kopya uyarisi
// sessizce yutulursa musteri askiya alinmis/bozuk lisansi hic fark etmez.
// Sorun yoksa bilesen hicbir sey cizmez — kart temiz kalir.
function LisansOzet({ l }: { l: LisansDurum }) {
  if (!l.var) return null
  if (!l.son_hata && !l.uyari) return null
  return (
    <div className="mt-3 space-y-1 text-xs">
      {l.son_hata && (
        <div className="rounded-lg border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-rose-700 dark:text-rose-400">
          {cevir("Not:")} {l.son_hata}
        </div>
      )}
      {/*
        Kopya uyarisi. Kirmizi DEGIL kehribar: hizmet calisiyor, lisans gecerli.
        Kirmiziya boyasaydik musteri calisan kurulumunu arizali sanardi. Metin
        lisans sunucusundan gelir; burada yorumlanmaz.
      */}
      {l.uyari && (
        <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-amber-700 dark:text-amber-400">
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
    <div className="mt-3 rounded-lg border border-slate-200 dark:border-dark-600 bg-slate-50 dark:bg-dark-800/50 p-3">
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
          {/* Yeniden deneme tüm adımları baştan koşturur; kullanıcı bunu
              "her şey yeniden mi kurulacak?" diye okuyup çekinmesin. */}
          <span className="block mt-1 text-slate-500 dark:text-slate-400">
            {cevir("Adımlar tekrarlanabilir: yeniden denemek zaten yapılmış işi bozmaz.")}
          </span>
        </p>
      )}
    </div>
  )
}

function KartGovde({ k, ...e }: { k: Kart } & Eylemler) {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 p-5 flex flex-col">
      <div className="flex items-start gap-3">
        <Ikon d={k.ikon} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <Link to={`/eklentiler/${k.slug}`} className="text-base font-semibold text-slate-900 dark:text-slate-100 hover:text-brand-600 dark:hover:text-brand-400 transition">
              {cevir(k.ad)}
            </Link>
            <Rozet k={k} />
          </div>
          <p className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mt-0.5">{cevir(k.kategori)}</p>
        </div>
      </div>

      <p className="mt-3 text-sm text-slate-600 dark:text-slate-300 flex-1">{cevir(k.kisa)}</p>
      <LisansOzet l={k.lisans} />
      {/* Canlı adım listesi SAĞ ALT PANELE taşındı (kart her adımda uzayıp
          ızgarayı zıplatıyordu). Kartta yalnız kalıcı iz kalır: uyarılar ve
          başarısızlık sebebi. */}
      {k.kurulum && k.kurulum.durum !== 'yok' && k.kurulum.adimlar.length > 0 && <KurulumIzi ku={k.kurulum} />}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <Link to={`/eklentiler/${k.slug}`} className="text-sm text-brand-600 dark:text-brand-400 hover:underline">{cevir("Ayrıntılar")}</Link>
        {/* shrink-0 + ust kap flex-wrap: dar kartta dugmeler ICERIDE alt alta
            sarmak yerine kap kendi satirina iner, tam genislikte YAN YANA kalir. */}
        <div className="shrink-0"><Eylem k={k} {...e} /></div>
      </div>
    </div>
  )
}

function Detay({ k, ...e }: { k: Kart } & Eylemler) {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 overflow-hidden">
      <div className="p-6 border-b border-slate-200 dark:border-dark-600">
        <div className="flex items-start gap-4">
          <Ikon d={k.ikon} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{cevir(k.ad)}</h1>
              <Rozet k={k} />
            </div>
            <p className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500 mt-0.5">
              {cevir(k.kategori)}{k.fiyat && ` · ${k.fiyat}`}{k.surum && cevirT(cevir(" · sürüm {0}"), k.surum)}
            </p>
          </div>
        </div>
        <p className="mt-4 text-sm text-slate-600 dark:text-slate-300">{cevir(k.kisa)}</p>
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
                <svg className="w-4 h-4 mt-0.5 shrink-0 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
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
