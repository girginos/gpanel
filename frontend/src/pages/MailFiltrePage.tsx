import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'

type Filtre = { whitelist: string; blacklist: string; ip_blocklist: string; asn_blocklist: string }
type AsnCozum = { asn: string; prefiks_sayisi: number; kaynak: string; uyari?: string }
type PutYanit = { durum: string; cozunum: AsnCozum[] | null; toplam_cidr: number; uyari?: string }

const BOS: Filtre = { whitelist: '', blacklist: '', ip_blocklist: '', asn_blocklist: '' }

export default function MailFiltrePage() {
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
      .catch(e => setHata(apiHata(e, 'Filtreler yüklenemedi (mail eklentisi aktif mi?)')))
      .finally(() => setYukleniyor(false))
  }, [])

  async function kaydet() {
    setKaydediliyor(true); setHata(null); setBildirim(null)
    try {
      const r = await api.put<PutYanit>('/eklenti/mail/genel/filtre', f)
      setCozunum(r.data.cozunum ?? [])
      setToplamCidr(r.data.toplam_cidr)
      if (r.data.durum === 'kısmi') {
        setHata('Bazı ASN girdileri çözülemedi (aşağıya bakın). Diğer kurallar uygulandı.')
      } else {
        setBildirim(`✓ Filtreler kaydedildi ve Postfix'e uygulandı. Toplam ${r.data.toplam_cidr} IP/ağ engelleniyor.`)
      }
    } catch (e) { setHata(apiHata(e, 'Kaydedilemedi')) }
    finally { setKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm'
  const ta = 'w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm font-mono resize-y focus:outline-none focus:ring-2'

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
        Gönderen (e-posta/domain) ve bağlanan istemci (IP, CIDR ağ veya tüm ASN) bazında kabul/engelleme kuralları.
        ASN girdikten sonra o otonom sistemin <span className="font-medium">duyurduğu tüm IP blokları</span> otomatik çözülüp engellenir.
        Kimlik doğrulamış kullanıcılar ve yerel ağ bu kurallardan <span className="font-medium">etkilenmez</span>.
      </p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div> : (
        <div className="space-y-5">
          {/* Gönderen filtreleri */}
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Gönderen Filtreleri (e-posta / domain)</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">Her satıra bir e-posta veya domain. Beyaz liste her zaman kabul edilir, kara liste reddedilir.</p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-emerald-700 dark:text-emerald-400 mb-1.5">Beyaz liste (kabul)</label>
                <textarea value={f.whitelist} onChange={e => setF(s => ({ ...s, whitelist: e.target.value }))} rows={5} placeholder={'dost@ornek.com\nguvenli.com'}
                  className={ta + ' focus:ring-emerald-500/40 focus:border-emerald-400'} />
              </div>
              <div>
                <label className="block text-sm font-medium text-red-700 dark:text-red-400 mb-1.5">Kara liste (reddet)</label>
                <textarea value={f.blacklist} onChange={e => setF(s => ({ ...s, blacklist: e.target.value }))} rows={5} placeholder={'spam@kotu.com\nreklam.net'}
                  className={ta + ' focus:ring-red-500/40 focus:border-red-400'} />
              </div>
            </div>
          </section>

          {/* IP / ASN engelleme */}
          <section className={kart}>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">IP ve ASN Engelleme (bağlanan istemci)</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">
              Buradaki IP/ağlardan gelen tüm SMTP bağlantıları reddedilir (kimlik doğrulamış kullanıcılar hariç).
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-200 mb-1.5">IP / CIDR ağ engelle</label>
                <p className="text-xs text-slate-400 mb-1.5">Tek IP (<span className="font-mono">203.0.113.5</span>) veya ağ (<span className="font-mono">203.0.113.0/24</span>). Satır başına bir tane.</p>
                <textarea value={f.ip_blocklist} onChange={e => setF(s => ({ ...s, ip_blocklist: e.target.value }))} rows={6} placeholder={'203.0.113.5\n45.146.0.0/16'}
                  className={ta + ' focus:ring-brand-500/40 focus:border-brand-400'} />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 dark:text-slate-200 mb-1.5">ASN engelle</label>
                <p className="text-xs text-slate-400 mb-1.5">Otonom sistem numarası (<span className="font-mono">AS9009</span>). O ASN'nin tüm IP blokları çözülüp engellenir.</p>
                <textarea value={f.asn_blocklist} onChange={e => setF(s => ({ ...s, asn_blocklist: e.target.value }))} rows={6} placeholder={'AS9009\nAS14061'}
                  className={ta + ' focus:ring-brand-500/40 focus:border-brand-400'} />
              </div>
            </div>

            {/* ASN çözüm özeti */}
            {(cozunum.length > 0 || toplamCidr !== null) && (
              <div className="mt-4 border-t border-slate-100 dark:border-slate-700/60 pt-3">
                {toplamCidr !== null && (
                  <p className="text-xs text-slate-600 dark:text-slate-300 mb-2">
                    Toplam <span className="font-semibold">{toplamCidr}</span> IP/ağ engelleniyor.
                  </p>
                )}
                {cozunum.length > 0 && (
                  <ul className="space-y-1">
                    {cozunum.map((c, i) => (
                      <li key={i} className="text-xs flex flex-wrap items-center gap-x-2">
                        <span className="font-mono font-medium text-slate-700 dark:text-slate-200">{c.asn}</span>
                        {c.kaynak === 'yok'
                          ? <span className="text-red-600 dark:text-red-400">🔴 {c.uyari || 'çözülemedi'}</span>
                          : <>
                              <span className="text-slate-500">{c.prefiks_sayisi} prefiks</span>
                              {c.kaynak === 'önbellek'
                                ? <span className="text-amber-600 dark:text-amber-400" title={c.uyari}>⚠ önbellekten</span>
                                : <span className="text-emerald-600 dark:text-emerald-400">✓ canlı</span>}
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
              {kaydediliyor ? 'Uygulanıyor…' : 'Kaydet ve Uygula'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
