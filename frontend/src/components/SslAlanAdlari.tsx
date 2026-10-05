// SslAlanAdlari — hesabın TÜM alan adlarının (ana + ek + alt) SSL durumu tek tabloda.
// Veri: GET /domains/{id}/ssl/hedefler (salt okuma). İşlemler MEVCUT uçlara hedefin
// türüne göre gider (yeni yazma ucu yok):
//   ana → sayfanın kendi akışı (asenkron kurulum + ilerleme paneli; DomainSSLPage)
//   ek  → /domains/{id}/ek-alan-adlari/{eid}/ssl   (POST {tip} | DELETE)
//   alt → /domains/{id}/subdomain/{sid}/ssl        (POST {tip} | DELETE)
// Sahte veri yok: sertifika türü/bitiş sunucunun dosyadan okuduğu değerdir.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import AlanAdiSecici, { TurRozeti, type AlanAdiSecenek } from '@/components/AlanAdiSecici'

export type SslHedef = {
  tur: 'ana' | 'ek' | 'alt'
  alan_adi: string
  ek_alan_id?: number
  alt_alan_id?: number
  ssl_aktif: boolean
  tur_sertifika: 'letsencrypt' | 'oz-imza' | 'bilinmiyor' | null
  bitis?: string
  kalan_gun: number | null
  durum: 'aktif' | 'yok' | 'yaklasiyor' | 'doldu' | 'okunamadi'
  otomatik_yenileme: boolean
  sonraki_yenileme?: string
  yenileme_gecikti: boolean
  kapsam: string[]
  dns_hazir: boolean | null
  askida: boolean
  demo: boolean
}

type Tip = 'letsencrypt' | 'self-signed'

const EN: Record<string, string> = {
  'Alan adları': 'Domains',
  '{0} alan adı · {1} SSL aktif · {2} süresi yaklaşan (≤14 gün)': '{0} domains · {1} with SSL · {2} expiring soon (≤14 days)',
  '{0} süresi dolmuş': '{0} expired',
  'Alan adı': 'Domain',
  'Tüm alan adları': 'All domains',
  'Alan adı seç': 'Select domain',
  'Yalnız SSL’i olmayanlar': 'Only without SSL',
  'Tümünü göster': 'Show all',
  'Durum': 'Status',
  'Sertifika': 'Certificate',
  'Bitiş': 'Expires',
  'Kapsam': 'Coverage',
  'İşlemler': 'Actions',
  'Aktif': 'Active',
  'Yok': 'None',
  'Süresi yaklaşıyor': 'Expiring soon',
  'Süresi doldu': 'Expired',
  'Okunamadı': 'Unreadable',
  'Let’s Encrypt': 'Let’s Encrypt',
  'Öz-imza': 'Self-signed',
  'Bilinmiyor': 'Unknown',
  '{0} gün': '{0} days',
  '{0} gün önce doldu': 'expired {0} days ago',
  'bugün': 'today',
  'alan adı': 'domain',
  'SSL kur': 'Install SSL',
  'Let’s Encrypt (ücretsiz)': 'Let’s Encrypt (free)',
  'Öz-imzalı': 'Self-signed',
  'Let’s Encrypt al': 'Get Let’s Encrypt',
  'Yenile': 'Renew',
  'Kaldır': 'Remove',
  'Kuruluyor…': 'Installing…',
  'Kaldırılıyor…': 'Removing…',
  'Otomatik yenileme açık': 'Auto-renewal on',
  'Otomatik yenileme kaydı yok': 'No auto-renewal record',
  'Otomatik yenileme gecikti ({0} bekleniyordu); “Yenile” ile deneyin.': 'Auto-renewal is overdue (expected {0}); try “Renew”.',
  'Alan adı bu sunucuya yönlenmiyor; Let’s Encrypt başarısız olabilir. DNS A kaydını yönlendirin, yayılınca tekrar deneyin.': 'The domain does not point to this server; Let’s Encrypt may fail. Point the DNS A record here and retry once it propagates.',
  'DNS’i tekrar kontrol et': 'Re-check DNS',
  'Hesap askıda; SSL işlemi yapılamaz.': 'Account is suspended; SSL actions are unavailable.',
  'Demo hesapta SSL işlemi yapılamaz.': 'SSL actions are unavailable on demo accounts.',
  'Sertifika dosyası okunamadı; yeniden kurmayı deneyin.': 'The certificate file could not be read; try installing again.',
  'Alan adı listesi alınamadı': 'Could not load the domain list',
  'Yükleniyor…': 'Loading…',
  'Yeniden dene': 'Try again',
  'Henüz alan adı yok': 'No domains yet',
  'Kaydedildi': 'Saved',
  'İşlem başarısız': 'Operation failed',
  'SSL kuruldu: {0}': 'SSL installed: {0}',
  'SSL kaldırıldı: {0}': 'SSL removed: {0}',
  'SSL kurulumu başlatıldı: {0}': 'SSL installation started: {0}',
  'Onay gerekiyor': 'Confirmation required',
  '{0} bu sunucuya yönlenmiyor görünüyor; Let’s Encrypt büyük olasılıkla başarısız olur. Yine de denensin mi?': '{0} does not appear to point to this server; Let’s Encrypt will most likely fail. Try anyway?',
  'Yine de dene': 'Try anyway',
  '{0} için SSL kaldırılsın mı? Site HTTP’ye dönecek.': 'Remove SSL for {0}? The site will revert to HTTP.',
  'SSL kurulamadı': 'SSL could not be installed',
  'SSL kaldırılamadı': 'SSL could not be removed',
  'Alan adı süzgeci': 'Domain filter',
  'Ana': 'Primary',
  'Ek': 'Addon',
  'Alt': 'Sub',
  'Hesap SSL listesi': 'Account SSL list',
  'SSL’i olan alan adı yok': 'No domain has SSL',
  'SSL’i olmayan alan adı yok': 'Every domain has SSL',
}

