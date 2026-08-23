import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

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

const BACKEND_BILGI: Record<string, { ad: string; ikon: string; aciklama: string; renk: string }> = {
  'php-fpm': {
    ad: 'nginx + PHP-FPM',
    ikon: '⚡',
    aciklama: 'Varsayılan. nginx PHP-FPM\'i doğrudan fastcgi ile çağırır. En düşük gecikme, WordPress/Laravel/dinamik PHP siteler için ideal.',
    renk: 'emerald',
  },
  'apache': {
    ad: 'nginx + Apache',
    ikon: '🪶',
    aciklama: 'nginx kenarda TLS terminatörü, Apache (10080) arkada vhost\'u servis eder. .htaccess tam desteği — Joomla, eski WP, legacy CMS\'ler için.',
    renk: 'indigo',
  },
  'static': {
    ad: 'Statik (PHP yok)',
    ikon: '📄',
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
  "CSS/JS/PNG/JPG/WOFF gibi statik dosyalar tarayıcıda önbellekte tutulur. Tekrar ziyaretlerde sayfa çok daha hızlı yüklenir.": "Static files like CSS/JS/PNG/JPG/WOFF are cached in the browser. Pages load much faster on repeat visits.",
  "Cache süresi (dakika)": "Cache duration (minutes)",
  "Cache süresi (gün)": "Cache duration (days)",
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
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WEBSRV_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainWebSunucuPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const [yanit, setYanit] = useState<Yanit | null>(null)
  const [a, setA] = useState<Ayarlar | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
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
    }).catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function backendKaydet(yeni: string) {
    if (yeni === backend || backendDegistiriliyor) return
    setBackendDegistiriliyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/web-backend`, { backend: yeni })
      setBackend(yeni)
      setBasari(`✓ Web sunucusu "${BACKEND_BILGI[yeni]?.ad || yeni}" olarak değiştirildi`)
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      setHata(apiHata(e, cevir("Backend değişimi başarısız")))
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
      setBasari(r.data.alt_dizin
        ? `✓ Belge kökü "public_html/${r.data.alt_dizin}" olarak ayarlandı`
        : cevir("✓ Belge kökü public_html köküne ayarlandı"))
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      setHata(apiHata(e, cevir("Belge kökü değiştirilemedi")))
    } finally {
      setWebRootKaydediliyor(false)
    }
  }

  async function kaydet() {
    if (!a) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/nginx-settings`, { ayarlar: a })
      setBasari(cevir("✓ Ayarlar uygulandı, nginx yeniden yüklendi"))
      yukle()
    } catch (e) {
      setHata(apiHata(e, cevir("Kaydetme başarısız")))
    } finally {
      setIsleniyor(false)
    }
  }

  function P<K extends keyof Ayarlar>(k: K, v: Ayarlar[K]) {
    if (!a) return
    setA({ ...a, [k]: v })
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: yanit?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Apache ve nginx Ayarları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Apache ve nginx Ayarları")}</h1>
      {yanit && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{yanit.alan_adi}</Link>
        {' · '}Güvenlik başlıkları ve özel direktifler. Kaydedince nginx vhost yeniden render edilir.
      </p>}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Web Sunucu Yığını Seçici */}
      <div className="mb-6 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Web Sunucu Yığını")}</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
              nginx kenarda TLS terminatörü olarak kalır; alttaki seçim domain'in arkasını çalışan motora yönlendirir.
            </p>
          </div>
          {backendDegistiriliyor && <span className="text-xs text-slate-400 dark:text-slate-500">{cevir("Uygulanıyor…")}</span>}
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {(['php-fpm','apache','static'] as const).map(k => {
            const b = BACKEND_BILGI[k]
            const aktif = backend === k
            const renkler: Record<string, string> = {
              emerald: aktif ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20 ring-2 ring-emerald-500/20' : 'border-slate-200 dark:border-slate-700 hover:border-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-900/30 dark:bg-emerald-900/20',
              indigo:  aktif ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20 ring-2 ring-indigo-500/20'    : 'border-slate-200 dark:border-slate-700 hover:border-indigo-300 hover:bg-indigo-50 dark:bg-indigo-900/20',
              slate:   aktif ? 'border-slate-500 bg-slate-100 dark:bg-slate-800 ring-2 ring-slate-400/20'      : 'border-slate-200 dark:border-slate-700 hover:border-slate-400 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800',
            }
            return (
              <button key={k} type="button"
                onClick={() => backendKaydet(k)}
                disabled={backendDegistiriliyor || aktif}
                className={`text-left p-4 border rounded-lg transition disabled:cursor-default ${renkler[b.renk]}`}
              >
                <div className="flex items-center justify-between mb-1.5">
                  <span className="text-lg leading-none">{b.ikon}</span>
                  {aktif && <span className="text-[10px] uppercase tracking-wider font-semibold text-emerald-700 dark:text-emerald-300">● Aktif</span>}
                </div>
                <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{b.ad}</div>
                <div className="text-[11px] text-slate-600 dark:text-slate-400 dark:text-slate-500 mt-1.5 leading-snug">{b.aciklama}</div>
              </button>
            )
          })}
        </div>
      </div>

      {/* Belge Kök Dizini (Laravel vb. için dinamik alt klasör) */}
      <div className="mb-6 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="mb-3">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Belge Kök Dizini")}</h3>
          <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
            {cevir(cevir("Sitenin açılış (index) dizini. Varsayılan"))} <code className="font-mono">public_html</code>. Laravel, Symfony gibi
            {cevir(cevir("çatılar"))} <code className="font-mono">index.php</code>'yi bir alt klasörde tutar — o klasörü buraya yazın.
          </p>
        </div>

        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("public_html içindeki alt klasör")}</label>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-stretch flex-1 min-w-0 rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden focus-within:ring-2 focus-within:ring-brand-500/30">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-slate-900 text-xs font-mono text-slate-400 dark:text-slate-500 border-r border-slate-200 dark:border-slate-700 select-none whitespace-nowrap">public_html/</span>
            <input
              list="webroot-adaylar"
              value={webRoot}
              onChange={e => setWebRoot(e.target.value.replace(/^\/+/, ''))}
              onKeyDown={e => { if (e.key === 'Enter') webRootKaydet() }}
              placeholder={cevir("(boş = public_html kökü)")}
              spellCheck={false} autoCapitalize="off" autoCorrect="off"
              className="flex-1 min-w-0 px-3 py-2 text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 outline-none"
            />
          </div>
          <button type="button" onClick={webRootKaydet}
            disabled={webRootKaydediliyor || webRoot.trim() === webRootKayitli}
            className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-50 text-sm font-medium rounded-md whitespace-nowrap">
            {webRootKaydediliyor ? 'Uygulanıyor…' : cevir("Belge Kökünü Kaydet")}
          </button>
        </div>
        <datalist id="webroot-adaylar">
          {webRootAdaylar.map(x => <option key={x} value={x} />)}
        </datalist>

        {webRootAdaylar.length > 0 && (
          <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-slate-400 dark:text-slate-500">{cevir("Algılanan klasörler:")}</span>
            {webRootAdaylar.map(x => (
              <button key={x} type="button" onClick={() => setWebRoot(x)}
                className="px-2 py-0.5 text-[11px] font-mono rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:border-brand-400 hover:text-brand-600 dark:hover:text-brand-400 transition">
                {x}
              </button>
            ))}
          </div>
        )}

        <div className="mt-3 flex items-start gap-2 px-2.5 py-2 bg-slate-50 dark:bg-slate-900/40 rounded-md">
          <span className="text-xs text-slate-400 dark:text-slate-500 mt-px">↳</span>
          <div className="text-xs text-slate-600 dark:text-slate-400 min-w-0">
            <span className="text-slate-400 dark:text-slate-500">{cevir("Etkin kök:")} </span>
            <code className="font-mono break-all text-slate-700 dark:text-slate-300">public_html{webRoot.trim() ? '/' + webRoot.trim().replace(/^\/+|\/+$/g, '') : ''}</code>
            <div className="mt-1 text-[11px] text-slate-400 dark:text-slate-500">
              {cevir(cevir("Klasör önceden var olmalı."))} <span className="font-medium text-slate-500 dark:text-slate-400">{cevir("Laravel için:")} <code className="font-mono">public</code></span> · Kaydedince nginx vhost anında yeniden render edilir.
            </div>
          </div>
        </div>
      </div>

      <div className="mb-5 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md text-xs text-amber-800 dark:text-amber-200">
        <strong>HSTS</strong> yalnızca HTTPS aktif siteler için anlamlıdır. Site HTTP-only ise tarayıcıya gönderilmez.
        {cevir(cevir("Ayarlar değiştiğinde"))} <code className="font-mono">nginx -t</code> + <code className="font-mono">reload</code> {cevir("otomatik tetiklenir — sıfır kesinti.")}
      </div>

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
                  aciklama={h.aciklama}
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
              <div className="mt-3 pl-4 border-l-2 border-slate-200 dark:border-slate-700 space-y-2">
                <div>
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">max-age (saniye)</label>
                  <select value={a.hsts_max_age} onChange={e => P('hsts_max_age', parseInt(e.target.value))}
                    className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono">
                    <option value={300}>{cevir("5 dk (test için)")}</option>
                    <option value={86400}>{cevir("1 gün")}</option>
                    <option value={604800}>1 hafta</option>
                    <option value={2592000}>{cevir("30 gün")}</option>
                    <option value={15768000}>6 ay</option>
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

          {/* Performans Önbelleği */}
          <Kart baslik={cevir("Performans Önbelleği")}>
            <SatirToggle
              etiket="Nginx FastCGI Cache"
              deger={cevirT(cevir("x-cache-status header · {0} dk önbellek süresi"), a.fastcgi_cache_dakika)}
              aciklama="WordPress/PHP sayfalarini diskte onbellege alir. POST/cookie/login/preview otomatik atlanir. WP Site Health (Page cache detected) uyarisini giderir."
              acik={a.fastcgi_cache}
              onToggle={() => P('fastcgi_cache', !a.fastcgi_cache)}
            />
            {a.fastcgi_cache && (
              <div className="mt-3 pl-4 border-l-2 border-slate-200 dark:border-slate-700">
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Cache süresi (dakika)")}</label>
                <select value={a.fastcgi_cache_dakika} onChange={e => P('fastcgi_cache_dakika', parseInt(e.target.value))}
                  className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono">
                  <option value={5}>5 dakika</option>
                  <option value={15}>15 dakika</option>
                  <option value={60}>{cevir("1 saat (önerilen)")}</option>
                  <option value={360}>6 saat</option>
                  <option value={1440}>{cevir("1 gün")}</option>
                </select>
              </div>
            )}

            <div className="mt-4 pt-4 border-t border-slate-100 dark:border-slate-800">
              <SatirToggle
                etiket="Browser Cache (statik dosyalar)"
                deger={`Cache-Control: public, immutable · expires ${a.browser_cache_gun}d`}
                aciklama={cevir("CSS/JS/PNG/JPG/WOFF gibi statik dosyalar tarayıcıda önbellekte tutulur. Tekrar ziyaretlerde sayfa çok daha hızlı yüklenir.")}
                acik={a.browser_cache}
                onToggle={() => P('browser_cache', !a.browser_cache)}
              />
              {a.browser_cache && (
                <div className="mt-3 pl-4 border-l-2 border-slate-200 dark:border-slate-700">
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Cache süresi (gün)")}</label>
                  <select value={a.browser_cache_gun} onChange={e => P('browser_cache_gun', parseInt(e.target.value))}
                    className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono">
                    <option value={1}>{cevir("1 gün")}</option>
                    <option value={7}>1 hafta</option>
                    <option value={30}>{cevir("30 gün (önerilen)")}</option>
                    <option value={90}>3 ay</option>
                    <option value={365}>{cevir("1 yıl")}</option>
                  </select>
                </div>
              )}
            </div>
          </Kart>

          {/* Ek direktifler */}
          <Kart baslik="Ek nginx Direktifleri">
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-2">
              Bu metin <code className="font-mono">server</code> {cevir("bloğunun sonuna eklenir. Örn:")} <code className="font-mono">client_max_body_size 200m;</code>
            </p>
            <textarea value={a.ek_direktifler} onChange={e => P('ek_direktifler', e.target.value)}
              rows={6}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-xs font-mono"
              placeholder="# Örn:&#10;client_max_body_size 200m;&#10;rewrite ^/eski/(.*)$ /yeni/$1 permanent;" />
          </Kart>

          <div className="flex gap-3 mt-6">
            <button onClick={kaydet} disabled={isleniyor}
              className="px-6 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
              {isleniyor ? 'Uygulanıyor…' : <span className="inline-flex items-center gap-1.5"><Ikon d={I.disket} /> Kaydet ve Uygula</span>}
            </button>
            <button onClick={yukle} disabled={isleniyor}
              className="px-4 py-2.5 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300 text-sm rounded-md">
              {cevir(cevir("Yeniden Yükle"))}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function Kart({ baslik, children }: { baslik: string; children: any }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-slate-800">{baslik}</h3>
      {children}
    </div>
  )
}

function SatirToggle({ etiket, deger, aciklama, acik, onToggle }:
  { etiket: string; deger: string; aciklama: string; acik: boolean; onToggle: () => void }) {
  return (
    <div className="flex items-start gap-3 py-2 border-b border-slate-50 last:border-0">
      <button onClick={onToggle}
        className={`flex-shrink-0 mt-0.5 relative inline-flex h-6 w-11 items-center rounded-full transition ${
          acik ? 'bg-emerald-500' : 'bg-slate-300'
        }`}>
        <span className={`inline-block h-4 w-4 transform rounded-full bg-white dark:bg-slate-800 shadow transition ${acik ? 'translate-x-6' : 'translate-x-1'}`} />
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