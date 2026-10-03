import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useId, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Badge, Button, Select } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'

type Ayarlar = {
  hdr_x_content_type: boolean
  hdr_x_xss: boolean
  hdr_referrer: boolean
  hdr_permissions: boolean
  hdr_csp_upgrade: boolean
  hdr_hsts: boolean
  hsts_max_age: number
  hsts_subdomains: boolean
  hsts_preload: boolean
  fastcgi_cache: boolean
  fastcgi_cache_dakika: number
  browser_cache: boolean
  browser_cache_gun: number
  ek_direktifler: string
}

type Yanit = { alan_adi: string; ayarlar: Ayarlar }

const BACKEND_BILGI: Record<string, { ad: string; ikon: React.ReactNode; aciklama: string; renk: string }> = {
  'php-fpm': {
    ad: 'nginx + PHP-FPM',
    ikon: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5"><path d="M13 2L3 14h7l-1 8 10-12h-7l1-8z"/></svg>,
    aciklama: 'Varsayılan. nginx PHP-FPM\'i doğrudan fastcgi ile çağırır. En düşük gecikme, WordPress/Laravel/dinamik PHP siteler için ideal.',
    renk: 'emerald',
  },
  'apache': {
    ad: 'nginx + Apache',
    ikon: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5"><path d="M20 4C11 4 7 8 5 14l-2 6 6-2c6-2 10-6 11-14zM8 16l8-8"/></svg>,
    aciklama: 'nginx kenarda TLS terminatörü, Apache (10080) arkada vhost\'u servis eder. .htaccess tam desteği — Joomla, eski WP, legacy CMS\'ler için.',
    renk: 'indigo',
  },
  'static': {
    ad: 'Statik (PHP yok)',
    ikon: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5"><path d="M14 3v4a1 1 0 001 1h4M14 3H7a2 2 0 00-2 2v14a2 2 0 002 2h10a2 2 0 002-2V8l-5-5z"/></svg>,
    aciklama: 'Yalnız dosya servisi — React/Vue/Angular SPA, statik site jeneratörleri (Hugo/Jekyll), CDN içeriği için. PHP çağrıları 404 döner.',
    renk: 'slate',
  },
}

const HEADERS = [
  { key: 'hdr_x_content_type', etiket: 'X-Content-Type-Options', deger: 'nosniff',
    aciklama: 'MIME sniffing engelle — XSS savunması' },
  { key: 'hdr_x_xss', etiket: 'X-XSS-Protection', deger: '1; mode=block',
    aciklama: 'Eski tarayıcı XSS koruması' },
  { key: 'hdr_referrer', etiket: 'Referrer-Policy', deger: 'strict-origin-when-cross-origin',
    aciklama: 'Cross-site Referer bilgisi kısıtlanır' },
  { key: 'hdr_permissions', etiket: 'Permissions-Policy', deger: 'geolocation=(), microphone=(), camera=(), interest-cohort=()',
    aciklama: 'Kamera/mikrofon/konum API\'lerini varsayılan kapat' },
  { key: 'hdr_csp_upgrade', etiket: 'Upgrade Insecure Requests', deger: 'CSP: upgrade-insecure-requests',
    aciklama: 'HTTP linkleri otomatik HTTPS\'e yükselt' },
] as const