function t(tr: string, ...a: Array<string | number>): string {
  const s = i18n.language === 'en' ? (EN[tr] || tr) : tr
  return s.replace(/\{(\d+)\}/g, (_, i) => String(a[Number(i)] ?? ''))
}

export function hedefAnahtari(h: Pick<SslHedef, 'tur' | 'ek_alan_id' | 'alt_alan_id'>): string {
  if (h.tur === 'ek') return `ek:${h.ek_alan_id}`
  if (h.tur === 'alt') return `alt:${h.alt_alan_id}`
  return 'ana'
}

// sslUc — hedefin MEVCUT SSL ucu (yalnız ek/alt; ana sayfanın kendi akışıyla yürür).
export function sslUc(id: string | number, h: SslHedef): string | null {
  if (h.tur === 'ek') return `/domains/${id}/ek-alan-adlari/${h.ek_alan_id}/ssl`
  if (h.tur === 'alt') return `/domains/${id}/subdomain/${h.alt_alan_id}/ssl`
  return null
}

function DurumRozeti({ h }: { h: SslHedef }) {
  const harita: Record<SslHedef['durum'], { renk: 'success' | 'neutral' | 'warning' | 'error'; yazi: string }> = {
    aktif: { renk: 'success', yazi: t('Aktif') },
    yok: { renk: 'neutral', yazi: t('Yok') },
    yaklasiyor: { renk: 'warning', yazi: t('Süresi yaklaşıyor') },
    doldu: { renk: 'error', yazi: t('Süresi doldu') },
    okunamadi: { renk: 'warning', yazi: t('Okunamadı') },
  }
  const d = harita[h.durum]
  return (
    <Badge color={d.renk} variant="soft" className="gap-1.5 px-1.5 py-0.5 text-tiny-plus font-semibold" data-durum={h.durum}>
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      {d.yazi}
    </Badge>
  )
}

function SertifikaHucre({ h }: { h: SslHedef }) {
  if (!h.tur_sertifika) return <span className="text-gray-400 dark:text-dark-300">—</span>
  const ad = h.tur_sertifika === 'letsencrypt' ? t('Let’s Encrypt') : h.tur_sertifika === 'oz-imza' ? t('Öz-imza') : t('Bilinmiyor')
  return (
    <span className={h.tur_sertifika === 'oz-imza' ? 'text-amber-700 dark:text-amber-300' : 'text-gray-800 dark:text-dark-50'}>{ad}</span>
  )
}

