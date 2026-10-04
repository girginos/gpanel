// Telegram Destek — YALNIZ kök. Modül şalteri + merkez bağlantı durumu + bağlantı listesi ve
// kaldırma. Eşleştirme (kod üretme, aydınlatma kabulü) BU PANELDE YAPILMAZ: sunucu sahibi
// müşteri portalında (app.girginos.io) yapar — panel kapalıyken de çalışsın diye.
// Kuralları sunucu zorlar (internal/destek/ajan/handlers.go); arayüz yalnız yansıtır.
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Breadcrumb from '@/components/Breadcrumb'
import { Badge, Button, Select, butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { apiHata } from '@/lib/api'
import {
  destekAyarKaydet, destekBaglantilar, destekDurum, destekIptal,
  type DestekAyar, type DestekBaglantisi, type DestekDurum, type Duzey, type MerkezDurumu,
} from '@/lib/destek'
import { cevir, cevirT, DUZEY_ETIKET, IPTAL_ETIKET, tarih } from '@/components/destek/ceviri'

const DUZEYLER: Duzey[] = ['kapali', 'okuma', 'onarim', 'tam']

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
    case 'ajan_yok': return <Badge color="error" variant="soft">{cevir('Destek servisi çalışmıyor')}</Badge>
    case 'yapilandirma_eksik': return <Badge color="warning" variant="soft">{cevir('Yapılandırma eksik')}</Badge>
    default: return <Badge variant="soft">{cevir('Kapalı')}</Badge>
  }
}

function DurumRozeti({ b }: { b: DestekBaglantisi }) {
  if (b.durum === 'aktif') return <Badge color="success" variant="soft">{cevir('Etkin')}</Badge>
  if (b.durum === 'bekliyor') return <Badge color="warning" variant="soft">{cevir('Eski kayıt (onaysız)')}</Badge>
  return <Badge variant="soft" title={cevir(IPTAL_ETIKET[b.iptal_nedeni] || b.iptal_nedeni)}>{cevir('Kaldırıldı')}</Badge>
}

function rolEtiket(r: string) {
  return r === 'kok' ? cevir('Kök') : r === 'bayi' ? cevir('Bayi') : cevir('Müşteri')
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
  const [islenen, setIslenen] = useState<string | null>(null)

  const yukle = useCallback(async () => {
    try {
      const [d, l] = await Promise.all([destekDurum(), destekBaglantilar()])
      setDurum(d)
      setListe(l)
      setAyar(d.ayar || {})
      setHata(null)
    } catch (e) {
      setHata(apiHata(e, cevir('Durum okunamadı')))
    } finally {
      setYukleniyor(false)
    }
  }, [])

  useEffect(() => { void yukle() }, [yukle])

  const ayarKaydet = async () => {
    setAyarKaydediliyor(true)
    try {
      await destekAyarKaydet({ modul_duzey: ayar.modul_duzey })
      toast.basari(cevir('Ayarlar kaydedildi'))
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setAyarKaydediliyor(false)
    }
  }

  const kaldir = async (b: DestekBaglantisi) => {
    const ad = b.tg_kullanici_adi ? '@' + b.tg_kullanici_adi : b.tg_kisa
    const ok = await dialog.onay({
      baslik: cevir('Bağlantı kaldırılsın mı?'),
      mesaj: cevirT('{0} Telegram hesabının bu paneldeki erişimi hemen sona erer. Yeniden bağlanmak için yeni bir eşleştirme gerekir.', ad),
      ton: 'tehlike', onayEtiketi: cevir('Kaldır'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.kapat,
    })
    if (!ok) return
    setIslenen(b.kimlik)
    try {
      await destekIptal(b.kimlik)
      toast.basari(cevir('Bağlantı kaldırıldı'), ad)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setIslenen(null)
    }
  }

  return (
    <div>
      <Breadcrumb items={[{ etiket: cevir('Telegram Destek') }]} />
      <div className="mb-6 mt-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir('Telegram Destek')}</h1>
          <p className="mt-1.5 max-w-3xl text-sm text-slate-600 dark:text-slate-400">
            {cevir('Telegram destek botu sorunu teşhis etmek için yalnız sizin seçtiğiniz kapsamda ve düzeyde günlükleri ve ayarları okur. Parolalar ve gizli değerler sunucudan çıkmadan maskelenir.')}
          </p>
        </div>
        <Button variant="outlined" ikon={I.yenile} yukleniyor={yukleniyor} onClick={() => { setYukleniyor(true); void yukle() }} className="shrink-0">{cevir('Yenile')}</Button>
      </div>

      {hata && <UyariKutusu ton="tehlike" className="mb-4">{hata}</UyariKutusu>}
      {yukleniyor && !durum && (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir('Yükleniyor…')}</div>
      )}

      {durum && (
        <Bolum baslik={cevir('Modül ayarları')} sag={durum.baglanti && <MerkezRozeti d={durum.baglanti} />}>
          <p className="mb-4 text-sm text-slate-600 dark:text-dark-200">
            {cevir('Modül varsayılan olarak kapalıdır. Açıldığında panel destek merkezine dışa doğru bağlanır; hiçbir bağlantı bu sınırın üzerinde yetki alamaz.')}
          </p>
          <div className="sm:w-80">
            <Select label={cevir('Telegram destek modülü')} value={ayar.modul_duzey || 'kapali'} data={DUZEYLER.map(duzeySecenek)}
              onChange={(e) => setAyar({ ...ayar, modul_duzey: e.target.value as Duzey })} />
          </div>
          {durum.ajan && !durum.ajan.canli && durum.baglanti?.durum === 'ajan_yok' && (
            <p className="mt-4 text-xs text-error dark:text-error-light">
              {cevir('Destek servisi (girginospanel-destek-ajan) çalışmıyor; Telegram desteği yanıt vermez. Sunucuda: systemctl restart girginospanel-destek-ajan')}
            </p>
          )}
          {durum.ajan?.canli && durum.ajan.kopru === 'yok' && (
            <p className="mt-4 text-xs text-slate-500">
              {cevir('Panel köprüsü kapalı: panele bağlı tanılama araçları geçici olarak kullanılamaz (servis ve günlük araçları çalışır).')}
            </p>
          )}
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

      {durum && (
        <Bolum baslik={cevir('Eşleştirme')}>
          <p className="text-sm text-slate-700 dark:text-dark-100">
            {cevir('Eşleştirme app.girginos.io\'dan yapılır.')}
          </p>
          <p className="mt-1 text-xs text-slate-500 dark:text-dark-300">
            {cevir('Yalnız sunucunun sahibi (lisansın portal hesabı) sunucuyu seçip aydınlatma metnini kabul ederek eşleştirme kodu alabilir. Bu sayfada eşleştirme yapılamaz; panel kapalıyken de portaldan eşleştirilebilir. Aşağıdaki bağlantılar her zaman süreli (en çok 30 gün) olup parolanız değişirse, hesabınız askıya alınırsa ya da 2FA kapatılırsa kendiliğinden kaldırılır.')}
          </p>
          {/^https:\/\//.test(durum.eslestirme_url) && (
            <a href={durum.eslestirme_url} target="_blank" rel="noopener noreferrer" className={butonSinifi({ color: 'primary', className: 'mt-3 gap-2' })}>
              <Ikon d={I.gonder} />{cevir('Portalı aç')}<Ikon d={I.harici} />
            </a>
          )}
        </Bolum>
      )}

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
    </div>
  )
}
