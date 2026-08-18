import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Website Security Monitor — admin.
 *
 * Panel'deki tüm web uygulamalarını 6 saatte bir tarar. Sayfa: sayaç kartları,
 * durum, filtre, ARAMA, SAYFALAMA, TEKİL TARAMA. Yalnız admin.
 *
 * Sayfa genişliği tam kapsayıcıyı kullanır — uzun domainlere ve tabloya yer.
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

type Bulgu = {
  id: number
  domain_id: number
  alan_adi: string
  app_type: string
  package_name: string
  installed_version: string
  cve_id: string
  severity: string
  cvss: number
  title: string
  fixed_in: string
  source: string
  last_seen: string
}

type Sayfa = {
  toplam: number
  sayfa: number
  sayfa_boyut: number
  items: Bulgu[]
}

// Taranan uygulama envanteri.
// 🔴 NEDEN VAR: sayfa bugüne kadar YALNIZCA bulguları listeliyordu. Açığı
// olmayan bir domain hiçbir yerde görünmüyordu; ekranda "her şey güvenli" ile
// "tarayıcı hiç çalışmadı" AYNI görünüyordu. Müşteri "2 domainim var ama
// listede yok" dedi — tarayıcı aslında çalışıyordu. Envanter, boş bulgu
// listesini KANITLI iyi habere çevirir.
type Uygulama = {
  domain_id: number
  alan_adi: string
  app_type: string
  install_path: string
  app_version: string
  paket_sayisi: number
  bulgu_sayisi: number
  son_tarama: string
}

type EnvanterYanit = {
  toplam: number
  items: Uygulama[]
  // Panelde kayıtlı ama envanterde HİÇ görünmeyen domain'ler.
  // Bu listenin dolu olması bir UYARIDIR: o siteler taranmamış demektir.
  taranmayan_domainler: string[]
}

const SEV_RENK: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300',
  high:     'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
  medium:   'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  low:      'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
}

// Ekosistem başına renk + görünen ad. Backend'in "app_type" değerine göre.
const APP_META: Record<string, { ad: string; renk: string }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'bg-blue-100 text-blue-800 dark:bg-blue-950/40 dark:text-blue-300' },
  'nodejs':       { ad: 'Node.js',      renk: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' },
  'php-composer': { ad: 'PHP Composer', renk: 'bg-violet-100 text-violet-800 dark:bg-violet-950/40 dark:text-violet-300' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? t
const appRenk = (t: string) => APP_META[t]?.renk ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'

// Basit debounce — arama input'u her tuşta API'ye vurmasın.
function useDebouncedValue<T>(v: T, ms: number): T {
  const [d, setD] = useState(v)
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms)
    return () => clearTimeout(t)
  }, [v, ms])
  return d
}

