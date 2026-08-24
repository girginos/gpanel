import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import Breadcrumb from '@/components/Breadcrumb'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import { T } from '@/lib/tablo'

/*
 * Site Taşıma — cPanel / Plesk / DirectAdmin'den bu panele uçtan uca aktarım.
 * 3 adımlı sihirbaz:
 *   1) Sunucu bilgileri + kontrol paneli + bağlantı testi
 *   2) Site listesi (tekil/toplu) + sahiplik + aktarım ayarları
 *   3) Taşıma — canlı durum, ETA, hangi domainin taşındığı
 * İş sunucuda çalışır; sayfa kapansa/yenilense bile devam eder.
 */

type Hesap = {
  kaynak_hesap: string; alan_adi: string; web_root: string; php_surum: string
  dbler: string[] | null; boyut_mb: number; not: string; mevcut: boolean
}
type Kalem = {
  id: number; kaynak_hesap: string; alan_adi: string
  durum: 'bekliyor' | 'calisiyor' | 'tamam' | 'hata' | 'atlandi'
  domain_id: number; dosya_bayt: number; db_sayisi: number; dns_sayisi: number; hata: string
}
type Is = {
  id: number; tip: string; host: string; mod: string; durum: string
  toplam: number; tamamlanan: number; basarisiz: number; baslatan: string; olusturma: string
}
type Plan = { id: number; ad: string }
type Bayi = { id: number; kullanici: string; ad_soyad?: string }
type Musteri = { id: number; ad: string }

// Su an YALNIZ Plesk destekleniyor (uctan uca test edildi). cPanel/DA backend'de
// hazir ama gercek sunucuda dogrulanmadigi icin UI'dan gecici olarak kaldirildi.
const PANELLER = [
  { deger: 'plesk', etiket: 'Plesk' },
]
const PHP_SURUMLERI = ['', '7.4', '8.0', '8.1', '8.2', '8.3', '8.4']


