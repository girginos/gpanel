// Müşteri ana sayfası — "Güvenlik" özet kartı (Website Security). GERÇEK veri:
// GET /domains/{id}/guvenlik/ozet (müşterinin kendi hesabı; hafif uç, açılışta BİR kez).
// Sahte/öntanımlı sayı yok: uç gelmeden "…", alınamazsa yeniden dene.
import { Badge, Card } from '@/components/ui'
import { I } from '@/components/Ikon'
import { useUc } from '@/components/alanpano/useUc'
import { BaglantiDugme, KartBasligi, YenidenDene } from './ortak'
import { cevir, ozetGorunumu, sablon, type GuvOzet } from '@/lib/musteriGuvenlik'

export default function GuvenlikKarti({ domainId }: { domainId: number }) {
  const uc = useUc<GuvOzet>(`/domains/${domainId}/guvenlik/ozet`)
  const o = uc.veri
  const g = o ? ozetGorunumu(o) : null
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi baslik={cevir('Güvenlik')} />
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
        <div className="min-w-0 text-sm">
          {g && o ? (
            <div className="flex flex-wrap items-center gap-2">
              <Badge color={g.ton} variant="soft" className="px-2 py-0.5 text-tiny-plus font-semibold">{g.metin}</Badge>
              {o.critical + o.high > 0 && (
                <span className="text-xs text-gray-500 dark:text-dark-300">
                  {sablon('Kritik {0}', o.critical)} · {sablon('Yüksek {0}', o.high)}
                </span>
              )}
            </div>
          ) : uc.hata ? (
            <div className="flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-dark-300">
              <span>{cevir('Güvenlik bilgileri alınamadı')}</span>
              <YenidenDene uc={uc} />
            </div>
          ) : (
            <span className="text-gray-400" aria-busy="true">…</span>
          )}
        </div>
        <BaglantiDugme to={`/abonelikler/${domainId}/guvenlik`} ikon={I.kalkanTara}>{cevir('Ayrıntılar')}</BaglantiDugme>
      </div>
    </Card>
  )
}