function BitisHucre({ h }: { h: SslHedef }) {
  if (!h.bitis) return <span className="text-gray-400 dark:text-dark-300">—</span>
  const k = h.kalan_gun
  const kalan = k == null ? '' : k < 0 ? t('{0} gün önce doldu', -k) : k === 0 ? t('bugün') : t('{0} gün', k)
  return (
    <span className="text-gray-800 dark:text-dark-50">
      <span className="font-mono text-xs">{h.bitis}</span>
      {kalan && <span className="block text-xs text-gray-500 dark:text-dark-300">{kalan}</span>}
    </span>
  )
}

function KapsamHucre({ h }: { h: SslHedef }) {
  return (
    <span className="flex flex-wrap gap-1">
      {h.kapsam.map(k => (
        <span key={k} className="rounded bg-gray-100 px-1.5 py-0.5 text-tiny-plus text-gray-700 dark:bg-dark-600 dark:text-dark-100">
          {k === 'www' ? 'www' : t('alan adı')}
        </span>
      ))}
    </span>
  )
}

type Mesgul = Record<string, 'kur' | 'kaldir'>

function Islemler({ h, mesgul, onKur, onKaldir }: { h: SslHedef; mesgul?: 'kur' | 'kaldir'; onKur: (tip: Tip) => void; onKaldir: () => void }) {
  const [acik, setAcik] = useState(false)
  const kilit = !!mesgul || h.askida || h.demo
  const le = h.tur_sertifika === 'letsencrypt'
  const kurulu = h.ssl_aktif
  return (
    <div className="flex flex-wrap items-start gap-1.5">
      {!kurulu || h.durum === 'okunamadi' ? (
        <div className="flex flex-col items-start gap-1">
          <Button color="primary" boyut="sm" ikon={I.kalkan} disabled={kilit} yukleniyor={mesgul === 'kur'}
            onClick={() => setAcik(a => !a)} aria-expanded={acik} aria-haspopup="menu">
            {mesgul === 'kur' ? t('Kuruluyor…') : `${t('SSL kur')} ▾`}
          </Button>
          {acik && !kilit && (
            <div role="menu" className="flex flex-col gap-1 rounded-md border border-gray-200 bg-white p-1 dark:border-dark-500 dark:bg-dark-700">
              <Button role="menuitem" variant="flat" boyut="sm" onClick={() => { setAcik(false); onKur('letsencrypt') }}>{t('Let’s Encrypt (ücretsiz)')}</Button>
              <Button role="menuitem" variant="flat" boyut="sm" onClick={() => { setAcik(false); onKur('self-signed') }}>{t('Öz-imzalı')}</Button>
            </div>
          )}
        </div>
      ) : le ? (
        <Button variant="outlined" boyut="sm" ikon={I.yenile} disabled={kilit} yukleniyor={mesgul === 'kur'} onClick={() => onKur('letsencrypt')}>
          {mesgul === 'kur' ? t('Kuruluyor…') : t('Yenile')}
        </Button>
      ) : (
        <Button color="primary" variant="outlined" boyut="sm" ikon={I.kalkan} disabled={kilit} yukleniyor={mesgul === 'kur'} onClick={() => onKur('letsencrypt')}>
          {mesgul === 'kur' ? t('Kuruluyor…') : t('Let’s Encrypt al')}
        </Button>
      )}
      {kurulu && (
        <Button color="error" variant="soft" boyut="sm" ikon={I.cop} disabled={kilit} yukleniyor={mesgul === 'kaldir'} onClick={onKaldir}>
          {mesgul === 'kaldir' ? t('Kaldırılıyor…') : t('Kaldır')}
        </Button>
      )}
    </div>
  )
}

