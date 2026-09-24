import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import Modal from './Modal'
import { Button } from './ui'
import { I } from './Ikon'
import { TONLAR, type Ton } from './tonlar'


const CMP_EN: Record<string, string> = {
  "Onayla": "Confirm",
  "İşleniyor…": "Processing…",
  "İptal": "Cancel",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function ConfirmDialog({
  acik, baslik, mesaj, onayMetni = cevir('Onayla'), ton = 'soru', onayIkonu = I.onay,
  onOnay, onIptal,
}: {
  acik: boolean
  baslik: string
  mesaj: string
  onayMetni?: string
  // ton — renk + ikon (tonlar.ts); yıkıcı onayda 'tehlike' (+ onayIkonu={I.cop} vb.).
  ton?: Ton
  onayIkonu?: string
  onOnay: () => Promise<void> | void
  onIptal: () => void
}) {
  useTranslation() // dil re-render aboneligi
  const [yukleniyor, setYukleniyor] = useState(false)

  async function onaylaTetik() {
    setYukleniyor(true)
    try { await onOnay() } finally { setYukleniyor(false) }
  }

  return (
    <Modal acik={acik} baslik={baslik} ton={ton} onKapat={onIptal} genislik="sm" kapatilamaz={yukleniyor}
      alt={<>
        <Button variant="outlined" onClick={onIptal} disabled={yukleniyor} ikon={I.kapat}>
          {cevir("İptal")}
        </Button>
        <Button color={TONLAR[ton].buton} onClick={onaylaTetik} yukleniyor={yukleniyor} ikon={onayIkonu}>
          {yukleniyor ? cevir('İşleniyor…') : onayMetni}
        </Button>
      </>}>
      <p className="text-sm leading-relaxed text-gray-600 dark:text-dark-200">{mesaj}</p>
    </Modal>
  )
}
