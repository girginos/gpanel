import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Durum = { kurulu: boolean; surum: string; composer_json: boolean; kullanici: string; dizin: string }

export default function DomainComposerPage() {
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [cikti, setCikti] = useState<string>('')
  const [calisan, setCalisan] = useState<string | null>(null)
  const [paket, setPaket] = useState('')

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Durum>(`/domains/${id}/composer`).then(r => setD(r.data)).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function calistir(komut: string, pkt?: string) {
    setCalisan(komut); setHata(null); setCikti(`$ composer ${komut}${pkt ? ' ' + pkt : ''}\n\nÇalışıyor…`)
    try {
      const { data } = await api.post(`/domains/${id}/composer`, { komut, paket: pkt || '' })
      setCikti(`$ composer ${komut}${pkt ? ' ' + pkt : ''}\n\n${data.cikti || '(çıktı yok)'}\n\n${data.ok ? '✓ Tamamlandı' : '✗ Hata ile bitti'}`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Çalıştırılamadı')); setCikti('')
    } finally { setCalisan(null) }
  }

  if (yuk) return <div className="px-6 py-5 text-slate-400">Yükleniyor…</div>
  if (!d) return <div className="px-6 py-5"><div className="text-sm text-red-600">{hata || 'Bulunamadı'}</div></div>

  const btnBase = 'px-3 py-1.5 rounded-lg text-sm font-medium disabled:opacity-50'

  return (
    <div className="px-6 py-5">
      <div className="max-w-3xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: 'Composer' },
        ]} />
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">PHP Composer</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
          <span className="font-mono">{d.dizin}</span> dizininde <span className="font-mono">{d.kullanici}</span> olarak çalışır.
        </p>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        {!d.kurulu ? (
          <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-2xl p-5 text-sm text-amber-800 dark:text-amber-200">
            Composer sunucuda kurulu değil. Yönetici tarafından kurulması gerekiyor.
          </div>
        ) : (
          <>
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
              <div className="flex items-center justify-between mb-3">
                <div>
                  <span className="text-xs font-mono text-slate-500">{d.surum}</span>
                  <span className={`ml-2 text-xs ${d.composer_json ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-400'}`}>
                    {d.composer_json ? '✓ composer.json bulundu' : 'composer.json yok'}
                  </span>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <button disabled={!!calisan} onClick={() => calistir('install')} className={`${btnBase} bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900`}>{calisan === 'install' ? '…' : 'install'}</button>
                <button disabled={!!calisan} onClick={() => calistir('update')} className={`${btnBase} bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900`}>{calisan === 'update' ? '…' : 'update'}</button>
                <button disabled={!!calisan} onClick={() => calistir('dump-autoload')} className={`${btnBase} border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800`}>dump-autoload</button>
                <button disabled={!!calisan} onClick={() => calistir('validate')} className={`${btnBase} border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800`}>validate</button>
                <button disabled={!!calisan} onClick={() => calistir('show')} className={`${btnBase} border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800`}>show</button>
              </div>
              <div className="mt-3 flex gap-2">
                <input value={paket} onChange={e => setPaket(e.target.value)} placeholder="vendor/paket veya vendor/paket:^1.2"
                  className="flex-1 px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
                <button disabled={!!calisan || !paket.trim()} onClick={() => calistir('require', paket.trim())} className={`${btnBase} bg-emerald-600 hover:bg-emerald-700 text-white`}>require</button>
                <button disabled={!!calisan || !paket.trim()} onClick={() => calistir('remove', paket.trim())} className={`${btnBase} border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20`}>remove</button>
              </div>
            </div>

            {cikti && (
              <div className="bg-slate-900 rounded-2xl p-4 shadow-sm">
                <pre className="text-xs font-mono text-slate-100 whitespace-pre-wrap break-all max-h-96 overflow-y-auto">{cikti}</pre>
              </div>
            )}
          </>
        )}

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}
