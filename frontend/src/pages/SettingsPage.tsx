import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useAuth } from '@/store/auth'

type Ben = {
  id: number; adi: string; rol: string; eposta: string; ad_soyad: string
  durum: string; iki_fa: boolean; tercih_tema: string; tercih_dil: string
}

function Kart({ baslik, aciklama, ikon, cocuk }: { baslik: string; aciklama?: string; ikon: React.ReactNode; cocuk: React.ReactNode }) {
  return (
    <section className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6 shadow-sm">
      <div className="flex items-start gap-3 mb-5">
        <div className="w-10 h-10 rounded-2xl bg-brand-50 dark:bg-brand-900/30 text-brand-600 dark:text-brand-400 flex items-center justify-center shrink-0">{ikon}</div>
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
      <input {...p} className="w-full px-3 py-2 text-sm bg-white dark:bg-slate-900 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none disabled:opacity-60 disabled:bg-slate-100 dark:disabled:bg-slate-800" />
    </label>
  )
}

function Uyari({ tip, mesaj }: { tip: 'ok' | 'err'; mesaj: string }) {
  if (!mesaj) return null
  const c = tip === 'ok'
    ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-200 dark:border-emerald-800 text-emerald-700 dark:text-emerald-300'
    : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300'
  return <div className={`text-sm px-3 py-2 rounded-lg border ${c}`}>{mesaj}</div>
}

