// Ek (addon) alan adı ekleme — /abonelikler/:id/ek-alanlar/yeni (müşteri/bayi/kök aynı sayfa).
// Ek alan adı: aynı hesapta (aynı sistem kullanıcısı) kendi docroot'u, PHP sürümü ve SSL'i olan site.
// E-posta bu sürümde ek alan adında yoktur; arayüz posta vaadi yapmaz.
// Veri: GET/POST /domains/{id}/ek-alan-adlari (limit sunucuda da denetlenir; ekran yalnız yol gösterir).
import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { Card, Input } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useUc } from '@/components/alanpano/useUc'
import type { Domain } from '@/components/domainTipi'
import { BaglantiDugme, Dugme, KopyaDugmesi, YenidenDene } from '@/components/musteri-ana/ortak'
import { cevir, cevirF } from '@/components/musteri-ana/dil'
import { alanAdiHatasi, alanAdiNormal } from '@/components/musteri-ana/ekleme'
import type { EkAlanListesi } from '@/components/musteri-ana/tipler'

type Sonuc = { ok: boolean; id: number; alan_adi: string; docroot: string; ssl?: { tip: string; ok: boolean; hata?: string } }

export default function EkAlanEklePage() {
  useTranslation() // dil değişince yeniden çiz
  const { id } = useParams()
  const toast = useToast()
  const domain = useUc<Domain>(id ? `/domains/${id}` : null)
  const liste = useUc<EkAlanListesi>(id ? `/domains/${id}/ek-alan-adlari` : null, cevir('Ek alan adları alınamadı.'))
  const [ad, setAd] = useState('')
  const [ssl, setSsl] = useState(false)
  const [mesgul, setMesgul] = useState(false)
  const [hata, setHata] = useState('')
  const [sonuc, setSonuc] = useState<Sonuc | null>(null)

  const kok = `/abonelikler/${id}`
  const l = liste.veri
  const bicimHatasi = alanAdiHatasi(ad)
  const gecerli = ad.trim() !== '' && bicimHatasi === ''
  const ip = l?.ip ?? ''

  async function gonder(e: FormEvent) {
    e.preventDefault()
    if (!gecerli || mesgul) return
    setHata('')
    setMesgul(true)
    try {
      const r = await api.post<Sonuc>(`/domains/${id}/ek-alan-adlari`, { alan_adi: alanAdiNormal(ad), ssl: ssl ? 'letsencrypt' : '' }, UZUN_ISTEK)
      setSonuc(r.data)
      toast.basari(cevir('Alan adı eklendi'), r.data.alan_adi)
      void liste.yenile()
    } catch (err) {
      const m = apiHata(err, cevir('Alan adı eklenemedi'))
      setHata(m)
      toast.hata(cevir('İşlem başarısız'), m)
    } finally {
      setMesgul(false)
    }
  }

  const ust = (
    <>
      <Breadcrumb
        items={[
          { etiket: cevir('Ana sayfa'), href: '/' },
          { etiket: cevir('Domainler'), href: '/domainler' },
          { etiket: domain.veri?.alan_adi || '…', href: kok },
          { etiket: cevir('Alan adı ekle') },
        ]}
      />
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold text-gray-900 dark:text-dark-50">
        <Ikon d={I.kure} className="size-5 text-gray-400 dark:text-dark-300" />
        {cevir('Alan adı ekle')}
      </h1>
      <p className="mb-5 text-sm text-gray-600 dark:text-dark-200">{cevir('Bu hesaba ek bir site ekleyin: kendi dizini, PHP sürümü ve SSL sertifikası olur.')}</p>
    </>
  )

  if (!l) {
    return (
      <div className="max-w-2xl">
        {ust}
        {liste.hata ? (
          <UyariKutusu ton="tehlike" baslik={cevir('Ek alan adları alınamadı.')}>
            <p>{liste.hata}</p>
            <YenidenDene uc={liste} className="mt-3" />
          </UyariKutusu>
        ) : (
          <p className="py-8 text-center text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
        )}
      </div>
    )
  }

  if (sonuc) {
    return (
      <div className="max-w-2xl">
        {ust}
        <Card className="bg-white p-4 dark:bg-dark-700">
          <UyariKutusu ton="basari" baslik={cevir('Alan adı eklendi')} kucuk>
            <p>{cevirF('{0} bu hesaba eklendi.', sonuc.alan_adi)}</p>
          </UyariKutusu>
          <dl className="mt-3 divide-y divide-gray-100 dark:divide-dark-600">
            <div className="flex min-h-10 items-center justify-between gap-3 py-1.5">
              <dt className="shrink-0 text-xs text-gray-500 dark:text-dark-300">{cevir('Alan adı')}</dt>
              <dd className="min-w-0 truncate text-sm text-gray-800 dark:text-dark-50">{sonuc.alan_adi}</dd>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-3 py-1.5">
              <dt className="shrink-0 text-xs text-gray-500 dark:text-dark-300">{cevir('Web dizini')}</dt>
              <dd className="flex min-w-0 items-center gap-0.5 font-mono text-xs text-gray-800 dark:text-dark-50">
                <span className="min-w-0 break-all">{sonuc.docroot}</span>
                <KopyaDugmesi deger={sonuc.docroot} etiket={cevir('Web dizini')} />
              </dd>
            </div>
            {sonuc.ssl && (
              <div className="flex min-h-10 items-center justify-between gap-3 py-1.5">
                <dt className="shrink-0 text-xs text-gray-500 dark:text-dark-300">SSL</dt>
                <dd className="min-w-0 text-end text-sm text-gray-800 dark:text-dark-50">{sonuc.ssl.ok ? cevir('SSL kuruldu') : cevir('SSL kurulamadı')}</dd>
              </div>
            )}
          </dl>
          {sonuc.ssl && !sonuc.ssl.ok && (
            <UyariKutusu ton="uyari" kucuk className="mt-3">
              <p>{sonuc.ssl.hata || cevir('SSL kurulamadı')}</p>
              <p className="mt-1">{cevir('DNS yayıldıktan sonra yönetim sayfasından SSL’i yeniden deneyebilirsiniz.')}</p>
            </UyariKutusu>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <BaglantiDugme to={`${kok}/ek-alanlar/${sonuc.id}`} variant="filled" color="primary" ikonSonda={I.chevronSag}>
              {cevir('Yönet')}
            </BaglantiDugme>
            <BaglantiDugme to={kok} variant="flat">
              {cevir('Hesaba dön')}
            </BaglantiDugme>
          </div>
        </Card>
      </div>
    )
  }

  if (!l.limit.eklenebilir) {
    return (
      <div className="max-w-2xl">
        {ust}
        <UyariKutusu ton="uyari" baslik={cevir('Alan adı eklenemiyor')}>
          <p>{l.limit.neden || cevir('Paketinizde yeni alan adı hakkı yok')}</p>
          <div className="mt-3">
            <BaglantiDugme to={kok} variant="outlined">
              {cevir('Hesaba dön')}
            </BaglantiDugme>
          </div>
        </UyariKutusu>
      </div>
    )
  }

  return (
    <div className="max-w-2xl">
      {ust}
      <form onSubmit={gonder} noValidate>
        <Card className="bg-white p-4 dark:bg-dark-700">
          <Input
            label={cevir('Alan adı')}
            value={ad}
            onChange={(e) => setAd(e.target.value.toLowerCase())}
            placeholder="ornek.com"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            inputMode="url"
            aria-describedby="ek-alan-yardim"
            aria-invalid={ad.trim() !== '' && bicimHatasi !== ''}
            error={ad.trim() !== '' && bicimHatasi !== '' ? cevir(bicimHatasi) : undefined}
            classNames={{ input: 'font-mono' }}
          />
          <div id="ek-alan-yardim" className="mt-4 rounded-lg bg-gray-50 p-3 text-xs leading-relaxed text-gray-600 dark:bg-dark-800 dark:text-dark-200">
            <p className="font-medium text-gray-800 dark:text-dark-50">{cevir('DNS’i bu sunucuya yönlendirin')}</p>
            {ip ? (
              <p className="mt-1 flex flex-wrap items-center gap-x-1">
                {cevir('A kaydı')} →
                <span className="inline-flex items-center font-mono text-gray-800 dark:text-dark-50">
                  {ip}
                  <KopyaDugmesi deger={ip} etiket={cevir('IP adresi')} />
                </span>
              </p>
            ) : (
              <p className="mt-1">{cevir('Alan adının A kaydı bu hesabın IP adresini göstermeli.')}</p>
            )}
            <p className="mt-1">{cevir('Ya da alan adı sağlayıcınızda NS kayıtlarını bu sunucuya yönlendirin.')}</p>
          </div>
          <label className="mt-4 flex min-h-10 cursor-pointer items-start gap-2.5 text-sm text-gray-800 dark:text-dark-50">
            <input type="checkbox" checked={ssl} onChange={(e) => setSsl(e.target.checked)} className="mt-0.5 size-4 shrink-0 rounded accent-primary-600" />
            <span>
              {cevir('SSL’i hemen dene')}
              <span className="mt-0.5 block text-xs text-gray-500 dark:text-dark-300">{cevir('DNS yayılmadan Let’s Encrypt başarısız olur. Emin değilseniz boş bırakın; sonra yönetim sayfasından kurabilirsiniz.')}</span>
            </span>
          </label>
          {hata && (
            <UyariKutusu ton="tehlike" kucuk className="mt-4">
              <p>{hata}</p>
            </UyariKutusu>
          )}
          <div className="mt-4 flex flex-wrap items-center justify-end gap-2">
            <Link to={kok} className="inline-flex min-h-10 items-center px-3 text-sm font-medium text-gray-600 hover:underline dark:text-dark-200">
              {cevir('Vazgeç')}
            </Link>
            <Dugme type="submit" color="primary" boyut="sm" ikon={I.arti} disabled={!gecerli} yukleniyor={mesgul}>
              {cevir('Alan adı ekle')}
            </Dugme>
          </div>
        </Card>
      </form>
    </div>
  )
}