const STASIMA_EN: Record<string, string> = {
  "(seçtiğiniz panel ile uyuşmuyor!)": "(does not match the panel you selected!)",
  "+ Yeni taşıma": "+ New migration",
  "Anahtar hazırlanıyor…": "Preparing key…",
  "Bayiler yüklenemedi": "Failed to load resellers",
  "Bağlantı testi başarısız": "Connection test failed",
  "Bağlantıyı test et": "Test connection",
  "Dump alınır, aktarılır, ayarlar güncellenir": "Dumped, transferred, settings updated",
  "En az bir site seçin.": "Select at least one site.",
  "Geçmiş taşımalar": "Past migrations",
  "Kaldığınız yerden devam edin": "Continue where you left off",
  "Kaynak sunucuda taşınabilir site bulunamadı.": "No migratable sites found on the source server.",
  "Kaynak zone okunur, A kayıtları çevrilir": "Source zone is read, A records are translated",
  "Kaynaktaki reseller/müşteri hesapları planlarıyla oluşturulur; her site kendi sahibine atanır (Plesk). Reseller parolaları başlatınca gösterilir.": "Reseller/customer accounts on the source are created with their plans; each site is assigned to its own owner (Plesk). Reseller passwords are shown when you start.",
  "Kaynaktakiyle aynı": "Same as the source",
  "Kayıt bekleniyor…": "Waiting for records…",
  "Keşif başarısız": "Discovery failed",
  "Mail (yakında)": "Mail (coming soon)",
  "Müşteriler yüklenemedi": "Failed to load customers",
  "Neyi yeniden taşıyalım?": "What should we migrate again?",
  "Neyin taşınacağını, hangi plana ve kime atanacağını seçin.": "Choose what will be migrated, to which plan and to whom it will be assigned.",
  "Oturum geri yüklenemedi": "Failed to restore session",
  "Panel anahtarı (parola çalışmıyorsa)": "Panel key (if the password doesn't work)",
  "Panel anahtarı — kaynak sunucuda tek komut çalıştırın": "Panel key — run a single command on the source server",
  "Panelde aynı alan adı varsa üzerine yazar": "Overwrites if the same domain exists in the panel",
  "Planlar yüklenemedi": "Failed to load plans",
  "Plansız (limit yok)": "No plan (no limits)",
  "SQL (Veritabanı)": "SQL (Database)",
  "SSH anahtarı": "SSH key",
  "SSH kullanıcısı": "SSH user",
  "SSH özel anahtarı": "SSH private key",
  "SSL sertifikası": "SSL certificate",
  "Posta (kutular + mesajlar)": "Mail (mailboxes + messages)",
  "Kutular oluşturulur, mail verisi taşınır, parolalar korunur": "Mailboxes created, mail data transferred, passwords preserved",
  "Mail": "Mail",
  "Sahiplik ve aktarım ayarları": "Ownership and transfer settings",
  "Site sahiplerini taşı (bayi/müşteri)": "Migrate site owners (reseller/customer)",
  "Siteler taranıyor…": "Scanning sites…",
  "Siteleri keşfet →": "Discover sites →",
  "Taşıma başlatılamadı": "Failed to start migration",
  "Taşınacak siteler": "Sites to migrate",
  "Web kök dizini rsync ile aktarılır": "The web root is transferred via rsync",
  "hazırlanıyor…": "preparing…",
  "parola girişini kapatmış": "has disabled password login",
  "İptal edilemedi": "Could not cancel",
  "🔑 Oluşturulan sahip hesapları — parolaları KAYDEDİN": "🔑 Created owner accounts — SAVE the passwords",
  "“Mevcut siteyi ez” açık — aynı alan adına sahip sitelerin dosya ve veritabanları üzerine yazılacak. Önce yedek önerilir.": "“Overwrite existing site” is on — files and databases of sites with the same domain will be overwritten. A backup first is recommended.",
  "Türkçe": "English",
  "Bekliyor": "Waiting",
  "Aktarılıyor": "Transferring",
  "Tamamlandı": "Completed",
  "Hata": "Error",
  "Atlandı": "Skipped",
  "İptal edildi": "Cancelled",
  "Kesildi": "Interrupted",
  "Sunucu bilgileri": "Server details",
  "Siteler & Ayarlar": "Sites & Settings",
  "dk": "min",
  "sn": "sec",
  "sunucu": "server",
  "bilinmiyor": "unknown",
  "Anasayfa": "Home",
  "parola saklı": "password saved",
  "Devam et": "Continue",
  "Oturumu unut": "Forget session",
  "Kaynak sunucu bilgileri": "Source server details",
  "Kontrol paneli": "Control panel",
  "Sunucu adresi (IP veya host)": "Server address (IP or host)",
  "1.2.3.4 veya sunucu.alanadi.com": "1.2.3.4 or server.domain.com",
  "SSH portu": "SSH port",
  "Gizle": "Hide",
  "Kaynak sunucu": "The source server",
  " olabilir (Plesk'te sık). Bunun yerine panel kendi anahtarını kullanır — parola gerekmez.": " may have (common in Plesk). Instead the panel uses its own key — no password needed.",
  "kaynak sunucuda": "on the source server",
  "bir kez": "once",
  "kaynak": "source",
  "Kopyalandı ✓": "Copied ✓",
  "Kopyala": "Copy",
  "Test ediliyor…": "Testing…",
  "site bulundu": "sites found",
  "seçili": "selected",
  "Temizle": "Clear",
  "Kaynak hesap": "Source account",
  "Panelde var": "Exists in panel",
  "Yeni": "New",
  "Dosyalar": "Files",
  "Let's Encrypt denenir": "Let's Encrypt is attempted",
  "Mevcut siteyi ez": "Overwrite existing site",
  "Hedef PHP": "Target PHP",
  "Bayi (sahip)": "Reseller (owner)",
  "Ana hesap (admin)": "Main account (admin)",
  "Yok": "None",
  "Dikkat:": "Warning:",
  "Taşımayı başlat": "Start migration",
  "Tip": "Type",
  "Durdur": "Stop",
  "Tekrar dene": "Retry",
  "Tahmini kalan": "Estimated remaining",
  "Toplam": "Total",
  "Tamamlanan": "Completed",
  "tamamlandı": "completed",
  "Kaynak": "Source",
  "hata": "error",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (STASIMA_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function mb(n: number) { if (!n) return '—'; return n < 1024 ? `${n} MB` : `${(n / 1024).toFixed(1)} GB` }
function bayt(n: number) { if (!n) return '—'; const m = n / (1024 * 1024); return m < 1024 ? `${m.toFixed(1)} MB` : `${(m / 1024).toFixed(2)} GB` }
function sure(sn: number) {
  if (!isFinite(sn) || sn <= 0) return '—'
  const dk = Math.floor(sn / 60), s = Math.round(sn % 60)
  return dk > 0 ? `${dk} ${cevir("dk")} ${s} ${cevir("sn")}` : `${s} ${cevir("sn")}`
}

const DURUM_ETIKET: Record<string, string> = {
  bekliyor: 'Bekliyor', calisiyor: 'Aktarılıyor', tamam: 'Tamamlandı',
  hata: 'Hata', atlandi: 'Atlandı', iptal: 'İptal edildi', kesildi: 'Kesildi',
}

const inputCls =
  'w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-900 ' +
  'placeholder:text-slate-400 transition focus:border-brand-400 focus:outline-none focus:ring-2 ' +
  'focus:ring-brand-500/15 disabled:cursor-not-allowed disabled:opacity-60 ' +
  'dark:border-slate-700 dark:bg-slate-900/60 dark:text-slate-100'
const labelCls = 'mb-1.5 block text-xs font-medium text-slate-500 dark:text-slate-400'
const selCls =
  'rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-sm text-slate-900 focus:border-brand-400 ' +
  'focus:outline-none focus:ring-2 focus:ring-brand-500/15 dark:border-slate-700 dark:bg-slate-900/60 dark:text-slate-100'
const btnBirincil =
  'inline-flex items-center justify-center gap-2 rounded-full bg-slate-900 px-5 py-2.5 text-sm font-medium ' +
  'text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-40 ' +
  'dark:bg-white dark:text-slate-900 dark:hover:bg-slate-100'
const btnIkincil =
  'inline-flex items-center justify-center gap-2 rounded-full border border-slate-200 px-4 py-2.5 text-sm ' +
  'font-medium text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40 ' +
  'dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800'
const btnKucuk =
  'inline-flex items-center gap-1.5 rounded-full border border-slate-200 px-3 py-1.5 text-xs font-medium ' +
  'text-slate-600 transition hover:bg-slate-50 disabled:opacity-40 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800'

function Ikon({ d }: { d: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7"
      strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><path d={d} /></svg>
  )
}
const I = {
  check: 'M20 6 9 17l-5-5',
  mail: 'M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zM22 7l-10 6L2 7',
  yenile: 'M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5',
  files: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6',
  db: 'M4 6c0-1.66 3.58-3 8-3s8 1.34 8 3-3.58 3-8 3-8-1.34-8-3M4 6v12c0 1.66 3.58 3 8 3s8-1.34 8-3V6M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3',
  dns: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20M2 12h20M12 2c2.5 2.7 3.9 6.3 4 10-.1 3.7-1.5 7.3-4 10-2.5-2.7-3.9-6.3-4-10 .1-3.7 1.5-7.3 4-10',
  ssl: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10M9 12l2 2 4-4',
  ez: 'M3 6h18M3 12h18M3 18h18',
  sunucu: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01',
  ok: 'M5 12h14M13 6l6 6-6 6',
  geri: 'M19 12H5M11 18l-6-6 6-6',
  uyari: 'M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z',
  saat: 'M12 6v6l4 2M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20',
  panel: 'M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83zM2 12.5l8.58 3.91a2 2 0 0 0 1.66 0L21 12.5M2 17l8.58 3.91a2 2 0 0 0 1.66 0L21 17',
  kullaniciIkon: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 0 0 0-8 4 4 0 0 0 0 8z',
  kilit: 'M19 11H5a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7a2 2 0 0 0-2-2zM7 11V7a5 5 0 0 1 10 0v4',
  hash: 'M4 9h16M4 15h16M10 3 8 21M16 3l-2 18',
  anahtarIkon: 'M2.6 17.4A2 2 0 0 0 2 18.8V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 0 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 0 1-1h.2a2 2 0 0 0 1.4-.6l.8-.8a6.5 6.5 0 1 0-4-4z',
  goz: 'M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  gozKapali: 'M9.9 9.9a3 3 0 1 0 4.2 4.2M10.7 5.1A11 11 0 0 1 12 5c7 0 10 7 10 7a13 13 0 0 1-1.7 2.7M6.6 6.6A13 13 0 0 0 2 12s3 7 10 7a11 11 0 0 0 5.4-1.4M2 2l20 20',
}

function DurumRozet({ durum }: { durum: string }) {
  const stil =
    durum === 'tamam' ? 'border-emerald-200 text-emerald-700 bg-emerald-50 dark:border-emerald-800/60 dark:text-emerald-300 dark:bg-emerald-900/20'
    : durum === 'hata' ? 'border-red-200 text-red-700 bg-red-50 dark:border-red-800/60 dark:text-red-300 dark:bg-red-900/20'
    : durum === 'calisiyor' ? 'border-brand-200 text-brand-700 bg-brand-50 dark:border-brand-800/60 dark:text-brand-300 dark:bg-brand-900/20'
    : 'border-slate-200 text-slate-600 bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:bg-slate-800'
  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${stil}`}>
      {durum === 'calisiyor' && <span className="h-2.5 w-2.5 shrink-0 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />}
      {cevir(DURUM_ETIKET[durum] || durum)}
    </span>
  )
}

function RetrySecim({ etiket, ikon, secili, degis, pasif }: { etiket: string; ikon: string; secili: boolean; degis: () => void; pasif?: boolean }) {
  return (
    <button type="button" disabled={pasif} onClick={degis}
      className={`flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-sm transition ${pasif ? 'opacity-40 cursor-not-allowed' : secili ? 'bg-brand-50 text-brand-700 dark:bg-brand-900/20 dark:text-brand-300' : 'text-slate-700 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-slate-800'}`}>
      <span className="flex items-center gap-2"><Ikon d={ikon} /> {etiket}</span>
      <span className={`flex h-4 w-4 items-center justify-center rounded border ${secili ? 'border-brand-500 bg-brand-500 text-white' : 'border-slate-300 dark:border-slate-600'}`}>{secili && <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" className="h-2.5 w-2.5"><path d={I.check} /></svg>}</span>
    </button>
  )
}

function Kart({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <section className={`rounded-2xl border border-slate-200/70 bg-white p-5 sm:p-6 dark:border-slate-700/60 dark:bg-slate-800/40 ${className || ''}`}>
      {children}
    </section>
  )
}

const ADIMLAR = ['Sunucu bilgileri', 'Siteler & Ayarlar', 'Taşıma']
function Stepper({ adim, git, erisim }: { adim: number; git: (n: number) => void; erisim: (n: number) => boolean }) {
  return (
    <div className="mb-6 flex items-center">
      {ADIMLAR.map((t, i) => {
        const no = i + 1
        const durum = no < adim ? 'done' : no === adim ? 'active' : 'todo'
        const acik = erisim(no)
        return (
          <div key={t} className={`flex items-center ${no < 3 ? 'flex-1' : ''}`}>
            <button type="button" disabled={!acik} onClick={() => acik && git(no)}
              className="flex items-center gap-2.5 disabled:cursor-default">
              <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold transition ${
                durum === 'done' ? 'bg-emerald-500 text-white'
                : durum === 'active' ? 'bg-slate-900 text-white dark:bg-white dark:text-slate-900'
                : 'bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500'}`}>
                {durum === 'done' ? <Ikon d={I.check} /> : no}
              </span>
              <span className={`hidden text-sm font-medium sm:block ${durum === 'todo' ? 'text-slate-400 dark:text-slate-500' : 'text-slate-800 dark:text-slate-100'}`}>{cevir(t)}</span>
            </button>
            {no < 3 && <span className={`mx-3 h-px flex-1 ${no < adim ? 'bg-emerald-400' : 'bg-slate-200 dark:bg-slate-700'}`} />}
          </div>
        )
      })}
    </div>
  )
}

