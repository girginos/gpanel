// Araç ızgarası: anında süzen arama ("/" odaklar, Enter ilk sonucu açar), kapsam süzgeci,
// kapsam şeridi (hesap geneli · alan adı başına + seçili alan adı) ve katlanabilir
// kategoriler. Kutucuklar yalnız mevcut, müşteriye açık sayfalara bağlantıdır (araclar.ts).
import { useEffect, useId, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Button, Card, Input } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { kaydet, oku } from '@/lib/kalici'
import { KATEGORILER, aracCevir, aracEslesir, aracHedefi, aramaSozcukleri, sade } from './araclar'
import type { KategoriKodu, MusteriArac } from './araclar'
import { DOKUN, Dugme, KapsamKaresi, TurRozeti } from './ortak'
import { cevir, cevirF, cevirSayili } from './dil'
import { okTusuylaGez, useDisariKapat } from './yardimci'
import type { AlanSatiri } from './tipler'

type KapsamFiltre = 'tumu' | 'hesap' | 'alan'

const KAPALI_ANAHTAR = 'musteri-ana.kapali-kategoriler'
const KAPALI_SURE = 30 * 24 * 3600 * 1000

function AracKutusu({ arac, domainId, secili, ana }: { arac: MusteriArac; domainId: number; secili: AlanSatiri; ana: AlanSatiri }) {
  const alan = arac.kapsam === 'alan'
  const hedef = aracHedefi(arac, domainId, secili.tur === 'alt' ? secili.altId : null)
  const hedefAd = hedef.altAlanda ? secili.ad : ana.ad
  const ipucu = alan ? cevirF('{0} — {1} için açılır', aracCevir(arac.aciklama), hedefAd) : cevirF('{0} — hesap geneli', aracCevir(arac.aciklama))
  return (
    <Link
      to={hedef.yol}
      title={ipucu}
      className="group flex min-h-[3.25rem] w-full min-w-0 items-center gap-3 rounded-lg px-2.5 py-2 text-start transition-colors hover:bg-gray-50 dark:hover:bg-dark-600/60"
    >
      <KapsamKaresi alan={alan} ikon={I[arac.ikon] ?? I.arac} />
      <span className="line-clamp-2 min-w-0 flex-1 text-sm font-medium leading-snug text-gray-800 group-hover:text-primary-700 dark:text-dark-50 dark:group-hover:text-primary-300">
        {aracCevir(arac.ad)}
      </span>
      <span className="sr-only">({alan ? cevirF('{0} için açılır', hedefAd) : cevir('Hesap geneli')})</span>
    </Link>
  )
}

