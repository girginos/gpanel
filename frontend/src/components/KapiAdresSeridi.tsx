import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { cevirT } from '@/lib/cevirT'

// Geçiş (eski) kapısından giriş yapan kullanıcıya KENDİ rolünün yeni adresini söyler
// (ADR-GP-001). Sunucu adresi yalnız eski kapıda ve yalnız kimliği doğrulanmış
// kullanıcıya verir — anonim ziyaretçiye yeni port sızdırılmaz. Başka her durumda
// yanıt boştur ve şerit çizilmez.

const EN: Record<string, string> = {
  'Panel adresiniz değişti.': 'Your panel address has changed.',
  'Yeni adresiniz:': 'Your new address:',
  'Bu adres {0} tarihinden sonra kapanabilir — yer imlerinizi güncelleyin.': 'This address may close after {0} — update your bookmarks.',
  'Yeni adreste yeniden giriş yapmanız gerekir.': 'You will need to sign in again at the new address.',
  'Yeni adrese git': 'Go to the new address',
}

type Yanit = { adres?: string; bitis?: number }

export default function KapiAdresSeridi() {
  const { i18n } = useTranslation()
  const cevir = (tr: string) => (i18n.language === 'en' ? EN[tr] || tr : tr)
  const [yanit, setYanit] = useState<Yanit>({})

  useEffect(() => {
    let iptal = false
    api.get<Yanit>('/kapi/adres')
      .then((r) => { if (!iptal) setYanit(r.data || {}) })
      .catch(() => { /* şerit bilgilendirmedir; yoksa çizilmez */ })
    return () => { iptal = true }
  }, [])

  if (!yanit.adres) return null
  const tarih = yanit.bitis
    ? new Date(yanit.bitis * 1000).toLocaleDateString(i18n.language === 'en' ? 'en-GB' : 'tr-TR')
    : ''

  return (
    <div role="status" className="border-b border-amber-300/70 bg-amber-50 px-4 py-2.5 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <span className="font-semibold">{cevir('Panel adresiniz değişti.')}</span>
        <span>
          {cevir('Yeni adresiniz:')}{' '}
          <a href={yanit.adres} className="break-all font-mono font-medium underline underline-offset-2">{yanit.adres}</a>
        </span>
        <span className="text-xs text-amber-800/80 dark:text-amber-300/80">
          {tarih ? cevirT(cevir('Bu adres {0} tarihinden sonra kapanabilir — yer imlerinizi güncelleyin.'), tarih) + ' ' : ''}
          {cevir('Yeni adreste yeniden giriş yapmanız gerekir.')}
        </span>
        <a
          href={yanit.adres}
          className="ml-auto shrink-0 rounded-md bg-amber-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-amber-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-amber-600"
        >
          {cevir('Yeni adrese git')}
        </a>
      </div>
    </div>
  )
}