const WEBSRV_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "Uygulanıyor…": "Applying…",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Statik (PHP yok)": "Static (no PHP)",
  "Varsayılan. nginx PHP-FPM'i doğrudan fastcgi ile çağırır. En düşük gecikme, WordPress/Laravel/dinamik PHP siteler için ideal.": "Default. nginx calls PHP-FPM directly over fastcgi. Lowest latency, ideal for WordPress/Laravel/dynamic PHP sites.",
  "nginx kenarda TLS terminatörü, Apache (10080) arkada vhost'u servis eder. .htaccess tam desteği — Joomla, eski WP, legacy CMS'ler için.": "nginx as the edge TLS terminator, Apache (10080) serves the vhost behind it. Full .htaccess support — for Joomla, old WP, legacy CMSs.",
  "Kamera/mikrofon/konum API'lerini varsayılan kapat": "Disable camera/microphone/location APIs by default",
  "HTTP linkleri otomatik HTTPS'e yükselt": "Automatically upgrade HTTP links to HTTPS",
  "Web sunucusu \"{0}\" olarak değiştirildi": "Web server changed to \"{0}\"",
  "Belge kökü \"public_html/{0}\" olarak ayarlandı": "Document root set to \"public_html/{0}\"",
  "Belge kökü public_html köküne ayarlandı": "Document root set to the public_html root",
  "Ayarlar uygulandı, nginx yeniden yüklendi": "Settings applied, nginx reloaded",
  "Güvenlik başlıkları ve özel direktifler. Kaydedince nginx vhost yeniden render edilir.": "Security headers and custom directives. When saved, the nginx vhost is re-rendered.",
  "nginx kenarda TLS terminatörü olarak kalır; alttaki seçim domain'in arkasını çalışan motora yönlendirir.": "nginx stays at the edge as the TLS terminator; the selection below routes the domain's backend to the running engine.",
  "Aktif": "Active",
  ". Laravel, Symfony gibi": ". Frameworks like Laravel, Symfony",
  "'yi bir alt klasörde tutar — o klasörü buraya yazın.": " keep in a subfolder — write that folder here.",
  "· Kaydedince nginx vhost anında yeniden render edilir.": "· When saved, the nginx vhost is re-rendered instantly.",
  " yalnızca HTTPS aktif siteler için anlamlıdır. Site HTTP-only ise tarayıcıya gönderilmez.": " is only meaningful for sites with HTTPS active. If the site is HTTP-only, it is not sent to the browser.",
  "1 hafta": "1 week",
  "6 ay": "6 months",
  "5 dakika": "5 minutes",
  "15 dakika": "15 minutes",
  "6 saat": "6 hours",
  "3 ay": "3 months",
  "Ek nginx Direktifleri": "Extra nginx Directives",
  "Bu metin": "This text",
  "max-age (saniye)": "max-age (seconds)",
  "Kaydet ve Uygula": "Save and Apply",
  "# Örn:\nclient_max_body_size 200m;\nrewrite ^/eski/(.*)$ /yeni/$1 permanent;": "# E.g:\nclient_max_body_size 200m;\nrewrite ^/old/(.*)$ /new/$1 permanent;",
  "(boş = public_html kökü)": "(empty = public_html root)",
  "1 gün": "1 day",
  "1 saat (önerilen)": "1 hour (recommended)",
  "1 yıl": "1 year",
  "1 yıl (önerilen)": "1 year (recommended)",
  "2 yıl (preload için)": "2 years (for preload)",
  "30 gün": "30 days",
  "30 gün (önerilen)": "30 days (recommended)",
  "5 dk (test için)": "5 min (for testing)",
  "Algılanan klasörler:": "Detected folders:",
  "Apache ve nginx Ayarları": "Apache and nginx Settings",
  "Backend değişimi başarısız": "Backend change failed",
  "Belge Kök Dizini": "Document Root",
  "Belge Kökünü Kaydet": "Save Document Root",
  "Belge kökü değiştirilemedi": "Failed to change document root",
  "Cross-site Referer bilgisi kısıtlanır": "Cross-site Referer info is restricted",
  "Eski tarayıcı XSS koruması": "Legacy browser XSS protection",
  "Etkin kök:": "Active root:",
  "Güvenlik Başlıkları (HTTP + HTTPS)": "Security Headers (HTTP + HTTPS)",
  "HTTP Strict Transport Security (yalnızca HTTPS)": "HTTP Strict Transport Security (HTTPS only)",
  "Laravel için:": "For Laravel:",
  "MIME sniffing engelle — XSS savunması": "Block MIME sniffing — XSS defense",
  "Performans Önbelleği": "Performance Cache",
  "Tarayıcılar siteye yalnızca HTTPS ile bağlanır. Yanlış konfigürasyon zor geri alınır — yetkili kullanın.": "Browsers connect to the site over HTTPS only. Misconfiguration is hard to undo — use with authority.",
  "Tarayıcılara fabrika ayarı olarak gönder (hstspreload.org'a kayıt gerekir)": "Send to browsers as a factory setting (registration at hstspreload.org required)",
  "Tüm subdomain'lere uygula (önce subdomain'lerin HTTPS olduğundan emin ol)": "Apply to all subdomains (make sure the subdomains are HTTPS first)",
  "Web Sunucu Yığını": "Web Server Stack",
  "Yalnız dosya servisi — React/Vue/Angular SPA, statik site jeneratörleri (Hugo/Jekyll), CDN içeriği için. PHP çağrıları 404 döner.": "File-serving only — for React/Vue/Angular SPAs, static site generators (Hugo/Jekyll), CDN content. PHP calls return 404.",
  "public_html içindeki alt klasör": "Subfolder inside public_html",
  "Sitenizin hızını artıran önbellek ayarları.": "Cache settings that speed up your site.",
  "Sayfa önbelleği": "Page cache",
  "Tarayıcı önbelleği": "Browser cache",
  "Statik dosyalar": "Static files",
  "WordPress/PHP sayfalarını önbelleğe alarak siteyi hızlandırır. Giriş yapmış kullanıcılar ve formlar etkilenmez.": "Speeds up the site by caching WordPress/PHP pages. Logged-in users and forms are not affected.",
  "Resim, CSS ve JS dosyalarını ziyaretçinin tarayıcısında saklar; tekrar gelenlerde sayfa daha hızlı açılır.": "Stores images, CSS and JS files in the visitor's browser; pages open faster for returning visitors.",
  "Saklama süresi": "Retention period",
  "Teknik ayrıntılar": "Technical details",
  "Yöntem": "Method",
  "Yanıt başlığı": "Response header",
  "Önbellek süresi": "Cache duration",
  "Geçerlilik": "Expires",
  "Dosya türleri": "File types",
  "Atlananlar": "Bypassed",
  "Not": "Note",
  "{0} dakika": "{0} minutes",
  "{0} gün": "{0} days",
  "POST istekleri, oturum çerezleri, giriş ve önizleme sayfaları": "POST requests, session cookies, login and preview pages",
  "WP Site Health \"Page cache detected\" uyarısını giderir.": "Resolves the WP Site Health \"Page cache detected\" warning.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WEBSRV_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainWebSunucuPage() {
  useTranslation() // dil re-render aboneligi
  const toast = useToast()
  const { id } = useParams()
  const [yanit, setYanit] = useState<Yanit | null>(null)
  const [a, setA] = useState<Ayarlar | null>(null)
  const [yuk, setYuk] = useState(true)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)

  const [backend, setBackend] = useState<string>('php-fpm')
  const [backendDegistiriliyor, setBackendDegistiriliyor] = useState(false)

  const [webRoot, setWebRoot] = useState('')
  const [webRootKayitli, setWebRootKayitli] = useState('')
  const [webRootAdaylar, setWebRootAdaylar] = useState<string[]>([])
  const [webRootKaydediliyor, setWebRootKaydediliyor] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    Promise.all([
      api.get<Yanit>(`/domains/${id}/nginx-settings`),
      api.get<{backend: string}>(`/domains/${id}/web-backend`),
      api.get<{alt_dizin: string; adaylar: string[]}>(`/domains/${id}/web-root`),
    ]).then(([y, b, w]) => {
      setYanit(y.data); setA(y.data.ayarlar)
      setBackend(b.data.backend)
      setWebRoot(w.data.alt_dizin); setWebRootKayitli(w.data.alt_dizin)
      setWebRootAdaylar(w.data.adaylar || [])
    }).catch(e => {
      const m = apiHata(e)
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function backendKaydet(yeni: string) {
    if (yeni === backend || backendDegistiriliyor) return
    setBackendDegistiriliyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/web-backend`, { backend: yeni })
      setBackend(yeni)
      const m = cevirT(cevir("Web sunucusu \"{0}\" olarak değiştirildi"), BACKEND_BILGI[yeni]?.ad || yeni)
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      const m = apiHata(e, cevir("Backend değişimi başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setBackendDegistiriliyor(false)
    }
  }

  async function webRootKaydet() {
    if (webRoot.trim() === webRootKayitli || webRootKaydediliyor) return
    setWebRootKaydediliyor(true); setHata(null); setBasari(null)
    try {
      const r = await api.put<{ alt_dizin: string }>(`/domains/${id}/web-root`, { alt_dizin: webRoot.trim() })
      setWebRoot(r.data.alt_dizin); setWebRootKayitli(r.data.alt_dizin)
      const m = r.data.alt_dizin
        ? cevirT(cevir("Belge kökü \"public_html/{0}\" olarak ayarlandı"), r.data.alt_dizin)
        : cevir("Belge kökü public_html köküne ayarlandı")
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      const m = apiHata(e, cevir("Belge kökü değiştirilemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setWebRootKaydediliyor(false)
    }
  }

  async function kaydet() {
    if (!a) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/nginx-settings`, { ayarlar: a })
      setBasari(cevir("Ayarlar uygulandı, nginx yeniden yüklendi"))
      toast.basari(cevir("Kaydedildi"), cevir("Ayarlar uygulandı, nginx yeniden yüklendi"))
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Kaydetme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  function P<K extends keyof Ayarlar>(k: K, v: Ayarlar[K]) {
    if (!a) return
    setA({ ...a, [k]: v })
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: yanit?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Apache ve nginx Ayarları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Apache ve nginx Ayarları")}</h1>
      {yanit && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{yanit.alan_adi}</Link>
        {' · '}{cevir("Güvenlik başlıkları ve özel direktifler. Kaydedince nginx vhost yeniden render edilir.")}
      </p>}

      {/* Web Sunucu Yığını Seçici */}
      <div className="mb-6 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Web Sunucu Yığını")}</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
              {cevir("nginx kenarda TLS terminatörü olarak kalır; alttaki seçim domain'in arkasını çalışan motora yönlendirir.")}
            </p>
          </div>
          {backendDegistiriliyor && <span className="text-xs text-slate-400 dark:text-slate-500">{cevir("Uygulanıyor…")}</span>}
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {(['php-fpm','apache','static'] as const).map(k => {
            const b = BACKEND_BILGI[k]
            const aktif = backend === k
            const renkler: Record<string, string> = {
              emerald: aktif ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20 ring-2 ring-emerald-500/20' : 'border-slate-200 dark:border-dark-600 hover:border-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-900/20',
              indigo:  aktif ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20 ring-2 ring-indigo-500/20'    : 'border-slate-200 dark:border-dark-600 hover:border-indigo-300 hover:bg-indigo-50 dark:hover:bg-indigo-900/20',
              slate:   aktif ? 'border-slate-500 dark:border-dark-400 bg-slate-100 dark:bg-dark-600 ring-2 ring-slate-400/20'      : 'border-slate-200 dark:border-dark-600 hover:border-slate-400 hover:bg-slate-50 dark:hover:bg-dark-800',
            }
            return (
              <button key={k} type="button"
                onClick={() => backendKaydet(k)}
                disabled={backendDegistiriliyor || aktif}
                className={`text-left p-4 border rounded-lg transition disabled:cursor-default ${renkler[b.renk]}`}
              >
                <div className="flex items-center justify-between mb-1.5">
                  <span className="text-lg leading-none">{b.ikon}</span>
                  {aktif && <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wider font-semibold text-emerald-700 dark:text-emerald-300"><span className="inline-block size-2 rounded-full bg-emerald-500" aria-hidden />{cevir("Aktif")}</span>}
                </div>
                <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir(b.ad)}</div>
                <div className="text-[11px] text-slate-600 dark:text-slate-400 dark:text-slate-500 mt-1.5 leading-snug">{cevir(b.aciklama)}</div>
              </button>
            )
          })}
        </div>
      </div>

      {/* Belge Kök Dizini (Laravel vb. için dinamik alt klasör) */}
      <div className="mb-6 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
        <div className="mb-3">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Belge Kök Dizini")}</h3>
          <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
            {cevir(cevir("Sitenin açılış (index) dizini. Varsayılan"))} <code className="font-mono">public_html</code>{cevir(". Laravel, Symfony gibi")}
            {cevir(cevir("çatılar"))} <code className="font-mono">index.php</code>{cevir("'yi bir alt klasörde tutar — o klasörü buraya yazın.")}
          </p>
        </div>

        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("public_html içindeki alt klasör")}</label>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-stretch flex-1 min-w-0 rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden focus-within:ring-2 focus-within:ring-brand-500/30">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-dark-800 text-xs font-mono text-slate-400 dark:text-slate-500 border-r border-slate-200 dark:border-dark-600 select-none whitespace-nowrap">public_html/</span>
            <input
              list="webroot-adaylar"
              value={webRoot}
              onChange={e => setWebRoot(e.target.value.replace(/^\/+/, ''))}
              onKeyDown={e => { if (e.key === 'Enter') webRootKaydet() }}
              placeholder={cevir("(boş = public_html kökü)")}
              spellCheck={false} autoCapitalize="off" autoCorrect="off"
              className="flex-1 min-w-0 px-3 py-2 text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100 outline-none"
            />
          </div>
          <Button color="primary" onClick={webRootKaydet}
            disabled={webRootKaydediliyor || webRoot.trim() === webRootKayitli} yukleniyor={webRootKaydediliyor}
            ikon={I.disket} className="whitespace-nowrap">
            {webRootKaydediliyor ? cevir("Uygulanıyor…") : cevir("Belge Kökünü Kaydet")}
          </Button>
        </div>
        <datalist id="webroot-adaylar">
          {webRootAdaylar.map(x => <option key={x} value={x} />)}
        </datalist>

        {webRootAdaylar.length > 0 && (
          <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-slate-400 dark:text-slate-500">{cevir("Algılanan klasörler:")}</span>
            {webRootAdaylar.map(x => (
              <Button key={x} variant="outlined" boyut="sm" ikon={I.klasor} onClick={() => setWebRoot(x)}
                className="font-mono">
                {x}
              </Button>
            ))}
          </div>
        )}

        <div className="mt-3 flex items-start gap-2 px-2.5 py-2 bg-slate-50 dark:bg-dark-800/40 rounded-md">
          <Ikon d={I.ileri} className="mt-px size-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
          <div className="text-xs text-slate-600 dark:text-slate-400 min-w-0">
            <span className="text-slate-400 dark:text-slate-500">{cevir("Etkin kök:")} </span>
            <code className="font-mono break-all text-slate-700 dark:text-slate-300">public_html{webRoot.trim() ? '/' + webRoot.trim().replace(/^\/+|\/+$/g, '') : ''}</code>
            <div className="mt-1 text-[11px] text-slate-400 dark:text-slate-500">
              {cevir(cevir("Klasör önceden var olmalı."))} <span className="font-medium text-slate-500 dark:text-slate-400">{cevir("Laravel için:")} <code className="font-mono">public</code></span> {cevir("· Kaydedince nginx vhost anında yeniden render edilir.")}
            </div>
          </div>
        </div>
      </div>

      <UyariKutusu ton="uyari" className="mb-5" kucuk>
        <strong>HSTS</strong>{cevir(" yalnızca HTTPS aktif siteler için anlamlıdır. Site HTTP-only ise tarayıcıya gönderilmez.")}
        {cevir(cevir("Ayarlar değiştiğinde"))} <code className="font-mono">nginx -t</code> + <code className="font-mono">reload</code> {cevir("otomatik tetiklenir — sıfır kesinti.")}
      </UyariKutusu>

      {yuk || !a ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> : (
        <>
          {/* Genel security headers */}
          <Kart baslik={cevir("Güvenlik Başlıkları (HTTP + HTTPS)")}>
            <div className="space-y-3">
              {HEADERS.map(h => (
                <SatirToggle
                  key={h.key}
                  etiket={h.etiket}
                  deger={h.deger}
                  aciklama={cevir(h.aciklama)}
                  acik={a[h.key] as boolean}
                  onToggle={() => P(h.key as keyof Ayarlar, !a[h.key] as never)}
                />
              ))}
            </div>
          </Kart>

          {/* HSTS özel */}
          <Kart baslik={cevir("HTTP Strict Transport Security (yalnızca HTTPS)")}>
            <SatirToggle
              etiket="Strict-Transport-Security"
              deger={`max-age=${a.hsts_max_age}${a.hsts_subdomains ? '; includeSubDomains' : ''}${a.hsts_preload ? '; preload' : ''}`}
              aciklama={cevir("Tarayıcılar siteye yalnızca HTTPS ile bağlanır. Yanlış konfigürasyon zor geri alınır — yetkili kullanın.")}
              acik={a.hdr_hsts}
              onToggle={() => P('hdr_hsts', !a.hdr_hsts)}
            />
            {a.hdr_hsts && (
              <div className="mt-3 pl-4 border-l-2 border-slate-200 dark:border-dark-600 space-y-2">
                <div>
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("max-age (saniye)")}</label>
                  <select value={a.hsts_max_age} onChange={e => P('hsts_max_age', parseInt(e.target.value))}
                    className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono">
                    <option value={300}>{cevir("5 dk (test için)")}</option>
                    <option value={86400}>{cevir("1 gün")}</option>
                    <option value={604800}>{cevir("1 hafta")}</option>
                    <option value={2592000}>{cevir("30 gün")}</option>
                    <option value={15768000}>{cevir("6 ay")}</option>
                    <option value={31536000}>{cevir("1 yıl (önerilen)")}</option>
                    <option value={63072000}>{cevir("2 yıl (preload için)")}</option>
                  </select>
                </div>
                <CheckboxRow
                  etiket="includeSubDomains"
                  aciklama={cevir("Tüm subdomain'lere uygula (önce subdomain'lerin HTTPS olduğundan emin ol)")}
                  checked={a.hsts_subdomains}
                  onChange={v => P('hsts_subdomains', v)}
                />
                <CheckboxRow
                  etiket="preload"
                  aciklama={cevir("Tarayıcılara fabrika ayarı olarak gönder (hstspreload.org'a kayıt gerekir)")}
                  checked={a.hsts_preload}
                  onChange={v => P('hsts_preload', v)}
                />
              </div>
            )}
          </Kart>

          {/* Performans Önbelleği — sade, insan dilinde sunum; teknik ayrıntılar katlanır bölümde */}
          <Kart baslik={cevir("Performans Önbelleği")}>
            <p className="-mt-1 mb-3 text-sm text-slate-500 dark:text-slate-400">
              {cevir("Sitenizin hızını artıran önbellek ayarları.")}
            </p>
            <div className="space-y-3">
              <OnbellekSecenegi
                ikon={I.simsek}
                baslik={cevir("Sayfa önbelleği")}
                altEtiket="Nginx FastCGI"
                aciklama={cevir("WordPress/PHP sayfalarını önbelleğe alarak siteyi hızlandırır. Giriş yapmış kullanıcılar ve formlar etkilenmez.")}
                acik={a.fastcgi_cache}
                onToggle={() => P('fastcgi_cache', !a.fastcgi_cache)}
                teknik={[
                  [cevir("Yöntem"), 'Nginx FastCGI Cache'],
                  [cevir("Yanıt başlığı"), 'X-Cache-Status: HIT / MISS / BYPASS'],
                  [cevir("Önbellek süresi"), cevirT(cevir("{0} dakika"), a.fastcgi_cache_dakika)],
                  [cevir("Atlananlar"), cevir("POST istekleri, oturum çerezleri, giriş ve önizleme sayfaları")],
                  [cevir("Not"), cevir("WP Site Health \"Page cache detected\" uyarısını giderir.")],
                ]}
              >
                <Select
                  label={cevir("Saklama süresi")}
                  value={a.fastcgi_cache_dakika}
                  onChange={e => P('fastcgi_cache_dakika', parseInt(e.target.value))}
                  classNames={{ root: 'w-full sm:max-w-xs' }}
                  data={[
                    { value: 5, label: cevir("5 dakika") },
                    { value: 15, label: cevir("15 dakika") },
                    { value: 60, label: cevir("1 saat (önerilen)") },
                    { value: 360, label: cevir("6 saat") },
                    { value: 1440, label: cevir("1 gün") },
                  ]}
                />
              </OnbellekSecenegi>

              <OnbellekSecenegi
                ikon={I.monitor}
                baslik={cevir("Tarayıcı önbelleği")}
                altEtiket={cevir("Statik dosyalar")}
                aciklama={cevir("Resim, CSS ve JS dosyalarını ziyaretçinin tarayıcısında saklar; tekrar gelenlerde sayfa daha hızlı açılır.")}
                acik={a.browser_cache}
                onToggle={() => P('browser_cache', !a.browser_cache)}
                teknik={[
                  [cevir("Yanıt başlığı"), 'Cache-Control: public, immutable'],
                  [cevir("Geçerlilik"), cevirT(cevir("{0} gün"), a.browser_cache_gun)],
                  [cevir("Dosya türleri"), 'CSS, JS, PNG, JPG, WOFF…'],
                ]}
              >
                <Select
                  label={cevir("Saklama süresi")}
                  value={a.browser_cache_gun}
                  onChange={e => P('browser_cache_gun', parseInt(e.target.value))}
                  classNames={{ root: 'w-full sm:max-w-xs' }}
                  data={[
                    { value: 1, label: cevir("1 gün") },
                    { value: 7, label: cevir("1 hafta") },
                    { value: 30, label: cevir("30 gün (önerilen)") },
                    { value: 90, label: cevir("3 ay") },
                    { value: 365, label: cevir("1 yıl") },
                  ]}
                />
              </OnbellekSecenegi>
            </div>
          </Kart>

          {/* Ek direktifler */}
          <Kart baslik={cevir("Ek nginx Direktifleri")}>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-2">
              {cevir("Bu metin")} <code className="font-mono">server</code> {cevir("bloğunun sonuna eklenir. Örn:")} <code className="font-mono">client_max_body_size 200m;</code>
            </p>
            <textarea value={a.ek_direktifler} onChange={e => P('ek_direktifler', e.target.value)}
              rows={6}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-xs font-mono"
              placeholder={cevir("# Örn:\nclient_max_body_size 200m;\nrewrite ^/eski/(.*)$ /yeni/$1 permanent;")} />
          </Kart>

          <div className="flex gap-3 mt-6">
            <Button color="primary" onClick={kaydet} disabled={isleniyor} yukleniyor={isleniyor}
              ikon={I.disket}>
              {isleniyor ? cevir("Uygulanıyor…") : cevir("Kaydet ve Uygula")}
            </Button>
            <Button variant="outlined" onClick={yukle} disabled={isleniyor} yukleniyor={yuk}
              ikon={I.yenile}>
              {cevir(cevir("Yeniden Yükle"))}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function Kart({ baslik, children }: { baslik: string; children: React.ReactNode }) {
  return (
    <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-dark-600">{baslik}</h3>
      {children}
    </div>
  )
}

function SatirToggle({ etiket, deger, aciklama, acik, onToggle }:
  { etiket: string; deger: string; aciklama: string; acik: boolean; onToggle: () => void }) {
  return (
    <div className="flex items-start gap-3 py-2 border-b border-slate-50 dark:border-dark-600/50 last:border-0">
      <button onClick={onToggle} role="switch" aria-checked={acik} aria-label={etiket}
        className={`shrink-0 mt-0.5 relative inline-flex h-6 w-11 items-center rounded-full transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 ${
          acik ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'
        }`}>
        <span className={`inline-block h-4 w-4 transform rounded-full bg-white dark:bg-dark-700 shadow transition ${acik ? 'translate-x-6' : 'translate-x-1'}`} />
      </button>
      <div className="flex-1 min-w-0">
        <div className="flex items-baseline justify-between gap-2">
          <div className="font-mono text-sm font-semibold text-slate-900 dark:text-slate-100">{etiket}</div>
          <code className="text-xs font-mono text-slate-500 dark:text-slate-500 truncate">{deger}</code>
        </div>
        <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">{aciklama}</div>
      </div>
    </div>
  )
}

function CheckboxRow({ etiket, aciklama, checked, onChange }:
  { etiket: string; aciklama: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-start gap-2 cursor-pointer">
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)}
        className="mt-1 cursor-pointer" />
      <div>
        <div className="font-mono text-xs font-medium text-slate-900 dark:text-slate-100">{etiket}</div>
        <div className="text-xs text-slate-500 dark:text-slate-500">{aciklama}</div>
      </div>
    </label>
  )
}

