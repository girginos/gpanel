import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'
import { useDialog } from '@/components/Dialog'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Alias = { id: number; domain_id: number; kaynak: string; hedef: string }


const MAILALIAS_EN: Record<string, string> = {
  "(bilinmeyen tüm adresler)": "(all unknown addresses)",
  "Bir adrese gelen postayı başka bir adrese yönlendirin. Catch-all ile bilinmeyen tüm adresleri tek kutuda toplayın.": "Forward mail sent to one address to another. With catch-all, collect all unknown addresses in a single mailbox.",
  "Henüz yönlendirme yok.": "No forwarding yet.",
  "Mail domainleri alınamadı": "Failed to get mail domains",
  "Takma Adlar & Yönlendirme": "Aliases & Forwarding",
  "Takma adlar alınamadı": "Failed to get aliases",
  "Yeni Yönlendirme": "New Forwarding",
  "Yönlendirmeler": "Forwardings",
  "✓ Yönlendirme silindi.": "✓ Forwarding deleted.",
  "Türkçe": "English",
  "Domain yüklenemedi": "Failed to load domain",
  "✓ {0} → {1} eklendi.": "✓ {0} → {1} added.",
  "Takma ad eklenemedi": "Failed to add alias",
  "Emin misiniz?": "Are you sure?",
  "\"{0} → {1}\" yönlendirmesi silinecek. Emin misiniz?": "The forwarding \"{0} → {1}\" will be deleted. Are you sure?",
  "Silinemedi": "Failed to delete",
  "Anasayfa": "Home",
  "Takma Adlar": "Aliases",
  "Bu domain için önce mail servisini kurun (Mail Kutuları sayfasından).": "First set up the mail service for this domain (from the Mailboxes page).",
  "Kaynak adres": "Source address",
  "Hedef adres (nereye gitsin)": "Target address (where it should go)",
  "Catch-all yap (bu domaine gelen": "Make catch-all (forward all",
  "bilinmeyen tüm adresleri": "unknown addresses",
  "hedefe yönlendir)": "sent to this domain to the target)",
  "— zaten bir catch-all var": "— a catch-all already exists",
  "adet": "item(s)",
  "Ekle": "Add",
  "Sil": "Delete",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MAILALIAS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainMailAliasPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [mailDomain, setMailDomain] = useState<MailDomain | null | undefined>(undefined)
  const [aliaslar, setAliaslar] = useState<Alias[] | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)

  // Form
  const [catchall, setCatchall] = useState(false)
  const [kaynak, setKaynak] = useState('')
  const [hedef, setHedef] = useState('')

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, cevir("Domain yüklenemedi"))))
  }, [id])

  function mailDomainYukle(d: Domain) {
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => setMailDomain((r.data.domainler || []).find(x => x.ad.toLowerCase() === d.alan_adi.toLowerCase()) || null))
      .catch(e => { setHata(apiHata(e, cevir("Mail domainleri alınamadı"))); setMailDomain(null) })
  }
  useEffect(() => { if (domain) mailDomainYukle(domain) }, [domain])

  function yukle(did: number) {
    setAliaslar(null)
    api.get<{ aliaslar: Alias[] }>(`/eklenti/mail/aliaslar?domain=${did}`)
      .then(r => setAliaslar(r.data.aliaslar || []))
      .catch(e => { setHata(apiHata(e, cevir("Takma adlar alınamadı"))); setAliaslar([]) })
  }
  useEffect(() => { if (mailDomain) yukle(mailDomain.id) }, [mailDomain])

  const catchallVar = (aliaslar || []).some(a => a.kaynak.startsWith('@'))

  async function ekle(e: React.FormEvent) {
    e.preventDefault()
    if (!mailDomain || !domain) return
    const src = catchall ? `@${domain.alan_adi}` : `${kaynak.trim().toLowerCase()}@${domain.alan_adi}`
    setIsleniyor(true); setHata(null); setBildirim(null)
    try {
      await api.post('/eklenti/mail/aliaslar', { domain_id: mailDomain.id, kaynak: src, hedef: hedef.trim().toLowerCase() })
      setBildirim(cevirT(cevir("✓ {0} → {1} eklendi."), src, hedef))
      setKaynak(''); setHedef(''); setCatchall(false)
      yukle(mailDomain.id)
    } catch (er) { setHata(apiHata(er, cevir("Takma ad eklenemedi"))) }
    finally { setIsleniyor(false) }
  }

  async function sil(a: Alias) {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("\"{0} → {1}\" yönlendirmesi silinecek. Emin misiniz?"), a.kaynak, a.hedef), tehlike: true }))) return
    setIsleniyor(true); setHata(null)
    try {
      await api.delete(`/eklenti/mail/aliaslar/${a.id}`)
      setBildirim(cevir("✓ Yönlendirme silindi."))
      if (mailDomain) yukle(mailDomain.id)
    } catch (e) { setHata(apiHata(e, cevir("Silinemedi"))) }
    finally { setIsleniyor(false) }
  }

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
    </div>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl mx-auto">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: cevir("Takma Adlar") },
      ]} />
      <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">{cevir("Takma Adlar & Yönlendirme")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{cevir("Bir adrese gelen postayı başka bir adrese yönlendirin. Catch-all ile bilinmeyen tüm adresleri tek kutuda toplayın.")}</p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {mailDomain === undefined && <div className="py-8 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
      {mailDomain === null && (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-8 text-center text-sm text-slate-500 dark:text-slate-400">
          {cevir("Bu domain için önce mail servisini kurun (Mail Kutuları sayfasından).")}
        </div>
      )}

      {mailDomain && (
        <>
          <form onSubmit={ekle} className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5 shadow-sm">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Yeni Yönlendirme")}</h2>
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Kaynak adres")}</label>
                {catchall ? (
                  <div className="rounded-lg border border-dashed border-brand-300 dark:border-brand-700 bg-brand-50/50 dark:bg-brand-900/20 px-3 py-2 text-sm text-brand-700 dark:text-brand-300">
                    @{domain.alan_adi} <span className="text-brand-500/70">{cevir("(bilinmeyen tüm adresler)")}</span>
                  </div>
                ) : (
                  <div className="flex items-center">
                    <input value={kaynak} onChange={e => setKaynak(e.target.value)} required={!catchall} placeholder="info"
                      className="w-full rounded-l-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                    <span className="px-3 py-2 text-sm bg-slate-50 dark:bg-slate-700 border border-l-0 border-slate-300 dark:border-slate-600 rounded-r-lg text-slate-500 dark:text-slate-400 whitespace-nowrap">@{domain.alan_adi}</span>
                  </div>
                )}
              </div>
              <div className="text-slate-400 pb-2 hidden sm:block">→</div>
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Hedef adres (nereye gitsin)")}</label>
                <input type="email" value={hedef} onChange={e => setHedef(e.target.value)} required placeholder="hedef@ornek.com"
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <button type="submit" disabled={isleniyor}
                className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-5 py-2 rounded-lg disabled:opacity-60 transition-colors">{cevir("Ekle")}</button>
            </div>
            <label className="mt-3 inline-flex items-center gap-2 text-xs text-slate-600 dark:text-slate-300 cursor-pointer">
              <input type="checkbox" checked={catchall} onChange={e => setCatchall(e.target.checked)} disabled={catchallVar && !catchall}
                className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              {cevir("Catch-all yap (bu domaine gelen")} <b>{cevir("bilinmeyen tüm adresleri")}</b> {cevir("hedefe yönlendir)")}
              {catchallVar && !catchall && <span className="text-amber-600 dark:text-amber-400">{cevir("— zaten bir catch-all var")}</span>}
            </label>
          </form>

          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden shadow-sm">
            <div className="px-5 py-3.5 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Yönlendirmeler")}</h2>
              <span className="text-xs text-slate-400 tabular-nums">{aliaslar?.length ?? 0} {cevir("adet")}</span>
            </div>
            {aliaslar === null && <div className="py-10 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
            {aliaslar?.length === 0 && <div className="py-12 text-center text-sm text-slate-400">{cevir("Henüz yönlendirme yok.")}</div>}
            {aliaslar && aliaslar.length > 0 && (
              <ul className="divide-y divide-slate-100 dark:divide-slate-700/70">
                {aliaslar.map(a => {
                  const isCatch = a.kaynak.startsWith('@')
                  return (
                    <li key={a.id} className="px-5 py-3.5 flex items-center gap-3 flex-wrap hover:bg-slate-50/70 dark:hover:bg-slate-700/20 transition-colors">
                      <span className="font-mono text-sm text-slate-800 dark:text-slate-200">
                        {isCatch ? <span className="inline-flex items-center gap-1.5">{a.kaynak}<span className="text-[10px] font-sans font-medium px-1.5 py-0.5 rounded bg-brand-50 dark:bg-brand-900/30 text-brand-600 dark:text-brand-300 uppercase">Catch-all</span></span> : a.kaynak}
                      </span>
                      <span className="text-slate-400">→</span>
                      <span className="font-mono text-sm text-slate-600 dark:text-slate-300">{a.hedef}</span>
                      <button onClick={() => sil(a)} disabled={isleniyor}
                        className="ml-auto text-xs font-medium px-2.5 py-1.5 rounded-lg border border-red-200 dark:border-red-800/70 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors">{cevir("Sil")}</button>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </>
      )}
    </div>
  )
}
