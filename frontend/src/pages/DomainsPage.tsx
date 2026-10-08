import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useState } from 'react'
import AramaliSecim from '@/components/AramaliSecim'
import { Ikon, I } from '@/components/Ikon'
import { Link, useSearchParams } from 'react-router-dom'
import { api, apiHata, sonucBelirsiz, UZUN_ISTEK } from '@/lib/api'
import { varsayilanPlanSec } from '@/lib/bayiPlan'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge, butonSinifi } from '@/components/ui'
import type { Color } from '@/components/ui'
import IPDegistirDiyalog from '@/components/IPDegistirDiyalog'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import type { Ton } from '@/components/tonlar'
import { useRol, yoneticiMi } from '@/lib/rol'
import BayiDomainler from '@/components/bayi/BayiDomainler'
import { KullanimHucre } from '@/components/bayi/Hucreler'

type Domain = {
  id: number; alan_adi: string; sistem_kullanici: string
  boyut_kb: number; trafik_kb: number; durum: string
  php_surum?: string; is_demo?: boolean
  olusturulma?: string; plan_id?: number; plan_ad?: string
  sahip_ad?: string; sahip_tur?: string; reseller_id?: number
  ssl?: boolean; ssl_bitis?: string; askida?: boolean; disk_kota_mb?: number; trafik_kota_mb?: number; ipv4?: string
}
type SubGenel = {
  id: number; alt_ad: string; tam_ad: string
  parent_id: number; parent_ad: string
  sistem_kullanici: string; php_surum: string; docroot: string; olusturulma: string
}
type Plan = { id: number; ad: string; disk_kota_mb?: number; varsayilan?: boolean; duzenlenebilir?: boolean }
type PHPVer = { surum: string; aciklama?: string; eol?: boolean }
type OlusturmaSonuc = {
  alan_adi: string; sistem_kullanici: string; ftp_user: string; ftp_host: string
  db_host: string; db_user: string; db_adi: string
  olusturulan_parolalar: { ftp: string; db: string }
}

// Domaini kimin actigini gosterir: ad soyad (yoksa kullanici adi) + rol rozeti.
function SahipHucre({ ad, tur }: { ad?: string; tur?: string }) {
  const cevir = useCevir()
  if (!ad) return <span className="text-slate-400 dark:text-slate-500 italic">—</span>
  const bayi = tur === 'bayi'
  return (
    <span className="inline-flex items-center gap-1.5 min-w-0 max-w-full lg:max-w-[8rem]" title={ad}>
      <span className="text-slate-700 dark:text-slate-300 truncate">{ad}</span>
      <Badge color={bayi ? 'primary' : 'neutral'} variant="soft"
        className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 font-semibold shrink-0">
        {bayi ? cevir('bayi') : cevir('yönetici')}
      </Badge>
    </span>
  )
}

// Domain durumu → Tailux Badge rengi (aktif: success, diğerleri: nötr).
function durumRenk(durum: string): Color {
  return durum === 'aktif' ? 'success' : 'neutral'
}

// Siteyi yeni sekmede acar. Sertifika varsa https, yoksa http denenir.
function SiteAc({ ad, ssl }: { ad: string; ssl?: boolean }) {
  const cevir = useCevir()
  return (
    <a href={`${ssl ? 'https' : 'http'}://${ad}`} target="_blank" rel="noopener noreferrer"
       title={cevirT(cevir("{0} sitesini yeni sekmede aç"), ad)} aria-label={cevirT(cevir("{0} sitesini yeni sekmede aç"), ad)}
       onClick={e => e.stopPropagation()}
       className={butonSinifi({ variant: 'flat', isIcon: true, className: 'size-6 rounded-md' })}>
      <Ikon d={I.harici} className="size-3.5" />
    </a>
  )
}

// Kök Domainler tablosu: sütunlar KAPSAYICI genişliğine göre kademeli gizlenir (container query).
// Ekran genişliği değil, tablonun gerçekten sahip olduğu alan ölçülür: sol rayın + alt menünün
// (Domainler/Bayiler) yediği ~316px düşüldükten sonra kalan genişlik. Gizleme yalnız masaüstü
// tablo kipinde (lg+) geçerlidir; mobil kartta tüm alanlar zaten alt alta gösterilir.
// Önem sırası (önce gizlenen): Oluşturulma > Sistem kullanıcısı > Disk > Kullanıcı (sahip) > Plan.
// Eşik = o sütun kümesinin EN KÖTÜ durumda (TR/EN etiketleri, uzun adlar kırpılma sınırında)
// ihtiyaç duyduğu en dar kapsayıcı genişliği (ölçüm: sütun taban genişlikleri toplamı) + ~14px pay.
// Satır içi "Subdomain" düğmesi kalkıp İşlemler yalnız "Yönet" kaldığından eşikler gevşetildi;
// taban (onay kutusu+Domain+Durum+İşlemler) ≈525px olduğundan 1024px'de bile taşma payı ≥24px.
// PHP sütunu kalktı (sürüm domain detayında/araçlarda): Oluşturulma eşiği 1150 -> 1103 (PHP ≈47px). Ölçüm: tüm sütunlar 1074px.
// `!` zorunlu: T.hucre'nin `lg:table-cell` kuralını sıra bağımsız ezmeli.
const KOL = {
  plan: '@max-[632px]:lg:hidden!',
  sahip: '@max-[786px]:lg:hidden!',
  disk: '@max-[863px]:lg:hidden!',
  trafik: '@max-[1003px]:lg:hidden!',
  olusturulma: '@max-[1103px]:lg:hidden!',
} as const



