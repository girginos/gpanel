import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import React from "react"
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import DomainKaynakKart from '@/components/DomainKaynakKart'
import DomainPano from "@/components/DomainPano"
import AlanPano from '@/components/AlanPano'
import { useUc } from '@/components/alanpano/useUc'
import type { KaynakOzet } from '@/components/alanpano/tipler'
import ToolCard from '@/components/ToolCard'
import EklentiEkrani from '@/components/EklentiEkrani'
import LisansAskida from '@/components/LisansAskida'
import type { Domain } from '@/components/domainTipi'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge, Card, butonSinifi } from '@/components/ui'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I, Ikon } from '@/components/Ikon'
import { useRol } from '@/lib/rol'

// 🔴 TEK KAYNAK. Önceki sürümde geri yükleme, elle yazılmış bir `v === '...'`
// zinciriyle doğruluyordu ve yeni sekme eklenince ORAYA yazmak unutuldu:
// kullanıcı Geliştirici sekmesini seçiyor, sayfayı yenileyince Pano'ya
// düşüyordu ve "sekme kaydetmiyor" olarak görünüyordu.
const GECERLI_TABLAR = ['dashboard', 'hosting', 'apps', 'mail', 'gelistirici'] as const
type Tab = (typeof GECERLI_TABLAR)[number]

const ICONS = {
  baglanti:  'M13.828 10.172a4 4 0 015.656 5.656l-3 3a4 4 0 01-5.656-5.656m.172-5.172a4 4 0 00-5.656 5.656l-3 3a4 4 0 005.656 5.656',
  hizmet:    'M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4',
  istatistik:'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z',
  posta:     'M3 8l9 6 9-6m-9 6V4m0 0v16',
  dns:       'M21 12a9 9 0 11-18 0 9 9 0 0118 0zM3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18',
  apache:    'M13 10V3L4 14h7v7l9-11h-7z',
}


