// Bayi: özel panel adresi (panel.bayi.com). Varsayılan kapalı; kök bayi başına açar.
import { useCallback, useEffect, useState } from 'react'
import axios from 'axios'
import { api, apiHata } from '@/lib/api'
import { Badge, Button, Card, Input } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { ALAN_DURUM, useMarkaCeviri } from './ceviri'

type Kayit = { alan_adi: string; durum: string; txt_ad: string; txt_deger: string; son_hata: string; ssl_bitis: string }
type Yanit = { kayit: Kayit | null; sunucu_ip: string[]; hata?: string }

const DURUM = ALAN_DURUM

export function OzelAdres() {
  const cevir = useMarkaCeviri()
  const toast = useToast()
  const { onay } = useDialog()
  const [veri, setVeri] = useState<Yanit | null>(null)
  const [izinYok, setIzinYok] = useState(false)
  const [ad, setAd] = useState('')
  const [mesgul, setMesgul] = useState(false)

  const yukle = useCallback(() => {
    api.get<Yanit>('/marka/alan-adi')
      .then((r) => { setVeri(r.data); setIzinYok(false) })
      .catch((e) => { if (axios.isAxiosError(e) && e.response?.status === 403) setIzinYok(true); else toast.hata(cevir('Kaydedilemedi'), apiHata(e)) })
  }, [toast, cevir])
  useEffect(() => { yukle() }, [yukle])
  // Sertifika sunucuda arka planda alınır: beklerken durumu kendiliğinden yenile.
  const bekliyor = veri?.kayit?.durum === 'ssl_bekliyor'
  useEffect(() => {
    if (!bekliyor) return
    const id = window.setInterval(() => {
      api.get<Yanit>('/marka/alan-adi').then((r) => setVeri(r.data)).catch(() => {})
    }, 15000)
    return () => window.clearInterval(id)
  }, [bekliyor])

  async function ekle() {
    setMesgul(true)
    try {
      const r = await api.post<Yanit>('/marka/alan-adi', { alan_adi: ad.trim() })
      setVeri(r.data)
      setAd('')
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(false)
    }
  }

  async function dogrula() {
    setMesgul(true)
    try {
      const r = await api.post<Yanit>('/marka/alan-adi/dogrula')
      setVeri(r.data)
      toast.basari(cevir('Doğrulandı'))
    } catch (e) {
      if (axios.isAxiosError(e) && e.response?.status === 422 && e.response.data?.kayit) {
        setVeri(e.response.data as Yanit)
        toast.uyari(cevir('Doğrulanamadı'), (e.response.data as Yanit).hata)
      } else {
        toast.hata(cevir('Doğrulanamadı'), apiHata(e))
      }
    } finally {
      setMesgul(false)
    }
  }

  async function kaldir() {
    if (!(await onay({ baslik: cevir('Adres kaldırılsın mı?'), mesaj: cevir('Müşterileriniz bu adresten artık giriş yapamaz.'), ton: 'tehlike', onayIkonu: I.cop, onayEtiketi: cevir('Kaldır') }))) return
    try {
      await api.delete('/marka/alan-adi')
      toast.basari(cevir('Kaldırıldı'))
      yukle()
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    }
  }

  if (izinYok) return <UyariKutusu ton="bilgi">{cevir('Özel panel adresi bu hesap için açık değil. Hizmet sağlayıcınızla iletişime geçin.')}</UyariKutusu>
  if (!veri) return <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>
  const k = veri.kayit
  const d = k ? DURUM[k.durum] || DURUM.hata : null

  return (
    <Card skin="bordered" className="space-y-4 p-4">
      <p className="text-sm text-gray-600 dark:text-dark-200">{cevir('Panelinize kendi alan adınızla ulaşın (ör. panel.ornek.com). Müşterileriniz bu adreste giriş sayfasında sizin markanızı görür.')}</p>
      {!k ? (
        <form className="flex flex-col gap-2 sm:flex-row sm:items-end" onSubmit={(e) => { e.preventDefault(); void ekle() }}>
          <Input label={cevir('Alan adı')} value={ad} onChange={(e) => setAd(e.target.value)} placeholder="panel.ornek.com" maxLength={253} classNames={{ root: 'flex-1' }} />
          <Button type="submit" color="primary" ikon={I.arti} yukleniyor={mesgul} disabled={!ad.trim()}>{cevir('Ekle')}</Button>
        </form>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <span className="break-all font-mono text-sm font-medium text-gray-800 dark:text-dark-50">{k.alan_adi}</span>
            {d && <Badge color={d.renk} variant="soft">{cevir(d.etiket)}</Badge>}
          </div>
          {k.son_hata && <UyariKutusu ton="uyari" kucuk>{k.son_hata}</UyariKutusu>}
          {k.durum === 'ssl_bekliyor' && <UyariKutusu ton="bilgi" kucuk>{cevir('DNS doğrulandı. Sertifika birkaç dakika içinde otomatik alınır ve adres etkinleşir; bu sayfa kendiliğinden yenilenir.')}</UyariKutusu>}
          {k.durum === 'aktif' && (
            <UyariKutusu ton="basari" kucuk>
              {cevir('Adres etkin.')}
              {k.ssl_bitis && <> {cevir('Sertifika geçerlilik sonu')}: <span className="font-mono">{k.ssl_bitis}</span> ({cevir('otomatik yenilenir')})</>}
            </UyariKutusu>
          )}
          {k.durum === 'hata' && <p className="text-xs text-gray-500 dark:text-dark-300">{cevir('DNS kayıtlarınızı düzelttiyseniz Doğrula’ya basın; sunucu yeniden dener.')}</p>}
          {k.durum === 'dns_bekliyor' && (
            <div>
              <h4 className="text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('DNS kayıtları')}</h4>
              <p className="mt-1 text-xs text-gray-500 dark:text-dark-300">{cevir('Alan adınızın DNS yönetiminde şu iki kaydı oluşturun, sonra Doğrula’ya basın:')}</p>
              <div className="mt-2 space-y-2 text-xs">
                {(veri.sunucu_ip || []).filter(Boolean).map((ip) => (
                  <div key={ip} className="grid grid-cols-[3rem_minmax(0,1fr)] gap-x-2 gap-y-0.5 rounded border border-gray-200 p-2 dark:border-dark-500">
                    <span className="text-gray-400">{cevir('Tür')}</span><span className="font-mono">A</span>
                    <span className="text-gray-400">{cevir('Ad')}</span><span className="break-all font-mono">{k.alan_adi}</span>
                    <span className="text-gray-400">{cevir('Değer')}</span><span className="break-all font-mono">{ip}</span>
                  </div>
                ))}
                <div className="grid grid-cols-[3rem_minmax(0,1fr)] gap-x-2 gap-y-0.5 rounded border border-gray-200 p-2 dark:border-dark-500">
                  <span className="text-gray-400">{cevir('Tür')}</span><span className="font-mono">TXT</span>
                  <span className="text-gray-400">{cevir('Ad')}</span><span className="break-all font-mono">{k.txt_ad}</span>
                  <span className="text-gray-400">{cevir('Değer')}</span><span className="break-all font-mono">{k.txt_deger}</span>
                </div>
              </div>
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            {k.durum !== 'aktif' && <Button color="primary" ikon={I.onay} yukleniyor={mesgul} onClick={dogrula}>{cevir('Doğrula')}</Button>}
            <Button variant="outlined" color="error" ikon={I.cop} disabled={mesgul} onClick={kaldir}>{cevir('Kaldır')}</Button>
          </div>
        </>
      )}
    </Card>
  )
}
