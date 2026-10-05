// Bayi: özel panel adresi (panel.bayi.com) — TEK "Uygula" eylemi.
// Uygula: alan adını doğrular, DNS'i kontrol eder (bölge bu panelde ve sizindeyse tek A kaydını
// kendisi ekler), SSL sertifikasını alır ve adresi etkinleştirir. İşlem sunucuda arka planda
// ilerler; bu bileşen durumu yoklar (işlem sürerken 2 sn, bitince durur, sekme gizliyken durur).
// Varsayılan kapalı; kök bayi başına açar (izin yoksa sekme görünür, içinde bilgi mesajı çıkar).
import { useCallback, useEffect, useRef, useState } from 'react'
import axios from 'axios'
import { api, apiHata } from '@/lib/api'
import { Badge, Button, Card, Input } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { ALAN_DURUM, useMarkaCeviri } from './ceviri'

type AdimDurum = 'tamam' | 'suruyor' | 'bekliyor' | 'hata'
type Kayit = {
  alan_adi: string; durum: string; txt_ad: string; txt_deger: string; son_hata: string; ssl_bitis: string
  adim?: { ad: string; mesaj: string; guncelleme: string }
  adimlar?: Array<{ ad: 'dogrulama' | 'dns' | 'ssl' | 'aktif'; durum: AdimDurum }>
  dns_kaynak?: string
  dns_kayitlari?: Array<{ tur: string; ad: string; deger: string }>
  isleniyor?: boolean
}
type Yanit = { kayit: Kayit | null; sunucu_ip: string[]; zaten_isleniyor?: boolean; zaten_aktif?: boolean }

const YOKLAMA_MS = 2000

// Adım listesinin etiketi: DNS adımı sunucunun bildirdiği alt adıma göre değişir.
function adimEtiketi(ad: string, altAdim: string): string {
  switch (ad) {
    case 'dogrulama': return 'Alan adı doğrulanıyor'
    case 'dns':
      if (altAdim === 'dns_eklendi') return 'DNS kaydı eklendi'
      if (altAdim === 'dns_bekleniyor' || altAdim === 'dns_zamanasimi') return 'DNS kaydı bekleniyor'
      if (altAdim === 'dns_cakisma') return 'DNS kaydı çakışıyor'
      return 'DNS kontrol ediliyor'
    case 'ssl': return 'SSL sertifikası alınıyor'
    default: return 'Aktif'
  }
}

// Sunucu mesajları sabit bir kümedir (arka uç alanadi_uygula.go); EN karşılıkları burada.
const MESAJ_EN: Record<string, string> = {
  'Alan adı doğrulanıyor.': 'Checking the domain.',
  'DNS kayıtları kontrol ediliyor.': 'Checking DNS records.',
  'DNS kaydı eklendi; yayılması bekleniyor.': 'DNS record added; waiting for it to propagate.',
  'DNS kaydı henüz görünmüyor; sayfa kendiliğinden kontrol ediyor.': 'The DNS record is not visible yet; this page checks by itself.',
  'Alan adınızın ad sunucuları bu sunucuyu göstermiyor; DNS bu sunucuda değil. Aşağıdaki kayıtları DNS’inizi yönettiğiniz yerde ekleyin.': 'Your domain’s name servers do not point to this server, so DNS is not hosted here. Add the records below where you manage your DNS.',
  'A kaydı henüz bu sunucunun IP adresine işaret etmiyor.': 'The A record does not point to this server’s IP address yet.',
  'Doğrulama TXT kaydı bulunamadı ya da değeri eşleşmiyor.': 'The verification TXT record was not found or its value does not match.',
  'A kaydı ve doğrulama TXT kaydı henüz görünmüyor.': 'The A record and the verification TXT record are not visible yet.',
  'DNS kayıtları 30 dakika içinde görünmedi. Kayıtları kontrol edip yeniden kontrol edin.': 'The DNS records did not appear within 30 minutes. Check them, then check again.',
  'DNS doğrulandı; SSL sertifikası alınıyor, birkaç dakika sürebilir.': 'DNS verified; the SSL certificate is being issued, this can take a few minutes.',
  'DNS kaydı eklenemedi; yeniden denenecek.': 'The DNS record could not be added; it will be retried.',
  'DNS durumu okunamadı; yeniden denenecek.': 'DNS status could not be read; it will be retried.',
  'Bu adres kullanılamıyor.': 'This address cannot be used.',
  'Bu adresin bağlı olduğu hosting hesabı askıda ya da pasif.': 'The hosting account this address belongs to is suspended or inactive.',
  'Sunucunun IP adresi bilinmiyor; yöneticinize başvurun.': 'The server IP address is unknown; contact your administrator.',
}