const SUBD2_EN: Record<string, string> = {
  "1-tıkla kurulum · yönetim": "1-click install · management",
  "A, MX, TXT, CNAME kayıtları": "A, MX, TXT, CNAME records",
  "Abonelik yüklenemedi": "Failed to load subscription",
  "Alan Adı ve DNS": "Domain and DNS",
  "Alt dizin (public_html/... altına)": "Subdirectory (under public_html/...)",
  "Aylık trafik": "Monthly traffic",
  "Başka aboneliğe geç": "Switch to another subscription",
  "Daha fazla işlem": "More actions",
  "Kaynak bilgisi alınamadı": "Could not load resource info",
  "Gönderim/teslimat günlüğü": "Sending/delivery log",
  "Güvenlik başlıkları, ek direktifler": "Security headers, extra directives",
  "Posta kutusu ekle/yönet": "Add/manage mailboxes",
  "SSL etkinleştirilince otomatik görünür": "Appears automatically when SSL is enabled",
  "Sağlık analizi, bağlantı bilgileri": "Health analysis, connection info",
  "Yönlendirme, catch-all": "Forwarding, catch-all",
  "Yükselt, düşür veya özel plan": "Upgrade, downgrade or custom plan",
  "boş = kök, blog, shop vb.": "empty = root, blog, shop etc.",
  "host seviyesinde bir uygulamadır, tenant panelinden kurulamaz.": "is a host-level application, cannot be installed from the tenant panel.",
  "yakında": "coming soon",
  "Önizleme yalnızca HTTPS sitelerde gösterilir": "Preview is shown only on HTTPS sites",
  "Askı kaldırıldı — site tekrar erişilebilir.": "Suspension lifted — the site is accessible again.",
  "Hesap askıya alındı — site artık 503 bakım sayfası döndürüyor.": "Account suspended — the site now returns a 503 maintenance page.",
  "Onay gerekiyor": "Confirmation required",
  "\"{0}\" askıya alınacak — site erişilemez olacak (503). Devam edilsin mi?": "\"{0}\" will be suspended — the site will become inaccessible (503). Continue?",
  "Anasayfa": "Home",
  "Hata": "Error",
  "Askıdan Al (Geri Getir)": "Unsuspend (Restore)",
  "Hesabı Askıya Al": "Suspend Account",
  "Pano": "Dashboard",
  "Geliştirici": "Developer",
  "Uygulamalar": "Applications",
  "Web sitesi:": "Website:",
  "Web Sitesi": "Website",
  "{0} silinsin mi? Dosyalar + DB kaybolur.": "Delete {0}? Files + DB will be lost.",
  "Silinemedi": "Could not be deleted",
  "Öne Çıkan": "Featured",
  "Kurulu Uygulamalar ({0})": "Installed Applications ({0})",
  "Henüz uygulama kurulu değil. Aşağıdaki katalogdan seç.": "No applications installed yet. Choose from the catalog below.",
  "Yönetim Paneli": "Admin Panel",
  "Katalog ({0})": "Catalog ({0})",
  "Admin'den kurulur": "Installed from Admin",
  "Kur": "Install",
  "Site:": "Site:",
  "Kuruluyor…": "Installing…",
  "Posta Hizmetleri": "Mail Services",
  "E-Posta Teslimat Takibi": "Email Delivery Tracking",
  "Performans": "Performance",
  "Apache ve nginx": "Apache and nginx",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SUBD2_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function SubscriptionDetailPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const navigate = useNavigate()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  // Seçili sekme kalıcı (localStorage) — sayfa yenilenince/geri gelince korunur.
  const [tab, setTabState] = useState<Tab>(() => {
    try {
      const v = localStorage.getItem('gosp.abonelik.tab')
      if (v && (GECERLI_TABLAR as readonly string[]).includes(v)) return v as Tab
    } catch { /* yok say */ }
    return 'dashboard'
  })
  // Alan adı panosu (AlanPano) Pano sekmesi İLK açıldığında bağlanır ve sonra bağlı
  // kalır (başka sekmede gizlenir): sekme değiştirip dönmek pahalı uçları (/istatistik,
  // /health …) yeniden çekmesin. Başka sekmeyle açılan sayfada Pano'ya girilmezse hiç
  // çekilmez.
  const [panoAcildi, setPanoAcildi] = useState(tab === 'dashboard')
  const setTab = (t: Tab) => {
    setTabState(t)
    if (t === 'dashboard') setPanoAcildi(true)
    try { localStorage.setItem('gosp.abonelik.tab', t) } catch { /* yok say */ }
  }
  // 🔴 /kaynak sayfa başına TEK çekim (kota kapalıyken `du -sm`, 0.7–2 sn): burada
  // çekilir; AlanPano, sağdaki DomainKaynakKart ve soldaki 'İstatistikler' kartı aynı
  // durumu prop ile paylaşır. Üç 'Yenile' de bu tek yenileyiciyi çağırır (uçuştaki
  // istek varken ikinci istek gitmez).
  const kaynak = useUc<KaynakOzet>(id ? `/domains/${id}/kaynak` : null, cevir("Kaynak bilgisi alınamadı"))
  const diskMB = kaynak.veri ? kaynak.veri.disk_mb.kullanim : null
  const [menuAcik, setMenuAcik] = useState(false)
  const [isleniyor, setIsleniyor] = useState(false)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [mailAktif, setMailAktif] = useState(false)
  const [calistiriciAktif, setCalistiriciAktif] = useState(false)
  // 🔴 KURULU (aktif'ten AYRI): askıya alınmış (kurulu ama aktif=0) eklentinin
  // sekmesi kaybolmasın — açık kalıp landing göstersin (Marka/top-level ile tutarlı).
  const [mailKurulu, setMailKurulu] = useState(false)
  const [calistiriciKurulu, setCalistiriciKurulu] = useState(false)
  const [yenileniyor, setYenileniyor] = useState(false)
  // Müşteri (rol 'user') tek aboneliğe sahip ve askıya alma yetkisi yok (SahipYonetim
  // 403): abonelik değiştirme ve '⋮' yönetim menüsü ona gösterilmez.
  const musteri = useRol() === 'user'

  const domainYukleNesli = useRef(0)
  function domainYukle(): Promise<void> {
    if (!id) return Promise.resolve()
    const _n = ++domainYukleNesli.current
    return api.get<Domain>(`/domains/${id}`)
      .then(r => { if (_n !== domainYukleNesli.current) return; setDomain(r.data) })
      .catch(e => { if (_n !== domainYukleNesli.current) return; const m = apiHata(e, cevir("Abonelik yüklenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
  }

  // İstatistikler kartının 'Yenile'si (önceden onClick'siz ölü düğmeydi): kartın
  // gösterdiği iki kaynağı — domain (trafik, PHP, oluşturulma) ve disk kullanımı —
  // yeniden çeker. Elle tetiklenir: /kaynak `du` çalıştırabilir, döngüye konmaz.
  // Yanıt gelmeden sayfa değişirse eski değer yazılmaz (domain: nesil; kaynak: useUc oturumu).
  async function istatistikYenile() {
    if (!id) return
    setYenileniyor(true)
    try {
      await Promise.all([domainYukle(), kaynak.yenile()])
    } finally { setYenileniyor(false) }
  }

  useEffect(() => {
    let iptal = false
    if (!id) return
    void domainYukle()
    // Mail eklentisi aktif+ödemeli ise Mail sekmesini göster.
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => {
        if (iptal) return
        setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif))
        setCalistiriciAktif(r.data.some(e => e.ad === 'calistirici' && e.aktif))
        setMailKurulu(r.data.some(e => e.ad === 'mail'))
        setCalistiriciKurulu(r.data.some(e => e.ad === 'calistirici'))
      })
      .catch(() => { if (iptal) return; setMailAktif(false); setCalistiriciAktif(false); setMailKurulu(false); setCalistiriciKurulu(false) })
    return () => { iptal = true; domainYukleNesli.current++ }
  }, [id])

  async function askiToggle() {
    if (!id || !domain) return
    const askiyaAl = !domain.askida
    if (askiyaAl && !(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("\"{0}\" askıya alınacak — site erişilemez olacak (503). Devam edilsin mi?"), domain.alan_adi), ton: 'uyari', onayIkonu: I.kilit }))) return
    setMenuAcik(false); setIsleniyor(true); setHata(null); setBildirim(null)
    try {
      await api.post(`/domains/${id}/${askiyaAl ? 'askiya-al' : 'askidan-al'}`)
      const iyi = askiyaAl ? cevir("Hesap askıya alındı — site artık 503 bakım sayfası döndürüyor.") : cevir("Askı kaldırıldı — site tekrar erişilebilir.")
      setBildirim(iyi)
      toast.basari(cevir("Kaydedildi"), iyi)
      setTimeout(() => setBildirim(null), 6000)
      void domainYukle()
    } catch (e) {
      const m = apiHata(e, cevir("İşlem başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setIsleniyor(false) }
  }

  if (hata && !domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }, { etiket: cevir("Hata") }]} />
      <UyariKutusu ton="tehlike">{hata}</UyariKutusu>
    </div>
  )

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
    </div>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain.alan_adi },
      ]} />

      <div className="flex items-center gap-3 mb-1">
        <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300">{domain.alan_adi}</h1>
        {!musteri && (
          <Button
            variant="flat"
            isIcon
            ikon={I.chevronAsagi}
            onClick={() => navigate('/abonelikler')}
            className="size-8 rounded-full"
            title={cevir("Başka aboneliğe geç")}
            aria-label={cevir("Başka aboneliğe geç")}
          />
        )}
        {domain.askida ? (
          <Badge color="error" variant="soft" className="text-[10px] px-2 py-0.5 uppercase font-semibold tracking-wider gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-current" aria-hidden="true"></span>
            {cevir("Askıda")}
          </Badge>
        ) : (
          <Badge color={domain.durum === 'aktif' ? 'success' : 'neutral'} variant="soft" className="text-[10px] px-2 py-0.5 uppercase font-semibold tracking-wider gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-current" aria-hidden="true"></span>
            {domain.durum}
          </Badge>
        )}
        {!musteri && (
          <div className="relative ml-1">
            <Button
              variant="flat"
              isIcon
              ikon={I.dikeyNokta}
              onClick={() => setMenuAcik(v => !v)}
              disabled={isleniyor}
              className="size-8 rounded-full"
              title={cevir("Daha fazla işlem")}
              aria-label={cevir("Daha fazla işlem")}
              aria-expanded={menuAcik} />
            {menuAcik && (
              <>
                <div className="fixed inset-0 z-10" onClick={() => setMenuAcik(false)} />
                <div className="absolute right-0 mt-1 z-20 w-56 max-w-[calc(100vw-1.5rem)] bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg shadow-lg py-1 text-sm">
                  <button
                    type="button"
                    onClick={askiToggle}
                    className={`w-full text-left px-3 py-2 flex items-center gap-2 hover:bg-slate-50 dark:hover:bg-dark-600/60 ${domain.askida ? 'text-emerald-700 dark:text-emerald-300' : 'text-red-600 dark:text-red-400'}`}>
                    {domain.askida ? (
                      <>
                        <Ikon d={I.kilitAcik} className="w-4 h-4 shrink-0" />
                        {cevir("Askıdan Al (Geri Getir)")}
                      </>
                    ) : (
                      <>
                        <Ikon d={I.kilit} className="w-4 h-4 shrink-0" />
                        {cevir("Hesabı Askıya Al")}
                      </>
                    )}
                  </button>
                </div>
              </>
            )}
          </div>
        )}
      </div>

      <div className="flex items-center gap-5 border-b border-slate-200 dark:border-dark-600 mb-5">
        <TabBtn aktif={tab === 'dashboard'} onClick={() => setTab('dashboard')}>{cevir("Pano")}</TabBtn>
        <TabBtn aktif={tab === 'hosting'}   onClick={() => setTab('hosting')}>{cevir("Barınma ve DNS")}</TabBtn>
        <TabBtn aktif={tab === 'apps'} onClick={() => setTab('apps')}>{cevir("Uygulamalar")}</TabBtn>
        {mailKurulu && <TabBtn aktif={tab === 'mail'} onClick={() => setTab('mail')}>Mail</TabBtn>}
        {calistiriciKurulu && <TabBtn aktif={tab === 'gelistirici'} onClick={() => setTab('gelistirici')}>{cevir("Geliştirici")}</TabBtn>}
      </div>

      {/* 🔴 GELİŞTİRİCİ SEKMESİ TAM GENİŞLİK.
          Uygulama çalıştırıcı; dağıtım adımları, uzun komut satırları, günlük
          çıktısı ve sürüm listesi gösteriyor — hepsi YATAY yer isteyen içerik.
          Üç sütunlu düzende ortadaki 6 sütuna sıkışınca komutlar sarılıyor ve
          günlük okunmaz hâle geliyordu. Yan sütunlardaki bilgiler (site
          önizlemesi, domain kaynak kartı) bu sekmede zaten işe yaramıyor:
          kullanıcı burada domainle değil UYGULAMAYLA ilgileniyor.
          Onun yerine eklentinin kendi sağ paneli var (servis durumu, port,
          disk) — yani bilgi kaybolmuyor, uygulamaya ÖZEL hâle geliyor. */}
      <div className={tab === 'gelistirici' ? '' : 'grid grid-cols-12 gap-5'}>
        {/* Alan adı panosu: Pano sekmesinde araç kartlarının (DomainPano) ÜSTÜNDE, tam
            genişlik. Başka sekmede gizli ama bağlı (veri korunur, canlı yoklama durur). */}
        {panoAcildi && (
          <div className="col-span-12 min-w-0" hidden={tab !== 'dashboard'}>
            <AlanPano key={domain.id} domain={domain} kaynak={kaynak} etkin={tab === 'dashboard'} />
          </div>
        )}
        {tab !== 'gelistirici' && (
        <aside className="col-span-12 lg:col-span-3 space-y-4">
          <WebSitePreview alanAdi={domain.alan_adi} ssl={domain.ssl} />

          <Card className="bg-white dark:bg-dark-700 p-5">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("İstatistikler")}</h3>
              <Button variant="flat" isIcon ikon={I.yenile} onClick={istatistikYenile} yukleniyor={yenileniyor} className="size-8 rounded-full" title={cevir("Yenile")} aria-label={cevir("Yenile")} />
            </div>
            <div className="space-y-2.5 text-sm">
              <Stat e={cevir("Disk alanı")}    d={diskMB != null ? `${diskMB} MB` : kaynak.hata ? '—' : '…'} />
              <Stat e={cevir("Aylık trafik")}  d={`${Math.round(domain.trafik_kb / 1024)} MB`} />
              <Stat e={cevir("Oluşturulma")}   d={domain.olusturulma} />
              <Stat e={cevir("PHP sürümü")}    d={domain.php_surum} />
            </div>
          </Card>
        </aside>
        )}

        <section className={tab === 'gelistirici' ? '' : 'col-span-12 lg:col-span-6'}>
          {tab === 'dashboard' && <DomainPano domain={domain} />}
          {tab === 'hosting'   && <HostingTab domain={domain} />}
          {tab === 'apps'      && <AppTab domain={domain} />}
          {tab === 'mail'      && (mailAktif ? <MailTab domain={domain} /> : <LisansAskida baslik={cevir("Mail Sunucu")} />)}
          {/* 🔴 `calistiriciAktif &&` şart: seçili sekme localStorage'da
              saklanıyor. Eklenti kaldırıldıktan sonra kullanıcı bu sayfaya
              dönerse sekme DÜĞMESİ çizilmez ama içerik yine de render edilir
              ve boş/hatalı bir ekran görünürdü. */}
          {tab === 'gelistirici' && calistiriciKurulu && (
            <EklentiEkrani
              ad="calistirici"
              baslik={cevir("Uygulama Çalıştırıcı")}
              sade
              ekBaglam={{ domainID: domain.id, alanAdi: domain.alan_adi }}
            />
          )}

          <div className="mt-5 pt-3 border-t border-slate-100 dark:border-dark-600 flex items-center justify-between text-xs text-slate-500 dark:text-slate-500 flex-wrap gap-2">
            <div className="flex items-center gap-4">
              <span>{cevir("Web sitesi:")} <span className="font-mono text-slate-700 dark:text-slate-300">public_html</span></span>
              <span>IP: <span className="font-mono text-slate-700 dark:text-slate-300">{domain.ipv4}</span></span>
              <span>{cevir("Sistem kullanıcısı:")} <span className="font-mono text-slate-700 dark:text-slate-300">{domain.sistem_kullanici}</span></span>
            </div>
          </div>
        </section>

        {tab !== 'gelistirici' && (
          <aside className="col-span-12 lg:col-span-3">
            <DomainKaynakKart domainId={domain.id} kaynak={kaynak} />
          </aside>
        )}
      </div>
    </div>
  )
}

