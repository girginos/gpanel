import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'
import { useDialog } from '@/components/Dialog'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Alias = { id: number; domain_id: number; kaynak: string; hedef: string }

export default function DomainMailAliasPage() {
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
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, 'Domain yüklenemedi')))
  }, [id])

  function mailDomainYukle(d: Domain) {
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => setMailDomain((r.data.domainler || []).find(x => x.ad.toLowerCase() === d.alan_adi.toLowerCase()) || null))
      .catch(e => { setHata(apiHata(e, 'Mail domainleri alınamadı')); setMailDomain(null) })
  }
  useEffect(() => { if (domain) mailDomainYukle(domain) }, [domain])

  function yukle(did: number) {
    setAliaslar(null)
    api.get<{ aliaslar: Alias[] }>(`/eklenti/mail/aliaslar?domain=${did}`)
      .then(r => setAliaslar(r.data.aliaslar || []))
      .catch(e => { setHata(apiHata(e, 'Takma adlar alınamadı')); setAliaslar([]) })
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
      setBildirim(`✓ ${src} → ${hedef} eklendi.`)
      setKaynak(''); setHedef(''); setCatchall(false)
      yukle(mailDomain.id)
    } catch (er) { setHata(apiHata(er, 'Takma ad eklenemedi')) }
    finally { setIsleniyor(false) }
  }

  async function sil(a: Alias) {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `"${a.kaynak} → ${a.hedef}" yönlendirmesi silinecek. Emin misiniz?`, tehlike: true }))) return
    setIsleniyor(true); setHata(null)
    try {
      await api.delete(`/eklenti/mail/aliaslar/${a.id}`)
      setBildirim('✓ Yönlendirme silindi.')
      if (mailDomain) yukle(mailDomain.id)
    } catch (e) { setHata(apiHata(e, 'Silinemedi')) }
    finally { setIsleniyor(false) }
  }

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
    </div>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl mx-auto">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: 'Takma Adlar' },
      ]} />
      <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">Takma Adlar & Yönlendirme</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">Bir adrese gelen postayı başka bir adrese yönlendirin. Catch-all ile bilinmeyen tüm adresleri tek kutuda toplayın.</p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {mailDomain === undefined && <div className="py-8 text-center text-sm text-slate-400">Yükleniyor…</div>}
      {mailDomain === null && (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-8 text-center text-sm text-slate-500 dark:text-slate-400">
          Bu domain için önce mail servisini kurun (Mail Kutuları sayfasından).
        </div>
      )}

      {mailDomain && (
        <>
          <form onSubmit={ekle} className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5 shadow-sm">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">Yeni Yönlendirme</h2>
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Kaynak adres</label>
                {catchall ? (
                  <div className="rounded-lg border border-dashed border-brand-300 dark:border-brand-700 bg-brand-50/50 dark:bg-brand-900/20 px-3 py-2 text-sm text-brand-700 dark:text-brand-300">
                    @{domain.alan_adi} <span className="text-brand-500/70">(bilinmeyen tüm adresler)</span>
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
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Hedef adres (nereye gitsin)</label>
                <input type="email" value={hedef} onChange={e => setHedef(e.target.value)} required placeholder="hedef@ornek.com"
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <button type="submit" disabled={isleniyor}
                className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-5 py-2 rounded-lg disabled:opacity-60 transition-colors">Ekle</button>
            </div>
            <label className="mt-3 inline-flex items-center gap-2 text-xs text-slate-600 dark:text-slate-300 cursor-pointer">
              <input type="checkbox" checked={catchall} onChange={e => setCatchall(e.target.checked)} disabled={catchallVar && !catchall}
                className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              Catch-all yap (bu domaine gelen <b>bilinmeyen tüm adresleri</b> hedefe yönlendir)
              {catchallVar && !catchall && <span className="text-amber-600 dark:text-amber-400">— zaten bir catch-all var</span>}
            </label>
          </form>

          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden shadow-sm">
            <div className="px-5 py-3.5 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Yönlendirmeler</h2>
              <span className="text-xs text-slate-400 tabular-nums">{aliaslar?.length ?? 0} adet</span>
            </div>
            {aliaslar === null && <div className="py-10 text-center text-sm text-slate-400">Yükleniyor…</div>}
            {aliaslar?.length === 0 && <div className="py-12 text-center text-sm text-slate-400">Henüz yönlendirme yok.</div>}
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
                        className="ml-auto text-xs font-medium px-2.5 py-1.5 rounded-lg border border-red-200 dark:border-red-800/70 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors">Sil</button>
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
