// Uygulama Kurulumu — alan adının uygulama kataloğu ayrı sayfa olarak
// (/abonelikler/{id}/uygulamalar). Kök/bayi aynı içeriği abonelik sayfasındaki
// "Uygulamalar" sekmesinde görür; müşterinin ana sayfasında (MusteriAnaSayfa) sekme
// olmadığı için araç kutucuğu buraya bağlanır. İçerik: components/UygulamaKatalogu.
import { useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import Breadcrumb from '@/components/Breadcrumb'
import UygulamaKatalogu from '@/components/UygulamaKatalogu'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useUc } from '@/components/alanpano/useUc'
import type { Domain } from '@/components/domainTipi'

const UYGSAYFA_EN: Record<string, string> = {
  'Uygulama Kurulumu': 'App Installer',
  'Hazır uygulamaları tek tıkla kurun ve kurulu uygulamaları yönetin.': 'Install ready-made applications in one click and manage installed ones.',
  'Alan adı bilgisi alınamadı': 'Could not load domain information',
  'Anasayfa': 'Home',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (UYGSAYFA_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainUygulamalarPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const domain = useUc<Domain>(id ? `/domains/${id}` : null, cevir('Alan adı bilgisi alınamadı'))
  const d = domain.veri
  return (
    <div>
      <Breadcrumb
        items={[
          { etiket: cevir('Anasayfa'), href: '/' },
          { etiket: cevir('Domainler'), href: '/domainler' },
          ...(d ? [{ etiket: d.alan_adi, href: `/abonelikler/${id}` }] : []),
          { etiket: cevir('Uygulama Kurulumu') },
        ]}
      />
      <h1 className="mb-1 text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir('Uygulama Kurulumu')}</h1>
      <p className="mb-5 text-sm text-slate-500 dark:text-slate-400">{cevir('Hazır uygulamaları tek tıkla kurun ve kurulu uygulamaları yönetin.')}</p>
      {d ? (
        <UygulamaKatalogu domain={d} />
      ) : domain.hata ? (
        <UyariKutusu ton="tehlike">{domain.hata}</UyariKutusu>
      ) : (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir('Yükleniyor…')}</div>
      )}
    </div>
  )
}
