// Hosting müşterisi — "Website Security" (/abonelikler/:id/guvenlik).
//
// Müşteri yalnız KENDİ hesabının ana ve ek alan adlarını görür; kapsamı sunucu zorlar
// (jetondaki domain; GET/POST /domains/{id}/guvenlik…, internal/websec/musteri.go).
// Bayi/yönetici sayfasındaki (WebsiteSecurityPage) "Tara" geri bildirimi AYNI mantıkla:
// 2 sn zincirleme setTimeout yoklaması, gizli sekmede durur, bileşen kalkınca temizlenir,
// 3 dk zaman aşımı, bitince toast (lib/websecTarama.ts ortak).
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, apiHata } from '@/lib/api'
import { musteriMi, useRol } from '@/lib/rol'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Badge, Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { TaramaButonu } from '@/components/TaramaButonu'
import { YOKLAMA_MS, degerlendir, satirAnahtari, zamanYaz, type Bekleyen } from '@/lib/websecTarama'
import {
  ONEMLER, appAd, cevir, oneriMetni, sablon,
  type GuvBulgu, type GuvBulguSayfa, type GuvListe, type GuvSatir, type Onem,
} from '@/lib/musteriGuvenlik'

const durumKodu = (e: unknown): number | undefined => (e as { response?: { status?: number } })?.response?.status

const ONEM_ROZET: Record<Onem, { color: 'error' | 'warning' | 'neutral'; variant: 'filled' | 'soft'; ad: string }> = {
  critical: { color: 'error', variant: 'filled', ad: 'Kritik' },
  high: { color: 'error', variant: 'soft', ad: 'Yüksek' },
  medium: { color: 'warning', variant: 'soft', ad: 'Orta' },
  low: { color: 'neutral', variant: 'soft', ad: 'Düşük' },
}
const ONEM_ARKA: Record<Onem, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300',
  high: 'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
  medium: 'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  low: 'bg-slate-100 text-slate-700 dark:bg-dark-700 dark:text-slate-300',
}

const SAYFA_BOYUT = 50

function DurumRozet({ s, bekliyor }: { s: GuvSatir; bekliyor: boolean }) {
  switch (bekliyor && s.durum !== 'sirada' ? 'taraniyor' : s.durum) {
    case 'taraniyor':
      return (
        <Badge color="info" variant="soft" className="inline-flex items-center gap-1.5 px-2 py-0.5 text-[11px] font-medium">
          <span className="size-1.5 animate-pulse rounded-full bg-current" aria-hidden="true" /> {cevir('Taranıyor…')}
        </Badge>
      )
    case 'sirada':
      return (
        <Badge color="info" variant="soft" className="inline-flex items-center gap-1.5 px-2 py-0.5 text-[11px] font-medium">
          <Ikon d={I.saat} className="size-3 shrink-0" /> {cevir('Sırada')}
        </Badge>
      )
    case 'acik':
      return <Badge color="error" variant="soft" className="px-2 py-0.5 text-[11px] font-semibold">{s.bulgu_sayisi} {cevir('açık')}</Badge>
    case 'temiz':
      return <Badge color="success" variant="soft" className="px-2 py-0.5 text-[11px] font-medium">{cevir('Temiz')}</Badge>
    case 'desteklenmiyor':
      return (
        <span title={cevir('Bu alan adında taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.')}>
          <Badge variant="soft" className="px-2 py-0.5 text-[11px]">{cevir('Uygulama yok')}</Badge>
        </span>
      )
    default:
      return <Badge color="warning" variant="soft" className="px-2 py-0.5 text-[11px]">{cevir('Beklemede')}</Badge>
  }
}

function EkRozeti() {
  return (
    <span title={cevir('Ek alan adı')} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-600 dark:bg-dark-700 dark:text-slate-300">
      {cevir('Ek')}
    </span>
  )
}

function SayacKart({ etiket, sayi, renk }: { etiket: string; sayi: number; renk: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white px-3 py-2.5 dark:border-dark-600 dark:bg-dark-800">
      <div className="text-[11px] font-medium uppercase tracking-wider text-slate-500">{etiket}</div>
      <div className={`mt-1 inline-block rounded-md px-2 py-0.5 text-xl font-semibold tabular-nums ${renk}`}>{sayi}</div>
    </div>
  )
}

