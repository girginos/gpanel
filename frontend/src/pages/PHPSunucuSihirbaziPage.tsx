// gosp-dark-swept
// PHP & Sunucu Kurulum Sihirbazı — dağınık PHP sürüm/modül/loader/web-sunucu
// yönetimini tek yerde toplar (EasyApache tarzı adım-adım). Her adım mevcut
// yönetim ekranını gömülü (gomulu) render eder; backend endpoint'leri aynıdır.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import PHPSurumleriPage from './PHPSurumleriPage'
import PHPModuleriPage from './PHPModuleriPage'

type Adim = { key: string; ad: string; aciklama: string }
const ADIMLAR: Adim[] = [
  { key: 'surumler', ad: 'PHP Sürümleri', aciklama: 'Sunucuya PHP sürümü ekle/kaldır' },
  { key: 'eklentiler', ad: 'PHP Eklentileri & Loader', aciklama: 'Modül aç/kapa, PECL kur, ionCube' },
  { key: 'websunucu', ad: 'Web Sunucu', aciklama: 'nginx / PHP-FPM durumu' },
  { key: 'ozet', ad: 'Özet', aciklama: 'Yapılandırmayı gözden geçir' },
]

export default function PHPSunucuSihirbaziPage() {
  const [aktif, setAktif] = useState('surumler')
  const aktifIdx = ADIMLAR.findIndex(a => a.key === aktif)

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Sunucu Yönetimi', href: '/araclar-ayarlar' },
        { etiket: 'PHP & Sunucu Sihirbazı' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">PHP &amp; Sunucu Kurulum Sihirbazı</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">PHP sürümleri, eklentiler, loader'lar ve web sunucu ayarlarını tek yerden yönetin. Değişiklikler her adımda anında uygulanır.</p>

      <div className="grid grid-cols-1 lg:grid-cols-[15rem_minmax(0,1fr)] gap-5 items-start">
        {/* Sol adım listesi */}
        <nav className="lg:sticky lg:top-[4.5rem] flex lg:flex-col gap-1 overflow-x-auto lg:overflow-visible pb-1">
          {ADIMLAR.map((a, i) => {
            const secili = a.key === aktif
            const tamam = i < aktifIdx
            return (
              <button
                key={a.key}
                onClick={() => setAktif(a.key)}
                className={`flex items-center gap-3 px-3 py-2.5 rounded-xl text-left transition shrink-0 lg:shrink w-auto lg:w-full ${
                  secili
                    ? 'bg-brand-50 dark:bg-brand-900/25 border border-brand-300 dark:border-brand-700'
                    : 'border border-transparent hover:bg-slate-50 dark:hover:bg-slate-800'
                }`}
              >
                <span className={`flex items-center justify-center w-6 h-6 rounded-full text-xs font-semibold shrink-0 ${
                  secili ? 'bg-brand-600 text-white' : tamam ? 'bg-emerald-500 text-white' : 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300'
                }`}>
                  {tamam ? '✓' : i + 1}
                </span>
                <span className="min-w-0">
                  <span className={`block text-sm font-medium ${secili ? 'text-brand-800 dark:text-brand-200' : 'text-slate-700 dark:text-slate-200'}`}>{a.ad}</span>
                  <span className="hidden lg:block text-[11px] text-slate-400 dark:text-slate-500 truncate">{a.aciklama}</span>
                </span>
              </button>
            )
          })}
        </nav>

        {/* İçerik */}
        <section className="min-w-0">
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 sm:p-5">
            {aktif === 'surumler' && <PHPSurumleriPage gomulu />}
            {aktif === 'eklentiler' && <PHPModuleriPage gomulu />}
            {aktif === 'websunucu' && <WebSunucuAdim />}
            {aktif === 'ozet' && <OzetAdim />}
          </div>

          {/* İleri / Geri */}
          <div className="flex items-center justify-between mt-4">
            <button
              onClick={() => aktifIdx > 0 && setAktif(ADIMLAR[aktifIdx - 1].key)}
              disabled={aktifIdx === 0}
              className="px-4 py-2 text-sm rounded-md border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40"
            >← Geri</button>
            {aktifIdx < ADIMLAR.length - 1 ? (
              <button
                onClick={() => setAktif(ADIMLAR[aktifIdx + 1].key)}
                className="px-4 py-2 text-sm rounded-md bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 font-medium"
              >İleri →</button>
            ) : (
              <Link to="/araclar-ayarlar" className="px-4 py-2 text-sm rounded-md bg-emerald-600 hover:bg-emerald-700 text-white font-medium">Bitir</Link>
            )}
          </div>
        </section>
      </div>
    </div>
  )
}

