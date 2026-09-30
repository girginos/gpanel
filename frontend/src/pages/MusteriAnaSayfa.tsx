// Hosting kullanıcısının (rol 'user') ana sayfası — "Modern klasik" (kullanıcı kararı
// 2026-09-29; tasarım kaynağı gpanel-tasarim __tasarim/secenek4.tsx).
//
// /abonelikler/{id} rotasında YALNIZ müşteriye çizilir (App.tsx AbonelikSayfasi); kök ve
// bayi aynı rotada SubscriptionDetailPage'i görmeye devam eder. Sol menü müşteride yok:
// tüm araçlar buradadır, araç sayfalarından dönüş Breadcrumb ('Anasayfa' → burası) ve
// mobil alt çubuk ile.
//
// 🔴 SAHTE VERİ YOK. Her bölüm müşteriye açık (MusteriScope / rol kapsamlı) bir uçtan:
//   Genel bilgi        GET /domains/{id}                (+ plan adı /kaynak)
//   Kullanım           GET /domains/{id}/kaynak         (tek çekim; elle yenilenir)
//   Alan adları        GET /domains/{id}/subdomain
//   Son yedek, uyarı   GET /domains/{id}/backups
//   PHP desteği        GET /php/versions                (eol)
//   Uyarı (bildirim)   GET /bildirimler                 (müşteri yalnız kendi domaini)
//   Eklenti kapıları   GET /eklentiler                  (müşteriye yalnız açık olanlar)
// Yönetici uçları ÇAĞRILMAZ (403 alan istek bırakılmaz). İstisna: askıdaki hesapta
// TÜM uçlar 403 + askı mesajı döner (RequireAuth / MusteriScope, bkz. useUc askiYaniti)
// → sayfa AskiKutusu çizer (hata değil).
import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { UyariKutusu } from '@/components/UyariKutusu'
import type { Domain } from '@/components/domainTipi'
import type { KaynakOzet } from '@/components/alanpano/tipler'
import { askiYaniti, useUc } from '@/components/alanpano/useUc'
import { kaydet, oku } from '@/lib/kalici'
import { MUSTERI_ARACLAR, eklentiKapisi } from '@/components/musteri-ana/araclar'
import AracIzgarasi from '@/components/musteri-ana/AracIzgarasi'
import BilgiPaneli from '@/components/musteri-ana/BilgiPaneli'
import UyariBandi from '@/components/musteri-ana/UyariBandi'
import { AskiKutusu, YenidenDene } from '@/components/musteri-ana/ortak'
import { cevir, cevirF } from '@/components/musteri-ana/dil'
import { alanSatirlari, eolSurumleri, sslDurumu, uyarilariTuret } from '@/components/musteri-ana/hesap'
import type { AltAlan, BildirimYaniti, EklentiKaydi, PhpSurum, Yedek } from '@/components/musteri-ana/tipler'

// "Gizle" denen uyarı bu tarayıcıda 24 saat gizli kalır; durum değişirse (ör. SSL
// yenilendi, yeni bildirim) maddenin kimliği değişir ve yeniden görünür.
const GIZLI_SURE = 24 * 3600 * 1000

function Iskelet() {
  const blok = 'animate-pulse rounded-lg bg-gray-150 dark:bg-dark-600'
  return (
    <div className="mx-auto max-w-[1440px] px-4 py-6 sm:px-6" aria-busy="true">
      <span className="sr-only">{cevir('Yükleniyor…')}</span>
      <div className={`h-11 ${blok}`} />
      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="space-y-3">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className={`h-24 ${blok}`} />
          ))}
        </div>
        <div className={`hidden h-80 lg:block ${blok}`} />
      </div>
    </div>
  )
}

