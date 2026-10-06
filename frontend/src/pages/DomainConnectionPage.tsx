import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { parolaUret, panoyaKopyala } from '@/lib/parola'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button, Badge, Card } from '@/components/ui'
import Modal from '@/components/Modal'
import SsoParolaNotu from '@/components/SsoParolaNotu'
import { ssoOturum } from '@/store/auth'


const CONN_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "Değer üstüne tıkla → otomatik kopyalanır": "Click a value → it's copied automatically",
  "FTP yönetimine git": "Go to FTP management",
  "Panoya kopyalanamadı": "Failed to copy to clipboard",
  "Sistem kullanıcısı": "System user",
  "Tıkla → kopyala": "Click → copy",
  "Veritabanları yönetimine git": "Go to database management",
  "Web kökü": "Web root",
  "veritabanı yok": "no database",
  "Parola güncellendi": "Password updated",
  "Türkçe": "English",
  "Anasayfa": "Home",
  "Domainler": "Domains",
  "Bağlantı Bilgisi": "Connection Info",
  "Ev dizini": "Home directory",
  "Sunucu": "Server",
  "Kullanıcı adı": "Username",
  "Parola": "Password",
  "Veritabanı": "Database",
  "Otomatik kopyalanamadı. Ctrl+C basıp Enter'a tıklayın:": "Auto-copy failed. Press Ctrl+C and click Enter:",
  "Şifreyi Göster / Yenile": "Show / Reset Password",
  "Parolası": "Password",
  "Kullanıcı:": "User:",
  "Kapat": "Close",
  "Kopyalandı": "Copied",
  "Oluştur": "Generate",
  "Güçlü parola oluştur": "Generate strong password",
  "Parolayı göster": "Show password",
  "Parolayı gizle": "Hide password",
  "Göster": "Show",
  "Gizle": "Hide",
  "Parolayı kopyala": "Copy password",
  "Kopyala": "Copy",
  "Parola panoya kopyalandı": "Password copied to clipboard",
  "Kaydetmeden önce kopyalayın — kaydedildikten sonra bir daha görüntülenemez.": "Copy it before saving — it cannot be viewed again after it is saved.",
  "“Oluştur” ile 16 karakterlik güçlü parola üretebilirsiniz.": "Use “Generate” to create a strong 16-character password.",
  "Yeni parola": "New password",
  "Yeni parola girin (≥8)": "Enter a new password (≥8)",
  "Parola en az 8 karakter olmalı": "Password must be at least 8 characters",
  "Parola güncellenemedi": "Failed to update password",
  "Parolayı güncelle": "Update password",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CONN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function panoYaz(text: string): boolean {
  // 1) Modern API (HTTPS / localhost only) — kullanıcı gesture içindeyse async ok
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(hataYakala(cevir("Panoya kopyalanamadı")))
    return true
  }
  // 2) Fallback: textarea + execCommand
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '0'
    ta.style.left = '0'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, text.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) return true
  } catch {}
  // 3) Son çare: prompt — kullanıcı Ctrl+C ile manuel kopyalar
  try {
    window.prompt(cevir("Otomatik kopyalanamadı. Ctrl+C basıp Enter'a tıklayın:"), text)
    return true
  } catch {
    return false
  }
}



type Domain = {
  id: number; alan_adi: string; ipv4: string
  ftp_host: string; ftp_user: string
  db_host: string; db_user: string; db_adi: string
  sistem_kullanici: string; web_root: string
}

