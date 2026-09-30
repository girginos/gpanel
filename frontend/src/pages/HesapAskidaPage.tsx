// Hesap askıda — TAM EKRAN, markalı durum sayfası.
//
// Askıdaki hesap (müşteri, bayi) panelin HİÇBİR parçasını görmez: menü, üst çubuk, alt
// gezinme, araçlar yok. App bu sayfayı Routes yerine çizer (store/hesapAski); panel
// sökülünce onun istekleri de durur. Bu sayfa kendisi API isteği atmaz — marka açılışta
// yüklenmiştir (lib/marka) — bu yüzden askı yanıtı → sayfa → yeni istek döngüsü kurulamaz.
//
// Kapanış yolu tek: "Çıkış yap" → oturum (varsa) kapanır, ilgili giriş formu açılır.
//
// Sağlayıcı iletişimi YALNIZ gerçek kaynaktan: markanın destek adresi (whitelabel, giriş
// sayfasıyla aynı alan). Boşsa ya da güvenli bir bağlantı değilse hiç çizilmez.
//
// Ziyaretçilerin gördüğü site askı sayfası (503, provisioner/landing.go) AYRI şeydir.
import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { logoURL, marka, useMarka, VARSAYILAN as MARKA_VARSAYILAN } from '@/lib/marka'
import { useAuth } from '@/store/auth'
import { useHesapAski } from '@/store/hesapAski'
import { Button, Card, butonSinifi } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { TonRozeti } from '@/components/UyariKutusu'
import DilSecici from '@/components/DilSecici'
import TemaDugmesi from '@/components/TemaDugmesi'

const ASKI_EN: Record<string, string> = {
  'Hesabınız askıya alındı': 'Your account has been suspended',
  'Nedeni ve yeniden açılması için hizmet sağlayıcınızla görüşün.': 'Contact your service provider to learn why and to have it reactivated.',
  'Hizmet sağlayıcıyla iletişim': 'Contact your service provider',
  'yeni sekmede açılır': 'opens in a new tab',
  'Çıkış yap': 'Log out',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (ASKI_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// Yönetici girişli bir alan olsa da bağlantı yalnız http(s)/mailto ise çizilir
// (javascript: vb. şemalar tıklanabilir hâle gelmesin).
function guvenliDestekAdresi(url: string): string | null {
  const u = url.trim()
  return /^(https?:\/\/|mailto:)\S+$/i.test(u) ? u : null
}

export default function HesapAskidaPage() {
  useTranslation() // dil değişince yeniden çiz
  const m = useMarka()
  const navigate = useNavigate()
  const kaynak = useHesapAski((s) => s.kaynak)
  const baslikRef = useRef<HTMLHeadingElement>(null)
  const baslik = cevir('Hesabınız askıya alındı')
  const destek = guvenliDestekAdresi(m.destek_url)
  const logo = logoURL(m)

  // Ekran okuyucu ve klavye: sayfa açılınca odak başlığa taşınır (panelden gelindiyse
  // odak sökülen bir öğede kalırdı). Sekme başlığı da durumu söyler.
  useEffect(() => {
    baslikRef.current?.focus()
  }, [])
  // Çıkışta markanın başlığına döner (sökülen panel sayfasının başlığına değil).
  useEffect(() => {
    document.title = `${baslik} — ${m.baslik || m.panel_adi}`
    return () => {
      const g = marka()
      document.title = g.baslik || g.panel_adi
    }
  }, [baslik, m.baslik, m.panel_adi])

  function cikisYap() {
    // Hedef, oturumdan değil askının geldiği girişten: giriş formunda askı alındıysa
    // oturum hiç açılmamıştır.
    const hedef = kaynak === 'musteri' ? '/cp/giris' : '/giris'
    const auth = useAuth.getState()
    if (auth.token) auth.cikis()
    navigate(hedef, { replace: true })
    useHesapAski.getState().kapat()
  }

  return (
    <div className="flex min-h-screen flex-col bg-gray-50 dark:bg-dark-900">
      <div className="flex items-center justify-end gap-0.5 px-3 pt-3 sm:gap-1 sm:px-4">
        <DilSecici />
        <TemaDugmesi />
      </div>
      <main className="flex flex-1 items-center justify-center px-4 pb-16 pt-6">
        <Card skin="shadow" className="w-full max-w-md p-6 text-center sm:p-8">
          <div className="flex flex-col items-center">
            {logo ? (
              <img src={logo} alt={m.panel_adi} className="max-h-16 w-auto max-w-[220px] object-contain" />
            ) : (
              <div className="flex size-12 items-center justify-center rounded-xl bg-brand-600 shadow-lg shadow-brand-600/30" aria-hidden="true">
                <svg viewBox="0 0 32 32" className="size-7 text-white" fill="currentColor">
                  <path d="M9 10h14v3H9zM9 15h14v3H9zM9 20h9v3H9z" />
                </svg>
              </div>
            )}
            {/* Panel adı yalnız özelleştirilmişse (giriş sayfasıyla aynı kural). */}
            {!logo && m.panel_adi !== MARKA_VARSAYILAN.panel_adi ? (
              <p className="mt-2 text-sm font-medium text-gray-600 dark:text-dark-200">{m.panel_adi}</p>
            ) : null}
          </div>

          <TonRozeti ton="uyari" className="mx-auto mt-6" />
          <h1 ref={baslikRef} tabIndex={-1} className="mt-4 text-xl font-semibold text-gray-800 outline-none dark:text-dark-50">
            {baslik}
          </h1>
          <p className="mx-auto mt-2 max-w-sm text-sm text-gray-600 dark:text-dark-200">
            {cevir('Nedeni ve yeniden açılması için hizmet sağlayıcınızla görüşün.')}
          </p>

          <div className="mt-6 flex flex-col gap-2.5">
            {destek ? (
              <a href={destek} target="_blank" rel="noopener noreferrer" className={`${butonSinifi({ variant: 'outlined' })} w-full`}>
                {cevir('Hizmet sağlayıcıyla iletişim')}
                <Ikon d={I.harici} className="size-4 shrink-0 ms-2 -me-1" />
                <span className="sr-only">({cevir('yeni sekmede açılır')})</span>
              </a>
            ) : null}
            <Button color="primary" ikon={I.cikis} onClick={cikisYap} className="w-full">
              {cevir('Çıkış yap')}
            </Button>
          </div>
        </Card>
      </main>
    </div>
  )
}
