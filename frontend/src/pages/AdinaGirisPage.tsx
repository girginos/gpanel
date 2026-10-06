// /adina — müşteri ADINA giriş kodunu (URL parçası #kod=…) kısa ömürlü token'a çevirir
// (bkz. lib/adina.ts). Kimliksiz rota: GuardedRoute dışında.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth, type AdinaBilgi } from '@/store/auth'
import { guvenliDonus } from '@/lib/adina'
import { useBayiCevir } from '@/lib/bayiCeviri'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

type Yanit = {
  token?: string; bitis?: number; domain_id?: number; alan_adi?: string; kullanici?: string
  adina?: { aktor?: string; rol?: string }; sso?: boolean; hata?: string
}

export default function AdinaGirisPage() {
  const t = useBayiCevir()
  const navigate = useNavigate()
  const girisAdina = useAuth((s) => s.girisAdina)
  const [hata, setHata] = useState('')
  const [don, setDon] = useState('/')
  // Bağlantıda `don` yoksa bayi değil API anahtarı (WHMCS) bağlantısıdır → "bayi paneline dön" yok.
  const [ssoLink, setSsoLink] = useState(false)
  const basladi = useRef(false)

  useEffect(() => {
    if (basladi.current) return // StrictMode çift koşusu: kod tek kullanımlık
    basladi.current = true
    const p = new URLSearchParams(window.location.hash.replace(/^#/, ''))
    const kod = p.get('kod') || ''
    const donus = guvenliDonus(p.get('don'))
    const sekme = p.get('sekme') === '1'
    const sso = !p.has('don')
    setSsoLink(sso)
    setDon(donus)
    // Kod adres çubuğunda/geçmişte KALMAZ.
    window.history.replaceState(null, '', window.location.pathname)
    if (!kod) { setHata(t('Giriş bağlantısı geçersiz ya da süresi doldu')); return }
    // Düz fetch: bu sekmede açık bir bayi oturumu varsa axios onun token'ını eklerdi.
    fetch(`${(import.meta.env.VITE_API_BASE as string) || '/api/v1'}/musteri/adina-giris`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kod }),
    })
      .then(async (r) => {
        const y = (await r.json().catch(() => ({}))) as Yanit
        if (!r.ok || !y.token || !y.domain_id || !y.alan_adi) {
          setHata(y.hata || t('Giriş bağlantısı geçersiz ya da süresi doldu'))
          return
        }
        const bilgi: AdinaBilgi = {
          aktor: y.adina?.aktor || '', rol: y.adina?.rol === 'admin' ? 'admin' : 'reseller',
          alan: y.alan_adi, domainID: y.domain_id, don: donus, sekme, sso: sso || y.sso === true,
        }
        girisAdina(y.token, y.bitis || 0, y.domain_id, y.alan_adi, y.kullanici || '', bilgi)
        navigate(`/abonelikler/${y.domain_id}`, { replace: true })
      })
      .catch(() => setHata(t('Sunucuya ulaşılamadı — bağlantınızı kontrol edin')))
  }, [girisAdina, navigate, t])

  return (
    <div className="grid min-h-screen place-items-center bg-gray-50 px-4 dark:bg-dark-900">
      {hata ? (
        <div className="w-full max-w-md space-y-4">
          <UyariKutusu ton="tehlike" baslik={t('Müşteri paneline girilemedi')}>{hata}</UyariKutusu>
          <Button color="primary" variant="soft" ikon={I.geri} onClick={() => window.location.assign(ssoLink ? '/cp/giris' : don)}>{t(ssoLink ? 'Giriş sayfasına git' : 'Bayi paneline dön')}</Button>
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