// Alan adı seçici — seçili alan adı her zaman görünür; ok tuşlarıyla gezilir.
function AlanSecici({ satirlar, secili, onSec }: { satirlar: AlanSatiri[]; secili: AlanSatiri; onSec: (anahtar: string) => void }) {
  const [acik, setAcik] = useState(false)
  const kokRef = useRef<HTMLDivElement>(null)
  const dugmeRef = useRef<HTMLButtonElement>(null)
  const listeRef = useRef<HTMLUListElement>(null)
  const listeId = useId()
  useDisariKapat(acik, kokRef, () => {
    const icerde = kokRef.current?.contains(document.activeElement)
    setAcik(false)
    if (icerde) dugmeRef.current?.focus()
  })
  useEffect(() => {
    if (acik) listeRef.current?.querySelector<HTMLButtonElement>('[aria-current="true"]')?.focus()
  }, [acik])
  return (
    <div ref={kokRef} className="relative min-w-0">
      <button
        ref={dugmeRef}
        type="button"
        aria-expanded={acik}
        aria-controls={listeId}
        onClick={() => setAcik((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && !acik) {
            e.preventDefault()
            setAcik(true)
          }
        }}
        className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-start text-sm transition-colors hover:border-gray-400 dark:border-dark-450 dark:bg-dark-800 dark:hover:border-dark-400"
      >
        <Ikon d={I.kure} className="size-4 shrink-0 text-primary-600 dark:text-primary-400" />
        <span className="sr-only">{cevir('Seçili alan adı')}:</span>
        <span className="min-w-0 flex-1 truncate font-medium text-gray-800 dark:text-dark-50">{secili.ad}</span>
        <TurRozeti tur={secili.tur} />
        <Ikon d={I.chevronAsagi} className={`size-4 shrink-0 text-gray-400 transition-transform dark:text-dark-300 ${acik ? 'rotate-180' : ''}`} />
      </button>
      {acik && (
        <div className="absolute inset-x-0 z-20 mt-1 rounded-lg border border-gray-200 bg-white p-1 shadow-soft dark:border-dark-500 dark:bg-dark-700 dark:shadow-none">
          <ul ref={listeRef} id={listeId} aria-label={cevir('Seçili alan adı')} onKeyDown={okTusuylaGez} className="max-h-72 overflow-y-auto">
            {satirlar.map((s) => {
              const aktif = s.anahtar === secili.anahtar
              return (
                <li key={s.anahtar}>
                  <button
                    type="button"
                    aria-current={aktif}
                    onClick={() => {
                      setAcik(false)
                      dugmeRef.current?.focus()
                      onSec(s.anahtar)
                    }}
                    className={`flex min-h-10 w-full min-w-0 items-center gap-2 rounded-md py-2 pe-2.5 text-start text-sm transition-colors ${s.tur === 'alt' ? 'ps-6' : 'ps-2.5'} ${
                      aktif ? 'bg-primary-50 text-primary-700 dark:bg-primary-400/10 dark:text-primary-300' : 'text-gray-700 hover:bg-gray-100 dark:text-dark-100 dark:hover:bg-dark-600'
                    }`}
                  >
                    <Ikon d={I.onay} className={`size-4 shrink-0 ${aktif ? '' : 'invisible'}`} />
                    <span className="min-w-0 flex-1 truncate">{s.ad}</span>
                    <TurRozeti tur={s.tur} />
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}

export default function AracIzgarasi({
  domainId,
  araclar,
  mailAskida,
  satirlar,
  seciliAnahtar,
  onSec,
  sistemKullanici,
}: {
  domainId: number
  araclar: MusteriArac[]
  mailAskida: boolean
  satirlar: AlanSatiri[]
  seciliAnahtar: string
  onSec: (anahtar: string) => void
  sistemKullanici: string
}) {
  const navigate = useNavigate()
  const aramaRef = useRef<HTMLInputElement>(null)
  const [arama, setArama] = useState('')
  const [filtre, setFiltre] = useState<KapsamFiltre>('tumu')
  const [kapali, setKapali] = useState<Set<KategoriKodu>>(() => new Set(oku<KategoriKodu[]>(KAPALI_ANAHTAR) ?? []))
  const [duyuru, setDuyuru] = useState('')
  const ana = satirlar[0]
  const secili = satirlar.find((s) => s.anahtar === seciliAnahtar) ?? ana

  // Katlanan kategoriler bu tarayıcıda hatırlanır (kişisel kolaylık; kritik değil).
  const kapaliDegistir = (n: Set<KategoriKodu>) => {
    setKapali(n)
    kaydet(KAPALI_ANAHTAR, [...n], KAPALI_SURE)
  }

  // "/" ile aramaya odaklan (yazı alanındayken ya da açık pencere varken değil).
  useEffect(() => {
    const tus = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey) return
      const el = e.target as HTMLElement | null
      if (el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))) return
      if (document.querySelector('[aria-modal="true"]')) return
      e.preventDefault()
      aramaRef.current?.focus()
    }
    window.addEventListener('keydown', tus)
    return () => window.removeEventListener('keydown', tus)
  }, [])

  const alanSec = (anahtar: string) => {
    onSec(anahtar)
    const s = satirlar.find((x) => x.anahtar === anahtar)
    if (s) setDuyuru(s.tur === 'ek' ? cevirF('Ek alan adının kendi aracı yok; araçlar {0} için açılır.', ana.ad) : cevirF('Alan adı başına araçlar artık {0} için açılır.', s.ad))
  }

  const sozcukler = aramaSozcukleri(arama)
  const aramaVar = sozcukler.length > 0
  const kategoriler = KATEGORILER.map((k) => {
    const tumu = araclar.filter((a) => a.kategori === k.kod)
    return { ...k, tumu, liste: tumu.filter((a) => (filtre === 'tumu' || a.kapsam === filtre) && aracEslesir(a, sozcukler)) }
  })
  // E-posta eklentisi kurulu ama lisansı askıdaysa kategori bir bilgi satırıyla durur
  // (mevcut kapı deseni: kurulu → görünür, askıda → açıklama; bkz. LisansAskida).
  const gorunenler = kategoriler.filter((k) => k.liste.length > 0 || (k.kod === 'eposta' && mailAskida && !aramaVar && filtre !== 'hesap'))
  const aracSonucu = gorunenler.reduce((n, k) => n + k.liste.length, 0)
  const alanSonucu = aramaVar ? satirlar.filter((s) => sozcukler.every((w) => sade(s.ad).includes(w))) : []
  const sayilar: Record<KapsamFiltre, number> = {
    tumu: araclar.length,
    hesap: araclar.filter((a) => a.kapsam === 'hesap').length,
    alan: araclar.filter((a) => a.kapsam === 'alan').length,
  }
  const kategoriSayisi = kategoriler.filter((k) => k.tumu.length > 0).length
  const hepsiKapali = gorunenler.length > 0 && gorunenler.every((k) => kapali.has(k.kod))

  const aramayaDon = (v: string) => {
    setArama(v)
    aramaRef.current?.focus()
  }
  const aramaTusu = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      if (arama) {
        e.preventDefault()
        setArama('')
      } else aramaRef.current?.blur()
    } else if (e.key === 'Enter' && aramaVar) {
      e.preventDefault()
      const ilk = gorunenler.find((k) => k.liste.length)?.liste[0]
      if (ilk) navigate(aracHedefi(ilk, domainId, secili.tur === 'alt' ? secili.altId : null).yol)
      else if (alanSonucu[0]) {
        alanSec(alanSonucu[0].anahtar)
        setArama('')
      }
    }
  }
  const kategoriDegistir = (kod: KategoriKodu) => {
    const n = new Set(kapali)
    if (n.has(kod)) n.delete(kod)
    else n.add(kod)
    kapaliDegistir(n)
  }

  return (
    <div>
      <div role="search" className="mb-4">
        <Input
          ref={aramaRef}
          type="text"
          value={arama}
          onChange={(e) => setArama(e.target.value)}
          onKeyDown={aramaTusu}
          placeholder={cevir('Araç ara… (ör. ssl, yedek, dns)')}
          aria-label={cevir('Araç ya da alan adı ara')}
          aria-keyshortcuts="/"
          autoComplete="off"
          spellCheck={false}
          prefix={<Ikon d={I.ara} className="size-4" />}
          suffix={
            arama ? (
              // Ek kutusu 36 px genişliğinde: düğme onu ve girdinin tüm yüksekliğini kaplar.
              <Button
                isIcon
                variant="flat"
                ikon={I.kapat}
                onClick={() => aramayaDon('')}
                aria-label={cevir('Aramayı temizle')}
                title={cevir('Aramayı temizle')}
                className="h-full w-9 rounded-e-lg rounded-s-none"
              />
            ) : (
              <kbd className="hidden rounded border border-gray-300 px-1.5 font-sans text-tiny-plus text-gray-500 @2xl/sayfa:inline dark:border-dark-450 dark:text-dark-300">/</kbd>
            )
          }
          classNames={{ input: 'h-11 bg-white py-0 text-sm @2xl/sayfa:h-10 dark:bg-dark-800' }}
        />
      </div>

      <div className="mb-4 flex flex-col gap-3 @xl/ana:flex-row @xl/ana:items-end @xl/ana:justify-between">
        <div>
          {/* Sayfanın h1'i MusteriAnaSayfa'da (görünmez, en başta); burası bölüm başlığı. */}
          <h2 className="text-xl font-semibold text-gray-800 dark:text-dark-50">{cevir('Araçlar')}</h2>
          <p className="mt-0.5 text-sm text-gray-500 dark:text-dark-300">{cevirF('{0} araç · {1} kategori', araclar.length, kategoriSayisi)}</p>
        </div>
        <div className="flex w-full flex-wrap items-center gap-2 @xl/ana:w-auto">
          {/* Dar sayfada üç eşit sütun (metin sarar, taşmaz); geniş sayfada tek satır bölmeli düğme */}
          <div role="group" aria-label={cevir('Kapsama göre süz')} className="grid w-full grid-cols-3 gap-0.5 rounded-lg bg-gray-150 p-0.5 @xl/ana:inline-flex @xl/ana:w-auto dark:bg-dark-800">
            {(['tumu', 'hesap', 'alan'] as const).map((k) => (
              <button
                key={k}
                type="button"
                aria-pressed={filtre === k}
                onClick={() => setFiltre(k)}
                className={`flex min-h-10 min-w-0 flex-wrap items-center justify-center gap-x-1.5 rounded-md px-2 py-1 text-center text-xs-plus font-medium leading-tight transition-colors @xl/ana:flex-nowrap @xl/ana:whitespace-nowrap @xl/ana:px-2.5 @2xl/sayfa:min-h-7 ${
                  filtre === k ? 'bg-white text-gray-800 shadow-sm dark:bg-dark-600 dark:text-dark-50' : 'text-gray-600 hover:text-gray-800 dark:text-dark-200 dark:hover:text-dark-50'
                }`}
              >
                <span>{cevir(k === 'tumu' ? 'Tümü' : k === 'hesap' ? 'Hesap geneli' : 'Alan adı başına')}</span>
                <span className="tabular-nums text-gray-500 dark:text-dark-200">{sayilar[k]}</span>
              </button>
            ))}
          </div>
          {!aramaVar && gorunenler.length > 0 && (
            <Dugme
              variant="flat"
              boyut="sm"
              ikon={hepsiKapali ? I.chevronAsagi : I.chevronYukari}
              onClick={() => kapaliDegistir(hepsiKapali ? new Set() : new Set(gorunenler.map((k) => k.kod)))}
            >
              {cevir(hepsiKapali ? 'Tümünü aç' : 'Tümünü daralt')}
            </Dugme>
          )}
        </div>
      </div>

      {/* Kapsam şeridi — modelin özü + lejant: gri kare hesabın, mavi kare seçili alan adının.
          overflow-hidden YOK: alan adı seçicinin açılır listesi şeridin dışına taşar. */}
      <div className="mb-4 rounded-lg border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-700">
        <div className="grid @2xl/ana:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
          <div className="flex items-start gap-3 p-3.5">
            <KapsamKaresi alan={false} ikon={I.kullanici} />
            <div className="min-w-0">
              <p className="text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Hesap geneli')}</p>
              <p className="mt-0.5 break-words text-xs text-gray-500 dark:text-dark-300">{cevirF('{0} sistem kullanıcısına ait; tüm alan adlarında ortak.', sistemKullanici)}</p>
            </div>
          </div>
          <div className="flex flex-col gap-2.5 border-t border-gray-200 p-3.5 @md/ana:flex-row @md/ana:items-center @2xl/ana:border-s @2xl/ana:border-t-0 dark:border-dark-600">
            <div className="flex min-w-0 flex-1 items-start gap-3">
              <KapsamKaresi alan ikon={I.kure} />
              <div className="min-w-0">
                <p className="text-sm font-semibold text-gray-800 dark:text-dark-50">{cevir('Alan adı başına')}</p>
                <p className="mt-0.5 text-xs text-gray-500 dark:text-dark-300">{cevir('Şu alan adında açılır:')}</p>
              </div>
            </div>
            <div className="min-w-0 @md/ana:w-64 @2xl/ana:w-60 @4xl/ana:w-72">
              {satirlar.length > 1 ? (
                <AlanSecici satirlar={satirlar} secili={secili} onSec={alanSec} />
              ) : (
                <p className="flex min-h-10 min-w-0 items-center gap-2 rounded-lg bg-gray-50 px-3 py-2 text-sm dark:bg-dark-800">
                  <Ikon d={I.kure} className="size-4 shrink-0 text-primary-600 dark:text-primary-400" />
                  <span className="min-w-0 flex-1 truncate font-medium text-gray-800 dark:text-dark-50">{ana.ad}</span>
                  <TurRozeti tur="ana" />
                </p>
              )}
            </div>
          </div>
        </div>
        {secili.tur === 'alt' && (
          <p className="border-t border-gray-200 px-3.5 py-2 text-xs text-gray-500 dark:border-dark-600 dark:text-dark-300">
            {cevirF('Alt alan adı sayfası olmayan araçlar (ör. DNS, WAF) ana alan adında ({0}) açılır.', ana.ad)}
          </p>
        )}
        {secili.tur === 'ek' && (
          <p className="border-t border-gray-200 px-3.5 py-2 text-xs text-gray-500 dark:border-dark-600 dark:text-dark-300">
            {cevirF('Ek alan adının kendi araç sayfası yok; araçlar ana alan adında ({0}) açılır. Ek alan adını yönetmek için alan adları kartındaki oku kullanın.', ana.ad)}
          </p>
        )}
      </div>

      <p className="sr-only" aria-live="polite">
        {aramaVar ? cevirSayili('“{0}” için {1} sonuç', aracSonucu + alanSonucu.length, arama.trim(), aracSonucu + alanSonucu.length) : duyuru}
      </p>

      {aramaVar && (aracSonucu > 0 || alanSonucu.length > 0) && (
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-sm">
          <p className="text-gray-600 dark:text-dark-200">
            {cevirSayili('“{0}” için {1} sonuç', aracSonucu + alanSonucu.length, arama.trim(), aracSonucu + alanSonucu.length)}
            <span className="ms-2 hidden text-xs text-gray-500 @xl/ana:inline dark:text-dark-300">{cevir('Enter ilk sonucu açar')}</span>
          </p>
          <Dugme variant="flat" boyut="sm" ikon={I.kapat} onClick={() => aramayaDon('')}>
            {cevir('Aramayı temizle')}
          </Dugme>
        </div>
      )}

      {alanSonucu.length > 0 && (
        <Card className="mb-3 bg-white dark:bg-dark-700">
          <h3 className="border-b border-gray-100 px-4 py-2.5 text-sm font-semibold text-gray-800 dark:border-dark-600 dark:text-dark-50">{cevir('Alan adları')}</h3>
          <ul className="p-1.5">
            {alanSonucu.map((s) => (
              <li key={s.anahtar}>
                <button
                  type="button"
                  onClick={() => {
                    alanSec(s.anahtar)
                    aramayaDon('')
                  }}
                  className="group flex min-h-10 w-full min-w-0 items-center gap-2.5 rounded-md px-2.5 py-2 text-start hover:bg-gray-50 dark:hover:bg-dark-600/60"
                >
                  <Ikon d={I.kure} className="size-4 shrink-0 text-gray-400 dark:text-dark-300" />
                  <span className="min-w-0 flex-1 truncate text-sm text-gray-800 dark:text-dark-50">{s.ad}</span>
                  <TurRozeti tur={s.tur} />
                  <span className="hidden text-xs font-medium text-primary-600 @md/ana:inline dark:text-primary-400">{cevir('Araçları bu alan adında aç')}</span>
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {aramaVar && aracSonucu === 0 && alanSonucu.length === 0 ? (
        <Card className="bg-white px-6 py-12 text-center dark:bg-dark-700">
          <span className="mx-auto grid size-12 place-items-center rounded-full bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-dark-200">
            <Ikon d={I.ara} className="size-5" />
          </span>
          <h3 className="mt-3 text-sm font-semibold text-gray-800 dark:text-dark-50">{cevirF('“{0}” ile eşleşen araç yok', arama.trim())}</h3>
          <p className="mx-auto mt-1 max-w-sm text-sm text-gray-500 dark:text-dark-300">{cevir('Başka bir sözcük deneyin ya da kapsam süzgecini kaldırın.')}</p>
          <div className="mt-4 flex flex-wrap justify-center gap-2">
            {cevir('ssl, yedek, dns, php, veritabanı')
              .split(', ')
              .map((s) => (
                <Dugme key={s} boyut="sm" variant="outlined" onClick={() => aramayaDon(s)}>
                  {s}
                </Dugme>
              ))}
            {filtre !== 'tumu' && (
              <Dugme
                boyut="sm"
                variant="soft"
                color="primary"
                ikon={I.filtre}
                onClick={() => {
                  setFiltre('tumu')
                  aramaRef.current?.focus()
                }}
              >
                {cevir('Süzgeci kaldır')}
              </Dugme>
            )}
          </div>
        </Card>
      ) : (
        <div className="space-y-3">
          {gorunenler.map((k) => {
            const acik = aramaVar || !kapali.has(k.kod)
            const govdeId = `ma-kat-${k.kod}-govde`
            const baslikId = `ma-kat-${k.kod}-b`
            const ic = (
              <>
                <span className="grid size-8 shrink-0 place-items-center rounded-md text-gray-500 dark:text-dark-300" aria-hidden>
                  <Ikon d={I[k.ikon] ?? I.arac} className="size-[18px]" />
                </span>
                <span className="min-w-0 flex-1 truncate text-sm-plus font-semibold text-gray-800 dark:text-dark-50">{aracCevir(k.ad)}</span>
                {k.tumu.length > 0 && (
                  <span className="text-xs tabular-nums text-gray-500 dark:text-dark-300">{aramaVar || filtre !== 'tumu' ? `${k.liste.length}/${k.tumu.length}` : k.tumu.length}</span>
                )}
              </>
            )
            return (
              <section key={k.kod} aria-labelledby={baslikId}>
                <Card className="@container bg-white dark:bg-dark-700">
                  <h3 id={baslikId}>
                    {aramaVar ? (
                      <div className="flex items-center gap-2 px-3 py-2">{ic}</div>
                    ) : (
                      <button
                        type="button"
                        aria-expanded={acik}
                        aria-controls={govdeId}
                        onClick={() => kategoriDegistir(k.kod)}
                        className={`flex w-full items-center gap-2 px-3 py-2 text-start transition-colors hover:bg-gray-50 dark:hover:bg-dark-600/50 ${DOKUN} ${acik ? 'rounded-t-lg' : 'rounded-lg'}`}
                      >
                        {ic}
                        <Ikon d={I.chevronAsagi} className={`size-4 shrink-0 text-gray-400 transition-transform dark:text-dark-300 ${acik ? 'rotate-180' : ''}`} />
                      </button>
                    )}
                  </h3>
                  <div id={govdeId} className={acik ? 'border-t border-gray-100 p-2 dark:border-dark-600' : 'hidden'}>
                    {k.liste.length > 0 ? (
                      <div className="grid grid-cols-2 gap-1 @lg:grid-cols-3 @2xl:grid-cols-4 @4xl:grid-cols-5">
                        {k.liste.map((a) => (
                          <AracKutusu key={a.kod} arac={a} domainId={domainId} secili={secili} ana={ana} />
                        ))}
                      </div>
                    ) : (
                      <UyariKutusu ton="uyari" kucuk>
                        {cevir('E-posta hizmetinin lisansı askıda; e-posta araçları lisans yenilenene kadar kullanılamaz. Lütfen hizmet sağlayıcınızla iletişime geçin.')}
                      </UyariKutusu>
                    )}
                  </div>
                </Card>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
