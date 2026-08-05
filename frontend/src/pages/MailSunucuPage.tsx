import { useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import Breadcrumb from '@/components/Breadcrumb'
import MailGenelAyarPage from './MailGenelAyarPage'
import MailIPHavuzuPage from './MailIPHavuzuPage'
import MailFiltrePage from './MailFiltrePage'

type Sekme = 'genel' | 'filtre' | 'ip-havuzu'
const SEKMELER: { k: Sekme; ad: string }[] = [
  { k: 'genel', ad: 'Genel Ayarlar' },
  { k: 'filtre', ad: 'Filtre' },
  { k: 'ip-havuzu', ad: 'IP Havuzu' },
]
const KEY = 'mail_sunucu_sekme'

function normalize(v: string | null): Sekme | null {
  return v === 'genel' || v === 'filtre' || v === 'ip-havuzu' ? v : null
}

export default function MailSunucuPage() {
  const [sp, setSp] = useSearchParams()
  // Sekme kaynağı önceliği: URL ?sekme → localStorage → varsayılan 'genel'.
  const kayitli = typeof localStorage !== 'undefined' ? localStorage.getItem(KEY) : null
  const sekme: Sekme = normalize(sp.get('sekme')) ?? normalize(kayitli) ?? 'genel'

  // URL'de sekme yoksa kayıtlı/varsayılanı adres çubuğuna yansıt (paylaşılabilir + yenilemede korunur).
  useEffect(() => {
    if (!normalize(sp.get('sekme'))) {
      setSp({ sekme }, { replace: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function setSekme(k: Sekme) {
    try { localStorage.setItem(KEY, k) } catch { /* yok say */ }
    setSp({ sekme: k }, { replace: true })
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-6xl mx-auto">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Sunucu Yönetimi' }, { etiket: 'Mail Sunucu' }]} />
      <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">Mail Sunucu</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">Sunucu geneli mail ayarları ve giden IP havuzu — tek yerden yönetin.</p>

      {/* segment sekmeler */}
      <div className="flex gap-1 border-b border-slate-200 dark:border-slate-700 mb-5" role="tablist" aria-label="Mail sunucu sekmeleri">
        {SEKMELER.map(s => {
          const aktif = sekme === s.k
          return (
            <button key={s.k} role="tab" aria-selected={aktif} onClick={() => setSekme(s.k)}
              className={`px-4 py-2.5 text-sm font-medium -mb-px border-b-2 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40 rounded-t ${
                aktif
                  ? 'border-brand-500 text-brand-700 dark:text-brand-300'
                  : 'border-transparent text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200'
              }`}>
              {s.ad}
            </button>
          )
        })}
      </div>

      {/* İçerik: her sekme kendi verisini/kaydını yönetir. */}
      {sekme === 'genel' ? <MailGenelAyarPage />
        : sekme === 'filtre' ? <MailFiltrePage />
        : <MailIPHavuzuPage />}
    </div>
  )
}
