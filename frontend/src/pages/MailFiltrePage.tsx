import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'

type Filtre = { whitelist: string; blacklist: string; ip_blocklist: string; asn_blocklist: string }
type AsnCozum = { asn: string; prefiks_sayisi: number; kaynak: string; uyari?: string }
type PutYanit = { durum: string; cozunum: AsnCozum[] | null; toplam_cidr: number; uyari?: string }

const BOS: Filtre = { whitelist: '', blacklist: '', ip_blocklist: '', asn_blocklist: '' }


const MFILT_EN: Record<string, string> = {
  "Bazı ASN girdileri çözülemedi (aşağıya bakın). Diğer kurallar uygulandı.": "Some ASN entries could not be resolved (see below). Other rules were applied.",
  "Filtreler yüklenemedi (mail eklentisi aktif mi?)": "Failed to load filters (is the mail add-on active?)",
  "Gönderen Filtreleri (e-posta / domain)": "Sender Filters (email / domain)",
  "Her satıra bir e-posta veya domain. Beyaz liste her zaman kabul edilir, kara liste reddedilir.": "One email or domain per line. The allowlist is always accepted, the blocklist is rejected.",
  "IP / CIDR ağ engelle": "Block IP / CIDR network",
  "IP ve ASN Engelleme (bağlanan istemci)": "IP and ASN Blocking (connecting client)",
  "kısmi": "partial",
  "çözülemedi": "could not resolve",
  "önbellek": "cache",
  "⚠ önbellekten": "⚠ from cache",
  "✓ canlı": "✓ live",
  "Türkçe": "English",
  "Buradaki IP/ağlardan gelen tüm SMTP bağlantıları reddedilir (kimlik doğrulamış kullanıcılar hariç).": "All SMTP connections from the IPs/networks here are rejected (except authenticated users).",
  "Gönderen (e-posta/domain) ve bağlanan istemci (IP, CIDR ağ veya tüm ASN) bazında kabul/engelleme kuralları.": "Accept/block rules based on sender (email/domain) and connecting client (IP, CIDR network, or entire ASN).",
  "IP/ağ engelleniyor.": "IPs/networks blocked.",
  "Kimlik doğrulamış kullanıcılar ve yerel ağ bu kurallardan": "Authenticated users and the local network are",
  "Toplam": "Total",
  "duyurduğu tüm IP blokları": "all IP blocks it announces",
  "otomatik çözülüp engellenir.": "are automatically resolved and blocked.",
  "✓ Filtreler kaydedildi ve Postfix'e uygulandı. Toplam {0} IP/ağ engelleniyor.": "✓ Filters saved and applied to Postfix. A total of {0} IPs/networks are blocked.",
  "Kaydedilemedi": "Failed to save",
  "ASN girdikten sonra o otonom sistemin": "After entering an ASN,",
  "etkilenmez": "not affected",
  "Beyaz liste (kabul)": "Allowlist (accept)",
  "Kara liste (reddet)": "Blocklist (reject)",
  "Tek IP": "Single IP",
  "veya ağ": "or network",
  "Satır başına bir tane.": "One per line.",
  "ASN engelle": "Block ASN",
  "Otonom sistem numarası": "Autonomous system number",
  "O ASN'nin tüm IP blokları çözülüp engellenir.": "All IP blocks of that ASN are resolved and blocked.",
  "prefiks": "prefixes",
  "Uygulanıyor…": "Applying…",
  "Kaydet ve Uygula": "Save and Apply",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MFILT_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function MailFiltrePage() {
  useTranslation() // dil re-render aboneligi
  const [f, setF] = useState<Filtre>(BOS)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [cozunum, setCozunum] = useState<AsnCozum[]>([])
  const [toplamCidr, setToplamCidr] = useState<number | null>(null)

  useEffect(() => {
    api.get<Filtre>('/eklenti/mail/genel/filtre')
      .then(r => setF({ ...BOS, ...r.data }))
      .catch(e => setHata(apiHata(e, cevir("Filtreler yüklenemedi (mail eklentisi aktif mi?)"))))
      .finally(() => setYukleniyor(false))
  }, [])

  async function kaydet() {
    setKaydediliyor(true); setHata(null); setBildirim(null)
    try {
      const r = await api.put<PutYanit>('/eklenti/mail/genel/filtre', f)
      setCozunum(r.data.cozunum ?? [])
      setToplamCidr(r.data.toplam_cidr)
      if (r.data.durum === cevir("kısmi")) {
        setHata(cevir("Bazı ASN girdileri çözülemedi (aşağıya bakın). Diğer kurallar uygulandı."))
      } else {
        setBildirim(cevirT(cevir("✓ Filtreler kaydedildi ve Postfix'e uygulandı. Toplam {0} IP/ağ engelleniyor."), r.data.toplam_cidr))
      }
    } catch (e) { setHata(apiHata(e, cevir("Kaydedilemedi"))) }
    finally { setKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm'
  const ta = 'w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm font-mono resize-y focus:outline-none focus:ring-2'

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
        {cevir("Gönderen (e-posta/domain) ve bağlanan istemci (IP, CIDR ağ veya tüm ASN) bazında kabul/engelleme kuralları.")}
        {cevir("ASN girdikten sonra o otonom sistemin")} <span className="font-medium">{cevir("duyurduğu tüm IP blokları")}</span> {cevir("otomatik çözülüp engellenir.")}
        {cevir("Kimlik doğrulamış kullanıcılar ve yerel ağ bu kurallardan")} <span className="font-medium">{cevir("etkilenmez")}</span>.
      </p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div> : (
        <div className="space-y-5">
          {/* Gönderen filtreleri */}
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Gönderen Filtreleri (e-posta / domain)")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">{cevir("Her satıra bir e-posta veya domain. Beyaz liste her zaman kabul edilir, kara liste reddedilir.")}</p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-emerald-700 dark:text-emerald-400 mb-1.5">{cevir("Beyaz liste (kabul)")}</label>
                <textarea value={f.whitelist} onChange={e => setF(s => ({ ...s, whitelist: e.target.value }))} rows={5} placeholder={'dost@ornek.com\nguvenli.com'}
                  className={ta + ' focus:ring-emerald-500/40 focus:border-emerald-400'} />
              </div>
              <div>
                <label className="block text-sm font-medium text-red-700 dark:text-red-400 mb-1.5">{cevir("Kara liste (reddet)")}</label>
                <textarea value={f.blacklist} onChange={e => setF(s => ({ ...s, blacklist: e.target.value }))} rows={5} placeholder={'spam@kotu.com\nreklam.net'}
                  className={ta + ' focus:ring-red-500/40 focus:border-red-400'} />
              </div>
            </div>
          </section>

          {/* IP / ASN engelleme */}
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("IP ve ASN Engelleme (bağlanan istemci)")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">
              {cevir("Buradaki IP/ağlardan gelen tüm SMTP bağlantıları reddedilir (kimlik doğrulamış kullanıcılar hariç).")}
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-200 mb-1.5">{cevir("IP / CIDR ağ engelle")}</label>
                <p className="text-xs text-slate-400 mb-1.5">{cevir("Tek IP")} (<span className="font-mono">203.0.113.5</span>) {cevir("veya ağ")} (<span className="font-mono">203.0.113.0/24</span>). {cevir("Satır başına bir tane.")}</p>
                <textarea value={f.ip_blocklist} onChange={e => setF(s => ({ ...s, ip_blocklist: e.target.value }))} rows={6} placeholder={'203.0.113.5\n45.146.0.0/16'}
                  className={ta + ' focus:ring-brand-500/40 focus:border-brand-400'} />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-200 mb-1.5">{cevir("ASN engelle")}</label>
                <p className="text-xs text-slate-400 mb-1.5">{cevir("Otonom sistem numarası")} (<span className="font-mono">AS9009</span>). {cevir("O ASN'nin tüm IP blokları çözülüp engellenir.")}</p>
                <textarea value={f.asn_blocklist} onChange={e => setF(s => ({ ...s, asn_blocklist: e.target.value }))} rows={6} placeholder={'AS9009\nAS14061'}
                  className={ta + ' focus:ring-brand-500/40 focus:border-brand-400'} />
              </div>
            </div>

            {/* ASN çözüm özeti */}
            {(cozunum.length > 0 || toplamCidr !== null) && (
              <div className="mt-4 border-t border-slate-100 dark:border-slate-700/60 pt-3">
                {toplamCidr !== null && (
                  <p className="text-xs text-slate-600 dark:text-slate-300 mb-2">
                    {cevir("Toplam")} <span className="font-semibold">{toplamCidr}</span> {cevir("IP/ağ engelleniyor.")}
                  </p>
                )}
                {cozunum.length > 0 && (
                  <ul className="space-y-1">
                    {cozunum.map((c, i) => (
                      <li key={i} className="text-xs flex flex-wrap items-center gap-x-2">
                        <span className="font-mono font-medium text-slate-700 dark:text-slate-200">{c.asn}</span>
                        {c.kaynak === 'yok'
                          ? <span className="text-red-600 dark:text-red-400">🔴 {c.uyari || cevir("çözülemedi")}</span>
                          : <>
                              <span className="text-slate-500">{c.prefiks_sayisi} {cevir("prefiks")}</span>
                              {c.kaynak === cevir("önbellek")
                                ? <span className="text-amber-600 dark:text-amber-400" title={c.uyari}>{cevir("⚠ önbellekten")}</span>
                                : <span className="text-emerald-600 dark:text-emerald-400">{cevir("✓ canlı")}</span>}
                            </>}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </section>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-6 py-2.5 rounded-lg disabled:opacity-60 transition-colors">
              {kaydediliyor ? cevir("Uygulanıyor…") : cevir("Kaydet ve Uygula")}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
