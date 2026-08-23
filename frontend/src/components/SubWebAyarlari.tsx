import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'

// Alt alan web-sunucu ayarları — domain paritesi: backend (php-fpm/apache/static),
// nginx fastcgi/browser cache, client_max_body, güvenlik başlıkları, ek direktifler.
type Nginx = {
  hdr_x_content_type: boolean; hdr_x_xss: boolean; hdr_referrer: boolean
  hdr_permissions: boolean; hdr_csp_upgrade: boolean; hdr_hsts: boolean
  hsts_max_age: number; hsts_subdomains: boolean; hsts_preload: boolean
  fastcgi_cache: boolean; fastcgi_cache_dakika: number
  browser_cache: boolean; browser_cache_gun: number
  client_max_body_mb: number; ek_direktifler: string
}

const kart = 'rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 md:p-5'
const etk = 'block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1'
const inp = 'w-full px-2.5 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-sm'

const backendler: { v: string; ad: string; aciklama: string }[] = [
  { v: 'php-fpm', ad: 'PHP-FPM', aciklama: 'nginx + PHP-FPM (varsayılan, en hızlı)' },
  { v: 'apache', ad: 'Apache', aciklama: 'nginx önde, Apache arkada (.htaccess desteği)' },
  { v: 'static', ad: 'Statik', aciklama: 'Yalnız statik dosya; PHP çalıştırma kapalı' },
]

