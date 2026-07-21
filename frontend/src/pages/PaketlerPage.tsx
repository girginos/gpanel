// gosp-dark-swept
// gosp-dark-swept-v2
import { useMemo, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Paket = {
  adi: string; surum?: string; aciklama?: string;
  kurulu: boolean; korunan: boolean
}

type Grup = { ad: string; ikon: string; paketler: string[]; aciklama: string }

type Sekme = 'ara' | 'kurulu'

/* Stroke SVG ikon seti — panelin monokrom diline uygun, emoji YOK */
const I = {
  wrench:   'M11.42 15.17 17.25 21A2.652 2.652 0 0 0 21 17.25l-5.877-5.877M11.42 15.17l2.496-3.03c.317-.384.74-.626 1.208-.766M11.42 15.17l-4.655 5.653a2.548 2.548 0 1 1-3.586-3.586l6.837-5.63m5.108-.233c.55-.164 1.163-.188 1.743-.14a4.5 4.5 0 0 0 4.486-6.336l-3.276 3.277a3.004 3.004 0 0 1-2.25-2.25l3.276-3.276a4.5 4.5 0 0 0-6.336 4.486c.091 1.076-.071 2.264-.904 2.95l-.102.085m-1.745 1.437L5.909 7.5H4.5L2.25 3.75l1.5-1.5L7.5 4.5v1.409l4.26 4.26',
  code:     'M17.25 6.75 22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3-4.5 16.5',
  bolt:     'm3.75 13.5 10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75Z',
  cube:     'm21 7.5-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9',
  beaker:   'M9.75 3.104v5.714a2.25 2.25 0 0 1-.659 1.591L5 14.5M9.75 3.104c-.251.023-.501.05-.75.082m.75-.082a24.301 24.301 0 0 1 4.5 0m0 0v5.714c0 .597.237 1.17.659 1.591L19.8 15.3M14.25 3.104c.251.023.501.05.75.082M19.8 15.3l-1.57.393A9.065 9.065 0 0 1 12 15a9.065 9.065 0 0 0-6.23-.693L5 14.5m14.8.8 1.402 1.402c1.232 1.232.65 3.318-1.067 3.611A48.309 48.309 0 0 1 12 21c-2.773 0-5.491-.235-8.135-.687-1.719-.293-2.3-2.379-1.067-3.61L5 14.5',
  cog:      'M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z',
  server:   'M5.25 14.25h13.5m-13.5 0a3 3 0 0 1-3-3m3 3a3 3 0 1 0 0 6h13.5a3 3 0 1 0 0-6m-16.5-3a3 3 0 0 1 3-3h13.5a3 3 0 0 1 3 3m-19.5 0a4.5 4.5 0 0 1 .9-2.7L5.737 5.1a3.375 3.375 0 0 1 2.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 0 1 .9 2.7m0 0a3 3 0 0 1-3 3m0 3h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Zm-3 6h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Z',
  terminal: 'm6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 18V6a2.25 2.25 0 0 0-2.25-2.25H5.25A2.25 2.25 0 0 0 3 6v12a2.25 2.25 0 0 0 2.25 2.25Z',
  photo:    'm2.25 15.75 5.159-5.159a2.25 2.25 0 0 1 3.182 0l5.159 5.159m-1.5-1.5 1.409-1.409a2.25 2.25 0 0 1 3.182 0l2.909 2.909m-18 3.75h16.5a1.5 1.5 0 0 0 1.5-1.5V6a1.5 1.5 0 0 0-1.5-1.5H3.75A1.5 1.5 0 0 0 2.25 6v12a1.5 1.5 0 0 0 1.5 1.5Zm10.5-11.25h.008v.008h-.008V8.25Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z',
  database: 'M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 0v3.75m-16.5-3.75v3.75m16.5 0v3.75C20.25 16.153 16.556 18 12 18s-8.25-1.847-8.25-4.125v-3.75m16.5 0c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125',
  shield:   'M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z',
  search:   'm21 21-4.34-4.34M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z',
  chevron:  'M19 9l-7 7-7-7',
  box:      'm7.5 4.27 9 5.15M21 8.25v7.5a2.25 2.25 0 0 1-1.125 1.95l-6.75 3.9a2.25 2.25 0 0 1-2.25 0l-6.75-3.9A2.25 2.25 0 0 1 3 15.75v-7.5a2.25 2.25 0 0 1 1.125-1.95l6.75-3.9a2.25 2.25 0 0 1 2.25 0l6.75 3.9A2.25 2.25 0 0 1 21 8.25Zm0 0-9 5.25m0 0L3 8.25m9 5.25v9.75',
  info:     'm11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z',
}

const HAZIR_GRUPLAR: Grup[] = [
  { ad: 'Geliştirme Araçları', ikon: I.wrench, aciklama: 'gcc, make, autoconf, automake, libtool, kernel-devel',
    paketler: ['gcc', 'gcc-c++', 'make', 'autoconf', 'automake', 'libtool', 'kernel-devel'] },
  { ad: 'Python', ikon: I.code, aciklama: 'Python 3 + pip + venv + devel başlıkları',
    paketler: ['python3', 'python3-pip', 'python3-devel', 'python3-virtualenv'] },
  { ad: 'Node.js + npm', ikon: I.bolt, aciklama: 'Node.js LTS + npm',
    paketler: ['nodejs', 'npm'] },
  { ad: 'Go', ikon: I.cube, aciklama: 'Golang derleyici',
    paketler: ['golang'] },
  { ad: 'Java', ikon: I.beaker, aciklama: 'OpenJDK 21 LTS + Maven',
    paketler: ['java-21-openjdk', 'java-21-openjdk-devel', 'maven'] },
  { ad: 'Rust', ikon: I.cog, aciklama: 'Rust + cargo',
    paketler: ['rust', 'cargo'] },
  { ad: 'Container / VM', ikon: I.server, aciklama: 'Docker uyumlu — podman + buildah + skopeo',
    paketler: ['podman', 'buildah', 'skopeo'] },
  { ad: 'Sistem Araçları', ikon: I.terminal, aciklama: 'CLI üretkenlik araçları',
    paketler: ['htop', 'ncdu', 'jq', 'tmux', 'vim-enhanced', 'git', 'rsync', 'mtr', 'iftop', 'iotop'] },
  { ad: 'Resim İşleme', ikon: I.photo, aciklama: 'ImageMagick + WebP + optimizasyon',
    paketler: ['ImageMagick', 'libwebp-tools', 'optipng', 'jpegoptim'] },
  { ad: 'DB İstemcileri', ikon: I.database, aciklama: 'PostgreSQL + Redis CLI',
    paketler: ['postgresql', 'redis'] },
  { ad: 'Güvenlik', ikon: I.shield, aciklama: 'GnuPG, OpenSSL, fail2ban',
    paketler: ['gnupg2', 'openssl', 'fail2ban'] },
]

function Ikon({ d, className = '' }: { d: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6}
      aria-hidden="true" className={className}>
      <path strokeLinecap="round" strokeLinejoin="round" d={d} />
    </svg>
  )
}

