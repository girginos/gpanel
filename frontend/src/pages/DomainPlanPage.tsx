import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button, Badge, Card, butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { useRol } from '@/lib/rol'
import { useDialog } from '@/components/Dialog'
import { limitGetir, type LimitYanit } from '@/lib/limit'
import OzelLimitDuzenleyici from '@/components/OzelLimitDuzenleyici'

type Plan = {
  id: number; ad: string; aciklama: string
  disk_kota_mb: number; trafik_kota_mb: number
  max_db: number; max_ftp: number; max_email: number; saatlik_mail_limiti: number; mail_kutu_kota_mb: number
  cpu_yuzde: number; ram_mb: number; inode_kota: number; php_surum: string
  varsayilan: boolean
  // Sunucu görünüm işaretleri: bayiye sahibi olmadığı (kökün paylaşılan) plan
  // yalnız seçim özetiyle ve düzenlenemez döner; domain_id = hostinge özel plan.
  ozet?: boolean; duzenlenebilir?: boolean; domain_id?: number
  [k: string]: unknown // PUT'ta bilinmeyen alanlar KAYBOLMASIN (waf, io, nginx…)
}
type Domain = { id: number; alan_adi: string; plan_id?: number; plan_ad?: string }

// Plan "ağırlığı": yükseltme mi düşürme mi olduğunu göstermek için kaba bir puan.
// Sınırsız (0) alanlar en yükseğe sayılır — aksi halde "sınırsız" düşürme görünür.

const DPLAN_EN: Record<string, string> = {
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Türkçe": "English",
  "Domainler": "Domains",
  "Bu hostingin paketini yükseltin ya da düşürün; gerekirse yalnız bu hostinge özel limitler belirleyin (katalogda yeni plan oluşmaz).": "Upgrade or downgrade this hosting's package; if needed, set limits specific to this hosting only (no new plan is added to the catalog).",
  "Özel limitler": "Custom limits",
  "Plan atanmamış · özel limitler": "No plan assigned · custom limits",
  "Bu hostingte plandan farklı limitler var; yalnız bu hostingi etkiler ve plan değişince korunur.": "This hosting has limits that differ from its plan; they affect only this hosting and are kept when the plan changes.",
  "Limitler plandan gelir. Yalnız bu hosting için farklı değer gerekiyorsa “Limitleri özelleştir”i kullanın — başka hostingler etkilenmez.": "Limits come from the plan. If this hosting alone needs different values, use “Customize limits” — other hostings are not affected.",
  "Bu hosting eski bir özel plan kopyasında. Bir katalog planı seçtiğinizde kopya kaldırılır; özel limitleri aşağıdan yeniden girebilirsiniz.": "This hosting is on an old custom plan copy. When you pick a catalog plan the copy is removed; you can re-enter custom limits below.",
  "Özel limitler korunur": "Custom limits are kept",
  "Bu hostinge özel {0} limit yeni planda da geçerli kalır: {1}. Plana geçilsin mi?": "{0} limit(s) specific to this hosting stay in effect on the new plan: {1}. Switch plan?",
  "Plana geç": "Switch plan",
  "Vazgeç": "Cancel",
  "Hosting Planı": "Hosting Plan",
  "Limitleri özelleştir": "Customize limits",
  "Tüm ayarlar": "All settings",
  "Aktif": "Active",
  "Mevcut plan": "Current plan",
  "Disk": "Disk",
  "Trafik": "Traffic",
  "Planlara git": "Go to plans",
  "Uygulanıyor…": "Applying…",
  "planı uygulandı. Kaynak limitleri arka planda güncelleniyor.": "plan applied. Resource limits are being updated in the background.",
  "Açıklama yok": "No description",
  "Bu plana geç": "Switch to this plan",
  "Kullanımda": "In use",
  "Plan atanmamış": "No plan assigned",
  "Plan değiştirilemedi": "Failed to change plan",
  "Planlar yükleniyor": "Loading plans",
  "Tanımlı plan yok": "No plans defined",
  "ağırlığı": "weight",
  "düşür": "decrease",
  "geç": "switch",
  "sınırsız": "unlimited",
  "yükselt": "increase",
  "Önce bir hosting planı oluşturun.": "Create a hosting plan first.",
  "Yükselt": "Upgrade",
  "Paylaşılan plan": "Shared plan",
  "Düşür": "Downgrade",
}
const cevir = (tr: string, ...a: (string | number)[]): string => {
  let s = i18n.language === "en" ? (DPLAN_EN[tr] || ORTAK_EN[tr] || tr) : tr
  a.forEach((v, i) => { s = s.split('{' + i + '}').join(String(v)) })
  return s
}