// "Bu ad için mevcut kayıt var: A → 1.2.3.4; kaldırın ya da başka bir ad seçin." (dinamik)
const CAKISMA = /^Bu ad için mevcut kayıt var: (.+?) → (.+); kaldırın ya da başka bir ad seçin\.$/

function mesajCevir(en: boolean, m: string): string {
  if (!en) return m
  const c = CAKISMA.exec(m)
  if (c) return `An existing record for this name exists: ${c[1]} → ${c[2]}; remove it or choose another name.`
  return MESAJ_EN[m] || m
}

function AdimSimgesi({ durum }: { durum: AdimDurum }) {
  if (durum === 'tamam') return <span className="grid size-5 shrink-0 place-items-center rounded-full bg-success/15 text-success" data-adim-durum="tamam"><Ikon d={I.onay} className="size-3.5" /></span>
  if (durum === 'hata') return <span className="grid size-5 shrink-0 place-items-center rounded-full bg-error/15 text-error" data-adim-durum="hata"><Ikon d={I.kapat} className="size-3.5" /></span>
  if (durum === 'suruyor') return <span className="size-5 shrink-0 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600 motion-reduce:animate-none dark:border-dark-600 dark:border-t-brand-400" data-adim-durum="suruyor" />
  return <span className="grid size-5 shrink-0 place-items-center text-gray-400 dark:text-dark-300" data-adim-durum="bekliyor"><Ikon d={I.saat} className="size-4" /></span>
}

