import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Button, Badge, Card, butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { useUc, type Uc } from '@/components/alanpano/useUc'
import type { KaynakOzet } from '@/components/alanpano/tipler'
import { mb } from '@/components/alanpano/bicim'

// /kaynak yanıtı — tek tanım alanpano/tipler.ts'te (db_mb, eposta_olculmuyor dahil).
export type Ozet = KaynakOzet


const CMP_EN: Record<string, string> = {
  "Alan adı": "Domains",
  "Türkçe": "English",
  "Paket ve Kaynaklar": "Package and Resources",
  "Disk": "Disk",
  "Inode (dosya)": "Inode (files)",
  "adet": "count",
  "hesap": "accounts",
  "E-posta Kutusu": "Email Mailbox",
  "kutu": "boxes",
  "IP Adresi": "IP Address",
  "Yok": "None",
  "Yedek": "Backup",
  "Yedek boyutu": "Backup size",
  "Kaynak bilgisi alınamadı.": "Could not load resource info.",
  "Yeniden dene": "Retry",
  "DB boyutu": "DB size",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// kaynak — üst sayfa /kaynak'ı zaten çekiyorsa durumu buradan verir (sayfa başına TEK
// çekim; bkz. SubscriptionDetailPage). Verilmezse kart kendi çekimini yapar (eski davranış).
export default function DomainKaynakKart({ domainId, kaynak }: { domainId: number | string; kaynak?: Uc<Ozet> }) {
  useTranslation() // dil re-render aboneligi
  // Kontrollü kullanımda (kaynak verildi) url null → kendi isteği YOK.
  const ic = useUc<Ozet>(kaynak ? null : `/domains/${domainId}/kaynak`)
  const k = kaynak ?? ic
  const ozet = k.veri
  const yukle = () => { void k.yenile() }

  // Yenileme sırasında eldeki veri korunur (yalnız düğme döner); iskelet yalnız ilk yüklemede.
  if (!ozet && k.yukleniyor) {
    return (
      <Card className="bg-white dark:bg-dark-700 p-5">
        {/*
          🔴 KOYU TEMADA GÖRÜNÜR OLMALI. Önceki hâlde çubuklar `dark:bg-dark-700`
          idi — kartın kendi arka planıyla AYNI renk. Sonuç: yükleme sırasında
          kutu tamamen BOŞ görünüyordu ve kullanıcı bunu "sağ bar bekliyor,
          sayfa bozuk" diye bildirdi. Bekleme durumunun görünmemesi, bekleyişi
          arızaya benzetir; `slate-700` karta göre bir ton açık.
        */}
        <div className="h-5 bg-slate-100 dark:bg-dark-600 rounded w-32 mb-3 animate-pulse" />
        <div className="space-y-3">
          {[1, 2, 3, 4].map(i => (
            <div key={i} className="h-3 bg-slate-100 dark:bg-dark-600 rounded animate-pulse" />
          ))}
        </div>
      </Card>
    )
  }
  if (!ozet) {
    // 🔴 SESSİZ BOŞLUK YOK. `null` döndürmek, istek başarısız olduğunda sayfada
    // açıklamasız bir boşluk bırakıyordu; kullanıcı bunu "yükleniyor" sanıp
    // bekliyordu. Başarısızlık, bekleyiş gibi görünmemeli.
    return (
      <Card className="bg-white dark:bg-dark-700 p-5 text-sm text-slate-500 dark:text-slate-400">
        <div className="mb-2">{cevir("Kaynak bilgisi alınamadı.")}</div>
        <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={yukle} yukleniyor={k.yukleniyor}>
          {cevir("Yeniden dene")}
        </Button>
      </Card>
    )
  }

  return (
    <div className="space-y-3">
      {/* Plan + Özet */}
      <Card className="bg-white dark:bg-dark-700 p-4">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Paket ve Kaynaklar")}</h3>
          <Button variant="flat" isIcon ikon={I.yenile} onClick={yukle} yukleniyor={k.yukleniyor} className="size-8 rounded-full" title={cevir("Yenile")} aria-label={cevir("Yenile")} />
        </div>

        <div className="mb-3 pb-3 border-b border-slate-100 dark:border-dark-600 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-0.5">{cevir("Hizmet Planı")}</div>
            <div className="text-sm font-semibold text-slate-900 dark:text-slate-100 truncate">{ozet.plan_adi}</div>
          </div>
          {/* Plan aksiyonu: yükseltme / düşürme / bu hostinge özel plan — hepsi plan sayfasında */}
          <Link
            to={`/abonelikler/${domainId}/plan`}
            className={butonSinifi({ variant: 'outlined', boyut: 'sm', className: 'shrink-0' })}
            title={cevir(cevir("Planı yükselt, düşür veya bu hostinge özel plan oluştur"))}
          >
            <Ikon d={I.degistir} className="size-3.5 shrink-0 -ms-0.5 me-1.5" />
            {cevir(cevir("Değiştir"))}
          </Link>
        </div>

        <Bar etiket={cevir("Disk")} k={ozet.disk_mb.kullanim} l={ozet.disk_mb.limit} birim="MB" renk="indigo" />
        {/* Inode (dosya sayısı) kotası — yalnız XFS kota aktifse (limit>0) gösterilir.
            Disk MB dolu değilken cevir("kota aşıldı") (EDQUOT) yaşanmasının nedeni burasıdır;
            kullanıcı yalnız MB gördüğü için sebebi anlaşılmıyordu. */}
        {ozet.inode_limit > 0 && (
          <Bar etiket={cevir("Inode (dosya)")} k={ozet.inode_kullanim} l={ozet.inode_limit} birim={cevir("adet")} renk="teal" />
        )}
        <Bar etiket={cevir(cevir("Trafik (aylık)"))} k={ozet.trafik_mb.kullanim} l={ozet.trafik_mb.limit} birim="MB" renk="sky" />
        <Bar etiket={cevir("Veritabanı")} k={ozet.db_sayisi.kullanim} l={ozet.db_sayisi.limit} birim="DB" renk="emerald" />
        <Bar etiket={cevir("FTP Hesabı")} k={ozet.ftp_sayisi.kullanim} l={ozet.ftp_sayisi.limit} birim={cevir("hesap")} renk="amber" />
        {/* E-posta sayacının gerçek kaynağı yoksa (eposta_olculmuyor) çubuk hep 0 gösterirdi — gizlenir. */}
        {!ozet.eposta_olculmuyor && (
          <Bar etiket={cevir("E-posta Kutusu")} k={ozet.eposta_sayi.kullanim} l={ozet.eposta_sayi.limit} birim={cevir("kutu")} renk="rose" />
        )}
        <Bar etiket={cevir("Alan adı")} k={ozet.domain_sayi.kullanim} l={ozet.domain_sayi.limit} birim="domain" renk="violet" />
      </Card>

      {/* Yapılandırma Özeti */}
      <Card className="bg-white dark:bg-dark-700 p-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Yapılandırma")}</h3>
        <Sat e={cevir("IP Adresi")} d={ozet.ipv4 || '—'} mono />
        <Sat e={cevir("Sistem Kullanıcısı")} d={ozet.sk} mono />
        <Sat e={cevir("PHP Sürümü")}
          d={<span><span className="font-mono font-medium text-slate-800 dark:text-slate-200">PHP {ozet.php_surum}</span></span>}
        />
        <Sat e="SSL/TLS"
          d={
            ozet.ssl_aktif
              ? <span className="flex items-center gap-1.5">
                  <Badge color="success" variant="soft" className="text-xs px-2 py-0.5 gap-1.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-current" aria-hidden="true" />
                    {cevir("Aktif")}
                  </Badge>
                  {ozet.ssl_bitis && <span className="inline-flex items-center gap-1 text-slate-400 dark:text-slate-500 text-[10px]"><Ikon d={I.ileri} className="size-3" />{ozet.ssl_bitis}</span>}
                </span>
              : <Badge variant="soft" className="text-xs px-2 py-0.5 gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-full bg-current opacity-50" aria-hidden="true" />
                  {cevir("Yok")}
                </Badge>
          }
        />
      </Card>

      {/* İlave Sayaclar */}
      <Card className="bg-white dark:bg-dark-700 p-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Sayaçlar")}</h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-y-2 gap-x-3">
          <Mini etiket={cevir(cevir("DNS kayıt"))} deger={ozet.dns_kayit} />
          <Mini etiket={cevir(cevir("Cron işi"))} deger={ozet.cron_is} />
          <Mini etiket={cevir("Yedek")} deger={ozet.yedek_sayisi} />
          <Mini etiket={cevir("Yedek boyutu")} deger={`${ozet.yedek_mb} MB`} />
          {/* DB boyutu ölçülemezse arka uç alanı hiç göndermez → gizli. */}
          {typeof ozet.db_mb === 'number' && <Mini etiket={cevir("DB boyutu")} deger={mb(ozet.db_mb)} />}
        </div>
      </Card>
    </div>
  )
}

