import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useAuth } from '@/store/auth'
import { Button, Badge, butonSinifi } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { musteriDomainID, useRol } from '@/lib/rol'

type Ben = {
  id: number; adi: string; rol: string; eposta: string; ad_soyad: string
  durum: string; iki_fa: boolean; tercih_tema: string; tercih_dil: string
}


const SETT_EN: Record<string, string> = {
  "ornek@site.com": "name@example.com",
  "1) Authenticator uygulamanıza (Google Authenticator, Authy, Microsoft Authenticator) ekleyin:": "1) Add to your authenticator app (Google Authenticator, Authy, Microsoft Authenticator):",
  "2FA'yı Etkinleştir": "Enable 2FA",
  "2FA'yı Kapat": "Disable 2FA",
  "Ad soyad ve e-posta adresinizi düzenleyin.": "Edit your full name and email address.",
  "Adınız Soyadınız": "Your Full Name",
  "Authenticator uygulamanızla tarayın": "Scan with your authenticator app",
  "Bu, sunucunun root parolasını değiştirir (SSH erişimi dahil).": "This changes the server's root password (including SSH access).",
  "Doğrula ve Aç": "Verify and Enable",
  "Girişte parolaya ek olarak authenticator uygulamasından 6 haneli kod ister.": "At login, requires a 6-digit code from the authenticator app in addition to the password.",
  "Hesap bilgileriniz, parola, iki adımlı doğrulama ve panel tercihleri.": "Your account info, password, two-factor authentication and panel preferences.",
  "Kapatmak için authenticator kodunu girin:": "Enter the authenticator code to disable:",
  "Kod doğrulanamadı": "Code could not be verified",
  "Panel görünüm ve dil tercihleri.": "Panel appearance and language preferences.",
  "Parola değiştirildi. (Sunucu root parolası güncellendi.)": "Password changed. (Server root password updated.)",
  "Parola değiştirilemedi": "Failed to change password",
  "Parolayı Değiştir": "Change Password",
  "Tarayamıyorsanız, elle giriş için gizli anahtar:": "If you can't scan, secret key for manual entry:",
  "Türkçe": "Turkish",
  "Yeni parola en az 8 karakter olmalı.": "The new password must be at least 8 characters.",
  "Yeni parolalar eşleşmiyor.": "The new passwords do not match.",
  "İki Adımlı Doğrulama (2FA)": "Two-Factor Authentication (2FA)",
  "Kapalı": "Off",
  "Kaydedilemedi": "Could not save",
  "Bilgiler kaydedildi.": "Information saved.",
  "Tercihler kaydedildi.": "Preferences saved.",
  "Anasayfa": "Home",
  "Profil ve Tercihler": "Profile and Preferences",
  "Hesap Bilgileri": "Account Information",
  "Rol / Durum": "Role / Status",
  "Ad Soyad": "Full Name",
  "Kaydediliyor…": "Saving…",
  "Kaydet": "Save",
  "Mevcut parola": "Current password",
  "Yeni parola": "New password",
  "Yeni parola (tekrar)": "New password (again)",
  "Değiştiriliyor…": "Changing…",
  "Durum:": "Status:",
  "Aktif": "Active",
  "2FA QR kodu": "2FA QR code",
  "2) Uygulamadaki 6 haneli kodu girin:": "2) Enter the 6-digit code from the app:",
  "Doğrulanıyor…": "Verifying…",
  "Tercihler": "Preferences",
  "Tema": "Theme",
  "Koyu": "Dark",
  "Dil": "Language",
  "Tercihleri Kaydet": "Save Preferences",
  "Parola": "Password",
  "Bu, panel giriş parolanızı değiştirir.": "This changes your panel login password.",
  "Parola değiştirildi.": "Password changed.",
  "Hesap bilgileriniz ve panel giriş parolanız.": "Your account information and panel login password.",
  "Hosting kullanıcısı": "Hosting user",
  "Panel giriş parolanız, aboneliğinizin FTP hesabının parolasıdır; FTP Hesapları sayfasından değiştirilir.": "Your panel login password is the password of your subscription's FTP account; it is changed on the FTP Accounts page.",
  "FTP Hesaplarına git": "Go to FTP accounts",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SETT_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function Kart({ baslik, aciklama, ikon, cocuk }: { baslik: string; aciklama?: string; ikon: React.ReactNode; cocuk: React.ReactNode }) {
  return (
    <section className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-6 shadow-xs">
      <div className="flex items-start gap-3 mb-5">
        <div className="w-10 h-10 rounded-lg bg-brand-50 dark:bg-brand-900/30 text-brand-600 dark:text-brand-400 flex items-center justify-center shrink-0">{ikon}</div>
        <div>
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{baslik}</h2>
          {aciklama && <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">{aciklama}</p>}
        </div>
      </div>
      {cocuk}
    </section>
  )
}

function Girdi({ etiket, ...p }: { etiket: string } & React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{etiket}</span>
      <input {...p} className="w-full px-3 py-2 text-sm bg-white dark:bg-dark-800 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none disabled:opacity-60 disabled:bg-slate-100 dark:disabled:bg-dark-700" />
    </label>
  )
}

// Sayfa içi sonuç kutusu — ortak ton dili (UyariKutusu): ok → basari, err → tehlike.
function Uyari({ tip, mesaj }: { tip: 'ok' | 'err'; mesaj: string }) {
  if (!mesaj) return null
  return <UyariKutusu ton={tip === 'ok' ? 'basari' : 'tehlike'} className="py-2">{mesaj}</UyariKutusu>
}

export default function SettingsPage() {
  useTranslation() // dil re-render aboneligi
  const guncelleAd = useAuth((s) => s.guncelleAd)
  // Rol tek kaynaktan (useAuth kullanici.rol). /me'nin 'rol' alanı yalnız rozet metni.
  //  - admin: parola = sunucunun root parolası (auth/profile.go)
  //  - bayi: arka uç root'a DOKUNMAZ, yalnız bayinin panel parolası değişir
  //  - müşteri: PUT /me, /me/parola, /me/2fa/* AdminVeyaReseller → 403. Müşterinin panel
  //    parolası FTP hesabının parolasıdır (musteri/login ftp_accounts'u doğrular) ve
  //    FTP sayfasındaki PUT /domains/{id}/ftp/password ile değişir. O uç mevcut parolayı
  //    SORMAZ; buradaki "Mevcut parola" formunu ona bağlamak yanıltıcı olurdu — bu
  //    yüzden müşteride çalışmayan formlar gizlenir, parola için FTP sayfasına yönlendirilir.
  const rol = useRol()
  const musteri = rol === 'user'
  const rootMu = rol === 'admin'
  const musteriDID = musteri ? musteriDomainID() : null
  const [ben, setBen] = useState<Ben | null>(null)
  const [yukHata, setYukHata] = useState('')

  const [ad, setAd] = useState(''); const [eposta, setEposta] = useState('')
  const [pOk, setPOk] = useState(''); const [pErr, setPErr] = useState(''); const [pYuk, setPYuk] = useState(false)

  const [mevcut, setMevcut] = useState(''); const [yeni, setYeni] = useState(''); const [yeni2, setYeni2] = useState('')
  const [paOk, setPaOk] = useState(''); const [paErr, setPaErr] = useState(''); const [paYuk, setPaYuk] = useState(false)

  const [f2Kur, setF2Kur] = useState<{ secret: string; otpauth: string; otpauth_uri?: string; qr_data_uri?: string } | null>(null)
  const [f2Kod, setF2Kod] = useState(''); const [f2Err, setF2Err] = useState(''); const [f2Yuk, setF2Yuk] = useState(false)
  const [f2Kapat, setF2Kapat] = useState(false); const [kapatKod, setKapatKod] = useState('')

  const [tema, setTema] = useState('system'); const [dil, setDil] = useState('tr')
  const [tOk, setTOk] = useState(''); const [tErr, setTErr] = useState(''); const [tYuk, setTYuk] = useState(false)

  function yukle() {
    api.get<Ben>('/me').then(r => {
      setBen(r.data); setAd(r.data.ad_soyad || ''); setEposta(r.data.eposta || '')
      setTema(r.data.tercih_tema || 'system'); setDil(r.data.tercih_dil || 'tr')
    }).catch(e => setYukHata(apiHata(e)))
  }
  useEffect(yukle, [])

  async function profilKaydet(e: React.FormEvent) {
    e.preventDefault(); setPOk(''); setPErr(''); setPYuk(true)
    try {
      await api.put('/me', { ad_soyad: ad, eposta })
      guncelleAd(ad) // sağ üst bar dinamik güncellensin
      setPOk(cevir("Bilgiler kaydedildi.")); setTimeout(() => setPOk(''), 3000); yukle()
    } catch (e) { setPErr(apiHata(e, cevir("Kaydedilemedi"))) } finally { setPYuk(false) }
  }

  async function parolaDegistir(e: React.FormEvent) {
    e.preventDefault(); setPaOk(''); setPaErr('')
    if (yeni.length < 8) { setPaErr(cevir("Yeni parola en az 8 karakter olmalı.")); return }
    if (yeni !== yeni2) { setPaErr(cevir("Yeni parolalar eşleşmiyor.")); return }
    setPaYuk(true)
    try {
      await api.post('/me/parola', { mevcut, yeni })
      setPaOk(rootMu ? cevir("Parola değiştirildi. (Sunucu root parolası güncellendi.)") : cevir("Parola değiştirildi."))
      setMevcut(''); setYeni(''); setYeni2(''); setTimeout(() => setPaOk(''), 5000)
    } catch (e) { setPaErr(apiHata(e, cevir("Parola değiştirilemedi"))) } finally { setPaYuk(false) }
  }

  async function f2Baslat() {
    setF2Err(''); setF2Kod('')
    try { const r = await api.get<{ secret: string; otpauth: string; otpauth_uri?: string; qr_data_uri?: string }>('/me/2fa/setup'); setF2Kur(r.data) }
    catch (e) { setF2Err(apiHata(e)) }
  }
  async function f2Etkinlestir(e: React.FormEvent) {
    e.preventDefault(); setF2Err(''); setF2Yuk(true)
    try {
      await api.post('/me/2fa/enable', { secret: f2Kur!.secret, kod: f2Kod })
      setF2Kur(null); setF2Kod(''); yukle()
    } catch (e) { setF2Err(apiHata(e, cevir("Kod doğrulanamadı"))) } finally { setF2Yuk(false) }
  }
  async function f2KapatOnay(e: React.FormEvent) {
    e.preventDefault(); setF2Err(''); setF2Yuk(true)
    try { await api.post('/me/2fa/disable', { kod: kapatKod }); setF2Kapat(false); setKapatKod(''); yukle() }
    catch (e) { setF2Err(apiHata(e, cevir("Kod doğrulanamadı"))) } finally { setF2Yuk(false) }
  }

  async function tercihKaydet() {
    setTOk(''); setTErr(''); setTYuk(true)
    try {
      await api.put('/me', { ad_soyad: ad, eposta, tercih_tema: tema, tercih_dil: dil })
      try { localStorage.setItem('gosp.tema', tema) } catch { /* yoksay */ }
      setTOk(cevir("Tercihler kaydedildi.")); setTimeout(() => setTOk(''), 3000)
    } catch (e) { setTErr(apiHata(e, cevir("Kaydedilemedi"))) } finally { setTYuk(false) }
  }

  const secretGruplu = f2Kur ? (f2Kur.secret.match(/.{1,4}/g) || []).join(' ') : ''

  return (
    <div className="px-6 md:px-8 py-6">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Profil ve Tercihler") }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Profil ve Tercihler")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">{musteri ? cevir("Hesap bilgileriniz ve panel giriş parolanız.") : cevir("Hesap bilgileriniz, parola, iki adımlı doğrulama ve panel tercihleri.")}</p>
      {yukHata && <div className="mb-4"><Uyari tip="err" mesaj={yukHata} /></div>}

      <div className="space-y-5">
        {/* 1) Hesap Bilgileri */}
        <Kart baslik={cevir("Hesap Bilgileri")} aciklama={musteri ? undefined : cevir("Ad soyad ve e-posta adresinizi düzenleyin.")}
          ikon={<Ikon d={I.kullanici} className="size-5" />}
          cocuk={
            <form onSubmit={profilKaydet} className="space-y-4">
              <div className="grid sm:grid-cols-2 gap-4">
                <Girdi etiket={cevir("Kullanıcı adı")} value={ben?.adi || 'root'} disabled />
                <div>
                  <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Rol / Durum")}</span>
                  <div className="flex gap-2 pt-1.5">
                    <Badge color="primary" variant="soft" className="text-[11px] uppercase tracking-wider px-2 py-1 font-semibold">{musteri ? cevir("Hosting kullanıcısı") : ben?.rol || 'admin'}</Badge>
                    <Badge color="success" variant="soft" className="text-[11px] uppercase tracking-wider px-2 py-1 font-semibold">{ben?.durum || 'active'}</Badge>
                  </div>
                </div>
                {!musteri && (
                  <>
                    <Girdi etiket={cevir("Ad Soyad")} value={ad} onChange={e => setAd(e.target.value)} placeholder={cevir("Adınız Soyadınız")} />
                    <Girdi etiket={cevir("E-posta")} type="email" value={eposta} onChange={e => setEposta(e.target.value)} placeholder={cevir("ornek@site.com")} />
                  </>
                )}
              </div>
              {!musteri && (
                <div className="flex items-center gap-3 flex-wrap">
                  <Button type="submit" color="primary" yukleniyor={pYuk} ikon={I.disket}>{pYuk ? cevir("Kaydediliyor…") : cevir("Kaydet")}</Button>
                  <Uyari tip="ok" mesaj={pOk} /><Uyari tip="err" mesaj={pErr} />
                </div>
              )}
            </form>
          } />

        {/* 2) Parola */}
        <Kart baslik={cevir("Parola")}
          aciklama={musteri ? undefined : rootMu ? cevir("Bu, sunucunun root parolasını değiştirir (SSH erişimi dahil).") : cevir("Bu, panel giriş parolanızı değiştirir.")}
          ikon={<Ikon d={I.kilit} className="size-5" />}
          cocuk={musteri ? (
            <div className="space-y-4">
              <UyariKutusu ton="bilgi">{cevir("Panel giriş parolanız, aboneliğinizin FTP hesabının parolasıdır; FTP Hesapları sayfasından değiştirilir.")}</UyariKutusu>
              {musteriDID !== null && (
                <Link to={`/abonelikler/${musteriDID}/ftp`} className={butonSinifi({ color: 'primary' })}>
                  {cevir("FTP Hesaplarına git")}
                  <Ikon d={I.ileri} className="size-4 shrink-0 ms-2 -me-1" />
                </Link>
              )}
            </div>
          ) : (
            <form onSubmit={parolaDegistir} className="space-y-4">
              <Girdi etiket={cevir("Mevcut parola")} type="password" value={mevcut} onChange={e => setMevcut(e.target.value)} autoComplete="current-password" />
              <div className="grid sm:grid-cols-2 gap-4">
                <Girdi etiket={cevir("Yeni parola")} type="password" value={yeni} onChange={e => setYeni(e.target.value)} autoComplete="new-password" />
                <Girdi etiket={cevir("Yeni parola (tekrar)")} type="password" value={yeni2} onChange={e => setYeni2(e.target.value)} autoComplete="new-password" />
              </div>
              <div className="flex items-center gap-3 flex-wrap">
                <Button type="submit" color="primary" yukleniyor={paYuk} disabled={!mevcut || !yeni} ikon={I.kalem}>{paYuk ? cevir("Değiştiriliyor…") : cevir("Parolayı Değiştir")}</Button>
                <Uyari tip="ok" mesaj={paOk} /><Uyari tip="err" mesaj={paErr} />
              </div>
            </form>
          )} />

        {/* 3) 2FA — müşteride yok (uçlar AdminVeyaReseller) */}
        {!musteri && (
          <Kart baslik={cevir("İki Adımlı Doğrulama (2FA)")} aciklama={cevir("Girişte parolaya ek olarak authenticator uygulamasından 6 haneli kod ister.")}
            ikon={<Ikon d={I.kalkan} className="size-5" />}
            cocuk={
              <div className="space-y-4">
                <div className="flex items-center gap-3">
                  <span className="text-sm text-slate-600 dark:text-slate-400">{cevir("Durum:")}</span>
                  {ben?.iki_fa
                    ? <Badge color="success" variant="soft" className="gap-1.5 text-xs font-semibold px-2.5 py-1"><span className="inline-block size-2 rounded-full bg-current" aria-hidden />{cevir("Aktif")}</Badge>
                    : <Badge variant="soft" className="gap-1.5 text-xs font-semibold px-2.5 py-1"><span className="inline-block size-2 rounded-full bg-slate-400 dark:bg-dark-300" aria-hidden />{cevir("Kapalı")}</Badge>}
                </div>

                {!ben?.iki_fa && !f2Kur && (
                  <Button onClick={f2Baslat} color="primary" ikon={I.kalkan}>{cevir("2FA'yı Etkinleştir")}</Button>
                )}

                {!ben?.iki_fa && f2Kur && (
                  <form onSubmit={f2Etkinlestir} className="space-y-3 border border-slate-200 dark:border-dark-600 rounded-lg p-4 bg-slate-50 dark:bg-dark-800">
                    <p className="text-sm text-slate-700 dark:text-slate-300">{cevir("1) Authenticator uygulamanıza (Google Authenticator, Authy, Microsoft Authenticator) ekleyin:")}</p>
                    {f2Kur.qr_data_uri && (
                      <div className="flex flex-col items-center gap-2 py-1">
                        <img src={f2Kur.qr_data_uri} alt={cevir("2FA QR kodu")} width={256} height={256}
                          className="w-64 h-64 rounded-lg bg-white p-3 border border-slate-200 dark:border-dark-600 shadow-xs" />
                        <p className="text-xs text-slate-500 dark:text-slate-500">{cevir("Authenticator uygulamanızla tarayın")}</p>
                      </div>
                    )}
                    <p className="text-xs text-slate-500 dark:text-slate-500">{cevir("Tarayamıyorsanız, elle giriş için gizli anahtar:")}</p>
                    <div className="flex items-center gap-2 flex-wrap">
                      <code className="font-mono text-sm px-3 py-2 rounded-lg bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 text-slate-800 dark:text-slate-100 tracking-widest select-all">{secretGruplu}</code>
                      <Button type="button" variant="outlined" boyut="sm" ikon={I.kopya} onClick={() => { navigator.clipboard?.writeText(f2Kur.secret) }}>{cevir("Kopyala")}</Button>
                    </div>
                    <p className="text-[11px] text-slate-500 dark:text-slate-500 break-all">{cevir("veya bağlantı:")} <span className="font-mono">{f2Kur.otpauth}</span></p>
                    <p className="text-sm text-slate-700 dark:text-slate-300">{cevir("2) Uygulamadaki 6 haneli kodu girin:")}</p>
                    <div className="flex items-center gap-3 flex-wrap">
                      <input value={f2Kod} onChange={e => setF2Kod(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" inputMode="numeric"
                        className="w-32 px-3 py-2 text-center text-lg font-mono tracking-[0.3em] bg-white dark:bg-dark-700 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 focus:border-brand-500 outline-none" />
                      <Button type="submit" color="primary" yukleniyor={f2Yuk} disabled={f2Kod.length !== 6} ikon={I.onay}>{f2Yuk ? cevir("Doğrulanıyor…") : cevir("Doğrula ve Aç")}</Button>
                      <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setF2Kur(null)}>{cevir("İptal")}</Button>
                    </div>
                    <Uyari tip="err" mesaj={f2Err} />
                  </form>
                )}

                {ben?.iki_fa && !f2Kapat && (
                  <Button color="error" variant="soft" ikon={I.ban} onClick={() => { setF2Kapat(true); setF2Err('') }}>{cevir("2FA'yı Kapat")}</Button>
                )}
                {ben?.iki_fa && f2Kapat && (
                  <form onSubmit={f2KapatOnay} className="space-y-3 border border-red-200 dark:border-red-800 rounded-lg p-4 bg-red-50 dark:bg-red-900/10">
                    <p className="text-sm text-slate-700 dark:text-slate-300">{cevir("Kapatmak için authenticator kodunu girin:")}</p>
                    <div className="flex items-center gap-3 flex-wrap">
                      <input value={kapatKod} onChange={e => setKapatKod(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" inputMode="numeric"
                        className="w-32 px-3 py-2 text-center text-lg font-mono tracking-[0.3em] bg-white dark:bg-dark-700 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none" />
                      <Button type="submit" color="error" yukleniyor={f2Yuk} disabled={kapatKod.length !== 6} ikon={I.ban}>{cevir("Kapat")}</Button>
                      <Button type="button" variant="outlined" ikon={I.kapat} onClick={() => setF2Kapat(false)}>{cevir("Vazgeç")}</Button>
                    </div>
                    <Uyari tip="err" mesaj={f2Err} />
                  </form>
                )}
              </div>
            } />
        )}

        {/* 4) Tercihler — müşteride yok (PUT /me 403); tema/dil üst çubuktan değişir */}
        {!musteri && (
          <Kart baslik={cevir("Tercihler")} aciklama={cevir("Panel görünüm ve dil tercihleri.")}
            ikon={<Ikon d={I.ayar} className="size-5" />}
            cocuk={
              <div className="space-y-4">
                <div className="grid sm:grid-cols-2 gap-4">
                  <label className="block">
                    <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Tema")}</span>
                    <select value={tema} onChange={e => setTema(e.target.value)} className="w-full px-3 py-2 text-sm bg-white dark:bg-dark-800 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none">
                      <option value="system">{cevir("Sistem")}</option><option value="light">{cevir("Açık")}</option><option value="dark">{cevir("Koyu")}</option>
                    </select>
                  </label>
                  <label className="block">
                    <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Dil")}</span>
                    <select value={dil} onChange={e => setDil(e.target.value)} className="w-full px-3 py-2 text-sm bg-white dark:bg-dark-800 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none">
                      <option value="tr">{cevir("Türkçe")}</option><option value="en">English</option>
                    </select>
                  </label>
                </div>
                <div className="flex items-center gap-3 flex-wrap">
                  <Button onClick={tercihKaydet} color="primary" yukleniyor={tYuk} ikon={I.disket}>{tYuk ? cevir("Kaydediliyor…") : cevir("Tercihleri Kaydet")}</Button>
                  <Uyari tip="ok" mesaj={tOk} /><Uyari tip="err" mesaj={tErr} />
                </div>
              </div>
            } />
        )}
      </div>
    </div>
  )
}