function puan(p: Plan) {
  const s = (v: number) => (v <= 0 ? 1_000_000 : v)
  return s(p.disk_kota_mb) + s(p.trafik_kota_mb) / 10 + s(p.ram_mb) * 2 + s(p.cpu_yuzde) * 4
}
const mb = (v: number) => (v <= 0 ? cevir('sınırsız') : v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`)
const adet = (v: number) => (v <= 0 ? cevir('sınırsız') : String(v))

export default function DomainPlanPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const { onay } = useDialog()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [limit, setLimit] = useState<LimitYanit | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState<number | null>(null)
  // "Bu hostinge özel limitler" düzenleyicisi açık mı?
  const [duzenle, setDuzenle] = useState(false)
  // Mail eklentisi aktif+ödemeli ise posta limiti alanları görünür.
  const [mailAktif, setMailAktif] = useState(false)
  // Müşteri plan tanımlayamaz (/plans AdminVeyaReseller); boş durumdaki
  // 'Planlara git' ona gösterilmez.
  const musteri = useRol() === 'user'

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    Promise.all([
      api.get<Domain>(`/domains/${id}`),
      // Yalnız KATALOG planları (hostinge özel limitler plan kopyası değil, üst yazım).
      api.get<Plan[]>('/plans'),
      limitGetir(id).catch(() => null),
    ])
      .then(([d, p, l]) => { setDomain(d.data); setPlanlar(p.data || []); setLimit(l) })
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
    // Mail eklentisi aktif mi? (posta limiti alanlarının gate'i)
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
  }
  useEffect(yukle, [id])

  const mevcut = planlar.find(p => p.id === domain?.plan_id) || null
  const mevcutPuan = mevcut ? puan(mevcut) : -1
  // Düzenleme/“Tüm ayarlar” yalnız planın sahibine (eski yanıtta işaret yoksa düzenlenebilir).
  const mevcutDuzenlenebilir = mevcut?.duzenlenebilir !== false
  const ozelAlanlar = (limit?.alanlar || []).filter(a => a.ust !== null || a.kilitli)
  const ef = (ad: string): number | null => {
    const a = limit?.alanlar.find(x => x.ad === ad)
    return a ? a.efektif : null
  }

  async function planUygula(p: Plan) {
    if (!id) return
    // Özel limitler plan değişiminde KORUNUR — kullanıcı bunu bilerek geçsin.
    if (ozelAlanlar.length > 0 && !(await onay({
      baslik: cevir("Özel limitler korunur"),
      mesaj: cevir("Bu hostinge özel {0} limit yeni planda da geçerli kalır: {1}. Plana geçilsin mi?",
        ozelAlanlar.length, ozelAlanlar.map(a => a.ad).join(', ')),
      ton: 'bilgi', onayEtiketi: cevir("Plana geç"), iptalEtiketi: cevir("Vazgeç"), onayIkonu: I.onay,
    }))) return
    setIsleniyor(p.id); setHata(null)
    try {
      await api.put(`/domains/${id}/plan`, { plan_id: p.id })
      const m = `"${p.ad}" ${cevir("planı uygulandı. Kaynak limitleri arka planda güncelleniyor.")}`
      toast.basari(cevir("Kaydedildi"), m)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Plan değiştirilemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally { setIsleniyor(null) }
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '…', href: `/abonelikler/${id}` },
        { etiket: cevir("Hosting Planı") },
      ]} />

      <div className="mb-4">
        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">{cevir("Hosting Planı")}</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          {cevir("Bu hostingin paketini yükseltin ya da düşürün; gerekirse yalnız bu hostinge özel limitler belirleyin (katalogda yeni plan oluşmaz).")}
        </p>
      </div>

      {hata && !yuk && (
        <p role="alert" className="mb-4 rounded-md border border-red-200 dark:border-red-900 bg-red-50 dark:bg-red-950/30 px-3 py-2 text-sm text-red-700 dark:text-red-300">{hata}</p>
      )}

      {yuk ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label={cevir("Planlar yükleniyor")}>
          {Array.from({ length: 3 }).map((_, i) => <div key={i} className="h-44 rounded-lg bg-slate-100 dark:bg-dark-700 animate-pulse" />)}
        </div>
      ) : (
        <>
          {/* Mevcut plan + aksiyonlar */}
          <Card className="mb-5 bg-white dark:bg-dark-800 p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">{cevir("Mevcut plan")}</div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-base font-semibold text-slate-900 dark:text-slate-100 break-words">
                    {limit?.plan?.ad || mevcut?.ad || domain?.plan_ad || (ozelAlanlar.length > 0 ? cevir("Plan atanmamış · özel limitler") : cevir("Plan atanmamış"))}
                  </span>
                  {ozelAlanlar.length > 0 && (
                    <Badge color="primary" variant="soft" className="text-[10px] uppercase font-semibold tracking-wider px-1.5 py-0.5">{cevir("Özel limitler")}</Badge>
                  )}
                </div>
                {limit && (
                  <dl className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-sm text-slate-600 dark:text-slate-300 tabular-nums">
                    <div><dt className="inline text-slate-400 dark:text-slate-500">{cevir("Disk")} </dt><dd className="inline font-medium">{ef('disk_kota_mb') === null ? '—' : mb(ef('disk_kota_mb') as number)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">{cevir("Trafik")} </dt><dd className="inline font-medium">{ef('trafik_kota_mb') === null ? '—' : mb(ef('trafik_kota_mb') as number)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">RAM </dt><dd className="inline font-medium">{ef('ram_mb') === null ? '—' : mb(ef('ram_mb') as number)}</dd></div>
                    <div><dt className="inline text-slate-400 dark:text-slate-500">CPU </dt><dd className="inline font-medium">{ef('cpu_yuzde') === null ? '—' : `%${ef('cpu_yuzde')}`}</dd></div>
                  </dl>
                )}
              </div>
              <div className="flex flex-wrap gap-2">
                {limit && !musteri && (
                  <Button type="button" color="primary" ikon={I.kalem} onClick={() => setDuzenle(true)} disabled={isleniyor !== null || duzenle}>
                    {cevir("Limitleri özelleştir")}
                  </Button>
                )}
                {mevcut && mevcutDuzenlenebilir && (
                  <Link to={`/araclar/paketler/${mevcut.id}`}
                        className={butonSinifi({ variant: 'outlined' })}>
                    <Ikon d={I.ayar} className="size-4 shrink-0 -ms-1 me-2" />
                    {cevir("Tüm ayarlar")}
                  </Link>
                )}
              </div>
            </div>
            <p className="mt-2 text-xs text-slate-400 dark:text-slate-500">
              {ozelAlanlar.length > 0
                ? cevir("Bu hostingte plandan farklı limitler var; yalnız bu hostingi etkiler ve plan değişince korunur.")
                : cevir("Limitler plandan gelir. Yalnız bu hosting için farklı değer gerekiyorsa “Limitleri özelleştir”i kullanın — başka hostingler etkilenmez.")}
            </p>
            {limit?.eski_ozel_kopya && (
              <p role="status" className="mt-2 flex items-start gap-1.5 rounded-md border border-amber-200 dark:border-amber-900 bg-amber-50 dark:bg-amber-950/30 px-2.5 py-1.5 text-xs text-amber-800 dark:text-amber-200">
                <Ikon d={I.uyari} className="mt-0.5 size-3.5 shrink-0" />
                {cevir("Bu hosting eski bir özel plan kopyasında. Bir katalog planı seçtiğinizde kopya kaldırılır; özel limitleri aşağıdan yeniden girebilirsiniz.")}
              </p>
            )}
          </Card>

          {/* Bu hostinge özel limitler (üst yazım) */}
          {duzenle && limit && id && (
            <OzelLimitDuzenleyici domainId={id} veri={limit} mailAktif={mailAktif}
                                  onDegisti={v => { setLimit(v); yukle() }} onKapat={() => setDuzenle(false)} />
          )}

          {planlar.length === 0 ? (
            <div role="status" className="text-center py-12 rounded-lg border border-dashed border-slate-300 dark:border-dark-600">
              <h3 className="text-sm font-medium text-slate-700 dark:text-slate-200">{cevir("Tanımlı plan yok")}</h3>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{cevir("Önce bir hosting planı oluşturun.")}</p>
              {/* Hosting planları /hizmet-planlari'dedir (/araclar/paketler RPM paket yöneticisi). */}
              {!musteri && (
                <Link to="/hizmet-planlari" className={butonSinifi({ color: 'primary', className: 'mt-4' })}>{cevir("Planlara git")}<Ikon d={I.ileri} className="size-4 shrink-0 ms-2 -me-1" /></Link>
              )}
            </div>
          ) : (
            <ul role="list" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {[...planlar].sort((a, b) => puan(a) - puan(b)).map(p => {
                const bu = p.id === domain?.plan_id
                const fark = puan(p) - mevcutPuan
                const yon = !mevcut ? 'geç' : fark > 0 ? 'yükselt' : fark < 0 ? 'düşür' : 'geç'
                return (
                  <li key={p.id} className={`rounded-lg border p-4 flex flex-col gap-3 ${bu ? 'border-brand-500 dark:border-brand-500 bg-brand-50/40 dark:bg-brand-950/20' : 'border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800'}`}>
                    <div>
                      <div className="flex items-start justify-between gap-2">
                        <h3 className="font-semibold text-slate-900 dark:text-slate-100">{p.ad}</h3>
                        {bu && <Badge color="primary" variant="soft" className="shrink-0 text-[10px] uppercase font-semibold tracking-wider px-1.5 py-0.5">{cevir("Aktif")}</Badge>}
                      </div>
                      <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400 line-clamp-2">{p.ozet ? cevir("Paylaşılan plan") : (p.aciklama || cevir("Açıklama yok"))}</p>
                    </div>
                    <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-sm text-slate-600 dark:text-slate-300 tabular-nums">
                      <div><dt className="inline text-slate-400 dark:text-slate-500">{cevir("Disk")} </dt><dd className="inline font-medium">{mb(p.disk_kota_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">{cevir("Trafik")} </dt><dd className="inline font-medium">{mb(p.trafik_kota_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">RAM </dt><dd className="inline font-medium">{mb(p.ram_mb)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">CPU </dt><dd className="inline font-medium">{p.cpu_yuzde <= 0 ? '∞' : `%${p.cpu_yuzde}`}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">DB </dt><dd className="inline font-medium">{adet(p.max_db)}</dd></div>
                      <div><dt className="inline text-slate-400 dark:text-slate-500">FTP </dt><dd className="inline font-medium">{adet(p.max_ftp)}</dd></div>
                    </dl>
                    <div className="mt-auto flex gap-2">
                      {bu ? (
                        <span className="flex-1 rounded-md border border-slate-200 dark:border-dark-600 px-3 py-1.5 text-sm text-center text-slate-400 dark:text-slate-500">{cevir("Kullanımda")}</span>
                      ) : (
                        <Button type="button" color={yon === 'düşür' ? 'neutral' : 'primary'}
                                onClick={() => planUygula(p)} disabled={isleniyor !== null}
                                yukleniyor={isleniyor === p.id} boyut="sm"
                                ikon={yon === 'yükselt' ? I.yukari : yon === 'düşür' ? I.asagi : I.onay}
                                className="w-full">
                          {isleniyor === p.id ? cevir('Uygulanıyor…') : yon === 'yükselt' ? cevir('Yükselt') : yon === 'düşür' ? cevir('Düşür') : cevir("Bu plana geç")}
                        </Button>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
