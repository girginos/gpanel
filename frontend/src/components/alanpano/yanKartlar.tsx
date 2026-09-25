// Yan kartlar: performans skoru halkası ve bu alan adına ait son bildirimler.
import { Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { TonRozeti } from '@/components/UyariKutusu'
import type { BildirimYanit, Performans } from './tipler'
import type { Uc } from './useUc'
import { AyrintiLink, BosDurum, Durumlu, Halka, Iskelet, PanoKart, type Vurgu } from './parcalar'
import { cevir, goreli, sablon, seviyeTonu, sunucuZamani, tarihSaat } from './bicim'

function skorTonu(s: number): Vurgu { return s >= 80 ? 'basari' : s >= 60 ? 'uyari' : 'tehlike' }
function skorAdi(s: number): string {
  return s >= 90 ? cevir('Mükemmel') : s >= 80 ? cevir('İyi') : s >= 60 ? cevir('Geliştirilebilir') : cevir('Zayıf')
}
const onemTonu = (o: string) => (o === 'yuksek' ? 'tehlike' : o === 'orta' ? 'uyari' : 'bilgi') as 'tehlike' | 'uyari' | 'bilgi'
const TON_RENK = { tehlike: 'this:error', uyari: 'this:warning', bilgi: 'this:info' } as const

export function PerformansKart({ perf, domainId, className = '' }: { perf: Uc<Performans>; domainId: number; className?: string }) {
  const skor = perf.veri?.skor
  return (
    <PanoKart className={className} ikon={I.simsek} baslik={cevir('Performans skoru')}
      vurgu={skor == null ? 'vurgu' : skorTonu(skor)}
      sag={<AyrintiLink to={`/abonelikler/${domainId}/performans`}>{cevir('Ayrıntı')}</AyrintiLink>}>
      <Durumlu uc={perf} iskelet={<Iskelet yukseklik="h-32" satir={2} />}>
        {(p) => {
          const ogeler = p.ogeler ?? []
          const acik = ogeler.filter((o) => o.aktif).length
          const oneriler = (p.oneriler ?? []).slice(0, 2)
          return (
            <div className="flex flex-1 flex-col items-center gap-4 @sm:flex-row @sm:items-start @4xl:flex-col @4xl:items-center">
              <Halka deger={p.skor} vurgu={skorTonu(p.skor)} etiket={sablon('Performans skoru {0} / 100', p.skor)}>
                <span className="text-3xl font-semibold tabular-nums text-gray-800 dark:text-dark-50">{p.skor}</span>
                <span className="text-xs text-gray-500 dark:text-dark-300">{skorAdi(p.skor)}</span>
              </Halka>
              <div className="w-full min-w-0 space-y-2">
                {ogeler.length > 0 && (
                  <div className="text-center text-xs text-gray-500 dark:text-dark-300 @sm:text-start @4xl:text-center">
                    {sablon('{0}/{1} hızlandırıcı açık', acik, ogeler.length)}
                  </div>
                )}
                {oneriler.length === 0 ? (
                  <div className="text-center text-xs text-gray-500 dark:text-dark-300">{cevir('Öneri yok — ayarlar yerinde.')}</div>
                ) : (
                  <ul className="space-y-1.5">
                    {oneriler.map((o, i) => (
                      <li key={i} className="flex items-start gap-2 text-xs text-gray-600 dark:text-dark-200">
                        <Ikon d={o.onem === 'yuksek' ? I.uyari : I.bilgi}
                          className={`${TON_RENK[onemTonu(o.onem)]} mt-0.5 size-3.5 shrink-0 text-this dark:text-this-lighter`} />
                        <span className="min-w-0">{o.metin}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
          )
        }}
      </Durumlu>
    </PanoKart>
  )
}

export function BildirimKart({ bildirim, domainId, className = '' }: { bildirim: Uc<BildirimYanit>; domainId: number; className?: string }) {
  return (
    <PanoKart className={className} ikon={I.zil} baslik={cevir('Bildirimler')} alt={cevir('Bu alan adına ait son kayıtlar')}
      sag={<AyrintiLink to="/bildirimler">{cevir('Tümü')}</AyrintiLink>}>
      <Durumlu uc={bildirim} iskelet={<Iskelet yukseklik="" satir={6} />}>
        {(y) => {
          // Uçta domain süzgeci yok (rol kapsamlı, en yeni 200) → istemcide domain_id ile süzülür.
          const liste = (y.bildirimler ?? []).filter((b) => b.domain_id === domainId).slice(0, 5)
          if (!liste.length) return <BosDurum ikon={I.zil} baslik={cevir('Bu alan adı için bildirim yok')} />
          return (
            <ul className="-my-1 divide-y divide-gray-100 dark:divide-dark-600">
              {liste.map((b) => {
                const d = sunucuZamani(b.tarih)
                return (
                  <li key={b.id} className="flex items-start gap-3 py-2.5">
                    <TonRozeti ton={seviyeTonu(b.seviye)} kucuk />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <span className={`min-w-0 text-sm ${b.okundu ? 'text-gray-700 dark:text-dark-100' : 'font-semibold text-gray-800 dark:text-dark-50'}`}>{b.baslik}</span>
                        {!b.okundu && <Badge color="primary" variant="soft" className="shrink-0">{cevir('Okunmadı')}</Badge>}
                      </div>
                      {b.mesaj && <div className="line-clamp-2 text-xs text-gray-500 dark:text-dark-300">{b.mesaj}</div>}
                      <div className="mt-0.5 text-[11px] text-gray-400 dark:text-dark-300" title={d ? tarihSaat(d) : b.tarih}>{goreli(d, b.tarih)}</div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )
        }}
      </Durumlu>
    </PanoKart>
  )
}
