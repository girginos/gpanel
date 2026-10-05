import { useCallback, useEffect, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api, apiHata } from '@/lib/api'
import { Badge, Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

// Şifreli UZAK KOPYA (YALNIZ kök): sistem (otomatik/toplu) yedekleri yerelde tamamlanınca age ile
// şifrelenip seçili uzak hedefe (şimdilik Google Drive) gönderilir, uzaktan doğrulanır, sha256
// manifestiyle saklanır. Varsayılan KAPALI. Özel anahtar yalnız üretildiği anda bir kez gösterilir.

const EN: Record<string, string> = {
  'Uzak kopya (şifreli)': 'Remote copy (encrypted)',
  'Sistem yedekleri yerelde tamamlanınca age ile şifrelenir, seçili uzak hedefe gönderilir, uzaktan geri okunarak doğrulanır ve sha256 manifestiyle saklanır. Yerel yedek her zaman önce alınır; uzak gönderim onu geciktirmez. Müşterilerin elle aldığı yedekler uzağa gönderilmez.':
    'When system backups finish locally they are encrypted with age, sent to the selected remote target, verified by reading them back, and stored with a sha256 manifest. The local backup is always taken first; the remote upload never delays it. Backups customers take by hand are not sent remotely.',
  'Şifreleme anahtarı olmadan uzak kopyalar GERİ YÜKLENEMEZ': 'Remote copies CANNOT be restored without the encryption key',
  'Özel anahtarı sunucu dışında güvenli bir yerde saklayın. Sunucu kaybolursa yalnızca bu anahtar uzak kopyaları açabilir.':
    'Keep the private key somewhere safe outside this server. If the server is lost, only this key can open the remote copies.',
  'Şifreleme anahtarı': 'Encryption key',
  'Etkin anahtar': 'Active key',
  'Etkin anahtar yok': 'No active key',
  'Kapalı': 'Off',
  'Parmak izi': 'Fingerprint',
  'Klasik (X25519)': 'Classic (X25519)',
  'Hibrit (ML-KEM-768 + X25519)': 'Hybrid (ML-KEM-768 + X25519)',
  'Klasik anahtar önerilir; hibrit post-kuantum korumalıdır ancak çözmek için age 1.3.0 ya da daha yenisi gerekir.':
    'Classic is recommended; hybrid is post-quantum protected but needs age 1.3.0 or newer to decrypt.',
  'Anahtar Üret': 'Generate Key',
  'Üretiliyor…': 'Generating…',
  'Yeni Anahtar Üret': 'Generate New Key',
  'Kurtarma anahtarı (bir daha GÖSTERİLMEZ)': 'Recovery key (will NOT be shown again)',
  'Bu özel anahtarı şimdi indirin ya da kopyalayın. Sayfayı kapatırsanız tekrar görüntülenemez; yeni anahtar üretmeniz gerekir.':
    'Download or copy this private key now. If you close the page it cannot be shown again; you would have to generate a new key.',
  'İndir': 'Download',
  'Kopyala': 'Copy',
  'Kopyalandı': 'Copied',
  'Özel anahtarı güvenli bir yere sakladım; kaybedersem uzak kopyaların geri yüklenemeyeceğini anlıyorum.':
    'I have saved the private key somewhere safe; I understand remote copies cannot be restored if I lose it.',
  'Anahtarı Etkinleştir': 'Activate Key',
  'Etkinleştiriliyor…': 'Activating…',
  'Anahtar etkinleştirildi.': 'Key activated.',
  'Anahtar üretilemedi': 'Could not generate key',
  'Anahtar etkinleştirilemedi': 'Could not activate key',
  'Önce anahtarı indirin ya da kopyalayın.': 'Download or copy the key first.',
  'Gelişmiş: yalnız açık anahtarımı ekle': 'Advanced: add my public key only',
  'Özel anahtar sizde kalır, sunucuya girmez. Hedefi kaybederseniz çözme sorumluluğu tamamen sizdedir.':
    'The private key stays with you and never enters the server. If you lose it, decrypting is entirely your responsibility.',
  'Açık anahtar (age1…)': 'Public key (age1…)',
  'Ekle': 'Add',
  'Açık anahtar eklendi.': 'Public key added.',
  'Açık anahtar eklenemedi': 'Could not add public key',
  'Ayarlar': 'Settings',
  'Uzak kopya açık': 'Remote copy enabled',
  'Uzak hedef': 'Remote target',
  'Hedef seçin': 'Select a target',
  'Önce yukarıdaki Google Drive kartından bir hedef ekleyip yetkilendirin.': 'First add and authorize a target in the Google Drive card above.',
  'Uzak klasör': 'Remote folder',
  'Domain başına saklanacak kopya': 'Copies kept per domain',
  'Gün sınırı (0 = yok)': 'Day limit (0 = none)',
  'Bant sınırı KiB/sn (0 = sınırsız)': 'Bandwidth limit KiB/s (0 = unlimited)',
  'Sınırdan fazla ya da eski kopyalar yalnızca bu uygulamanın kendi manifestli kopyaları arasından silinir; klasördeki başkasının dosyalarına dokunulmaz. Her domainin en yeni kopyası korunur.':
    'Extra or old copies are deleted only among this app’s own manifested copies; other files in the folder are never touched. The newest copy of each domain is always kept.',
  'Kaydet': 'Save',
  'Kaydediliyor…': 'Saving…',
  'Ayarlar kaydedildi.': 'Settings saved.',
  'Ayarlar kaydedilemedi': 'Could not save settings',
  'Saklama önizlemesi': 'Retention preview',
  'Silinecek kopya yok.': 'No copies would be deleted.',
  'kopya silinecek': 'copies would be deleted',
  'Son işler': 'Recent jobs',
  'Henüz uzak kopya işi yok.': 'No remote copy jobs yet.',
  'Yedek': 'Backup',
  'Alan adı': 'Domain',
  'Durum': 'Status',
  'Boyut': 'Size',
  'Deneme': 'Attempts',
  'Zaman': 'Time',
  'Sırada': 'Queued',
  'Şifreleniyor': 'Encrypting',
  'Yükleniyor': 'Uploading',
  'Doğrulanıyor': 'Verifying',
  'Gönderildi': 'Sent',
  'Hata': 'Error',
  'İptal': 'Cancelled',
  'Silindi (saklama)': 'Deleted (retention)',
  'Yeniden dene': 'Retry',
  'İptal et': 'Cancel',
  'Yeniden denenecek': 'Will retry',
  'Yedek no': 'Backup no',
  'Şimdi gönder': 'Send now',
  'Sıraya alındı.': 'Queued.',
  'Sıraya alınamadı': 'Could not queue',
  'İşlem başarısız': 'Operation failed',
  'Uzak kopya durumu alınamadı': 'Could not load remote copy status',
  'Önceki sayfa': 'Previous page',
  'Sonraki sayfa': 'Next page',
  'Yeni anahtar etkinleşince yeni kopyalar onunla şifrelenir; eski kopyalar eski anahtarla çözülür, eski anahtarı da saklayın.':
    'Once the new key is active new copies are encrypted with it; old copies are decrypted with the old key, so keep the old key too.',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Anahtar = { id: number; parmak: string; kisa_parmak: string; sifreleme: string; durum: string; sunucuda_ozel: boolean; olusturma: string }
type HedefOz = { id: number; ad: string; tur: string; klasor?: string }
type Ayar = { aktif: boolean; hedef_id: number; klasor: string; saklama_n: number; saklama_gun: number; bant_kbps: number }
type Durum = { ayar: Ayar; hedefler: HedefOz[]; etkin_anahtar: Anahtar | null; bekleyen_anahtar: boolean; age_surum: string }
type Is = {
  id: number; yedek_id?: number; domain?: string; yedek_dosya: string; durum: string; deneme: number; son_hata?: string
  boyut_sifreli: number; yuklenen_b: number; olusturma: string; bitis?: string; sonraki_deneme?: string
}
type Uretilen = { parmak: string; sifreleme: string; dosya_adi: string; kurtarma_dosyasi: string }

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
    case 'sifreleniyor': return <Badge color="info" variant="soft">{cevir('Şifreleniyor')}</Badge>
    case 'yukleniyor': {
      const y = i.boyut_sifreli > 0 ? ` %${Math.min(99, Math.floor((i.yuklenen_b * 100) / i.boyut_sifreli))}` : ''
      return <Badge color="info" variant="soft">{cevir('Yükleniyor')}{y}</Badge>
    }
    case 'dogrulaniyor': return <Badge color="info" variant="soft">{cevir('Doğrulanıyor')}</Badge>
    case 'silindi': return <Badge variant="soft">{cevir('Silindi (saklama)')}</Badge>
    default: return <Badge variant="soft">{cevir('İptal')}</Badge>
  }
}

