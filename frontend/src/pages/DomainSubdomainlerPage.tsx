import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, butonSinifi } from '@/components/ui'

type Sub = { id: number; alt_ad: string; tam_ad: string; php_surum: string; docroot: string; created_at: string }


const SUBD_EN: Record<string, string> = {
  "Henüz subdomain yok.": "No subdomains yet.",
  "SSL kurulamadı": "Failed to install SSL",
  "Öz-imzalı SSL kur": "Install self-signed SSL",
  "Küçük harf, rakam ve tire. Örn:": "Lowercase, digits and hyphen. E.g:",
  "{0} oluşturuldu. DNS A kaydı eklendi.": "{0} created. DNS A record added.",
  "Oluşturulamadı": "Could not be created",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Emin misiniz?": "Are you sure?",
  "{0} subdomaini silinsin mi?\nvhost + dosyaları (docroot) + DNS kaydı kaldırılır. Geri alınamaz.": "Delete the subdomain {0}?\nThe vhost + its files (docroot) + DNS record will be removed. This cannot be undone.",
  "Silinemedi": "Could not be deleted",
  "{0} için SSL kuruldu ({1}). Artık https:// ile erişilebilir.": "SSL installed for {0} ({1}). It is now accessible via https://.",
  "öz-imzalı": "self-signed",
  "öz-imza": "self-signed",
  "Anasayfa": "Home",
  "Subdomainler": "Subdomains",
  "Bu domain altında alt alan adları (örn.": "Create subdomains under this domain (e.g.",
  ") oluşturun; her biri ayrı web dizinine sahiptir.": "); each one has its own web directory.",
  "Yeni Subdomain": "New Subdomain",
  "Alt Alan": "Subdomain",
  "Oluşturuluyor…": "Creating…",
  "Subdomain Ekle": "Add Subdomain",
  "Mevcut Subdomainler": "Existing Subdomains",
  "Let's Encrypt SSL kur": "Install Let's Encrypt SSL",
  "Aboneliğe dön": "Back to subscription",
  "Subdomain hemen web sunucusunda tanımlanır. Erişim için alan adınızın DNS'i (A kaydı) bu sunucuya yönlendirilmiş olmalıdır.": "The subdomain is defined on the web server immediately. For access, your domain's DNS (A record) must point to this server.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SUBD_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainSubdomainlerPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [liste, setListe] = useState<Sub[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setOk] = useState<string | null>(null)
  const [altAd, setAltAd] = useState('')
  const [kaydediyor, setKaydediyor] = useState(false)
  const [silinen, setSilinen] = useState<number | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<Sub[]>(`/domains/${id}/subdomain`).then(r => setListe(r.data || [])).catch(e => {
      const m = apiHata(e)
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }).finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function olustur(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setOk(null); setKaydediyor(true)
    try {
      const { data } = await api.post(`/domains/${id}/subdomain`, { alt_ad: altAd.trim() })
      const m = cevirT(cevir("{0} oluşturuldu. DNS A kaydı eklendi."), data.tam_ad)
      setOk(m)
      toast.basari(cevir("Kaydedildi"), m)
      setAltAd('')
      yukle()
    } catch (err) {
      const m = apiHata(err, cevir("Oluşturulamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setKaydediyor(false) }
  }

  async function sil(s: Sub) {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("{0} subdomaini silinsin mi?\nvhost + dosyaları (docroot) + DNS kaydı kaldırılır. Geri alınamaz."), s.tam_ad), ton: 'tehlike', onayIkonu: I.cop }))) return
    setHata(null); setOk(null); setSilinen(s.id)
    try { await api.delete(`/domains/${id}/subdomain/${s.id}`); yukle() }
    catch (err) {
      const m = apiHata(err, cevir("Silinemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setSilinen(null) }
  }

  const [sslMesgul, setSslMesgul] = useState<number | null>(null)
  async function sslKur(s: Sub, tip: 'letsencrypt' | 'self-signed') {
    setHata(null); setOk(null); setSslMesgul(s.id)
    try {
      await api.post(`/domains/${id}/subdomain/${s.id}/ssl`, { tip })
      const m = cevirT(cevir("{0} için SSL kuruldu ({1}). Artık https:// ile erişilebilir."), s.tam_ad, tip === 'letsencrypt' ? "Let's Encrypt" : cevir("öz-imzalı"))
      setOk(m)
      toast.basari(cevir("Kaydedildi"), m)
    } catch (err) {
      const m = apiHata(err, cevir("SSL kurulamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setSslMesgul(null) }
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: cevir("Subdomainler") },
      ]} />
      <div className="flex items-center gap-3 mb-1">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6 text-slate-400 dark:text-slate-500"><path d="M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.4 3.8 5.6 3.8 9s-1.3 6.6-3.8 9c-2.5-2.4-3.8-5.6-3.8-9S9.5 5.4 12 3z"/></svg>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Subdomainler")}</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{cevir("Bu domain altında alt alan adları (örn.")} <span className="font-mono">blog.alan.com</span>{cevir(") oluşturun; her biri ayrı web dizinine sahiptir.")}</p>

      <form onSubmit={olustur} className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg p-4 mb-5">
        <h3 className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-3">{cevir("Yeni Subdomain")}</h3>
        <div className="flex flex-wrap items-end gap-2">
          <label className="block">
            <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{cevir("Alt Alan")}</span>
            <input value={altAd} onChange={e => setAltAd(e.target.value.toLowerCase())} required placeholder="blog"
              className="mt-1 w-48 px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
          <Button type="submit" color="primary" ikon={I.arti} disabled={!altAd.trim()} yukleniyor={kaydediyor}>
            {kaydediyor ? cevir("Oluşturuluyor…") : cevir("Subdomain Ekle")}
          </Button>
        </div>
        <p className="text-[11px] text-slate-400 mt-2">{cevir("Küçük harf, rakam ve tire. Örn:")} <span className="font-mono">blog</span>, <span className="font-mono">shop</span>, <span className="font-mono">api</span>.</p>
      </form>

      <div className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg p-4">
        <h3 className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-3">{cevir("Mevcut Subdomainler")}</h3>
        {yuk ? <div className="text-sm text-slate-400 py-2">{cevir("Yükleniyor…")}</div>
          : liste.length === 0 ? (
            <div className="text-center py-6">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6 mx-auto mb-1 text-slate-300 dark:text-slate-600"><path d="M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.4 3.8 5.6 3.8 9s-1.3 6.6-3.8 9c-2.5-2.4-3.8-5.6-3.8-9S9.5 5.4 12 3z"/></svg>
              <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz subdomain yok.")}</p>
            </div>
          ) : (
            <ul className="divide-y divide-slate-100 dark:divide-dark-600/60">
              {liste.map(s => (
                <li key={s.id} className="flex items-center justify-between gap-3 py-2.5">
                  <div className="min-w-0">
                    <a href={`http://${s.tam_ad}`} target="_blank" rel="noreferrer" className="font-mono text-sm text-brand-600 dark:text-brand-400 hover:underline">{s.tam_ad}</a>
                    <div className="text-[11px] text-slate-400 font-mono truncate">{s.docroot} · PHP {s.php_surum}</div>
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5 shrink-0">
                    <Button color="primary" variant="soft" boyut="sm" ikon={I.kalkan} onClick={() => sslKur(s, 'letsencrypt')} yukleniyor={sslMesgul === s.id} title={cevir("Let's Encrypt SSL kur")}>
                      Let's Encrypt
                    </Button>
                    <Button variant="outlined" boyut="sm" ikon={I.kalkan} onClick={() => sslKur(s, 'self-signed')} disabled={sslMesgul === s.id} title={cevir("Öz-imzalı SSL kur")}>
                      {cevir("öz-imza")}
                    </Button>
                    <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => sil(s)} yukleniyor={silinen === s.id}>{cevir("Sil")}</Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        <p className="text-[11px] text-slate-400 mt-3 pt-3 border-t border-slate-100 dark:border-dark-600/60">
          {cevir("Subdomain hemen web sunucusunda tanımlanır. Erişim için alan adınızın DNS'i (A kaydı) bu sunucuya yönlendirilmiş olmalıdır.")}
        </p>
      </div>

      <div className="mt-4">
        <Link to={`/abonelikler/${id}`} className={butonSinifi({ variant: 'flat', boyut: 'sm' })}>
          <Ikon d={I.geri} className="size-3.5 shrink-0 -ms-0.5 me-1.5" />{cevir("Aboneliğe dön")}
        </Link>
      </div>
    </div>
  )
}
