import { useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api, apiHata } from '@/lib/api'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

// Sistem geneli yedek ayarlari: ana salter + disk korumasi + GLOBAL uzak hedef.
// Eskiden otomatik yedegi topluca kapatmanin yolu yoktu ve uzak hedef YALNIZ domain
// bazliydi; yedekler kok diski doldurup paneli+siteleri dusurebiliyordu.

const AYAR_EN: Record<string, string> = {
  'Genel Yedek Ayarları': 'Global Backup Settings',
  'Otomatik yedekleme': 'Automatic backup',
  'Kapalıyken hiçbir domain için zamanlanmış yedek alınmaz.': 'When off, no scheduled backup runs for any domain.',
  'Açık': 'On',
  'Kapalı': 'Off',
  'Disk koruması': 'Disk protection',
  'En az boş alan (GB)': 'Minimum free space (GB)',
  'Bu eşiğin altına inince yedekleme durur ve kritik bildirim gönderilir.': 'Backups stop below this threshold and a critical notification is sent.',
  'Yedek deposu tavanı (GB)': 'Backup storage cap (GB)',
  '0 = sınırsız': '0 = unlimited',
  'Boş alan': 'Free space',
  'Yedek deposu': 'Backup storage',
  'Uzak hedef (sistem geneli)': 'Remote target (system-wide)',
  'Tüm domainlerin yedekleri bu hedefe yüklenir. Domain bazlı hedeflerden bağımsızdır.': 'Backups of all domains are uploaded here. Independent of per-domain targets.',
  'Sunucu': 'Host',
  'Port': 'Port',
  'Kullanıcı': 'User',
  'Parola': 'Password',
  'Değiştirmemek için boş bırakın': 'Leave blank to keep unchanged',
  'Uzak dizin': 'Remote directory',
  'Yükledikten sonra yerel kopyayı sil': 'Delete local copy after upload',
  'Disk büyümesini tamamen durdurur — yedek yalnızca uzak sunucuda kalır.': 'Stops disk growth entirely — the backup lives only on the remote server.',
  'Bağlantıyı Test Et': 'Test Connection',
  'Test ediliyor…': 'Testing…',
  'Bağlantı başarılı': 'Connection successful',
  'Kaydet': 'Save',
  'Kaydediliyor…': 'Saving…',
  'Ayarlar kaydedildi.': 'Settings saved.',
  'Ayarlar alınamadı': 'Could not load settings',
  'Ayarlar kaydedilemedi': 'Could not save settings',
  'Son yükleme': 'Last upload',
  'başarılı': 'successful',
  'hata': 'error',
  'Google Drive': 'Google Drive',
  'Google Drive (önce bağlanın)': 'Google Drive (connect first)',
  'Önce Google ile bağlanın': 'Connect with Google first',
  'Google Drive’ı seçmek için önce hesabınızı bağlayın.': 'To choose Google Drive, connect your account first.',
  'Google ile bağlan': 'Connect with Google',
  'Google hesabı': 'Google account',
  'Sistem yedekleri otomatik ve şifreli olarak Google Drive’a gönderilir. Müşterilerin elle aldığı yedekler gönderilmez.': 'System backups are sent to Google Drive automatically and encrypted. Backups customers take by hand are not sent.',
  'Google Drive hedefi kaydedildi. Yeni yedekler kendiliğinden gönderilecek.': 'Google Drive target saved. New backups will be sent automatically.',
  'Google Drive hedefi seçilmedi': 'No Google Drive account selected',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (AYAR_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type UzakAyar = { aktif: boolean; hedef_id: number; klasor: string; saklama_n: number; saklama_gun: number; bant_kbps: number }
type UzakDurum = { ayar: UzakAyar; hedefler: { id: number; ad: string; tur: string }[] }
type HedefTur = 'sftp' | 'ftp' | 'drive'

type Ayar = {
  aktif: boolean
  min_bos_gb: number
  max_depo_gb: number
  uzak_aktif: boolean
  uzak_tip: string
  uzak_host: string
  uzak_port: number
  uzak_kullanici: string
  uzak_parola?: string
  uzak_dizin: string
  uzak_yerel_sil: boolean
  son_yukleme?: string
  son_durum?: string
  son_hata?: string
  bos_gb: number
  depo_gb: number
}

const kutu =
  'w-full px-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-dark-600 ' +
  'bg-white dark:bg-dark-800/40 text-slate-900 dark:text-slate-100 ' +
  'focus:outline-none focus:ring-2 focus:ring-sky-500/40'
const etiket = 'block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1'

function Salter({ acik, degisti, etiketMetni }: { acik: boolean; degisti: (v: boolean) => void; etiketMetni: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={acik}
      aria-label={etiketMetni}
      onClick={() => degisti(!acik)}
      className={
        'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ' +
        'focus:outline-none focus:ring-2 focus:ring-sky-500/40 ' +
        (acik ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600')
      }
    >
      <span
        className={
          'inline-block h-4 w-4 transform rounded-full bg-white transition-transform ' +
          (acik ? 'translate-x-6' : 'translate-x-1')
        }
      />
    </button>
  )
}

export default function YedekGenelAyar({ onUzakDegisti, driveSayac = 0 }: { onUzakDegisti?: () => void; driveSayac?: number }) {
  useTranslation() // dil re-render aboneligi
  const [a, setA] = useState<Ayar | null>(null)
  const [kaydediyor, setKaydediyor] = useState(false)
  const [testEdiyor, setTestEdiyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  // Uzak hedef seçimi: SFTP / FTP / Google Drive tek yerde. Drive seçilince şifreleme ve
  // uzak kopya kendiliğinden açılır (sunucu anahtarı üretir); kullanıcıdan anahtar istenmez.
  const [uzakAcik, setUzakAcik] = useState(false)
  const [hedefTur, setHedefTur] = useState<HedefTur>('sftp')
  const [driveId, setDriveId] = useState(0)
  const [drives, setDrives] = useState<{ id: number; ad: string }[]>([])
  const [uzakVar, setUzakVar] = useState(false) // uzak kopya ucu cevap verdi mi

  // Google Drive kartında hesap bağlanınca/silinince seçenek listesi tazelenir (form değerlerine dokunmaz).
  useEffect(() => {
    if (driveSayac === 0) return
    api.get<UzakDurum>('/admin/backups/uzak-kopya').then(r => {
      const liste = r.data.hedefler.filter(h => h.tur === 'drive')
      setDrives(liste)
      setDriveId(id => (liste.some(h => h.id === id) ? id : liste[0]?.id || 0))
    }).catch(() => { /* seçenek listesi bir sonraki yüklemede gelir */ })
  }, [driveSayac])

  useEffect(() => {
    let iptal = false
    Promise.all([
      api.get<Ayar>('/admin/backups/ayar'),
      api.get<UzakDurum>('/admin/backups/uzak-kopya').then(r => r.data).catch(() => null),
    ])
      .then(([r, u]) => {
        if (iptal) return
        setA({ ...r.data, uzak_parola: '' })
        const driveAktif = !!u?.ayar.aktif
        const liste = (u?.hedefler || []).filter(h => h.tur === 'drive')
        setUzakVar(!!u)
        setDrives(liste)
        setDriveId(u?.ayar.hedef_id || liste[0]?.id || 0)
        setUzakAcik(r.data.uzak_aktif || driveAktif)
        setHedefTur(driveAktif ? 'drive' : r.data.uzak_tip === 'ftp' ? 'ftp' : 'sftp')
      })
      .catch(e => { if (iptal) return; setHata(apiHata(e, cevir('Ayarlar alınamadı'))) })
    return () => { iptal = true }
  }, [])

  function yaz<K extends keyof Ayar>(k: K, v: Ayar[K]) {
    setA(o => (o ? { ...o, [k]: v } : o))
  }

  async function kaydet() {
    if (!a) return
    setHata(null); setBasari(null); setKaydediyor(true)
    try {
      const drive = uzakAcik && hedefTur === 'drive'
      if (drive && !driveId) { setHata(cevir('Google Drive hedefi seçilmedi')); return }
      const govde = { ...a, uzak_aktif: uzakAcik && hedefTur !== 'drive', uzak_tip: hedefTur === 'drive' ? a.uzak_tip : hedefTur }
      // Güncel uzak kopya ayarı (saklama/klasör/bant başka kartta değişmiş olabilir): yalnız aktif+hedef değişir.
      const uzakAyarYaz = async (aktif: boolean) => {
        const { data: u } = await api.get<UzakDurum>('/admin/backups/uzak-kopya')
        if (aktif || u.ayar.aktif) {
          await api.put('/admin/backups/uzak-kopya/ayar', { ...u.ayar, aktif, hedef_id: aktif ? driveId : u.ayar.hedef_id })
        }
      }
      if (drive) await uzakAyarYaz(true)
      await api.put('/admin/backups/ayar', govde)
      if (!drive && uzakVar) await uzakAyarYaz(false)
      const { data } = await api.get<Ayar>('/admin/backups/ayar')
      setA({ ...data, uzak_parola: '' })
      setBasari(drive ? cevir('Google Drive hedefi kaydedildi. Yeni yedekler kendiliğinden gönderilecek.') : cevir('Ayarlar kaydedildi.'))
      onUzakDegisti?.()
    } catch (e) {
      setHata(apiHata(e, cevir('Ayarlar kaydedilemedi')))
    } finally {
      setKaydediyor(false)
    }
  }

  async function test() {
    if (!a) return
    setHata(null); setBasari(null); setTestEdiyor(true)
    try {
      const { data } = await api.post('/admin/backups/ayar/test', { ...a, uzak_tip: hedefTur === 'drive' ? a.uzak_tip : hedefTur })
      if (data.ok) setBasari(cevir('Bağlantı başarılı'))
      else setHata(String(data.hata || ''))
    } catch (e) {
      setHata(apiHata(e))
    } finally {
      setTestEdiyor(false)
    }
  }

  if (!a) return null

  const dusukAlan = a.min_bos_gb > 0 && a.bos_gb < a.min_bos_gb

  return (
    <div className="mb-5 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center gap-2">
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Genel Yedek Ayarları')}</h3>
        <span className="ml-auto text-[11px] text-slate-500 dark:text-slate-400 tabular-nums">
          {cevir('Boş alan')}: <strong className={dusukAlan ? 'text-red-600 dark:text-red-400' : ''}>{a.bos_gb.toFixed(1)} GB</strong>
          {' · '}
          {cevir('Yedek deposu')}: <strong>{a.depo_gb.toFixed(1)} GB</strong>
        </span>
      </div>

      <div className="p-4 space-y-5">
        {hata && <UyariKutusu ton="tehlike">{hata}</UyariKutusu>}
        {basari && <UyariKutusu ton="basari">{basari}</UyariKutusu>}

        {/* Ana salter */}
        <div className="flex items-start gap-3">
          <Salter acik={a.aktif} degisti={v => yaz('aktif', v)} etiketMetni={cevir('Otomatik yedekleme')} />
          <div>
            <div className="text-sm font-medium text-slate-800 dark:text-slate-100">
              {cevir('Otomatik yedekleme')} — <span className={a.aktif ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-500'}>{a.aktif ? cevir('Açık') : cevir('Kapalı')}</span>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Kapalıyken hiçbir domain için zamanlanmış yedek alınmaz.')}</p>
          </div>
        </div>

        {/* Disk korumasi */}
        <div>
          <div className="text-sm font-medium text-slate-800 dark:text-slate-100 mb-2">{cevir('Disk koruması')}</div>
          <div className="grid sm:grid-cols-2 gap-3">
            <div>
              <label className={etiket} htmlFor="min-bos">{cevir('En az boş alan (GB)')}</label>
              <input id="min-bos" type="number" min={0} className={kutu} value={a.min_bos_gb}
                onChange={e => yaz('min_bos_gb', Number(e.target.value))} />
              <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('Bu eşiğin altına inince yedekleme durur ve kritik bildirim gönderilir.')}</p>
            </div>
            <div>
              <label className={etiket} htmlFor="max-depo">{cevir('Yedek deposu tavanı (GB)')}</label>
              <input id="max-depo" type="number" min={0} className={kutu} value={a.max_depo_gb}
                onChange={e => yaz('max_depo_gb', Number(e.target.value))} />
              <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('0 = sınırsız')}</p>
            </div>
          </div>
        </div>

        {/* Global uzak hedef */}
        <div>
          <div className="flex items-start gap-3 mb-3">
            <Salter acik={uzakAcik} degisti={setUzakAcik} etiketMetni={cevir('Uzak hedef (sistem geneli)')} />
            <div>
              <div className="text-sm font-medium text-slate-800 dark:text-slate-100">{cevir('Uzak hedef (sistem geneli)')}</div>
              <p className="text-xs text-slate-500 dark:text-slate-400">{hedefTur === 'drive' ? cevir('Sistem yedekleri otomatik ve şifreli olarak Google Drive’a gönderilir. Müşterilerin elle aldığı yedekler gönderilmez.') : cevir('Tüm domainlerin yedekleri bu hedefe yüklenir. Domain bazlı hedeflerden bağımsızdır.')}</p>
            </div>
          </div>

          {uzakAcik && (
            <div className="pl-0 sm:pl-14 space-y-3">
              <div className="grid sm:grid-cols-4 gap-3">
                <div>
                  <label className={etiket} htmlFor="uzak-tip">{cevir('Tip')}</label>
                  <select id="uzak-tip" className={kutu} value={hedefTur}
                    onChange={e => {
                      const v = e.target.value as HedefTur
                      setHedefTur(v)
                      if (v !== 'drive') yaz('uzak_port', v === 'ftp' ? 21 : 22)
                    }}>
                    <option value="sftp">SFTP</option>
                    <option value="ftp">FTP</option>
                    {uzakVar && <option value="drive" disabled={drives.length === 0}>{drives.length === 0 ? cevir('Google Drive (önce bağlanın)') : cevir('Google Drive')}</option>}
                  </select>
                </div>
                {hedefTur === 'drive' && drives.length > 1 && (
                  <div className="sm:col-span-3">
                    <label className={etiket} htmlFor="uzak-drive">{cevir('Google hesabı')}</label>
                    <select id="uzak-drive" className={kutu} value={driveId} onChange={e => setDriveId(Number(e.target.value))}>
                      {drives.map(d => <option key={d.id} value={d.id}>{d.ad}</option>)}
                    </select>
                  </div>
                )}
                {hedefTur !== 'drive' && (
                <>
                <div className="sm:col-span-2">
                  <label className={etiket} htmlFor="uzak-host">{cevir('Sunucu')}</label>
                  <input id="uzak-host" className={kutu} value={a.uzak_host} placeholder="yedek.ornek.com"
                    onChange={e => yaz('uzak_host', e.target.value)} />
                </div>
                <div>
                  <label className={etiket} htmlFor="uzak-port">{cevir('Port')}</label>
                  <input id="uzak-port" type="number" min={1} max={65535} className={kutu} value={a.uzak_port}
                    onChange={e => yaz('uzak_port', Number(e.target.value))} />
                </div>
                </>
                )}
              </div>
              {uzakVar && drives.length === 0 && (
                <p className="text-xs text-amber-700 dark:text-amber-300">
                  {cevir('Google Drive’ı seçmek için önce hesabınızı bağlayın.')}{' '}
                  <a href="#google-drive" className="font-medium underline">{cevir('Önce Google ile bağlanın')}</a>
                </p>
              )}
              {hedefTur !== 'drive' && (
              <>
              <div className="grid sm:grid-cols-3 gap-3">
                <div>
                  <label className={etiket} htmlFor="uzak-kul">{cevir('Kullanıcı')}</label>
                  <input id="uzak-kul" className={kutu} value={a.uzak_kullanici} autoComplete="off"
                    onChange={e => yaz('uzak_kullanici', e.target.value)} />
                </div>
                <div>
                  <label className={etiket} htmlFor="uzak-par">{cevir('Parola')}</label>
                  <input id="uzak-par" type="password" className={kutu} value={a.uzak_parola || ''} autoComplete="new-password"
                    placeholder={cevir('Değiştirmemek için boş bırakın')}
                    onChange={e => yaz('uzak_parola', e.target.value)} />
                </div>
                <div>
                  <label className={etiket} htmlFor="uzak-dizin">{cevir('Uzak dizin')}</label>
                  <input id="uzak-dizin" className={kutu} value={a.uzak_dizin}
                    onChange={e => yaz('uzak_dizin', e.target.value)} />
                </div>
              </div>

              <div className="flex items-start gap-3">
                <Salter acik={a.uzak_yerel_sil} degisti={v => yaz('uzak_yerel_sil', v)} etiketMetni={cevir('Yükledikten sonra yerel kopyayı sil')} />
                <div>
                  <div className="text-sm text-slate-800 dark:text-slate-100">{cevir('Yükledikten sonra yerel kopyayı sil')}</div>
                  <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Disk büyümesini tamamen durdurur — yedek yalnızca uzak sunucuda kalır.')}</p>
                </div>
              </div>

              {a.son_yukleme && (
                <p className="text-xs text-slate-500 dark:text-slate-400">
                  {cevir('Son yükleme')}: {a.son_yukleme} — {a.son_durum === 'ok'
                    ? <span className="text-emerald-600 dark:text-emerald-400">{cevir('başarılı')}</span>
                    : <span className="text-red-600 dark:text-red-400">{cevir('hata')}{a.son_hata ? ': ' + a.son_hata : ''}</span>}
                </p>
              )}

              <Button variant="outlined" ikon={I.yenile} onClick={test} yukleniyor={testEdiyor}>
                {testEdiyor ? cevir('Test ediliyor…') : cevir('Bağlantıyı Test Et')}
              </Button>
              </>
              )}
            </div>
          )}
        </div>

        <div className="pt-1">
          <Button color="primary" ikon={I.disket} onClick={kaydet} yukleniyor={kaydediyor}>
            {kaydediyor ? cevir('Kaydediliyor…') : cevir('Kaydet')}
          </Button>
        </div>
      </div>
    </div>
  )
}
