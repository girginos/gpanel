import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useState } from 'react'
import AramaliSecim from '@/components/AramaliSecim'
import { Ikon, I } from '@/components/Ikon'
import { Link, useSearchParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Domain = {
  id: number; alan_adi: string; sistem_kullanici: string
  boyut_kb: number; trafik_kb: number; durum: string
  php_surum?: string; is_demo?: boolean
  olusturulma?: string; plan_id?: number; plan_ad?: string
  sahip_ad?: string; sahip_tur?: string; reseller_id?: number
  ssl?: boolean
}
type SubGenel = {
  id: number; alt_ad: string; tam_ad: string
  parent_id: number; parent_ad: string
  sistem_kullanici: string; php_surum: string; docroot: string; olusturulma: string
}
type Plan = { id: number; ad: string; disk_kota_mb?: number }
type PHPVer = { surum: string; aciklama?: string }
type OlusturmaSonuc = {
  alan_adi: string; sistem_kullanici: string; ftp_user: string; ftp_host: string
  db_host: string; db_user: string; db_adi: string
  olusturulan_parolalar: { ftp: string; db: string }
}

// Panel sahibi mi? Bayi yalniz kendi domainlerini gordugu icin kolon ona kapali.
function panelSahibiMi() {
  try { return JSON.parse(localStorage.getItem('gosp.user') || '{}').rol === 'admin' } catch { return false }
}

// Domaini kimin actigini gosterir: ad soyad (yoksa kullanici adi) + rol rozeti.
function SahipHucre({ ad, tur }: { ad?: string; tur?: string }) {
  const cevir = useCevir()
  if (!ad) return <span className="text-slate-400 dark:text-slate-500 italic">—</span>
  const bayi = tur === 'bayi'
  return (
    <span className="inline-flex items-center gap-1.5 min-w-0">
      <span className="text-slate-700 dark:text-slate-300 truncate">{ad}</span>
      <span className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold shrink-0 ${
        bayi ? 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300'
             : 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400'}`}>
        {bayi ? cevir('bayi') : cevir('yönetici')}
      </span>
    </span>
  )
}

// Siteyi yeni sekmede acar. Sertifika varsa https, yoksa http denenir.
function SiteAc({ ad, ssl }: { ad: string; ssl?: boolean }) {
  return (
    <a href={`${ssl ? 'https' : 'http'}://${ad}`} target="_blank" rel="noopener noreferrer"
       title={cevirT("{0} sitesini yeni sekmede aç", ad)} aria-label={cevirT("{0} sitesini yeni sekmede aç", ad)}
       onClick={e => e.stopPropagation()}
       className="shrink-0 inline-flex items-center justify-center w-6 h-6 rounded-md text-slate-400 hover:text-brand-600 dark:text-slate-500 dark:hover:text-brand-400 hover:bg-slate-100 dark:hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-400 transition">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
           strokeLinecap="round" strokeLinejoin="round" className="w-3.5 h-3.5" aria-hidden="true">
        <path d="M14 4h6v6" /><path d="M20 4l-8.5 8.5" />
        <path d="M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5" />
      </svg>
    </a>
  )
}

function fmtKB(kb: number) {
  if (kb < 1024) return kb + ' KB'
  if (kb < 1024 * 1024) return (kb / 1024).toFixed(1) + ' MB'
  return (kb / 1024 / 1024).toFixed(2) + ' GB'
}


const DOMAINS_EN: Record<string, string> = {
  "(varsayılan)": "(default)",
  "Alan adı": "Domain name",
  "Başlangıç": "Start",
  "DNS kayıtları sıfırlansın mı?": "Reset DNS records?",
  "DNS varsayılana sıfırlama": "Reset DNS to default",
  "Domain Adı": "Domain Name",
  "Domain Oluştur": "Create Domain",
  "Domain Oluşturuldu": "Domain Created",
  "Domain oluşturulamadı": "Failed to create domain",
  "Durum değiştirme başarısız": "Status change failed",
  "Geçersiz alan adı. Örn: ornek.com veya panel.ornek.com": "Invalid domain name. E.g: example.com or panel.example.com",
  "Göster": "Show",
  "Henüz domain yok": "No domains yet",
  "Hosting planı": "Hosting plan",
  "Kopyalamak için Ctrl+C, sonra Enter:": "To copy press Ctrl+C, then Enter:",
  "Kopyalandı": "Copied",
  "Kullanıcı": "User",
  "MySQL Veritabanı": "MySQL Database",
  "Müşteri": "Customer",
  "Müşteri ara…": "Search customer…",
  "Müşteri yok": "No customers",
  "Oluştur": "Create",
  "Oluşturulma": "Created",
  "Oluşturuluyor…": "Creating…",
  "PHP 8.3 (varsayılan)": "PHP 8.3 (default)",
  "Plan bulunamadı": "Plan not found",
  "Plan değiştirme": "Change plan",
  "Sahip değiştirme": "Change owner",
  "Seçiniz…": "Select…",
  "Sistem Kullanıcısı": "System User",
  "Toplu işlem başlatılamadı": "Failed to start bulk operation",
  "Toplu plan değiştir": "Bulk change plan",
  "Toplu sahip değiştir": "Bulk change owner",
  "Tümünü seç": "Select all",
  "Tıklayarak kopyala": "Click to copy",
  "Vazgeç": "Cancel",
  "Veritabanı": "Database",
  "Yeni Domain Oluştur": "Create New Domain",
  "Yeni parola işlem sonucunda gösterilir": "New password is shown after the operation",
  "Yeni plan limitleri (disk, inode, FPM) her domaine uygulanır.": "New plan limits (disk, inode, FPM) apply to every domain.",
  "Yönet →": "Manage →",
  "Yükleniyor…": "Loading…",
  "geri dönüşsüz": "irreversible",
  "kısmen tamamlandı": "partially completed",
  "parolası:": "password:",
  "sağlamlandı. Aşağıdaki parolalar": "secured. The passwords below",
  "tamamlandı": "completed",
  "uyarı": "warning",
  "yönetici": "admin",
  "yükleniyor…": "loading…",
  "Özel": "Custom",
  "çalışıyor": "running",
  "İlk domain'inizi ekleyerek başlayın. Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL veritabanı ve DNS zone otomatik oluşturulur.": "Start by adding your first domain. Linux user, nginx vhost, PHP-FPM pool, FTP account, MySQL database and DNS zone are created automatically.",
  "İptal": "Cancel",
  "İstemezseniz kapalı bırakın — sonradan Veritabanları sekmesinden açabilirsiniz.": "Leave it off if you prefer — you can enable it later from the Databases tab.",
  "İşlemler": "Actions",
  "Küçük harf, rakam, tire ve nokta. Örn:": "Lowercase, digits, hyphen and dot. E.g:",
}

function useCevir() {
  const { i18n } = useTranslation()
  return (tr: string) => (i18n.language === "en" ? (DOMAINS_EN[tr] || ORTAK_EN[tr] || tr) : tr)
}

export default function DomainsPage() {
  const cevir = useCevir()

  const { sor, onay } = useDialog()
  const sahipKolonu = panelSahibiMi()
  const [items, setItems] = useState<Domain[]>([])
  const [subler, setSubler] = useState<SubGenel[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [secili, setSecili] = useState<Set<number>>(new Set())
  // Toplu async islem (DNS reset / SSL / sahip / plan) — process bar ile
  const [topluIsID, setTopluIsID] = useState<number>(0)
  const [topluModal, setTopluModal] = useState<'' | 'sahip' | 'plan'>('')
  const [topluBayiler, setTopluBayiler] = useState<Array<{ id: number; kullanici: string; ad_soyad?: string }>>([])
  const [topluMusteriler, setTopluMusteriler] = useState<Array<{ id: number; ad: string }>>([])
  const [topluHedefBayi, setTopluHedefBayi] = useState(0)
  const [topluHedefMusteri, setTopluHedefMusteri] = useState(0)
  const [topluHedefPlan, setTopluHedefPlan] = useState(0)
  const [isleniyor, setIsleniyor] = useState(false)
  const [silOnay, setSilOnay] = useState(false)

  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [phpSurumler, setPhpSurumler] = useState<PHPVer[]>([])
  const [modalVeriYuk, setModalVeriYuk] = useState(false) // plan+php sürüm yüklemesi (listeyi BLOKLAMAZ)
  const [modalVeriGeldi, setModalVeriGeldi] = useState(false)
  const [olusturAcik, setOlusturAcik] = useState(false)
  const [olusturuluyor, setOlusturuluyor] = useState(false)
  const [olusturmaSonuc, setOlusturmaSonuc] = useState<OlusturmaSonuc | null>(null)
  const [fAlanAdi, setFAlanAdi] = useState('')
  const [fPHPSurum, setFPHPSurum] = useState('8.3')
  const [fPlanID, setFPlanID] = useState<number | ''>('')
  const [fSqlOlustur, setFSqlOlustur] = useState(false) // SQL varsayilan KAPALI

  // Liste yalnızca /domains'e bağlıdır. /plans + /php/versions (yavaş olabilen dnf keşfi)
  // listeyi BLOKLAMAZ — modal açılınca lazy çekilir. Böylece dnf yavaş/kilitliyken bile
  // cevir("Domainler") gelir gelmez render olur.
  function yukle() {
    setYuk(true)
    api.get<Domain[]>('/domains')
      .then(r => setItems(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
    api.get<SubGenel[]>('/subdomains')
      .then(r => setSubler(r.data))
      .catch(() => setSubler([]))
  }
  useEffect(() => {
    if (topluModal === 'sahip') {
      api.get('/resellers').then(r => setTopluBayiler((r.data?.bayiler || r.data || []) as never)).catch(() => {})
      api.get('/customers').then(r => setTopluMusteriler((r.data?.musteriler || r.data || []) as never)).catch(() => {})
    } else if (topluModal === 'plan') {
      // Toplu plan modalı planları modalVeriYukle üzerinden çeker; eskiden yalnız
      // "yeni domain" modalında yükleniyordu, o yüzden toplu planda liste boştu.
      modalVeriYukle()
    }
  }, [topluModal])

  useEffect(yukle, [])
  // SSL degisimi nginx reload tetikleyip sayfayi yenileyebilir → devam eden isi bul.
  useEffect(() => {
    api.get<{ is_id: number }>('/domains/toplu/aktif')
      .then(r => { if (r.data.is_id > 0) setTopluIsID(r.data.is_id) })
      .catch(() => { /* sessiz */ })
  }, [])

  // Alt alanlar da toplu seçime dahil — sol kutucukla seçilir, "🗑 Sil" ile domainlerle
  // birlikte silinir (kendi endpoint'i). Domain seçimiyle çakışmasın diye AYRI set.
  const [subSecili, setSubSecili] = useState<Set<number>>(new Set())
  function subTogga(id: number) {
    setSubSecili(prev => { const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n })
  }

  // Mobil alt gezinme cubugundaki "Yeni" eylemi buraya ?yeni=1 ile gelir.
  // Kipi acip parametreyi TEMIZLIYORUZ: aksi halde geri/yenilemede kip
  // tekrar tekrar acilir ve kullanici sikisir.
  const [aramaParam, setAramaParam] = useSearchParams()
  useEffect(() => {
    if (aramaParam.get('yeni') !== '1') return
    olusturAc()
    const kalan = new URLSearchParams(aramaParam)
    kalan.delete('yeni')
    setAramaParam(kalan, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [aramaParam])

  // Modal için gereken plan + php sürümleri — listeyi bloklamayan ayrı yükleme.
  // Modal ilk açılışında lazy çekilir; bir kez geldiyse tekrar çekmez.
  function modalVeriYukle() {
    if (modalVeriGeldi || modalVeriYuk) return
    setModalVeriYuk(true)
    Promise.all([
      api.get<Plan[]>('/plans').catch(() => ({ data: [] })),
      api.get<PHPVer[]>('/php/versions').catch(() => ({ data: [] })),
    ]).then(([pr, phpr]) => {
      const pl = pr.data as Plan[]
      setPlanlar(pl)
      setPhpSurumler(phpr.data as PHPVer[])
      setModalVeriGeldi(true)
      // Plan henüz seçilmediyse (modal veri gelmeden açıldıysa) varsayılanı ata.
      setFPlanID(prev => {
        if (prev !== '') return prev
        const v = pl.find(p => p.ad === cevir("Başlangıç")) || pl[0]
        return v ? v.id : ''
      })
    }).finally(() => setModalVeriYuk(false))
  }

  function olusturAc() {
    setHata(null); setBasari(null); setOlusturmaSonuc(null)
    // varsayılan plan = cevir("Başlangıç") (yoksa ilk plan, o da yoksa boş) — veri geldiyse hemen ata,
    // gelmediyse modalVeriYukle tamamlanınca atanır.
    const varsayilan = planlar.find(p => p.ad === cevir("Başlangıç")) || planlar[0]
    setFAlanAdi(''); setFPHPSurum('8.3'); setFPlanID(varsayilan ? varsayilan.id : '')
    setOlusturAcik(true)
    modalVeriYukle() // lazy: plan/php sürümleri henüz gelmediyse şimdi çek (listeyi bloklamaz)
  }

  async function olusturGonder(e: React.FormEvent) {
    e.preventDefault()
    setHata(null)
    const alanAdi = fAlanAdi.trim().toLowerCase()
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(alanAdi)) {
      setHata(cevir("Geçersiz alan adı. Örn: ornek.com veya panel.ornek.com"))
      return
    }
    setOlusturuluyor(true)
    try {
      const body: any = { alan_adi: alanAdi, php_surum: fPHPSurum }
      if (fPlanID !== '') body.plan_id = fPlanID
      if (fSqlOlustur) body.sql_olustur = true
      const r = await api.post<OlusturmaSonuc>('/domains', body)
      setOlusturAcik(false)
      setOlusturmaSonuc(r.data)
      setBasari(`✓ "${alanAdi}" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL DB ve DNS zone hazır.`)
      setTimeout(() => setBasari(null), 8000)
      yukle()
    } catch (e: any) {
      setHata(apiHata(e, cevir("Domain oluşturulamadı")))
    } finally {
      setOlusturuluyor(false)
    }
  }

  async function panoYaz(metin: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(metin); return true
      }
    } catch {}
    try {
      const ta = document.createElement('textarea')
      ta.value = metin; ta.style.position = 'fixed'; ta.style.opacity = '0'
      document.body.appendChild(ta); ta.select(); document.execCommand('copy')
      document.body.removeChild(ta); return true
    } catch {}
    try { (await sor({ baslik: cevir("Kopyalamak için Ctrl+C, sonra Enter:"), varsayilan: metin })); return true } catch {}
    return false
  }

  const filtreli = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) return items
    const subParents = new Set(subler.filter(sd => sd.tam_ad.toLowerCase().includes(s) || sd.sistem_kullanici.toLowerCase().includes(s)).map(sd => sd.parent_id))
    return items.filter(d => d.alan_adi.toLowerCase().includes(s) || d.sistem_kullanici.toLowerCase().includes(s)
      || (d.sahip_ad || '').toLowerCase().includes(s) || subParents.has(d.id))
  }, [items, q, subler])

  const sublerByParent = useMemo(() => {
    const s = q.trim().toLowerCase()
    const m = new Map<number, SubGenel[]>()
    for (const sd of subler) {
      if (s && !sd.tam_ad.toLowerCase().includes(s) && !sd.sistem_kullanici.toLowerCase().includes(s)) continue
      const arr = m.get(sd.parent_id) || []
      arr.push(sd); m.set(sd.parent_id, arr)
    }
    return m
  }, [subler, q])

  function togga(id: number) {
    setSecili(prev => {
      const yeni = new Set(prev)
      if (yeni.has(id)) yeni.delete(id); else yeni.add(id)
      return yeni
    })
  }
  function tumunuSec(secVar: boolean) {
    if (secVar) setSecili(new Set(filtreli.map(d => d.id)))
    else setSecili(new Set())
  }

  async function topluSil() {
    setSilOnay(false); setIsleniyor(true); setHata(null)
    const ids = Array.from(secili); let basarili = 0
    for (const id of ids) {
      try { await api.delete(`/domains/${id}`); basarili++ } catch {}
    }
    // Seçili alt alanları da sil (kendi endpoint'i: parent üzerinden).
    const subIds = Array.from(subSecili); let subBasarili = 0
    for (const sid of subIds) {
      const sd = subler.find(s => s.id === sid); if (!sd) continue
      try { await api.delete(`/domains/${sd.parent_id}/subdomain/${sid}`); subBasarili++ } catch {}
    }
    setSecili(new Set()); setSubSecili(new Set())
    setBasari(`✓ ${basarili + subBasarili}/${ids.length + subIds.length} ${cevir("kayıt silindi")}`)
    setTimeout(() => setBasari(null), 4000)
    setIsleniyor(false); yukle()
  }

  // topluBaslat — asenkron toplu is baslatir; sayfa BLOKLANMAZ, ilerleme
  // TopluIsPaneli'nde process bar olarak akar (SSL'de nginx reload sayfayi
  // etkilerse bile is DB'de surer ve panel kaldigi yerden takip eder).
  async function topluBaslat(tip: 'dns_reset' | 'ssl_kur' | 'sahip' | 'plan', ek?: Record<string, unknown>) {
    const ids = Array.from(secili)
    if (!ids.length) return
    if (tip === 'dns_reset' && !(await onay({
      baslik: cevir("DNS kayıtları sıfırlansın mı?"),
      mesaj: cevirT("{0} domainin TÜM DNS kayıtları silinip şablondan yeniden oluşturulacak. Özel kayıtlar (MX, TXT, doğrulama kayıtları) KAYBOLUR.", ids.length),
      tehlike: true,
    }))) return
    try {
      const { data } = await api.post<{ is_id: number }>('/domains/toplu/is', { tip, ids, ...(ek || {}) })
      setTopluIsID(data.is_id); setTopluModal('')
    } catch (e) { setHata(apiHata(e, cevir("Toplu işlem başlatılamadı"))) }
  }

  async function durumDegistir(yeniDurum: 'aktif' | 'pasif') {
    setIsleniyor(true); setHata(null)
    const ids = Array.from(secili)
    try {
      await api.post('/domains/toplu/durum', { ids, durum: yeniDurum })
      setBasari(`✓ ${ids.length} domain "${yeniDurum}" durumuna geçirildi`)
      setTimeout(() => setBasari(null), 4000)
      setSecili(new Set()); yukle()
    } catch (e) { setHata(apiHata(e, cevir("Durum değiştirme başarısız"))) }
    finally { setIsleniyor(false) }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: cevir("Domainler") }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-2">{cevir("Domainler")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        {cevir(cevir("Tüm kayıtlı domainlerinizi listeleyin, toplu seçim ile durum değiştirin veya silin."))}
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Toolbar */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <div className="flex-1 max-w-md">
          <input type="text" value={q} onChange={e => setQ(e.target.value)}
            placeholder="🔍 Domain ara..."
            className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none" />
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-500">{filtreli.length} / {items.length}</span>
        <button onClick={olusturAc}
          className="ml-auto inline-flex items-center gap-1.5 text-sm px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-md font-medium shadow-sm">
          <span className="text-base leading-none">+</span> Yeni Domain
        </button>
      </div>

      {topluIsID > 0 && (
        <TopluIsPaneli isID={topluIsID} onBitti={() => { setTopluIsID(0); setSecili(new Set()); yukle() }} />
      )}

      {/* Toplu işlem barı */}
      {(secili.size + subSecili.size) > 0 && (
        <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-300 dark:border-amber-700 rounded-md flex items-center gap-2 flex-wrap">
          <span className="text-sm font-semibold text-amber-800 dark:text-amber-200">
            {secili.size + subSecili.size} seçili{subSecili.size > 0 ? ` (${subSecili.size} alt alan)` : ''}
          </span>
          {secili.size > 0 && <>
            <button onClick={() => durumDegistir('aktif')} disabled={isleniyor}
              className="text-xs px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded">
              <span className="inline-flex items-center gap-1.5"><Ikon d={I.oynat} /> Aktif Et</span>
            </button>
            <button onClick={() => durumDegistir('pasif')} disabled={isleniyor}
              className="text-xs px-3 py-1.5 bg-slate-600 hover:bg-slate-700 text-white rounded">
              <span className="inline-flex items-center gap-1.5"><Ikon d={I.durakla} /> Pasif Et</span>
            </button>
          </>}
          {secili.size > 0 && <>
            <button onClick={() => topluBaslat('dns_reset')} disabled={isleniyor || topluIsID > 0}
              className="text-xs px-3 py-1.5 bg-sky-600 hover:bg-sky-700 text-white rounded">
              {cevir(cevir("DNS varsayılana sıfırla"))}
            </button>
            <button onClick={() => topluBaslat('ssl_kur')} disabled={isleniyor || topluIsID > 0}
              className="text-xs px-3 py-1.5 bg-indigo-600 hover:bg-indigo-700 text-white rounded">
              SSL kur (Let's Encrypt)
            </button>
            <button onClick={() => setTopluModal('sahip')} disabled={isleniyor || topluIsID > 0}
              className="text-xs px-3 py-1.5 bg-violet-600 hover:bg-violet-700 text-white rounded">
              {cevir(cevir("Sahip değiştir"))}
            </button>
            <button onClick={() => setTopluModal('plan')} disabled={isleniyor || topluIsID > 0}
              className="text-xs px-3 py-1.5 bg-teal-600 hover:bg-teal-700 text-white rounded">
              {cevir(cevir("Plan değiştir"))}
            </button>
          </>}
          <button onClick={() => setSilOnay(true)} disabled={isleniyor}
            className="text-xs px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded font-medium">
            <span className="inline-flex items-center gap-1.5"><Ikon d={I.cop} /> Sil ({secili.size + subSecili.size})</span>
          </button>
          <button onClick={() => { setSecili(new Set()); setSubSecili(new Set()) }} disabled={isleniyor}
            className="text-xs px-3 py-1.5 border border-amber-300 dark:border-amber-700 text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:bg-amber-900/30 rounded">
            {cevir(cevir("Seçimi temizle"))}
          </button>
        </div>
      )}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState baslik={cevir("Henüz domain yok")}
          aciklama={cevir("İlk domain'inizi ekleyerek başlayın. Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL veritabanı ve DNS zone otomatik oluşturulur.")}
          buton={{ etiket: cevir("Domain Oluştur"), onClick: olusturAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
              <tr>
                <th className={`${T.baslik} w-10 text-center`}>
                  <input type="checkbox"
                    checked={filtreli.length > 0 && secili.size === filtreli.length}
                    ref={ref => { if (ref) ref.indeterminate = secili.size > 0 && secili.size < filtreli.length }}
                    onChange={e => tumunuSec(e.target.checked)}
                    aria-label={cevir("Tümünü seç")}
                    className="cursor-pointer" />
                </th>
                <th className={T.baslik}>{cevir("Domain Adı")}</th>
                <th className={T.baslik}>{cevir("Sistem Kullanıcısı")}</th>
                {sahipKolonu && <th className={T.baslik}>{cevir("Kullanıcı")}</th>}
                <th className={T.baslik}>Plan</th>
                <th className={T.baslik}>PHP</th>
                <th className={T.baslik}>Disk</th>
                <th className={T.baslik}>Durum</th>
                <th className={T.baslik}>{cevir("Oluşturulma")}</th>
                <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
              </tr>
            </thead>
            <tbody className={T.govde}>
              {filtreli.flatMap(d => {
                const subs = sublerByParent.get(d.id) || []
                const parentRow = (
                  <tr key={d.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800 transition ${secili.has(d.id) ? 'ring-2 ring-brand-400 lg:ring-0 bg-brand-50 dark:bg-brand-900/20' : ''}`}>
                    <td className={T.hucreSecim}>
                      <input type="checkbox" checked={secili.has(d.id)}
                        onChange={() => togga(d.id)}
                        aria-label={cevirT(cevir("{0} seç"), d.alan_adi)}
                        className="cursor-pointer" />
                    </td>
                    <td className={T.hucreBaslikSecimli}>
                      <span className="inline-flex items-center gap-1.5 min-w-0 align-middle">
                      <Link to={`/abonelikler/${d.id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300">
                        {d.alan_adi}
                      </Link>
                      <SiteAc ad={d.alan_adi} ssl={d.ssl} />
                      {d.is_demo && <span className="text-[10px] uppercase tracking-wider bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 px-1.5 py-0.5 rounded">DEMO</span>}
                      </span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Sistem Kullanıcısı")}>
                      <span title={d.sistem_kullanici} className="font-mono text-xs text-slate-600 dark:text-slate-400 inline-block max-w-[8rem] truncate align-bottom lg:text-left">{d.sistem_kullanici}</span>
                    </td>
                    {sahipKolonu && (
                      <td className={T.hucre} data-etiket={cevir("Kullanıcı")}>
                        <SahipHucre ad={d.sahip_ad} tur={d.sahip_tur} />
                      </td>
                    )}
                    <td className={T.hucre} data-etiket="Plan">
                      {!d.plan_ad
                        ? <span className="text-slate-400 dark:text-slate-500 italic">—</span>
                        : (/—\s*Özel\s*$/.test(d.plan_ad) || d.plan_ad.trim() === cevir("Özel"))
                          ? <span title={d.plan_ad} className="inline-flex rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-600 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300">{cevir("Özel")}</span>
                          : <span className="text-slate-700 dark:text-slate-300">{d.plan_ad}</span>}
                    </td>
                    <td className={T.hucre} data-etiket="PHP">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400">{d.php_surum || '-'}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Disk">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtKB(d.boyut_kb)}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Durum">
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${
                        d.durum === 'aktif' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400'
                      }`}>{d.durum}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Oluşturulma")}>
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 whitespace-nowrap">{d.olusturulma || '-'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Link to={`/abonelikler/${d.id}/subdomainler`} className="text-xs text-slate-500 dark:text-slate-400 hover:text-brand-600 dark:hover:text-brand-400 lg:mr-3">+ Subdomain</Link>
                      <Link to={`/abonelikler/${d.id}`} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 ml-auto lg:ml-0">{cevir("Yönet →")}</Link>
                    </td>
                  </tr>
                )
                const subRows = subs.map(sd => (
                  <tr key={'sub-' + sd.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/50 transition bg-slate-50/40 dark:bg-slate-900/20`}>
                    <td className={T.hucreSecim}>
                      <input type="checkbox" checked={subSecili.has(sd.id)}
                        onChange={() => subTogga(sd.id)}
                        aria-label={cevirT(cevir("{0} seç"), sd.tam_ad)}
                        className="cursor-pointer" />
                    </td>
                    <td className={T.hucreBaslikSecimli}>
                      <span className="inline-flex items-center gap-1.5 lg:pl-5">
                        <span className="text-slate-300 dark:text-slate-600 select-none hidden lg:inline">└─</span>
                        <Link to={`/abonelikler/${sd.parent_id}/subdomainler/${sd.id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300">{sd.tam_ad}</Link>
                        <SiteAc ad={sd.tam_ad} ssl={d.ssl} />
                        <span className="text-[9px] uppercase tracking-wider bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300 px-1.5 py-0.5 rounded align-middle">alt alan</span>
                      </span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Sistem Kullanıcısı")}>
                      <span title={sd.sistem_kullanici} className="font-mono text-xs text-slate-500 dark:text-slate-400 inline-block max-w-[8rem] truncate align-bottom">{sd.sistem_kullanici}</span>
                    </td>
                    {sahipKolonu && (
                      <td className={T.hucre} data-etiket={cevir("Kullanıcı")}>
                        <span className="text-xs text-slate-400 dark:text-slate-500 truncate">{d.sahip_ad || '—'}</span>
                      </td>
                    )}
                    <td className={T.hucre} data-etiket="Plan">
                      <span className="text-slate-400 dark:text-slate-500 italic">—</span>
                    </td>
                    <td className={T.hucre} data-etiket="PHP">
                      <span className="font-mono text-xs text-slate-500 dark:text-slate-400">{sd.php_surum || '-'}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Disk">
                      <span className="text-slate-300 dark:text-slate-600">—</span>
                    </td>
                    <td className={T.hucre} data-etiket="Durum">
                      <span className="text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300">aktif</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Oluşturulma")}>
                      <span className="font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{sd.olusturulma || '-'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Link to={`/abonelikler/${sd.parent_id}/subdomainler/${sd.id}`} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300">{cevir("Yönet →")}</Link>
                    </td>
                  </tr>
                ))
                return [parentRow, ...subRows]
              })}
            </tbody>
          </table>
          </div>
        </div>
      )}

      {/* Domain Oluştur Modal */}
      {olusturAcik && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => !olusturuluyor && setOlusturAcik(false)}>
          <form onSubmit={olusturGonder} className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Yeni Domain Oluştur")}</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              {cevir(cevir("Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL veritabanı ve DNS zone otomatik kurulur."))}
            </p>

            {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Alan adı")} <span className="text-red-500">*</span></label>
                <input
                  type="text"
                  value={fAlanAdi}
                  onChange={e => setFAlanAdi(e.target.value)}
                  placeholder="ornek.com"
                  autoFocus
                  required
                  disabled={olusturuluyor}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/15 outline-none"
                />
                <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">{cevir("Küçük harf, rakam, tire ve nokta. Örn:")} <span className="font-mono">site.com</span> veya <span className="font-mono">panel.site.com</span></div>
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">
                  {cevir(cevir("PHP Sürümü"))}
                  {modalVeriYuk && phpSurumler.length === 0 && <span className="ml-2 text-[11px] text-slate-400 dark:text-slate-500">{cevir("yükleniyor…")}</span>}
                </label>
                <select
                  value={fPHPSurum}
                  onChange={e => setFPHPSurum(e.target.value)}
                  disabled={olusturuluyor}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none bg-white dark:bg-slate-800"
                >
                  {phpSurumler.length === 0
                    ? <option value="8.3">{cevir("PHP 8.3 (varsayılan)")}</option>
                    : phpSurumler.map(p => (
                        <option key={p.surum} value={p.surum}>PHP {p.surum}{/default|appstream/i.test(p.aciklama || '') ? ' (varsayılan)' : ''}</option>
                      ))
                  }
                </select>
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">
                  {cevir(cevir("Hizmet Planı"))}
                  {modalVeriYuk && planlar.length === 0 && <span className="ml-2 text-[11px] text-slate-400 dark:text-slate-500">{cevir("yükleniyor…")}</span>}
                </label>
                <AramaliSecim
                  deger={fPlanID === '' ? 0 : fPlanID}
                  onDegis={v => setFPlanID(Number(v) === 0 ? '' : Number(v))}
                  yerTutucu="— (yok) —"
                  aramaYerTutucu="Plan ara…"
                  bosMetin={cevir("Plan bulunamadı")}
                  yukleniyor={modalVeriYuk && planlar.length === 0}
                  secenekler={[
                    { id: 0, etiket: '— (yok) —' },
                    ...planlar.map(p => ({ id: p.id, etiket: p.ad })),
                  ]}
                />
              </div>
            </div>

            <label className="flex items-start gap-2.5 mt-4 cursor-pointer select-none">
              <input type="checkbox" checked={fSqlOlustur} onChange={e => setFSqlOlustur(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              <span className="text-sm text-slate-700 dark:text-slate-300">
                {cevir(cevir("MySQL veritabanı oluştur"))}
                <span className="block text-xs text-slate-400 dark:text-slate-500">{cevir("İstemezseniz kapalı bırakın — sonradan Veritabanları sekmesinden açabilirsiniz.")}</span>
              </span>
            </label>

            <div className="flex justify-end gap-2 mt-5">
              <button type="button" onClick={() => setOlusturAcik(false)} disabled={olusturuluyor}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">{cevir("İptal")}</button>
              <button type="submit" disabled={olusturuluyor || !fAlanAdi.trim()}
                className="px-4 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm rounded font-medium inline-flex items-center gap-2">
                {olusturuluyor && (
                  <svg className="animate-spin w-3.5 h-3.5" viewBox="0 0 24 24" fill="none">
                    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3"/>
                    <path d="M22 12a10 10 0 0 1-10 10" stroke="currentColor" strokeWidth="3"/>
                  </svg>
                )}
                {olusturuluyor ? 'Oluşturuluyor…' : cevir("Oluştur")}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Oluşturma Sonucu Modal (FTP + DB parolaları) */}
      {olusturmaSonuc && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setOlusturmaSonuc(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-emerald-700 dark:text-emerald-300 mb-1"><span className="inline-flex items-center gap-2"><Ikon d={I.onay} className="h-5 w-5" /> {cevir("Domain Oluşturuldu")}</span></h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{olusturmaSonuc.alan_adi}</span> {cevir("sağlamlandı. Aşağıdaki parolalar")} <strong>sadece bir kez</strong> gösterilir — güvenli bir yere kaydedin.
            </p>

            <div className="space-y-3">
              <div className="border border-slate-200 dark:border-slate-700 rounded-md p-3 bg-slate-50 dark:bg-slate-900">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">FTP</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.ftp_host || '—'} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Kullanıcı")} v={olusturmaSonuc.ftp_user} kopyala={panoYaz} />
                <KopyaSatir e="Parola" v={olusturmaSonuc.olusturulan_parolalar.ftp} kopyala={panoYaz} parola />
              </div>

              <div className="border border-slate-200 dark:border-slate-700 rounded-md p-3 bg-slate-50 dark:bg-slate-900">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">{cevir("MySQL Veritabanı")}</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.db_host || 'localhost'} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Veritabanı")} v={olusturmaSonuc.db_adi} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Kullanıcı")} v={olusturmaSonuc.db_user} kopyala={panoYaz} />
                <KopyaSatir e="Parola" v={olusturmaSonuc.olusturulan_parolalar.db} kopyala={panoYaz} parola />
              </div>

              <div className="text-[11px] text-slate-500 dark:text-slate-500 italic">
                {cevir(cevir("Sistem kullanıcısı:"))} <span className="font-mono">{olusturmaSonuc.sistem_kullanici}</span>
              </div>
            </div>

            <div className="flex justify-end mt-5">
              <button onClick={() => setOlusturmaSonuc(null)}
                className="px-4 py-1.5 bg-slate-700 hover:bg-slate-800 text-white text-sm rounded">Tamam</button>
            </div>
          </div>
        </div>
      )}

      {/* Toplu Sil Onay */}
      {/* topluHedefModal — sahip/plan hedefi secimi (async is bundan sonra baslar) */}
      {topluModal !== '' && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setTopluModal('')}>
          <div className="w-full max-w-md rounded-2xl bg-white p-5 dark:bg-slate-800" onClick={e => e.stopPropagation()}>
            <h3 className="mb-1 text-base font-semibold text-slate-900 dark:text-slate-100">
              {topluModal === 'sahip' ? 'Toplu sahip değiştir' : 'Toplu plan değiştir'}
            </h3>
            <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">{secili.size} domain etkilenecek.</p>
            {topluModal === 'sahip' && (
              <div className="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[11px] leading-relaxed text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/30 dark:text-amber-200">
                {cevir(cevir("Devir eski sahibin erişimini keser: FTP/SSH parolası yenilenir, açık müşteri"))}
                oturumları düşer, Git webhook anahtarı değişir, SSH anahtarları arşivlenir,
                uzak yedek hedefi ve GitHub bağlantısı kaldırılır.
                <b className="font-semibold"> {cevir("Yeni parola işlem sonucunda gösterilir")}</b> — kaydedin.
              </div>
            )}
            {topluModal === 'sahip' ? (
              <div className="space-y-3">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Bayi (sahip)</span>
                  <AramaliSecim
                    deger={topluHedefBayi}
                    onDegis={v => setTopluHedefBayi(Number(v))}
                    yerTutucu="Ana hesap (admin)"
                    aramaYerTutucu="Bayi ara…"
                    secenekler={[
                      { id: 0, etiket: 'Ana hesap (admin)' },
                      ...topluBayiler.map(b => ({ id: b.id, etiket: b.ad_soyad ? `${b.ad_soyad} (${b.kullanici})` : b.kullanici, altEtiket: b.ad_soyad ? b.kullanici : undefined })),
                    ]}
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Müşteri")}</span>
                  <AramaliSecim
                    deger={topluHedefMusteri}
                    onDegis={v => setTopluHedefMusteri(Number(v))}
                    yerTutucu="Yok"
                    aramaYerTutucu={cevir("Müşteri ara…")}
                    bosMetin={cevir("Müşteri yok")}
                    secenekler={[
                      { id: 0, etiket: 'Yok' },
                      ...topluMusteriler.map(m => ({ id: m.id, etiket: m.ad })),
                    ]}
                  />
                </label>
              </div>
            ) : (
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Hosting planı")}</span>
                <AramaliSecim
                  deger={topluHedefPlan || null}
                  onDegis={v => setTopluHedefPlan(Number(v))}
                  yerTutucu={cevir("Seçiniz…")}
                  aramaYerTutucu="Plan ara…"
                  bosMetin={cevir("Plan bulunamadı")}
                  yukleniyor={modalVeriYuk && planlar.length === 0}
                  secenekler={planlar.map(p => ({ id: p.id, etiket: p.ad }))}
                />
                <span className="mt-1 block text-[11px] text-slate-400">{cevir("Yeni plan limitleri (disk, inode, FPM) her domaine uygulanır.")}</span>
              </label>
            )}
            <div className="mt-5 flex justify-end gap-2">
              <button type="button" onClick={() => setTopluModal('')}
                className="rounded-lg border border-slate-300 px-3.5 py-2 text-sm dark:border-slate-600 dark:text-slate-200">{cevir("Vazgeç")}</button>
              <button type="button"
                disabled={topluModal === 'plan' && topluHedefPlan <= 0}
                onClick={() => topluModal === 'sahip'
                  ? topluBaslat('sahip', { reseller_id: topluHedefBayi, customer_id: topluHedefMusteri })
                  : topluBaslat('plan', { plan_id: topluHedefPlan })}
                className="rounded-lg bg-brand-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50">
                Uygula ({secili.size})
              </button>
            </div>
          </div>
        </div>
      )}

      {silOnay && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setSilOnay(false)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-red-700 dark:text-red-300 mb-2"><span className="inline-flex items-center gap-2"><Ikon d={I.uyari} className="h-5 w-5" /> Toplu Silme</span></h3>
            <p className="text-sm text-slate-700 dark:text-slate-300 mb-3">
              <span className="font-semibold">{secili.size}</span> domain{subSecili.size > 0 ? <> + <span className="font-semibold">{subSecili.size}</span> alt alan</> : ''} ve tüm bağımlı kaynakları (Linux kullanıcı, ev dizini, DB, FTP, vhost, DNS zone) <strong>{cevir("geri dönüşsüz")}</strong> silinecek.
            </p>
            <ul className="text-xs font-mono text-slate-500 dark:text-slate-500 bg-slate-50 dark:bg-slate-900 rounded p-2 max-h-40 overflow-auto mb-4">
              {Array.from(secili).slice(0, 8).map(id => {
                const d = items.find(x => x.id === id)
                return <li key={id} className="truncate">{d?.alan_adi || '?'}</li>
              })}
              {secili.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {secili.size - 8} daha…</li>}
              {Array.from(subSecili).slice(0, 8).map(sid => {
                const s = subler.find(x => x.id === sid)
                return <li key={'s' + sid} className="truncate text-teal-600 dark:text-teal-400">{s?.tam_ad || '?'} <span className="text-[10px]">(alt alan)</span></li>
              })}
              {subSecili.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {subSecili.size - 8} alt alan daha…</li>}
            </ul>
            <div className="flex justify-end gap-2">
              <button onClick={() => setSilOnay(false)}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">{cevir("İptal")}</button>
              <button onClick={topluSil} disabled={isleniyor}
                className="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded font-medium">
                Evet, Sil
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function KopyaSatir({ e, v, kopyala, parola }: { e: string; v: string; kopyala: (m: string) => Promise<boolean>; parola?: boolean }) {
  const cevir = useCevir()
  const [kopyalandi, setKopyalandi] = useState(false)
  const [acik, setAcik] = useState(!parola)
  async function tikla() {
    const ok = await kopyala(v)
    if (ok) { setKopyalandi(true); setTimeout(() => setKopyalandi(false), 1500) }
  }
  return (
    <div className="flex items-center gap-2 text-xs py-1">
      <span className="w-24 text-slate-500 dark:text-slate-500 shrink-0">{e}</span>
      <code
        onClick={tikla}
        className={`flex-1 font-mono px-2 py-1 rounded border cursor-pointer select-all transition ${
          kopyalandi ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 hover:border-brand-400 text-slate-800 dark:text-slate-200'
        }`}
        title={cevir("Tıklayarak kopyala")}
      >
        {parola && !acik ? '••••••••••' : v}
      </code>
      {parola && (
        <button type="button" onClick={() => setAcik(s => !s)}
          className="text-[10px] px-1.5 py-0.5 rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">
          {acik ? 'Gizle' : cevir("Göster")}
        </button>
      )}
      {kopyalandi && <span className="text-[10px] text-emerald-600 dark:text-emerald-400 font-semibold">{cevir("Kopyalandı")}</span>}
    </div>
  )
}

// TopluIsPaneli — asenkron toplu islemin CANLI ilerlemesi (process bar).
// 1sn'de bir poll eder; is bitince ozet + hata detaylarini gosterir.
// 🔴 Ag hatasinda (SSL degisimi sirasinda nginx reload) POLL DURMAZ — birkac
// deneme sessizce yutulur, baglanti gelince kaldigi yerden devam eder.
function TopluIsPaneli({ isID, onBitti }: { isID: number; onBitti: () => void }) {
  const cevir = useCevir()
  type Durum = {
    tip: string; durum: string; toplam: number; tamamlanan: number
    basari: number; hata: number; aktif_domain: string; detaylar?: string[]
  }
  const [d, setD] = useState<Durum | null>(null)
  const [kapandi, setKapandi] = useState(false)

  useEffect(() => {
    let dur = false
    const tik = async () => {
      try {
        const r = await api.get<Durum>(`/domains/toplu/is?id=${isID}`)
        if (dur) return
        setD(r.data)
        if (r.data.durum !== 'calisiyor') return // poll biter
      } catch { /* gecici ag hatasi — poll surer */ }
      if (!dur) setTimeout(tik, 1200)
    }
    tik()
    return () => { dur = true }
  }, [isID])

  if (!d || kapandi) return null
  const ADLAR: Record<string, string> = {
    dns_reset: 'DNS varsayılana sıfırlama', ssl_kur: "Let's Encrypt SSL kurulumu",
    sahip: 'Sahip değiştirme', plan: 'Plan değiştirme',
  }
  const yuzde = d.toplam > 0 ? Math.round((d.tamamlanan / d.toplam) * 100) : 0
  const bitti = d.durum !== 'calisiyor'
  const renk = d.durum === 'tamam' ? 'emerald' : d.durum === 'hata' ? 'red' : d.durum === 'kismi' ? 'amber' : 'brand'

  async function iptalEt() {
    try { await api.post(`/domains/toplu/is/iptal?id=${isID}`) } catch { /* */ }
  }

  return (
    <div className={`mb-3 rounded-xl border px-4 py-3 border-${renk}-200 bg-${renk}-50 dark:border-${renk}-800/60 dark:bg-${renk}-900/20`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-slate-800 dark:text-slate-100">
            {ADLAR[d.tip] || d.tip} — {bitti ? (d.durum === 'tamam' ? 'tamamlandı' : d.durum === 'kismi' ? 'kısmen tamamlandı' : d.durum) : 'çalışıyor'}
          </p>
          <p className="mt-0.5 text-xs text-slate-600 dark:text-slate-400">
            {d.tamamlanan}/{d.toplam} · ✓ {d.basari} başarılı{d.hata > 0 ? ` · ✗ ${d.hata} hata` : ''}
            {!bitti && d.aktif_domain ? cevirT(cevir(" · şu an: {0}"), d.aktif_domain) : ''}
          </p>
        </div>
        {bitti
          ? <button type="button" onClick={() => { setKapandi(true); onBitti() }}
              className="shrink-0 rounded-lg bg-slate-700 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-800">Kapat</button>
          : <button type="button" onClick={iptalEt}
              className="shrink-0 rounded-lg border border-red-300 px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-300">Durdur</button>}
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
        <div className={`h-full rounded-full transition-all duration-500 ${bitti && d.durum === 'tamam' ? 'bg-emerald-500' : bitti && d.durum === 'hata' ? 'bg-red-500' : 'bg-brand-500'}`}
          style={{ width: `${yuzde}%` }} />
      </div>
      {bitti && (d.detaylar?.length ?? 0) > 0 && (
        // Devir işi detay satırlarına yalnız hata değil, ADMIN'İN GÖRMESİ GEREKEN
        // sonuçları da yazar (yeni FTP/SSH parolası, devralınan cron uyarısı).
        // Hepsini "✗" ile kırmızı göstermek başarılı bir devri hata gibi okuturdu.
        <ul className="mt-2 max-h-40 space-y-0.5 overflow-auto text-[11px]">
          {d.detaylar!.slice(0, 20).map((x, i) => {
            const parola = x.includes(cevir("parolası:"))
            const uyari = !parola && (x.includes('dikkat:') || x.includes(cevir("uyarı")))
            return (
              <li key={i} className={parola
                ? 'text-emerald-700 dark:text-emerald-300 font-mono select-all'
                : uyari ? 'text-amber-700 dark:text-amber-300' : 'text-red-700 dark:text-red-300'}>
                {parola ? '🔑' : uyari ? '⚠' : '✗'} {x}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
