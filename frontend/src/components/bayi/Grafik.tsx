// Bayi paneli görsel parçaları: doluluk çubuğu, halka grafik (kütüphanesiz inline SVG;
// koyu tema html.dark ile).
import { cx } from './ortak'

export function Cubuk({ deger, etiket }: { deger: number; etiket: string }) {
  const c = deger >= 90 ? 'bg-error' : deger >= 75 ? 'bg-warning' : 'bg-primary-600 dark:bg-primary-500'
  return (
    <div role="progressbar" aria-label={etiket} aria-valuemin={0} aria-valuemax={100} aria-valuenow={deger}
      className="h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-500">
      <div className={cx('h-full rounded-full', c)} style={{ width: `${deger}%` }} />
    </div>
  )
}

export function Donut({ deger, etiket, merkez, boyut = 112 }: { deger: number | null; etiket: string; merkez: string; boyut?: number }) {
  const r = 42, c = 2 * Math.PI * r
  const d = deger ?? 0
  const ton = d >= 90 ? 'text-error' : d >= 75 ? 'text-warning' : 'text-primary-600 dark:text-primary-400'
  return (
    <div className="relative shrink-0" style={{ width: boyut, height: boyut }}>
      <svg role="img" aria-label={etiket} viewBox="0 0 100 100" className="size-full -rotate-90">
        <circle cx="50" cy="50" r={r} fill="none" strokeWidth="9" className="stroke-gray-200 dark:stroke-dark-500" />
        {deger !== null && <circle cx="50" cy="50" r={r} fill="none" strokeWidth="9" strokeLinecap={d > 0 ? 'round' : 'butt'}
          className={cx('stroke-current', ton)} strokeDasharray={`${(d / 100) * c} ${c}`} />}
      </svg>
      <div className="absolute inset-0 grid place-content-center text-center">
        <div className="text-lg font-semibold leading-none tabular-nums text-gray-900 dark:text-dark-50">{merkez}</div>
      </div>
    </div>
  )
}
