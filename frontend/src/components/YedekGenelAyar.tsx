import { useEffect, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { api, apiHata } from '@/lib/api'

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
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (AYAR_EN[tr] || ORTAK_EN[tr] || tr) : tr)

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
  'w-full px-3 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700 ' +
  'bg-white dark:bg-slate-900/40 text-slate-900 dark:text-slate-100 ' +
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

export default function YedekGenelAyar() {
  useTranslation() // dil re-render aboneligi
  const [a, setA] = useState<Ayar | null>(null)
  const [kaydediyor, setKaydediyor] = useState(false)
  const [testEdiyor, setTestEdiyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  useEffect(() => {
    api
      .get<Ayar>('/admin/backups/ayar')
      .then(r => setA({ ...r.data, uzak_parola: '' }))
      .catch(e => setHata(apiHata(e, cevir('Ayarlar alınamadı'))))
  }, [])

  function yaz<K extends keyof Ayar>(k: K, v: Ayar[K]) {
    setA(o => (o ? { ...o, [k]: v } : o))
  }

  async function kaydet() {
    if (!a) return
    setHata(null); setBasari(null); setKaydediyor(true)
    try {
      await api.put('/admin/backups/ayar', a)
      const { data } = await api.get<Ayar>('/admin/backups/ayar')
      setA({ ...data, uzak_parola: '' })
      setBasari(cevir('Ayarlar kaydedildi.'))
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
      const { data } = await api.post('/admin/backups/ayar/test', a)
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
    <div className="mb-5 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 flex items-center gap-2">
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Genel Yedek Ayarları')}</h3>
        <span className="ml-auto text-[11px] text-slate-500 dark:text-slate-400 tabular-nums">
          {cevir('Boş alan')}: <strong className={dusukAlan ? 'text-red-600 dark:text-red-400' : ''}>{a.bos_gb.toFixed(1)} GB</strong>
          {' · '}
          {cevir('Yedek deposu')}: <strong>{a.depo_gb.toFixed(1)} GB</strong>
        </span>
      </div>

      <div className="p-4 space-y-5">
        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {basari && <div className="px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

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
            <Salter acik={a.uzak_aktif} degisti={v => yaz('uzak_aktif', v)} etiketMetni={cevir('Uzak hedef (sistem geneli)')} />
            <div>
              <div className="text-sm font-medium text-slate-800 dark:text-slate-100">{cevir('Uzak hedef (sistem geneli)')}</div>
              <p className="text-xs text-slate-500 dark:text-slate-400">{cevir('Tüm domainlerin yedekleri bu hedefe yüklenir. Domain bazlı hedeflerden bağımsızdır.')}</p>
            </div>
          </div>

          {a.uzak_aktif && (
            <div className="pl-0 sm:pl-14 space-y-3">
              <div className="grid sm:grid-cols-4 gap-3">
                <div>
                  <label className={etiket} htmlFor="uzak-tip">{cevir('Tip')}</label>
                  <select id="uzak-tip" className={kutu} value={a.uzak_tip}
                    onChange={e => { yaz('uzak_tip', e.target.value); yaz('uzak_port', e.target.value === 'ftp' ? 21 : 22) }}>
                    <option value="sftp">SFTP</option>
                    <option value="ftp">FTP</option>
                  </select>
                </div>
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
              </div>
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

              <button type="button" onClick={test} disabled={testEdiyor}
                className="px-3 py-1.5 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700/40 disabled:opacity-50">
                {testEdiyor ? cevir('Test ediliyor…') : cevir('Bağlantıyı Test Et')}
              </button>
            </div>
          )}
        </div>

        <div className="pt-1">
          <button type="button" onClick={kaydet} disabled={kaydediyor}
            className="px-3.5 py-2 text-sm font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
            {kaydediyor ? cevir('Kaydediliyor…') : cevir('Kaydet')}
          </button>
        </div>
      </div>
    </div>
  )
}
