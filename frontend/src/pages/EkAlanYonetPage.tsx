// Ek (addon) alan adı yönetimi — /abonelikler/:id/ek-alanlar/:eid.
// Durum, web dizini, PHP sürümü, SSL, DNS bölgesi (salt okunur) ve silme. E-posta bu sürümde yok.
// Veri: /domains/{id}/ek-alan-adlari/{eid}[/php|/ssl|/dns] (MusteriScope: yalnız kendi hesabı).
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { Badge, Card, Input } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { useUc } from '@/components/alanpano/useUc'
import type { Uc } from '@/components/alanpano/useUc'
import type { Domain } from '@/components/domainTipi'
import { BaglantiDugme, Dugme, KartBasligi, KopyaDugmesi, YenidenDene } from '@/components/musteri-ana/ortak'
import { cevir, cevirF, tarihBicim } from '@/components/musteri-ana/dil'
import type { EkAlan } from '@/components/musteri-ana/tipler'

type Detay = EkAlan & { ip?: string }
type SslYaniti = { aktif: boolean; bitis: string }
type DnsKaydi = { ad: string; tip: string; deger: string; ttl: number; oncelik?: number }

function Satir({ etiket, children }: { etiket: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-10 items-center justify-between gap-3 py-1.5">
      <dt className="shrink-0 text-xs text-gray-500 dark:text-dark-300">{etiket}</dt>
      <dd className="flex min-w-0 items-center justify-end gap-0.5 text-end text-sm text-gray-800 dark:text-dark-50">{children}</dd>
    </div>
  )
}

// PHP sürümü ek alan adına özel DEĞİL: ek alan adı hesabın PHP havuzunu kullanır (salt okunur).
function PhpKarti({ kok, d }: { kok: string; d: Detay }) {
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi baslik={cevir('PHP sürümü')} />
      <div className="px-4 py-4">
        <p className="text-sm font-medium text-gray-800 dark:text-dark-50">PHP {d.php_surum}</p>
        <p className="mt-2 text-xs text-gray-600 dark:text-dark-200">{cevir('Ek alan adı hesabın PHP ayarlarını kullanır; sürüm hesabın PHP ayarlarından değişir.')}</p>
        <div className="mt-3">
          <BaglantiDugme to={`${kok}/php`} variant="outlined" ikonSonda={I.chevronSag}>
            {cevir('Hesabın PHP ayarları')}
          </BaglantiDugme>
        </div>
      </div>
    </Card>
  )
}

