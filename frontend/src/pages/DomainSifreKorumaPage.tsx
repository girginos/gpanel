import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Kayit = { id: number; yol: string; kullanici: string; created_at: string }

export default function DomainSifreKorumaPage() {
  const { id } = useParams()
  const [liste, setListe] = useState<Kayit[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)
  const [yol, setYol] = useState('/gizli')
  const [kullanici, setKullanici] = useState('')
  const [parola, setParola] = useState('')
  const [kaydediyor, setKaydediyor] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Kayit[]>(`/domains/${id}/koruma`)
      .then(r => setListe(r.data || [])).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function ekle(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setOk(null); setKaydediyor(true)
    try {
      await api.post(`/domains/${id}/koruma`, { yol, kullanici, parola })
      setOk(`${yol} dizini "${kullanici}" ile korumaya alındı.`)
      setParola('')
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'Eklenemedi'))
    } finally { setKaydediyor(false) }
  }

  async function sil(k: Kayit) {
    if (!confirm(`"${k.kullanici}" kullanıcısını ${k.yol} korumasından kaldır?`)) return
    setHata(null); setOk(null)
    try {
      await api.delete(`/domains/${id}/koruma/${k.id}`)
      yukle()
    } catch (err) { setHata(apiHata(err, 'Silinemedi')) }
  }

  // yol -> o yola ait kullanıcılar
  const grup = liste.reduce<Record<string, Kayit[]>>((a, k) => { (a[k.yol] ||= []).push(k); return a }, {})

  return (
    <div className="px-6 py-5">
      <div className="max-w-3xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: 'Şifre Korumalı Dizinler' },
        ]} />
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Şifre Korumalı Dizinler</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
          Belirli bir dizini HTTP kimlik doğrulaması (<span className="font-mono">.htpasswd</span>) ile koruyun. Ziyaretçiler kullanıcı adı ve parola olmadan erişemez.
        </p>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {ok && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{ok}</div>}

        {/* Ekleme formu */}
        <form onSubmit={ekle} className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5 shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Yeni koruma / kullanıcı ekle</h3>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <label className="block">
              <span className="text-xs text-slate-500 dark:text-slate-400">Dizin yolu</span>
              <input value={yol} onChange={e => setYol(e.target.value)} required placeholder="/gizli"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
            <label className="block">
              <span className="text-xs text-slate-500 dark:text-slate-400">Kullanıcı adı</span>
              <input value={kullanici} onChange={e => setKullanici(e.target.value)} required placeholder="kullanici"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
            <label className="block">
              <span className="text-xs text-slate-500 dark:text-slate-400">Parola</span>
              <input value={parola} onChange={e => setParola(e.target.value)} required type="password" placeholder="••••••••"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
          </div>
          <p className="text-[11px] text-slate-400 mt-2">Yol <span className="font-mono">/</span> ile başlamalı (örn. <span className="font-mono">/gizli</span>, <span className="font-mono">/admin</span>). Aynı yola birden fazla kullanıcı ekleyebilirsiniz.</p>
          <button disabled={kaydediyor} className="mt-3 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-lg disabled:opacity-50">
            {kaydediyor ? 'Ekleniyor…' : 'Koruma Ekle'}
          </button>
        </form>

        {/* Mevcut korumalar */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Korunan dizinler</h3>
          {yuk ? (
            <div className="text-sm text-slate-400">Yükleniyor…</div>
          ) : liste.length === 0 ? (
            <div className="text-center py-8">
              <div className="text-3xl mb-2">🔒</div>
              <p className="text-sm text-slate-500 dark:text-slate-400">Henüz korumalı dizin yok.</p>
            </div>
          ) : (
            <div className="space-y-4">
              {Object.entries(grup).map(([g, ks]) => (
                <div key={g} className="border border-slate-100 dark:border-slate-700 rounded-lg overflow-hidden">
                  <div className="flex items-center gap-2 px-3 py-2 bg-slate-50 dark:bg-slate-900/40">
                    <span className="text-sm">🔒</span>
                    <span className="font-mono text-sm text-slate-700 dark:text-slate-200">{g}</span>
                    <span className="text-xs text-slate-400">· {ks.length} kullanıcı</span>
                  </div>
                  <ul className="divide-y divide-slate-50 dark:divide-slate-700/50">
                    {ks.map(k => (
                      <li key={k.id} className="flex items-center justify-between px-3 py-2">
                        <span className="text-sm text-slate-600 dark:text-slate-300">{k.kullanici}</span>
                        <button onClick={() => sil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">Kaldır</button>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}
