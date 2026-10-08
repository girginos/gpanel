import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import OpsPanel from '@/components/OpsPanel'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; durum: string }

type SSLBilgi = { gecerli: boolean; bitis_tarihi: string; kalan_gun: number; cikaran?: string; ozne?: string }
type Health = {
  url: string; durum_kodu: number; yanit_suresi_ms: number; erisilebilir: boolean
  hata?: string; sema: string; ssl?: SSLBilgi; boyut_byte: number; server?: string
}

const POLL_MS = 5000


const IZL_EN: Record<string, string> = {
  "(kayıt yok)": "(no records)",
  "Alan adı listesi alınamadı": "Failed to get domain list",
  "Ağ Trafiği": "Network Traffic",
  "Bu domain HTTPS üzerinden erişilemiyor.": "This domain is not accessible over HTTPS.",
  "Domain Bazlı": "Per Domain",
  "Domain Seçimi": "Domain Selection",
  "En Yoğun İşlemler": "Top Processes",
  "Erişilebilir": "Accessible",
  "Erişilemez": "Unreachable",
  "Erişim Logu (Access)": "Access Log",
  "Hata günlüğü alınamadı": "Failed to get error log",
  "Henüz hata kaydı yok": "No error records yet",
  "Henüz log yok…": "No logs yet…",
  "Hızlı": "Fast",
  "SSL Geçerli": "SSL Valid",
  "SSL Geçersiz": "SSL Invalid",
  "Sağlık Probe": "Health Probe",
  "Sistem Kaynakları": "System Resources",
  "Sistem Bilgisi": "System Info",
  "Sunucu adı": "Hostname",
  "İşletim sistemi": "Operating system",
  "Çekirdek (kernel)": "Kernel",
  "İşlemci": "Processor",
  "Panel sürümü": "Panel version",
  "Siteyi aç": "Open site",
  "Sorgulanıyor…": "Querying…",
  "Sunucu Günlükleri": "Server Logs",
  "Süreç listesi alınamadı": "Failed to get process list",
  "Tüm Sistem": "Whole System",
  "Uçtan Uca İzleme": "End-to-End Monitoring",
  "Veri toplanıyor…": "Collecting data…",
  "Yanıt Süresi": "Response Time",
  "Yavaş": "Slow",
  "Yük (norm)": "Load (norm)",
  "gün": "days",
  "Anasayfa": "Home",
  "İzleme": "Monitoring",
  "Sunucu": "Server",
  "Canlı": "Live",
  "sn yenile": "s refresh",
  "çekirdek": "cores",
  "Bellek": "Memory",
  "Yük (1dk)": "Load (1m)",
  "5dk {0} · 15dk {1}": "5m {0} · 15m {1}",
  "{0}/{1} örnek · {2}dk": "{0}/{1} samples · {2}m",
  "Arayüz: {0}": "Interface: {0}",
  "Servisler": "Services",
  "aktif": "active",
  "Aktif": "Active",
  "Kapalı": "Off",
  "Kullanıcı": "User",
  "Komut": "Command",
  "Aktif domain yok": "No active domains",
  "son": "last",
  "satır": "lines",
  "Hata Logu (Error)": "Error Log",
  "Ara…": "Search…",
  "Yenile": "Refresh",
  "\"{0}\" bulunamadı.": "\"{0}\" not found.",
  "SSL Yok": "No SSL",
  "Bitiş:": "Expiry:",
  "Çıkaran:": "Issuer:",
  "Kabul edilebilir": "Acceptable",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (IZL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function IzlemePage() {
  useTranslation() // dil re-render aboneligi
  const [tab, setTab] = useState<'sunucu' | 'domain' | 'loglar'>('sunucu')
  return (
    <div>
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("İzleme") }]} />
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between mb-4">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Uçtan Uca İzleme")}</h1>
        <span className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-500">
          <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
          {cevir("Canlı")} · {POLL_MS / 1000}{cevir("sn yenile")}
        </span>
      </div>

      <div className="flex gap-1 mb-5 border-b border-slate-200 dark:border-dark-600 overflow-x-auto [&>*]:shrink-0">
        <SekmeButon aktif={tab === 'sunucu'}  onClick={() => setTab('sunucu')}>{cevir(cevir("Sunucu"))}</SekmeButon>
        <SekmeButon aktif={tab === 'domain'} onClick={() => setTab('domain')}>{cevir("Domain Bazlı")}</SekmeButon>
        <SekmeButon aktif={tab === 'loglar'} onClick={() => setTab('loglar')}>{cevir("Sunucu Günlükleri")}</SekmeButon>
      </div>

      {tab === 'sunucu' ? <OpsPanel /> : tab === 'domain' ? <DomainIzleme /> : <SunucuLoglari />}
    </div>
  )
}

