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
type Ayar = {
  gercek_zamanli: boolean; zamanli_tarama: boolean; wp_butunluk: boolean; kural_motoru: boolean
  konum_sezgileri: boolean; oto_karantina: boolean; esik_kritik: number; kapsam: string
  haric_yollar: string; cpu_yuzde: number; ram_mb: number; io_agirlik: number
  is_parcacigi: number; dosya_hiz_sn: number; zamanli_saat: string; yuk_esigi: number
}
type AyarYanit = { ayarlar: Ayar; kapasite: { cpu_cekirdek: number; ram_toplam_mb: number; oneri_cpu_yuzde: number; oneri_ram_mb: number; oneri_is_parcacigi: number } }

function Anahtar({ acik, ayarla, etiket, aciklama, uyari }: { acik: boolean; ayarla: (v: boolean) => void; etiket: string; aciklama?: string; uyari?: boolean }) {
  return (
    <button type="button" onClick={() => ayarla(!acik)} className="flex items-start gap-3 text-left w-full py-1.5 group">
      <span className={`mt-0.5 relative inline-flex h-5 w-9 flex-shrink-0 rounded-full transition ${acik ? (uyari ? 'bg-amber-500' : 'bg-emerald-500') : 'bg-slate-300 dark:bg-slate-600'}`}>
        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition ${acik ? 'left-4' : 'left-0.5'}`} />
      </span>
      <span className="min-w-0">
        <span className="block text-sm text-slate-800 dark:text-slate-100">{etiket}</span>
        {aciklama && <span className="block text-xs text-slate-400 dark:text-slate-500">{aciklama}</span>}
      </span>
    </button>
  )
}

export default function AntivirusPanel() {
  const { onay } = useDialog()
  const [d, setD] = useState<Durum | null>(null)
  const [kliste, setKliste] = useState<Kar[]>([])
  const [ayar, setAyar] = useState<Ayar | null>(null)
  const [kap, setKap] = useState<AyarYanit['kapasite'] | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [bilgi, setBilgi] = useState<string | null>(null)
  const [tarariyor, setTarariyor] = useState(false)
  const [kaydediyor, setKaydediyor] = useState(false)
  const [gelismis, setGelismis] = useState(false)
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)

  function durumYukle() {
    api.get<Durum>('/antivirus/durum').then(r => setD(r.data)).catch(e => setHata(apiHata(e)))
    api.get<{ kayitlar: Kar[] }>('/antivirus/karantina').then(r => setKliste(r.data.kayitlar || [])).catch(() => { /* sessiz */ })
  }
  function ayarYukle() {
    api.get<AyarYanit>('/antivirus/ayarlar').then(r => { setAyar(r.data.ayarlar); setKap(r.data.kapasite) }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(() => { durumYukle(); ayarYukle() }, [])

  const set = (k: keyof Ayar, v: any) => setAyar(a => a ? { ...a, [k]: v } : a)

  function yogunluk(p: 'dusuk' | 'dengeli' | 'yuksek') {
    // Dinamik: dusuk=sunucuyu az yorar, yuksek=hizli tarar. yuk_esigi=% cekirdek (0=kapali).
    const on = p === 'dusuk' ? { cpu_yuzde: 25, is_parcacigi: 1, dosya_hiz_sn: 200, yuk_esigi: 50 }
      : p === 'dengeli' ? { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 80 }
      : { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 0 }
    setAyar(a => a ? { ...a, ...on } : a)
  }
  async function ayarKaydet() {
    if (!ayar) return
    setHata(null); setBilgi(null); setKaydediyor(true)
    try {
      await api.put('/antivirus/ayarlar', ayar)
      setBilgi('Ayarlar kaydedildi ve uygulandı.')
      durumYukle(); ayarYukle()
    } catch (e) { setHata(apiHata(e, 'Ayarlar kaydedilemedi')) }
    finally { setKaydediyor(false) }
  }
  async function taraTumu() {
    if (!(await onay({ baslik: 'Tüm sunucuyu tara', mesaj: 'Tüm /home dizini arka planda taranacak. Büyük sunucuda birkaç dakika sürebilir. Başlatılsın mı?' }))) return
    setHata(null); setTarariyor(true)
    try { await api.post('/antivirus/tara-tumu', {}); setBilgi('Tarama başlatıldı — sonuçlar birkaç dakika içinde listeye düşer.'); setTimeout(durumYukle, 3000) }
    catch (e) { setHata(apiHata(e, 'Tarama başlatılamadı')) }
    finally { setTarariyor(false) }
  }
  async function geriYukle(k: Kar) {
    if (!(await onay({ baslik: 'Geri yükleme', mesaj: `Dosya orijinal konumuna geri yüklensin mi?\n${k.orijinal_yol}\n(${k.alan_adi})` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/geri-yukle`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
  }
  async function sil(k: Kar) {
    if (!(await onay({ baslik: 'Kalıcı silme', mesaj: `Karantinadaki dosya KALICI silinsin mi?\n${k.orijinal_yol}\n(${k.alan_adi})\n\nGeri alınamaz.` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/sil`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
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
  const alan = 'w-full px-2.5 py-1.5 text-sm rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-100'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-5xl mx-auto">
        <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Antivirüs' }]} />
        <div className="flex flex-wrap items-center justify-between gap-3 mb-1">
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Antivirüs</h1>
          <button onClick={taraTumu} disabled={tarariyor}
            className="px-4 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {tarariyor ? 'Başlatılıyor…' : 'Tüm Sunucuyu Tara'}</button>
        </div>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">Sunucu geneli zararlı yazılım tespiti, karantina yönetimi ve gerçek zamanlı proaktif izleme.</p>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {bilgi && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bilgi}</div>}

        {/* Durum + istatistik */}
        {d && (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 mb-4">
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm sm:col-span-2">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-2">Motor Durumu</div>
              <div className="space-y-1.5">
                {durumRozet(d.ajan_kurulu, 'Ajan kurulu')}
                {durumRozet(d.izleyici_aktif, 'Gerçek zamanlı izleme' + (d.izleyici_aktif ? ' (aktif)' : ' (kapalı)'))}
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

        {/* AYARLAR / KONTROLLER */}
        {ayar && (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm mb-4">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Ayarlar</h3>
              <button onClick={ayarKaydet} disabled={kaydediyor}
                className="px-4 py-1.5 text-sm font-medium bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50">
                {kaydediyor ? 'Kaydediliyor…' : 'Kaydet'}</button>
            </div>

            <div className="grid gap-x-8 gap-y-1 sm:grid-cols-2">
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">Koruma</div>
                <Anahtar acik={ayar.gercek_zamanli} ayarla={v => set('gercek_zamanli', v)} etiket="Gerçek zamanlı koruma" aciklama="Yeni/değişen dosyayı anında tarar (fanotify). Açınca izleyici servisi başlar." />
                <Anahtar acik={ayar.zamanli_tarama} ayarla={v => set('zamanli_tarama', v)} etiket="Zamanlı tarama" aciklama="Günlük tam tarama." />
                {ayar.zamanli_tarama && (
                  <div className="ml-12 mb-1 flex items-center gap-2">
                    <span className="text-xs text-slate-400">Saat</span>
                    <input type="time" value={ayar.zamanli_saat} onChange={e => set('zamanli_saat', e.target.value)} className={`${alan} w-28`} />
                  </div>
                )}
                <Anahtar acik={ayar.oto_karantina} ayarla={v => set('oto_karantina', v)} uyari etiket="Otomatik karantina" aciklama="Kritik bulgu bulununca dosyayı otomatik karantinaya alır (WP çekirdeği hariç). KAPALIYKEN sadece bildirir." />
              </div>
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">Tespit katmanları</div>
                <Anahtar acik={ayar.kural_motoru} ayarla={v => set('kural_motoru', v)} etiket="Kural motoru" aciklama="İmza/örüntü zinciri (eval+superglobal, shell, webshell)." />
                <Anahtar acik={ayar.konum_sezgileri} ayarla={v => set('konum_sezgileri', v)} etiket="Konum sezgileri" aciklama="uploads/*.php, çift uzantı, gizli dizin." />
                <Anahtar acik={ayar.wp_butunluk} ayarla={v => set('wp_butunluk', v)} etiket="WordPress çekirdek bütünlüğü" aciklama="Resmî md5 ile değişmiş/yabancı çekirdek dosyası." />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-3 mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <label className="block">
                <span className="block text-xs text-slate-400 mb-1">Kapsam</span>
                <select value={ayar.kapsam} onChange={e => set('kapsam', e.target.value)} className={alan}>
                  <option value="host">host (/home — müşteri siteleri)</option>
                  <option value="sunucu">sunucu (/ — tüm sistem)</option>
                </select>
              </label>
              <label className="block">
                <span className="block text-xs text-slate-400 mb-1">Kritik eşik (≥20)</span>
                <input type="number" min={20} value={ayar.esik_kritik} onChange={e => set('esik_kritik', Number(e.target.value))} className={alan} />
              </label>
              <label className="block">
                <span className="block text-xs text-slate-400 mb-1">Hariç yollar (virgülle)</span>
                <input type="text" value={ayar.haric_yollar} onChange={e => set('haric_yollar', e.target.value)} placeholder="node_modules,cache" className={alan} />
              </label>
            </div>

            <div className="mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-2">Tarama yoğunluğu (dinamik kaynak)</div>
              <div className="flex flex-wrap items-center gap-2 mb-3">
                <button type="button" onClick={() => yogunluk('dusuk')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Düşük (sunucuyu az yorar)</button>
                <button type="button" onClick={() => yogunluk('dengeli')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Dengeli</button>
                <button type="button" onClick={() => yogunluk('yuksek')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Yüksek (hızlı)</button>
              </div>
              <label className="flex items-center gap-3 flex-wrap">
                <span className="text-sm text-slate-700 dark:text-slate-200">Dinamik yük eşiği</span>
                <input type="number" min={0} max={400} value={ayar.yuk_esigi} onChange={e => set('yuk_esigi', Number(e.target.value))} className={`${alan} w-24`} />
                <span className="text-xs text-slate-400">% çekirdek · 0 = kapalı. Sistem 1-dk yükü bu değeri (ör. 80 = ×0.8 çekirdek) aşarsa tarama kendini duraklatır; gerçek trafik önceliklenir.</span>
              </label>
            </div>
            <button onClick={() => setGelismis(g => !g)} className="text-xs text-brand-600 dark:text-brand-400 mt-4">{gelismis ? '▾ Kaynak limitleri' : '▸ Kaynak limitleri (cgroup)'}</button>
            {gelismis && (
              <div className="grid gap-4 sm:grid-cols-4 mt-2">
                <label className="block"><span className="block text-xs text-slate-400 mb-1">CPU %{kap ? ` (öneri ${kap.oneri_cpu_yuzde})` : ''}</span><input type="number" min={0} value={ayar.cpu_yuzde} onChange={e => set('cpu_yuzde', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">RAM MB{kap ? ` (öneri ${kap.oneri_ram_mb})` : ''}</span><input type="number" min={0} value={ayar.ram_mb} onChange={e => set('ram_mb', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">İş parçacığı</span><input type="number" min={0} value={ayar.is_parcacigi} onChange={e => set('is_parcacigi', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">Dosya hız/sn (0=sınırsız)</span><input type="number" min={0} value={ayar.dosya_hiz_sn} onChange={e => set('dosya_hiz_sn', Number(e.target.value))} className={alan} /></label>
                <div className="sm:col-span-4 text-xs text-slate-400">0 = otomatik/sınırsız. {kap && `Sunucu: ${kap.cpu_cekirdek} çekirdek · ${kap.ram_toplam_mb} MB RAM.`}</div>
              </div>
            )}
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
