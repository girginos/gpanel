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
  const [a, setA] = useState<Ayar>(BOS)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)

  useEffect(() => {
    api.get<Ayar>('/eklenti/mail/genel-ayarlar')
      .then(r => setA({ ...BOS, ...r.data }))
      .catch(e => setHata(apiHata(e, 'Ayarlar yüklenemedi (mail eklentisi aktif mi?)')))
      .finally(() => setYukleniyor(false))
  }, [])

  async function kaydet() {
    setKaydediliyor(true); setHata(null); setBildirim(null)
    try {
      await api.put('/eklenti/mail/genel-ayarlar', a)
      setBildirim('✓ Ayarlar kaydedildi ve sunucuya uygulandı.')
    } catch (e) { setHata(apiHata(e, 'Kaydedilemedi')) }
    finally { setKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm'

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">Sunucu genelinde geçerli mail ayarları (tüm domainler için varsayılan).</p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div> : (
        <div className="space-y-5">
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">Mesaj Boyutu</h2>
            <SayiAlan etiket="Maksimum mesaj boyutu (MB)" aciklama="Bir e-postanın (ekler dahil) izin verilen en büyük boyutu." min={1} deger={a.max_mesaj_mb} setir={n => setA(s => ({ ...s, max_mesaj_mb: n }))} />
          </section>

          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Giden Gönderim Limitleri (saatlik)</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">Spam/kötüye kullanımı önler. 0 = sınırsız. En kısıtlayıcı olan uygulanır. Domain planı özel limit belirlemişse o öncelikli olur.</p>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-5">
              <SayiAlan etiket="Posta kutusu başına" aciklama="Tek bir kutunun saatlik giden limiti." deger={a.saatlik_kutu} setir={n => setA(s => ({ ...s, saatlik_kutu: n }))} />
              <SayiAlan etiket="Alan adı başına" aciklama="Bir domainin tüm kutuları toplam." deger={a.saatlik_domain} setir={n => setA(s => ({ ...s, saatlik_domain: n }))} />
              <SayiAlan etiket="IP başına" aciklama="Aynı IP'den saatlik giden." deger={a.saatlik_ip} setir={n => setA(s => ({ ...s, saatlik_ip: n }))} />
            </div>
            <label className="mt-4 inline-flex items-center gap-2.5 cursor-pointer">
              <input type="checkbox" checked={a.alici_say} onChange={e => setA(s => ({ ...s, alici_say: e.target.checked }))}
                className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              <span className="text-sm text-slate-700 dark:text-slate-200">Mesaj yerine alıcı say <span className="text-slate-400">— 10 alıcıya giden 1 mesaj, 10 mesaj sayılır</span></span>
            </label>
          </section>

          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">DNSBL (kara delik listeleri)</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-3">Sağlık analizinde kontrol edilecek DNSBL bölgeleri. Noktalı virgül veya virgülle ayırın.</p>
            <input value={a.dnsbl} onChange={e => setA(s => ({ ...s, dnsbl: e.target.value }))} placeholder="zen.spamhaus.org; bl.spamcop.net"
              className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
          </section>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-6 py-2.5 rounded-lg disabled:opacity-60 transition-colors">
              {kaydediliyor ? 'Kaydediliyor…' : 'Kaydet ve Uygula'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
