import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { parolaUret, panoyaKopyala } from '@/lib/parola'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Reseller = {
  id: number; kullanici: string; ad_soyad: string; durum: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  domain_sayisi: number; disk_kullanim_kb: number; paket_id: number; paket_ad: string; son_giris: string; olusturulma: string
}


const RES_EN: Record<string, string> = {
  "0 = limitsiz. Disk/trafik havuzu bayinin tüm hosting hesaplarına dağıtılır.": "0 = unlimited. The disk/traffic pool is distributed across all the reseller's hosting accounts.",
  "Bayi Oluştur": "Create Reseller",
  "Bayi güncellendi.": "Reseller updated.",
  "Bayiye iletmeyi unutmayın — kaydedildikten sonra bir daha görüntülenemez.": "Don't forget to share it with the reseller — it can't be viewed again after saving.",
  "Bu limit seçili paketten gelir": "This limit comes from the selected package",
  "Henüz bayi yok": "No resellers yet",
  "Her bayi kendi kullanıcı adı/parolasıyla panele girer; yalnız kendi hosting hesaplarını, planlarını ve DNS şablonunu görür.": "Each reseller logs into the panel with their own username/password; they only see their own hosting accounts, plans and DNS template.",
  "Paketsiz (özel limitler)": "No package (custom limits)",
  "Paketsiz: limitleri aşağıdan siz belirlersiniz.": "No package: you set the limits below.",
  "Parolayı gizle": "Hide password",
  "Parolayı göster": "Show password",
  "Parolayı kopyala": "Copy password",
  "Yeni parola (boş = değişmez)": "New password (empty = unchanged)",
  "aşıma izin yok": "no overuse allowed",
  "bir paket oluşturabilirsiniz": "you can create a package",
  "disk + trafik aşımına izin": "allow disk + traffic overuse",
  "fazla satış kapalı": "overselling off",
  "fazla satışa izin": "allow overselling",
  "paketi düzenle": "edit package",
  "tüm kaynaklarda aşıma izin": "allow overuse on all resources",
  "İlk bayinizi oluşturun; kendi hosting hesaplarını, planlarını ve DNS şablonunu yönetebilir.": "Create your first reseller; they can manage their own hosting accounts, plans and DNS template.",
  "✓ Parola panoya kopyalandı": "✓ Password copied to clipboard",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (RES_EN[tr] || ORTAK_EN[tr] || tr) : tr)

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
  asim_ilkesi?: string; asim_bildirim?: boolean; fazla_satis?: boolean
  id: number; ad: string; aciklama: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  fiyat_kurus: number; varsayilan: boolean; bayi_sayisi: number
}

const bos = { kullanici: '', parola: '', ad_soyad: '', paket_id: 0, max_domain: 0, max_disk_mb: 0, max_trafik_mb: 0 }

