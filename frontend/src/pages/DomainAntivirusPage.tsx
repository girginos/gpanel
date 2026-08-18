import { useEffect, useRef, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Bulgu = { dosya: string; imza: string; motor: string; karantina: number }
type Tarama = { id: number; durum: string; motor: string; taranan: number; enfekte: number; baslangic: string; bitis: string }
type Durum = { clamav: boolean; imza_tarihi: string; kullanici: string; son_tarama: Tarama | null; bulgular: Bulgu[] }
type KarantinaKayit = { id: number; orijinal_yol: string; imza: string; seviye: string; puan: number; durum: string; tarih: string; boyut: number; mevcut: boolean }

export default function DomainAntivirusPage() {
  const { onay } = useDialog()
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [tarariyor, setTarariyor] = useState(false)
  const [imzaYuk, setImzaYuk] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [kliste, setKliste] = useState<KarantinaKayit[]>([])
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string } | null>(null)

  function yukle() {
    if (!id) return
    api.get<Durum>(`/domains/${id}/antivirus`).then(r => {
      setD(r.data)
      if (r.data.son_tarama?.durum === 'calisiyor') startPoll(r.data.son_tarama.id)
    }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(() => { yukle(); kyukle(); return () => { if (pollRef.current) clearInterval(pollRef.current) } }, [id])

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
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: `Dosya karantinaya alınsın mı?\n${b.dosya}\n\n(Dosya ~/.karantina altına taşınır ve erişilemez hâle gelir.)` }))) return
    setHata(null)
    try { await api.post(`/domains/${id}/antivirus/karantina`, { dosya: b.dosya }); yukle(); kyukle() }
    catch (e) { setHata(apiHata(e, 'Karantinaya alınamadı')) }
  }

  async function kyukle() {
    if (!id) return
    try { const { data } = await api.get<{ kayitlar: KarantinaKayit[] }>(`/domains/${id}/antivirus/karantina/liste`); setKliste(data.kayitlar || []) } catch { /* sessiz */ }
  }
  async function geriYukle(k: KarantinaKayit) {
    if (!(await onay({ baslik: 'Geri yükleme', mesaj: `Dosya orijinal konumuna geri yüklensin mi?\n${k.orijinal_yol}\n\n(Yanlış pozitifse güvenli; gerçek zararlıysa siteyi tekrar riske atar.)` }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/geri-yukle`, {}); kyukle(); yukle() }
    catch (e: any) { setHata(apiHata(e)) }
  }
  async function karSil(k: KarantinaKayit) {
    if (!(await onay({ baslik: 'Kalıcı silme', mesaj: `Karantinadaki dosya KALICI silinsin mi?\n${k.orijinal_yol}\n\n(Geri alınamaz.)` }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/sil`, {}); kyukle() }
    catch (e: any) { setHata(apiHata(e)) }
  }
  async function karIncele(k: KarantinaKayit) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean }>(`/domains/${id}/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: k.orijinal_yol, icerik: data.ikili ? '[ikili dosya]' : data.icerik }) }
    catch (e: any) { setHata(apiHata(e)) }
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

        {/* Karantina yönetimi */}
        {kliste.length > 0 && (
          <div className="mt-4 lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">🔒 Karantina</h3>
            <div className="lg:overflow-x-auto">
              <table className={`${T.tablo} text-sm`}>
                <thead className={T.baslikGrubu}>
                  <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Dosya</th><th className={T.baslik}>Tespit</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Tarih</th><th className={T.baslik}></th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {kliste.map(k => (
                    <tr key={k.id} className={T.satir}>
                      <td className={`${T.hucreBaslik} font-mono break-all lg:max-w-xs`}>{k.orijinal_yol}</td>
                      <td className={T.hucre} data-etiket="Tespit"><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{k.imza} <span className="text-slate-400">({k.puan})</span></span></td>
                      <td className={T.hucre} data-etiket="Durum">
                        {k.durum === 'karantina' ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantinada</span>
                          : k.durum === 'geri_yuklendi' ? <span className="text-xs text-emerald-600 dark:text-emerald-400">↩ Geri yüklendi</span>
                          : <span className="text-xs text-slate-400">🗑 Silindi</span>}
                      </td>
                      <td className={T.hucre} data-etiket="Tarih"><span className="text-xs text-slate-400">{k.tarih}</span></td>
                      <td className={`${T.hucreAksiyon} lg:text-right`}>
                        {k.durum === 'karantina' && k.mevcut && (
                          <span className="flex gap-2 lg:justify-end whitespace-nowrap">
                            <button onClick={() => karIncele(k)} className="text-xs text-slate-500 hover:underline">İncele</button>
                            <button onClick={() => geriYukle(k)} className="text-xs text-emerald-600 dark:text-emerald-400 hover:underline">Geri yükle</button>
                            <button onClick={() => karSil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">Sil</button>
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* İnceleme modalı — dosya ÇALIŞTIRILMADAN düz metin gösterilir */}
        {inceleModal && (
          <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" onClick={() => setInceleModal(null)}>
            <div className="bg-white dark:bg-slate-800 rounded-2xl max-w-3xl w-full max-h-[80vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
                <span className="text-sm font-mono text-slate-700 dark:text-slate-200 break-all">{inceleModal.ad}</span>
                <button onClick={() => setInceleModal(null)} className="text-slate-400 hover:text-slate-600 text-lg">×</button>
              </div>
              <pre className="p-4 overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </div>
          </div>
        )}

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link></div>
      </div>
    </div>
  )
}
