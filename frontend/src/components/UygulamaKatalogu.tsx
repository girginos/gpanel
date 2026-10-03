// Alan adının uygulama kataloğu + kurulu uygulamalar (tenant installer).
// SubscriptionDetailPage'in "Uygulamalar" sekmesinden (kök/bayi) buraya taşındı; aynı
// bileşen müşterinin /abonelikler/{id}/uygulamalar sayfasında da çizilir (DomainUygulamalarPage).
// Uçlar: GET /uygulamalar/katalog (oturum yeter), GET/POST/DELETE /domains/{id}/uygulamalar
// (MusteriScope). Davranış taşımadan önceki sekmeyle aynıdır.
// Simge: sunucunun Ikon alanı emoji (internal/uygulama) — ÇİZİLMEZ (emoji yok kuralı);
// LogoURL varsa logo, yoksa ya da yüklenemezse nötr uygulama simgesi (I.kutu).
import { useCallback, useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import ToolCard from '@/components/ToolCard'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge, Card, Input, butonSinifi } from '@/components/ui'
import Modal from '@/components/Modal'
import { I, Ikon } from '@/components/Ikon'

// /uygulamalar/katalog — internal/uygulama Tarif (JSON etiketi yok: alan adları büyük harfli).
type KatalogOgesi = { Kod: string; Ad: string; Ikon: string; LogoURL?: string; Surum: string; Kategori: string; Aciklama: string; NativeApp?: boolean }
// /domains/{id}/uygulamalar — internal/uygulama Kayit.
type KuruluUygulama = { id: number; ad: string; surum: string; alt_dizin: string; db_adi?: string; yonetim_url: string }

