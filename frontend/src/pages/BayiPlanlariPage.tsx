import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'

type Paket = {
  id: number; ad: string; aciklama: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  fiyat_kurus: number; varsayilan: boolean; bayi_sayisi: number; olusturulma: string
}


const BPLAN_EN: Record<string, string> = {
  "Türkçe": "English",
  "Bayi Planı Oluştur": "Create Reseller Plan",
  "Bayilerinize satacağınız limit paketlerini tanımlayın (ör. Bronz: 10 hosting / 5 GB). Bayi oluştururken tek tıkla atanır.": "Define the limit packages you'll sell to your resellers (e.g. Bronze: 10 hosting / 5 GB). Assigned with one click when creating a reseller.",
  "Henüz bayi planı yok": "No reseller plans yet",
  "Yeni Bayi Planı": "New Reseller Plan",
  "Bayi Planları": "Reseller Plans",
  "Bayilere satacağınız hazır limit paketleri.": "Ready-made limit packages to sell to your resellers.",
  "Bayi oluştururken": "When creating a reseller",
  "paket seçilir, limitler otomatik atanır.": "a package is selected and limits are assigned automatically.",
  "Hosting müşterilerine satılan planlar için": "For plans sold to hosting customers,",
  "Hosting Planları": "Hosting Plans",
  "sayfasını kullanın.": "use the page.",
  "İşlem başarısız": "Operation failed",
  "Yükleniyor…": "Loading…",
  "Plan": "Plan",
  "Bayi": "Reseller",
  "İşlemler": "Actions",
  "varsayılan": "default",
  "Düzenle": "Edit",
  "Sil": "Delete",
  "Disk": "Disk",
  "Trafik": "Traffic",
  "Fiyat": "Price",
  "Limitsiz": "Unlimited",
  "Emin misiniz?": "Are you sure?",
  "paketi silinsin mi?": "package be deleted?",
  "Paket silindi.": "Package deleted.",
  "Silinemedi": "Could not delete",
  "Anasayfa": "Home",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BPLAN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function fmtMB(mb: number) {
  if (!mb) return cevir('Limitsiz')
  if (mb < 1024) return mb + ' MB'
  return (mb / 1024).toFixed(1) + ' GB'
}
function fmtFiyat(kurus: number) {
  if (!kurus) return '—'
  return (kurus / 100).toLocaleString('tr-TR', { minimumFractionDigits: 2 }) + ' ₺'
}

export default function BayiPlanlariPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const nav = useNavigate()
  const [items, setItems] = useState<Paket[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setOk] = useState<string | null>(null)

  function yukle() {
    setYuk(true)
    api.get<Paket[]>('/reseller-plans').then(r => setItems(r.data)).catch(e => {
      const m = apiHata(e)
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }).finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  function yeniAc() { nav('/bayi-planlari/yeni') }   // modal yerine OZEL SAYFA (ilke ayarlari icin)
  async function sil(p: Paket) {
    if (!(await onay({ baslik: cevir('Emin misiniz?'), mesaj: `"${p.ad}" ${cevir('paketi silinsin mi?')}`, tehlike: true }))) return
    setHata(null); setOk(null)
    try {
      await api.delete(`/reseller-plans/${p.id}`)
      setOk(cevir('Paket silindi.'))
      toast.basari(cevir('Paket silindi.'))
      yukle()
    }
    catch (err) {
      const m = apiHata(err, cevir('Silinemedi'))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Bayi Planları") }]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Bayi Planları")}</h1>
        <Button color="primary" onClick={yeniAc} className="gap-1.5 text-sm px-3 py-1.5">
          <span className="text-base leading-none">+</span> {cevir("Yeni Bayi Planı")}
        </Button>
      </div>
      <p className="text-sm text-slate-500 mb-4">
        {cevir("Bayilere satacağınız hazır limit paketleri.")} <Link to="/bayiler" className="text-brand-600 dark:text-brand-400 hover:underline">{cevir("Bayi oluştururken")}</Link> {cevir("paket seçilir, limitler otomatik atanır.")}
        {cevir("Hosting müşterilerine satılan planlar için")} <Link to="/hizmet-planlari" className="text-brand-600 dark:text-brand-400 hover:underline">{cevir("Hosting Planları")}</Link> {cevir("sayfasını kullanın.")}
      </p>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState baslik={cevir("Henüz bayi planı yok")}
          aciklama={cevir("Bayilerinize satacağınız limit paketlerini tanımlayın (ör. Bronz: 10 hosting / 5 GB). Bayi oluştururken tek tıkla atanır.")}
          buton={{ etiket: cevir("Bayi Planı Oluştur"), onClick: yeniAc }} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
                <tr>
                  <th className={T.baslik}>{cevir("Plan")}</th>
                  <th className={T.baslik}>Hosting</th>
                  <th className={T.baslik}>{cevir("Disk")}</th>
                  <th className={T.baslik}>{cevir("Trafik")}</th>
                  <th className={T.baslik}>{cevir("Fiyat")}</th>
                  <th className={T.baslik}>{cevir("Bayi")}</th>
                  <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {items.map(p => (
                  <tr key={p.id} className={T.satir}>
                    <td className={T.hucreBaslik}>
                      <span className="font-semibold text-slate-800 dark:text-slate-200">{p.ad}</span>
                      {p.varsayilan && <span className="ml-2 text-[9px] uppercase tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded align-middle">{cevir("varsayılan")}</span>}
                      {p.aciklama && <div className="text-xs text-slate-500 mt-0.5">{p.aciklama}</div>}
                    </td>
                    <td className={T.hucre} data-etiket="Hosting"><span className="font-mono text-sm text-slate-700 dark:text-slate-300">{p.max_domain > 0 ? p.max_domain : '∞'}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Disk")}><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtMB(p.max_disk_mb)}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Trafik")}><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtMB(p.max_trafik_mb)}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Fiyat")}><span className="font-mono text-xs text-slate-600 dark:text-slate-400">{fmtFiyat(p.fiyat_kurus)}</span></td>
                    <td className={T.hucre} data-etiket={cevir("Bayi")}><span className="text-slate-700 dark:text-slate-300">{p.bayi_sayisi}</span></td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <button onClick={() => nav(`/bayi-planlari/${p.id}`)} className="text-xs font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 lg:mr-3">{cevir("Düzenle")}</button>
                      <button onClick={() => sil(p)} className="text-xs text-red-600 dark:text-red-400 hover:text-red-700">{cevir("Sil")}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
