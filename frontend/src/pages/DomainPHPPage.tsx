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

type Surum = { surum: string; pool_dir: string; sock_dir: string; service: string; aciklama: string }

type Ayarlar = {
  memory_limit: string; max_execution_time: number; max_input_time: number
  post_max_size: string; upload_max_filesize: string; opcache_enable: boolean
  disable_functions: string
  display_errors: boolean; log_errors: boolean; allow_url_fopen: boolean
  file_uploads: boolean; short_open_tag: boolean
  error_reporting: string; include_path: string; open_basedir: string
  session_save_path: string; mail_force_extra_parameters: string
  pm_strategy: string; pm_max_children: number; pm_max_requests: number
  pm_start_servers: number; pm_min_spare_servers: number; pm_max_spare_servers: number
  ek_direktifler: string
  debug_mode: boolean
}

type Yanit = {
  alan_adi: string; sk: string; php_surum: string
  ayarlar: Ayarlar; surumler: Surum[]
  moduller?: string[]
}

const PMS = [
  { v: 'ondemand', t: 'ondemand (önerilen — boşken sıfır işlemci)' },
  { v: 'dynamic',  t: 'dynamic (yüke göre)' },
  { v: 'static',   t: 'static (sabit havuz)' },
]


const PHP_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "static (sabit havuz)": "static (fixed pool)",
  "Kaydedildi.": "Saved.",
  "Debug log temizlenemedi": "Failed to clear debug log",
  "Sistem kullanıcısı:": "System user:",
  "Burada belirlediğiniz ayarlar PHP-FPM havuzuna (": "The settings you define here are written to the PHP-FPM pool (",
  ") yazılır. Web sitesindeki ": ") . The website's ",
  "Kaydet ile uygula": "Apply with Save",
  "PHP'nin erişebileceği dizinler (boş = sınır yok). : ile ayır": "Directories PHP can access (empty = no limit). Separate with :",
  "örn: /home/kullanici/:/tmp/": "e.g: /home/user/:/tmp/",
  "PHP Debug Modu": "PHP Debug Mode",
  "Debug modunu kapat": "Turn off debug mode",
  "Debug modunu ac": "Turn on debug mode",
  "Debug modu": "Debug mode",
  "Acik": "On",
  "Kapali": "Off",
  "Acikken PHP hatalarini ekrana yazdirir ve olumcul (fatal) hatalari guvenilir sekilde yakalayip": "When on, it prints PHP errors to the screen and reliably catches fatal errors, saving them to",
  "'a kaydeder. Uygulama kendi ": ". Even if the application calls its own ",
  "'ini cagirsa bile fatal hatalar yine yakalanir.": ", fatal errors are still caught.",
  "⚠️ Debug modu acikken ": "⚠️ When debug mode is on, ",
  " ve ": " and ",
  " zorlanir; hata detaylari ziyaretcilere gorunebilir. Yalnizca sorun giderirken acin, canli sitede ": " are forced; error details may be visible to visitors. Turn it on only while troubleshooting, and on a live site ",
  "kapatin": "turn it off",
  ". Degisiklik ": ". The change ",
  "'ten sonra uygulanir.": " is applied afterwards.",
  "Son Hatalar (Debug Log)": "Recent Errors (Debug Log)",
  "En yeni fatal hatalar ustte. Kaynak: ": "Newest fatal errors on top. Source: ",
  " (son 200 satir).": " (last 200 lines).",
  "Temizle": "Clear",
  "Yukleniyor…": "Loading…",
  "Henuz kayitli fatal hata yok. Bir hata olusursa burada gorunur.": "No fatal errors recorded yet. If an error occurs it will appear here.",
  "Debug modu kapali. Fatal hatalarin kaydedilmesi icin yukaridan debug modunu acip kaydedin.": "Debug mode is off. To record fatal errors, turn on debug mode above and save.",
  "PHP-FPM Havuzu": "PHP-FPM Pool",
  "Process manager stratejisi": "Process manager strategy",
  "için yüklü": "has",
  "modül. Modüller sunucu seviyesindedir — tek bir domain için ayrı kapatılamaz, server-wide yönetilir.": "modules installed. Modules are server-level — they cannot be disabled for a single domain and are managed server-wide.",
  "Bloke": "Blocked",
  "Aktif": "Active",
  "'a yazılır.": " is written.",
  "(engellenmemiş),": "(not blocked),",
  "php.ini sözdizimini kullanarak ek parametreler tanımlayın. Örn: ": "Define additional parameters using php.ini syntax. E.g: ",
  "Kaydet ve Uygula": "Save and Apply",
  "<? ?> kısa tag desteği": "<? ?> short tag support",
  "Aynı anda en çok kaç PHP worker": "Max concurrent PHP workers",
  "Açık = aktif": "On = active",
  "Ağ erişimi (riskli)": "Network access (risky)",
  "Başlangıçta açılacak worker sayısı (dynamic için)": "Number of workers to start initially (for dynamic)",
  "Bekleyen maksimum worker sayısı": "Max idle workers",
  "Bekleyen minimum worker sayısı": "Min idle workers",
  "Bir scriptin ayırabileceği maksimum bellek (byte). Örn: 256M.": "Max memory a script can allocate (bytes). E.g: 256M.",
  "Devre dışı bırakılacak PHP fonksiyonları (virgülle ayrılmış)": "PHP functions to disable (comma-separated)",
  "Dosya çalıştırma": "File execution",
  "Ek Yapılandırma Direktifleri": "Additional Configuration Directives",
  "HTTP dosya yükleme": "HTTP file upload",
  "HTTP/FTP üzerinden dosya açma": "Open files over HTTP/FTP",
  "Hata loglamayı aç": "Enable error logging",
  "Hata raporlama seviyesi (örn: E_ALL & ~E_DEPRECATED)": "Error reporting level (e.g: E_ALL & ~E_DEPRECATED)",
  "Hataları çıktıya yazdır (canlıda kapalı tut)": "Print errors to output (keep off in production)",
  "Karışık": "Mixed",
  "Kurulu PHP Modülleri": "Installed PHP Modules",
  "Maks. çalışma süresi (saniye)": "Max execution time (seconds)",
  "Manuel düzenle (ham disable_functions)": "Edit manually (raw disable_functions)",
  "Modül yükleme": "Module loading",
  "OPcache opcode cache (önerilen: AÇIK)": "OPcache opcode cache (recommended: ON)",
  "PHP Ayarları": "PHP Settings",
  "POST verisi maksimum büyüklük. upload_max'tan büyük olmalı.": "Max POST data size. Must be larger than upload_max.",
  "POST/GET verisini ayrıştırma süresi (saniye)": "POST/GET data parsing time (seconds)",
  "Performans ve Güvenlik": "Performance and Security",
  "Script include dizinleri (Linux: : ile ayır)": "Script include directories (Linux: separate with :)",
  "Session dosyaları dizini (boş = /home/{sk}/tmp)": "Session files directory (empty = /home/{sk}/tmp)",
  "Shell yürütme": "Shell execution",
  "Sistem keşfi": "System discovery",
  "Tehlikeli Fonksiyonları Devre Dışı Bırak": "Disable Dangerous Functions",
  "Tek dosya yükleme limiti": "Single file upload limit",
  "Tümünü aç (etkin yap)": "Enable all",
  "Tümünü kapat": "Disable all",
  "Tümünü kapat (engelle)": "Disable all (block)",
  "Virgülle ayrılmış fonksiyon adları.": "Comma-separated function names.",
  "Worker'ı kaç istek sonra yeniden başlat (memory leak önler)": "Restart worker after N requests (prevents memory leak)",
  "dynamic (yüke göre)": "dynamic (load-based)",
  "kapalı = bloke": "off = blocked",
  "mail() fonksiyonu için ek parametreler": "Additional parameters for the mail() function",
  "ondemand (önerilen — boşken sıfır işlemci)": "ondemand (recommended — zero processes when idle)",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PHP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainPHPPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const [yanit, setYanit] = useState<Yanit | null>(null)
  const [secili, setSurum] = useState<string>('')
  const [a, setA] = useState<Ayarlar | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [dlog, setDlog] = useState<string[]>([])
  const [dlogYuk, setDlogYuk] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Yanit>(`/domains/${id}/php-settings`)
      .then(r => { setYanit(r.data); setSurum(r.data.php_surum); setA(r.data.ayarlar); debugLogYukle() })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function kaydet() {
    if (!a) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const { data } = await api.put(`/domains/${id}/php-settings`, { php_surum: secili, ayarlar: a })
      setBasari(`✓ ${cevir("Kaydedildi.")} PHP ${data.php_surum}, socket: ${data.socket}`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, cevir("Kaydetme başarısız")))
    } finally {
      setIsleniyor(false)
    }
  }

  function P<K extends keyof Ayarlar>(k: K, v: Ayarlar[K]) {
    if (!a) return; setA({ ...a, [k]: v })
  }

  async function debugLogYukle() {
    if (!id) return
    setDlogYuk(true)
    try {
      const { data } = await api.get<{ satirlar: string[] }>(`/domains/${id}/php/debug-log`)
      setDlog(data.satirlar || [])
    } catch {
      setDlog([])
    } finally {
      setDlogYuk(false)
    }
  }

  async function debugLogTemizle() {
    if (!id) return
    setDlogYuk(true); setHata(null)
    try {
      await api.delete(`/domains/${id}/php/debug-log`)
      setDlog([])
    } catch (e) {
      setHata(apiHata(e, cevir("Debug log temizlenemedi")))
    } finally {
      setDlogYuk(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: yanit?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("PHP Ayarları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("PHP Ayarları")}</h1>
      {yanit && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{yanit.alan_adi}</Link>
        {' · '}{cevir("Sistem kullanıcısı:")}{' '}<code className="font-mono">{yanit.sk}</code>
      </p>}

      <div className="mb-5 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md text-xs text-amber-800 dark:text-amber-200">
        {cevir("Burada belirlediğiniz ayarlar PHP-FPM havuzuna (")}<code className="font-mono">php_admin_value/flag</code>{cevir(") yazılır. Web sitesindeki ")}<code className="font-mono">.htaccess</code>, <code className="font-mono">.user.ini</code> {cevir("bunları override edebilir.")}
        {cevir(cevir("Kaydedince PHP-FPM otomatik yeniden başlatılır — site indirilmez."))}
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {yuk || !a || !yanit ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> : (
        <>
          {/* PHP Sürümü — kompakt segmented pill */}
          <Kart baslik={cevir("PHP Sürümü")}>
            <div className="flex flex-wrap items-center gap-2">
              <div className="inline-flex rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 p-1">
                {yanit.surumler.map(s => {
                  const sec = secili === s.surum
                  const akt = yanit.php_surum === s.surum
                  return (
                    <button key={s.surum} onClick={() => setSurum(s.surum)}
                      className={`relative inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-mono transition ${
                        sec
                          ? 'bg-white dark:bg-slate-800 shadow-sm text-slate-900 dark:text-slate-100 ring-1 ring-brand-300'
                          : 'text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 dark:text-slate-100 hover:bg-white dark:bg-slate-800/60'
                      }`}>
                      <span className="font-semibold">PHP {s.surum}</span>
                      {akt && <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" title={cevir("Aktif")} />}
                    </button>
                  )
                })}
              </div>
              {(() => {
                const s = yanit.surumler.find(x => x.surum === secili)
                if (!s) return null
                const akt = yanit.php_surum === secili
                return (
                  <span className="text-xs text-slate-500 dark:text-slate-500 flex items-center gap-2">
                    <span>{s.aciklama}</span>
                    {akt ? (
                      <span className="text-[10px] uppercase tracking-wider bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 px-1.5 py-0.5 rounded font-semibold">{cevir("Aktif")}</span>
                    ) : (
                      <span className="text-[10px] uppercase tracking-wider bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 px-1.5 py-0.5 rounded font-semibold">{cevir("Kaydet ile uygula")}</span>
                    )}
                  </span>
                )
              })()}
            </div>
          </Kart>

          {/* Performance & Security */}
          <Kart baslik={cevir("Performans ve Güvenlik")}>
            <Grid>
              <Sec etiket="memory_limit" yardim={cevir("Bir scriptin ayırabileceği maksimum bellek (byte). Örn: 256M.")}>
                <Boyut value={a.memory_limit} onChange={v => P('memory_limit', v)} />
              </Sec>
              <Saytec etiket="max_execution_time" suffix="sn" yardim={cevir("Maks. çalışma süresi (saniye)")} value={a.max_execution_time} onChange={v => P('max_execution_time', v)} />
              <Saytec etiket="max_input_time" suffix="sn" yardim={cevir("POST/GET verisini ayrıştırma süresi (saniye)")} value={a.max_input_time} onChange={v => P('max_input_time', v)} />
              <Sec etiket="post_max_size" yardim={cevir("POST verisi maksimum büyüklük. upload_max'tan büyük olmalı.")}>
                <Boyut value={a.post_max_size} onChange={v => P('post_max_size', v)} />
              </Sec>
              <Sec etiket="upload_max_filesize" yardim={cevir("Tek dosya yükleme limiti")}>
                <Boyut value={a.upload_max_filesize} onChange={v => P('upload_max_filesize', v)} />
              </Sec>
              <Bayrak etiket="opcache.enable" yardim={cevir("OPcache opcode cache (önerilen: AÇIK)")} value={a.opcache_enable} onChange={v => P('opcache_enable', v)} />
            </Grid>
            <div className="mt-4">
              <Etiket>disable_functions <Ipucu t={cevir("Devre dışı bırakılacak PHP fonksiyonları (virgülle ayrılmış)")} /></Etiket>
              <Txt value={a.disable_functions} onChange={v => P('disable_functions', v)} mono />
            </div>
          </Kart>

          {/* Common */}
          <Kart baslik={cevir(cevir("Genel"))}>
            <Grid>
              <Bayrak etiket="display_errors" yardim={cevir("Hataları çıktıya yazdır (canlıda kapalı tut)")} value={a.display_errors} onChange={v => P('display_errors', v)} />
              <Bayrak etiket="log_errors" yardim={cevir("Hata loglamayı aç")} value={a.log_errors} onChange={v => P('log_errors', v)} />
              <Bayrak etiket="allow_url_fopen" yardim={cevir("HTTP/FTP üzerinden dosya açma")} value={a.allow_url_fopen} onChange={v => P('allow_url_fopen', v)} />
              <Bayrak etiket="file_uploads" yardim={cevir("HTTP dosya yükleme")} value={a.file_uploads} onChange={v => P('file_uploads', v)} />
              <Bayrak etiket="short_open_tag" yardim={cevir("<? ?> kısa tag desteği")} value={a.short_open_tag} onChange={v => P('short_open_tag', v)} />
            </Grid>
            <Tek e="error_reporting" h={cevir("Hata raporlama seviyesi (örn: E_ALL & ~E_DEPRECATED)")}>
              <Txt value={a.error_reporting} onChange={v => P('error_reporting', v)} mono />
            </Tek>
            <Tek e="include_path" h={cevir("Script include dizinleri (Linux: : ile ayır)")}>
              <Txt value={a.include_path} onChange={v => P('include_path', v)} mono />
            </Tek>
            <Tek e="open_basedir" h={cevir("PHP'nin erişebileceği dizinler (boş = sınır yok). : ile ayır")}>
              <Txt value={a.open_basedir} onChange={v => P('open_basedir', v)} mono placeholder={cevir("örn: /home/kullanici/:/tmp/")} />
            </Tek>
            <Tek e="session.save_path" h={cevir("Session dosyaları dizini (boş = /home/{sk}/tmp)")}>
              <Txt value={a.session_save_path} onChange={v => P('session_save_path', v)} mono />
            </Tek>
            <Tek e="mail.force_extra_parameters" h={cevir("mail() fonksiyonu için ek parametreler")}>
              <Txt value={a.mail_force_extra_parameters} onChange={v => P('mail_force_extra_parameters', v)} mono />
            </Tek>
          </Kart>

          {/* PHP Debug Modu — master switch */}
          <Kart baslik={cevir("PHP Debug Modu")}>
            <div className="flex items-start gap-4">
              <button onClick={() => P('debug_mode', !a.debug_mode)}
                className={`flex-shrink-0 mt-0.5 relative inline-flex h-6 w-11 items-center rounded-full transition ${a.debug_mode ? 'bg-amber-500' : 'bg-slate-300 dark:bg-slate-600'}`}
                title={a.debug_mode ? cevir("Debug modunu kapat") : cevir("Debug modunu ac")}>
                <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition ${a.debug_mode ? 'translate-x-6' : 'translate-x-1'}`} />
              </button>
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Debug modu")}</span>
                  <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold ${a.debug_mode ? 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400'}`}>
                    {a.debug_mode ? cevir("Acik") : cevir("Kapali")}
                  </span>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-500 mt-1 leading-relaxed">
                  {cevir("Acikken PHP hatalarini ekrana yazdirir ve olumcul (fatal) hatalari guvenilir sekilde yakalayip")}
                  <code className="font-mono"> .gpanel/php_debug.log</code>{cevir("'a kaydeder. Uygulama kendi ")}<code className="font-mono">error_reporting(0)</code>{cevir("'ini cagirsa bile fatal hatalar yine yakalanir.")}
                </p>
              </div>
            </div>
            {a.debug_mode && (
              <div className="mt-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md text-xs text-amber-800 dark:text-amber-200">
                {cevir("⚠️ Debug modu acikken ")}<strong>display_errors</strong>{cevir(" ve ")}<strong>error_reporting = E_ALL</strong>{cevir(" zorlanir; hata detaylari ziyaretcilere gorunebilir. Yalnizca sorun giderirken acin, canli sitede ")}<strong>{cevir("kapatin")}</strong>{cevir(". Degisiklik ")}<strong>{cevir("Kaydet")}</strong>{cevir("'ten sonra uygulanir.")}
              </div>
            )}
          </Kart>

          {/* Son Hatalar — debug log paneli */}
          <Kart baslik={cevir("Son Hatalar (Debug Log)")}>
            <div className="flex flex-wrap items-center justify-between gap-3 mb-3">
              <p className="text-xs text-slate-500 dark:text-slate-500 min-w-0 break-all">
                {cevir("En yeni fatal hatalar ustte. Kaynak: ")}<code className="font-mono">/home/{yanit.sk}/.gpanel/php_debug.log</code>{cevir(" (son 200 satir).")}
              </p>
              <div className="flex gap-2 flex-shrink-0">
                <button onClick={debugLogYukle} disabled={dlogYuk}
                  className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300 text-xs rounded-md disabled:opacity-60">
                  <span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} /> {cevir("Yenile")}</span>
                </button>
                <button onClick={debugLogTemizle} disabled={dlogYuk || dlog.length === 0}
                  className="px-3 py-1.5 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 text-xs rounded-md disabled:opacity-40">
                  <span className="inline-flex items-center gap-1.5"><Ikon d={I.cop} /> {cevir("Temizle")}</span>
                </button>
              </div>
            </div>
            {dlogYuk ? (
              <div className="py-6 text-center text-xs text-slate-400 dark:text-slate-500">{cevir("Yukleniyor…")}</div>
            ) : dlog.length === 0 ? (
              <div className="py-6 text-center text-xs text-slate-400 dark:text-slate-500 border border-dashed border-slate-200 dark:border-slate-700 rounded-lg">
                {a.debug_mode
                  ? cevir("Henuz kayitli fatal hata yok. Bir hata olusursa burada gorunur.")
                  : cevir("Debug modu kapali. Fatal hatalarin kaydedilmesi icin yukaridan debug modunu acip kaydedin.")}
              </div>
            ) : (
              <div className="max-h-80 overflow-auto rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-950">
                <ul className="divide-y divide-slate-800">
                  {[...dlog].reverse().map((satir, i) => (
                    <li key={i} className="px-3 py-1.5 text-[11px] font-mono text-red-300 whitespace-pre-wrap break-all leading-relaxed">
                      {satir}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </Kart>

          {/* PHP-FPM */}
          <Kart baslik={cevir("PHP-FPM Havuzu")}>
            <Grid>
              <Sec etiket="pm" yardim={cevir("Process manager stratejisi")}>
                <select value={a.pm_strategy} onChange={e => P('pm_strategy', e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono">
                  {PMS.map(p => <option key={p.v} value={p.v}>{cevir(p.t)}</option>)}
                </select>
              </Sec>
              <Saytec etiket="pm.max_children" yardim={cevir("Aynı anda en çok kaç PHP worker")} value={a.pm_max_children} onChange={v => P('pm_max_children', v)} />
              <Saytec etiket="pm.max_requests" yardim={cevir("Worker'ı kaç istek sonra yeniden başlat (memory leak önler)")} value={a.pm_max_requests} onChange={v => P('pm_max_requests', v)} />
              <Saytec etiket="pm.start_servers" yardim={cevir("Başlangıçta açılacak worker sayısı (dynamic için)")} value={a.pm_start_servers} onChange={v => P('pm_start_servers', v)} />
              <Saytec etiket="pm.min_spare_servers" yardim={cevir("Bekleyen minimum worker sayısı")} value={a.pm_min_spare_servers} onChange={v => P('pm_min_spare_servers', v)} />
              <Saytec etiket="pm.max_spare_servers" yardim={cevir("Bekleyen maksimum worker sayısı")} value={a.pm_max_spare_servers} onChange={v => P('pm_max_spare_servers', v)} />
            </Grid>
          </Kart>

          {/* PHP Modülleri (read-only, sunucu seviyesi) */}
          <Kart baslik={cevir("Kurulu PHP Modülleri")}>
            <div className="flex items-baseline justify-between mb-2">
              <p className="text-xs text-slate-500 dark:text-slate-500">
                PHP {yanit.php_surum} {cevir("için yüklü")} <strong>{yanit.moduller?.length || 0}</strong> {cevir("modül. Modüller sunucu seviyesindedir — tek bir domain için ayrı kapatılamaz, server-wide yönetilir.")}
              </p>
              <Link to="/sistem/php-modulleri" className="text-xs text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium whitespace-nowrap">
                {cevir(cevir("↗ Sunucu Modüllerini Yönet"))}
              </Link>
            </div>
            <div className="flex flex-wrap gap-1">
              {(yanit.moduller || []).map(m => (
                <span key={m} className="text-[11px] font-mono px-2 py-0.5 rounded bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800">
                  {m}
                </span>
              ))}
            </div>
          </Kart>

          {/* Tehlikeli Fonksiyonlar — per-domain disable_functions toggle */}
          <Kart baslik={cevir("Tehlikeli Fonksiyonları Devre Dışı Bırak")}>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
              {cevir(cevir("Bu fonksiyonlar shell injection, RCE, malware riski oluşturur. Kategori bazlı toggle ile"))} <code className="font-mono">disable_functions</code>{cevir("'a yazılır.")} <strong>{cevir("Açık = aktif")}</strong> {cevir("(engellenmemiş),")} <strong>{cevir("kapalı = bloke")}</strong>.
            </p>
            {(() => {
              const gruplar = [
                { ad: cevir("Shell yürütme"), renk: 'red',
                  fonk: ['exec', 'passthru', 'shell_exec', 'system', 'proc_open', 'popen', 'pcntl_exec'] },
                { ad: cevir("Dosya çalıştırma"), renk: 'orange',
                  fonk: ['assert', 'create_function'] },
                { ad: cevir("Ağ erişimi (riskli)"), renk: 'amber',
                  fonk: ['fsockopen', 'pfsockopen', 'stream_socket_client', 'curl_multi_exec'] },
                { ad: cevir("Sistem keşfi"), renk: 'sky',
                  fonk: ['phpinfo', 'posix_kill', 'posix_setuid', 'posix_setgid', 'posix_setpgid'] },
                { ad: cevir("Modül yükleme"), renk: 'violet',
                  fonk: ['dl', 'putenv', 'pcntl_signal', 'pcntl_fork'] },
              ]
              const mevcutDis = (a.disable_functions || '').split(',').map(s => s.trim()).filter(Boolean)
              const mevcutSet = new Set(mevcutDis)

              function grupAktif(g: typeof gruplar[0]) {
                // Hepsi disable edilmişse → bloke. Hepsi yoksa → aktif. Karışıksa → karışık.
                const tum = g.fonk.every(f => mevcutSet.has(f))
                const hicbiri = g.fonk.every(f => !mevcutSet.has(f))
                if (tum) return 'blokeli'
                if (hicbiri) return 'aktif'
                return 'karisik'
              }
              function grupTogga(g: typeof gruplar[0]) {
                const yeni = new Set(mevcutSet)
                const tum = g.fonk.every(f => yeni.has(f))
                if (tum) {
                  // Tum fonksiyonlari cikar (aktif et)
                  g.fonk.forEach(f => yeni.delete(f))
                } else {
                  // Hepsini ekle (blokela)
                  g.fonk.forEach(f => yeni.add(f))
                }
                P('disable_functions', Array.from(yeni).join(','))
              }
              const renkMap: Record<string, string> = {
                red: 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20',
                orange: 'border-orange-200 bg-orange-50/40',
                amber: 'border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20',
                sky: 'border-sky-200 bg-sky-50 dark:bg-sky-900/20',
                violet: 'border-violet-200 dark:border-violet-800 bg-violet-50 dark:bg-violet-900/20',
              }

              return (
                <div className="space-y-2">
                  {gruplar.map(g => {
                    const durum = grupAktif(g)
                    const blokeli = durum === 'blokeli'
                    const karisik = durum === 'karisik'
                    return (
                      <div key={g.renk} className={`border rounded-lg p-3 ${renkMap[g.renk]}`}>
                        <div className="flex items-start gap-3">
                          <button onClick={() => grupTogga(g)}
                            className={`flex-shrink-0 mt-0.5 relative inline-flex h-5 w-9 items-center rounded-full transition ${
                              blokeli ? 'bg-red-500' : (karisik ? 'bg-amber-400' : 'bg-emerald-500')
                            }`}
                            title={blokeli ? cevir("Tümünü aç (etkin yap)") : (karisik ? cevir("Tümünü kapat") : cevir("Tümünü kapat (engelle)"))}>
                            <span className={`inline-block h-3 w-3 transform rounded-full bg-white dark:bg-slate-800 shadow transition ${
                              blokeli ? 'translate-x-1' : 'translate-x-5'
                            }`} />
                          </button>
                          <div className="flex-1 min-w-0">
                            <div className="flex items-baseline gap-2">
                              <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir(g.ad)}</span>
                              <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium ${
                                blokeli ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300' : (karisik ? 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300' : 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300')
                              }`}>
                                {blokeli ? cevir("Bloke") : (karisik ? cevir("Karışık") : cevir("Aktif"))}
                              </span>
                            </div>
                            <div className="text-[11px] text-slate-600 dark:text-slate-400 dark:text-slate-500 font-mono mt-0.5 break-all">
                              {g.fonk.join(', ')}
                            </div>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              )
            })()}

            <details className="mt-4">
              <summary className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 cursor-pointer hover:text-slate-900 dark:hover:text-slate-100 dark:text-slate-100">{cevir("Manuel düzenle (ham disable_functions)")}</summary>
              <input value={a.disable_functions} onChange={e => P('disable_functions', e.target.value)}
                className="w-full mt-2 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-xs font-mono" />
              <p className="text-[11px] text-slate-500 dark:text-slate-500 mt-1">{cevir("Virgülle ayrılmış fonksiyon adları.")}</p>
            </details>
          </Kart>

          {/* Additional */}
          <Kart baslik={cevir("Ek Yapılandırma Direktifleri")}>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-2">
              {cevir("php.ini sözdizimini kullanarak ek parametreler tanımlayın. Örn: ")}<code className="font-mono">extension=imagick.so</code>
            </p>
            <textarea value={a.ek_direktifler} onChange={e => P('ek_direktifler', e.target.value)}
              rows={5}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-xs font-mono"
              placeholder=";extension=imagick.so&#10;date.timezone = Europe/Istanbul" />
          </Kart>

          {/* Kaydet */}
          <div className="flex gap-3 mt-6">
            <button onClick={kaydet} disabled={isleniyor}
              className="px-6 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
              {isleniyor ? cevir("Kaydediliyor…") : <span className="inline-flex items-center gap-1.5"><Ikon d={I.disket} /> {cevir("Kaydet ve Uygula")}</span>}
            </button>
            <button onClick={yukle} disabled={isleniyor}
              className="px-4 py-2.5 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300 text-sm rounded-md">
              {cevir(cevir("İptal / Yeniden Yükle"))}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// ----- helper components -----
function Kart({ baslik, children }: { baslik: string; children: any }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-4 pb-2 border-b border-slate-100 dark:border-slate-800">{baslik}</h3>
      {children}
    </div>
  )
}
function Grid({ children }: { children: any }) {
  return <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3">{children}</div>
}
function Etiket({ children }: { children: any }) {
  return <label className="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">{children}</label>
}
function Ipucu({ t }: { t: string }) {
  return <span title={t} className="inline-block ml-1 text-slate-400 dark:text-slate-500 cursor-help"><Ikon d={I.bilgi} className="inline-block h-3.5 w-3.5 align-middle" /></span>
}
function Tek({ e, h, children }: { e: string; h: string; children: any }) {
  return (
    <div className="mt-3">
      <Etiket>{e} <Ipucu t={h} /></Etiket>
      {children}
    </div>
  )
}
function Sec({ etiket, yardim, children }: { etiket: string; yardim: string; children: any }) {
  return (
    <div>
      <Etiket>{etiket} <Ipucu t={yardim} /></Etiket>
      {children}
    </div>
  )
}
function Saytec({ etiket, yardim, suffix, value, onChange }: { etiket: string; yardim: string; suffix?: string; value: number; onChange: (v: number) => void }) {
  return (
    <Sec etiket={etiket} yardim={yardim}>
      <div className="flex">
        <input type="number" value={value} onChange={e => onChange(parseInt(e.target.value || '0'))}
          className="flex-1 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono" />
        {suffix && <span className="ml-2 text-xs text-slate-500 dark:text-slate-500 self-center">{suffix}</span>}
      </div>
    </Sec>
  )
}
function Txt({ value, onChange, mono, placeholder }: { value: string; onChange: (v: string) => void; mono?: boolean; placeholder?: string }) {
  return (
    <input type="text" value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder}
      className={`w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm ${mono ? 'font-mono' : ''}`} />
  )
}
function Bayrak({ etiket, yardim, value, onChange }: { etiket: string; yardim: string; value: boolean; onChange: (v: boolean) => void }) {
  return (
    <Sec etiket={etiket} yardim={yardim}>
      <button onClick={() => onChange(!value)}
        className={`px-3 py-2 rounded-md text-sm font-mono w-full text-left transition border ${value ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-300 text-emerald-700 dark:text-emerald-300' : 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 dark:text-slate-500'}`}>
        {value ? <span className="inline-flex items-center gap-1.5"><Ikon d={I.onay} /> On</span> : '○ Off'}
      </button>
    </Sec>
  )
}
// Boyut — memory_limit / post_max_size / upload_max_filesize icin SERBEST METIN.
//
// 🔴 Onceki `SecveOzel` bileseni acilir menu + "Ozel…" secenegi sunuyordu
// ama OZEL SECENEK HIC CALISMIYORDU:
//     if (e.target.value === '__ozel') return   // <- hicbir sey yapmadan cikar
// "Ozel…" secilince deger DEGISMIYOR; deger hala listede oldugu icin `ozelMi`
// false kaliyor ve yanindaki metin kutusu ACILMIYOR. Kullanici listede olmayan
// bir deger giremiyordu. (Listede olmayan bir deger DB'den gelirse kutu
// aciliyordu — bu yuzden hata "bazen calisiyor" gibi gorunuyordu.)
//
// Sabit liste ayrica bakim yuku: her yeni varsayilanda listeyi guncellemek
// gerekiyor, unutulursa operatorun KENDI ayari "Ozel…" diye gorunuyor.
// Cozum: menuyu kaldir, operator ne isterse yazsin.
function Boyut({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <input
      type="text"
      value={value}
      onChange={e => onChange(e.target.value)}
      placeholder="orn: 2048M, 8G, -1 (sinirsiz)"
      spellCheck={false}
      autoComplete="off"
      className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm font-mono"
    />
  )
}
