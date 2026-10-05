// Kök: bayi markalarını görme + izin / özel adres izni / moderasyon (AdminOnly uçlar).
import { useCallback, useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import { gorselAdres, VARSAYILAN } from '@/lib/marka'
import { Badge, Button, Card, Select, Toggle } from '@/components/ui'
import { I } from '@/components/Ikon'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { ALAN_DURUM, useMarkaCeviri } from './ceviri'

type Satir = {
  id: number
  kullanici: string
  durum: string
  anahtar: string
  kayitli: boolean
  panel_adi: string
  vurgu_renk: string
  logo: boolean
  logo_surum: number
  izin: boolean
  alanadi_izni: boolean
  kok_kapatti: boolean
  alan_adi: string
  alan_durum: string
  alan_hata: string
  alan_ayrinti: string
  alan_ssl_bitis: string
}

const HEX = /^#[0-9a-fA-F]{6}$/

export function BayiMarkalari() {
  const cevir = useMarkaCeviri()
  const toast = useToast()
  const { onay } = useDialog()
  const [liste, setListe] = useState<Satir[] | null>(null)
  const [yedek, setYedek] = useState('kok')
  const [mesgul, setMesgul] = useState<number | null>(null)

  const yukle = useCallback(() => {
    api.get<{ bayiler: Satir[]; yedek: string }>('/marka/bayiler')
      .then((r) => { setListe(r.data.bayiler || []); setYedek(r.data.yedek || 'kok') })
      .catch((e) => { setListe([]); toast.hata(cevir('Kaydedilemedi'), apiHata(e)) })
  }, [toast, cevir])
  useEffect(() => { yukle() }, [yukle])

  async function izin(s: Satir, govde: Partial<Pick<Satir, 'izin' | 'alanadi_izni' | 'kok_kapatti'>>) {
    setMesgul(s.id)
    try {
      await api.put(`/marka/bayiler/${s.id}`, govde)
      toast.basari(cevir('Kaydedildi'))
      yukle()
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(null)
    }
  }

  async function sifirla(s: Satir) {
    if (!(await onay({ baslik: cevir('Bayinin markası sıfırlansın mı?'), mesaj: cevir('Bayinin metinleri ve görselleri silinir; izinleri korunur.'), ton: 'tehlike', onayIkonu: I.sifirla, onayEtiketi: cevir('Sıfırla') }))) return
    setMesgul(s.id)
    try {
      await api.post(`/marka/bayiler/${s.id}/sifirla`)
      toast.basari(cevir('Kaydedildi'))
      yukle()
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(null)
    }
  }

  async function adresKaldir(s: Satir) {
    if (!(await onay({ baslik: cevir('Özel adres kaldırılsın mı?'), mesaj: s.alan_adi, ton: 'tehlike', onayIkonu: I.cop, onayEtiketi: cevir('Kaldır') }))) return
    setMesgul(s.id)
    try {
      await api.delete(`/marka/bayiler/${s.id}/alan-adi`)
      toast.basari(cevir('Kaldırıldı'))
      yukle()
    } catch (e) {
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    } finally {
      setMesgul(null)
    }
  }

  async function yedekKaydet(v: string) {
    const eski = yedek
    setYedek(v)
    try {
      await api.put('/marka/ayar', { bayi_yedek: v })
      toast.basari(cevir('Kaydedildi'))
    } catch (e) {
      setYedek(eski)
      toast.hata(cevir('Kaydedilemedi'), apiHata(e))
    }
  }

  if (liste === null) return <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Yükleniyor…')}</p>

  return (
    <div className="space-y-4">
      <Card skin="bordered" className="p-4">
        <Select label={cevir('Bayi markası yoksa müşterisi')} value={yedek} onChange={(e) => void yedekKaydet(e.target.value)}
          data={[{ value: 'kok', label: cevir('Sistem varsayılanı (kök marka)') }, { value: 'notr', label: cevir('nötr markayı görür') }]}
          classNames={{ root: 'max-w-sm' }} />
      </Card>
      {liste.length === 0 ? (
        <p className="text-sm text-gray-500 dark:text-dark-300">{cevir('Henüz bayi yok.')}</p>
      ) : (
        <Card skin="bordered" className="overflow-hidden">
          <ul className="divide-y divide-gray-200 dark:divide-dark-500">
            {liste.map((s) => {
              const renk = HEX.test(s.vurgu_renk) ? s.vurgu_renk : '#94a3b8'
              const logo = s.logo ? gorselAdres({ ...VARSAYILAN, anahtar: s.anahtar }, 'logo', s.logo_surum) : null
              const isleniyor = mesgul === s.id
              const anahtarlar: { etiket: string; acik: boolean; govde: Partial<Pick<Satir, 'izin' | 'alanadi_izni' | 'kok_kapatti'>> }[] = [
                { etiket: cevir('Marka yönetimi'), acik: s.izin, govde: { izin: !s.izin } },
                { etiket: cevir('Özel adres izni'), acik: s.alanadi_izni, govde: { alanadi_izni: !s.alanadi_izni } },
                { etiket: cevir('Bayi markası'), acik: !s.kok_kapatti, govde: { kok_kapatti: !s.kok_kapatti } },
              ]
              return (
                <li key={s.id} className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
                    <div className="flex min-w-0 flex-1 basis-56 items-center gap-3">
                      <div className="flex h-10 w-14 shrink-0 items-center justify-center overflow-hidden rounded bg-gray-100 dark:bg-dark-600">
                        {logo ? <img src={logo} alt="" className="max-h-10 max-w-14 object-contain" /> : <span className="size-4 rounded-full" style={{ backgroundColor: renk }} />}
                      </div>
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-gray-800 dark:text-dark-50">{s.kullanici}</p>
                        <p className="truncate text-xs text-gray-500 dark:text-dark-300">{s.kayitli ? s.panel_adi : cevir('Sistem varsayılanı')}</p>
                        <div className="mt-1 flex flex-wrap gap-1">
                          {s.kayitli && <Badge color="success" variant="soft">{cevir('Kayıtlı')}</Badge>}
                          {s.alan_adi && (
                            <Badge color={ALAN_DURUM[s.alan_durum]?.renk || 'neutral'} variant="soft" className="max-w-full truncate" title={s.alan_ssl_bitis ? `${cevir('Sertifika geçerlilik sonu')}: ${s.alan_ssl_bitis}` : undefined}>
                              {s.alan_adi} · {cevir(ALAN_DURUM[s.alan_durum]?.etiket || s.alan_durum)}
                            </Badge>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
                      {anahtarlar.map((a) => (
                        <label key={a.etiket} className="inline-flex items-center gap-2 text-xs text-gray-700 dark:text-dark-100">
                          <Toggle boyut="sm" acik={a.acik} disabled={isleniyor} aria-label={a.etiket} onDegis={() => void izin(s, a.govde)} />
                          {a.etiket}
                        </label>
                      ))}
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      {s.kayitli && (
                        <Button boyut="sm" variant="flat" color="error" ikon={I.sifirla} disabled={isleniyor} onClick={() => sifirla(s)}>{cevir('Markayı sıfırla')}</Button>
                      )}
                      {s.alan_adi && (
                        <Button boyut="sm" variant="flat" color="error" ikon={I.cop} disabled={isleniyor} onClick={() => adresKaldir(s)}>{cevir('Adresi kaldır')}</Button>
                      )}
                    </div>
                  </div>
                  {s.alan_hata && <p className="mt-2 break-words text-xs text-error dark:text-error-light">{s.alan_hata}</p>}
                  {s.alan_ayrinti && (
                    <details className="mt-1 text-xs text-gray-500 dark:text-dark-300">
                      <summary className="cursor-pointer">{cevir('Teknik ayrıntı')}</summary>
                      <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-words font-mono">{s.alan_ayrinti}</pre>
                    </details>
                  )}
                </li>
              )
            })}
          </ul>
        </Card>
      )}
    </div>
  )
}