export default function MusteriAnaSayfa() {
  useTranslation() // dil değişince yeniden çiz
  const { id } = useParams()
  const domainId = Number(id)
  const kok = id ? `/domains/${id}` : null
  const domain = useUc<Domain>(kok, cevir('Hesap bilgileri alınamadı'))
  const kaynak = useUc<KaynakOzet>(kok && `${kok}/kaynak`, cevir('Kaynak bilgisi alınamadı'))
  const altlar = useUc<AltAlan[]>(kok && `${kok}/subdomain`)
  const yedekler = useUc<Yedek[]>(kok && `${kok}/backups`)
  const surumler = useUc<PhpSurum[]>('/php/versions')
  const bildirim = useUc<BildirimYaniti>('/bildirimler')
  const eklentiler = useUc<EklentiKaydi[]>('/eklentiler')

  const [seciliAnahtar, setSeciliAnahtar] = useState('ana')
  const [bantAcik, setBantAcik] = useState(false)
  const [bilgiAcik, setBilgiAcik] = useState(false)
  const gizliAnahtar = `musteri-ana.gizli-uyarilar.${id}`
  const [gizli, setGizli] = useState<Set<string>>(() => new Set(oku<string[]>(gizliAnahtar) ?? []))
  const gizle = (uyariId: string) => {
    const n = new Set(gizli).add(uyariId)
    setGizli(n)
    kaydet(gizliAnahtar, [...n], GIZLI_SURE)
  }

  if (!domain.veri) {
    if (domain.yukleniyor || !id) return <Iskelet />
    return (
      <div className="@container/sayfa">
        <div className="mx-auto max-w-3xl px-4 py-6 sm:px-6">
          <h1 className="sr-only">{cevir('Ana sayfa')}</h1>
          {askiYaniti(domain) ? (
            <AskiKutusu />
          ) : (
            <UyariKutusu ton="tehlike" baslik={cevir('Hesap bilgileri alınamadı')}>
              <p>{domain.hata}</p>
              <YenidenDene uc={domain} className="mt-3" />
            </UyariKutusu>
          )}
        </div>
      </div>
    )
  }

  const d = domain.veri
  const simdi = new Date()
  const eol = eolSurumleri(surumler.veri)
  const satirlar = alanSatirlari(d, altlar.veri, eol)
  const ssl = sslDurumu(d, simdi)
  const uyarilar = uyarilariTuret({
    domainId,
    domain: d,
    kaynak: kaynak.veri,
    altlar: altlar.veri,
    yedekler: yedekler.veri,
    surumler: surumler.veri,
    bildirim: bildirim.veri,
    simdi,
  }).filter((u) => !gizli.has(u.id))

  // Eklenti kapısı (SubscriptionDetailPage ile aynı desen; üst çubuk araması da aynı
  // süzgeç — araclar.ts eklentiKapisi): liste gelmeden eklenti aracı çizilmez. Mail:
  // aktifse araçlar; kurulu ama askıdaysa kategori açıklamayla durur. Çalıştırıcı:
  // kuruluysa görünür, askı durumunu kendi sayfası (EklentiEkrani) söyler.
  const ekl = eklentiler.veri ?? []
  const mailKurulu = ekl.some((e) => e.ad === 'mail')
  const mailAktif = ekl.some((e) => e.ad === 'mail' && e.aktif)
  const araclar = MUSTERI_ARACLAR.filter(eklentiKapisi(eklentiler.veri))
  const secili = satirlar.find((s) => s.anahtar === seciliAnahtar) ? seciliAnahtar : 'ana'

  return (
    <div className="@container/sayfa">
      {/* Sayfanın tek h1'i en başta (görünmez): uyarı bandının ve panelin h2'leri ondan sonra gelir. */}
      <h1 className="sr-only">{cevirF('{0} — ana sayfa', d.alan_adi)}</h1>
      <UyariBandi liste={uyarilar} acik={bantAcik} setAcik={setBantAcik} onGizle={gizle} />
      {/* DOM sırası = dar sayfadaki görsel sıra (odak sırası, WCAG 2.4.3): önce bilgi paneli
          ("Hesap özeti"), sonra araçlar. Geniş sayfada ızgara hücreleriyle yer değiştirir:
          araçlar solda, panel sağda. */}
      <div className="mx-auto flex max-w-[1440px] flex-col gap-4 px-4 py-4 @2xl/sayfa:px-6 @2xl/sayfa:py-6 @4xl/sayfa:grid @4xl/sayfa:grid-cols-[minmax(0,1fr)_300px] @4xl/sayfa:items-start @4xl/sayfa:gap-6 @6xl/sayfa:grid-cols-[minmax(0,1fr)_340px]">
        <aside aria-label={cevir('Hesap bilgileri')} className="min-w-0 @4xl/sayfa:col-start-2 @4xl/sayfa:row-start-1">
          <BilgiPaneli
            domainId={domainId}
            domain={d}
            kaynak={kaynak}
            yedekler={yedekler}
            altlar={altlar}
            satirlar={satirlar}
            ssl={ssl}
            seciliAnahtar={secili}
            onSec={setSeciliAnahtar}
            acik={bilgiAcik}
            setAcik={setBilgiAcik}
          />
        </aside>
        <div className="@container/ana min-w-0 @4xl/sayfa:col-start-1 @4xl/sayfa:row-start-1">
          <AracIzgarasi
            domainId={domainId}
            araclar={araclar}
            mailAskida={mailKurulu && !mailAktif}
            satirlar={satirlar}
            seciliAnahtar={secili}
            onSec={setSeciliAnahtar}
            sistemKullanici={d.sistem_kullanici}
          />
        </div>
      </div>
    </div>
  )
}
