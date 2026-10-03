// Telegram Destek — kök, bayi ve müşteri. Kök: modül şalteri + üst sınırlar + merkez
// bağlantı durumu; bayi: "müşterilerim kullanabilir"; herkes: eşleştirme kodu (QR +
// t.me bağlantısı), bekleyen bağlantının İKİNCİ ONAYI (KVKK aydınlatma kabulüyle),
// bağlantı listesi ve kaldırma. Kapsamı ve kuralları sunucu zorlar
// (internal/destek/ajan/handlers.go); arayüz yalnız yansıtır.
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Breadcrumb from '@/components/Breadcrumb'
import { Badge, Button, Select, butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { apiHata } from '@/lib/api'
import i18n from '@/lib/i18n'
import {
  destekAyarKaydet, destekBaglantilar, destekDurum, destekEslestir, destekIptal, destekOnayla,
  type DestekAyar, type DestekBaglantisi, type DestekDurum, type Duzey, type EslesmeKodu, type MerkezDurumu,
} from '@/lib/destek'
import { cevir, cevirT, DUZEY_ETIKET, IPTAL_ETIKET, tarih } from '@/components/destek/ceviri'

const DUZEYLER: Duzey[] = ['kapali', 'okuma', 'onarim', 'tam']
const BAGLANTI_DUZEYLERI: Duzey[] = ['okuma', 'onarim', 'tam']

const duzeySecenek = (d: Duzey) => ({ value: d, label: cevir(DUZEY_ETIKET[d]) })

function Bolum({ baslik, children, sag }: { baslik: string; children: React.ReactNode; sag?: React.ReactNode }) {
  return (
    <section className="mb-6 rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-600 dark:bg-dark-800">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-slate-900 dark:text-dark-50">{baslik}</h2>
        {sag}
      </div>
      {children}
    </section>
  )
}

function MerkezRozeti({ d }: { d: MerkezDurumu }) {
  switch (d.durum) {
    case 'bagli': return <Badge color="success" variant="soft">{cevir('Bağlı')} · {d.tasima}</Badge>
    case 'baglaniyor': return <Badge color="info" variant="soft">{cevir('Bağlanıyor')}</Badge>
    case 'hata': return <Badge color="error" variant="soft">{cevir('Hata')}</Badge>
    case 'yapilandirma_eksik': return <Badge color="warning" variant="soft">{cevir('Yapılandırma eksik')}</Badge>
    default: return <Badge variant="soft">{cevir('Kapalı')}</Badge>
  }
}

function DurumRozeti({ b }: { b: DestekBaglantisi }) {
  if (b.durum === 'aktif') return <Badge color="success" variant="soft">{cevir('Etkin')}</Badge>
  if (b.durum === 'bekliyor') return <Badge color="warning" variant="soft">{cevir('Onay bekliyor')}</Badge>
  return <Badge variant="soft" title={cevir(IPTAL_ETIKET[b.iptal_nedeni] || b.iptal_nedeni)}>{cevir('Kaldırıldı')}</Badge>
}

function rolEtiket(r: string) {
  return r === 'kok' ? cevir('Kök') : r === 'bayi' ? cevir('Bayi') : cevir('Müşteri')
}

function kalanSure(sn: number): string {
  const d = Math.floor(sn / 60)
  const s = sn % 60
  return `${d}:${String(s).padStart(2, '0')}`
}

export default function TelegramDestekPage() {
  useTranslation()
  const toast = useToast()
  const dialog = useDialog()
  const [durum, setDurum] = useState<DestekDurum | null>(null)
  const [liste, setListe] = useState<DestekBaglantisi[]>([])
  const [hata, setHata] = useState<string | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [ayar, setAyar] = useState<DestekAyar>({})
  const [ayarKaydediliyor, setAyarKaydediliyor] = useState(false)
  const [duzey, setDuzey] = useState<Duzey>('okuma')
  const [sure, setSure] = useState(7)
  const [kod, setKod] = useState<EslesmeKodu | null>(null)
  const [kodUretiliyor, setKodUretiliyor] = useState(false)
  const [simdi, setSimdi] = useState(() => Math.floor(Date.now() / 1000))
  const [kabul, setKabul] = useState<Record<string, boolean>>({})
  const [islenen, setIslenen] = useState<string | null>(null)
  const [metinAcik, setMetinAcik] = useState(false)

  const yukle = useCallback(async () => {
    try {
      const [d, l] = await Promise.all([destekDurum(), destekBaglantilar()])
      setDurum(d)
      setListe(l)
      setAyar(d.ayar || {})
      setSure((s) => (d.sure_secenekleri.includes(s) ? s : d.varsayilan_gun))
      setHata(null)
    } catch (e) {
      setHata(apiHata(e, cevir('Durum okunamadı')))
    } finally {
      setYukleniyor(false)
    }
  }, [])

  useEffect(() => { void yukle() }, [yukle])

  // Kod gösterilirken: geri sayım + Telegram'dan gelen eşleşmeyi yakalamak için liste yoklaması.
  const kodGecerli = kod !== null && simdi < kod.bitis
  useEffect(() => {
    if (!kod) return
    const t = window.setInterval(() => setSimdi(Math.floor(Date.now() / 1000)), 1000)
    const y = window.setInterval(() => { destekBaglantilar().then(setListe).catch(() => undefined) }, 4000)
    return () => { window.clearInterval(t); window.clearInterval(y) }
  }, [kod])

  const ayarKaydet = async () => {
    setAyarKaydediliyor(true)
    try {
      await destekAyarKaydet(durum?.rol === 'bayi' ? { musteri_duzey: ayar.musteri_duzey } : ayar)
      toast.basari(cevir('Ayarlar kaydedildi'))
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setAyarKaydediliyor(false)
    }
  }

  const kodUret = async () => {
    setKodUretiliyor(true)
    try {
      const k = await destekEslestir(duzey, sure)
      setKod(k)
      setSimdi(Math.floor(Date.now() / 1000))
    } catch (e) {
      toast.hata(cevir('Kod oluşturulamadı'), apiHata(e))
    } finally {
      setKodUretiliyor(false)
    }
  }

  const kopyala = async (metin: string) => {
    try {
      await navigator.clipboard.writeText(metin)
      toast.basari(cevir('Kopyalandı'))
    } catch {
      /* pano izni yok — kod ekranda */
    }
  }

  const onayla = async (b: DestekBaglantisi) => {
    if (!durum) return
    setIslenen(b.kimlik)
    try {
      await destekOnayla(b.kimlik, durum.aydinlatma.surum)
      toast.basari(cevir('Bağlantı onaylandı'), '@' + (b.tg_kullanici_adi || b.tg_kisa))
      setKod(null)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setIslenen(null)
    }
  }

  const kaldir = async (b: DestekBaglantisi, reddet = false) => {
    const ad = b.tg_kullanici_adi ? '@' + b.tg_kullanici_adi : b.tg_kisa
    if (!reddet) {
      const ok = await dialog.onay({
        baslik: cevir('Bağlantı kaldırılsın mı?'),
        mesaj: cevirT('{0} Telegram hesabının bu paneldeki erişimi hemen sona erer. Yeniden bağlanmak için yeni bir eşleştirme gerekir.', ad),
        ton: 'tehlike', onayEtiketi: cevir('Kaldır'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.kapat,
      })
      if (!ok) return
    }
    setIslenen(b.kimlik)
    try {
      await destekIptal(b.kimlik)
      toast.basari(reddet ? cevir('Bağlantı reddedildi') : cevir('Bağlantı kaldırıldı'), ad)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setIslenen(null)
    }
  }

  const rol = durum?.rol
  const bekleyenler = liste.filter((b) => b.durum === 'bekliyor' && b.benim)
  const aydinlatmaMetni = durum ? (i18n.language === 'en' ? durum.aydinlatma.en : durum.aydinlatma.tr) : ''

  return (
    <div>
      <Breadcrumb items={[{ etiket: cevir('Telegram Destek') }]} />
      <div className="mb-6 mt-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir('Telegram Destek')}</h1>
          <p className="mt-1.5 max-w-3xl text-sm text-slate-600 dark:text-slate-400">
            {cevir('Hesabınızı Telegram destek botuna bağlayın: bot sorunu teşhis etmek için yalnız sizin seçtiğiniz kapsamda ve düzeyde günlükleri ve ayarları okur. Parolalar ve gizli değerler sunucudan çıkmadan maskelenir.')}
          </p>
        </div>
        <Button variant="outlined" ikon={I.yenile} yukleniyor={yukleniyor} onClick={() => { setYukleniyor(true); void yukle() }} className="shrink-0">{cevir('Yenile')}</Button>
      </div>

      {hata && <UyariKutusu ton="tehlike" className="mb-4">{hata}</UyariKutusu>}
      {yukleniyor && !durum && (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir('Yükleniyor…')}</div>
      )}

      {durum && rol === 'kok' && (
        <Bolum baslik={cevir('Modül ayarları')} sag={durum.baglanti && <MerkezRozeti d={durum.baglanti} />}>
          <p className="mb-4 text-sm text-slate-600 dark:text-dark-200">
            {cevir('Modül varsayılan olarak kapalıdır. Açıldığında panel destek merkezine dışa doğru bağlanır; hiçbir bağlantı bu sınırların üzerinde yetki alamaz.')}
          </p>
          <div className="grid gap-4 sm:grid-cols-3">
            <Select label={cevir('Telegram destek modülü')} value={ayar.modul_duzey || 'kapali'} data={DUZEYLER.map(duzeySecenek)}
              onChange={(e) => setAyar({ ...ayar, modul_duzey: e.target.value as Duzey })} />
            <Select label={cevir('Bayiler için üst sınır')} value={ayar.bayi_duzey || 'kapali'} data={DUZEYLER.map(duzeySecenek)}
              onChange={(e) => setAyar({ ...ayar, bayi_duzey: e.target.value as Duzey })} />
            <Select label={cevir('Müşteriler için üst sınır')} value={ayar.musteri_duzey || 'kapali'} data={DUZEYLER.map(duzeySecenek)}
              onChange={(e) => setAyar({ ...ayar, musteri_duzey: e.target.value as Duzey })} />
          </div>
          {durum.baglanti && (durum.baglanti.son_hata || durum.baglanti.cihaz_kimligi) && (
            <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
              {durum.baglanti.cihaz_kimligi && (<><dt className="text-slate-500">{cevir('Cihaz kimliği')}</dt><dd className="font-mono text-slate-700 dark:text-dark-100">{durum.baglanti.cihaz_kimligi}</dd></>)}
              {durum.baglanti.son_hata && (<><dt className="text-slate-500">{cevir('Son hata')}</dt><dd className="break-all text-error dark:text-error-light">{durum.baglanti.son_hata}</dd></>)}
            </dl>
          )}
          <div className="mt-4 flex justify-end">
            <Button color="primary" ikon={I.disket} yukleniyor={ayarKaydediliyor} onClick={() => void ayarKaydet()}>{cevir('Kaydet')}</Button>
          </div>
        </Bolum>
      )}

      {durum && rol === 'bayi' && (
        <Bolum baslik={cevir('Müşterilerim kullanabilir')}>
          <p className="mb-4 text-sm text-slate-600 dark:text-dark-200">
            {cevirT('Müşterilerinizin bota bağlanabileceği en yüksek düzey. Yöneticinin izin verdiği üst sınır: {0}.', cevir(DUZEY_ETIKET[durum.musteri_ust_sinir || 'kapali']))}
          </p>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="sm:w-80">
              <Select aria-label={cevir('Müşterilerim kullanabilir')} value={ayar.musteri_duzey || 'kapali'} data={DUZEYLER.map(duzeySecenek)}
                onChange={(e) => setAyar({ musteri_duzey: e.target.value as Duzey })} />
            </div>
            <Button color="primary" ikon={I.disket} yukleniyor={ayarKaydediliyor} onClick={() => void ayarKaydet()}>{cevir('Kaydet')}</Button>
          </div>
        </Bolum>
      )}

      {durum && (
        <Bolum baslik={cevir('Eşleştir')}>
          {durum.adina ? (
            <UyariKutusu ton="uyari">{cevir('Müşteri adına açılan oturumda Telegram bağlantısı kurulamaz ve onaylanamaz; müşterinin kendisi yapmalıdır.')}</UyariKutusu>
          ) : durum.tavan === 'kapali' ? (
            <UyariKutusu ton="bilgi">{cevir('Telegram destek bu hesap için kapalı. Sunucu yöneticiniz (ya da bayiniz) açtığında buradan eşleştirebilirsiniz.')}</UyariKutusu>
          ) : (
            <>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
                <div className="sm:w-72">
                  <Select label={cevir('Yetki düzeyi')} value={duzey} data={BAGLANTI_DUZEYLERI.map(duzeySecenek)} onChange={(e) => setDuzey(e.target.value as Duzey)} />
                </div>
                <div className="sm:w-40">
                  <Select label={cevir('Süre')} value={sure} onChange={(e) => setSure(Number(e.target.value))}
                    data={durum.sure_secenekleri.map((g) => ({ value: g, label: g === 1 ? cevir('1 gün') : cevirT('{0} gün', g) }))} />
                </div>
                <Button color="primary" ikon={I.gonder} yukleniyor={kodUretiliyor} onClick={() => void kodUret()}>{cevir('Eşleştirme kodu oluştur')}</Button>
              </div>
              <p className="mt-3 text-xs text-slate-500 dark:text-dark-300">
                {cevir('Bu sürümde bot yalnız okuma araçlarını kullanır; daha yüksek düzey ileride açılacak onarım araçları içindir. Etkin düzey her zaman yönetici ve bayi sınırlarıyla kısılır.')}{' '}
                {cevirT('Bağlantı her zaman süreli (en çok {0} gün); parolanız değişirse, hesabınız askıya alınırsa ya da 2FA kapatılırsa kendiliğinden kaldırılır.', durum.en_uzun_gun)}
              </p>
              {kod && (
                <div className="mt-5 flex flex-col gap-5 rounded-lg border border-slate-200 p-4 dark:border-dark-600 sm:flex-row sm:items-center">
                  {kod.qr && kodGecerli && <img src={kod.qr} alt={cevir('Eşleştirme kodu')} className="size-48 shrink-0 rounded-md bg-white p-2" />}
                  <div className="min-w-0 flex-1">
                    <div className="text-xs font-medium uppercase tracking-wider text-slate-500">{cevir('Eşleştirme kodu')}</div>
                    <div className={'mt-1 break-all font-mono text-2xl font-semibold tracking-widest ' + (kodGecerli ? 'text-slate-900 dark:text-dark-50' : 'text-slate-400 line-through')}>{kod.gorunen}</div>
                    {kodGecerli ? (
                      <>
                        <p className="mt-2 text-sm text-slate-600 dark:text-dark-200">
                          {cevir('QR kodu telefonunuzla okutun ya da bağlantıyı açın. Bot kullanıcı adı yapılandırılmadıysa Telegram\'da bota şunu yazın:')}{' '}
                          <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-700">/start {kod.kod}</code>
                        </p>
                        <p className="mt-1 text-xs text-slate-500">{cevirT('Kod {0} içinde geçersiz olur ve yalnız bir kez kullanılabilir.', kalanSure(Math.max(0, kod.bitis - simdi)))}</p>
                        <div className="mt-3 flex flex-wrap gap-2">
                          {kod.baglanti_url && (
                            <a href={kod.baglanti_url} target="_blank" rel="noopener noreferrer"
                              className={butonSinifi({ color: 'primary', className: 'gap-2' })}>
                              <Ikon d={I.gonder} />{cevir('Telegram\'da aç')}<Ikon d={I.harici} />
                            </a>
                          )}
                          <Button variant="outlined" ikon={I.kopya} onClick={() => void kopyala(kod.kod)}>{cevir('Kodu kopyala')}</Button>
                        </div>
                        {!kod.merkez_bagli && <UyariKutusu ton="uyari" kucuk className="mt-3">{cevir('Panel şu an destek merkezine bağlı değil; bağlantı kurulunca kod merkeze iletilir.')}</UyariKutusu>}
                      </>
                    ) : (
                      <p className="mt-2 text-sm text-slate-500">{cevir('Kodun süresi doldu.')}</p>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </Bolum>
      )}

      {durum && bekleyenler.map((b) => (
        <section key={b.kimlik} className="mb-6 rounded-lg border border-warning/40 bg-warning/5 p-5 dark:border-warning-light/30">
          <div className="flex items-start gap-3">
            <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-warning/15 text-warning dark:text-warning-light"><Ikon d={I.gonder} className="size-5" /></span>
            <div className="min-w-0 flex-1">
              <h2 className="text-base font-semibold text-slate-900 dark:text-dark-50">{cevir('Onay bekleyen bağlantı')}</h2>
              <p className="mt-1 text-sm text-slate-700 dark:text-dark-100">
                {cevirT('Telegram hesabı {0} bu panele bağlanmak istiyor. Siz onaylamadan bot hiçbir şey okuyamaz.', b.tg_kullanici_adi ? '@' + b.tg_kullanici_adi : b.tg_kisa)}
              </p>
              <p className="mt-1 text-xs text-slate-500">{cevir(DUZEY_ETIKET[b.duzey])} · {b.sure_gun === 1 ? cevir('1 gün') : cevirT('{0} gün', b.sure_gun)} · {b.hedef}</p>
              <label className="mt-3 flex items-start gap-2 text-sm text-slate-700 dark:text-dark-100">
                <input type="checkbox" className="mt-0.5 size-4" checked={!!kabul[b.kimlik]} onChange={(e) => setKabul({ ...kabul, [b.kimlik]: e.target.checked })} />
                <span>{cevirT('Aydınlatma metnini okudum ve kabul ediyorum (sürüm {0}).', durum.aydinlatma.surum)}{' '}
                  <button type="button" className="text-primary-600 underline dark:text-primary-400" onClick={() => setMetinAcik(true)}>{cevir('Metni göster')}</button>
                </span>
              </label>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button color="primary" ikon={I.onay} disabled={!kabul[b.kimlik]} yukleniyor={islenen === b.kimlik} onClick={() => void onayla(b)}>{cevir('Onayla')}</Button>
                <Button variant="outlined" color="error" ikon={I.kapat} disabled={islenen === b.kimlik} onClick={() => void kaldir(b, true)}>{cevir('Reddet')}</Button>
              </div>
            </div>
          </div>
        </section>
      ))}

      {durum && (
        <Bolum baslik={cevir('Bağlantılar')}>
          {liste.length === 0 ? (
            <div className="rounded-lg border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500 dark:border-dark-500 dark:text-dark-300">{cevir('Henüz Telegram bağlantısı yok.')}</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                  <tr>
                    <th className="px-3 py-2 font-semibold">{cevir('Telegram')}</th>
                    <th className="px-3 py-2 font-semibold">{cevir('Kapsam')}</th>
                    <th className="px-3 py-2 font-semibold">{cevir('Durum')}</th>
                    <th className="px-3 py-2 font-semibold">{cevir('Düzey')}</th>
                    <th className="px-3 py-2 font-semibold">{cevir('Bitiş')}</th>
                    <th className="px-3 py-2 font-semibold">{cevir('Son kullanım')}</th>
                    <th className="px-3 py-2" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-dark-600">
                  {liste.map((b) => (
                    <tr key={b.kimlik} className={b.durum === 'iptal' ? 'opacity-60' : ''}>
                      <td className="px-3 py-2 align-top">
                        <div className="font-medium text-slate-900 dark:text-dark-50">{b.tg_kullanici_adi ? '@' + b.tg_kullanici_adi : b.tg_kisa}</div>
                        <div className="text-xs text-slate-500">{rolEtiket(b.rol)}</div>
                      </td>
                      <td className="px-3 py-2 align-top text-slate-700 dark:text-dark-100">{b.hedef}</td>
                      <td className="px-3 py-2 align-top">
                        <DurumRozeti b={b} />
                        {b.durum === 'iptal' && b.iptal_nedeni && <div className="mt-1 text-xs text-slate-500">{cevir(IPTAL_ETIKET[b.iptal_nedeni] || b.iptal_nedeni)}</div>}
                        {b.durum === 'aktif' && b.onaylayan && <div className="mt-1 text-xs text-slate-500" title={b.aydinlatma_surum}>{cevir('Onaylayan')}: {b.onaylayan}</div>}
                      </td>
                      <td className="px-3 py-2 align-top text-xs text-slate-600 dark:text-dark-200">
                        {cevir(DUZEY_ETIKET[b.durum === 'aktif' ? b.etkin_duzey : b.duzey])}
                      </td>
                      <td className="px-3 py-2 align-top text-xs text-slate-600 dark:text-dark-200">{tarih(b.durum === 'iptal' ? b.iptal_at : b.bitis)}</td>
                      <td className="px-3 py-2 align-top text-xs text-slate-600 dark:text-dark-200">{b.son_kullanim ? tarih(b.son_kullanim) : cevir('hiç')}</td>
                      <td className="px-3 py-2 align-top text-right">
                        {b.durum !== 'iptal' && (
                          <Button boyut="sm" variant="outlined" color="error" ikon={I.kapat} yukleniyor={islenen === b.kimlik} onClick={() => void kaldir(b)}>{cevir('Kaldır')}</Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Bolum>
      )}

      {durum && (
        <Bolum baslik={cevir('Aydınlatma metni')} sag={
          <div className="flex items-center gap-2">
            {durum.aydinlatma.taslak && <Badge color="warning" variant="soft" title={cevir('TASLAK — hukuki onaydan önce gerçek müşterilere açılmaz')}>TASLAK</Badge>}
            <Badge variant="outlined">{cevirT('Sürüm {0}', durum.aydinlatma.surum)}</Badge>
            <Button boyut="sm" variant="outlined" ikon={I.goz} onClick={() => setMetinAcik(!metinAcik)}>{metinAcik ? cevir('Metni gizle') : cevir('Metni göster')}</Button>
          </div>
        }>
          {durum.aydinlatma.taslak && <UyariKutusu ton="uyari" kucuk className="mb-3">{cevir('TASLAK — hukuki onaydan önce gerçek müşterilere açılmaz')}</UyariKutusu>}
          {metinAcik && <pre className="whitespace-pre-wrap font-sans text-sm leading-relaxed text-slate-700 dark:text-dark-100">{aydinlatmaMetni}</pre>}
          {liste.filter((b) => b.benim && b.aydinlatma_surum).slice(0, 1).map((b) => (
            <p key={b.kimlik} className="mt-2 text-xs text-slate-500">{cevirT('Kabul edildi: {0}', `${b.aydinlatma_surum} · ${tarih(b.onay_at)}`)}</p>
          ))}
        </Bolum>
      )}
    </div>
  )
}