export default function ResellerlarPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const [items, setItems] = useState<Reseller[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)
  const [paketler, setPaketler] = useState<Paket[]>([])
  const [parolaGoster, setParolaGoster] = useState(false)
  const [kopyalandi, setKopyalandi] = useState(false)
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
    setModal(x); setHata(null); setOk(null); setParolaGoster(false); setKopyalandi(false)
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
        setOk(cevir("Bayi güncellendi."))
      }
      setModal(null); yukle()
    } catch (err) { setHata(apiHata(err, cevir("İşlem başarısız"))) }
    finally { setKaydet(false) }
  }

  async function sil(x: Reseller) {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `"${x.kullanici}" bayisi silinsin mi?`, tehlike: true }))) return
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
      <p className="text-sm text-slate-500 mb-4">{cevir("Her bayi kendi kullanıcı adı/parolasıyla panele girer; yalnız kendi hosting hesaplarını, planlarını ve DNS şablonunu görür.")}</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {ok && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{ok}</div>}

      <div className="mb-5 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50 px-4 py-3 text-sm text-slate-600 dark:text-slate-400">
        Bayi paketlerini <Link to="/bayi-planlari" className="text-brand-600 dark:text-brand-400 font-medium hover:underline">{cevir("Bayi Planları")}</Link> {cevir("sayfasından yönetirsiniz; burada bayi eklerken seçersiniz.")}
      </div>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState baslik={cevir("Henüz bayi yok")} aciklama={cevir("İlk bayinizi oluşturun; kendi hosting hesaplarını, planlarını ve DNS şablonunu yönetebilir.")} buton={{ etiket: cevir("Bayi Oluştur"), onClick: yeniAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                <tr>
                  <th className={T.baslik}>{cevir("Bayi")}</th>
                  <th className={T.baslik}>Ad Soyad</th>
                  <th className={T.baslik}>Paket</th>
                  <th className={T.baslik}>Hosting</th>
                  <th className={T.baslik}>Disk</th>
                  <th className={T.baslik}>{cevir("Durum")}</th>
                  <th className={T.baslik}>{cevir("Son Giriş")}</th>
                  <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
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
                    <td className={T.hucre} data-etiket={cevir("Durum")}>
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${x.durum === 'active' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'}`}>{x.durum === 'active' ? 'Aktif' : cevir("Askıda")}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Son Giriş")}><span className="font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{x.son_giris || '—'}</span></td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <button onClick={() => duzenleAc(x)} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 lg:mr-3">{cevir("Düzenle")}</button>
                      <button onClick={() => sil(x)} className="text-xs text-red-600 dark:text-red-400 hover:text-red-700">{cevir("Sil")}</button>
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
            <h2 className="text-lg font-semibold mb-4 text-slate-900 dark:text-slate-100">{modal === 'yeni' ? 'Yeni Bayi' : cevirT(cevir("Bayi Düzenle — {0}"), modal.kullanici)}</h2>
            <div className="space-y-3">
              {modal === 'yeni' && (
                <div><label className={lbl}>{cevir("Kullanıcı adı")}</label>
                  <input className={inp} value={form.kullanici} onChange={e => setForm({ ...form, kullanici: e.target.value })} placeholder="bayiadi" autoFocus required /></div>
              )}
              <div><label className={lbl}>Ad Soyad</label>
                <input className={inp} value={form.ad_soyad} onChange={e => setForm({ ...form, ad_soyad: e.target.value })} placeholder={cevir("Firma / Kişi")} /></div>
              <div><label className={lbl}>Bayi paketi</label>
                {paketler.length === 0 ? (
                  // 🔴 Paket yokken alani GIZLEME: yonetici paket kavramindan
                  // habersiz kaliyordu. Bos durumda dogrudan olusturma yolunu goster.
                  <div className="rounded-lg border border-dashed border-slate-300 dark:border-slate-600 px-3 py-2.5 text-sm text-slate-500 dark:text-slate-400">
                    Henüz bayi paketi yok — limitleri aşağıdan tek tek girebilir ya da{' '}
                    <Link to="/bayi-planlari/yeni" className="text-brand-600 dark:text-brand-400 font-medium hover:underline">{cevir("bir paket oluşturabilirsiniz")}</Link>.
                  </div>
                ) : (
                  <>
                    <select className={inp} value={form.paket_id || 0} onChange={e => {
                      const pid = Number(e.target.value)
                      const pk = paketler.find(x => x.id === pid)
                      setForm({ ...form, paket_id: pid, ...(pk ? { max_domain: pk.max_domain, max_disk_mb: pk.max_disk_mb, max_trafik_mb: pk.max_trafik_mb } : {}) })
                    }}>
                      <option value={0}>{cevir("Paketsiz (özel limitler)")}</option>
                      {paketler.map(pk => <option key={pk.id} value={pk.id}>{pk.ad} — {pk.max_domain > 0 ? pk.max_domain + ' hosting' : '∞'} / {fmtMB(pk.max_disk_mb)}</option>)}
                    </select>
                    {(() => {
                      const pk = paketler.find(x => x.id === Number(form.paket_id))
                      if (!pk) return <p className="text-[11px] text-slate-400 mt-1">{cevir("Paketsiz: limitleri aşağıdan siz belirlersiniz.")}</p>
                      const asim = pk.asim_ilkesi === 'yok' ? cevir("aşıma izin yok")
                        : pk.asim_ilkesi === 'tumu' ? cevir("tüm kaynaklarda aşıma izin")
                        : cevir("disk + trafik aşımına izin")
                      return (
                        <p className="text-[11px] text-slate-500 dark:text-slate-400 mt-1">
                          Limitler paketten gelir · {asim} · {pk.fazla_satis ? 'fazla satışa izin' : cevir("fazla satış kapalı")}
                          {' · '}<Link to={`/bayi-planlari/${pk.id}`} className="text-brand-600 dark:text-brand-400 hover:underline">{cevir("paketi düzenle")}</Link>
                        </p>
                      )
                    })()}
                  </>
                )}
              </div>
              <div>
                <label className={lbl}>{modal === 'yeni' ? 'Parola' : cevir("Yeni parola (boş = değişmez)")}</label>
                <div className="flex gap-2">
                  <input
                    className={inp + ' flex-1'}
                    type={parolaGoster ? 'text' : 'password'}
                    value={form.parola}
                    onChange={e => { setForm({ ...form, parola: e.target.value }); setKopyalandi(false) }}
                    placeholder="En az 8 karakter"
                    autoComplete="new-password"
                    required={modal === 'yeni'}
                  />
                  {/* Otomatik parola: kripto-rastgele 16 karakter; uretince ACIK gosterilir
                      ki yonetici bayiye iletebilsin (karistirilan 0/O, 1/l harfleri yok). */}
                  <button
                    type="button"
                    onClick={() => { const p = parolaUret(16); setForm({ ...form, parola: p }); setParolaGoster(true); setKopyalandi(false) }}
                    className="shrink-0 rounded-lg border border-slate-300 dark:border-slate-600 px-3 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
                    title={cevir("Güçlü parola oluştur")}
                  >
                    {cevir("Oluştur")}
                  </button>
                  <button
                    type="button"
                    onClick={() => setParolaGoster(v => !v)}
                    className="shrink-0 rounded-lg border border-slate-300 dark:border-slate-600 px-2.5 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
                    aria-label={parolaGoster ? 'Parolayı gizle' : cevir("Parolayı göster")}
                    title={parolaGoster ? 'Gizle' : cevir("Göster")}
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                      {parolaGoster
                        ? <path strokeLinecap="round" strokeLinejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
                        : <><path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /><path strokeLinecap="round" strokeLinejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" /></>}
                    </svg>
                  </button>
                  <button
                    type="button"
                    disabled={!form.parola}
                    onClick={async () => { if (await panoyaKopyala(form.parola)) { setKopyalandi(true); setTimeout(() => setKopyalandi(false), 2000) } }}
                    className="shrink-0 rounded-lg border border-slate-300 dark:border-slate-600 px-2.5 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
                    aria-label={cevir("Parolayı kopyala")}
                    title={cevir("Kopyala")}
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </button>
                </div>
                <p className="text-[11px] mt-1 h-4 text-slate-400 dark:text-slate-500" aria-live="polite">
                  {kopyalandi ? <span className="text-emerald-600 dark:text-emerald-400">{cevir("✓ Parola panoya kopyalandı")}</span>
                    : form.parola ? cevir("Bayiye iletmeyi unutmayın — kaydedildikten sonra bir daha görüntülenemez.")
                    : '“Oluştur” ile 16 karakterlik güçlü parola üretebilirsiniz.'}
                </p>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <div><label className={lbl}>Max hosting</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_domain} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? 'Bu limit seçili paketten gelir' : ''} onChange={e => setForm({ ...form, max_domain: e.target.value })} /></div>
                <div><label className={lbl}>Disk (MB)</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_disk_mb} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? 'Bu limit seçili paketten gelir' : ''} onChange={e => setForm({ ...form, max_disk_mb: e.target.value })} /></div>
                <div><label className={lbl}>Trafik (MB)</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_trafik_mb} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? 'Bu limit seçili paketten gelir' : ''} onChange={e => setForm({ ...form, max_trafik_mb: e.target.value })} /></div>
              </div>
              <p className="text-[11px] text-slate-400">{Number(form.paket_id) ? 'Limitler seçili paketten gelir — değiştirmek için paketi düzenleyin ya da “Paketsiz” seçin.' : cevir("0 = limitsiz. Disk/trafik havuzu bayinin tüm hosting hesaplarına dağıtılır.")}</p>
              {modal !== 'yeni' && (
                <div><label className={lbl}>{cevir("Durum")}</label>
                  <select className={inp} value={form.durum} onChange={e => setForm({ ...form, durum: e.target.value })}>
                    <option value="active">{cevir("Aktif")}</option><option value="suspended">{cevir("Askıda")}</option>
                  </select></div>
              )}
            </div>
            <div className="flex justify-end gap-2 mt-5">
              <button type="button" onClick={() => setModal(null)} disabled={kaydet} className="px-4 py-2 text-sm border border-slate-300 dark:border-slate-600 rounded-md text-slate-600 dark:text-slate-300">{cevir("İptal")}</button>
              <button type="submit" disabled={kaydet} className="px-4 py-2 text-sm bg-slate-900 dark:bg-slate-700 text-white rounded-md font-medium disabled:opacity-50">{kaydet ? '…' : (modal === 'yeni' ? 'Oluştur' : 'Kaydet')}</button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
