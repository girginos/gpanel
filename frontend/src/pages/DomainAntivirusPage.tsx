import { useEffect, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'

type Bulgu = { dosya: string; imza: string; motor: string; karantina: number }
type Tarama = { id: number; durum: string; motor: string; taranan: number; enfekte: number; baslangic: string; bitis: string }
type Durum = { clamav: boolean; imza_tarihi: string; kullanici: string; son_tarama: Tarama | null; bulgular: Bulgu[] }

export default function DomainAntivirusPage() {
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [tarariyor, setTarariyor] = useState(false)
  const [imzaYuk, setImzaYuk] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  function yukle() {
    if (!id) return
    api.get<Durum>(`/domains/${id}/antivirus`).then(r => {
      setD(r.data)
      if (r.data.son_tarama?.durum === 'calisiyor') startPoll(r.data.son_tarama.id)
    }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(() => { yukle(); return () => { if (pollRef.current) clearInterval(pollRef.current) } }, [id])

  function startPoll(sid: number) {
    setTarariyor(true)
    if (pollRef.current) clearInterval(pollRef.current)
    pollRef.current = setInterval(async () => {
      try {
        const { data } = await api.get<Tarama & { bulgular: Bulgu[] }>(`/domains/${id}/antivirus/tara/${sid}`)
        if (data.durum !== 'calisiyor') {
          if (pollRef.current) clearInterval(pollRef.current)
          setTarariyor(false)
          yukle()
        }
      } catch { if (pollRef.current) clearInterval(pollRef.current); setTarariyor(false) }
    }, 2500)
  }

  async function tara() {
    setHata(null); setTarariyor(true)
    try {
      const { data } = await api.post(`/domains/${id}/antivirus/tara`, {})
      startPoll(data.scan_id)
    } catch (e) { setHata(apiHata(e, 'Tarama başlatılamadı')); setTarariyor(false) }
  }

  async function karantina(b: Bulgu) {
    if (!confirm(`Dosya karantinaya alınsın mı?\n${b.dosya}\n\n(Dosya ~/.karantina altına taşınır ve erişilemez hâle gelir.)`)) return
    setHata(null)
    try { await api.post(`/domains/${id}/antivirus/karantina`, { dosya: b.dosya }); yukle() }
    catch (e) { setHata(apiHata(e, 'Karantinaya alınamadı')) }
  }

  async function imzaGuncelle() {
    setImzaYuk(true); setHata(null)
    try { await api.post(`/domains/${id}/antivirus/imza-guncelle`, {}); yukle() }
    catch (e) { setHata(apiHata(e, 'İmza güncellenemedi')) }
    finally { setImzaYuk(false) }
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">Yükleniyor…</div>
  if (!d) return <div className="px-4 py-4 sm:px-6 sm:py-5"><div className="text-sm text-red-600">{hata || 'Bulunamadı'}</div></div>

  const aktif = d.bulgular.filter(b => !b.karantina)

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-4xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: 'Antivirüs' },
        ]} />
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Antivirüs — Zararlı Yazılım Taraması</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
          <span className="font-mono">public_html</span> dizini ClamAV imzaları + yerleşik webshell heuristiği ile taranır.
        </p>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        {/* Durum + eylemler */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="text-sm space-y-0.5">
              <div className="flex flex-wrap items-center gap-2">
                <span className={`w-2 h-2 rounded-full ${d.clamav ? 'bg-emerald-500' : 'bg-amber-500'}`} />
                <span className="text-slate-700 dark:text-slate-200">Motor: <span className="font-medium">{d.clamav ? 'ClamAV + Heuristik' : 'Sadece Heuristik'}</span></span>
              </div>
              {d.clamav && <div className="text-xs text-slate-400 ml-4">İmza veritabanı: {d.imza_tarihi || '—'}</div>}
              {d.son_tarama && <div className="text-xs text-slate-400 ml-4">
                Son tarama: {d.son_tarama.bitis || d.son_tarama.baslangic} · {d.son_tarama.taranan} dosya · {d.son_tarama.enfekte} bulgu
              </div>}
            </div>
            <div className="flex gap-2">
              {d.clamav && <button onClick={imzaGuncelle} disabled={imzaYuk || tarariyor}
                className="px-3 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-50">
                {imzaYuk ? 'Güncelleniyor…' : 'İmzaları Güncelle'}</button>}
              <button onClick={tara} disabled={tarariyor}
                className="px-4 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
                {tarariyor ? 'Taranıyor…' : 'Şimdi Tara'}</button>
            </div>
          </div>
          {tarariyor && (
            <div className="mt-3 flex items-center gap-2 text-sm text-brand-600 dark:text-brand-400">
              <span className="inline-block w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
              Tarama sürüyor… (büyük sitelerde birkaç dakika sürebilir)
            </div>
          )}
        </div>

        {/* Bulgular */}
        {/* Mobilde kart çerçevesi kaldırılır: bulgu satırları zaten kart olur,
            aksi hâlde kartlar ikinci bir çerçevenin içine hapsolurdu. */}
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">
            Bulgular {d.son_tarama && <span className="text-xs font-normal text-slate-400">— son taramadan</span>}
          </h3>
          {!d.son_tarama ? (
            <div className="text-center py-8 text-sm text-slate-500 dark:text-slate-400">Henüz tarama yapılmadı. “Şimdi Tara” ile başlayın.</div>
          ) : aktif.length === 0 && d.bulgular.length === 0 ? (
            <div className="text-center py-8">
              <div className="text-3xl mb-2">✅</div>
              <p className="text-sm text-emerald-600 dark:text-emerald-400 font-medium">Temiz — zararlı yazılım bulunmadı.</p>
            </div>
          ) : (
            <div className="lg:overflow-x-auto">
              <table className={`${T.tablo} text-sm`}>
                <thead className={T.baslikGrubu}>
                  <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Dosya</th><th className={T.baslik}>İmza</th><th className={T.baslik}>Motor</th><th className={T.baslik}>Durum</th><th className={T.baslik}></th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {d.bulgular.map((b, i) => (
                    <tr key={i} className={T.satir}>
                      {/* Birincil tanımlayıcı: dosya yolu — mobilde kart başlığı olur */}
                      <td className={`${T.hucreBaslik} font-mono break-all lg:max-w-xs`}>{b.dosya}</td>
                      <td className={T.hucre} data-etiket="İmza">
                        <span className="text-slate-700 dark:text-slate-200 text-right lg:text-left break-all">{b.imza}</span>
                      </td>
                      <td className={T.hucre} data-etiket="Motor"><span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500">{b.motor}</span></td>
                      <td className={T.hucre} data-etiket="Durum">
                        {b.karantina ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantinada</span>
                          : <span className="text-xs text-red-600 dark:text-red-400">⚠ Aktif</span>}
                      </td>
                      {/* Karantinadaki bulguda buton yok: mobilde boş hücre yalnızca
                          asılı bir ayraç çizgisi bırakırdı, o yüzden gizlenir.
                          Masaüstünde kolon hizası için lg:table-cell ile geri gelir. */}
                      <td className={`${T.hucreAksiyon} lg:text-right ${b.karantina ? 'hidden lg:table-cell' : ''}`}>
                        {!b.karantina && <button onClick={() => karantina(b)} className="text-xs text-red-600 dark:text-red-400 hover:underline whitespace-nowrap">Karantinaya al</button>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}
