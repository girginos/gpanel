import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useRol } from '@/lib/rol'

type Row = {
  id?: number
  ad: string
  tip: string
  deger: string
  ttl: number
  oncelik: number
  sira: number
  aktif: boolean
}
type Meta = {
  soa_refresh: number
  soa_retry: number
  soa_expire: number
  soa_minimum: number
  soa_ttl: number
  dkim_selector: string
  dkim_aktif: boolean
}

const TIPLER = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR']


const DNSTPL_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "Ad (alt-ad)": "Name (sub-name)",
  "Tip": "Type",
  "Yer tutucular:": "Placeholders:",
  "alan adı": "domain",
  "sunucu IP": "server IP",
  "DKIM seçici": "DKIM selector",
  "otomatik üretilen DKIM public key (TXT)": "auto-generated DKIM public key (TXT)",
  "Yeni bir domain eklendiğinde ve": "When a new domain is added and",
  "butonuna basıldığında bu şablon uygulanır.": "is pressed, this template is applied.",
  "Değişiklikleriniz anında geçerli olur.": "Your changes take effect immediately.",
  "Şablon kaydedildi. Yeni domainler ve": "Template saved. New domains and",
  "bu şablonu kullanır.": "use this template.",
  "Şablonda kayıt yok —": "No records in the template —",
  "ile başlayın.": "to get started.",
  "SOA & DKIM Parametreleri": "SOA & DKIM Parameters",
  "DKIM anahtar üretimini etkinleştir": "Enable DKIM key generation",
  "DKIM açıkken şablonda": "When DKIM is on, the template",
  "içeren bir TXT kaydı olmalıdır (ör. ad:": "must contain a TXT record with (e.g. name:",
  "Her domain için 2048-bit RSA anahtar çifti otomatik üretilir; public key DNS'e yazılır, private key OpenDKIM kuruluysa mail sunucusuyla eşlenir.": "A 2048-bit RSA key pair is auto-generated for each domain; the public key is written to DNS, and the private key is paired with the mail server if OpenDKIM is installed.",
  "Kaydediliyor…": "Saving…",
  "Geri Al": "Undo",
  "DKIM Seçici": "DKIM Selector",
  "DNS Şablonu": "DNS Template",
  "Kayıt Ekle": "Add Record",
  "Merkezi DNS Şablonu": "Central DNS Template",
  "DNS Şablonları": "DNS Templates",
  "Yalnızca sizin hosting hesaplarınıza uygulanır; SOA ve DKIM ayarlarını sunucu yöneticisi yönetir.": "Applies only to your hosting accounts; SOA and DKIM settings are managed by the server administrator.",
  "Satırı sil": "Delete row",
  "Varsayılan Şablonu Uygula": "Apply Default Template",
  "Şablon kaydedilemedi": "Failed to save template",
  "Şablonu Kaydet": "Save Template",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DNSTPL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DNSSablonuPage() {
  useTranslation() // dil re-render aboneligi
  // SOA/DKIM (meta) SUNUCU GENELİ tek ayardır — yalnız yönetici görür ve kaydeder.
  // Bayi yalnız kendi kayıt satırlarını yönetir; sunucu ona meta döndürmez, bayinin
  // gönderdiği meta yok sayılır (GL-01 / BAYI-UI-01).
  const yonetici = useRol() === 'admin'
  const [rows, setRows] = useState<Row[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [yuk, setYuk] = useState(true)
  const [kaydediyor, setKaydediyor] = useState(false)
  const toast = useToast()

  function yukle() {
    setYuk(true)
    api.get<{ kayitlar: Row[]; meta?: Meta }>('/dns-template')
      .then(r => { setRows(r.data.kayitlar || []); setMeta(r.data.meta ?? null) })
      .catch(e => toast.hata(cevir("İşlem başarısız"), apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  function setRow(i: number, patch: Partial<Row>) {
    setRows(rs => rs.map((r, idx) => idx === i ? { ...r, ...patch } : r))
  }
  function satirEkle() {
    setRows(rs => [...rs, { ad: '@', tip: 'A', deger: '{IP}', ttl: 3600, oncelik: 0, sira: (rs.length + 1) * 10, aktif: true }])
  }
  function satirSil(i: number) {
    setRows(rs => rs.filter((_, idx) => idx !== i))
  }

  async function kaydet() {
    if (yonetici && !meta) return
    setKaydediyor(true)
    try {
      await api.put('/dns-template', yonetici ? { kayitlar: rows, meta } : { kayitlar: rows })
      toast.basari(cevir("Kaydedildi"), cevir("Şablon kaydedildi. Yeni domainler ve") + ' “' + cevir("Varsayılan Şablonu Uygula") + '” ' + cevir("bu şablonu kullanır."))
      yukle()
    } catch (e) {
      toast.hata(cevir("İşlem başarısız"), apiHata(e, cevir("Şablon kaydedilemedi")))
    }
    finally { setKaydediyor(false) }
  }

  const inp = 'w-full px-2.5 py-1.5 bg-white dark:bg-dark-800 border border-slate-300 dark:border-slate-600 rounded-lg text-sm text-slate-800 dark:text-slate-100 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none'

  return (
    <div className="px-6 md:px-8 py-6">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' }, { etiket: cevir("DNS Şablonu") }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir(yonetici ? "Merkezi DNS Şablonu" : "DNS Şablonları")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        {cevir("Yeni bir domain eklendiğinde ve")} <span className="font-medium">{cevir("Varsayılan Şablonu Uygula")}</span> {cevir("butonuna basıldığında bu şablon uygulanır.")}
        {' '}{cevir("Değişiklikleriniz anında geçerli olur.")}
        {!yonetici && <>{' '}{cevir("Yalnızca sizin hosting hesaplarınıza uygulanır; SOA ve DKIM ayarlarını sunucu yöneticisi yönetir.")}</>}
      </p>

      <UyariKutusu ton="bilgi" className="mb-4">
        <p className="text-xs">
          <strong>{cevir("Yer tutucular:")}</strong>{' '}
          <code className="font-mono">{'{DOMAIN}'}</code> {cevir("alan adı")} ·{' '}
          <code className="font-mono">{'{IP}'}</code> {cevir("sunucu IP")} ·{' '}
          <code className="font-mono">{'{SELECTOR}'}</code> {cevir("DKIM seçici")} ·{' '}
          <code className="font-mono">{'{DKIM}'}</code> {cevir("otomatik üretilen DKIM public key (TXT)")}
        </p>
      </UyariKutusu>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : (
        <>
          {/* Kayıt satırları */}
          <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden mb-5">
            <div className="lg:overflow-x-auto">
              <table className={T.tablo}>
                <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
                  <tr>
                    <th className={`${T.baslik} w-40`}>{cevir("Ad (alt-ad)")}</th>
                    <th className={`${T.baslik} w-28`}>{cevir("Tip")}</th>
                    <th className={T.baslik}>{cevir("Değer")}</th>
                    <th className={`${T.baslik} w-24`}>TTL</th>
                    <th className={`${T.baslik} w-24`}>{cevir("Öncelik")}</th>
                    <th className={`${T.baslik} text-center w-20`}>{cevir("Aktif")}</th>
                    <th className={`${T.baslik} w-12`}></th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {rows.map((r, i) => (
                    <tr key={i} className={`${T.satir} lg:last:border-b-0 lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700/60`}>
                      {/* Birincil hücre: kaydın adı — mobilde kart başlığı olur, etiket istemez */}
                      <td className={T.hucreBaslik}><input value={r.ad} onChange={e => setRow(i, { ad: e.target.value })} className={inp + ' font-mono'} /></td>
                      <td className={T.hucre} data-etiket={cevir("Tip")}>
                        <span className="block flex-1 min-w-0">
                          <select value={r.tip} onChange={e => setRow(i, { tip: e.target.value })} className={inp + ' font-mono'}>
                            {TIPLER.map(t => <option key={t} value={t}>{t}</option>)}
                          </select>
                        </span>
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Değer")}>
                        <span className="block flex-1 min-w-0"><input value={r.deger} onChange={e => setRow(i, { deger: e.target.value })} className={inp + ' font-mono'} /></span>
                      </td>
                      <td className={T.hucre} data-etiket="TTL">
                        <span className="block flex-1 min-w-0"><input type="number" min={60} value={r.ttl} onChange={e => setRow(i, { ttl: parseInt(e.target.value) || 3600 })} className={inp + ' font-mono'} /></span>
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Öncelik")}>
                        {/* Tek flex öğesi: koşullu içerik ortak bir span'e sarıldı */}
                        <span className="block flex-1 min-w-0">
                          {(r.tip === 'MX' || r.tip === 'SRV')
                            ? <input type="number" min={0} value={r.oncelik} onChange={e => setRow(i, { oncelik: parseInt(e.target.value) || 0 })} className={inp + ' font-mono'} />
                            : <span className="text-slate-300 dark:text-slate-600 text-sm pl-2">—</span>}
                        </span>
                      </td>
                      <td className={`${T.hucre} lg:text-center`} data-etiket={cevir("Aktif")}>
                        <input type="checkbox" checked={r.aktif} onChange={e => setRow(i, { aktif: e.target.checked })} className="cursor-pointer w-4 h-4 accent-brand-600" />
                      </td>
                      <td className={`${T.hucreAksiyon} lg:text-center`}>
                        <Button color="error" variant="flat" isIcon ikon={I.cop} onClick={() => satirSil(i)} title={cevir("Satırı sil")} aria-label={cevir("Satırı sil")} className="size-8" />
                      </td>
                    </tr>
                  ))}
                  {/* Boş durum da tek/son satır olduğu için masaüstünde alt çizgi çizmemeli (eski divide-y davranışı) */}
                  {rows.length === 0 && (
                    <tr className={`${T.satir} lg:last:border-b-0`}><td colSpan={7} className={T.hucreDurum}>{cevir("Şablonda kayıt yok —")} {cevir("Kayıt Ekle")} {cevir("ile başlayın.")}</td></tr>
                  )}
                </tbody>
              </table>
            </div>
            <div className="py-2.5 lg:px-3 lg:border-t lg:border-slate-100 dark:lg:border-dark-600">
              <Button variant="outlined" boyut="sm" ikon={I.arti} onClick={satirEkle}>
                {cevir("Kayıt Ekle")}
              </Button>
            </div>
          </div>

          {/* Meta: SOA + DKIM — sunucu geneli, YALNIZ yönetici */}
          {yonetici && meta && (
            <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-5">
              <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("SOA & DKIM Parametreleri")}</h2>
              <div className="grid grid-cols-2 md:grid-cols-5 gap-3 mb-4">
                {(['soa_refresh', 'soa_retry', 'soa_expire', 'soa_minimum', 'soa_ttl'] as const).map(f => (
                  <label key={f} className="block">
                    <span className="block text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">{f.replace('soa_', '')} (sn)</span>
                    <input type="number" min={0} value={meta[f]} onChange={e => setMeta({ ...meta, [f]: parseInt(e.target.value) || 0 })} className={inp + ' font-mono'} />
                  </label>
                ))}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3 items-end">
                <label className="block">
                  <span className="block text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">{cevir("DKIM Seçici")}</span>
                  <input value={meta.dkim_selector} onChange={e => setMeta({ ...meta, dkim_selector: e.target.value })} className={inp + ' font-mono'} placeholder="default" />
                </label>
                <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer pb-2">
                  <input type="checkbox" checked={meta.dkim_aktif} onChange={e => setMeta({ ...meta, dkim_aktif: e.target.checked })} className="w-4 h-4 accent-brand-600" />
                  {cevir("DKIM anahtar üretimini etkinleştir")}
                </label>
              </div>
              <p className="text-[11px] text-slate-500 dark:text-slate-500 mt-3">
                {cevir("DKIM açıkken şablonda")} <code className="font-mono">{'{DKIM}'}</code> {cevir("içeren bir TXT kaydı olmalıdır (ör. ad:")} <code className="font-mono">{'{SELECTOR}._domainkey'}</code>).
                {' '}{cevir("Her domain için 2048-bit RSA anahtar çifti otomatik üretilir; public key DNS'e yazılır, private key OpenDKIM kuruluysa mail sunucusuyla eşlenir.")}
              </p>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <Button color="primary" onClick={kaydet} yukleniyor={kaydediyor} ikon={I.disket}>
              {kaydediyor ? cevir("Kaydediliyor…") : cevir("Şablonu Kaydet")}
            </Button>
            <Button variant="outlined" onClick={yukle} ikon={I.geriAl}>{cevir("Geri Al")}</Button>
          </div>
        </>
      )}
    </div>
  )
}
