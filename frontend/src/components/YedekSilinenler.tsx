import { useCallback, useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api, apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'
import { Link } from 'react-router-dom'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'

// Silinmiş hesap yedekleri (çöp kutusu). Hosting ya da bayi silinince yedekleri SİLİNMEZ; burada
// ayarlanan süre boyunca saklanır (boş = hostingin yedek slotu kadar gün, en az 7; 0 = otomatik silme yok).
// Süre dolunca otomatik, ya da buradan elle kalıcı silinir. Yöneticiye özel.

const EN: Record<string, string> = {
  'Silinmiş hesap yedekleri': 'Deleted account backups',
  'Hosting ya da bayi silinince yedekleri silinmez; burada saklanır ve süre dolunca otomatik temizlenir.': 'When a hosting account or reseller is deleted its backups are not deleted; they are kept here and cleaned up automatically when the period ends.',
  'Saklama süresi (gün)': 'Retention (days)',
  'Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.': 'Empty = the hosting’s backup slot (at least 7 days). 0 = never delete automatically.',
  'Kaydet': 'Save',
  'Kaydedildi': 'Saved',
  'Kaydedilemedi': 'Could not save',
  'Silinmiş hesap yedeği yok.': 'No deleted account backups.',
  'Alan adı': 'Domain',
  'Silinme': 'Deleted',
  'Son saklama': 'Keep until',
  'Yedek': 'Backups',
  'Yedek dosyalarını göster': 'Show backup files',
  'Yedek dosyalarını gizle': 'Hide backup files',
  'Yerel kopya yok': 'No local copy',
  'Domaine bağla': 'Link to domain',
  'Bu alan adının yedek sayfasında görünür ve oradan geri yüklenir.': 'Shows up on this domain’s backup page, where it can be restored.',
  '{0} yedek bağlandı': '{0} backups linked',
  'Yedek sayfasına git': 'Open backup page',
  'Bağlanamadı': 'Could not link',
  'otomatik silinmez': 'never automatic',
  'İndir': 'Download',
  'Kalıcı sil': 'Delete permanently',
  'Emin misiniz?': 'Are you sure?',
  '"{0}" için saklanan yedekler KALICI silinsin mi? Uzak depodaki kopyalar da silinir. Geri alınamaz.': 'Permanently delete the stored backups for "{0}"? Remote copies are removed too. This cannot be undone.',
  'Uzak kopya silinemedi. Yine de yerel yedekleri silip uzak kopyaları BIRAKMAK ister misiniz?': 'The remote copy could not be deleted. Delete the local backups anyway and LEAVE the remote copies?',
  'Silindi': 'Deleted',
  'İşlem başarısız': 'Operation failed',
  'İndirme başarısız': 'Download failed',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Yedek = { id: number; dosya: string; boyut_b: number; olusturma: string; yerel: boolean }
type Grup = {
  sk: string; alan_adi: string; reseller_id?: number | null; silinme: string; son_kullanma: string
  dosya_sayisi: number; boyut_b: number; yerel_var: boolean; baglanabilir_domain_id: number | null
  yedekler: Yedek[]
}

function fmtB(b: number): string {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(1) + ' GB'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + ' MB'
  return Math.max(1, Math.round(b / 1024)) + ' KB'
}

export default function YedekSilinenler() {
  useTranslation()
  const toast = useToast()
  const { onay } = useDialog()
  const [satirlar, setSatirlar] = useState<Grup[]>([])
  const [gun, setGun] = useState('')
  const [kaydediyor, setKaydediyor] = useState(false)
  const [acik, setAcik] = useState<Record<string, boolean>>({})
  const [bagli, setBagli] = useState<Record<string, number>>({})
  const [mesgul, setMesgul] = useState('')

  const yukle = useCallback(() => {
    api.get<Grup[]>('/admin/silinen-yedekler').then(r => setSatirlar(r.data)).catch(() => setSatirlar([]))
    api.get<{ gun: number | null }>('/admin/silinen-yedekler/ayar').then(r => setGun(r.data.gun == null ? '' : String(r.data.gun))).catch(() => undefined)
  }, [])
  useEffect(() => { yukle() }, [yukle])

  async function ayarKaydet() {
    const v = gun.trim()
    if (v !== '' && !/^\d{1,4}$/.test(v)) { toast.hata(cevir('Kaydedilemedi'), cevir('Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.')); return }
    setKaydediyor(true)
    try {
      await api.put('/admin/silinen-yedekler/ayar', { gun: v === '' ? null : Number(v) })
      toast.basari(cevir('Kaydedildi'))
    } catch (e) { toast.hata(cevir('Kaydedilemedi'), apiHata(e)) }
    finally { setKaydediyor(false) }
  }

  async function sil(s: Grup, uzagiYoksay = false) {
    if (!uzagiYoksay && !(await onay({ baslik: cevir('Emin misiniz?'), mesaj: cevirT(cevir('"{0}" için saklanan yedekler KALICI silinsin mi? Uzak depodaki kopyalar da silinir. Geri alınamaz.'), s.alan_adi), ton: 'tehlike', onayIkonu: I.cop }))) return
    try {
      await api.delete(`/admin/silinen-yedekler/${encodeURIComponent(s.sk)}${uzagiYoksay ? '?uzagi_yoksay=1' : ''}`)
      toast.basari(cevir('Silindi')); yukle()
    } catch (e) {
      const x = e as { response?: { status?: number; data?: { uzagi_yoksay?: boolean } } }
      if (x.response?.status === 409 && x.response.data?.uzagi_yoksay && !uzagiYoksay) {
        toast.hata(cevir('İşlem başarısız'), apiHata(e))
        if (await onay({ baslik: cevir('Emin misiniz?'), mesaj: cevir('Uzak kopya silinemedi. Yine de yerel yedekleri silip uzak kopyaları BIRAKMAK ister misiniz?'), ton: 'tehlike', onayIkonu: I.cop })) await sil(s, true)
        return
      }
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    }
  }

  async function bagla(s: Grup) {
    if (s.baglanabilir_domain_id == null) return
    setMesgul(s.sk)
    try {
      const { data } = await api.post<{ ok: boolean; baglanan: number }>(`/admin/silinen-yedekler/${encodeURIComponent(s.sk)}/bagla`, { domain_id: s.baglanabilir_domain_id })
      toast.basari(cevirT(cevir('{0} yedek bağlandı'), data.baglanan))
      setBagli(b => ({ ...b, [s.sk]: s.baglanabilir_domain_id as number }))
      yukle()
    } catch (e) { toast.hata(cevir('Bağlanamadı'), apiHata(e)) }
    finally { setMesgul('') }
  }

  function indir(y: Yedek) {
    const tok = useAuth.getState().token || ''
    fetch(`/api/v1/admin/silinen-yedekler/yedek/${y.id}/indir`, { headers: { Authorization: `Bearer ${tok}` } })
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.blob() })
      .then(blob => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob); a.download = y.dosya; a.click()
        setTimeout(() => URL.revokeObjectURL(a.href), 1000)
      })
      .catch(e => toast.hata(cevir('İndirme başarısız'), e instanceof Error ? e.message : String(e)))
  }

  return (
    <div className="mt-4 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60">
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Silinmiş hesap yedekleri')}</h3>
        <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('Hosting ya da bayi silinince yedekleri silinmez; burada saklanır ve süre dolunca otomatik temizlenir.')}</p>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <label className="text-xs text-slate-600 dark:text-slate-300">
            {cevir('Saklama süresi (gün)')}
            <input value={gun} onChange={e => setGun(e.target.value)} inputMode="numeric" placeholder="slot"
              className="block mt-1 w-28 px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded-md text-sm bg-white dark:bg-dark-800 focus:border-brand-500 outline-none" />
          </label>
          <Button color="primary" boyut="sm" onClick={ayarKaydet} yukleniyor={kaydediyor}>{cevir('Kaydet')}</Button>
          <span className="text-[11px] text-slate-400 basis-full sm:basis-auto">{cevir('Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.')}</span>
        </div>
      </div>
      <div className="divide-y divide-slate-100 dark:divide-dark-600/60">
        {satirlar.length === 0 && <div className="px-4 py-6 text-center text-sm text-slate-400">{cevir('Silinmiş hesap yedeği yok.')}</div>}
        {satirlar.map(s => {
          const ac = !!acik[s.sk]
          const bagliId = bagli[s.sk]
          return (
            <div key={s.sk} className="px-4 py-2.5 text-sm">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <button type="button" onClick={() => setAcik(a => ({ ...a, [s.sk]: !ac }))} aria-expanded={ac}
                  aria-label={cevir(ac ? 'Yedek dosyalarını gizle' : 'Yedek dosyalarını göster')} title={cevir(ac ? 'Yedek dosyalarını gizle' : 'Yedek dosyalarını göster')}
                  className="inline-flex items-center gap-1 font-medium text-slate-800 dark:text-slate-200 break-all text-left">
                  <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0 fill-none stroke-current" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={ac ? 'm6 9 6 6 6-6' : 'm9 18 6-6-6-6'} /></svg>
                  {s.alan_adi}
                </button>
                <span className="font-mono text-xs text-slate-400">{cevir('Silinme')}: {s.silinme}</span>
                <span className="font-mono text-xs text-slate-400">{cevir('Son saklama')}: {s.son_kullanma || cevir('otomatik silinmez')}</span>
                <span className="font-mono text-xs text-slate-400">{s.dosya_sayisi} {cevir('Yedek').toLowerCase()} · {fmtB(s.boyut_b)}</span>
                <span className="ml-auto flex flex-wrap items-center gap-2">
                  {bagliId != null && (
                    <Link to={`/abonelikler/${bagliId}/yedekler`} className="text-xs text-brand-600 dark:text-brand-400 hover:underline">{cevir('Yedek sayfasına git')}</Link>
                  )}
                  {s.baglanabilir_domain_id != null && (
                    <Button type="button" variant="outlined" boyut="sm" yukleniyor={mesgul === s.sk} onClick={() => bagla(s)}
                      title={cevir('Bu alan adının yedek sayfasında görünür ve oradan geri yüklenir.')} aria-label={`${cevir('Domaine bağla')}: ${s.alan_adi}`}>{cevir('Domaine bağla')}</Button>
                  )}
                  <Button type="button" color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(s)}
                    title={cevir('Kalıcı sil')} aria-label={`${cevir('Kalıcı sil')}: ${s.alan_adi}`}>{cevir('Kalıcı sil')}</Button>
                </span>
              </div>
              {ac && (
                <ul className="mt-2 space-y-1">
                  {s.yedekler.map(y => (
                    <li key={y.id} className={`flex flex-wrap items-center gap-x-3 gap-y-1 text-xs ${y.yerel ? '' : 'opacity-50'}`}>
                      <span className="font-mono text-slate-600 dark:text-slate-300 break-all min-w-0">{y.dosya}</span>
                      <span className="font-mono text-slate-400">{y.olusturma}</span>
                      <span className="font-mono text-slate-400">{fmtB(y.boyut_b)}</span>
                      <Button type="button" variant="outlined" boyut="sm" className="ml-auto" disabled={!y.yerel} onClick={() => indir(y)}
                        title={y.yerel ? y.dosya : cevir('Yerel kopya yok')} aria-label={`${cevir('İndir')}: ${y.dosya}`}>{cevir('İndir')}</Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
