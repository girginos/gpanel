import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'

type Kopya = { ad: string; boyut_mb: number; tarih: string }


const DKOPYA_EN: Record<string, string> = {
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Henüz kopya yok.": "No copies yet.",
  "Kopya Oluştur": "Create Copy",
  "Kopya oluşturulamadı": "Failed to create copy",
  "Kopyalanıyor…": "Copying…",
  "Yedekle ve Geri Yükle": "Backup and Restore",
  "public_html içeriğinden yeni bir kopya oluştur.": "Create a new copy from the public_html contents.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DKOPYA_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainKopyaPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [liste, setListe] = useState<Kopya[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ok, setOk] = useState<string | null>(null)
  const [olusturuyor, setOlusturuyor] = useState(false)

  function yukle() {
    if (!id) return
    api.get<Kopya[]>(`/domains/${id}/kopya`).then(r => setListe(r.data || [])).catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }).finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function olustur() {
    setHata(null); setOk(null); setOlusturuyor(true)
    try {
      const { data } = await api.post(`/domains/${id}/kopya`, {})
      const m = cevirT(cevir("Kopya oluşturuldu: {0} ({1} MB)"), data.ad, data.boyut_mb)
      setOk(m)
      toast.basari(cevir("Kaydedildi"), m)
      yukle()
    } catch (e) { const m = apiHata(e, cevir("Kopya oluşturulamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
    finally { setOlusturuyor(false) }
  }

  async function sil(k: Kopya) {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `Kopya silinsin mi?\n${k.ad} (${k.boyut_mb} MB)`, tehlike: true }))) return
    setHata(null); setOk(null)
    try { await api.delete(`/domains/${id}/kopya/${k.ad}`); yukle() }
    catch (e) { const m = apiHata(e, 'Silinemedi'); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-3xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: cevir("Domainler"), href: '/domainler' },
          { etiket: 'Web Sitesini Kopyala' },
        ]} />
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Web Sitesini Kopyala</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
          {cevir(cevir("Sitenizin dosyalarının zaman-damgalı bir anlık-görüntüsünü"))} <span className="font-mono">~/kopyalar/</span> {cevir("altında oluşturur — değişiklik yapmadan önce güvenli bir yedek noktası.")}
        </p>

        <div className="bg-amber-50 dark:bg-amber-900/10 border border-amber-200 dark:border-amber-800/40 rounded-lg p-4 mb-4 text-xs text-amber-800 dark:text-amber-300">
          Bu araç yalnızca <b>{cevir(cevir("dosyaları"))}</b> {cevir(cevir("kopyalar (veritabanı dahil değildir). Tam yedek için"))} <b>{cevir("Yedekle ve Geri Yükle")}</b> {cevir(cevir("aracını kullanın."))}
        </div>

        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-5 shadow-xs flex items-center justify-between">
          <div className="text-sm text-slate-600 dark:text-slate-300">{cevir("public_html içeriğinden yeni bir kopya oluştur.")}</div>
          <button onClick={olustur} disabled={olusturuyor}
            className="px-4 py-2 text-sm font-medium bg-dark-800 hover:bg-dark-700 dark:bg-dark-600 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {olusturuyor ? 'Kopyalanıyor…' : cevir("Kopya Oluştur")}
          </button>
        </div>

        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 shadow-xs">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Mevcut kopyalar</h3>
          {yuk ? (
            <div className="text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
          ) : liste.length === 0 ? (
            <div className="text-center py-8">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-7 w-7 mx-auto mb-2 text-slate-300 dark:text-slate-600"><path d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z"/></svg>
              <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz kopya yok.")}</p>
            </div>
          ) : (
            <ul className="divide-y divide-slate-50 dark:divide-dark-600/50">
              {liste.map(k => (
                <li key={k.ad} className="flex items-center justify-between py-2.5">
                  <div>
                    <div className="font-mono text-sm text-slate-700 dark:text-slate-200">{k.ad}</div>
                    <div className="text-xs text-slate-400">{k.tarih} · {k.boyut_mb} MB</div>
                  </div>
                  <button onClick={() => sil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">{cevir("Sil")}</button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="mt-4"><Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">{cevir("← Aboneliğe dön")}</Link></div>
      </div>
    </div>
  )
}