function WebSitePreview({ alanAdi, ssl }: { alanAdi: string; ssl: boolean }) {
  const url = `${ssl ? 'https' : 'http'}://${alanAdi}`
  return (
    <Card className="bg-white dark:bg-dark-700 overflow-hidden">
      <div className="relative aspect-[4/3] bg-gradient-to-br from-dark-700 to-dark-900 overflow-hidden">
        {ssl ? (
          <div className="absolute inset-0 overflow-hidden pointer-events-none">
            <iframe
              src={url}
              title={cevirT(cevir("{0} önizleme"), alanAdi)}
              loading="lazy"
              sandbox="allow-scripts allow-same-origin"
              tabIndex={-1}
              aria-hidden
              className="origin-top-left"
              style={{ width: '400%', height: '400%', transform: 'scale(0.25)', border: 0, background: '#fff' }}
            />
          </div>
        ) : (
          <div className="absolute inset-0 flex flex-col items-center justify-center text-center px-4">
            <Ikon d={I.gozKapali} className="w-9 h-9 text-white/40 mb-2" />
            <div className="text-[11px] text-white/60">{cevir("Önizleme yalnızca HTTPS sitelerde gösterilir")}</div>
            <div className="text-[10px] text-white/40 mt-0.5">{cevir("SSL etkinleştirilince otomatik görünür")}</div>
          </div>
        )}
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/45 to-transparent p-3 flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="text-[9px] uppercase tracking-wider text-white/60">{cevir("Web Sitesi")}</div>
            <div className="text-xs font-semibold text-white truncate">{alanAdi}</div>
          </div>
          <a href={url} target="_blank" rel="noreferrer"
            className={butonSinifi({ boyut: 'sm', className: 'shrink-0' })}>
            {cevir("Aç")}
            <Ikon d={I.harici} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
          </a>
        </div>
      </div>
    </Card>
  )
}