// Bir alan adının açıkları: önem süzgeci + paket/sürüm/CVE/düzeltme sürümü/öneri.
function BulguPaneli({ domainId, satir, yenile }: { domainId: number; satir: GuvSatir; yenile: number }) {
  const [filtre, setFiltre] = useState('')
  const [sayfa, setSayfa] = useState(1)
  const [veri, setVeri] = useState<GuvBulguSayfa | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const ekID = satir.ek_alan_id ?? 0

  useEffect(() => {
    let iptal = false
    setYukleniyor(true)
    setHata(null)
    const p = new URLSearchParams({ ek_alan_id: String(ekID), page: '1', page_size: String(SAYFA_BOYUT * sayfa) })
    if (filtre) p.set('severity', filtre)
    api.get<GuvBulguSayfa>(`/domains/${domainId}/guvenlik/bulgular?${p.toString()}`)
      .then((r) => { if (!iptal) setVeri(r.data) })
      .catch((e: unknown) => { if (!iptal) setHata(apiHata(e, cevir('Açıklar alınamadı'))) })
      .finally(() => { if (!iptal) setYukleniyor(false) })
    return () => { iptal = true }
  }, [domainId, ekID, filtre, sayfa, yenile])

  const filtreler: [string, string][] = [
    ['', cevir('Hepsi')],
    ...ONEMLER.map((o): [string, string] => [o, sablon(`${ONEM_ROZET[o].ad} {0}`, satir.onem[o])]),
  ]
  return (
    <div className="border-t border-slate-100 bg-slate-50/60 px-3 py-3 dark:border-dark-600 dark:bg-dark-900/40">
      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <span className="me-1 text-slate-500">{cevir('Filtre:')}</span>
        {filtreler.map(([v, ad]) => (
          <button key={v} type="button" onClick={() => { setFiltre(v); setSayfa(1) }} aria-pressed={filtre === v}
            className={`min-h-8 rounded-md border px-2 py-1 ${filtre === v ? 'border-dark-700 bg-dark-800 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-dark-600 dark:text-slate-400 dark:hover:bg-dark-700'}`}>
            {ad}
          </button>
        ))}
      </div>
      {hata ? (
        <UyariKutusu ton="tehlike" kucuk>{hata}</UyariKutusu>
      ) : !veri ? (
        <div className="py-4 text-center text-sm text-slate-500">{cevir('Yükleniyor…')}</div>
      ) : veri.items.length === 0 ? (
        <UyariKutusu ton="basari" kucuk>{filtre ? cevir('Bu filtreyle açık yok.') : cevir('Bu alan adında bilinen açık bulunmadı.')}</UyariKutusu>
      ) : (
        <>
          <ul className="space-y-2">
            {veri.items.map((b: GuvBulgu) => {
              const o = (ONEM_ROZET[b.onem as Onem] ?? ONEM_ROZET.low)
              return (
                <li key={b.id} className="rounded-lg border border-slate-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                    <Badge color={o.color} variant={o.variant} className="whitespace-nowrap text-[11px]">
                      {cevir(ONEM_ROZET[b.onem as Onem]?.ad ?? b.onem)}{b.cvss ? ' · ' + b.cvss.toFixed(1) : ''}
                    </Badge>
                    <span className="min-w-0 break-all font-mono text-[13px] text-slate-900 dark:text-slate-100">{b.paket}</span>
                    <span className="inline-flex flex-wrap items-center gap-1 font-mono text-[12px] text-slate-600 dark:text-slate-300">
                      <span title={cevir('Kurulu sürüm')}>{b.surum}</span>
                      {b.duzeltme_surumu && (
                        <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400" title={cevir('Düzeltme sürümü')}>
                          <Ikon d={I.ileri} className="size-3 shrink-0" />{b.duzeltme_surumu}
                        </span>
                      )}
                    </span>
                    {b.baglanti ? (
                      <a href={b.baglanti} target="_blank" rel="noreferrer noopener"
                        aria-label={sablon('Güvenlik bildirimi ({0}) — yeni sekmede açılır', b.cve)}
                        title={sablon('Güvenlik bildirimi ({0}) — yeni sekmede açılır', b.cve)}
                        className="ms-auto inline-flex items-center gap-1 font-mono text-[12px] text-primary-600 hover:underline dark:text-primary-400">
                        {b.cve}<Ikon d={I.harici} className="size-3 shrink-0" />
                      </a>
                    ) : (
                      <span className="ms-auto font-mono text-[12px] text-slate-500">{b.cve}</span>
                    )}
                  </div>
                  {b.baslik && <div className="mt-1.5 line-clamp-2 text-xs text-slate-500">{b.baslik}</div>}
                  <div className="mt-2 flex items-start gap-1.5 rounded-md bg-primary-50/60 px-2 py-1.5 text-xs text-slate-700 dark:bg-primary-400/10 dark:text-slate-200">
                    <Ikon d={I.onayDaire} className="mt-0.5 size-3.5 shrink-0 text-primary-600 dark:text-primary-300" />
                    <span><span className="font-semibold">{cevir('Öneri')}: </span>{oneriMetni(b)}</span>
                  </div>
                </li>
              )
            })}
          </ul>
          {veri.toplam > veri.items.length && (
            <div className="mt-3 text-center">
              <Button variant="outlined" boyut="sm" onClick={() => setSayfa((n) => n + 1)} yukleniyor={yukleniyor}>{cevir('Daha fazla göster')}</Button>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default function MusteriGuvenlikPage() {
  useTranslation() // dil re-render aboneliği
  const { id } = useParams<{ id: string }>()
  const domainId = Number(id)
  const gecerli = Number.isSafeInteger(domainId) && domainId > 0
  // Uçlar yalnız hosting hesabı (müşteri / adına) oturumuna açıktır; yönetici ve bayi kendi
  // sayfalarını (Website Security Monitor) kullanır — bu yüzden onlara istek ATILMAZ.
  const musteri = musteriMi(useRol())
  const [liste, setListe] = useState<GuvListe | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [bulunamadi, setBulunamadi] = useState(false)
  const [acik, setAcik] = useState<number | null>(null) // açılmış satırın hedef anahtarı
  const [panelYenile, setPanelYenile] = useState(0)
  const [baslatan, setBaslatan] = useState<number | null>(null) // istek uçuşta olan satır
  const [ilkTaramaYapiliyor, setIlkTaramaYapiliyor] = useState(false)
  // Sonucu beklenen alan adları (istek sonrası): satır "Taranıyor…" gösterir, bitince toast.
  const [bekleyenIds, setBekleyenIds] = useState<Set<number>>(new Set())
  const bekleyen = useRef<Map<number, Bekleyen>>(new Map())
  const listeRef = useRef<GuvListe | null>(null)
  const yukluyor = useRef(false) // istekler üst üste binmesin
  const canli = useRef(true) // bileşen mount'lu mu
  const ilkYuklendi = useRef(false)
  const acikRef = useRef<number | null>(null)

  const dialog = useDialog()
  const toast = useToast()

  const bekleyenYaz = () => setBekleyenIds(new Set(bekleyen.current.keys()))

  // Bekleyenlerin sonucunu değerlendir: biten varsa tek toast; zaman aşımı → bilgi ver.
  const sonucuDegerlendir = (l: GuvListe) => {
    if (bekleyen.current.size === 0) return
    const d = degerlendir(bekleyen.current, l.items, Date.now())
    if (d.biten.length === 0 && d.zamanAsimi.length === 0) {
      bekleyen.current = d.kalan
      return
    }
    bekleyen.current = d.kalan
    bekleyenYaz()
    if (d.biten.length > 0) {
      if (d.uygulama === 0) toast.bilgi(cevir('Tarama tamamlandı'), cevir('Taranacak uygulama bulunamadı (WordPress, Node.js veya PHP Composer yok).'))
      else toast.basari(cevir('Tarama tamamlandı'), sablon('{0} uygulama tarandı, {1} bulgu.', d.uygulama, d.bulgu))
      if (acikRef.current !== null && d.biten.includes(acikRef.current)) setPanelYenile((n) => n + 1)
    }
    if (d.zamanAsimi.length > 0) {
      toast.uyari(cevir('Tarama beklenenden uzun sürüyor'), cevir('Sonuç gelince bu sayfada görünür; sayfayı yenileyebilirsiniz.'))
    }
  }

  // taze: sürmekte olan yüklemenin BİTMESİNİ bekleyip yeniden yükler (üst üste binmez).
  const yukle = async (taze = false) => {
    if (!gecerli || !musteri) return
    if (yukluyor.current) {
      if (!taze) return
      while (yukluyor.current) await new Promise((r) => setTimeout(r, 50))
    }
    yukluyor.current = true
    if (!ilkYuklendi.current) setYukleniyor(true)
    setHata(null)
    try {
      const r = await api.get<GuvListe>(`/domains/${domainId}/guvenlik`)
      if (!canli.current) return
      setListe(r.data)
      listeRef.current = r.data
      ilkYuklendi.current = true
      sonucuDegerlendir(r.data)
    } catch (e) {
      if (!canli.current) return
      if (durumKodu(e) === 404) setBulunamadi(true)
      else setHata(apiHata(e, cevir('Güvenlik bilgileri alınamadı')))
    } finally { yukluyor.current = false; setYukleniyor(false) }
  }
  useEffect(() => {
    canli.current = true
    void yukle()
    return () => { canli.current = false }
  }, [domainId])

  // Yoklama: tarama aktifken (özet taranıyor / satır taranıyor-sırada / sonucu beklenen
  // alan adı var) YOKLAMA_MS aralıkla tazele; bitince dur. Sekme gizliyken yoklama yok
  // (görününce hemen tazelenir); önceki istek bitmeden yenisi başlamaz (zincirleme
  // setTimeout); bileşen kalkınca zamanlayıcı ve dinleyici temizlenir.
  const aktifTarama = !!liste?.ozet.taraniyor
    || (liste?.items.some((u) => u.durum === 'taraniyor' || u.durum === 'sirada') ?? false)
    || bekleyenIds.size > 0
  useEffect(() => {
    if (!aktifTarama) return
    let zamanlayici: ReturnType<typeof setTimeout> | undefined
    let durdu = false
    const planla = () => {
      if (durdu) return
      zamanlayici = setTimeout(async () => {
        if (!document.hidden) await yukle()
        planla()
      }, YOKLAMA_MS)
    }
    const gorunur = () => { if (!document.hidden) void yukle() }
    document.addEventListener('visibilitychange', gorunur)
    planla()
    return () => {
      durdu = true
      if (zamanlayici) clearTimeout(zamanlayici)
      document.removeEventListener('visibilitychange', gorunur)
    }
  }, [aktifTarama])

  // Tek hedefi kuyruğa yazar; arayüz bildirimi çağıranın işi. Dönüş: ok | zaten | sinir | yok | hata.
  type Sonuc = { kod: 'ok' | 'zaten' | 'sinir' | 'yok' | 'hata'; mesaj: string }
  const postTara = async (s: GuvSatir): Promise<Sonuc> => {
    try {
      const r = await api.post<{ sirada?: number; zaten?: boolean }>(`/domains/${domainId}/guvenlik/tara`, { ek_alan_id: s.ek_alan_id ?? 0 })
      return { kod: r.data?.zaten ? 'zaten' : 'ok', mesaj: '' }
    } catch (e) {
      const k = durumKodu(e)
      const m = apiHata(e, cevir('Tarama başlatılamadı. Lütfen biraz sonra tekrar deneyin.'))
      return { kod: k === 429 ? 'sinir' : k === 404 ? 'yok' : 'hata', mesaj: m }
    }
  }

  // İstek öncesi damgalar: bitiş "son tarama damgası değişti" ile anlaşılır.
  const oncekiDamgalar = () => {
    const m = new Map<number, string>()
    for (const u of listeRef.current?.items ?? []) m.set(satirAnahtari(u), u.domain_son_tarama ?? '')
    return m
  }
  const bekleyeneEkle = (anahtarlar: number[], onceki: Map<number, string>) => {
    const t0 = Date.now()
    for (const a of anahtarlar) bekleyen.current.set(a, { onceki: onceki.get(a) ?? '', aktifGoruldu: false, t0 })
    if (listeRef.current) sonucuDegerlendir(listeRef.current)
    bekleyenYaz()
  }

  const satirTara = async (s: GuvSatir) => {
    const a = satirAnahtari(s)
    const onceki = oncekiDamgalar()
    setBaslatan(a)
    try {
      const r = await postTara(s)
      if (r.kod === 'sinir') { await dialog.bilgi({ baslik: cevir('Tarama sınırı'), mesaj: r.mesaj, ton: 'uyari' }); return }
      if (r.kod === 'yok') { await dialog.bilgi({ baslik: cevir('Başlatılamadı'), mesaj: r.mesaj, ton: 'tehlike' }); await yukle(true); return }
      if (r.kod === 'hata') { await dialog.bilgi({ baslik: cevir('Başlatılamadı'), mesaj: r.mesaj, ton: 'tehlike' }); return }
      if (r.kod === 'zaten') toast.bilgi(cevir('Zaten sırada ya da taranıyor'), cevir('Bu alan adı için tarama zaten sırada ya da sürüyor.'))
      else toast.bilgi(cevir('Tarama sıraya alındı'), s.alan_adi)
      await yukle(true)
      bekleyeneEkle([a], onceki)
    } finally { setBaslatan(null) }
  }

  // Hiç taranmamış hesapta tüm satırları sırayla kuyruğa yazar (tek tek; her biri kendi
  // soğuma/günlük kapısından geçer). Sınıra çarparsa kalanlar bırakılır.
  const ilkTarama = async () => {
    const hedefler = (listeRef.current?.items ?? []).filter((u) => u.durum === 'beklemede')
    if (hedefler.length === 0) return
    const onceki = oncekiDamgalar()
    setIlkTaramaYapiliyor(true)
    try {
      const alinan: number[] = []
      let sonHata = ''
      for (const s of hedefler) {
        const r = await postTara(s)
        if (r.kod === 'ok' || r.kod === 'zaten') alinan.push(satirAnahtari(s))
        else { sonHata = r.mesaj; if (r.kod === 'sinir') break }
      }
      if (alinan.length > 0) toast.bilgi(cevir('Tarama sıraya alındı'), sablon('{0} alan adı tarama sırasına alındı.', alinan.length))
      else if (sonHata) await dialog.bilgi({ baslik: cevir('Başlatılamadı'), mesaj: sonHata, ton: 'uyari' })
      await yukle(true)
      bekleyeneEkle(alinan, onceki)
    } finally { setIlkTaramaYapiliyor(false) }
  }

  const ac = (s: GuvSatir) => {
    const a = satirAnahtari(s)
    const yeni = acik === a ? null : a
    acikRef.current = yeni
    setAcik(yeni)
  }
  const acilabilir = (s: GuvSatir) => s.durum === 'acik' || s.durum === 'temiz'
  const taramaDurumu = (s: GuvSatir) => {
    const a = satirAnahtari(s)
    return s.durum === 'taraniyor' || (bekleyenIds.has(a) && s.durum !== 'sirada') ? 'taraniyor' as const
      : s.durum === 'sirada' ? 'sirada' as const
      : (baslatan === a || ilkTaramaYapiliyor) ? 'baslatiliyor' as const
      : 'hazir' as const
  }
  const taraDugmesi = (s: GuvSatir) => (
    <TaramaButonu
      durum={taramaDurumu(s)}
      etiket={cevir('Tara')}
      baglam={s.alan_adi}
      acikSayisi={s.durum === 'acik' ? s.bulgu_sayisi : 0}
      onClick={(e) => { e.stopPropagation(); void satirTara(s) }}
    />
  )

  const alanAdi = liste?.items[0]?.alan_adi || '…'
  const kirinti = (
    <Breadcrumb items={[
      { etiket: cevir('Anasayfa'), href: '/' },
      { etiket: alanAdi, href: `/abonelikler/${id}` },
      { etiket: 'Website Security' },
    ]} />
  )

  if (!musteri) {
    return (
      <div>
        {kirinti}
        <UyariKutusu ton="bilgi" className="mt-4">
          {cevir('Bu sayfa hosting hesabı oturumuna özeldir. Hesapları görmek için Website Security Monitor sayfasını kullanın.')}
          <div className="mt-2">
            <Link to="/website-security" className="font-medium text-primary-600 hover:underline dark:text-primary-400">Website Security Monitor</Link>
          </div>
        </UyariKutusu>
      </div>
    )
  }

  if (!gecerli || bulunamadi) {
    return (
      <div>
        {kirinti}
        <UyariKutusu ton="uyari" className="mt-4">{cevir('Alan adı bulunamadı.')}</UyariKutusu>
      </div>
    )
  }

  const hicTaranmamis = !!liste && liste.ozet.durum === 'beklemede' && !liste.ozet.taraniyor && bekleyenIds.size === 0
    && liste.items.every((u) => u.durum === 'beklemede')

  return (
    <div>
      {kirinti}
      <div className="mt-4 mb-5">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Website Security</h1>
        <p className="mt-1.5 max-w-3xl text-sm text-slate-600 dark:text-slate-400">
          {cevir('Hesabınızdaki alan adlarını (ana ve ek alan adları) WordPress eklentileri, temalar ve Node.js / PHP Composer paketleri açısından bilinen güvenlik açıklarına karşı tarar.')}
        </p>
      </div>

      {yukleniyor && !ilkYuklendi.current ? (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600" aria-busy="true">{cevir('Yükleniyor…')}</div>
      ) : hata && !liste ? (
        <UyariKutusu ton="tehlike">
          {hata}
          <div className="mt-2">
            <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={() => void yukle(true)} yukleniyor={yukleniyor}>{cevir('Tekrar dene')}</Button>
          </div>
        </UyariKutusu>
      ) : liste && (
        <>
          {/* Özet şeridi */}
          <section aria-label={cevir('Güvenlik özeti')} className="mb-4">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {ONEMLER.map((o) => <SayacKart key={o} etiket={cevir(ONEM_ROZET[o].ad)} sayi={liste.ozet[o]} renk={ONEM_ARKA[o]} />)}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-500">
              <span>{cevir('Son tarama')}: <span className="font-medium text-slate-700 dark:text-slate-200">{zamanYaz(liste.ozet.son_tarama)}</span></span>
              {liste.ozet.taraniyor && (
                <Badge color="info" variant="soft" className="gap-1.5 text-[11px]">
                  <span className="size-1.5 animate-pulse rounded-full bg-current" aria-hidden="true" /> {cevir('Taranıyor…')}
                </Badge>
              )}
            </div>
          </section>

          {liste.feed_uyari && (
            <UyariKutusu ton="uyari" className="mb-4" kucuk>
              {cevir('Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.')}
            </UyariKutusu>
          )}
          {hata && <UyariKutusu ton="tehlike" className="mb-4" kucuk>{hata}</UyariKutusu>}

          {hicTaranmamis ? (
            <div className="rounded-lg border border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-800">
              <EmptyState
                baslik={cevir('Henüz tarama yapılmadı')}
                aciklama={cevir('Alan adlarınız ilk kez taranacak. Sonuçlar tarama bitince burada görünür.')}
                buton={{ etiket: cevir('İlk taramayı başlat'), onClick: () => { void ilkTarama() }, disabled: ilkTaramaYapiliyor }}
              />
            </div>
          ) : (
            <>
              {/* Geniş ekran: tablo */}
              <div className="hidden overflow-hidden rounded-lg border border-slate-200 md:block dark:border-dark-600">
                <table className="w-full table-fixed text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                    <tr>
                      <th className="w-[30%] px-3 py-3 font-semibold">{cevir('Alan adı')}</th>
                      <th className="w-[20%] px-3 py-3 font-semibold">{cevir('Tür')}</th>
                      <th className="w-[8%] px-3 py-3 text-right font-semibold">{cevir('Paket')}</th>
                      <th className="w-[16%] px-3 py-3 font-semibold">{cevir('Durum')}</th>
                      <th className="w-[14%] px-3 py-3 font-semibold">{cevir('Son tarama')}</th>
                      <th className="w-[12%] px-3 py-3 text-right font-semibold">{cevir('İşlem')}</th>
                    </tr>
                  </thead>
                  {liste.items.map((u) => {
                    const a = satirAnahtari(u)
                    const bek = bekleyenIds.has(a)
                    const acilir = acilabilir(u)
                    const acikMi = acik === a && acilir
                    return (
                      <tbody key={a} className="divide-y divide-slate-100 border-t border-slate-100 dark:divide-dark-600 dark:border-dark-600">
                        <tr className="bg-white dark:bg-dark-900">
                          <td className="px-3 py-2.5 font-medium text-slate-900 dark:text-slate-100">
                            <div className="flex flex-wrap items-center gap-1.5">
                              {acilir ? (
                                <button type="button" onClick={() => ac(u)} aria-expanded={acikMi}
                                  title={acikMi ? cevir('Bu alan adının açıklarını gizle') : cevir('Bu alan adının açıklarını göster')}
                                  className="inline-flex min-w-0 items-center gap-1 text-start hover:underline">
                                  <Ikon d={I.chevronAsagi} className={`size-3.5 shrink-0 transition-transform ${acikMi ? 'rotate-180' : '-rotate-90'}`} />
                                  <span className="break-all">{u.alan_adi}</span>
                                </button>
                              ) : <span className="break-all ps-4">{u.alan_adi}</span>}
                              {u.ek && <EkRozeti />}
                            </div>
                          </td>
                          <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">
                            {u.app_type ? <span>{appAd(u.app_type)}{u.app_version ? ' ' + u.app_version : ''}{u.uygulama_sayisi > 1 ? ` +${u.uygulama_sayisi - 1}` : ''}</span> : <span className="text-slate-400">—</span>}
                          </td>
                          <td className="px-3 py-2.5 text-right tabular-nums">{u.paket_sayisi || '—'}</td>
                          <td className="px-3 py-2.5">
                            <DurumRozet s={u} bekliyor={bek} />
                            {u.durum === 'desteklenmiyor' && !bek && (
                              <div className="mt-1 text-[11px] leading-snug text-slate-500">
                                {cevir('Bu alan adında taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.')}
                              </div>
                            )}
                          </td>
                          <td className="px-3 py-2.5 text-xs text-slate-500">{zamanYaz(u.domain_son_tarama)}</td>
                          <td className="px-3 py-2.5 text-right">{taraDugmesi(u)}</td>
                        </tr>
                        {acikMi && (
                          <tr><td colSpan={6} className="p-0"><BulguPaneli domainId={domainId} satir={u} yenile={panelYenile} /></td></tr>
                        )}
                      </tbody>
                    )
                  })}
                </table>
              </div>

              {/* Dar ekran (375 px): kart listesi, yatay kaydırma yok */}
              <ul className="space-y-2 md:hidden">
                {liste.items.map((u) => {
                  const a = satirAnahtari(u)
                  const bek = bekleyenIds.has(a)
                  const acilir = acilabilir(u)
                  const acikMi = acik === a && acilir
                  return (
                    <li key={a} className="overflow-hidden rounded-lg border border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-900">
                      <div className="flex items-start justify-between gap-3 p-3">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5 font-medium text-slate-900 dark:text-slate-100">
                            {acilir ? (
                              <button type="button" onClick={() => ac(u)} aria-expanded={acikMi}
                                title={acikMi ? cevir('Bu alan adının açıklarını gizle') : cevir('Bu alan adının açıklarını göster')}
                                className="inline-flex min-h-8 min-w-0 items-center gap-1 text-start">
                                <Ikon d={I.chevronAsagi} className={`size-3.5 shrink-0 transition-transform ${acikMi ? 'rotate-180' : '-rotate-90'}`} />
                                <span className="break-all">{u.alan_adi}</span>
                              </button>
                            ) : <span className="break-all">{u.alan_adi}</span>}
                            {u.ek && <EkRozeti />}
                          </div>
                          <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-slate-500">
                            <DurumRozet s={u} bekliyor={bek} />
                            {u.app_type && <span>{appAd(u.app_type)}{u.app_version ? ' ' + u.app_version : ''}</span>}
                            {u.paket_sayisi > 0 && <span>{u.paket_sayisi} {cevir('Paket').toLowerCase()}</span>}
                          </div>
                          <div className="mt-1 text-[11px] text-slate-500">{cevir('Son tarama')}: {zamanYaz(u.domain_son_tarama)}</div>
                          {u.durum === 'desteklenmiyor' && !bek && (
                            <div className="mt-1 text-[11px] leading-snug text-slate-500">
                              {cevir('Bu alan adında taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.')}
                            </div>
                          )}
                        </div>
                        {taraDugmesi(u)}
                      </div>
                      {acikMi && <BulguPaneli domainId={domainId} satir={u} yenile={panelYenile} />}
                    </li>
                  )
                })}
              </ul>

              <p className="mt-3 text-xs text-slate-500">
                {cevir('Alan adı satırına tıklayarak o alan adının açıklarını görün.')}{' '}
                {sablon('Her alan adı en az {0} dakikada bir, hesabınız günde en çok {1} kez taranabilir. Bugün kalan hak: {2}.', liste.tara.soguma_dk, liste.tara.gunluk_sinir, liste.tara.gunluk_kalan)}
              </p>
            </>
          )}
        </>
      )}
    </div>
  )
}
