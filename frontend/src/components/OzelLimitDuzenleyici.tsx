// "Bu hostinge özel limitler" düzenleyicisi (D1 — özel plan = üst yazım).
//
// Hosting katalog planında kalır; yalnız "Özel" işaretlenen alanlar plandan farklı
// olur (hosting_limit_ust), gerisi planı izler. Plan değişse de özel değerler
// korunur. Plan değeri her alanın yanında görünür; özel alanlar vurgulanır;
// alan başına "Özel" kutusunu kaldırmak o alanı plana döndürür, "Plan değerlerine
// dön" tümünü. Yöneticinin kilitlediği alan bayiye kilitli/salt okunur görünür;
// kökün planındaki iç ayarların değeri bayiye "gizli" döner (sunucu maskeler).
import { useEffect, useMemo, useState } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { apiHata } from '@/lib/api'
import { limitKaydet, limitSifirla, type LimitAlan, type LimitYanit } from '@/lib/limit'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { Button, Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'

const OL_EN: Record<string, string> = {
  "Bu hostinge özel limitler": "Limits specific to this hosting",
  "Yalnız “Özel” işaretlediğiniz alanlar plandan farklı olur; diğerleri planı izler. Plan değişse de özel değerler korunur.": "Only the fields you mark “Custom” differ from the plan; the rest follow the plan. Custom values are kept even if the plan changes.",
  "Disk ve trafik": "Disk and traffic",
  "İşlemci ve bellek": "CPU and memory",
  "Disk G/Ç": "Disk I/O",
  "Veritabanı sınırları": "Database limits",
  "Hesap sayıları": "Account counts",
  "Posta": "Mail",
  "Disk": "Disk",
  "Aylık trafik": "Monthly traffic",
  "Dosya sayısı (inode)": "File count (inode)",
  "CPU": "CPU",
  "RAM": "RAM",
  "İşlem sayısı": "Process count",
  "Disk G/Ç önceliği": "Disk I/O priority",
  "PHP işçi sayısı": "PHP worker count",
  "Disk okuma hızı": "Disk read speed",
  "Disk yazma hızı": "Disk write speed",
  "Okuma IOPS": "Read IOPS",
  "Yazma IOPS": "Write IOPS",
  "MySQL bağlantı": "MySQL connections",
  "Sorgu / saat": "Queries / hour",
  "Güncelleme / saat": "Updates / hour",
  "Uzun sorgu sınırı": "Long query limit",
  "Alan adı (ana + ek)": "Domains (primary + addon)",
  "Veritabanı": "Databases",
  "FTP hesabı": "FTP accounts",
  "Posta kutusu": "Mailboxes",
  "Giden mail / saat (kutu başına)": "Outgoing mail / hour (per mailbox)",
  "Kutu depolama": "Mailbox storage",
  "Özel": "Custom",
  "Plan": "Plan",
  "gizli": "hidden",
  "yok": "none",
  "sınırsız": "unlimited",
  "0 = sınırsız": "0 = unlimited",
  "0 = otomatik": "0 = automatic",
  "0 = kapalı": "0 = off",
  "En az {0}": "At least {0}",
  "Yönetici belirledi": "Set by administrator",
  "Kaydet ve uygula": "Save and apply",
  "Kaydediliyor…": "Saving…",
  "Plan değerlerine dön": "Revert to plan values",
  "Kapat": "Close",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Özel limitler kaydedildi ve bu hostinge uygulanıyor.": "Custom limits saved and being applied to this hosting.",
  "Plan değerlerine dönüldü.": "Reverted to plan values.",
  "Tüm özel limitler kaldırılsın mı?": "Remove all custom limits?",
  "Bu hosting yeniden planın değerlerini kullanır. Yöneticinin belirlediği alanlar değişmez.": "This hosting will use the plan's values again. Fields set by the administrator stay unchanged.",
  "Dön": "Revert",
  "Vazgeç": "Cancel",
  "{0}: bir sayı girin.": "{0}: enter a number.",
  "{0}: {1} ile {2} arasında olmalı.": "{0}: must be between {1} and {2}.",
  "Özel değer": "Custom value",
  "Plana dön": "Use plan value",
}
const cevir = (tr: string, ...a: (string | number)[]): string => {
  let s = i18n.language === 'en' ? (OL_EN[tr] || ORTAK_EN[tr] || tr) : tr
  a.forEach((v, i) => { s = s.split('{' + i + '}').join(String(v)) })
  return s
}

// Etiket ve grup başlıkları (TR kaynak). Sütun adları sunucunun sabit listesidir.
const ETIKET: Record<string, string> = {
  disk_kota_mb: 'Disk', trafik_kota_mb: 'Aylık trafik', inode_kota: 'Dosya sayısı (inode)',
  cpu_yuzde: 'CPU', ram_mb: 'RAM', max_process: 'İşlem sayısı', io_agirlik: 'Disk G/Ç önceliği',
  pm_max_children: 'PHP işçi sayısı', io_read_mbps: 'Disk okuma hızı', io_write_mbps: 'Disk yazma hızı',
  io_read_iops: 'Okuma IOPS', io_write_iops: 'Yazma IOPS', mysql_max_baglanti: 'MySQL bağlantı',
  db_max_queries_per_hour: 'Sorgu / saat', db_max_updates_per_hour: 'Güncelleme / saat',
  db_max_query_seconds: 'Uzun sorgu sınırı', max_domain: 'Alan adı (ana + ek)', max_db: 'Veritabanı',
  max_ftp: 'FTP hesabı', max_email: 'Posta kutusu', saatlik_mail_limiti: 'Giden mail / saat (kutu başına)',
  mail_kutu_kota_mb: 'Kutu depolama',
}
const GRUPLAR: [LimitAlan['grup'], string][] = [
  ['disk', 'Disk ve trafik'], ['kaynak', 'İşlemci ve bellek'], ['io', 'Disk G/Ç'],
  ['db', 'Veritabanı sınırları'], ['sayi', 'Hesap sayıları'], ['posta', 'Posta'],
]
// Zorlaması olmayan alan formda gösterilmez (olmayan özelliği vaat etme).
const GIZLI_ALAN = new Set(['max_domain'])

function degerMetni(a: LimitAlan, v: number | null): string {
  if (v === null) return cevir('yok')
  if (v === 0 && (a.sifir === 'sinirsiz' || a.sifir === 'kullanilmaz')) return cevir('sınırsız')
  if (a.birim === 'MB' || a.birim === 'MB/ay') {
    const s = v >= 1024 ? `${(v / 1024).toFixed(v % 1024 ? 1 : 0)} GB` : `${v} MB`
    return a.birim === 'MB/ay' ? `${s}/${i18n.language === 'en' ? 'mo' : 'ay'}` : s
  }
  if (a.birim === '%') return `%${v}`
  if (a.birim === 'adet' || a.birim === 'ağırlık') return String(v)
  return `${v} ${a.birim}`
}

function sifirIpucu(a: LimitAlan): string {
  switch (a.sifir) {
    case 'sinirsiz': return cevir('0 = sınırsız')
    case 'otomatik': return cevir('0 = otomatik')
    case 'kullanilmaz': return cevir('0 = kapalı')
    default: return cevir('En az {0}', Math.max(1, a.min))
  }
}

type Taslak = Record<string, { ozel: boolean; deger: string }>

function taslakKur(alanlar: LimitAlan[]): Taslak {
  const t: Taslak = {}
  for (const a of alanlar) t[a.ad] = { ozel: a.ust !== null || !!a.kilitli, deger: a.ust !== null ? String(a.ust) : '' }
  return t
}

export default function OzelLimitDuzenleyici({ domainId, veri, mailAktif, onDegisti, onKapat }: {
  domainId: string | number
  veri: LimitYanit
  mailAktif: boolean
  onDegisti: (v: LimitYanit) => void
  onKapat: () => void
}) {
  const toast = useToast()
  const { onay } = useDialog()
  const [taslak, setTaslak] = useState<Taslak>(() => taslakKur(veri.alanlar))
  const [isleniyor, setIsleniyor] = useState<'kaydet' | 'sifirla' | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  useEffect(() => { setTaslak(taslakKur(veri.alanlar)) }, [veri])

  // Kökün iç ayarları (sunucu `ic` işaretler; tek kaynak internal/efektiflimit IcAlan)
  // bayiye hiç gösterilmez ve gönderilen gövdeye girmez — sunucu gelirse 403 döner.
  const gorunen = useMemo(() => veri.alanlar.filter(a => !a.ic && !GIZLI_ALAN.has(a.ad) && (a.grup !== 'posta' || mailAktif)), [veri, mailAktif])
  const degisti = useMemo(() => veri.alanlar.some(a => {
    const t = taslak[a.ad]
    if (!t || a.kilitli || a.ic) return false
    const yeniOzel = t.ozel
    if (yeniOzel !== (a.ust !== null)) return true
    return yeniOzel && String(a.ust) !== t.deger.trim()
  }), [veri, taslak])
  const ozelVar = veri.alanlar.some(a => a.ust !== null && !a.kilitli && !a.ic)

  function ozelDegis(a: LimitAlan, ozel: boolean) {
    const onceki = taslak[a.ad]
    const baslangic = onceki?.deger || (a.efektif ?? a.plan ?? '').toString()
    setTaslak({ ...taslak, [a.ad]: { ozel, deger: ozel ? baslangic : '' } })
  }

  async function kaydet() {
    setHata(null)
    const ust: Record<string, number | null> = {}
    for (const a of veri.alanlar) {
      const t = taslak[a.ad]
      if (!t || a.kilitli || a.ic || !t.ozel) continue
      const s = t.deger.trim()
      const n = Number(s)
      const etiket = cevir(ETIKET[a.ad] || a.ad)
      if (s === '' || !Number.isInteger(n)) { setHata(cevir('{0}: bir sayı girin.', etiket)); return }
      const alt = a.sifir === 'yasak' ? Math.max(1, a.min) : 0
      if (n < alt || (a.max > 0 && n > a.max)) { setHata(cevir('{0}: {1} ile {2} arasında olmalı.', etiket, alt, a.max)); return }
      ust[a.ad] = n
    }
    setIsleniyor('kaydet')
    try {
      const v = await limitKaydet(domainId, ust)
      onDegisti(v)
      toast.basari(cevir('Kaydedildi'), cevir('Özel limitler kaydedildi ve bu hostinge uygulanıyor.'))
    } catch (e) {
      const m = apiHata(e); setHata(m); toast.hata(cevir('İşlem başarısız'), m)
    } finally { setIsleniyor(null) }
  }

  async function sifirla() {
    if (!(await onay({
      baslik: cevir('Tüm özel limitler kaldırılsın mı?'),
      mesaj: cevir('Bu hosting yeniden planın değerlerini kullanır. Yöneticinin belirlediği alanlar değişmez.'),
      ton: 'uyari', onayEtiketi: cevir('Dön'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.geriAl,
    }))) return
    setHata(null); setIsleniyor('sifirla')
    try {
      const v = await limitSifirla(domainId)
      onDegisti(v)
      toast.basari(cevir('Kaydedildi'), cevir('Plan değerlerine dönüldü.'))
    } catch (e) {
      const m = apiHata(e); setHata(m); toast.hata(cevir('İşlem başarısız'), m)
    } finally { setIsleniyor(null) }
  }

  return (
    <section aria-labelledby="ozel-limit-baslik"
             className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800 p-4">
      <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 id="ozel-limit-baslik" className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir('Bu hostinge özel limitler')}</h2>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            {cevir('Yalnız “Özel” işaretlediğiniz alanlar plandan farklı olur; diğerleri planı izler. Plan değişse de özel değerler korunur.')}
          </p>
        </div>
        <Button type="button" variant="outlined" boyut="sm" ikon={I.kapat} onClick={onKapat} disabled={isleniyor !== null}>
          {cevir('Kapat')}
        </Button>
      </div>

      {hata && (
        <p role="alert" className="mb-3 rounded-md border border-red-200 dark:border-red-900 bg-red-50 dark:bg-red-950/30 px-3 py-2 text-sm text-red-700 dark:text-red-300">{hata}</p>
      )}

      {GRUPLAR.map(([g, baslik]) => {
        const liste = gorunen.filter(a => a.grup === g)
        if (liste.length === 0) return null
        return (
          <fieldset key={g} className="mb-4 last:mb-0">
            <legend className="mb-2 text-[11px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-500">{cevir(baslik)}</legend>
            <ul role="list" className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {liste.map(a => {
                const t = taslak[a.ad] || { ozel: false, deger: '' }
                const etiket = cevir(ETIKET[a.ad] || a.ad)
                const girdiId = `ol-${a.ad}`
                const planMetni = a.gizli && a.plan === null ? cevir('gizli') : degerMetni(a, a.plan)
                return (
                  <li key={a.ad}
                      className={`rounded-lg border p-3 ${t.ozel
                        ? 'border-brand-400 bg-brand-50/60 dark:border-brand-700 dark:bg-brand-950/30'
                        : 'border-slate-200 dark:border-dark-600'}`}>
                    <div className="flex items-center justify-between gap-2">
                      <label htmlFor={girdiId} className="text-xs font-medium text-slate-700 dark:text-slate-200">{etiket}</label>
                      {a.kilitli ? (
                        <Badge variant="soft" className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px]">
                          <Ikon d={I.kilit} className="size-3 shrink-0" />{cevir('Yönetici belirledi')}
                        </Badge>
                      ) : (
                        <label className="inline-flex cursor-pointer items-center gap-1.5 text-xs text-slate-600 dark:text-slate-300">
                          <input type="checkbox" checked={t.ozel} onChange={e => ozelDegis(a, e.target.checked)}
                                 disabled={isleniyor !== null}
                                 className="size-4 rounded border-slate-300 dark:border-dark-500 text-brand-600 focus-visible:ring-2 focus-visible:ring-brand-500" />
                          {cevir('Özel')}
                        </label>
                      )}
                    </div>
                    <div className="mt-1.5 flex items-center gap-2">
                      <input id={girdiId} type="number" inputMode="numeric" min={a.sifir === 'yasak' ? Math.max(1, a.min) : 0}
                             max={a.max > 0 ? a.max : undefined}
                             value={t.ozel ? (a.kilitli && a.ust === null ? '' : t.deger) : ''}
                             placeholder={a.kilitli && a.ust === null ? cevir('gizli') : planMetni}
                             disabled={!t.ozel || !!a.kilitli || isleniyor !== null}
                             aria-label={t.ozel ? `${etiket} — ${cevir('Özel değer')}` : etiket}
                             onChange={e => setTaslak({ ...taslak, [a.ad]: { ozel: true, deger: e.target.value } })}
                             className="w-full min-w-0 rounded-md border border-slate-300 dark:border-dark-500 bg-white dark:bg-dark-900 px-2.5 py-1.5 text-sm tabular-nums text-slate-900 dark:text-slate-100 disabled:bg-slate-50 disabled:text-slate-400 dark:disabled:bg-dark-800 dark:disabled:text-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500" />
                      {a.birim !== 'adet' && a.birim !== 'ağırlık' && (
                        <span className="shrink-0 text-xs text-slate-500 dark:text-slate-400">{a.birim}</span>
                      )}
                    </div>
                    <p className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                      {cevir('Plan')}: <span className="font-medium tabular-nums text-slate-600 dark:text-slate-300">{planMetni}</span>
                      <span aria-hidden="true"> · </span>{sifirIpucu(a)}
                    </p>
                  </li>
                )
              })}
            </ul>
          </fieldset>
        )
      })}

      <div className="mt-4 flex flex-wrap gap-2">
        <Button type="button" color="primary" ikon={I.disket} onClick={kaydet}
                disabled={isleniyor !== null || !degisti} yukleniyor={isleniyor === 'kaydet'}>
          {isleniyor === 'kaydet' ? cevir('Kaydediliyor…') : cevir('Kaydet ve uygula')}
        </Button>
        {ozelVar && (
          <Button type="button" variant="outlined" ikon={I.geriAl} onClick={sifirla}
                  disabled={isleniyor !== null} yukleniyor={isleniyor === 'sifirla'}>
            {cevir('Plan değerlerine dön')}
          </Button>
        )}
      </div>
    </section>
  )
}
