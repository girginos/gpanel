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

type Sekme = 'bulgular' | 'karantina'

// Görüntülenen yoldan kiracı ev-öneki (/home/<kullanıcı>/) çıkarılır: hem daha
// KISA hem de sunucu düzenini sızdırmaz — public_html/… göreli yol kalır.
const kisaYol = (p: string) => p.replace(/^\/home\/[^/]+\//, '')

// YolKutu — uzun dosya yolunu SATIR TAŞIRMADAN, kendi içinde yatay kaydırılan
// bordürlü bir kutuda gösterir (tablo satırı asla ikinci satıra düşmez).
function YolKutu({ yol }: { yol: string }) {
  return (
    <span
      title={yol}
      className="block max-w-full overflow-x-auto whitespace-nowrap font-mono text-xs bg-slate-50 dark:bg-slate-900/40 border border-slate-200 dark:border-slate-700 rounded-md px-2 py-1 text-slate-700 dark:text-slate-200"
    >
      {kisaYol(yol)}
    </span>
  )
}

// Küçük tek-path ikon (buton içi).
const Svg = ({ d, className = 'w-3.5 h-3.5' }: { d: string; className?: string }) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.9} className={className}>
    <path strokeLinecap="round" strokeLinejoin="round" d={d} />
  </svg>
)
const IK = {
  kilit: 'M8 11V7a4 4 0 118 0v4m-9 0h10a1 1 0 011 1v7a1 1 0 01-1 1H7a1 1 0 01-1-1v-7a1 1 0 011-1z',
  ara: 'M21 21l-4.35-4.35M11 18a7 7 0 110-14 7 7 0 010 14z',
  geri: 'M9 15L3 9l6-6M3 9h12a6 6 0 010 12h-3',
  cop: 'M6 7h12M9 7V5a1 1 0 011-1h4a1 1 0 011 1v2m-1 0v11a2 2 0 01-2 2h-4a2 2 0 01-2-2V7',
}
// Buton stilleri — tutarlı, erişilebilir odak halkası.
const BTN = {
  tehlikeCizgi: 'inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded-md border border-red-300 dark:border-red-700/70 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/30 focus:outline-none focus:ring-2 focus:ring-red-400/40 transition whitespace-nowrap',
  notr: 'inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded-md border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-slate-400/30 transition whitespace-nowrap',
  onayCizgi: 'inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded-md border border-emerald-300 dark:border-emerald-700/70 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-900/30 focus:outline-none focus:ring-2 focus:ring-emerald-400/40 transition whitespace-nowrap',
  tehlikeDolu: 'inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded-md bg-red-600 hover:bg-red-700 text-white focus:outline-none focus:ring-2 focus:ring-red-500/50 transition whitespace-nowrap',
}

