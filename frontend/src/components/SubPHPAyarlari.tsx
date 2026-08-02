import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'

// Alt alan PHP ayarları — domain PHP ayarlarının aynısı; kaydedilince alt alana
// AYRI bir PHP-FPM havuzu render edilir (bağımsız memory_limit/upload/pm.* vb.).
type Ayar = {
  memory_limit: string; max_execution_time: number; max_input_time: number
  post_max_size: string; upload_max_filesize: string; opcache_enable: boolean
  disable_functions: string; display_errors: boolean; log_errors: boolean
  allow_url_fopen: boolean; file_uploads: boolean; short_open_tag: boolean
  error_reporting: string; include_path: string; open_basedir: string
  session_save_path: string; mail_force_extra_parameters: string
  pm_strategy: string; pm_max_children: number; pm_max_requests: number
  pm_start_servers: number; pm_min_spare_servers: number; pm_max_spare_servers: number
  ek_direktifler: string; debug_mode: boolean
}

const kart = 'rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-4 md:p-5'
const etk = 'block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1'
const inp = 'w-full px-2.5 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-sm'

export default function SubPHPAyarlari({ domainId, sid }: { domainId: string; sid: string }) {
  const [a, setA] = useState<Ayar | null>(null)
  const [ozel, setOzel] = useState(false)
  const [acik, setAcik] = useState(false)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [mesaj, setMesaj] = useState<{ t: 'ok' | 'hata'; m: string } | null>(null)

  useEffect(() => {
    api.get<{ ayarlar: Ayar; ozel_havuz: boolean }>(`/domains/${domainId}/subdomain/${sid}/php-settings`)
      .then(r => { setA(r.data.ayarlar); setOzel(r.data.ozel_havuz) })
      .catch(() => {})
  }, [domainId, sid])

  function P<K extends keyof Ayar>(k: K, v: Ayar[K]) { setA(prev => prev ? { ...prev, [k]: v } : prev) }

  async function kaydet() {
    if (!a) return
    setKaydediliyor(true); setMesaj(null)
    try {
      const r = await api.put<{ ozel_havuz: boolean }>(`/domains/${domainId}/subdomain/${sid}/php-settings`, { ayarlar: a })
      setOzel(r.data.ozel_havuz)
      setMesaj({ t: 'ok', m: '✓ Ayarlar kaydedildi — bu alt alan için ayrı FPM havuzu etkin.' })
    } catch (e) {
      setMesaj({ t: 'hata', m: apiHata(e) })
    } finally { setKaydediliyor(false) }
  }

  if (!a) return null

  const num = (k: keyof Ayar) => (
    <input type="number" value={a[k] as number} onChange={e => P(k, Number(e.target.value) as never)} className={inp} />
  )
  const txt = (k: keyof Ayar) => (
    <input type="text" value={a[k] as string} onChange={e => P(k, e.target.value as never)} className={inp} />
  )
  const Toggle = ({ k, l }: { k: keyof Ayar; l: string }) => (
    <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
      <input type="checkbox" checked={a[k] as boolean} onChange={e => P(k, e.target.checked as never)} className="cursor-pointer" />
      {l}
    </label>
  )

  return (
    <div className={`${kart} md:col-span-2`}>
      <button onClick={() => setAcik(v => !v)} className="w-full flex items-center justify-between text-left">
        <div>
          <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200">PHP Ayarları</h2>
          <p className="text-xs text-slate-500 mt-0.5">Bellek, yükleme boyutu, hata gösterimi ve FPM havuz limitleri — domain gibi tam özelleştirme.</p>
        </div>
        <span className="flex items-center gap-2">
          {ozel && <span className="text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300">ayrı havuz</span>}
          <span className="text-slate-400 text-lg">{acik ? '−' : '+'}</span>
        </span>
      </button>

      {acik && (
        <div className="mt-4 space-y-5">
          {mesaj && (
            <div className={`text-sm px-3 py-2 rounded-md ${mesaj.t === 'ok' ? 'bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'}`}>{mesaj.m}</div>
          )}
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <div><label className={etk}>memory_limit</label>{txt('memory_limit')}</div>
            <div><label className={etk}>upload_max_filesize</label>{txt('upload_max_filesize')}</div>
            <div><label className={etk}>post_max_size</label>{txt('post_max_size')}</div>
            <div><label className={etk}>max_execution_time (s)</label>{num('max_execution_time')}</div>
            <div><label className={etk}>max_input_time (s)</label>{num('max_input_time')}</div>
            <div><label className={etk}>error_reporting</label>{txt('error_reporting')}</div>
          </div>

          <div>
            <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">Genel</h3>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              <Toggle k="opcache_enable" l="OPcache" />
              <Toggle k="display_errors" l="display_errors" />
              <Toggle k="log_errors" l="log_errors" />
              <Toggle k="allow_url_fopen" l="allow_url_fopen" />
              <Toggle k="file_uploads" l="file_uploads" />
              <Toggle k="short_open_tag" l="short_open_tag" />
              <Toggle k="debug_mode" l="Hata ayıklama modu" />
            </div>
          </div>

          <div>
            <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">FPM Havuzu</h3>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
              <div>
                <label className={etk}>pm stratejisi</label>
                <select value={a.pm_strategy} onChange={e => P('pm_strategy', e.target.value)} className={inp}>
                  <option value="ondemand">ondemand</option>
                  <option value="dynamic">dynamic</option>
                  <option value="static">static</option>
                </select>
              </div>
              <div><label className={etk}>pm.max_children</label>{num('pm_max_children')}</div>
              <div><label className={etk}>pm.max_requests</label>{num('pm_max_requests')}</div>
              <div><label className={etk}>pm.start_servers</label>{num('pm_start_servers')}</div>
              <div><label className={etk}>pm.min_spare</label>{num('pm_min_spare_servers')}</div>
              <div><label className={etk}>pm.max_spare</label>{num('pm_max_spare_servers')}</div>
            </div>
          </div>

          <div>
            <label className={etk}>disable_functions</label>{txt('disable_functions')}
          </div>
          <div>
            <label className={etk}>Ek direktifler (php_value[...] / php_flag[...])</label>
            <textarea value={a.ek_direktifler} onChange={e => P('ek_direktifler', e.target.value)} rows={3}
              className={`${inp} font-mono text-xs`} placeholder="php_value[max_input_vars]=20000" />
          </div>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="px-4 py-2 rounded-md bg-slate-900 dark:bg-slate-700 text-white text-sm font-medium disabled:opacity-40">
              {kaydediliyor ? 'Kaydediliyor…' : 'PHP ayarlarını kaydet'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
