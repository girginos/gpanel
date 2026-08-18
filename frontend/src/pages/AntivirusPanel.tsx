import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Tarama = { id: number; alan_adi: string; durum: string; kaynak: string; taranan: number; enfekte: number; bitis: string }
type Durum = {
  ajan_kurulu: boolean; izleyici_aktif: boolean; slice_aktif: boolean
  kural_surum: number; kural_uretim: string; kural_sayisi: number
  toplam_karantina: number; toplam_bulgu: number; taranan_domain: number
  son_taramalar: Tarama[]
}
type Kar = { id: number; alan_adi: string; domain_id: number; orijinal_yol: string; imza: string; seviye: string; puan: number; durum: string; tarih: string; mevcut: boolean }

export default function AntivirusPanel() {
  const { onay } = useDialog()
  const [d, setD] = useState<Durum | null>(null)
  const [kliste, setKliste] = useState<Kar[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [tarariyor, setTarariyor] = useState(false)
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)

  function yukle() {
    api.get<Durum>('/antivirus/durum').then(r => setD(r.data)).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
    api.get<{ kayitlar: Kar[] }>('/antivirus/karantina').then(r => setKliste(r.data.kayitlar || [])).catch(() => { /* sessiz */ })
  }
  useEffect(() => { yukle() }, [])

  async function taraTumu() {
    if (!(await onay({ baslik: 'Tüm sunucuyu tara', mesaj: 'Tüm /home dizini arka planda taranacak (zamanlı tarama şimdi çalışır). Büyük sunucuda birkaç dakika sürebilir. Başlatılsın mı?' }))) return
    setHata(null); setTarariyor(true)
    try { await api.post('/antivirus/tara-tumu', {}); setTimeout(yukle, 3000) }
    catch (e) { setHata(apiHata(e, 'Tarama başlatılamadı')) }
    finally { setTarariyor(false) }
  }
  async function geriYukle(k: Kar) {
    if (!(await onay({ baslik: 'Geri yükleme', mesaj: `Dosya orijinal konumuna geri yüklensin mi?\n${k.orijinal_yol}\n(${k.alan_adi})\n\nYanlış pozitifse güvenli; gerçek zararlıysa siteyi tekrar riske atar.` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/geri-yukle`, {}); yukle() }
    catch (e) { setHata(apiHata(e)) }
  }
  async function sil(k: Kar) {
    if (!(await onay({ baslik: 'Kalıcı silme', mesaj: `Karantinadaki dosya KALICI silinsin mi?\n${k.orijinal_yol}\n(${k.alan_adi})\n\nGeri alınamaz.` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/sil`, {}); yukle() }
    catch (e) { setHata(apiHata(e)) }
  }
  async function incele(k: Kar) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean; kesik?: boolean }>(`/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: `${k.orijinal_yol} (${k.alan_adi})`, icerik: data.ikili ? '[ikili dosya]' : data.icerik, kesik: data.kesik }) }
    catch (e) { setHata(apiHata(e)) }
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">Yükleniyor…</div>

  const durumRozet = (aktif: boolean, ad: string) => (
    <span className="inline-flex items-center gap-1.5 text-sm">
      <span className={`w-2 h-2 rounded-full ${aktif ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
      <span className="text-slate-600 dark:text-slate-300">{ad}</span>
    </span>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-5xl mx-auto">
        <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Antivirüs' }]} />
        <div className="flex flex-wrap items-center justify-between gap-3 mb-1">
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Antivirüs</h1>
          <div className="flex gap-2">
            <button onClick={taraTumu} disabled={tarariyor}
              className="px-4 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
              {tarariyor ? 'Başlatılıyor…' : 'Tüm Sunucuyu Tara'}</button>
          </div>
        </div>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">Sunucu geneli zararlı yazılım tespiti, karantina yönetimi ve gerçek zamanlı izleme.</p>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

        {/* Durum + istatistik */}
        {d && (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 mb-4">
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm sm:col-span-2">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-2">Motor Durumu</div>
              <div className="space-y-1.5">
                {durumRozet(d.ajan_kurulu, 'Ajan kurulu')}
                {durumRozet(d.izleyici_aktif, 'Gerçek zamanlı izleme')}
                {durumRozet(d.slice_aktif, 'Kaynak dilimi (cgroup)')}
                <div className="text-xs text-slate-400 pt-1">Kural seti: sürüm {d.kural_surum} · {d.kural_sayisi} kural{d.kural_uretim && d.kural_uretim !== 'gomulu' ? ` · imzalı (${d.kural_uretim})` : ' · gömülü taban'}</div>
              </div>
            </div>
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-1">Karantinada</div>
              <div className="text-3xl font-semibold text-amber-600 dark:text-amber-400">{d.toplam_karantina}</div>
              <div className="text-xs text-slate-400 mt-1">{d.taranan_domain} domain tarandı</div>
            </div>
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-1">Toplam Bulgu</div>
              <div className="text-3xl font-semibold text-slate-700 dark:text-slate-200">{d.toplam_bulgu}</div>
              <Link to="/denetim" className="text-xs text-brand-600 dark:text-brand-400 mt-1 inline-block">Denetim kaydı →</Link>
            </div>
          </div>
        )}

        {/* Karantina (tüm domainler) */}
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm mb-4">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">🔒 Karantina — tüm sunucu</h3>
          {kliste.length === 0 ? (
            <div className="text-center py-8 text-sm text-slate-500 dark:text-slate-400">Karantinada dosya yok.</div>
          ) : (
            <div className="lg:overflow-x-auto">
              <table className={`${T.tablo} text-sm`}>
                <thead className={T.baslikGrubu}>
                  <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>Dosya</th><th className={T.baslik}>Tespit</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Tarih</th><th className={T.baslik}></th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {kliste.map(k => (
                    <tr key={k.id} className={T.satir}>
                      <td className={`${T.hucreBaslik}`}>{k.alan_adi || `#${k.domain_id}`}</td>
                      <td className={T.hucre} data-etiket="Dosya"><span className="font-mono text-xs break-all lg:max-w-xs inline-block">{k.orijinal_yol}</span></td>
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
                            <button onClick={() => incele(k)} className="text-xs text-slate-500 hover:underline">İncele</button>
                            <button onClick={() => geriYukle(k)} className="text-xs text-emerald-600 dark:text-emerald-400 hover:underline">Geri yükle</button>
                            <button onClick={() => sil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">Sil</button>
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

        {/* Son taramalar */}
        {d && d.son_taramalar.length > 0 && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Son taramalar</h3>
            <div className="lg:overflow-x-auto">
              <table className={`${T.tablo} text-sm`}>
                <thead className={T.baslikGrubu}>
                  <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>Kaynak</th><th className={T.baslik}>Taranan</th><th className={T.baslik}>Enfekte</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Bitiş</th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {d.son_taramalar.map(t => (
                    <tr key={t.id} className={T.satir}>
                      <td className={T.hucreBaslik}>{t.alan_adi || '— (sunucu)'}</td>
                      <td className={T.hucre} data-etiket="Kaynak"><span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500">{t.kaynak || 'panel'}</span></td>
                      <td className={T.hucre} data-etiket="Taranan">{t.taranan}</td>
                      <td className={T.hucre} data-etiket="Enfekte"><span className={t.enfekte > 0 ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-400'}>{t.enfekte}</span></td>
                      <td className={T.hucre} data-etiket="Durum"><span className="text-xs text-slate-500">{t.durum}</span></td>
                      <td className={T.hucre} data-etiket="Bitiş"><span className="text-xs text-slate-400">{t.bitis || '—'}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* İnceleme modalı — dosya ÇALIŞTIRILMADAN düz metin */}
        {inceleModal && (
          <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" onClick={() => setInceleModal(null)}>
            <div className="bg-white dark:bg-slate-800 rounded-2xl max-w-3xl w-full max-h-[80vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
                <span className="text-sm font-mono text-slate-700 dark:text-slate-200 break-all">{inceleModal.ad}</span>
                <button onClick={() => setInceleModal(null)} className="text-slate-400 hover:text-slate-600 text-lg">×</button>
              </div>
              {inceleModal.kesik && <div className="px-4 pt-3 text-xs text-amber-600 dark:text-amber-400">⚠ Kesik gösterim — yalnızca ilk 64 KB.</div>}
              <pre className="p-4 overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
