// Bildirim tercihleri (D3b): olay türüne göre panel içi bildirimi aç/kapat.
// Tercih görüntüleyene aittir (müşterinin kapatması bayiyi etkilemez); kapalı tür
// listede ve zil sayacında gizlenir. E-posta kanalı yok: sunucu
// eposta_kullanilabilir=false döner, burada açıkça yazılır.
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { api, apiHata } from '@/lib/api'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { kategoriAd } from '@/lib/bildirim'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I } from '@/components/Ikon'

type Tercih = { kategori: string; panel: boolean }

const EN: Record<string, string> = {
  'Bildirim tercihleri': 'Notification preferences',
  'Hangi olaylar için panel içi bildirim almak istediğinizi seçin. Kapattığınız türler listede ve zil sayacında görünmez; kayıtlar silinmez.':
    'Choose which events create in-panel notifications for you. Types you turn off are hidden from the list and the bell counter; records are not deleted.',
  'E-posta bildirimi henüz yok: panelde giden posta altyapısı bulunmuyor, bildirimler yalnız panel içinde gösterilir.':
    'Email notifications are not available yet: the panel has no outgoing mail service, notifications are shown in the panel only.',
  'Kaydet': 'Save',
  'Tercihler kaydedildi': 'Preferences saved',
  'Tercihler yüklenemedi': 'Preferences could not be loaded',
  'Kaydedilemedi': 'Could not save',
  'Açık': 'On',
  'Kapalı': 'Off',
  'Yeniden dene': 'Retry',
  'Yükleniyor…': 'Loading…',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function BildirimTercihleri() {
  useTranslation()
  const toast = useToast()
  const [liste, setListe] = useState<Tercih[] | null>(null)
  const [eposta, setEposta] = useState(false)
  const [hata, setHata] = useState('')
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [degisti, setDegisti] = useState(false)

  function yukle() {
    setHata('')
    api.get<{ tercihler: Tercih[]; eposta_kullanilabilir: boolean }>('/bildirim-tercihleri')
      .then((r) => { setListe(r.data.tercihler || []); setEposta(!!r.data.eposta_kullanilabilir); setDegisti(false) })
      .catch((e) => setHata(apiHata(e)))
  }
  useEffect(() => { yukle() }, [])
  // Zildeki "Tercihler" bağlantısı (/bildirimler#tercihler): yüklenince bölüme kaydır.
  useEffect(() => {
    if (liste && window.location.hash === '#tercihler') document.getElementById('tercihler')?.scrollIntoView({ block: 'start' })
  }, [liste])

  async function kaydet() {
    if (!liste) return
    setKaydediliyor(true)
    try {
      await api.put('/bildirim-tercihleri', { tercihler: liste })
      setDegisti(false)
      toast.basari(cevir('Tercihler kaydedildi'))
    } catch (e) { toast.hata(cevir('Kaydedilemedi'), apiHata(e)) } finally { setKaydediliyor(false) }
  }

  return (
    <section id="tercihler" aria-labelledby="bt-baslik" className="rounded-lg border border-gray-200 bg-white p-4 sm:p-5 dark:border-dark-600 dark:bg-dark-700">
      <h2 id="bt-baslik" className="text-base font-semibold text-gray-900 dark:text-dark-50">{cevir('Bildirim tercihleri')}</h2>
      <p className="mt-1 text-sm text-gray-500 dark:text-dark-300">{cevir('Hangi olaylar için panel içi bildirim almak istediğinizi seçin. Kapattığınız türler listede ve zil sayacında görünmez; kayıtlar silinmez.')}</p>
      {!eposta && liste && (
        <UyariKutusu ton="bilgi" kucuk className="mt-3">{cevir('E-posta bildirimi henüz yok: panelde giden posta altyapısı bulunmuyor, bildirimler yalnız panel içinde gösterilir.')}</UyariKutusu>
      )}
      {hata ? (
        <UyariKutusu ton="tehlike" baslik={cevir('Tercihler yüklenemedi')} className="mt-3">
          {hata} <Button boyut="sm" variant="outlined" ikon={I.yenile} onClick={yukle} className="ms-2">{cevir('Yeniden dene')}</Button>
        </UyariKutusu>
      ) : !liste ? (
        <p className="mt-4 text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
      ) : (
        <>
          <ul className="mt-4 grid gap-2 sm:grid-cols-2">
            {liste.map((t) => (
              <li key={t.kategori}>
                <label className="flex cursor-pointer items-center justify-between gap-3 rounded-md border border-gray-200 px-3 py-2.5 hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-600/40">
                  <span className="text-sm font-medium text-gray-800 dark:text-dark-100">{kategoriAd(t.kategori)}</span>
                  <span className="flex items-center gap-2">
                    <span className="text-xs text-gray-500 dark:text-dark-300">{t.panel ? cevir('Açık') : cevir('Kapalı')}</span>
                    <input type="checkbox" role="switch" checked={t.panel}
                      onChange={(e) => { const v = e.target.checked; setListe((l) => (l || []).map((x) => (x.kategori === t.kategori ? { ...x, panel: v } : x))); setDegisti(true) }}
                      className="size-4 accent-primary-600" />
                  </span>
                </label>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex justify-end">
            <Button color="primary" ikon={I.disket} onClick={kaydet} yukleniyor={kaydediliyor} disabled={!degisti}>{cevir('Kaydet')}</Button>
          </div>
        </>
      )}
    </section>
  )
}
