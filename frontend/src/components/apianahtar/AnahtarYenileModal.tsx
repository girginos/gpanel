// Anahtar döndürme: kimlik yeniden doğrulaması + eski anahtarın geçiş süresi.
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import Modal from '@/components/Modal'
import { Button, Input, Select } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { apiHata } from '@/lib/api'
import { anahtarYenile, hataKodu, type ApiAnahtari, type TekKezYanit } from '@/lib/apiAnahtar'
import { cevir, cevirT } from './ceviri'

export default function AnahtarYenileModal({ hedef, onKapat, onYenilendi }: {
  hedef: ApiAnahtari | null
  onKapat: () => void
  onYenilendi: (y: TekKezYanit) => void
}) {
  const [parola, setParola] = useState('')
  const [kod, setKod] = useState('')
  const [kodGerekli, setKodGerekli] = useState(false)
  const [gecis, setGecis] = useState(0)
  const [hata, setHata] = useState<string | null>(null)
  const [gonderiliyor, setGonderiliyor] = useState(false)

  useEffect(() => { setParola(''); setKod(''); setKodGerekli(false); setGecis(0); setHata(null) }, [hedef])

  const gonder = async (ev: FormEvent) => {
    ev.preventDefault()
    if (!hedef) return
    setHata(null)
    if (!parola) return setHata(cevir('Parolanızı girin.'))
    setGonderiliyor(true)
    try {
      const y = await anahtarYenile(hedef.id, parola, kod.trim(), gecis)
      setParola(''); setKod('')
      onYenilendi(y)
    } catch (e) {
      if (hataKodu(e) === 'iki_fa_gerekli') {
        setKodGerekli(true)
        setHata(cevir('İki aşamalı doğrulama kodunu girin.'))
      } else {
        setHata(apiHata(e, cevir('Anahtar döndürülemedi')))
      }
    } finally {
      setGonderiliyor(false)
    }
  }

  return (
    <Modal
      acik={!!hedef}
      baslik={cevir('Anahtarı döndür')}
      ton="soru"
      onKapat={onKapat}
      kapatilamaz={gonderiliyor}
      alt={<>
        <Button type="button" variant="outlined" onClick={onKapat} disabled={gonderiliyor}>{cevir('Vazgeç')}</Button>
        <Button type="submit" form="api-anahtar-yenile" color="primary" ikon={I.yenile} yukleniyor={gonderiliyor}>{cevir('Yeni anahtar üret')}</Button>
      </>}
    >
      <form id="api-anahtar-yenile" onSubmit={gonder} className="space-y-4">
        <p className="text-sm text-slate-600 dark:text-dark-200">
          {cevirT('"{0}" için aynı izinlerle yeni bir anahtar üretilir. Eski anahtar seçtiğiniz geçiş süresi sonunda geçersiz olur.', hedef?.ad || '')}
        </p>
        <Select
          label={cevir('Eski anahtarın geçiş süresi')}
          value={gecis}
          onChange={(e) => setGecis(parseInt(e.target.value, 10))}
          data={[
            { value: 0, label: cevir('Hemen geçersiz olsun') },
            { value: 1, label: cevirT('{0} saat', 1) },
            { value: 24, label: cevirT('{0} saat', 24) },
            { value: 72, label: cevirT('{0} saat', 72) },
          ]}
        />
        <Input label={cevir('Parolanız')} type="password" autoComplete="current-password" value={parola} onChange={(e) => setParola(e.target.value)} />
        {kodGerekli && (
          <Input label={cevir('2FA kodu')} inputMode="numeric" autoComplete="one-time-code" maxLength={8} value={kod} onChange={(e) => setKod(e.target.value)} />
        )}
        {hata && <UyariKutusu ton="tehlike" kucuk>{hata}</UyariKutusu>}
      </form>
    </Modal>
  )
}