// Performans Önbelleği seçeneği: ikon + insan dilinde başlık + gri teknik alt etiket,
// sağda durum rozeti ve anahtar. Teknik ayrıntılar <details> içinde katlanır (klavye ve
// ekran okuyucu yerel olarak destekler). Ayar alanı (children) yalnız açıkken görünür.
function OnbellekSecenegi({ ikon, baslik, altEtiket, aciklama, acik, onToggle, teknik, children }: {
  ikon: string; baslik: string; altEtiket: string; aciklama: string; acik: boolean
  onToggle: () => void; teknik: Array<[string, string]>; children?: React.ReactNode
}) {
  const kimlik = useId()
  const baslikId = `${kimlik}-baslik`
  const aciklamaId = `${kimlik}-aciklama`
  return (
    <div className={`rounded-lg border p-4 transition-colors ${
      acik
        ? 'border-emerald-200 bg-emerald-50/40 dark:border-emerald-500/25 dark:bg-emerald-500/[0.06]'
        : 'border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-700'
    }`}>
      <div className="flex items-start gap-3">
        <div className={`flex size-9 shrink-0 items-center justify-center rounded-lg ${
          acik
            ? 'bg-emerald-100 text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-400'
            : 'bg-slate-100 text-slate-500 dark:bg-dark-600 dark:text-slate-400'
        }`}>
          <Ikon d={ikon} className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h4 id={baslikId} className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h4>
            <Badge color={acik ? 'success' : 'neutral'} variant="soft" className="text-[11px]">
              {acik ? cevir("Açık") : cevir("Kapalı")}
            </Badge>
          </div>
          <div className="mt-0.5 text-xs text-slate-400 dark:text-slate-500">{altEtiket}</div>
          <p id={aciklamaId} className="mt-1.5 text-sm text-slate-600 dark:text-slate-300">{aciklama}</p>
        </div>
        <button type="button" onClick={onToggle} role="switch" aria-checked={acik}
          aria-labelledby={baslikId} aria-describedby={aciklamaId}
          className={`relative mt-1 inline-flex h-6 w-11 shrink-0 items-center rounded-full transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 ${
            acik ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'
          }`}>
          <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition ${acik ? 'translate-x-6' : 'translate-x-1'}`} />
        </button>
      </div>

      {acik && children && (
        <div className="mt-4 border-t border-slate-200/70 pt-4 dark:border-dark-600 sm:ml-12">
          {children}
        </div>
      )}

      <details className="group mt-3 sm:ml-12">
        <summary className="inline-flex cursor-pointer select-none list-none items-center gap-1 rounded text-xs font-medium text-slate-500 hover:text-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-slate-400 dark:hover:text-slate-200 [&::-webkit-details-marker]:hidden">
          <Ikon d={I.chevronSag} className="size-3.5 transition-transform group-open:rotate-90" />
          {cevir("Teknik ayrıntılar")}
        </summary>
        <dl className="mt-2 grid grid-cols-1 gap-x-4 gap-y-1.5 rounded-md bg-slate-50 px-3 py-2.5 text-xs dark:bg-dark-800/50 sm:grid-cols-[max-content_1fr]">
          {teknik.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-slate-400 dark:text-slate-500">{k}</dt>
              <dd className="min-w-0 break-words font-mono text-slate-700 dark:text-slate-300">{v}</dd>
            </div>
          ))}
        </dl>
      </details>
    </div>
  )
}
