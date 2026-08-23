import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'

type Ayar = {
  max_mesaj_mb: number
  saatlik_kutu: number
  saatlik_domain: number
  saatlik_ip: number
  alici_say: boolean
  dnsbl: string
}

const BOS: Ayar = { max_mesaj_mb: 25, saatlik_kutu: 100, saatlik_domain: 250, saatlik_ip: 500, alici_say: true, dnsbl: '' }


const MGENEL_EN: Record<string, string> = {
  "Alan adı başına": "Per domain",
  "Ayarlar yüklenemedi (mail eklentisi aktif mi?)": "Failed to load settings (is the mail add-on active?)",
  "Aynı IP'den saatlik giden.": "Outgoing per hour from the same IP.",
  "Bir domainin tüm kutuları toplam.": "Total of all mailboxes of a domain.",
  "Bir e-postanın (ekler dahil) izin verilen en büyük boyutu.": "The maximum allowed size of an email (including attachments).",
  "Giden Gönderim Limitleri (saatlik)": "Outgoing Sending Limits (hourly)",
  "IP başına": "Per IP",
  "Mesaj yerine alıcı say": "Count recipients instead of messages",
  "Posta kutusu başına": "Per mailbox",
  "Sağlık analizinde kontrol edilecek DNSBL bölgeleri. Noktalı virgül veya virgülle ayırın.": "DNSBL zones to check in health analysis. Separate with semicolons or commas.",
  "Spam/kötüye kullanımı önler. 0 = sınırsız. En kısıtlayıcı olan uygulanır. Domain planı özel limit belirlemişse o öncelikli olur.": "Prevents spam/abuse. 0 = unlimited. The most restrictive one applies. If a domain plan sets a specific limit it takes precedence.",
  "Sunucu genelinde geçerli mail ayarları (tüm domainler için varsayılan).": "Server-wide mail settings (default for all domains).",
  "✓ Ayarlar kaydedildi ve sunucuya uygulandı.": "✓ Settings saved and applied to the server.",
  "Türkçe": "English",
  "Kaydedilemedi": "Failed to save",
  "Mesaj Boyutu": "Message Size",
  "Maksimum mesaj boyutu (MB)": "Maximum message size (MB)",
  "Tek bir kutunun saatlik giden limiti.": "Hourly outgoing limit for a single mailbox.",
  "— 10 alıcıya giden 1 mesaj, 10 mesaj sayılır": "— 1 message to 10 recipients counts as 10 messages",
  "DNSBL (kara delik listeleri)": "DNSBL (blackhole lists)",
  "Kaydediliyor…": "Saving…",
  "Kaydet ve Uygula": "Save and Apply",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MGENEL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function SayiAlan({ etiket, aciklama, deger, setir, min = 0 }: { etiket: string; aciklama: string; deger: number; setir: (n: number) => void; min?: number }) {
  return (
    <div>
      <label className="block text-sm font-medium text-slate-700 dark:text-slate-200">{etiket}</label>
      <p className="text-xs text-slate-500 dark:text-slate-400 mb-1.5">{aciklama}</p>
      <input type="number" min={min} value={deger} onChange={e => setir(+e.target.value)}
        className="w-40 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
    </div>
  )
}

export default function MailGenelAyarPage() {
  useTranslation() // dil re-render aboneligi
  const [a, setA] = useState<Ayar>(BOS)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)

  useEffect(() => {
    api.get<Ayar>('/eklenti/mail/genel-ayarlar')
      .then(r => setA({ ...BOS, ...r.data }))
      .catch(e => setHata(apiHata(e, cevir("Ayarlar yüklenemedi (mail eklentisi aktif mi?)"))))
      .finally(() => setYukleniyor(false))
  }, [])

  async function kaydet() {
    setKaydediliyor(true); setHata(null); setBildirim(null)
    try {
      await api.put('/eklenti/mail/genel-ayarlar', a)
      setBildirim(cevir("✓ Ayarlar kaydedildi ve sunucuya uygulandı."))
    } catch (e) { setHata(apiHata(e, cevir("Kaydedilemedi"))) }
    finally { setKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm'

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{cevir("Sunucu genelinde geçerli mail ayarları (tüm domainler için varsayılan).")}</p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div> : (
        <div className="space-y-5">
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Mesaj Boyutu")}</h2>
            <SayiAlan etiket={cevir("Maksimum mesaj boyutu (MB)")} aciklama={cevir("Bir e-postanın (ekler dahil) izin verilen en büyük boyutu.")} min={1} deger={a.max_mesaj_mb} setir={n => setA(s => ({ ...s, max_mesaj_mb: n }))} />
          </section>

          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Giden Gönderim Limitleri (saatlik)")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">{cevir("Spam/kötüye kullanımı önler. 0 = sınırsız. En kısıtlayıcı olan uygulanır. Domain planı özel limit belirlemişse o öncelikli olur.")}</p>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-5">
              <SayiAlan etiket={cevir("Posta kutusu başına")} aciklama={cevir("Tek bir kutunun saatlik giden limiti.")} deger={a.saatlik_kutu} setir={n => setA(s => ({ ...s, saatlik_kutu: n }))} />
              <SayiAlan etiket={cevir("Alan adı başına")} aciklama={cevir("Bir domainin tüm kutuları toplam.")} deger={a.saatlik_domain} setir={n => setA(s => ({ ...s, saatlik_domain: n }))} />
              <SayiAlan etiket={cevir("IP başına")} aciklama={cevir("Aynı IP'den saatlik giden.")} deger={a.saatlik_ip} setir={n => setA(s => ({ ...s, saatlik_ip: n }))} />
            </div>
            <label className="mt-4 inline-flex items-center gap-2.5 cursor-pointer">
              <input type="checkbox" checked={a.alici_say} onChange={e => setA(s => ({ ...s, alici_say: e.target.checked }))}
                className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              <span className="text-sm text-slate-700 dark:text-slate-200">{cevir("Mesaj yerine alıcı say")} <span className="text-slate-400">{cevir("— 10 alıcıya giden 1 mesaj, 10 mesaj sayılır")}</span></span>
            </label>
          </section>

          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("DNSBL (kara delik listeleri)")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-3">{cevir("Sağlık analizinde kontrol edilecek DNSBL bölgeleri. Noktalı virgül veya virgülle ayırın.")}</p>
            <input value={a.dnsbl} onChange={e => setA(s => ({ ...s, dnsbl: e.target.value }))} placeholder="zen.spamhaus.org; bl.spamcop.net"
              className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
          </section>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-6 py-2.5 rounded-lg disabled:opacity-60 transition-colors">
              {kaydediliyor ? cevir("Kaydediliyor…") : cevir("Kaydet ve Uygula")}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
