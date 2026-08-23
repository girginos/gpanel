import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Gorev = {
  idx: number
  dakika: string
  saat: string
  gun: string
  ay: string
  hafta: string
  komut: string
  yorum?: string
  etkin: boolean
  tip?: string        // "komut" | "url" | "php"
  php_surum?: string
  bildirim?: string   // "bilgi" | "hata" | "her" | "yok"
}

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }

type ListResp = { sistem_kullanici: string; toplam: number; gorevler: Gorev[] }

const PHP_SURUMLER = ['8.3', '8.2', '8.1', '8.0', '7.4']

const ON_AYARLAR: Array<{ etiket: string; secim: { dakika: string; saat: string; gun: string; ay: string; hafta: string } }> = [
  { etiket: 'Her dakika',        secim: { dakika: '*',  saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her saat',          secim: { dakika: '0',  saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her gün gece 03:00', secim: { dakika: '0',  saat: '3', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Pazartesi 09:00',   secim: { dakika: '0',  saat: '9', gun: '*', ay: '*', hafta: '1' } },
  { etiket: 'Her 5 dakika',      secim: { dakika: '*/5', saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her 15 dakika',     secim: { dakika: '*/15', saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Ayın 1\'i 00:00',   secim: { dakika: '0',  saat: '0', gun: '1', ay: '*', hafta: '*' } },
]


const CRON_EN: Record<string, string> = {
  "(kapalıysa görev çalışmaz ama saklanır)": "(if off, the task won't run but is kept)",
  "(çıktı yok)": "(no output)",
  "Argümanlar": "Arguments",
  "Bir komut çalıştır": "Run a command",
  "Görev tipi": "Task type",
  "Görevi Düzenle": "Edit Task",
  "Görevi şimdi elle çalıştır": "Run the task manually now",
  "Gün": "Day",
  "Henüz görev yok. Yukarıdan ekleyin.": "No tasks yet. Add one above.",
  "Her gün gece 03:00": "Every day at 03:00",
  "Komut / Açıklama": "Command / Description",
  "PHP dosyası çalıştır": "Run a PHP file",
  "Yalnızca hatalar": "Errors only",
  "Yeni Zamanlanmış Görev": "New Cron Job",
  "curl ile getirilir (300sn zaman aşımı, çıktı atılır).": "Fetched via curl (300s timeout, output discarded).",
  "örn. Her gece yedek scripti": "e.g. Nightly backup script",
  "⚠ Görev hata verdi": "⚠ Task returned an error",
  "✓ Görev çalıştı": "✓ Task ran",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CRON_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainCronPage() {
  useTranslation() // dil re-render aboneligi
  const { onay, bilgi } = useDialog()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [gorevler, setGorevler] = useState<Gorev[]>([])
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  // null=kapalı, 'yeni'=ekleme, Gorev=düzenleme
  const [modalGorev, setModalGorev] = useState<Gorev | 'yeni' | null>(null)
  const [calisan, setCalisan] = useState<number | null>(null)

  function yukle() {
    if (!id) return
    setYukleniyor(true); setHata(null)
    api.get<ListResp>(`/domains/${id}/cron`)
      .then(r => setGorevler(r.data.gorevler))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYukleniyor(false))
  }

  useEffect(() => {
    if (id) api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(hataYakala(cevir("Alan adı bilgisi alınamadı")))
    yukle()
  }, [id])

  async function calistir(g: Gorev) {
    setCalisan(g.idx)
    try {
      const { data } = await api.post(`/domains/${id}/cron/${g.idx}/calistir`)
      const baslik = data.ok ? '✓ Görev çalıştı' : cevir("⚠ Görev hata verdi")
      const cikti = (data.cikti || '').trim() || cevir("(çıktı yok)")
      const hataStr = data.hata ? `\n\nHata: ${data.hata}` : ''
      await bilgi({ baslik, mesaj: `$ ${data.komut}\n\n${cikti}${hataStr}` })
    } catch (e) {
      (await bilgi({ baslik: 'Bilgi', mesaj: apiHata(e, cevir("Çalıştırma başarısız")) }))
    } finally {
      setCalisan(null)
    }
  }

  async function sil(g: Gorev) {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `"${g.komut.slice(0, 60)}..." görevi silinsin mi?`, tehlike: true }))) return
    try {
      await api.delete(`/domains/${id}/cron/${g.idx}`)
      yukle()
    } catch (e) {
      (await bilgi({ baslik: 'Bilgi', mesaj: apiHata(e, cevir("Silme başarısız")) }))
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1300px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Zamanlanmış Görevler") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Zamanlanmış Görevler")}</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}
          <span className="font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">/var/spool/cron/{domain.sistem_kullanici}</span>
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <button
          onClick={() => setModalGorev('yeni')}
          className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md shadow-sm transition"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
          </svg>
          {cevir(cevir("Görev Ekle"))}
        </button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md transition"><span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} /> {cevir("Yenile")}</span></button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{gorevler.length} görev</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
        {yukleniyor ? (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
        ) : gorevler.length === 0 ? (
          <div className="py-16 text-center">
            <div className="w-14 h-14 mx-auto rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center mb-3">
              <svg className="w-7 h-7 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500">{cevir("Henüz görev yok. Yukarıdan ekleyin.")}</p>
          </div>
        ) : (
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
              <tr>
                <th className={T.baslik}>Zamanlama</th>
                <th className={T.baslik}>{cevir("Komut / Açıklama")}</th>
                <th className={`${T.baslik} text-right`}>{cevir("İşlem")}</th>
              </tr>
            </thead>
            <tbody className={T.govde}>
              {gorevler.map((g) => (
                <tr key={g.idx} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800 transition ${!g.etkin ? 'opacity-55' : ''}`}>
                  <td className={T.hucre} data-etiket="Zamanlama">
                    <span className="font-mono text-sm whitespace-nowrap">{g.dakika} {g.saat} {g.gun} {g.ay} {g.hafta}</span>
                  </td>
                  <td className={T.hucreBaslik}>
                    <div className="flex items-center gap-2">
                      {!g.etkin && <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300">{cevir("Pasif")}</span>}
                      {g.tip && g.tip !== 'komut' && <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-brand-100 dark:bg-brand-900/40 text-brand-700 dark:text-brand-300">{g.tip === 'php' ? `PHP ${g.php_surum || ''}` : g.tip}</span>}
                      <span className="font-mono text-slate-800 dark:text-slate-200 break-all lg:break-normal lg:truncate lg:max-w-md" title={g.komut}>{g.komut}</span>
                    </div>
                    {g.yorum && <div className="text-xs font-normal text-slate-500 dark:text-slate-500 mt-0.5">{g.yorum}</div>}
                  </td>
                  <td className={`${T.hucreAksiyon} lg:text-right lg:space-x-1`}>
                    <button onClick={() => calistir(g)} disabled={calisan === g.idx} className="text-sm text-emerald-600 dark:text-emerald-400 hover:text-emerald-700 dark:hover:text-emerald-300 px-2 py-1 rounded hover:bg-emerald-50 dark:hover:bg-emerald-900/30 transition disabled:opacity-50" title={cevir("Görevi şimdi elle çalıştır")}>{calisan === g.idx ? 'Çalışıyor…' : cevir("Çalıştır")}</button>
                    <button onClick={() => setModalGorev(g)} className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 px-2 py-1 rounded hover:bg-brand-50 dark:hover:bg-brand-900/30 transition">{cevir("Düzenle")}</button>
                    <button onClick={() => sil(g)} className="text-sm text-red-600 dark:text-red-400 hover:text-red-700 dark:text-red-300 px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 transition">{cevir("Sil")}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        )}
      </div>

      {modalGorev !== null && (
        <CronModal
          gorev={modalGorev}
          domainId={Number(id)}
          onKapat={() => setModalGorev(null)}
          onKaydedildi={() => { setModalGorev(null); yukle() }}
        />
      )}
    </div>
  )
}

// komuttan tip'e göre ham alanları geri çıkar (düzenleme için best-effort).
function komutParse(g: Gorev): { url: string; script: string; args: string; komut: string } {
  const komut = g.komut
  if (g.tip === 'url') {
    const m = komut.match(/'([^']+)'/)
    return { url: m ? m[1] : '', script: '', args: '', komut }
  }
  if (g.tip === 'php') {
    const m = komut.match(/-q '([^']+)'(.*)$/)
    return { url: '', script: m ? m[1] : '', args: m ? m[2].trim() : '', komut }
  }
  return { url: '', script: '', args: '', komut }
}

function CronModal({ gorev, domainId, onKapat, onKaydedildi }: {
  gorev: Gorev | 'yeni'; domainId: number; onKapat: () => void; onKaydedildi: () => void
}) {
  const yeni = gorev === 'yeni'
  const mevcut = yeni ? null : (gorev as Gorev)
  const parsed = mevcut ? komutParse(mevcut) : { url: '', script: '', args: '', komut: '' }

  const [etkin, setEtkin] = useState(mevcut ? mevcut.etkin : true)
  const [tip, setTip] = useState<string>(mevcut?.tip || 'komut')
  const [dakika, setDakika] = useState(mevcut?.dakika || '0')
  const [saat, setSaat] = useState(mevcut?.saat || '3')
  const [gun, setGun] = useState(mevcut?.gun || '*')
  const [ay, setAy] = useState(mevcut?.ay || '*')
  const [hafta, setHafta] = useState(mevcut?.hafta || '*')
  const [komut, setKomut] = useState(parsed.komut && tip === 'komut' ? parsed.komut : (mevcut && tip === 'komut' ? mevcut.komut : ''))
  const [url, setUrl] = useState(parsed.url)
  const [script, setScript] = useState(parsed.script)
  const [args, setArgs] = useState(parsed.args)
  const [phpSurum, setPhpSurum] = useState(mevcut?.php_surum || '8.3')
  const [bildirim, setBildirim] = useState(mevcut?.bildirim || 'bilgi')
  const [yorum, setYorum] = useState(mevcut?.yorum || '')
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  function uygulaPreset(p: typeof ON_AYARLAR[number]['secim']) {
    setDakika(p.dakika); setSaat(p.saat); setGun(p.gun); setAy(p.ay); setHafta(p.hafta)
  }

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      const body: Record<string, unknown> = {
        dakika, saat, gun, ay, hafta, etkin, tip, bildirim, yorum: yorum.trim(),
      }
      if (tip === 'komut') body.komut = komut.trim()
      else if (tip === 'url') body.url = url.trim()
      else if (tip === 'php') { body.script = script.trim(); body.args = args.trim(); body.php_surum = phpSurum }

      if (yeni) await api.post(`/domains/${domainId}/cron`, body)
      else await api.put(`/domains/${domainId}/cron/${(gorev as Gorev).idx}`, body)
      onKaydedildi()
    } catch (e) {
      setHata(apiHata(e, cevir("Kaydetme başarısız")))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={yeni ? 'Yeni Zamanlanmış Görev' : cevir("Görevi Düzenle")} onKapat={onKapat} genislik="lg">
      <form onSubmit={gonder} className="space-y-4">
        {/* Etkin */}
        <label className="flex items-center gap-2.5 cursor-pointer select-none">
          <input type="checkbox" checked={etkin} onChange={e => setEtkin(e.target.checked)} className="h-4 w-4 accent-brand-600" />
          <span className="text-sm font-medium text-slate-700 dark:text-slate-300">Etkin <span className="font-normal text-slate-400 dark:text-slate-500">{cevir("(kapalıysa görev çalışmaz ama saklanır)")}</span></span>
        </label>

        {/* Görev tipi */}
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{cevir("Görev tipi")}</label>
          <div className="flex flex-wrap gap-4">
            {[['komut', cevir("Bir komut çalıştır")], ['url', 'Bir URL getir'], ['php', cevir("PHP dosyası çalıştır")]].map(([v, l]) => (
              <label key={v} className="flex items-center gap-1.5 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
                <input type="radio" name="tip" checked={tip === v} onChange={() => setTip(v)} className="accent-brand-600" />
                {l}
              </label>
            ))}
          </div>
        </div>

        {/* Tipe özel alanlar */}
        {tip === 'komut' && (
          <div>
            <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Komut</label>
            <input value={komut} onChange={e => setKomut(e.target.value)} required placeholder="/usr/bin/php /home/c_user/public_html/cron.php"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </div>
        )}
        {tip === 'url' && (
          <div>
            <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">URL</label>
            <input value={url} onChange={e => setUrl(e.target.value)} required placeholder="https://example.com/cron.php"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-500">{cevir("curl ile getirilir (300sn zaman aşımı, çıktı atılır).")}</p>
          </div>
        )}
        {tip === 'php' && (
          <div className="space-y-3">
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3">
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">PHP dosya yolu</label>
                <input value={script} onChange={e => setScript(e.target.value)} required placeholder="/home/c_user/public_html/cron.php"
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{cevir("PHP sürümü")}</label>
                <select value={phpSurum} onChange={e => setPhpSurum(e.target.value)}
                  className="px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
                  {PHP_SURUMLER.map(s => <option key={s} value={s}>PHP {s}</option>)}
                </select>
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{cevir("Argümanlar")} <span className="font-normal text-slate-400 dark:text-slate-500">(opsiyonel)</span></label>
              <input value={args} onChange={e => setArgs(e.target.value)} placeholder="--verbose görev=1"
                className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </div>
          </div>
        )}

        {/* Zamanlama */}
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Zamanlama</label>
          <div className="flex flex-wrap gap-1.5 mb-2">
            {ON_AYARLAR.map(p => (
              <button key={p.etiket} type="button" onClick={() => uygulaPreset(p.secim)}
                className="px-2.5 py-1 text-xs font-medium bg-slate-100 dark:bg-slate-700/60 text-slate-700 dark:text-slate-200 border border-slate-200 dark:border-slate-600 hover:bg-brand-100 dark:hover:bg-brand-900/40 hover:text-brand-700 dark:hover:text-brand-300 hover:border-brand-300 dark:hover:border-brand-700 rounded-md transition">
                {p.etiket}
              </button>
            ))}
          </div>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-2">
            <Alan etiket="Dakika" value={dakika} onChange={setDakika} />
            <Alan etiket="Saat"   value={saat}   onChange={setSaat} />
            <Alan etiket={cevir("Gün")}    value={gun}    onChange={setGun} />
            <Alan etiket="Ay"     value={ay}     onChange={setAy} />
            <Alan etiket="Hafta"  value={hafta}  onChange={setHafta} />
          </div>
          <p className="mt-1 text-xs text-slate-500 dark:text-slate-500">{cevir("Cron biçimi —")} <code className="font-mono">*</code> her zaman, <code className="font-mono">*/5</code> her 5'te bir, <code className="font-mono">0,15,30</code> liste, <code className="font-mono">9-17</code> aralık.</p>
        </div>

        {/* Bildirim */}
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Bildirim</label>
          <div className="flex flex-wrap gap-4">
            {[['bilgi', 'Bilgilendirme'], ['hata', cevir("Yalnızca hatalar")], ['her', 'Her zaman'], ['yok', cevir("Gönderme")]].map(([v, l]) => (
              <label key={v} className="flex items-center gap-1.5 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
                <input type="radio" name="bildirim" checked={bildirim === v} onChange={() => setBildirim(v)} className="accent-brand-600" />
                {l}
              </label>
            ))}
          </div>
        </div>

        {/* Açıklama */}
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{cevir("Açıklama")} <span className="font-normal text-slate-400 dark:text-slate-500">(opsiyonel)</span></label>
          <input value={yorum} onChange={e => setYorum(e.target.value)} placeholder={cevir("örn. Her gece yedek scripti")}
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-md text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
        </div>

        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onKapat} disabled={isleniyor} className="px-4 py-2 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 rounded-md text-sm">{cevir("İptal")}</button>
          <button type="submit" disabled={isleniyor} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
            {isleniyor ? 'Kaydediliyor…' : (yeni ? 'Ekle' : 'Kaydet')}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function Alan({ etiket, value, onChange }: { etiket: string; value: string; onChange: (v: string) => void }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input
        type="text"
        value={value}
        onChange={e => onChange(e.target.value)}
        className="w-full px-2 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
      />
    </div>
  )
}
