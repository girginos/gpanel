// gosp-mobil-v1
// Mobil alt gezinme çubuğu (tab bar). Yalnız < lg genişlikte görünür;
// masaüstünde kenar çubuğu zaten kalıcı olduğu için gizlenir.
//
// Neden: mobilde en sık kullanılan 3-4 hedefe ve birincil eyleme başparmakla
// erişilebilir bir yer gerekiyor. Menü sekmesi, çubuğa sığmayan her şey için
// mevcut çekmeceyi açar — yani hiçbir sayfa erişilemez hâle gelmez.
import { NavLink, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { I, Ikon } from '@/components/Ikon'
import { musteriDomainID, useRol } from '@/lib/rol'

const NAV_EN: Record<string, string> = {
  'Anasayfa': 'Home', 'Domainler': 'Domains', 'İzleme': 'Monitoring',
  'Planlar': 'Plans',
  'Genel': 'Overview', 'Dosyalar': 'Files', 'Veritabanı': 'Database',
  'Yedekler': 'Backups', 'Yeni': 'New', 'Menü': 'Menu',
  'Alt gezinme': 'Bottom navigation',
  'Yeni domain oluştur': 'Create new domain',
  'Menüyü aç': 'Open menu',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (NAV_EN[tr] || tr) : tr)

type Sekme = { to: string; etiket: string; ikon: string; end?: boolean }

const IK = {
  home:   'M3 12l2-2 7-7 7 7 2 2v8a2 2 0 01-2 2h-3v-7H10v7H7a2 2 0 01-2-2v-8z',
  domain: 'M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
  izleme: 'M3 12l3-3 3 6 4-9 3 6h5',
  plan:   'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  dosya:  'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z',
  veri:   'M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3-3.582 3-8 3-8-1.343-8-3zM4 7v10c0 1.657 3.582 3 8 3s8-1.343 8-3V7',
  yedek:  'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15',
}

export default function AltNavBar({ onMenuAc }: { onMenuAc: () => void }) {
  useTranslation() // dil re-render aboneligi
  const navigate = useNavigate()

  // Rol tek kaynaktan (useAuth kullanici.rol): 'user' = müşteri, 'reseller' = bayi.
  const rol = useRol()
  const isMusteri = rol === 'user'
  const isBayi = rol === 'reseller'
  const domainID = musteriDomainID() ?? ''

  // Yönetici: birincil eylem "yeni domain". Müşteri domain oluşturamaz —
  // onun için orta eylem yok, düz 4 sekme + menü.
  const yoneticiSekmeler: Sekme[] = [
    { to: '/', etiket: 'Anasayfa', ikon: IK.home, end: true },
    { to: '/domainler', etiket: 'Domainler', ikon: IK.domain },
    { to: '/izleme', etiket: 'İzleme', ikon: IK.izleme },
  ]

  // Bayi: kendi menüsündeki (RESELLER_NAV) hedefler. /izleme YÖNETİCİ-YALNIZ —
  // sunucu metrikleri bayiye 403 döner; yerine bayinin planları.
  const bayiSekmeler: Sekme[] = [
    { to: '/', etiket: 'Anasayfa', ikon: IK.home, end: true },
    { to: '/domainler', etiket: 'Domainler', ikon: IK.domain },
    { to: '/hizmet-planlari', etiket: 'Planlar', ikon: IK.plan },
  ]

  const musteriSekmeler: Sekme[] = [
    { to: `/abonelikler/${domainID}`, etiket: 'Genel', ikon: IK.home, end: true },
    { to: `/abonelikler/${domainID}/dosyalar`, etiket: 'Dosyalar', ikon: IK.dosya },
    { to: `/abonelikler/${domainID}/veritabanlari`, etiket: 'Veritabanı', ikon: IK.veri },
    { to: `/abonelikler/${domainID}/yedekler`, etiket: 'Yedekler', ikon: IK.yedek },
  ]

  const sekmeler = isMusteri ? musteriSekmeler : isBayi ? bayiSekmeler : yoneticiSekmeler

  const sekmeSinif = ({ isActive }: { isActive: boolean }) =>
    `flex flex-1 flex-col items-center justify-center gap-0.5 py-1.5 min-w-0 transition ${
      isActive
        ? 'text-brand-600 dark:text-brand-400'
        : 'text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'
    }`

  return (
    <nav
      className="lg:hidden fixed bottom-0 inset-x-0 z-30 flex items-stretch
                 border-t border-slate-200 dark:border-dark-600
                 bg-white/95 dark:bg-dark-800/95 backdrop-blur
                 pb-[env(safe-area-inset-bottom)]"
      aria-label={cevir("Alt gezinme")}
    >
      {sekmeler.map((s) => (
        <NavLink key={s.to} to={s.to} end={s.end} className={sekmeSinif}>
          {({ isActive }) => (
            <>
              {/* Aktif sekme daha kalın çizgiyle vurgulanır (Ikon kalinlik). */}
              <Ikon d={s.ikon} kalinlik={isActive ? 2.2 : 1.7} className="w-[22px] h-[22px] flex-shrink-0" />
              <span className="text-[10px] leading-none truncate max-w-full px-0.5">{cevir(s.etiket)}</span>
            </>
          )}
        </NavLink>
      ))}

      {/* Birincil eylem — yönetici ve bayide. Çubuğun ritmini bozmayacak kadar
          yükseltilmiş; DomainsPage bu parametreyi görüp oluşturma kipini açar. */}
      {!isMusteri && (
        <button
          type="button"
          onClick={() => navigate('/domainler?yeni=1')}
          className="flex flex-1 flex-col items-center justify-center gap-0.5 py-1.5 min-w-0
                     text-slate-500 dark:text-slate-400"
          aria-label={cevir("Yeni domain oluştur")}
        >
          <span className="flex items-center justify-center w-[26px] h-[26px] rounded-lg
                           bg-brand-600 text-white shadow-sm shadow-brand-600/40">
            <Ikon d={I.arti} kalinlik={2.4} className="w-4 h-4" />
          </span>
          <span className="text-[10px] leading-none truncate max-w-full px-0.5">{cevir('Yeni')}</span>
        </button>
      )}

      <button
        type="button"
        onClick={onMenuAc}
        className="flex flex-1 flex-col items-center justify-center gap-0.5 py-1.5 min-w-0
                   text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 transition"
        aria-label={cevir("Menüyü aç")}
        aria-controls="gosp-kenar-cubugu"
      >
        <Ikon d={I.menu} className="w-[22px] h-[22px] flex-shrink-0" />
        <span className="text-[10px] leading-none">{cevir('Menü')}</span>
      </button>
    </nav>
  )
}