const aktifDurum = new Set(['bekliyor', 'sifreleniyor', 'yukleniyor', 'dogrulaniyor'])

export default function YedekUzakKopya() {
  useTranslation()
  const [d, setD] = useState<Durum | null>(null)
  const [isler, setIsler] = useState<Is[]>([])
  const [toplam, setToplam] = useState(0)
  const [sayfa, setSayfa] = useState(1)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [form, setForm] = useState<Ayar | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null)
  const [hibrit, setHibrit] = useState(false)
  const [uretilen, setUretilen] = useState<Uretilen | null>(null)
  const [kullandi, setKullandi] = useState(false) // indirdi ya da kopyaladı
  const [sakladim, setSakladim] = useState(false)
  const [kopyalandi, setKopyalandi] = useState(false)
  const [aliciMetni, setAliciMetni] = useState('')
  const [onizleme, setOnizleme] = useState<{ yedek_dosya: string; neden: string }[] | null>(null)
  const [yedekNo, setYedekNo] = useState('')
  const formDirty = useRef(false)

  const temizle = () => { setHata(null); setBasari(null) }

  const yukle = useCallback(async () => {
    try {
      const { data } = await api.get<Durum>('/admin/backups/uzak-kopya')
      setD(data)
      if (!formDirty.current) setForm(data.ayar)
    } catch (e) {
      setHata(apiHata(e, cevir('Uzak kopya durumu alınamadı')))
    }
  }, [])
  const islerYukle = useCallback(async (s: number) => {
    try {
      const { data } = await api.get<{ isler: Is[]; toplam: number }>(`/admin/backups/uzak-kopya/isler?sayfa=${s}&adet=${SAYFA}`)
      setIsler(data.isler); setToplam(data.toplam)
    } catch { /* tablo bir sonraki turda yenilenir */ }
  }, [])
  useEffect(() => { void yukle() }, [yukle])
  useEffect(() => { void islerYukle(sayfa) }, [islerYukle, sayfa])
  // Aktif iş varken tabloyu 8 sn'de bir tazele
  const etkinIs = isler.some(i => aktifDurum.has(i.durum))
  useEffect(() => {
    if (!etkinIs) return
    const t = setInterval(() => void islerYukle(sayfa), 8000)
    return () => clearInterval(t)
  }, [etkinIs, islerYukle, sayfa])

  async function anahtarUret() {
    temizle(); setMesgul('uret')
    try {
      const { data } = await api.post<Uretilen>('/admin/backups/uzak-kopya/anahtar-uret', { hibrit })
      setUretilen(data); setKullandi(false); setSakladim(false); setKopyalandi(false)
      await yukle()
    } catch (e) { setHata(apiHata(e, cevir('Anahtar üretilemedi'))) } finally { setMesgul(null) }
  }
  function indir() {
    if (!uretilen) return
    const url = URL.createObjectURL(new Blob([uretilen.kurtarma_dosyasi], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url; a.download = uretilen.dosya_adi
    document.body.appendChild(a); a.click(); a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    setKullandi(true)
  }
  async function kopyala() {
    if (!uretilen) return
    try { await navigator.clipboard.writeText(uretilen.kurtarma_dosyasi); setKopyalandi(true); setKullandi(true) } catch { /* elle seçilebilir */ }
  }
  async function onayla() {
    if (!uretilen) return
    temizle(); setMesgul('onay')
    try {
      await api.post('/admin/backups/uzak-kopya/anahtar-onayla', { parmak: uretilen.parmak, sakladim: true, indirildi: kopyalandi ? false : true })
      setUretilen(null); setSakladim(false); setKullandi(false)
      setBasari(cevir('Anahtar etkinleştirildi.')); await yukle()
    } catch (e) { setHata(apiHata(e, cevir('Anahtar etkinleştirilemedi'))) } finally { setMesgul(null) }
  }
  async function iceAktar() {
    temizle(); setMesgul('ice')
    try {
      await api.post('/admin/backups/uzak-kopya/anahtar-ice-aktar', { alici: aliciMetni.trim() })
      setAliciMetni(''); setBasari(cevir('Açık anahtar eklendi.')); await yukle()
    } catch (e) { setHata(apiHata(e, cevir('Açık anahtar eklenemedi'))) } finally { setMesgul(null) }
  }
  async function kaydet() {
    if (!form) return
    temizle(); setMesgul('kaydet')
    try {
      const { data } = await api.put<Durum>('/admin/backups/uzak-kopya/ayar', form)
      setD(data); setForm(data.ayar); formDirty.current = false
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
  async function isEylem(yol: string, id: string) {
    temizle(); setMesgul(yol + id)
    try {
      await api.post(`/admin/backups/uzak-kopya/${yol}/${id}`)
      await islerYukle(sayfa)
    } catch (e) { setHata(apiHata(e, cevir('İşlem başarısız'))) } finally { setMesgul(null) }
  }
  async function simdiGonder() {
    temizle(); setMesgul('simdi')
    try {
      await api.post(`/admin/backups/uzak-kopya/simdi-gonder/${encodeURIComponent(yedekNo.trim())}`)
      setBasari(cevir('Sıraya alındı.')); setYedekNo(''); await islerYukle(1); setSayfa(1)
    } catch (e) { setHata(apiHata(e, cevir('Sıraya alınamadı'))) } finally { setMesgul(null) }
  }
  function ayarDegis(p: Partial<Ayar>) { formDirty.current = true; setForm(f => (f ? { ...f, ...p } : f)) }

  if (!d || !form) return hata ? <div className="mb-5"><UyariKutusu ton="tehlike">{hata}</UyariKutusu></div> : null
  const etkin = d.etkin_anahtar
  const sayfaSayisi = Math.max(1, Math.ceil(toplam / SAYFA))

  return (
    <div className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center gap-2">
        <Ikon d={I.kilit} className="h-4 w-4" />
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Uzak kopya (şifreli)')}</h3>
        <Badge color={d.ayar.aktif ? 'success' : 'neutral'} variant="soft">{d.ayar.aktif ? cevir('Uzak kopya açık') : cevir('Kapalı')}</Badge>
      </div>
      <div className="p-4 space-y-4">
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {cevir('Sistem yedekleri yerelde tamamlanınca age ile şifrelenir, seçili uzak hedefe gönderilir, uzaktan geri okunarak doğrulanır ve sha256 manifestiyle saklanır. Yerel yedek her zaman önce alınır; uzak gönderim onu geciktirmez. Müşterilerin elle aldığı yedekler uzağa gönderilmez.')}
        </p>
        <UyariKutusu ton="uyari" baslik={cevir('Şifreleme anahtarı olmadan uzak kopyalar GERİ YÜKLENEMEZ')} kucuk>
          {cevir('Özel anahtarı sunucu dışında güvenli bir yerde saklayın. Sunucu kaybolursa yalnızca bu anahtar uzak kopyaları açabilir.')}
        </UyariKutusu>
        {hata && <UyariKutusu ton="tehlike">{hata}</UyariKutusu>}
        {basari && <UyariKutusu ton="basari">{basari}</UyariKutusu>}

        {/* Anahtar */}
        <div className="rounded-lg border border-slate-200 dark:border-dark-600/60 p-3 space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{cevir('Şifreleme anahtarı')}</span>
            {etkin
              ? <>
                  <Badge color="success" variant="soft">{cevir('Etkin anahtar')}</Badge>
                  <span className="text-xs text-slate-500 dark:text-slate-400">
                    {etkin.sifreleme === 'hibrit' ? cevir('Hibrit (ML-KEM-768 + X25519)') : cevir('Klasik (X25519)')} · {cevir('Parmak izi')}: <span className="font-mono">{etkin.kisa_parmak}</span>
                  </span>
                </>
              : <Badge color="warning" variant="soft">{cevir('Etkin anahtar yok')}</Badge>}
          </div>

          {!uretilen && (
            <div className="space-y-2">
              <div className="flex flex-wrap gap-4 text-sm">
                <label className="inline-flex items-center gap-2"><input type="radio" name="uk-tur" checked={!hibrit} onChange={() => setHibrit(false)} />{cevir('Klasik (X25519)')}</label>
                <label className="inline-flex items-center gap-2"><input type="radio" name="uk-tur" checked={hibrit} onChange={() => setHibrit(true)} />{cevir('Hibrit (ML-KEM-768 + X25519)')}</label>
              </div>
              <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Klasik anahtar önerilir; hibrit post-kuantum korumalıdır ancak çözmek için age 1.3.0 ya da daha yenisi gerekir.')}</p>
              {etkin && <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Yeni anahtar etkinleşince yeni kopyalar onunla şifrelenir; eski kopyalar eski anahtarla çözülür, eski anahtarı da saklayın.')}</p>}
              <Button color="primary" variant={etkin ? 'outlined' : 'filled'} ikon={I.anahtar} onClick={anahtarUret} yukleniyor={mesgul === 'uret'}>
                {mesgul === 'uret' ? cevir('Üretiliyor…') : etkin ? cevir('Yeni Anahtar Üret') : cevir('Anahtar Üret')}
              </Button>
            </div>
          )}

          {uretilen && (
            <div className="rounded-lg border border-amber-300/60 dark:border-amber-400/30 bg-amber-50/50 dark:bg-amber-400/5 p-3 space-y-2">
              <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">{cevir('Kurtarma anahtarı (bir daha GÖSTERİLMEZ)')}</div>
              <p className="text-xs text-slate-600 dark:text-slate-300">{cevir('Bu özel anahtarı şimdi indirin ya da kopyalayın. Sayfayı kapatırsanız tekrar görüntülenemez; yeni anahtar üretmeniz gerekir.')}</p>
              <textarea readOnly rows={9} className={kutu + ' font-mono text-xs'} value={uretilen.kurtarma_dosyasi} onFocus={e => e.currentTarget.select()} aria-label={cevir('Kurtarma anahtarı (bir daha GÖSTERİLMEZ)')} />
              <div className="flex flex-wrap gap-2">
                <Button color="primary" boyut="sm" ikon={I.indir} onClick={indir}>{cevir('İndir')}</Button>
                <Button variant="outlined" boyut="sm" ikon={I.kopya} onClick={kopyala}>{kopyalandi ? cevir('Kopyalandı') : cevir('Kopyala')}</Button>
              </div>
              <label className="flex items-start gap-2 text-sm text-slate-700 dark:text-slate-200">
                <input type="checkbox" className="mt-1" checked={sakladim} onChange={e => setSakladim(e.target.checked)} />
                <span>{cevir('Özel anahtarı güvenli bir yere sakladım; kaybedersem uzak kopyaların geri yüklenemeyeceğini anlıyorum.')}</span>
              </label>
              {!kullandi && <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Önce anahtarı indirin ya da kopyalayın.')}</p>}
              <Button color="primary" ikon={I.onay} onClick={onayla} disabled={!sakladim || !kullandi} yukleniyor={mesgul === 'onay'}>
                {mesgul === 'onay' ? cevir('Etkinleştiriliyor…') : cevir('Anahtarı Etkinleştir')}
              </Button>
            </div>
          )}

          <details className="text-xs text-slate-600 dark:text-slate-300">
            <summary className="cursor-pointer font-medium">{cevir('Gelişmiş: yalnız açık anahtarımı ekle')}</summary>
            <div className="mt-2 space-y-2">
              <p>{cevir('Özel anahtar sizde kalır, sunucuya girmez. Hedefi kaybederseniz çözme sorumluluğu tamamen sizdedir.')}</p>
              <label className={etiket} htmlFor="uk-alici">{cevir('Açık anahtar (age1…)')}</label>
              <input id="uk-alici" className={kutu + ' font-mono'} value={aliciMetni} autoComplete="off" onChange={e => setAliciMetni(e.target.value)} />
              <Button variant="outlined" boyut="sm" ikon={I.arti} onClick={iceAktar} disabled={!aliciMetni.trim()} yukleniyor={mesgul === 'ice'}>{cevir('Ekle')}</Button>
            </div>
          </details>
        </div>

        {/* Ayarlar */}
        <div className="rounded-lg border border-slate-200 dark:border-dark-600/60 p-3 space-y-3">
          <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{cevir('Ayarlar')}</span>
          <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-200">
            <input type="checkbox" checked={form.aktif} onChange={e => ayarDegis({ aktif: e.target.checked })} disabled={!etkin} />
            {cevir('Uzak kopya açık')}
          </label>
          <div className="grid sm:grid-cols-2 gap-3">
            <div>
              <label className={etiket} htmlFor="uk-hedef">{cevir('Uzak hedef')}</label>
              <select id="uk-hedef" className={kutu} value={form.hedef_id || 0} onChange={e => ayarDegis({ hedef_id: Number(e.target.value) })}>
                <option value={0}>{cevir('Hedef seçin')}</option>
                {d.hedefler.map(h => <option key={h.id} value={h.id}>{h.ad}</option>)}
              </select>
              {d.hedefler.length === 0 && <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('Önce yukarıdaki Google Drive kartından bir hedef ekleyip yetkilendirin.')}</p>}
            </div>
            <div>
              <label className={etiket} htmlFor="uk-klasor">{cevir('Uzak klasör')}</label>
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
          <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Sınırdan fazla ya da eski kopyalar yalnızca bu uygulamanın kendi manifestli kopyaları arasından silinir; klasördeki başkasının dosyalarına dokunulmaz. Her domainin en yeni kopyası korunur.')}</p>
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
        </div>

        {/* İşler */}
        <div className="rounded-lg border border-slate-200 dark:border-dark-600/60 overflow-hidden">
          <div className="px-3 py-2 flex flex-wrap items-center gap-2 border-b border-slate-100 dark:border-dark-600/60">
            <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{cevir('Son işler')}</span>
            <div className="ml-auto flex items-center gap-2">
              <input aria-label={cevir('Yedek no')} placeholder={cevir('Yedek no')} inputMode="numeric" className={kutu + ' !w-28 !py-1'} value={yedekNo} onChange={e => setYedekNo(e.target.value.replace(/\D/g, ''))} />
              <Button variant="outlined" boyut="sm" ikon={I.gonder} onClick={simdiGonder} disabled={!yedekNo || !d.ayar.aktif} yukleniyor={mesgul === 'simdi'}>{cevir('Şimdi gönder')}</Button>
            </div>
          </div>
          {isler.length === 0 ? (
            <p className="p-3 text-sm text-slate-500 dark:text-slate-400">{cevir('Henüz uzak kopya işi yok.')}</p>
          ) : (
            <div className="overflow-x-auto">
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
        </div>
      </div>
    </div>
  )
}
