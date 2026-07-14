// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type CPU = { yuzde: number; cekirdek: number; yuk_1dk: number; yuk_5dk: number; yuk_15dk: number }
type Bellek = { toplam_kb: number; kullanilan_kb: number; bos_kb: number; yuzde: number }
type Swap = { toplam_kb: number; kullanilan_kb: number; yuzde: number }
type Disk = { toplam_byte: number; kullanilan_byte: number; bos_byte: number; yuzde: number; mount: string; fs?: string }
type Ag = { arayuz: string; rx_bytes_sn: number; tx_bytes_sn: number; rx_toplam_byte: number; tx_toplam_byte: number }
type Servis = { ad: string; etiket: string; aktif: boolean }
type SistemInfo = { hostname: string; ip: string; os_adi: string; kernel: string; cpu_modeli: string; cpu_cekirdek: number; panel_surum: string }

type Usage = {
  sistem: SistemInfo; cpu: CPU; bellek: Bellek; swap: Swap
  disk: Disk; diskler: Disk[]; ag: Ag; servisler: Servis[]
  uptime_sn: number
}

type Process = { pid: number; user: string; cpu_yuzde: number; mem_yuzde: number; komut: string }

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; durum: string }

type SSLBilgi = { gecerli: boolean; bitis_tarihi: string; kalan_gun: number; cikaran?: string; ozne?: string }
type Health = {
  url: string; durum_kodu: number; yanit_suresi_ms: number; erisilebilir: boolean
  hata?: string; sema: string; ssl?: SSLBilgi; boyut_byte: number; server?: string
}

type Nokta = { t: number; cpu: number; mem: number; swap: number; load: number; rx: number; tx: number }

const MAX_NOKTA = 60 // 60 sample × 5s = 5 dakika
const POLL_MS = 5000

