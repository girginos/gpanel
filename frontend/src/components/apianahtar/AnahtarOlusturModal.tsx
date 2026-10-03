// Yeni API anahtarı diyaloğu: ad, izin ızgarası (katalog sunucudan), IP listesi
// (kökte zorunlu), geçerlilik, hız sınırı ve kimlik yeniden doğrulaması (parola + 2FA).
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import Modal from '@/components/Modal'
import { Button, Input, Select } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { apiHata } from '@/lib/api'
import { anahtarOlustur, hataKodu, type Katalog, type TekKezYanit } from '@/lib/apiAnahtar'
import { cevir, cevirT } from './ceviri'

type Erisim = '' | 'oku' | 'yaz'

const GUNLER = [30, 90, 180, 365, 730]

export default function AnahtarOlusturModal({ acik, katalog, onKapat, onOlusturuldu }: {
  acik: boolean
  katalog: Katalog | null
  onKapat: () => void
  onOlusturuldu: (y: TekKezYanit) => void
}) {
  const [ad, setAd] = useState('')
  const [izin, setIzin] = useState<Record<string, Erisim>>({})
  const [ipMetni, setIpMetni] = useState('')
  const [gun, setGun] = useState(365)
  const [hiz, setHiz] = useState('120')
  const [parola, setParola] = useState('')
  const [kod, setKod] = useState('')
  const [kodGerekli, setKodGerekli] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [gonderiliyor, setGonderiliyor] = useState(false)

  useEffect(() => {
    if (!acik) return
    setAd(''); setIzin({}); setIpMetni(''); setParola(''); setKod(''); setKodGerekli(false); setHata(null)
    setGun(katalog?.varsayilan_gun || 365); setHiz(String(katalog?.varsayilan_hiz || 120))
  }, [acik, katalog])

  const ipZorunlu = !!katalog?.ip_zorunlu
  const gunler = GUNLER.filter((g) => !katalog || g <= katalog.maks_gun)

  const gonder = async (ev: FormEvent) => {
    ev.preventDefault()
    setHata(null)
    const izinler = Object.entries(izin).filter(([, e]) => e).map(([g, e]) => `${g}:${e}`)
    const ipler = ipMetni.split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean)
    if (!ad.trim()) return setHata(cevir('Anahtar adı zorunlu.'))
    if (izinler.length === 0) return setHata(cevir('En az bir izin seçin.'))
    if (ipZorunlu && ipler.length === 0) return setHata(cevir('Kök anahtarında en az bir izinli IP zorunludur.'))
    if (!parola) return setHata(cevir('Parolanızı girin.'))
    if (kodGerekli && !kod.trim()) return setHata(cevir('İki aşamalı doğrulama kodunu girin.'))
    setGonderiliyor(true)
    try {
      const y = await anahtarOlustur({
        ad: ad.trim(), izinler, izinli_ipler: ipler, son_kullanma_gun: gun,
        hiz_dk: parseInt(hiz, 10) || 0, parola, kod: kod.trim() || undefined,
      })
      setParola(''); setKod('')
      onOlusturuldu(y)
    } catch (e) {
      const k = hataKodu(e)
      if (k === 'iki_fa_gerekli') {
        setKodGerekli(true)
        setHata(cevir('İki aşamalı doğrulama kodunu girin.'))
      } else {
        if (k === 'iki_fa_hatali') setKod('')
        setHata(apiHata(e, cevir('Anahtar oluşturulamadı')))
      }
    } finally {
      setGonderiliyor(false)
    }
  }

  const erisimDugmesi = (grup: string, e: Erisim, etiket: string) => {
    const secili = (izin[grup] || '') === e
    return (
      <button
        key={e || 'yok'}
        type="button"
        onClick={() => setIzin((m) => ({ ...m, [grup]: e }))}
        aria-pressed={secili}
        className={
          'rounded-md px-2.5 py-1 text-xs font-medium transition-colors ' +
          (secili
            ? 'bg-primary-600 text-white dark:bg-primary-500'
            : 'text-slate-600 hover:bg-slate-100 dark:text-dark-200 dark:hover:bg-dark-600')
        }
      >
        {etiket}
      </button>
    )
  }

  return (
    <Modal
      acik={acik}
      baslik={cevir('Yeni API anahtarı')}
      ton="soru"
      genislik="lg"
      onKapat={onKapat}
      kapatilamaz={gonderiliyor}
      alt={<>
        <Button type="button" variant="outlined" onClick={onKapat} disabled={gonderiliyor}>{cevir('Vazgeç')}</Button>
        <Button type="submit" form="api-anahtar-olustur" color="primary" ikon={I.anahtar} yukleniyor={gonderiliyor}>{cevir('Oluştur')}</Button>
      </>}
    >
      <form id="api-anahtar-olustur" onSubmit={gonder} className="space-y-5">
        <Input label={cevir('Anahtar adı')} value={ad} maxLength={64} placeholder={cevir('ör. WHMCS üretim')} onChange={(e) => setAd(e.target.value)} />

        <fieldset>
          <legend className="mb-2 text-sm font-medium text-slate-700 dark:text-dark-100">{cevir('İzinler')}</legend>
          <div className="divide-y divide-slate-200 rounded-lg border border-slate-200 dark:divide-dark-600 dark:border-dark-600">
            {(katalog?.gruplar || []).map((g) => (
              <div key={g.ad} className="flex flex-col gap-2 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-slate-800 dark:text-dark-50">{cevir(g.etiket)}</div>
                  <div className="text-xs text-slate-500 dark:text-dark-300">{cevir(g.aciklama)}</div>
                </div>
                <div className="flex shrink-0 gap-1 rounded-lg bg-slate-50 p-0.5 dark:bg-dark-700" role="group" aria-label={cevir(g.etiket)}>
                  {erisimDugmesi(g.ad, '', cevir('Yok'))}
                  {erisimDugmesi(g.ad, 'oku', cevir('Okuma'))}
                  {!g.yalniz_oku && erisimDugmesi(g.ad, 'yaz', cevir('Okuma + yazma'))}
                </div>
              </div>
            ))}
          </div>
        </fieldset>

        <div>
          <label htmlFor="api-anahtar-ip" className="mb-1 block text-sm font-medium text-slate-700 dark:text-dark-100">
            {cevir('İzinli IP adresleri')}{ipZorunlu && <span className="text-error"> *</span>}
          </label>
          <textarea
            id="api-anahtar-ip"
            rows={3}
            value={ipMetni}
            onChange={(e) => setIpMetni(e.target.value)}
            placeholder={'203.0.113.10\n198.51.100.0/24'}
            className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm outline-none focus:border-primary-600 dark:border-dark-450 dark:bg-dark-900 dark:text-dark-50"
          />
          <p className="mt-1 text-xs text-slate-500 dark:text-dark-300">
            {cevir('Her satıra bir IP ya da CIDR (ör. 203.0.113.10 ya da 203.0.113.0/24).')}{' '}
            {ipZorunlu ? cevir('Kök anahtarında zorunludur.') : cevir('Boş bırakılırsa her IP\'den kullanılabilir (önerilmez).')}
          </p>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Select
            label={cevir('Geçerlilik süresi')}
            value={gun}
            onChange={(e) => setGun(parseInt(e.target.value, 10))}
            data={gunler.map((g) => ({ value: g, label: cevirT('{0} gün', g) }))}
          />
          <Input
            label={cevir('Hız sınırı (istek/dk)')}
            type="number"
            min={katalog?.min_hiz || 10}
            max={katalog?.maks_hiz || 600}
            value={hiz}
            onChange={(e) => setHiz(e.target.value)}
          />
        </div>

        <div className="rounded-lg border border-slate-200 p-3 dark:border-dark-600">
          <div className="mb-2 text-sm font-medium text-slate-700 dark:text-dark-100">{cevir('Kimliğinizi doğrulayın')}</div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Input label={cevir('Parolanız')} type="password" autoComplete="current-password" value={parola} onChange={(e) => setParola(e.target.value)} />
            {kodGerekli && (
              <Input label={cevir('2FA kodu')} inputMode="numeric" autoComplete="one-time-code" maxLength={8} value={kod} onChange={(e) => setKod(e.target.value)} />
            )}
          </div>
        </div>

        {hata && <UyariKutusu ton="tehlike" kucuk>{hata}</UyariKutusu>}
      </form>
    </Modal>
  )
}
