import { useCallback, useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api, apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'
import { Badge, Button } from '@/components/ui'
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
  'otomatik silinmez': 'never automatic',
  'Temizlendi': 'Cleaned up',
  'Uzak kopya': 'Remote copy',
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

type Satir = {
  id: number; alan_adi: string; sistem_kullanici: string; silinme: string; son_kullanma: string
  dosya_sayisi: number; boyut_b: number; dosyalar: string[]; uzak_hedef: boolean; yerel_var: boolean
  durum: string; son_hata?: string
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
  const [satirlar, setSatirlar] = useState<Satir[]>([])
  const [gun, setGun] = useState('')
  const [kaydediyor, setKaydediyor] = useState(false)

  const yukle = useCallback(() => {
    api.get<Satir[]>('/admin/silinen-yedekler').then(r => setSatirlar(r.data)).catch(() => setSatirlar([]))
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

  async function sil(s: Satir, uzagiYoksay = false) {
    if (!uzagiYoksay && !(await onay({ baslik: cevir('Emin misiniz?'), mesaj: cevirT(cevir('"{0}" için saklanan yedekler KALICI silinsin mi? Uzak depodaki kopyalar da silinir. Geri alınamaz.'), s.alan_adi), ton: 'tehlike', onayIkonu: I.cop }))) return
    try {
      await api.delete(`/admin/silinen-yedekler/${s.id}${uzagiYoksay ? '?uzagi_yoksay=1' : ''}`)
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

  function indir(s: Satir, dosya: string) {
    const tok = useAuth.getState().token || ''
    fetch(`/api/v1/admin/silinen-yedekler/${s.id}/indir?dosya=${encodeURIComponent(dosya)}`, { headers: { Authorization: `Bearer ${tok}` } })
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.blob() })
      .then(blob => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob); a.download = dosya; a.click()
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
        {satirlar.map(s => (
          <div key={s.id} className="px-4 py-2.5 text-sm flex flex-wrap items-center gap-x-4 gap-y-1">
            <span className="font-medium text-slate-800 dark:text-slate-200">{s.alan_adi}</span>
            <span className="font-mono text-xs text-slate-400">{cevir('Silinme')}: {s.silinme}</span>
            <span className="font-mono text-xs text-slate-400">{cevir('Son saklama')}: {s.son_kullanma || cevir('otomatik silinmez')}</span>
            <span className="font-mono text-xs text-slate-400">{s.dosya_sayisi} {cevir('Yedek').toLowerCase()} · {fmtB(s.boyut_b)}</span>
            {s.uzak_hedef && <Badge variant="soft" color="info" className="px-1.5 py-0.5 text-[10px]">{cevir('Uzak kopya')}</Badge>}
            {s.durum === 'temizlendi' && <Badge variant="soft" color="neutral" className="px-1.5 py-0.5 text-[10px]">{cevir('Temizlendi')}</Badge>}
            {s.son_hata && <span className="text-xs text-red-600 dark:text-red-400 basis-full">{s.son_hata}</span>}
            {s.durum === 'saklaniyor' && (
              <span className="ml-auto flex flex-wrap items-center gap-2">
                {s.yerel_var && s.dosyalar.slice(-3).reverse().map(d => (
                  <Button key={d} type="button" variant="outlined" boyut="sm" onClick={() => indir(s, d)} title={d}>{cevir('İndir')}</Button>
                ))}
                <Button type="button" color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(s)}>{cevir('Kalıcı sil')}</Button>
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
