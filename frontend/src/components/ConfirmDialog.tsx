import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import Modal from './Modal'
import { Button } from './ui'


const CMP_EN: Record<string, string> = {
  "Onayla": "Confirm",
  "İşleniyor…": "Processing…",
  "İptal": "Cancel",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function ConfirmDialog({
  acik, baslik, mesaj, onayMetni = cevir('Onayla'), tehlikeli = false,
  onOnay, onIptal,
}: {
  acik: boolean
  baslik: string
  mesaj: string
  onayMetni?: string
  tehlikeli?: boolean
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
    <Modal acik={acik} baslik={baslik} onKapat={onIptal} genislik="sm">
      <p className="mb-5 text-sm text-gray-600 dark:text-dark-200">{mesaj}</p>
      <div className="flex justify-end gap-2">
        <Button variant="outlined" onClick={onIptal} disabled={yukleniyor} className="px-4 py-2 text-sm">
          {cevir("İptal")}
        </Button>
        <Button color={tehlikeli ? 'error' : 'primary'} onClick={onaylaTetik} disabled={yukleniyor} className="px-4 py-2 text-sm">
          {yukleniyor ? cevir('İşleniyor…') : onayMetni}
        </Button>
      </div>
    </Modal>
  )
}
