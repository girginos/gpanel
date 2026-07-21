// gosp-dark-swept
// gosp-dark-swept-v2
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'

type LoginResp = {
  token?: string
  bitis?: number
  kullanici?: { id: number; adi: string; rol: 'admin' | 'reseller' | 'user'; ad_soyad?: string }
  iki_fa_gerekli?: boolean
}

export default function LoginPage() {
  const [kullanici, setKullanici] = useState('')
  const [parola, setParola] = useState('')
  const [kod, setKod] = useState('')
  const [ikiFa, setIkiFa] = useState(false)
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const navigate = useNavigate()
  const giris = useAuth((s) => s.giris)

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
      navigate('/', { replace: true })
    } catch (err) {
      setHata(apiHata(err, 'Giriş başarısız'))
    } finally {
      setYukleniyor(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 to-orange-50 dark:from-slate-950 dark:to-slate-900 px-4">
      <div className="w-full max-w-md">
        <div className="flex items-center justify-center mb-8">
          <div className="w-12 h-12 rounded-2xl bg-brand-600 flex items-center justify-center shadow-lg shadow-brand-600/30">
            <svg viewBox="0 0 32 32" className="w-7 h-7 text-white" fill="currentColor">
              <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
            </svg>
          </div>
          <div className="ml-3">
            <div className="text-xl font-semibold text-slate-900 dark:text-slate-100">GirginOSPanel</div>
            <div className="text-xs text-slate-500 dark:text-slate-500">Hosting Kontrol Paneli</div>
          </div>
        </div>

        <div className="bg-white dark:bg-slate-800 rounded-2xl shadow-xl border border-slate-200 dark:border-slate-700/60 p-8">
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Hoş geldiniz</h1>
          <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">Devam etmek için giriş yapın.</p>

          <form onSubmit={onSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Kullanıcı adı</label>
              <input
                type="text"
                value={kullanici}
                onChange={(e) => setKullanici(e.target.value)}
                autoComplete="username"
                autoFocus
                required
                className="w-full px-3.5 py-2.5 border border-slate-300 dark:border-slate-600 rounded-lg focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Parola</label>
              <input
                type="password"
                value={parola}
                onChange={(e) => setParola(e.target.value)}
                autoComplete="current-password"
                required
                readOnly={ikiFa}
                className="w-full px-3.5 py-2.5 border border-slate-300 dark:border-slate-600 rounded-lg focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition disabled:opacity-60"
              />
            </div>

            {ikiFa && (
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">İki adımlı doğrulama kodu</label>
                <input
                  type="text"
                  inputMode="numeric"
                  value={kod}
                  onChange={(e) => setKod(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  autoFocus
                  placeholder="000000"
                  className="w-full px-3.5 py-2.5 text-center text-lg font-mono tracking-[0.4em] border border-slate-300 dark:border-slate-600 rounded-lg focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition"
                />
                <p className="text-xs text-slate-400 dark:text-slate-500 mt-1.5">Authenticator uygulamanızdaki 6 haneli kodu girin.</p>
              </div>
            )}

            {hata && (
              <div className="px-3.5 py-2.5 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">
                {hata}
              </div>
            )}

            <button
              type="submit"
              disabled={yukleniyor}
              className="w-full bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 font-medium py-2.5 rounded-lg transition shadow-lg shadow-brand-600/20 disabled:shadow-none"
            >
              {yukleniyor ? 'Giriş yapılıyor…' : ikiFa ? 'Doğrula ve giriş yap' : 'Giriş yap'}
            </button>
          </form>
        </div>

        <p className="text-center text-xs text-slate-400 dark:text-slate-500 mt-6">
          GirginOSPanel · sürüm 0.2.0-f1
        </p>
      </div>
    </div>
  )
}