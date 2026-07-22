// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Durum = {
  kurulu: boolean
  kayit_var: boolean
  app_root: string
  kullanici: string
  dizin: string
  php_surum: string
  node_surum: string
  composer_json: boolean
  git_var: boolean
  last_commit: string
  bakim: boolean
  schedule_enabled: boolean
  queue_enabled: boolean
  queue_timeout: number
  queue_max_jobs: number
  queue_connection: string
  son_deploy_durum: string
  php_binary: string
}

const SEKMELER = ['Kontrol Paneli', 'Artisan', 'Composer', 'Node.js', 'Dağıtım', 'Kuyruk'] as const
type Sekme = typeof SEKMELER[number]

const ARTISAN_KOMUTLAR = [
  'about', 'migrate --force', 'migrate:status', 'migrate:rollback --force', 'db:seed --force',
  'optimize', 'optimize:clear', 'config:cache', 'config:clear', 'cache:clear',
  'route:cache', 'route:clear', 'route:list', 'view:cache', 'view:clear',
  'storage:link', 'key:generate --force', 'queue:restart', 'queue:failed', 'schedule:list',
  'event:list', 'down', 'up',
]
const COMPOSER_KOMUTLAR = ['install', 'update', 'dump-autoload', 'validate', 'show', 'diagnose', 'require', 'remove']
const NPM_KOMUTLAR = ['install', 'ci', 'run', 'prune', 'audit', 'outdated', 'ls']

export default function DomainLaravelPage() {
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [sekme, setSekme] = useState<Sekme>('Kontrol Paneli')

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Durum>(`/domains/${id}/laravel`)
      .then(r => setD(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  function bildir(msg: string) { setBasari(msg); setTimeout(() => setBasari(null), 4000) }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: d?.kullanici ? d.kullanici.replace(/^c_/, '') : '...', href: `/abonelikler/${id}` },
        { etiket: 'Laravel Toolkit' },
      ]} />

      <div className="flex items-center gap-2.5 mb-1">
        <span className="text-2xl leading-none">🅛</span>
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Laravel Toolkit</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Laravel uygulamanızı kurun, Artisan/Composer/npm çalıştırın, dağıtın; zamanlanmış görev ve kuyruk işleyiciyi yönetin.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {yuk || !d ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : !d.kurulu && d.son_deploy_durum !== 'kuruluyor' ? (
        <KurulumSihirbazi id={id!} onKuruldu={() => { yukle(); bildir('✓ Laravel uygulaması kuruldu') }} onHata={setHata} />
      ) : d.son_deploy_durum === 'kuruluyor' ? (
        <KurulumIlerleme id={id!} onBitti={(basarili, log) => { yukle(); basarili ? bildir('✓ Laravel uygulaması kuruldu') : setHata('Kurulum başarısız oldu:\n' + log) }} />
      ) : (
        <>
          {/* Sekme çubuğu (mobilde yatay kaydırılır) */}
          <div className="flex gap-1 border-b border-slate-200 dark:border-slate-700 mb-5 overflow-x-auto">
            {SEKMELER.map(s => (
              <button key={s} onClick={() => setSekme(s)}
                className={`px-4 py-2.5 text-sm font-medium whitespace-nowrap border-b-2 transition -mb-px ${
                  sekme === s
                    ? 'border-brand-500 text-brand-600 dark:text-brand-400'
                    : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'
                }`}>{s}</button>
            ))}
          </div>

          {sekme === 'Kontrol Paneli' && <KontrolPaneli id={id!} d={d} onDegisti={yukle} onBildir={bildir} onHata={setHata} />}
          {sekme === 'Artisan' && <KomutSekmesi baslik="Artisan" id={id!} d={d} url={`/domains/${id}/laravel/artisan`}
            komutlar={ARTISAN_KOMUTLAR} onHata={setHata} />}
          {sekme === 'Composer' && <ComposerSekmesi id={id!} d={d} onHata={setHata} />}
          {sekme === 'Node.js' && <NodeSekmesi id={id!} d={d} onHata={setHata} />}
          {sekme === 'Dağıtım' && <DeploySekmesi id={id!} d={d} onHata={setHata} />}
          {sekme === 'Kuyruk' && <KuyrukSekmesi id={id!} d={d} onDegisti={yukle} onHata={setHata} />}
        </>
      )}
    </div>
  )
}

