import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Website Security Monitor — admin.
 *
 * Panel'deki TÜM domainleri 6 saatte bir tarar. Bu sayfa artık DOMAIN ODAKLI:
 * her domain bir satır (durum: taranıyor / N açık / temiz / uygulama yok / beklemede).
 * Açıkların kendisi domaine ÖZEL sayfada listelenir (/website-security/domain/:id)
 * — böylece domain listesi ile açık listesi artık alt alta yığılmaz.
 */

type Status = {
  running: boolean
  last_run: string | null
  last_success: string | null
  total_findings: number
  critical: number; high: number; medium: number; low: number
  scanned_apps: number
  duration_ms: number
  last_error: string | null
  next_estimate: string
  ekosistemler?: string[] | null
  app_sayaclari?: Record<string, number> | null
}

// Panel'deki her domain'in güvenlik durumu.
type Uygulama = {
  domain_id: number
  alan_adi: string
  app_type: string
  install_path: string
  app_version: string
  paket_sayisi: number
  bulgu_sayisi: number
  son_tarama: string
  durum: string // taraniyor | acik | temiz | desteklenmiyor | beklemede
}

type EnvanterYanit = {
  toplam: number
  items: Uygulama[]
  desteklenmeyen: number
}

const SEV_RENK: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300',
  high:     'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
  medium:   'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  low:      'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
}

