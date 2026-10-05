import { useCallback, useEffect, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import { Badge, Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import ConfirmDialog from '@/components/ConfirmDialog'

// Google Drive uzak yedek hedefi (YALNIZ kök): varsayılan olarak GirginOS'un ORTAK OAuth
// istemcisiyle tek düğmeyle ("Google ile bağlan") ya da "Gelişmiş"ten kendi Google Cloud
// istemcinizle (cihaz kodu akışı, yalnız drive.file izni) bağlanır. Sırlar (client secret, yenileme
// jetonu) sunucuda şifreli durur ve bu ekrana ASLA geri dönmez.

const DRIVE_EN: Record<string, string> = {
  'Google Drive Yedek Hedefi': 'Google Drive Backup Target',
  'Kendi Google Cloud OAuth istemcinizle bağlanır; yalnızca uygulamanın oluşturduğu dosyalara erişir (drive.file). Yedekler yüklenmeden önce şifrelenir.':
    'Connects with your own Google Cloud OAuth client; it can only access files the app created (drive.file). Backups are encrypted before upload.',
  'Hedefi burada tanımlayıp yetkilendirin; yedeklerin şifreli gönderimi aşağıdaki “Uzak kopya” kartından açılır.':
    'Define and authorize the target here; encrypted upload of backups is turned on in the “Remote copy” card below.',
  'Yenileme jetonu 7 günde dolar': 'Refresh token expires in 7 days',
  'Google OAuth uygulamanız “Test” durumundaysa yenileme jetonu 7 gün sonra geçersiz olur ve hedefi yeniden yetkilendirmeniz gerekir. Kalıcı kullanım için OAuth onay ekranını “Üretim”e (In production) alın.':
    'If your Google OAuth app is in “Testing” status, the refresh token becomes invalid after 7 days and you must re-authorize the target. For permanent use, set the OAuth consent screen to “In production”.',
  'Kurulum adımları': 'Setup steps',
  'Google Cloud Console’da bir proje oluşturun (ya da var olanı seçin).': 'Create a project in Google Cloud Console (or pick an existing one).',
  '“API’ler ve Hizmetler” bölümünden Google Drive API’yi etkinleştirin.': 'Enable the Google Drive API under “APIs & Services”.',
  'OAuth izin ekranını yapılandırın (kullanıcı türü: Harici). drive.file kapsamı yeterlidir; daha geniş kapsam EKLEMEYİN.':
    'Configure the OAuth consent screen (user type: External). The drive.file scope is enough; do NOT add broader scopes.',
  'Kimlik bilgileri → OAuth istemci kimliği oluştur → uygulama türü “TV’ler ve Sınırlı Girişli Cihazlar”.':
    'Credentials → Create OAuth client ID → application type “TVs and Limited Input devices”.',
  'Client ID ve Client Secret değerlerini aşağıya girin, sonra “Yetkilendir”e basın.': 'Enter the Client ID and Client Secret below, then press “Authorize”.',
  'Google hesabınızla bağlanın; yalnızca uygulamanın oluşturduğu dosyalara erişir (drive.file). Yedekler yüklenmeden önce şifrelenir.':
    'Connect with your Google account; it can only access files the app created (drive.file). Backups are encrypted before upload.',
  'Google ile bağlan': 'Connect with Google',
  'Bağlanıyor…': 'Connecting…',
  'Gelişmiş: kendi Google istemcimi kullan': 'Advanced: use my own Google client',
  'Kendi Google istemcimi kullan (Client ID / Secret)': 'Use my own Google client (Client ID / Secret)',
  'Hedefler alınamadı': 'Could not load targets',
  'Hedef Adı': 'Target name',
  'Client ID': 'Client ID',
  'Client Secret': 'Client Secret',
  'Drive klasörü': 'Drive folder',
  'Drive kökünde açılacak klasörün adı': 'Name of the folder created in Drive root',
  'Hedef Ekle': 'Add Target',
  'Ekleniyor…': 'Adding…',
  'Hedef eklendi. Şimdi “Yetkilendir”e basın.': 'Target added. Now press “Authorize”.',
  'Hedef eklenemedi': 'Could not add target',
  'Henüz Google Drive hedefi yok.': 'No Google Drive targets yet.',
  'Bağlı': 'Connected',
  'Yetkilendirme gerekli': 'Authorization required',
  'Yetkilendirilmedi': 'Not authorized',
  'Hata': 'Error',
  'Yetkilendir': 'Authorize',
  'Yeniden Yetkilendir': 'Re-authorize',
  'Başlatılıyor…': 'Starting…',
  'Bağlantıyı Test Et': 'Test Connection',
  'Test ediliyor…': 'Testing…',
  'Bağlantı başarılı': 'Connection successful',
  'Bağlantıyı Kes': 'Disconnect',
  'Sil': 'Delete',
  'İptal': 'Cancel',
  'Klasör': 'Folder',
  'Son deneme': 'Last attempt',
  'Son başarı': 'Last success',
  'Google ile yetkilendirme': 'Authorize with Google',
  '1) Aşağıdaki adresi açın': '1) Open the address below',
  '2) Bu kodu girin': '2) Enter this code',
  '3) Google hesabınızla izin verin. Bu sayfa onayı otomatik algılar.': '3) Grant access with your Google account. This page detects the approval automatically.',
  'Onay bekleniyor…': 'Waiting for approval…',
  'Yetkilendirme tamamlandı.': 'Authorization complete.',
  'Google erişimi reddedildi.': 'Google access was denied.',
  'Kodun süresi doldu; yeniden başlatın.': 'The code expired; start again.',
  'Yetkilendirme başlatılamadı': 'Could not start authorization',
  'Yetkilendirme sorgulanamadı': 'Could not check authorization',
  'Bağlantı kesildi.': 'Disconnected.',
  'Bağlantı kesildi; ancak Google tarafında erişim iptal edilemedi. Erişimi Google Hesabı → Güvenlik → Üçüncü taraf erişimi sayfasından da kaldırabilirsiniz.':
    'Disconnected; but access could not be revoked at Google. You can also remove it under Google Account → Security → Third-party access.',
  'Bağlantı kesilemedi': 'Could not disconnect',
  'Bağlantıyı kes?': 'Disconnect?',
  'Google’daki yenileme jetonu iptal edilir ve sunucudan silinir. Hedefi yeniden kullanmak için yeniden yetkilendirmeniz gerekir.':
    'The refresh token at Google is revoked and deleted from the server. You must re-authorize to use the target again.',
  'Hedefi sil?': 'Delete target?',
  'Hedef ve kayıtlı kimlik bilgileri silinir; Google erişimi iptal edilir. Drive’daki yedek dosyalarınız silinmez.':
    'The target and stored credentials are deleted; Google access is revoked. Backup files in Drive are NOT deleted.',
  'Hedef silindi.': 'Target deleted.',
  'Hedef silinemedi': 'Could not delete target',
  'Test başarısız': 'Test failed',
  'Kopyala': 'Copy',
  // Hata kategorisi açıklamaları (sunucu Türkçe döner)
  'kimlik doğrulanamadı (kullanıcı adı, parola, erişim anahtarı ya da yetki jetonu)': 'authentication failed (user name, password, access key or authorization token)',
  'erişim reddedildi (izin yok ya da adres engelli)': 'access denied (no permission or address blocked)',
  'uzak dosya ya da dizin bulunamadı': 'remote file or directory not found',
  'ağ hatası (bağlantı kurulamadı, zaman aşımı ya da ad çözülemedi)': 'network error (could not connect, timed out or name not resolved)',
  'TLS sertifikası ya da sunucu anahtarı doğrulanamadı': 'TLS certificate or server key could not be verified',
  'uzak depoda yer ya da kota yetersiz': 'not enough space or quota on the remote store',
  'sağlayıcının istek sınırına takıldı; daha sonra yeniden denenecek': 'hit the provider’s rate limit; will be retried later',
  'bilinmeyen hata (ayrıntı sunucu günlüğünde)': 'unknown error (details in the server log)',
  'Google hesabı henüz yetkilendirilmedi': 'The Google account is not authorized yet',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (DRIVE_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Hedef = {
  id: number
  ad: string
  tur: string
  aktif: boolean
  client_id?: string
  klasor?: string
  durum: string
  yetki_var: boolean
  yetki_bekliyor: boolean
  hata_kategori?: string
  hata_aciklama?: string
  istemci?: string
  son_deneme?: string
  son_basari?: string
}

type Yetki = { id: number; kod: string; adres: string; aralikSn: number; bitis: number }

const kutu =
  'w-full px-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-dark-600 ' +
  'bg-white dark:bg-dark-800/40 text-slate-900 dark:text-slate-100 ' +
  'focus:outline-none focus:ring-2 focus:ring-sky-500/40'
const etiket = 'block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1'

function DurumRozeti({ h }: { h: Hedef }) {
  switch (h.durum) {
    case 'bagli': return <Badge color="success" variant="soft">{cevir('Bağlı')}</Badge>
    case 'yetki-gerekli': return <Badge color="warning" variant="soft">{cevir('Yetkilendirme gerekli')}</Badge>
    case 'hata': return <Badge color="error" variant="soft">{cevir('Hata')}</Badge>
    default: return <Badge variant="soft">{cevir('Yetkilendirilmedi')}</Badge>
  }
}

export default function YedekGoogleDrive() {
  useTranslation() // dil re-render aboneligi
  const [liste, setListe] = useState<Hedef[] | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [uyari, setUyari] = useState<string | null>(null)
  const [ekleniyor, setEkleniyor] = useState(false)
  const [form, setForm] = useState({ ad: '', client_id: '', client_secret: '', klasor: '' })
  const [ortak, setOrtak] = useState(false) // ortak (GirginOS) Google istemcisi hazır mı
  const [kendiIstemci, setKendiIstemci] = useState(false) // Gelişmiş: kendi istemcimi kullan
  const kendi = !ortak || kendiIstemci
  const [yetki, setYetki] = useState<Yetki | null>(null)
  const [yetkiMesaj, setYetkiMesaj] = useState<string | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null) // "<islem>:<id>"
  const [silOnay, setSilOnay] = useState<Hedef | null>(null)
  const [kesOnay, setKesOnay] = useState<Hedef | null>(null)
  const yukle = useCallback(async () => {
    try {
      const { data } = await api.get<Hedef[]>('/admin/backups/hedefler')
      setListe(data.filter(h => h.tur === 'drive'))
      try {
        const { data: o } = await api.get<{ ortak_istemci: boolean }>('/admin/backups/hedefler/ortak-istemci')
        setOrtak(!!o.ortak_istemci)
      } catch { setOrtak(false) }
    } catch (e) {
      setHata(apiHata(e, cevir('Hedefler alınamadı')))
    }
  }, [])
  useEffect(() => { void yukle() }, [yukle])

  // Yetkilendirme sorgusu: Google'ın bildirdiği aralıkla; onay/ret/süre dolunca durur.
  const yetkiRef = useRef<Yetki | null>(null)
  yetkiRef.current = yetki
  useEffect(() => {
    if (!yetki) return
    const sure = Math.max(yetki.aralikSn, 5) * 1000
    const t = setInterval(async () => {
      const y = yetkiRef.current
      if (!y) return
      if (Date.now() > y.bitis) {
        setYetkiMesaj(cevir('Kodun süresi doldu; yeniden başlatın.'))
        setYetki(null)
        return
      }
      try {
        const { data } = await api.post(`/admin/backups/hedefler/${y.id}/yetki-sorgu`)
        const durum = String(data.durum || '')
        if (durum === 'tamam') {
          setYetki(null); setYetkiMesaj(null); setBasari(cevir('Yetkilendirme tamamlandı.')); void yukle()
        } else if (durum === 'reddedildi') {
          setYetki(null); setYetkiMesaj(cevir('Google erişimi reddedildi.'))
        } else if (durum === 'suresi_doldu') {
          setYetki(null); setYetkiMesaj(cevir('Kodun süresi doldu; yeniden başlatın.'))
        } else if (durum === 'yok') {
          setYetki(null)
        } else if (data.ok === false) {
          setYetki(null); setHata(cevir(String(data.aciklama || '')) || cevir('Yetkilendirme sorgulanamadı'))
        }
      } catch (e) {
        setYetki(null); setHata(apiHata(e, cevir('Yetkilendirme sorgulanamadı')))
      }
    }, sure)
    return () => clearInterval(t)
  }, [yetki?.id, yetki?.aralikSn, yukle]) // eslint-disable-line react-hooks/exhaustive-deps

  function temizle() { setHata(null); setBasari(null); setUyari(null) }

  // Ortak istemci: ad otomatik ("Google Drive", doluysa "Google Drive 2"...); eklenir eklenmez
  // cihaz kodu akışı başlar. Kendi istemci: Client ID/Secret ile eklenir, sonra "Yetkilendir".
  function otomatikAd(): string {
    const var_ = new Set((liste ?? []).map(h => h.ad))
    if (!var_.has('Google Drive')) return 'Google Drive'
    for (let n = 2; n < 100; n++) if (!var_.has(`Google Drive ${n}`)) return `Google Drive ${n}`
    return 'Google Drive'
  }

  async function ekle() {
    temizle(); setEkleniyor(true)
    try {
      if (!kendi) {
        const { data } = await api.post<Hedef>('/admin/backups/hedefler/drive',
          { ad: form.ad.trim() || otomatikAd(), klasor: form.klasor.trim() })
        setForm({ ad: '', client_id: '', client_secret: '', klasor: '' })
        await yukle()
        if (data?.id) await yetkilendir({ id: data.id })
        return
      }
      await api.post('/admin/backups/hedefler/drive', form)
      setForm({ ad: '', client_id: '', client_secret: '', klasor: '' })
      setBasari(cevir('Hedef eklendi. Şimdi “Yetkilendir”e basın.'))
      await yukle()
    } catch (e) {
      setHata(apiHata(e, cevir('Hedef eklenemedi')))
    } finally {
      setEkleniyor(false)
    }
  }

  async function yetkilendir(h: Pick<Hedef, 'id'>) {
    temizle(); setYetkiMesaj(null); setMesgul('yetki:' + h.id)
    try {
      const { data } = await api.post(`/admin/backups/hedefler/${h.id}/yetki-baslat`)
      if (data.ok === false) { setHata(cevir(String(data.aciklama || '')) || cevir('Yetkilendirme başlatılamadı')); return }
      setYetki({ id: h.id, kod: String(data.kullanici_kodu), adres: String(data.dogrulama_adresi),
        aralikSn: Number(data.aralik_sn) || 5, bitis: Date.now() + (Number(data.gecerlilik_sn) || 1800) * 1000 })
    } catch (e) {
      setHata(apiHata(e, cevir('Yetkilendirme başlatılamadı')))
    } finally {
      setMesgul(null)
    }
  }

  async function test(h: Hedef) {
    temizle(); setMesgul('test:' + h.id)
    try {
      const { data } = await api.post(`/admin/backups/hedefler/${h.id}/test`, undefined, UZUN_ISTEK)
      if (data.ok) setBasari(cevir('Bağlantı başarılı'))
      else setHata(`${cevir('Test başarısız')}: ${cevir(String(data.aciklama || ''))}`)
      await yukle()
    } catch (e) {
      setHata(apiHata(e, cevir('Test başarısız')))
    } finally {
      setMesgul(null)
    }
  }

  async function baglantiKes(h: Hedef) {
    temizle()
    try {
      const { data } = await api.post(`/admin/backups/hedefler/${h.id}/baglantiyi-kes`)
      if (data.google_iptal === false) setUyari(cevir('Bağlantı kesildi; ancak Google tarafında erişim iptal edilemedi. Erişimi Google Hesabı → Güvenlik → Üçüncü taraf erişimi sayfasından da kaldırabilirsiniz.'))
      else setBasari(cevir('Bağlantı kesildi.'))
      if (yetki?.id === h.id) setYetki(null)
      await yukle()
    } catch (e) {
      setHata(apiHata(e, cevir('Bağlantı kesilemedi')))
    } finally {
      setKesOnay(null)
    }
  }

  async function sil(h: Hedef) {
    temizle()
    try {
      await api.delete(`/admin/backups/hedefler/${h.id}`)
      if (yetki?.id === h.id) setYetki(null)
      setBasari(cevir('Hedef silindi.'))
      await yukle()
    } catch (e) {
      setHata(apiHata(e, cevir('Hedef silinemedi')))
    } finally {
      setSilOnay(null)
    }
  }

  if (liste === null && !hata) return null

  return (
    <div className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center gap-2">
        <Ikon d={I.bulut} className="h-4 w-4" />
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Google Drive Yedek Hedefi')}</h3>
      </div>

      <div className="p-4 space-y-4">
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {kendi
            ? cevir('Kendi Google Cloud OAuth istemcinizle bağlanır; yalnızca uygulamanın oluşturduğu dosyalara erişir (drive.file). Yedekler yüklenmeden önce şifrelenir.')
            : cevir('Google hesabınızla bağlanın; yalnızca uygulamanın oluşturduğu dosyalara erişir (drive.file). Yedekler yüklenmeden önce şifrelenir.')}
          {' '}{cevir('Hedefi burada tanımlayıp yetkilendirin; yedeklerin şifreli gönderimi aşağıdaki “Uzak kopya” kartından açılır.')}
        </p>

        {kendi && <UyariKutusu ton="uyari" baslik={cevir('Yenileme jetonu 7 günde dolar')} kucuk>
          {cevir('Google OAuth uygulamanız “Test” durumundaysa yenileme jetonu 7 gün sonra geçersiz olur ve hedefi yeniden yetkilendirmeniz gerekir. Kalıcı kullanım için OAuth onay ekranını “Üretim”e (In production) alın.')}
        </UyariKutusu>}

        {hata && <UyariKutusu ton="tehlike">{hata}</UyariKutusu>}
        {uyari && <UyariKutusu ton="uyari">{uyari}</UyariKutusu>}
        {basari && <UyariKutusu ton="basari">{basari}</UyariKutusu>}
        {yetkiMesaj && <UyariKutusu ton="uyari">{yetkiMesaj}</UyariKutusu>}

        {kendi && <details className="text-xs text-slate-600 dark:text-slate-300">
          <summary className="cursor-pointer font-medium">{cevir('Kurulum adımları')}</summary>
          <ol className="mt-2 ml-5 list-decimal space-y-1">
            <li>{cevir('Google Cloud Console’da bir proje oluşturun (ya da var olanı seçin).')}</li>
            <li>{cevir('“API’ler ve Hizmetler” bölümünden Google Drive API’yi etkinleştirin.')}</li>
            <li>{cevir('OAuth izin ekranını yapılandırın (kullanıcı türü: Harici). drive.file kapsamı yeterlidir; daha geniş kapsam EKLEMEYİN.')}</li>
            <li>{cevir('Kimlik bilgileri → OAuth istemci kimliği oluştur → uygulama türü “TV’ler ve Sınırlı Girişli Cihazlar”.')}</li>
            <li>{cevir('Client ID ve Client Secret değerlerini aşağıya girin, sonra “Yetkilendir”e basın.')}</li>
          </ol>
        </details>}

        {/* Yetkilendirme paneli: kod + adres; onayı otomatik algılar */}
        {yetki && (
          <div className="rounded-lg border border-sky-300/50 dark:border-sky-400/30 bg-sky-50/50 dark:bg-sky-400/5 p-4 space-y-2">
            <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">{cevir('Google ile yetkilendirme')}</div>
            <div className="text-xs text-slate-600 dark:text-slate-300">{cevir('1) Aşağıdaki adresi açın')}</div>
            <a href={yetki.adres} target="_blank" rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-sm font-medium text-sky-700 dark:text-sky-300 underline">
              {yetki.adres}<Ikon d={I.harici} className="h-3.5 w-3.5" />
            </a>
            <div className="text-xs text-slate-600 dark:text-slate-300">{cevir('2) Bu kodu girin')}</div>
            <div className="font-mono text-2xl tracking-widest text-slate-900 dark:text-slate-50 select-all">{yetki.kod}</div>
            <div className="text-xs text-slate-600 dark:text-slate-300">{cevir('3) Google hesabınızla izin verin. Bu sayfa onayı otomatik algılar.')}</div>
            <div className="flex items-center gap-3 pt-1">
              <span className="text-xs text-slate-500 dark:text-slate-400 inline-flex items-center gap-1.5">
                <Ikon d={I.yenile} className="h-3.5 w-3.5 animate-spin" />{cevir('Onay bekleniyor…')}
              </span>
              <Button variant="outlined" boyut="sm" ikon={I.kapat} onClick={() => setYetki(null)}>{cevir('İptal')}</Button>
            </div>
          </div>
        )}

        {/* Hedef listesi */}
        <div className="space-y-2">
          {liste && liste.length === 0 && (
            <p className="text-sm text-slate-500 dark:text-slate-400">{cevir('Henüz Google Drive hedefi yok.')}</p>
          )}
          {liste?.map(h => (
            <div key={h.id} className="rounded-lg border border-slate-200 dark:border-dark-600/60 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{h.ad}</span>
                <DurumRozeti h={h} />
                <span className="text-xs text-slate-500 dark:text-slate-400">
                  {cevir('Klasör')}: <strong>{h.klasor}</strong>
                  {h.istemci === 'ortak' ? null : h.client_id ? <> · <span className="font-mono">{h.client_id.slice(0, 14)}…</span></> : null}
                </span>
              </div>
              {(h.son_deneme || h.son_basari) && (
                <p className="mt-1 text-xs text-slate-500 dark:text-slate-400 tabular-nums">
                  {h.son_deneme && <>{cevir('Son deneme')}: {h.son_deneme}</>}
                  {h.son_basari && <>{h.son_deneme ? ' · ' : ''}{cevir('Son başarı')}: {h.son_basari}</>}
                </p>
              )}
              {h.hata_aciklama && h.durum !== 'bagli' && (
                <p className="mt-1 text-xs text-red-600 dark:text-red-400">{cevir(h.hata_aciklama)}</p>
              )}
              <div className="mt-2 flex flex-wrap gap-2">
                <Button variant={h.durum === 'bagli' ? 'outlined' : 'filled'} color="primary" boyut="sm" ikon={I.anahtar}
                  onClick={() => yetkilendir(h)} yukleniyor={mesgul === 'yetki:' + h.id}
                  disabled={yetki?.id === h.id}>
                  {mesgul === 'yetki:' + h.id ? cevir('Başlatılıyor…') : h.yetki_var ? cevir('Yeniden Yetkilendir') : cevir('Yetkilendir')}
                </Button>
                {h.yetki_var && (
                  <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={() => test(h)} yukleniyor={mesgul === 'test:' + h.id}>
                    {mesgul === 'test:' + h.id ? cevir('Test ediliyor…') : cevir('Bağlantıyı Test Et')}
                  </Button>
                )}
                {h.yetki_var && (
                  <Button variant="outlined" color="error" boyut="sm" ikon={I.ban} onClick={() => setKesOnay(h)}>
                    {cevir('Bağlantıyı Kes')}
                  </Button>
                )}
                <Button variant="outlined" color="error" boyut="sm" ikon={I.cop} onClick={() => setSilOnay(h)}>
                  {cevir('Sil')}
                </Button>
              </div>
            </div>
          ))}
        </div>

        {/* Ekleme: ortak istemci varsa tek düğme; alanlar "Gelişmiş"te */}
        {!kendi && (
          <div>
            <Button color="primary" ikon={I.bulut} onClick={ekle} yukleniyor={ekleniyor}>
              {ekleniyor ? cevir('Bağlanıyor…') : cevir('Google ile bağlan')}
            </Button>
          </div>
        )}
        {kendi && (
          <div className="grid sm:grid-cols-2 gap-3">
            <div>
              <label className={etiket} htmlFor="gd-ad">{cevir('Hedef Adı')}</label>
              <input id="gd-ad" className={kutu} value={form.ad} maxLength={64}
                onChange={e => setForm(f => ({ ...f, ad: e.target.value }))} />
            </div>
            <div>
              <label className={etiket} htmlFor="gd-klasor">{cevir('Drive klasörü')}</label>
              <input id="gd-klasor" className={kutu} value={form.klasor} placeholder="GirginOSPanel Yedek" maxLength={100}
                onChange={e => setForm(f => ({ ...f, klasor: e.target.value }))} />
              <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('Drive kökünde açılacak klasörün adı')}</p>
            </div>
            <div>
              <label className={etiket} htmlFor="gd-cid">{cevir('Client ID')}</label>
              <input id="gd-cid" className={kutu} value={form.client_id} autoComplete="off" placeholder="…apps.googleusercontent.com"
                onChange={e => setForm(f => ({ ...f, client_id: e.target.value }))} />
            </div>
            <div>
              <label className={etiket} htmlFor="gd-sec">{cevir('Client Secret')}</label>
              <input id="gd-sec" type="password" className={kutu} value={form.client_secret} autoComplete="new-password"
                onChange={e => setForm(f => ({ ...f, client_secret: e.target.value }))} />
            </div>
          </div>
        )}
        {kendi && (
          <div>
            <Button color="primary" ikon={I.arti} onClick={ekle} yukleniyor={ekleniyor}
              disabled={!form.ad.trim() || !form.client_id.trim() || !form.client_secret.trim()}>
              {ekleniyor ? cevir('Ekleniyor…') : cevir('Hedef Ekle')}
            </Button>
          </div>
        )}

        {ortak && (
          <details className="text-xs text-slate-600 dark:text-slate-300" open={kendiIstemci}>
            <summary className="cursor-pointer font-medium">{cevir('Gelişmiş: kendi Google istemcimi kullan')}</summary>
            <div className="mt-2 space-y-3">
              {!kendiIstemci && (
                <div className="grid sm:grid-cols-2 gap-3">
                  <div>
                    <label className={etiket} htmlFor="gd-ad2">{cevir('Hedef Adı')}</label>
                    <input id="gd-ad2" className={kutu} value={form.ad} maxLength={64} placeholder="Google Drive"
                      onChange={e => setForm(f => ({ ...f, ad: e.target.value }))} />
                  </div>
                  <div>
                    <label className={etiket} htmlFor="gd-klasor2">{cevir('Drive klasörü')}</label>
                    <input id="gd-klasor2" className={kutu} value={form.klasor} placeholder="GirginOSPanel Yedek" maxLength={100}
                      onChange={e => setForm(f => ({ ...f, klasor: e.target.value }))} />
                  </div>
                </div>
              )}
              <label className="inline-flex items-center gap-2">
                <input type="checkbox" checked={kendiIstemci} onChange={e => setKendiIstemci(e.target.checked)} />
                {cevir('Kendi Google istemcimi kullan (Client ID / Secret)')}
              </label>
            </div>
          </details>
        )}
      </div>

      <ConfirmDialog acik={kesOnay !== null} baslik={cevir('Bağlantıyı kes?')}
        mesaj={cevir('Google’daki yenileme jetonu iptal edilir ve sunucudan silinir. Hedefi yeniden kullanmak için yeniden yetkilendirmeniz gerekir.')}
        onayMetni={cevir('Bağlantıyı Kes')} ton="tehlike" onayIkonu={I.ban}
        onOnay={() => (kesOnay ? baglantiKes(kesOnay) : undefined)} onIptal={() => setKesOnay(null)} />
      <ConfirmDialog acik={silOnay !== null} baslik={cevir('Hedefi sil?')}
        mesaj={cevir('Hedef ve kayıtlı kimlik bilgileri silinir; Google erişimi iptal edilir. Drive’daki yedek dosyalarınız silinmez.')}
        onayMetni={cevir('Sil')} ton="tehlike" onayIkonu={I.cop}
        onOnay={() => (silOnay ? sil(silOnay) : undefined)} onIptal={() => setSilOnay(null)} />
    </div>
  )
}