export function OzelAdres({ onDegisti }: { onDegisti?: () => void }) {
  const cevir = useMarkaCeviri()
  const en = cevir('Alan adı') === 'Domain'
  const toast = useToast()
  const { onay } = useDialog()
  const [veri, setVeri] = useState<Yanit | null>(null)
  const [izinYok, setIzinYok] = useState(false)
  const [ad, setAd] = useState('')
  const [mesgul, setMesgul] = useState(false)
  const sorguda = useRef(false)
  const canli = useRef(true)
  const oncekiDurum = useRef('')
  const adDoldu = useRef(false)

  useEffect(() => { canli.current = true; return () => { canli.current = false } }, [])

  const uygula = useCallback((r: Yanit) => {
    if (!canli.current) return
    setVeri(r)
    setIzinYok(false)
    const d = r.kayit?.durum || ''
    if (d === 'aktif' && oncekiDurum.current && oncekiDurum.current !== 'aktif') onDegisti?.()
    oncekiDurum.current = d
    if (r.kayit && !adDoldu.current) { adDoldu.current = true; setAd(r.kayit.alan_adi) }
  }, [onDegisti])

  // Tek uçuş: önceki istek bitmeden yenisi başlamaz (istek çakışması yok).
  const yukle = useCallback(async (sessiz = false) => {
    if (sorguda.current) return
    sorguda.current = true
    try {
      const r = await api.get<Yanit>('/marka/alan-adi')
      uygula(r.data)
    } catch (e) {
      if (!canli.current) return
      if (axios.isAxiosError(e) && e.response?.status === 403) setIzinYok(true)
      else if (!sessiz) toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      sorguda.current = false
    }
  }, [toast, cevir, uygula])
  useEffect(() => { void yukle() }, [yukle])

  // Yoklama: yalnız işlem sürerken (2 sn); sekme gizliyken dur, görününce hemen yenile; kalkınca temizle.
  const isleniyor = !!veri?.kayit?.isleniyor
  useEffect(() => {
    if (!isleniyor) return
    const id = window.setInterval(() => { if (!document.hidden) void yukle(true) }, YOKLAMA_MS)
    const gorunur = () => { if (!document.hidden) void yukle(true) }
    document.addEventListener('visibilitychange', gorunur)
    return () => { window.clearInterval(id); document.removeEventListener('visibilitychange', gorunur) }
  }, [isleniyor, yukle])

  async function uygulaTikla() {
    setMesgul(true)
    try {
      const r = await api.post<Yanit>('/marka/alan-adi/uygula', { alan_adi: ad.trim() })
      uygula(r.data)
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      if (canli.current) setMesgul(false)
    }
  }

  async function simdiKontrol() {
    setMesgul(true)
    try {
      const r = await api.post<Yanit>('/marka/alan-adi/dogrula')
      uygula(r.data)
    } catch (e) {
      toast.hata(cevir('Doğrulanamadı'), apiHata(e))
    } finally {
      if (canli.current) setMesgul(false)
    }
  }

  async function kaldir() {
    if (!(await onay({ baslik: cevir('Adres kaldırılsın mı?'), mesaj: cevir('Müşterileriniz bu adresten artık giriş yapamaz.'), ton: 'tehlike', onayIkonu: I.cop, onayEtiketi: cevir('Kaldır') }))) return
    try {
      await api.delete('/marka/alan-adi')
      toast.basari(cevir('Kaldırıldı'))
      adDoldu.current = false
      oncekiDurum.current = ''
      setAd('')
      await yukle()
      onDegisti?.()
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    }
  }

  if (izinYok) return <UyariKutusu ton="bilgi">{cevir('Özel panel adresi bu hesap için açık değil. Hizmet sağlayıcınızla iletişime geçin.')}</UyariKutusu>
  if (!veri) return <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
  const k = veri.kayit
  const d = k ? ALAN_DURUM[k.durum] || ALAN_DURUM.hata : null
  const altAdim = k?.adim?.ad || ''
  const aktif = k?.durum === 'aktif'
  const dnsHata = altAdim === 'dns_cakisma' || altAdim === 'dns_red'
  const yenidenDene = !!k && !aktif && (dnsHata || k.durum === 'hata')
  const simdiKontrolGoster = !!k && !aktif && k.durum === 'dns_bekliyor' && !dnsHata
  const mesaj = k?.adim?.mesaj || ''
  const sonHata = k?.son_hata || ''

  return (
    <Card skin="bordered" className="space-y-4 p-4">
      <p className="text-sm text-gray-600 dark:text-dark-200">{cevir('Panelinize kendi alan adınızla ulaşın (ör. panel.ornek.com). Müşterileriniz bu adreste giriş sayfasında sizin markanızı görür.')}</p>

      {!aktif && (
        <form className="flex flex-col gap-2 sm:flex-row sm:items-end" onSubmit={(e) => { e.preventDefault(); void uygulaTikla() }}>
          <Input label={cevir('Özel adres')} value={ad} onChange={(e) => setAd(e.target.value)} placeholder="panel.ornek.com" maxLength={253} classNames={{ root: 'flex-1' }} />
          <Button type="submit" color="primary" ikon={I.onay} yukleniyor={mesgul} disabled={!ad.trim() || !!k?.isleniyor}>{cevir('Uygula')}</Button>
        </form>
      )}

      {k && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <span className="break-all font-mono text-sm font-medium text-gray-800 dark:text-dark-50">{k.alan_adi}</span>
            {d && <Badge color={d.renk} variant="soft">{cevir(d.etiket)}</Badge>}
          </div>

          <ol className="space-y-2" aria-label={cevir('Adımlar')} data-adim-listesi>
            {(k.adimlar || []).map((a) => (
              <li key={a.ad} className="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-100" data-adim={a.ad}>
                <AdimSimgesi durum={a.durum} />
                <span className={a.durum === 'bekliyor' ? 'text-gray-500 dark:text-dark-300' : ''}>{cevir(adimEtiketi(a.ad, altAdim))}</span>
              </li>
            ))}
          </ol>

          {mesaj && !aktif && k.durum === 'dns_bekliyor' && !sonHata && <p className="text-xs text-gray-500 dark:text-dark-300">{mesajCevir(en, mesaj)}</p>}
          {sonHata && <UyariKutusu ton="uyari" kucuk>{mesajCevir(en, sonHata)}</UyariKutusu>}
          {k.durum === 'ssl_bekliyor' && <UyariKutusu ton="bilgi" kucuk>{cevir('DNS doğrulandı. Sertifika birkaç dakika içinde otomatik alınır ve adres etkinleşir; bu sayfa kendiliğinden yenilenir.')}</UyariKutusu>}
          {aktif && (
            <UyariKutusu ton="basari" kucuk>
              {cevir('Adres etkin.')}
              {k.ssl_bitis && <> {cevir('Sertifika geçerlilik sonu')}: <span className="font-mono">{k.ssl_bitis}</span> ({cevir('otomatik yenilenir')})</>}
            </UyariKutusu>
          )}
          {k.durum === 'hata' && <p className="text-xs text-gray-500 dark:text-dark-300">{cevir('Sorunu giderdiyseniz Yeniden dene’ye basın; sunucu yeniden dener.')}</p>}

          {!aktif && k.durum === 'dns_bekliyor' && (k.dns_kayitlari?.length ?? 0) > 0 && (
            <div data-dns-tablosu>
              <h4 className="text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('DNS kayıtları')}</h4>
              <p className="mt-1 text-xs text-gray-500 dark:text-dark-300">{cevir('Alan adınızın DNS yönetiminde şu kayıtları oluşturun:')}</p>
              <div className="mt-2 space-y-2 text-xs">
                {(k.dns_kayitlari || []).map((r) => (
                  <div key={r.tur + r.deger} className="grid grid-cols-[3rem_minmax(0,1fr)_auto] items-start gap-x-2 gap-y-0.5 rounded border border-gray-200 p-2 dark:border-dark-500">
                    <span className="text-gray-400">{cevir('Tür')}</span><span className="font-mono">{r.tur}</span>
                    <Button boyut="sm" variant="outlined" ikon={I.kopya} className="row-span-3" onClick={() => {
                      navigator.clipboard?.writeText(r.deger).then(() => toast.basari(cevir('Kopyalandı')), () => { /* pano kapalı */ })
                    }}>{cevir('Kopyala')}</Button>
                    <span className="text-gray-400">{cevir('Ad')}</span><span className="break-all font-mono">{r.ad}</span>
                    <span className="text-gray-400">{cevir('Değer')}</span><span className="break-all font-mono">{r.deger}</span>
                  </div>
                ))}
              </div>
              <p className="mt-2 text-xs text-gray-500 dark:text-dark-300">{cevir('DNS yayılımı birkaç dakika sürebilir; sayfa kendiliğinden kontrol eder.')}</p>
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            {simdiKontrolGoster && <Button color="primary" variant="outlined" ikon={I.yenile} yukleniyor={mesgul} onClick={simdiKontrol}>{cevir('Şimdi kontrol et')}</Button>}
            {yenidenDene && <Button color="primary" ikon={I.yenile} yukleniyor={mesgul} onClick={uygulaTikla}>{cevir('Yeniden dene')}</Button>}
            <Button variant="outlined" color="error" ikon={I.cop} disabled={mesgul} onClick={kaldir}>{cevir('Adresi kaldır')}</Button>
          </div>
        </>
      )}
    </Card>
  )
}
