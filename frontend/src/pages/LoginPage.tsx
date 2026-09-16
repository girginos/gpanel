import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'
import { useMarka, logoURL, bannerURL, VARSAYILAN as MARKA_VARSAYILAN } from '@/lib/marka'
import { Button, Input } from '@/components/ui'
import { useToast } from '@/components/Toast'
import GlobeBanner from '@/components/GlobeBanner'

type LoginResp = {
  token?: string
  bitis?: number
  kullanici?: { id: number; adi: string; rol: 'admin' | 'reseller' | 'user'; ad_soyad?: string }
  iki_fa_gerekli?: boolean
}

const LOGINP_EN: Record<string, string> = {
  "Authenticator uygulamanızdaki 6 haneli kodu girin.": "Enter the 6-digit code from your authenticator app.",
  "Devam etmek için giriş yapın.": "Sign in to continue.",
  "Doğrula ve giriş yap": "Verify and sign in",
  "Giriş başarısız": "Sign-in failed",
  "Giriş yap": "Sign in",
  "Giriş yapılıyor…": "Signing in…",
  "Hoş geldiniz": "Welcome",
  "İki adımlı doğrulama kodu": "Two-factor authentication code",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (LOGINP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// Prefix ikonları (gerçek SVG)
const IkonKullanici = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-[18px] w-[18px]">
    <path d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.5 20.25a7.5 7.5 0 0115 0" />
  </svg>
)
const IkonKilit = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-[18px] w-[18px]">
    <path d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75M6.75 10.5h10.5a1.5 1.5 0 011.5 1.5v6a1.5 1.5 0 01-1.5 1.5H6.75a1.5 1.5 0 01-1.5-1.5v-6a1.5 1.5 0 011.5-1.5z" />
  </svg>
)