// Web Sunucu adımı: bu mimaride web sunucu nginx + per-tenant PHP-FPM'dir
// (cPanel/EasyApache'nin Apache-derleme modeli GEÇERLİ DEĞİL). Durum gösterilir;
// site-bazlı ayarlar (Apache uyumluluk katmanı, güvenlik başlıkları) domain
// detayındaki "Apache & nginx" ekranında yapılır.
function WebSunucuAdim() {
  const [durum, setDurum] = useState<{ nginx?: boolean; fpm_sayisi?: number } | null>(null)
  useEffect(() => {
    api.get('/php-surumler').then(r => {
      const yuklu = (r.data?.surumler || []).filter((s: any) => s.yuklu).length
      setDurum({ nginx: true, fpm_sayisi: yuklu })
    }).catch(() => setDurum({ nginx: true }))
  }, [])
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">Web Sunucu</h2>
      <div className="rounded-xl border border-sky-200 dark:border-sky-800/50 bg-sky-50 dark:bg-sky-900/15 p-4 text-sm text-sky-800 dark:text-sky-200">
        Bu platform <strong>nginx + izole per-tenant PHP-FPM</strong> mimarisi kullanır. cPanel/EasyApache'deki gibi Apache derlemesi veya global Apache modül seçimi <strong>yoktur</strong>; her domain kendi PHP-FPM havuzunda ve nginx vhost'unda çalışır.
      </div>
      <dl className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <DurumKart etiket="Web Sunucu" deger="nginx" ok />
        <DurumKart etiket="PHP çalışma modu" deger="PHP-FPM (per-tenant izole)" ok />
        <DurumKart etiket="Kurulu PHP sürümü" deger={durum?.fpm_sayisi != null ? `${durum.fpm_sayisi} sürüm` : '…'} ok={!!durum?.fpm_sayisi} />
        <DurumKart etiket="Apache uyumluluk" deger="Site bazında (.htaccess → nginx)" ok />
      </dl>
      <p className="text-xs text-slate-500 dark:text-slate-500">
        Bir sitenin güvenlik başlıkları, ek direktifler ve Apache/nginx uyumluluk ayarları için ilgili domainin <strong>Barınma ve DNS → Apache &amp; nginx</strong> ekranını kullanın.
      </p>
    </div>
  )
}

function DurumKart({ etiket, deger, ok }: { etiket: string; deger: string; ok?: boolean }) {
  return (
    <div className="rounded-xl border border-slate-200 dark:border-slate-700 p-3">
      <div className="text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-1">{etiket}</div>
      <div className="flex items-center gap-1.5 text-sm font-medium text-slate-800 dark:text-slate-200">
        <span className={`w-1.5 h-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
        {deger}
      </div>
    </div>
  )
}

// Özet adımı: kurulu PHP sürümleri + hızlı doğrulama listesi.
function OzetAdim() {
  const [surumler, setSurumler] = useState<any[]>([])
  const [yuk, setYuk] = useState(true)
  useEffect(() => {
    api.get('/php-surumler').then(r => setSurumler((r.data?.surumler || []).filter((s: any) => s.yuklu)))
      .catch(() => {}).finally(() => setYuk(false))
  }, [])
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100">Özet</h2>
      <p className="text-sm text-slate-500 dark:text-slate-500">Sunucunun mevcut PHP yapılandırması. Her değişiklik uygulandığı adımda anında etkinleşir.</p>

      <div>
        <div className="text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">Kurulu PHP sürümleri</div>
        {yuk ? <div className="text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> : surumler.length === 0 ? (
          <div className="text-sm text-slate-500">Kurulu sürüm bulunamadı. "PHP Sürümleri" adımından ekleyin.</div>
        ) : (
          <div className="flex flex-wrap gap-2">
            {surumler.map((s: any) => (
              <span key={s.surum} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-slate-100 dark:bg-slate-700/60 text-sm font-mono text-slate-800 dark:text-slate-200">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />PHP {s.surum}
              </span>
            ))}
          </div>
        )}
      </div>

      <div className="rounded-xl border border-emerald-200 dark:border-emerald-800/50 bg-emerald-50 dark:bg-emerald-900/15 p-4 text-sm text-emerald-800 dark:text-emerald-200">
        ✓ Yapılandırma tamamlandı. Domain bazında PHP sürümü ve ayarları, ilgili domainin PHP ayarları ekranından seçilir.
      </div>
    </div>
  )
}
