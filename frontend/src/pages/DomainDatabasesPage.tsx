import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import { Button, Toggle } from '@/components/ui'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { T } from '@/lib/tablo'
import { useToast } from '@/components/Toast'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
export type DB = {
  id: number; domain_id: number; db_adi: string; db_kullanici: string;
  db_host: string; db_parola: string; olusturulma: string; boyut: number
}


const DB_EN: Record<string, string> = {
  "(boş bırakırsanız panel üretir)": "(leave empty and the panel generates one)",
  "İşlem başarısız": "Operation failed",
  "Alan adı bilgisi alınamadı": "Failed to get domain info",
  "Bilgileri güvenli bir yere kaydedin. Parolayı sonra düz metin göremeyebilirsiniz:": "Save this info somewhere safe. You may not be able to see the password in plain text later:",
  "Henüz veritabanı yok": "No databases yet",
  "Kullanıcı": "User",
  "Kullanıcı adı çok uzun (önek + sonek ≤64 karakter olmalı)": "Username too long (prefix + suffix must be ≤64 characters)",
  "Kullanıcı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter": "User suffix: lowercase letters/digits/underscore only, 1-32 characters",
  "Mevcut bir kullanıcı seçin": "Select an existing user",
  "Oluştur": "Create",
  "Oluşturma başarısız": "Creation failed",
  "Oluşturulma": "Created",
  "Oluşturuluyor…": "Creating…",
  "Parola en az 12 karakter ve harf+rakam karışık olmalı": "Password must be at least 12 characters and mix letters+digits",
  "Parola en az 12 karakter ve harf+rakam karışık olmalı.": "Password must be at least 12 characters and mix letters+digits.",
  "Parola en az 6 karakter olmalı": "Password must be at least 6 characters",
  "Rastgele Üret": "Generate Random",
  "Sıfırlama başarısız": "Reset failed",
  "Sıfırlanıyor…": "Resetting…",
  "Veritabanları": "Databases",
  "Veritabanı": "Database",
  "Veritabanı adı": "Database name",
  "Veritabanı adı çok uzun (önek + sonek ≤64 karakter olmalı)": "Database name too long (prefix + suffix must be ≤64 characters)",
  "Veritabanı kullanıcısı": "Database user",
  "Veritabanı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter": "Database suffix: lowercase letters/digits/underscore only, 1-32 characters",
  "Yeni Veritabanı": "New Database",
  "Yükleniyor…": "Loading…",
  "Özel parola (boş bırakırsanız rastgele)": "Custom password (random if left empty)",
  "Üret": "Generate",
  "İptal": "Cancel",
  "Parola güncellendi": "Password updated",
  "Kullanıcı oluşturuldu": "User created",
  "Parolayı Güncelle": "Update Password",
  "Veritabanı oluşturuldu": "Database created",
  "Anasayfa": "Home",
  "Yenile": "Refresh",
  "veritabanı": "databases",
  "Boyut": "Size",
  "Parola": "Password",
  "Tamam": "OK",
  "Otomatik": "Automatic",
  " — DB adı, kullanıcı ve parolayı panel üretsin": " — let the panel generate the DB name, user and password",
  "Yeni kullanıcı": "New user",
  "Mevcut kullanıcı seç": "Select existing user",
  "En az 12 karakter, harf+rakam": "At least 12 characters, letters+digits",
  "Kopyala": "Copy",
  " kullanıcısının parolası MariaDB ve panel'de eşzamanlı güncellenir.": "'s password is updated simultaneously in MariaDB and the panel.",
  "En az 6 karakter": "At least 6 characters",
  "Bunu Ayarla": "Set This",
  "Parola Sıfırla — {0}": "Reset Password — {0}",
  "Bunu güvenli bir yere kaydedin. Sonra göremezsiniz:": "Save this somewhere safe. You won't be able to see it later:",
  "Yalnız panel kaydını güncelle (MySQL parolası değişmez)": "Update the panel record only (MySQL password is not changed)",
  "Parola panel dışından değiştirildiyse (ör. uygulama kurulumu sırasında) uygulamanın yapılandırma dosyasındaki MEVCUT parolayı girin (XenForo: src/config.php, WordPress: wp-config.php). Panel parolayı MySQL'de doğrular ve yalnız kendi kaydını günceller — site etkilenmez.": "If the password was changed outside the panel (e.g. during an application install), enter the CURRENT password from the application's configuration file (XenForo: src/config.php, WordPress: wp-config.php). The panel verifies it against MySQL and updates only its own record — the site is not affected.",
  "Panel Kaydını Güncelle — {0}": "Update Panel Record — {0}",
  "Mevcut parola": "Current password",
  "Mevcut parolayı girin": "Enter the current password",
  "Doğrula ve Kaydet": "Verify and Save",
  "Panel kaydı güncellendi": "Panel record updated",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DB_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export function boyutFmt(b: number): string {
  if (!b || b <= 0) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0, v = b
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return v.toFixed(v < 10 && i > 0 ? 1 : 0) + ' ' + u[i]
}

export default function DomainDatabasesPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const nav = useNavigate()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [dbler, setDbler] = useState<DB[]>([])
  const [yuk, setYuk] = useState(true)
  const [, setHata] = useState<string | null>(null)
  const [ekleAcik, setEkleAcik] = useState(false)

  const yukleNesli = useRef(0)
  function yukle() {
    if (!id) return
    setYuk(true)
    const _n = ++yukleNesli.current
    api.get<DB[]>(`/domains/${id}/databases`)
      .then(r => { if (_n !== yukleNesli.current) return; setDbler(r.data) })
      .catch(e => { if (_n !== yukleNesli.current) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => { if (_n === yukleNesli.current) setYuk(false) })
  }

  useEffect(() => {
    let iptal = false
    if (id) api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("Alan adı bilgisi alınamadı"))(e) })
    yukle()
    return () => { iptal = true; yukleNesli.current++ }
  }, [id])

  // Domain'in mevcut DB-kullanıcıları (mevcut-kullanıcı seçimi için, benzersiz).
  const mevcutKullanicilar = useMemo(
    () => Array.from(new Set(dbler.map(d => d.db_kullanici))),
    [dbler],
  )

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Veritabanları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Veritabanları")}</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5"><Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link></p>}

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <Button onClick={() => setEkleAcik(true)} color="primary" ikon={I.arti}>{cevir("Yeni Veritabanı")}</Button>
        <Button onClick={yukle} variant="outlined" ikon={I.yenile} yukleniyor={yuk}>{cevir("Yenile")}</Button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{dbler.length} {cevir("veritabanı")}</span>
      </div>

      {/* Sadeleştirilmiş liste: kullanıcı/sunucu/parola/işlemler her DB'nin KENDİ
          detay sayfasında. Böylece tablo dar kalır, yatay scroll gerekmez. */}
      <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
        {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> :
         dbler.length === 0 ? <div className="py-12 text-center text-sm text-slate-500 dark:text-slate-500">{cevir("Henüz veritabanı yok")}</div> :
        <div className="lg:overflow-x-auto">
          <table className={T.tablo}>
          <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-dark-600`}>
            <tr>
              <th className={T.baslik}>{cevir("Veritabanı")}</th>
              <th className={T.baslik}>{cevir("Oluşturulma")}</th>
              <th className={`${T.baslik} text-right`}>{cevir("Boyut")}</th>
              <th className={`${T.baslik} text-right`}></th>
            </tr>
          </thead>
          <tbody className={T.govde}>
            {dbler.map(d => (
              <tr
                key={d.id}
                onClick={() => nav(`/abonelikler/${id}/veritabanlari/${d.id}`)}
                className={`${T.satir} cursor-pointer lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700`}
              >
                <td className={`${T.hucreBaslik} font-mono`}><span className="text-brand-700 dark:text-brand-300">{d.db_adi}</span></td>
                <td className={T.hucre} data-etiket={cevir("Oluşturulma")}>
                  <span className="text-slate-600 dark:text-slate-400 whitespace-nowrap">{d.olusturulma}</span>
                </td>
                <td className={`${T.hucre} text-right`} data-etiket={cevir("Boyut")}><span className="font-mono tabular-nums text-slate-700 dark:text-slate-300 whitespace-nowrap">{boyutFmt(d.boyut)}</span></td>
                <td className={`${T.hucreAksiyon} lg:text-right`}>
                  <span className="inline-flex items-center gap-1 text-sm text-slate-500 dark:text-slate-400">{cevir("Yönet")}
                    <Ikon d={I.chevronSag} className="size-4 shrink-0" />
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
  ;(window.crypto || (window as unknown as { msCrypto: Crypto }).msCrypto).getRandomValues(buf)
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
  const toast = useToast()
  // DB oneki cPanel gibi c_ ONEKSIZ (backend TrimPrefix ile ayni): c_girgin -> girgin_
  const onek = sk.replace(/^c_/, '') + '_'
  const [otomatik, setOtomatik] = useState(true)
  const [dbSonek, setDbSonek] = useState('')
  const [kullaniciTipi, setKullaniciTipi] = useState<'yeni' | 'mevcut'>('yeni')
  const [kullaniciSonek, setKullaniciSonek] = useState('')
  const [mevcutKullanici, setMevcutKullanici] = useState(mevcutKullanicilar[0] || '')
  const [parola, setParola] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [, setHata] = useState<string | null>(null)
  const [sonuc, setSonuc] = useState<{ db_adi: string; db_kullanici: string; db_parola: string } | null>(null)

  const dbAdiOnizleme = onek + (dbSonek || '…')
  const kullaniciOnizleme = onek + (kullaniciSonek || '…')
  const parolaGucSorunu =
    parola !== '' && (parola.length < 12 || !/[A-Za-z]/.test(parola) || !/[0-9]/.test(parola))

  function yerelDogrula(): string | null {
    if (otomatik) return null
    if (!SONEK_RE.test(dbSonek)) return cevir("Veritabanı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter")
    if ((onek + dbSonek).length > 64) return cevir("Veritabanı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
    if (kullaniciTipi === 'yeni') {
      if (!SONEK_RE.test(kullaniciSonek)) return cevir("Kullanıcı soneki: yalnız küçük harf/rakam/alt-çizgi, 1-32 karakter")
      if ((onek + kullaniciSonek).length > 64) return cevir("Kullanıcı adı çok uzun (önek + sonek ≤64 karakter olmalı)")
      if (parola !== '' && parolaGucSorunu) return cevir("Parola en az 12 karakter ve harf+rakam karışık olmalı")
    } else {
      if (!mevcutKullanici) return cevir("Mevcut bir kullanıcı seçin")
    }
    return null
  }

  async function olustur() {
    const y = yerelDogrula()
    if (y) { setHata(y); toast.hata(cevir("İşlem başarısız"), y); return }
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
      const { data } = await api.post(`/domains/${domainId}/databases`, body, UZUN_ISTEK)
      setSonuc({ db_adi: data.db_adi, db_kullanici: data.db_kullanici, db_parola: data.db_parola })
    } catch (e) {
      const m = apiHata(e, cevir("Oluşturma başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  const inputCls = 'w-full px-3 py-2 border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-slate-900 dark:text-slate-100 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none disabled:opacity-50'

  return (
    <Modal acik={true} baslik={cevir("Yeni Veritabanı")} onKapat={sonuc ? onTamam : onKapat} genislik="lg"
      ton={sonuc ? 'basari' : undefined} kapatilamaz={isleniyor}
      alt={sonuc
        ? <Button onClick={onTamam} color="primary" ikon={I.onay}>{cevir("Tamam")}</Button>
        : <>
            <Button onClick={onKapat} disabled={isleniyor} variant="outlined" ikon={I.kapat}>{cevir("İptal")}</Button>
            <Button onClick={olustur} yukleniyor={isleniyor} color="primary" ikon={I.arti}>{isleniyor ? cevir("Oluşturuluyor…") : cevir("Oluştur")}</Button>
          </>}>
      {sonuc ? (
        <UyariKutusu ton="basari" baslik={cevir("Veritabanı oluşturuldu")}>
          <div className="space-y-3">
            <p className="text-xs">{cevir("Bilgileri güvenli bir yere kaydedin. Parolayı sonra düz metin göremeyebilirsiniz:")}</p>
            <SonucSatir e={cevir("Veritabanı")} v={sonuc.db_adi} />
            <SonucSatir e={cevir("Kullanıcı")} v={sonuc.db_kullanici} />
            <SonucSatir e={cevir("Parola")} v={sonuc.db_parola} />
          </div>
        </UyariKutusu>
      ) : (
        <div className="space-y-5">
          {/* Otomatik toggle */}
          <label className="flex items-center gap-3 cursor-pointer select-none">
            <input type="checkbox" checked={otomatik} onChange={e => setOtomatik(e.target.checked)} className="h-4 w-4 accent-brand-600" />
            <span className="text-sm text-slate-700 dark:text-slate-300">
              <strong className="font-medium">{cevir("Otomatik")}</strong>{cevir(" — DB adı, kullanıcı ve parolayı panel üretsin")}
            </span>
          </label>

          {!otomatik && (
            <div className="space-y-5 pt-1">
              {/* DB adı */}
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Veritabanı adı")}</label>
                <div className="flex items-stretch">
                  <span className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-slate-300 dark:border-slate-600 bg-slate-100 dark:bg-dark-700 text-slate-500 dark:text-slate-400 text-sm font-mono select-none">{onek}</span>
                  <input value={dbSonek} onChange={e => setDbSonek(e.target.value.toLowerCase())} placeholder="blog" className={inputCls + ' rounded-l-none'} />
                </div>
                <p className="mt-1 flex items-center gap-1 text-xs text-slate-400 dark:text-slate-500 font-mono"><Ikon d={I.ileri} className="size-3 shrink-0" />{dbAdiOnizleme}</p>
              </div>

              {/* DB kullanıcısı */}
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1.5">{cevir("Veritabanı kullanıcısı")}</label>
                <div className="flex gap-4 mb-2">
                  <label className="flex items-center gap-1.5 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
                    <input type="radio" name="kullaniciTipi" checked={kullaniciTipi === 'yeni'} onChange={() => setKullaniciTipi('yeni')} className="accent-brand-600" />
                    {cevir("Yeni kullanıcı")}
                  </label>
                  <label className={'flex items-center gap-1.5 text-sm cursor-pointer ' + (mevcutKullanicilar.length ? 'text-slate-700 dark:text-slate-300' : 'text-slate-400 dark:text-slate-600 cursor-not-allowed')}>
                    <input type="radio" name="kullaniciTipi" disabled={!mevcutKullanicilar.length} checked={kullaniciTipi === 'mevcut'} onChange={() => setKullaniciTipi('mevcut')} className="accent-brand-600" />
                    {cevir("Mevcut kullanıcı seç")}
                  </label>
                </div>

                {kullaniciTipi === 'yeni' ? (
                  <>
                    <div className="flex items-stretch">
                      <span className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-slate-300 dark:border-slate-600 bg-slate-100 dark:bg-dark-700 text-slate-500 dark:text-slate-400 text-sm font-mono select-none">{onek}</span>
                      <input value={kullaniciSonek} onChange={e => setKullaniciSonek(e.target.value.toLowerCase())} placeholder="bloguser" className={inputCls + ' rounded-l-none'} />
                    </div>
                    <p className="mt-1 flex items-center gap-1 text-xs text-slate-400 dark:text-slate-500 font-mono"><Ikon d={I.ileri} className="size-3 shrink-0" />{kullaniciOnizleme}</p>
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
                  <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Parola")} <span className="text-slate-400 dark:text-slate-500">{cevir("(boş bırakırsanız panel üretir)")}</span></label>
                  <div className="flex gap-2">
                    <input type="text" value={parola} onChange={e => setParola(e.target.value)} placeholder={cevir("En az 12 karakter, harf+rakam")} className={inputCls} />
                    <Button onClick={() => setParola(uretGucluParola())} variant="outlined" ikon={I.anahtar} className="whitespace-nowrap">{cevir("Üret")}</Button>
                  </div>
                  {parolaGucSorunu && <p className="mt-1 text-xs text-amber-600 dark:text-amber-400">{cevir("Parola en az 12 karakter ve harf+rakam karışık olmalı.")}</p>}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

function SonucSatir({ e, v }: { e: string; v: string }) {
  const [ok, setOk] = useState(false)
  return (
    <div className="flex items-center gap-2">
      <span className="w-24 shrink-0 text-xs text-slate-500 dark:text-dark-200">{e}</span>
      <code className="flex-1 bg-white dark:bg-dark-700 px-3 py-1.5 font-mono text-sm text-slate-900 dark:text-slate-100 rounded border border-slate-200 dark:border-dark-500 break-all">{v}</code>
      <Button onClick={() => { navigator.clipboard.writeText(v); setOk(true); setTimeout(() => setOk(false), 1500) }} color={ok ? 'success' : 'neutral'} variant="outlined" boyut="sm" ikon={ok ? I.onay : I.kopya} className="shrink-0">{cevir("Kopyala")}</Button>
    </div>
  )
}

// PwResetModal — DB parolasını sıfırlar (rastgele/özel). Detay sayfası da kullanır.
export function PwResetModal({ db, onKapat, onTamam, yalnizKayitIle = false }: { db: DB; onKapat: () => void; onTamam: () => void; yalnizKayitIle?: boolean }) {
  const toast = useToast()
  const [ozelPw, setOzelPw] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [, setHata] = useState<string | null>(null)
  // 🔴 Kullanicisi OLMAYAN veritabani: DB panelden silinip yedekten geri
  // yuklendiginde olusur — yedek yalniz sema+veri icerir, MySQL kullanicisi ve
  // GRANT'ler arsivde bulunmaz. Panel her DB'nin kullanicisi oldugunu varsayiyor
  // ve bu durumda kullanici olusturmanin HICBIR yolu yoktu (site baglanamiyordu).
  const kullaniciYok = !db.db_kullanici
  const [yeniKullanici, setYeniKullanici] = useState('')
  // 🔴 Yalnız panel kaydı: parola panel dışından değiştiğinde (kayıt bayat → phpMyAdmin
  // "Access denied", 2026-10-12 .180) MariaDB'ye DOKUNMADAN kaydı MEVCUT parolaya eşitler.
  // "Parola Sıfırla" (ALTER USER) çalışan sitenin yapılandırmasındaki parolayı geçersiz kılardı.
  const [yalnizKayit, setYalnizKayit] = useState(yalnizKayitIle && !kullaniciYok)
  const asgari = yalnizKayit ? 1 : 6

  async function sifirla() {
    if (ozelPw.length < asgari) {
      const m = cevir(yalnizKayit ? "Mevcut parolayı girin" : "Parola en az 6 karakter olmalı")
      setHata(m); toast.hata(m)
      return
    }
    if (kullaniciYok && !yeniKullanici.trim()) {
      setHata(cevir("Kullanıcı adı girin")); toast.hata(cevir("Kullanıcı adı girin"))
      return
    }
    setIsleniyor(true); setHata(null)
    try {
      const body: Record<string, string | boolean> = { parola: ozelPw }
      if (kullaniciYok) body.kullanici = yeniKullanici.trim()
      if (yalnizKayit) body.yalniz_kayit = true
      await api.put(`/databases/${db.id}/password`, body)
      toast.basari(cevir(kullaniciYok ? "Kullanıcı oluşturuldu" : yalnizKayit ? "Panel kaydı güncellendi" : "Parola güncellendi"))
      onTamam()
    } catch (e) {
      const m = apiHata(e, cevir("Sıfırlama başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={cevirT(cevir(kullaniciYok ? "Kullanıcı Oluştur — {0}" : yalnizKayit ? "Panel Kaydını Güncelle — {0}" : "Parola Sıfırla — {0}"), db.db_adi)} onKapat={onKapat} genislik="md"
      kapatilamaz={isleniyor}
      alt={<>
        <Button onClick={onKapat} disabled={isleniyor} variant="outlined" ikon={I.kapat}>{cevir("İptal")}</Button>
        <Button onClick={() => sifirla()} yukleniyor={isleniyor} disabled={ozelPw.length < asgari} color="primary" ikon={kullaniciYok ? I.arti : I.disket}>{isleniyor ? cevir("Sıfırlanıyor…") : (kullaniciYok ? cevir("Oluştur") : yalnizKayit ? cevir("Doğrula ve Kaydet") : cevir("Parolayı Güncelle"))}</Button>
      </>}>
        <div className="space-y-4">
          {kullaniciYok ? (
            <>
              <UyariKutusu ton="uyari">
                {cevir("Bu veritabanının MySQL kullanıcısı yok. Yedekten geri yüklenen veritabanlarında olur — yedek yalnızca şema ve veri içerir, kullanıcı ile yetkiler içermez.")}
              </UyariKutusu>
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1" htmlFor="yeni-db-kullanici">{cevir("Kullanıcı adı")}</label>
                <input
                  id="yeni-db-kullanici"
                  value={yeniKullanici}
                  onChange={e => setYeniKullanici(e.target.value)}
                  placeholder={cevir("uygulamanın beklediği kullanıcı adı")}
                  className="w-full px-3 py-2 text-sm font-mono rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800/40 text-slate-900 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
                />
                <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                  {cevir("Site zaten kuruluysa uygulamanın yapılandırma dosyasındaki kullanıcı adını girin (WordPress: wp-config.php → DB_USER) — böylece dosyayı düzenlemeden bağlanır.")}
                </p>
              </div>
            </>
          ) : (
            <>
              <label className="flex items-center gap-2.5 text-sm text-slate-700 dark:text-slate-300">
                <Toggle acik={yalnizKayit} onDegis={setYalnizKayit} renk="brand" disabled={isleniyor}
                  aria-label={cevir("Yalnız panel kaydını güncelle (MySQL parolası değişmez)")} />
                <span>{cevir("Yalnız panel kaydını güncelle (MySQL parolası değişmez)")}</span>
              </label>
              {yalnizKayit ? (
                <UyariKutusu ton="bilgi">
                  {cevir("Parola panel dışından değiştirildiyse (ör. uygulama kurulumu sırasında) uygulamanın yapılandırma dosyasındaki MEVCUT parolayı girin (XenForo: src/config.php, WordPress: wp-config.php). Panel parolayı MySQL'de doğrular ve yalnız kendi kaydını günceller — site etkilenmez.")}
                </UyariKutusu>
              ) : (
                <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
                  <strong className="font-mono">{db.db_kullanici}</strong>{cevir(" kullanıcısının parolası MariaDB ve panel'de eşzamanlı güncellenir.")}
                </div>
              )}
            </>
          )}
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Parola")}</label>
            <input
              type="text"
              value={ozelPw}
              onChange={e => setOzelPw(e.target.value)}
              placeholder={cevir(yalnizKayit ? "Mevcut parola" : "En az 6 karakter")}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
            />
          </div>
        </div>
    </Modal>
  )
}
