// gosp-dark-swept
// gosp-dark-swept-v2
import { useState } from 'react'
import { useMarka, logoURL } from '@/lib/marka'
import { useTranslation } from 'react-i18next'
import DilSecici from '@/components/DilSecici'
import { Ikon, I } from '@/components/Ikon'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { apiHata, hesapAskidaMi } from '@/lib/api'
import { useKapiTipi } from '@/lib/kapi'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'
import { useAuth } from '@/store/auth'
import { useHesapAski } from '@/store/hesapAski'
import axios from 'axios'

const CPG_EN: Record<string, string> = {
  'Yönetici ya da bayi misiniz?': 'Administrator or reseller?',
  'Yönetim girişi': 'Management sign-in',
}

export default function CPanelGirisPage() {
  const marka = useMarka()
  const { t, i18n } = useTranslation()
  const cevir = (tr: string): string => (i18n.language === 'en' ? CPG_EN[tr] || tr : tr)
  // Karma kapıda (kapı modu kapalı / geçiş kapısı) bu form, aynı tarayıcıda son oturumu
  // müşteri olan yönetici/bayiye de açılabilir (GuardedRoute '/'): yönetim formuna çıkış
  // bağlantısı olmadan tek yol adres çubuğuydu (MFE-1). Rol kapısında (tip 'musteri')
  // bağlantı gösterilmez — orada yönetici girişi yok, /giris buraya geri çevrilir.
  const kapi = useKapiTipi()
  const loc = useLocation()
  const toast = useToast()
  const [kullanici, setKullanici] = useState('')
  const [parola, setParola] = useState('')
  // Hata artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
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
      // Parola doğru ama hesap askıda: form hatası yerine tam ekran askı sayfası
      // (sunucu askıyı yalnız doğru parolayla bildirir).
      if (hesapAskidaMi(e)) {
        useHesapAski.getState().askiyaGec('musteri')
        return
      }
      const baslik = t('giris.hata')
      const mesaj = apiHata(e, baslik)
      setHata(mesaj)
      toast.hata(baslik, mesaj === baslik ? undefined : mesaj)
    } finally {
      setYuk(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-100 to-brand-50 px-4">
      <div className="w-full max-w-md bg-white dark:bg-dark-700 rounded-lg shadow-xl p-7">
        <div className="flex justify-end mb-1"><DilSecici /></div>
        <div className="text-center mb-6">
          {/* 🔴 Bu sayfayi BAYININ MUSTERISI gorur — whitelabel'in asil hedefi.
              Yuklenmis logo varsa jenerik kure ikonu yerine o gosterilir. */}
          {logoURL(marka) ? (
            <img
              src={logoURL(marka) as string}
              alt={marka.panel_adi}
              className="inline-block w-14 h-14 rounded-lg object-contain bg-white dark:bg-dark-600 border border-slate-200 dark:border-slate-600 mb-3"
            />
          ) : (
            <div className="inline-flex items-center justify-center w-14 h-14 rounded-lg bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 text-2xl mb-3"><Ikon d={I.kure} className="h-7 w-7" /></div>
          )}
          <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{t('giris.baslik')}</h1>
          {/* Panel adi yalniz OZELLESTIRILMISSE yazilir: varsayilan kurulumda
              "GirginOSPanel" satiri sayfaya bilgi katmaz, gurultu olurdu. */}
          {marka.panel_adi !== 'GirginOSPanel' ? (
            <p className="text-sm font-medium text-brand-700 dark:text-brand-300 mt-0.5">{marka.panel_adi}</p>
          ) : null}
          <p className="text-sm text-slate-500 dark:text-slate-500 mt-1">{t('giris.altbaslik')}</p>
        </div>

        <form onSubmit={gir} className="space-y-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{t('giris.kullanici')}</label>
            <input type="text" value={kullanici} onChange={e => setKullanici(e.target.value)}
              autoComplete="username" required autoFocus
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm focus:border-brand-500 outline-none" />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{t('giris.parola')}</label>
            <input type="password" value={parola} onChange={e => setParola(e.target.value)}
              autoComplete="current-password" required
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm focus:border-brand-500 outline-none" />
          </div>
          <Button type="submit" color="primary" yukleniyor={yuk} disabled={!kullanici || !parola}
            ikon={I.giris} className="w-full">
            {yuk ? t('ortak.yukleniyor') : t('giris.girisYap')}
          </Button>
        </form>
        {kapi === 'karma' && (
          <p className="mt-5 text-center text-xs text-slate-500 dark:text-slate-400">
            {cevir('Yönetici ya da bayi misiniz?')}{' '}
            {/* ?next= korunur: yönetici giriş sonrası istediği yola döner. */}
            <Link to={'/giris' + loc.search}
              className="font-medium text-brand-700 underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-500 dark:text-brand-300">
              {cevir('Yönetim girişi')}
            </Link>
          </p>
        )}
      </div>
    </div>
  )
}