function TabBtn({ aktif, onClick, children }: { aktif: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`relative pb-3 pt-1 text-sm transition ${
        aktif ? 'text-slate-900 dark:text-slate-100 font-semibold' : 'text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300'
      }`}
    >
      {children}
      {aktif && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-brand-500 rounded-t"></span>}
    </button>
  )
}

function Stat({ e, d }: { e: string; d: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-slate-500 dark:text-slate-500">{e}</span>
      <span className="text-slate-800 dark:text-slate-200 font-medium font-mono">{d}</span>
    </div>
  )
}

function Grup({ baslik, children }: { baslik: string; children: React.ReactNode }) {
  return (
    <section className="mb-5 last:mb-0">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{baslik}</h3>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">{children}</div>
    </section>
  )
}

function HostingTab({ domain }: { domain: Domain }) {
  return (
    <div>
      <Grup baslik={cevir("Barınma Hizmetleri")}>
        <ToolCard etiket={cevir("Hosting Planı")} aciklama={cevir("Yükselt, düşür veya özel plan")} ikon={ICONS.hizmet} renk="violet" to={`/abonelikler/${domain.id}/plan`} />
        <ToolCard etiket={cevir("Apache ve nginx")}     aciklama={cevir("Güvenlik başlıkları, ek direktifler")}  ikon={ICONS.apache} renk="orange" to={`/abonelikler/${domain.id}/web-sunucu`} />
      </Grup>
      <Grup baslik={cevir("Alan Adı ve DNS")}>
        <ToolCard etiket={cevir("DNS Yönetimi")} aciklama={cevir("A, MX, TXT, CNAME kayıtları")} ikon={ICONS.dns} renk="sky" to={`/abonelikler/${domain.id}/dns`} />
      </Grup>
    </div>
  )
}

