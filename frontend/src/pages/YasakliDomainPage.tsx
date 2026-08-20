import type { ChangeEvent, FormEvent } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { useDialog } from '@/components/Dialog'

/*
 * Yasaklı Alan Adları — phishing koruması.
 *
 * Amaç: kullanıcıların "sahibinden.com" veya "login.sahibinden.com" gibi
 * marka look-alike hostname'leri panele eklemesini engellemek. DNS'in gerçek
 * sahibi olmasa bile panelde vhost açıp trafik yönlendirilebilir — klasik
 * phishing tekniği.
 *
 * Alt-domain kapsamı seçilebilir. Varsayılan AÇIK: bir domaini yasakladığında
 * tüm alt-domain'leri de otomatik engelli (phishing tam olarak burada gizler).
 * Kapatmak sadece istisnai durumlar için.
 *
 * Değişiklikler en fazla 60 sn içinde tüm domain oluşturma yollarına yansır.
 * MEVCUT domainlere DOKUNMAZ.
 */

type Kayit = {
  domain: string
  description: string
  match_subdomains: boolean
  created_at: string
}

export default function YasakliDomainPage() {
  const [list, setList] = useState<Kayit[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [domain, setDomain] = useState('')
  const [aciklama, setAciklama] = useState('')
  const [altDahil, setAltDahil] = useState(true) // phishing için doğru varsayılan
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const [ekleHata, setEkleHata] = useState<string | null>(null)
  const dialog = useDialog()

  // Toplu ekleme
  const [topluAcik, setTopluAcik] = useState(false)
  const [topluMetin, setTopluMetin] = useState('')
  const [topluAltDahil, setTopluAltDahil] = useState(true)
  const [topluAciklama, setTopluAciklama] = useState('')
  const [topluGidiyor, setTopluGidiyor] = useState(false)
  const [topluHata, setTopluHata] = useState<string | null>(null)
  const dosyaRef = useRef<HTMLInputElement>(null)

  // Toplu silme — seçili domain kümesi (Set: O(1) ekle/çıkar/kontrol).
  const [secili, setSecili] = useState<Set<string>>(new Set())
  const [siliniyor, setSiliniyor] = useState(false)

  const yukle = async () => {
    setYukleniyor(true); setHata(null)
    try {
      const r = await api.get<Kayit[]>('/banned-domains')
      setList(Array.isArray(r.data) ? r.data : [])
    } catch (e) {
      setHata(apiHata(e, 'Liste alınamadı'))
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { yukle() }, [])

  const ekle = async (ev: FormEvent) => {
    ev.preventDefault(); setEkleHata(null)
    const d = domain.trim().replace(/^https?:\/\//, '').replace(/^www\./, '')
      .replace(/[/?#].*$/, '').replace(/^\.+|\.+$/g, '').toLowerCase()
    if (!d) { setEkleHata('Alan adı boş olamaz'); return }
    if (!d.includes('.')) { setEkleHata('Tam alan adı girin (örn. sahibinden.com)'); return }
    setGonderiliyor(true)
    try {
      await api.post('/banned-domains', {
        domain: d, description: aciklama.trim(), match_subdomains: altDahil,
      })
      setDomain(''); setAciklama(''); setAltDahil(true)
      await yukle()
    } catch (e) {
      setEkleHata(apiHata(e, 'Eklenemedi'))
    } finally { setGonderiliyor(false) }
  }

  // TXT dosyası yüklendiğinde textarea'ya doldur — kullanıcı ne aldığımızı
  // GÖRSÜN, yanlış dosyayı fark etmeden yüklemesin. Ayrıca yüklendikten sonra
  // düzenlenebilir (istenmeyen satırı çıkarsın).
  const dosyaYuklendi = async (ev: ChangeEvent<HTMLInputElement>) => {
    const f = ev.target.files?.[0]
    if (!f) return
    // 5 MB'lık üst sınır — daha büyük dosya = başka bir şey.
    if (f.size > 5 * 1024 * 1024) {
      setTopluHata('Dosya çok büyük (üst sınır 5 MB)')
      return
    }
    try {
      const metin = await f.text()
      // Mevcut içerik varsa üstüne ekle, üzerine yaz sürprizi olmasın.
      setTopluMetin((eski) => (eski.trim() ? eski + '\n' + metin : metin))
      setTopluHata(null)
    } catch (e) {
      setTopluHata('Dosya okunamadı: ' + (e as Error).message)
    } finally {
      // Aynı dosyayı tekrar yükleyebilmek için değeri sıfırla.
      if (dosyaRef.current) dosyaRef.current.value = ''
    }
  }

  const topluGonder = async () => {
    setTopluHata(null)
    if (!topluMetin.trim()) { setTopluHata('İçerik boş'); return }
    setTopluGidiyor(true)
    try {
      const r = await api.post('/banned-domains/bulk', {
        domains: topluMetin,
        description: topluAciklama.trim(),
        match_subdomains: topluAltDahil,
      })
      const d = r.data as { toplam?: number; islendi?: number; yoksayildi?: number; basarisiz?: { domain: string; sebep: string }[] | null }
      // 🔴 Defansif: eski Go build'i basarisiz'i null dönebiliyordu (nil
      // slice → JSON null). Hem burayı hem de ilerideki API sürprizlerini
      // yakalamak için tümünü ?? ile normalize et.
      const islendi = d.islendi ?? 0
      const yoksayildi = d.yoksayildi ?? 0
      const basarisiz = Array.isArray(d.basarisiz) ? d.basarisiz : []
      const basarisizOzet = basarisiz.length
        ? '\n\nBaşarısız (' + basarisiz.length + '):\n' +
          basarisiz.slice(0, 20).map((x) => `  • ${x.domain} — ${x.sebep}`).join('\n') +
          (basarisiz.length > 20 ? `\n  … ve ${basarisiz.length - 20} tane daha` : '')
        : ''
      await dialog.bilgi({
        baslik: 'Toplu ekleme tamamlandı',
        mesaj:
          `${islendi} eklendi/güncellendi, ${yoksayildi} atlandı (boş/yorum/tekrar), ` +
          `${basarisiz.length} başarısız.` + basarisizOzet,
      })
      setTopluMetin('')
      await yukle()
      if (basarisiz.length === 0) setTopluAcik(false)
    } catch (e) {
      setTopluHata(apiHata(e, 'Toplu ekleme başarısız'))
    } finally {
      setTopluGidiyor(false)
    }
  }

  // Seçim yardımcıları
  const seciliDegistir = (dom: string) => {
    setSecili((eski) => {
      const yeni = new Set(eski)
      if (yeni.has(dom)) yeni.delete(dom); else yeni.add(dom)
      return yeni
    })
  }
  const hepsiSecili = list.length > 0 && list.every((k) => secili.has(k.domain))
  const bazisiSecili = secili.size > 0 && !hepsiSecili
  const hepsiSecDegistir = () => {
    if (hepsiSecili) setSecili(new Set())
    else setSecili(new Set(list.map((k) => k.domain)))
  }

  const topluSil = async () => {
    if (secili.size === 0) return
    const kumeArr = Array.from(secili)
    const ok = await dialog.onay({
      baslik: `${kumeArr.length} kaydı sil?`,
      mesaj: `Seçili ${kumeArr.length} alan adı yasak listesinden kaldırılacak. Bu alan adlarına YENİ vhost açılışına yeniden izin verilecek. Mevcut siteler etkilenmez.`,
      onayEtiketi: 'Sil', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    setSiliniyor(true)
    try {
      const r = await api.post('/banned-domains/bulk-delete', { domains: kumeArr })
      const d = r.data as { silinen?: number; bulunamayan?: string[] | null; gecersiz?: string[] | null }
      const silinen = d.silinen ?? 0
      const bulunamayan = Array.isArray(d.bulunamayan) ? d.bulunamayan : []
      const gecersiz = Array.isArray(d.gecersiz) ? d.gecersiz : []
      const uyari = bulunamayan.length + gecersiz.length
      if (uyari === 0) {
        // Sessiz başarı — çok tıklamadan iş bitti
      } else {
        await dialog.bilgi({
          baslik: 'Toplu silme tamamlandı',
          mesaj:
            `${silinen} silindi. ` +
            (bulunamayan.length ? `${bulunamayan.length} kayıt zaten yoktu. ` : '') +
            (gecersiz.length ? `${gecersiz.length} geçersiz biçim.` : ''),
        })
      }
      setSecili(new Set())
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Silinemedi', mesaj: apiHata(e, 'Toplu silme hatası') })
    } finally {
      setSiliniyor(false)
    }
  }

  const sil = async (k: Kayit) => {
    const kapsam = k.match_subdomains ? ' (alt-domainler dahil)' : ''
    const ok = await dialog.onay({
      baslik: `${k.domain}${kapsam} yasağını kaldır?`,
      mesaj: 'Bu alan adına ve alt-domainlerine YENİ vhost açılışına yeniden izin verilecek. Mevcut domainler etkilenmez.',
      onayEtiketi: 'Kaldır', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    try {
      await api.delete(`/banned-domains/${encodeURIComponent(k.domain)}`)
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Silinemedi', mesaj: apiHata(e, 'Silme hatası') })
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'Yasaklı Alan Adları' },
      ]} />

      <div className="mt-4 mb-2">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
          Yasaklı Alan Adları
        </h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          Bayi ve müşteriler bu alan adlarında yeni domain açamaz.{' '}
          <span className="font-medium text-slate-800 dark:text-slate-200">Mevcut siteler etkilenmez.</span>
        </p>
      </div>

      {/* Bilgilendirme */}
      <div className="mb-6 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200">
        <strong>Phishing koruması.</strong> Alt-domainler dahil seçeneği açıkken
        <code className="mx-1 rounded bg-amber-100 px-1 py-0.5 font-mono text-[11px] dark:bg-amber-900/50">sahibinden.com</code>
        eklendiğinde <code className="rounded bg-amber-100 px-1 py-0.5 font-mono text-[11px] dark:bg-amber-900/50">login.sahibinden.com</code> gibi tüm alt-domainler de otomatik engellenir.
      </div>

      {/* Ekleme formu */}
      <form onSubmit={ekle} className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(200px,1.2fr)_1fr_auto]">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Alan Adı</label>
            <input
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              placeholder="sahibinden.com"
              className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              autoComplete="off"
              spellCheck={false}
            />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Açıklama (opsiyonel)</label>
            <input
              value={aciklama}
              onChange={(e) => setAciklama(e.target.value)}
              placeholder="Ör. phishing hedefi marka"
              maxLength={255}
              className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
            />
          </div>
          <div className="flex items-end">
            <button
              type="submit"
              disabled={gonderiliyor || !domain.trim()}
              className="h-[38px] rounded-lg bg-slate-900 px-4 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
            >
              {gonderiliyor ? 'Ekleniyor…' : 'Yasakla'}
            </button>
          </div>
        </div>
        <label className="mt-3 flex cursor-pointer select-none items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
          <input
            type="checkbox"
            checked={altDahil}
            onChange={(e) => setAltDahil(e.target.checked)}
            className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
          />
          Alt-domain'ler dahil <span className="text-xs text-slate-500">(phishing için önerilen)</span>
        </label>
        {ekleHata && (
          <p className="mt-3 text-sm text-red-600 dark:text-red-400">{ekleHata}</p>
        )}

        {/* Toplu ekleme aç/kapa — mevcut tekil formun kirlenmemesi için ayrı
            expander. Kullanıcı tek eklerken bu panel görünmez. */}
        <div className="mt-3 border-t border-slate-100 pt-3 dark:border-slate-800">
          <button
            type="button"
            onClick={() => setTopluAcik((v) => !v)}
            className="text-sm font-medium text-slate-700 hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-100"
          >
            {topluAcik ? '− Toplu ekleme panelini kapat' : '+ Toplu ekle (TXT / liste yapıştır)'}
          </button>
        </div>
      </form>

      {topluAcik && (
        <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Toplu Ekleme</h2>
              <p className="mt-0.5 text-xs text-slate-500">
                Her satıra bir domain. Boşluk, virgül, noktalı virgül veya sekme ile de
                ayırabilirsin. <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">#</code> ile
                başlayan satırlar yorumdur, atlanır. Üst sınır 5.000 domain.
              </p>
            </div>
            <div>
              <input
                ref={dosyaRef}
                type="file"
                accept=".txt,text/plain,.csv,text/csv"
                onChange={dosyaYuklendi}
                className="hidden"
                id="toplu-dosya"
              />
              <label
                htmlFor="toplu-dosya"
                className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm text-slate-700 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300 dark:hover:bg-slate-900"
              >
                <Ikon d={I.klasor} className="h-4 w-4" /> Dosya seç (TXT)
              </label>
            </div>
          </div>

          <textarea
            value={topluMetin}
            onChange={(e) => setTopluMetin(e.target.value)}
            placeholder={'sahibinden.com\ntrendyol.com\nhepsiburada.com\n# yorum satırı — atlanır\nakbank.com garanti.com.tr'}
            rows={10}
            spellCheck={false}
            className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-[13px] leading-6 outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />

          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto]">
            <input
              value={topluAciklama}
              onChange={(e) => setTopluAciklama(e.target.value)}
              placeholder="Tümüne uygulanacak açıklama (opsiyonel, ör. 'phishing marka listesi')"
              maxLength={255}
              className="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
            />
            <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
              <input
                type="checkbox"
                checked={topluAltDahil}
                onChange={(e) => setTopluAltDahil(e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
              />
              Alt-domain'ler dahil
            </label>
          </div>

          <div className="mt-3 flex items-center justify-between gap-3">
            <div className="text-xs text-slate-500">
              {topluMetin.trim() ? (
                <>~{(topluMetin.match(/[\n,;\s]+/g)?.length ?? 0) + 1} satır algılandı</>
              ) : (
                'Metin bekleniyor…'
              )}
            </div>
            <button
              type="button"
              onClick={topluGonder}
              disabled={topluGidiyor || !topluMetin.trim()}
              className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
            >
              {topluGidiyor ? 'Yükleniyor…' : 'Toplu Yasakla'}
            </button>
          </div>

          {topluHata && (
            <p className="mt-3 text-sm text-red-600 dark:text-red-400">{topluHata}</p>
          )}
        </div>
      )}

      {/* Liste */}
      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
          {hata} <button onClick={yukle} className="underline">Tekrar dene</button>
        </div>
      ) : list.length === 0 ? (
        <EmptyState baslik="Henüz yasaklı alan adı yok" aciklama="Phishing hedefi olabilecek marka domainleri için yukarıdaki formu kullanın." />
      ) : (
        <>
          {/* Seçim çubuğu — sadece bir şey seçildiğinde görünür */}
          {secili.size > 0 && (
            <div className="mb-3 flex items-center justify-between gap-3 rounded-2xl border border-slate-300 bg-slate-50 px-4 py-2.5 dark:border-slate-700 dark:bg-slate-900">
              <div className="text-sm text-slate-700 dark:text-slate-300">
                <span className="font-semibold">{secili.size}</span> kayıt seçili
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setSecili(new Set())}
                  className="rounded-md px-2.5 py-1 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
                >
                  Seçimi temizle
                </button>
                <button
                  onClick={topluSil}
                  disabled={siliniyor}
                  className="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {siliniyor ? 'Siliniyor…' : `Seçilenleri Sil (${secili.size})`}
                </button>
              </div>
            </div>
          )}

          <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
            <table className="w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th className="w-10 px-4 py-3">
                    <input
                      type="checkbox"
                      aria-label="Hepsini seç"
                      checked={hepsiSecili}
                      ref={(el) => { if (el) el.indeterminate = bazisiSecili }}
                      onChange={hepsiSecDegistir}
                      className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
                    />
                  </th>
                  <th className="px-4 py-3 font-semibold">Alan Adı</th>
                  <th className="px-4 py-3 font-semibold">Açıklama</th>
                  <th className="px-4 py-3 font-semibold">Kapsam</th>
                  <th className="px-4 py-3 font-semibold">Eklendi</th>
                  <th className="px-4 py-3 text-right font-semibold">İşlem</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {list.map((k) => (
                  <tr key={k.domain} className={secili.has(k.domain) ? 'bg-slate-50 dark:bg-slate-900/60' : 'bg-white dark:bg-slate-950'}>
                    <td className="px-4 py-3">
                      <input
                        type="checkbox"
                        aria-label={`${k.domain} seç`}
                        checked={secili.has(k.domain)}
                        onChange={() => seciliDegistir(k.domain)}
                        className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-400 dark:border-slate-700 dark:bg-slate-950"
                      />
                    </td>
                    <td className="px-4 py-3 font-mono font-medium text-slate-900 dark:text-slate-100">{k.domain}</td>
                    <td className="px-4 py-3 text-slate-600 dark:text-slate-400">
                      {k.description || <span className="text-slate-400">—</span>}
                    </td>
                    <td className="px-4 py-3 text-xs">
                      {k.match_subdomains ? (
                        <span className="rounded-full bg-slate-100 px-2 py-0.5 text-slate-700 dark:bg-slate-800 dark:text-slate-300">alt-domainler dahil</span>
                      ) : (
                        <span className="rounded-full bg-slate-50 px-2 py-0.5 text-slate-500 dark:bg-slate-900 dark:text-slate-500">yalnız tam eşleşme</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-xs text-slate-500">
                      {new Date(k.created_at).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' })}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => sil(k)}
                        className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40"
                      >
                        Kaldır
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      <p className="mt-4 text-xs text-slate-500">
        {list.length} alan adı yasaklı · değişiklikler en fazla 60 saniye içinde tüm domain oluşturma yollarına yansır
      </p>
    </div>
  )
}
