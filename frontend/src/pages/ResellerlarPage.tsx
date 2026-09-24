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
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import Modal from '@/components/Modal'

type Reseller = {
  id: number; kullanici: string; ad_soyad: string; durum: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  domain_sayisi: number; disk_kullanim_kb: number; paket_id: number; paket_ad: string; son_giris: string; olusturulma: string
}


const RES_EN: Record<string, string> = {
  "Türkçe": "English",
  "Limitsiz": "Unlimited",
  "Bayi": "Reseller",
  "oluşturuldu.": "created.",
  "Emin misiniz?": "Are you sure?",
  "bayisi silinsin mi?": "be deleted?",
  "Bayi silindi.": "Reseller deleted.",
  "Silinemedi": "Could not delete",
  "Anasayfa": "Home",
  "Bayiler": "Resellers",
  "Bayiler (Reseller)": "Resellers",
  "Yeni Bayi": "New Reseller",
  "Bayi paketlerini": "You manage reseller packages from the",
  "Bayi Planları": "Reseller Plans",
  "sayfasından yönetirsiniz; burada bayi eklerken seçersiniz.": "page; you select it here when adding a reseller.",
  "Ad Soyad": "Full Name",
  "Paket": "Package",
  "Durum": "Status",
  "İşlemler": "Actions",
  "Aktif": "Active",
  "Askıda": "Suspended",
  "Düzenle": "Edit",
  "Sil": "Delete",
  "Bayi paketi": "Reseller package",
  "Henüz bayi paketi yok — limitleri aşağıdan tek tek girebilir ya da": "No reseller packages yet — you can enter the limits one by one below or",
  "Limitler paketten gelir": "Limits come from the package",
  "Parola": "Password",
  "En az 8 karakter": "At least 8 characters",
  "Gizle": "Hide",
  "Göster": "Show",
  "“Oluştur” ile 16 karakterlik güçlü parola üretebilirsiniz.": "You can generate a strong 16-character password with “Generate”.",
  "Trafik (MB)": "Traffic (MB)",
  "Limitler seçili paketten gelir — değiştirmek için paketi düzenleyin ya da “Paketsiz” seçin.": "Limits come from the selected package — to change them, edit the package or choose “No package”.",
  "Oluştur": "Create",
  "Kaydet": "Save",
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
  "Parola panoya kopyalandı": "Password copied to clipboard",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (RES_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function fmtMB(mb: number) {
  if (!mb) return cevir('Limitsiz')
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
  const toast = useToast()
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
  const [silinen, setSilinen] = useState<number | null>(null)

  function yukle() {
    setYuk(true)
    api.get<Reseller[]>('/resellers').then(r => setItems(r.data)).catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }).finally(() => setYuk(false))
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
        const yeniOk = `${cevir("Bayi")} "${form.kullanici}" ${cevir("oluşturuldu.")}`
        setOk(yeniOk); toast.basari(yeniOk)
      } else if (modal) {
        const body: any = {
          ad_soyad: form.ad_soyad, durum: form.durum, paket_id: Number(form.paket_id) || 0,
          max_domain: Number(form.max_domain), max_disk_mb: Number(form.max_disk_mb), max_trafik_mb: Number(form.max_trafik_mb),
        }
        if (form.parola) body.parola = form.parola
        await api.put(`/resellers/${modal.id}`, body)
        setOk(cevir("Bayi güncellendi."))
        toast.basari(cevir("Bayi güncellendi."))
      }
      setModal(null); yukle()
    } catch (err) {
      const m = apiHata(err, cevir("İşlem başarısız"))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setKaydet(false) }
  }

  async function sil(x: Reseller) {
    if (!(await onay({ baslik: cevir('Emin misiniz?'), mesaj: `"${x.kullanici}" ${cevir("bayisi silinsin mi?")}`, ton: 'tehlike', onayIkonu: I.cop }))) return
    setHata(null); setOk(null); setSilinen(x.id)
    try { await api.delete(`/resellers/${x.id}`); setOk(cevir('Bayi silindi.')); toast.basari(cevir('Bayi silindi.')); yukle() }
    catch (err) {
      const m = apiHata(err, cevir('Silinemedi'))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setSilinen(null) }
  }

  const inp = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm bg-white dark:bg-dark-800 focus:border-brand-500 outline-none'
  const lbl = 'block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir('Bayiler') }]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Bayiler (Reseller)")}</h1>
        <Button color="primary" ikon={I.kullaniciArti} onClick={yeniAc}>
          {cevir("Yeni Bayi")}
        </Button>
      </div>
      <p className="text-sm text-slate-500 mb-4">{cevir("Her bayi kendi kullanıcı adı/parolasıyla panele girer; yalnız kendi hosting hesaplarını, planlarını ve DNS şablonunu görür.")}</p>

      <div className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600 bg-slate-50 dark:bg-dark-700/50 px-4 py-3 text-sm text-slate-600 dark:text-slate-400">
        {cevir("Bayi paketlerini")} <Link to="/bayi-planlari" className="text-brand-600 dark:text-brand-400 font-medium hover:underline">{cevir("Bayi Planları")}</Link> {cevir("sayfasından yönetirsiniz; burada bayi eklerken seçersiniz.")}
      </div>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState baslik={cevir("Henüz bayi yok")} aciklama={cevir("İlk bayinizi oluşturun; kendi hosting hesaplarını, planlarını ve DNS şablonunu yönetebilir.")} buton={{ etiket: cevir("Bayi Oluştur"), onClick: yeniAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
                <tr>
                  <th className={T.baslik}>{cevir("Bayi")}</th>
                  <th className={T.baslik}>{cevir("Ad Soyad")}</th>
                  <th className={T.baslik}>{cevir("Paket")}</th>
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
                    <td className={T.hucre} data-etiket={cevir("Ad Soyad")}><span className="text-slate-700 dark:text-slate-300">{x.ad_soyad || '—'}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Paket")}><span className="text-slate-600 dark:text-slate-400">{x.paket_ad || '—'}</span></td>
                    <td className={T.hucre} data-etiket="Hosting"><span className="text-slate-700 dark:text-slate-300">{x.domain_sayisi}{x.max_domain > 0 ? ` / ${x.max_domain}` : ' / ∞'}</span></td>
                    <td className={T.hucre} data-etiket="Disk"><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtKB(x.disk_kullanim_kb)} / {fmtMB(x.max_disk_mb)}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Durum")}>
                      <Badge color={x.durum === 'active' ? 'success' : 'error'} variant="soft" className="text-[10px] uppercase tracking-wider">{x.durum === 'active' ? cevir('Aktif') : cevir("Askıda")}</Badge>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Son Giriş")}><span className="font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{x.son_giris || '—'}</span></td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Button variant="outlined" boyut="sm" ikon={I.kalem} onClick={() => duzenleAc(x)} className="lg:mr-2">{cevir("Düzenle")}</Button>
                      <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(x)} yukleniyor={silinen === x.id}>{cevir("Sil")}</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <Modal acik={!!modal} baslik={modal === 'yeni' ? cevir('Yeni Bayi') : modal ? cevirT(cevir("Bayi Düzenle — {0}"), modal.kullanici) : ''}
        onKapat={() => setModal(null)} kapatilamaz={kaydet} genislik="lg"
        alt={<>
          <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setModal(null)} disabled={kaydet}>{cevir("İptal")}</Button>
          <Button type="submit" form="gosp-bayi-form" color="primary" ikon={modal === 'yeni' ? I.arti : I.disket} yukleniyor={kaydet}>{modal === 'yeni' ? cevir('Oluştur') : cevir('Kaydet')}</Button>
        </>}>
        {modal && (
          <form id="gosp-bayi-form" onSubmit={gonder}>
            <div className="space-y-3">
              {modal === 'yeni' && (
                <div><label className={lbl}>{cevir("Kullanıcı adı")}</label>
                  <input className={inp} value={form.kullanici} onChange={e => setForm({ ...form, kullanici: e.target.value })} placeholder="bayiadi" autoFocus required /></div>
              )}
              <div><label className={lbl}>{cevir("Ad Soyad")}</label>
                <input className={inp} value={form.ad_soyad} onChange={e => setForm({ ...form, ad_soyad: e.target.value })} placeholder={cevir("Firma / Kişi")} /></div>
              <div><label className={lbl}>{cevir("Bayi paketi")}</label>
                {paketler.length === 0 ? (
                  // 🔴 Paket yokken alani GIZLEME: yonetici paket kavramindan
                  // habersiz kaliyordu. Bos durumda dogrudan olusturma yolunu goster.
                  <div className="rounded-lg border border-dashed border-slate-300 dark:border-slate-600 px-3 py-2.5 text-sm text-slate-500 dark:text-slate-400">
                    {cevir("Henüz bayi paketi yok — limitleri aşağıdan tek tek girebilir ya da")}{' '}
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
                          {cevir("Limitler paketten gelir")} · {asim} · {pk.fazla_satis ? cevir('fazla satışa izin') : cevir("fazla satış kapalı")}
                          {' · '}<Link to={`/bayi-planlari/${pk.id}`} className="text-brand-600 dark:text-brand-400 hover:underline">{cevir("paketi düzenle")}</Link>
                        </p>
                      )
                    })()}
                  </>
                )}
              </div>
              <div>
                <label className={lbl}>{modal === 'yeni' ? cevir('Parola') : cevir("Yeni parola (boş = değişmez)")}</label>
                <div className="flex gap-2">
                  <input
                    className={inp + ' flex-1'}
                    type={parolaGoster ? 'text' : 'password'}
                    value={form.parola}
                    onChange={e => { setForm({ ...form, parola: e.target.value }); setKopyalandi(false) }}
                    placeholder={cevir("En az 8 karakter")}
                    autoComplete="new-password"
                    required={modal === 'yeni'}
                  />
                  {/* Otomatik parola: kripto-rastgele 16 karakter; uretince ACIK gosterilir
                      ki yonetici bayiye iletebilsin (karistirilan 0/O, 1/l harfleri yok). */}
                  <Button
                    type="button"
                    variant="outlined"
                    ikon={I.anahtar}
                    onClick={() => { const p = parolaUret(16); setForm({ ...form, parola: p }); setParolaGoster(true); setKopyalandi(false) }}
                    className="shrink-0"
                    title={cevir("Güçlü parola oluştur")}
                  >
                    {cevir("Oluştur")}
                  </Button>
                  <Button
                    type="button"
                    variant="outlined"
                    isIcon
                    ikon={parolaGoster ? I.gozKapali : I.goz}
                    onClick={() => setParolaGoster(v => !v)}
                    className="shrink-0 w-10"
                    aria-label={parolaGoster ? cevir('Parolayı gizle') : cevir("Parolayı göster")}
                    title={parolaGoster ? cevir('Gizle') : cevir("Göster")}
                  />
                  <Button
                    type="button"
                    variant="outlined"
                    isIcon
                    ikon={I.kopya}
                    disabled={!form.parola}
                    onClick={async () => { if (await panoyaKopyala(form.parola)) { setKopyalandi(true); setTimeout(() => setKopyalandi(false), 2000) } }}
                    className="shrink-0 w-10"
                    aria-label={cevir("Parolayı kopyala")}
                    title={cevir("Kopyala")}
                  />
                </div>
                <p className="text-[11px] mt-1 h-4 text-slate-400 dark:text-slate-500" aria-live="polite">
                  {kopyalandi ? <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400"><Ikon d={I.onay} className="size-3 shrink-0" />{cevir("Parola panoya kopyalandı")}</span>
                    : form.parola ? cevir("Bayiye iletmeyi unutmayın — kaydedildikten sonra bir daha görüntülenemez.")
                    : cevir('“Oluştur” ile 16 karakterlik güçlü parola üretebilirsiniz.')}
                </p>
              </div>
              <div className="grid grid-cols-3 gap-2">
                <div><label className={lbl}>Max hosting</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_domain} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? cevir('Bu limit seçili paketten gelir') : ''} onChange={e => setForm({ ...form, max_domain: e.target.value })} /></div>
                <div><label className={lbl}>Disk (MB)</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_disk_mb} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? cevir('Bu limit seçili paketten gelir') : ''} onChange={e => setForm({ ...form, max_disk_mb: e.target.value })} /></div>
                <div><label className={lbl}>{cevir("Trafik (MB)")}</label><input className={`${inp} ${Number(form.paket_id) ? 'opacity-60 cursor-not-allowed' : ''}`} type="number" min={0} value={form.max_trafik_mb} readOnly={!!Number(form.paket_id)} title={Number(form.paket_id) ? cevir('Bu limit seçili paketten gelir') : ''} onChange={e => setForm({ ...form, max_trafik_mb: e.target.value })} /></div>
              </div>
              <p className="text-[11px] text-slate-400">{Number(form.paket_id) ? cevir('Limitler seçili paketten gelir — değiştirmek için paketi düzenleyin ya da “Paketsiz” seçin.') : cevir("0 = limitsiz. Disk/trafik havuzu bayinin tüm hosting hesaplarına dağıtılır.")}</p>
              {modal !== 'yeni' && (
                <div><label className={lbl}>{cevir("Durum")}</label>
                  <select className={inp} value={form.durum} onChange={e => setForm({ ...form, durum: e.target.value })}>
                    <option value="active">{cevir("Aktif")}</option><option value="suspended">{cevir("Askıda")}</option>
                  </select></div>
              )}
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}