const DOMAINS_EN: Record<string, string> = {
  "Trafik": "Traffic",
  "ornek.com": "example.com",
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
  "Yönet": "Manage",
  "Üst domain": "Parent domain",
  "Domain seçin…": "Select a domain…",
  "Domain ara…": "Search domains…",
  "Yeni Subdomain": "New Subdomain",
  "Alt Alan": "Subdomain",
  "Subdomain Ekle": "Add Subdomain",
  "Küçük harf, rakam ve tire. Örn:": "Lowercase, digits and hyphen. E.g:",
  "{0} oluşturuldu. DNS A kaydı eklendi.": "{0} created. DNS A record added.",
  "Oluşturulamadı": "Could not be created",
  "Kaydedildi": "Saved",
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
  "İlk domain'inizi ekleyerek başlayın. Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı ve DNS zone otomatik oluşturulur.": "Start by adding your first domain. Linux user, nginx vhost, PHP-FPM pool, FTP account and DNS zone are created automatically.",
  "İptal": "Cancel",
  "İstemezseniz kapalı bırakın — sonradan Veritabanları sekmesinden açabilirsiniz.": "Leave it off if you prefer — you can enable it later from the Databases tab.",
  "İşlemler": "Actions",
  "Küçük harf, rakam, tire ve nokta. Örn:": "Lowercase, digits, hyphen and dot. E.g:",
  "Türkçe": "English",
  "Anasayfa": "Home",
  "bayi": "reseller",
  "aktif": "active",
  "Durum": "Status",
  "seçili": "selected",
  "alt alan": "subdomain",
  "Yeni Domain": "New Domain",
  "Aktif Et": "Activate",
  "Pasif Et": "Deactivate",
  "SSL kur (Let's Encrypt)": "Install SSL (Let's Encrypt)",
  "Sil": "Delete",
  "Domain ara...": "Search domain...",
  "{0} sitesini yeni sekmede aç": "Open site {0} in a new tab",
  "\"{0}\" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL DB ve DNS zone hazır.": "\"{0}\" created — Linux user, nginx vhost, PHP-FPM pool, FTP account, MySQL DB and DNS zone are ready.",
  "\"{0}\" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı ve DNS zone hazır.": "\"{0}\" created — Linux user, nginx vhost, PHP-FPM pool, FTP account and DNS zone are ready.",
  "güncelleme almıyor": "no longer updated",
  "{0} domainin TÜM DNS kayıtları silinip şablondan yeniden oluşturulacak. Özel kayıtlar (MX, TXT, doğrulama kayıtları) KAYBOLUR.": "ALL DNS records of {0} domain(s) will be deleted and recreated from the template. Custom records (MX, TXT, verification records) WILL BE LOST.",
  "{0} domain \"{1}\" durumuna geçirildi": "{0} domain(s) switched to \"{1}\" status",
  "domain etkilenecek.": "domain(s) will be affected.",
  "Bayi (sahip)": "Reseller (owner)",
  "Ana hesap (admin)": "Main account (admin)",
  "Bayi ara…": "Search reseller…",
  "Yok": "None",
  "Plan ara…": "Search plan…",
  "— (yok) —": "— (none) —",
  "Uygula": "Apply",
  "Toplu Silme": "Bulk Delete",
  "ve tüm bağımlı kaynakları (Linux kullanıcı, ev dizini, DB, FTP, vhost, DNS zone)": "and all dependent resources (Linux user, home directory, DB, FTP, vhost, DNS zone)",
  "silinecek.": "will be deleted.",
  "daha…": "more…",
  "(alt alan)": "(subdomain)",
  "alt alan daha…": "more subdomains…",
  "Evet, Sil": "Yes, Delete",
  "Gizle": "Hide",
  "Parola": "Password",
  "Tamam": "OK",
  "Kapat": "Close",
  "Durdur": "Stop",
  "Let's Encrypt SSL kurulumu": "Let's Encrypt SSL installation",
  "başarılı": "successful",
  "hata": "error",
  "kayıt silindi": "records deleted",
  "oturumları düşer, Git webhook anahtarı değişir, SSH anahtarları arşivlenir, uzak yedek hedefi ve GitHub bağlantısı kaldırılır.": "sessions are dropped, the Git webhook key changes, SSH keys are archived, the remote backup target and GitHub connection are removed.",
  "— kaydedin.": "— save it.",
  "Oluşturuldu": "Created",
  "İşlem başarısız": "Operation failed",
  "IP değiştir": "Change IP",
  "IP değiştirme": "IP change",
  "Bayi silme": "Reseller deletion",
  "Durum kontrol ediliyor…": "Checking status…",
  "Sonuç belirsiz": "Result unknown",
  "Kuruluyor… ({0} sn)": "Provisioning… ({0} s)",
  "Linux kullanıcısı, web sunucusu, PHP havuzu, FTP ve DNS hazırlanıyor; genellikle 10–40 sn sürer. Sayfadan ayrılsanız da işlem sunucuda tamamlanır.": "Setting up the Linux user, web server, PHP pool, FTP and DNS; this usually takes 10–40 s. The server finishes the job even if you leave this page.",
  "Sunucu yanıtı gecikti — alan adının oluşup oluşmadığı kontrol ediliyor…": "The server response is late — checking whether the domain was created…",
  '"{0}" oluşturuldu (sunucu yanıtı gecikti). Bu yüzden oluşturma parolaları gösterilemedi — FTP parolasını hosting sayfasından yenileyebilirsiniz.': '"{0}" was created (the server response was late), so the generated passwords could not be shown — you can reset the FTP password on the hosting page.',
  "Sunucudan yanıt alınamadı ve alan adı henüz listede görünmüyor. İşlem sunucuda sürüyor olabilir — tekrar oluşturmadan önce birkaç dakika sonra listeyi yenileyin.": "No response from the server and the domain is not in the list yet. It may still be in progress on the server — refresh the list in a few minutes before trying again.",
}

function useCevir() {
  const { i18n } = useTranslation()
  return (tr: string) => (i18n.language === "en" ? (DOMAINS_EN[tr] || ORTAK_EN[tr] || tr) : tr)
}