// Resmî WordPress logosu (simple-icons, fill-tabanlı) — genel stroke ikonları
// markayı temsil etmiyordu; gerçek logo kullanılır.
function WPLogo() {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className="w-5 h-5" aria-hidden="true">
      <path d="M21.469 6.825c.84 1.537 1.318 3.3 1.318 5.175 0 3.979-2.156 7.456-5.363 9.325l3.295-9.527c.615-1.54.82-2.771.82-3.864 0-.405-.026-.78-.07-1.11m-7.981.105c.647-.03 1.232-.105 1.232-.105.582-.075.514-.93-.067-.899 0 0-1.755.135-2.88.135-1.064 0-2.85-.15-2.85-.15-.585-.03-.661.855-.075.885 0 0 .54.061 1.125.09l1.68 4.605-2.37 7.08L5.354 6.9c.649-.03 1.234-.1 1.234-.1.585-.075.516-.93-.065-.896 0 0-1.746.138-2.874.138-.2 0-.438-.008-.69-.015C4.911 3.15 8.235 1.215 12 1.215c2.809 0 5.365 1.072 7.286 2.833-.046-.003-.091-.009-.141-.009-1.06 0-1.812.923-1.812 1.914 0 .89.513 1.643 1.06 2.531.411.72.89 1.643.89 2.977 0 .915-.354 1.994-.821 3.479l-1.075 3.585-3.9-11.61.001.014zM12 22.784c-1.059 0-2.081-.153-3.048-.437l3.237-9.406 3.315 9.087c.024.053.05.101.078.149-1.12.393-2.325.607-3.582.607M1.211 12c0-1.564.336-3.05.935-4.39L7.29 21.709C3.694 19.96 1.212 16.271 1.211 12M12 0C5.385 0 0 5.385 0 12s5.385 12 12 12 12-5.385 12-12S18.615 0 12 0" />
    </svg>
  )
}

