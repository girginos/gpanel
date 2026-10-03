import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import ListToolbar from '@/components/ListToolbar'
import EmptyState from '@/components/EmptyState'
import Modal from '@/components/Modal'
import ConfirmDialog from '@/components/ConfirmDialog'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, butonSinifi } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { useBayiSinir, havuzDurumu, mbOkunur, type BayiSinir } from '@/lib/bayiPlan'
import { cevirT } from '@/lib/cevirT'

type Plan = {
  id: number
  ad: string
  aciklama: string
  disk_kota_mb: number
  trafik_kota_mb: number
  max_domain: number
  max_db: number
  max_email: number
  saatlik_mail_limiti: number
  mail_kutu_kota_mb: number
  max_ftp: number
  php_surum: string
  fastcgi_cache: boolean
  client_max_body_mb: number
  nginx_ek_direktifler: string
  varsayilan: boolean
  olusturulma: string
  // Sunucu görünüm işaretleri: bayiye sahibi olmadığı (kökün paylaşılan) plan
  // yalnız seçim özetiyle döner — düzenleme/silme öğeleri yalnız sahibine.
  global?: boolean
  duzenlenebilir?: boolean
  ozet?: boolean
}
type Surum = { surum: string; aciklama?: string }


const SVCPLAN_EN: Record<string, string> = {
  "Türkçe": "English",
  "Bilgi": "Info",
  "Anasayfa": "Home",
  "Plan Ekle": "Add Plan",
  "Trafik": "Traffic",
  "adet": "pcs",
  "hesap": "accounts",
  "MB/ay": "MB/month",
  "MB": "MB",
  "Detay & Kaynak Limitleri": "Detail & Resource Limits",
  "Detay": "Details",
  "Paylaşılan": "Shared",
  "Paylaşılan plan: hostinglerinize atayabilirsiniz; yalnız yönetici düzenler.": "Shared plan: you can assign it to your hostings; only the administrator can edit it.",
  "planı silinsin mi?": "be deleted?",
  "Evet, sil": "Yes, delete",
  "Yeni Plan": "New Plan",
  "Trafik (MB)": "Traffic (MB)",
  "E-posta (Mail eklentisi)": "Email (Mail add-on)",
  "E-posta kutusu": "Mailbox",
  "Kutu depolama (MB)": "Mailbox storage (MB)",
  "Kaydediliyor…": "Saving…",
  "Ekle": "Add",
  "Domainler için hizmet planları (paketler) tanımlayın. Her domain bir plana bağlanır;": "Define service plans (packages) for domains. Each domain is bound to a plan;",
  "disk, trafik, PHP sürümü, veritabanı ve alt domain limiti gibi kaynaklar paket başına ayarlanır.": "resources such as disk, traffic, PHP version, database and subdomain limit are set per package.",
  "Varsayılan": "Default",
  "Veritabanı": "Database",
  "Plan adı": "Plan name",
  "Açıklama": "Description",
  "PHP Sürümü": "PHP Version",
  "Güncelle": "Update",
  "Yeni domainlerde varsayılan plan": "Default plan for new domains",
  "0 = sınırsız. Disk/trafik MB cinsindendir. Bu plandaki yeni domainler seçili PHP sürümüyle kurulur.": "0 = unlimited. Disk/traffic are in MB. New domains on this plan are installed with the selected PHP version.",
  "Henüz hizmet planı yok": "No service plans yet",
  "Hizmet Planları": "Service Plans",
  "Kutu = posta kutusu sayısı · Saatlik = kutu başına giden mail/saat · Depolama = kutu başına disk (MB). 0 = sınırsız.": "Mailbox = number of mailboxes · Hourly = outgoing mail per mailbox/hour · Storage = disk per mailbox (MB). 0 = unlimited.",
  "PHP sürümleri alınamadı": "Failed to get PHP versions",
  "Planı Düzenle": "Edit Plan",
  "Planı sil": "Delete plan",
  "Saatlik gönderim": "Hourly sending",
  "İlk paketinizi tanımlayarak başlayın.": "Start by defining your first package.",
  "İşlem başarısız": "Operation failed",
  "Planlarım": "My plans",
  "Paylaşılan planlar": "Shared plans",
  "Düzenleyebileceğiniz planlar. Varsayılan plan yeni hostinglerde ön seçilir ve site taşımada plansız hesaplara uygulanır.": "Plans you can edit. The default plan is preselected for new hostings and applied to plan-less accounts in site migration.",
  "Yönetici tarafından tanımlanır: hostinglerinize atayabilirsiniz, düzenleyemezsiniz.": "Defined by the administrator: you can assign them to your hostings but cannot edit them.",
  "Varsayılan yap": "Make default",
  "Varsayılan plan değiştirildi": "Default plan changed",
  "Son planınız silinemez": "Your last plan cannot be deleted",
  "Henüz kendi planınız yok": "You have no plans of your own yet",
  "Paylaşılan plan yok": "No shared plans",
  "Bayi planında 0 (sınırsız) olamaz.": "Cannot be 0 (unlimited) in a reseller plan.",
  "Paketinizde kalan: {0} / {1}": "Left in your package: {0} / {1}",
  "plan değeri {0} altında olmalı": "plan value must be below {0}",
  "fazla satış açık": "overselling on",
  "Paket tavanı: {0}": "Package cap: {0}",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SVCPLAN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function ServicePlansPage() {
  useTranslation() // dil re-render aboneligi
  const { bilgi } = useDialog()
  const toast = useToast()
  const [items, setItems] = useState<Plan[]>([])
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [modal, setModal] = useState<Plan | null>(null)
  const [silinecek, setSilinecek] = useState<Plan | null>(null)
  const [sinirNesli, setSinirNesli] = useState(0)
  const sinir = useBayiSinir(sinirNesli)
  // Bayi: kendi planları (düzenlenebilir) + kökün paylaşılan planları (salt okunur).
  const bayi = !!sinir?.bayi || items.some(p => p.duzenlenebilir === false)
  const kendi = items.filter(p => p.duzenlenebilir !== false)
  const paylasilan = items.filter(p => p.duzenlenebilir === false)

  function yukle() {
    setYuk(true); setHata(null)
    api.get<Plan[]>('/plans')
      .then(r => setItems(r.data))
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])
  useEffect(() => {
    let iptal = false
    api.get<Surum[]>('/php/versions').then(r => { if (iptal) return; setSurumler(r.data || []) }).catch(e => { if (!iptal) hataYakala(cevir("PHP sürümleri alınamadı"))(e) })
    return () => { iptal = true }
  }, [])

  async function varsayilanYap(p: Plan) {
    try {
      await api.post(`/plans/${p.id}/varsayilan`)
      toast.basari(cevir("Varsayılan plan değiştirildi"), p.ad)
      yukle()
    } catch (e) {
      toast.hata(cevir("İşlem başarısız"), apiHata(e))
    }
  }

  async function sil() {
    if (!silinecek) return
    try {
      await api.delete(`/plans/${silinecek.id}`)
      setSilinecek(null); yukle(); setSinirNesli(n => n + 1)
    } catch (e) {
      (await bilgi({ baslik: cevir('Bilgi'), mesaj: apiHata(e, cevir("Silme başarısız")), ton: 'tehlike' }))
    }
  }

  // Plan kartı. sonPlan: bayinin tek kendi planı (sunucu silmeyi reddeder; düğme gizli).
  function kart(p: Plan, sonPlan: boolean) {
    // Eski sunucu yanıtında işaret yoksa düzenlenebilir say (geri uyum).
    const duzenlenebilir = p.duzenlenebilir !== false
    return (
      <div key={p.id} className={`bg-white dark:bg-dark-700 border rounded-lg p-5 shadow-xs ${p.varsayilan ? 'border-brand-400 ring-2 ring-brand-100 dark:ring-brand-900/40' : 'border-slate-200 dark:border-dark-600'}`}>
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="min-w-0">
            <h3 className="text-lg font-semibold text-slate-900 dark:text-slate-100 flex flex-wrap items-center gap-2 break-words">
              {p.ad}
              {p.varsayilan && <span className="text-[10px] uppercase tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded font-semibold">{cevir("Varsayılan")}</span>}
              {!duzenlenebilir && <span title={cevir("Paylaşılan plan: hostinglerinize atayabilirsiniz; yalnız yönetici düzenler.")} className="text-[10px] uppercase tracking-wider bg-slate-100 dark:bg-dark-600/60 text-slate-600 dark:text-slate-300 px-1.5 py-0.5 rounded font-semibold">{cevir("Paylaşılan")}</span>}
            </h3>
            {p.aciklama && <p className="text-sm text-slate-500 dark:text-slate-500 mt-0.5">{p.aciklama}</p>}
          </div>
          {p.php_surum && <span className="shrink-0 text-[11px] font-mono font-semibold bg-slate-100 dark:bg-dark-600/60 text-slate-600 dark:text-slate-300 px-2 py-0.5 rounded">PHP {p.php_surum}</span>}
        </div>

        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-y-1.5 text-sm mt-4">
          <Sat e="Disk" d={fmt(p.disk_kota_mb, 'MB')} />
          <Sat e={cevir("Trafik")} d={fmt(p.trafik_kota_mb, 'MB/ay')} />
          <Sat e="Domain" d={fmt(p.max_domain, 'adet')} />
          <Sat e={cevir("Veritabanı")} d={fmt(p.max_db, 'adet')} />
          <Sat e="FTP" d={fmt(p.max_ftp, 'hesap')} />
        </dl>

        <div className="mt-4 flex flex-wrap gap-2">
          <Link to={`/araclar/paketler/${p.id}`} className={butonSinifi({ color: 'primary', variant: 'soft', boyut: 'sm', className: 'flex-1' })}>
            {duzenlenebilir ? cevir("Detay & Kaynak Limitleri") : cevir("Detay")}
            <Ikon d={I.ileri} className="size-3.5 shrink-0 ms-1.5 -me-0.5" />
          </Link>
          {duzenlenebilir && bayi && !p.varsayilan && <Button onClick={() => varsayilanYap(p)} variant="outlined" boyut="sm" ikon={I.onay}>{cevir("Varsayılan yap")}</Button>}
          {duzenlenebilir && !sonPlan && <Button onClick={() => setSilinecek(p)} color="error" variant="soft" boyut="sm" ikon={I.cop}>{cevir("Sil")}</Button>}
          {duzenlenebilir && sonPlan && <span className="self-center text-[11px] text-slate-400 dark:text-slate-500">{cevir("Son planınız silinemez")}</span>}
        </div>
      </div>
    )
  }

  return (
    <div>
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Hizmet Planları") }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-2">{cevir("Hizmet Planları")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
        {cevir("Domainler için hizmet planları (paketler) tanımlayın. Her domain bir plana bağlanır;")}
        {cevir("disk, trafik, PHP sürümü, veritabanı ve alt domain limiti gibi kaynaklar paket başına ayarlanır.")}
      </p>

      <ListToolbar
        birincil={{ etiket: cevir('Plan Ekle'), onClick: () => setModal({} as Plan) }}
        butonlar={[]}
      />

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : items.length === 0 ? (
        <EmptyState
          baslik={cevir("Henüz hizmet planı yok")}
          aciklama={cevir("İlk paketinizi tanımlayarak başlayın.")}
          buton={{ etiket: cevir('Plan Ekle'), onClick: () => setModal({} as Plan) }}
        />
      ) : bayi ? (
        <div className="space-y-8">
          <section aria-labelledby="planlarim-baslik">
            <h2 id="planlarim-baslik" className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Planlarım")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 mb-3">{cevir("Düzenleyebileceğiniz planlar. Varsayılan plan yeni hostinglerde ön seçilir ve site taşımada plansız hesaplara uygulanır.")}</p>
            {kendi.length === 0
              ? <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz kendi planınız yok")}</p>
              : <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">{kendi.map(p => kart(p, kendi.length <= 1))}</div>}
          </section>
          <section aria-labelledby="paylasilan-baslik">
            <h2 id="paylasilan-baslik" className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Paylaşılan planlar")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 mb-3">{cevir("Yönetici tarafından tanımlanır: hostinglerinize atayabilirsiniz, düzenleyemezsiniz.")}</p>
            {paylasilan.length === 0
              ? <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Paylaşılan plan yok")}</p>
              : <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">{paylasilan.map(p => kart(p, false))}</div>}
          </section>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map(p => kart(p, false))}
        </div>
      )}

      {modal && (
        <PlanModal
          plan={modal}
          surumler={surumler}
          sinir={sinir}
          onKapat={() => setModal(null)}
          onKayit={() => { setModal(null); yukle(); setSinirNesli(n => n + 1) }}
        />
      )}

      <ConfirmDialog
        acik={!!silinecek}
        baslik={cevir("Planı sil")}
        mesaj={`"${silinecek?.ad}" ${cevir("planı silinsin mi?")}`}
        ton="tehlike"
        onayIkonu={I.cop}
        onayMetni={cevir("Evet, sil")}
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />
    </div>
  )
}

function Sat({ e, d }: { e: string; d: string }) {
  return (
    <>
      <dt className="text-slate-500 dark:text-slate-500">{e}</dt>
      <dd className="text-slate-800 dark:text-slate-200 text-right font-mono">{d}</dd>
    </>
  )
}

function fmt(n: number, birim: string) {
  if (n <= 0) return cevir("sınırsız")
  if (birim.startsWith('MB') && n >= 1024) return `${(n / 1024).toFixed(1)} G${cevir(birim).slice(2)}`
  return `${n.toLocaleString('tr-TR')} ${cevir(birim)}`
}

function PlanModal({ plan, surumler, sinir, onKapat, onKayit }: { plan: Plan; surumler: Surum[]; sinir: BayiSinir | null; onKapat: () => void; onKayit: () => void }) {
  const yeni = !plan.id
  const bayi = !!sinir?.bayi
  const hDisk = havuzDurumu(sinir, 'disk')
  const hTrafik = havuzDurumu(sinir, 'trafik')
  const [form, setForm] = useState<Plan>({
    id: plan.id || 0,
    ad: plan.ad || '',
    aciklama: plan.aciklama || '',
    disk_kota_mb: plan.disk_kota_mb || 1024,
    trafik_kota_mb: plan.trafik_kota_mb || 10240,
    max_domain: plan.max_domain || 1,
    max_db: plan.max_db || 1,
    // Bayi: posta alanları paket tavanıyla başlar (0 = sınırsız tavanı aşar; mail
    // eklentisi kapalıyken alanlar gizli olsa da sunucu tavanı doğrular).
    max_email: plan.max_email || (bayi ? sinir?.mail_max_email || 0 : 0),
    saatlik_mail_limiti: plan.saatlik_mail_limiti || (bayi ? sinir?.mail_saatlik_limit || 0 : 0),
    mail_kutu_kota_mb: plan.mail_kutu_kota_mb || (bayi ? sinir?.mail_kutu_kota_mb || 0 : 0),
    max_ftp: plan.max_ftp || 2,
    php_surum: plan.php_surum || '8.3',
    fastcgi_cache: plan.fastcgi_cache || false,
    client_max_body_mb: plan.client_max_body_mb || 64,
    nginx_ek_direktifler: plan.nginx_ek_direktifler || '',
    varsayilan: plan.varsayilan || false,
    olusturulma: '',
  })
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const toast = useToast()
  // Mail eklentisi aktif+ödemeli ise plandaki mail kotası alanlarını göster.
  // DomainPlanPage/PaketDetayPage ile AYNI kapı — lisans kalkınca alanlar
  // kendiliğinden gizlenir; olmayan bir özelliği vaat etmeyiz.
  const [mailAktif, setMailAktif] = useState(false)
  useEffect(() => {
    let iptal = false
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => { if (iptal) return; setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)) })
      .catch(() => { if (!iptal) setMailAktif(false) })
    return () => { iptal = true }
  }, [])

  const phpOpts = Array.from(new Set([
    ...surumler.map(s => s.surum),
    form.php_surum,
    ...(surumler.length === 0 ? ['7.4', '8.1', '8.2', '8.3', '8.4'] : []),
  ].filter(Boolean)))

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      if (yeni) await api.post('/plans', form)
      else await api.put(`/plans/${form.id}`, form)
      onKayit()
    } catch (e) {
      const m = apiHata(e, cevir("Kayıt başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={yeni ? cevir('Yeni Plan') : cevir("Planı Düzenle")} onKapat={onKapat} genislik="lg" kapatilamaz={isleniyor}
      alt={<>
        <Button type="button" variant="outlined" onClick={onKapat} disabled={isleniyor} ikon={I.kapat}>{cevir("İptal")}</Button>
        <Button type="submit" form="hizmet-plani-formu" color="primary" yukleniyor={isleniyor} disabled={!form.ad.trim()} ikon={yeni ? I.arti : I.disket}>{isleniyor ? cevir('Kaydediliyor…') : (yeni ? cevir('Ekle') : cevir("Güncelle"))}</Button>
      </>}>
      <form id="hizmet-plani-formu" onSubmit={gonder} className="space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Alan etiket={cevir("Plan adı")} value={form.ad} setVal={v => setForm({ ...form, ad: v })} required />
          <Alan etiket={cevir("Açıklama")} value={form.aciklama} setVal={v => setForm({ ...form, aciklama: v })} />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <Sayi etiket="Disk (MB)" value={form.disk_kota_mb} setVal={v => setForm({ ...form, disk_kota_mb: v })} min={bayi ? 1 : 0} ipucu={havuzMetni(hDisk)} />
          <Sayi etiket={cevir("Trafik (MB)")} value={form.trafik_kota_mb} setVal={v => setForm({ ...form, trafik_kota_mb: v })} min={bayi ? 1 : 0} ipucu={havuzMetni(hTrafik)} />
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("PHP Sürümü")}</label>
            <select value={form.php_surum} onChange={e => setForm({ ...form, php_surum: e.target.value })}
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-700 rounded text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
              {phpOpts.map(v => <option key={v} value={v}>PHP {v}</option>)}
            </select>
          </div>
          <Sayi etiket="Max domain" value={form.max_domain} setVal={v => setForm({ ...form, max_domain: v })} />
          <Sayi etiket="Max DB" value={form.max_db} setVal={v => setForm({ ...form, max_db: v })} />
          <Sayi etiket="Max FTP" value={form.max_ftp} setVal={v => setForm({ ...form, max_ftp: v })} />
        </div>

        {/* Mail kotaları — yalnız mail eklentisi aktifken. Olmayan bir özelliği
            plan formunda göstermek yanıltıcı olurdu (lisans kalkınca gizlenir). */}
        {mailAktif && (
          <div className="rounded-lg border border-slate-200 dark:border-dark-600 p-3">
            <div className="text-xs font-semibold text-slate-600 dark:text-slate-300 mb-2">{cevir("E-posta (Mail eklentisi)")}</div>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
              <Sayi etiket={cevir("E-posta kutusu")} value={form.max_email} setVal={v => setForm({ ...form, max_email: v })} ipucu={tavanMetni(bayi ? sinir?.mail_max_email : 0)} />
              <Sayi etiket={cevir("Saatlik gönderim")} value={form.saatlik_mail_limiti} setVal={v => setForm({ ...form, saatlik_mail_limiti: v })} ipucu={tavanMetni(bayi ? sinir?.mail_saatlik_limit : 0)} />
              <Sayi etiket={cevir("Kutu depolama (MB)")} value={form.mail_kutu_kota_mb} setVal={v => setForm({ ...form, mail_kutu_kota_mb: v })} ipucu={tavanMetni(bayi ? sinir?.mail_kutu_kota_mb : 0, true)} />
            </div>
            <p className="mt-2 text-xs text-slate-500 dark:text-slate-500">{cevir("Kutu = posta kutusu sayısı · Saatlik = kutu başına giden mail/saat · Depolama = kutu başına disk (MB). 0 = sınırsız.")}</p>
          </div>
        )}
        <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
          <input type="checkbox" checked={form.varsayilan} onChange={e => setForm({ ...form, varsayilan: e.target.checked })} className="rounded" />
          {cevir("Yeni domainlerde varsayılan plan")}
        </label>
        <p className="text-xs text-slate-500 dark:text-slate-500">{cevir("0 = sınırsız. Disk/trafik MB cinsindendir. Bu plandaki yeni domainler seçili PHP sürümüyle kurulur.")}{bayi && <> {cevir("Bayi planında 0 (sınırsız) olamaz.")}</>}</p>
      </form>
    </Modal>
  )
}

