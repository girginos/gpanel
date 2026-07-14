// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Paket = {
  adi: string; surum?: string; aciklama?: string;
  kurulu: boolean; korunan: boolean
}

type Grup = { ad: string; paketler: string[]; aciklama: string }

type Sekme = 'ara' | 'kurulu'

const HAZIR_GRUPLAR: Grup[] = [
  { ad: '🛠️ Geliştirme Araçları', aciklama: 'gcc, make, autoconf, automake, libtool, kernel-devel',
    paketler: ['gcc', 'gcc-c++', 'make', 'autoconf', 'automake', 'libtool', 'kernel-devel'] },
  { ad: '🐍 Python', aciklama: 'Python 3 + pip + venv + devel headers',
    paketler: ['python3', 'python3-pip', 'python3-devel', 'python3-virtualenv'] },
  { ad: '⚛️ Node.js + npm', aciklama: 'Node.js LTS + npm',
    paketler: ['nodejs', 'npm'] },
  { ad: '🟦 Go', aciklama: 'Golang derleyici',
    paketler: ['golang'] },
  { ad: '☕ Java', aciklama: 'OpenJDK 21 LTS + Maven',
    paketler: ['java-21-openjdk', 'java-21-openjdk-devel', 'maven'] },
  { ad: '🦀 Rust', aciklama: 'Rust + cargo',
    paketler: ['rust', 'cargo'] },
  { ad: '📦 Container/VM', aciklama: 'Docker uyumlu — podman + buildah + skopeo',
    paketler: ['podman', 'buildah', 'skopeo'] },
  { ad: '🔧 Sistem araçları', aciklama: 'CLI üretkenlik araçları',
    paketler: ['htop', 'ncdu', 'jq', 'tmux', 'vim-enhanced', 'git', 'rsync', 'mtr', 'iftop', 'iotop'] },
  { ad: '🖼️ Resim işleme', aciklama: 'ImageMagick + WebP + optimizasyon',
    paketler: ['ImageMagick', 'libwebp-tools', 'optipng', 'jpegoptim'] },
  { ad: '🗄️ DB istemcileri', aciklama: 'PostgreSQL + Redis CLI',
    paketler: ['postgresql', 'redis'] },
  { ad: '🔐 Güvenlik', aciklama: 'GnuPG, OpenSSL, fail2ban',
    paketler: ['gnupg2', 'openssl', 'fail2ban'] },
]

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
  const [acikGrup, setAcikGrup] = useState<string | null>(null)
  const [grupDurum, setGrupDurum] = useState<Record<string, boolean>>({})

  async function grupDurumYukle(g: Grup) {
    try {
      const r = await api.get<Record<string, boolean>>('/paketler/durum', {
        params: { adlar: g.paketler.join(',') }
      })
      setGrupDurum(prev => ({ ...prev, ...r.data }))
    } catch {
      // sessizce yutuyor — grup expand state'i bozulmasin
    }
  }

  function grupTogga(g: Grup) {
    if (acikGrup === g.ad) {
      setAcikGrup(null)
    } else {
      setAcikGrup(g.ad)
      grupDurumYukle(g)
    }
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
      setBasari(`✓ ${paket} ${suankiKurulu ? 'kaldırıldı' : 'kuruldu'}`)
      setGrupDurum(prev => ({ ...prev, [paket]: !suankiKurulu }))
      setOutputModal({
        baslik: `${suankiKurulu ? 'Kaldırma' : 'Kurulum'} çıktısı: ${paket}`,
        output: (r.data as any).output || ''
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
    if (!confirm(`Paket "${paket}" sunucu genelinde kurulacak. Devam?`)) return
    setIsleniyor(paket); setHata(null); setBasari(null)
    try {
      const r = await api.post('/paketler/kur', { paket })
      setBasari(`✓ ${paket} kuruldu`)
      setOutputModal({ baslik: `Kurulum çıktısı: ${paket}`, output: r.data.output || '' })
      setTimeout(() => setBasari(null), 4000)
      if (sekme === 'ara') ara()
    } catch (e) { setHata(apiHata(e, 'Kurulum başarısız')) }
    finally { setIsleniyor(null) }
  }
  async function kaldir(paket: string) {
    if (!confirm(`Paket "${paket}" KALDIRILACAK. Devam?`)) return
    setIsleniyor(paket); setHata(null); setBasari(null)
    try {
      const r = await api.post('/paketler/kaldir', { paket })
      setBasari(`✓ ${paket} kaldırıldı`)
      setOutputModal({ baslik: `Kaldırma çıktısı: ${paket}`, output: r.data.output || '' })
      setTimeout(() => setBasari(null), 4000)
      ara()
    } catch (e) { setHata(apiHata(e, 'Kaldırma başarısız')) }
    finally { setIsleniyor(null) }
  }

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'Paket Yöneticisi' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Paket Yöneticisi · Derleyiciler</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        DNF üzerinden sunucu paketleri. Kritik paketler (kernel, bash, openssh, nginx, mariadb…) <strong>korunmaktadır</strong>.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Grup kartları — accordion */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">📦 Hızlı Kurulum Grupları</h3>
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">Bir gruba tıkla, içindeki paketleri tek tek aç/kapat.</p>
        <div className="space-y-2">
          {HAZIR_GRUPLAR.map(g => {
            const acik = acikGrup === g.ad
            const kuruluSayi = g.paketler.filter(p => grupDurum[p]).length
            return (
              <div key={g.ad} className="border border-slate-200 dark:border-slate-700 rounded-lg overflow-hidden">
                <button onClick={() => grupTogga(g)}
                  className="w-full text-left px-3 py-2.5 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 flex items-center justify-between transition">
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{g.ad}</div>
                    <div className="text-[11px] text-slate-500 dark:text-slate-500">{g.aciklama}</div>
                  </div>
                  <div className="flex items-center gap-3 flex-shrink-0">
                    {acik && (
                      <span className="text-[11px] text-slate-500 dark:text-slate-500">
                        <span className="font-semibold text-emerald-700 dark:text-emerald-300">{kuruluSayi}</span>
                        <span> / {g.paketler.length} kurulu</span>
                      </span>
                    )}
                    <svg className={`w-4 h-4 text-slate-400 dark:text-slate-500 transition-transform ${acik ? 'rotate-180' : ''}`}
                      fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
                    </svg>
                  </div>
                </button>
                {acik && (
                  <div className="border-t border-slate-100 dark:border-slate-800 bg-slate-50 dark:bg-slate-900/50 px-3 py-2 space-y-1">
                    {g.paketler.map(p => {
                      const kurulu = !!grupDurum[p]
                      const bekleniyor = isleniyor === p
                      return (
                        <div key={p} className="flex items-center justify-between gap-3 px-2 py-1.5 rounded hover:bg-white dark:bg-slate-800 transition">
                          <div className="flex-1 min-w-0">
                            <code className="text-sm font-mono text-slate-900 dark:text-slate-100">{p}</code>
                            {kurulu && <span className="ml-2 text-[10px] px-1.5 py-0.5 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 font-medium">KURULU</span>}
                          </div>
                          <button onClick={() => paketToggle(p, kurulu)}
                            disabled={bekleniyor}
                            className={`relative inline-flex h-5 w-9 items-center rounded-full transition flex-shrink-0 ${
                              kurulu ? 'bg-emerald-500' : 'bg-slate-300'
                            } ${bekleniyor ? 'opacity-50 cursor-wait' : ''}`}
                            title={bekleniyor ? 'İşleniyor…' : (kurulu ? 'Kaldır' : 'Kur')}>
                            <span className={`inline-block h-3 w-3 transform rounded-full bg-white dark:bg-slate-800 shadow transition ${kurulu ? 'translate-x-5' : 'translate-x-1'}`} />
                          </button>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>

      {/* Arama */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="flex items-center gap-2 mb-3 border-b border-slate-100 dark:border-slate-800 pb-2">
          <button onClick={() => { setSekme('ara'); setSonuc([]); setArandi(false) }}
            className={`px-3 py-1.5 text-sm rounded ${sekme === 'ara' ? 'bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300 font-medium' : 'text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
            🔍 Repolarda Ara
          </button>
          <button onClick={() => { setSekme('kurulu'); setSonuc([]); setArandi(false) }}
            className={`px-3 py-1.5 text-sm rounded ${sekme === 'kurulu' ? 'bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300 font-medium' : 'text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
            📦 Kurulu Paketler
          </button>
        </div>

        <div className="flex gap-2 mb-4">
          <input type="text" value={q} onChange={e => setQ(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && ara()}
            placeholder={sekme === 'ara' ? 'örn: mongodb, redis, nodejs, gcc, htop' : 'kurulu paket adı veya açıklama'}
            className="flex-1 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 outline-none" />
          <button onClick={ara} disabled={yuk || !q.trim()}
            className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm font-medium rounded">
            {yuk ? 'Aranıyor…' : 'Ara'}
          </button>
        </div>

        {arandi && !yuk && sonuc.length === 0 && (
          <div className="py-8 text-center text-sm text-slate-400 dark:text-slate-500">Sonuç yok.</div>
        )}

        {sonuc.length > 0 && (
          <div className="space-y-1.5">
            <div className="text-xs text-slate-500 dark:text-slate-500 mb-2">{sonuc.length} sonuç</div>
            {sonuc.map(p => (
              <div key={p.adi}
                className={`flex items-center gap-3 px-3 py-2 rounded border ${p.kurulu ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-200 dark:border-emerald-800' : 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700'}`}>
                <div className="flex-1 min-w-0">
                  <div className="flex items-baseline gap-2">
                    <span className="font-mono text-sm font-semibold text-slate-900 dark:text-slate-100">{p.adi}</span>
                    {p.surum && <span className="text-[10px] font-mono text-slate-500 dark:text-slate-500">{p.surum}</span>}
                    {p.kurulu && <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 font-medium">KURULU</span>}
                    {p.korunan && <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 font-medium">KORUMALI</span>}
                  </div>
                  {p.aciklama && <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 truncate">{p.aciklama}</div>}
                </div>
                {p.kurulu ? (
                  <button onClick={() => kaldir(p.adi)}
                    disabled={p.korunan || isleniyor === p.adi}
                    className="text-xs px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:bg-slate-300 disabled:cursor-not-allowed text-white rounded">
                    {isleniyor === p.adi ? 'Kaldırılıyor…' : 'Kaldır'}
                  </button>
                ) : (
                  <button onClick={() => kur(p.adi)}
                    disabled={isleniyor === p.adi}
                    className="text-xs px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 rounded">
                    {isleniyor === p.adi ? 'Kuruluyor…' : 'Kur'}
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {outputModal && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setOutputModal(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full shadow-xl flex flex-col max-h-[80vh]" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-700">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{outputModal.baslik}</h3>
              <button onClick={() => setOutputModal(null)} className="text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300">×</button>
            </div>
            <pre className="flex-1 overflow-auto p-3 bg-slate-900 text-slate-100 text-xs font-mono whitespace-pre-wrap">{outputModal.output}</pre>
            <div className="px-4 py-2 border-t border-slate-200 dark:border-slate-700 text-right">
              <button onClick={() => setOutputModal(null)}
                className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm rounded">Kapat</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}