export default function PaketlerPage() {
  const [sekme, setSekme] = useState<Sekme>('ara')
  const [q, setQ] = useState('')
  const [sonuc, setSonuc] = useState<Paket[]>([])
  const [arandi, setArandi] = useState(false)
  const [yuk, setYuk] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState<string | null>(null)
  const [outputModal, setOutputModal] = useState<{ baslik: string; output: string } | null>(null)
  const [acik, setAcik] = useState<Set<string>>(new Set())
  const [grupDurum, setGrupDurum] = useState<Record<string, boolean>>({})

  async function grupDurumYukle(g: Grup) {
    try {
      const r = await api.get<Record<string, boolean>>('/paketler/durum', {
        params: { adlar: g.paketler.join(',') },
      })
      setGrupDurum(prev => ({ ...prev, ...r.data }))
    } catch {
      // sessizce yut — grup expand state'i bozulmasın
    }
  }

  function grupToggle(g: Grup) {
    setAcik(prev => {
      const yeni = new Set(prev)
      if (yeni.has(g.ad)) {
        yeni.delete(g.ad)
      } else {
        yeni.add(g.ad)
        grupDurumYukle(g)
      }
      return yeni
    })
  }

  async function paketToggle(paket: string, suankiKurulu: boolean) {
    const eylem = suankiKurulu ? 'kaldir' : 'kur'
    const onayMesaji = suankiKurulu
      ? `"${paket}" paketi KALDIRILACAK. Devam?`
      : `"${paket}" paketi sunucuya kurulacak. Devam?`
    if (!confirm(onayMesaji)) return

    setIsleniyor(paket); setHata(null); setBasari(null)
    try {
      const r = await api.post(`/paketler/${eylem}`, { paket })
      setBasari(`${paket} ${suankiKurulu ? 'kaldırıldı' : 'kuruldu'}`)
      setGrupDurum(prev => ({ ...prev, [paket]: !suankiKurulu }))
      setOutputModal({
        baslik: `${suankiKurulu ? 'Kaldırma' : 'Kurulum'} çıktısı — ${paket}`,
        output: (r.data as any).output || '',
      })
      setTimeout(() => setBasari(null), 3500)
    } catch (e) {
      setHata(apiHata(e, `${eylem} başarısız`))
    } finally {
      setIsleniyor(null)
    }
  }

  async function ara() {
    if (!q.trim()) return
    setYuk(true); setHata(null); setArandi(true)
    try {
      const ep = sekme === 'ara' ? '/paketler' : '/paketler/kurulu'
      const r = await api.get<{ icerik: Paket[]; toplam: number }>(ep, { params: { q } })
      setSonuc(r.data.icerik || [])
    } catch (e) {
      setHata(apiHata(e, 'Arama başarısız'))
    } finally {
      setYuk(false)
    }
  }

  async function kur(paket: string) {
    if (!confirm(`"${paket}" paketi sunucu genelinde kurulacak. Devam?`)) return
    setIsleniyor(paket); setHata(null); setBasari(null)
    try {
      const r = await api.post('/paketler/kur', { paket })
      setBasari(`${paket} kuruldu`)
      setOutputModal({ baslik: `Kurulum çıktısı — ${paket}`, output: r.data.output || '' })
      setTimeout(() => setBasari(null), 4000)
      if (sekme === 'ara') ara()
    } catch (e) { setHata(apiHata(e, 'Kurulum başarısız')) }
    finally { setIsleniyor(null) }
  }
  async function kaldir(paket: string) {
    if (!confirm(`"${paket}" paketi KALDIRILACAK. Devam?`)) return
    setIsleniyor(paket); setHata(null); setBasari(null)
    try {
      const r = await api.post('/paketler/kaldir', { paket })
      setBasari(`${paket} kaldırıldı`)
      setOutputModal({ baslik: `Kaldırma çıktısı — ${paket}`, output: r.data.output || '' })
      setTimeout(() => setBasari(null), 4000)
      ara()
    } catch (e) { setHata(apiHata(e, 'Kaldırma başarısız')) }
    finally { setIsleniyor(null) }
  }

  const kuruluToplam = useMemo(
    () => HAZIR_GRUPLAR.reduce((n, g) => n + g.paketler.filter(p => grupDurum[p]).length, 0),
    [grupDurum]
  )

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Paket Yöneticisi' },
      ]} />

      {/* Başlık */}
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Paket Yöneticisi</h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            DNF üzerinden sunucu paketleri ve derleyici ortamları.
          </p>
        </div>
        <div className="inline-flex items-center gap-2 self-start rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-700
                        dark:border-amber-900/40 dark:bg-amber-900/15 dark:text-amber-300">
          <Ikon d={I.shield} className="h-4 w-4 flex-shrink-0" />
          <span>Kritik paketler (kernel, bash, openssh, nginx, mariadb…) korumalıdır</span>
        </div>
      </div>

      {/* Uyarılar */}
      {hata && (
        <div role="alert" className="mb-3 flex items-start gap-2 rounded-xl border border-red-200 bg-red-50 px-3.5 py-2.5 text-sm text-red-700
                                     dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300">
          <Ikon d={I.info} className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <span className="whitespace-pre-wrap">{hata}</span>
        </div>
      )}
      {basari && (
        <div role="status" className="mb-3 flex items-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 py-2.5 text-sm font-medium text-emerald-700
                                      dark:border-emerald-900/50 dark:bg-emerald-900/20 dark:text-emerald-300">
          <Ikon d={I.shield} className="h-4 w-4 flex-shrink-0" />
          <span>{basari}</span>
        </div>
      )}

      {/* Hızlı Kurulum Grupları */}
      <section aria-labelledby="grup-baslik" className="mb-6">
        <div className="mb-3 flex items-center gap-2">
          <Ikon d={I.box} className="h-4 w-4 text-slate-400" />
          <h2 id="grup-baslik" className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Hızlı Kurulum Grupları
          </h2>
          <span className="text-xs text-slate-300 dark:text-slate-600">·</span>
          <span className="text-xs text-slate-400 dark:text-slate-500">bir grubu aç, paketleri tek tek aç/kapat</span>
        </div>

        <div className="grid grid-cols-1 items-start gap-3 lg:grid-cols-2">
          {HAZIR_GRUPLAR.map(g => {
            const open = acik.has(g.ad)
            const kuruluSayi = g.paketler.filter(p => grupDurum[p]).length
            return (
              <div key={g.ad}
                className="self-start overflow-hidden rounded-2xl border border-slate-200 bg-white transition-colors
                           dark:border-slate-800 dark:bg-slate-900">
                <button onClick={() => grupToggle(g)}
                  aria-expanded={open}
                  className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors
                             hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-500/40
                             dark:hover:bg-slate-800/60">
                  <span className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-500
                                   dark:bg-slate-800 dark:text-slate-400">
                    <Ikon d={g.ikon} className="h-5 w-5" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{g.ad}</span>
                    <span className="mt-0.5 block truncate text-[11px] text-slate-500 dark:text-slate-400">{g.aciklama}</span>
                  </span>
                  {open && (
                    <span className="flex-shrink-0 rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-500 tabular-nums
                                     dark:bg-slate-800 dark:text-slate-400">
                      <span className="text-emerald-600 dark:text-emerald-400">{kuruluSayi}</span>/{g.paketler.length}
                    </span>
                  )}
                  <Ikon d={I.chevron}
                    className={`h-4 w-4 flex-shrink-0 text-slate-400 transition-transform dark:text-slate-500 ${open ? 'rotate-180' : ''}`} />
                </button>

                {open && (
                  <div className="border-t border-slate-100 bg-slate-50/60 px-2.5 py-2 dark:border-slate-800 dark:bg-slate-950/40">
                    <ul className="space-y-0.5">
                      {g.paketler.map(p => {
                        const kurulu = !!grupDurum[p]
                        const bekleniyor = isleniyor === p
                        return (
                          <li key={p}
                            className="flex items-center justify-between gap-3 rounded-lg px-2.5 py-1.5 transition-colors
                                       hover:bg-white dark:hover:bg-slate-800/50">
                            <span className="flex min-w-0 items-center gap-2">
                              <code className="truncate font-mono text-[13px] text-slate-700 dark:text-slate-200">{p}</code>
                              {kurulu && (
                                <span className="flex-shrink-0 rounded bg-emerald-100 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-emerald-700
                                                 dark:bg-emerald-900/30 dark:text-emerald-300">kurulu</span>
                              )}
                            </span>
                            <button onClick={() => paketToggle(p, kurulu)} disabled={bekleniyor}
                              role="switch" aria-checked={kurulu} aria-label={`${p} ${kurulu ? 'kaldır' : 'kur'}`}
                              title={bekleniyor ? 'İşleniyor…' : (kurulu ? 'Kaldır' : 'Kur')}
                              className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full transition-colors
                                          focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40 focus-visible:ring-offset-1
                                          dark:focus-visible:ring-offset-slate-900
                                          ${kurulu ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}
                                          ${bekleniyor ? 'cursor-wait opacity-50' : ''}`}>
                              <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform
                                                ${kurulu ? 'translate-x-[18px]' : 'translate-x-0.5'}`} />
                            </button>
                          </li>
                        )
                      })}
                    </ul>
                  </div>
                )}
              </div>
            )
          })}
        </div>
        {kuruluToplam > 0 && (
          <p className="mt-2 text-[11px] text-slate-400 dark:text-slate-600">{kuruluToplam} paket kurulu (açılan gruplarda)</p>
        )}
      </section>

      {/* Paket Arama */}
      <section aria-labelledby="ara-baslik">
        <div className="mb-3 flex items-center gap-2">
          <Ikon d={I.search} className="h-4 w-4 text-slate-400" />
          <h2 id="ara-baslik" className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Paket Ara
          </h2>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          {/* Segment sekmeler */}
          <div className="mb-4 inline-flex rounded-xl border border-slate-200 bg-slate-100 p-0.5 dark:border-slate-800 dark:bg-slate-800/60">
            {([['ara', 'Repolarda Ara'], ['kurulu', 'Kurulu Paketler']] as [Sekme, string][]).map(([s, etiket]) => (
              <button key={s}
                onClick={() => { setSekme(s); setSonuc([]); setArandi(false) }}
                className={`rounded-lg px-3.5 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none
                            ${sekme === s
                              ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-slate-100'
                              : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'}`}>
                {etiket}
              </button>
            ))}
          </div>

          {/* Arama kutusu */}
          <div className="flex flex-col gap-2 sm:flex-row">
            <div className="relative flex-1">
              <Ikon d={I.search} className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input type="text" value={q} onChange={e => setQ(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && ara()}
                placeholder={sekme === 'ara' ? 'örn: mongodb, redis, nodejs, gcc, htop' : 'kurulu paket adı veya açıklama'}
                aria-label="Paket ara"
                className="w-full rounded-xl border border-slate-200 bg-white py-2 pl-9 pr-3 font-mono text-sm text-slate-900
                           placeholder:font-sans placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/30
                           dark:border-slate-700 dark:bg-slate-950/50 dark:text-slate-100" />
            </div>
            <button onClick={ara} disabled={yuk || !q.trim()}
              className="inline-flex items-center justify-center gap-2 rounded-xl bg-slate-900 px-5 py-2 text-sm font-medium text-white transition-colors
                         hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-700 dark:text-slate-100 dark:hover:bg-slate-600">
              {yuk ? 'Aranıyor…' : 'Ara'}
            </button>
          </div>

          {/* Sonuçlar */}
          {arandi && !yuk && sonuc.length === 0 && (
            <div role="status" className="py-10 text-center">
              <Ikon d={I.search} className="mx-auto h-8 w-8 text-slate-300 dark:text-slate-600" />
              <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">"{q}" için sonuç bulunamadı.</p>
            </div>
          )}

          {sonuc.length > 0 && (
            <div className="mt-4">
              <div className="mb-2 text-xs text-slate-400 dark:text-slate-500">{sonuc.length} sonuç</div>
              <ul className="space-y-1.5">
                {sonuc.map(p => (
                  <li key={p.adi}
                    className={`flex items-center gap-3 rounded-xl border px-3.5 py-2.5 transition-colors
                                ${p.kurulu
                                  ? 'border-emerald-200 bg-emerald-50/60 dark:border-emerald-900/40 dark:bg-emerald-900/10'
                                  : 'border-slate-200 bg-slate-50/60 dark:border-slate-800 dark:bg-slate-950/40'}`}>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-baseline gap-2">
                        <span className="font-mono text-sm font-semibold text-slate-900 dark:text-slate-100">{p.adi}</span>
                        {p.surum && <span className="font-mono text-[11px] text-slate-400 dark:text-slate-500">{p.surum}</span>}
                        {p.kurulu && (
                          <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-emerald-700
                                           dark:bg-emerald-900/30 dark:text-emerald-300">kurulu</span>
                        )}
                        {p.korunan && (
                          <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-amber-700
                                           dark:bg-amber-900/30 dark:text-amber-300">korumalı</span>
                        )}
                      </div>
                      {p.aciklama && <div className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400">{p.aciklama}</div>}
                    </div>
                    {p.kurulu ? (
                      <button onClick={() => kaldir(p.adi)} disabled={p.korunan || isleniyor === p.adi}
                        className="flex-shrink-0 rounded-lg border border-red-200 px-3 py-1.5 text-xs font-medium text-red-600 transition-colors
                                   hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-40
                                   dark:border-red-900/50 dark:text-red-400 dark:hover:bg-red-900/20">
                        {isleniyor === p.adi ? 'Kaldırılıyor…' : 'Kaldır'}
                      </button>
                    ) : (
                      <button onClick={() => kur(p.adi)} disabled={isleniyor === p.adi}
                        className="flex-shrink-0 rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white transition-colors
                                   hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-700 dark:text-slate-100 dark:hover:bg-slate-600">
                        {isleniyor === p.adi ? 'Kuruluyor…' : 'Kur'}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </section>

      {/* Çıktı modalı */}
      {outputModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onClick={() => setOutputModal(null)}>
          <div className="flex max-h-[80vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-slate-900"
            onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3 dark:border-slate-800">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{outputModal.baslik}</h3>
              <button onClick={() => setOutputModal(null)} aria-label="Kapat"
                className="rounded-lg p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700
                           dark:hover:bg-slate-800 dark:hover:text-slate-200">
                <Ikon d="M6 18 18 6M6 6l12 12" className="h-4 w-4" />
              </button>
            </div>
            <pre className="flex-1 overflow-auto bg-slate-950 p-4 text-xs font-mono leading-relaxed text-slate-100 whitespace-pre-wrap">
              {outputModal.output || '(çıktı yok)'}
            </pre>
            <div className="border-t border-slate-200 px-4 py-2.5 text-right dark:border-slate-800">
              <button onClick={() => setOutputModal(null)}
                className="rounded-lg bg-slate-900 px-4 py-1.5 text-sm font-medium text-white transition-colors
                           hover:bg-slate-800 dark:bg-slate-700 dark:text-slate-100 dark:hover:bg-slate-600">
                Kapat
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
