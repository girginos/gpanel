// "Şifre değiştir" penceresi — kök + bayi Domainler listesi (tek hesap işlemi).
//
// Hesabın müşteri GİRİŞ (ana FTP) parolasını değiştirir: PUT /domains/{id}/giris-parola.
// Kullanıcı adı (sistem kullanıcısı) DEĞİŞTİRİLEMEZ: salt okunur gösterilir + kopyalanır.
//
//   · Parola ya elle yazılır ya da "Güçlü parola üret" ile SUNUCUYA ürettirilir (boş
//     gövde → sunucu kriptografik rastgele üretir, yanıtta BİR KEZ döner).
//   · Başarıda parola bir kez gösterilir ("tekrar gösterilmez"); pencere kapanınca
//     bileşen sökülür → durum bellekten silinir.
//   · 🔴 Parola YALNIZ bu bileşenin React durumunda yaşar: localStorage/sessionStorage/
//     URL/konsol/toast'a YAZILMAZ. Çağıran bileşen `key` ile söker (kapatınca null).
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, apiHata } from '@/lib/api'
import { panoyaKopyala } from '@/lib/parola'
import Modal from '@/components/Modal'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

export type SifreDomain = { id: number; alan_adi: string; sistem_kullanici: string }

const EN: Record<string, string> = {
  'Şifre değiştir': 'Change password',
  'Hesabın müşteri giriş parolasını değiştirir. Müşterinin açık oturumları sonlandırılır; müşteriye bildirim gitmez.': 'Changes the account’s customer login password. The customer’s open sessions are ended; the customer is not notified.',
  'Alan adı': 'Domain',
  'Kullanıcı adı': 'Username',
  'Kullanıcı adı değiştirilemez': 'The username cannot be changed',
  'Kopyala': 'Copy',
  'Kopyalandı': 'Copied',
  'Yeni parola': 'New password',
  'Göster': 'Show',
  'Gizle': 'Hide',
  'Güçlü parola üret': 'Generate strong password',
  'Güçlü bir parola sunucuda üretilecek; değişimden sonra bir kez gösterilir.': 'A strong password will be generated on the server and shown once after the change.',
  'Kendim yazacağım': 'I’ll type my own',
  'Parola en az 12 karakter olmalı': 'Password must be at least 12 characters',
  'Parola harf ve rakam içermeli': 'Password must contain letters and digits',
  'Parola en çok 128 karakter olmalı': 'Password must be at most 128 characters',
  'Parola satır sonu içeremez': 'Password cannot contain line breaks',
  'En az 12 karakter, harf ve rakam içermeli': 'At least 12 characters, with letters and digits',
  'Vazgeç': 'Cancel',
  'Parolayı değiştir': 'Change password',
  'Parola değiştirildi': 'Password changed',
  'Bu parola tekrar gösterilmez. Şimdi kopyalayıp müşteriye güvenli bir yoldan iletin.': 'This password will not be shown again. Copy it now and pass it to the customer securely.',
  'Parolayı kopyaladım': 'I copied the password',
  'Kapat': 'Close',
  'Yeni parola kopyala': 'Copy new password',
  'Parola': 'Password',
  'Panoya kopyalanamadı; elle seçip kopyalayın.': 'Could not copy to clipboard; select and copy it manually.',
  'Domain bulunamadı': 'Domain not found',
  'Bu işlem için yetkiniz yok': 'You are not allowed to do this',
  'Çok fazla parola değişimi denendi; biraz sonra tekrar deneyin': 'Too many password changes attempted; try again shortly',
  'Parola değiştirilemedi': 'Password could not be changed',
  'Demo hesabın parolası değiştirilemez': 'A demo account’s password cannot be changed',
}

// Panelin sunucu kuralının (hesaplar.ParolaGucluMu + api_parola.go) istemci yansıması.
export function sifreKuralHatasi(pw: string): string | null {
  if (/[\r\n\0]/.test(pw)) return 'Parola satır sonu içeremez'
  const len = Array.from(pw).length
  if (len < 12) return 'Parola en az 12 karakter olmalı'
  if (len > 128) return 'Parola en çok 128 karakter olmalı'
  if (!/[A-Za-z]/.test(pw) || !/[0-9]/.test(pw)) return 'Parola harf ve rakam içermeli'
  return null
}