function SslKarti({ id, eid, d, ssl }: { id: string; eid: string; d: Detay; ssl: Uc<SslYaniti> }) {
  const toast = useToast()
  const { onay } = useDialog()
  const [mesgul, setMesgul] = useState(false)
  const [hata, setHata] = useState('')
  const aktif = ssl.veri?.aktif ?? d.ssl_aktif
  const bitis = ssl.veri?.bitis ?? d.ssl_bitis

  async function kur(tip: 'letsencrypt' | 'self-signed') {
    if (tip === 'letsencrypt') {
      const tamam = await onay({
        baslik: cevir('Let’s Encrypt kurulsun mu?'),
        mesaj: cevirF('{0} için A kaydı bu sunucuyu göstermeli{1}. DNS henüz yayılmadıysa kurulum başarısız olur.', d.alan_adi, d.ip ? ` (${d.ip})` : ''),
        ton: 'uyari',
        onayEtiketi: cevir('Kur'),
        onayIkonu: I.kalkan,
      })
      if (!tamam) return
    }
    setHata('')
    setMesgul(true)
    try {
      await api.post(`/domains/${id}/ek-alan-adlari/${eid}/ssl`, { tip }, UZUN_ISTEK)
      toast.basari(cevir('Kaydedildi'), cevir('SSL kuruldu.'))
      await ssl.yenile()
    } catch (e) {
      const m = apiHata(e, cevir('SSL kurulamadı'))
      setHata(m)
      toast.hata(cevir('İşlem başarısız'), m)
    } finally {
      setMesgul(false)
    }
  }
  async function kaldir() {
    const tamam = await onay({ baslik: cevir('SSL kaldırılsın mı?'), mesaj: cevir('Sertifika kaldırılır; site yalnız http:// ile erişilebilir olur.'), ton: 'uyari', onayEtiketi: cevir('Kaldır'), onayIkonu: I.cop })
    if (!tamam) return
    setHata('')
    setMesgul(true)
    try {
      await api.delete(`/domains/${id}/ek-alan-adlari/${eid}/ssl`, UZUN_ISTEK)
      toast.basari(cevir('SSL kaldırıldı.'))
      await ssl.yenile()
    } catch (e) {
      const m = apiHata(e, cevir('SSL kaldırılamadı'))
      setHata(m)
      toast.hata(cevir('İşlem başarısız'), m)
    } finally {
      setMesgul(false)
    }
  }
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi
        baslik="SSL / TLS"
        sag={
          <Badge color={aktif ? 'success' : 'neutral'} variant="soft" className="px-1.5 py-0.5 text-tiny-plus">
            {aktif ? cevir('Aktif') : cevir('Yok')}
          </Badge>
        }
      />
      <div className="px-4 py-4">
        {aktif && bitis && <p className="mb-3 text-xs text-gray-600 dark:text-dark-200">{cevirF('Bitiş: {0}', tarihBicim(bitis))}</p>}
        <div className="flex flex-wrap items-center gap-2">
          <Dugme color="primary" boyut="sm" ikon={I.kalkan} onClick={() => void kur('letsencrypt')} disabled={mesgul}>
            {cevir('Let’s Encrypt kur')}
          </Dugme>
          <Dugme variant="outlined" boyut="sm" ikon={I.anahtar} onClick={() => void kur('self-signed')} disabled={mesgul}>
            {cevir('Öz-imzalı')}
          </Dugme>
          {aktif && (
            <Dugme color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => void kaldir()} disabled={mesgul} className="sm:ms-auto">
              {cevir('Kaldır')}
            </Dugme>
          )}
        </div>
        <p className="mt-3 text-xs text-gray-500 dark:text-dark-300">{cevirF('Let’s Encrypt için {0} A kaydının bu sunucuya{1} çözümlenmesi gerekir.', d.alan_adi, d.ip ? ` (${d.ip})` : '')}</p>
        {hata && (
          <UyariKutusu ton="tehlike" kucuk className="mt-3">
            <p className="break-words">{hata}</p>
          </UyariKutusu>
        )}
        {ssl.hata && !ssl.veri && <p className="mt-3 text-xs text-gray-500 dark:text-dark-300">{cevir('SSL durumu okunamadı; listedeki son bilgi gösteriliyor.')}</p>}
      </div>
    </Card>
  )
}

