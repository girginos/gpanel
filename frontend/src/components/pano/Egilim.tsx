// Eğilim parçaları (D3b): sparkline, KPI değişim satırı, disk/trafik eğilim kartı.
// Veri GET /pano/w/egilim (pano_gunluk, günlük anlık görüntü). SAHTE VERİ YOK:
// iki günden az nokta → "Veri toplanıyor" (geriye dönük dolum yapılmaz).
import { useTranslation } from 'react-i18next'
import { useWidget } from './useWidget'
import { pt } from './ceviri'
import { degisimMetni, gunlukTrafik, seri, type EgilimMetrik, type EgilimVeri } from './egilimHesap'
import { goreliZaman } from '@/lib/bildirim'
import { mbYaz } from '@/components/bayi/ortak'

export function Sparkline({ deger, etiket, en = 72, boy = 20 }: { deger: number[]; etiket: string; en?: number; boy?: number }) {
  if (deger.length < 2) return null
  const min = Math.min(...deger), max = Math.max(...deger)
  const a = max - min || 1
  const p = deger.map((v, i) => `${((i / (deger.length - 1)) * en).toFixed(1)},${(boy - 1 - ((v - min) / a) * (boy - 2)).toFixed(1)}`).join(' ')
  return (
    <svg role="img" aria-label={etiket} viewBox={`0 0 ${en} ${boy}`} width={en} height={boy} className="shrink-0 overflow-visible text-primary-600 dark:text-primary-400">
      <polyline points={p} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

// KpiEgilim — KPI hücresinin altındaki sparkline + değişim (yoksa "Veri toplanıyor").
export function KpiEgilim({ metrik, etiket }: { metrik: EgilimMetrik; etiket: string }) {
  useTranslation()
  const { veri, yuk, hata } = useWidget<EgilimVeri>('egilim')
  if (yuk || hata || !veri) return <div className="mt-2 h-5" aria-hidden="true" />
  const n = veri.noktalar
  const d = seri(n, metrik)
  if (d.length < 2) {
    return <div className="mt-2 text-[11px] text-gray-400 dark:text-dark-300">{pt('Veri toplanıyor')}{n.length === 1 && ` · ${pt('{0} gündür toplanıyor', 1)}`}</div>
  }
  const metin = metrik === 'trafik' ? pt('Günlük trafik') : degisimMetni(d, n.length - 1)
  return (
    <div className="mt-2 flex items-center gap-2 text-[11px] tabular-nums text-gray-500 dark:text-dark-300">
      <Sparkline deger={d} etiket={`${etiket}: ${pt('Son 30 gün')}`} />
      <span className="truncate">{metin}</span>
    </div>
  )
}

// EgilimGrafik — kartta büyük çizgi (disk) / sütun (günlük trafik) grafiği.
function Cizgi({ deger, etiket }: { deger: number[]; etiket: string }) {
  const en = 300, boy = 72
  const min = Math.min(...deger), max = Math.max(...deger), a = max - min || 1
  const p = deger.map((v, i) => [(i / Math.max(1, deger.length - 1)) * en, boy - 4 - ((v - min) / a) * (boy - 8)])
  const yol = p.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('')
  return (
    <svg role="img" aria-label={etiket} viewBox={`0 0 ${en} ${boy}`} preserveAspectRatio="none" className="h-16 w-full text-primary-600 dark:text-primary-400">
      <path d={`${yol}L${en},${boy}L0,${boy}Z`} className="fill-current opacity-10" />
      <path d={yol} fill="none" stroke="currentColor" strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

function Sutun({ deger, etiket }: { deger: number[]; etiket: string }) {
  const max = Math.max(...deger, 1)
  return (
    <div role="img" aria-label={etiket} className="flex h-16 items-end gap-[2px]">
      {deger.map((v, i) => <div key={i} className="flex-1 rounded-sm bg-info/70 dark:bg-info/60" style={{ height: `${Math.max(2, (v / max) * 100)}%` }} />)}
    </div>
  )
}

export function EgilimIcerik({ veri }: { veri: EgilimVeri }) {
  const n = veri.noktalar
  if (n.length < 2) {
    return (
      <div className="py-4 text-center">
        <p className="text-sm font-medium text-gray-700 dark:text-dark-100">{pt('Veri toplanıyor')}</p>
        <p className="mx-auto mt-1 max-w-xs text-xs text-gray-500 dark:text-dark-300">{pt('Geçmiş, bu sürümden itibaren günde bir birikir; eğilim için en az iki gün gerekir.')}</p>
        {n.length === 1 && <p className="mt-1 text-xs text-gray-400 dark:text-dark-300">{pt('{0} gündür toplanıyor', 1)}</p>}
      </div>
    )
  }
  const disk = n.map((x) => x.disk_kb / 1024)
  const trafik = gunlukTrafik(n).map((kb) => kb / 1024)
  const son = n[n.length - 1]
  return (
    <div className="space-y-4">
      <div>
        <div className="flex items-baseline justify-between gap-2 text-xs">
          <span className="font-medium text-gray-600 dark:text-dark-200">{pt('Toplam disk')}</span>
          <span className="tabular-nums text-gray-500 dark:text-dark-300">{mbYaz(son.disk_kb / 1024)} · {degisimMetni(disk, n.length - 1)}</span>
        </div>
        <Cizgi deger={disk} etiket={`${pt('Toplam disk')}: ${n[0].tarih} → ${son.tarih}`} />
      </div>
      <div>
        <div className="flex items-baseline justify-between gap-2 text-xs">
          <span className="font-medium text-gray-600 dark:text-dark-200">{pt('Günlük trafik')}</span>
          <span className="tabular-nums text-gray-500 dark:text-dark-300">{mbYaz(trafik[trafik.length - 1] ?? 0)}</span>
        </div>
        <Sutun deger={trafik} etiket={`${pt('Günlük trafik')}: ${n[1].tarih} → ${son.tarih}`} />
      </div>
      <p className="text-[11px] text-gray-400 dark:text-dark-300">
        {veri.disk_olcum ? `${pt('Son disk ölçümü')}: ${goreliZaman(veri.disk_olcum)}` : pt('Disk kotası ölçümü kapalı; değerler son elle ölçümden.')}
      </p>
    </div>
  )
}
