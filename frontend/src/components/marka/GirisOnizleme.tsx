// Giriş sayfası önizlemesi — kaydetmeden müşterinin göreceği giriş kartı.
// 🔴 Değerler YALNIZ metin düğümü olarak basılır; renk yalnız #rrggbb ise inline
// stile girer (sunucu da aynı kuralı uygular).
import type { Marka } from '@/lib/marka'
import { useMarkaCeviri } from './ceviri'

const HEX = /^#[0-9a-fA-F]{6}$/

export function GirisOnizleme({ m, logo, banner }: { m: Marka; logo: string | null; banner: string | null }) {
  const cevir = useMarkaCeviri()
  const renk = HEX.test(m.vurgu_renk) ? m.vurgu_renk : '#2563eb'
  return (
    <div className="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-500 dark:bg-dark-800" aria-label={cevir('Giriş sayfası önizlemesi')}>
      <div className="mx-auto w-full max-w-xs rounded-lg bg-white p-5 shadow-sm dark:bg-dark-700">
        <div className="flex flex-col items-center text-center">
          {logo ? (
            <img src={logo} alt="" className="max-h-14 w-auto max-w-[200px] object-contain" />
          ) : (
            <div className="flex h-11 w-11 items-center justify-center rounded-lg text-lg font-bold text-white" style={{ backgroundColor: renk }}>
              {(m.kisa_ad || m.panel_adi || 'P').slice(0, 1)}
            </div>
          )}
          <p className="mt-2 text-xs font-medium" style={{ color: renk }}>{m.panel_adi}</p>
          <p className="mt-1 break-words text-base font-semibold text-gray-800 dark:text-dark-50">{m.giris_baslik || '—'}</p>
          <p className="mt-0.5 break-words text-xs text-gray-400 dark:text-dark-300">{m.giris_alt || cevir('Devam etmek için giriş yapın.')}</p>
        </div>
        {(banner || m.banner_metin) && (
          <div className="mt-3">
            {banner && <img src={banner} alt="" className="max-h-16 w-full rounded object-contain" />}
            {m.banner_metin && <p className="mt-1 break-words text-center text-xs text-gray-600 dark:text-dark-200">{m.banner_metin}</p>}
          </div>
        )}
        <div className="mt-3 space-y-2" aria-hidden>
          <div className="h-8 rounded border border-gray-200 px-2 text-xs leading-8 text-gray-400 dark:border-dark-500 dark:text-dark-300">{cevir('Kullanıcı')}</div>
          <div className="h-8 rounded border border-gray-200 px-2 text-xs leading-8 text-gray-400 dark:border-dark-500 dark:text-dark-300">{cevir('Parola')}</div>
          <div className="h-8 rounded text-center text-xs font-medium leading-8 text-white" style={{ backgroundColor: renk }}>{cevir('Giriş yap')}</div>
        </div>
        {(m.giris_dipnot || m.destek_eposta || m.destek_telefon || m.destek_url) && (
          <div className="mt-3 space-y-0.5 text-center text-[11px] text-gray-400 dark:text-dark-300">
            {m.giris_dipnot && <p className="break-words">{m.giris_dipnot}</p>}
            {(m.destek_eposta || m.destek_telefon) && (
              <p className="break-all">{[m.destek_eposta, m.destek_telefon].filter(Boolean).join(' · ')}</p>
            )}
            {m.destek_url && <p className="break-all">{m.destek_url}</p>}
          </div>
        )}
      </div>
    </div>
  )
}
