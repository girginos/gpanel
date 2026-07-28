import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'

type Reseller = {
  id: number; kullanici: string; ad_soyad: string; durum: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  domain_sayisi: number; disk_kullanim_kb: number; paket_id: number; paket_ad: string; son_giris: string; olusturulma: string
}

function fmtMB(mb: number) {
  if (!mb) return 'Limitsiz'
  if (mb < 1024) return mb + ' MB'
  return (mb / 1024).toFixed(1) + ' GB'
}
function fmtKB(kb: number) {
  if (kb < 1024) return kb + ' KB'
  if (kb < 1024 * 1024) return (kb / 1024).toFixed(1) + ' MB'
  return (kb / 1024 / 1024).toFixed(2) + ' GB'
}

type Paket = {
  id: number; ad: string; aciklama: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  fiyat_kurus: number; varsayilan: boolean; bayi_sayisi: number
}

const bos = { kullanici: '', parola: '', ad_soyad: '', paket_id: 0, max_domain: 0, max_disk_mb: 0, max_trafik_mb: 0 }

export default function ResellerlarPage() {
  const [items, setItems] = useState<Reseller[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)
  const [paketler, setPaketler] = useState<Paket[]>([])
  const [modal, setModal] = useState<'yeni' | Reseller | null>(null)
  const [form, setForm] = useState<any>(bos)
  const [kaydet, setKaydet] = useState(false)

  function yukle() {
    setYuk(true)
    api.get<Reseller[]>('/resellers').then(r => setItems(r.data)).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
    api.get<Paket[]>('/reseller-plans').then(r => setPaketler(r.data)).catch(() => setPaketler([]))
  }
  useEffect(yukle, [])

  function yeniAc() { setForm(bos); setModal('yeni'); setHata(null); setOk(null) }
  function duzenleAc(x: Reseller) {
    setForm({ kullanici: x.kullanici, parola: '', ad_soyad: x.ad_soyad, durum: x.durum, paket_id: x.paket_id || 0,
      max_domain: x.max_domain, max_disk_mb: x.max_disk_mb, max_trafik_mb: x.max_trafik_mb })
    setModal(x); setHata(null); setOk(null)
  }

  async function gonder(e: React.FormEvent) {
    e.preventDefault(); setHata(null); setOk(null); setKaydet(true)
    try {
      if (modal === 'yeni') {
        await api.post('/resellers', {
          kullanici: form.kullanici, parola: form.parola, ad_soyad: form.ad_soyad, paket_id: Number(form.paket_id) || 0,
          max_domain: Number(form.max_domain), max_disk_mb: Number(form.max_disk_mb), max_trafik_mb: Number(form.max_trafik_mb),
        })
        setOk(`Bayi "${form.kullanici}" oluşturuldu.`)
      } else if (modal) {
        const body: any = {
          ad_soyad: form.ad_soyad, durum: form.durum, paket_id: Number(form.paket_id) || 0,
          max_domain: Number(form.max_domain), max_disk_mb: Number(form.max_disk_mb), max_trafik_mb: Number(form.max_trafik_mb),
        }
        if (form.parola) body.parola = form.parola
        await api.put(`/resellers/${modal.id}`, body)
        setOk('Bayi güncellendi.')
      }
      setModal(null); yukle()
    } catch (err) { setHata(apiHata(err, 'İşlem başarısız')) }
    finally { setKaydet(false) }
  }

  async function sil(x: Reseller) {
    if (!confirm(`"${x.kullanici}" bayisi silinsin mi?`)) return
    setHata(null); setOk(null)
    try { await api.delete(`/resellers/${x.id}`); setOk('Bayi silindi.'); yukle() }
    catch (err) { setHata(apiHata(err, 'Silinemedi')) }
  }

  const inp = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm bg-white dark:bg-slate-900 focus:border-brand-500 outline-none'
  const lbl = 'block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Bayiler' }]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Bayiler (Reseller)</h1>
        <button onClick={yeniAc} className="inline-flex items-center gap-1.5 text-sm px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white rounded-md font-medium">
          <span className="text-base leading-none">+</span> Yeni Bayi
        </button>
      </div>
      <p className="text-sm text-slate-500 mb-4">Her bayi kendi kullanıcı adı/parolasıyla panele girer; yalnız kendi hosting hesaplarını, planlarını ve DNS şablonunu görür.</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {ok && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{ok}</div>}

      <div className="mb-5 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50 px-4 py-3 text-sm text-slate-600 dark:text-slate-400">
        Bayi paketlerini <Link to="/bayi-planlari" className="text-brand-600 dark:text-brand-400 font-medium hover:underline">Bayi Planları</Link> sayfasından yönetirsiniz; burada bayi eklerken seçersiniz.
      </div>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
      ) : items.length === 0 ? (
        <EmptyState baslik="Henüz bayi yok" aciklama="İlk bayinizi oluşturun; kendi hosting hesaplarını, planlarını ve DNS şablonunu yönetebilir." buton={{ etiket: 'Bayi Oluştur', onClick: yeniAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                <tr>
                  <th className={T.baslik}>Bayi</th>
                  <th className={T.baslik}>Ad Soyad</th>
                  <th className={T.baslik}>Paket</th>
                  <th className={T.baslik}>Hosting</th>
                  <th className={T.baslik}>Disk</th>
                  <th className={T.baslik}>Durum</th>
                  <th className={T.baslik}>Son Giriş</th>
                  <th className={`${T.baslik} text-right`}>İşlemler</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {items.map(x => (
                  <tr key={x.id} className={T.satir}>
                    <td className={T.hucreBaslik}><span className="font-mono text-sm text-slate-800 dark:text-slate-200">{x.kullanici}</span></td>
                    <td className={T.hucre} data-etiket="Ad Soyad"><span className="text-slate-700 dark:text-slate-300">{x.ad_soyad || '—'}</span></td>
                    <td className={T.hucre} data-etiket="Paket"><span className="text-slate-600 dark:text-slate-400">{x.paket_ad || '—'}</span></td>
                    <td className={T.hucre} data-etiket="Hosting"><span className="text-slate-700 dark:text-slate-300">{x.domain_sayisi}{x.max_domain > 0 ? ` / ${x.max_domain}` : ' / ∞'}</span></td>
                    <td className={T.hucre} data-etiket="Disk"><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtKB(x.disk_kullanim_kb)} / {fmtMB(x.max_disk_mb)}</span></td>
                    <td className={T.hucre} data-etiket="Durum">
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${x.durum === 'active' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'}`}>{x.durum === 'active' ? 'Aktif' : 'Askıda'}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Son Giriş"><span className="font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{x.son_giris || '—'}</span></td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <button onClick={() => duzenleAc(x)} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 lg:mr-3">Düzenle</button>
                      <button onClick={() => sil(x)} className="text-xs text-red-600 dark:text-red-400 hover:text-red-700">Sil</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {modal && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => !kaydet && setModal(null)}>
          <form onSubmit={gonder} className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <h2 className="text-lg font-semibold mb-4 text-slate-900 dark:text-slate-100">{modal === 'yeni' ? 'Yeni Bayi' : `Bayi Düzenle — ${modal.kullanici}`}</h2>
            <div className="space-y-3">
              {modal === 'yeni' && (
                <div><label className={lbl}>Kullanıcı adı</label>
                  <input className={inp} value={form.kullanici} onChange={e => setForm({ ...form, kullanici: e.target.value })} placeholder="bayiadi" autoFocus required /></div>
              )}
              <div><label className={lbl}>Ad Soyad</label>
                <input className={inp} value={form.ad_soyad} onChange={e => setForm({ ...form, ad_soyad: e.target.value })} placeholder="Firma / Kişi" /></div>
              {paketler.length > 0 && (
                <div><label className={lbl}>Bayi paketi</label>
                  <select className={inp} value={form.paket_id || 0} onChange={e => {
                    const pid = Number(e.target.value)
                    const pk = paketler.find(x => x.id === pid)
                    setForm({ ...form, paket_id: pid, ...(pk ? { max_domain: pk.max_domain, max_disk_mb: pk.max_disk_mb, max_trafik_mb: pk.max_trafik_mb } : {}) })
                  }}>
                    <option value={0}>Paketsiz (özel limitler)</option>
                    {paketler.map(pk => <option key={pk.id} value={pk.id}>{pk.ad} — {pk.max_domain > 0 ? pk.max_domain + ' hosting' : '∞'} / {fmtMB(pk.max_disk_mb)}</option>)}
                  </select>
                  <p className="text-[11px] text-slate-400 mt-1">Paket seçilirse aşağıdaki limitler paketten gelir.</p>
                </div>
              )}
              <div><label className={lbl}>{modal === 'yeni' ? 'Parola' : 'Yeni parola (boş = değişmez)'}</label>
                <input className={inp} type="password" value={form.parola} onChange={e => setForm({ ...form, parola: e.target.value })} placeholder="En az 8 karakter" required={modal === 'yeni'} /></div>
              <div className="grid grid-cols-3 gap-2">
                <div><label className={lbl}>Max hosting</label><input className={inp} type="number" min={0} value={form.max_domain} onChange={e => setForm({ ...form, max_domain: e.target.value })} /></div>
                <div><label className={lbl}>Disk (MB)</label><input className={inp} type="number" min={0} value={form.max_disk_mb} onChange={e => setForm({ ...form, max_disk_mb: e.target.value })} /></div>
                <div><label className={lbl}>Trafik (MB)</label><input className={inp} type="number" min={0} value={form.max_trafik_mb} onChange={e => setForm({ ...form, max_trafik_mb: e.target.value })} /></div>
              </div>
              <p className="text-[11px] text-slate-400">0 = limitsiz. Disk/trafik havuzu bayinin tüm hosting hesaplarına dağıtılır.</p>
              {modal !== 'yeni' && (
                <div><label className={lbl}>Durum</label>
                  <select className={inp} value={form.durum} onChange={e => setForm({ ...form, durum: e.target.value })}>
                    <option value="active">Aktif</option><option value="suspended">Askıda</option>
                  </select></div>
              )}
            </div>
            <div className="flex justify-end gap-2 mt-5">
              <button type="button" onClick={() => setModal(null)} disabled={kaydet} className="px-4 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-md text-slate-600 dark:text-slate-300">İptal</button>
              <button type="submit" disabled={kaydet} className="px-4 py-2 text-sm bg-slate-900 dark:bg-slate-700 text-white rounded-md font-medium disabled:opacity-50">{kaydet ? '…' : (modal === 'yeni' ? 'Oluştur' : 'Kaydet')}</button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
