import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// PHP & Sunucu Kurulum Sihirbazı — dağınık PHP sürüm/modül/loader/web-sunucu
// yönetimini tek yerde toplar (EasyApache tarzı adım-adım). Her adım mevcut
// yönetim ekranını gömülü (gomulu) render eder; backend endpoint'leri aynıdır.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { apiHata } from '@/lib/api'
import { useDialog } from '@/components/Dialog'
import PHPSurumleriPage, { SurumSecim } from './PHPSurumleriPage'
import PHPModuleriPage, { Secim } from './PHPModuleriPage'

type Adim = { key: string; ad: string; aciklama: string }
const ADIMLAR: Adim[] = [
  { key: 'surumler', ad: 'PHP Sürümleri', aciklama: 'Sunucuya PHP sürümü ekle/kaldır' },
  { key: 'eklentiler', ad: 'PHP Eklentileri & Loader', aciklama: 'Modül aç/kapa, PECL kur, ionCube' },
  { key: 'runtimeler', ad: 'Ek Runtime\'lar', aciklama: '.NET Core, Node.js, Python' },
  { key: 'websunucu', ad: 'Web Sunucu', aciklama: 'nginx / PHP-FPM durumu' },
  { key: 'ozet', ad: 'Özet', aciklama: 'Yapılandırmayı gözden geçir' },
]

type RuntimeSecim = { anahtar: string; ad: string }
type RuntimeEk = { ad: string; anahtar: string; aciklama: string; kurulu: boolean; yonetilemez?: boolean }
type RuntimeGrup = { tip: string; ad: string; ekler: RuntimeEk[] }