function SekmeButon({ aktif, onClick, children }: { aktif: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button onClick={onClick} className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition ${
      aktif ? 'border-brand-600 text-brand-700 dark:text-brand-300' : 'border-transparent text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300'
    }`}>{children}</button>
  )
}

// ============================================================================
// DOMAIN BAZLI İZLEME
// ============================================================================
function DomainIzleme() {
  const [domains, setDomains] = useState<Domain[]>([])
  const [secili, setSecili] = useState<number | null>(null)
  const [health, setHealth] = useState<Health | null>(null)
  const [hSorgulaniyor, setHSorgulaniyor] = useState(false)
  const [accessLog, setAccessLog] = useState<string[]>([])
  const [errorLog, setErrorLog] = useState<string[]>([])
  const [logHata, setLogHata] = useState<string | null>(null)
  const accessRef = useRef<HTMLDivElement>(null)
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let iptal = false
    api.get<Domain[]>('/domains').then(r => {
      if (iptal) return
      const aktif = r.data.filter(d => d.durum === 'aktif')
      setDomains(aktif)
      if (aktif.length > 0 && secili === null) setSecili(aktif[0].id)
    }).catch(e => { if (!iptal) hataYakala(cevir("Alan adı listesi alınamadı"))(e) })
    return () => { iptal = true }
  }, [])

  function probet(id: number) {
    setHSorgulaniyor(true); setHealth(null)
    api.get<Health>(`/domains/${id}/health`).then(r => setHealth(r.data))
      .catch(e => setHealth({ url: '', durum_kodu: 0, yanit_suresi_ms: 0, erisilebilir: false, hata: apiHata(e), sema: '', boyut_byte: 0 }))
      .finally(() => setHSorgulaniyor(false))
  }

  useEffect(() => {
    if (!secili) return
    probet(secili)
    setAccessLog([]); setErrorLog([]); setLogHata(null)

    function logCek() {
      api.get<{ satirlar: string[]; mevcut: boolean }>(`/domains/${secili}/logs/oku?dosya=access&son=80`)
        .then(r => setAccessLog(r.data.satirlar || []))
        .catch(e => setLogHata(apiHata(e)))
      api.get<{ satirlar: string[]; mevcut: boolean }>(`/domains/${secili}/logs/oku?dosya=error&son=40`)
        .then(r => setErrorLog(r.data.satirlar || []))
        .catch(hataYakala(cevir("Hata günlüğü alınamadı")))
    }
    logCek()
    const t = setInterval(logCek, POLL_MS)
    return () => clearInterval(t)
  }, [secili])

  // Auto-scroll to bottom on log update
  useEffect(() => { if (accessRef.current) accessRef.current.scrollTop = accessRef.current.scrollHeight }, [accessLog])
  useEffect(() => { if (errorRef.current) errorRef.current.scrollTop = errorRef.current.scrollHeight }, [errorLog])

  const seciliDomain = useMemo(() => domains.find(d => d.id === secili), [domains, secili])

  return (
    <>
      <Kart baslik={cevir("Domain Seçimi")}>
        <div className="flex items-center gap-3 flex-wrap">
          <select value={secili ?? ''} onChange={e => setSecili(Number(e.target.value))}
            className="px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm bg-white dark:bg-dark-700 w-full sm:w-auto sm:min-w-[280px] focus:border-brand-500 outline-none">
            {domains.length === 0 && <option value="">{cevir("Aktif domain yok")}</option>}
            {domains.map(d => <option key={d.id} value={d.id}>{d.alan_adi}</option>)}
          </select>
          {secili && (
            <Button onClick={() => probet(secili)} yukleniyor={hSorgulaniyor} variant="outlined" ikon={I.yenile}>
              {hSorgulaniyor ? cevir("Sorgulanıyor…") : cevir("Sağlık Probe")}
            </Button>
          )}
          {seciliDomain && (
            <a href={`https://${seciliDomain.alan_adi}`} target="_blank" rel="noreferrer"
              className="inline-flex items-center gap-1 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300">{cevir("Siteyi aç")}<Ikon d={I.harici} className="size-3.5 shrink-0" /></a>
          )}
        </div>
      </Kart>

      <div className="h-5" />

      {/* HTTP sağlık + SSL */}
      {health && (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-3 mb-5">
          <SaglikKart h={health} />
          <SSLKart ssl={health.ssl} sema={health.sema} />
          <YanitKart h={health} />
        </div>
      )}

      {/* Loglar */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <Kart baslik={cevir("Erişim Logu (Access)")} sag={`${cevir("son")} ${accessLog.length} ${cevir("satır")}`}>
          <div ref={accessRef} className="bg-slate-50 dark:bg-dark-900 border border-slate-200 dark:border-transparent text-emerald-800 dark:text-emerald-300 font-mono text-[11px] p-3 rounded h-80 overflow-auto whitespace-pre">
            {accessLog.length === 0
              ? <div className="text-slate-500 dark:text-slate-500 italic">{cevir("Henüz log yok…")}</div>
              : accessLog.join('\n')}
          </div>
        </Kart>
        <Kart baslik={cevir("Hata Logu (Error)")} sag={`${cevir("son")} ${errorLog.length} ${cevir("satır")}`}>
          <div ref={errorRef} className="bg-slate-50 dark:bg-dark-900 border border-slate-200 dark:border-transparent text-rose-800 dark:text-rose-300 font-mono text-[11px] p-3 rounded h-80 overflow-auto whitespace-pre">
            {errorLog.length === 0
              ? <div className="text-slate-500 dark:text-slate-500 italic">{logHata || cevir("Henüz hata kaydı yok")}</div>
              : errorLog.join('\n')}
          </div>
        </Kart>
      </div>
    </>
  )
}