export default function SifreDegistirModal({ domain, onKapat }: { domain: SifreDomain; onKapat: () => void }) {
  const { i18n } = useTranslation()
  const t = (tr: string) => (i18n.language === 'en' ? EN[tr] || tr : tr)
  const pwId = useId()
  const hataId = useId()
  const [parola, setParola] = useState('')
  const [uret, setUret] = useState(false) // true → sunucu üretir
  const [goster, setGoster] = useState(false)
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState('')
  const [dokundu, setDokundu] = useState(false)
  const [sonuc, setSonuc] = useState<string | null>(null) // bir kez gösterilen parola
  const [onay, setOnay] = useState(false)
  const [kopyaPw, setKopyaPw] = useState(false)
  const [kopyaKul, setKopyaKul] = useState(false)
  const [kopyaHata, setKopyaHata] = useState(false)

  const kuralHata = !uret && dokundu && parola !== '' ? sifreKuralHatasi(parola) : null
  const gonderilebilir = !yukleniyor && (uret || (parola !== '' && sifreKuralHatasi(parola) === null))

  async function kopyala(metin: string, isaretle: (v: boolean) => void) {
    const ok = await panoyaKopyala(metin)
    setKopyaHata(!ok)
    isaretle(ok)
    if (ok) window.setTimeout(() => isaretle(false), 2000)
  }

  async function gonder() {
    if (!uret) {
      const k = sifreKuralHatasi(parola)
      if (k) { setDokundu(true); return }
    }
    setYukleniyor(true); setHata('')
    try {
      const r = await api.put<{ uretildi?: boolean; password?: string }>(
        `/domains/${domain.id}/giris-parola`, uret ? {} : { parola })
      setSonuc(r.data?.uretildi && r.data.password ? r.data.password : parola)
      setParola(''); setUret(false)
    } catch (e) {
      const st = (e as { response?: { status?: number } })?.response?.status
      const tr = apiHata(e, 'Parola değiştirilemedi')
      setHata(i18n.language !== 'en' ? tr
        : st === 404 ? t('Domain bulunamadı')
          : st === 403 && /demo/i.test(tr) ? t('Demo hesabın parolası değiştirilemez')
            : st === 403 ? t('Bu işlem için yetkiniz yok')
              : st === 429 ? t('Çok fazla parola değişimi denendi; biraz sonra tekrar deneyin')
                : t('Parola değiştirilemedi'))
    } finally {
      setYukleniyor(false)
    }
  }

  const kullaniciSatiri = (
    <dl className="grid grid-cols-1 gap-3 text-sm">
      <div>
        <dt className="mb-1 text-xs font-medium text-gray-500 dark:text-dark-300">{t('Alan adı')}</dt>
        <dd className="break-all font-medium text-gray-900 dark:text-dark-50">{domain.alan_adi}</dd>
      </div>
      <div>
        <dt className="mb-1 text-xs font-medium text-gray-500 dark:text-dark-300">{t('Kullanıcı adı')}</dt>
        <dd className="flex items-stretch gap-2">
          <code title={t('Kullanıcı adı değiştirilemez')} aria-label={t('Kullanıcı adı')}
            className="block min-w-0 flex-1 select-all break-all rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 font-mono text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-dark-50">
            {domain.sistem_kullanici}
          </code>
          <Button variant="outlined" ikon={kopyaKul ? I.onay : I.kopya} onClick={() => kopyala(domain.sistem_kullanici, setKopyaKul)}
            aria-label={`${t('Kullanıcı adı')}: ${t('Kopyala')}`}>
            <span className="sr-only sm:not-sr-only">{kopyaKul ? t('Kopyalandı') : t('Kopyala')}</span>
          </Button>
        </dd>
      </div>
    </dl>
  )

  // ---- Sonuç: parola BİR KEZ ----
  if (sonuc !== null) {
    return (
      <Modal acik baslik={t('Parola değiştirildi')} ton="basari" kapatilamaz={!onay} onKapat={onKapat}
        alt={<Button color="primary" ikon={I.onay} disabled={!onay} onClick={onKapat}>{t('Kapat')}</Button>}>
        <div className="space-y-4">
          <UyariKutusu ton="uyari" kucuk>{t('Bu parola tekrar gösterilmez. Şimdi kopyalayıp müşteriye güvenli bir yoldan iletin.')}</UyariKutusu>
          {kullaniciSatiri}
          <div>
            <div className="mb-1 text-xs font-medium text-gray-500 dark:text-dark-300">{t('Parola')}</div>
            <div className="flex items-stretch gap-2">
              <code data-testid="yeni-parola" className="block min-w-0 flex-1 select-all break-all rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 font-mono text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-dark-50">{sonuc}</code>
              <Button variant="outlined" ikon={kopyaPw ? I.onay : I.kopya} onClick={() => kopyala(sonuc, setKopyaPw)} aria-label={t('Yeni parola kopyala')}>
                <span className="sr-only sm:not-sr-only">{kopyaPw ? t('Kopyalandı') : t('Kopyala')}</span>
              </Button>
            </div>
            {kopyaHata && <p role="alert" className="mt-1.5 text-xs text-error">{t('Panoya kopyalanamadı; elle seçip kopyalayın.')}</p>}
          </div>
          <label className="flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-dark-100">
            <input type="checkbox" checked={onay} onChange={(e) => setOnay(e.target.checked)} className="size-4 rounded accent-primary-600" />
            {t('Parolayı kopyaladım')}
          </label>
        </div>
      </Modal>
    )
  }

  // ---- Form ----
  return (
    <Modal acik baslik={t('Şifre değiştir')} ton="soru" kapatilamaz={yukleniyor} onKapat={onKapat}
      alt={<>
        <Button variant="outlined" onClick={onKapat} disabled={yukleniyor}>{t('Vazgeç')}</Button>
        <Button color="primary" ikon={I.anahtar} yukleniyor={yukleniyor} disabled={!gonderilebilir} onClick={gonder}>{t('Parolayı değiştir')}</Button>
      </>}>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (gonderilebilir) void gonder() }} noValidate>
        <p className="text-sm text-gray-600 dark:text-dark-200">{t('Hesabın müşteri giriş parolasını değiştirir. Müşterinin açık oturumları sonlandırılır; müşteriye bildirim gitmez.')}</p>
        {kullaniciSatiri}
        <div>
          <label htmlFor={pwId} className="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-300">{t('Yeni parola')}</label>
          <div className="flex items-stretch gap-2">
            <div className="relative min-w-0 flex-1">
              <input id={pwId} type={goster && !uret ? 'text' : 'password'} value={uret ? '' : parola} disabled={uret}
                onChange={(e) => { setParola(e.target.value); setDokundu(true); setHata('') }}
                onBlur={() => setDokundu(true)}
                autoComplete="new-password" autoCorrect="off" autoCapitalize="off" spellCheck={false} maxLength={256}
                data-lpignore="true" aria-invalid={!!kuralHata} aria-describedby={hataId}
                placeholder={uret ? '••••••••••••' : ''}
                className={`h-10 w-full rounded-lg border bg-white pe-10 ps-3 font-mono text-sm text-gray-900 outline-none focus-visible:ring-2 focus-visible:ring-primary-400 disabled:bg-gray-50 disabled:text-gray-400 dark:bg-dark-800 dark:text-dark-50 dark:disabled:bg-dark-900 ${kuralHata ? 'border-error' : 'border-gray-300 dark:border-dark-450'}`} />
              <button type="button" onClick={() => setGoster((g) => !g)} disabled={uret} aria-pressed={goster}
                aria-label={goster ? t('Gizle') : t('Göster')} title={goster ? t('Gizle') : t('Göster')}
                className="absolute inset-y-0 end-0 grid w-10 place-items-center rounded-e-lg text-gray-400 outline-none hover:text-gray-700 focus-visible:ring-2 focus-visible:ring-primary-400 disabled:opacity-40 dark:text-dark-300 dark:hover:text-dark-50">
                <Ikon d={goster ? I.gozKapali : I.goz} className="size-4" />
              </button>
            </div>
          </div>
          <p id={hataId} className={`mt-1.5 text-xs ${kuralHata ? 'text-error' : 'text-gray-500 dark:text-dark-300'}`} role={kuralHata ? 'alert' : undefined}>
            {kuralHata ? t(kuralHata) : uret ? t('Güçlü bir parola sunucuda üretilecek; değişimden sonra bir kez gösterilir.') : t('En az 12 karakter, harf ve rakam içermeli')}
          </p>
          <div className="mt-2">
            {uret ? (
              <Button type="button" variant="flat" boyut="sm" ikon={I.kalem} onClick={() => { setUret(false); setHata('') }}>{t('Kendim yazacağım')}</Button>
            ) : (
              <Button type="button" variant="soft" color="primary" boyut="sm" ikon={I.yenile} onClick={() => { setUret(true); setParola(''); setGoster(false); setHata('') }}>{t('Güçlü parola üret')}</Button>
            )}
          </div>
        </div>
        {hata && <UyariKutusu ton="tehlike" kucuk>{hata}</UyariKutusu>}
        <button type="submit" className="sr-only" tabIndex={-1} aria-hidden>{t('Parolayı değiştir')}</button>
      </form>
    </Modal>
  )
}
