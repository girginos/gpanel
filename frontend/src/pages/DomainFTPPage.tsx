import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useCallback, useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { Button, Badge, Input } from '@/components/ui'
import { I } from '@/components/Ikon'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import ConfirmDialog from '@/components/ConfirmDialog'
import EmptyState from '@/components/EmptyState'
import FtpKlasorSecici from '@/components/FtpKlasorSecici'
import { useToast } from '@/components/Toast'
import { parolaUret, panoyaKopyala } from '@/lib/parola'
import SsoParolaNotu from '@/components/SsoParolaNotu'
import { ssoOturum } from '@/store/auth'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ftp_host: string; ftp_user: string }
type EkHesap = { kullanici: string; ek: string; dizin: string; izin: string; durum: 'aktif' | 'askida'; etkin: boolean; olusturma: string }
type EkListe = { onek: string; hesaplar: EkHesap[]; azami: number; kullanim: number; host_askida: boolean }
type Sonuc = { kullanici: string; dizin: string; parola: string }

const FTP_EN: Record<string, string> = {
  "FTP Hesapları": "FTP Accounts",
  "FTP Hesabı": "FTP Account",
  "Ana FTP hesabı": "Main FTP account",
  "Hosting hesabınızın ana kullanıcısı: tüm dosyalarınıza erişir, panel girişinizle aynı parolayı kullanır.": "Your hosting account's main user: reaches all your files and uses the same password as your panel login.",
  "Sunucu": "Server",
  "Port (FTP)": "Port (FTP)",
  "Port (SFTP)": "Port (SFTP)",
  "Kullanıcı adı": "Username",
  "Ev dizini": "Home directory",
  "Kopyala": "Copy",
  "Kopyalandı": "Copied",
  "Panoya kopyalanamadı": "Could not copy to clipboard",
  "Parola sıfırlama": "Password reset",
  "Yeni parola": "New password",
  "Yeni FTP parolası girin": "Enter new FTP password",
  "Parola üret": "Generate password",
  "Parolayı göster": "Show password",
  "Parolayı gizle": "Hide password",
  "Parolayı kopyala": "Copy password",
  "Parolayı ayarla": "Set password",
  "Parola sıfırlama başarısız": "Password reset failed",
  "FTP parolası güncellendi": "FTP password updated",
  "İşlem başarısız": "Operation failed",
  "FTP bağlantısı şifrelenmez; şifrelemek için SFTP (port 22) kullanın.": "FTP is not encrypted; use SFTP (port 22) to encrypt.",
  "Zayıf": "Weak",
  "Orta": "Fair",
  "İyi": "Good",
  "Güçlü": "Strong",
  "Ek FTP hesapları": "Extra FTP accounts",
  "Belirli klasörlere özel hesaplar açın; her hesap yalnız kendi klasörünü görür. Ek hesaplar yalnız FTP (port 21) ile bağlanır, panele giriş yapamaz.": "Create accounts limited to specific folders; each account sees only its own folder. Extra accounts connect over FTP (port 21) only and cannot sign in to the panel.",
  "Yeni FTP hesabı": "New FTP account",
  "Henüz ek FTP hesabı yok": "No extra FTP accounts yet",
  "Bir klasöre özel hesap açarak, ana parolanızı paylaşmadan başkalarına yalnız o klasörde erişim verebilirsiniz.": "Create a folder-specific account to give others access to just that folder without sharing your main password.",
  "Hesap sınırına ulaştınız": "You have reached the account limit",
  "Okuma-Yazma": "Read-Write",
  "Aktif": "Active",
  "Askıda": "Suspended",
  "Hosting askıda: ek hesaplar da kapalı.": "Hosting is suspended: extra accounts are disabled too.",
  "Parola": "Password",
  "Düzenle": "Edit",
  "Askıya al": "Suspend",
  "Etkinleştir": "Enable",
  "Sil": "Delete",
  "İptal": "Cancel",
  "Oluştur": "Create",
  "Kaydet": "Save",
  "Tamam": "Done",
  "Yükleniyor…": "Loading…",
  "yükleniyor…": "loading…",
  "Hesaplar okunamadı": "Accounts could not be loaded",
  "Kullanıcı adı (önek sabit)": "Username (prefix is fixed)",
  "küçük harf, rakam, alt çizgi": "lowercase letters, digits, underscore",
  "Klasör": "Folder",
  "Üst klasör": "Parent folder",
  "Klasörler okunamadı; yolu elle yazabilirsiniz.": "Folders could not be read; you can type the path.",
  "Alt klasör yok.": "No subfolders.",
  "Klasör yoksa oluştur": "Create the folder if it does not exist",
  "Hesap yalnız bu klasörü ve altındakileri görür; ev dizininin kendisi ya da gizli (.) klasörler seçilemez.": "The account sees only this folder and what is inside it; the home directory itself and hidden (.) folders cannot be chosen.",
  "İzin": "Permission",
  "Salt-okuma izni henüz yok: hesaplar okuma-yazma çalışır.": "Read-only permission is not available yet: accounts are read-write.",
  "Bağlantı bilgileri": "Connection details",
  "Port": "Port",
  "Bilgileri kopyala": "Copy details",
  "Bilgiler kopyalandı": "Details copied",
  "Kullanıcı adı yalnız küçük harf, rakam ve alt çizgi içerebilir (en çok 24).": "Username may only contain lowercase letters, digits and underscore (max 24).",
  "Bir klasör seçin (ev dizininin kendisi olamaz).": "Choose a folder (not the home directory itself).",
  "Parola en az 12 karakter olmalı ve harf ile rakam içermeli.": "Password must be at least 12 characters and include letters and digits.",
  "Hesap oluşturuldu": "Account created",
  "FTP hesabı oluşturuldu": "FTP account created",
  "Parola bir daha gösterilmez; şimdi kopyalayın.": "The password will not be shown again; copy it now.",
  "Hesap oluşturulamadı": "Account could not be created",
  "Hesabı düzenle": "Edit account",
  "FTP hesabı güncellendi": "FTP account updated",
  "Hesap güncellenemedi": "Account could not be updated",
  "Parolayı değiştir": "Change password",
  "Parola değiştirilemedi": "Password could not be changed",
  "Ek hesabın parolası değişti": "Extra account password changed",
  "Hesabı sil": "Delete account",
  "FTP hesabı silindi": "FTP account deleted",
  "Hesap silinemedi": "Account could not be deleted",
  "hesabı silinsin mi? Klasördeki dosyalar silinmez; yalnız bu FTP girişi kalkar.": "will be deleted. Files in the folder are kept; only this FTP login is removed.",
  "Hesap askıya alındı": "Account suspended",
  "Hesap etkinleştirildi": "Account enabled",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (FTP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

const EK_RE = /^[a-z0-9][a-z0-9_]{0,23}$/

function parolaUygun(p: string): boolean {
  return p.length >= 12 && /[A-Za-z]/.test(p) && /[0-9]/.test(p)
}

// Güç ölçer (yalnız yol gösterir; asıl kural sunucuda): uzunluk + karakter çeşitliliği.
function parolaGuc(p: string): { puan: number; etiket: string } {
  if (!p) return { puan: 0, etiket: '' }
  const sinif = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter(r => r.test(p)).length
  let puan = 0
  if (p.length >= 8) puan++
  if (p.length >= 12) puan++
  if (p.length >= 16 && sinif >= 3) puan++
  if (sinif === 4 && p.length >= 14) puan++
  if (sinif < 2) puan = Math.min(puan, 1)
  return { puan, etiket: [cevir("Zayıf"), cevir("Zayıf"), cevir("Orta"), cevir("İyi"), cevir("Güçlü")][puan] }
}

function GucCubugu({ parola }: { parola: string }) {
  const { puan, etiket } = parolaGuc(parola)
  const renk = puan <= 1 ? 'bg-error' : puan === 2 ? 'bg-warning' : 'bg-success'
  return (
    <div className="mt-2 flex items-center gap-2" aria-live="polite">
      <div className="flex flex-1 gap-1">
        {[1, 2, 3, 4].map(i => (
          <span key={i} className={`h-1.5 flex-1 rounded-full ${parola && i <= puan ? renk : 'bg-gray-200 dark:bg-dark-500'}`} />
        ))}
      </div>
      <span className="w-12 text-end text-xs text-gray-500 dark:text-dark-300">{parola ? etiket : ''}</span>
    </div>
  )
}

// Değer + tıkla-kopyala satırı.
function KopyaSatiri({ etiket, deger, onKopya }: { etiket: string; deger: string; onKopya: (v: string) => void }) {
  const [tamam, setTamam] = useState(false)
  return (
    <div className="flex items-center gap-3 py-2">
      <span className="w-28 shrink-0 text-sm text-gray-500 dark:text-dark-300 sm:w-36">{etiket}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-sm text-gray-800 dark:text-dark-50" title={deger}>{deger}</span>
      <Button variant="flat" boyut="sm" isIcon ikon={tamam ? I.onay : I.kopya} className="size-8"
        title={cevir("Kopyala")} aria-label={`${cevir("Kopyala")}: ${etiket}`}
        onClick={() => { onKopya(deger); setTamam(true); window.setTimeout(() => setTamam(false), 1500) }} />
    </div>
  )
}

// Parola girişi: göster/gizle, üret, kopyala, güç göstergesi.
function ParolaAlani({ deger, onDeger, goster, onGoster, onKopya, etiket, yer }: {
  deger: string; onDeger: (v: string) => void; goster: boolean; onGoster: (v: boolean) => void
  onKopya: (v: string) => void; etiket?: string; yer?: string
}) {
  return (
    <div>
      <div className="flex flex-wrap items-end gap-2">
        <div className="min-w-0 flex-1 basis-48">
          <Input label={etiket} type={goster ? 'text' : 'password'} value={deger} onChange={e => onDeger(e.target.value)}
            placeholder={yer} autoComplete="new-password" spellCheck={false} classNames={{ input: 'font-mono text-sm' }} />
        </div>
        <Button variant="outlined" isIcon ikon={goster ? I.gozKapali : I.goz} className="size-9"
          title={goster ? cevir("Parolayı gizle") : cevir("Parolayı göster")} aria-label={goster ? cevir("Parolayı gizle") : cevir("Parolayı göster")}
          onClick={() => onGoster(!goster)} />
        <Button variant="outlined" isIcon ikon={I.kopya} className="size-9" disabled={!deger}
          title={cevir("Parolayı kopyala")} aria-label={cevir("Parolayı kopyala")} onClick={() => onKopya(deger)} />
        <Button variant="outlined" ikon={I.yenile} onClick={() => { onDeger(parolaUret(16)); onGoster(true) }}>{cevir("Parola üret")}</Button>
      </div>
      <GucCubugu parola={deger} />
    </div>
  )
}

export default function DomainFTPPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [liste, setListe] = useState<EkListe | null>(null)
  const [listeHata, setListeHata] = useState(false)

  // Ana hesap parola sıfırlama
  const [ozelPw, setOzelPw] = useState('')
  const [anaGoster, setAnaGoster] = useState(false)
  const [anaIsleniyor, setAnaIsleniyor] = useState(false)

  // Modallar
  const [yeniAcik, setYeniAcik] = useState(false)
  const [ek, setEk] = useState('')
  const [dizin, setDizin] = useState('')
  const [dizinOlustur, setDizinOlustur] = useState(true)
  const [pw, setPw] = useState('')
  const [pwGoster, setPwGoster] = useState(true)
  const [kaydediyor, setKaydediyor] = useState(false)
  const [sonuc, setSonuc] = useState<Sonuc | null>(null)
  const [duzenle, setDuzenle] = useState<EkHesap | null>(null)
  const [parolaHesap, setParolaHesap] = useState<EkHesap | null>(null)
  const [silHesap, setSilHesap] = useState<EkHesap | null>(null)
  const [satirIslem, setSatirIslem] = useState<string | null>(null)

  const kopyala = useCallback(async (v: string) => {
    if (await panoyaKopyala(v)) toast.basari(cevir("Kopyalandı"))
    else toast.hata(cevir("Panoya kopyalanamadı"))
  }, [toast])

  const listeYukle = useCallback(() => {
    if (!id) return Promise.resolve()
    return api.get<EkListe>(`/domains/${id}/ftp-ek`)
      .then(r => { setListe(r.data); setListeHata(false) })
      .catch(() => setListeHata(true))
  }, [id])

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (!iptal) setDomain(r.data) })
      .catch(e => { if (!iptal) toast.hata(cevir("İşlem başarısız"), apiHata(e)) })
    listeYukle()
    return () => { iptal = true }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  async function anaParolaSifirla() {
    if (!ozelPw) return
    setAnaIsleniyor(true)
    try {
      await api.put(`/domains/${id}/ftp/password`, { parola: ozelPw })
      setOzelPw('')
      toast.basari(cevir("FTP parolası güncellendi"))
    } catch (e) {
      toast.hata(cevir("İşlem başarısız"), apiHata(e, cevir("Parola sıfırlama başarısız")))
    } finally {
      setAnaIsleniyor(false)
    }
  }

  function yeniAc() {
    setEk(''); setDizin(''); setDizinOlustur(true); setPw(parolaUret(16)); setPwGoster(true); setSonuc(null)
    setYeniAcik(true)
  }

  const ekGecerli = EK_RE.test(ek.trim().toLowerCase())
  const dizinGecerli = dizin.trim() !== '' && dizin.trim() !== '/'
  const yeniGecerli = ekGecerli && dizinGecerli && parolaUygun(pw)
  const onek = liste?.onek || (domain ? `${domain.sistem_kullanici}-` : '')
  const tamKullanici = `${onek}${ek.trim().toLowerCase()}`

  function baglantiMetni(kullanici: string, klasor: string, parola?: string): string {
    return [
      `${cevir("Sunucu")}: ${domain?.ftp_host || ''}`,
      `${cevir("Port")}: 21 (FTP)`,
      `${cevir("Kullanıcı adı")}: ${kullanici}`,
      ...(parola ? [`${cevir("Parola")}: ${parola}`] : []),
      `${cevir("Klasör")}: ${klasor}`,
    ].join('\n')
  }

  async function olustur() {
    if (!yeniGecerli) return
    setKaydediyor(true)
    try {
      const r = await api.post<{ hesap: EkHesap }>(`/domains/${id}/ftp-ek`, {
        ek: ek.trim().toLowerCase(), dizin: dizin.trim(), dizin_olustur: dizinOlustur, parola: pw,
      })
      setSonuc({ kullanici: r.data.hesap.kullanici, dizin: r.data.hesap.dizin, parola: pw })
      toast.basari(cevir("FTP hesabı oluşturuldu"))
      listeYukle()
    } catch (e) {
      toast.hata(cevir("Hesap oluşturulamadı"), apiHata(e))
    } finally {
      setKaydediyor(false)
    }
  }

  function duzenleAc(h: EkHesap) { setDizin(h.dizin); setDizinOlustur(false); setDuzenle(h) }

  async function duzenleKaydet() {
    if (!duzenle || !dizinGecerli) return
    setKaydediyor(true)
    try {
      await api.put(`/domains/${id}/ftp-ek/${encodeURIComponent(duzenle.kullanici)}`, { dizin: dizin.trim(), dizin_olustur: dizinOlustur })
      toast.basari(cevir("FTP hesabı güncellendi"))
      setDuzenle(null)
      listeYukle()
    } catch (e) {
      toast.hata(cevir("Hesap güncellenemedi"), apiHata(e))
    } finally {
      setKaydediyor(false)
    }
  }

  function parolaAc(h: EkHesap) { setPw(parolaUret(16)); setPwGoster(true); setParolaHesap(h) }

  async function parolaKaydet() {
    if (!parolaHesap || !parolaUygun(pw)) return
    setKaydediyor(true)
    try {
      await api.put(`/domains/${id}/ftp-ek/${encodeURIComponent(parolaHesap.kullanici)}/password`, { parola: pw })
      toast.basari(cevir("Ek hesabın parolası değişti"))
      setParolaHesap(null)
    } catch (e) {
      toast.hata(cevir("Parola değiştirilemedi"), apiHata(e))
    } finally {
      setKaydediyor(false)
    }
  }

  async function etkinDegistir(h: EkHesap) {
    setSatirIslem(h.kullanici)
    try {
      await api.put(`/domains/${id}/ftp-ek/${encodeURIComponent(h.kullanici)}`, { etkin: !h.etkin })
      toast.basari(cevir(h.etkin ? "Hesap askıya alındı" : "Hesap etkinleştirildi"))
      await listeYukle()
    } catch (e) {
      toast.hata(cevir("Hesap güncellenemedi"), apiHata(e))
    } finally {
      setSatirIslem(null)
    }
  }

  async function sil() {
    if (!silHesap) return
    try {
      await api.delete(`/domains/${id}/ftp-ek/${encodeURIComponent(silHesap.kullanici)}`)
      toast.basari(cevir("FTP hesabı silindi"))
      setSilHesap(null)
      listeYukle()
    } catch (e) {
      toast.hata(cevir("Hesap silinemedi"), apiHata(e))
      setSilHesap(null)
    }
  }

  const sinirDolu = !!liste && liste.kullanim >= liste.azami
  const anaDizin = domain ? `/home/${domain.sistem_kullanici}` : ''

  return (
    <div className="mx-auto w-full max-w-4xl space-y-5">
      <div>
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
          { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
          { etiket: cevir("FTP Hesabı") },
        ]} />
        <h1 className="mb-1 text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("FTP Hesapları")}</h1>
        {domain && <p className="text-sm text-slate-500"><Link to={`/abonelikler/${id}`} className="font-medium text-brand-600 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300">{domain.alan_adi}</Link></p>}
      </div>

      {/* ── Ana FTP hesabı ── */}
      {domain && (
        <section className="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-600 dark:bg-dark-700 sm:p-6">
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Ana FTP hesabı")}</h2>
          <p className="mb-3 mt-1 text-sm text-slate-500 dark:text-dark-300">{cevir("Hosting hesabınızın ana kullanıcısı: tüm dosyalarınıza erişir, panel girişinizle aynı parolayı kullanır.")}</p>
          <div className="divide-y divide-slate-100 dark:divide-dark-600">
            <KopyaSatiri etiket={cevir("Sunucu")} deger={domain.ftp_host} onKopya={kopyala} />
            <KopyaSatiri etiket={cevir("Port (FTP)")} deger="21" onKopya={kopyala} />
            <KopyaSatiri etiket={cevir("Port (SFTP)")} deger="22" onKopya={kopyala} />
            <KopyaSatiri etiket={cevir("Kullanıcı adı")} deger={domain.ftp_user} onKopya={kopyala} />
            <KopyaSatiri etiket={cevir("Ev dizini")} deger={anaDizin} onKopya={kopyala} />
          </div>

          <div className="mt-5 border-t border-slate-200 pt-5 dark:border-dark-600">
            <h3 className="mb-3 text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Parola sıfırlama")}</h3>
            {ssoOturum() ? <SsoParolaNotu /> : <>
            <ParolaAlani deger={ozelPw} onDeger={setOzelPw} goster={anaGoster} onGoster={setAnaGoster} onKopya={kopyala} yer={cevir("Yeni FTP parolası girin")} />
            <div className="mt-3 flex justify-end">
              <Button onClick={anaParolaSifirla} yukleniyor={anaIsleniyor} disabled={!ozelPw} color="primary" ikon={I.disket}>{cevir("Parolayı ayarla")}</Button>
            </div>
            </>}
            <p className="mt-3 text-xs text-slate-500 dark:text-dark-300">{cevir("FTP bağlantısı şifrelenmez; şifrelemek için SFTP (port 22) kullanın.")}</p>
          </div>
        </section>
      )}

      {/* ── Ek FTP hesapları ── */}
      <section className="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-600 dark:bg-dark-700 sm:p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1 basis-64">
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">
              {cevir("Ek FTP hesapları")}
              {liste && <span className="ms-2 text-sm font-normal text-slate-500 dark:text-dark-300">{liste.kullanim} / {liste.azami}</span>}
            </h2>
            <p className="mt-1 text-sm text-slate-500 dark:text-dark-300">{cevir("Belirli klasörlere özel hesaplar açın; her hesap yalnız kendi klasörünü görür. Ek hesaplar yalnız FTP (port 21) ile bağlanır, panele giriş yapamaz.")}</p>
          </div>
          {liste && liste.hesaplar.length > 0 && (
            <Button color="primary" ikon={I.arti} onClick={yeniAc} disabled={sinirDolu} title={sinirDolu ? cevir("Hesap sınırına ulaştınız") : undefined}>{cevir("Yeni FTP hesabı")}</Button>
          )}
        </div>

        {liste?.host_askida && <p className="mt-3 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-500/10 dark:text-amber-300">{cevir("Hosting askıda: ek hesaplar da kapalı.")}</p>}

        <div className="mt-4">
          {!liste && !listeHata && <p className="py-6 text-center text-sm text-slate-500">{cevir("Yükleniyor…")}</p>}
          {listeHata && !liste && <p className="py-6 text-center text-sm text-slate-500">{cevir("Hesaplar okunamadı")}</p>}
          {liste && liste.hesaplar.length === 0 && (
            <EmptyState baslik={cevir("Henüz ek FTP hesabı yok")}
              aciklama={cevir("Bir klasöre özel hesap açarak, ana parolanızı paylaşmadan başkalarına yalnız o klasörde erişim verebilirsiniz.")}
              buton={{ etiket: cevir("Yeni FTP hesabı"), onClick: yeniAc, disabled: sinirDolu, ipucu: sinirDolu ? cevir("Hesap sınırına ulaştınız") : undefined }} />
          )}
          {liste && liste.hesaplar.length > 0 && (
            <ul className="divide-y divide-slate-100 dark:divide-dark-600">
              {liste.hesaplar.map(h => (
                <li key={h.kullanici} className="py-3">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                    <div className="min-w-0 flex-1 basis-56">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-mono text-sm font-medium text-slate-900 dark:text-slate-100" title={h.kullanici}>{h.kullanici}</span>
                        <Button variant="flat" boyut="sm" isIcon ikon={I.kopya} className="size-7 shrink-0" title={cevir("Kopyala")} aria-label={`${cevir("Kopyala")}: ${h.kullanici}`} onClick={() => kopyala(h.kullanici)} />
                      </div>
                      <p className="mt-0.5 truncate font-mono text-xs text-slate-500 dark:text-dark-300" title={`${anaDizin}${h.dizin}`}>{h.dizin}</p>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge color="info" variant="soft">{cevir("Okuma-Yazma")}</Badge>
                      <Badge color={h.durum === 'aktif' ? 'success' : 'warning'} variant="soft">{cevir(h.durum === 'aktif' ? "Aktif" : "Askıda")}</Badge>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Button variant="outlined" boyut="sm" ikon={I.anahtar} onClick={() => parolaAc(h)}>{cevir("Parola")}</Button>
                      <Button variant="outlined" boyut="sm" ikon={I.kalem} onClick={() => duzenleAc(h)}>{cevir("Düzenle")}</Button>
                      <Button variant="outlined" boyut="sm" ikon={h.etkin ? I.durakla : I.oynat} yukleniyor={satirIslem === h.kullanici} onClick={() => etkinDegistir(h)}>{cevir(h.etkin ? "Askıya al" : "Etkinleştir")}</Button>
                      <Button color="error" variant="outlined" boyut="sm" ikon={I.cop} onClick={() => setSilHesap(h)}>{cevir("Sil")}</Button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      {/* ── Yeni hesap ── */}
      <Modal acik={yeniAcik} baslik={sonuc ? cevir("Hesap oluşturuldu") : cevir("Yeni FTP hesabı")} genislik="lg"
        ton={sonuc ? 'basari' : undefined} onKapat={() => setYeniAcik(false)} kapatilamaz={kaydediyor}
        alt={sonuc ? (
          <>
            <Button variant="outlined" ikon={I.kopya} onClick={async () => { if (await panoyaKopyala(baglantiMetni(sonuc.kullanici, sonuc.dizin, sonuc.parola))) toast.basari(cevir("Bilgiler kopyalandı")) }}>{cevir("Bilgileri kopyala")}</Button>
            <Button color="primary" ikon={I.onay} onClick={() => setYeniAcik(false)}>{cevir("Tamam")}</Button>
          </>
        ) : (
          <>
            <Button variant="outlined" ikon={I.kapat} onClick={() => setYeniAcik(false)} disabled={kaydediyor}>{cevir("İptal")}</Button>
            <Button color="primary" ikon={I.arti} onClick={olustur} yukleniyor={kaydediyor} disabled={!yeniGecerli}>{cevir("Oluştur")}</Button>
          </>
        )}>
        {sonuc ? (
          <div className="space-y-3">
            <p className="text-sm text-slate-600 dark:text-dark-200">{cevir("Parola bir daha gösterilmez; şimdi kopyalayın.")}</p>
            <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 px-3 dark:divide-dark-600 dark:border-dark-500">
              <KopyaSatiri etiket={cevir("Sunucu")} deger={domain?.ftp_host || ''} onKopya={kopyala} />
              <KopyaSatiri etiket={cevir("Port")} deger="21" onKopya={kopyala} />
              <KopyaSatiri etiket={cevir("Kullanıcı adı")} deger={sonuc.kullanici} onKopya={kopyala} />
              <KopyaSatiri etiket={cevir("Parola")} deger={sonuc.parola} onKopya={kopyala} />
              <KopyaSatiri etiket={cevir("Klasör")} deger={sonuc.dizin} onKopya={kopyala} />
            </div>
          </div>
        ) : (
          <div className="space-y-5">
            <div>
              <label className="input-label" htmlFor="ek-ad">{cevir("Kullanıcı adı (önek sabit)")}</label>
              <div className="mt-1.5 flex items-stretch gap-2">
                <span className="flex max-w-[45%] shrink-0 items-center truncate rounded-md bg-gray-100 px-3 font-mono text-sm text-gray-600 dark:bg-dark-600 dark:text-dark-200" title={onek}>{onek}</span>
                <div className="min-w-0 flex-1">
                  <Input id="ek-ad" value={ek} onChange={e => setEk(e.target.value)} placeholder="foto" autoComplete="off" spellCheck={false}
                    error={ek !== '' && !ekGecerli ? cevir("Kullanıcı adı yalnız küçük harf, rakam ve alt çizgi içerebilir (en çok 24).") : undefined}
                    classNames={{ input: 'font-mono text-sm' }} />
                </div>
              </div>
              <p className="mt-1 text-xs text-gray-500 dark:text-dark-300">{cevir("küçük harf, rakam, alt çizgi")}</p>
            </div>
            <FtpKlasorSecici domainId={id || ''} deger={dizin} onDeger={setDizin} olustur={dizinOlustur} onOlustur={setDizinOlustur} t={cevir} />
            {dizin !== '' && !dizinGecerli && <p className="-mt-3 text-xs text-error">{cevir("Bir klasör seçin (ev dizininin kendisi olamaz).")}</p>}
            <div>
              <p className="input-label">{cevir("İzin")}</p>
              <div className="mt-1.5 flex flex-wrap items-center gap-2">
                <Badge color="info" variant="soft">{cevir("Okuma-Yazma")}</Badge>
                <span className="text-xs text-gray-500 dark:text-dark-300">{cevir("Salt-okuma izni henüz yok: hesaplar okuma-yazma çalışır.")}</span>
              </div>
            </div>
            <div>
              <ParolaAlani etiket={cevir("Parola")} deger={pw} onDeger={setPw} goster={pwGoster} onGoster={setPwGoster} onKopya={kopyala} />
              {pw !== '' && !parolaUygun(pw) && <p className="mt-1 text-xs text-error">{cevir("Parola en az 12 karakter olmalı ve harf ile rakam içermeli.")}</p>}
            </div>
            <div>
              <h3 className="mb-1 text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Bağlantı bilgileri")}</h3>
              <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 px-3 dark:divide-dark-600 dark:border-dark-500">
                <KopyaSatiri etiket={cevir("Sunucu")} deger={domain?.ftp_host || ''} onKopya={kopyala} />
                <KopyaSatiri etiket={cevir("Port")} deger="21" onKopya={kopyala} />
                <KopyaSatiri etiket={cevir("Kullanıcı adı")} deger={tamKullanici} onKopya={kopyala} />
                <KopyaSatiri etiket={cevir("Klasör")} deger={dizin || '/'} onKopya={kopyala} />
              </div>
              <div className="mt-2 flex justify-end">
                <Button variant="outlined" boyut="sm" ikon={I.kopya} onClick={async () => { if (await panoyaKopyala(baglantiMetni(tamKullanici, dizin || '/'))) toast.basari(cevir("Bilgiler kopyalandı")) }}>{cevir("Bilgileri kopyala")}</Button>
              </div>
            </div>
          </div>
        )}
      </Modal>

      {/* ── Düzenle ── */}
      <Modal acik={!!duzenle} baslik={cevir("Hesabı düzenle")} genislik="md" onKapat={() => setDuzenle(null)} kapatilamaz={kaydediyor}
        alt={<>
          <Button variant="outlined" ikon={I.kapat} onClick={() => setDuzenle(null)} disabled={kaydediyor}>{cevir("İptal")}</Button>
          <Button color="primary" ikon={I.disket} onClick={duzenleKaydet} yukleniyor={kaydediyor} disabled={!dizinGecerli}>{cevir("Kaydet")}</Button>
        </>}>
        {duzenle && (
          <div className="space-y-4">
            <p className="truncate font-mono text-sm text-slate-700 dark:text-dark-100" title={duzenle.kullanici}>{duzenle.kullanici}</p>
            <FtpKlasorSecici domainId={id || ''} deger={dizin} onDeger={setDizin} olustur={dizinOlustur} onOlustur={setDizinOlustur} t={cevir} />
          </div>
        )}
      </Modal>

      {/* ── Parola ── */}
      <Modal acik={!!parolaHesap} baslik={cevir("Parolayı değiştir")} genislik="md" onKapat={() => setParolaHesap(null)} kapatilamaz={kaydediyor}
        alt={<>
          <Button variant="outlined" ikon={I.kapat} onClick={() => setParolaHesap(null)} disabled={kaydediyor}>{cevir("İptal")}</Button>
          <Button color="primary" ikon={I.disket} onClick={parolaKaydet} yukleniyor={kaydediyor} disabled={!parolaUygun(pw)}>{cevir("Kaydet")}</Button>
        </>}>
        {parolaHesap && (
          <div className="space-y-3">
            <p className="truncate font-mono text-sm text-slate-700 dark:text-dark-100" title={parolaHesap.kullanici}>{parolaHesap.kullanici}</p>
            <ParolaAlani etiket={cevir("Yeni parola")} deger={pw} onDeger={setPw} goster={pwGoster} onGoster={setPwGoster} onKopya={kopyala} />
            {pw !== '' && !parolaUygun(pw) && <p className="text-xs text-error">{cevir("Parola en az 12 karakter olmalı ve harf ile rakam içermeli.")}</p>}
          </div>
        )}
      </Modal>

      <ConfirmDialog acik={!!silHesap} baslik={cevir("Hesabı sil")} ton="tehlike" onayIkonu={I.cop} onayMetni={cevir("Sil")}
        mesaj={silHesap ? `${silHesap.kullanici} ${cevir("hesabı silinsin mi? Klasördeki dosyalar silinmez; yalnız bu FTP girişi kalkar.")}` : ''}
        onOnay={sil} onIptal={() => setSilHesap(null)} />
    </div>
  )
}
