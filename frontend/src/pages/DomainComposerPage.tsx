import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

type Durum = { kurulu: boolean; surum: string; composer_json: boolean; kullanici: string; dizin: string }


const COMPOSER_EN: Record<string, string> = {
  "Türkçe": "English",
  "Çalışıyor…": "Running…",
  "✓ Tamamlandı": "✓ Completed",
  "✗ Hata ile bitti": "✗ Finished with error",
  "Anasayfa": "Home",
  "dizininde": "directory, running as user",
  "olarak çalışır.": ".",
  "composer.json bulundu": "composer.json found",
  "Aboneliğe dön": "Back to subscription",
  "composer.json yok": "composer.json not found",
  "vendor/paket veya vendor/paket:^1.2": "vendor/package or vendor/package:^1.2",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (COMPOSER_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainComposerPage() {
  useTranslation() // dil re-render aboneligi
  const { id, sid } = useParams()
  const toast = useToast()
  const base = sid ? `/domains/${id}/subdomain/${sid}` : `/domains/${id}`
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [cikti, setCikti] = useState<string>('')
  const [calisan, setCalisan] = useState<string | null>(null)
  const [paket, setPaket] = useState('')

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Durum>(`${base}/composer`).then(r => setD(r.data)).catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }).finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function calistir(komut: string, pkt?: string) {
    setCalisan(komut); setHata(null); setCikti(`$ composer ${komut}${pkt ? ' ' + pkt : ''}\n\n${cevir("Çalışıyor…")}`)
    try {
      const { data } = await api.post(`${base}/composer`, { komut, paket: pkt || '' }, UZUN_ISTEK)
      setCikti(`$ composer ${komut}${pkt ? ' ' + pkt : ''}\n\n${data.cikti || cevir("(çıktı yok)")}\n\n${data.ok ? cevir('✓ Tamamlandı') : cevir('✗ Hata ile bitti')}`)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Çalıştırılamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m); setCikti('')
    } finally { setCalisan(null) }
  }

  if (yuk) return <div className="text-slate-400">{cevir("Yükleniyor…")}</div>
  if (!d) return <div className="text-sm text-red-600">{hata || cevir("Bulunamadı")}</div>

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: 'Composer' },
      ]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">PHP Composer</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        <span className="font-mono">{d.dizin}</span> {cevir("dizininde")} <span className="font-mono">{d.kullanici}</span> {cevir("olarak çalışır.")}
      </p>

      {!d.kurulu ? (
        <UyariKutusu ton="uyari">
          {cevir(cevir("Composer sunucuda kurulu değil. Yönetici tarafından kurulması gerekiyor."))}
        </UyariKutusu>
      ) : (
        <>
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4 shadow-xs">
            <div className="flex items-center justify-between mb-3">
              <div>
                <span className="text-xs font-mono text-slate-500">{d.surum}</span>
                <Badge variant="soft" color={d.composer_json ? 'success' : 'neutral'} className="ml-2">
                  {d.composer_json && <Ikon d={I.onay} className="size-3 shrink-0 me-1" />}
                  {d.composer_json ? cevir('composer.json bulundu') : cevir('composer.json yok')}
                </Badge>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button color="primary" boyut="sm" ikon={I.oynat} disabled={!!calisan} yukleniyor={calisan === 'install'} onClick={() => calistir('install')}>install</Button>
              <Button color="primary" boyut="sm" ikon={I.indir} disabled={!!calisan} yukleniyor={calisan === 'update'} onClick={() => calistir('update')}>update</Button>
              <Button variant="outlined" boyut="sm" ikon={I.terminal} disabled={!!calisan} yukleniyor={calisan === 'dump-autoload'} onClick={() => calistir('dump-autoload')}>dump-autoload</Button>
              <Button variant="outlined" boyut="sm" ikon={I.yenile} disabled={!!calisan} yukleniyor={calisan === 'validate'} onClick={() => calistir('validate')}>validate</Button>
              <Button variant="outlined" boyut="sm" ikon={I.goz} disabled={!!calisan} yukleniyor={calisan === 'show'} onClick={() => calistir('show')}>show</Button>
            </div>
            <div className="mt-3 flex gap-2">
              <input value={paket} onChange={e => setPaket(e.target.value)} placeholder={cevir("vendor/paket veya vendor/paket:^1.2")}
                className="flex-1 px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
              <Button color="primary" boyut="sm" ikon={I.arti} disabled={!!calisan || !paket.trim()} yukleniyor={calisan === 'require'} onClick={() => calistir('require', paket.trim())}>require</Button>
              <Button color="error" variant="soft" boyut="sm" ikon={I.cop} disabled={!!calisan || !paket.trim()} yukleniyor={calisan === 'remove'} onClick={() => calistir('remove', paket.trim())}>remove</Button>
            </div>
          </div>

          {cikti && (
            <div className="bg-dark-800 rounded-lg p-4 shadow-xs">
              <pre className="text-xs font-mono text-slate-100 whitespace-pre-wrap break-all max-h-96 overflow-y-auto">{cikti}</pre>
            </div>
          )}
        </>
      )}

      <div className="mt-4"><Link to={`/abonelikler/${id}`} className="inline-flex items-center gap-1.5 text-sm text-brand-600 dark:text-brand-400"><Ikon d={I.geri} className="size-4 shrink-0" />{cevir("Aboneliğe dön")}</Link></div>
    </div>
  )
}