export default function SettingsPage() {
  const guncelleAd = useAuth((s) => s.guncelleAd)
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
  const [tOk, setTOk] = useState(''); const [tYuk, setTYuk] = useState(false)

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
      setPOk('Bilgiler kaydedildi.'); setTimeout(() => setPOk(''), 3000); yukle()
    } catch (e) { setPErr(apiHata(e, 'Kaydedilemedi')) } finally { setPYuk(false) }
  }

  async function parolaDegistir(e: React.FormEvent) {
    e.preventDefault(); setPaOk(''); setPaErr('')
    if (yeni.length < 8) { setPaErr('Yeni parola en az 8 karakter olmalı.'); return }
    if (yeni !== yeni2) { setPaErr('Yeni parolalar eşleşmiyor.'); return }
    setPaYuk(true)
    try {
      await api.post('/me/parola', { mevcut, yeni })
      setPaOk('Parola değiştirildi. (Sunucu root parolası güncellendi.)')
      setMevcut(''); setYeni(''); setYeni2(''); setTimeout(() => setPaOk(''), 5000)
    } catch (e) { setPaErr(apiHata(e, 'Parola değiştirilemedi')) } finally { setPaYuk(false) }
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
    } catch (e) { setF2Err(apiHata(e, 'Kod doğrulanamadı')) } finally { setF2Yuk(false) }
  }
  async function f2KapatOnay(e: React.FormEvent) {
    e.preventDefault(); setF2Err(''); setF2Yuk(true)
    try { await api.post('/me/2fa/disable', { kod: kapatKod }); setF2Kapat(false); setKapatKod(''); yukle() }
    catch (e) { setF2Err(apiHata(e, 'Kod doğrulanamadı')) } finally { setF2Yuk(false) }
  }

  async function tercihKaydet() {
    setTOk(''); setTYuk(true)
    try {
      await api.put('/me', { ad_soyad: ad, eposta, tercih_tema: tema, tercih_dil: dil })
      try { localStorage.setItem('gosp.tema', tema) } catch { /* yoksay */ }
      setTOk('Tercihler kaydedildi.'); setTimeout(() => setTOk(''), 3000)
    } catch { setTOk('') } finally { setTYuk(false) }
  }

  const btn = 'px-4 py-2 text-sm font-medium rounded-lg bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-50 inline-flex items-center gap-2'
  const secretGruplu = f2Kur ? (f2Kur.secret.match(/.{1,4}/g) || []).join(' ') : ''

  return (
    <div className="px-6 md:px-8 py-6">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Profil ve Tercihler' }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Profil ve Tercihler</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">Hesap bilgileriniz, parola, iki adımlı doğrulama ve panel tercihleri.</p>
      {yukHata && <div className="mb-4"><Uyari tip="err" mesaj={yukHata} /></div>}

      <div className="space-y-5">
        {/* 1) Hesap Bilgileri */}
        <Kart baslik="Hesap Bilgileri" aciklama="Ad soyad ve e-posta adresinizi düzenleyin."
          ikon={<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>}
          cocuk={
            <form onSubmit={profilKaydet} className="space-y-4">
              <div className="grid sm:grid-cols-2 gap-4">
                <Girdi etiket="Kullanıcı adı" value={ben?.adi || 'root'} disabled />
                <div>
                  <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Rol / Durum</span>
                  <div className="flex gap-2 pt-1.5">
                    <span className="text-[11px] uppercase tracking-wider px-2 py-1 rounded bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 font-semibold">{ben?.rol || 'admin'}</span>
                    <span className="text-[11px] uppercase tracking-wider px-2 py-1 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 font-semibold">{ben?.durum || 'active'}</span>
                  </div>
                </div>
                <Girdi etiket="Ad Soyad" value={ad} onChange={e => setAd(e.target.value)} placeholder="Adınız Soyadınız" />
                <Girdi etiket="E-posta" type="email" value={eposta} onChange={e => setEposta(e.target.value)} placeholder="ornek@site.com" />
              </div>
              <div className="flex items-center gap-3 flex-wrap">
                <button type="submit" disabled={pYuk} className={btn}>{pYuk ? 'Kaydediliyor…' : 'Kaydet'}</button>
                <Uyari tip="ok" mesaj={pOk} /><Uyari tip="err" mesaj={pErr} />
              </div>
            </form>
          } />

        {/* 2) Parola */}
        <Kart baslik="Parola" aciklama="Bu, sunucunun root parolasını değiştirir (SSH erişimi dahil)."
          ikon={<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>}
          cocuk={
            <form onSubmit={parolaDegistir} className="space-y-4">
              <Girdi etiket="Mevcut parola" type="password" value={mevcut} onChange={e => setMevcut(e.target.value)} autoComplete="current-password" />
              <div className="grid sm:grid-cols-2 gap-4">
                <Girdi etiket="Yeni parola" type="password" value={yeni} onChange={e => setYeni(e.target.value)} autoComplete="new-password" />
                <Girdi etiket="Yeni parola (tekrar)" type="password" value={yeni2} onChange={e => setYeni2(e.target.value)} autoComplete="new-password" />
              </div>
              <div className="flex items-center gap-3 flex-wrap">
                <button type="submit" disabled={paYuk || !mevcut || !yeni} className={btn}>{paYuk ? 'Değiştiriliyor…' : 'Parolayı Değiştir'}</button>
                <Uyari tip="ok" mesaj={paOk} /><Uyari tip="err" mesaj={paErr} />
              </div>
            </form>
          } />

        {/* 3) 2FA */}
        <Kart baslik="İki Adımlı Doğrulama (2FA)" aciklama="Girişte parolaya ek olarak authenticator uygulamasından 6 haneli kod ister."
          ikon={<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="m9 12 2 2 4-4"/></svg>}
          cocuk={
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <span className="text-sm text-slate-600 dark:text-slate-400">Durum:</span>
                {ben?.iki_fa
                  ? <span className="text-xs font-semibold px-2.5 py-1 rounded-full bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300">● Aktif</span>
                  : <span className="text-xs font-semibold px-2.5 py-1 rounded-full bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300">○ Kapalı</span>}
              </div>

              {!ben?.iki_fa && !f2Kur && (
                <button onClick={f2Baslat} className={btn}>2FA'yı Etkinleştir</button>
              )}

              {!ben?.iki_fa && f2Kur && (
                <form onSubmit={f2Etkinlestir} className="space-y-3 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 bg-slate-50 dark:bg-slate-900">
                  <p className="text-sm text-slate-700 dark:text-slate-300">1) Authenticator uygulamanıza (Google Authenticator, Authy, Microsoft Authenticator) ekleyin:</p>
                  {f2Kur.qr_data_uri && (
                    <div className="flex flex-col items-center gap-2 py-1">
                      <img src={f2Kur.qr_data_uri} alt="2FA QR kodu" width={256} height={256}
                        className="w-64 h-64 rounded-2xl bg-white p-3 border border-slate-200 dark:border-slate-700 shadow-sm" />
                      <p className="text-xs text-slate-500 dark:text-slate-500">Authenticator uygulamanızla tarayın</p>
                    </div>
                  )}
                  <p className="text-xs text-slate-500 dark:text-slate-500">Tarayamıyorsanız, elle giriş için gizli anahtar:</p>
                  <div className="flex items-center gap-2 flex-wrap">
                    <code className="font-mono text-sm px-3 py-2 rounded-lg bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-800 dark:text-slate-100 tracking-widest select-all">{secretGruplu}</code>
                    <button type="button" onClick={() => { navigator.clipboard?.writeText(f2Kur.secret) }} className="text-xs px-2.5 py-1.5 rounded border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700">Kopyala</button>
                  </div>
                  <p className="text-[11px] text-slate-500 dark:text-slate-500 break-all">veya bağlantı: <span className="font-mono">{f2Kur.otpauth}</span></p>
                  <p className="text-sm text-slate-700 dark:text-slate-300">2) Uygulamadaki 6 haneli kodu girin:</p>
                  <div className="flex items-center gap-3 flex-wrap">
                    <input value={f2Kod} onChange={e => setF2Kod(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" inputMode="numeric"
                      className="w-32 px-3 py-2 text-center text-lg font-mono tracking-[0.3em] bg-white dark:bg-slate-800 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 focus:border-brand-500 outline-none" />
                    <button type="submit" disabled={f2Yuk || f2Kod.length !== 6} className={btn}>{f2Yuk ? 'Doğrulanıyor…' : 'Doğrula ve Aç'}</button>
                    <button type="button" onClick={() => setF2Kur(null)} className="text-xs text-slate-500 hover:text-slate-700 dark:hover:text-slate-300">İptal</button>
                  </div>
                  <Uyari tip="err" mesaj={f2Err} />
                </form>
              )}

              {ben?.iki_fa && !f2Kapat && (
                <button onClick={() => { setF2Kapat(true); setF2Err('') }} className="px-4 py-2 text-sm font-medium rounded-lg border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20">2FA'yı Kapat</button>
              )}
              {ben?.iki_fa && f2Kapat && (
                <form onSubmit={f2KapatOnay} className="space-y-3 border border-red-200 dark:border-red-800 rounded-2xl p-4 bg-red-50 dark:bg-red-900/10">
                  <p className="text-sm text-slate-700 dark:text-slate-300">Kapatmak için authenticator kodunu girin:</p>
                  <div className="flex items-center gap-3 flex-wrap">
                    <input value={kapatKod} onChange={e => setKapatKod(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" inputMode="numeric"
                      className="w-32 px-3 py-2 text-center text-lg font-mono tracking-[0.3em] bg-white dark:bg-slate-800 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none" />
                    <button type="submit" disabled={f2Yuk || kapatKod.length !== 6} className="px-4 py-2 text-sm font-medium rounded-lg bg-red-600 hover:bg-red-700 text-white disabled:opacity-50">Kapat</button>
                    <button type="button" onClick={() => setF2Kapat(false)} className="text-xs text-slate-500 hover:text-slate-700 dark:hover:text-slate-300">Vazgeç</button>
                  </div>
                  <Uyari tip="err" mesaj={f2Err} />
                </form>
              )}
            </div>
          } />

        {/* 4) Tercihler */}
        <Kart baslik="Tercihler" aciklama="Panel görünüm ve dil tercihleri."
          ikon={<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>}
          cocuk={
            <div className="space-y-4">
              <div className="grid sm:grid-cols-2 gap-4">
                <label className="block">
                  <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Tema</span>
                  <select value={tema} onChange={e => setTema(e.target.value)} className="w-full px-3 py-2 text-sm bg-white dark:bg-slate-900 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none">
                    <option value="system">Sistem</option><option value="light">Açık</option><option value="dark">Koyu</option>
                  </select>
                </label>
                <label className="block">
                  <span className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">Dil</span>
                  <select value={dil} onChange={e => setDil(e.target.value)} className="w-full px-3 py-2 text-sm bg-white dark:bg-slate-900 border border-slate-300 dark:border-slate-600 rounded-lg text-slate-800 dark:text-slate-100 outline-none">
                    <option value="tr">Türkçe</option><option value="en">English</option>
                  </select>
                </label>
              </div>
              <div className="flex items-center gap-3 flex-wrap">
                <button onClick={tercihKaydet} disabled={tYuk} className={btn}>{tYuk ? 'Kaydediliyor…' : 'Tercihleri Kaydet'}</button>
                <Uyari tip="ok" mesaj={tOk} />
              </div>
            </div>
          } />
      </div>
    </div>
  )
}