export default function DomainConnectionPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [, setHata] = useState<string | null>(null)
  const [kopya, setKopya] = useState<string | null>(null)
  const [parolaModal, setParolaModal] = useState<{ tip: 'ftp' | 'db'; cikti?: string } | null>(null)

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  function kopyala(deg: string) {
    panoYaz(deg)
    setKopya(deg)
    setTimeout(() => setKopya(null), 1800)
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Bağlantı Bilgisi") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Bağlantı Bilgisi")}</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}<span className="text-xs text-slate-400 dark:text-slate-500">{cevir("Değer üstüne tıkla → otomatik kopyalanır")}</span>
        </p>
      )}

      {domain && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <Kart baslik="FTP / SFTP" renk="sky" ikon={I.klasor}>
            <Sat e={cevir(cevir("Sunucu"))} d={domain.ftp_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="21" onKopya={kopyala} kopya={kopya} />
            <Sat e={cevir("Kullanıcı adı")} d={domain.ftp_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e={cevir("Parola")} id={id!} tip="ftp" onAc={() => setParolaModal({ tip: 'ftp' })} />
            <Sat e={cevir("Ev dizini")} d={`/home/${domain.sistem_kullanici}`} onKopya={kopyala} kopya={kopya} mono />
            <Link to={`/abonelikler/${id}/ftp`} className="mt-2 flex items-center gap-1 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{cevir("FTP yönetimine git")}<Ikon d={I.ileri} className="size-4 shrink-0" /></Link>
          </Kart>

          <Kart baslik="MySQL / MariaDB" renk="violet" ikon={I.db}>
            <Sat e={cevir(cevir("Sunucu"))} d={domain.db_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="3306" onKopya={kopyala} kopya={kopya} />
            <Sat e={cevir("Veritabanı")} d={domain.db_adi} onKopya={kopyala} kopya={kopya} mono />
            <Sat e={cevir("Kullanıcı adı")} d={domain.db_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e={cevir("Parola")} id={id!} tip="db" onAc={() => setParolaModal({ tip: 'db' })} />
            <Link to={`/abonelikler/${id}/veritabanlari`} className="mt-2 flex items-center gap-1 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{cevir("Veritabanları yönetimine git")}<Ikon d={I.ileri} className="size-4 shrink-0" /></Link>
          </Kart>

          <Kart baslik="Web" renk="amber" ikon={I.kure} cift>
            <Sat e={cevir("Web kökü")} d={domain.web_root} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="IPv4" d={domain.ipv4} onKopya={kopyala} kopya={kopya} mono />
            <Sat e={cevir("Sistem kullanıcısı")} d={domain.sistem_kullanici} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="HTTP URL" d={`http://${domain.alan_adi}/`} onKopya={kopyala} kopya={kopya} />
            <Sat e="HTTPS URL" d={`https://${domain.alan_adi}/`} onKopya={kopyala} kopya={kopya} />
          </Kart>
        </div>
      )}

      {parolaModal && (
        <ParolaSifirlaModal
          tip={parolaModal.tip}
          domainId={id!}
          ftpUser={domain?.ftp_user || ''}
          dbUser={domain?.db_user || ''}
          onKapat={() => setParolaModal(null)}
        />
      )}
    </div>
  )
}

function Kart({ baslik, renk, ikon, children, cift }: { baslik: string; renk: string; ikon: string; children: React.ReactNode; cift?: boolean }) {
  const bg: Record<string, string> = {
    sky: 'bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300',
    violet: 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300',
    amber: 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300',
  }
  return (
    <Card className={`bg-white dark:bg-dark-700 p-5 ${cift ? 'lg:col-span-2' : ''}`}>
      <div className="flex items-center gap-2 mb-3">
        <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${bg[renk]}`}>
          <Ikon d={ikon} className="w-5 h-5" />
        </div>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
      </div>
      <dl className="space-y-2 text-sm">{children}</dl>
    </Card>
  )
}

function Sat({ e, d, mono, onKopya, kopya }: { e: string; d: string; mono?: boolean; onKopya?: (s: string) => void; kopya?: string | null }) {
  const aktif = !!onKopya
  const kopyalandi = kopya === d
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-dark-600 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd
        onClick={() => aktif && onKopya!(d)}
        className={`text-right flex items-center gap-2 group ${aktif ? 'cursor-pointer' : ''}`}
        title={aktif ? cevir('Tıkla → kopyala') : ''}
      >
        <span className={`${mono ? 'font-mono text-xs' : 'text-sm'} ${aktif ? 'text-slate-800 dark:text-slate-200 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 transition' : 'text-slate-800 dark:text-slate-200'}`}>
          {d}
        </span>
        {kopyalandi && (
          <Badge color="success" variant="soft" className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 animate-pulse">
            {cevir(cevir("Kopyalandı"))}
          </Badge>
        )}
      </dd>
    </div>
  )
}

function Parola({ e, tip, onAc }: { e: string; id: string; tip: string; onAc: () => void }) {
  // SSO (WHMCS) oturumunda FTP parolası bu panelden değişmez: form yerine açıklama.
  if (tip === 'ftp' && ssoOturum()) {
    return (
      <div className="py-1.5 border-b border-slate-100 dark:border-dark-600 last:border-0">
        <SsoParolaNotu />
      </div>
    )
  }
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-dark-600 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd className="text-right">
        <Button variant="outlined" boyut="sm" ikon={I.anahtar} onClick={onAc}>
          {cevir(cevir("Şifreyi Göster / Yenile"))}
        </Button>
      </dd>
    </div>
  )
}

function ParolaSifirlaModal({ tip, domainId, ftpUser, dbUser, onKapat }:
  { tip: 'ftp' | 'db'; domainId: string; ftpUser: string; dbUser: string; onKapat: () => void }) {
  const toast = useToast()
  const [parola, setParola] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [goster, setGoster] = useState(false)
  const [kopyalandi, setKopyalandi] = useState(false)

  // Kripto-rastgele 16 karakter (lib/parola: karıştırılan 0/O, 1/l harfleri yok). Üretilince
  // AÇIK gösterilir: kullanıcı kaydetmeden önce görüp kopyalayabilsin — kayıttan sonra
  // parola bir daha görüntülenemez.
  function uret() {
    setParola(parolaUret(16)); setGoster(true); setKopyalandi(false)
  }
  async function kopyala() {
    if (await panoyaKopyala(parola)) { setKopyalandi(true); setTimeout(() => setKopyalandi(false), 2000) }
    else toast.hata(cevir("İşlem başarısız"), cevir("Panoya kopyalanamadı"))
  }

  async function kaydet() {
    if (parola.trim().length < 8) { toast.hata(cevir("İşlem başarısız"), cevir("Parola en az 8 karakter olmalı")); return }
    setIsleniyor(true)
    try {
      if (tip === 'ftp') {
        await api.put(`/domains/${domainId}/ftp/password`, { parola })
      } else {
        // İlk DB id'sini al
        const dbs = await api.get<any[]>(`/domains/${domainId}/databases`)
        const main = (dbs.data || [])[0]
        if (!main) throw new Error(cevir("veritabanı yok"))
        await api.put(`/databases/${main.id}/password`, { parola })
      }
      toast.basari(cevir("Parola güncellendi"))
      onKapat()
    } catch (e) { toast.hata(cevir("İşlem başarısız"), apiHata(e, cevir("Parola güncellenemedi"))) }
    finally { setIsleniyor(false) }
  }

  const user = tip === 'ftp' ? ftpUser : dbUser
  const tipAd = tip === 'ftp' ? 'FTP' : cevir("Veritabanı")

  return (
    <Modal acik baslik={`${tipAd} ${cevir("Parolası")}`} onKapat={onKapat} kapatilamaz={isleniyor}
      alt={<>
        <Button variant="outlined" ikon={I.kapat} onClick={onKapat} disabled={isleniyor}>
          {cevir("Kapat")}
        </Button>
        <Button color="primary" ikon={I.disket} onClick={kaydet} disabled={parola.trim().length < 8} yukleniyor={isleniyor}>
          {isleniyor ? cevir('Kaydediliyor…') : cevir("Parolayı güncelle")}
        </Button>
      </>}>
        <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-4 bg-slate-50 dark:bg-dark-800 px-3 py-2 rounded">
          <span className="text-slate-500 dark:text-slate-500">{cevir("Kullanıcı:")}</span> <code className="font-mono text-slate-900 dark:text-slate-100">{user}</code>
        </div>

        {/* Yeni parola — yazılır ya da "Oluştur" ile üretilir; sunucu kaydettikten sonra
            geri okunamaz (write-only), bu yüzden kopyalama burada. */}
        <div>
          <label htmlFor="yeni-parola" className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Yeni parola")}</label>
          <div className="flex gap-2">
            <input id="yeni-parola" type={goster ? 'text' : 'password'} value={parola}
              onChange={e => { setParola(e.target.value); setKopyalandi(false) }} autoComplete="new-password"
              placeholder={cevir("Yeni parola girin (≥8)")} spellCheck={false}
              className="flex-1 min-w-0 px-2.5 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-sm font-mono" />
            <Button type="button" variant="outlined" ikon={I.anahtar} onClick={uret} disabled={isleniyor}
              className="shrink-0" title={cevir("Güçlü parola oluştur")}>
              {cevir("Oluştur")}
            </Button>
            <Button type="button" variant="outlined" isIcon ikon={goster ? I.gozKapali : I.goz}
              onClick={() => setGoster(v => !v)} className="shrink-0 w-10"
              aria-label={goster ? cevir("Parolayı gizle") : cevir("Parolayı göster")}
              title={goster ? cevir("Gizle") : cevir("Göster")} />
            <Button type="button" variant="outlined" isIcon ikon={I.kopya} disabled={!parola} onClick={kopyala}
              className="shrink-0 w-10" aria-label={cevir("Parolayı kopyala")} title={cevir("Kopyala")} />
          </div>
          <p className="text-[11px] mt-1 min-h-4 text-slate-400 dark:text-slate-500" aria-live="polite">
            {kopyalandi
              ? <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400"><Ikon d={I.onay} className="size-3 shrink-0" />{cevir("Parola panoya kopyalandı")}</span>
              : parola ? cevir("Kaydetmeden önce kopyalayın — kaydedildikten sonra bir daha görüntülenemez.")
              : cevir("“Oluştur” ile 16 karakterlik güçlü parola üretebilirsiniz.")}
          </p>
        </div>
    </Modal>
  )
}
