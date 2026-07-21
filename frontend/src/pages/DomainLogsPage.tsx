// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
type LogDosya = { anahtar: string; etiket: string; yol: string; boyut_b: number; degisme: string; mevcut: boolean }
type ReadResp = { dosya: string; yol: string; satirlar: string[]; mevcut: boolean }

const MAX_PENCERE = 1000

export default function DomainLogsPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [dosyalar, setDosyalar] = useState<LogDosya[]>([])
  const [aktif, setAktif] = useState<string>('access')
  const [satirlar, setSatirlar] = useState<string[]>([])
  const [canli, setCanli] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [otoScroll, setOtoScroll] = useState(true)
  const [gorunum, setGorunum] = useState<'tablo' | 'ham'>('tablo')
  const [arama, setArama] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  const hataTab = aktif === 'error' || aktif.includes('error') || aktif.includes('hata')

  // İstemci tarafı arama: ham satırda büyük/küçük harf duyarsız substring
  const gorunen = useMemo(() => {
    const q = arama.trim().toLowerCase()
    if (!q) return satirlar
    return satirlar.filter(l => l.toLowerCase().includes(q))
  }, [satirlar, arama])

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
    api.get<LogDosya[]>(`/domains/${id}/logs`).then(r => setDosyalar(r.data)).catch(e => setHata(apiHata(e)))
  }, [id])

  // Aktif dosya değişince son N satırı yükle
  async function ilkYukle() {
    if (!id || !aktif) return
    try {
      const { data } = await api.get<ReadResp>(`/domains/${id}/logs/oku`, { params: { dosya: aktif, son: 200 } })
      setSatirlar(data.satirlar || [])
      setHata(null)
    } catch (e) {
      setHata(apiHata(e))
    }
  }
  useEffect(() => {
    setCanli(false)
    ilkYukle()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [aktif, id])

  // Canlı tail başlat/durdur
  useEffect(() => {
    if (!canli || !id) return
    setSatirlar([]) // canlı başlangıçta tail kendi 200 satırı zaten gönderiyor
    const ctrl = new AbortController()
    abortRef.current = ctrl
    const tok = localStorage.getItem('gosp.token') || ''

    ;(async () => {
      try {
        const res = await fetch(`/api/v1/domains/${id}/logs/canli?dosya=${aktif}`, {
          headers: { Authorization: `Bearer ${tok}` },
          signal: ctrl.signal,
        })
        if (!res.ok || !res.body) {
          setHata(`stream başlamadı (HTTP ${res.status})`)
          setCanli(false)
          return
        }
        const reader = res.body.getReader()
        const dec = new TextDecoder()
        let buf = ''
        while (true) {
          const { value, done } = await reader.read()
          if (done) break
          buf += dec.decode(value, { stream: true })
          // SSE event parse: "\n\n" ile ayır
          let idxBlk
          while ((idxBlk = buf.indexOf('\n\n')) >= 0) {
            const blk = buf.slice(0, idxBlk)
            buf = buf.slice(idxBlk + 2)
            const dataLines = blk.split('\n').filter(l => l.startsWith('data: ')).map(l => l.slice(6))
            if (dataLines.length === 0) continue
            const line = dataLines.join('\n')
            setSatirlar(prev => {
              const next = [...prev, line]
              return next.length > MAX_PENCERE ? next.slice(-MAX_PENCERE) : next
            })
          }
        }
      } catch (e: any) {
        if (e.name !== 'AbortError') setHata(e.message)
      }
    })()
    return () => ctrl.abort()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canli, aktif, id])

  // Oto scroll
  useEffect(() => {
    if (!otoScroll || !scrollRef.current) return
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight
  }, [satirlar, otoScroll, gorunum])

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Günlükler' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Günlükler</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}
          <span className="font-mono">/var/log/nginx/{domain.alan_adi}.*.log</span>
        </p>
      )}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Sekmeler */}
      <div className="flex items-center flex-wrap gap-y-2 border-b border-slate-200 dark:border-slate-700 mb-3">
        {dosyalar.map(d => (
          <button
            key={d.anahtar}
            onClick={() => setAktif(d.anahtar)}
            className={`px-4 py-2.5 text-sm transition border-b-2 -mb-px ${
              aktif === d.anahtar
                ? 'border-brand-500 text-slate-900 dark:text-slate-100 font-semibold'
                : 'border-transparent text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'
            }`}
          >
            {d.etiket}
            {d.mevcut && (
              <span className="ml-2 text-[10px] font-mono text-slate-400 dark:text-slate-500">
                {formatBoyut(d.boyut_b)}
              </span>
            )}
          </button>
        ))}

        <div className="ml-auto flex flex-wrap items-center gap-2">
          {/* Görünüm toggle */}
          <div className="flex rounded-md border border-slate-200 dark:border-slate-700 overflow-hidden text-xs">
            <button
              onClick={() => setGorunum('tablo')}
              className={`px-2.5 py-1.5 font-medium transition ${gorunum === 'tablo' ? 'bg-brand-600 text-white' : 'bg-white dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'}`}
            >Tablo</button>
            <button
              onClick={() => setGorunum('ham')}
              className={`px-2.5 py-1.5 font-medium transition ${gorunum === 'ham' ? 'bg-brand-600 text-white' : 'bg-white dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'}`}
            >Ham</button>
          </div>

          <label className="text-xs text-slate-500 dark:text-slate-500 flex items-center gap-1.5 select-none cursor-pointer">
            <input type="checkbox" checked={otoScroll} onChange={e => setOtoScroll(e.target.checked)} className="rounded" />
            Otomatik kaydır
          </label>
          <button
            onClick={() => setCanli(c => !c)}
            className={`px-3 py-1.5 text-xs font-medium rounded-md transition ${
              canli
                ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 hover:bg-red-200 dark:hover:bg-red-900/50'
                : 'bg-emerald-600 text-white hover:bg-emerald-700'
            }`}
          >
            {canli ? '■ Durdur' : '▶ Canlı Takip'}
          </button>
          <button
            onClick={ilkYukle}
            disabled={canli}
            className="px-3 py-1.5 text-xs font-medium bg-white dark:bg-slate-800 hover:bg-slate-50 dark:hover:bg-slate-700 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 rounded-md transition disabled:opacity-50"
          >
            ↻ Son 200
          </button>
          <button
            onClick={() => setSatirlar([])}
            className="px-3 py-1.5 text-xs font-medium bg-white dark:bg-slate-800 hover:bg-slate-50 dark:hover:bg-slate-700 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 rounded-md transition"
          >
            Temizle
          </button>
        </div>
      </div>

      {/* Arama */}
      <div className="flex items-center gap-2 mb-2">
        <div className="relative flex-1 max-w-md">
          <svg className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-4.35-4.35M17 11a6 6 0 11-12 0 6 6 0 0112 0z" />
          </svg>
          <input
            value={arama}
            onChange={e => setArama(e.target.value)}
            placeholder="Ara — IP, yol, durum kodu, tarayıcı…"
            className="w-full pl-8 pr-8 py-1.5 text-sm bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md text-slate-800 dark:text-slate-200 placeholder:text-slate-400 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
          />
          {arama && (
            <button
              onClick={() => setArama('')}
              aria-label="Aramayı temizle"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-lg leading-none"
            >×</button>
          )}
        </div>
        {arama && (
          <span className="text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">
            {gorunen.length} / {satirlar.length} eşleşti
          </span>
        )}
      </div>

      {/* Log gövdesi */}
      <div
        ref={scrollRef}
        className="bg-slate-900 border border-slate-800 rounded-2xl overflow-auto h-[min(60vh,320px)] sm:h-[420px] lg:h-[540px]"
      >
        {satirlar.length === 0 ? (
          <div className="p-6 text-sm text-slate-500 font-mono">{canli ? 'Bekleniyor… yeni satırlar geldikçe akacak.' : '(log dosyası boş veya henüz oluşmadı)'}</div>
        ) : gorunen.length === 0 ? (
          <div className="p-6 text-sm text-slate-500 font-mono">"{arama}" aramasına uygun satır yok.</div>
        ) : gorunum === 'ham' ? (
          <div className="p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
            {gorunen.map((s, i) => (
              <div key={i} className={renkSec(s)}>{s}</div>
            ))}
          </div>
        ) : hataTab ? (
          <ErrorTablosu satirlar={gorunen} />
        ) : (
          <AccessTablosu satirlar={gorunen} />
        )}
      </div>

      <div className="mt-2 text-xs text-slate-500 dark:text-slate-500 flex items-center justify-between">
        <span>{arama ? `${gorunen.length} / ${satirlar.length} satır (filtreli)` : `${satirlar.length} satır`} · pencere {MAX_PENCERE}</span>
        {canli && <span className="text-emerald-600 dark:text-emerald-400 flex items-center gap-1.5"><span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>canlı yayın</span>}
      </div>
    </div>
  )
}