const SWA_EN: Record<string, string> = {
  "Statik": "Static",
  "nginx + PHP-FPM (varsayılan, en hızlı)": "nginx + PHP-FPM (default, fastest)",
  "nginx önde, Apache arkada (.htaccess desteği)": "nginx in front, Apache behind (.htaccess support)",
  "Yalnız statik dosya; PHP çalıştırma kapalı": "Static files only; PHP execution disabled",
  "Web Sunucu (Backend)": "Web Server (Backend)",
  "Tarayıcı önbellek (gün)": "Browser cache (days)",
  "FastCGI önbellek (dk)": "FastCGI cache (min)",
  "HSTS (yalnız SSL)": "HSTS (SSL only)",
  "Ek nginx direktifleri (server bağlamı)": "Extra nginx directives (server context)",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SWA_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function SubWebAyarlari({ domainId, sid }: { domainId: string; sid: string }) {
  useTranslation() // dil re-render aboneligi
  const [n, setN] = useState<Nginx | null>(null)
  const [backend, setBackend] = useState('php-fpm')
  const [acik, setAcik] = useState(false)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [mesaj, setMesaj] = useState<{ t: 'ok' | 'hata'; m: string } | null>(null)

  useEffect(() => {
    api.get<{ backend: string; nginx: Nginx }>(`/domains/${domainId}/subdomain/${sid}/web-sunucu`)
      .then(r => { setN(r.data.nginx); setBackend(r.data.backend) })
      .catch(hataYakala(cevir("Alt alan web sunucu ayarları yüklenemedi")))
  }, [domainId, sid])

  function P<K extends keyof Nginx>(k: K, v: Nginx[K]) { setN(prev => prev ? { ...prev, [k]: v } : prev) }

  async function kaydet() {
    if (!n) return
    setKaydediliyor(true); setMesaj(null)
    try {
      await api.put(`/domains/${domainId}/subdomain/${sid}/web-sunucu`, { backend, nginx: n })
      setMesaj({ t: 'ok', m: cevir("✓ Web sunucu ayarları kaydedildi ve vhost yeniden yazıldı.") })
    } catch (e) {
      setMesaj({ t: 'hata', m: apiHata(e) })
    } finally { setKaydediliyor(false) }
  }

  if (!n) return null
  const Toggle = ({ k, l }: { k: keyof Nginx; l: string }) => (
    <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
      <input type="checkbox" checked={n[k] as boolean} onChange={e => P(k, e.target.checked as never)} className="cursor-pointer" />{l}
    </label>
  )

  return (
    <div className={`${kart} md:col-span-2`}>
      <button onClick={() => setAcik(v => !v)} className="w-full flex items-center justify-between text-left">
        <div>
          <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200">{cevir("Web Sunucu Ayarları")}</h2>
          <p className="text-xs text-slate-500 mt-0.5">{cevir("Backend, önbellek, yükleme limiti ve güvenlik başlıkları — domain gibi tam parite.")}</p>
        </div>
        <span className="flex items-center gap-2">
          <span className="text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300">{backend}</span>
          <span className="text-slate-400 text-lg">{acik ? '−' : '+'}</span>
        </span>
      </button>

      {acik && (
        <div className="mt-4 space-y-5">
          {mesaj && (
            <div className={`text-sm px-3 py-2 rounded-md ${mesaj.t === 'ok' ? 'bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'}`}>{mesaj.m}</div>
          )}

          <div>
            <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">{cevir("Web Sunucu (Backend)")}</h3>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
              {backendler.map(b => (
                <button key={b.v} onClick={() => setBackend(b.v)}
                  className={`text-left p-3 rounded-lg border transition ${backend === b.v ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 ring-1 ring-brand-400' : 'border-slate-200 dark:border-slate-700 hover:border-slate-300 dark:hover:border-slate-600'}`}>
                  <div className="text-sm font-semibold text-slate-800 dark:text-slate-200">{cevir(b.ad)}</div>
                  <div className="text-[11px] text-slate-500 mt-0.5">{cevir(b.aciklama)}</div>
                </button>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <div>
              <label className={etk}>client_max_body_size (MB)</label>
              <input type="number" value={n.client_max_body_mb} onChange={e => P('client_max_body_mb', Number(e.target.value))} className={inp} />
            </div>
            <div>
              <label className={etk}>{cevir("Tarayıcı önbellek (gün)")}</label>
              <input type="number" value={n.browser_cache_gun} onChange={e => P('browser_cache_gun', Number(e.target.value))} className={inp} disabled={!n.browser_cache} />
            </div>
            <div>
              <label className={etk}>{cevir("FastCGI önbellek (dk)")}</label>
              <input type="number" value={n.fastcgi_cache_dakika} onChange={e => P('fastcgi_cache_dakika', Number(e.target.value))} className={inp} disabled={!n.fastcgi_cache} />
            </div>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            <Toggle k="browser_cache" l={cevir("Tarayıcı önbelleği")} />
            <Toggle k="fastcgi_cache" l={cevir("FastCGI önbelleği")} />
            <Toggle k="hdr_x_content_type" l="X-Content-Type-Options" />
            <Toggle k="hdr_x_xss" l="X-XSS-Protection" />
            <Toggle k="hdr_referrer" l="Referrer-Policy" />
            <Toggle k="hdr_permissions" l="Permissions-Policy" />
            <Toggle k="hdr_csp_upgrade" l="CSP upgrade-insecure" />
            <Toggle k="hdr_hsts" l={cevir("HSTS (yalnız SSL)")} />
          </div>

          {n.hdr_hsts && (
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-3 pl-1 border-l-2 border-slate-200 dark:border-slate-700">
              <div><label className={etk}>HSTS max-age</label><input type="number" value={n.hsts_max_age} onChange={e => P('hsts_max_age', Number(e.target.value))} className={inp} /></div>
              <Toggle k="hsts_subdomains" l="includeSubDomains" />
              <Toggle k="hsts_preload" l="preload" />
            </div>
          )}

          <div>
            <label className={etk}>{cevir("Ek nginx direktifleri (server bağlamı)")}</label>
            <textarea value={n.ek_direktifler} onChange={e => P('ek_direktifler', e.target.value)} rows={3}
              className={`${inp} font-mono text-xs`} placeholder="add_header X-Frame-Options SAMEORIGIN;" />
          </div>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="px-4 py-2 rounded-md bg-slate-900 dark:bg-slate-700 text-white text-sm font-medium disabled:opacity-40">
              {kaydediliyor ? cevir("Kaydediliyor…") : cevir("Web sunucu ayarlarını kaydet")}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
