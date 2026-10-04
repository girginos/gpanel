// API Anahtarları (D4) — kök ve bayi. Herkes YALNIZ kendi anahtarlarını görür
// (kapsam sunucuda, oturumdan; kökte de bayi anahtarları listelenmez).
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Breadcrumb from '@/components/Breadcrumb'
import { Badge, Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'
import { apiHata } from '@/lib/api'
import {
  anahtarIptal, anahtarKatalog, anahtarListe, anahtarSil,
  type ApiAnahtari, type Katalog, type TekKezYanit,
} from '@/lib/apiAnahtar'
import AnahtarOlusturModal from '@/components/apianahtar/AnahtarOlusturModal'
import AnahtarGosterModal from '@/components/apianahtar/AnahtarGosterModal'
import AnahtarYenileModal from '@/components/apianahtar/AnahtarYenileModal'
import { cevir, cevirT, tarih } from '@/components/apianahtar/ceviri'

function DurumRozeti({ k }: { k: ApiAnahtari }) {
  switch (k.durum) {
    case 'etkin': return <Badge color="success" variant="soft">{cevir('Etkin')}</Badge>
    case 'gecis': return <Badge color="warning" variant="soft">{cevir('Geçiş süresinde')}</Badge>
    case 'askida': return <Badge color="warning" variant="soft" title={cevir('Bayi askıdan alınınca anahtar kendiliğinden yeniden çalışır.')}>{cevir('Askıda (bayi askıya alındı)')}</Badge>
    case 'suresi_doldu': return <Badge variant="soft">{cevir('Süresi doldu')}</Badge>
    default: return <Badge color="error" variant="soft">{k.iptal_yonetim ? cevir('İptal edildi (yönetim)') : cevir('İptal edildi')}</Badge>
  }
}

// kimlikBasligi: örneklerdeki Authorization değeri — anahtar YALNIZ sahibinin
// kullanıcı adıyla geçer ("gosp <kullanıcı adı>:<anahtar>").
function kimlikBasligi(katalog: Katalog | null): string {
  return `gosp ${katalog?.kullanici_adi || '<kullanici>'}:gosp_xxxxxxxxxxxx_…`
}

// kullanimOrnekleri: curl örnekleri (bağlantı testi, listeleme, üç parola ucu).
function kullanimOrnekleri(taban: string, kimlik: string): { baslik: string; kod: string }[] {
  const h = `-H "Authorization: ${kimlik}"`
  const put = (yol: string) => `curl -s -X PUT ${taban}${yol} \\\n  ${h} \\\n  -H "Content-Type: application/json" \\\n  -d '{"password":"<yeni-parola>"}'`
  return [
    { baslik: 'Bağlantıyı test et', kod: `curl -s ${taban}/api-anahtari/ben \\\n  ${h}` },
    { baslik: 'Hosting hesaplarını listele', kod: `curl -s ${taban}/domains \\\n  ${h}` },
    { baslik: 'Hostingin ana FTP parolasını değiştir (FTP parolası izni)', kod: put('/domains/{id}/ftp/password') },
    { baslik: 'FTP hesabının parolasını değiştir (FTP parolası izni)', kod: put('/domains/{id}/ftp-accounts/{kullanici}/password') },
    { baslik: 'Veritabanı kullanıcısının parolasını değiştir (DB parolası izni)', kod: put('/domains/{id}/databases/users/{kullanici}/password') },
  ]
}

function IzinRozetleri({ k, katalog }: { k: ApiAnahtari; katalog: Katalog | null }) {
  return (
    <div className="flex flex-wrap gap-1">
      {k.izinler.map((iz) => {
        const [g, e] = iz.split(':')
        const grup = katalog?.gruplar.find((x) => x.ad === g)
        const etiket = grup?.etiket || g
        // Tek düzeyli (yalnız yazma) izin — parola değiştirme: düzey eki gösterilmez.
        if (grup?.yalniz_yaz) {
          return <Badge key={iz} color="primary" variant="outlined" className="text-[11px]">{cevir(etiket)}</Badge>
        }
        return (
          <Badge key={iz} color={e === 'yaz' ? 'primary' : 'info'} variant="outlined" className="text-[11px]" title={e === 'yaz' ? cevir('okuma + yazma') : cevir('okuma')}>
            {cevir(etiket)}{e === 'yaz' ? ' · RW' : ' · R'}
          </Badge>
        )
      })}
    </div>
  )
}

export default function ApiAnahtarlariPage() {
  useTranslation()
  const toast = useToast()
  const dialog = useDialog()
  const [liste, setListe] = useState<ApiAnahtari[]>([])
  const [katalog, setKatalog] = useState<Katalog | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [olusturAcik, setOlusturAcik] = useState(false)
  const [yenileHedef, setYenileHedef] = useState<ApiAnahtari | null>(null)
  const [tekKez, setTekKez] = useState<TekKezYanit | null>(null)
  const [iptalEdilen, setIptalEdilen] = useState<number | null>(null)
  const [silinen, setSilinen] = useState<number | null>(null)

  const yukle = useCallback(async () => {
    setYukleniyor(true); setHata(null)
    try {
      setListe(await anahtarListe())
    } catch (e) {
      setHata(apiHata(e, cevir('Liste okunamadı')))
    } finally {
      setYukleniyor(false)
    }
  }, [])

  useEffect(() => { void yukle() }, [yukle])
  useEffect(() => { anahtarKatalog().then(setKatalog).catch(() => setKatalog(null)) }, [])

  const iptalEt = async (k: ApiAnahtari) => {
    const ok = await dialog.onay({
      baslik: cevir('Anahtar iptal edilsin mi?'),
      mesaj: cevirT('"{0}" anahtarı hemen geçersiz olur; onu kullanan entegrasyonlar çalışmayı bırakır. Bu işlem geri alınamaz.', k.ad),
      ton: 'tehlike', onayEtiketi: cevir('İptal et'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.kapat,
    })
    if (!ok) return
    setIptalEdilen(k.id)
    try {
      await anahtarIptal(k.id)
      toast.basari(cevir('Anahtar iptal edildi'), k.ad)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setIptalEdilen(null)
    }
  }

  // Kalıcı silme. Etkin (geçiş/askıda dahil) anahtar: adını yazarak tek adımda
  // "iptal et ve sil"; iptal edilmiş/süresi dolmuş: düz onay.
  const sil = async (k: ApiAnahtari) => {
    let onayAd: string | undefined
    if (etkin(k)) {
      const girilen = await dialog.sor({
        baslik: cevir('Anahtar iptal edilip silinsin mi?'),
        mesaj: cevirT('"{0}" anahtarı hâlâ etkin. Silinince hemen geçersiz olur ve listeden kalkar; denetim geçmişi korunur. Onaylamak için anahtarın adını yazın.', k.ad),
        yerTutucu: k.ad, ton: 'tehlike', onayEtiketi: cevir('İptal et ve sil'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.cop,
      })
      if (girilen === null) return
      if (girilen.trim() !== k.ad) { toast.hata(cevir('İşlem başarısız'), cevir('Yazılan ad anahtarın adıyla eşleşmiyor.')); return }
      onayAd = girilen.trim()
    } else {
      const ok = await dialog.onay({
        baslik: cevir('Anahtar kalıcı olarak silinsin mi?'),
        mesaj: cevirT('"{0}" anahtarının kaydı listeden kalıcı olarak silinir; denetim geçmişi korunur. Bu işlem geri alınamaz.', k.ad),
        ton: 'tehlike', onayEtiketi: cevir('Sil'), iptalEtiketi: cevir('Vazgeç'), onayIkonu: I.cop,
      })
      if (!ok) return
    }
    setSilinen(k.id)
    try {
      await anahtarSil(k.id, onayAd)
      toast.basari(cevir('Anahtar silindi'), k.ad)
      await yukle()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setSilinen(null)
    }
  }

  const tekKezGoster = (y: TekKezYanit) => {
    setOlusturAcik(false); setYenileHedef(null)
    setTekKez(y)
    void yukle()
  }

  // etkin: anahtar kaydı geçerli (askıdaki bayinin anahtarı da — iptal edilebilir,
  // silmek için ad onayı ister).
  const etkin = (k: ApiAnahtari) => k.durum === 'etkin' || k.durum === 'gecis' || k.durum === 'askida'
  const soluk = (k: ApiAnahtari) => k.durum === 'suresi_doldu' || k.durum === 'iptal'
  const taban = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1'

  const eylemler = (k: ApiAnahtari) => (
    <div className="flex flex-wrap justify-end gap-1.5">
      {k.durum === 'etkin' && (
        <Button boyut="sm" variant="outlined" ikon={I.yenile} onClick={() => setYenileHedef(k)}>{cevir('Döndür')}</Button>
      )}
      {etkin(k) && (
        <Button boyut="sm" variant="outlined" color="error" ikon={I.kapat} yukleniyor={iptalEdilen === k.id} onClick={() => void iptalEt(k)}>{cevir('İptal et')}</Button>
      )}
      <Button boyut="sm" variant="outlined" color="error" ikon={I.cop} yukleniyor={silinen === k.id} onClick={() => void sil(k)} title={cevir('Kalıcı olarak sil')} aria-label={cevirT('{0} anahtarını sil', k.ad)}>{cevir('Sil')}</Button>
    </div>
  )

  return (
    <div>
      <Breadcrumb items={[{ etiket: cevir('API Anahtarları') }]} />
      <div className="mb-6 mt-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">{cevir('API Anahtarları')}</h1>
          <p className="mt-1.5 max-w-3xl text-sm text-slate-600 dark:text-slate-400">
            {cevir('Faturalama otomasyonu (WHMCS, Blesta) ve betikler için kalıcı erişim anahtarları. Anahtar yalnız seçtiğiniz izinlerle ve kendi kapsamınızda çalışır.')}
          </p>
        </div>
        <Button color="primary" ikon={I.arti} onClick={() => setOlusturAcik(true)} disabled={!katalog} className="shrink-0">{cevir('Yeni anahtar')}</Button>
      </div>

      {hata && <UyariKutusu ton="tehlike" className="mb-4">{hata}</UyariKutusu>}

      {yukleniyor ? (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir('Yükleniyor…')}</div>
      ) : liste.length === 0 ? (
        <div className="rounded-lg border border-dashed border-slate-300 py-10 text-center text-sm text-slate-500 dark:border-dark-500 dark:text-dark-300">{cevir('Henüz API anahtarı yok.')}</div>
      ) : (
        <>
          {/* Masaüstü: tablo */}
          <div className="hidden overflow-x-auto rounded-lg border border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-800 lg:block">
            <table className="w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                <tr>
                  <th className="px-4 py-3 font-semibold">{cevir('Ad')}</th>
                  <th className="px-4 py-3 font-semibold">{cevir('İzinler')}</th>
                  <th className="px-4 py-3 font-semibold">{cevir('İzinli IP')}</th>
                  <th className="px-4 py-3 font-semibold">{cevir('Son kullanım')}</th>
                  <th className="px-4 py-3 font-semibold">{cevir('Bitiş')}</th>
                  <th className="px-4 py-3 font-semibold">{cevir('Durum')}</th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-dark-600">
                {liste.map((k) => (
                  <tr key={k.id} className={soluk(k) ? 'opacity-60' : ''}>
                    <td className="px-4 py-3 align-top">
                      <div className="font-medium text-slate-900 dark:text-dark-50">{k.ad}</div>
                      <div className="font-mono text-xs text-slate-500 dark:text-dark-300">{k.gosterim}</div>
                    </td>
                    <td className="max-w-xs px-4 py-3 align-top"><IzinRozetleri k={k} katalog={katalog} /></td>
                    <td className="px-4 py-3 align-top font-mono text-xs text-slate-600 dark:text-dark-200">
                      {k.izinli_ipler.length ? k.izinli_ipler.map((ip) => <div key={ip}>{ip}</div>) : <span className="font-sans text-slate-400">{cevir('her IP')}</span>}
                    </td>
                    <td className="px-4 py-3 align-top text-xs text-slate-600 dark:text-dark-200">
                      {k.son_kullanim_at ? <>{tarih(k.son_kullanim_at)}<div className="font-mono text-slate-400">{k.son_kullanim_ip}</div></> : cevir('hiç')}
                    </td>
                    <td className="px-4 py-3 align-top text-xs text-slate-600 dark:text-dark-200">{tarih(k.son_kullanma_at)}</td>
                    <td className="px-4 py-3 align-top"><DurumRozeti k={k} /></td>
                    <td className="px-4 py-3 align-top">{eylemler(k)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {/* Mobil/tablet: kartlar */}
          <div className="space-y-3 lg:hidden">
            {liste.map((k) => (
              <div key={k.id} className={'rounded-lg border border-slate-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 ' + (soluk(k) ? 'opacity-60' : '')}>
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="truncate font-medium text-slate-900 dark:text-dark-50">{k.ad}</div>
                    <div className="break-all font-mono text-xs text-slate-500 dark:text-dark-300">{k.gosterim}</div>
                  </div>
                  <DurumRozeti k={k} />
                </div>
                <div className="mt-3"><IzinRozetleri k={k} katalog={katalog} /></div>
                <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                  <dt className="text-slate-500">{cevir('İzinli IP')}</dt>
                  <dd className="break-all font-mono text-slate-700 dark:text-dark-100">{k.izinli_ipler.join(', ') || cevir('her IP')}</dd>
                  <dt className="text-slate-500">{cevir('Son kullanım')}</dt>
                  <dd className="text-slate-700 dark:text-dark-100">{k.son_kullanim_at ? `${tarih(k.son_kullanim_at)} · ${k.son_kullanim_ip}` : cevir('hiç')}</dd>
                  <dt className="text-slate-500">{cevir('Bitiş')}</dt>
                  <dd className="text-slate-700 dark:text-dark-100">{tarih(k.son_kullanma_at)}</dd>
                </dl>
                <div className="mt-3">{eylemler(k)}</div>
              </div>
            ))}
          </div>
        </>
      )}

      <section className="mt-8 rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-600 dark:bg-dark-800">
        <h2 className="text-base font-semibold text-slate-900 dark:text-dark-50">{cevir('Kullanım')}</h2>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-dark-200">
          {cevirT('Anahtarı her istekte, kullanıcı adınızla birlikte Authorization başlığında gönderin: {0}. Anahtar başka bir kullanıcı adıyla geçmez.', `gosp ${katalog?.kullanici_adi || cevir('<kullanıcı adı>')}:<${cevir('anahtar')}>`)}{' '}
          {cevir('Anahtar yalnız kendi kapınızdan (kök anahtarı yönetici portundan, bayi anahtarı bayi portundan) ve yalnız sunucudan sunucuya çağrılarda çalışır; tarayıcıdan (Origin başlığıyla) gelen istekler reddedilir.')}
        </p>
        <div className="mt-3 space-y-3">
          {kullanimOrnekleri(taban, kimlikBasligi(katalog)).map((o) => (
            <div key={o.baslik}>
              <div className="mb-1 text-xs font-medium text-slate-500 dark:text-dark-300">{cevir(o.baslik)}</div>
              <pre className="overflow-x-auto rounded-lg bg-slate-900 p-3 text-xs text-slate-100 dark:bg-dark-900"><code>{o.kod}</code></pre>
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs text-slate-500 dark:text-dark-300">
          {cevir('Parola uçlarında gövde boş bırakılırsa ("password" gönderilmezse) sunucu güçlü bir parola üretir ve yanıtta yalnız bir kez döndürür. FTP ve veritabanı parolaları birbirinden bağımsızdır; biri değişince öteki değişmez.')}
        </p>
        <UyariKutusu ton="bilgi" kucuk className="mt-4">
          {cevir('Anahtar giriş, parola/2FA değişikliği, müşteri adına oturum, phpMyAdmin oturumu ve anahtar yönetimi gibi oturum açan ya da kimlik değiştiren uçlara hiçbir izinle erişemez.')}
          {' '}{cevir('Bayi parolası sıfırlandığında ya da bayi silindiğinde o bayinin tüm anahtarları iptal edilir. Bayi askıya alındığında anahtarları iptal edilmez, askıda kalır; bayi askıdan alınınca yeniden çalışır.')}
        </UyariKutusu>
      </section>

      <AnahtarOlusturModal acik={olusturAcik} katalog={katalog} onKapat={() => setOlusturAcik(false)} onOlusturuldu={tekKezGoster} />
      <AnahtarYenileModal hedef={yenileHedef} onKapat={() => setYenileHedef(null)} onYenilendi={tekKezGoster} />
      <AnahtarGosterModal anahtar={tekKez?.anahtar || null} ad={tekKez?.anahtar_kaydi.ad || ''} kullaniciAdi={katalog?.kullanici_adi || ''} onKapat={() => setTekKez(null)} />
    </div>
  )
}