const PSIHIR_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "Bilgi": "Info",
  "PHP Sürümleri": "PHP Versions",
  "PHP Eklentileri & Loader": "PHP Extensions & Loader",
  "Ek Runtime'lar": "Additional Runtimes",
  "Web Sunucu": "Web Server",
  ".NET Core, Node.js, Python": ".NET Core, Node.js, Python",
  "nginx / PHP-FPM durumu": "nginx / PHP-FPM status",
  "PHP & Sunucu Kurulum Sihirbazı": "PHP & Server Setup Wizard",
  "← Geri": "← Back",
  "Bitir": "Finish",
  "Kurulacaklara ekle": "Add to install list",
  "runtime bileşeni kurulmak üzere seçildi — Özet adımında kurulur.": "runtime component(s) selected to install — installed in the Summary step.",
  "✓ Kurulu": "✓ Installed",
  "Bu platform": "This platform",
  "nginx + izole per-tenant PHP-FPM": "nginx + isolated per-tenant PHP-FPM",
  "mimarisi kullanır. cPanel/EasyApache'deki gibi Apache derlemesi veya global Apache modül seçimi": "architecture is used. Unlike cPanel/EasyApache there is no Apache compilation or global Apache module selection",
  "yoktur": "none",
  "; her domain kendi PHP-FPM havuzunda ve nginx vhost'unda çalışır.": "; each domain runs in its own PHP-FPM pool and nginx vhost.",
  "Apache uyumluluk": "Apache compatibility",
  "Barınma ve DNS → Apache & nginx": "Hosting and DNS → Apache & nginx",
  "PHP-FPM (per-tenant izole)": "PHP-FPM (per-tenant isolated)",
  "Özet & Uygula": "Summary & Apply",
  "Kurulacak bileşenler": "Components to install",
  "\"PHP Sürümleri\" ve \"PHP Eklentileri\" adımlarından kurmak istediklerinizi toggle ile işaretleyin, sonra buradan tümünü birlikte kurun.": "Mark what you want to install with the toggle in the \"PHP Versions\" and \"PHP Extensions\" steps, then install them all together here.",
  "sürüm": "version",
  "eklenti": "extension",
  "kuruluyor": "installing",
  "Paketler kuruluyor…": "Installing packages…",
  "Kuruluyor…": "Installing…",
  "Bekliyor": "Waiting",
  "✓ Kuruldu": "✓ Installed",
  "✕ Hata": "✕ Error",
  "Kurulu PHP sürümleri": "Installed PHP versions",
  "Kurulu PHP sürümü": "Installed PHP version",
  "Modül aç/kapa, PECL kur, ionCube": "Toggle modules, install PECL, ionCube",
  "PHP dışı diller. Kurulacakları toggle ile işaretleyin — Özet adımında birlikte kurulur. Kurulu olanı kapatınca kaldırılır.": "Non-PHP languages. Mark those to install with the toggle — installed together in the Summary step. Disabling an installed one removes it.",
  "PHP sürümleri, eklentiler, loader'lar ve web sunucu ayarlarını tek yerden yönetin. Değişiklikler her adımda anında uygulanır.": "Manage PHP versions, extensions, loaders and web server settings in one place. Changes are applied instantly at each step.",
  "Site bazında (.htaccess → nginx)": "Per-site (.htaccess → nginx)",
  "Sunucuya PHP sürümü ekle/kaldır": "Add/remove PHP version on the server",
  "Yapılandırmayı gözden geçir": "Review the configuration",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PSIHIR_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PHPSunucuSihirbaziPage() {
  useTranslation() // dil re-render aboneligi
  // Aktif adım kalıcı (localStorage) — sayfa yenilenince kalınan adım korunur.
  const [aktif, setAktifState] = useState<string>(() => {
    try {
      const v = localStorage.getItem('gosp.sihirbaz.adim')
      if (v && ADIMLAR.some(a => a.key === v)) return v
    } catch { /* yok say */ }
    return 'surumler'
  })
  const setAktif = (k: string) => {
    setAktifState(k)
    try { localStorage.setItem('gosp.sihirbaz.adim', k) } catch { /* yok say */ }
  }
  const aktifIdx = ADIMLAR.findIndex(a => a.key === aktif)
  // Sihirbaz genelinde biriktirilen "kurulacak" seçimler (Özet adımında toplu kurulur).
  const [secilenler, setSecilenler] = useState<Secim[]>([])
  const [secilenSurumler, setSecilenSurumler] = useState<SurumSecim[]>([])
  const [secilenRuntimeler, setSecilenRuntimeler] = useState<RuntimeSecim[]>([])

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Sunucu Yönetimi"), href: '/araclar-ayarlar' },
        { etiket: cevir("PHP & Sunucu Sihirbazı") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("PHP & Sunucu Kurulum Sihirbazı")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">{cevir("PHP sürümleri, eklentiler, loader'lar ve web sunucu ayarlarını tek yerden yönetin. Değişiklikler her adımda anında uygulanır.")}</p>

      <div className="grid grid-cols-1 lg:grid-cols-[15rem_minmax(0,1fr)] gap-5 items-start">
        {/* Sol adım listesi */}
        <nav className="lg:sticky lg:top-[4.5rem] flex lg:flex-col gap-1 overflow-x-auto lg:overflow-visible pb-1">
          {ADIMLAR.map((a, i) => {
            const secili = a.key === aktif
            const tamam = i < aktifIdx
            return (
              <button
                key={a.key}
                onClick={() => setAktif(a.key)}
                className={`flex items-center gap-3 px-3 py-2.5 rounded-xl text-left transition shrink-0 lg:shrink w-auto lg:w-full ${
                  secili
                    ? 'bg-brand-50 dark:bg-brand-900/25 border border-brand-300 dark:border-brand-700'
                    : 'border border-transparent hover:bg-slate-50 dark:hover:bg-slate-800'
                }`}
              >
                <span className={`flex items-center justify-center w-6 h-6 rounded-full text-xs font-semibold shrink-0 ${
                  secili ? 'bg-brand-600 text-white' : tamam ? 'bg-emerald-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300'
                }`}>
                  {tamam ? '✓' : i + 1}
                </span>
                <span className="min-w-0">
                  <span className={`block text-sm font-medium ${secili ? 'text-brand-800 dark:text-brand-200' : 'text-slate-700 dark:text-slate-200'}`}>{cevir(a.ad)}</span>
                  <span className="hidden lg:block text-[11px] text-slate-400 dark:text-slate-500 truncate">{cevir(a.aciklama)}</span>
                </span>
              </button>
            )
          })}
        </nav>

        {/* İçerik */}
        <section className="min-w-0">
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 sm:p-5">
            {aktif === 'surumler' && <PHPSurumleriPage gomulu secilenSurumler={secilenSurumler} setSecilenSurumler={setSecilenSurumler} />}
            {aktif === 'eklentiler' && <PHPModuleriPage gomulu secilenler={secilenler} setSecilenler={setSecilenler} />}
            {aktif === 'runtimeler' && <RuntimeAdim secilenRuntimeler={secilenRuntimeler} setSecilenRuntimeler={setSecilenRuntimeler} />}
            {aktif === 'websunucu' && <WebSunucuAdim />}
            {aktif === 'ozet' && <OzetAdim secilenler={secilenler} secilenSurumler={secilenSurumler} secilenRuntimeler={secilenRuntimeler} onTemizle={() => { setSecilenler([]); setSecilenSurumler([]); setSecilenRuntimeler([]) }} />}
          </div>

          {/* İleri / Geri */}
          <div className="flex items-center justify-between mt-4">
            <button
              onClick={() => aktifIdx > 0 && setAktif(ADIMLAR[aktifIdx - 1].key)}
              disabled={aktifIdx === 0}
              className="px-4 py-2 text-sm rounded-md border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40"
            >{cevir("← Geri")}</button>
            {aktifIdx < ADIMLAR.length - 1 ? (
              <button
                onClick={() => setAktif(ADIMLAR[aktifIdx + 1].key)}
                className="px-4 py-2 text-sm rounded-md bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 font-medium"
              >{cevir("İleri →")}</button>
            ) : (
              <Link to="/araclar-ayarlar" className="px-4 py-2 text-sm rounded-md bg-emerald-600 hover:bg-emerald-700 text-white font-medium">{cevir("Bitir")}</Link>
            )}
          </div>
        </section>
      </div>
    </div>
  )
}

