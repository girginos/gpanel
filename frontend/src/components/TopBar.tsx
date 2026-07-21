// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/store/auth'
import { getTheme, setTheme, type Theme } from '@/lib/theme'

export default function TopBar({ onMenuAc, menuAcik }: { onMenuAc?: () => void; menuAcik?: boolean }) {
  const kullanici = useAuth((s) => s.kullanici)
  const cikis = useAuth((s) => s.cikis)
  const navigate = useNavigate()
  const [menuAcikProfil, setMenuAcik] = useState(false)
  const [tema, setTema] = useState<Theme>(getTheme())

  useEffect(() => {
    const h = (e: Event) => setTema((e as CustomEvent<Theme>).detail)
    window.addEventListener('gosp:theme-change', h)
    return () => window.removeEventListener('gosp:theme-change', h)
  }, [])

  function temaDegistir() {
    const siradaki: Theme = tema === 'light' ? 'dark' : tema === 'dark' ? 'system' : 'light'
    setTheme(siradaki)
    setTema(siradaki)
  }

  function onCikis() {
    cikis()
    navigate('/giris', { replace: true })
  }

  return (
    <header className="h-14 bg-white dark:bg-slate-800 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 dark:border-slate-800 flex items-center px-3 sm:px-4 sticky top-0 z-30 gap-2 sm:gap-4">
      {/* Hamburger — yalnız < lg; kenar çubuğu orada çekmeceye dönüşüyor */}
      <button
        onClick={onMenuAc}
        className="lg:hidden -ml-1 p-2 text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-md transition flex-shrink-0"
        aria-label="Menüyü aç"
        aria-expanded={!!menuAcik}
        aria-controls="gosp-kenar-cubugu"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>

      {/* Dar ekranda ortalama boşluğu yok; lg'de eski ortalanmış arama korunur */}
      <div className="hidden lg:block flex-1" />

      <div className="flex-1 lg:flex-none w-full lg:max-w-xl min-w-0">
        <div className="relative">
          <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            placeholder="Ara..."
            aria-label="Ara"
            className="w-full pl-9 pr-3 py-1.5 text-sm bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg focus:bg-white dark:bg-slate-800 focus:border-brand-400 focus:ring-2 focus:ring-brand-500/15 outline-none transition"
          />
        </div>
      </div>

      <div className="flex-none lg:flex-1 flex items-center justify-end gap-0.5 sm:gap-1">
        <button onClick={temaDegistir}
          className="p-2 text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 dark:text-slate-400 dark:text-slate-500 dark:hover:text-slate-200 dark:hover:bg-slate-800 rounded-md transition"
          title={`Tema: ${tema} — tıkla değiştir`}>
          {tema === 'dark' ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
            </svg>
          ) : tema === 'light' ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
            </svg>
          ) : (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
          )}
        </button>
        <button className="hidden sm:inline-flex p-2 text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 dark:text-slate-400 dark:text-slate-500 dark:hover:text-slate-200 dark:hover:bg-slate-800 rounded-md transition" title="Bildirimler">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
          </svg>
        </button>

        <div className="relative">
          <button
            onClick={() => setMenuAcik((v) => !v)}
            className="flex items-center gap-2 px-1.5 sm:px-2 py-1.5 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 rounded-md transition"
            aria-label="Hesap menüsü"
          >
            <div className="w-7 h-7 rounded-full bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 font-semibold text-xs flex items-center justify-center flex-shrink-0">
              {(kullanici?.ad_soyad || kullanici?.adi || '?').slice(0, 1).toUpperCase()}
            </div>
            {/* İsim dar ekranda gizli — avatar zaten kimliği taşıyor */}
            <span className="hidden md:inline text-sm font-medium text-slate-700 dark:text-slate-300 max-w-[12rem] truncate">{kullanici?.ad_soyad || kullanici?.adi}</span>
            <svg className="hidden sm:block w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {menuAcikProfil && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMenuAcik(false)} />
              <div className="absolute right-0 mt-1 w-56 max-w-[calc(100vw-1.5rem)] bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg z-50 py-1">
                <div className="px-3 py-2 border-b border-slate-100 dark:border-slate-800">
                  <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{kullanici?.ad_soyad || kullanici?.adi}</div>
                  <div className="text-xs text-slate-500 dark:text-slate-500 capitalize">{kullanici?.rol}</div>
                </div>
                <button
                  onClick={() => { setMenuAcik(false); navigate('/profil') }}
                  className="w-full text-left px-3 py-2 text-sm text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800"
                >
                  Profil ve Tercihler
                </button>
                <div className="border-t border-slate-100 dark:border-slate-800 my-1"></div>
                <button
                  onClick={onCikis}
                  className="w-full text-left px-3 py-2 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20"
                >
                  Çıkış Yap
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  )
}
