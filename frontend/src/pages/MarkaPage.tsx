// Marka (whitelabel) — çekirdek ekran (D2; eski whitelabel eklenti ekranının yerine).
//
// Sekmeler: Marka (kök: panelin kendi markası, bayi: kendi markası) · Özel adres
// (bayi) · Bayi markaları (kök). Sahibi her zaman sunucu JETONDAN belirler; bu
// sayfa hiçbir kimlik göndermez. Müşteri bu sayfaya giremez (rol kapısı + 403).
import { useCallback, useEffect, useState } from 'react'
import axios from 'axios'
import Breadcrumb from '@/components/Breadcrumb'
import { api, apiHata } from '@/lib/api'
import { useRol, yoneticiMi } from '@/lib/rol'
import { UyariKutusu } from '@/components/UyariKutusu'
import { kaydet, oku } from '@/lib/kalici'
import { MarkaFormu, type YonetGorunum } from '@/components/marka/MarkaFormu'
import { BayiMarkalari } from '@/components/marka/BayiMarkalari'
import { OzelAdres } from '@/components/marka/OzelAdres'
import { useMarkaCeviri } from '@/components/marka/ceviri'

type Sekme = 'marka' | 'adres' | 'bayiler'

// Açık sekme bu tarayıcıda hatırlanır (sayfa yenilenince / başka sayfadan dönünce kaldığın yerden).
// Kişisel kolaylık; kritik değil. Kayıtlı sekme bu rolde yoksa (ör. bayiye 'bayiler') 'marka'ya düşer.
const SEKME_ANAHTAR = 'marka.sekme'
const SEKME_SURE = 30 * 24 * 3600 * 1000
const SEKMELER: readonly Sekme[] = ['marka', 'adres', 'bayiler']

export default function MarkaPage() {
  const cevir = useMarkaCeviri()
  const rol = useRol()
  const kok = yoneticiMi(rol)
  const bayi = rol === 'reseller'
  const [kayitliSekme, setKayitliSekme] = useState<Sekme>(() => {
    const k = oku<string>(SEKME_ANAHTAR)
    return SEKMELER.find((x) => x === k) ?? 'marka'
  })
  const [veri, setVeri] = useState<YonetGorunum | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [kapali, setKapali] = useState(false)

  const yukle = useCallback(() => {
    api.get<YonetGorunum>('/marka/yonet')
      .then((r) => { setVeri(r.data); setHata(null); setKapali(false) })
      .catch((e) => {
        if (axios.isAxiosError(e) && e.response?.status === 403) setKapali(true)
        else setHata(apiHata(e))
      })
  }, [])
  useEffect(() => { if (kok || bayi) yukle() }, [kok, bayi, yukle])

  if (!kok && !bayi) {
    return <UyariKutusu ton="uyari">{cevir('Bu ekran yalnız yönetici ve bayiler içindir.')}</UyariKutusu>
  }

  const sekmeler: Array<{ k: Sekme; ad: string }> = [{ k: 'marka', ad: bayi ? 'Markam' : 'Marka' }]
  // Sekme HER ZAMAN görünür: izin yoksa içinde bilgi mesajı çıkar (backend 403 davranışı aynen).
  if (bayi) sekmeler.push({ k: 'adres', ad: 'Özel adres' })
  if (kok) sekmeler.push({ k: 'bayiler', ad: 'Bayi markaları' })
  const sekme: Sekme = sekmeler.some((x) => x.k === kayitliSekme) ? kayitliSekme : 'marka'
  const sekmeSec = (k: Sekme) => {
    setKayitliSekme(k)
    kaydet(SEKME_ANAHTAR, k, SEKME_SURE)
  }

  return (
    <div>
      <Breadcrumb items={kok
        ? [{ href: '/araclar-ayarlar', etiket: cevir('Araçlar ve Ayarlar') }, { etiket: cevir('Marka') }]
        : [{ etiket: cevir('Markam') }]} />
      <div className="mb-4 mt-4">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir(bayi ? 'Markam' : 'Marka')}</h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          {cevir(bayi ? 'Müşterileriniz panelde ve giriş sayfasında bu markayı görür.' : 'Panelin görünen kimliği: ad, logo, renk, giriş sayfası metinleri ve destek iletişimi.')}
        </p>
      </div>

      {sekmeler.length > 1 && (
        <div className="mb-5 flex gap-1 overflow-x-auto border-b border-slate-200 dark:border-dark-600" role="tablist" aria-label={cevir('Marka')}>
          {sekmeler.map((s) => {
            const aktif = sekme === s.k
            return (
              <button key={s.k} type="button" role="tab" aria-selected={aktif} onClick={() => sekmeSec(s.k)}
                className={`-mb-px shrink-0 rounded-t border-b-2 px-4 py-2.5 text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40 ${aktif
                  ? 'border-brand-500 text-brand-700 dark:text-brand-300'
                  : 'border-transparent text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200'}`}>
                {cevir(s.ad)}
              </button>
            )
          })}
        </div>
      )}

      {sekme === 'bayiler' && kok ? <BayiMarkalari />
        : sekme === 'adres' && bayi ? <OzelAdres onDegisti={yukle} />
          : kapali ? <UyariKutusu ton="bilgi">{cevir('Marka ayarları bu hesap için kapalı.')}</UyariKutusu>
            : hata ? <UyariKutusu ton="tehlike">{hata}</UyariKutusu>
              : !veri ? <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
                : (
                  <div className="space-y-4">
                    <MarkaFormu veri={veri} onDegisti={setVeri} />
                  </div>
                )}
    </div>
  )
}
