// Müşteri ADINA oturumda (bayi/yönetici "panele gir") her sayfanın üstünde sabit şerit:
// "X olarak görüntülüyorsunuz · Bayi paneline dön". Hesap askıdaysa rozet gösterir
// (adına-oturum askıdaki hesaba da girer; tam ekran askı sayfasına düşülmez).
import { useEffect, useState } from 'react'
import { adinaBilgi } from '@/store/auth'
import { adinaCik } from '@/lib/adina'
import { api } from '@/lib/api'
import { useBayiCevir } from '@/lib/bayiCeviri'
import { I, Ikon } from '@/components/Ikon'

export default function AdinaSeridi() {
  const t = useBayiCevir()
  const b = adinaBilgi()
  const [askida, setAskida] = useState(false)
  const did = b?.domainID
  useEffect(() => {
    if (!did) return
    let iptal = false
    api.get<{ askida?: boolean }>(`/domains/${did}`)
      .then((r) => { if (!iptal) setAskida(!!r.data?.askida) })
      .catch(() => { /* şerit için kritik değil */ })
    return () => { iptal = true }
  }, [did])
  if (!b) return null
  return (
    <div role="status" aria-live="polite"
      className="border-b border-amber-300 bg-amber-100 text-amber-950 dark:border-amber-500/40 dark:bg-amber-950 dark:text-amber-100">
      <div className="mx-auto flex w-full max-w-sayfa flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-sm sm:px-6">
        <Ikon d={I.goz} className="size-4 shrink-0" />
        <span className="min-w-0 flex-1">
          <b className="font-semibold break-all">{b.alan}</b> {t('olarak görüntülüyorsunuz')}
          {b.aktor && <span className="ms-1 opacity-75">· {b.aktor}</span>}
          {askida && <span className="ms-2 rounded-full bg-amber-200 px-2 py-0.5 text-xs font-semibold text-amber-900 dark:bg-amber-500/25 dark:text-amber-100">{t('Hesap askıda')}</span>}
        </span>
        <button type="button" onClick={adinaCik}
          className="rounded font-semibold underline underline-offset-2 outline-none hover:no-underline focus-visible:ring-2 focus-visible:ring-amber-700 dark:focus-visible:ring-amber-300">
          {t(b.rol === 'admin' ? 'Yönetim paneline dön' : 'Bayi paneline dön')}
        </button>
      </div>
    </div>
  )
}