// ─────────────────────────── Kurulum Sihirbazı ───────────────────────────
function KurulumSihirbazi({ id, onKuruldu, onHata }:
  { id: string; onKuruldu: () => void; onHata: (m: string) => void }) {
  const [mode, setMode] = useState<'iskele' | 'uzak' | 'yerel'>('iskele')
  const [repoURL, setRepoURL] = useState('')
  const [branch, setBranch] = useState('main')
  const [appRoot, setAppRoot] = useState('public_html')
  const [isleniyor, setIsleniyor] = useState(false)
  const [asyncUnit, setAsyncUnit] = useState(false)
  const [seciciAcik, setSeciciAcik] = useState(false)

  const MODLAR = [
    { key: 'iskele', ikon: '✨', ad: 'Sıfırdan Laravel', ac: 'Boş Laravel iskeleti kurulur (composer create-project). Yeni proje için ideal.' },
    { key: 'uzak', ikon: '🌐', ad: 'Uzak Depo', ac: 'Git deposundan (GitHub/GitLab) klonlanır + composer install çalışır.' },
    { key: 'yerel', ikon: '📁', ad: 'Yerel Git', ac: 'Boş bir git deposu oluşturulur; kodunuzu push edip Dağıtım’dan yayınlarsınız.' },
  ] as const

  async function kur() {
    setIsleniyor(true); onHata('')
    try {
      const r = await api.post<{ async: boolean }>(`/domains/${id}/laravel/kur`, {
        mode, repo_url: repoURL, branch, app_root: appRoot,
      })
      if (r.data.async) setAsyncUnit(true)
      else onKuruldu()
    } catch (e) {
      onHata(apiHata(e, 'Kurulum başlatılamadı'))
    } finally {
      setIsleniyor(false)
    }
  }

  if (asyncUnit) return <KurulumIlerleme id={id} onBitti={(basarili, log) => {
    if (basarili) { onKuruldu() } else { onHata('Kurulum başarısız oldu:\n' + log); setAsyncUnit(false) }
  }} />

  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 sm:p-6">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">Laravel uygulaması kur</h3>
      <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">Bir kurulum yöntemi seçin. Kurulum bitince belge kökü otomatik <code className="font-mono">public</code> olarak ayarlanır.</p>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-4">
        {MODLAR.map(m => {
          const aktif = mode === m.key
          return (
            <button key={m.key} type="button" onClick={() => setMode(m.key)}
              className={`text-left p-4 border rounded-lg transition ${
                aktif ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 ring-2 ring-brand-500/20'
                      : 'border-slate-200 dark:border-slate-700 hover:border-brand-300 dark:hover:border-brand-700'}`}>
              <div className="text-lg mb-1">{m.ikon}</div>
              <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ad}</div>
              <div className="text-[11px] text-slate-500 dark:text-slate-400 mt-1 leading-snug">{m.ac}</div>
            </button>
          )
        })}
      </div>

      {mode === 'uzak' && (
        <div className="space-y-3 mb-4">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Depo URL</label>
            <input value={repoURL} onChange={e => setRepoURL(e.target.value)} spellCheck={false}
              placeholder="https://github.com/kullanici/proje.git veya git@github.com:kullanici/proje.git"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          </div>
          <div className="max-w-[220px]">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Branch</label>
            <input value={branch} onChange={e => setBranch(e.target.value)} spellCheck={false}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          </div>
        </div>
      )}

      <div className="max-w-[440px] mb-5">
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Uygulama dizini (public_html altında)</label>
        <div className="flex gap-2">
          <input value={appRoot} onChange={e => setAppRoot(e.target.value.replace(/^\/+/, ''))} spellCheck={false}
            className="flex-1 min-w-0 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          <button type="button" onClick={() => setSeciciAcik(true)}
            className="px-4 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 whitespace-nowrap">📁 Seç</button>
        </div>
        <p className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">Örn: <code className="font-mono">public_html</code> (ana site) veya <code className="font-mono">public_html/uygulama</code>. Yeni klasör için elle yazın.</p>
      </div>
      {seciciAcik && (
        <KlasorSecici id={id} baslangic={appRoot} onKapat={() => setSeciciAcik(false)}
          onSec={(y) => { setAppRoot(y); setSeciciAcik(false) }} />
      )}

      <button onClick={kur} disabled={isleniyor || (mode === 'uzak' && !repoURL.trim())}
        className="px-6 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md">
        {isleniyor ? 'Başlatılıyor…' : '🚀 Kur'}
      </button>
    </div>
  )
}

// Kurulum ilerleme (async unit poll)
function KurulumIlerleme({ id, onBitti }: { id: string; onBitti: (basarili: boolean, log: string) => void }) {
  const [log, setLog] = useState('')
  const [durum, setDurum] = useState('kuruluyor')
  const bitti = useRef(false)
  useEffect(() => {
    const t = setInterval(async () => {
      try {
        const r = await api.get<{ calisiyor: boolean; durum: string; log: string }>(`/domains/${id}/laravel/kur/durum`)
        setLog(r.data.log || ''); setDurum(r.data.durum)
        if (!r.data.calisiyor && r.data.durum !== 'kuruluyor' && !bitti.current) {
          bitti.current = true
          clearInterval(t)
          setTimeout(onBitti, 1200)
        }
      } catch { /* devam */ }
    }, 2500)
    return () => clearInterval(t)
  }, [id])
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
      <div className="flex items-center gap-2 mb-3">
        {durum === 'kuruluyor'
          ? <span className="inline-block h-2 w-2 rounded-full bg-brand-500 animate-pulse" />
          : <span className={`text-sm ${durum === 'hazir' ? 'text-emerald-500' : 'text-red-500'}`}>{durum === 'hazir' ? '✓' : '✗'}</span>}
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {durum === 'kuruluyor' ? 'Kuruluyor…' : durum === 'hazir' ? 'Kurulum tamamlandı' : 'Kurulum hata verdi'}
        </h3>
      </div>
      <CiktiKutusu cikti={log || 'Kurulum başlatılıyor…'} />
    </div>
  )
}