export default function DomainsPage() {
  const cevir = useCevir()

  const { sor, onay } = useDialog()
  const toast = useToast()
  // Rol TEK kaynaktan (useAuth kullanici.rol). Panel sahibi değilse (bayi):
  //  - sahip kolonu yok (bayi yalnız kendi hesaplarını görür),
  //  - AdminOnly toplu işler (POST/GET /domains/toplu/is, GET /domains/toplu/aktif) ve
  //    sahip modalının /resellers + /customers çekimi YOK. Bayide kalan toplu eylemler
  //    kapsamlı uçlardır: POST /domains/toplu/durum (AdminVeyaReseller, handler bayinin
  //    reseller_id'siyle süzer), DELETE /domains/{id} (SahipYonetim),
  //    DELETE /domains/{id}/subdomain/{sid} (MusteriScope → bayi sahipliği).
  const yonetici = yoneticiMi(useRol())
  const sahipKolonu = yonetici
  const [items, setItems] = useState<Domain[]>([])
  const [subler, setSubler] = useState<SubGenel[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata/başarı artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [secili, setSecili] = useState<Set<number>>(new Set())
  // Toplu async islem (DNS reset / SSL / sahip / plan) — process bar ile
  const [topluIsID, setTopluIsID] = useState<number>(0)
  const [topluModal, setTopluModal] = useState<'' | 'sahip' | 'plan' | 'ip'>('')
  const [topluBayiler, setTopluBayiler] = useState<Array<{ id: number; kullanici: string; ad_soyad?: string }>>([])
  const [topluMusteriler, setTopluMusteriler] = useState<Array<{ id: number; ad: string }>>([])
  const [topluHedefBayi, setTopluHedefBayi] = useState(0)
  const [topluHedefMusteri, setTopluHedefMusteri] = useState(0)
  const [topluHedefPlan, setTopluHedefPlan] = useState(0)
  const [isleniyor, setIsleniyor] = useState(false)
  const [silOnay, setSilOnay] = useState(false)
  // Hangi toplu eylemin sürdüğü — yalnız ilgili butonda dönen gösterge için.
  const [durumBekleyen, setDurumBekleyen] = useState<'' | 'aktif' | 'pasif'>('')
  const [topluBasliyor, setTopluBasliyor] = useState('')

  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [phpSurumler, setPhpSurumler] = useState<PHPVer[]>([])
  const [modalVeriYuk, setModalVeriYuk] = useState(false) // plan+php sürüm yüklemesi (listeyi BLOKLAMAZ)
  const [modalVeriGeldi, setModalVeriGeldi] = useState(false)
  const [olusturAcik, setOlusturAcik] = useState(false)
  const [olusturuluyor, setOlusturuluyor] = useState(false)
  // Oluşturma sürerken geçen süre (sn) ve yanıt gecikince "listede var mı" yoklaması.
  const [olusturGecen, setOlusturGecen] = useState(0)
  const [olusturDurumKontrol, setOlusturDurumKontrol] = useState(false)
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
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
    api.get<SubGenel[]>('/subdomains')
      .then(r => setSubler(r.data))
      .catch(() => setSubler([]))
  }
  useEffect(() => {
    let iptal = false
    // Sahip/plan modalı yalnız yöneticide açılır; bayi oturumunda AdminOnly
    // /resellers ve /customers hiç istenmez.
    if (!yonetici) return
    if (topluModal === 'sahip') {
      api.get('/resellers').then(r => { if (iptal) return; setTopluBayiler((r.data?.bayiler || r.data || []) as never) }).catch(() => {})
      api.get('/customers').then(r => { if (iptal) return; setTopluMusteriler((r.data?.musteriler || r.data || []) as never) }).catch(() => {})
    } else if (topluModal === 'plan') {
      // Toplu plan modalı planları modalVeriYukle üzerinden çeker; eskiden yalnız
      // "yeni domain" modalında yükleniyordu, o yüzden toplu planda liste boştu.
      modalVeriYukle()
    }
    return () => { iptal = true }
  }, [topluModal, yonetici])

  useEffect(yukle, [])
  // SSL degisimi nginx reload tetikleyip sayfayi yenileyebilir → devam eden isi bul.
  // Toplu işler (ve bu yoklama) AdminOnly: bayi oturumunda istek atılmaz.
  useEffect(() => {
    if (!yonetici) return
    let iptal = false
    api.get<{ is_id: number }>('/domains/toplu/aktif')
      .then(r => { if (iptal) return; if (r.data.is_id > 0) setTopluIsID(r.data.is_id) })
      .catch(() => { /* sessiz */ })
    return () => { iptal = true }
  }, [yonetici])

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
        const v = varsayilanPlanSec(pl)
        return v ? v.id : ''
      })
    }).finally(() => setModalVeriYuk(false))
  }

  // --- Üst bardaki "Subdomain": üst domain seçicili alt alan oluşturma diyaloğu ---
  // Çağrı DomainSubdomainlerPage.olustur ile AYNI uç: POST /domains/{id}/subdomain {alt_ad}.
  const [subAcik, setSubAcik] = useState(false)
  const [subUst, setSubUst] = useState<number>(0)
  const [subAd, setSubAd] = useState('')
  const [subKaydediyor, setSubKaydediyor] = useState(false)
  // Yalnız ana alan adları (items alt alan içermez), alan adına göre sıralı.
  const subUstSecenekleri = useMemo(
    () => [...items].sort((x, y) => x.alan_adi.localeCompare(y.alan_adi, 'tr'))
      .map(d => ({ id: d.id, etiket: d.alan_adi, altEtiket: d.sahip_ad || undefined })),
    [items])
  function subdomainAc() {
    // Tablodan TEK domain seçiliyse ön-seçili; aksi halde boş.
    setSubUst(secili.size === 1 ? Array.from(secili)[0] : 0)
    setSubAd('')
    setSubAcik(true)
  }
  async function subdomainOlustur(e: React.FormEvent) {
    e.preventDefault()
    if (!subUst || !subAd.trim() || subKaydediyor) return
    setHata(null); setSubKaydediyor(true)
    try {
      const { data } = await api.post(`/domains/${subUst}/subdomain`, { alt_ad: subAd.trim() }, UZUN_ISTEK)
      const m = cevirT(cevir("{0} oluşturuldu. DNS A kaydı eklendi."), data.tam_ad)
      toast.basari(cevir("Kaydedildi"), m)
      setSubAcik(false); setSubAd('')
      yukle()
    } catch (err) {
      const m = apiHata(err, cevir("Oluşturulamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setSubKaydediyor(false) }
  }

  function olusturAc() {
    setHata(null); setBasari(null); setOlusturmaSonuc(null)
    // ön seçili plan = çağıranın varsayılan planı (bayide bayinin kendi varsayılanı;
    // yoksa "Başlangıç", o da yoksa ilk plan — lib/bayiPlan.ts) — veri geldiyse hemen ata,
    // gelmediyse modalVeriYukle tamamlanınca atanır.
    const varsayilan = varsayilanPlanSec(planlar)
    setFAlanAdi(''); setFPHPSurum('8.3'); setFPlanID(varsayilan ? varsayilan.id : '')
    setOlusturAcik(true)
    modalVeriYukle() // lazy: plan/php sürümleri henüz gelmediyse şimdi çek (listeyi bloklamaz)
  }

  useEffect(() => {
    if (!olusturuluyor) return
    setOlusturGecen(0)
    const t = setInterval(() => setOlusturGecen(g => g + 1), 1000)
    return () => clearInterval(t)
  }, [olusturuluyor])

  // Yanıt alınamadıysa (zaman aşımı / ağ / 502-504) sunucu işi istemciden KOPUK
  // sürdürür: "başarısız" demek yerine alan adının listede belirip belirmediğini
  // yokla (en çok ~2 dk). true → oluşturuldu.
  async function olusturmaDurumuBekle(alanAdi: string): Promise<boolean> {
    setOlusturDurumKontrol(true)
    try {
      for (let i = 0; i < 24; i++) {
        try {
          const r = await api.get<Domain[]>('/domains')
          if (r.data.some(d => d.alan_adi === alanAdi)) return true
        } catch { /* geçici hata: yoklamayı sürdür */ }
        await new Promise(res => setTimeout(res, 5000))
      }
      return false
    } finally {
      setOlusturDurumKontrol(false)
    }
  }

  async function olusturGonder(e: React.FormEvent) {
    e.preventDefault()
    setHata(null)
    const alanAdi = fAlanAdi.trim().toLowerCase()
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(alanAdi)) {
      setHata(cevir("Geçersiz alan adı. Örn: ornek.com veya panel.ornek.com"))
      toast.hata(cevir("İşlem başarısız"), cevir("Geçersiz alan adı. Örn: ornek.com veya panel.ornek.com"))
      return
    }
    setOlusturuluyor(true)
    try {
      const body: any = { alan_adi: alanAdi, php_surum: fPHPSurum }
      if (fPlanID !== '') body.plan_id = fPlanID
      if (fSqlOlustur) body.sql_olustur = true
      // Sağlama 10-40 sn sürer (SELinux, php-fpm/nginx doğrulama); sunucu istemciden
      // kopuk tamamlar → genel 30 sn sınırına takılmasın.
      const r = await api.post<OlusturmaSonuc>('/domains', body, UZUN_ISTEK)
      setOlusturAcik(false)
      setOlusturmaSonuc(r.data)
      // Metin davranisla ortusmeli: DB yalniz kutu isaretliyse olusturulur.
      const m = cevirT(cevir(fSqlOlustur
        ? '"{0}" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL DB ve DNS zone hazır.'
        : '"{0}" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı ve DNS zone hazır.'), alanAdi)
      setBasari(m)
      toast.basari(cevir("Oluşturuldu"), m)
      setTimeout(() => setBasari(null), 8000)
      yukle()
    } catch (e: any) {
      if (sonucBelirsiz(e)) {
        if (await olusturmaDurumuBekle(alanAdi)) {
          setOlusturAcik(false)
          const m = cevirT(cevir('"{0}" oluşturuldu (sunucu yanıtı gecikti). Bu yüzden oluşturma parolaları gösterilemedi — FTP parolasını hosting sayfasından yenileyebilirsiniz.'), alanAdi)
          setBasari(m)
          toast.basari(cevir("Oluşturuldu"), m)
          yukle()
        } else {
          const m = cevir("Sunucudan yanıt alınamadı ve alan adı henüz listede görünmüyor. İşlem sunucuda sürüyor olabilir — tekrar oluşturmadan önce birkaç dakika sonra listeyi yenileyin.")
          setHata(m)
          toast.uyari(cevir("Sonuç belirsiz"), m)
          yukle()
        }
        return
      }
      const m = apiHata(e, cevir("Domain oluşturulamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
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
    const ids = Array.from(secili)
    const subIds = Array.from(subSecili)
    // O(n^2) subler.find yerine id -> subdomain on-indeksleme.
    const subById = new Map(subler.map(sd => [sd.id, sd]))
    const domById = new Map(items.map(d => [d.id, d.alan_adi]))
    type SilGorev = { url: string; etiket: string }
    const gorevler: SilGorev[] = [
      // Onay metni (?onay=<alan adı>): sunucu bayide zorunlu tutar, yöneticide doğrular.
      ...ids.map(id => ({ url: `/domains/${id}?onay=${encodeURIComponent(domById.get(id) ?? '')}`, etiket: domById.get(id) ?? String(id) })),
      ...subIds
        .map(sid => {
          const sd = subById.get(sid)
          return sd ? { url: `/domains/${sd.parent_id}/subdomain/${sid}`, etiket: sd.tam_ad } : null
        })
        .filter((g): g is SilGorev => g !== null),
    ]
    // Sinirli-eszamanli sil (sunucuyu bogmadan paralel; sirali await UI'yi kilitliyordu).
    const ESZAMAN = 5
    let basarili = 0
    const basarisizlar: string[] = []
    let idx = 0
    const isci = async () => {
      while (idx < gorevler.length) {
        const g = gorevler[idx++]
        try { await api.delete(g.url, UZUN_ISTEK); basarili++ } catch { basarisizlar.push(g.etiket) }
      }
    }
    await Promise.all(Array.from({ length: Math.min(ESZAMAN, gorevler.length) }, isci))
    setSecili(new Set()); setSubSecili(new Set())
    const toplam = gorevler.length
    if (basarisizlar.length === 0) {
      const m = `${basarili}/${toplam} ${cevir("kayıt silindi")}`
      setBasari(m); toast.basari(m)
    } else {
      // Sessiz catch{} yerine: hangileri basarisiz oldu, YUZEYE CIKAR.
      const ornek = basarisizlar.slice(0, 5).join(', ') + (basarisizlar.length > 5 ? '…' : '')
      const m = `${basarili}/${toplam} ${cevir("kayıt silindi")} — ${basarisizlar.length} ${cevir("başarısız")}: ${ornek}`
      setBasari(m); toast.hata(m)
    }
    setTimeout(() => setBasari(null), 4000)
    setIsleniyor(false); yukle()
  }

  // topluBaslat — asenkron toplu is baslatir; sayfa BLOKLANMAZ, ilerleme
  // TopluIsPaneli'nde process bar olarak akar (SSL'de nginx reload sayfayi
  // etkilerse bile is DB'de surer ve panel kaldigi yerden takip eder).
  async function topluBaslat(tip: 'dns_reset' | 'ssl_kur' | 'sahip' | 'plan' | 'ip', ek?: Record<string, unknown>) {
    const ids = Array.from(secili)
    if (!ids.length || !yonetici) return
    if (tip === 'dns_reset' && !(await onay({
      baslik: cevir("DNS kayıtları sıfırlansın mı?"),
      mesaj: cevirT(cevir("{0} domainin TÜM DNS kayıtları silinip şablondan yeniden oluşturulacak. Özel kayıtlar (MX, TXT, doğrulama kayıtları) KAYBOLUR."), ids.length),
      ton: 'tehlike', onayIkonu: I.sifirla,
    }))) return
    setTopluBasliyor(tip)
    try {
      const { data } = await api.post<{ is_id: number }>('/domains/toplu/is', { tip, ids, ...(ek || {}) })
      setTopluIsID(data.is_id); setTopluModal('')
    } catch (e) {
      const m = apiHata(e, cevir("Toplu işlem başlatılamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setTopluBasliyor('') }
  }

  async function durumDegistir(yeniDurum: 'aktif' | 'pasif') {
    setIsleniyor(true); setHata(null); setDurumBekleyen(yeniDurum)
    const ids = Array.from(secili)
    try {
      await api.post('/domains/toplu/durum', { ids, durum: yeniDurum })
      const m = cevirT(cevir('{0} domain "{1}" durumuna geçirildi'), ids.length, yeniDurum)
      setBasari(m)
      toast.basari(m)
      setTimeout(() => setBasari(null), 4000)
      setSecili(new Set()); yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Durum değiştirme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setIsleniyor(false); setDurumBekleyen('') }
  }

  return (
    <div>
      {/* Bayi: Seçenek 1 Domainler görünümü (alan adı odaklı liste, satıra tık = müşteri
          paneline tek tık giriş, toplu işlemler). Oluşturma modalı aşağıda ORTAK. */}
      {!yonetici ? (
        <BayiDomainler items={items} yuk={yuk} yenile={yukle} onYeni={olusturAc} />
      ) : (<>
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Domainler") }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-2">{cevir("Domainler")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        {cevir(cevir("Tüm kayıtlı domainlerinizi listeleyin, toplu seçim ile durum değiştirin veya silin."))}
      </p>


      {/* Toolbar */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <div className="flex-1 min-w-[10rem] max-w-md">
          <input type="text" value={q} onChange={e => setQ(e.target.value)}
            placeholder={cevir("Domain ara...")}
            className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none" />
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-500">{filtreli.length} / {items.length}</span>
        <div className="ml-auto flex flex-wrap items-center justify-end gap-2 max-sm:w-full">
          <Button variant="outlined" ikon={I.arti} onClick={subdomainAc} disabled={items.length === 0}>
            Subdomain
          </Button>
          <Button color="primary" ikon={I.arti} onClick={olusturAc}>
            {cevir("Yeni Domain")}
          </Button>
        </div>
      </div>

      {yonetici && topluIsID > 0 && (
        <TopluIsPaneli isID={topluIsID} onBitti={() => { setTopluIsID(0); setSecili(new Set()); yukle() }} />
      )}

      {/* Toplu işlem barı */}
      {(secili.size + subSecili.size) > 0 && (
        <div className="mb-3 px-3 py-2 bg-primary-50 dark:bg-primary-900/15 border border-primary-200 dark:border-primary-800/60 rounded-lg flex items-center gap-2 flex-wrap">
          <span className="text-sm font-semibold text-primary-800 dark:text-primary-200">
            {secili.size + subSecili.size} {cevir("seçili")}{subSecili.size > 0 ? ` (${subSecili.size} ${cevir("alt alan")})` : ''}
          </span>
          {secili.size > 0 && <>
            <Button variant="outlined" boyut="sm" ikon={I.oynat} onClick={() => durumDegistir('aktif')} disabled={isleniyor} yukleniyor={durumBekleyen === 'aktif'}>
              {cevir("Aktif Et")}
            </Button>
            <Button variant="outlined" boyut="sm" ikon={I.durakla} onClick={() => durumDegistir('pasif')} disabled={isleniyor} yukleniyor={durumBekleyen === 'pasif'}>
              {cevir("Pasif Et")}
            </Button>
          </>}
          {/* DNS/SSL/sahip/plan/IP → POST /domains/toplu/is (AdminOnly): yalnız yönetici. */}
          {yonetici && secili.size > 0 && <>
            <Button color="error" variant="soft" boyut="sm" ikon={I.sifirla} onClick={() => topluBaslat('dns_reset')} disabled={isleniyor || topluIsID > 0} yukleniyor={topluBasliyor === 'dns_reset'}>
              {cevir(cevir("DNS varsayılana sıfırla"))}
            </Button>
            <Button variant="outlined" boyut="sm" ikon={I.kalkan} onClick={() => topluBaslat('ssl_kur')} disabled={isleniyor || topluIsID > 0} yukleniyor={topluBasliyor === 'ssl_kur'}>
              {cevir("SSL kur (Let's Encrypt)")}
            </Button>
            <Button variant="outlined" boyut="sm" ikon={I.kullanici} onClick={() => setTopluModal('sahip')} disabled={isleniyor || topluIsID > 0}>
              {cevir(cevir("Sahip değiştir"))}
            </Button>
            <Button variant="outlined" boyut="sm" ikon={I.kalem} onClick={() => setTopluModal('plan')} disabled={isleniyor || topluIsID > 0}>
              {cevir(cevir("Plan değiştir"))}
            </Button>
            <Button variant="outlined" boyut="sm" ikon={I.kure} onClick={() => setTopluModal('ip')} disabled={isleniyor || topluIsID > 0}>
              {cevir("IP değiştir")}
            </Button>
          </>}
          <Button color="error" boyut="sm" ikon={I.cop} onClick={() => setSilOnay(true)} disabled={isleniyor}>
            {cevir("Sil")} ({secili.size + subSecili.size})
          </Button>
          <Button variant="outlined" boyut="sm" ikon={I.kapat} onClick={() => { setSecili(new Set()); setSubSecili(new Set()) }} disabled={isleniyor}>
            {cevir(cevir("Seçimi temizle"))}
          </Button>
        </div>
      )}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState baslik={cevir("Henüz domain yok")}
          aciklama={cevir("İlk domain'inizi ekleyerek başlayın. Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı ve DNS zone otomatik oluşturulur.")}
          buton={{ etiket: cevir("Domain Oluştur"), onClick: olusturAc }} />
      ) : (
        <div className="@container lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={`${T.tablo} lg:min-w-0!`}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
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
                {sahipKolonu && <th className={`${T.baslik} ${KOL.sahip}`}>{cevir("Kullanıcı")}</th>}
                <th className={`${T.baslik} ${KOL.plan}`}>Plan</th>
                <th className={`${T.baslik} ${KOL.disk}`}>Disk</th>
                <th className={`${T.baslik} ${KOL.trafik}`}>{cevir("Trafik")}</th>
                <th className={T.baslik}>{cevir("Durum")}</th>
                <th className={`${T.baslik} ${KOL.olusturulma}`}>{cevir("Oluşturulma")}</th>
                <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
              </tr>
            </thead>
            <tbody className={T.govde}>
              {filtreli.flatMap(d => {
                const subs = sublerByParent.get(d.id) || []
                const parentRow = (
                  <tr key={d.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700 transition ${secili.has(d.id) ? 'ring-2 ring-brand-400 lg:ring-0 bg-brand-50 dark:bg-brand-900/20' : ''}`}>
                    <td className={T.hucreSecim}>
                      <input type="checkbox" checked={secili.has(d.id)}
                        onChange={() => togga(d.id)}
                        aria-label={cevirT(cevir("{0} seç"), d.alan_adi)}
                        className="cursor-pointer" />
                    </td>
                    <td className={T.hucreBaslikSecimli}>
                      <span className="inline-flex items-center gap-1.5 min-w-0 max-w-full lg:max-w-[14rem] align-middle">
                      <Link to={`/abonelikler/${d.id}`} title={d.alan_adi} className="min-w-0 lg:truncate text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300">
                        {d.alan_adi}
                      </Link>
                      <SiteAc ad={d.alan_adi} ssl={d.ssl} />
                      {d.is_demo && <Badge color="warning" variant="soft" className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 shrink-0">DEMO</Badge>}
                      </span>
                    </td>
                    {sahipKolonu && (
                      <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.sahip}`} data-etiket={cevir("Kullanıcı")}>
                        <SahipHucre ad={d.sahip_ad} tur={d.sahip_tur} />
                      </td>
                    )}
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.plan}`} data-etiket="Plan">
                      {!d.plan_ad
                        ? <span className="text-slate-400 dark:text-slate-500 italic">—</span>
                        : (/—\s*Özel\s*$/.test(d.plan_ad) || d.plan_ad.trim() === cevir("Özel"))
                          ? <Badge title={d.plan_ad} variant="outlined" className="px-2 py-0.5 text-[11px] font-medium">{cevir("Özel")}</Badge>
                          : <span title={d.plan_ad} className="text-slate-700 dark:text-slate-300 inline-block max-w-full lg:max-w-[7rem] truncate align-bottom">{d.plan_ad}</span>}
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.disk}`} data-etiket="Disk">
                      <KullanimHucre kullanilanMB={d.boyut_kb / 1024} kotaMB={d.disk_kota_mb} etiket={`${d.alan_adi} ${cevir("Disk")}`} />
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.trafik}`} data-etiket={cevir("Trafik")}>
                      <KullanimHucre kullanilanMB={(d.trafik_kb || 0) / 1024} kotaMB={d.trafik_kota_mb} etiket={`${d.alan_adi} ${cevir("Trafik")}`} />
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap`} data-etiket={cevir("Durum")}>
                      <Badge color={durumRenk(d.durum)} variant="soft" className="text-[10px] uppercase tracking-wider px-2 py-0.5 font-semibold">{d.durum}</Badge>
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.olusturulma}`} data-etiket={cevir("Oluşturulma")}>
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 whitespace-nowrap">{d.olusturulma?.slice(0, 10) || '-'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Link to={`/abonelikler/${d.id}`} className={butonSinifi({ color: 'primary', variant: 'soft', boyut: 'sm', className: 'ml-auto lg:ml-0' })}>
                        {cevir("Yönet")}<Ikon d={I.ileri} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
                      </Link>
                    </td>
                  </tr>
                )
                const subRows = subs.map(sd => (
                  <tr key={'sub-' + sd.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700/50 transition bg-slate-50/40 dark:bg-dark-800/20`}>
                    <td className={T.hucreSecim}>
                      <input type="checkbox" checked={subSecili.has(sd.id)}
                        onChange={() => subTogga(sd.id)}
                        aria-label={cevirT(cevir("{0} seç"), sd.tam_ad)}
                        className="cursor-pointer" />
                    </td>
                    <td className={T.hucreBaslikSecimli}>
                      <span className="inline-flex items-center gap-1.5 min-w-0 max-w-full lg:max-w-[14rem] @min-[950px]:lg:max-w-[17rem]! lg:pl-5 align-middle">
                        <span className="text-slate-300 dark:text-slate-600 select-none hidden lg:inline shrink-0">└─</span>
                        <Link to={`/abonelikler/${sd.parent_id}/subdomainler/${sd.id}`} title={sd.tam_ad} className="min-w-0 lg:truncate text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300">{sd.tam_ad}</Link>
                        <SiteAc ad={sd.tam_ad} ssl={d.ssl} />
                        <Badge color="info" variant="soft" className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 align-middle shrink-0 whitespace-nowrap @max-[739px]:lg:hidden!">{cevir("alt alan")}</Badge>
                      </span>
                    </td>
                    {sahipKolonu && (
                      <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.sahip}`} data-etiket={cevir("Kullanıcı")}>
                        <span title={d.sahip_ad} className="text-xs text-slate-400 dark:text-slate-500 truncate inline-block max-w-full lg:max-w-[8rem] align-bottom">{d.sahip_ad || '—'}</span>
                      </td>
                    )}
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.plan}`} data-etiket="Plan">
                      <span className="text-slate-400 dark:text-slate-500 italic">—</span>
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.disk}`} data-etiket="Disk">
                      <span className="text-slate-300 dark:text-slate-600">—</span>
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.trafik}`} data-etiket={cevir("Trafik")}>
                      <span className="text-slate-300 dark:text-slate-600">—</span>
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap`} data-etiket={cevir("Durum")}>
                      <Badge color="success" variant="soft" className="text-[10px] uppercase tracking-wider px-2 py-0.5 font-semibold">{cevir("aktif")}</Badge>
                    </td>
                    <td className={`${T.hucre} lg:whitespace-nowrap ${KOL.olusturulma}`} data-etiket={cevir("Oluşturulma")}>
                      <span className="font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{sd.olusturulma?.slice(0, 10) || '-'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Link to={`/abonelikler/${sd.parent_id}/subdomainler/${sd.id}`} className={butonSinifi({ color: 'primary', variant: 'soft', boyut: 'sm' })}>
                        {cevir("Yönet")}<Ikon d={I.ileri} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
                      </Link>
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
      </>)}

      {/* Subdomain Oluştur Modal (kök liste, yönetici) */}
      <Modal acik={subAcik} baslik={cevir("Yeni Subdomain")} onKapat={() => setSubAcik(false)} kapatilamaz={subKaydediyor} genislik="md"
        alt={<>
          <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setSubAcik(false)} disabled={subKaydediyor}>{cevir("İptal")}</Button>
          <Button type="submit" form="gosp-subdomain-olustur" color="primary" ikon={I.arti} yukleniyor={subKaydediyor} disabled={!subUst || !subAd.trim()}>
            {subKaydediyor ? cevir("Oluşturuluyor…") : cevir("Subdomain Ekle")}
          </Button>
        </>}>
        <form id="gosp-subdomain-olustur" onSubmit={subdomainOlustur} className="space-y-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Üst domain")} <span className="text-red-500">*</span></label>
            <AramaliSecim
              deger={subUst || null}
              onDegis={v => setSubUst(Number(v))}
              yerTutucu={cevir("Domain seçin…")}
              aramaYerTutucu={cevir("Domain ara…")}
              bosMetin={cevir("Henüz domain yok")}
              secenekler={subUstSecenekleri}
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Alt Alan")} <span className="text-red-500">*</span></label>
            <div className="flex items-center gap-2 min-w-0">
              <input value={subAd} onChange={e => setSubAd(e.target.value.toLowerCase())} required placeholder="blog" disabled={subKaydediyor}
                className="w-40 shrink-0 px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
              <span className="font-mono text-sm text-slate-500 dark:text-slate-400 truncate min-w-0"
                title={subUst ? `.${items.find(d => d.id === subUst)?.alan_adi || ''}` : undefined}>
                {subUst ? `.${items.find(d => d.id === subUst)?.alan_adi || ''}` : ''}
              </span>
            </div>
            <p className="text-[11px] text-slate-400 mt-2">{cevir("Küçük harf, rakam ve tire. Örn:")} <span className="font-mono">blog</span>, <span className="font-mono">shop</span>, <span className="font-mono">api</span>.</p>
          </div>
        </form>
      </Modal>

      {/* Domain Oluştur Modal */}
      <Modal acik={olusturAcik} baslik={cevir("Yeni Domain Oluştur")} onKapat={() => setOlusturAcik(false)} kapatilamaz={olusturuluyor} genislik="lg"
        alt={<>
          <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setOlusturAcik(false)} disabled={olusturuluyor}>{cevir("İptal")}</Button>
          <Button type="submit" form="gosp-domain-olustur" color="primary" ikon={I.arti} yukleniyor={olusturuluyor} disabled={!fAlanAdi.trim()}>
            {olusturDurumKontrol ? cevir('Durum kontrol ediliyor…') : olusturuluyor ? cevir('Oluşturuluyor…') : cevir("Oluştur")}
          </Button>
        </>}>
          <form id="gosp-domain-olustur" onSubmit={olusturGonder}>
            {olusturuluyor && (
              <UyariKutusu ton="bilgi" className="mb-4" kucuk>
                {olusturDurumKontrol
                  ? cevir("Sunucu yanıtı gecikti — alan adının oluşup oluşmadığı kontrol ediliyor…")
                  : <>{cevirT(cevir("Kuruluyor… ({0} sn)"), String(olusturGecen))} {cevir("Linux kullanıcısı, web sunucusu, PHP havuzu, FTP ve DNS hazırlanıyor; genellikle 10–40 sn sürer. Sayfadan ayrılsanız da işlem sunucuda tamamlanır.")}</>}
              </UyariKutusu>
            )}
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              {cevir(cevir("Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı ve DNS zone otomatik kurulur."))}
            </p>

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Alan adı")} <span className="text-red-500">*</span></label>
                <input
                  type="text"
                  value={fAlanAdi}
                  onChange={e => setFAlanAdi(e.target.value)}
                  placeholder={cevir("ornek.com")}
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
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none bg-white dark:bg-dark-700"
                >
                  {phpSurumler.length === 0
                    ? <option value="8.3">{cevir("PHP 8.3 (varsayılan)")}</option>
                    : phpSurumler.map(p => (
                        <option key={p.surum} value={p.surum}>PHP {p.surum}{/default|appstream/i.test(p.aciklama || '') ? ` ${cevir("(varsayılan)")}` : ''}{p.eol ? ` — ${cevir("güncelleme almıyor")}` : ''}</option>
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
                  yerTutucu={cevir("— (yok) —")}
                  aramaYerTutucu={cevir("Plan ara…")}
                  bosMetin={cevir("Plan bulunamadı")}
                  yukleniyor={modalVeriYuk && planlar.length === 0}
                  secenekler={[
                    { id: 0, etiket: cevir('— (yok) —') },
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
          </form>
      </Modal>

      {/* Oluşturma Sonucu Modal (FTP + DB parolaları) */}
      <Modal acik={!!olusturmaSonuc} baslik={cevir("Domain Oluşturuldu")} ton="basari" onKapat={() => setOlusturmaSonuc(null)} genislik="lg"
        alt={<Button color="primary" ikon={I.onay} onClick={() => setOlusturmaSonuc(null)}>{cevir("Tamam")}</Button>}>
        {olusturmaSonuc && (
          <div>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{olusturmaSonuc.alan_adi}</span> {cevir("sağlamlandı. Aşağıdaki parolalar")} <strong>sadece bir kez</strong> gösterilir — güvenli bir yere kaydedin.
            </p>

            <div className="space-y-3">
              <div className="border border-slate-200 dark:border-dark-600 rounded-md p-3 bg-slate-50 dark:bg-dark-800">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">FTP</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.ftp_host || '—'} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Kullanıcı")} v={olusturmaSonuc.ftp_user} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Parola")} v={olusturmaSonuc.olusturulan_parolalar.ftp} kopyala={panoYaz} parola />
              </div>

              {olusturmaSonuc.db_adi && (
              <div className="border border-slate-200 dark:border-dark-600 rounded-md p-3 bg-slate-50 dark:bg-dark-800">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">{cevir("MySQL Veritabanı")}</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.db_host || 'localhost'} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Veritabanı")} v={olusturmaSonuc.db_adi} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Kullanıcı")} v={olusturmaSonuc.db_user} kopyala={panoYaz} />
                <KopyaSatir e={cevir("Parola")} v={olusturmaSonuc.olusturulan_parolalar.db} kopyala={panoYaz} parola />
              </div>
              )}

              <div className="text-[11px] text-slate-500 dark:text-slate-500 italic">
                {cevir(cevir("Sistem kullanıcısı:"))} <span className="font-mono">{olusturmaSonuc.sistem_kullanici}</span>
              </div>
            </div>
          </div>
        )}
      </Modal>

      {/* Toplu Sil Onay */}
      {/* topluHedefModal — sahip/plan hedefi secimi (async is bundan sonra baslar) */}
      {yonetici && topluModal === 'ip' && (
        <IPDegistirDiyalog
          liste={Array.from(secili).map(id => items.find(x => x.id === id)).filter((d): d is Domain => !!d)}
          kapat={() => setTopluModal('')} yukleniyor={topluBasliyor === 'ip'}
          onayla={ip => topluBaslat('ip', { ipv4: ip })} />
      )}
      <Modal acik={yonetici && (topluModal === 'sahip' || topluModal === 'plan')} baslik={topluModal === 'sahip' ? cevir('Toplu sahip değiştir') : cevir('Toplu plan değiştir')}
        onKapat={() => setTopluModal('')} kapatilamaz={topluBasliyor === 'sahip' || topluBasliyor === 'plan'}
        alt={<>
          <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setTopluModal('')}>{cevir("Vazgeç")}</Button>
          <Button type="button" color="primary" ikon={I.onay}
            disabled={topluModal === 'plan' && topluHedefPlan <= 0}
            yukleniyor={topluBasliyor === 'sahip' || topluBasliyor === 'plan'}
            onClick={() => topluModal === 'sahip'
              ? topluBaslat('sahip', { reseller_id: topluHedefBayi, customer_id: topluHedefMusteri })
              : topluBaslat('plan', { plan_id: topluHedefPlan })}>
            {cevir("Uygula")} ({secili.size})
          </Button>
        </>}>
          <div>
            <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">{secili.size} {cevir("domain etkilenecek.")}</p>
            {topluModal === 'sahip' && (
              <UyariKutusu ton="uyari" className="mb-4" kucuk>
                <span className="leading-relaxed">
                {cevir("Devir eski sahibin erişimini keser: FTP/SSH parolası yenilenir, açık müşteri")}{' '}
                {cevir("oturumları düşer, Git webhook anahtarı değişir, SSH anahtarları arşivlenir, uzak yedek hedefi ve GitHub bağlantısı kaldırılır.")}
                <b className="font-semibold"> {cevir("Yeni parola işlem sonucunda gösterilir")}</b> {cevir("— kaydedin.")}
                </span>
              </UyariKutusu>
            )}
            {topluModal === 'sahip' ? (
              <div className="space-y-3">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Bayi (sahip)")}</span>
                  <AramaliSecim
                    deger={topluHedefBayi}
                    onDegis={v => setTopluHedefBayi(Number(v))}
                    yerTutucu={cevir("Ana hesap (admin)")}
                    aramaYerTutucu={cevir("Bayi ara…")}
                    secenekler={[
                      { id: 0, etiket: cevir('Ana hesap (admin)') },
                      ...topluBayiler.map(b => ({ id: b.id, etiket: b.ad_soyad ? `${b.ad_soyad} (${b.kullanici})` : b.kullanici, altEtiket: b.ad_soyad ? b.kullanici : undefined })),
                    ]}
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Müşteri")}</span>
                  <AramaliSecim
                    deger={topluHedefMusteri}
                    onDegis={v => setTopluHedefMusteri(Number(v))}
                    yerTutucu={cevir("Yok")}
                    aramaYerTutucu={cevir("Müşteri ara…")}
                    bosMetin={cevir("Müşteri yok")}
                    secenekler={[
                      { id: 0, etiket: cevir('Yok') },
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
                  aramaYerTutucu={cevir("Plan ara…")}
                  bosMetin={cevir("Plan bulunamadı")}
                  yukleniyor={modalVeriYuk && planlar.length === 0}
                  secenekler={planlar.map(p => ({ id: p.id, etiket: p.ad }))}
                />
                <span className="mt-1 block text-[11px] text-slate-400">{cevir("Yeni plan limitleri (disk, inode, FPM) her domaine uygulanır.")}</span>
              </label>
            )}
          </div>
      </Modal>

      <Modal acik={silOnay} baslik={cevir("Toplu Silme")} ton="tehlike" onKapat={() => setSilOnay(false)}
        alt={<>
          <Button variant="outlined" ikon={I.kapat} onClick={() => setSilOnay(false)}>{cevir("İptal")}</Button>
          <Button color="error" ikon={I.cop} onClick={topluSil} yukleniyor={isleniyor}>
            {cevir("Evet, Sil")}
          </Button>
        </>}>
          <div>
            <p className="text-sm text-slate-700 dark:text-slate-300 mb-3">
              <span className="font-semibold">{secili.size}</span> domain{subSecili.size > 0 ? <> + <span className="font-semibold">{subSecili.size}</span> {cevir("alt alan")}</> : ''} {cevir("ve tüm bağımlı kaynakları (Linux kullanıcı, ev dizini, DB, FTP, vhost, DNS zone)")} <strong>{cevir("geri dönüşsüz")}</strong> {cevir("silinecek.")}
            </p>
            <ul className="text-xs font-mono text-slate-500 dark:text-slate-500 bg-slate-50 dark:bg-dark-800 rounded p-2 max-h-40 overflow-auto mb-4">
              {Array.from(secili).slice(0, 8).map(id => {
                const d = items.find(x => x.id === id)
                return <li key={id} className="truncate">{d?.alan_adi || '?'}</li>
              })}
              {secili.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {secili.size - 8} {cevir("daha…")}</li>}
              {Array.from(subSecili).slice(0, 8).map(sid => {
                const s = subler.find(x => x.id === sid)
                return <li key={'s' + sid} className="truncate text-teal-600 dark:text-teal-400">{s?.tam_ad || '?'} <span className="text-[10px]">{cevir("(alt alan)")}</span></li>
              })}
              {subSecili.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {subSecili.size - 8} {cevir("alt alan daha…")}</li>}
            </ul>
          </div>
      </Modal>
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
          kopyalandi ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 hover:border-brand-400 text-slate-800 dark:text-slate-200'
        }`}
        title={cevir("Tıklayarak kopyala")}
      >
        {parola && !acik ? '••••••••••' : v}
      </code>
      {parola && (
        <Button type="button" variant="outlined" boyut="sm" ikon={acik ? I.gozKapali : I.goz} onClick={() => setAcik(s => !s)}>
          {acik ? cevir('Gizle') : cevir("Göster")}
        </Button>
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
  const [iptalEdiliyor, setIptalEdiliyor] = useState(false)

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
    dns_reset: cevir('DNS varsayılana sıfırlama'), ssl_kur: cevir("Let's Encrypt SSL kurulumu"),
    sahip: cevir('Sahip değiştirme'), plan: cevir('Plan değiştirme'), ip: cevir('IP değiştirme'), bayi_sil: cevir('Bayi silme'),
  }
  const yuzde = d.toplam > 0 ? Math.round((d.tamamlanan / d.toplam) * 100) : 0
  const bitti = d.durum !== 'calisiyor'
  // Durum → ortak ton (tonlar.ts): kutunun rengi ve ikonu tondan gelir.
  const ton: Exclude<Ton, 'soru'> = d.durum === 'tamam' ? 'basari'
    : d.durum === 'hata' ? 'tehlike'
      : d.durum === 'kismi' ? 'uyari'
        : 'bilgi'

  async function iptalEt() {
    setIptalEdiliyor(true)
    try { await api.post(`/domains/toplu/is/iptal?id=${isID}`) } catch { /* */ }
    finally { setIptalEdiliyor(false) }
  }

  return (
    <UyariKutusu ton={ton} className="mb-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-slate-800 dark:text-slate-100">
            {ADLAR[d.tip] || d.tip} — {bitti ? (d.durum === 'tamam' ? cevir('tamamlandı') : d.durum === 'kismi' ? cevir('kısmen tamamlandı') : d.durum) : cevir('çalışıyor')}
          </p>
          <p className="mt-0.5 text-xs text-slate-600 dark:text-slate-400">
            {d.tamamlanan}/{d.toplam} · <span className="inline-flex items-center gap-0.5 align-middle"><Ikon d={I.onay} className="size-3 shrink-0" />{d.basari} {cevir("başarılı")}</span>
            {d.hata > 0 && <> · <span className="inline-flex items-center gap-0.5 align-middle"><Ikon d={I.kapat} className="size-3 shrink-0" />{d.hata} {cevir("hata")}</span></>}
            {!bitti && d.aktif_domain ? cevirT(cevir(" · şu an: {0}"), d.aktif_domain) : ''}
          </p>
        </div>
        {bitti
          ? <Button type="button" variant="outlined" boyut="sm" ikon={I.kapat} onClick={() => { setKapandi(true); onBitti() }}
              className="shrink-0">{cevir("Kapat")}</Button>
          : <Button type="button" color="error" variant="soft" boyut="sm" ikon={I.durdur} onClick={iptalEt} yukleniyor={iptalEdiliyor}
              className="shrink-0">{cevir("Durdur")}</Button>}
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-dark-600">
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
                <Ikon d={parola ? I.anahtar : uyari ? I.uyari : I.kapat} className="inline-block h-3.5 w-3.5 mr-0.5 align-[-2px]" /> {x}
              </li>
            )
          })}
        </ul>
      )}
    </UyariKutusu>
  )
}