function Alan({ etiket, value, setVal, required }: { etiket: string; value: string; setVal: (v: string) => void; required?: boolean }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input type="text" value={value} onChange={e => setVal(e.target.value)} required={required}
        className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
    </div>
  )
}
function Sayi({ etiket, value, setVal, min = 0, ipucu }: { etiket: string; value: number; setVal: (v: number) => void; min?: number; ipucu?: string }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input type="number" min={min} value={value} onChange={e => setVal(parseInt(e.target.value) || 0)}
        className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-700 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
      {ipucu && <p className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">{ipucu}</p>}
    </div>
  )
}

// Bayi paket havuzu ipucu (sunucu kuralı: fazla satış kapalıyken plan < havuz).
function havuzMetni(d: ReturnType<typeof havuzDurumu>): string | undefined {
  if (!d) return undefined
  return cevirT(cevir("Paketinizde kalan: {0} / {1}"), mbOkunur(d.kalan), mbOkunur(d.havuz)) + ' · ' +
    (d.fazla ? cevir("fazla satış açık") : cevirT(cevir("plan değeri {0} altında olmalı"), mbOkunur(d.havuz)))
}

function tavanMetni(tavan?: number, mb?: boolean): string | undefined {
  if (!tavan || tavan <= 0) return undefined
  return cevirT(cevir("Paket tavanı: {0}"), mb ? mbOkunur(tavan) : tavan.toLocaleString())
}