// Notlar — satır altı sade uyarılar (DNS hazır değil, yenileme gecikti, okunamadı, askı).
function Notlar({ h, onDnsTekrar }: { h: SslHedef; onDnsTekrar: () => void }) {
  const le = h.tur_sertifika === 'letsencrypt'
  const dnsUyari = h.dns_hazir === false && (!h.ssl_aktif || h.tur_sertifika === 'oz-imza')
  return (
    <div className="space-y-1">
      {dnsUyari && (
        <p className="text-xs text-amber-700 dark:text-amber-300" data-not="dns">
          {t('Alan adı bu sunucuya yönlenmiyor; Let’s Encrypt başarısız olabilir. DNS A kaydını yönlendirin, yayılınca tekrar deneyin.')}{' '}
          <button type="button" className="underline" onClick={onDnsTekrar}>{t('DNS’i tekrar kontrol et')}</button>
        </p>
      )}
      {le && h.yenileme_gecikti && (
        <p className="text-xs text-amber-700 dark:text-amber-300" data-not="yenileme">{t('Otomatik yenileme gecikti ({0} bekleniyordu); “Yenile” ile deneyin.', h.sonraki_yenileme || '')}</p>
      )}
      {le && !h.yenileme_gecikti && (
        <p className="text-xs text-gray-500 dark:text-dark-300">{h.otomatik_yenileme ? t('Otomatik yenileme açık') : t('Otomatik yenileme kaydı yok')}</p>
      )}
      {h.durum === 'okunamadi' && <p className="text-xs text-amber-700 dark:text-amber-300">{t('Sertifika dosyası okunamadı; yeniden kurmayı deneyin.')}</p>}
      {h.askida && <p className="text-xs text-gray-500 dark:text-dark-300">{t('Hesap askıda; SSL işlemi yapılamaz.')}</p>}
      {!h.askida && h.demo && <p className="text-xs text-gray-500 dark:text-dark-300">{t('Demo hesapta SSL işlemi yapılamaz.')}</p>}
    </div>
  )
}

export type SslAlanAdlariProps = {
  id: string
  hedef: string // '' = tümü | 'ana' | 'ek:<id>' | 'alt:<id>'
  onHedef: (h: string) => void
  // ana alan adı işlemleri sayfanın mevcut (asenkron, ilerleme panelli) akışıdır
  anaKur: (tip: Tip) => void
  anaKaldir: () => void
  anaMesgul: boolean
  yenileSay: number // artınca liste yeniden okunur
}

