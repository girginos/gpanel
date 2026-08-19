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

function mb(n: number) { if (!n) return '—'; return n < 1024 ? `${n} MB` : `${(n / 1024).toFixed(1)} GB` }
function bayt(n: number) { if (!n) return '—'; const m = n / (1024 * 1024); return m < 1024 ? `${m.toFixed(1)} MB` : `${(m / 1024).toFixed(2)} GB` }
function sure(sn: number) {
  if (!isFinite(sn) || sn <= 0) return '—'
  const dk = Math.floor(sn / 60), s = Math.round(sn % 60)
  return dk > 0 ? `${dk} dk ${s} sn` : `${s} sn`
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
      {DURUM_ETIKET[durum] || durum}
    </span>
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
              <span className={`hidden text-sm font-medium sm:block ${durum === 'todo' ? 'text-slate-400 dark:text-slate-500' : 'text-slate-800 dark:text-slate-100'}`}>{t}</span>
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
type KayitliKaynak = { tip?: string; host?: string; port?: number; kullanici?: string; kimlikTipi?: 'parola' | 'anahtar' }
function kayitliKaynakOku(): KayitliKaynak {
  try { return JSON.parse(localStorage.getItem(KAYNAK_KEY) || '{}') } catch { return {} }
}

export default function SiteTasimaPage() {
  const kk = kayitliKaynakOku()
  // --- kaynak sunucu ---
  const [tip, setTip] = useState('plesk')
  const [host, setHost] = useState(kk.host || '')
  const [port, setPort] = useState(kk.port || 22)
  const [kullanici, setKullanici] = useState(kk.kullanici || 'root')
  const [kimlikTipi, setKimlikTipi] = useState<'parola' | 'anahtar'>(kk.kimlikTipi || 'parola')
  const [parola, setParola] = useState('')
  const [anahtar, setAnahtar] = useState('')
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
  const [ustune, setUstune] = useState(false)
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
  const [kimlikSakli, setKimlikSakli] = useState(false) // oturumdan geldiyse parola sunucuda saklı
  const logRef = useRef<HTMLPreElement>(null)
  const basladiRef = useRef<number | null>(null) // ETA için başlangıç ms

  const kaynakGovde = () => ({
    tip, host: host.trim(), port: Number(port) || 22, kullanici: kullanici.trim(),
    parola: kimlikTipi === 'parola' ? parola : '', anahtar: kimlikTipi === 'anahtar' ? anahtar : '',
  })

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
    } catch (e) { setHata(apiHata(e, 'Oturum geri yüklenemedi')) }
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
    api.get<Plan[]>('/plans').then(r => { const l = r.data || []; setPlanlar(l); if (l.length && !planID) setPlanID(l[0].id) }).catch(hataYakala('Planlar yüklenemedi'))
    api.get<Bayi[]>('/resellers').then(r => setBayiler(r.data || [])).catch(hataYakala('Bayiler yüklenemedi'))
    api.get<Musteri[]>('/customers').then(r => setMusteriler(r.data || [])).catch(hataYakala('Müşteriler yüklenemedi'))

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
      setTestSonuc(`Bağlantı başarılı — ${data.sunucu_adi || 'sunucu'} · tespit edilen panel: ${data.tespit_edilen || 'bilinmiyor'}` + (data.uyusuyor ? '' : ' (seçtiğiniz panel ile uyuşmuyor!)'))
    } catch (e) { setHata(apiHata(e, 'Bağlantı testi başarısız')) } finally { setTestYuk(false) }
  }

  async function kesfet() {
    setKesifYuk(true); setHata(null); setHesaplar(null)
    try {
      const { data } = await api.post<{ hesaplar: Hesap[]; oturum_id: number }>('/system/tasima/kesif', kaynakGovde())
      const list = data.hesaplar || []
      setHesaplar(list)
      const s: Record<string, boolean> = {}; list.forEach(h => { s[h.alan_adi] = !h.mevcut }); setSecili(s)
      if (data.oturum_id) { setOturumID(data.oturum_id); setKimlikSakli(kimlikTipi === 'parola' ? !!parola : !!anahtar); try { localStorage.setItem(OTURUM_KEY, String(data.oturum_id)) } catch { /* */ } }
      oturumlarYukle()
      if (!list.length) setHata('Kaynak sunucuda taşınabilir site bulunamadı.')
      else setAdim(2)
    } catch (e) { setHata(apiHata(e, 'Keşif başarısız')) } finally { setKesifYuk(false) }
  }

  async function baslat() {
    const secilenler = (hesaplar || []).filter(h => secili[h.alan_adi])
    if (!secilenler.length) { setHata('En az bir site seçin.'); return }
    setHata(null)
    try {
      const { data } = await api.post<{ is_id: number }>('/system/tasima/baslat', {
        ...kaynakGovde(),
        oturum_id: oturumID || 0, // parola yeniden girilmediyse sunucu bunu kullanır
        mod: secilenler.length === 1 ? 'tekil' : 'toplu',
        ayarlar: { dosyalar, veritabani, dns, ssl, ustune, hedef_php: hedefPHP, plan_id: planID, reseller_id: resellerID, customer_id: customerID, hesaplar: [] },
        secilen: secilenler,
      })
      basladiRef.current = Date.now()
      setIsID(data.is_id); setCalisiyor(true); setLogMetin(''); setKalemler([]); setAdim(3)
    } catch (e) { setHata(apiHata(e, 'Taşıma başlatılamadı')) }
  }

  async function iptalEt() {
    if (!isID) return
    try { await api.post(`/system/tasima/${isID}/iptal`, {}) } catch (e) { setHata(apiHata(e, 'İptal edilemedi')) }
  }

  function yeniTasima() {
    setIsID(null); setCalisiyor(false); setKalemler([]); setLogMetin(''); setOzet({ toplam: 0, tamamlanan: 0, basarisiz: 0, durum: '' })
    basladiRef.current = null; setAdim(hesaplar && hesaplar.length ? 2 : 1)
  }

  // Tekrar dene — aynı seçimle yeniden başlat. Kimlik saklı oturumdan (oturum_id)
  // sunucu tarafında çözüldüğü için sunucu bilgilerini yeniden girmeye gerek yok.
  // State yoksa (sayfa yenilendi) forma dön; "Kaldığınız yerden devam edin" ile gelinir.
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
    ['Dosyalar', dosyalar, setDosyalar, 'Web kök dizini rsync ile aktarılır', I.files],
    ['Veritabanları', veritabani, setVeritabani, 'Dump alınır, aktarılır, ayarlar güncellenir', I.db],
    ['DNS kayıtları', dns, setDns, 'Kaynak zone okunur, A kayıtları çevrilir', I.dns],
    ['SSL sertifikası', ssl, setSsl, "Let's Encrypt denenir", I.ssl],
    ['Mevcut siteyi ez', ustune, setUstune, 'Panelde aynı alan adı varsa üzerine yazar', I.ez],
  ]

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-6">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Site Taşıma' },
      ]} />

      <div className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Site Taşıma</h1>
        <p className="mt-1.5 max-w-2xl text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          Plesk sunucunuzdaki siteleri dosyaları, veritabanları, DNS ve SSL ile
          birlikte 3 adımda bu panele aktarın. İşlem arka planda çalışır — bu sayfayı kapatabilirsiniz.
        </p>
      </div>

      <Stepper adim={adim} git={setAdim} erisim={erisim} />

      {/* Kaydedilmiş oturumlar — sunucu bilgilerini yeniden girmeden devam et */}
      {adim === 1 && oturumlar.length > 0 && (
        <div className="mb-5 rounded-2xl border border-blue-200 bg-blue-50 px-4 py-3 dark:border-blue-900/50 dark:bg-blue-950/30">
          <div className="mb-2 text-sm font-medium text-blue-900 dark:text-blue-200">
            Kaldığınız yerden devam edin
          </div>
          <div className="space-y-2">
            {oturumlar.map(o => (
              <div key={o.id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-blue-200/70 bg-white px-3 py-2 text-sm dark:border-blue-900/40 dark:bg-slate-900">
                <div className="min-w-0">
                  <span className="font-medium text-slate-900 dark:text-slate-100">{o.kullanici}@{o.host}</span>
                  <span className="ml-2 text-xs text-slate-500">{o.tip} · {o.site_sayisi} site{o.kimlik_sakli ? ' · parola saklı' : ''}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <button type="button" onClick={() => oturumaGeri(o.id)} className="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700">Devam et</button>
                  <button type="button" onClick={() => oturumSil(o.id)} className="rounded-lg px-2 py-1.5 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 dark:hover:bg-slate-800" title="Oturumu unut">✕</button>
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
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Kaynak sunucu bilgileri</h2>
            <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
              Taşınacak sitelerin bulunduğu sunucunun SSH bilgileri. Kimlik bilgileri şifreli saklanır, iş bitince silinir.
            </p>
            <div className="mt-5 grid max-w-4xl gap-x-4 gap-y-4 sm:grid-cols-2 lg:grid-cols-6">
              <label className="block lg:col-span-2">
                <span className={labelCls}>Kontrol paneli</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.panel} /></span>
                  <select value={tip} onChange={e => setTip(e.target.value)} className={inputCls + ' pl-9'}>
                    {PANELLER.map(p => <option key={p.deger} value={p.deger}>{p.etiket}</option>)}
                  </select>
                </div>
              </label>
              <label className="block sm:col-span-2 lg:col-span-3">
                <span className={labelCls}>Sunucu adresi (IP veya host)</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.sunucu} /></span>
                  <input value={host} onChange={e => setHost(e.target.value)} placeholder="1.2.3.4 veya sunucu.alanadi.com" className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-1">
                <span className={labelCls}>SSH portu</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.hash} /></span>
                  <input type="number" value={port} onChange={e => setPort(Number(e.target.value))} className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-2">
                <span className={labelCls}>SSH kullanıcısı</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.kullaniciIkon} /></span>
                  <input value={kullanici} onChange={e => setKullanici(e.target.value)} placeholder="root" className={inputCls + ' pl-9'} />
                </div>
              </label>
              <label className="block lg:col-span-2">
                <span className={labelCls}>Kimlik doğrulama</span>
                <div className="relative">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.anahtarIkon} /></span>
                  <select value={kimlikTipi} onChange={e => setKimlikTipi(e.target.value as 'parola' | 'anahtar')} className={inputCls + ' pl-9'}>
                    <option value="parola">Parola</option>
                    <option value="anahtar">SSH anahtarı</option>
                  </select>
                </div>
              </label>
              {kimlikTipi === 'parola' ? (
                <label className="block lg:col-span-2">
                  <span className={labelCls}>Parola</span>
                  <div className="relative">
                    <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"><Ikon d={I.kilit} /></span>
                    <input type={parolaGoster ? 'text' : 'password'} value={parola} onChange={e => setParola(e.target.value)} autoComplete="new-password" placeholder="••••••••" className={inputCls + ' pl-9 pr-10'} />
                    <button type="button" onClick={() => setParolaGoster(v => !v)} title={parolaGoster ? 'Gizle' : 'Göster'}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-0.5 text-slate-400 transition hover:text-slate-600 dark:hover:text-slate-200">
                      <Ikon d={parolaGoster ? I.gozKapali : I.goz} />
                    </button>
                  </div>
                </label>
              ) : (
                <label className="block sm:col-span-2 lg:col-span-4">
                  <span className={labelCls}>SSH özel anahtarı</span>
                  <textarea value={anahtar} onChange={e => setAnahtar(e.target.value)} rows={3}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" className={`${inputCls} font-mono text-xs`} />
                </label>
              )}
            </div>

            <div className="mt-4 inline-flex items-center gap-1.5 rounded-full bg-slate-50 px-3 py-1.5 text-[11px] font-medium text-slate-500 dark:bg-slate-800/60 dark:text-slate-400">
              <span className="text-emerald-500"><Ikon d={I.ssl} /></span>
              Kimlik bilgileri AES ile şifreli saklanır, taşıma bitince otomatik silinir.
            </div>

            {testSonuc && (
              <div className="mt-4 flex items-start gap-2.5 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 py-2.5 text-xs text-emerald-700 dark:border-emerald-800/60 dark:bg-emerald-900/20 dark:text-emerald-300">
                <span className="mt-0.5 shrink-0"><Ikon d={I.check} /></span>
                <span className="min-w-0 break-words">{testSonuc}</span>
              </div>
            )}

            <div className="mt-5 flex flex-wrap gap-2.5">
              <button type="button" onClick={baglantiTest} disabled={testYuk || !host} className={btnIkincil}>
                {testYuk ? 'Test ediliyor…' : 'Bağlantıyı test et'}
              </button>
              <button type="button" onClick={kesfet} disabled={kesifYuk || !host} className={btnBirincil}>
                <Ikon d={I.sunucu} />{kesifYuk ? 'Siteler taranıyor…' : 'Siteleri keşfet →'}
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
                <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Taşınacak siteler</h2>
                <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{hesaplar.length} site bulundu · {secilenSayi} seçili</p>
              </div>
              <div className="flex gap-2">
                <button type="button" className={btnKucuk} onClick={() => setSecili(Object.fromEntries(hesaplar.map(h => [h.alan_adi, true])))}>Tümünü seç</button>
                <button type="button" className={btnKucuk} onClick={() => setSecili({})}>Temizle</button>
              </div>
            </div>
            <div className="mt-4 overflow-x-auto">
              <table className={T.tablo}>
                <thead className={T.baslikGrubu}>
                  <tr>
                    <th className={T.baslik}>Seç</th><th className={T.baslik}>Alan adı</th><th className={T.baslik}>Kaynak hesap</th>
                    <th className={T.baslik}>PHP</th><th className={T.baslik}>Boyut</th><th className={T.baslik}>Veritabanı</th><th className={T.baslik}>Durum</th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {hesaplar.map(h => {
                    const sec = !!secili[h.alan_adi]
                    return (
                      <tr key={`${h.kaynak_hesap}|${h.alan_adi}`} onClick={() => setSecili(s => ({ ...s, [h.alan_adi]: !sec }))}
                        className={`${T.satir} cursor-pointer transition ${sec ? 'bg-brand-50/30 dark:bg-brand-900/10' : ''}`}>
                        <td className={T.hucre} data-etiket="Seç">
                          <input type="checkbox" checked={sec} onClick={e => e.stopPropagation()} onChange={e => setSecili(s => ({ ...s, [h.alan_adi]: e.target.checked }))}
                            className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-brand-500/30 dark:border-slate-600 dark:bg-slate-800" />
                        </td>
                        <td className={T.hucreBaslik} data-etiket="Alan adı">
                          <span className="font-medium">{h.alan_adi}</span>
                          {h.not && <span className="ml-1.5 text-[11px] text-slate-400">({h.not})</span>}
                        </td>
                        <td className={T.hucre} data-etiket="Kaynak hesap"><span className="text-slate-500 dark:text-slate-400">{h.kaynak_hesap || '—'}</span></td>
                        <td className={T.hucre} data-etiket="PHP">{h.php_surum || '—'}</td>
                        <td className={T.hucre} data-etiket="Boyut">{mb(h.boyut_mb)}</td>
                        <td className={T.hucre} data-etiket="Veritabanı">{h.dbler?.length || 0}</td>
                        <td className={T.hucre} data-etiket="Durum">
                          {h.mevcut
                            ? <span className="inline-flex rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-300">Panelde var</span>
                            : <span className="inline-flex rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400">Yeni</span>}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </Kart>

          <Kart>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Sahiplik ve aktarım ayarları</h2>
            <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">Neyin taşınacağını, hangi plana ve kime atanacağını seçin.</p>
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
              <SelKart etiket="Hedef PHP"><select value={hedefPHP} onChange={e => setHedefPHP(e.target.value)} className={selCls}>{PHP_SURUMLERI.map(v => <option key={v} value={v}>{v || 'Kaynaktakiyle aynı'}</option>)}</select></SelKart>
              <SelKart etiket="Hosting planı"><select value={planID} onChange={e => setPlanID(Number(e.target.value))} className={selCls}><option value={0}>Plansız (limit yok)</option>{planlar.map(p => <option key={p.id} value={p.id}>{p.ad}</option>)}</select></SelKart>
              <SelKart etiket="Bayi (sahip)"><select value={resellerID} onChange={e => setResellerID(Number(e.target.value))} className={selCls}><option value={0}>Ana hesap (admin)</option>{bayiler.map(b => <option key={b.id} value={b.id}>{b.ad_soyad ? `${b.ad_soyad} (${b.kullanici})` : b.kullanici}</option>)}</select></SelKart>
              <SelKart etiket="Müşteri"><select value={customerID} onChange={e => setCustomerID(Number(e.target.value))} className={selCls}><option value={0}>Yok</option>{musteriler.map(m => <option key={m.id} value={m.id}>{m.ad}</option>)}</select></SelKart>
            </div>

            {ustune && (
              <div className="mt-4 flex items-start gap-2.5 rounded-xl border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-xs text-amber-700 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-300">
                <span className="mt-0.5 shrink-0"><Ikon d={I.uyari} /></span>
                <span><strong className="font-semibold">Dikkat:</strong> “Mevcut siteyi ez” açık — aynı alan adına sahip sitelerin dosya ve veritabanları üzerine yazılacak. Önce yedek önerilir.</span>
              </div>
            )}

            <div className="mt-5 flex flex-wrap items-center justify-between gap-2.5">
              <button type="button" onClick={() => setAdim(1)} className={btnIkincil}><Ikon d={I.geri} />Geri</button>
              <button type="button" onClick={baslat} disabled={!secilenSayi} className={btnBirincil}>
                <Ikon d={I.ok} />Taşımayı başlat ({secilenSayi} site) →
              </button>
            </div>
          </Kart>
        </div>
      )}

      {/* ==================== ADIM 3 — Taşıma (canlı) ==================== */}
      {adim === 3 && isID && (
        <div className="space-y-5">
          <Kart>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Taşıma #{isID}</h2>
                <DurumRozet durum={calisiyor ? 'calisiyor' : (ozet.durum || 'bekliyor')} />
              </div>
              {calisiyor
                ? <button type="button" onClick={iptalEt} className="inline-flex items-center gap-1.5 rounded-full border border-red-200 px-3.5 py-1.5 text-xs font-medium text-red-700 transition hover:bg-red-50 dark:border-red-800/60 dark:text-red-300 dark:hover:bg-red-900/20">■ Durdur</button>
                : <div className="flex items-center gap-2">
                    {hesaplar && hesaplar.length > 0 && (
                      <button type="button" onClick={tekrarDene} className={btnKucuk}>↻ Tekrar dene</button>
                    )}
                    <button type="button" onClick={yeniTasima} className={btnKucuk}>+ Yeni taşıma</button>
                  </div>}
            </div>

            {/* Canlı: hangi domain taşınıyor + ETA */}
            {calisiyor && (
              <div className="mt-4 flex flex-wrap items-center gap-4 rounded-2xl border border-brand-200/60 bg-brand-50/50 px-4 py-3 dark:border-brand-800/40 dark:bg-brand-900/10">
                <span className="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />
                <div className="min-w-0 flex-1">
                  <div className="text-[11px] font-medium uppercase tracking-wide text-brand-600 dark:text-brand-400">Şu an taşınıyor</div>
                  <div className="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{aktifKalem?.alan_adi || 'hazırlanıyor…'}</div>
                </div>
                <div className="text-right">
                  <div className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500"><Ikon d={I.saat} />Tahmini kalan</div>
                  <div className="text-sm font-semibold tabular-nums text-slate-900 dark:text-slate-100">{sure(etaSn)}</div>
                </div>
              </div>
            )}

            {ozet.toplam > 0 && (
              <div className="mt-4">
                <div className="mb-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
                  {[
                    ['Toplam', ozet.toplam, 'text-slate-900 dark:text-slate-100'],
                    ['Tamamlanan', ozet.tamamlanan, 'text-emerald-600 dark:text-emerald-400'],
                    ['Hata', ozet.basarisiz, ozet.basarisiz ? 'text-red-600 dark:text-red-400' : 'text-slate-400'],
                    ['Geçen süre', sure(gecenSn), 'text-slate-900 dark:text-slate-100'],
                  ].map(([et, deg, renk]) => (
                    <div key={et as string} className="rounded-2xl bg-slate-50 px-3.5 py-3 dark:bg-slate-900/40">
                      <div className="text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{et}</div>
                      <div className={`mt-0.5 text-lg font-semibold tabular-nums ${renk}`}>{deg as ReactNode}</div>
                    </div>
                  ))}
                </div>
                <div className="mb-1.5 flex justify-between text-xs text-slate-500 dark:text-slate-400">
                  <span>{done} / {ozet.toplam} tamamlandı</span><span className="tabular-nums">{yuzde}%</span>
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
                    <tr><th className={T.baslik}>Alan adı</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Dosya</th><th className={T.baslik}>DB</th><th className={T.baslik}>DNS</th><th className={T.baslik}>Not</th></tr>
                  </thead>
                  <tbody className={T.govde}>
                    {kalemler.map(k => (
                      <tr key={k.id} className={`${T.satir} ${k.durum === 'calisiyor' ? 'bg-brand-50/40 dark:bg-brand-900/10' : ''}`}>
                        <td className={T.hucreBaslik} data-etiket="Alan adı"><span className="font-medium">{k.alan_adi}</span></td>
                        <td className={T.hucre} data-etiket="Durum"><DurumRozet durum={k.durum} /></td>
                        <td className={T.hucre} data-etiket="Dosya">{bayt(k.dosya_bayt)}</td>
                        <td className={T.hucre} data-etiket="DB">{k.db_sayisi || '—'}</td>
                        <td className={T.hucre} data-etiket="DNS">{k.dns_sayisi || '—'}</td>
                        <td className={T.hucre} data-etiket="Not"><span className="text-[11px] text-slate-500 dark:text-slate-400">{k.hata || '—'}</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <pre ref={logRef} className="mt-5 max-h-72 overflow-auto whitespace-pre-wrap rounded-xl bg-slate-950 p-3.5 font-mono text-[11px] leading-relaxed text-slate-300 ring-1 ring-slate-800">
              {logMetin || 'Kayıt bekleniyor…'}
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
      <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Geçmiş taşımalar</h2>
      <div className="mt-4 overflow-x-auto">
        <table className={T.tablo}>
          <thead className={T.baslikGrubu}>
            <tr><th className={T.baslik}>#</th><th className={T.baslik}>Kaynak</th><th className={T.baslik}>Panel</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Sonuç</th><th className={T.baslik}>Başlatan</th></tr>
          </thead>
          <tbody className={T.govde}>
            {gecmis.map(g => (
              <tr key={g.id} className={`${T.satir} cursor-pointer`} onClick={() => sec(g)}>
                <td className={T.hucreBaslik} data-etiket="#">{g.id}</td>
                <td className={T.hucre} data-etiket="Kaynak">{g.host}</td>
                <td className={T.hucre} data-etiket="Panel">{g.tip}</td>
                <td className={T.hucre} data-etiket="Durum"><DurumRozet durum={g.durum} /></td>
                <td className={T.hucre} data-etiket="Sonuç">{g.tamamlanan}/{g.toplam}{g.basarisiz ? ` (${g.basarisiz} hata)` : ''}</td>
                <td className={T.hucre} data-etiket="Başlatan">{g.baslatan || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Kart>
  )
}
