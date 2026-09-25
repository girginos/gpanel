// Alan adı panosu — abonelik 'Pano' sekmesinde araç kartlarının (DomainPano) üstünde,
// root panosu kalitesinde ama TEMAYA UYUMLU (açık + koyu; Tailux Card/Button/Badge,
// tonlar.ts renkleri, Ikon seti) bir alan adı özeti. Müşteri, bayi ve yönetici aynı
// sayfayı açar: tüm uçlar MusteriScope / rol kapsamlıdır; bildirimler istemcide
// domain_id ile süzülür (uçta domain süzgeci yok).
//
// 🔴 ÇEKİM KURALLARI (maliyet — musteri-bulgular MP-09):
//  - /kaynak burada ÇEKİLMEZ: SubscriptionDetailPage tek kez çeker, prop ile gelir
//    (aynı veri sağdaki DomainKaynakKart ve soldaki 'İstatistikler' kartında da).
//  - /istatistik, /health, /performans, /ssl, /backups, /bildirimler, /trafik-gecmis
//    açılışta BİR kez; sonra yalnız başlıktaki 'Yenile' ile.
//  - Yalnız /canli (ucuz dosya okuma) 10 sn'de bir, sekme görünürken ve Pano sekmesi
//    etkinken yoklanır (bkz. alanpano/useCanli.ts).
//  - SAHTE/SEED VERİ YOK: veri yoksa iskelet ya da açıklamalı boş durum.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import type { Domain } from '@/components/domainTipi'
import type { BildirimYanit, Istatistik, KaynakOzet, Performans, Saglik, SSLDurum, TrafikGecmis, Yedek } from './alanpano/tipler'
import { useUc, type Uc } from './alanpano/useUc'
import { useCanli } from './alanpano/useCanli'
import { DiskKart, SaglikKart, SSLKart, TrafikKart, VeritabaniKart, YedekKart } from './alanpano/ozetKartlari'
import { AylikTrafikKart, CanliKart, DurumKoduKart, GunlukIstekKart } from './alanpano/grafikler'
import { BildirimKart, PerformansKart } from './alanpano/yanKartlar'
import { cevir, saat, sablon } from './alanpano/bicim'

export default function AlanPano({ domain, kaynak, etkin = true }: {
  domain: Domain
  // /kaynak durumu + yenileyicisi — üst sayfadan (tek çekim).
  kaynak: Uc<KaynakOzet>
  // Pano sekmesi açık mı: değilse canlı yoklama durur (bileşen veriyi korumak için
  // bağlı kalır — sekme değişince pahalı uçlar yeniden çekilmez).
  etkin?: boolean
}) {
  useTranslation() // dil re-render aboneliği
  const id = domain.id
  const ist = useUc<Istatistik>(`/domains/${id}/istatistik`)
  const perf = useUc<Performans>(`/domains/${id}/performans`)
  const saglik = useUc<Saglik>(`/domains/${id}/health`)
  const ssl = useUc<SSLDurum>(`/domains/${id}/ssl`)
  const yedek = useUc<Yedek[]>(`/domains/${id}/backups`)
  const bildirim = useUc<BildirimYanit>('/bildirimler')
  const trafik = useUc<TrafikGecmis>(`/domains/${id}/trafik-gecmis?ay=12`)
  const canli = useCanli(id, etkin)

  const [yenileniyor, setYenileniyor] = useState(false)
  const [sonGuncelleme, setSonGuncelleme] = useState(() => new Date())

  async function yenile() {
    setYenileniyor(true)
    try {
      await Promise.all([
        kaynak.yenile(), ist.yenile(), perf.yenile(), saglik.yenile(),
        ssl.yenile(), yedek.yenile(), bildirim.yenile(), trafik.yenile(),
      ])
      // Canlı yoklama hata ya da "ölçüm yok" yüzünden durduysa bir kez daha denenir.
      if (canli.durum === 'hata' || canli.durum === 'destekyok') canli.yeniden()
    } finally {
      setYenileniyor(false)
      setSonGuncelleme(new Date())
    }
  }

  return (
    <section aria-labelledby="alanpano-baslik" className="@container space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <h2 id="alanpano-baslik" className="text-base font-semibold text-gray-800 dark:text-dark-50">{cevir('Alan adı özeti')}</h2>
          <p className="text-xs text-gray-500 dark:text-dark-300">{sablon('Son güncelleme {0}', saat(sonGuncelleme, true))}</p>
        </div>
        <Button variant="outlined" ikon={I.yenile} yukleniyor={yenileniyor} onClick={() => void yenile()}>{cevir('Yenile')}</Button>
      </div>

      <div className="grid grid-cols-1 gap-4 @lg:grid-cols-2 @4xl:grid-cols-3 @7xl:grid-cols-6">
        <DiskKart kaynak={kaynak} />
        <TrafikKart kaynak={kaynak} />
        <VeritabaniKart kaynak={kaynak} />
        <SaglikKart saglik={saglik} />
        <SSLKart ssl={ssl} domainId={id} />
        <YedekKart yedekler={yedek} domainId={id} />
      </div>

      <div className="grid grid-cols-1 gap-4 @4xl:grid-cols-3">
        <CanliKart c={canli} className="@4xl:col-span-2" />
        <PerformansKart perf={perf} domainId={id} />
        <GunlukIstekKart ist={ist} domainId={id} className="@4xl:col-span-2" />
        <DurumKoduKart ist={ist} />
        <AylikTrafikKart trafik={trafik} kotaMB={kaynak.veri?.trafik_mb.limit ?? 0} className="@4xl:col-span-2" />
        <BildirimKart bildirim={bildirim} domainId={id} />
      </div>
    </section>
  )
}
