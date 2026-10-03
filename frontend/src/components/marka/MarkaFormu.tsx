// Marka formu — kök (dosya) ve bayi (kendi satırı) için ORTAK. Sahibi sunucu
// jetondan belirler; bu bileşen hiçbir kimlik göndermez.
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { gorselAdres, markaTazele, markaUygula, type Marka } from '@/lib/marka'
import { Button, Card, Input } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { GirisOnizleme } from './GirisOnizleme'
import { useMarkaCeviri } from './ceviri'

export type YonetGorunum = {
  sahip: 'kok' | 'bayi'
  anahtar: string
  kayitli: boolean
  surum: number
  kok_kapatti: boolean
  alanadi_izni: boolean
  marka: Marka & { webmail_marka?: boolean }
  musteri_adresi?: string
}

type Metin = keyof Pick<Marka, 'panel_adi' | 'baslik' | 'kisa_ad' | 'vurgu_renk' | 'giris_baslik' | 'giris_alt' |
  'giris_dipnot' | 'footer' | 'destek_url' | 'destek_eposta' | 'destek_telefon' | 'banner_metin' | 'banner_url'>

const METINLER: Metin[] = ['panel_adi', 'baslik', 'kisa_ad', 'vurgu_renk', 'giris_baslik', 'giris_alt', 'giris_dipnot',
  'footer', 'destek_url', 'destek_eposta', 'destek_telefon', 'banner_metin', 'banner_url']

const HEX = /^#[0-9a-fA-F]{6}$/

function kontrastBeyaz(hex: string): number {
  if (!HEX.test(hex)) return 21
  const k = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  const L = 0.2126 * k[0] + 0.7152 * k[1] + 0.0722 * k[2]
  return 1.05 / (L + 0.05)
}

type YuvaAd = 'logo' | 'favicon' | 'banner'

function GorselYuvasi({ ad, etiket, url, onDegisti }: { ad: YuvaAd; etiket: string; url: string | null; onDegisti: (g: YonetGorunum) => void }) {
  const cevir = useMarkaCeviri()
  const toast = useToast()
  const { onay } = useDialog()
  const dosya = useRef<HTMLInputElement>(null)
  const [mesgul, setMesgul] = useState(false)

  async function yukle(f: File) {
    setMesgul(true)
    try {
      const fd = new FormData()
      fd.append('dosya', f)
      const r = await api.post<YonetGorunum>(`/marka/yonet/gorsel/${ad}`, fd)
      onDegisti(r.data)
      toast.basari(cevir('Görsel yüklendi'))
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(false)
      if (dosya.current) dosya.current.value = ''
    }
  }

  async function kaldir() {
    if (!(await onay({ baslik: cevir('Kaldır') + ': ' + etiket, ton: 'tehlike', onayIkonu: I.cop, onayEtiketi: cevir('Kaldır') }))) return
    setMesgul(true)
    try {
      const r = await api.delete<YonetGorunum>(`/marka/yonet/gorsel/${ad}`)
      onDegisti(r.data)
      toast.basari(cevir('Görsel kaldırıldı'))
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(false)
    }
  }

  return (
    <div className="flex items-center gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-500">
      <div className="flex h-14 w-20 shrink-0 items-center justify-center overflow-hidden rounded bg-gray-100 dark:bg-dark-600">
        {url ? <img src={url} alt="" className="max-h-14 max-w-20 object-contain" /> : <Ikon d={I.kure} className="size-6 text-gray-300 dark:text-dark-400" />}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-gray-800 dark:text-dark-100">{etiket}</p>
        <div className="mt-1.5 flex flex-wrap gap-2">
          <Button boyut="sm" variant="outlined" ikon={I.yukle} yukleniyor={mesgul} onClick={() => dosya.current?.click()}>
            {url ? cevir('Değiştir') : cevir('Yükle')}
          </Button>
          {url && (
            <Button boyut="sm" variant="flat" color="error" ikon={I.cop} disabled={mesgul} onClick={kaldir}>{cevir('Kaldır')}</Button>
          )}
        </div>
      </div>
      <input ref={dosya} type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/x-icon" className="hidden"
        onChange={(e) => { const f = e.target.files?.[0]; if (f) void yukle(f) }} />
    </div>
  )
}