// Kaynak bağlantı bilgileri sayfa yenilenince kaybolmasın diye saklanır.
// 🔴 GÜVENLİK: parola/özel anahtar ASLA saklanmaz (localStorage XSS ile okunur).
const KAYNAK_KEY = 'gosp_tasima_kaynak'
const OTURUM_KEY = 'gosp_tasima_oturum' // son keşif oturumu id'si (sır değil)

type Oturum = { id: number; tip: string; host: string; port: number; kullanici: string; site_sayisi: number; kimlik_sakli: boolean; son_kullanim: string }
type KayitliKaynak = { tip?: string; host?: string; port?: number; kullanici?: string; kimlikTipi?: 'parola' | 'anahtar' | 'panel' }
function kayitliKaynakOku(): KayitliKaynak {
  try { return JSON.parse(localStorage.getItem(KAYNAK_KEY) || '{}') } catch { return {} }
}

export default function SiteTasimaPage() {
  useTranslation() // dil re-render aboneligi
  const kk = kayitliKaynakOku()
  // --- kaynak sunucu ---
  const [tip, setTip] = useState('plesk')
  const [host, setHost] = useState(kk.host || '')
  const [port, setPort] = useState(kk.port || 22)
  const [kullanici, setKullanici] = useState(kk.kullanici || 'root')
  const [kimlikTipi, setKimlikTipi] = useState<'parola' | 'anahtar' | 'panel'>(kk.kimlikTipi || 'parola')
  const [parola, setParola] = useState('')
  const [anahtar, setAnahtar] = useState('')
  // Panel taşıma anahtarı (parola çalışmadığında): kaynağa eklenecek public anahtar + komut.
  const [panelPub, setPanelPub] = useState('')
  const [panelKomut, setPanelKomut] = useState('')
  const [panelKopya, setPanelKopya] = useState(false)
  const [parolaGoster, setParolaGoster] = useState(false)

  // --- akış ---
  const [adim, setAdim] = useState(1)
  const [testSonuc, setTestSonuc] = useState<string | null>(null)
  const [testYuk, setTestYuk] = useState(false)
  const [kesifYuk, setKesifYuk] = useState(false)
  const [hesaplar, setHesaplar] = useState<Hesap[] | null>(null)
  const [secili, setSecili] = useState<Record<string, boolean>>({})
  const [hata, setHata] = useState<string | null>(null)

  // --- ayarlar ---
  const [dosyalar, setDosyalar] = useState(true)
  const [veritabani, setVeritabani] = useState(true)
  const [dns, setDns] = useState(true)
  const [ssl, setSsl] = useState(true)
  const [posta, setPosta] = useState(true)
  const [ustune, setUstune] = useState(false)
  const [sahipleriTasi, setSahipleriTasi] = useState(false)
  // Taşımada oluşturulan reseller/müşteri hesapları (üretilen parolalar admin'e bir kez gösterilir).
  const [uretilenKimlikler, setUretilenKimlikler] = useState<Array<{ tip: string; ad: string; login: string; eposta: string; parola?: string }>>([])
  const [hedefPHP, setHedefPHP] = useState('')
  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [planID, setPlanID] = useState(0)
  const [bayiler, setBayiler] = useState<Bayi[]>([])
  const [musteriler, setMusteriler] = useState<Musteri[]>([])
  const [resellerID, setResellerID] = useState(0)
  const [customerID, setCustomerID] = useState(0)

  // --- çalışan iş ---
  const [isID, setIsID] = useState<number | null>(null)
  const [calisiyor, setCalisiyor] = useState(false)
  const [logMetin, setLogMetin] = useState('')
  const [kalemler, setKalemler] = useState<Kalem[]>([])
  const [ozet, setOzet] = useState({ toplam: 0, tamamlanan: 0, basarisiz: 0, durum: '' })
  const [gecmis, setGecmis] = useState<Is[]>([])
  const [oturumID, setOturumID] = useState<number | null>(null)
  const [oturumlar, setOturumlar] = useState<Oturum[]>([])
  const [kimlikSakli, setKimlikSakli] = useState(false)
  const [retryAcik, setRetryAcik] = useState(false) // oturumdan geldiyse parola sunucuda saklı
  const logRef = useRef<HTMLPreElement>(null)
  const basladiRef = useRef<number | null>(null) // ETA için başlangıç ms

  const kaynakGovde = () => ({
    tip, host: host.trim(), port: Number(port) || 22, kullanici: kullanici.trim(),
    parola: kimlikTipi === 'parola' ? parola : '', anahtar: kimlikTipi === 'anahtar' ? anahtar : '',
    panel_anahtari: kimlikTipi === 'panel',
  })

  // Panel taşıma anahtarını (public + kurulum komutu) getir — 'panel' modu seçilince.
  useEffect(() => {
    if (kimlikTipi !== 'panel' || panelPub) return
    api.get<{ pubkey: string; komut: string }>('/system/tasima/anahtar')
      .then(r => { setPanelPub(r.data.pubkey || ''); setPanelKomut(r.data.komut || '') })
      .catch(() => { /* sessiz */ })
  }, [kimlikTipi, panelPub])

  const durumYukle = useCallback(async () => {
    try {
      const { data } = await api.get<{ isler: Is[]; aktif_is: number }>('/system/tasima')
      setGecmis(data.isler || [])
      if (data.aktif_is) {
        setIsID(data.aktif_is); setCalisiyor(true); setAdim(3)
        if (basladiRef.current === null) basladiRef.current = Date.now()
      }
    } catch { /* panel yeniden başlıyor olabilir */ }
  }, [])

  useEffect(() => { durumYukle() }, [durumYukle])

  // Kaydedilmiş keşif oturumları — sayfa yenilenince sunucu bilgilerini yeniden
  // girmeden kaldığı yerden devam.
  const oturumlarYukle = useCallback(async () => {
    try {
      const { data } = await api.get<{ oturumlar: Oturum[] }>('/system/tasima/oturumlar')
      setOturumlar(data.oturumlar || [])
    } catch { /* yok say */ }
  }, [])
  useEffect(() => { oturumlarYukle() }, [oturumlarYukle])

  async function oturumaGeri(id: number) {
    setHata(null)
    try {
      const { data } = await api.get<{ id: number; tip: string; host: string; port: number; kullanici: string; hesaplar: Hesap[]; secim: string[]; kimlik_sakli: boolean }>(`/system/tasima/oturum/${id}`)
      setTip(data.tip); setHost(data.host); setPort(data.port); setKullanici(data.kullanici)
      const list = data.hesaplar || []
      setHesaplar(list)
      const s: Record<string, boolean> = {}
      if (data.secim && data.secim.length) data.secim.forEach(a => { s[a] = true })
      else list.forEach(h => { s[h.alan_adi] = !h.mevcut })
      setSecili(s)
      setOturumID(data.id); setKimlikSakli(data.kimlik_sakli)
      try { localStorage.setItem(OTURUM_KEY, String(data.id)) } catch { /* */ }
      setAdim(list.length ? 2 : 1)
    } catch (e) { setHata(apiHata(e, cevir("Oturum geri yüklenemedi"))) }
  }

  async function oturumSil(id: number) {
    try { await api.delete(`/system/tasima/oturum/${id}`) } catch { /* */ }
    if (oturumID === id) { setOturumID(null); setKimlikSakli(false); localStorage.removeItem(OTURUM_KEY) }
    oturumlarYukle()
  }

  // Sır-olmayan kaynak alanlarını sakla (parola/anahtar HARİÇ).
  useEffect(() => {
    try { localStorage.setItem(KAYNAK_KEY, JSON.stringify({ tip, host, port, kullanici, kimlikTipi })) } catch { /* */ }
  }, [tip, host, port, kullanici, kimlikTipi])

  // Plan + sahiplik hedefleri.
  useEffect(() => {
    api.get<Plan[]>('/plans').then(r => { const l = r.data || []; setPlanlar(l); if (l.length && !planID) setPlanID(l[0].id) }).catch(hataYakala(cevir("Planlar yüklenemedi")))
    api.get<Bayi[]>('/resellers').then(r => setBayiler(r.data || [])).catch(hataYakala(cevir("Bayiler yüklenemedi")))
    api.get<Musteri[]>('/customers').then(r => setMusteriler(r.data || [])).catch(hataYakala(cevir("Müşteriler yüklenemedi")))

  }, [])

  // Çalışan işi izle (2sn polling).
  useEffect(() => {
    if (!isID) return
    let dur = false
    const tik = async () => {
      try {
        const [l, d] = await Promise.all([
          api.get<{ log: string; calisiyor: boolean; durum: string }>(`/system/tasima/${isID}/log`),
          api.get<{ kalemler: Kalem[]; durum: string; toplam: number; tamamlanan: number; basarisiz: number }>(`/system/tasima/${isID}`),
        ])
        if (dur) return
        setLogMetin(l.data.log || '')
        setKalemler(d.data.kalemler || [])
        setOzet({ toplam: d.data.toplam, tamamlanan: d.data.tamamlanan, basarisiz: d.data.basarisiz, durum: d.data.durum })
        if (!l.data.calisiyor) { setCalisiyor(false); durumYukle() }
      } catch { /* geçici hata — yut */ }
    }
    tik()
    const t = calisiyor ? window.setInterval(tik, 2000) : 0
    return () => { dur = true; if (t) window.clearInterval(t) }
  }, [isID, calisiyor, durumYukle])

  useEffect(() => { logRef.current?.scrollTo({ top: logRef.current.scrollHeight }) }, [logMetin])

  // --- eylemler ---
  async function baglantiTest() {
    setTestYuk(true); setHata(null); setTestSonuc(null)
    try {
      const { data } = await api.post<{ sunucu_adi: string; tespit_edilen: string; uyusuyor: boolean }>('/system/tasima/test', kaynakGovde())
      setTestSonuc(cevirT(cevir("Bağlantı başarılı — {0} · tespit edilen panel: {1}"), data.sunucu_adi || cevir('sunucu'), data.tespit_edilen || cevir('bilinmiyor')) + (data.uyusuyor ? '' : ' ' + cevir('(seçtiğiniz panel ile uyuşmuyor!)')))
    } catch (e) { setHata(apiHata(e, cevir("Bağlantı testi başarısız"))) } finally { setTestYuk(false) }
  }

  async function kesfet() {
    setKesifYuk(true); setHata(null); setHesaplar(null)
    try {
      const { data } = await api.post<{ hesaplar: Hesap[]; oturum_id: number }>('/system/tasima/kesif', kaynakGovde())
      const list = data.hesaplar || []
      setHesaplar(list)
      const s: Record<string, boolean> = {}; list.forEach(h => { s[h.alan_adi] = !h.mevcut }); setSecili(s)
      if (data.oturum_id) { setOturumID(data.oturum_id); setKimlikSakli(kimlikTipi === 'parola' ? !!parola : kimlikTipi === 'anahtar' ? !!anahtar : false); try { localStorage.setItem(OTURUM_KEY, String(data.oturum_id)) } catch { /* */ } }
      oturumlarYukle()
      if (!list.length) setHata(cevir("Kaynak sunucuda taşınabilir site bulunamadı."))
      else setAdim(2)
    } catch (e) { setHata(apiHata(e, cevir("Keşif başarısız"))) } finally { setKesifYuk(false) }
  }

  async function baslat() {
    const secilenler = (hesaplar || []).filter(h => secili[h.alan_adi])
    if (!secilenler.length) { setHata(cevir("En az bir site seçin.")); return }
    setHata(null)
    try {
      const { data } = await api.post<{ is_id: number; uretilen_kimlikler?: Array<{ tip: string; ad: string; login: string; eposta: string; parola?: string }> }>('/system/tasima/baslat', {
        ...kaynakGovde(),
        oturum_id: oturumID || 0, // parola yeniden girilmediyse sunucu bunu kullanır
        mod: secilenler.length === 1 ? 'tekil' : 'toplu',
        ayarlar: { dosyalar, veritabani, dns, ssl, posta, ustune, hedef_php: hedefPHP, plan_id: planID, reseller_id: resellerID, customer_id: customerID, sahipleri_tasi: sahipleriTasi, hesaplar: [] },
        secilen: secilenler,
      })
      basladiRef.current = Date.now()
      setUretilenKimlikler(data.uretilen_kimlikler || [])
      setIsID(data.is_id); setCalisiyor(true); setLogMetin(''); setKalemler([]); setAdim(3)
    } catch (e) { setHata(apiHata(e, cevir("Taşıma başlatılamadı"))) }
  }

  async function iptalEt() {
    if (!isID) return
    try { await api.post(`/system/tasima/${isID}/iptal`, {}) } catch (e) { setHata(apiHata(e, cevir("İptal edilemedi"))) }
  }

  function yeniTasima() {
    setIsID(null); setCalisiyor(false); setKalemler([]); setLogMetin(''); setOzet({ toplam: 0, tamamlanan: 0, basarisiz: 0, durum: '' })
    basladiRef.current = null; setAdim(hesaplar && hesaplar.length ? 2 : 1)
  }

  // Tekrar dene — aynı seçimle yeniden başlat. Kimlik saklı oturumdan (oturum_id)
  // sunucu tarafında çözüldüğü için sunucu bilgilerini yeniden girmeye gerek yok.
  // State yoksa (sayfa yenilendi) forma dön; cevir("Kaldığınız yerden devam edin") ile gelinir.
  async function tekrarDene() {
    if (!(hesaplar && hesaplar.length && secilenSayi > 0)) { setAdim(hesaplar && hesaplar.length ? 2 : 1); return }
    setIsID(null); setCalisiyor(false); setKalemler([]); setLogMetin(''); setOzet({ toplam: 0, tamamlanan: 0, basarisiz: 0, durum: '' })
    basladiRef.current = null
    await baslat()
  }

  const secilenSayi = (hesaplar || []).filter(h => secili[h.alan_adi]).length
  const kilitli = calisiyor
  const done = ozet.tamamlanan + ozet.basarisiz
  const yuzde = ozet.toplam > 0 ? Math.round((done / ozet.toplam) * 100) : 0
  const gecenSn = basladiRef.current ? (Date.now() - basladiRef.current) / 1000 : 0
  const etaSn = calisiyor && done > 0 && basladiRef.current ? (gecenSn / done) * (ozet.toplam - done) : 0
  const aktifKalem = kalemler.find(k => k.durum === 'calisiyor')
  const erisim = (n: number) => n === 1 || (n === 2 && !!(hesaplar && hesaplar.length)) || (n === 3 && !!isID)

  const ayarlar: [string, boolean, (v: boolean) => void, string, string][] = [
    [cevir("Dosyalar"), dosyalar, setDosyalar, cevir("Web kök dizini rsync ile aktarılır"), I.files],
    [cevir("Veritabanları"), veritabani, setVeritabani, cevir("Dump alınır, aktarılır, ayarlar güncellenir"), I.db],
    [cevir("DNS kayıtları"), dns, setDns, cevir("Kaynak zone okunur, A kayıtları çevrilir"), I.dns],
    [cevir("SSL sertifikası"), ssl, setSsl, cevir("Let's Encrypt denenir"), I.ssl],
    [cevir("Posta (kutular + mesajlar)"), posta, setPosta, cevir("Kutular oluşturulur, mail verisi taşınır, parolalar korunur"), I.mail],
    [cevir("Site sahiplerini taşı (bayi/müşteri)"), sahipleriTasi, setSahipleriTasi, cevir("Kaynaktaki reseller/müşteri hesapları planlarıyla oluşturulur; her site kendi sahibine atanır (Plesk). Reseller parolaları başlatınca gösterilir."), I.anahtarIkon],
    [cevir("Mevcut siteyi ez"), ustune, setUstune, cevir("Panelde aynı alan adı varsa üzerine yazar"), I.ez],
  ]

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-6">
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' },
        { etiket: cevir("Site Taşıma") },
      ]} />

      <div className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir("Site Taşıma")}</h1>
        <p className="mt-1.5 max-w-2xl text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          {cevir(cevir("Plesk sunucunuzdaki siteleri dosyaları, veritabanları, DNS ve SSL ile"))}
          {cevir(cevir("birlikte 3 adımda bu panele aktarın. İşlem arka planda çalışır — bu sayfayı kapatabilirsiniz."))}
        </p>
      </div>

      <Stepper adim={adim} git={setAdim} erisim={erisim} />

      {/* Kaydedilmiş oturumlar — sunucu bilgilerini yeniden girmeden devam et */}
      {adim === 1 && oturumlar.length > 0 && (
        <div className="mb-5 rounded-2xl border border-blue-200 bg-blue-50 px-4 py-3 dark:border-blue-900/50 dark:bg-blue-950/30">
          <div className="mb-2 text-sm font-medium text-blue-900 dark:text-blue-200">
            {cevir("Kaldığınız yerden devam edin")}
          </div>
          <div className="space-y-2">
            {oturumlar.map(o => (
              <div key={o.id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-blue-200/70 bg-white px-3 py-2 text-sm dark:border-blue-900/40 dark:bg-slate-900">
                <div className="min-w-0">
                  <span className="font-medium text-slate-900 dark:text-slate-100">{o.kullanici}@{o.host}</span>
                  <span className="ml-2 text-xs text-slate-500">{o.tip} · {o.site_sayisi} site{o.kimlik_sakli ? ` · ${cevir("parola saklı")}` : ''}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <button type="button" onClick={() => oturumaGeri(o.id)} className="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700">{cevir("Devam et")}</button>
                  <button type="button" onClick={() => oturumSil(o.id)} className="rounded-lg px-2 py-1.5 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 dark:hover:bg-slate-800" title={cevir("Oturumu unut")}>✕</button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {hata && (
        <div className="mb-5 flex items-start gap-2.5 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-900/20 dark:text-red-300">
          <span className="mt-0.5 shrink-0"><Ikon d={I.uyari} /></span>
          <span className="min-w-0 break-words">{hata}</span>
        </div>
      )}

      {/* ==================== ADIM 1 — Sunucu ==================== */}
      {adim === 1 && (
        <div className="space-y-5">
          <Kart>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Kaynak sunucu bilgileri")}</h2>
            <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
              {cevir(cevir("Taşınacak sitelerin bulunduğu sunucunun SSH bilgileri. Kimlik bilgileri şifreli saklanır, iş bitince silinir."))}
            </p>
            <div className="mt-5 grid max-w-4xl gap-x-4 gap-y-4 sm:grid-cols-2 lg:grid-cols-6">
              <label className="block lg:col-span-2">
                <span className={labelCls}>{cevir("Kontrol paneli")}</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.panel} /></span>
                  <select value={tip} onChange={e => setTip(e.target.value)} className={inputCls + ' pl-9'}>
                    {PANELLER.map(p => <option key={p.deger} value={p.deger}>{p.etiket}</option>)}
                  </select>
                </div>
              </label>
              <label className="block sm:col-span-2 lg:col-span-3">
                <span className={labelCls}>{cevir("Sunucu adresi (IP veya host)")}</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.sunucu} /></span>
                  <input value={host} onChange={e => setHost(e.target.value)} placeholder={cevir("1.2.3.4 veya sunucu.alanadi.com")} className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-1">
                <span className={labelCls}>{cevir("SSH portu")}</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.hash} /></span>
                  <input type="number" value={port} onChange={e => setPort(Number(e.target.value))} className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-2">
                <span className={labelCls}>{cevir("SSH kullanıcısı")}</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.kullaniciIkon} /></span>
                  <input value={kullanici} onChange={e => setKullanici(e.target.value)} placeholder="root" className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-2">
                <span className={labelCls}>{cevir("Kimlik doğrulama")}</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.anahtarIkon} /></span>
                  <select value={kimlikTipi} onChange={e => setKimlikTipi(e.target.value as 'parola' | 'anahtar' | 'panel')} className={inputCls + ' pl-9'}>
                    <option value="parola">{cevir("Parola")}</option>
                    <option value="anahtar">{cevir("SSH anahtarı")}</option>
                    <option value="panel">{cevir("Panel anahtarı (parola çalışmıyorsa)")}</option>
                  </select>
                </div>
              </label>
              {kimlikTipi === 'parola' ? (
                <label className="block lg:col-span-2">
                  <span className={labelCls}>{cevir("Parola")}</span>
                  <div className="relative">
                    <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.kilit} /></span>
                    <input type={parolaGoster ? 'text' : 'password'} value={parola} onChange={e => setParola(e.target.value)} autoComplete="new-password" placeholder="••••••••" className={inputCls + ' pl-9 pr-10'} />
                    <button type="button" onClick={() => setParolaGoster(v => !v)} title={parolaGoster ? cevir('Gizle') : cevir("Göster")}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-0.5 text-slate-400 transition hover:text-slate-600 dark:hover:text-slate-200">
                      <Ikon d={parolaGoster ? I.gozKapali : I.goz} />
                    </button>
                  </div>
                </label>
              ) : kimlikTipi === 'anahtar' ? (
                <label className="block sm:col-span-2 lg:col-span-4">
                  <span className={labelCls}>{cevir("SSH özel anahtarı")}</span>
                  <textarea value={anahtar} onChange={e => setAnahtar(e.target.value)} rows={3}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" className={`${inputCls} font-mono text-xs`} />
                </label>
              ) : (
                <div className="block sm:col-span-2 lg:col-span-4">
                  <span className={labelCls}>{cevir("Panel anahtarı — kaynak sunucuda tek komut çalıştırın")}</span>
                  <div className="rounded-xl border border-amber-200 bg-amber-50 px-3.5 py-3 text-xs text-amber-800 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-200">
                    <p className="mb-2 leading-relaxed">
                      {cevir("Kaynak sunucu")} <strong>{cevir("parola girişini kapatmış")}</strong>{cevir(" olabilir (Plesk'te sık). Bunun yerine panel kendi anahtarını kullanır — parola gerekmez.")}
                      {cevir("Aşağıdaki komutu")} <strong>{cevir("kaynak sunucuda")}</strong> ({host || cevir('kaynak')}) <strong>{cevir("bir kez")}</strong> {cevir("çalıştırın, sonra “Siteleri keşfet”e basın:")}
                    </p>
                    <div className="relative">
                      <pre className="max-h-28 overflow-auto rounded-lg bg-slate-900 px-3 py-2 pr-16 font-mono text-[11px] leading-snug text-slate-100 whitespace-pre-wrap break-all">{panelKomut || cevir("Anahtar hazırlanıyor…")}</pre>
                      <button type="button" disabled={!panelKomut}
                        onClick={() => { if (panelKomut) { navigator.clipboard?.writeText(panelKomut); setPanelKopya(true); setTimeout(() => setPanelKopya(false), 1500) } }}
                        className="absolute right-2 top-2 rounded-md bg-slate-700 px-2 py-1 text-[11px] font-medium text-white transition hover:bg-slate-600 disabled:opacity-50">
                        {panelKopya ? cevir('Kopyalandı ✓') : cevir('Kopyala')}
                      </button>
                    </div>
                    <p className="mt-2 text-[11px] text-amber-700/80 dark:text-amber-300/70">
                      {cevir(cevir("Not: Komutu kaynağa Plesk’in SSH terminalinden veya hosting konsolundan yapıştırabilirsiniz. Taşıma bitince anahtarı kaynaktan silebilirsiniz."))}
                    </p>
                  </div>
                </div>
              )}
            </div>

            <div className="mt-4 inline-flex items-center gap-1.5 rounded-full bg-slate-50 px-3 py-1.5 text-[11px] font-medium text-slate-500 dark:bg-slate-800/60 dark:text-slate-400">
              <span className="text-emerald-500"><Ikon d={I.ssl} /></span>
              {cevir(cevir("Kimlik bilgileri AES ile şifreli saklanır, taşıma bitince otomatik silinir."))}
            </div>

            {testSonuc && (
              <div className="mt-4 flex items-start gap-2.5 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 py-2.5 text-xs text-emerald-700 dark:border-emerald-800/60 dark:bg-emerald-900/20 dark:text-emerald-300">
                <span className="mt-0.5 shrink-0"><Ikon d={I.check} /></span>
                <span className="min-w-0 break-words">{testSonuc}</span>
              </div>
            )}

            <div className="mt-5 flex flex-wrap gap-2.5">
              <button type="button" onClick={baglantiTest} disabled={testYuk || !host} className={btnIkincil}>
                {testYuk ? cevir('Test ediliyor…') : cevir("Bağlantıyı test et")}
              </button>
              <button type="button" onClick={kesfet} disabled={kesifYuk || !host} className={btnBirincil}>
                <Ikon d={I.sunucu} />{kesifYuk ? cevir('Siteler taranıyor…') : cevir("Siteleri keşfet →")}
              </button>
            </div>
          </Kart>

          {gecmis.length > 0 && <GecmisKart gecmis={gecmis} sec={(g) => { setIsID(g.id); setCalisiyor(g.durum === 'calisiyor'); if (basladiRef.current === null) basladiRef.current = Date.now(); setAdim(3) }} />}
        </div>
      )}

      {/* ==================== ADIM 2 — Siteler + Ayarlar ==================== */}
      {adim === 2 && hesaplar && (
        <div className="space-y-5">
          <Kart>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Taşınacak siteler")}</h2>
                <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{hesaplar.length} {cevir("site bulundu")} · {secilenSayi} {cevir("seçili")}</p>
              </div>
              <div className="flex gap-2">
                <button type="button" className={btnKucuk} onClick={() => setSecili(Object.fromEntries(hesaplar.map(h => [h.alan_adi, true])))}>{cevir("Tümünü seç")}</button>
                <button type="button" className={btnKucuk} onClick={() => setSecili({})}>{cevir("Temizle")}</button>
              </div>
            </div>
            <div className="mt-4 overflow-x-auto">
              <table className={T.tablo}>
                <thead className={T.baslikGrubu}>
                  <tr>
                    <th className={T.baslik}>{cevir("Seç")}</th><th className={T.baslik}>{cevir("Alan adı")}</th><th className={T.baslik}>{cevir("Kaynak hesap")}</th>
                    <th className={T.baslik}>PHP</th><th className={T.baslik}>{cevir("Boyut")}</th><th className={T.baslik}>{cevir("Veritabanı")}</th><th className={T.baslik}>{cevir("Durum")}</th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {hesaplar.map(h => {
                    const sec = !!secili[h.alan_adi]
                    return (
                      <tr key={`${h.kaynak_hesap}|${h.alan_adi}`} onClick={() => setSecili(s => ({ ...s, [h.alan_adi]: !sec }))}
                        className={`${T.satir} cursor-pointer transition ${sec ? 'bg-brand-50/30 dark:bg-brand-900/10' : ''}`}>
                        <td className={T.hucre} data-etiket={cevir("Seç")}>
                          <input type="checkbox" checked={sec} onClick={e => e.stopPropagation()} onChange={e => setSecili(s => ({ ...s, [h.alan_adi]: e.target.checked }))}
                            className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-brand-500/30 dark:border-slate-600 dark:bg-slate-800" />
                        </td>
                        <td className={T.hucreBaslik} data-etiket={cevir("Alan adı")}>
                          <span className="font-medium">{h.alan_adi}</span>
                          {h.not && <span className="ml-1.5 text-[11px] text-slate-400">({h.not})</span>}
                        </td>
                        <td className={T.hucre} data-etiket={cevir("Kaynak hesap")}><span className="text-slate-500 dark:text-slate-400">{h.kaynak_hesap || '—'}</span></td>
                        <td className={T.hucre} data-etiket="PHP">{h.php_surum || '—'}</td>
                        <td className={T.hucre} data-etiket={cevir("Boyut")}>{mb(h.boyut_mb)}</td>
                        <td className={T.hucre} data-etiket={cevir("Veritabanı")}>{h.dbler?.length || 0}</td>
                        <td className={T.hucre} data-etiket={cevir("Durum")}>
                          {h.mevcut
                            ? <span className="inline-flex rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-300">{cevir("Panelde var")}</span>
                            : <span className="inline-flex rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400">{cevir("Yeni")}</span>}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </Kart>

          <Kart>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Sahiplik ve aktarım ayarları")}</h2>
            <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{cevir("Neyin taşınacağını, hangi plana ve kime atanacağını seçin.")}</p>
            <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {ayarlar.map(([et, deger, ayarla, ipucu, ikon]) => (
                <button key={et} type="button" onClick={() => ayarla(!deger)}
                  className={`flex items-start gap-3 rounded-2xl border p-3.5 text-left transition ${
                    deger ? 'border-slate-900/15 bg-slate-50 dark:border-white/15 dark:bg-slate-800/70' : 'border-slate-200/70 hover:border-slate-300 dark:border-slate-700/60 dark:hover:border-slate-600'}`}>
                  <span className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-md border transition ${
                    deger ? 'border-slate-900 bg-slate-900 text-white dark:border-white dark:bg-white dark:text-slate-900' : 'border-slate-300 text-transparent dark:border-slate-600'}`}>
                    <Ikon d={I.check} />
                  </span>
                  <span className="min-w-0">
                    <span className="flex items-center gap-1.5 text-sm font-medium text-slate-800 dark:text-slate-100">
                      <span className="text-slate-400 dark:text-slate-500"><Ikon d={ikon} /></span>{et}
                    </span>
                    <span className="mt-0.5 block text-[11px] leading-relaxed text-slate-400 dark:text-slate-500">{ipucu}</span>
                  </span>
                </button>
              ))}
              <SelKart etiket={cevir("Hedef PHP")}><select value={hedefPHP} onChange={e => setHedefPHP(e.target.value)} className={selCls}>{PHP_SURUMLERI.map(v => <option key={v} value={v}>{v || cevir("Kaynaktakiyle aynı")}</option>)}</select></SelKart>
              <SelKart etiket={cevir("Hosting planı")}><select value={planID} onChange={e => setPlanID(Number(e.target.value))} className={selCls}><option value={0}>{cevir("Plansız (limit yok)")}</option>{planlar.map(p => <option key={p.id} value={p.id}>{p.ad}</option>)}</select></SelKart>
              <SelKart etiket={cevir("Bayi (sahip)")}><select value={resellerID} onChange={e => setResellerID(Number(e.target.value))} className={selCls}><option value={0}>{cevir("Ana hesap (admin)")}</option>{bayiler.map(b => <option key={b.id} value={b.id}>{b.ad_soyad ? `${b.ad_soyad} (${b.kullanici})` : b.kullanici}</option>)}</select></SelKart>
              <SelKart etiket={cevir("Müşteri")}><select value={customerID} onChange={e => setCustomerID(Number(e.target.value))} className={selCls}><option value={0}>{cevir("Yok")}</option>{musteriler.map(m => <option key={m.id} value={m.id}>{m.ad}</option>)}</select></SelKart>
            </div>

            {ustune && (
              <div className="mt-4 flex items-start gap-2.5 rounded-xl border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-xs text-amber-700 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-300">
                <span className="mt-0.5 shrink-0"><Ikon d={I.uyari} /></span>
                <span><strong className="font-semibold">{cevir("Dikkat:")}</strong> {cevir("“Mevcut siteyi ez” açık — aynı alan adına sahip sitelerin dosya ve veritabanları üzerine yazılacak. Önce yedek önerilir.")}</span>
              </div>
            )}

            <div className="mt-5 flex flex-wrap items-center justify-between gap-2.5">
              <button type="button" onClick={() => setAdim(1)} className={btnIkincil}><Ikon d={I.geri} />{cevir("Geri")}</button>
              <button type="button" onClick={baslat} disabled={!secilenSayi} className={btnBirincil}>
                <Ikon d={I.ok} />{cevir("Taşımayı başlat")} ({secilenSayi} site) →
              </button>
            </div>
          </Kart>
        </div>
      )}

      {/* ==================== ADIM 3 — Taşıma (canlı) ==================== */}
      {adim === 3 && isID && (
        <div className="space-y-5">
          {uretilenKimlikler.length > 0 && (
            <Kart>
              <div className="rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-3 dark:border-amber-800/60 dark:bg-amber-900/20">
                <h3 className="text-sm font-semibold text-amber-800 dark:text-amber-200">{cevir("🔑 Oluşturulan sahip hesapları — parolaları KAYDEDİN")}</h3>
                <p className="mb-2 mt-0.5 text-xs text-amber-700/80 dark:text-amber-300/70">
                  {cevir(cevir("Kaynaktaki reseller/müşteri hesapları planlarıyla oluşturuldu. Reseller parolaları YALNIZ ŞİMDİ gösterilir (bir daha görünmez) — sahiplere siz iletin."))}
                </p>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead><tr className="text-left text-slate-500 dark:text-slate-400">
                      <th className="py-1 pr-3">{cevir("Tip")}</th><th className="py-1 pr-3">{cevir("Ad")}</th><th className="py-1 pr-3">{cevir("Kullanıcı")}</th><th className="py-1 pr-3">{cevir("E-posta")}</th><th className="py-1">{cevir("Parola")}</th>
                    </tr></thead>
                    <tbody>
                      {uretilenKimlikler.map((u, i) => (
                        <tr key={i} className="border-t border-amber-200/50 dark:border-amber-800/40">
                          <td className="py-1 pr-3 capitalize">{u.tip}</td>
                          <td className="py-1 pr-3">{u.ad}</td>
                          <td className="py-1 pr-3 font-mono">{u.login}</td>
                          <td className="py-1 pr-3">{u.eposta || '—'}</td>
                          <td className="py-1 font-mono font-semibold">{u.parola || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </Kart>
          )}
          <Kart>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Taşıma")} #{isID}</h2>
                <DurumRozet durum={calisiyor ? 'calisiyor' : (ozet.durum || 'bekliyor')} />
              </div>
              {calisiyor
                ? <button type="button" onClick={iptalEt} className="inline-flex items-center gap-1.5 rounded-full border border-red-200 px-3.5 py-1.5 text-xs font-medium text-red-700 transition hover:bg-red-50 dark:border-red-800/60 dark:text-red-300 dark:hover:bg-red-900/20">■ {cevir("Durdur")}</button>
                : <div className="flex items-center gap-2">
                    {hesaplar && hesaplar.length > 0 && (
                      <div className="relative">
                        <button type="button" onClick={() => setRetryAcik(v => !v)} className={btnKucuk}><Ikon d={I.yenile} /> {cevir("Tekrar dene")}</button>
                        {retryAcik && (
                          <>
                            <div className="fixed inset-0 z-10" onClick={() => setRetryAcik(false)} />
                            <div className="absolute right-0 z-20 mt-2 w-64 rounded-xl border border-slate-200 bg-white p-3 shadow-xl dark:border-slate-700 dark:bg-slate-900">
                              <div className="mb-2 text-xs font-medium text-slate-500">{cevir("Neyi yeniden taşıyalım?")}</div>
                              <div className="space-y-1">
                                <RetrySecim etiket={cevir("Dosyalar")} ikon={I.files} secili={dosyalar} degis={() => setDosyalar(v => !v)} />
                                <RetrySecim etiket={cevir("SQL (Veritabanı)")} ikon={I.db} secili={veritabani} degis={() => setVeritabani(v => !v)} />
                                <RetrySecim etiket="DNS" ikon={I.dns} secili={dns} degis={() => setDns(v => !v)} />
                                <RetrySecim etiket="SSL" ikon={I.ssl} secili={ssl} degis={() => setSsl(v => !v)} />
                                <RetrySecim etiket="Mail" ikon={I.mail} secili={posta} degis={() => setPosta(v => !v)} />
                              </div>
                              <label className="mt-2 flex cursor-pointer items-center gap-2 border-t border-slate-100 pt-2 text-xs text-slate-600 dark:border-slate-800 dark:text-slate-400">
                                <input type="checkbox" checked={ustune} onChange={() => setUstune(v => !v)} className="h-3.5 w-3.5 rounded border-slate-300 dark:border-slate-600" />
                                {cevir(cevir("Mevcut siteyi ez (üzerine yaz)"))}
                              </label>
                              <button type="button" onClick={() => { setRetryAcik(false); tekrarDene() }} className="mt-2 w-full rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-slate-800 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">{cevir("Seçileni Taşı")}</button>
                            </div>
                          </>
                        )}
                      </div>
                    )}
                    <button type="button" onClick={yeniTasima} className={btnKucuk}>{cevir("+ Yeni taşıma")}</button>
                  </div>}
            </div>

            {/* Canlı: hangi domain taşınıyor + ETA */}
            {calisiyor && (
              <div className="mt-4 flex flex-wrap items-center gap-4 rounded-2xl border border-brand-200/60 bg-brand-50/50 px-4 py-3 dark:border-brand-800/40 dark:bg-brand-900/10">
                <span className="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />
                <div className="min-w-0 flex-1">
                  <div className="text-[11px] font-medium uppercase tracking-wide text-brand-600 dark:text-brand-400">{cevir("Şu an taşınıyor")}</div>
                  <div className="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{aktifKalem?.alan_adi || cevir("hazırlanıyor…")}</div>
                </div>
                <div className="text-right">
                  <div className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500"><Ikon d={I.saat} />{cevir("Tahmini kalan")}</div>
                  <div className="text-sm font-semibold tabular-nums text-slate-900 dark:text-slate-100">{sure(etaSn)}</div>
                </div>
              </div>
            )}

            {ozet.toplam > 0 && (
              <div className="mt-4">
                <div className="mb-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
                  {[
                    [cevir('Toplam'), ozet.toplam, 'text-slate-900 dark:text-slate-100'],
                    [cevir('Tamamlanan'), ozet.tamamlanan, 'text-emerald-600 dark:text-emerald-400'],
                    [cevir('Hata'), ozet.basarisiz, ozet.basarisiz ? 'text-red-600 dark:text-red-400' : 'text-slate-400'],
                    [cevir("Geçen süre"), sure(gecenSn), 'text-slate-900 dark:text-slate-100'],
                  ].map(([et, deg, renk]) => (
                    <div key={et as string} className="rounded-2xl bg-slate-50 px-3.5 py-3 dark:bg-slate-900/40">
                      <div className="text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{et}</div>
                      <div className={`mt-0.5 text-lg font-semibold tabular-nums ${renk}`}>{deg as ReactNode}</div>
                    </div>
                  ))}
                </div>
                <div className="mb-1.5 flex justify-between text-xs text-slate-500 dark:text-slate-400">
                  <span>{done} / {ozet.toplam} {cevir("tamamlandı")}</span><span className="tabular-nums">{yuzde}%</span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                  <div className="h-full rounded-full bg-brand-500 transition-all duration-500" style={{ width: `${yuzde}%` }} />
                </div>
              </div>
            )}

            {kalemler.length > 0 && (
              <div className="mt-5 overflow-x-auto">
                <table className={T.tablo}>
                  <thead className={T.baslikGrubu}>
                    <tr><th className={T.baslik}>{cevir("Alan adı")}</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>Dosya</th><th className={T.baslik}>DB</th><th className={T.baslik}>DNS</th><th className={T.baslik}>{cevir("Not")}</th></tr>
                  </thead>
                  <tbody className={T.govde}>
                    {kalemler.map(k => (
                      <tr key={k.id} className={`${T.satir} ${k.durum === 'calisiyor' ? 'bg-brand-50/40 dark:bg-brand-900/10' : ''}`}>
                        <td className={T.hucreBaslik} data-etiket={cevir("Alan adı")}><span className="font-medium">{k.alan_adi}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Durum")}><DurumRozet durum={k.durum} /></td>
                        <td className={T.hucre} data-etiket="Dosya">{bayt(k.dosya_bayt)}</td>
                        <td className={T.hucre} data-etiket="DB">{k.db_sayisi || '—'}</td>
                        <td className={T.hucre} data-etiket="DNS">{k.dns_sayisi || '—'}</td>
                        <td className={T.hucre} data-etiket={cevir("Not")}><span className="text-[11px] text-slate-500 dark:text-slate-400">{k.hata || '—'}</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <pre ref={logRef} className="mt-5 max-h-72 overflow-auto whitespace-pre-wrap rounded-xl bg-slate-950 p-3.5 font-mono text-[11px] leading-relaxed text-slate-300 ring-1 ring-slate-800">
              {logMetin || cevir("Kayıt bekleniyor…")}
            </pre>
          </Kart>

          {gecmis.length > 0 && <GecmisKart gecmis={gecmis} sec={(g) => { setIsID(g.id); setCalisiyor(g.durum === 'calisiyor') }} />}
        </div>
      )}
    </div>
  )
}

function SelKart({ etiket, children }: { etiket: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-2xl border border-slate-200/70 p-3.5 dark:border-slate-700/60">
      <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{etiket}</span>
      {children}
    </div>
  )
}

function GecmisKart({ gecmis, sec }: { gecmis: Is[]; sec: (g: Is) => void }) {
  return (
    <Kart>
      <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Geçmiş taşımalar")}</h2>
      <div className="mt-4 overflow-x-auto">
        <table className={T.tablo}>
          <thead className={T.baslikGrubu}>
            <tr><th className={T.baslik}>#</th><th className={T.baslik}>{cevir("Kaynak")}</th><th className={T.baslik}>Panel</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>{cevir("Sonuç")}</th><th className={T.baslik}>{cevir("Başlatan")}</th></tr>
          </thead>
          <tbody className={T.govde}>
            {gecmis.map(g => (
              <tr key={g.id} className={`${T.satir} cursor-pointer`} onClick={() => sec(g)}>
                <td className={T.hucreBaslik} data-etiket="#">{g.id}</td>
                <td className={T.hucre} data-etiket={cevir("Kaynak")}>{g.host}</td>
                <td className={T.hucre} data-etiket="Panel">{g.tip}</td>
                <td className={T.hucre} data-etiket={cevir("Durum")}><DurumRozet durum={g.durum} /></td>
                <td className={T.hucre} data-etiket={cevir("Sonuç")}>{g.tamamlanan}/{g.toplam}{g.basarisiz ? ` (${g.basarisiz} ${cevir("hata")})` : ''}</td>
                <td className={T.hucre} data-etiket={cevir("Başlatan")}>{g.baslatan || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Kart>
  )
}