export default function SslAlanAdlari({ id, hedef, onHedef, anaKur, anaKaldir, anaMesgul, yenileSay }: SslAlanAdlariProps) {
  useTranslation()
  const toast = useToast()
  const { onay } = useDialog()
  const [liste, setListe] = useState<SslHedef[] | null>(null)
  const [hata, setHata] = useState(false)
  const [yukleniyor, setYukleniyor] = useState(false)
  const [mesgul, setMesgul] = useState<Mesgul>({})
  const [yalnizYok, setYalnizYok] = useState(false)
  const nesil = useRef(0)
  const dnsDeneme = useRef(0)

  const yukle = useCallback(async () => {
    const n = ++nesil.current
    setYukleniyor(true)
    try {
      const r = await api.get<{ hedefler: SslHedef[] }>(`/domains/${id}/ssl/hedefler`)
      if (n !== nesil.current) return
      setListe(r.data.hedefler || [])
      setHata(false)
    } catch {
      if (n === nesil.current) setHata(true)
    } finally {
      if (n === nesil.current) setYukleniyor(false)
    }
  }, [id])

  useEffect(() => { dnsDeneme.current = 0; void yukle(); return () => { nesil.current++ } }, [yukle, yenileSay])

  // DNS hazırlığı sunucuda arka planda çözülür (null = henüz bilinmiyor): en çok 3 kez, 3 sn arayla tekrar oku.
  useEffect(() => {
    if (!liste || !liste.some(h => h.dns_hazir === null && (!h.ssl_aktif || h.tur_sertifika === 'oz-imza'))) return
    if (dnsDeneme.current >= 3) return
    const z = setTimeout(() => { dnsDeneme.current++; void yukle() }, 3000)
    return () => clearTimeout(z)
  }, [liste, yukle])

  const secenekler: AlanAdiSecenek[] = useMemo(() => (liste || []).map(h => ({
    deger: hedefAnahtari(h), alanAdi: h.alan_adi, tur: h.tur,
    pasif: h.askida || h.demo, ipucu: h.askida ? t('Hesap askıda; SSL işlemi yapılamaz.') : h.demo ? t('Demo hesapta SSL işlemi yapılamaz.') : undefined,
  })), [liste])
  // Pasif (askıda) seçenekler yalnız bilgi amaçlı listelenir; seçilebilir kalması için
  // süzgeçte yine de gösterilir (disabled seçilemez): askıda hesapta tümü pasif olur.
  const ozet = useMemo(() => {
    const l = liste || []
    return {
      toplam: l.length,
      aktif: l.filter(h => h.ssl_aktif).length,
      yaklasan: l.filter(h => h.durum === 'yaklasiyor').length,
      dolmus: l.filter(h => h.durum === 'doldu').length,
    }
  }, [liste])

  const gorunen = useMemo(() => {
    let l = liste || []
    if (hedef) l = l.filter(h => hedefAnahtari(h) === hedef)
    if (yalnizYok) l = l.filter(h => !h.ssl_aktif)
    return l
  }, [liste, hedef, yalnizYok])

  async function kur(h: SslHedef, tip: Tip) {
    if (h.tur === 'ana') { anaKur(tip); return }
    const uc = sslUc(id, h)
    if (!uc) return
    if (tip === 'letsencrypt' && h.dns_hazir === false) {
      if (!(await onay({ baslik: t('Onay gerekiyor'), mesaj: t('{0} bu sunucuya yönlenmiyor görünüyor; Let’s Encrypt büyük olasılıkla başarısız olur. Yine de denensin mi?', h.alan_adi), onayEtiketi: t('Yine de dene') }))) return
    }
    const k = hedefAnahtari(h)
    setMesgul(m => ({ ...m, [k]: 'kur' }))
    try {
      await api.post(uc, { tip }, UZUN_ISTEK)
      toast.basari(t('Kaydedildi'), t('SSL kuruldu: {0}', h.alan_adi))
    } catch (e) {
      toast.hata(t('SSL kurulamadı'), apiHata(e, t('İşlem başarısız')))
    } finally {
      setMesgul(m => { const n = { ...m }; delete n[k]; return n })
      void yukle()
    }
  }

  async function kaldir(h: SslHedef) {
    if (h.tur === 'ana') { anaKaldir(); return }
    const uc = sslUc(id, h)
    if (!uc) return
    if (!(await onay({ baslik: t('Onay gerekiyor'), mesaj: t('{0} için SSL kaldırılsın mı? Site HTTP’ye dönecek.', h.alan_adi), ton: 'tehlike', onayIkonu: I.cop }))) return
    const k = hedefAnahtari(h)
    setMesgul(m => ({ ...m, [k]: 'kaldir' }))
    try {
      await api.delete(uc, UZUN_ISTEK)
      toast.basari(t('Kaydedildi'), t('SSL kaldırıldı: {0}', h.alan_adi))
    } catch (e) {
      toast.hata(t('SSL kaldırılamadı'), apiHata(e, t('İşlem başarısız')))
    } finally {
      setMesgul(m => { const n = { ...m }; delete n[k]; return n })
      void yukle()
    }
  }

  const mesgulDe = (h: SslHedef): 'kur' | 'kaldir' | undefined => mesgul[hedefAnahtari(h)] ?? (h.tur === 'ana' && anaMesgul ? 'kur' : undefined)
  const dnsTekrar = () => { dnsDeneme.current = 0; void yukle() }

  return (
    <section className="mb-5 rounded-lg border border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-700" aria-label={t('Hesap SSL listesi')}>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-2.5 dark:border-dark-600">
        <h2 className="text-sm font-semibold text-gray-800 dark:text-dark-50">{t('Alan adları')}</h2>
        {liste && (
          <p className="text-xs text-gray-600 dark:text-dark-200" data-ozet>
            {t('{0} alan adı · {1} SSL aktif · {2} süresi yaklaşan (≤14 gün)', ozet.toplam, ozet.aktif, ozet.yaklasan)}
            {ozet.dolmus > 0 && <span className="ms-1 font-medium text-red-600 dark:text-red-400">· {t('{0} süresi dolmuş', ozet.dolmus)}</span>}
          </p>
        )}
      </div>

      <div className="flex flex-wrap items-end gap-3 px-4 py-3">
        <AlanAdiSecici
          className="w-full sm:w-80"
          ariaEtiket={t('Alan adı süzgeci')}
          secenekler={secenekler}
          deger={hedef}
          onDegis={onHedef}
          tumuEtiketi={t('Tüm alan adları')}
          disabled={!liste}
        />
        <Button variant={yalnizYok ? 'filled' : 'outlined'} color={yalnizYok ? 'primary' : 'neutral'} boyut="sm" onClick={() => setYalnizYok(v => !v)} aria-pressed={yalnizYok}>
          {t('Yalnız SSL’i olmayanlar')}
        </Button>
      </div>

      {hata && !liste && (
        <div className="px-4 pb-4">
          <UyariKutusu ton="tehlike" kucuk>{t('Alan adı listesi alınamadı')}</UyariKutusu>
          <Button className="mt-2" variant="outlined" boyut="sm" ikon={I.yenile} yukleniyor={yukleniyor} onClick={() => void yukle()}>{t('Yeniden dene')}</Button>
        </div>
      )}
      {!liste && !hata && <p className="px-4 pb-4 text-sm text-gray-500 dark:text-dark-300">{t('Yükleniyor…')}</p>}

      {liste && gorunen.length === 0 && (
        <p className="px-4 pb-4 text-sm text-gray-500 dark:text-dark-300">{liste.length === 0 ? t('Henüz alan adı yok') : t('SSL’i olmayan alan adı yok')}</p>
      )}

      {liste && gorunen.length > 0 && (
        <>
          {/* Geniş ekran: tablo */}
          <div className="hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-y border-gray-100 text-start text-xs text-gray-500 dark:border-dark-600 dark:text-dark-300">
                  <th scope="col" className="px-4 py-2 text-start font-medium">{t('Alan adı')}</th>
                  <th scope="col" className="px-2 py-2 text-start font-medium">{t('Durum')}</th>
                  <th scope="col" className="px-2 py-2 text-start font-medium">{t('Sertifika')}</th>
                  <th scope="col" className="px-2 py-2 text-start font-medium">{t('Bitiş')}</th>
                  <th scope="col" className="px-2 py-2 text-start font-medium">{t('Kapsam')}</th>
                  <th scope="col" className="px-4 py-2 text-start font-medium">{t('İşlemler')}</th>
                </tr>
              </thead>
              <tbody>
                {gorunen.map(h => (
                  <tr key={hedefAnahtari(h)} className="border-b border-gray-100 align-top last:border-0 dark:border-dark-600" data-hedef={hedefAnahtari(h)}>
                    <td className="px-4 py-2.5">
                      <div className="flex items-center gap-2">
                        <span className="break-all font-medium text-gray-900 dark:text-dark-50">{h.alan_adi}</span>
                        <TurRozeti tur={h.tur} />
                      </div>
                      <div className="mt-1"><Notlar h={h} onDnsTekrar={dnsTekrar} /></div>
                    </td>
                    <td className="px-2 py-2.5"><DurumRozeti h={h} /></td>
                    <td className="px-2 py-2.5"><SertifikaHucre h={h} /></td>
                    <td className="px-2 py-2.5"><BitisHucre h={h} /></td>
                    <td className="px-2 py-2.5"><KapsamHucre h={h} /></td>
                    <td className="px-4 py-2.5">
                      <Islemler h={h} mesgul={mesgulDe(h)} onKur={tip => void kur(h, tip)} onKaldir={() => void kaldir(h)} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {/* Dar ekran (375px): kart listesi */}
          <ul className="divide-y divide-gray-100 border-t border-gray-100 dark:divide-dark-600 dark:border-dark-600 md:hidden">
            {gorunen.map(h => (
              <li key={hedefAnahtari(h)} className="space-y-2 px-4 py-3" data-hedef={hedefAnahtari(h)}>
                <div className="flex items-center justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="break-all font-medium text-gray-900 dark:text-dark-50">{h.alan_adi}</span>
                    <TurRozeti tur={h.tur} />
                  </div>
                  <DurumRozeti h={h} />
                </div>
                <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                  <dt className="text-gray-500 dark:text-dark-300">{t('Sertifika')}</dt><dd><SertifikaHucre h={h} /></dd>
                  <dt className="text-gray-500 dark:text-dark-300">{t('Bitiş')}</dt><dd><BitisHucre h={h} /></dd>
                  <dt className="text-gray-500 dark:text-dark-300">{t('Kapsam')}</dt><dd><KapsamHucre h={h} /></dd>
                </dl>
                <Notlar h={h} onDnsTekrar={dnsTekrar} />
                <Islemler h={h} mesgul={mesgulDe(h)} onKur={tip => void kur(h, tip)} onKaldir={() => void kaldir(h)} />
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