// ----- helpers -----
// Canlı gradient bar'lar. Sınırsız (limit=0) durumda soluk 8% stub yerine
// sağa doğru sönümlenen tam-genişlik gradient → cevir("sınırsız/akan") hissi (canlı).
function Bar({ etiket, k, l, birim, renk }: { etiket: string; k: number; l: number; birim: string; renk: string }) {
  const sinirsiz = l === 0
  const oran = sinirsiz ? 0 : Math.min(100, Math.round((k / l) * 100))
  const grad: Record<string, string> = {
    indigo:  'from-indigo-400 to-indigo-600',
    sky:     'from-sky-400 to-sky-600',
    emerald: 'from-emerald-400 to-emerald-600',
    amber:   'from-amber-400 to-amber-600',
    rose:    'from-rose-400 to-rose-600',
    violet:  'from-violet-400 to-violet-600',
  }
  const fill = oran >= 90 ? 'from-red-400 to-red-600' : (oran >= 75 ? 'from-amber-400 to-amber-600' : (grad[renk] || 'from-slate-400 to-slate-600'))
  const fade = 'linear-gradient(to right, black 0%, black 35%, transparent 96%)'
  return (
    <div className="mb-3 last:mb-0">
      <div className="flex items-baseline justify-between mb-1">
        <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{etiket}</span>
        <span className="text-[11px] font-mono text-slate-500 dark:text-slate-400">
          {sinirsiz
            ? <><span className="text-slate-700 dark:text-slate-200 font-semibold">{fmt(k)}</span> {birim} · <span className="text-emerald-500 font-bold">∞</span></>
            : <><span className="text-slate-700 dark:text-slate-200 font-semibold">{fmt(k)}</span> / {fmt(l)} {birim}</>
          }
        </span>
      </div>
      <div className="h-2 rounded-full bg-slate-100 dark:bg-dark-600/50 overflow-hidden">
        {sinirsiz ? (
          <div
            className={`h-full rounded-full bg-gradient-to-r ${grad[renk] || 'from-slate-400 to-slate-600'}`}
            style={{ width: '100%', maskImage: fade, WebkitMaskImage: fade }}
          />
        ) : (
          <div className={`h-full rounded-full bg-gradient-to-r ${fill}`} style={{ width: Math.max(oran, 3) + '%' }} />
        )}
      </div>
    </div>
  )
}
function fmt(n: number) {
  if (n >= 1024) return (n / 1024).toFixed(1) + 'k'
  return String(n)
}
function Sat({ e, d, mono }: { e: string; d: ReactNode; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between py-1.5 border-b border-slate-50 dark:border-dark-600 last:border-0">
      <span className="text-xs text-slate-500 dark:text-slate-500">{e}</span>
      <span className={`text-xs text-slate-700 dark:text-slate-300 text-right ${mono ? 'font-mono' : ''} max-w-[60%] truncate`} title={typeof d === 'string' ? d : undefined}>{d}</span>
    </div>
  )
}
function Mini({ etiket, deger }: { etiket: string; deger: number | string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500">{etiket}</div>
      <div className="text-sm font-mono font-medium text-slate-800 dark:text-slate-200">{deger}</div>
    </div>
  )
}
