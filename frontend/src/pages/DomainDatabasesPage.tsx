// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { T } from '@/lib/tablo'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
export type DB = {
  id: number; domain_id: number; db_adi: string; db_kullanici: string;
  db_host: string; db_parola: string; olusturulma: string; boyut: number
}

export function boyutFmt(b: number): string {
  if (!b || b <= 0) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0, v = b
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return v.toFixed(v < 10 && i > 0 ? 1 : 0) + ' ' + u[i]
}

export default function DomainDatabasesPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [dbler, setDbler] = useState<DB[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [ekleAcik, setEkleAcik] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<DB[]>(`/domains/${id}/databases`)
      .then(r => setDbler(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }

  useEffect(() => {
    if (id) api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(hataYakala('Alan adı bilgisi alınamadı'))
    yukle()
  }, [id])

  // Domain'in mevcut DB-kullanıcıları (mevcut-kullanıcı seçimi için, benzersiz).
  const mevcutKullanicilar = useMemo(
    () => Array.from(new Set(dbler.map(d => d.db_kullanici))),
    [dbler],
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1300px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Veritabanları' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Veritabanları</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5"><Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link></p>}

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <button onClick={() => setEkleAcik(true)} className="px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md">+ Yeni Veritabanı</button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md">↻ Yenile</button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{dbler.length} veritabanı</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Sadeleştirilmiş liste: kullanıcı/sunucu/parola/işlemler her DB'nin KENDİ
          detay sayfasında. Böylece tablo dar kalır, yatay scroll gerekmez. */}
      <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
        {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> :
         dbler.length === 0 ? <div className="py-12 text-center text-sm text-slate-500 dark:text-slate-500">Henüz veritabanı yok</div> :
        <div className="lg:overflow-x-auto">
          <table className={T.tablo}>
          <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700`}>
            <tr>
              <th className={T.baslik}>Veritabanı</th>
              <th className={T.baslik}>Oluşturulma</th>
              <th className={`${T.baslik} text-right`}>Boyut</th>
              <th className={`${T.baslik} text-right`}></th>
            </tr>
          </thead>
          <tbody className={T.govde}>
            {dbler.map(d => (
              <tr
                key={d.id}
                onClick={() => nav(`/abonelikler/${id}/veritabanlari/${d.id}`)}
                className={`${T.satir} cursor-pointer lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800`}
              >
                <td className={`${T.hucreBaslik} font-mono`}><span className="text-brand-700 dark:text-brand-300">{d.db_adi}</span></td>
                <td className={T.hucre} data-etiket="Oluşturulma">
                  <span className="text-slate-600 dark:text-slate-400 whitespace-nowrap">{d.olusturulma}</span>
                </td>
                <td className={`${T.hucre} text-right`} data-etiket="Boyut"><span className="font-mono tabular-nums text-slate-700 dark:text-slate-300 whitespace-nowrap">{boyutFmt(d.boyut)}</span></td>
                <td className={`${T.hucreAksiyon} lg:text-right`}>
                  <span className="inline-flex items-center gap-1 text-sm text-slate-500 dark:text-slate-400">Yönet
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" /></svg>
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>}
      </div>

      {ekleAcik && domain && (
        <YeniDBModal
          domainId={Number(id)}
          sk={domain.sistem_kullanici}
          mevcutKullanicilar={mevcutKullanicilar}
          onKapat={() => setEkleAcik(false)}
          onTamam={() => { setEkleAcik(false); yukle() }}
        />
      )}
    </div>
  )
}

// uretGucluParola: tarayıcı tarafı güçlü parola (harf+rakam karışık, min-güç geçer).
export function uretGucluParola(n = 20): string {
  const harf = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789'
  const buf = new Uint32Array(n)
  ;(window.crypto || (window as any).msCrypto).getRandomValues(buf)
  let s = ''
  for (let i = 0; i < n; i++) s += harf[buf[i] % harf.length]
  return s
}

type YeniDBModalProps = {
  domainId: number
  sk: string
  mevcutKullanicilar: string[]
  onKapat: () => void
  onTamam: () => void
}

const SONEK_RE = /^[a-z0-9_]{1,32}$/

function YeniDBModal({ domainId, sk, mevcutKullanicilar, onKapat, onTamam }: YeniDBModalProps) {
  const onek = sk + '_'
  const [otomatik, setOtomatik] = useState(true)
  const [dbSonek, setDbSonek] = useState('')
  const [kullaniciTipi, setKullaniciTipi] = useState<'yeni' | 'mevcut'>('yeni')
  const [kullaniciSonek, setKullaniciSonek] = useState('')
  const [mevcutKullanici, setMevcutKullanici] = useState(mevcutKullanicilar[0] || '')
  const [parola, setParola] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [sonuc, setSonuc] = useState<{ db_adi: string; db_kullanici: string; db_parola: string } | null>(null)

  const dbAdiOnizleme = onek + (dbSonek || '…')
  const kullaniciOnizleme = onek + (kullaniciSonek || '…')
  const parolaGucSorunu =
    parola !== '' && (parola.length < 12 || !/[A-Za-z]/.test(parola) || !/[0-9]/.test(parola))

  function yerelDogrula(): string | null {
    if (otomatik) return null
    if (!SONEK_RE.test(dbSonek)) return 'Veritabanı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter'
    if ((onek + dbSonek).length > 64) return 'Veritabanı adı çok uzun (önek + sonek ≤64 karakter olmalı)'
    if (kullaniciTipi === 'yeni') {
      if (!SONEK_RE.test(kullaniciSonek)) return 'Kullanıcı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter'
      if ((onek + kullaniciSonek).length > 64) return 'Kullanıcı adı çok uzun (önek + sonek ≤64 karakter olmalı)'
      if (parola !== '' && parolaGucSorunu) return 'Parola en az 12 karakter ve harf+rakam karışık olmalı'
    } else {
      if (!mevcutKullanici) return 'Mevcut bir kullanıcı seçin'
    }
    return null
  }

  async function olustur() {
    const y = yerelDogrula()
    if (y) { setHata(y); return }
    setIsleniyor(true); setHata(null)
    try {
      const body: Record<string, unknown> = otomatik
        ? { otomatik: true }
        : {
            db_sonek: dbSonek,
            kullanici_tipi: kullaniciTipi,
            ...(kullaniciTipi === 'yeni'
              ? { kullanici_sonek: kullaniciSonek, parola }
              : { mevcut_kullanici: mevcutKullanici }),
          }
      const { data } = await api.post(`/domains/${domainId}/databases`, body)
      setSonuc({ db_adi: data.db_adi, db_kullanici: data.db_kullanici, db_parola: data.db_parola })
    } catch (e) {
      setHata(apiHata(e, 'Oluşturma başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  const inputCls = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-900 dark:text-slate-100 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none disabled:opacity-50'

  return (
    <Modal acik={true} baslik="Yeni Veritabanı" onKapat={sonuc ? onTamam : onKapat} genislik="lg">
      {sonuc ? (
        <div className="space-y-4">
          <div className="bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md p-4 space-y-3">
            <p className="text-sm text-emerald-800 dark:text-emerald-200 font-medium">✓ Veritabanı oluşturuldu</p>
            <p className="text-xs text-emerald-700 dark:text-emerald-300">Bilgileri güvenli bir yere kaydedin. Parolayı sonra düz metin göremeyebilirsiniz:</p>
            <SonucSatir e="Veritabanı" v={sonuc.db_adi} />
            <SonucSatir e="Kullanıcı" v={sonuc.db_kullanici} />
            <SonucSatir e="Parola" v={sonuc.db_parola} />
          </div>
          <div className="flex justify-end">
            <button onClick={onTamam} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm rounded-md">Tamam</button>
          </div>
        </div>
      ) : (
        <div className="space-y-5">
          {/* Otomatik toggle */}
          <label className="flex items-center gap-3 cursor-pointer select-none">
            <input type="checkbox" checked={otomatik} onChange={e => setOtomatik(e.target.checked)} className="h-4 w-4 accent-brand-600" />
            <span className="text-sm text-slate-700 dark:text-slate-300">
              <strong className="font-medium">Otomatik</strong> — DB adı, kullanıcı ve parolayı panel üretsin
            </span>
          </label>

          {!otomatik && (
            <div className="space-y-5 pt-1">
              {/* DB adı */}
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Veritabanı adı</label>
                <div className="flex items-stretch">
                  <span className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-slate-300 dark:border-slate-600 bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 text-sm font-mono select-none">{onek}</span>
                  <input value={dbSonek} onChange={e => setDbSonek(e.target.value.toLowerCase())} placeholder="blog" className={inputCls + ' rounded-l-none'} />
                </div>
                <p className="mt-1 text-xs text-slate-400 dark:text-slate-500 font-mono">→ {dbAdiOnizleme}</p>
              </div>

              {/* DB kullanıcısı */}
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1.5">Veritabanı kullanıcısı</label>
                <div className="flex gap-4 mb-2">
                  <label className="flex items-center gap-1.5 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
                    <input type="radio" name="kullaniciTipi" checked={kullaniciTipi === 'yeni'} onChange={() => setKullaniciTipi('yeni')} className="accent-brand-600" />
                    Yeni kullanıcı
                  </label>
                  <label className={'flex items-center gap-1.5 text-sm cursor-pointer ' + (mevcutKullanicilar.length ? 'text-slate-700 dark:text-slate-300' : 'text-slate-400 dark:text-slate-600 cursor-not-allowed')}>
                    <input type="radio" name="kullaniciTipi" disabled={!mevcutKullanicilar.length} checked={kullaniciTipi === 'mevcut'} onChange={() => setKullaniciTipi('mevcut')} className="accent-brand-600" />
                    Mevcut kullanıcı seç
                  </label>
                </div>

                {kullaniciTipi === 'yeni' ? (
                  <>
                    <div className="flex items-stretch">
                      <span className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-slate-300 dark:border-slate-600 bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 text-sm font-mono select-none">{onek}</span>
                      <input value={kullaniciSonek} onChange={e => setKullaniciSonek(e.target.value.toLowerCase())} placeholder="bloguser" className={inputCls + ' rounded-l-none'} />
                    </div>
                    <p className="mt-1 text-xs text-slate-400 dark:text-slate-500 font-mono">→ {kullaniciOnizleme}</p>
                  </>
                ) : (
                  <select value={mevcutKullanici} onChange={e => setMevcutKullanici(e.target.value)} className={inputCls}>
                    {mevcutKullanicilar.map(u => <option key={u} value={u}>{u}</option>)}
                  </select>
                )}
              </div>

              {/* Parola (yalnız yeni kullanıcı için) */}
              {kullaniciTipi === 'yeni' && (
                <div>
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Parola <span className="text-slate-400 dark:text-slate-500">(boş bırakırsanız panel üretir)</span></label>
                  <div className="flex gap-2">
                    <input type="text" value={parola} onChange={e => setParola(e.target.value)} placeholder="En az 12 karakter, harf+rakam" className={inputCls} />
                    <button type="button" onClick={() => setParola(uretGucluParola())} className="whitespace-nowrap px-3 py-2 bg-white dark:bg-slate-800 border border-brand-600 text-brand-700 dark:text-brand-300 hover:bg-brand-50 dark:hover:bg-brand-900/30 text-sm rounded-md">Üret</button>
                  </div>
                  {parolaGucSorunu && <p className="mt-1 text-xs text-amber-600 dark:text-amber-400">Parola en az 12 karakter ve harf+rakam karışık olmalı.</p>}
                </div>
              )}
            </div>
          )}

          {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-700 dark:text-red-300">{hata}</div>}

          <div className="flex justify-end gap-2 pt-1">
            <button onClick={onKapat} disabled={isleniyor} className="px-4 py-2 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 rounded-md text-sm">İptal</button>
            <button onClick={olustur} disabled={isleniyor} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">{isleniyor ? 'Oluşturuluyor…' : 'Oluştur'}</button>
          </div>
        </div>
      )}
    </Modal>
  )
}

function SonucSatir({ e, v }: { e: string; v: string }) {
  const [ok, setOk] = useState(false)
  return (
    <div className="flex items-center gap-2">
      <span className="w-24 shrink-0 text-xs text-emerald-700 dark:text-emerald-300">{e}</span>
      <code className="flex-1 bg-white dark:bg-slate-800 px-3 py-1.5 font-mono text-sm text-slate-900 dark:text-slate-100 rounded border border-emerald-200 dark:border-emerald-800 break-all">{v}</code>
      <button onClick={() => { navigator.clipboard.writeText(v); setOk(true); setTimeout(() => setOk(false), 1500) }} className="px-2.5 py-1.5 bg-emerald-100 dark:bg-emerald-900/30 hover:bg-emerald-200 text-emerald-800 dark:text-emerald-200 text-xs rounded">{ok ? '✓' : 'Kopyala'}</button>
    </div>
  )
}

// PwResetModal — DB parolasını sıfırlar (rastgele/özel). Detay sayfası da kullanır.
export function PwResetModal({ db, onKapat, onTamam }: { db: DB; onKapat: () => void; onTamam: () => void }) {
  const [ozelPw, setOzelPw] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [yeniPw, setYeniPw] = useState<string | null>(null)

  async function sifirla(rastgele: boolean) {
    if (!rastgele && ozelPw.length < 6) {
      setHata('Parola en az 6 karakter olmalı')
      return
    }
    setIsleniyor(true); setHata(null)
    try {
      const body = rastgele ? {} : { parola: ozelPw }
      const { data } = await api.put(`/databases/${db.id}/password`, body)
      setYeniPw(data.db_parola)
    } catch (e) {
      setHata(apiHata(e, 'Sıfırlama başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={`Parola Sıfırla — ${db.db_adi}`} onKapat={yeniPw ? onTamam : onKapat} genislik="md">
      {!yeniPw ? (
        <div className="space-y-4">
          <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
            <strong className="font-mono">{db.db_kullanici}</strong> kullanıcısının parolası MariaDB ve panel'de eşzamanlı güncellenir.
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Özel parola (boş bırakırsanız rastgele)</label>
            <input
              type="text"
              value={ozelPw}
              onChange={e => setOzelPw(e.target.value)}
              placeholder="En az 6 karakter"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
            />
          </div>
          {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-700 dark:text-red-300">{hata}</div>}
          <div className="flex justify-end gap-2 pt-2">
            <button onClick={onKapat} disabled={isleniyor} className="px-4 py-2 border border-slate-200 dark:border-slate-700 rounded-md text-sm">İptal</button>
            <button onClick={() => sifirla(false)} disabled={isleniyor || !ozelPw} className="px-4 py-2 bg-white dark:bg-slate-800 border border-brand-600 text-brand-700 dark:text-brand-300 hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 disabled:opacity-50 rounded-md text-sm">Bunu Ayarla</button>
            <button onClick={() => sifirla(true)} disabled={isleniyor} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">{isleniyor ? 'Sıfırlanıyor…' : 'Rastgele Üret'}</button>
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <div className="bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md p-4">
            <p className="text-sm text-emerald-800 dark:text-emerald-200 font-medium mb-2">✓ Parola güncellendi</p>
            <p className="text-xs text-emerald-700 dark:text-emerald-300 mb-2">Bunu güvenli bir yere kaydedin. Sonra göremezsiniz:</p>
            <div className="flex flex-wrap items-center gap-2">
              <code className="flex-1 bg-white dark:bg-slate-800 px-3 py-2 font-mono text-sm text-slate-900 dark:text-slate-100 rounded border border-emerald-200 dark:border-emerald-800 break-all">{yeniPw}</code>
              <button onClick={() => navigator.clipboard.writeText(yeniPw)} className="px-3 py-2 bg-emerald-100 dark:bg-emerald-900/30 hover:bg-emerald-200 text-emerald-800 dark:text-emerald-200 text-xs rounded">Kopyala</button>
            </div>
          </div>
          <div className="flex justify-end">
            <button onClick={onTamam} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm rounded-md">Tamam</button>
          </div>
        </div>
      )}
    </Modal>
  )
}