export function MarkaFormu({ veri, onDegisti }: { veri: YonetGorunum; onDegisti: (g: YonetGorunum) => void }) {
  const cevir = useMarkaCeviri()
  const toast = useToast()
  const { onay } = useDialog()
  const [taslak, setTaslak] = useState<YonetGorunum['marka']>(veri.marka)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  useEffect(() => { setTaslak(veri.marka) }, [veri])
  const kok = veri.sahip === 'kok'

  const degisen = useMemo(() => {
    const d: Record<string, string | boolean> = {}
    for (const k of METINLER) if ((taslak[k] ?? '') !== (veri.marka[k] ?? '')) d[k] = taslak[k] as string
    if (taslak.surum_goster !== veri.marka.surum_goster) d.surum_goster = taslak.surum_goster
    if (kok && !!taslak.webmail_marka !== !!veri.marka.webmail_marka) d.webmail_marka = !!taslak.webmail_marka
    return d
  }, [taslak, veri, kok])
  const kirli = Object.keys(degisen).length > 0 || (!kok && !veri.kayitli)

  function ayarla<K extends keyof YonetGorunum['marka']>(k: K, v: YonetGorunum['marka'][K]) {
    setTaslak((t) => ({ ...t, [k]: v }))
  }

  function yansit(g: YonetGorunum) {
    onDegisti(g)
    // Yönetici kök markasını, bayi kendi markasını görür (K9) → kayıt panele hemen yansır.
    markaUygula({ ...g.marka, anahtar: g.anahtar })
  }

  async function kaydet() {
    setKaydediliyor(true)
    try {
      const govde: Record<string, unknown> = { ...degisen }
      if (!kok) govde.surum = veri.surum
      if (!kok && !veri.kayitli && Object.keys(degisen).length === 0) govde.panel_adi = taslak.panel_adi
      const r = await api.put<YonetGorunum>('/marka/yonet', govde)
      yansit(r.data)
      toast.basari(cevir('Marka kaydedildi'))
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setKaydediliyor(false)
    }
  }

  async function sifirla() {
    const mesaj = kok
      ? cevir('Tüm metinler ve görseller silinir; panel varsayılan markaya döner.')
      : cevir('Tüm metinler ve görseller silinir; müşterileriniz yeniden varsayılan markayı görür.')
    if (!(await onay({ baslik: cevir('Marka sıfırlansın mı?'), mesaj, ton: 'tehlike', onayIkonu: I.sifirla, onayEtiketi: cevir('Sıfırla') }))) return
    try {
      const r = await api.post<YonetGorunum>('/marka/yonet/sifirla')
      onDegisti(r.data)
      void markaTazele()
      toast.basari(cevir('Kaydedildi'))
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    }
  }

  const gm = { ...taslak, anahtar: veri.anahtar }
  const logo = veri.marka.logo ? gorselAdres(gm, 'logo', veri.marka.logo_surum) : null
  const favicon = veri.marka.favicon ? gorselAdres(gm, 'favicon', veri.marka.favicon_surum) : null
  const banner = veri.marka.banner ? gorselAdres(gm, 'banner', veri.marka.banner_surum) : null
  const alan = (k: Metin, etiket: string, ek: Partial<React.ComponentProps<typeof Input>> = {}) => (
    <Input label={etiket} value={(taslak[k] as string) ?? ''} onChange={(e) => ayarla(k, e.target.value)} {...ek} />
  )

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
      <div className="min-w-0 space-y-4">
        {!kok && veri.kok_kapatti && (
          <UyariKutusu ton="uyari">{cevir('Markanız sunucu yöneticisi tarafından geçici olarak kapatıldı; müşterileriniz varsayılan markayı görüyor.')}</UyariKutusu>
        )}
        {!kok && !veri.kayitli && !veri.kok_kapatti && (
          <UyariKutusu ton="bilgi">{cevir('Henüz kendi markanızı kaydetmediniz; müşterileriniz şu an sağlayıcının markasını görüyor.')}</UyariKutusu>
        )}

        <Card skin="bordered" className="p-4">
          <h3 className="mb-3 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Kimlik')}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            {alan('panel_adi', cevir('Panel adı'), { maxLength: 40, required: true })}
            {alan('baslik', cevir('Sekme başlığı'), { maxLength: 60 })}
            {alan('kisa_ad', cevir('Kısa ad'), { maxLength: 12 })}
            <div>
              <label className="input-label" htmlFor="marka-renk">{cevir('Vurgu rengi')}</label>
              <div className="mt-1.5 flex items-center gap-2">
                <input id="marka-renk-secici" type="color" aria-label={cevir('Vurgu rengi')} value={HEX.test(taslak.vurgu_renk) ? taslak.vurgu_renk : '#2563eb'}
                  onChange={(e) => ayarla('vurgu_renk', e.target.value)} className="h-9 w-12 shrink-0 cursor-pointer rounded border border-gray-300 bg-transparent dark:border-dark-450" />
                <Input id="marka-renk" value={taslak.vurgu_renk} maxLength={7} onChange={(e) => ayarla('vurgu_renk', e.target.value)}
                  error={!HEX.test(taslak.vurgu_renk)} classNames={{ root: 'flex-1', input: 'font-mono' }} />
              </div>
            </div>
          </div>
          {HEX.test(taslak.vurgu_renk) && kontrastBeyaz(taslak.vurgu_renk) < 3 && (
            <UyariKutusu ton="uyari" kucuk className="mt-3">{cevir('Kontrast düşük: bu renk beyaz yazıyla okunaksız olabilir.')}</UyariKutusu>
          )}
        </Card>

        <Card skin="bordered" className="p-4">
          <h3 className="mb-3 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Görseller')}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <GorselYuvasi ad="logo" etiket={cevir('Logo')} url={logo} onDegisti={yansit} />
            <GorselYuvasi ad="favicon" etiket={cevir('Sekme simgesi')} url={favicon} onDegisti={yansit} />
            <GorselYuvasi ad="banner" etiket={cevir('Giriş bandı')} url={banner} onDegisti={yansit} />
          </div>
          <p className="mt-2 text-xs text-gray-400 dark:text-dark-300">{cevir('PNG, JPEG, WEBP, GIF veya ICO · en fazla 512 KB · SVG kabul edilmez')}</p>
        </Card>

        <Card skin="bordered" className="p-4">
          <h3 className="mb-3 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Giriş sayfası')}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            {alan('giris_baslik', cevir('Karşılama başlığı'), { maxLength: 60 })}
            {alan('giris_alt', cevir('Alt başlık'), { maxLength: 120 })}
            {alan('giris_dipnot', cevir('Alt not'), { maxLength: 120 })}
            {alan('banner_metin', cevir('Bant metni'), { maxLength: 160 })}
            {alan('banner_url', cevir('Bant bağlantısı'), { maxLength: 200, type: 'url', placeholder: 'https://' })}
          </div>
          <label className="mt-3 flex items-center gap-2 text-sm text-gray-700 dark:text-dark-100">
            <input type="checkbox" className="size-4 accent-brand-600" checked={taslak.surum_goster}
              onChange={(e) => ayarla('surum_goster', e.target.checked)} />
            {cevir('Sürüm satırını göster')}
          </label>
        </Card>

        <Card skin="bordered" className="p-4">
          <h3 className="mb-3 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Destek iletişimi')}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            {alan('destek_eposta', cevir('Destek e-postası'), { maxLength: 254, type: 'email', placeholder: 'destek@ornek.com' })}
            {alan('destek_telefon', cevir('Destek telefonu'), { maxLength: 20, type: 'tel', placeholder: '+90 212 000 00 00' })}
            {alan('destek_url', cevir('Destek adresi (web)'), { maxLength: 200, type: 'url', placeholder: 'https://' })}
            {alan('footer', cevir('Panel alt bilgisi'), { maxLength: 200 })}
          </div>
          {kok && (
            <div className="mt-3">
              <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-100">
                <input type="checkbox" className="size-4 accent-brand-600" checked={!!taslak.webmail_marka}
                  onChange={(e) => ayarla('webmail_marka', e.target.checked)} />
                {cevir('Webmail de markalansın')}
              </label>
              <p className="mt-1 text-xs text-gray-400 dark:text-dark-300">{cevir('Roundcube webmail kök markasını kullanır (bayiye göre webmail markası v1 kapsamında değil).')}</p>
            </div>
          )}
        </Card>

        <div className="flex flex-wrap items-center gap-2">
          <Button color="primary" ikon={I.disket} yukleniyor={kaydediliyor} disabled={!kirli || !HEX.test(taslak.vurgu_renk) || !taslak.panel_adi.trim()} onClick={kaydet}>
            {cevir('Kaydet')}
          </Button>
          {(kok || veri.kayitli || veri.marka.logo || veri.marka.favicon || veri.marka.banner) && (
            <Button variant="outlined" color="error" ikon={I.sifirla} onClick={sifirla}>{cevir('Varsayılana dön')}</Button>
          )}
        </div>
      </div>

      <div className="min-w-0 lg:sticky lg:top-20 lg:self-start">
        <h3 className="mb-2 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Önizleme')}</h3>
        <GirisOnizleme m={taslak} logo={logo} banner={banner} />
      </div>
    </div>
  )
}