export default function IzlemePage() {
  const [tab, setTab] = useState<'sunucu' | 'domain' | 'loglar'>('sunucu')
  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'İzleme' }]} />
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Uçtan Uca İzleme</h1>
        <span className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-500">
          <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
          Canlı · {POLL_MS / 1000}sn yenile
        </span>
      </div>

      <div className="flex gap-1 mb-5 border-b border-slate-200 dark:border-slate-700">
        <SekmeButon aktif={tab === 'sunucu'}  onClick={() => setTab('sunucu')}>Sunucu</SekmeButon>
        <SekmeButon aktif={tab === 'domain'} onClick={() => setTab('domain')}>Domain Bazlı</SekmeButon>
        <SekmeButon aktif={tab === 'loglar'} onClick={() => setTab('loglar')}>Sunucu Günlükleri</SekmeButon>
      </div>

      {tab === 'sunucu' ? <SunucuIzleme /> : tab === 'domain' ? <DomainIzleme /> : <SunucuLoglari />}
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
// SUNUCU İZLEME
// ============================================================================
function SunucuIzleme() {
  const [u, setU] = useState<Usage | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [noktalar, setNoktalar] = useState<Nokta[]>([])
  const [procs, setProcs] = useState<Process[]>([])
  const [procSort, setProcSort] = useState<'cpu' | 'mem'>('cpu')

  useEffect(() => {
    function yukle() {
      api.get<Usage>('/system/usage').then(r => {
        setU(r.data)
        setNoktalar(prev => {
          const n: Nokta = {
            t: Date.now(),
            cpu: r.data.cpu.yuzde,
            mem: r.data.bellek.yuzde,
            swap: r.data.swap.yuzde || 0,
            load: Math.min(100, (r.data.cpu.yuk_1dk / Math.max(1, r.data.cpu.cekirdek)) * 100),
            rx: r.data.ag.rx_bytes_sn || 0,
            tx: r.data.ag.tx_bytes_sn || 0,
          }
          const yeni = [...prev, n]
          return yeni.length > MAX_NOKTA ? yeni.slice(-MAX_NOKTA) : yeni
        })
      }).catch(e => setHata(apiHata(e)))
    }
    yukle()
    const t = setInterval(yukle, POLL_MS)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    function yukleProcs() {
      api.get<Process[]>(`/system/processes?n=15&sirala=${procSort}`).then(r => setProcs(r.data)).catch(() => {})
    }
    yukleProcs()
    const t = setInterval(yukleProcs, POLL_MS * 2)
    return () => clearInterval(t)
  }, [procSort])

  return (
    <>
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Snapshot grid */}
      {u && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
          <Snap baslik="CPU" deger={u.cpu.yuzde.toFixed(1) + '%'} alt={`${u.cpu.cekirdek} çekirdek`} renk="indigo" />
          <Snap baslik="Bellek" deger={u.bellek.yuzde.toFixed(1) + '%'}
            alt={`${(u.bellek.kullanilan_kb/1024).toFixed(0)} / ${(u.bellek.toplam_kb/1024).toFixed(0)} MB`} renk="emerald" />
          <Snap baslik="Yük (1dk)" deger={u.cpu.yuk_1dk.toFixed(2)}
            alt={`5dk ${u.cpu.yuk_5dk.toFixed(2)} · 15dk ${u.cpu.yuk_15dk.toFixed(2)}`} renk="amber" />
          <Snap baslik="Disk (/)" deger={u.disk.yuzde.toFixed(1) + '%'}
            alt={`${fmtByte(u.disk.kullanilan_byte)} / ${fmtByte(u.disk.toplam_byte)}`} renk="violet" />
        </div>
      )}

      {/* Çok serili çizgi grafik */}
      <Kart baslik="Sistem Kaynakları" sag={`${noktalar.length}/${MAX_NOKTA} örnek · ${(noktalar.length*POLL_MS/1000/60).toFixed(1)}dk`}>
        <div className="flex items-center gap-4 mb-2 text-xs">
          <Lej renk="bg-indigo-500" et="CPU" />
          <Lej renk="bg-emerald-500" et="Bellek" />
          <Lej renk="bg-violet-500" et="Swap" />
          <Lej renk="bg-amber-500" et="Yük (norm)" />
        </div>
        <CokSeriliGrafik noktalar={noktalar} alanlar={[
          { anahtar: 'cpu', renk: '#6366f1' },
          { anahtar: 'mem', renk: '#10b981' },
          { anahtar: 'swap', renk: '#8b5cf6' },
          { anahtar: 'load', renk: '#f59e0b' },
        ]} yMaks={100} ekstra="(%)" />
      </Kart>

      <div className="h-5" />

      {/* Ağ trafiği grafiği */}
      <Kart baslik="Ağ Trafiği" sag={u?.ag?.arayuz ? `Arayüz: ${u.ag.arayuz}` : ''}>
        <div className="flex items-center gap-4 mb-2 text-xs">
          <Lej renk="bg-sky-500" et="↓ RX (KB/s)" />
          <Lej renk="bg-pink-500" et="↑ TX (KB/s)" />
        </div>
        <CokSeriliGrafik
          noktalar={noktalar.map(n => ({ ...n, rx: n.rx/1024, tx: n.tx/1024 }))}
          alanlar={[
            { anahtar: 'rx', renk: '#0ea5e9' },
            { anahtar: 'tx', renk: '#ec4899' },
          ]}
          yMaks={Math.max(10, ...noktalar.map(n => Math.max(n.rx, n.tx)/1024) ) * 1.2}
          ekstra="KB/s"
        />
      </Kart>

      <div className="h-5" />

      {/* Servisler */}
      {u && (
        <Kart baslik="Servisler" sag={`${u.servisler.filter(s => s.aktif).length}/${u.servisler.length} aktif`}>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-2">
            {u.servisler.map(s => (
              <div key={s.ad} className={`flex items-center gap-2 px-3 py-2 rounded-md border text-xs ${
                s.aktif ? 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20' : 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20'
              }`}>
                <span className={`w-2 h-2 rounded-full ${s.aktif ? 'bg-emerald-500' : 'bg-red-500'}`}></span>
                <div className="flex-1 min-w-0">
                  <div className="font-medium text-slate-800 dark:text-slate-200 truncate">{s.etiket}</div>
                  <div className="text-[10px] font-mono text-slate-500 dark:text-slate-500 truncate">{s.ad}</div>
                </div>
                <span className={`text-[10px] font-semibold uppercase ${s.aktif ? 'text-emerald-700 dark:text-emerald-300' : 'text-red-700 dark:text-red-300'}`}>
                  {s.aktif ? 'Aktif' : 'Kapalı'}
                </span>
              </div>
            ))}
          </div>
        </Kart>
      )}

      <div className="h-5" />

      {/* Top processes */}
      <Kart baslik="En Yoğun İşlemler" sag={
        <div className="flex items-center gap-1">
          <button onClick={() => setProcSort('cpu')}
            className={`text-[11px] px-2 py-1 rounded ${procSort === 'cpu' ? 'bg-indigo-600 text-white' : 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-200'}`}>CPU</button>
          <button onClick={() => setProcSort('mem')}
            className={`text-[11px] px-2 py-1 rounded ${procSort === 'mem' ? 'bg-emerald-600 text-white' : 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-200'}`}>Bellek</button>
        </div>
      }>
        <table className="w-full text-sm">
          <thead className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700">
            <tr>
              <th className="text-left py-2 w-16">PID</th>
              <th className="text-left py-2">Kullanıcı</th>
              <th className="text-right py-2 w-16">CPU%</th>
              <th className="text-right py-2 w-16">MEM%</th>
              <th className="text-left py-2">Komut</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {procs.length === 0 && (
              <tr><td colSpan={5} className="py-6 text-center text-xs text-slate-400 dark:text-slate-500">Yükleniyor…</td></tr>
            )}
            {procs.map(p => (
              <tr key={p.pid} className="hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">
                <td className="py-1.5 font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{p.pid}</td>
                <td className="py-1.5 font-mono text-xs text-slate-700 dark:text-slate-300 truncate max-w-[120px]">{p.user}</td>
                <td className={`py-1.5 text-right font-mono text-xs ${p.cpu_yuzde >= 50 ? 'text-red-600 dark:text-red-400 font-semibold' : p.cpu_yuzde >= 20 ? 'text-amber-600 dark:text-amber-400' : 'text-slate-600 dark:text-slate-400 dark:text-slate-500'}`}>{p.cpu_yuzde.toFixed(1)}</td>
                <td className={`py-1.5 text-right font-mono text-xs ${p.mem_yuzde >= 30 ? 'text-red-600 dark:text-red-400 font-semibold' : p.mem_yuzde >= 10 ? 'text-amber-600 dark:text-amber-400' : 'text-slate-600 dark:text-slate-400 dark:text-slate-500'}`}>{p.mem_yuzde.toFixed(1)}</td>
                <td className="py-1.5 font-mono text-xs text-slate-800 dark:text-slate-200 truncate max-w-md" title={p.komut}>{p.komut}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Kart>
    </>
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
    api.get<Domain[]>('/domains').then(r => {
      const aktif = r.data.filter(d => d.durum === 'aktif')
      setDomains(aktif)
      if (aktif.length > 0 && secili === null) setSecili(aktif[0].id)
    }).catch(() => {})
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
        .catch(() => {})
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
      <Kart baslik="Domain Seçimi">
        <div className="flex items-center gap-3 flex-wrap">
          <select value={secili ?? ''} onChange={e => setSecili(Number(e.target.value))}
            className="px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm bg-white dark:bg-slate-800 min-w-[280px] focus:border-brand-500 outline-none">
            {domains.length === 0 && <option value="">Aktif domain yok</option>}
            {domains.map(d => <option key={d.id} value={d.id}>{d.alan_adi}</option>)}
          </select>
          {secili && (
            <button onClick={() => probet(secili)} disabled={hSorgulaniyor}
              className="text-sm px-3 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 rounded">
              {hSorgulaniyor ? 'Sorgulanıyor…' : '↻ Sağlık Probe'}
            </button>
          )}
          {seciliDomain && (
            <a href={`https://${seciliDomain.alan_adi}`} target="_blank" rel="noreferrer"
              className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300">Siteyi aç ↗</a>
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
        <Kart baslik="Erişim Logu (Access)" sag={`son ${accessLog.length} satır`}>
          <div ref={accessRef} className="bg-slate-950 text-emerald-300 font-mono text-[11px] p-3 rounded h-80 overflow-auto whitespace-pre">
            {accessLog.length === 0
              ? <div className="text-slate-500 dark:text-slate-500 italic">Henüz log yok…</div>
              : accessLog.join('\n')}
          </div>
        </Kart>
        <Kart baslik="Hata Logu (Error)" sag={`son ${errorLog.length} satır`}>
          <div ref={errorRef} className="bg-slate-950 text-rose-300 font-mono text-[11px] p-3 rounded h-80 overflow-auto whitespace-pre">
            {errorLog.length === 0
              ? <div className="text-slate-500 dark:text-slate-500 italic">{logHata || 'Henüz hata kaydı yok'}</div>
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
  const [kaynak, setKaynak] = useState('panel')
  const [kaynaklar, setKaynaklar] = useState<string[]>(['panel', 'nginx', 'mariadb', 'named', 'sshd', 'cron', 'sistem'])
  const [satirlar, setSatirlar] = useState<string[]>([])
  const [son, setSon] = useState(200)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [arama, setArama] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  function yukle(k = kaynak, n = son) {
    setYuk(true); setHata(null)
    api.get('/admin/system/loglar', { params: { kaynak: k, son: n } })
      .then((r: any) => { setSatirlar(r.data.satirlar || []); if (r.data.kaynaklar) setKaynaklar(r.data.kaynaklar) })
      .catch((e: any) => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(() => { yukle(kaynak, son) /* eslint-disable-next-line */ }, [kaynak, son])
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
            <button key={k} onClick={() => setKaynak(k)}
              className={`px-3 py-1.5 text-xs font-medium rounded-md border transition ${kaynak === k
                ? 'bg-brand-600 border-brand-600 text-white'
                : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'}`}>
              {LOG_KAYNAK_ET[k] || k}
            </button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <input value={arama} onChange={e => setArama(e.target.value)} placeholder="Ara…"
            className="px-2.5 py-1.5 text-xs w-40 border border-slate-200 dark:border-slate-700 rounded-md bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-200 placeholder:text-slate-400 outline-none focus:border-brand-500" />
          <select value={son} onChange={e => setSon(Number(e.target.value))}
            className="px-2 py-1.5 text-xs border border-slate-200 dark:border-slate-700 rounded-md bg-white dark:bg-slate-800 text-slate-600 dark:text-slate-300">
            {[100, 200, 500, 1000].map(n => <option key={n} value={n}>son {n}</option>)}
          </select>
          <button onClick={() => yukle()} disabled={yuk} className="px-3 py-1.5 text-xs border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">↻ Yenile</button>
        </div>
      </div>
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      <div ref={scrollRef} className="bg-slate-900 border border-slate-800 rounded-2xl overflow-auto p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all" style={{ height: 560 }}>
        {yuk ? <div className="text-slate-500 py-4">Yükleniyor…</div>
          : gorunen.length === 0 ? <div className="text-slate-500 py-4">{arama ? `"${arama}" bulunamadı.` : '(kayıt yok)'}</div>
            : gorunen.map((s, i) => <div key={i} className={logRenk(s)}>{s}</div>)}
      </div>
      <p className="text-xs text-slate-400 mt-2">{arama ? `${gorunen.length} / ${satirlar.length}` : satirlar.length} satır · journald · {LOG_KAYNAK_ET[kaynak] || kaynak}</p>
    </div>
  )
}

function logRenk(s: string): string {
  if (/error|fail|fatal|denied|refused|panic|segfault/i.test(s)) return 'text-red-400'
  if (/warn/i.test(s)) return 'text-amber-400'
  if (/notice|started|listening|active/i.test(s)) return 'text-sky-400'
  return 'text-slate-300'
}

function Kart({ baslik, children, sag }: { baslik: string; children: React.ReactNode; sag?: React.ReactNode }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
        {sag && <div className="text-xs text-slate-500 dark:text-slate-500">{sag}</div>}
      </div>
      {children}
    </div>
  )
}
function Snap({ baslik, deger, alt, renk }: { baslik: string; deger: string; alt?: string; renk: string }) {
  const m: Record<string, string> = {
    indigo: 'border-indigo-200 dark:border-indigo-800 bg-indigo-50 dark:bg-indigo-900/20',
    emerald: 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20',
    amber: 'border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20',
    violet: 'border-violet-200 dark:border-violet-800 bg-violet-50 dark:bg-violet-900/20',
  }
  return (
    <div className={`border rounded-2xl p-3 ${m[renk]}`}>
      <div className="text-xs text-slate-500 dark:text-slate-500 uppercase tracking-wider">{baslik}</div>
      <div className="text-2xl font-bold text-slate-900 dark:text-slate-100 mt-1 font-mono">{deger}</div>
      {alt && <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-0.5">{alt}</div>}
    </div>
  )
}
function Lej({ renk, et }: { renk: string; et: string }) {
  return <span className="flex items-center gap-1.5 text-slate-600 dark:text-slate-400 dark:text-slate-500"><span className={`w-3 h-3 rounded ${renk}`}></span>{et}</span>
}

function CokSeriliGrafik({
  noktalar, alanlar, yMaks, ekstra,
}: {
  noktalar: any[]; alanlar: { anahtar: string; renk: string }[]; yMaks: number; ekstra?: string
}) {
  const W = 1000, H = 180, P = 8
  const innerW = W - P * 2, innerH = H - P * 2
  if (noktalar.length < 2) {
    return <div className="text-xs text-slate-400 dark:text-slate-500 italic py-12 text-center">Veri toplanıyor…</div>
  }
  function path(anahtar: string) {
    return noktalar.map((n, i) => {
      const x = P + (innerW * i) / Math.max(1, MAX_NOKTA - 1)
      const v = Math.max(0, Math.min(yMaks, n[anahtar] || 0))
      const y = P + innerH - (v / yMaks) * innerH
      return (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1)
    }).join(' ')
  }
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-[180px]" preserveAspectRatio="none">
      {/* grid */}
      {[0, 25, 50, 75, 100].map(p => {
        const y = P + innerH - (p / 100) * innerH
        return <line key={p} x1={P} y1={y} x2={W - P} y2={y} stroke="#f1f5f9" strokeWidth="1" />
      })}
      {alanlar.map(a => (
        <path key={a.anahtar} d={path(a.anahtar)} stroke={a.renk} strokeWidth="2" fill="none" />
      ))}
      {/* y axis labels */}
      <text x={P + 2} y={P + 10} fontSize="9" fill="#94a3b8">{yMaks.toFixed(0)}{ekstra || ''}</text>
      <text x={P + 2} y={H - P + 1} fontSize="9" fill="#94a3b8">0</text>
    </svg>
  )
}

function SaglikKart({ h }: { h: Health }) {
  const ok = h.erisilebilir && h.durum_kodu >= 200 && h.durum_kodu < 400
  return (
    <div className={`rounded-2xl p-4 border ${ok ? 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20' : 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20'}`}>
      <div className="flex items-center gap-2 mb-2">
        <span className={`w-2.5 h-2.5 rounded-full ${ok ? 'bg-emerald-500 animate-pulse' : 'bg-red-500'}`}></span>
        <span className={`text-sm font-semibold ${ok ? 'text-emerald-800 dark:text-emerald-200' : 'text-red-800 dark:text-red-200'}`}>
          {ok ? 'Erişilebilir' : 'Erişilemez'}
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
      <div className="rounded-2xl p-4 border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20">
        <div className="text-sm font-semibold text-amber-800 dark:text-amber-200 mb-2">⚠ SSL Yok</div>
        <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">Bu domain HTTPS üzerinden erişilemiyor.</div>
      </div>
    )
  }
  const krit = !ssl.gecerli || ssl.kalan_gun < 15
  return (
    <div className={`rounded-2xl p-4 border ${krit ? 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20' : 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20'}`}>
      <div className={`text-sm font-semibold mb-2 ${krit ? 'text-red-800 dark:text-red-200' : 'text-emerald-800 dark:text-emerald-200'}`}>
        {ssl.gecerli ? '🔒 SSL Geçerli' : '✗ SSL Geçersiz'}
      </div>
      <div className="text-2xl font-bold text-slate-900 dark:text-slate-100 font-mono">{ssl.kalan_gun}<span className="text-base ml-1 text-slate-500 dark:text-slate-500">gün</span></div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-1">Bitiş: <span className="font-mono">{ssl.bitis_tarihi}</span></div>
      {ssl.cikaran && <div className="text-[11px] text-slate-500 dark:text-slate-500">Çıkaran: <span className="font-mono">{ssl.cikaran}</span></div>}
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
    <div className={`rounded-2xl p-4 border ${m[renk]}`}>
      <div className="text-sm font-semibold mb-2">Yanıt Süresi</div>
      <div className="text-3xl font-bold text-slate-900 dark:text-slate-100 font-mono">{ms.toFixed(0)}<span className="text-base ml-1 text-slate-500 dark:text-slate-500">ms</span></div>
      <div className="text-[11px] text-slate-500 dark:text-slate-500 mt-1">
        {ms < 300 ? 'Hızlı' : ms < 1000 ? 'Kabul edilebilir' : 'Yavaş'} · {h.sema.toUpperCase()}
      </div>
    </div>
  )
}

function fmtByte(b: number): string {
  if (b < 1024) return b + ' B'
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + ' KB'
  if (b < 1024 * 1024 * 1024) return (b / 1024 / 1024).toFixed(1) + ' MB'
  if (b < 1024 * 1024 * 1024 * 1024) return (b / 1024 / 1024 / 1024).toFixed(2) + ' GB'
  return (b / 1024 / 1024 / 1024 / 1024).toFixed(2) + ' TB'
}