// Ek Runtime'lar adımı: .NET Core / Node.js / Python bileşenleri. Kurulu değilse
// toggle ile "kurulacak" seçilir (Özet'te toplu kurulur); kurulu ise toggle kapatınca
// ANINDA kaldırılır (onay + async). PHP eklentileriyle aynı seç→uygula mantığı.
function RuntimeAdim({ secilenRuntimeler, setSecilenRuntimeler }: {
  secilenRuntimeler: RuntimeSecim[]
  setSecilenRuntimeler: (fn: (s: RuntimeSecim[]) => RuntimeSecim[]) => void
}) {
  const { onay, bilgi } = useDialog()
  const [gruplar, setGruplar] = useState<RuntimeGrup[]>([])
  const [yuk, setYuk] = useState(true)
  const [kaldiran, setKaldiran] = useState<string | null>(null)

  function yukle() {
    setYuk(true)
    api.get('/runtimeler').then(r => setGruplar(r.data?.gruplar || [])).catch(() => {}).finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  const secili = (anahtar: string) => secilenRuntimeler.some(s => s.anahtar === anahtar)
  const secimToggle = (e: RuntimeEk) => {
    setSecilenRuntimeler(prev => secili(e.anahtar)
      ? prev.filter(s => s.anahtar !== e.anahtar)
      : [...prev, { anahtar: e.anahtar, ad: e.ad }])
  }

  async function kaldir(e: RuntimeEk) {
    if (kaldiran) return
    if (!(await onay({ baslik: cevir("Kaldır"), mesaj: cevirT(cevir("{0} sunucudan kaldırılacak. Devam?"), e.ad), tehlike: true }))) return
    setKaldiran(e.anahtar)
    try {
      const { data } = await api.post('/runtimeler/kaldir', { anahtar: e.anahtar })
      if (data.is_id) {
        for (;;) {
          await new Promise(r => setTimeout(r, 1500))
          const p = await api.get('/runtimeler/durum', { params: { id: data.is_id } })
          if (p.data.durum === 'hata') throw new Error(p.data.hata || cevir("Kaldırma başarısız"))
          if (p.data.durum === 'tamam') break
        }
      }
      yukle()
    } catch (err) {
      (await bilgi({ baslik: cevir("Bilgi"), mesaj: apiHata(err, cevir("Kaldırma başarısız")) }))
    } finally {
      setKaldiran(null)
    }
  }

  if (yuk) return <div className="py-10 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">{cevir("Ek Runtime'lar")}</h2>
        <p className="text-sm text-slate-500 dark:text-slate-500">{cevir("PHP dışı diller. Kurulacakları toggle ile işaretleyin — Özet adımında birlikte kurulur. Kurulu olanı kapatınca kaldırılır.")}</p>
      </div>
      {(secilenRuntimeler.length > 0) && (
        <div className="px-3 py-2 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-md text-sm text-brand-700 dark:text-brand-300">
          {secilenRuntimeler.length} {cevir("runtime bileşeni kurulmak üzere seçildi — Özet adımında kurulur.")}
        </div>
      )}
      {gruplar.map(g => (
        <section key={g.tip}>
          <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{g.ad}</h3>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {g.ekler.map(e => {
              const sec = !e.kurulu && secili(e.anahtar)
              return (
                <div key={e.anahtar}
                  className={`flex items-center justify-between gap-2 px-3 py-2.5 rounded-lg border ${
                    e.kurulu ? 'bg-emerald-50 dark:bg-emerald-900/15 border-emerald-200 dark:border-emerald-800'
                    : sec ? 'bg-brand-50 dark:bg-brand-900/15 border-brand-300 dark:border-brand-700'
                    : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700'
                  }`}>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{e.ad}</div>
                    <div className="text-[11px] text-slate-500 dark:text-slate-500 truncate">{e.aciklama}</div>
                  </div>
                  {e.yonetilemez ? (
                    // Sistem yönetimli (ör. Node.js/nodesource) — yalnız durum, aksiyon yok.
                    <span className={`shrink-0 text-[11px] px-2 py-0.5 rounded-full font-medium ${e.kurulu ? 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400'}`}>
                      {e.kurulu ? cevir("✓ Kurulu") : cevir("Kurulu değil")}
                    </span>
                  ) : (
                    <button
                      onClick={() => e.kurulu ? kaldir(e) : secimToggle(e)}
                      disabled={kaldiran === e.anahtar}
                      title={e.kurulu ? cevir("Kaldır") : (sec ? cevir("Seçimi kaldır") : cevir("Kurulacaklara ekle"))}
                      className={`flex-shrink-0 relative inline-flex h-5 w-9 items-center rounded-full transition ${
                        kaldiran === e.anahtar ? 'bg-sky-400 animate-pulse' : e.kurulu ? 'bg-emerald-500' : sec ? 'bg-brand-500' : 'bg-slate-300 dark:bg-slate-600'
                      } ${kaldiran === e.anahtar ? 'opacity-60' : ''}`}
                    >
                      <span className={`inline-block h-3 w-3 transform rounded-full bg-white shadow transition ${(e.kurulu || sec) ? 'translate-x-5' : 'translate-x-1'}`} />
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        </section>
      ))}
    </div>
  )
}

// Web Sunucu adımı: bu mimaride web sunucu nginx + per-tenant PHP-FPM'dir
// (cPanel/EasyApache'nin Apache-derleme modeli GEÇERLİ DEĞİL). Durum gösterilir;
// site-bazlı ayarlar (Apache uyumluluk katmanı, güvenlik başlıkları) domain
// detayındaki "Apache & nginx" ekranında yapılır.
function WebSunucuAdim() {
  const [durum, setDurum] = useState<{ nginx?: boolean; fpm_sayisi?: number } | null>(null)
  useEffect(() => {
    api.get('/php-surumler').then(r => {
      const yuklu = (r.data?.surumler || []).filter((s: any) => s.yuklu).length
      setDurum({ nginx: true, fpm_sayisi: yuklu })
    }).catch(() => setDurum({ nginx: true }))
  }, [])
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">{cevir("Web Sunucu")}</h2>
      <div className="rounded-xl border border-sky-200 dark:border-sky-800/50 bg-sky-50 dark:bg-sky-900/15 p-4 text-sm text-sky-800 dark:text-sky-200">
        {cevir("Bu platform")} <strong>{cevir("nginx + izole per-tenant PHP-FPM")}</strong> {cevir("mimarisi kullanır. cPanel/EasyApache'deki gibi Apache derlemesi veya global Apache modül seçimi")} <strong>{cevir("yoktur")}</strong>{cevir("; her domain kendi PHP-FPM havuzunda ve nginx vhost'unda çalışır.")}
      </div>
      <dl className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <DurumKart etiket={cevir("Web Sunucu")} deger="nginx" ok />
        <DurumKart etiket={cevir("PHP çalışma modu")} deger={cevir("PHP-FPM (per-tenant izole)")} ok />
        <DurumKart etiket={cevir("Kurulu PHP sürümü")} deger={durum?.fpm_sayisi != null ? cevirT(cevir("{0} sürüm"), durum.fpm_sayisi) : '…'} ok={!!durum?.fpm_sayisi} />
        <DurumKart etiket={cevir("Apache uyumluluk")} deger={cevir("Site bazında (.htaccess → nginx)")} ok />
      </dl>
      <p className="text-xs text-slate-500 dark:text-slate-500">
        {cevir(cevir("Bir sitenin güvenlik başlıkları, ek direktifler ve Apache/nginx uyumluluk ayarları için ilgili domainin"))} <strong>{cevir("Barınma ve DNS → Apache & nginx")}</strong> {cevir(cevir("ekranını kullanın."))}
      </p>
    </div>
  )
}

function DurumKart({ etiket, deger, ok }: { etiket: string; deger: string; ok?: boolean }) {
  return (
    <div className="rounded-xl border border-slate-200 dark:border-slate-700 p-3">
      <div className="text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-1">{etiket}</div>
      <div className="flex items-center gap-1.5 text-sm font-medium text-slate-800 dark:text-slate-200">
        <span className={`w-1.5 h-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
        {deger}
      </div>
    </div>
  )
}

// Özet adımı: seçilen eklentileri TOPLU kurar (kullanıcı isteği). Her eklenti
// için mevcut async PECL-Kur akışı sırayla çalışır, canlı ilerleme gösterilir.
type Toplu = { tip: 'surum' | 'eklenti' | 'runtime'; anahtar: string; ad: string; surum: string; kaynak?: string; durum: 'bekliyor' | 'kuruluyor' | 'tamam' | 'hata'; mesaj?: string }
function OzetAdim({ secilenler, secilenSurumler, secilenRuntimeler, onTemizle }: { secilenler: Secim[]; secilenSurumler: SurumSecim[]; secilenRuntimeler: RuntimeSecim[]; onTemizle: () => void }) {
  const [surumler, setSurumler] = useState<any[]>([])
  const [calisiyor, setCalisiyor] = useState(false)
  const [durumlar, setDurumlar] = useState<Toplu[]>([])
  const [aktifAdim, setAktifAdim] = useState<{ ad: string; adim: string; yuzde: number } | null>(null)
  const [bitti, setBitti] = useState(false)

  useEffect(() => {
    api.get('/php-surumler').then(r => setSurumler((r.data?.surumler || []).filter((s: any) => s.yuklu))).catch(() => {})
  }, [])

  const toplamIs = secilenSurumler.length + secilenler.length + secilenRuntimeler.length

  async function topluKur() {
    if (calisiyor || toplamIs === 0) return
    setCalisiyor(true); setBitti(false)
    // Sıra: PHP sürümleri → eklentiler → runtime'lar.
    const liste: Toplu[] = [
      ...secilenSurumler.map(s => ({ tip: 'surum' as const, anahtar: s.surum, ad: `PHP ${s.surum}`, surum: s.surum, kaynak: s.kaynak, durum: 'bekliyor' as const })),
      ...secilenler.map(s => ({ tip: 'eklenti' as const, anahtar: s.anahtar, ad: s.ad, surum: s.surum, durum: 'bekliyor' as const })),
      ...secilenRuntimeler.map(s => ({ tip: 'runtime' as const, anahtar: s.anahtar, ad: s.ad, surum: '', durum: 'bekliyor' as const })),
    ]
    setDurumlar(liste)
    for (let i = 0; i < liste.length; i++) {
      const s = liste[i]
      setDurumlar(prev => prev.map((x, j) => j === i ? { ...x, durum: 'kuruluyor' } : x))
      setAktifAdim({ ad: s.ad, adim: cevir("Başlatılıyor…"), yuzde: 2 })
      try {
        if (s.tip === 'surum') {
          // PHP sürüm kurulumu — detached iş (systemd-run); durum poll ile izlenir.
          await api.post('/php-surumler/kur', { surum: s.surum, kaynak: s.kaynak })
          // 🔴 YARIŞ FIX: systemd-run unit'i aktifleşmeden ilk poll calisiyor=false
          // görebiliyordu → döngü ERKEN kırılıp kurulum bitmeden doğrulama yapıyor,
          // YENİ sürüm (ör. 8.6) sahte "kurulamadı" veriyordu. Çözüm: (a) unit'in
          // aktifleşmesi için ~20sn başlama-grace'i (calisiyor'u en az bir kez gör),
          // (b) sonra bitene kadar poll.
          let gordukCalisiyor = false
          const basla = Date.now()
          for (;;) {
            await new Promise(r => setTimeout(r, 2000))
            const d = await api.get('/php-surumler/durum')
            const calis = !!(d.data?.calisiyor && d.data?.surum === s.surum)
            if (calis) gordukCalisiyor = true
            setAktifAdim({ ad: s.ad, adim: calis ? cevir("Paketler kuruluyor…") : cevir("Tamamlanıyor…"), yuzde: calis ? 55 : 90 })
            if (!d.data?.calisiyor && (gordukCalisiyor || Date.now() - basla > 20000)) break
          }
          // 🔴 GERÇEK doğrulama: detached iş HÂLÂ kuruyor olabilir (grace'te kırıldıysa)
          // ve binary birkaç saniye sonra görünebilir. Sürüm yüklü listesinde belirene
          // kadar ~90sn boyunca yeniden kontrol et; kaynak eşitliği ŞART DEĞİL (yüklüyse
          // hangi kaynaktan olursa olsun kuruldu demektir).
          let yuklendi = false
          for (let t = 0; t < 45; t++) {
            const chk = await api.get('/php-surumler')
            if ((chk.data?.surumler || []).some((x: any) => x.surum === s.surum && x.yuklu)) { yuklendi = true; break }
            setAktifAdim({ ad: s.ad, adim: cevir("Doğrulanıyor…"), yuzde: 95 })
            await new Promise(r => setTimeout(r, 2000))
          }
          if (!yuklendi) throw new Error(cevirT(cevir("PHP {0} kurulamadı (paket eksik/derlenemedi olabilir — Sürümler sekmesindeki loga bakın)"), s.surum))
        } else if (s.tip === 'runtime') {
          // Runtime kurulumu (.NET/Node/Python) — async is_id + durum poll.
          const { data } = await api.post('/runtimeler/kur', { anahtar: s.anahtar })
          if (data.is_id) {
            for (;;) {
              await new Promise(r => setTimeout(r, 1500))
              const p = await api.get('/runtimeler/durum', { params: { id: data.is_id } })
              setAktifAdim({ ad: s.ad, adim: p.data.adim || '…', yuzde: p.data.yuzde || 0 })
              if (p.data.durum === 'hata') throw new Error(p.data.hata || cevir("Kurulum başarısız"))
              if (p.data.durum === 'tamam') break
            }
          }
        } else {
          // Eklenti — bundled dnf / pecl / derleme (async is_id + yüzde).
          const { data } = await api.post('/php-extensions/pecl-install', { surum: s.surum, paket: s.anahtar })
          if (data.is_id) {
            for (;;) {
              await new Promise(r => setTimeout(r, 1500))
              const p = await api.get('/php-extensions/pecl-durum', { params: { id: data.is_id } })
              setAktifAdim({ ad: s.ad, adim: p.data.adim || '…', yuzde: p.data.yuzde || 0 })
              if (p.data.durum === 'hata') throw new Error(p.data.hata || cevir("Kurulum başarısız"))
              if (p.data.durum === 'tamam') break
            }
          }
        }
        setDurumlar(prev => prev.map((x, j) => j === i ? { ...x, durum: 'tamam' } : x))
      } catch (e) {
        setDurumlar(prev => prev.map((x, j) => j === i ? { ...x, durum: 'hata', mesaj: apiHata(e) } : x))
      }
    }
    setAktifAdim(null); setCalisiyor(false); setBitti(true)
    onTemizle() // seçim listelerini temizle (kurulanlar artık "kurulu")
  }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">{cevir("Özet & Uygula")}</h2>

      {/* Kurulu sürümler */}
      <div>
        <div className="text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{cevir("Kurulu PHP sürümleri")}</div>
        <div className="flex flex-wrap gap-2">
          {surumler.length === 0 ? <span className="text-sm text-slate-500">—</span> : surumler.map((s: any) => (
            <span key={s.surum} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-slate-100 dark:bg-slate-700/60 text-sm font-mono text-slate-800 dark:text-slate-200">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />PHP {s.surum}
            </span>
          ))}
        </div>
      </div>

      {/* Kurulacak bileşenler (PHP sürümleri + eklentiler) */}
      <div>
        <div className="text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{cevir("Kurulacak bileşenler")} ({toplamIs})</div>
        {toplamIs === 0 && !bitti ? (
          <div className="rounded-xl border border-dashed border-slate-300 dark:border-slate-700 p-4 text-sm text-slate-500 dark:text-slate-400">
            {cevir("\"PHP Sürümleri\" ve \"PHP Eklentileri\" adımlarından kurmak istediklerinizi toggle ile işaretleyin, sonra buradan tümünü birlikte kurun.")}
          </div>
        ) : (
          <div className="space-y-2">
            {(durumlar.length > 0 ? durumlar : [
              ...secilenSurumler.map(s => ({ tip: 'surum' as const, anahtar: s.surum, ad: `PHP ${s.surum}`, surum: s.surum, kaynak: s.kaynak, durum: 'bekliyor' as const })),
              ...secilenler.map(s => ({ tip: 'eklenti' as const, anahtar: s.anahtar, ad: s.ad, surum: s.surum, durum: 'bekliyor' as const })),
              ...secilenRuntimeler.map(s => ({ tip: 'runtime' as const, anahtar: s.anahtar, ad: s.ad, surum: '', durum: 'bekliyor' as const })),
            ] as Toplu[]).map((s) => (
              <div key={s.tip + s.surum + s.anahtar} className="flex items-center justify-between gap-2 px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800">
                <div className="min-w-0">
                  <span className="text-[9px] uppercase tracking-wide mr-1.5 px-1 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400">{s.tip === 'surum' ? cevir("sürüm") : s.tip === 'runtime' ? 'runtime' : cevir("eklenti")}</span>
                  <span className="text-sm font-medium text-slate-800 dark:text-slate-200">{s.ad}</span>
                  {s.tip === 'eklenti' && <span className="ml-1.5 font-mono text-[11px] text-slate-400 dark:text-slate-500">PHP {s.surum}</span>}
                  {s.mesaj && <div className="text-[11px] text-red-600 dark:text-red-400 mt-0.5">{s.mesaj}</div>}
                </div>
                <DurumRozet durum={s.durum} />
              </div>
            ))}
          </div>
        )}
      </div>

      {aktifAdim && (
        <div className="rounded-lg border border-brand-200 dark:border-brand-800 bg-brand-50 dark:bg-brand-950/40 px-4 py-3">
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-sm font-medium text-brand-800 dark:text-brand-200">
              <span className="inline-block w-3.5 h-3.5 mr-2 align-[-2px] rounded-full border-2 border-brand-400 border-t-transparent animate-spin" />
              <span className="font-mono">{aktifAdim.ad}</span> {cevir("kuruluyor")}
            </span>
            <span className="text-xs tabular-nums text-brand-700 dark:text-brand-300">%{aktifAdim.yuzde}</span>
          </div>
          <div className="text-xs text-brand-700 dark:text-brand-300 mb-2">{aktifAdim.adim}</div>
          <div className="h-2 rounded-full bg-brand-100 dark:bg-brand-900 overflow-hidden">
            <div className="h-full rounded-full bg-brand-500 transition-all duration-500" style={{ width: `${Math.min(100, aktifAdim.yuzde)}%` }} />
          </div>
        </div>
      )}

      {bitti && <div className="rounded-xl border border-emerald-200 dark:border-emerald-800/50 bg-emerald-50 dark:bg-emerald-900/15 p-4 text-sm text-emerald-800 dark:text-emerald-200">{cevir("✓ Toplu kurulum tamamlandı. Kurulan sürüm/eklentiler ilgili adımlarda aktif görünür.")}</div>}

      {toplamIs > 0 && (
        <button onClick={topluKur} disabled={calisiyor}
          className="w-full sm:w-auto px-5 py-2.5 rounded-md bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium disabled:opacity-60">
          {calisiyor ? cevir("Kuruluyor…") : cevirT(cevir("{0} bileşeni kur"), toplamIs)}
        </button>
      )}
    </div>
  )
}

function DurumRozet({ durum }: { durum: Toplu['durum'] }) {
  const map: Record<Toplu['durum'], { t: string; c: string }> = {
    bekliyor: { t: 'Bekliyor', c: 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400' },
    kuruluyor: { t: 'Kuruluyor…', c: 'bg-brand-100 dark:bg-brand-900/40 text-brand-700 dark:text-brand-300' },
    tamam: { t: '✓ Kuruldu', c: 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300' },
    hata: { t: '✕ Hata', c: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300' },
  }
  const m = map[durum]
  return <span className={`shrink-0 text-[11px] px-2 py-0.5 rounded-full font-medium ${m.c}`}>{cevir(m.t)}</span>
}