export default function LoginPage() {
  const m = useMarka()
  useTranslation() // dil re-render aboneligi
  const [kullanici, setKullanici] = useState('')
  const [parola, setParola] = useState('')
  const [kod, setKod] = useState('')
  const [ikiFa, setIkiFa] = useState(false)
  const [yukleniyor, setYukleniyor] = useState(false)
  // Hata artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
  const toast = useToast()
  const navigate = useNavigate()
  const [sp] = useSearchParams()
  const giris = useAuth((s) => s.giris)
  // Sürüm TEK kaynaktan: backend /healthz (public, kimlik gerekmez). Footer'da
  // hardcode yerine canlı sürüm; başarısız/beklerken sessizce boş kalır ve
  // GİRİŞ AKIŞINI ASLA etkilemez (yalnız dipnot gösterimi).
  const [surum, setSurum] = useState('')
  useEffect(() => {
    let iptal = false
    fetch('/healthz').then((r) => (r.ok ? r.json() : null)).then((v) => {
      if (!iptal && v && v.surum) setSurum(v.surum)
    }).catch(() => {})
    return () => { iptal = true }
  }, [])

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setYukleniyor(true)
    try {
      const { data } = await api.post<LoginResp>('/auth/login', { kullanici, parola, kod })
      if (data.iki_fa_gerekli) {
        setIkiFa(true); setYukleniyor(false)
        return
      }
      giris(data.token!, data.kullanici!, data.bitis!)
      // returnTo: gelmek istenen iç yola dön. 🔴 Açık-yönlendirme koruması —
      // yalnız '/' ile başlayan ama '//' (protokol-göreli) OLMAYAN yol kabul edilir.
      const next = sp.get('next') || ''
      const hedef = next.startsWith('/') && !next.startsWith('//') ? next : '/'
      navigate(hedef, { replace: true })
    } catch (err) {
      const baslik = cevir("Giriş başarısız")
      const m = apiHata(err, baslik)
      setHata(m)
      toast.hata(baslik, m === baslik ? undefined : m)
    } finally {
      setYukleniyor(false)
    }
  }

  // 62/38: banner icindeki tasarim %35 icerik + %48 dashboard oraninda kurgulanmis;
  // yariya sikistirilirsa iki sutun da ezilir. Dar ekranda banner kendi CSS'inde
  // gizleniyor, form tek sutun olarak ortalanir.
  return (
    <div className="grid min-h-screen lg:grid-cols-[62fr_38fr]">
      <GlobeBanner />
      <div className="flex items-center justify-center bg-gray-50 px-4 py-10 dark:bg-dark-900">
        <div className="w-full max-w-[25rem]">
        {/* Ortalanmış logo + başlık (Tailux sign-in) */}
        <div className="mb-8 flex flex-col items-center text-center">
          {/* 🔴 MARKA ALANI — logo KUTUYA SIKIŞTIRILMAZ.
              Önceki hâli 56x56'lık kare bir çerçeveydi; yatay bir marka logosu
              (ki çoğu öyledir) orada tanınmaz bir leke oluyordu. Yüklenmiş logo
              artık kendi en-boy oranıyla, geniş ve çerçevesiz durur — giriş
              ekranının en görünür yeri markanın kendisine ayrılmıştır.
              Logo YOKKA panelin kendi işareti eski kare biçiminde kalır. */}
          {logoURL(m) ? (
            <img
              src={logoURL(m) as string}
              alt={m.panel_adi}
              className="max-h-24 w-auto max-w-[280px] object-contain"
            />
          ) : (
            <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-brand-600 shadow-lg shadow-brand-600/30">
              <svg viewBox="0 0 32 32" className="h-8 w-8 text-white" fill="currentColor">
                <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
              </svg>
            </div>
          )}
          <h1 className="mt-4 text-2xl font-semibold text-gray-800 dark:text-dark-50">
            {m.giris_baslik === MARKA_VARSAYILAN.giris_baslik ? cevir("Hoş geldiniz") : m.giris_baslik}
          </h1>
          <p className="mt-1 text-sm text-gray-400 dark:text-dark-300">{cevir("Devam etmek için giriş yapın.")}</p>
        </div>

        {/* Whitelabel giriş bandı */}
        {(bannerURL(m) || m.banner_metin) ? (
          <div className="mb-6">
            {(() => {
              const icerik = (
                <>
                  {bannerURL(m) ? (
                    <img src={bannerURL(m) as string} alt={m.banner_metin ? '' : m.panel_adi} className="max-h-32 w-full rounded-lg object-contain" />
                  ) : null}
                  {m.banner_metin ? (
                    <p className={`text-center text-sm text-gray-600 dark:text-dark-200 ${bannerURL(m) ? 'mt-2.5' : ''}`}>{m.banner_metin}</p>
                  ) : null}
                </>
              )
              return m.banner_url ? (
                <a href={m.banner_url} target="_blank" rel="noopener noreferrer"
                  className="block rounded-lg transition hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-primary-500/40">
                  {icerik}
                </a>
              ) : icerik
            })()}
          </div>
        ) : null}

        <form onSubmit={onSubmit} className="space-y-4">
          <Input
            prefix={<IkonKullanici />}
            type="text"
            placeholder="User"
            value={kullanici}
            onChange={(e) => setKullanici(e.target.value)}
            autoComplete="username"
            autoFocus
            required
            classNames={{ input: 'py-2.5' }}
          />
          <Input
            prefix={<IkonKilit />}
            type="password"
            placeholder="Şifre"
            value={parola}
            onChange={(e) => setParola(e.target.value)}
            autoComplete="current-password"
            required
            readOnly={ikiFa}
            classNames={{ input: 'py-2.5' }}
          />

          {ikiFa && (
            <div>
              <Input
                type="text"
                inputMode="numeric"
                value={kod}
                onChange={(e) => setKod(e.target.value.replace(/\D/g, '').slice(0, 6))}
                autoFocus
                placeholder="000000"
                classNames={{ input: 'py-2.5 text-center text-lg font-mono tracking-[0.4em]' }}
              />
              <p className="mt-1.5 text-xs text-gray-400 dark:text-dark-300">{cevir("Authenticator uygulamanızdaki 6 haneli kodu girin.")}</p>
            </div>
          )}

          <Button type="submit" color="primary" disabled={yukleniyor} className="w-full py-2.5">
            {yukleniyor ? cevir('Giriş yapılıyor…') : ikiFa ? cevir('Doğrula ve giriş yap') : cevir("Giriş yap")}
          </Button>
        </form>

        {m.giris_dipnot ? (
          <p className="mt-6 text-center text-xs text-gray-400 dark:text-dark-300">{m.giris_dipnot}</p>
        ) : m.surum_goster ? (
          <p className="mt-6 text-center text-xs text-gray-400 dark:text-dark-300">
            {m.panel_adi}{surum ? ' · ' + surum : ''}
          </p>
        ) : null}
        {m.destek_url ? (
          <p className="mt-2 text-center text-xs">
            <a href={m.destek_url} target="_blank" rel="noopener noreferrer"
              className="inline-block min-h-[44px] px-3 py-2 text-primary-700 hover:underline dark:text-primary-400">
              {cevir("Destek")}
            </a>
          </p>
        ) : null}
        </div>
      </div>
    </div>
  )
}
