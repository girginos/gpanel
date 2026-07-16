import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Servis = {
  birim: string
  etiket: string
  grup: string
  reload: boolean
  durum: string // active | inactive | failed | absent
}

const DURUM_STIL: Record<string, string> = {
  active:   'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
  inactive: 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400',
  failed:   'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300',
  absent:   'bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500',
}
const DURUM_ETIKET: Record<string, string> = {
  active: '● Çalışıyor', inactive: '○ Durmuş', failed: '✕ Hatalı', absent: '— Kurulu değil',
}
const GRUP_IKON: Record<string, string> = {
  'Web Sunucusu': '🌐',
  'Veritabanı & Önbellek': '🗄️',
  'DNS': '📡',
  'PHP-FPM': '🐘',
  'Diğer': '⚙️',
}

export default function ServislerPage() {
  const [liste, setListe] = useState<Servis[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [islemBirim, setIslemBirim] = useState<string | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  async function getir() {
    try {
      const r = await api.get<Servis[]>('/system/servisler')
      setListe(r.data)
    } catch (e) {
      setHata(apiHata(e, 'Servisler alınamadı'))
    } finally {
      setYukleniyor(false)
    }
  }
  useEffect(() => { getir() }, [])

  async function islem(s: Servis, aksiyon: 'restart' | 'reload') {
    setIslemBirim(s.birim); setHata(null); setBasari(null)
    try {
      await api.post('/system/servis-islem', { birim: s.birim, aksiyon })
      setBasari(`${s.etiket} ${aksiyon === 'reload' ? 'yeniden yüklendi' : 'yeniden başlatıldı'}.`)
      await getir()
    } catch (e) {
      setHata(apiHata(e, `${s.etiket} işlemi başarısız`))
    } finally {
      setIslemBirim(null)
    }
  }

  // Grupları görülme sırasına göre, grup içindeki servis sırasını koruyarak topla
  const gruplar: { ad: string; servisler: Servis[] }[] = []
  for (const s of liste) {
    let g = gruplar.find(x => x.ad === s.grup)
    if (!g) { g = { ad: s.grup, servisler: [] }; gruplar.push(g) }
    g.servisler.push(s)
  }

  return (
    <div className="max-w-4xl mx-auto px-4 py-6">
      <Breadcrumb items={[
        { etiket: 'Araçlar & Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Servisler' },
      ]} />
      <div className="mb-5">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Servis Yönetimi</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
          Web, veritabanı, DNS ve PHP servislerini buradan yeniden başlatın. Ayar değişikliği sonrası kullanışlıdır.
        </p>
      </div>

      {hata && <div className="mb-4 px-4 py-2.5 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-4 px-4 py-2.5 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {yukleniyor ? (
        <div className="p-8 text-center text-sm text-slate-400">Yükleniyor…</div>
      ) : (
        <div className="space-y-6">
          {gruplar.map(g => (
            <section key={g.ad}>
              <h2 className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500 mb-2 px-1">
                <span className="text-sm">{GRUP_IKON[g.ad] || '•'}</span>{g.ad}
              </h2>
              <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-2xl overflow-hidden">
                <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                  {g.servisler.map(s => {
                    const absent = s.durum === 'absent'
                    const mesgul = islemBirim === s.birim
                    return (
                      <li key={s.birim} className="flex items-center gap-4 px-5 py-3.5">
                        <div className="flex-1 min-w-0">
                          <div className="font-medium text-slate-900 dark:text-slate-100">{s.etiket}</div>
                          <div className="text-xs font-mono text-slate-400 dark:text-slate-500">{s.birim}</div>
                        </div>
                        <span className={`w-28 text-center text-xs px-2.5 py-1 rounded-full font-medium ${DURUM_STIL[s.durum] || DURUM_STIL.inactive}`}>
                          {DURUM_ETIKET[s.durum] || s.durum}
                        </span>
                        <div className="flex items-center gap-2 shrink-0">
                          {/* Reload slotu her satırda yer kaplar → Restart hizalı kalır */}
                          <button disabled={!s.reload || absent || mesgul} onClick={() => islem(s, 'reload')}
                            className={`w-20 px-3 py-1.5 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-40 disabled:cursor-not-allowed transition ${s.reload ? '' : 'invisible'}`}>
                            Reload
                          </button>
                          <button disabled={absent || mesgul} onClick={() => islem(s, 'restart')}
                            className="w-20 px-3.5 py-1.5 text-sm rounded-lg bg-slate-900 dark:bg-white text-white dark:text-slate-900 hover:bg-slate-800 dark:hover:bg-slate-100 disabled:opacity-40 disabled:cursor-not-allowed transition">
                            {mesgul ? '…' : 'Restart'}
                          </button>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  )
}
