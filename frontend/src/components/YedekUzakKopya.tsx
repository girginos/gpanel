import { useCallback, useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api, apiHata } from '@/lib/api'
import { Badge, Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useDialog } from '@/components/Dialog'

// Google Drive'a gönderim durumu (YALNIZ kök; Drive hedefi Genel Yedek Ayarları'nda seçilince görünür).
// Sade akış: hedef seçilip kaydedilince gönderim kendiliğinden açılır, şifreleme otomatiktir ve
// kullanıcıdan anahtar istenmez. Kurtarma anahtarı isteğe bağlı indirilir. Saklama ve bant sınırı
// "Gelişmiş" altındadır. Varsayılan davranış: mevcut yedekler otomatik gönderilmez, tek tıkla gönderilir.

const EN: Record<string, string> = {
  'Google Drive’a gönderim': 'Sending to Google Drive',
  'Açık': 'On',
  'Drive’a gönderilen': 'Sent to Drive',
  'yedek': 'backups',
  'Son gönderim': 'Last upload',
  'Henüz yok': 'None yet',
  'Hata': 'Errors',
  'Sırada': 'Queued',
  'Sistem yedekleri yerelde tamamlanınca kendiliğinden, şifrelenerek Google Drive’a gönderilir. Yerel yedek her zaman önce alınır; gönderim onu geciktirmez. Müşterilerin elle aldığı yedekler gönderilmez.':
    'When system backups finish locally they are sent to Google Drive automatically, encrypted. The local backup is always taken first; sending never delays it. Backups customers take by hand are not sent.',
  'Şifreleme anahtarı sunucuda saklı. Sunucu kaybolursa uzak kopyalar çözülemez —': 'The encryption key is stored on the server. If the server is lost the remote copies cannot be opened —',
  'Kurtarma anahtarını indir': 'Download recovery key',
  'İndiriliyor…': 'Downloading…',
  'Son indirme': 'Last download',
  'Kurtarma anahtarı henüz indirilmedi.': 'The recovery key has not been downloaded yet.',
  'Kurtarma anahtarı indirilemedi': 'Could not download the recovery key',
  'Mevcut yedekleri de gönder': 'Also send existing backups',
  'Her alan adı için en yeni': 'For each domain the newest',
  'Hesaplanıyor…': 'Calculating…',
  'Gönderilecek yedek yok.': 'There are no backups to send.',
  'Drive’a gönderilsin mi?': 'Send to Drive?',
  '{0} yedek ({1}, {2} alan adı) Google Drive’a gönderilecek. Büyük yedekler uzun sürebilir; gönderim arka planda, düşük öncelikle yapılır.':
    '{0} backups ({1}, {2} domains) will be sent to Google Drive. Large backups can take a while; sending runs in the background at low priority.',
  'Gönder': 'Send',
  '{0} yedek sıraya alındı.': '{0} backups queued.',
  'Mevcut yedekler gönderilemedi': 'Could not send existing backups',
  'Son işler': 'Recent jobs',
  'Henüz gönderim işi yok.': 'No upload jobs yet.',
  'Yedek': 'Backup',
  'Alan adı': 'Domain',
  'Durum': 'Status',
  'Boyut': 'Size',
  'Deneme': 'Attempts',
  'Zaman': 'Time',
  'Gönderildi': 'Sent',
  'İptal': 'Cancelled',
  'Gönderiliyor': 'Sending',
  'Silindi (saklama)': 'Deleted (retention)',
  'Yeniden dene': 'Retry',
  'İptal et': 'Cancel',
  'Yeniden denenecek': 'Will retry',
  'İşlem başarısız': 'Operation failed',
  'Gönderim durumu alınamadı': 'Could not load sending status',
  'Önceki sayfa': 'Previous page',
  'Sonraki sayfa': 'Next page',
  'Gelişmiş': 'Advanced',
  'Klasör': 'Folder',
  'Domain başına saklanacak kopya': 'Copies kept per domain',
  'Gün sınırı (0 = yok)': 'Day limit (0 = none)',
  'Bant sınırı KiB/sn (0 = sınırsız)': 'Bandwidth limit KiB/s (0 = unlimited)',
  'Sınırdan fazla ya da eski kopyalar yalnızca bu uygulamanın gönderdikleri arasından silinir; klasördeki başkasının dosyalarına dokunulmaz. Her alan adının en yeni kopyası korunur.':
    'Extra or old copies are deleted only among those this app sent; other files in the folder are never touched. The newest copy of each domain is always kept.',
  'Kaydet': 'Save',
  'Kaydediliyor…': 'Saving…',
  'Ayarlar kaydedildi.': 'Settings saved.',
  'Ayarlar kaydedilemedi': 'Could not save settings',
  'Saklama önizlemesi': 'Retention preview',
  'Silinecek kopya yok.': 'No copies would be deleted.',
  'kopya silinecek': 'copies would be deleted',
  'Güçlendirilmiş şifreleme (yeni nesil)': 'Strengthened encryption (next generation)',
  'Yeni anahtar üret': 'Create a new key',
  'Yeni anahtar hemen kullanılır; eski kopyalar eski anahtarla açılır. Kurtarma anahtarı dosyası eski anahtarları da içerir — yenisini indirip saklayın. Güçlendirilmiş şifrelemeyi açmak için kurtarma araçlarının güncel sürümü gerekir.':
    'The new key is used immediately; older copies are opened with the older key. The recovery file includes the old keys too — download and keep the new one. Strengthened encryption needs the current version of the recovery tools.',
  'Anahtar yenilensin mi?': 'Create a new key?',
  'Yeni kopyalar yeni anahtarla şifrelenecek. Sonrasında kurtarma anahtarını yeniden indirmeyi unutmayın.': 'New copies will use the new key. Remember to download the recovery key again afterwards.',
  'Yenile': 'Renew',
  'Anahtar yenilendi. Kurtarma anahtarını yeniden indirin.': 'Key renewed. Download the recovery key again.',
  'Anahtar yenilenemedi': 'Could not renew the key',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Ayar = { aktif: boolean; hedef_id: number; klasor: string; saklama_n: number; saklama_gun: number; bant_kbps: number }
type Gonderim = { gonderilen: number; bekleyen: number; hata: number; son_gonderim: string }
type Durum = {
  ayar: Ayar
  gonderim: Gonderim
  kurtarma: { var: boolean; indirildi: string }
}
type Is = {
  id: number; yedek_id?: number; domain?: string; yedek_dosya: string; durum: string; deneme: number; son_hata?: string
  boyut_sifreli: number; yuklenen_b: number; olusturma: string; bitis?: string; sonraki_deneme?: string
}
type Onizleme = { n: number; adet: number; domain: number; toplam_b: number; saklama_n: number }

const kutu =
  'w-full px-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-dark-600 ' +
  'bg-white dark:bg-dark-800/40 text-slate-900 dark:text-slate-100 ' +
  'focus:outline-none focus:ring-2 focus:ring-sky-500/40'
const etiket = 'block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1'
const SAYFA = 10

function fmtB(b: number): string {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(2) + ' GB'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + ' MB'
  if (b >= 1 << 10) return (b / (1 << 10)).toFixed(0) + ' KB'
  return b + ' B'
}

function DurumRozeti({ i }: { i: Is }) {
  switch (i.durum) {
    case 'tamam': return <Badge color="success" variant="soft">{cevir('Gönderildi')}</Badge>
    case 'hata': return <Badge color="error" variant="soft">{cevir('Hata')}</Badge>
    case 'bekliyor': return <Badge color="warning" variant="soft">{i.deneme > 0 ? cevir('Yeniden denenecek') : cevir('Sırada')}</Badge>
    case 'sifreleniyor':
    case 'dogrulaniyor': return <Badge color="info" variant="soft">{cevir('Gönderiliyor')}</Badge>
    case 'yukleniyor': {
      const y = i.boyut_sifreli > 0 ? ` %${Math.min(99, Math.floor((i.yuklenen_b * 100) / i.boyut_sifreli))}` : ''
      return <Badge color="info" variant="soft">{cevir('Gönderiliyor')}{y}</Badge>
    }
    case 'silindi':
    case 'siliniyor': return <Badge variant="soft">{cevir('Silindi (saklama)')}</Badge>
    default: return <Badge variant="soft">{cevir('İptal')}</Badge>
  }
}

const aktifDurum = new Set(['bekliyor', 'sifreleniyor', 'yukleniyor', 'dogrulaniyor'])

// yenile: üst sayfa (Genel Yedek Ayarları) hedefi değiştirince artan sayaç → yeniden yükle.
export default function YedekUzakKopya({ yenile = 0 }: { yenile?: number }) {
  useTranslation()
  const { onay } = useDialog()
  const [d, setD] = useState<Durum | null>(null)
  const [isler, setIsler] = useState<Is[]>([])
  const [toplam, setToplam] = useState(0)
  const [sayfa, setSayfa] = useState(1)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [form, setForm] = useState<Ayar | null>(null)
  const [dirty, setDirty] = useState(false)
  const [mesgul, setMesgul] = useState<string | null>(null)
  const [hibrit, setHibrit] = useState(false)
  const [onizleme, setOnizleme] = useState<{ yedek_dosya: string; neden: string }[] | null>(null)
  const [topluN, setTopluN] = useState(7)

  const temizle = () => { setHata(null); setBasari(null) }

  const yukle = useCallback(async () => {
    try {
      const { data } = await api.get<Durum>('/admin/backups/uzak-kopya')
      setD(data)
      setForm(f => (f && dirty ? f : data.ayar))
    } catch (e) {
      setHata(apiHata(e, cevir('Gönderim durumu alınamadı')))
    }
  }, [dirty])
  const islerYukle = useCallback(async (s: number) => {
    try {
      const { data } = await api.get<{ isler: Is[]; toplam: number }>(`/admin/backups/uzak-kopya/isler?sayfa=${s}&adet=${SAYFA}`)
      setIsler(data.isler); setToplam(data.toplam)
    } catch { /* tablo bir sonraki turda yenilenir */ }
  }, [])
  useEffect(() => { void yukle() }, [yukle, yenile])
  useEffect(() => { void islerYukle(sayfa) }, [islerYukle, sayfa, yenile])
  // Aktif iş varken özet + tabloyu 8 sn'de bir tazele
  const etkinIs = isler.some(i => aktifDurum.has(i.durum))
  useEffect(() => {
    if (!etkinIs) return
    const t = setInterval(() => { void islerYukle(sayfa); void yukle() }, 8000)
    return () => clearInterval(t)
  }, [etkinIs, islerYukle, yukle, sayfa])

  async function kurtarmaIndir() {
    temizle(); setMesgul('kurtarma')
    try {
      const r = await api.post('/admin/backups/uzak-kopya/kurtarma-anahtari', undefined, { responseType: 'blob' })
      const cd = String(r.headers['content-disposition'] || '')
      const ad = /filename="([^"]+)"/.exec(cd)?.[1] || 'gosp-yedek-anahtar.txt'
      const url = URL.createObjectURL(new Blob([r.data as BlobPart], { type: 'text/plain' }))
      const a = document.createElement('a')
      a.href = url; a.download = ad
      document.body.appendChild(a); a.click(); a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      await yukle()
    } catch (e) { setHata(apiHata(e, cevir('Kurtarma anahtarı indirilemedi'))) } finally { setMesgul(null) }
  }

  async function mevcutlariGonder() {
    temizle(); setMesgul('toplu')
    try {
      const { data: on } = await api.get<Onizleme>(`/admin/backups/uzak-kopya/mevcutlari-gonder?n=${topluN}`)
      if (on.adet === 0) { setBasari(cevir('Gönderilecek yedek yok.')); return }
      const evet = await onay({
        baslik: cevir('Drive’a gönderilsin mi?'),
        mesaj: cevirT(cevir('{0} yedek ({1}, {2} alan adı) Google Drive’a gönderilecek. Büyük yedekler uzun sürebilir; gönderim arka planda, düşük öncelikle yapılır.'), on.adet, fmtB(on.toplam_b), on.domain),
        onayEtiketi: cevir('Gönder'), onayIkonu: I.gonder,
      })
      if (!evet) return
      const { data } = await api.post<{ kuyruga: number }>('/admin/backups/uzak-kopya/mevcutlari-gonder', { n: topluN })
      setBasari(cevirT(cevir('{0} yedek sıraya alındı.'), data.kuyruga))
      await yukle(); await islerYukle(1); setSayfa(1)
    } catch (e) { setHata(apiHata(e, cevir('Mevcut yedekler gönderilemedi'))) } finally { setMesgul(null) }
  }

  async function kaydet() {
    if (!form) return
    temizle(); setMesgul('kaydet')
    try {
      const { data } = await api.put<Durum>('/admin/backups/uzak-kopya/ayar', form)
      setD(data); setForm(data.ayar); setDirty(false)
      setBasari(cevir('Ayarlar kaydedildi.'))
    } catch (e) { setHata(apiHata(e, cevir('Ayarlar kaydedilemedi'))) } finally { setMesgul(null) }
  }
  async function onizle() {
    temizle(); setMesgul('onizle')
    try {
      const { data } = await api.get<{ silinecek: { yedek_dosya: string; neden: string }[] }>('/admin/backups/uzak-kopya/saklama-onizle')
      setOnizleme(data.silinecek)
    } catch (e) { setHata(apiHata(e, cevir('İşlem başarısız'))) } finally { setMesgul(null) }
  }
  async function anahtarYenile() {
    const evet = await onay({
      baslik: cevir('Anahtar yenilensin mi?'),
      mesaj: cevir('Yeni kopyalar yeni anahtarla şifrelenecek. Sonrasında kurtarma anahtarını yeniden indirmeyi unutmayın.'),
      onayEtiketi: cevir('Yenile'), onayIkonu: I.yenile,
    })
    if (!evet) return
    temizle(); setMesgul('yenile')
    try {
      const { data } = await api.post<Durum>('/admin/backups/uzak-kopya/anahtar-yenile', { hibrit })
      setD(data); setBasari(cevir('Anahtar yenilendi. Kurtarma anahtarını yeniden indirin.'))
    } catch (e) { setHata(apiHata(e, cevir('Anahtar yenilenemedi'))) } finally { setMesgul(null) }
  }
  async function isEylem(yol: string, id: string) {
    temizle(); setMesgul(yol + id)
    try {
      await api.post(`/admin/backups/uzak-kopya/${yol}/${id}`)
      await islerYukle(sayfa); await yukle()
    } catch (e) { setHata(apiHata(e, cevir('İşlem başarısız'))) } finally { setMesgul(null) }
  }
  function ayarDegis(p: Partial<Ayar>) { setDirty(true); setForm(f => (f ? { ...f, ...p } : f)) }

  // Drive gönderimi açık değilse kart hiç görünmez (varsayılan: sayfa değişmez).
  if (!d || !form || !d.ayar.aktif) return hata ? <div className="mb-5"><UyariKutusu ton="tehlike">{hata}</UyariKutusu></div> : null
  const g = d.gonderim
  const sayfaSayisi = Math.max(1, Math.ceil(toplam / SAYFA))

  return (
    <div className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center gap-2">
        <Ikon d={I.bulut} className="h-4 w-4" />
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Google Drive’a gönderim')}</h3>
        <Badge color="success" variant="soft">{cevir('Açık')}</Badge>
      </div>
      <div className="p-4 space-y-4">
        {hata && <UyariKutusu ton="tehlike">{hata}</UyariKutusu>}
        {basari && <UyariKutusu ton="basari">{basari}</UyariKutusu>}

        {/* Sade durum */}
        <p className="text-sm text-slate-700 dark:text-slate-200">
          {cevir('Drive’a gönderilen')}: <strong>{g.gonderilen} {cevir('yedek')}</strong>
          {' · '}{cevir('Son gönderim')}: <strong>{g.son_gonderim || cevir('Henüz yok')}</strong>
          {' · '}{cevir('Hata')}: <strong className={g.hata > 0 ? 'text-red-600 dark:text-red-400' : ''}>{g.hata}</strong>
          {g.bekleyen > 0 && <>{' · '}{cevir('Sırada')}: <strong>{g.bekleyen}</strong></>}
        </p>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {cevir('Sistem yedekleri yerelde tamamlanınca kendiliğinden, şifrelenerek Google Drive’a gönderilir. Yerel yedek her zaman önce alınır; gönderim onu geciktirmez. Müşterilerin elle aldığı yedekler gönderilmez.')}
        </p>

        {/* Kurtarma anahtarı: zorunlu değil, her zaman indirilebilir */}
        {d.kurtarma.var && (
          <div className="rounded-lg border border-slate-200 dark:border-dark-600/60 p-3 flex flex-wrap items-center gap-3">
            <Ikon d={I.kilit} className="h-4 w-4 shrink-0" />
            <p className="text-xs text-slate-600 dark:text-slate-300 flex-1 min-w-[16rem]">
              {cevir('Şifreleme anahtarı sunucuda saklı. Sunucu kaybolursa uzak kopyalar çözülemez —')}
              {d.kurtarma.indirildi
                ? <span className="text-slate-500 dark:text-slate-400"> ({cevir('Son indirme')}: {d.kurtarma.indirildi})</span>
                : <span className="text-amber-700 dark:text-amber-300"> {cevir('Kurtarma anahtarı henüz indirilmedi.')}</span>}
            </p>
            <Button variant="outlined" boyut="sm" ikon={I.indir} onClick={kurtarmaIndir} yukleniyor={mesgul === 'kurtarma'}>
              {mesgul === 'kurtarma' ? cevir('İndiriliyor…') : cevir('Kurtarma anahtarını indir')}
            </Button>
          </div>
        )}

        {/* Mevcut yedekleri de gönder */}
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label className={etiket} htmlFor="uk-toplu-n">{cevir('Her alan adı için en yeni')}</label>
            <input id="uk-toplu-n" type="number" min={1} max={100} className={kutu + ' !w-24'} value={topluN}
              onChange={e => setTopluN(Math.min(100, Math.max(1, Number(e.target.value) || 1)))} />
          </div>
          <Button color="primary" ikon={I.gonder} onClick={mevcutlariGonder} yukleniyor={mesgul === 'toplu'}>
            {mesgul === 'toplu' ? cevir('Hesaplanıyor…') : cevir('Mevcut yedekleri de gönder')}
          </Button>
        </div>

        {/* Son işler (daraltılabilir) */}
        <details className="rounded-lg border border-slate-200 dark:border-dark-600/60 overflow-hidden">
          <summary className="px-3 py-2 text-sm font-medium text-slate-800 dark:text-slate-100 cursor-pointer select-none">{cevir('Son işler')}</summary>
          {isler.length === 0 ? (
            <p className="p-3 text-sm text-slate-500 dark:text-slate-400 border-t border-slate-100 dark:border-dark-600/60">{cevir('Henüz gönderim işi yok.')}</p>
          ) : (
            <div className="overflow-x-auto border-t border-slate-100 dark:border-dark-600/60">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-left text-slate-500 dark:text-slate-400">
                    <th className="px-3 py-2 font-medium">{cevir('Yedek')}</th>
                    <th className="px-3 py-2 font-medium">{cevir('Alan adı')}</th>
                    <th className="px-3 py-2 font-medium">{cevir('Durum')}</th>
                    <th className="px-3 py-2 font-medium">{cevir('Boyut')}</th>
                    <th className="px-3 py-2 font-medium">{cevir('Deneme')}</th>
                    <th className="px-3 py-2 font-medium">{cevir('Zaman')}</th>
                    <th className="px-3 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {isler.map(i => (
                    <tr key={i.id} className="border-t border-slate-100 dark:border-dark-600/60 align-top">
                      <td className="px-3 py-2 font-mono break-all">{i.yedek_dosya}</td>
                      <td className="px-3 py-2">{i.domain || '—'}</td>
                      <td className="px-3 py-2">
                        <DurumRozeti i={i} />
                        {i.son_hata && i.durum !== 'tamam' && <p className="mt-1 max-w-xs text-red-600 dark:text-red-400">{cevir(i.son_hata)}</p>}
                      </td>
                      <td className="px-3 py-2 tabular-nums whitespace-nowrap">{i.boyut_sifreli > 0 ? fmtB(i.boyut_sifreli) : '—'}</td>
                      <td className="px-3 py-2 tabular-nums">{i.deneme}</td>
                      <td className="px-3 py-2 tabular-nums whitespace-nowrap">{i.bitis || i.olusturma}</td>
                      <td className="px-3 py-2 whitespace-nowrap">
                        <div className="flex gap-1.5">
                          {(i.durum === 'hata' || i.durum === 'iptal') && (
                            <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={() => isEylem('yeniden-dene', String(i.id))} yukleniyor={mesgul === 'yeniden-dene' + i.id}>{cevir('Yeniden dene')}</Button>
                          )}
                          {aktifDurum.has(i.durum) && (
                            <Button variant="outlined" color="error" boyut="sm" ikon={I.ban} onClick={() => isEylem('iptal', String(i.id))} yukleniyor={mesgul === 'iptal' + i.id}>{cevir('İptal et')}</Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {toplam > SAYFA && (
            <div className="px-3 py-2 flex items-center gap-2 border-t border-slate-100 dark:border-dark-600/60 text-xs text-slate-500 dark:text-slate-400">
              <Button variant="flat" boyut="sm" ikon={I.geri} onClick={() => setSayfa(s => Math.max(1, s - 1))} disabled={sayfa <= 1} aria-label={cevir('Önceki sayfa')} />
              <span className="tabular-nums">{sayfa} / {sayfaSayisi}</span>
              <Button variant="flat" boyut="sm" ikon={I.ileri} onClick={() => setSayfa(s => Math.min(sayfaSayisi, s + 1))} disabled={sayfa >= sayfaSayisi} aria-label={cevir('Sonraki sayfa')} />
            </div>
          )}
        </details>

        {/* Gelişmiş: klasör, saklama, bant sınırı, anahtar yenileme */}
        <details className="rounded-lg border border-slate-200 dark:border-dark-600/60 overflow-hidden">
          <summary className="px-3 py-2 text-sm font-medium text-slate-800 dark:text-slate-100 cursor-pointer select-none">{cevir('Gelişmiş')}</summary>
          <div className="p-3 space-y-3 border-t border-slate-100 dark:border-dark-600/60">
            <div className="grid sm:grid-cols-2 gap-3">
              <div>
                <label className={etiket} htmlFor="uk-klasor">{cevir('Klasör')}</label>
                <input id="uk-klasor" className={kutu} value={form.klasor} maxLength={128} onChange={e => ayarDegis({ klasor: e.target.value })} />
              </div>
              <div>
                <label className={etiket} htmlFor="uk-n">{cevir('Domain başına saklanacak kopya')}</label>
                <input id="uk-n" type="number" min={1} max={3650} className={kutu} value={form.saklama_n} onChange={e => ayarDegis({ saklama_n: Number(e.target.value) })} />
              </div>
              <div>
                <label className={etiket} htmlFor="uk-gun">{cevir('Gün sınırı (0 = yok)')}</label>
                <input id="uk-gun" type="number" min={0} max={3650} className={kutu} value={form.saklama_gun} onChange={e => ayarDegis({ saklama_gun: Number(e.target.value) })} />
              </div>
              <div>
                <label className={etiket} htmlFor="uk-bant">{cevir('Bant sınırı KiB/sn (0 = sınırsız)')}</label>
                <input id="uk-bant" type="number" min={0} className={kutu} value={form.bant_kbps} onChange={e => ayarDegis({ bant_kbps: Number(e.target.value) })} />
              </div>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Sınırdan fazla ya da eski kopyalar yalnızca bu uygulamanın gönderdikleri arasından silinir; klasördeki başkasının dosyalarına dokunulmaz. Her alan adının en yeni kopyası korunur.')}</p>
            <div className="flex flex-wrap gap-2">
              <Button color="primary" ikon={I.disket} onClick={kaydet} yukleniyor={mesgul === 'kaydet'}>{mesgul === 'kaydet' ? cevir('Kaydediliyor…') : cevir('Kaydet')}</Button>
              <Button variant="outlined" ikon={I.goz} onClick={onizle} yukleniyor={mesgul === 'onizle'}>{cevir('Saklama önizlemesi')}</Button>
            </div>
            {onizleme && (
              <div className="text-xs text-slate-600 dark:text-slate-300">
                {onizleme.length === 0 ? cevir('Silinecek kopya yok.') : (
                  <>
                    <p className="font-medium">{onizleme.length} {cevir('kopya silinecek')}</p>
                    <ul className="mt-1 ml-4 list-disc space-y-0.5">{onizleme.slice(0, 20).map((a, k) => <li key={k}><span className="font-mono">{a.yedek_dosya}</span> — {a.neden}</li>)}</ul>
                  </>
                )}
              </div>
            )}

            <div className="pt-2 border-t border-slate-100 dark:border-dark-600/60 space-y-2">
              <label className="inline-flex items-center gap-2 text-sm text-slate-700 dark:text-slate-200">
                <input type="checkbox" checked={hibrit} onChange={e => setHibrit(e.target.checked)} />
                {cevir('Güçlendirilmiş şifreleme (yeni nesil)')}
              </label>
              <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Yeni anahtar hemen kullanılır; eski kopyalar eski anahtarla açılır. Kurtarma anahtarı dosyası eski anahtarları da içerir — yenisini indirip saklayın. Güçlendirilmiş şifrelemeyi açmak için kurtarma araçlarının güncel sürümü gerekir.')}</p>
              <Button variant="outlined" boyut="sm" ikon={I.anahtar} onClick={anahtarYenile} yukleniyor={mesgul === 'yenile'}>{cevir('Yeni anahtar üret')}</Button>
            </div>
          </div>
        </details>
      </div>
    </div>
  )
}
