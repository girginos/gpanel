// Tam anahtarın TEK gösterimi. "Kopyaladım" işaretlenmeden kapanmaz. Anahtar yalnız
// bu bileşenin prop'unda yaşar; kapatılınca çağıran onu durumdan siler.
import { useEffect, useState } from 'react'
import Modal from '@/components/Modal'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { cevir } from './ceviri'

export default function AnahtarGosterModal({ anahtar, ad, onKapat }: {
  anahtar: string | null
  ad: string
  onKapat: () => void
}) {
  const [onay, setOnay] = useState(false)
  const [kopyalandi, setKopyalandi] = useState(false)
  const toast = useToast()

  useEffect(() => { setOnay(false); setKopyalandi(false) }, [anahtar])

  const kopyala = async () => {
    if (!anahtar) return
    try {
      await navigator.clipboard.writeText(anahtar)
      setKopyalandi(true)
    } catch {
      toast.hata(cevir('Hata oluştu'), cevir('Panoya kopyalanamadı; anahtarı elle seçip kopyalayın.'))
    }
  }

  return (
    <Modal
      acik={!!anahtar}
      baslik={cevir('Anahtarınız hazır')}
      ton="uyari"
      genislik="lg"
      kapatilamaz
      onKapat={onKapat}
      alt={<Button color="primary" ikon={I.onay} disabled={!onay} onClick={onKapat}>{cevir('Kapat')}</Button>}
    >
      <div className="space-y-4">
        <UyariKutusu ton="uyari" kucuk>
          {cevir('Bu anahtar bir daha gösterilmeyecek. Şimdi kopyalayıp güvenli bir yerde (parola yöneticisi, faturalama sisteminin ayarları) saklayın.')}
        </UyariKutusu>
        <div>
          <div className="mb-1 text-xs font-medium text-slate-500 dark:text-dark-300">{ad}</div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-stretch">
            <code
              className="block min-w-0 flex-1 select-all break-all rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-sm text-slate-900 dark:border-dark-600 dark:bg-dark-900 dark:text-dark-50"
              data-testid="api-anahtar-deger"
            >
              {anahtar}
            </code>
            <Button variant="outlined" ikon={kopyalandi ? I.onay : I.kopya} onClick={kopyala}>
              {kopyalandi ? cevir('Kopyalandı') : cevir('Kopyala')}
            </Button>
          </div>
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm text-slate-700 dark:text-dark-100">
          <input type="checkbox" checked={onay} onChange={(e) => setOnay(e.target.checked)} className="size-4 rounded border-slate-300" />
          {cevir('Anahtarı güvenli bir yere kopyaladım')}
        </label>
      </div>
    </Modal>
  )
}