// ─────────────────────────── Kontrol Paneli ───────────────────────────
function KontrolPaneli({ id, d, onDegisti, onBildir, onHata }:
  { id: string; d: Durum; onDegisti: () => void; onBildir: (m: string) => void; onHata: (m: string) => void }) {
  const [env, setEnv] = useState<string | null>(null)
  const [envVar, setEnvVar] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)

  const [appRoot, setAppRoot] = useState(d.app_root || 'public_html')
  const [appKayitli, setAppKayitli] = useState(d.app_root || 'public_html')
  const [appAdaylar, setAppAdaylar] = useState<string[]>([])
  const [appKaydediliyor, setAppKaydediliyor] = useState(false)
  const [seciciAcik, setSeciciAcik] = useState(false)

  useEffect(() => {
    api.get<{ var: boolean; icerik: string }>(`/domains/${id}/laravel/env`)
      .then(r => { setEnvVar(r.data.var); setEnv(r.data.icerik) })
      .catch(() => setEnv(''))
    api.get<{ mevcut: string; adaylar: string[] }>(`/domains/${id}/laravel/app-adaylar`)
      .then(r => { setAppRoot(r.data.mevcut); setAppKayitli(r.data.mevcut); setAppAdaylar(r.data.adaylar || []) })
      .catch(() => {})
  }, [id])

  async function appRootKaydet() {
    if (appRoot.trim() === appKayitli || appKaydediliyor) return
    setAppKaydediliyor(true); onHata('')
    try {
      const r = await api.put<{ app_root: string; kurulu: boolean }>(`/domains/${id}/laravel/app-root`, { app_root: appRoot.trim() })
      setAppKayitli(r.data.app_root)
      onBildir(`✓ Uygulama dizini "${r.data.app_root}" olarak ayarlandı`)
      onDegisti() // kurulu değişmiş olabilir → sayfa yeniden yüklensin
    } catch (e) { onHata(apiHata(e, 'Uygulama dizini değiştirilemedi')) }
    finally { setAppKaydediliyor(false) }
  }

  async function envKaydet() {
    if (env === null) return
    setKaydediliyor(true); onHata('')
    try {
      await api.put(`/domains/${id}/laravel/env`, { icerik: env })
      onBildir('✓ .env kaydedildi'); setEnvVar(true)
    } catch (e) { onHata(apiHata(e, '.env kaydedilemedi')) }
    finally { setKaydediliyor(false) }
  }

  async function toggle(url: string, aktif: boolean, ad: string) {
    onHata('')
    try {
      await api.post(url, { aktif }); onBildir(`✓ ${ad} ${aktif ? 'açıldı' : 'kapatıldı'}`); onDegisti()
    } catch (e) { onHata(apiHata(e)) }
  }

  return (
    <div className="space-y-4">
      {/* Uygulama bilgisi */}
      <Kart baslik="Uygulama Bilgisi">
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2.5 text-sm">
          <Bilgi etiket="Uygulama dizini" deger={d.app_root} mono />
          <Bilgi etiket="Sistem kullanıcısı" deger={d.kullanici} mono />
          <Bilgi etiket="PHP sürümü" deger={`${d.php_surum} (${d.php_binary})`} mono />
          <Bilgi etiket="Son commit" deger={d.last_commit || (d.git_var ? '—' : 'git deposu yok')} mono />
          <Bilgi etiket="Composer.json" deger={d.composer_json ? 'var' : 'yok'} />
          <Bilgi etiket="Son dağıtım" deger={d.son_deploy_durum || '—'} />
        </dl>
      </Kart>

      {/* Uygulama Dizini — düzenlenebilir (Laravel projesi kökü) */}
      <Kart baslik="Uygulama Dizini (Laravel projesi kökü)">
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
          <code className="font-mono">artisan</code>, <code className="font-mono">composer.json</code> ve <code className="font-mono">.env</code>'in bulunduğu klasör.
          Normalde <code className="font-mono">public_html</code>; uygulamanız bir alt klasördeyse buradan değiştirebilirsiniz.
        </p>
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Ev dizinine göre yol</label>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-stretch flex-1 min-w-0 rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden focus-within:ring-2 focus-within:ring-brand-500/30">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-slate-900 text-xs font-mono text-slate-400 dark:text-slate-500 border-r border-slate-200 dark:border-slate-700 select-none whitespace-nowrap">/home/{d.kullanici}/</span>
            <input list="lt-app-adaylar" value={appRoot}
              onChange={e => setAppRoot(e.target.value.replace(/^\/+/, ''))}
              onKeyDown={e => { if (e.key === 'Enter') appRootKaydet() }}
              spellCheck={false} autoCapitalize="off" autoCorrect="off"
              className="flex-1 min-w-0 px-3 py-2 text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 outline-none" />
          </div>
          <button type="button" onClick={() => setSeciciAcik(true)}
            className="px-4 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 whitespace-nowrap">
            📁 Seç
          </button>
          <button type="button" onClick={appRootKaydet}
            disabled={appKaydediliyor || appRoot.trim() === appKayitli}
            className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md whitespace-nowrap">
            {appKaydediliyor ? 'Uygulanıyor…' : 'Kaydet'}
          </button>
        </div>
        {seciciAcik && (
          <KlasorSecici id={id} baslangic={appRoot} onKapat={() => setSeciciAcik(false)}
            onSec={(y) => { setAppRoot(y); setSeciciAcik(false) }} />
        )}
        <datalist id="lt-app-adaylar">
          {appAdaylar.map(x => <option key={x} value={x} />)}
        </datalist>
        {appAdaylar.length > 0 && (
          <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-slate-400 dark:text-slate-500">Algılanan Laravel kökleri:</span>
            {appAdaylar.map(x => (
              <button key={x} type="button" onClick={() => setAppRoot(x)}
                className="px-2 py-0.5 text-[11px] font-mono rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:border-brand-400 hover:text-brand-600 dark:hover:text-brand-400 transition">{x}</button>
            ))}
          </div>
        )}
        <p className="mt-2 text-[11px] text-slate-400 dark:text-slate-500">Değiştirince belge kökü de otomatik <code className="font-mono">…/public</code>'e taşınır. Yol <code className="font-mono">public_html</code> içinde olmalıdır.</p>
      </Kart>

      {/* Ayarlar */}
      <Kart baslik="Ayarlar">
        <div className="space-y-3">
          <SatirToggle etiket="Bakım Modu" aciklama="Site geçici olarak 503 döner (php artisan down/up)."
            acik={d.bakim} onToggle={() => toggle(`/domains/${id}/laravel/bakim`, !d.bakim, 'Bakım modu')} />
          <div className="border-t border-slate-100 dark:border-slate-800" />
          <SatirToggle etiket="Zamanlanmış Görevler" aciklama="Dakikada bir 'schedule:run' (cron). Laravel Scheduler için gerekli."
            acik={d.schedule_enabled} onToggle={() => toggle(`/domains/${id}/laravel/schedule`, !d.schedule_enabled, 'Zamanlanmış görev')} />
        </div>
      </Kart>

      {/* .env editörü */}
      <Kart baslik="Ortam Değişkenleri (.env)">
        {env === null ? (
          <div className="text-sm text-slate-400 dark:text-slate-500 py-4">Yükleniyor…</div>
        ) : (
          <>
            {!envVar && <div className="mb-2 text-xs text-amber-600 dark:text-amber-400">⚠ .env henüz yok — kaydedince oluşturulur.</div>}
            <textarea value={env} onChange={e => setEnv(e.target.value)} spellCheck={false} rows={14}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-xs font-mono bg-slate-50 dark:bg-slate-900 text-slate-900 dark:text-slate-100 leading-relaxed"
              placeholder="APP_NAME=Laravel&#10;APP_ENV=production&#10;APP_KEY=..." />
            <div className="mt-3">
              <button onClick={envKaydet} disabled={kaydediliyor}
                className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md">
                {kaydediliyor ? 'Kaydediliyor…' : '💾 .env Kaydet'}
              </button>
            </div>
          </>
        )}
      </Kart>
    </div>
  )
}