function DnsKarti({ dns }: { dns: Uc<{ kayitlar: DnsKaydi[] }> }) {
  const k = dns.veri?.kayitlar ?? []
  return (
    <Card className="bg-white dark:bg-dark-700">
      <KartBasligi baslik={cevir('DNS bölgesi')} />
      <div className="px-4 py-4">
        {!dns.veri ? (
          dns.yukleniyor ? (
            <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
          ) : (
            <div className="text-sm text-gray-600 dark:text-dark-200">
              <p>{cevir('DNS kayıtları alınamadı.')}</p>
              <YenidenDene uc={dns} className="mt-3" />
            </div>
          )
        ) : (
          <>
            <p className="mb-3 text-xs text-gray-600 dark:text-dark-200">{cevir('DNS bu sunucuda otomatik oluşturuldu. Kayıtlar yalnız görüntülenir.')}</p>
            {k.length === 0 ? (
              <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Kayıt yok.')}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[28rem] text-start text-xs">
                  <thead>
                    <tr className="border-b border-gray-100 text-gray-500 dark:border-dark-600 dark:text-dark-300">
                      <th scope="col" className="py-1.5 pe-3 text-start font-medium">{cevir('Ad')}</th>
                      <th scope="col" className="py-1.5 pe-3 text-start font-medium">{cevir('Tür')}</th>
                      <th scope="col" className="py-1.5 pe-3 text-start font-medium">{cevir('Değer')}</th>
                      <th scope="col" className="py-1.5 text-end font-medium">TTL</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100 font-mono text-gray-800 dark:divide-dark-600 dark:text-dark-50">
                    {k.map((r, i) => (
                      <tr key={`${r.ad}-${r.tip}-${i}`}>
                        <td className="py-1.5 pe-3 align-top break-all">{r.ad}</td>
                        <td className="py-1.5 pe-3 align-top">{r.tip}</td>
                        <td className="py-1.5 pe-3 align-top break-all">{r.oncelik ? `${r.oncelik} ${r.deger}` : r.deger}</td>
                        <td className="py-1.5 text-end align-top tabular-nums">{r.ttl}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </div>
    </Card>
  )
}

export default function EkAlanYonetPage() {
  useTranslation() // dil değişince yeniden çiz
  const { id, eid } = useParams()
  const navigate = useNavigate()
  const toast = useToast()
  const kok = `/abonelikler/${id}`
  const taban = `/domains/${id}/ek-alan-adlari/${eid}`
  const domain = useUc<Domain>(id ? `/domains/${id}` : null)
  const detay = useUc<Detay>(id && eid ? taban : null, cevir('Ek alan adı bilgisi alınamadı.'))
  const ssl = useUc<SslYaniti>(id && eid ? `${taban}/ssl` : null)
  const dns = useUc<{ kayitlar: DnsKaydi[] }>(id && eid ? `${taban}/dns` : null)
  const [silAcik, setSilAcik] = useState(false)
  const [silYazi, setSilYazi] = useState('')
  const [siliniyor, setSiliniyor] = useState(false)
  const d = detay.veri

  const kirinti = (
    <Breadcrumb
      items={[
        { etiket: cevir('Ana sayfa'), href: '/' },
        { etiket: cevir('Domainler'), href: '/domainler' },
        { etiket: domain.veri?.alan_adi || '…', href: kok },
        { etiket: d?.alan_adi || '…' },
      ]}
    />
  )

  if (!d) {
    return (
      <div className="max-w-3xl">
        {kirinti}
        {detay.hata ? (
          <UyariKutusu ton="tehlike" baslik={cevir('Ek alan adı bilgisi alınamadı.')}>
            <p>{detay.hata}</p>
            <YenidenDene uc={detay} className="mt-3" />
          </UyariKutusu>
        ) : (
          <p className="py-8 text-center text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
        )}
      </div>
    )
  }

  const aktifSsl = ssl.veri?.aktif ?? d.ssl_aktif
  async function sil() {
    if (!d) return
    setSiliniyor(true)
    try {
      await api.delete(`${taban}?onay=${encodeURIComponent(d.alan_adi)}`, UZUN_ISTEK)
      toast.basari(cevir('Alan adı silindi'), d.alan_adi)
      navigate(kok)
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e, cevir('Silinemedi')))
      setSiliniyor(false)
    }
  }

  return (
    <div className="max-w-3xl">
      {kirinti}
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 break-all text-xl font-semibold text-gray-900 dark:text-dark-50">{d.alan_adi}</h1>
            <Badge color="info" variant="soft" className="px-1.5 py-0.5 text-tiny-plus" title={cevir('Ek alan adı — ana hesabın ek sitesi')}>
              {cevir('Ek')}
            </Badge>
            <Badge color={d.durum === 'aktif' ? 'success' : 'warning'} variant="soft" className="px-1.5 py-0.5 text-tiny-plus">
              {d.durum === 'aktif' ? cevir('Aktif') : cevir('Askıda')}
            </Badge>
          </div>
          <p className="mt-1 text-sm text-gray-600 dark:text-dark-200">{cevirF('{0} hesabının ek alan adı.', domain.veri?.alan_adi || '…')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <a
            href={`${aktifSsl ? 'https' : 'http'}://${d.alan_adi}`}
            target="_blank"
            rel="noreferrer"
            className="inline-flex min-h-10 items-center gap-1.5 rounded-lg border border-gray-300 px-3 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-dark-450 dark:text-dark-100 dark:hover:bg-dark-600/60 @2xl/sayfa:min-h-0 @2xl/sayfa:py-1.5"
          >
            {cevir('Ziyaret et')}
            <Ikon d={I.harici} className="size-3.5 shrink-0" />
            <span className="sr-only">({cevir('yeni sekmede açılır')})</span>
          </a>
          <Dugme color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => { setSilYazi(''); setSilAcik(true) }}>
            {cevir('Sil')}
          </Dugme>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card className="bg-white dark:bg-dark-700">
          <KartBasligi baslik={cevir('Genel bilgi')} />
          <dl className="divide-y divide-gray-100 px-4 dark:divide-dark-600">
            <Satir etiket={cevir('Web dizini')}>
              <span className="min-w-0 break-all font-mono text-xs">{d.docroot}</span>
              <KopyaDugmesi deger={d.docroot} etiket={cevir('Web dizini')} />
            </Satir>
            <Satir etiket={cevir('PHP sürümü')}>PHP {d.php_surum}</Satir>
            {d.ip && (
              <Satir etiket="DNS (A)">
                <span className="min-w-0 truncate font-mono text-xs">{d.ip}</span>
                <KopyaDugmesi deger={d.ip} etiket={cevir('IP adresi')} />
              </Satir>
            )}
            {d.created_at && <Satir etiket={cevir('Oluşturulma')}>{tarihBicim(d.created_at)}</Satir>}
          </dl>
        </Card>
        <PhpKarti kok={kok} d={d} />
        <div className="md:col-span-2">
          <SslKarti id={String(id)} eid={String(eid)} d={d} ssl={ssl} />
        </div>
        <div className="md:col-span-2">
          <DnsKarti dns={dns} />
        </div>
      </div>
      <p className="mt-4">
        <Link to={kok} className="inline-flex min-h-10 items-center text-sm font-medium text-primary-600 hover:underline dark:text-primary-400">
          {cevir('Hesaba dön')}
        </Link>
      </p>

      <Modal
        acik={silAcik}
        baslik={cevir('Alan adı silinsin mi?')}
        ton="tehlike"
        genislik="sm"
        onKapat={() => setSilAcik(false)}
        kapatilamaz={siliniyor}
        alt={
          <>
            <Dugme variant="outlined" ikon={I.kapat} onClick={() => setSilAcik(false)} disabled={siliniyor}>
              {cevir('Vazgeç')}
            </Dugme>
            <Dugme color="error" ikon={I.cop} onClick={() => void sil()} disabled={silYazi.trim() !== d.alan_adi} yukleniyor={siliniyor}>
              {cevir('Sil')}
            </Dugme>
          </>
        }
      >
        <p className="text-sm leading-relaxed text-gray-600 dark:text-dark-200">{cevirF('{0} silinir: web dizini (docroot) ve sertifikası kalıcı olarak silinir. Bu işlem geri alınamaz.', d.alan_adi)}</p>
        <div className="mt-3">
          <Input label={cevirF('Onaylamak için alan adını yazın: {0}', d.alan_adi)} value={silYazi} onChange={(e) => setSilYazi(e.target.value)} autoComplete="off" spellCheck={false} classNames={{ input: 'font-mono' }} />
        </div>
      </Modal>
    </div>
  )
}
