// Uygulama Çalıştırıcı (calistirici eklentisi) — alan adına gömülü eklenti ekranı ayrı
// sayfa olarak (/abonelikler/{id}/gelistirici). Kök/bayi aynı ekranı abonelik sayfasının
// "Geliştirici" sekmesinde görür; müşteri ana sayfasında sekme olmadığı için araç
// kutucuğu (eklenti müşteriye açıksa) buraya bağlanır. Lisans askıdaysa EklentiEkrani
// yönetim yerine açıklama gösterir; asıl engel sunucudadır (eklenti proxy'si 402/403).
import { useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import EklentiEkrani from '@/components/EklentiEkrani'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useUc } from '@/components/alanpano/useUc'
import type { Domain } from '@/components/domainTipi'

const GELISTIRICI_EN: Record<string, string> = {
  'Uygulama Çalıştırıcı': 'Application Runner',
  'Alan adı bilgisi alınamadı': 'Could not load domain information',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (GELISTIRICI_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainGelistiriciPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const domain = useUc<Domain>(id ? `/domains/${id}` : null, cevir('Alan adı bilgisi alınamadı'))
  const d = domain.veri
  if (!d) {
    return (
      <div className="px-4 py-4 sm:px-6 sm:py-5">
        {domain.hata ? (
          <UyariKutusu ton="tehlike">{domain.hata}</UyariKutusu>
        ) : (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir('Yükleniyor…')}</div>
        )}
      </div>
    )
  }
  // Alan adı yüklendikten sonra monte edilir: eklenti kendini baştan bu alan adına daraltır.
  return <EklentiEkrani ad="calistirici" baslik={cevir('Uygulama Çalıştırıcı')} aciklama={d.alan_adi} ekBaglam={{ domainID: d.id, alanAdi: d.alan_adi }} />
}
