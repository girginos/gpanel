import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'

type Plan = {
  id: number; ad: string; aciklama: string
  disk_kota_mb: number; trafik_kota_mb: number
  max_domain: number; max_db: number; max_email: number; max_ftp: number
  saatlik_mail_limiti: number; mail_kutu_kota_mb: number
  cpu_yuzde: number; ram_mb: number; max_process: number
  inode_kota: number; io_agirlik: number; mysql_max_baglanti: number
  pm_max_children: number
  io_read_mbps: number; io_write_mbps: number; io_read_iops: number; io_write_iops: number
  db_max_queries_per_hour: number; db_max_updates_per_hour: number; db_max_query_seconds: number
  php_surum: string
  fastcgi_cache: boolean; client_max_body_mb: number; nginx_ek_direktifler: string
  waf_enabled: boolean; waf_mode: string; waf_paranoia: number
  varsayilan: boolean; olusturulma: string
}
type Domain = { id: number; alan_adi: string; sistem_kullanici: string; durum: string; olusturulma: string }
type GetResp = { plan: Plan; domain_sayisi: number }
type Surum = { surum: string; aciklama?: string }


const PKTDET_EN: Record<string, string> = {
  "0 = Otomatik (max(4, RAM/64)). Per-tenant FPM'de RAM tavanıyla tutarlı.": "0 = Automatic (max(4, RAM/64)). Consistent with the RAM cap in per-tenant FPM.",
  "100 = 1 çekirdek (systemd CPUQuota)": "100 = 1 core (systemd CPUQuota)",
  "Bu plana bağlı hesapta oluşturulabilecek nesne sayıları. 0 = sınırsız.": "Number of objects that can be created on an account bound to this plan. 0 = unlimited.",
  "Bu plana bağlı yeni bir domain oluşturulduğunda uygulanacak başlangıç değerleri.": "Initial values applied when a new domain bound to this plan is created.",
  "Bu planda açık olsun": "Enabled on this plan",
  "Bu plandaki yeni domainler bu PHP sürümüyle kurulur.": "New domains on this plan are installed with this PHP version.",
  "Bu plandaki yeni domainlerde WAF açık mı gelsin (per-domain override edilebilir).": "Whether WAF is on for new domains on this plan (can be overridden per-domain).",
  "Bu süreyi aşan sorgu KILL edilir (watchdog). 0 = öldürme yok": "Queries exceeding this time are KILLED (watchdog). 0 = no killing",
  "CRS paranoia 1–4. Yüksek = daha sıkı koruma + daha çok yanlış-pozitif.": "CRS paranoia 1–4. Higher = stricter protection + more false positives.",
  "Dinamik PHP çıktısını nginx tarafında önbelleğe alır (yüksek trafik için)": "Caches dynamic PHP output on the nginx side (for high traffic)",
  "Disk G/Ç (mutlak throttle — IOWeight'ten farklı; cgroup v2)": "Disk I/O (absolute throttle — different from IOWeight; cgroup v2)",
  "Güvenlik Duvarı (WAF) Varsayılanı": "Firewall (WAF) Default",
  "Henüz bu plana atanmış domain yok.": "No domains assigned to this plan yet.",
  "I/O Ağırlık": "I/O Weight",
  "Inode Kotası": "Inode Quota",
  "MAX_QUERIES_PER_HOUR. 0 = sınırsız": "MAX_QUERIES_PER_HOUR. 0 = unlimited",
  "MAX_UPDATES_PER_HOUR. 0 = sınırsız": "MAX_UPDATES_PER_HOUR. 0 = unlimited",
  "Maks. Güncelleme/Saat": "Max Updates/Hour",
  "Maks. Sorgu Süresi (sn)": "Max Query Time (s)",
  "Mutlak okuma bant genişliği. 0 = sınırsız": "Absolute read bandwidth. 0 = unlimited",
  "Mutlak yazma bant genişliği. 0 = sınırsız": "Absolute write bandwidth. 0 = unlimited",
  "MySQL Bağlantı": "MySQL Connection",
  "Saatlik Gönderim": "Hourly Sending",
  "Saniyedeki maksimum okuma işlemi. 0 = sınırsız": "Maximum read operations per second. 0 = unlimited",
  "Saniyedeki maksimum yazma işlemi. 0 = sınırsız": "Maximum write operations per second. 0 = unlimited",
  "Sayısal Sınırlar": "Numeric Limits",
  "Seviye 1 (Düşük — önerilen)": "Level 1 (Low — recommended)",
  "Seviye 3 (Yüksek)": "Level 3 (High)",
  "Seviye 4 (Sıkı)": "Level 4 (Strict)",
  "Toplam dosya + dizin sayısı": "Total number of files + directories",
  "Varsayılanlar": "Defaults",
  "WAF Varsayılanı": "WAF Default",
  "Yeni domainlerde açık olsun": "Enabled on new domains",
  "Yeni domainlere otomatik atansın": "Auto-assign to new domains",
  "Yükleme Boyutu Limiti (MB)": "Upload Size Limit (MB)",
  "nginx client_max_body_size — dosya yükleme üst sınırı": "nginx client_max_body_size — file upload cap",
  "server{} bloğuna eklenir; kaydederken doğrulanır.": "Added to the server{} block; validated on save.",
  "✓ Uygulandı": "✓ Applied",
  "✗ Başarısız": "✗ Failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PKTDET_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PaketDetayPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const [plan, setPlan] = useState<Plan | null>(null)
  const [domainSayisi, setDomainSayisi] = useState(0)
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  // Mail eklentisi aktif+odemeli ise posta limiti alanlari gorunur.
  // DomainPlanPage ile AYNI kapi -- lisans kalkinca alanlar kendiliginden gizlenir.
  const [mailAktif, setMailAktif] = useState(false)
  const [uygulananID, setUygulananID] = useState<number | null>(null)
  const [sonucID, setSonucID] = useState<{ id: number; ok: boolean } | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    // Mail eklentisi aktif mi? (posta limiti alanlarinin kapisi)
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
    Promise.all([
      api.get<GetResp>(`/plans/${id}`),
      api.get<Domain[]>(`/plans/${id}/domains`),
    ]).then(([g, d]) => {
      setPlan(g.data.plan)
      setDomainSayisi(g.data.domain_sayisi)
      setDomainler(d.data || [])
    }).catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])
  useEffect(() => {
    api.get<Surum[]>('/php/versions').then(r => setSurumler(r.data || [])).catch(hataYakala(cevir("PHP sürümleri alınamadı")))
  }, [])

  async function kaydet() {
    if (!plan) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/plans/${id}`, plan)
      setBasari(`"${plan.ad}" kaydedildi. Atanmış domainlere uygulamak için aşağıdan “Yeniden Uygula”.`)
      setTimeout(() => setBasari(null), 6000)
      yukle()
    } catch (e) {
      setHata(apiHata(e, cevir("Kaydetme başarısız")))
    } finally {
      setIsleniyor(false)
    }
  }

  async function domainicinYenidenUygula(domID: number) {
    if (!plan) return
    setUygulananID(domID); setSonucID(null); setHata(null)
    try {
      await api.put(`/domains/${domID}/plan`, { plan_id: plan.id })
      setSonucID({ id: domID, ok: true })
    } catch (e) {
      setSonucID({ id: domID, ok: false }); setHata(apiHata(e))
    } finally {
      setUygulananID(null)
      setTimeout(() => setSonucID(v => (v?.id === domID ? null : v)), 3500)
    }
  }

  function P<K extends keyof Plan>(k: K, v: Plan[K]) {
    if (!plan) return
    setPlan({ ...plan, [k]: v })
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">{cevir("Yükleniyor…")}</div>
  if (!plan) return <div className="px-4 py-4 sm:px-6 sm:py-5"><div className="text-sm text-red-600">{hata || cevir("Plan bulunamadı")}</div></div>

  // Kurulu PHP sürümleri + planın mevcut değeri (kurulu olmasa da görünsün)
  const phpOpts = Array.from(new Set([
    ...surumler.map(s => s.surum),
    plan.php_surum,
    ...(surumler.length === 0 ? ['7.4', '8.1', '8.2', '8.3', '8.4'] : []),
  ].filter(Boolean)))

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-5xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' },
          { etiket: cevir("Hizmet Planları"), href: '/araclar/paketler' },
          { etiket: plan.ad },
        ]} />

        {/* Başlık + kaydet (yapışkan) */}
        <div className="sticky top-0 z-10 -mx-2 px-2 py-3 mb-4 bg-slate-50/85 dark:bg-slate-900/85 backdrop-blur border-b border-slate-200/70 dark:border-slate-800 flex items-center justify-between gap-4">
          <div className="min-w-0">
            <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100 flex items-center gap-2 truncate">
              {plan.ad}
              {plan.varsayilan && <span className="shrink-0 text-[10px] uppercase font-semibold tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded">{cevir("Varsayılan")}</span>}
            </h1>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 truncate">
              {plan.aciklama || cevir("Açıklama yok")} · <span className="font-mono">{domainSayisi}</span> {cevir("domainde kullanılıyor")}
            </p>
          </div>
          <button onClick={kaydet} disabled={isleniyor}
            className="shrink-0 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-lg shadow-sm">
            {isleniyor ? 'Kaydediliyor…' : cevir("Değişiklikleri Kaydet")}
          </button>
        </div>

        {hata && <div className="mb-4 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {basari && <div className="mb-4 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

        {/* Genel */}
        <Kart baslik={cevir(cevir("Genel"))} ikon="⚙️">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Alan etiket={cevir("Plan Adı")}>
              <input value={plan.ad} onChange={e => P('ad', e.target.value)} className={inp} />
            </Alan>
            <Alan etiket={cevir("Varsayılan Plan")}>
              <label className="flex items-center gap-2 h-[38px] px-3 border border-slate-200 dark:border-slate-700 rounded-lg bg-slate-50/60 dark:bg-slate-900/40 cursor-pointer">
                <input type="checkbox" checked={plan.varsayilan} onChange={e => P('varsayilan', e.target.checked)} className="rounded" />
                <span className="text-sm text-slate-700 dark:text-slate-300">{cevir("Yeni domainlere otomatik atansın")}</span>
              </label>
            </Alan>
            <Alan etiket={cevir("Açıklama")} span={2}>
              <textarea value={plan.aciklama} onChange={e => P('aciklama', e.target.value)} rows={2} className={inp} />
            </Alan>
          </div>
        </Kart>

        {/* Varsayılanlar — yeni domainler bu değerleri miras alır */}
        <Kart baslik={cevir("Varsayılanlar")} ikon="🧩" alt={cevir("Bu plana bağlı yeni bir domain oluşturulduğunda uygulanacak başlangıç değerleri.")}>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Alan etiket={cevir("PHP Sürümü")} ipucu={cevir("Bu plandaki yeni domainler bu PHP sürümüyle kurulur.")}>
              <select value={plan.php_surum} onChange={e => P('php_surum', e.target.value)} className={inp}>
                {phpOpts.map(v => <option key={v} value={v}>PHP {v}</option>)}
              </select>
            </Alan>
          </div>
        </Kart>

        {/* Kaynak Limitleri */}
        <Kart baslik="Kaynak Limitleri" ikon="📊" alt="systemd cgroup + xfs_quota + MariaDB GRANT ile sistem seviyesinde uygulanır. Kaydettikten sonra atanmış domainler için “Yeniden Uygula” tetikleyin.">
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <Alan etiket="CPU %" ipucu={cevir("100 = 1 çekirdek (systemd CPUQuota)")}>
              <input type="number" min={10} max={2000} value={plan.cpu_yuzde} onChange={e => P('cpu_yuzde', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket="RAM (MB)" ipucu="Hard MemoryMax; MemoryHigh otomatik %90">
              <input type="number" min={64} value={plan.ram_mb} onChange={e => P('ram_mb', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket="Max Process" ipucu="systemd TasksMax — PHP-FPM worker dahil">
              <input type="number" min={5} value={plan.max_process} onChange={e => P('max_process', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket={cevir("MySQL Bağlantı")} ipucu="MAX_USER_CONNECTIONS">
              <input type="number" min={1} value={plan.mysql_max_baglanti} onChange={e => P('mysql_max_baglanti', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket="Disk (MB)" ipucu={cevir("0 = sınırsız")}>
              <input type="number" min={0} value={plan.disk_kota_mb} onChange={e => P('disk_kota_mb', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket="Trafik (MB/ay)" ipucu={cevir("0 = sınırsız")}>
              <input type="number" min={0} value={plan.trafik_kota_mb} onChange={e => P('trafik_kota_mb', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket={cevir("Inode Kotası")} ipucu={cevir("Toplam dosya + dizin sayısı")}>
              <input type="number" min={1000} value={plan.inode_kota} onChange={e => P('inode_kota', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket={cevir("I/O Ağırlık")} ipucu="systemd IOWeight (1-1000)">
              <input type="number" min={1} max={1000} value={plan.io_agirlik} onChange={e => P('io_agirlik', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket="PHP-FPM max_children" ipucu={cevir("0 = Otomatik (max(4, RAM/64)). Per-tenant FPM'de RAM tavanıyla tutarlı.")}>
              <input type="number" min={0} value={plan.pm_max_children} onChange={e => P('pm_max_children', Number(e.target.value) || 0)} className={inpNum} placeholder="0 = Otomatik" />
            </Alan>
          </div>
          <div className="mt-4 text-xs font-medium text-slate-500">{cevir("Disk G/Ç (mutlak throttle — IOWeight'ten farklı; cgroup v2)")}</div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mt-2">
            <Alan etiket="Disk Okuma (MB/s)" ipucu={cevir("Mutlak okuma bant genişliği. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.io_read_mbps} onChange={e => P('io_read_mbps', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
            <Alan etiket="Disk Yazma (MB/s)" ipucu={cevir("Mutlak yazma bant genişliği. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.io_write_mbps} onChange={e => P('io_write_mbps', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
            <Alan etiket="Okuma IOPS" ipucu={cevir("Saniyedeki maksimum okuma işlemi. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.io_read_iops} onChange={e => P('io_read_iops', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
            <Alan etiket="Yazma IOPS" ipucu={cevir("Saniyedeki maksimum yazma işlemi. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.io_write_iops} onChange={e => P('io_write_iops', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
          </div>
          <div className="mt-4 text-xs font-medium text-slate-500">{cevir("Veritabanı (MySQL Governor — native MariaDB; Bağlantı limiti yukarıdaki “MySQL Bağlantı”)")}</div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mt-2">
            <Alan etiket="Maks. Sorgu/Saat" ipucu={cevir("MAX_QUERIES_PER_HOUR. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.db_max_queries_per_hour} onChange={e => P('db_max_queries_per_hour', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
            <Alan etiket={cevir("Maks. Güncelleme/Saat")} ipucu={cevir("MAX_UPDATES_PER_HOUR. 0 = sınırsız")}>
              <input type="number" min={0} value={plan.db_max_updates_per_hour} onChange={e => P('db_max_updates_per_hour', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
            <Alan etiket={cevir("Maks. Sorgu Süresi (sn)")} ipucu={cevir("Bu süreyi aşan sorgu KILL edilir (watchdog). 0 = öldürme yok")}>
              <input type="number" min={0} value={plan.db_max_query_seconds} onChange={e => P('db_max_query_seconds', Number(e.target.value) || 0)} className={inpNum} placeholder={cevir("0 = sınırsız")} />
            </Alan>
          </div>
        </Kart>

        {/* Sayısal Sınırlar — posta alanlari mail eklentisi aktifken eklenir */}
        <Kart baslik={cevir("Sayısal Sınırlar")} ikon="🔢" alt={cevir("Bu plana bağlı hesapta oluşturulabilecek nesne sayıları. 0 = sınırsız.")}>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
            <Alan etiket="Domain">
              <input type="number" min={0} value={plan.max_domain} onChange={e => P('max_domain', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket={cevir("Veritabanı")}>
              <input type="number" min={0} value={plan.max_db} onChange={e => P('max_db', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            <Alan etiket={cevir("FTP Hesabı")}>
              <input type="number" min={0} value={plan.max_ftp} onChange={e => P('max_ftp', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
            {/* Posta limitleri: YALNIZ mail eklentisi aktifken. Eklenti yokken
                bu alanlari gostermek, olmayan bir ozelligi vaat etmek olurdu. */}
            {mailAktif && (
              <>
                <Alan etiket="E-posta Kutusu" ipucu="Bu plandaki hesapta acilabilecek posta kutusu sayisi. 0 = sinirsiz">
                  <input type="number" min={0} value={plan.max_email} onChange={e => P('max_email', Number(e.target.value) || 0)} className={inpNum} />
                </Alan>
                <Alan etiket={cevir("Saatlik Gönderim")} ipucu="Kutu basina giden mail/saat. 0 = sinirsiz">
                  <input type="number" min={0} value={plan.saatlik_mail_limiti} onChange={e => P('saatlik_mail_limiti', Number(e.target.value) || 0)} className={inpNum} />
                </Alan>
                <Alan etiket="Kutu Depolama (MB)" ipucu="Posta kutusu basina disk kotasi. 0 = sinirsiz">
                  <input type="number" min={0} value={plan.mail_kutu_kota_mb} onChange={e => P('mail_kutu_kota_mb', Number(e.target.value) || 0)} className={inpNum} />
                </Alan>
              </>
            )}
          </div>
        </Kart>

        {/* Web Sunucusu (nginx) */}
        <Kart baslik="Web Sunucusu (nginx)" ikon="🛠️" alt={cevir("Bu plandaki yeni domainler bu nginx ayarlarıyla kurulur. Ek direktifler kaydederken “nginx -t” ile doğrulanır.")}>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
            <Alan etiket="FastCGI Cache" ipucu={cevir("Dinamik PHP çıktısını nginx tarafında önbelleğe alır (yüksek trafik için)")}>
              <label className="flex items-center gap-2 h-[38px] px-3 border border-slate-200 dark:border-slate-700 rounded-lg bg-slate-50/60 dark:bg-slate-900/40 cursor-pointer">
                <input type="checkbox" checked={plan.fastcgi_cache} onChange={e => P('fastcgi_cache', e.target.checked)} className="rounded" />
                <span className="text-sm text-slate-700 dark:text-slate-300">{cevir("Yeni domainlerde açık olsun")}</span>
              </label>
            </Alan>
            <Alan etiket={cevir("Yükleme Boyutu Limiti (MB)")} ipucu={cevir("nginx client_max_body_size — dosya yükleme üst sınırı")}>
              <input type="number" min={1} max={4096} value={plan.client_max_body_mb} onChange={e => P('client_max_body_mb', Number(e.target.value) || 0)} className={inpNum} />
            </Alan>
          </div>
          <Alan etiket="Ek nginx Direktifleri" ipucu={cevir("server{} bloğuna eklenir; kaydederken doğrulanır.")}>
            <textarea
              value={plan.nginx_ek_direktifler || ''}
              onChange={e => P('nginx_ek_direktifler', e.target.value)}
              rows={6}
              spellCheck={false}
              placeholder={'# Örnek:\nadd_header X-Robots-Tag "noindex" always;\nlocation = /saglik { return 200 "ok"; }'}
              className={inp + ' font-mono text-xs leading-relaxed'}
            />
          </Alan>
          <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
            ⓘ Kaydet'e bastığınızda direktifler geçici bir sunucu bloğunda <code className="font-mono">nginx -t</code> ile test edilir. Geçersizse plan <strong>kaydedilmez</strong> ve nginx'in hata çıktısı yukarıda gösterilir.
          </p>
        </Kart>

        {/* WAF (ModSecurity + OWASP CRS) plan varsayılanı */}
        <Kart baslik={cevir("Güvenlik Duvarı (WAF) Varsayılanı")} ikon="🛡️" alt="ModSecurity v3 + OWASP Core Rule Set. Bu plandaki domainler (kendi WAF override'ı yoksa) bu değerleri devralır. Domain düzeyinde ‘Plandan Devral’ seçiliyse buradaki ayar geçerlidir.">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Alan etiket={cevir("WAF Varsayılanı")} ipucu={cevir("Bu plandaki yeni domainlerde WAF açık mı gelsin (per-domain override edilebilir).")}>
              <label className="flex items-center gap-2 h-[38px] px-3 border border-slate-200 dark:border-slate-700 rounded-lg bg-slate-50/60 dark:bg-slate-900/40 cursor-pointer">
                <input type="checkbox" checked={plan.waf_enabled} onChange={e => P('waf_enabled', e.target.checked)} className="rounded" />
                <span className="text-sm text-slate-700 dark:text-slate-300">{cevir("Bu planda açık olsun")}</span>
              </label>
            </Alan>
            <Alan etiket="Mod" ipucu="Engelle = kötü istekleri 403’ler (SecRuleEngine On). Denetle = yalnızca audit log’a yazar (DetectionOnly).">
              <select value={plan.waf_mode} onChange={e => P('waf_mode', e.target.value)} className={inp} disabled={!plan.waf_enabled}>
                <option value="on">Engelle (On)</option>
                <option value="detect">{cevir("Denetle (yalnızca kaydet)")}</option>
              </select>
            </Alan>
            <Alan etiket="Paranoya Seviyesi" ipucu={cevir("CRS paranoia 1–4. Yüksek = daha sıkı koruma + daha çok yanlış-pozitif.")}>
              <select value={plan.waf_paranoia} onChange={e => P('waf_paranoia', Number(e.target.value) || 1)} className={inp} disabled={!plan.waf_enabled}>
                <option value={1}>{cevir("Seviye 1 (Düşük — önerilen)")}</option>
                <option value={2}>Seviye 2 (Orta)</option>
                <option value={3}>{cevir("Seviye 3 (Yüksek)")}</option>
                <option value={4}>{cevir("Seviye 4 (Sıkı)")}</option>
              </select>
            </Alan>
          </div>
          <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
            ⓘ Değişiklikten sonra bu plana bağlı domainlerin vhost’ları arka planda otomatik yeniden render edilir (nginx -t korumalı, sıfır kesinti). Sunucuda modül kurulu değilse ayar saklanır ve <code className="font-mono">girginospanel-waf-setup</code> ile etkinleşir.
          </p>
        </Kart>

        {/* Atanmış domainler */}
        <Kart baslik={cevirT(cevir("Atanmış Domainler ({0})"), domainler.length)} ikon="🌐" alt="Plan güncellendikten sonra “Yeniden Uygula” ile ilgili domain'in cgroup + quota + MySQL limitleri güncellenir.">
          {domainler.length === 0 ? (
            <div className="text-sm text-slate-400 py-6 text-center">{cevir("Henüz bu plana atanmış domain yok.")}</div>
          ) : (
            // Mobilde yatay kaydırma yok — her satır kart olur (bkz. @/lib/tablo)
            <div className="lg:overflow-x-auto">
              <table className={`${T.tablo} text-sm`}>
                <thead className={`${T.baslikGrubu} text-xs uppercase tracking-wider text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700`}>
                  <tr>
                    <th className={T.baslik}>Domain</th>
                    <th className={T.baslik}>{cevir("Sistem Kullanıcısı")}</th>
                    <th className={T.baslik}>{cevir("Durum")}</th>
                    <th className={T.baslik}>{cevir("Oluşturma")}</th>
                    <th className={`${T.baslik} text-right`}>{cevir("İşlem")}</th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {domainler.map(d => (
                    <tr key={d.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/60`}>
                      {/* Birincil tanımlayıcı: mobilde kart başlığı olur, etiket istemez */}
                      <td className={T.hucreBaslik}>
                        <Link to={`/abonelikler/${d.id}`} className="text-brand-600 dark:text-brand-400 font-medium">{d.alan_adi}</Link>
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Sistem Kullanıcısı")}>
                        <span className="font-mono text-xs break-all">{d.sistem_kullanici}</span>
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Durum")}>
                        <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${
                          d.durum === 'aktif' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500'
                        }`}>{d.durum}</span>
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Oluşturma")}>
                        <span className="font-mono text-xs text-slate-500 whitespace-nowrap">{d.olusturulma}</span>
                      </td>
                      <td className={`${T.hucreAksiyon} lg:text-right`}>
                        <button type="button" onClick={() => domainicinYenidenUygula(d.id)} disabled={uygulananID !== null}
                          className={`text-xs px-2 py-1 border rounded-md disabled:opacity-60 transition ${
                            sonucID?.id === d.id
                              ? (sonucID.ok
                                  ? 'border-emerald-300 dark:border-emerald-700 text-emerald-700 dark:text-emerald-300 bg-emerald-50 dark:bg-emerald-900/20'
                                  : 'border-red-300 dark:border-red-700 text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/20')
                              : 'border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800'}`}>
                          {uygulananID === d.id ? 'Uygulanıyor…' : sonucID?.id === d.id ? (sonucID.ok ? '✓ Uygulandı' : cevir("✗ Başarısız")) : 'Yeniden Uygula'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Kart>
      </div>
    </div>
  )
}

const inp = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-800 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none'
const inpNum = inp + ' font-mono'

function Kart({ baslik, alt, ikon, children }: { baslik: string; alt?: string; ikon?: string; children: React.ReactNode }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
      <div className="flex items-center gap-2 mb-1">
        {ikon && <span className="text-base leading-none" aria-hidden>{ikon}</span>}
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
      </div>
      {alt && <p className="text-xs text-slate-500 dark:text-slate-400 mb-4 max-w-2xl">{alt}</p>}
      {children}
    </div>
  )
}

function Alan({ etiket, ipucu, span, children }: { etiket: string; ipucu?: string; span?: number; children: React.ReactNode }) {
  return (
    <label className={`block ${span === 2 ? 'sm:col-span-2' : ''}`}>
      <span className="text-xs font-medium text-slate-600 dark:text-slate-400">{etiket}</span>
      {ipucu && <span className="text-[10px] text-slate-400 dark:text-slate-500 ml-1 cursor-help" title={ipucu}>ⓘ</span>}
      <div className="mt-1.5">{children}</div>
    </label>
  )
}