const APP_META: Record<string, { ad: string; renk: string }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'bg-blue-100 text-blue-800 dark:bg-blue-950/40 dark:text-blue-300' },
  'nodejs':       { ad: 'Node.js',      renk: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' },
  'php-composer': { ad: 'PHP Composer', renk: 'bg-violet-100 text-violet-800 dark:bg-violet-950/40 dark:text-violet-300' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? (t || '—')
const appRenk = (t: string) => APP_META[t]?.renk ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'

// Durum rozetleri — domain başına.

const WSEC_EN: Record<string, string> = {
  "Domain bulunamadı.": "Domain not found.",
  "Rescan hatası": "Rescan error",
  "Son başarılı": "Last successful",
  "Tarama hatası": "Scan error",
  "Tümünü Tara": "Scan All",
  "Tür:": "Type:",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WSEC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function DurumRozet({ u }: { u: Uygulama }) {
  switch (u.durum) {
    case 'taraniyor':
      return (
        <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-100 px-2 py-0.5 text-[11px] font-medium text-blue-800 dark:bg-blue-950/40 dark:text-blue-300">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" /> {cevir("Taranıyor…")}
        </span>
      )
    case 'acik':
      return (
        <span className="rounded-full bg-red-100 px-2 py-0.5 text-[11px] font-semibold text-red-800 dark:bg-red-950/40 dark:text-red-300">
          {u.bulgu_sayisi} açık
        </span>
      )
    case 'temiz':
      return (
        <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-medium text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">
          Temiz
        </span>
      )
    case 'desteklenmiyor':
      return (
        <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500 dark:bg-slate-800 dark:text-slate-400">
          Uygulama yok
        </span>
      )
    default: // beklemede
      return (
        <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">
          Beklemede
        </span>
      )
  }
}

export default function WebsiteSecurityPage() {
  useTranslation() // dil re-render aboneligi
  const [status, setStatus] = useState<Status | null>(null)
  const [envanter, setEnvanter] = useState<EnvanterYanit | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taranıyor, setTaranıyor] = useState(false)
  const [appFiltre, setAppFiltre] = useState<string>('') // domain tablosunu ekosisteme göre süz
  const [arama, setArama] = useState<string>('')
  const [tarananDomain, setTarananDomain] = useState<number | null>(null)

  const dialog = useDialog()
  const nav = useNavigate()
  const ilkYuklendi = useRef(false)

  const yukle = async () => {
    if (!ilkYuklendi.current) setYukleniyor(true)
    setHata(null)
    try {
      const [s, env] = await Promise.all([
        api.get<Status>('/websec/status'),
        api.get<EnvanterYanit>('/websec/apps'),
      ])
      setStatus(s.data)
      setEnvanter(env.data)
      ilkYuklendi.current = true
    } catch (e) {
      setHata(apiHata(e, cevir("Yüklenemedi")))
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { void yukle() }, [])

  // Tarama aktifse 5sn'de bir tazele — domain durumları canlı güncellensin.
  useEffect(() => {
    const canli = status?.running || (envanter?.items.some((u) => u.durum === 'taraniyor') ?? false)
    if (!canli) return
    const t = setInterval(yukle, 5000)
    return () => clearInterval(t)
  }, [status?.running, envanter])

  const tumunuTara = async () => {
    setTaranıyor(true)
    try {
      await api.post('/websec/rescan', {})
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Rescan hatası")) })
    } finally { setTaranıyor(false) }
  }

  const domainTara = async (id: number) => {
    setTarananDomain(id)
    try {
      await api.post('/websec/rescan?domain_id=' + id, {})
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Tarama hatası")) })
    } finally { setTarananDomain(null) }
  }

  const zamanFmt = (s: string | null) => {
    if (!s) return '—'
    try { return new Date(s.replace(' ', 'T') + (s.includes(' ') ? 'Z' : '')).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' }) }
    catch { return s }
  }

  const listelenen = useMemo(() => {
    let arr = envanter?.items ?? []
    if (appFiltre) arr = arr.filter((u) => u.app_type === appFiltre)
    const q = arama.trim().toLowerCase()
    if (q) arr = arr.filter((u) => (u.alan_adi || '').toLowerCase().includes(q))
    return arr
  }, [envanter, appFiltre, arama])

  const detayaGit = (u: Uygulama) => {
    // App'i olan domain'in detay (açık) sayfası anlamlı; app yoksa gitme.
    if (u.app_type) nav('/website-security/domain/' + u.domain_id)
  }

  return (
    <div className="w-full px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[{ etiket: 'Website Security Monitor' }]} />

      <div className="mt-4 mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            Website Security Monitor
          </h1>
          <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
            {cevir(cevir("Barındırılan tüm domainleri 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir."))}
            {cevir(cevir("Bir domainin açıkları için satırına tıklayın."))}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <span className="text-xs text-slate-500">{cevir("Tür:")}</span>
            {(status?.ekosistemler ?? ['wordpress', 'nodejs', 'php-composer']).map((e) => {
              const aktif = appFiltre === e
              return (
                <button key={e} onClick={() => setAppFiltre(aktif ? '' : e)}
                  className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors ${appRenk(e)} ${aktif ? 'ring-2 ring-slate-900 dark:ring-slate-100' : 'hover:opacity-80'}`}>
                  {appAd(e)}
                </button>
              )
            })}
            {appFiltre && (
              <button onClick={() => setAppFiltre('')} className="text-[11px] text-slate-500 underline hover:text-slate-700 dark:hover:text-slate-300">
                filtre temizle
              </button>
            )}
          </div>
        </div>
        <button
          onClick={tumunuTara}
          disabled={taranıyor || status?.running}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
        >
          {status?.running ? 'Taranıyor…' : (taranıyor ? 'Başlatılıyor…' : cevir("Tümünü Tara"))}
        </button>
      </div>

      {/* Sayaç kartları — global şiddet toplamları (görüntü) */}
      {status && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2 md:grid-cols-4">
          <SayacKart etiket="Kritik" sayi={status.critical} renk={SEV_RENK.critical} />
          <SayacKart etiket={cevir("Yüksek")} sayi={status.high} renk={SEV_RENK.high} />
          <SayacKart etiket="Orta" sayi={status.medium} renk={SEV_RENK.medium} />
          <SayacKart etiket={cevir("Düşük")} sayi={status.low} renk={SEV_RENK.low} />
        </div>
      )}

      {/* Durum */}
      {status && (
        <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <div><div className="text-xs text-slate-500">Son tarama</div><div className="mt-0.5 font-medium">{zamanFmt(status.last_run)}</div></div>
            <div><div className="text-xs text-slate-500">{cevir("Son başarılı")}</div><div className="mt-0.5 font-medium">{zamanFmt(status.last_success)}</div></div>
            <div><div className="text-xs text-slate-500">Taranan uygulama</div><div className="mt-0.5 font-medium">{status.scanned_apps}</div></div>
            <div><div className="text-xs text-slate-500">Toplam bulgu</div><div className="mt-0.5 font-medium">{status.total_findings}</div></div>
          </div>
          {status.last_error && (
            <div className="mt-3 rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
              Son hata: {status.last_error}
            </div>
          )}
        </div>
      )}

      {/* Domain arama */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[220px] max-w-md">
          <input
            value={arama}
            onChange={(e) => setArama(e.target.value)}
            placeholder="Domain ara…"
            className="w-full rounded-lg border border-slate-300 bg-white pl-9 pr-8 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
          <svg viewBox="0 0 24 24" className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-slate-400" fill="none" stroke="currentColor" strokeWidth="2">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
          </svg>
        </div>
        {envanter && (
          <div className="ml-auto text-xs text-slate-500">
            {envanter.toplam} domain{envanter.desteklenmeyen > 0 ? ` · ${envanter.desteklenmeyen} uygulama yok` : ''}
          </div>
        )}
      </div>

      {/* Domain tablosu — TEK ana tablo (açık listesi ayrı sayfada) */}
      {yukleniyor && !ilkYuklendi.current ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata} <button onClick={yukle} className="underline">Tekrar dene</button></div>
      ) : listelenen.length === 0 ? (
        <div className="rounded-2xl border border-slate-200 bg-slate-50 py-10 text-center text-sm text-slate-500 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400">
          {arama || appFiltre ? 'Bu arama/filtreyle domain yok.' : cevir("Domain bulunamadı.")}
        </div>
      ) : (
        <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
          <div className="overflow-x-auto">
            <table className="min-w-[820px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th className="px-3 py-3 font-semibold">{cevir("Alan adı")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Tür")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Sürüm")}</th>
                  <th className="px-3 py-3 text-right font-semibold">Paket</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Durum")}</th>
                  <th className="px-3 py-3 font-semibold">Son tarama</th>
                  <th className="px-3 py-3 text-right font-semibold">{cevir("İşlem")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {listelenen.map((u) => {
                  const tiklanabilir = !!u.app_type
                  return (
                    <tr key={`${u.domain_id}-${u.app_type}-${u.install_path}`}
                      onClick={() => detayaGit(u)}
                      className={`bg-white dark:bg-slate-950 ${tiklanabilir ? 'cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-900/60' : ''}`}>
                      <td className="px-3 py-2.5 font-medium text-slate-900 dark:text-slate-100">{u.alan_adi || `#${u.domain_id}`}</td>
                      <td className="px-3 py-2.5">
                        {u.app_type
                          ? <span className={`rounded px-1.5 py-0.5 text-xs ${appRenk(u.app_type)}`}>{appAd(u.app_type)}</span>
                          : <span className="text-slate-400">—</span>}
                      </td>
                      <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">{u.app_version || '—'}</td>
                      <td className="px-3 py-2.5 text-right tabular-nums">{u.paket_sayisi || '—'}</td>
                      <td className="px-3 py-2.5"><DurumRozet u={u} /></td>
                      <td className="px-3 py-2.5 text-xs text-slate-500">{u.son_tarama || '—'}</td>
                      <td className="px-3 py-2.5 text-right">
                        <button
                          onClick={(e) => { e.stopPropagation(); void domainTara(u.domain_id) }}
                          disabled={status?.running || tarananDomain === u.domain_id || u.durum === 'taraniyor'}
                          className="rounded-md border border-slate-300 px-2 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
                        >
                          {tarananDomain === u.domain_id ? '…' : 'Tara'}
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <p className="mt-4 text-xs text-slate-500">
        {cevir(cevir("Veri kaynakları: WordPress → wpvulnerability.net · Node.js / PHP Composer → osv.dev · Yeni bulgular her 6 saatte bir güncellenir"))}
      </p>
    </div>
  )
}

function SayacKart({ etiket, sayi, renk }: { etiket: string; sayi: number; renk: string }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-4 text-left dark:border-slate-800 dark:bg-slate-900">
      <div className="text-xs font-medium uppercase tracking-wider text-slate-500">{etiket}</div>
      <div className="mt-1 flex items-baseline gap-2">
        <div className={`rounded-md px-2 py-0.5 text-2xl font-semibold ${renk}`}>{sayi}</div>
      </div>
    </div>
  )
}
