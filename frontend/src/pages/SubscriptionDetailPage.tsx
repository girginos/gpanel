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
import { useUc } from '@/components/alanpano/useUc'
import type { KaynakOzet } from '@/components/alanpano/tipler'
import ToolCard from '@/components/ToolCard'
import EklentiEkrani from '@/components/EklentiEkrani'
import LisansAskida from '@/components/LisansAskida'
import UygulamaKatalogu from '@/components/UygulamaKatalogu'
import type { Domain } from '@/components/domainTipi'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge, Card, butonSinifi } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I, Ikon } from '@/components/Ikon'

// Yalnız kök (admin) ve bayi bu sayfayı görür: hosting müşterisine (rol 'user') aynı
// rotada MusteriAnaSayfa çizilir (App.tsx AbonelikSayfasi). Müşteriye özel dallar
// (abonelik değiştirme / yönetim menüsünü gizleme) bu yüzden kaldırıldı.

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
  "A, MX, TXT, CNAME kayıtları": "A, MX, TXT, CNAME records",
  "Abonelik yüklenemedi": "Failed to load subscription",
  "Alan Adı ve DNS": "Domain and DNS",
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
  const setTab = (t: Tab) => {
    setTabState(t)
    try { localStorage.setItem('gosp.abonelik.tab', t) } catch { /* yok say */ }
  }
  // 🔴 /kaynak sayfa başına TEK çekim (kota kapalıyken `du -sm`, 0.7–2 sn): burada
  // çekilir; sağdaki DomainKaynakKart ve soldaki 'İstatistikler' kartı aynı durumu
  // prop ile paylaşır. İki 'Yenile' de bu tek yenileyiciyi çağırır (uçuştaki istek
  // varken ikinci istek gitmez).
  // Not: Pano sekmesinin üstündeki alan adı özet panosu (AlanPano) 2026-09-29'da
  // kaldırıldı (kullanıcı kararı; bu tür widget'lar bayi panosuna taşınacak).
  const kaynak = useUc<KaynakOzet>(id ? `/domains/${id}/kaynak` : null, cevir("Kaynak bilgisi alınamadı"))
  const diskMB = kaynak.veri ? kaynak.veri.disk_mb.kullanim : null
  const [menuAcik, setMenuAcik] = useState(false)
  const [isleniyor, setIsleniyor] = useState(false)
  const [mailAktif, setMailAktif] = useState(false)
  // 🔴 KURULU (aktif'ten AYRI): askıya alınmış (kurulu ama aktif=0) eklentinin
  // sekmesi kaybolmasın — açık kalıp landing göstersin (Marka/top-level ile tutarlı).
  const [mailKurulu, setMailKurulu] = useState(false)
  const [calistiriciKurulu, setCalistiriciKurulu] = useState(false)
  const [yenileniyor, setYenileniyor] = useState(false)

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
        setMailKurulu(r.data.some(e => e.ad === 'mail'))
        setCalistiriciKurulu(r.data.some(e => e.ad === 'calistirici'))
      })
      .catch(() => { if (iptal) return; setMailAktif(false); setMailKurulu(false); setCalistiriciKurulu(false) })
    return () => { iptal = true; domainYukleNesli.current++ }
  }, [id])

  async function askiToggle() {
    if (!id || !domain) return
    const askiyaAl = !domain.askida
    if (askiyaAl && !(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("\"{0}\" askıya alınacak — site erişilemez olacak (503). Devam edilsin mi?"), domain.alan_adi), ton: 'uyari', onayIkonu: I.kilit }))) return
    setMenuAcik(false); setIsleniyor(true); setHata(null)
    try {
      await api.post(`/domains/${id}/${askiyaAl ? 'askiya-al' : 'askidan-al'}`)
      // Başarı mesajı toast ile gösterilir (ayrı bir sayfa içi bildirim alanı yok).
      const iyi = askiyaAl ? cevir("Hesap askıya alındı — site artık 503 bakım sayfası döndürüyor.") : cevir("Askı kaldırıldı — site tekrar erişilebilir.")
      toast.basari(cevir("Kaydedildi"), iyi)
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
        <Button
          variant="flat"
          isIcon
          ikon={I.chevronAsagi}
          onClick={() => navigate('/abonelikler')}
          className="size-8 rounded-full"
          title={cevir("Başka aboneliğe geç")}
          aria-label={cevir("Başka aboneliğe geç")}
        />
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
          {tab === 'apps'      && <UygulamaKatalogu domain={domain} />}
          {tab === 'mail'      && (mailAktif ? <MailTab domain={domain} /> : <LisansAskida baslik={cevir("Mail Sunucu")} />)}
          {/* 🔴 `calistiriciKurulu &&` şart: seçili sekme localStorage'da
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
