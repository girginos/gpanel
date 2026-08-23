import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Paket = {
  id?: number; ad: string; aciklama: string
  max_domain: number; max_disk_mb: number; max_trafik_mb: number
  // Posta TAVANLARI — domain basina ust sinir (havuz DEGIL). 0 = sinirsiz.
  mail_max_email: number; mail_saatlik_limit: number; mail_kutu_kota_mb: number
  fiyat_kurus: number
  asim_ilkesi: 'yok' | 'disk_trafik' | 'tumu'
  asim_bildirim: boolean
  fazla_satis: boolean
  varsayilan: boolean
  bayi_sayisi?: number
}

const BOS: Paket = {
  ad: '', aciklama: '',
  max_domain: 10, max_disk_mb: 10240, max_trafik_mb: 102400, fiyat_kurus: 0,
  mail_max_email: 0, mail_saatlik_limit: 0, mail_kutu_kota_mb: 0,
  asim_ilkesi: 'disk_trafik', asim_bildirim: true, fazla_satis: false, varsayilan: false,
}

const inp = 'w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 tabular-nums'


const BPDUZ_EN: Record<string, string> = {
  "0 = sınırsız. Bu limitler bayiye atandığı anda kopyalanır; planı sonradan değiştirmek mevcut bayileri etkilemez.": "0 = unlimited. These limits are copied when assigned to a reseller; changing the plan later does not affect existing resellers.",
  "Bayi, hosting planlarının kotaları toplamı paketini aşacak şekilde hesap açamaz (taahhüt kontrolü).": "A reseller cannot open accounts exceeding the sum of the hosting plan quotas in their package (commitment check).",
  "Bayinin BİR domaine tanımlayabileceği en fazla kutu sayısı. 0 = sınırsız": "The maximum number of mailboxes the reseller can define for ONE domain. 0 = unlimited",
  "Bayinin açabileceği hosting hesabı sayısı": "The number of hosting accounts the reseller can open",
  "Bayinin hesapları limitleri aştığında ne olacağını belirler.": "Determines what happens when the reseller's accounts exceed the limits.",
  "Bayinin, kendisine ayrılandan daha fazla kaynağı müşterilerine satıp satamayacağını belirler.": "Determines whether the reseller can sell more resources to their customers than allocated to them.",
  "Disk alanı ve trafiği fazla kullanıma izin verilir": "Overuse of disk space and traffic is allowed",
  "Disk ve trafikte esneklik tanınır; diğer kaynaklarda (hosting adedi vb.) sınır serttir.": "Flexibility is granted for disk and traffic; for other resources (hosting count etc.) the limit is hard.",
  "Domain başına en fazla giden mail/saat. 0 = sınırsız": "Maximum outgoing mail/hour per domain. 0 = unlimited",
  "Fazla kullanım durumlarında beni e-postayla bilgilendir.": "Notify me by email in case of overuse.",
  "Fazla kullanım ilkesi": "Overuse policy",
  "Fazla kullanıma izin verilmez": "Overuse is not allowed",
  "Fiyat ve varsayılan": "Price and default",
  "Kaynak kullanımı sınır değerlerini aştığında yeni hosting açılamaz ve yükseltme reddedilir.": "When resource usage exceeds the limits, no new hosting can be opened and upgrades are rejected.",
  "Kutu depolama tavanı (MB)": "Mailbox storage cap (MB)",
  "Planı oluştur": "Create plan",
  "Posta kutusu tavanı": "Mailbox cap",
  "Saatlik gönderim tavanı": "Hourly sending cap",
  "Taahhüt yerine gerçek kullanım esas alınır; bayi kağıt üzerinde paketinden fazlasını satabilir.": "Actual usage is used instead of commitment; the reseller can sell more than their package on paper.",
  "Tüm kaynaklarda fazla kullanıma izin verilir": "Overuse is allowed for all resources",
  "Varsayılan plan — yeni bayi oluştururken önceden seçili gelir.": "Default plan — pre-selected when creating a new reseller.",
  "Önerilmez: bayi paketinde tanımlı hiçbir limit sert biçimde uygulanmaz.": "Not recommended: none of the limits defined in the reseller package are hard-enforced.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BPDUZ_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function BayiPlaniDuzenlePage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const nav = useNavigate()
  const yeni = !id || id === 'yeni'
  const [p, setP] = useState<Paket>(BOS)
  const [yuk, setYuk] = useState(!yeni)
  const [hata, setHata] = useState<string | null>(null)
  const [kaydediyor, setKaydediyor] = useState(false)
  // Mail eklentisi aktifse posta tavani alanlari gorunur (ayni kapi).
  const [mailAktif, setMailAktif] = useState(false)

  useEffect(() => {
    // 🔴 mailAktif YENİ planda da gerekli: eski kod `if (yeni) return`'i eklenti
    // kontrolünden ÖNCE yapıyordu → yeni bayi planı oluştururken mail bölümü hiç
    // görünmüyordu (mailAktif false kalıyordu). Eklenti kontrolü HER ZAMAN çalışır;
    // yalnız mevcut planı yükleme (yeni değilken) atlanır.
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
    if (yeni) return
    api.get<Paket>(`/reseller-plans/${id}`)
      .then(r => setP(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }, [id, yeni])

  function S<K extends keyof Paket>(k: K, v: Paket[K]) { setP({ ...p, [k]: v }) }

  async function kaydet(e: React.FormEvent) {
    e.preventDefault()
    setKaydediyor(true); setHata(null)
    try {
      if (yeni) await api.post('/reseller-plans', p)
      else await api.put(`/reseller-plans/${id}`, p)
      nav('/bayi-planlari')
    } catch (err) {
      setHata(apiHata(err, 'Kaydedilemedi'))
      setKaydediyor(false)
    }
  }

  if (yuk) return <div className="px-4 py-6 sm:px-6"><div className="h-64 rounded-lg bg-slate-100 dark:bg-slate-800 animate-pulse" /></div>

  return (
    <form onSubmit={kaydet} className="px-4 py-4 sm:px-6 sm:py-5 max-w-4xl">
      <Breadcrumb items={[{ etiket: cevir("Bayi Planları"), href: '/bayi-planlari' }, { etiket: yeni ? 'Yeni plan' : p.ad || '…' }]} />

      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
            {yeni ? 'Yeni Bayi Planı' : p.ad}
          </h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">
            {cevir(cevir("Bayiye satılan kaynak paketi: limitler, aşım ve fazla satma ilkeleri."))}
          </p>
        </div>
        {!yeni && !!p.bayi_sayisi && (
          <span className="rounded-md bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-xs text-slate-600 dark:text-slate-300 tabular-nums">
            {p.bayi_sayisi} bayi kullanıyor
          </span>
        )}
      </div>

      {hata && <div role="alert" className="mb-4 text-sm rounded-md border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-950/30 text-red-700 dark:text-red-300 px-3 py-2">{hata}</div>}

      <div className="space-y-5">
        <Bolum baslik={cevir("Tanım")}>
          <div className="grid gap-4 sm:grid-cols-2">
            <Alan etiket={cevir("Plan adı")} gerekli>
              <input className={inp} required maxLength={100} placeholder="Bronz Bayi"
                     value={p.ad} onChange={e => S('ad', e.target.value)} />
            </Alan>
            <Alan etiket={cevir("Açıklama")}>
              <input className={inp} maxLength={255} placeholder={cevir("Başlangıç paketi")}
                     value={p.aciklama} onChange={e => S('aciklama', e.target.value)} />
            </Alan>
          </div>
        </Bolum>

        <Bolum baslik="Kaynak limitleri" alt={cevir("0 = sınırsız. Bu limitler bayiye atandığı anda kopyalanır; planı sonradan değiştirmek mevcut bayileri etkilemez.")}>
          <div className="grid gap-4 sm:grid-cols-3">
            <Alan etiket="Domain (hosting) limiti" ipucu={cevir("Bayinin açabileceği hosting hesabı sayısı")}>
              <input type="number" min={0} className={inp} value={p.max_domain}
                     onChange={e => S('max_domain', Number(e.target.value))} />
            </Alan>
            <Alan etiket="Disk limiti (MB)" ipucu={p.max_disk_mb > 0 ? `${(p.max_disk_mb / 1024).toFixed(1)} GB` : cevir("sınırsız")}>
              <input type="number" min={0} className={inp} value={p.max_disk_mb}
                     onChange={e => S('max_disk_mb', Number(e.target.value))} />
            </Alan>
            <Alan etiket="Trafik limiti (MB/ay)" ipucu={p.max_trafik_mb > 0 ? `${(p.max_trafik_mb / 1024).toFixed(1)} GB` : cevir("sınırsız")}>
              <input type="number" min={0} className={inp} value={p.max_trafik_mb}
                     onChange={e => S('max_trafik_mb', Number(e.target.value))} />
            </Alan>
            {/* 🔴 Bu üç alan TAVAN'dır, havuz DEĞİL: yukarıdaki domain/disk/trafik
                bayinin toplam kaynağıdır ve tükenir; aşağıdakiler ise bayinin
                TEK BİR domaine atayabileceği en yüksek değerdir, tükenmez.
                Aşan değer backend'de REDDEDİLİR (plans.go bayiTavanKontrol). */}
            {mailAktif && (
              <>
            <Alan etiket={cevir("Posta kutusu tavanı")} ipucu={cevir("Bayinin BİR domaine tanımlayabileceği en fazla kutu sayısı. 0 = sınırsız")}>
              <input type="number" min={0} className={inp} value={p.mail_max_email}
                     onChange={e => S('mail_max_email', Number(e.target.value))} />
            </Alan>
            <Alan etiket={cevir("Saatlik gönderim tavanı")} ipucu={cevir("Domain başına en fazla giden mail/saat. 0 = sınırsız")}>
              <input type="number" min={0} className={inp} value={p.mail_saatlik_limit}
                     onChange={e => S('mail_saatlik_limit', Number(e.target.value))} />
            </Alan>
            <Alan etiket={cevir("Kutu depolama tavanı (MB)")} ipucu={p.mail_kutu_kota_mb > 0 ? cevirT(cevir("kutu başına en fazla {0} GB"), (p.mail_kutu_kota_mb / 1024).toFixed(1)) : cevir("sınırsız")}>
              <input type="number" min={0} className={inp} value={p.mail_kutu_kota_mb}
                     onChange={e => S('mail_kutu_kota_mb', Number(e.target.value))} />
            </Alan>
              </>
            )}
          </div>
        </Bolum>

        <Bolum baslik={cevir("Fazla kullanım ilkesi")} alt={cevir("Bayinin hesapları limitleri aştığında ne olacağını belirler.")}>
          <fieldset className="space-y-3">
            <legend className="sr-only">{cevir("Fazla kullanım ilkesi")}</legend>
            <Secenek
              secili={p.asim_ilkesi === 'yok'} onSec={() => S('asim_ilkesi', 'yok')}
              baslik={cevir("Fazla kullanıma izin verilmez")}
              aciklama={cevir("Kaynak kullanımı sınır değerlerini aştığında yeni hosting açılamaz ve yükseltme reddedilir.")}
            />
            <Secenek
              secili={p.asim_ilkesi === 'disk_trafik'} onSec={() => S('asim_ilkesi', 'disk_trafik')}
              baslik={cevir("Disk alanı ve trafiği fazla kullanıma izin verilir")}
              aciklama={cevir("Disk ve trafikte esneklik tanınır; diğer kaynaklarda (hosting adedi vb.) sınır serttir.")}
            />
            <Secenek
              secili={p.asim_ilkesi === 'tumu'} onSec={() => S('asim_ilkesi', 'tumu')}
              baslik={cevir("Tüm kaynaklarda fazla kullanıma izin verilir")}
              aciklama={cevir("Önerilmez: bayi paketinde tanımlı hiçbir limit sert biçimde uygulanmaz.")}
            />
            <label className="flex items-start gap-2.5 pl-7 text-sm text-slate-600 dark:text-slate-300">
              <input type="checkbox" className="mt-0.5 rounded border-slate-300 dark:border-slate-600"
                     checked={p.asim_bildirim} onChange={e => S('asim_bildirim', e.target.checked)} />
              <span>{cevir("Fazla kullanım durumlarında beni e-postayla bilgilendir.")}</span>
            </label>
          </fieldset>
        </Bolum>

        <Bolum baslik="Fazla satma ilkesi" alt={cevir("Bayinin, kendisine ayrılandan daha fazla kaynağı müşterilerine satıp satamayacağını belirler.")}>
          <fieldset className="space-y-3">
            <legend className="sr-only">Fazla satma ilkesi</legend>
            <Secenek
              secili={!p.fazla_satis} onSec={() => S('fazla_satis', false)}
              baslik="Fazla satmaya izin verilmez"
              aciklama={cevir("Bayi, hosting planlarının kotaları toplamı paketini aşacak şekilde hesap açamaz (taahhüt kontrolü).")}
            />
            <Secenek
              secili={p.fazla_satis} onSec={() => S('fazla_satis', true)}
              baslik="Fazla satmaya izin verilir"
              aciklama={cevir("Taahhüt yerine gerçek kullanım esas alınır; bayi kağıt üzerinde paketinden fazlasını satabilir.")}
            />
          </fieldset>
        </Bolum>

        <Bolum baslik={cevir("Fiyat ve varsayılan")}>
          <div className="grid gap-4 sm:grid-cols-2">
            <Alan etiket={cevir("Fiyat (kuruş)")} ipucu={p.fiyat_kurus > 0 ? `${(p.fiyat_kurus / 100).toFixed(2)} ₺` : cevir("belirtilmemiş")}>
              <input type="number" min={0} className={inp} value={p.fiyat_kurus}
                     onChange={e => S('fiyat_kurus', Number(e.target.value))} />
            </Alan>
            <label className="flex items-start gap-2.5 self-end pb-2 text-sm text-slate-600 dark:text-slate-300">
              <input type="checkbox" className="mt-0.5 rounded border-slate-300 dark:border-slate-600"
                     checked={p.varsayilan} onChange={e => S('varsayilan', e.target.checked)} />
              <span>{cevir("Varsayılan plan — yeni bayi oluştururken önceden seçili gelir.")}</span>
            </label>
          </div>
        </Bolum>
      </div>

      <div className="mt-6 flex flex-wrap gap-2">
        <button type="submit" disabled={kaydediyor}
                className="inline-flex items-center rounded-md bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">
          {kaydediyor ? 'Kaydediliyor…' : yeni ? 'Planı oluştur' : 'Kaydet'}
        </button>
        <Link to="/bayi-planlari"
              className="inline-flex items-center rounded-md border border-slate-300 dark:border-slate-600 px-4 py-2 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800">
          {cevir("İptal")}
        </Link>
      </div>
    </form>
  )
}

function Bolum({ baslik, alt, children }: { baslik: string; alt?: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
      <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h2>
      {alt && <p className="mt-0.5 mb-3 text-xs text-slate-500 dark:text-slate-400 max-w-2xl">{alt}</p>}
      <div className={alt ? '' : 'mt-3'}>{children}</div>
    </section>
  )
}

function Alan({ etiket, ipucu, gerekli, children }: { etiket: string; ipucu?: string; gerekli?: boolean; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-slate-600 dark:text-slate-300 mb-1">
        {etiket}{gerekli && <span className="text-brand-600 dark:text-brand-400"> *</span>}
      </span>
      {children}
      {ipucu && <span className="mt-1 block text-[11px] text-slate-400 dark:text-slate-500">{ipucu}</span>}
    </label>
  )
}

function Secenek({ secili, onSec, baslik, aciklama }: { secili: boolean; onSec: () => void; baslik: string; aciklama: string }) {
  return (
    <label className={`flex gap-3 rounded-lg border p-3 cursor-pointer transition ${
      secili
        ? 'border-brand-500 bg-brand-50/40 dark:bg-brand-950/20'
        : 'border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-800/60'
    }`}>
      <input type="radio" checked={secili} onChange={onSec} className="mt-0.5 shrink-0" />
      <span>
        <span className="block text-sm font-medium text-slate-900 dark:text-slate-100">{baslik}</span>
        <span className="block text-xs text-slate-500 dark:text-slate-400 mt-0.5">{aciklama}</span>
      </span>
    </label>
  )
}
