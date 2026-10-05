import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { cevirT } from '@/lib/cevirT'
import { Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { cevir, evreEtiket, fmtBayt, ilerlemeGorunur, kalanSure, sinirla, type UzakAktifIs, type UzakIlerleme } from '@/lib/uzakIlerleme'

// UzakIlerlemePaneli — kartın üstündeki genel çubuk + aktif iş satırı.
export function UzakIlerlemePaneli({ v, sonIslerAc }: { v: UzakIlerleme | null; sonIslerAc?: () => void }) {
  useTranslation()
  if (!v || !ilerlemeGorunur(v)) return null
  const k = v.kuyruk
  const a = v.aktif
  const bitti = v.tamamlandi && !v.aktif_mi && !a
  const yuzde = bitti ? 100 : sinirla(k.yuzde)
  const hatali = bitti && k.hata > 0
  const parcalar: string[] = []
  if (bitti) {
    parcalar.push(cevirT(cevir('Tamamlandı — {0} yedek gönderildi, {1} hata'), k.tamam, k.hata))
  } else {
    parcalar.push(cevirT(cevir('{0}/{1} yedek'), k.tamam, k.toplam_is))
    if (k.toplam_bayt > 0) parcalar.push(`${fmtBayt(k.tamamlanan_bayt)} / ${fmtBayt(k.toplam_bayt)}`)
    if (v.aktif_mi) parcalar.push(k.eta_sn === null ? cevir('süre hesaplanıyor…') : kalanSure(k.eta_sn))
    if (k.hiz_bps > 0) parcalar.push(`${fmtBayt(k.hiz_bps)}/${i18n.language === 'en' ? 's' : 'sn'}`)
  }
  const metin = parcalar.join(' · ')
  const baslik = bitti ? cevir('Gönderim tamamlandı') : cevir('Drive’a gönderiliyor')
  const cubukRenk = bitti ? (hatali ? 'bg-amber-500' : 'bg-emerald-500') : 'bg-sky-500'
  return (
    <section aria-label={cevir('Gönderim ilerlemesi')}
      className="rounded-lg border border-sky-200 dark:border-sky-500/30 bg-sky-50/60 dark:bg-sky-500/10 p-3 space-y-2 min-w-0">
      <div className="flex flex-wrap items-center gap-2">
        <Ikon d={I.bulut} className="h-4 w-4 shrink-0" />
        <h4 className="text-sm font-semibold text-slate-800 dark:text-slate-100">{baslik}</h4>
        {a && <Badge color="info" variant="soft">{evreEtiket(a.evre, true)}</Badge>}
        <span className="ml-auto text-sm font-semibold tabular-nums text-slate-700 dark:text-slate-200">%{Math.floor(yuzde)}</span>
      </div>
      <div role="progressbar" aria-label={baslik} aria-valuemin={0} aria-valuemax={100}
        aria-valuenow={Math.floor(yuzde)} aria-valuetext={`%${Math.floor(yuzde)} — ${metin}`}
        className="h-2.5 w-full rounded-full bg-slate-200 dark:bg-dark-600 overflow-hidden">
        <div className={`h-full rounded-full ${cubukRenk} transition-[width] duration-500 motion-reduce:transition-none`} style={{ width: `${yuzde}%` }} />
      </div>
      <p className="text-xs text-slate-600 dark:text-slate-300 tabular-nums break-words">{metin}</p>
      {a && (
        <p className="text-xs text-slate-700 dark:text-slate-200 break-all">
          <strong className="font-medium">{a.domain || a.dosya}</strong>
          {' — '}{evreEtiket(a.evre)} %{Math.floor(sinirla(a.evre_yuzde))}
          {a.bayt_toplam > 0 && <span className="tabular-nums"> · {fmtBayt(a.bayt_gonderilen)} / {fmtBayt(a.bayt_toplam)}</span>}
        </p>
      )}
      {!bitti && k.yeniden_denenecek > 0 && (
        <p className="text-xs text-amber-700 dark:text-amber-300">{cevirT(cevir('{0} yedek yeniden denenecek'), k.yeniden_denenecek)}</p>
      )}
      {k.hata > 0 && (
        <p className="text-xs text-amber-700 dark:text-amber-300">
          {cevir('Hata alan işler için “Son işler” bölümüne bakın.')}
          {sonIslerAc && <>{' '}<button type="button" onClick={sonIslerAc} className="underline underline-offset-2 font-medium">{cevir('Son işler')}</button></>}
        </p>
      )}
    </section>
  )
}

// UzakSatirCubugu — yedek satırında küçük çubuk (yalnız o an gönderilen iş).
export function UzakSatirCubugu({ a }: { a: UzakAktifIs }) {
  useTranslation()
  const y = Math.floor(sinirla(a.is_yuzde))
  const etiket = `${evreEtiket(a.evre, true)} %${y}`
  return (
    <div className="mt-1 min-w-0 max-w-[14rem]">
      <div role="progressbar" aria-label={cevir('Gönderim ilerlemesi')} aria-valuemin={0} aria-valuemax={100}
        aria-valuenow={y} aria-valuetext={etiket}
        className="h-1.5 w-full rounded-full bg-slate-200 dark:bg-dark-600 overflow-hidden">
        <div className="h-full rounded-full bg-sky-500 transition-[width] duration-500 motion-reduce:transition-none" style={{ width: `${y}%` }} />
      </div>
      <p className="mt-0.5 text-[11px] text-slate-500 dark:text-slate-400 tabular-nums">{etiket}</p>
    </div>
  )
}
