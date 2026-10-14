// /sso — bayi paneline TEK TIK giriş (WHMCS bayi ürünü, kök API anahtarı; bkz.
// internal/bayisso). Giriş kodu URL parçasında (#kod=…) gelir: sunucuya, nginx günlüğüne
// ve Referer'a gitmez; sayfa onu hemen adres çubuğundan siler ve POST /auth/sso-giris ile
// olağan bir bayi oturumuna çevirir. İsteğe bağlı `next` (iç yol, ör. /profil) girişten
// sonra açılacak sayfadır. Kimliksiz rota: GuardedRoute dışında.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/store/auth'
import { useBayiCevir } from '@/lib/bayiCeviri'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

type Yanit = {
  token?: string; bitis?: number; hata?: string
  kullanici?: { id: number; adi: string; rol: 'admin' | 'reseller' | 'user'; ad_soyad?: string }
}

// guvenliIcYol: yalnız bu panelin iç yolu (açık yönlendirme yok); değilse ana sayfa.
function guvenliIcYol(next: string | null): string {
  return next && /^\/[A-Za-z0-9/_-]*$/.test(next) && !next.startsWith('//') ? next : '/'
}

export default function BayiSSOPage() {
  const t = useBayiCevir()
  const navigate = useNavigate()
  const giris = useAuth((s) => s.giris)
  const [hata, setHata] = useState('')
  const basladi = useRef(false)

  useEffect(() => {
    if (basladi.current) return // StrictMode çift koşusu: kod tek kullanımlık
    basladi.current = true
    const p = new URLSearchParams(window.location.hash.replace(/^#/, ''))
    const kod = p.get('kod') || ''
    const hedef = guvenliIcYol(p.get('next'))
    // Kod adres çubuğunda/geçmişte KALMAZ.
    window.history.replaceState(null, '', window.location.pathname)
    if (!kod) { setHata(t('Giriş bağlantısı geçersiz ya da süresi doldu')); return }
    // Düz fetch: bu sekmede açık bir oturum varsa axios onun token'ını eklerdi.
    fetch(`${(import.meta.env.VITE_API_BASE as string) || '/api/v1'}/auth/sso-giris`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kod }),
    })
      .then(async (r) => {
        const y = (await r.json().catch(() => ({}))) as Yanit
        if (!r.ok || !y.token || !y.kullanici || y.kullanici.rol !== 'reseller') {
          setHata(y.hata || t('Giriş bağlantısı geçersiz ya da süresi doldu'))
          return
        }
        giris(y.token, y.kullanici, y.bitis || 0)
        navigate(hedef, { replace: true })
      })
      .catch(() => setHata(t('Sunucuya ulaşılamadı — bağlantınızı kontrol edin')))
  }, [giris, navigate, t])

  return (
    <div className="grid min-h-screen place-items-center bg-gray-50 px-4 dark:bg-dark-900">
      {hata ? (
        <div className="w-full max-w-md space-y-4">
          <UyariKutusu ton="tehlike" baslik={t('Bayi paneline girilemedi')}>{hata}</UyariKutusu>
          <Button color="primary" variant="soft" ikon={I.geri} onClick={() => window.location.assign('/giris')}>{t('Giriş sayfasına git')}</Button>
        </div>
      ) : (
        <div role="status" aria-live="polite" className="text-center">
          <div className="mx-auto size-9 animate-spin rounded-full border-2 border-gray-300 border-t-primary-600 motion-reduce:animate-none dark:border-dark-500 dark:border-t-primary-400" aria-hidden="true" />
          <p className="mt-4 text-sm font-medium text-gray-900 dark:text-dark-50">{t('Oturum açılıyor…')}</p>
        </div>
      )}
    </div>
  )
}