function AppTab({ domain }: { domain: Domain }) {
  const [katalog, setKatalog] = React.useState<any[]>([])
  const [kurulu, setKurulu] = React.useState<any[]>([])
  const [yukleniyor, setYukleniyor] = React.useState(true)
  const [kurTaslak, setKurTaslak] = React.useState<{kod: string; ad: string; ikon: string; alt_dizin: string} | null>(null)
  const [gonderiliyor, setGonderiliyor] = React.useState(false)
  const [hata, setHata] = React.useState<string | null>(null)
  const toast = useToast()
  const dialog = useDialog()

  const yukle = async () => {
    setYukleniyor(true); setHata(null)
    try {
      const [k, l] = await Promise.all([
        api.get<{items: any[]}>(`/uygulamalar/katalog`),
        api.get<{items: any[]}>(`/domains/${domain.id}/uygulamalar`),
      ])
      setKatalog(k.data.items || [])
      setKurulu(l.data.items || [])
    } catch (e: any) {
      const m = e?.response?.data?.error || cevir("Yüklenemedi")
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setYukleniyor(false) }
  }
  React.useEffect(() => { void yukle() }, [])

  const kur = async () => {
    if (!kurTaslak) return
    setGonderiliyor(true); setHata(null)
    try {
      await api.post(`/domains/${domain.id}/uygulamalar`, {
        kod: kurTaslak.kod, alt_dizin: kurTaslak.alt_dizin
      })
      setKurTaslak(null)
      await yukle()
    } catch (e: any) {
      const m = e?.response?.data?.error || cevir("Kurulum başarısız")
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally { setGonderiliyor(false) }
  }
  const sil = async (kayitID: number, ad: string) => {
    if (!(await dialog.onay({ baslik: cevirT(cevir("{0} silinsin mi? Dosyalar + DB kaybolur."), ad), onayEtiketi: cevir("Sil"), ton: 'tehlike', onayIkonu: I.cop }))) return
    try {
      await api.delete(`/domains/${domain.id}/uygulamalar/${kayitID}`)
      await yukle()
    } catch (e: any) { await dialog.bilgi({ baslik: e?.response?.data?.error || cevir("Silinemedi"), ton: 'tehlike' }) }
  }

  if (yukleniyor) return <div className="py-8 text-center text-slate-500">{cevir("Yükleniyor…")}</div>
  return (
    <div className="space-y-6">
      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 mb-3">
          {cevir("Öne Çıkan")}
        </h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
          <ToolCard etiket="WordPress" aciklama={cevir("1-tıkla kurulum · yönetim")} ikonNode={<WPLogo />} renk="sky"
            to={`/abonelikler/${domain.id}/wordpress`} />
        </div>
      </section>

      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 mb-3">
          {cevirT(cevir("Kurulu Uygulamalar ({0})"), kurulu.length)}
        </h3>
        {kurulu.length === 0 ? (
          <div className="rounded-lg border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500">
            {cevir("Henüz uygulama kurulu değil. Aşağıdaki katalogdan seç.")}
          </div>
        ) : (
          <div className="space-y-2">
            {kurulu.map((u) => (
              <Card key={u.id} className="flex items-center justify-between bg-white dark:bg-dark-800 p-3">
                <div className="flex-1">
                  <div className="font-medium">{u.ad} <span className="text-xs text-slate-500">v{u.surum}</span></div>
                  <div className="text-xs text-slate-500 font-mono">/{u.alt_dizin || ''} · DB: {u.db_adi || '—'}</div>
                </div>
                <div className="flex items-center gap-2">
                  <a href={u.yonetim_url} target="_blank" rel="noreferrer"
                    className={butonSinifi({ color: 'primary', boyut: 'sm' })}>
                    {cevir("Yönetim Paneli")}
                    <Ikon d={I.harici} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
                  </a>
                  <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(u.id, u.ad)}>{cevir("Sil")}</Button>
                </div>
              </Card>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 mb-3">
          {cevirT(cevir("Katalog ({0})"), katalog.length)}
        </h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {katalog.map((t) => (
            <Card key={t.Kod} className="bg-white dark:bg-dark-800 p-3">
              <div className="flex items-start gap-2 mb-2">
                {t.LogoURL ? (
                  <img src={t.LogoURL} alt={t.Ad} className="w-8 h-8 object-contain shrink-0"
                    onError={(e) => { (e.currentTarget as any).style.display = 'none' }} />
                ) : (
                  <span className="text-2xl">{t.Ikon}</span>
                )}
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate">{t.Ad}</div>
                  <div className="text-[10px] text-slate-500">v{t.Surum} · {t.Kategori}</div>
                </div>
                {t.NativeApp && (
                  <Badge color="warning" variant="soft" className="text-[9px] px-1.5 py-0.5 whitespace-nowrap">{cevir("yakında")}</Badge>
                )}
              </div>
              <p className="text-xs text-slate-600 mb-3">{t.Aciklama}</p>
              <Button
                color="primary"
                boyut="sm"
                ikon={t.NativeApp ? I.kilit : I.oynat}
                onClick={() => t.NativeApp
                  ? void dialog.bilgi({ baslik: t.Ad + ' ' + cevir("host seviyesinde bir uygulamadır, tenant panelinden kurulamaz.") })
                  : setKurTaslak({kod: t.Kod, ad: t.Ad, ikon: t.Ikon, alt_dizin: t.Kod})}
                disabled={t.NativeApp}
                className="w-full">
                {t.NativeApp ? cevir("Admin'den kurulur") : cevir("Kur")}
              </Button>
            </Card>
          ))}
        </div>
      </section>

      <Modal acik={!!kurTaslak} baslik={kurTaslak ? `${kurTaslak.ikon} ${kurTaslak.ad} ${cevir("Kur")}` : ''}
        onKapat={() => setKurTaslak(null)} kapatilamaz={gonderiliyor}
        alt={<>
          <Button variant="outlined" ikon={I.kapat} onClick={() => setKurTaslak(null)} disabled={gonderiliyor}>{cevir("Vazgeç")}</Button>
          <Button color="primary" ikon={I.oynat} onClick={kur} yukleniyor={gonderiliyor}>
            {gonderiliyor ? cevir("Kuruluyor…") : cevir("Kur")}
          </Button>
        </>}>
        {kurTaslak && (
          <>
            <label className="block text-xs font-medium text-slate-600 mb-1">{cevir("Alt dizin (public_html/... altına)")}</label>
            <input value={kurTaslak.alt_dizin} onChange={(e) => setKurTaslak({...kurTaslak, alt_dizin: e.target.value})}
              placeholder={cevir("boş = kök, blog, shop vb.")}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 font-mono text-sm mb-4 dark:bg-dark-900 dark:border-dark-600" />
            <p className="text-xs text-slate-500">
              {cevir("Site:")} <span className="font-mono">https://{domain.alan_adi}/{kurTaslak.alt_dizin}</span>
            </p>
          </>
        )}
      </Modal>
    </div>
  )
}

function MailTab({ domain }: { domain: Domain }) {
  return (
    <Grup baslik={cevir("Posta Hizmetleri")}>
      <ToolCard etiket={cevir("Mail Ayarları")}            aciklama={cevir("Sağlık analizi, bağlantı bilgileri")} ikon={ICONS.posta}      renk="emerald" to={`/abonelikler/${domain.id}/mail/ayarlar`} />
      <ToolCard etiket={cevir("Mail Kutuları")}            aciklama={cevir("Posta kutusu ekle/yönet")}            ikon={ICONS.baglanti}   renk="sky"     to={`/abonelikler/${domain.id}/mail/kutular`} />
      <ToolCard etiket={cevir("E-Posta Teslimat Takibi")}  aciklama={cevir("Gönderim/teslimat günlüğü")}          ikon={ICONS.istatistik} renk="indigo"  to={`/abonelikler/${domain.id}/mail/teslimat`} />
      <ToolCard etiket={cevir("Takma Adlar & Yönlendirme")} aciklama={cevir("Yönlendirme, catch-all")}            ikon={ICONS.baglanti}   renk="amber"   to={`/abonelikler/${domain.id}/mail/takmaadlar`} />
    </Grup>
  )
}
