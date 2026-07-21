// gosp-dark-swept
// gosp-dark-swept-v2
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { apiHata } from '@/lib/api'
import { useAuth } from '@/store/auth'
import axios from 'axios'

export default function CPanelGirisPage() {
  const [kullanici, setKullanici] = useState('')
  const [parola, setParola] = useState('')
  const [hata, setHata] = useState<string | null>(null)
  const [yuk, setYuk] = useState(false)
  const nav = useNavigate()

  async function gir(e: React.FormEvent) {
    e.preventDefault()
    setYuk(true); setHata(null)
    try {
      const r = await axios.post('/api/v1/musteri/login', { kullanici, parola })
      const { token, bitis, domain_id, alan_adi } = r.data
      // Tek atomik nokta — store hem token'ı hem müşteri bayraklarını yazıyor.
      useAuth.getState().girisMusteri(token, bitis, domain_id, alan_adi, kullanici)
      nav('/abonelikler/' + domain_id, { replace: true })
      setTimeout(() => window.location.reload(), 100)
    } catch (e) {
      setHata(apiHata(e, 'Giriş başarısız'))
    } finally {
      setYuk(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-100 to-brand-50 px-4">
      <div className="w-full max-w-md bg-white dark:bg-slate-800 rounded-2xl shadow-xl p-7">
        <div className="text-center mb-6">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 text-2xl mb-3">🌐</div>
          <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-100">Müşteri Paneli</h1>
          <p className="text-sm text-slate-500 dark:text-slate-500 mt-1">Kullanıcı bilgilerinizle giriş yapın</p>
        </div>

        {hata && (
          <div className="mb-4 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>
        )}

        <form onSubmit={gir} className="space-y-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Kullanıcı Adı</label>
            <input type="text" value={kullanici} onChange={e => setKullanici(e.target.value)}
              autoComplete="username" required autoFocus
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm focus:border-brand-500 outline-none" />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Şifre</label>
            <input type="password" value={parola} onChange={e => setParola(e.target.value)}
              autoComplete="current-password" required
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm focus:border-brand-500 outline-none" />
          </div>
          <button type="submit" disabled={yuk || !kullanici || !parola}
            className="w-full px-4 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 font-medium rounded-md">
            {yuk ? 'Giriş yapılıyor…' : 'Giriş'}
          </button>
        </form>
      </div>
    </div>
  )
}