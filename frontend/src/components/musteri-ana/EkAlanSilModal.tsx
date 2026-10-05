// Ek alan adı silme onayı — alan adı yazdırılarak (geri alınamaz). Ek alan detay sayfası kaldırıldı;
// silme artık hesap sayfasındaki "Alan adları" listesinden yapılır.
// Uç: DELETE /domains/{id}/ek-alan-adlari/{eid}?onay=<alan adı>
import { useEffect, useState } from 'react'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import Modal from '@/components/Modal'
import { Input } from '@/components/ui'
import { I } from '@/components/Ikon'
import { useToast } from '@/components/Toast'
import { Dugme } from './ortak'
import { cevir, cevirF } from './dil'

export default function EkAlanSilModal({ domainId, ekId, alanAdi, onKapat, onSilindi }: {
  domainId: number
  ekId: number
  alanAdi: string
  onKapat: () => void
  onSilindi: () => void
}) {
  const toast = useToast()
  const [yazi, setYazi] = useState('')
  const [siliniyor, setSiliniyor] = useState(false)
  useEffect(() => { setYazi('') }, [ekId])

  async function sil() {
    setSiliniyor(true)
    try {
      await api.delete(`/domains/${domainId}/ek-alan-adlari/${ekId}?onay=${encodeURIComponent(alanAdi)}`, UZUN_ISTEK)
      toast.basari(cevir('Alan adı silindi'), alanAdi)
      onSilindi()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e, cevir('Silinemedi')))
      setSiliniyor(false)
    }
  }

  return (
    <Modal
      acik
      baslik={cevir('Alan adı silinsin mi?')}
      ton="tehlike"
      genislik="sm"
      onKapat={onKapat}
      kapatilamaz={siliniyor}
      alt={
        <>
          <Dugme variant="outlined" ikon={I.kapat} onClick={onKapat} disabled={siliniyor}>{cevir('Vazgeç')}</Dugme>
          <Dugme color="error" ikon={I.cop} onClick={() => void sil()} disabled={yazi.trim() !== alanAdi} yukleniyor={siliniyor}>{cevir('Sil')}</Dugme>
        </>
      }
    >
      <p className="text-sm leading-relaxed text-gray-600 dark:text-dark-200">{cevirF('{0} silinir: web dizini (docroot) ve sertifikası kalıcı olarak silinir. Bu işlem geri alınamaz.', alanAdi)}</p>
      <div className="mt-3">
        <Input label={cevirF('Onaylamak için alan adını yazın: {0}', alanAdi)} value={yazi} onChange={(e) => setYazi(e.target.value)} autoComplete="off" spellCheck={false} classNames={{ input: 'font-mono' }} />
      </div>
    </Modal>
  )
}