// ============================================================================
// COMPONENTS
// ============================================================================
// ============================================================================
// Sunucu Günlükleri (journald) — panel/nginx/mariadb/named/sshd/cron/sistem
// ============================================================================
const LOG_KAYNAK_ET: Record<string, string> = {
  panel: 'Panel', nginx: 'nginx', mariadb: 'MariaDB', named: 'DNS (named)',
  sshd: 'SSH', cron: 'Cron', sistem: 'Tüm Sistem',
}

function SunucuLoglari() {
  const toast = useToast()
  const [kaynak, setKaynak] = useState('panel')
  const [kaynaklar, setKaynaklar] = useState<string[]>(['panel', 'nginx', 'mariadb', 'named', 'sshd', 'cron', 'sistem'])
  const [satirlar, setSatirlar] = useState<string[]>([])
  const [son, setSon] = useState(200)
  const [yuk, setYuk] = useState(true)
  // Hata artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
  const [arama, setArama] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  const yukleNesli = useRef(0)
  function yukle(k = kaynak, n = son) {
    setYuk(true); setHata(null)
    const _n = ++yukleNesli.current
    api.get('/admin/system/loglar', { params: { kaynak: k, son: n } })
      .then((r) => { if (_n !== yukleNesli.current) return; setSatirlar(r.data.satirlar || []); if (r.data.kaynaklar) setKaynaklar(r.data.kaynaklar) })
      .catch((e) => { if (_n !== yukleNesli.current) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => { if (_n === yukleNesli.current) setYuk(false) })
  }
  useEffect(() => { yukle(kaynak, son); return () => { yukleNesli.current++ } /* eslint-disable-next-line */ }, [kaynak, son])
  const gorunen = useMemo(() => {
    const q = arama.trim().toLowerCase()
    return q ? satirlar.filter(s => s.toLowerCase().includes(q)) : satirlar
  }, [satirlar, arama])
  useEffect(() => { if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight }, [gorunen])

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <div className="flex flex-wrap gap-1">
          {kaynaklar.map(k => (
            <Button key={k} onClick={() => setKaynak(k)}
              color={kaynak === k ? 'primary' : 'neutral'}
              variant={kaynak === k ? 'filled' : 'outlined'}
              boyut="sm">
              {cevir(LOG_KAYNAK_ET[k] || k)}
            </Button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <input value={arama} onChange={e => setArama(e.target.value)} placeholder={cevir("Ara…")}
            className="px-2.5 py-1.5 text-xs w-40 border border-slate-200 dark:border-dark-600 rounded-md bg-white dark:bg-dark-700 text-slate-700 dark:text-slate-200 placeholder:text-slate-400 outline-none focus:border-brand-500" />
          <select value={son} onChange={e => setSon(Number(e.target.value))}
            className="px-2 py-1.5 text-xs border border-slate-200 dark:border-dark-600 rounded-md bg-white dark:bg-dark-700 text-slate-600 dark:text-slate-300">
            {[100, 200, 500, 1000].map(n => <option key={n} value={n}>{cevir("son")} {n}</option>)}
          </select>
          <Button onClick={() => yukle()} yukleniyor={yuk} variant="outlined" boyut="sm" ikon={I.yenile}>{cevir("Yenile")}</Button>
        </div>
      </div>
      <div ref={scrollRef} className="bg-slate-50 dark:bg-dark-800 border border-slate-200 dark:border-dark-600 rounded-lg overflow-auto p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all h-[min(60vh,320px)] sm:h-[420px] lg:h-[560px]">
        {yuk ? <div className="text-slate-500 py-4">{cevir("Yükleniyor…")}</div>
          : gorunen.length === 0 ? <div className="text-slate-500 py-4">{arama ? cevirT(cevir("\"{0}\" bulunamadı."), arama) : cevir("(kayıt yok)")}</div>
            : gorunen.map((s, i) => <div key={i} className={logRenk(s)}>{s}</div>)}
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400 mt-2">{arama ? `${gorunen.length} / ${satirlar.length}` : satirlar.length} {cevir("satır")} · journald · {cevir(LOG_KAYNAK_ET[kaynak] || kaynak)}</p>
    </div>
  )
}

function logRenk(s: string): string {
  if (/error|fail|fatal|denied|refused|panic|segfault/i.test(s)) return 'text-red-700 dark:text-red-400'
  if (/warn/i.test(s)) return 'text-amber-700 dark:text-amber-400'
  if (/notice|started|listening|active/i.test(s)) return 'text-sky-700 dark:text-sky-400'
  return 'text-slate-700 dark:text-slate-300'
}

// tabloMod: içinde duyarlı tablo varsa kullanılır. Mobilde çerçeve/zemin/padding
// kaldırılır — aksi halde satır kartları ikinci bir çerçeveye hapsolup iç içe görünür.
// >= lg görünüm her iki modda da aynıdır.
function Kart({ baslik, children, sag, tabloMod }: { baslik: string; children: React.ReactNode; sag?: React.ReactNode; tabloMod?: boolean }) {
  return (
    <div className={tabloMod
      ? 'lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:p-5'
      : 'bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5'}>
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
        {sag && <div className="text-xs text-slate-500 dark:text-slate-500">{sag}</div>}
      </div>
      {children}
    </div>
  )
}
function SaglikKart({ h }: { h: Health }) {
  const ok = h.erisilebilir && h.durum_kodu >= 200 && h.durum_kodu < 400
  return (
    <div className={`rounded-lg p-4 border ${ok ? 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20' : 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20'}`}>
      <div className="flex items-center gap-2 mb-2">
        <span className={`w-2.5 h-2.5 rounded-full ${ok ? 'bg-emerald-500 animate-pulse' : 'bg-red-500'}`}></span>
        <span className={`text-sm font-semibold ${ok ? 'text-emerald-800 dark:text-emerald-200' : 'text-red-800 dark:text-red-200'}`}>
          {ok ? cevir("Erişilebilir") : cevir("Erişilemez")}
        </span>
      </div>
      <div className="text-3xl font-bold font-mono mt-1">
        {h.durum_kodu > 0 ? h.durum_kodu : '—'}
      </div>
      <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 mt-1 truncate" title={h.url}>{h.url}</div>
      {h.hata && <div className="mt-2 text-[11px] text-red-700 dark:text-red-300 break-words">{h.hata}</div>}
      {h.server && <div className="mt-2 text-[11px] text-slate-500 dark:text-slate-500">Server: <span className="font-mono">{h.server}</span></div>}
    </div>
  )
}
function SSLKart({ ssl, sema }: { ssl?: SSLBilgi; sema: string }) {
  if (sema !== 'https' || !ssl) {
    return (
      <div className="rounded-lg p-4 border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20">
        <div className="text-sm font-semibold text-amber-800 dark:text-amber-200 mb-2"><span className="inline-flex items-center gap-1.5"><Ikon d={I.uyari} /> {cevir("SSL Yok")}</span></div>
        <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{cevir("Bu domain HTTPS üzerinden erişilemiyor.")}</div>
      </div>
    )
  }
  const krit = !ssl.gecerli || ssl.kalan_gun < 15
  return (
    <div className={`rounded-lg p-4 border ${krit ? 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20' : 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20'}`}>
      <div className={`text-sm font-semibold mb-2 ${krit ? 'text-red-800 dark:text-red-200' : 'text-emerald-800 dark:text-emerald-200'}`}>
        {ssl.gecerli ? <span className="inline-flex items-center gap-1.5"><Ikon d={I.kilit} /> {cevir("SSL Geçerli")}</span> : <span className="inline-flex items-center gap-1.5"><Ikon d={I.kapat} /> {cevir("SSL Geçersiz")}</span>}
      </div>
      <div className="text-2xl font-bold text-slate-900 dark:text-slate-100 font-mono">{ssl.kalan_gun}<span className="text-base ml-1 text-slate-500 dark:text-slate-500">{cevir("gün")}</span></div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-1">{cevir("Bitiş:")} <span className="font-mono">{ssl.bitis_tarihi}</span></div>
      {ssl.cikaran && <div className="text-[11px] text-slate-500 dark:text-slate-500">{cevir("Çıkaran:")} <span className="font-mono">{ssl.cikaran}</span></div>}
    </div>
  )
}
function YanitKart({ h }: { h: Health }) {
  const ms = h.yanit_suresi_ms
  const renk = ms < 300 ? 'emerald' : ms < 1000 ? 'amber' : 'red'
  const m: Record<string, string> = {
    emerald: 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-800 dark:text-emerald-200',
    amber: 'border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 text-amber-800 dark:text-amber-200',
    red: 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200',
  }
  return (
    <div className={`rounded-lg p-4 border ${m[renk]}`}>
      <div className="text-sm font-semibold mb-2">{cevir("Yanıt Süresi")}</div>
      <div className="text-3xl font-bold text-slate-900 dark:text-slate-100 font-mono">{ms.toFixed(0)}<span className="text-base ml-1 text-slate-500 dark:text-slate-500">ms</span></div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-1">
        {ms < 300 ? cevir("Hızlı") : ms < 1000 ? cevir("Kabul edilebilir") : cevir("Yavaş")} · {h.sema.toUpperCase()}
      </div>
    </div>
  )
}