// ─────────────────────────── Komut sekmeleri ───────────────────────────
function useRunner(url: string, onHata: (m: string) => void) {
  const [cik, setCik] = useState('')
  const [calisan, setCalisan] = useState(false)
  const [sonOk, setSonOk] = useState<boolean | null>(null)
  async function calistir(body: any, etiket: string) {
    setCalisan(true); setSonOk(null); onHata('')
    setCik(prev => `${prev}${prev ? '\n\n' : ''}$ ${etiket}\n(çalışıyor…)\n`)
    try {
      const r = await api.post<{ ok: boolean; cikti: string }>(url, body)
      setSonOk(r.data.ok)
      setCik(prev => prev.replace(/\(çalışıyor…\)\n$/, '') + (r.data.cikti || '(çıktı yok)') + `\n[${r.data.ok ? '✓ başarılı' : '✗ hata'}]\n`)
    } catch (e) {
      setSonOk(false); onHata(apiHata(e))
      setCik(prev => prev.replace(/\(çalışıyor…\)\n$/, '') + '[✗ istek hatası]\n')
    } finally { setCalisan(false) }
  }
  return { cik, setCik, calisan, sonOk, calistir }
}

function KomutSekmesi({ baslik, id, d, url, komutlar, onHata }:
  { baslik: string; id: string; d: Durum; url: string; komutlar: string[]; onHata: (m: string) => void }) {
  const [komut, setKomut] = useState(komutlar[0])
  const { cik, calisan, calistir } = useRunner(url, onHata)
  return (
    <div className="space-y-3">
      <Kart baslik={`${baslik} komutu çalıştır`}>
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
          <code className="font-mono">{d.kullanici}</code> olarak <code className="font-mono">{d.dizin}</code> dizininde çalışır.
        </p>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-stretch flex-1 min-w-0 rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-slate-900 text-xs font-mono text-slate-400 border-r border-slate-200 dark:border-slate-700 select-none">php artisan</span>
            <select value={komut} onChange={e => setKomut(e.target.value)}
              className="flex-1 min-w-0 px-3 py-2 text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 outline-none">
              {komutlar.map(k => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          <button onClick={() => calistir({ komut }, `artisan ${komut}`)} disabled={calisan}
            className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md whitespace-nowrap">
            {calisan ? 'Çalışıyor…' : '▶ Çalıştır'}
          </button>
        </div>
      </Kart>
      {cik && <CiktiKutusu cikti={cik} />}
    </div>
  )
}

function ComposerSekmesi({ id, d, onHata }: { id: string; d: Durum; onHata: (m: string) => void }) {
  const [komut, setKomut] = useState('install')
  const [paket, setPaket] = useState('')
  const { cik, calisan, calistir } = useRunner(`/domains/${id}/laravel/composer`, onHata)
  const paketGerek = komut === 'require' || komut === 'remove'
  return (
    <div className="space-y-3">
      <Kart baslik="Composer komutu çalıştır">
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
          <code className="font-mono">{d.kullanici}</code> olarak çalışır. install/update scriptli çalışır (Laravel package:discover).
        </p>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-stretch rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden sm:w-[220px]">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-slate-900 text-xs font-mono text-slate-400 border-r border-slate-200 dark:border-slate-700 select-none">composer</span>
            <select value={komut} onChange={e => setKomut(e.target.value)}
              className="flex-1 min-w-0 px-3 py-2 text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 outline-none">
              {COMPOSER_KOMUTLAR.map(k => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          {paketGerek && (
            <input value={paket} onChange={e => setPaket(e.target.value)} spellCheck={false} placeholder="vendor/paket:^1.0"
              className="flex-1 min-w-0 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          )}
          <button onClick={() => calistir({ komut, paket }, `composer ${komut}${paketGerek ? ' ' + paket : ''}`)}
            disabled={calisan || (paketGerek && !paket.trim())}
            className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md whitespace-nowrap">
            {calisan ? 'Çalışıyor…' : '▶ Çalıştır'}
          </button>
        </div>
      </Kart>
      {cik && <CiktiKutusu cikti={cik} />}
    </div>
  )
}

function NodeSekmesi({ id, d, onHata }: { id: string; d: Durum; onHata: (m: string) => void }) {
  const [surumler, setSurumler] = useState<string[]>([])
  const [nodeSurum, setNodeSurum] = useState('')
  const [komut, setKomut] = useState('install')
  const [script, setScript] = useState('build')
  const [ignoreScripts, setIgnoreScripts] = useState(false)
  const { cik, calisan, calistir } = useRunner(`/domains/${id}/laravel/npm`, onHata)
  useEffect(() => {
    api.get<{ surumler: string[] }>(`/domains/${id}/laravel/node`).then(r => {
      setSurumler(r.data.surumler || []); if (r.data.surumler?.length) setNodeSurum(r.data.surumler[0])
    }).catch(() => {})
  }, [id])
  const runGerek = komut === 'run'
  return (
    <div className="space-y-3">
      <Kart baslik="npm komutu çalıştır">
        {surumler.length === 0
          ? <div className="text-xs text-amber-600 dark:text-amber-400 mb-3">⚠ Sunucuda Node.js kurulu değil.</div>
          : <p className="text-xs text-slate-500 dark:text-slate-500 mb-3"><code className="font-mono">{d.kullanici}</code> olarak çalışır.</p>}
        <div className="flex flex-col sm:flex-row sm:flex-wrap gap-2">
          <select value={nodeSurum} onChange={e => setNodeSurum(e.target.value)}
            className="px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100">
            {surumler.map(s => <option key={s} value={s}>{s === 'sistem' ? 'sistem node' : 'Node ' + s}</option>)}
          </select>
          <div className="flex items-stretch rounded-md border border-slate-300 dark:border-slate-600 overflow-hidden">
            <span className="px-2.5 flex items-center bg-slate-50 dark:bg-slate-900 text-xs font-mono text-slate-400 border-r border-slate-200 dark:border-slate-700 select-none">npm</span>
            <select value={komut} onChange={e => setKomut(e.target.value)}
              className="px-3 py-2 text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 outline-none">
              {NPM_KOMUTLAR.map(k => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          {runGerek && (
            <input value={script} onChange={e => setScript(e.target.value)} spellCheck={false} placeholder="build"
              className="px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 w-[140px]" />
          )}
          <button onClick={() => calistir({ komut, script, node_surum: nodeSurum, ignore_scripts: ignoreScripts }, `npm ${komut}${runGerek ? ' ' + script : ''}`)}
            disabled={calisan || surumler.length === 0}
            className="px-5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-brand-600 dark:hover:bg-brand-500 text-white disabled:opacity-50 text-sm font-medium rounded-md whitespace-nowrap">
            {calisan ? 'Çalışıyor…' : '▶ Çalıştır'}
          </button>
        </div>
        <label className="flex items-center gap-2 mt-3 text-xs text-slate-500 dark:text-slate-400 cursor-pointer">
          <input type="checkbox" checked={ignoreScripts} onChange={e => setIgnoreScripts(e.target.checked)} />
          postinstall scriptlerini atla (--ignore-scripts)
        </label>
      </Kart>
      {cik && <CiktiKutusu cikti={cik} />}
    </div>
  )
}

// ─────────────────────────── Dağıtım ───────────────────────────
function DeploySekmesi({ id, d, onHata }: { id: string; d: Durum; onHata: (m: string) => void }) {
  const [migrate, setMigrate] = useState(true)
  const [npmBuild, setNpmBuild] = useState(false)
  const [nodeSurum, setNodeSurum] = useState('')
  const [surumler, setSurumler] = useState<string[]>([])
  const [log, setLog] = useState('')
  const [calisiyor, setCalisiyor] = useState(false)
  const [durum, setDurum] = useState(d.son_deploy_durum || '')
  const poll = useRef<any>(null)
  useEffect(() => {
    api.get<{ surumler: string[] }>(`/domains/${id}/laravel/node`).then(r => {
      setSurumler(r.data.surumler || []); if (r.data.surumler?.length) setNodeSurum(r.data.surumler[0])
    }).catch(() => {})
    return () => { if (poll.current) clearInterval(poll.current) }
  }, [id])

  async function deploy() {
    onHata(''); setLog(''); setCalisiyor(true); setDurum('calisiyor')
    try {
      await api.post(`/domains/${id}/laravel/deploy`, { migrate, npm_build: npmBuild, node_surum: nodeSurum })
      poll.current = setInterval(async () => {
        const r = await api.get<{ calisiyor: boolean; durum: string; log: string }>(`/domains/${id}/laravel/deploy/durum`)
        setLog(r.data.log || ''); setDurum(r.data.durum)
        if (!r.data.calisiyor) { clearInterval(poll.current); setCalisiyor(false) }
      }, 2500)
    } catch (e) { onHata(apiHata(e)); setCalisiyor(false) }
  }

  const ADIMLAR = [
    'Bakım moduna al (artisan down)', 'Git’ten kodu çek (git pull)', 'Composer bağımlılıkları (--no-dev)',
    ...(npmBuild ? ['npm ci + build'] : []), ...(migrate ? ['Veritabanı migrasyonları (--force)'] : []),
    'Cache (config + route)', 'Bakım modundan çıkar (artisan up)',
  ]

  return (
    <div className="space-y-4">
      <Kart baslik="Dağıtım">
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">Bakım modu sarmalıyla yayınlar. Sekmeyi kapatsanız bile arka planda tamamlanır.</p>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
          <div>
            <div className="text-xs font-medium text-slate-600 dark:text-slate-400 mb-2">Dağıtım adımları</div>
            <ol className="space-y-1.5">
              {ADIMLAR.map((a, i) => (
                <li key={i} className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
                  <span className="flex-shrink-0 h-5 w-5 rounded-full bg-slate-100 dark:bg-slate-700 text-[11px] flex items-center justify-center text-slate-500 dark:text-slate-400">{i + 1}</span>
                  {a}
                </li>
              ))}
            </ol>
          </div>
          <div className="space-y-3">
            <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
              <input type="checkbox" checked={migrate} onChange={e => setMigrate(e.target.checked)} />
              Veritabanı migrasyonlarını çalıştır (--force)
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
              <input type="checkbox" checked={npmBuild} onChange={e => setNpmBuild(e.target.checked)} />
              npm ci + build (frontend derle)
            </label>
            {npmBuild && surumler.length > 0 && (
              <select value={nodeSurum} onChange={e => setNodeSurum(e.target.value)}
                className="px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100">
                {surumler.map(s => <option key={s} value={s}>{s === 'sistem' ? 'sistem node' : 'Node ' + s}</option>)}
              </select>
            )}
            <button onClick={deploy} disabled={calisiyor}
              className="px-6 py-2.5 bg-emerald-600 hover:bg-emerald-500 text-white disabled:opacity-50 text-sm font-medium rounded-md">
              {calisiyor ? 'Dağıtılıyor…' : '🚀 Dağıt'}
            </button>
            {durum && durum !== 'calisiyor' && (
              <div className={`text-xs font-medium ${durum === 'basarili' ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}`}>
                Son dağıtım: {durum}
              </div>
            )}
          </div>
        </div>
      </Kart>
      {log && <CiktiKutusu cikti={log} />}
    </div>
  )
}

// ─────────────────────────── Kuyruk ───────────────────────────
function KuyrukSekmesi({ id, d, onDegisti, onHata }:
  { id: string; d: Durum; onDegisti: () => void; onHata: (m: string) => void }) {
  const [timeout, setTimeoutV] = useState(d.queue_timeout || 60)
  const [maxJobs, setMaxJobs] = useState(d.queue_max_jobs || 1000)
  const [conn, setConn] = useState(d.queue_connection || 'database')
  const [isleniyor, setIsleniyor] = useState(false)
  const [durum, setDurum] = useState<{ active_state: string; sub_state: string; restarts: string } | null>(null)

  function durumYukle() {
    api.get<any>(`/domains/${id}/laravel/queue/durum`).then(r => setDurum(r.data)).catch(() => {})
  }
  useEffect(() => { if (d.queue_enabled) durumYukle() }, [id, d.queue_enabled])

  async function ayarla(aktif: boolean) {
    setIsleniyor(true); onHata('')
    try {
      const r = await api.post<{ saglikli?: boolean; uyari?: string }>(`/domains/${id}/laravel/queue`,
        { aktif, timeout, max_jobs: maxJobs, connection: conn })
      if (aktif && r.data.saglikli === false && r.data.uyari) onHata(r.data.uyari)
      onDegisti(); setTimeout(durumYukle, 500)
    } catch (e) { onHata(apiHata(e)) }
    finally { setIsleniyor(false) }
  }

  return (
    <div className="space-y-4">
      <Kart baslik="Kuyruk İşleyici (queue:work)">
        <SatirToggle etiket="Kuyruk İşleyici" aciklama="Kalıcı systemd servisi olarak 'php artisan queue:work' çalıştırır."
          acik={d.queue_enabled} onToggle={() => ayarla(!d.queue_enabled)} />
        {isleniyor && <div className="text-xs text-slate-400 dark:text-slate-500 mt-2">Uygulanıyor…</div>}

        <div className="mt-4 grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Zaman aşımı (sn)</label>
            <input type="number" min={5} max={600} value={timeout} onChange={e => setTimeoutV(+e.target.value)}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Max iş (yeniden başlat)</label>
            <input type="number" min={10} value={maxJobs} onChange={e => setMaxJobs(+e.target.value)}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Bağlantı</label>
            <input value={conn} onChange={e => setConn(e.target.value)} spellCheck={false}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100" />
          </div>
        </div>
        {d.queue_enabled && (
          <div className="mt-3 flex flex-wrap gap-2 items-center">
            <button onClick={() => ayarla(true)} disabled={isleniyor}
              className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded-md text-xs hover:bg-slate-50 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300">
              Ayarları uygula + yeniden başlat
            </button>
            {durum && (
              <span className={`text-xs px-2 py-1 rounded font-mono ${durum.active_state === 'active' ? 'bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-400'}`}>
                {durum.active_state}/{durum.sub_state} · {durum.restarts} yeniden başlatma
              </span>
            )}
            <button onClick={durumYukle} className="text-xs text-brand-600 dark:text-brand-400 hover:underline">↻ durum</button>
          </div>
        )}
      </Kart>
    </div>
  )
}

// ─────────────────────────── Ortak bileşenler ───────────────────────────
function Kart({ baslik, children }: { baslik: string; children: any }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-slate-800">{baslik}</h3>
      {children}
    </div>
  )
}

function Bilgi({ etiket, deger, mono }: { etiket: string; deger: string; mono?: boolean }) {
  return (
    <div className="flex justify-between gap-3 border-b border-slate-50 dark:border-slate-800/50 py-1">
      <dt className="text-slate-500 dark:text-slate-500">{etiket}</dt>
      <dd className={`text-slate-800 dark:text-slate-200 text-right truncate ${mono ? 'font-mono text-xs' : ''}`}>{deger}</dd>
    </div>
  )
}

function SatirToggle({ etiket, aciklama, acik, onToggle }:
  { etiket: string; aciklama: string; acik: boolean; onToggle: () => void }) {
  return (
    <div className="flex items-start gap-3 py-1">
      <button onClick={onToggle}
        className={`flex-shrink-0 mt-0.5 relative inline-flex h-6 w-11 items-center rounded-full transition ${acik ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`}>
        <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition ${acik ? 'translate-x-6' : 'translate-x-1'}`} />
      </button>
      <div className="flex-1 min-w-0">
        <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{etiket}</div>
        <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">{aciklama}</div>
      </div>
    </div>
  )
}

// KlasorSecici: dosya yöneticisi endpoint'iyle (GET /files?yol=) public_html altında
// klasör gezip seçtiren modal. Yalnız klasörleri gösterir; public_html üstüne çıkamaz.
function KlasorSecici({ id, baslangic, onSec, onKapat }:
  { id: string; baslangic: string; onSec: (yol: string) => void; onKapat: () => void }) {
  const kok = 'public_html'
  const bas = (baslangic || '').replace(/^\/+/, '')
  const [yol, setYol] = useState(bas.startsWith(kok) ? bas : kok)
  const [klasorler, setKlasorler] = useState<{ adi: string; yol: string }[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState('')

  useEffect(() => {
    setYuk(true); setHata('')
    api.get<{ icerik: { adi: string; yol: string; tip: string }[] }>(`/domains/${id}/files`, { params: { yol } })
      .then(r => setKlasorler((r.data.icerik || []).filter(e => e.tip === 'klasor')
        .map(e => ({ adi: e.adi, yol: e.yol.replace(/^\/+/, '') }))))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }, [id, yol])

  const kokte = yol === kok
  function yukari() {
    if (kokte) return
    const parts = yol.split('/').filter(Boolean)
    parts.pop()
    setYol(parts.join('/') || kok)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onKapat}>
      <div className="w-full max-w-lg bg-white dark:bg-slate-800 rounded-2xl shadow-xl border border-slate-200 dark:border-slate-700 flex flex-col max-h-[80vh]" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-3 border-b border-slate-100 dark:border-slate-700">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">📁 Klasör seç</h3>
          <button onClick={onKapat} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-xl leading-none">×</button>
        </div>
        <div className="px-5 py-2 border-b border-slate-100 dark:border-slate-800 flex items-center gap-2 text-xs">
          <button onClick={yukari} disabled={kokte}
            className="px-2 py-1 rounded border border-slate-300 dark:border-slate-600 disabled:opacity-40 hover:bg-slate-50 dark:hover:bg-slate-700 text-slate-600 dark:text-slate-300 whitespace-nowrap">↑ Üst</button>
          <code className="font-mono text-slate-500 dark:text-slate-400 truncate">/{yol}</code>
        </div>
        <div className="flex-1 overflow-y-auto px-2 py-2 min-h-[160px]">
          {yuk ? <div className="text-center text-sm text-slate-400 dark:text-slate-500 py-8">Yükleniyor…</div>
            : hata ? <div className="text-sm text-red-600 dark:text-red-400 px-3 py-2 whitespace-pre-wrap">{hata}</div>
              : klasorler.length === 0 ? <div className="text-center text-sm text-slate-400 dark:text-slate-500 py-8">Bu dizinde alt klasör yok</div>
                : klasorler.map(k => (
                  <button key={k.yol} onClick={() => setYol(k.yol)}
                    className="w-full flex items-center gap-2 px-3 py-2 rounded-md hover:bg-slate-50 dark:hover:bg-slate-700/50 text-left text-sm text-slate-700 dark:text-slate-200">
                    <span>📁</span><span className="font-mono truncate">{k.adi}</span>
                    <span className="ml-auto text-slate-300 dark:text-slate-600">›</span>
                  </button>
                ))}
        </div>
        <div className="px-5 py-3 border-t border-slate-100 dark:border-slate-700 flex items-center justify-between gap-3">
          <div className="text-xs text-slate-500 dark:text-slate-400 truncate min-w-0">Seçilen: <code className="font-mono text-slate-700 dark:text-slate-300">{yol}</code></div>
          <div className="flex gap-2 flex-shrink-0">
            <button onClick={onKapat} className="px-3 py-1.5 text-sm border border-slate-300 dark:border-slate-600 rounded-md hover:bg-slate-50 dark:hover:bg-slate-700 text-slate-600 dark:text-slate-300">İptal</button>
            <button onClick={() => onSec(yol)} className="px-4 py-1.5 text-sm bg-slate-900 dark:bg-brand-600 hover:bg-slate-800 dark:hover:bg-brand-500 text-white rounded-md whitespace-nowrap">Bu klasörü seç</button>
          </div>
        </div>
      </div>
    </div>
  )
}

function CiktiKutusu({ cikti }: { cikti: string }) {
  const ref = useRef<HTMLPreElement>(null)
  useEffect(() => { if (ref.current) ref.current.scrollTop = ref.current.scrollHeight }, [cikti])
  return (
    <div className="bg-slate-900 rounded-2xl p-4 border border-slate-700">
      <pre ref={ref} className="text-xs font-mono text-slate-100 whitespace-pre-wrap break-all max-h-96 overflow-y-auto">{cikti}</pre>
    </div>
  )
}