// G-AV marka logosu — GirginOS'un KENDİ antivirüs motoru. (Imunify lisanslı bir
// markadır; hiçbir yerde kullanılmaz.) Gerçek, dolgulu kalkan SVG — jenerik
// tek-çizgi path değil.
function GavLogo({ className = 'w-11 h-11' }: { className?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} xmlns="http://www.w3.org/2000/svg" role="img" aria-label="G-AV antivirüs">
      <defs>
        <linearGradient id="gav-shield" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#34d399" />
          <stop offset="1" stopColor="#059669" />
        </linearGradient>
      </defs>
      <path d="M24 3.5l15.5 4.8v10.4c0 10.2-6.6 19.6-15.5 22.3C15.1 38.3 8.5 28.9 8.5 18.7V8.3L24 3.5z" fill="url(#gav-shield)" />
      {/* sağ yarıya hafif gölge → hacim hissi */}
      <path d="M24 3.5l15.5 4.8v10.4c0 10.2-6.6 19.6-15.5 22.3V3.5z" fill="#000" opacity="0.07" />
      {/* onay işareti */}
      <path d="M16.3 24.2l5.2 5.1 9.8-11.4" fill="none" stroke="#fff" strokeWidth="3.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

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
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)
  const [klHata, setKlHata] = useState(false)
  const [sekme, setSekme] = useState<Sekme>('bulgular')

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
    try { const { data } = await api.get<{ kayitlar: KarantinaKayit[] }>(`/domains/${id}/antivirus/karantina/liste`); setKliste(data.kayitlar || []); setKlHata(false) } catch { setKlHata(true) }
  }
  async function geriYukle(k: KarantinaKayit) {
    if (!(await onay({ baslik: 'Geri yükleme', mesaj: `Dosya orijinal konumuna geri yüklensin mi?\n${k.orijinal_yol}\n\n(Yanlış pozitifse güvenli; gerçek zararlıysa siteyi tekrar riske atar.)` }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/geri-yukle`, {}); kyukle(); yukle() }
    catch (e: any) { setHata(apiHata(e)) }
  }
  async function karSil(k: KarantinaKayit) {
    if (!(await onay({ baslik: 'Kalıcı silme', mesaj: `Karantinadaki dosya KALICI silinsin mi?\n${k.orijinal_yol}\n\n(Geri alınamaz.)` }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/sil`, {}); kyukle(); yukle() }
    catch (e: any) { setHata(apiHata(e)) }
  }
  async function karIncele(k: KarantinaKayit) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean; kesik?: boolean }>(`/domains/${id}/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: k.orijinal_yol, icerik: data.ikili ? '[ikili dosya]' : data.icerik, kesik: data.kesik }) }
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
  const karAktif = kliste.filter(k => k.durum === 'karantina' && k.mevcut).length

  const sekmeBtn = (k: Sekme, etiket: string, rozet: number, vurgu?: 'kirmizi' | 'kehribar') => (
    <button
      onClick={() => setSekme(k)}
      className={`relative px-4 py-2.5 text-sm font-medium border-b-2 -mb-px transition whitespace-nowrap ${
        sekme === k
          ? 'border-brand-500 text-brand-700 dark:text-brand-300'
          : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'
      }`}
    >
      {etiket}
      {rozet > 0 && (
        <span className={`ml-2 inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1.5 rounded-full text-[11px] font-semibold ${
          vurgu === 'kirmizi' ? 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300'
          : vurgu === 'kehribar' ? 'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300'
          : 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300'
        }`}>{rozet}</span>
      )}
    </button>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-7xl mx-auto">
        <Link to={`/abonelikler/${id}`} className="inline-flex items-center gap-1 text-sm text-brand-600 dark:text-brand-400 hover:underline mb-2">← Aboneliğe dön</Link>
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: 'Antivirüs' },
        ]} />

        {/* Başlık — gerçek G-AV logo görseli */}
        <div className="flex items-start gap-3 mb-4">
          <GavLogo className="w-11 h-11 flex-shrink-0" />
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 leading-tight">
              G-AV <span className="text-slate-400 dark:text-slate-500 font-normal text-lg">— Antivirüs</span>
            </h1>
            <p className="text-sm text-slate-500 dark:text-slate-400">
              <span className="font-mono">public_html</span> dizini GirginOS'un kendi motoruyla taranır: imza + webshell heuristiği.
            </p>
          </div>
        </div>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        {/* Durum + eylemler — her zaman üstte */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="text-sm space-y-0.5">
              <div className="flex flex-wrap items-center gap-2">
                <span className={`w-2 h-2 rounded-full ${d.clamav ? 'bg-emerald-500' : 'bg-amber-500'}`} />
                <span className="text-slate-700 dark:text-slate-200">Motor: <span className="font-medium">{d.clamav ? 'G-AV + İmza veritabanı' : 'G-AV Heuristik'}</span></span>
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

        {/* Sekme çubuğu — sayfa aşağı inmesin diye içerik sekmelere bölündü */}
        <div className="border-b border-slate-200 dark:border-slate-700 flex gap-1 mb-4 overflow-x-auto">
          {sekmeBtn('bulgular', 'Bulgular', aktif.length, 'kirmizi')}
          {sekmeBtn('karantina', 'Karantina', karAktif, 'kehribar')}
        </div>

        {/* ── BULGULAR SEKMESİ ── */}
        {sekme === 'bulgular' && (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 sm:p-5 shadow-sm">
            {!d.son_tarama ? (
              <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">Henüz tarama yapılmadı. “Şimdi Tara” ile başlayın.</div>
            ) : d.bulgular.length === 0 ? (
              <div className="text-center py-10">
                <GavLogo className="w-12 h-12 mx-auto mb-2 opacity-90" />
                <p className="text-sm text-emerald-600 dark:text-emerald-400 font-medium">Temiz — zararlı yazılım bulunmadı.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}>
                    <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                      <th className={T.baslik}>Dosya</th><th className={T.baslik}>İmza</th><th className={T.baslik}>Motor</th><th className={T.baslik}>Durum</th><th className={T.baslik}></th>
                    </tr>
                  </thead>
                  <tbody className={T.govde}>
                    {d.bulgular.map((b, i) => (
                      <tr key={i} className={T.satir}>
                        <td className={`${T.hucreBaslik} lg:min-w-[22rem]`}><YolKutu yol={b.dosya} /></td>
                        <td className={T.hucre} data-etiket="İmza">
                          <span className="text-slate-700 dark:text-slate-200 text-right lg:text-left break-all">{b.imza}</span>
                        </td>
                        <td className={T.hucre} data-etiket="Motor"><span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500">{b.motor}</span></td>
                        <td className={T.hucre} data-etiket="Durum">
                          {b.karantina ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantinada</span>
                            : <span className="text-xs text-red-600 dark:text-red-400">⚠ Aktif</span>}
                        </td>
                        <td className={`${T.hucreAksiyon} lg:text-right ${b.karantina ? 'hidden lg:table-cell' : ''}`}>
                          {!b.karantina && (
                            <button onClick={() => karantina(b)} className={`${BTN.tehlikeCizgi} lg:ml-auto`}>
                              <Svg d={IK.kilit} /> Karantinaya al
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ── KARANTİNA SEKMESİ ── */}
        {sekme === 'karantina' && (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 sm:p-5 shadow-sm">
            {klHata && kliste.length === 0 && (
              <div className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-2 py-2">Karantina listesi yüklenemedi. <button onClick={kyukle} className="underline">Yeniden dene</button></div>
            )}
            {!klHata && kliste.length === 0 ? (
              <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">Karantinada dosya yok.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}>
                    <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                      <th className={T.baslik}>Dosya</th><th className={T.baslik}>Tespit</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Tarih</th><th className={T.baslik}></th>
                    </tr>
                  </thead>
                  <tbody className={T.govde}>
                    {kliste.map(k => (
                      <tr key={k.id} className={T.satir}>
                        <td className={`${T.hucreBaslik} lg:min-w-[22rem]`}><YolKutu yol={k.orijinal_yol} /></td>
                        <td className={T.hucre} data-etiket="Tespit"><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{k.imza} <span className="text-slate-400">({k.puan})</span></span></td>
                        <td className={T.hucre} data-etiket="Durum">
                          {k.durum === 'karantina' ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantinada</span>
                            : k.durum === 'geri_yuklendi' ? <span className="text-xs text-emerald-600 dark:text-emerald-400">↩ Geri yüklendi</span>
                            : <span className="text-xs text-slate-400">🗑 Silindi</span>}
                        </td>
                        <td className={T.hucre} data-etiket="Tarih"><span className="text-xs text-slate-400">{k.tarih}</span></td>
                        <td className={`${T.hucreAksiyon} lg:text-right`}>
                          {k.durum === 'karantina' && k.mevcut && (
                            <span className="flex flex-wrap gap-1.5 lg:justify-end">
                              <button onClick={() => karIncele(k)} className={BTN.notr}><Svg d={IK.ara} /> İncele</button>
                              <button onClick={() => geriYukle(k)} className={BTN.onayCizgi}><Svg d={IK.geri} /> Geri yükle</button>
                              <button onClick={() => karSil(k)} className={BTN.tehlikeDolu}><Svg d={IK.cop} /> Sil</button>
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
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
              {inceleModal.kesik && <div className="px-4 pt-3 text-xs text-amber-600 dark:text-amber-400">⚠ Kesik gösterim — yalnızca ilk 64 KB. Dosya daha uzun; kalanı görünmüyor.</div>}
              <pre className="p-4 overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