export default function WebsiteSecurityPage() {
  const [status, setStatus] = useState<Status | null>(null)
  const [sayfa, setSayfa] = useState<Sayfa>({ toplam: 0, sayfa: 1, sayfa_boyut: 50, items: [] })
  const [envanter, setEnvanter] = useState<EnvanterYanit | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taranıyor, setTaranıyor] = useState(false)

  // Filtre + arama + sayfalama
  const [filtre, setFiltre] = useState<string>('')
  const [appFiltre, setAppFiltre] = useState<string>('') // '' | 'wordpress' | 'nodejs' | 'php-composer'
  const [arama, setArama] = useState<string>('')
  const aramaDebounce = useDebouncedValue(arama, 300)
  const [aktifSayfa, setAktifSayfa] = useState(1)
  const [sayfaBoyut, setSayfaBoyut] = useState(50)

  // Satır seçimi — bulgu id'leri (domain'ler bulgulardan türetilir)
  const [seciliBulgu, setSeciliBulgu] = useState<Set<number>>(new Set())

  const dialog = useDialog()
  const ilkYuklendi = useRef(false)

  // Filtre/arama değiştiğinde 1. sayfaya sıfırla
  useEffect(() => { if (ilkYuklendi.current) setAktifSayfa(1) }, [filtre, appFiltre, aramaDebounce, sayfaBoyut])

  const yukle = async () => {
    if (ilkYuklendi.current) setYukleniyor(true)
    setHata(null)
    try {
      const params = new URLSearchParams({
        page: String(aktifSayfa),
        page_size: String(sayfaBoyut),
      })
      if (filtre) params.set('severity', filtre)
      if (appFiltre) params.set('app_type', appFiltre)
      if (aramaDebounce) params.set('q', aramaDebounce)
      const [s, p, env] = await Promise.all([
        api.get<Status>('/websec/status'),
        api.get<Sayfa>('/websec/findings?' + params.toString()),
        api.get<EnvanterYanit>('/websec/apps'),
      ])
      setStatus(s.data)
      // Backend'in beklenen şemayı güvenli aldığımızdan emin ol
      setSayfa({
        toplam: p.data.toplam ?? 0,
        sayfa: p.data.sayfa ?? 1,
        sayfa_boyut: p.data.sayfa_boyut ?? sayfaBoyut,
        items: Array.isArray(p.data.items) ? p.data.items : [],
      })
      setEnvanter(env.data)
      ilkYuklendi.current = true
    } catch (e) {
      setHata(apiHata(e, 'Yüklenemedi'))
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { void yukle() }, [filtre, appFiltre, aramaDebounce, aktifSayfa, sayfaBoyut])

  // Tarama aktifse 5sn'de bir tazele
  useEffect(() => {
    if (!status?.running) return
    const t = setInterval(yukle, 5000)
    return () => clearInterval(t)
  }, [status?.running])

  const yenidenTara = async () => {
    setTaranıyor(true)
    try {
      await api.post('/websec/rescan', {})
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Başlatılamadı', mesaj: apiHata(e, 'Rescan hatası') })
    } finally { setTaranıyor(false) }
  }

  // Seçili bulgulardan UNIQUE domain kümesini çıkar. Aynı domain'de 5 bulgu
  // seçmek 5 tarama tetiklemesin — o domain 1 kez taransın.
  const seciliDomainSayisi = useMemo(() => {
    const doms = new Set<number>()
    for (const b of sayfa.items) if (seciliBulgu.has(b.id)) doms.add(b.domain_id)
    return doms.size
  }, [seciliBulgu, sayfa.items])

  const seciliTara = async () => {
    const doms = new Set<number>()
    for (const b of sayfa.items) if (seciliBulgu.has(b.id)) doms.add(b.domain_id)
    if (doms.size === 0) return
    const ids = Array.from(doms)
    const ok = await dialog.onay({
      baslik: `${ids.length} domain'i tara?`,
      mesaj: `Seçili bulgulardan çıkan ${ids.length} benzersiz domain yeniden taranacak. Sürelik birkaç saniye ile birkaç dakika arasında.`,
      onayEtiketi: 'Tara', iptalEtiketi: 'Vazgeç',
    })
    if (!ok) return
    setTaranıyor(true)
    try {
      await api.post('/websec/rescan-many', { domain_ids: ids })
      setSeciliBulgu(new Set())
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Başlatılamadı', mesaj: apiHata(e, 'Toplu tarama hatası') })
    } finally { setTaranıyor(false) }
  }

  // Seçim yardımcıları — Yasaklı Domain sayfasındaki desen
  const bulguSecDegistir = (id: number) => {
    setSeciliBulgu((eski) => {
      const yeni = new Set(eski)
      if (yeni.has(id)) yeni.delete(id); else yeni.add(id)
      return yeni
    })
  }
  const hepsiSecili = sayfa.items.length > 0 && sayfa.items.every((b) => seciliBulgu.has(b.id))
  const bazisiSecili = seciliBulgu.size > 0 && !hepsiSecili
  const hepsiSecDegistir = () => {
    if (hepsiSecili) {
      // Yalnız görünen sayfadakileri kaldır — başka sayfada seçili kalanları
      // kaybetme.
      setSeciliBulgu((eski) => {
        const yeni = new Set(eski)
        for (const b of sayfa.items) yeni.delete(b.id)
        return yeni
      })
    } else {
      setSeciliBulgu((eski) => {
        const yeni = new Set(eski)
        for (const b of sayfa.items) yeni.add(b.id)
        return yeni
      })
    }
  }

  const zamanFmt = (s: string | null) => {
    if (!s) return '—'
    try { return new Date(s.replace(' ', 'T')).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' }) }
    catch { return s }
  }

  const toplamSayfa = Math.max(1, Math.ceil(sayfa.toplam / sayfaBoyut))
  const ilk = sayfa.toplam === 0 ? 0 : (aktifSayfa - 1) * sayfaBoyut + 1
  const son = Math.min(aktifSayfa * sayfaBoyut, sayfa.toplam)

  // Sayfa numaraları — çok sayfa varsa akıllı kısaltma: 1 … 4 5 6 … N
  const sayfaListesi = useMemo<(number | 'e')[]>(() => {
    const n = toplamSayfa
    const c = aktifSayfa
    if (n <= 7) return Array.from({ length: n }, (_, i) => i + 1)
    const out: (number | 'e')[] = [1]
    if (c > 3) out.push('e')
    for (let i = Math.max(2, c - 1); i <= Math.min(n - 1, c + 1); i++) out.push(i)
    if (c < n - 2) out.push('e')
    out.push(n)
    return out
  }, [toplamSayfa, aktifSayfa])

  return (
    <div className="w-full px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[{ etiket: 'Website Security Monitor' }]} />

      <div className="mt-4 mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            Website Security Monitor
          </h1>
          <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
            Barındırılan tüm web uygulamalarını 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir.
          </p>
          {/* Aktif ekosistem chip'leri — backend'in dönen listesinden üretilir.
              Her chip yanında bulgu sayısı (varsa). Tıklanınca o app_type'a filtre. */}
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <span className="text-xs text-slate-500">Tarayıcılar:</span>
            {(status?.ekosistemler ?? ['wordpress']).map((e) => {
              const sayi = status?.app_sayaclari?.[e] ?? 0
              const aktif = appFiltre === e
              return (
                <button key={e} onClick={() => setAppFiltre(aktif ? '' : e)}
                  className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors ${appRenk(e)} ${aktif ? 'ring-2 ring-slate-900 dark:ring-slate-100' : 'hover:opacity-80'}`}>
                  <span>{appAd(e)}</span>
                  {sayi > 0 && <span className="rounded-full bg-white/60 px-1.5 text-[10px] tabular-nums dark:bg-black/30">{sayi}</span>}
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
          onClick={yenidenTara}
          disabled={taranıyor || status?.running}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
        >
          {status?.running ? 'Taranıyor…' : (taranıyor ? 'Başlatılıyor…' : 'Tümünü Tara')}
        </button>
      </div>

      {/* Sayaç kartları */}
      {status && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2 md:grid-cols-4">
          <SayacKart etiket="Kritik" sayi={status.critical} renk={SEV_RENK.critical} onClick={() => setFiltre('critical')} />
          <SayacKart etiket="Yüksek" sayi={status.high} renk={SEV_RENK.high} onClick={() => setFiltre('high')} />
          <SayacKart etiket="Orta" sayi={status.medium} renk={SEV_RENK.medium} onClick={() => setFiltre('medium')} />
          <SayacKart etiket="Düşük" sayi={status.low} renk={SEV_RENK.low} onClick={() => setFiltre('low')} />
        </div>
      )}

      {/* Durum */}
      {status && (
        <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <div><div className="text-xs text-slate-500">Son tarama</div><div className="mt-0.5 font-medium">{zamanFmt(status.last_run)}</div></div>
            <div><div className="text-xs text-slate-500">Son başarılı</div><div className="mt-0.5 font-medium">{zamanFmt(status.last_success)}</div></div>
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

      {/* Taranan uygulama envanteri — "bulgu yok" ile "hiç bakılmadı" ayrımı */}
      {envanter && (
        <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">
              Taranan uygulamalar
              <span className="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-xs font-normal text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                {envanter.toplam}
              </span>
            </h2>
          </div>

          {envanter.taranmayan_domainler.length > 0 && (
            <div className="mb-3 rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300">
              <b>{envanter.taranmayan_domainler.length} alan adı taranmadı:</b>{' '}
              {envanter.taranmayan_domainler.join(', ')}
              <div className="mt-1 opacity-80">
                Bu alan adlarında desteklenen bir uygulama (WordPress, Node.js, Composer) bulunamadı
                ya da tarama henüz ulaşmadı.
              </div>
            </div>
          )}

          {envanter.items.length === 0 ? (
            <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-600 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400">
              Henüz taranmış uygulama yok. Tarama hiç çalışmadıysa yukarıdaki
              <b> Yeniden tara</b> düğmesini kullanın.
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-[720px] w-full text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                  <tr>
                    <th className="px-3 py-2">Alan adı</th>
                    <th className="px-3 py-2">Tür</th>
                    <th className="px-3 py-2">Sürüm</th>
                    <th className="px-3 py-2 text-right">Paket</th>
                    <th className="px-3 py-2 text-right">Bulgu</th>
                    <th className="px-3 py-2">Son tarama</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                  {envanter.items.map((u) => (
                    <tr key={`${u.domain_id}-${u.app_type}-${u.install_path}`}>
                      <td className="px-3 py-2 font-medium">{u.alan_adi || `#${u.domain_id}`}</td>
                      <td className="px-3 py-2">
                        <span className={`rounded px-1.5 py-0.5 text-xs ${appRenk(u.app_type)}`}>
                          {appAd(u.app_type)}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-slate-600 dark:text-slate-400">{u.app_version || '—'}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{u.paket_sayisi}</td>
                      <td className="px-3 py-2 text-right tabular-nums">
                        {u.bulgu_sayisi > 0 ? (
                          <span className="rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-800 dark:bg-red-950/40 dark:text-red-300">
                            {u.bulgu_sayisi}
                          </span>
                        ) : (
                          <span className="text-emerald-600 dark:text-emerald-400">temiz</span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-xs text-slate-500">{u.son_tarama}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Kontrol çubuğu — arama + filtre + sayfa boyutu */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[220px] max-w-md">
          <input
            value={arama}
            onChange={(e) => setArama(e.target.value)}
            placeholder="Ara: domain, paket, CVE, başlık…"
            className="w-full rounded-lg border border-slate-300 bg-white pl-9 pr-8 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
          <svg viewBox="0 0 24 24" className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-slate-400" fill="none" stroke="currentColor" strokeWidth="2">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
          </svg>
          {arama && (
            <button onClick={() => setArama('')} aria-label="Temizle" className="absolute right-2 top-2 rounded p-1 text-slate-400 hover:text-slate-700 dark:hover:text-slate-200">
              ✕
            </button>
          )}
        </div>
        <div className="flex items-center gap-1 text-xs">
          <span className="text-slate-500 mr-1">Filtre:</span>
          {[['', 'Hepsi'], ['critical', 'Kritik'], ['high', 'Yüksek'], ['medium', 'Orta'], ['low', 'Düşük']].map(([v, ad]) => (
            <button key={v} onClick={() => setFiltre(v)}
              className={`rounded-md border px-2 py-1 ${filtre === v ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800'}`}>
              {ad}
            </button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2 text-xs text-slate-500">
          <span>Sayfa boyutu:</span>
          <select value={sayfaBoyut} onChange={(e) => setSayfaBoyut(Number(e.target.value))}
            className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs dark:border-slate-700 dark:bg-slate-950">
            <option value={20}>20</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
            <option value={200}>200</option>
          </select>
        </div>
      </div>

      {/* Bulgu tablosu */}
      {yukleniyor && !ilkYuklendi.current ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata} <button onClick={yukle} className="underline">Tekrar dene</button></div>
      ) : sayfa.items.length === 0 ? (
        <div className="rounded-2xl border border-emerald-200 bg-emerald-50 py-10 text-center text-sm text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300">
          {status && status.total_findings === 0 ? 'Bilinen zafiyet bulunmadı.' : (arama || filtre ? 'Bu arama/filtreyle bulgu yok.' : 'Bulgu yok.')}
        </div>
      ) : (
        <>
        {/* Seçim çubuğu — bir şey seçildiğinde görünür */}
        {seciliBulgu.size > 0 && (
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-slate-300 bg-slate-50 px-4 py-2.5 dark:border-slate-700 dark:bg-slate-900">
            <div className="text-sm text-slate-700 dark:text-slate-300">
              <span className="font-semibold">{seciliBulgu.size}</span> bulgu seçili
              <span className="ml-2 text-xs text-slate-500">({seciliDomainSayisi} benzersiz domain)</span>
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setSeciliBulgu(new Set())}
                className="rounded-md px-2.5 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                Seçimi temizle
              </button>
              <button
                onClick={seciliTara}
                disabled={taranıyor || status?.running}
                className="rounded-md bg-slate-900 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
              >
                Seçilenleri Tara ({seciliDomainSayisi} domain)
              </button>
            </div>
          </div>
        )}
        <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
          <div className="overflow-x-auto">
            <table className="min-w-[900px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th className="w-10 px-3 py-3">
                    <input
                      type="checkbox"
                      aria-label="Sayfadakileri seç"
                      checked={hepsiSecili}
                      ref={(el) => { if (el) el.indeterminate = bazisiSecili }}
                      onChange={hepsiSecDegistir}
                      className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
                    />
                  </th>
                  <th className="w-32 px-3 py-3 font-semibold">Şiddet</th>
                  <th className="px-3 py-3 font-semibold">Alan</th>
                  <th className="px-3 py-3 font-semibold">Paket</th>
                  <th className="w-40 px-3 py-3 font-semibold">Kurulu / Fix</th>
                  <th className="w-40 px-3 py-3 font-semibold">CVE</th>
                  <th className="px-3 py-3 font-semibold">Başlık</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {sayfa.items.map((b) => (
                  <tr key={b.id} className={seciliBulgu.has(b.id) ? 'bg-slate-50 dark:bg-slate-900/60' : 'bg-white dark:bg-slate-950'}>
                    <td className="px-3 py-2.5">
                      <input
                        type="checkbox"
                        aria-label={`${b.package_name} bulgusunu seç`}
                        checked={seciliBulgu.has(b.id)}
                        onChange={() => bulguSecDegistir(b.id)}
                        className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
                      />
                    </td>
                    <td className="px-3 py-2.5">
                      {/* whitespace-nowrap — "critical · 9.8" iki satıra kırılmasın */}
                      <span className={`inline-block whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium ${SEV_RENK[b.severity] || SEV_RENK.low}`}>
                        {b.severity || '—'}{b.cvss > 0 ? ' · ' + b.cvss.toFixed(1) : ''}
                      </span>
                    </td>
                    <td className="px-3 py-2.5 break-words">
                      <Link to={`/abonelikler/${b.domain_id}`} className="font-medium text-slate-900 hover:underline dark:text-slate-100">
                        {b.alan_adi || 'domain#' + b.domain_id}
                      </Link>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="font-mono text-[13px] break-all">{b.package_name}</div>
                      <span className={`mt-0.5 inline-block whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium ${appRenk(b.app_type)}`}>
                        {appAd(b.app_type)}
                      </span>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[13px] whitespace-nowrap">
                      <span className="text-slate-900 dark:text-slate-100">{b.installed_version}</span>
                      {b.fixed_in && <span className="ml-1 text-emerald-600 dark:text-emerald-400">→ {b.fixed_in}</span>}
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12px] text-slate-700 dark:text-slate-300 whitespace-nowrap">{b.cve_id}</td>
                    <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">{b.title}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Sayfalama kontrolü */}
          {toplamSayfa > 1 && (
            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-100 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
              <div>
                {ilk}–{son} / <span className="font-medium">{sayfa.toplam}</span> bulgu
              </div>
              <div className="flex items-center gap-1">
                <button onClick={() => setAktifSayfa(Math.max(1, aktifSayfa - 1))} disabled={aktifSayfa <= 1}
                  className="rounded border border-slate-300 bg-white px-2 py-1 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-950">‹</button>
                {sayfaListesi.map((n, i) => n === 'e' ? (
                  <span key={'e' + i} className="px-1">…</span>
                ) : (
                  <button key={n} onClick={() => setAktifSayfa(n)}
                    className={`rounded border px-2 py-1 ${n === aktifSayfa ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950'}`}>
                    {n}
                  </button>
                ))}
                <button onClick={() => setAktifSayfa(Math.min(toplamSayfa, aktifSayfa + 1))} disabled={aktifSayfa >= toplamSayfa}
                  className="rounded border border-slate-300 bg-white px-2 py-1 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-950">›</button>
              </div>
            </div>
          )}
        </div>
        </>
      )}

      <p className="mt-4 text-xs text-slate-500">
        Veri kaynakları: WordPress → wpvulnerability.net · Node.js / PHP Composer → osv.dev · Yeni bulgular her 6 saatte bir güncellenir
      </p>
    </div>
  )
}

function SayacKart({ etiket, sayi, renk, onClick }: { etiket: string; sayi: number; renk: string; onClick?: () => void }) {
  return (
    <button
      onClick={onClick}
      type="button"
      className="rounded-2xl border border-slate-200 bg-white p-4 text-left transition-colors hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900 dark:hover:border-slate-700"
    >
      <div className="text-xs font-medium uppercase tracking-wider text-slate-500">{etiket}</div>
      <div className="mt-1 flex items-baseline gap-2">
        <div className={`rounded-md px-2 py-0.5 text-2xl font-semibold ${renk}`}>{sayi}</div>
      </div>
    </button>
  )
}