const UYG_EN: Record<string, string> = {
  '1-tıkla kurulum · yönetim': '1-click install · management',
  'host seviyesinde bir uygulamadır, tenant panelinden kurulamaz.': 'is a host-level application, cannot be installed from the tenant panel.',
  'yakında': 'coming soon',
  '{0} silinsin mi? Dosyalar + DB kaybolur.': 'Delete {0}? Files + DB will be lost.',
  'Silinemedi': 'Could not be deleted',
  'Öne Çıkan': 'Featured',
  'Kurulu Uygulamalar ({0})': 'Installed Applications ({0})',
  'Henüz uygulama kurulu değil. Aşağıdaki katalogdan seç.': 'No applications installed yet. Choose from the catalog below.',
  'Yönetim Paneli': 'Admin Panel',
  'Katalog ({0})': 'Catalog ({0})',
  "Admin'den kurulur": 'Installed from Admin',
  'Kur': 'Install',
  'Site:': 'Site:',
  'Kuruluyor…': 'Installing…',
  'Alt dizin (public_html/... altına)': 'Subdirectory (under public_html/...)',
  'boş = kök, blog, shop vb.': 'empty = root, blog, shop etc.',
  'İşlem başarısız': 'Operation failed',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (UYG_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type AlanAdi = { id: number; alan_adi: string }

// Resmî WordPress logosu (simple-icons, fill-tabanlı) — genel stroke ikonları
// markayı temsil etmiyordu; gerçek logo kullanılır.
function WPLogo() {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className="w-5 h-5" aria-hidden="true">
      <path d="M21.469 6.825c.84 1.537 1.318 3.3 1.318 5.175 0 3.979-2.156 7.456-5.363 9.325l3.295-9.527c.615-1.54.82-2.771.82-3.864 0-.405-.026-.78-.07-1.11m-7.981.105c.647-.03 1.232-.105 1.232-.105.582-.075.514-.93-.067-.899 0 0-1.755.135-2.88.135-1.064 0-2.85-.15-2.85-.15-.585-.03-.661.855-.075.885 0 0 .54.061 1.125.09l1.68 4.605-2.37 7.08L5.354 6.9c.649-.03 1.234-.1 1.234-.1.585-.075.516-.93-.065-.896 0 0-1.746.138-2.874.138-.2 0-.438-.008-.69-.015C4.911 3.15 8.235 1.215 12 1.215c2.809 0 5.365 1.072 7.286 2.833-.046-.003-.091-.009-.141-.009-1.06 0-1.812.923-1.812 1.914 0 .89.513 1.643 1.06 2.531.411.72.89 1.643.89 2.977 0 .915-.354 1.994-.821 3.479l-1.075 3.585-3.9-11.61.001.014zM12 22.784c-1.059 0-2.081-.153-3.048-.437l3.237-9.406 3.315 9.087c.024.053.05.101.078.149-1.12.393-2.325.607-3.582.607M1.211 12c0-1.564.336-3.05.935-4.39L7.29 21.709C3.694 19.96 1.212 16.271 1.211 12M12 0C5.385 0 0 5.385 0 12s5.385 12 12 12 12-5.385 12-12S18.615 0 12 0" />
    </svg>
  )
}

export default function UygulamaKatalogu({ domain }: { domain: AlanAdi }) {
  const [katalog, setKatalog] = useState<KatalogOgesi[]>([])
  const [kurulu, setKurulu] = useState<KuruluUygulama[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kurTaslak, setKurTaslak] = useState<{ kod: string; ad: string; alt_dizin: string } | null>(null)
  // Yüklenemeyen logolar: yerine nötr simge çizilir (boş yer kalmasın).
  const [kirikLogo, setKirikLogo] = useState<Set<string>>(() => new Set())
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const toast = useToast()
  const dialog = useDialog()

  const yukle = useCallback(async () => {
    setYukleniyor(true)
    try {
      const [k, l] = await Promise.all([
        api.get<{ items: KatalogOgesi[] }>(`/uygulamalar/katalog`),
        api.get<{ items: KuruluUygulama[] }>(`/domains/${domain.id}/uygulamalar`),
      ])
      setKatalog(k.data.items || [])
      setKurulu(l.data.items || [])
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e, cevir('Yüklenemedi')))
    } finally {
      setYukleniyor(false)
    }
  }, [domain.id, toast])
  useEffect(() => {
    void yukle()
    // Yalnız alan adı değişince yeniden çek (toast kimliği değişse bile değil).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [domain.id])

  const kur = async () => {
    if (!kurTaslak) return
    setGonderiliyor(true)
    try {
      await api.post(`/domains/${domain.id}/uygulamalar`, {
        kod: kurTaslak.kod, alt_dizin: kurTaslak.alt_dizin,
      }, UZUN_ISTEK)
      setKurTaslak(null)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e, cevir('Kurulum başarısız')))
    } finally { setGonderiliyor(false) }
  }
  const sil = async (kayitID: number, ad: string) => {
    if (!(await dialog.onay({ baslik: cevirT(cevir('{0} silinsin mi? Dosyalar + DB kaybolur.'), ad), onayEtiketi: cevir('Sil'), ton: 'tehlike', onayIkonu: I.cop }))) return
    try {
      await api.delete(`/domains/${domain.id}/uygulamalar/${kayitID}`, UZUN_ISTEK)
      await yukle()
    } catch (e) { await dialog.bilgi({ baslik: apiHata(e, cevir('Silinemedi')), ton: 'tehlike' }) }
  }

  if (yukleniyor) return <div className="py-8 text-center text-sm text-slate-500 dark:text-slate-400">{cevir('Yükleniyor…')}</div>
  return (
    <div className="space-y-6">
      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-3">
          {cevir('Öne Çıkan')}
        </h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
          <ToolCard etiket="WordPress" aciklama={cevir('1-tıkla kurulum · yönetim')} ikonNode={<WPLogo />} renk="sky"
            to={`/abonelikler/${domain.id}/wordpress`} />
        </div>
      </section>

      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-3">
          {cevirT(cevir('Kurulu Uygulamalar ({0})'), kurulu.length)}
        </h3>
        {kurulu.length === 0 ? (
          <div className="rounded-lg border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500 dark:border-dark-500 dark:text-slate-400">
            {cevir('Henüz uygulama kurulu değil. Aşağıdaki katalogdan seç.')}
          </div>
        ) : (
          <div className="space-y-2">
            {kurulu.map((u) => (
              <Card key={u.id} className="flex flex-wrap items-center justify-between gap-2 bg-white dark:bg-dark-800 p-3">
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-slate-800 dark:text-slate-100">{u.ad} <span className="text-xs font-normal text-slate-500 dark:text-slate-400">v{u.surum}</span></div>
                  <div className="truncate text-xs text-slate-500 dark:text-slate-400 font-mono">/{u.alt_dizin || ''} · DB: {u.db_adi || '—'}</div>
                </div>
                <div className="flex items-center gap-2">
                  <a href={u.yonetim_url} target="_blank" rel="noreferrer"
                    className={butonSinifi({ color: 'primary', boyut: 'sm' })}>
                    {cevir('Yönetim Paneli')}
                    <Ikon d={I.harici} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
                  </a>
                  <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(u.id, u.ad)}>{cevir('Sil')}</Button>
                </div>
              </Card>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="text-sm font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-3">
          {cevirT(cevir('Katalog ({0})'), katalog.length)}
        </h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {katalog.map((t) => (
            <Card key={t.Kod} className="bg-white dark:bg-dark-800 p-3">
              <div className="flex items-start gap-2 mb-2">
                {t.LogoURL && !kirikLogo.has(t.Kod) ? (
                  <img src={t.LogoURL} alt="" className="size-8 object-contain shrink-0"
                    onError={() => setKirikLogo((s) => new Set(s).add(t.Kod))} />
                ) : (
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-200" aria-hidden="true">
                    <Ikon d={I.kutu} className="size-[18px]" />
                  </span>
                )}
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate text-slate-800 dark:text-slate-100">{t.Ad}</div>
                  <div className="text-tiny-plus text-slate-500 dark:text-slate-400">v{t.Surum} · {t.Kategori}</div>
                </div>
                {t.NativeApp && (
                  <Badge color="warning" variant="soft" className="text-tiny-plus px-1.5 py-0.5 whitespace-nowrap">{cevir('yakında')}</Badge>
                )}
              </div>
              <p className="text-xs text-slate-600 dark:text-slate-300 mb-3">{t.Aciklama}</p>
              <Button
                color="primary"
                boyut="sm"
                ikon={t.NativeApp ? I.kilit : I.oynat}
                onClick={() => t.NativeApp
                  ? void dialog.bilgi({ baslik: t.Ad + ' ' + cevir('host seviyesinde bir uygulamadır, tenant panelinden kurulamaz.') })
                  : setKurTaslak({ kod: t.Kod, ad: t.Ad, alt_dizin: t.Kod })}
                disabled={t.NativeApp}
                className="w-full">
                {t.NativeApp ? cevir("Admin'den kurulur") : cevir('Kur')}
              </Button>
            </Card>
          ))}
        </div>
      </section>

      <Modal acik={!!kurTaslak} baslik={kurTaslak ? `${kurTaslak.ad} ${cevir('Kur')}` : ''}
        onKapat={() => setKurTaslak(null)} kapatilamaz={gonderiliyor}
        alt={<>
          <Button variant="outlined" ikon={I.kapat} onClick={() => setKurTaslak(null)} disabled={gonderiliyor}>{cevir('Vazgeç')}</Button>
          <Button color="primary" ikon={I.oynat} onClick={kur} yukleniyor={gonderiliyor}>
            {gonderiliyor ? cevir('Kuruluyor…') : cevir('Kur')}
          </Button>
        </>}>
        {kurTaslak && (
          <>
            <Input
              label={cevir('Alt dizin (public_html/... altına)')}
              value={kurTaslak.alt_dizin}
              onChange={(e) => setKurTaslak({ ...kurTaslak, alt_dizin: e.target.value })}
              placeholder={cevir('boş = kök, blog, shop vb.')}
              autoComplete="off"
              spellCheck={false}
              classNames={{ root: 'mb-4', input: 'font-mono text-sm' }}
            />
            <p className="text-xs text-slate-500 dark:text-slate-400">
              {cevir('Site:')} <span className="font-mono">https://{domain.alan_adi}/{kurTaslak.alt_dizin}</span>
            </p>
          </>
        )}
      </Modal>
    </div>
  )
}
