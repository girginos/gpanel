import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState, useCallback, useRef } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { Button, Badge } from '@/components/ui'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { I } from '@/components/Ikon'
import type { Domain } from '@/components/domainTipi'

type Kayit = {
  zaman: string; qid: string; gonderen: string; alici: string
  durum: string; relay: string; detay: string
}

// Teslimat durumu → Tailux Badge rengi + etiket.
type RozetRenk = 'success' | 'error' | 'warning' | 'neutral'
const DURUM: Record<string, { renk: RozetRenk; etiket: string }> = {
  sent: { renk: 'success', etiket: 'Teslim' },
  bounced: { renk: 'error', etiket: 'Reddedildi' },
  deferred: { renk: 'warning', etiket: 'Ertelendi' },
}

const MAILTES_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "Alıcı": "Recipient",
  "Ayrıntı": "Detail",
  "Bu domain için teslimat kaydı bulunamadı.": "No delivery records found for this domain.",
  "Gönderen": "Sender",
  "Teslimat kayıtları alınamadı": "Failed to get delivery records",
  "Türkçe": "English",
  "Teslim": "Delivered",
  "Reddedildi": "Rejected",
  "Ertelendi": "Deferred",
  "Domain yüklenemedi": "Failed to load domain",
  "Anasayfa": "Home",
  "E-Posta Teslimat Takibi": "Email Delivery Tracking",
  "gönderilen/alınan son e-postaların teslimat durumu (sunucu günlüğünden).": "delivery status of recent sent/received emails (from the server log).",
  "Zaman": "Time",
  "Durum": "Status",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MAILTES_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function rozet(durum: string) {
  return DURUM[durum] || { renk: 'neutral' as RozetRenk, etiket: durum }
}

export default function DomainMailTeslimatPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [kayitlar, setKayitlar] = useState<Kayit[] | null>(null)
  const [, setHata] = useState<string | null>(null)
  const [yenileniyor, setYenileniyor] = useState(false)

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e, cevir("Domain yüklenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  const yukleNesli = useRef(0)
  const yukle = useCallback((d: Domain) => {
    setYenileniyor(true)
    const _n = ++yukleNesli.current
    api.get<{ kayitlar: Kayit[] }>(`/eklenti/mail/teslimat?domain=${encodeURIComponent(d.alan_adi)}&limit=150`)
      .then(r => { if (_n !== yukleNesli.current) return; setKayitlar(r.data.kayitlar || []) })
      .catch(e => { if (_n !== yukleNesli.current) return; const m = apiHata(e, cevir("Teslimat kayıtları alınamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m); setKayitlar([]) })
      .finally(() => { if (_n !== yukleNesli.current) return; setYenileniyor(false) })
  }, [])

  useEffect(() => { if (domain) yukle(domain); return () => { yukleNesli.current++ } }, [domain, yukle])

  if (!domain) return (
    <div>
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
    </div>
  )

  const sayac = (d: string) => (kayitlar || []).filter(k => k.durum === d).length

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: cevir("E-Posta Teslimat Takibi") },
      ]} />
      <div className="flex items-center justify-between gap-3 mb-1 flex-wrap">
        <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300">{cevir("E-Posta Teslimat Takibi")}</h1>
        <Button onClick={() => yukle(domain)} yukleniyor={yenileniyor} variant="outlined" ikon={I.yenile}>
          {cevir("Yenile")}
        </Button>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">{domain.alan_adi} {cevir("gönderilen/alınan son e-postaların teslimat durumu (sunucu günlüğünden).")}</p>

      {kayitlar && kayitlar.length > 0 && (
        <div className="flex items-center gap-2 mb-3 text-xs">
          <Badge variant="soft" color={DURUM.sent.renk}>{sayac('sent')} {cevir("Teslim")}</Badge>
          <Badge variant="soft" color={DURUM.deferred.renk}>{sayac('deferred')} {cevir("Ertelendi")}</Badge>
          <Badge variant="soft" color={DURUM.bounced.renk}>{sayac('bounced')} {cevir("Reddedildi")}</Badge>
        </div>
      )}

      <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg overflow-hidden">
        {kayitlar === null && <div className="py-10 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
        {kayitlar?.length === 0 && <div className="py-10 text-center text-sm text-slate-400">{cevir("Bu domain için teslimat kaydı bulunamadı.")}</div>}
        {kayitlar && kayitlar.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wider text-slate-500 dark:text-slate-400 border-b border-slate-100 dark:border-dark-600">
                  <th className="px-4 py-2.5 font-semibold">{cevir("Zaman")}</th>
                  <th className="px-4 py-2.5 font-semibold">{cevir("Gönderen")}</th>
                  <th className="px-4 py-2.5 font-semibold">{cevir("Alıcı")}</th>
                  <th className="px-4 py-2.5 font-semibold">{cevir("Durum")}</th>
                  <th className="px-4 py-2.5 font-semibold">{cevir("Ayrıntı")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-50 dark:divide-dark-600/50">
                {kayitlar.map((k, i) => {
                  const r = rozet(k.durum)
                  return (
                    <tr key={i} className="hover:bg-slate-50 dark:hover:bg-dark-600/30">
                      <td className="px-4 py-2 whitespace-nowrap text-slate-500 dark:text-slate-400 font-mono text-xs">{k.zaman}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-700 dark:text-slate-300 max-w-[160px] truncate" title={k.gonderen}>{k.gonderen || '—'}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-700 dark:text-slate-300 max-w-[180px] truncate" title={k.alici}>{k.alici}</td>
                      <td className="px-4 py-2"><Badge variant="soft" color={r.renk} className="text-[10px] font-bold px-2 py-0.5 uppercase">{cevir(r.etiket)}</Badge></td>
                      <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400 max-w-[240px] truncate" title={k.detay}>{k.detay}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
