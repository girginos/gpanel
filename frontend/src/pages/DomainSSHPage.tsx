import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useRol, yoneticiMi } from '@/lib/rol'

type Durum = {
  alan_adi: string
  kullanici: string
  aktif: boolean
  shell: string
  ssh_host: string
  ssh_port: number
  anahtar_var: boolean
  is_demo: boolean
}


const SSH_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "ssh-ed25519 AAAA... kullanici@makine": "ssh-ed25519 AAAA... user@machine",
  "Anasayfa": "Home",
  "Anahtar kaydedilemedi": "Failed to save key",
  "hesabı için kabuk (shell) erişimi.": "shell access for the account.",
  "SSH KAPALI": "SSH OFF",
  "Açık:": "On:",
  "Kapalı:": "Off:",
  "Mevcut:": "Current:",
  "SSH'i Kapat": "Disable SSH",
  "SSH'i Aç": "Enable SSH",
  "Bağlantı Bilgisi": "Connection Info",
  "Parola:": "Password:",
  "Parola yerine anahtarla giriş için genel anahtarınızı ekleyin.": "To log in with a key instead of a password, add your public key.",
  "Anahtarı Kaydet": "Save Key",
  "Bağlantı komutu": "Connection command",
  "Boş bırakıp kaydederseniz tüm anahtarlar silinir.": "If you save it empty, all keys are deleted.",
  "Demo domainde SSH değiştirilemez.": "SSH cannot be changed on a demo domain.",
  "FTP hesabınızla aynı": "Same as your FTP account",
  "Henüz anahtar tanımlı değil.": "No key defined yet.",
  "Kabuk Erişimi": "Shell Access",
  "SSH AÇIK": "SSH ON",
  "SSH Erişimi": "SSH Access",
  "SSH Genel Anahtarı (authorized_keys)": "SSH Public Key (authorized_keys)",
  "SSH erişimi açıldı.": "SSH access enabled.",
  "SSH erişimi kapatıldı.": "SSH access disabled.",
  "Şu an bir anahtar tanımlı.": "A key is currently defined.",
  "— SSH açıkken otomatik eşitlenir. Alternatif olarak aşağıya SSH genel anahtarı ekleyebilirsiniz.": "— Synced automatically while SSH is on. Alternatively you can add an SSH public key below.",
  "SSH anahtarları temizlendi.": "SSH keys cleared.",
  "SSH anahtarı kaydedildi.": "SSH key saved.",
  "Aboneliğe dön": "Back to subscription",
  "SSH erişimi ve anahtarlar yalnız sunucu yöneticisi tarafından değiştirilebilir. Değişiklik için hizmet sağlayıcınızla iletişime geçin.": "SSH access and keys can only be changed by the server administrator. Contact your service provider for changes.",
  "— SSH açıkken otomatik eşitlenir.": "— Synced automatically while SSH is on.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SSH_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainSSHPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  // PUT /domains/{id}/ssh ve /ssh/anahtar AdminOnly: bayi ve müşteri durumu ve bağlantı
  // bilgisini SALT OKUNUR görür (GET /domains/{id}/ssh MusteriScope → kendi hesabı).
  const yonetici = yoneticiMi(useRol())
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [anahtar, setAnahtar] = useState('')
  // Yalnız görünüm: isleniyor iki işte ortak — spinner tetikleyen butonda dönsün.
  const [islem, setIslem] = useState<'toggle' | 'anahtar' | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Durum>(`/domains/${id}/ssh`)
      .then(r => setD(r.data))
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function toggle(aktif: boolean) {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/ssh`, { aktif })
      const m = aktif ? cevir("SSH erişimi açıldı.") : cevir("SSH erişimi kapatıldı.")
      setBasari(m)
      toast.basari(m)
      setTimeout(() => setBasari(null), 4000)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("İşlem başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally { setIsleniyor(false) }
  }

  async function anahtarKaydet() {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const { data } = await api.put(`/domains/${id}/ssh/anahtar`, { anahtar })
      const m = data.anahtar_var ? cevir("SSH anahtarı kaydedildi.") : cevir("SSH anahtarları temizlendi.")
      setBasari(m)
      toast.basari(m)
      setTimeout(() => setBasari(null), 4000)
      setAnahtar('')
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Anahtar kaydedilemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally { setIsleniyor(false) }
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">{cevir("Yükleniyor…")}</div>
  if (!d) return <div className="px-4 py-4 sm:px-6 sm:py-5"><div className="text-sm text-red-600">{hata || cevir("Bulunamadı")}</div></div>

  const sshKomut = `ssh ${d.kullanici}@${d.ssh_host} -p ${d.ssh_port}`

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-3xl mx-auto">
        <Breadcrumb items={[
          { etiket: cevir("Anasayfa"), href: '/' },
          { etiket: cevir("Domainler"), href: '/domainler' },
          { etiket: d.alan_adi, href: `/abonelikler/${id}` },
          { etiket: cevir("SSH Erişimi") },
        ]} />

        <div className="flex items-start justify-between gap-4 mb-1">
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("SSH Erişimi")}</h1>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
              <span className="font-mono">{d.alan_adi}</span> {cevir("hesabı için kabuk (shell) erişimi.")}
            </p>
          </div>
          <Badge color={d.aktif ? 'success' : 'neutral'} variant="soft" className="shrink-0 gap-1.5 px-2.5 font-semibold">
            <span className="size-2 rounded-full bg-current" aria-hidden="true" />
            {d.aktif ? cevir("SSH AÇIK") : cevir("SSH KAPALI")}
          </Badge>
        </div>

        {!yonetici && (
          <UyariKutusu ton="bilgi" kucuk className="mt-3 mb-4">
            {cevir("SSH erişimi ve anahtarlar yalnız sunucu yöneticisi tarafından değiştirilebilir. Değişiklik için hizmet sağlayıcınızla iletişime geçin.")}
          </UyariKutusu>
        )}

        {/* Durum + toggle */}
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4 shadow-xs">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Kabuk Erişimi")}</h3>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
                {cevir("Açık:")} <code className="font-mono">/bin/bash</code> · {cevir("Kapalı:")} <code className="font-mono">/usr/sbin/nologin</code>.
                {' '}{cevir("Mevcut:")} <code className="font-mono">{d.shell || '—'}</code>
              </p>
            </div>
            {!yonetici ? null : d.aktif ? (
              <Button color="error" variant="soft" onClick={() => { setIslem('toggle'); toggle(false) }} disabled={isleniyor || d.is_demo}
                yukleniyor={isleniyor && islem === 'toggle'} ikon={I.kilit} className="shrink-0">
                {cevir("SSH'i Kapat")}
              </Button>
            ) : (
              <Button color="primary" onClick={() => { setIslem('toggle'); toggle(true) }} disabled={isleniyor || d.is_demo}
                yukleniyor={isleniyor && islem === 'toggle'} ikon={I.kilitAcik} className="shrink-0">
                {cevir("SSH'i Aç")}
              </Button>
            )}
          </div>
          {yonetici && d.is_demo && <p className="mt-3 text-xs text-amber-600 dark:text-amber-400">{cevir("Demo domainde SSH değiştirilemez.")}</p>}
        </div>

        {/* Bağlantı bilgisi */}
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4 shadow-xs">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Bağlantı Bilgisi")}</h3>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
            <Bilgi etiket={cevir("Kullanıcı")} deger={d.kullanici} />
            <Bilgi etiket={cevir("Sunucu")} deger={d.ssh_host} />
            <Bilgi etiket="Port" deger={String(d.ssh_port)} />
          </div>
          <div className="mt-3">
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Bağlantı komutu")}</label>
            <div className="mt-1 flex items-center gap-2">
              <code className="flex-1 px-3 py-2 bg-dark-800 text-slate-100 rounded-lg text-xs font-mono overflow-x-auto">{sshKomut}</code>
              <Button variant="outlined" boyut="sm" ikon={I.kopya} onClick={() => navigator.clipboard?.writeText(sshKomut)} className="shrink-0">{cevir("Kopyala")}</Button>
            </div>
          </div>
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{cevir("Parola:")} <strong>{cevir("FTP hesabınızla aynı")}</strong> {yonetici
            ? cevir("— SSH açıkken otomatik eşitlenir. Alternatif olarak aşağıya SSH genel anahtarı ekleyebilirsiniz.")
            : cevir("— SSH açıkken otomatik eşitlenir.")}</p>
        </div>

        {/* SSH Public Key */}
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 shadow-xs">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("SSH Genel Anahtarı (authorized_keys)")}</h3>
          <p className={`text-xs text-slate-500 dark:text-slate-400 ${yonetici ? 'mb-3' : ''}`}>
            {yonetici && <>{cevir("Parola yerine anahtarla giriş için genel anahtarınızı ekleyin.")} </>}{d.anahtar_var
              ? <span className="text-emerald-600 dark:text-emerald-400">{cevir("Şu an bir anahtar tanımlı.")}</span>
              : <span className="text-slate-500">{cevir("Henüz anahtar tanımlı değil.")}</span>}
          </p>
          {yonetici && <>
          <textarea
            value={anahtar}
            onChange={e => setAnahtar(e.target.value)}
            rows={4}
            spellCheck={false}
            placeholder={cevir("ssh-ed25519 AAAA... kullanici@makine")}
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-xs font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
          />
          <div className="mt-3 flex items-center justify-between">
            <p className="text-xs text-slate-400">{cevir("Boş bırakıp kaydederseniz tüm anahtarlar silinir.")}</p>
            <Button color="primary" onClick={() => { setIslem('anahtar'); anahtarKaydet() }} disabled={isleniyor || d.is_demo}
              yukleniyor={isleniyor && islem === 'anahtar'} ikon={I.disket} className="shrink-0">
              {cevir("Anahtarı Kaydet")}
            </Button>
          </div>
          </>}
        </div>

        <div className="mt-4">
          <Link to={`/abonelikler/${id}`} className="inline-flex items-center gap-1.5 text-sm text-brand-600 dark:text-brand-400"><Ikon d={I.geri} className="size-4 shrink-0" />{cevir("Aboneliğe dön")}</Link>
        </div>
      </div>
    </div>
  )
}

function Bilgi({ etiket, deger }: { etiket: string; deger: string }) {
  return (
    <div className="px-3 py-2 bg-slate-50 dark:bg-dark-800/40 rounded-lg border border-slate-200 dark:border-dark-600">
      <div className="text-[10px] uppercase tracking-wider text-slate-400">{etiket}</div>
      <div className="font-mono text-slate-800 dark:text-slate-200 truncate">{deger}</div>
    </div>
  )
}
