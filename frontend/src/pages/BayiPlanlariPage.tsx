import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'

type Paket = {
  id: number; ad: string; aciklama: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  fiyat_kurus: number; varsayilan: boolean; bayi_sayisi: number; olusturulma: string
}

const bos = { ad: '', aciklama: '', max_domain: 0, max_disk_mb: 0, max_trafik_mb: 0, fiyat_kurus: 0, varsayilan: false }

function fmtMB(mb: number) {
  if (!mb) return 'Limitsiz'
  if (mb < 1024) return mb + ' MB'
  return (mb / 1024).toFixed(1) + ' GB'
}
function fmtFiyat(kurus: number) {
  if (!kurus) return '—'
  return (kurus / 100).toLocaleString('tr-TR', { minimumFractionDigits: 2 }) + ' ₺'
}

export default function BayiPlanlariPage() {
  const [items, setItems] = useState<Paket[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)
  const [modal, setModal] = useState<'yeni' | Paket | null>(null)
  const [form, setForm] = useState<any>(bos)
  const [kaydet, setKaydet] = useState(false)

  function yukle() {
    setYuk(true)
    api.get<Paket[]>('/reseller-plans').then(r => setItems(r.data)).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  function yeniAc() { setForm(bos); setModal('yeni'); setHata(null); setOk(null) }
  function duzenleAc(p: Paket) {
    setForm({ ad: p.ad, aciklama: p.aciklama, max_domain: p.max_domain, max_disk_mb: p.max_disk_mb,
      max_trafik_mb: p.max_trafik_mb, fiyat_kurus: p.fiyat_kurus, varsayilan: p.varsayilan })
    setModal(p); setHata(null); setOk(null)
  }

  async function gonder(e: React.FormEvent) {
    e.preventDefault(); setHata(null); setOk(null); setKaydet(true)
    const body = {
      ad: form.ad, aciklama: form.aciklama,
      max_domain: Number(form.max_domain), max_disk_mb: Number(form.max_disk_mb),
      max_trafik_mb: Number(form.max_trafik_mb), fiyat_kurus: Number(form.fiyat_kurus),
      varsayilan: !!form.varsayilan,
    }
    try {
      if (modal === 'yeni') { await api.post('/reseller-plans', body); setOk(`"${form.ad}" paketi oluşturuldu.`) }
      else if (modal) { await api.put(`/reseller-plans/${modal.id}`, body); setOk('Paket güncellendi.') }
      setModal(null); yukle()
    } catch (err) { setHata(apiHata(err, 'İşlem başarısız')) }
    finally { setKaydet(false) }
  }

  async function sil(p: Paket) {
    if (!confirm(`"${p.ad}" paketi silinsin mi?`)) return
    setHata(null); setOk(null)
    try { await api.delete(`/reseller-plans/${p.id}`); setOk('Paket silindi.'); yukle() }
    catch (err) { setHata(apiHata(err, 'Silinemedi')) }
  }

  const inp = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm bg-white dark:bg-slate-900 focus:border-brand-500 outline-none'
  const lbl = 'block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Bayi Planları' }]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Bayi Planları</h1>
        <button onClick={yeniAc} className="inline-flex items-center gap-1.5 text-sm px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white rounded-md font-medium">
          <span className="text-base leading-none">+</span> Yeni Bayi Planı
        </button>
      </div>
      <p className="text-sm text-slate-500 mb-4">
        Bayilere satacağınız hazır limit paketleri. <Link to="/bayiler" className="text-brand-600 dark:text-brand-400 hover:underline">Bayi oluştururken</Link> paket seçilir, limitler otomatik atanır.
        Hosting müşterilerine satılan planlar için <Link to="/hizmet-planlari" className="text-brand-600 dark:text-brand-400 hover:underline">Hosting Planları</Link> sayfasını kullanın.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {ok && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{ok}</div>}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
      ) : items.length === 0 ? (
        <EmptyState baslik="Henüz bayi planı yok"
          aciklama="Bayilerinize satacağınız limit paketlerini tanımlayın (ör. Bronz: 10 hosting / 5 GB). Bayi oluştururken tek tıkla atanır."
          buton={{ etiket: 'Bayi Planı Oluştur', onClick: yeniAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                <tr>
                  <th className={T.baslik}>Plan</th>
                  <th className={T.baslik}>Hosting</th>
                  <th className={T.baslik}>Disk</th>
                  <th className={T.baslik}>Trafik</th>
                  <th className={T.baslik}>Fiyat</th>
                  <th className={T.baslik}>Bayi</th>
                  <th className={`${T.baslik} text-right`}>İşlemler</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {items.map(p => (
                  <tr key={p.id} className={T.satir}>
                    <td className={T.hucreBaslik}>
                      <span className="font-semibold text-slate-800 dark:text-slate-200">{p.ad}</span>
                      {p.varsayilan && <span className="ml-2 text-[9px] uppercase tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded align-middle">varsayılan</span>}
                      {p.aciklama && <div className="text-xs text-slate-500 mt-0.5">{p.aciklama}</div>}
                    </td>
                    <td className={T.hucre} data-etiket="Hosting"><span className="font-mono text-sm text-slate-700 dark:text-slate-300">{p.max_domain > 0 ? p.max_domain : '∞'}</span></td>
                    <td className={T.hucre} data-etiket="Disk"><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtMB(p.max_disk_mb)}</span></td>
                    <td className={T.hucre} data-etiket="Trafik"><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtMB(p.max_trafik_mb)}</span></td>
                    <td className={T.hucre} data-etiket="Fiyat"><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtFiyat(p.fiyat_kurus)}</span></td>
                    <td className={T.hucre} data-etiket="Bayi"><span className="text-slate-700 dark:text-slate-300">{p.bayi_sayisi}</span></td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <button onClick={() => duzenleAc(p)} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 lg:mr-3">Düzenle</button>
                      <button onClick={() => sil(p)} className="text-xs text-red-600 dark:text-red-400 hover:text-red-700">Sil</button>
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
          <form onSubmit={gonder} className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <h2 className="text-lg font-semibold mb-4 text-slate-900 dark:text-slate-100">{modal === 'yeni' ? 'Yeni Bayi Planı' : `Plan — ${modal.ad}`}</h2>
            <div className="space-y-3">
              <div><label className={lbl}>Plan adı</label>
                <input className={inp} value={form.ad} onChange={e => setForm({ ...form, ad: e.target.value })} placeholder="Bronz Bayi" autoFocus required /></div>
              <div><label className={lbl}>Açıklama</label>
                <input className={inp} value={form.aciklama} onChange={e => setForm({ ...form, aciklama: e.target.value })} placeholder="Başlangıç paketi" /></div>
              <div className="grid grid-cols-3 gap-2">
                <div><label className={lbl}>Max hosting</label><input className={inp} type="number" min={0} value={form.max_domain} onChange={e => setForm({ ...form, max_domain: e.target.value })} /></div>
                <div><label className={lbl}>Disk (MB)</label><input className={inp} type="number" min={0} value={form.max_disk_mb} onChange={e => setForm({ ...form, max_disk_mb: e.target.value })} /></div>
                <div><label className={lbl}>Trafik (MB)</label><input className={inp} type="number" min={0} value={form.max_trafik_mb} onChange={e => setForm({ ...form, max_trafik_mb: e.target.value })} /></div>
              </div>
              <div><label className={lbl}>Fiyat (kuruş) — 0 = belirtilmemiş</label>
                <input className={inp} type="number" min={0} value={form.fiyat_kurus} onChange={e => setForm({ ...form, fiyat_kurus: e.target.value })} placeholder="49900 = 499,00 ₺" /></div>
              <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
                <input type="checkbox" checked={!!form.varsayilan} onChange={e => setForm({ ...form, varsayilan: e.target.checked })} />
                Varsayılan plan
              </label>
              <p className="text-[11px] text-slate-400">0 = limitsiz. Plan güncellenince MEVCUT bayilerin limitleri değişmez; bayiyi düzenleyip planı yeniden seçin.</p>
            </div>
            <div className="flex justify-end gap-2 mt-5">
              <button type="button" onClick={() => setModal(null)} disabled={kaydet} className="px-4 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-md text-slate-600 dark:text-slate-300">İptal</button>
              <button type="submit" disabled={kaydet} className="px-4 py-2 text-sm bg-slate-900 dark:bg-slate-700 text-white rounded-md font-medium disabled:opacity-50">{kaydet ? '…' : 'Kaydet'}</button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