/* ---------------- Access log tablosu ---------------- */

type AccessSatir = {
  ip: string; zaman: string; method: string; yol: string; proto: string
  durum: number; boyut: string; referer: string; ua: string; ham: string
}

// nginx "combined": $remote_addr - $remote_user [$time_local] "$request" $status $bytes "$referer" "$ua"
const ACCESS_RE = /^(\S+) \S+ (\S+) \[([^\]]+)\] "([^"]*)" (\d{3}) (\S+) "([^"]*)" "([^"]*)"/

function parseAccess(line: string): AccessSatir | null {
  const m = ACCESS_RE.exec(line)
  if (!m) return null
  const req = m[4]
  const parcalar = req.split(' ')
  let method = '', yol = req, proto = ''
  if (parcalar.length >= 2) {
    method = parcalar[0]
    proto = parcalar[parcalar.length - 1]
    yol = parcalar.slice(1, -1).join(' ') || parcalar[1]
  }
  return {
    ip: m[1], zaman: m[3], method, yol, proto,
    durum: parseInt(m[5], 10), boyut: m[6], referer: m[7], ua: m[8], ham: line,
  }
}

function AccessTablosu({ satirlar }: { satirlar: string[] }) {
  const satirNesne = useMemo(() => satirlar.map(parseAccess), [satirlar])
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs border-collapse min-w-[640px]">
      <thead className="sticky top-0 z-10 bg-slate-900/95 backdrop-blur text-[10px] uppercase tracking-wider text-slate-500 border-b border-slate-800">
        <tr>
          <th className="text-left font-medium px-3 py-2 whitespace-nowrap">Zaman</th>
          <th className="text-left font-medium px-3 py-2 whitespace-nowrap">IP</th>
          <th className="text-left font-medium px-3 py-2">Method</th>
          <th className="text-left font-medium px-3 py-2 w-full">Yol</th>
          <th className="text-left font-medium px-3 py-2">Durum</th>
          <th className="text-right font-medium px-3 py-2 whitespace-nowrap">Boyut</th>
          <th className="text-left font-medium px-3 py-2">Tarayıcı</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-slate-800/70">
        {satirlar.map((ham, i) => {
          const r = satirNesne[i]
          if (!r) {
            return (
              <tr key={i}>
                <td colSpan={7} className="px-3 py-1.5 font-mono text-slate-500 break-all">{ham}</td>
              </tr>
            )
          }
          return (
            <tr key={i} className="hover:bg-slate-800/40">
              <td className="px-3 py-1.5 font-mono text-slate-400 whitespace-nowrap">{kisaZaman(r.zaman)}</td>
              <td className="px-3 py-1.5 font-mono text-slate-300 whitespace-nowrap">{r.ip}</td>
              <td className="px-3 py-1.5">
                <span className={`inline-block px-1.5 py-0.5 rounded font-mono font-semibold text-[10px] ${methodRenk(r.method)}`}>{r.method || '—'}</span>
              </td>
              <td className="px-3 py-1.5 font-mono text-slate-200 max-w-0">
                <div className="truncate" title={r.referer && r.referer !== '-' ? `${r.yol}\n← ${r.referer}` : r.yol}>{r.yol}</div>
              </td>
              <td className="px-3 py-1.5">
                <span className={`inline-block px-1.5 py-0.5 rounded font-mono font-semibold text-[10px] ${durumRenk(r.durum)}`}>{r.durum}</span>
              </td>
              <td className="px-3 py-1.5 font-mono text-slate-400 text-right whitespace-nowrap">{boyutFmt(r.boyut)}</td>
              <td className="px-3 py-1.5 text-slate-400 max-w-[220px]">
                <div className="truncate" title={r.ua}>{uaKisa(r.ua)}</div>
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
    </div>
  )
}

/* ---------------- Error log tablosu ---------------- */

// 2026/07/03 22:40:31 [error] 12345#0: *67 mesaj, client: 1.2.3.4, server: ...
const ERROR_RE = /^(\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}) \[(\w+)\] (.*)$/
const CLIENT_RE = /client: (\S+?)[,\s]/

function ErrorTablosu({ satirlar }: { satirlar: string[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs border-collapse min-w-[640px]">
      <thead className="sticky top-0 z-10 bg-slate-900/95 backdrop-blur text-[10px] uppercase tracking-wider text-slate-500 border-b border-slate-800">
        <tr>
          <th className="text-left font-medium px-3 py-2 whitespace-nowrap">Zaman</th>
          <th className="text-left font-medium px-3 py-2">Seviye</th>
          <th className="text-left font-medium px-3 py-2 whitespace-nowrap">İstemci</th>
          <th className="text-left font-medium px-3 py-2 w-full">Mesaj</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-slate-800/70">
        {satirlar.map((ham, i) => {
          const m = ERROR_RE.exec(ham)
          if (!m) {
            return (
              <tr key={i}>
                <td colSpan={4} className="px-3 py-1.5 font-mono text-slate-500 break-all">{ham}</td>
              </tr>
            )
          }
          const cm = CLIENT_RE.exec(m[3])
          const client = cm ? cm[1] : ''
          return (
            <tr key={i} className="hover:bg-slate-800/40">
              <td className="px-3 py-1.5 font-mono text-slate-400 whitespace-nowrap">{m[1].slice(5)}</td>
              <td className="px-3 py-1.5">
                <span className={`inline-block px-1.5 py-0.5 rounded font-mono font-semibold text-[10px] ${seviyeRenk(m[2])}`}>{m[2]}</span>
              </td>
              <td className="px-3 py-1.5 font-mono text-slate-300 whitespace-nowrap">{client || '—'}</td>
              <td className="px-3 py-1.5 font-mono text-slate-200 max-w-0">
                <div className="truncate" title={m[3]}>{m[3]}</div>
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
    </div>
  )
}

/* ---------------- Yardımcılar ---------------- */

function methodRenk(m: string): string {
  switch (m) {
    case 'GET': return 'bg-slate-700 text-slate-200'
    case 'POST': return 'bg-sky-900/70 text-sky-200'
    case 'PUT':
    case 'PATCH': return 'bg-amber-900/70 text-amber-200'
    case 'DELETE': return 'bg-red-900/70 text-red-200'
    case 'HEAD':
    case 'OPTIONS': return 'bg-slate-700/60 text-slate-300'
    default: return 'bg-slate-800 text-slate-400'
  }
}

function durumRenk(s: number): string {
  if (s >= 500) return 'bg-red-900/70 text-red-200'
  if (s >= 400) return 'bg-amber-900/70 text-amber-200'
  if (s >= 300) return 'bg-sky-900/70 text-sky-200'
  if (s >= 200) return 'bg-emerald-900/70 text-emerald-200'
  return 'bg-slate-700 text-slate-300'
}

function seviyeRenk(s: string): string {
  const l = s.toLowerCase()
  if (l === 'emerg' || l === 'alert' || l === 'crit' || l === 'error') return 'bg-red-900/70 text-red-200'
  if (l === 'warn') return 'bg-amber-900/70 text-amber-200'
  if (l === 'notice') return 'bg-sky-900/70 text-sky-200'
  return 'bg-slate-700 text-slate-300'
}

// 03/Jul/2026:22:40:31 +0000  ->  03/Jul 22:40:31
function kisaZaman(z: string): string {
  const m = /^(\d{2})\/(\w{3})\/\d{4}:(\d{2}:\d{2}:\d{2})/.exec(z)
  return m ? `${m[1]}/${m[2]} ${m[3]}` : z
}

function boyutFmt(b: string): string {
  const n = parseInt(b, 10)
  if (isNaN(n)) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// User-agent'ı kısa okunur özete indir
function uaKisa(ua: string): string {
  if (!ua || ua === '-') return '—'
  const bot = /(bot|crawl|spider|zgrab|curl|wget|python|go-http|scan|nikto|masscan)/i.exec(ua)
  if (bot) return `🤖 ${bot[1]}`
  let os = ''
  if (/Windows NT 10/.test(ua)) os = 'Windows'
  else if (/Mac OS X/.test(ua)) os = 'macOS'
  else if (/iPhone|iPad/.test(ua)) os = 'iOS'
  else if (/Android/.test(ua)) os = 'Android'
  else if (/Linux/.test(ua)) os = 'Linux'
  let tar = ''
  if (/Edg\//.test(ua)) tar = 'Edge'
  else if (/Chrome\//.test(ua)) tar = 'Chrome'
  else if (/Firefox\//.test(ua)) tar = 'Firefox'
  else if (/Safari\//.test(ua)) tar = 'Safari'
  const parts = [tar, os].filter(Boolean)
  return parts.length ? parts.join(' · ') : ua.slice(0, 40)
}

function renkSec(s: string): string {
  // Ham görünüm renklendirmesi
  if (/\s5\d\d\s/.test(s)) return 'text-red-400'
  if (/\s4\d\d\s/.test(s)) return 'text-amber-400'
  if (/\[error\]|\[crit\]|\[emerg\]|\[alert\]/i.test(s)) return 'text-red-400'
  if (/\[warn\]/i.test(s)) return 'text-amber-400'
  if (/\[notice\]/i.test(s)) return 'text-sky-400'
  return 'text-slate-300'
}

function formatBoyut(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}
