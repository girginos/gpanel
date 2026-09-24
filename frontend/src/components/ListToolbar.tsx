// gosp-dark-swept
// gosp-dark-swept-v2
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'

// ikon — butonun solundaki eylem ikonu (I.* — components/Ikon); birincil buton varsayılan I.arti.
export type ToolbarButton = { etiket: string; onClick?: () => void; disabled?: boolean; ipucu?: string; ikon?: string }

export default function ListToolbar({
  birincil, butonlar, arananSetter, aranan,
}: {
  birincil?: ToolbarButton
  butonlar?: ToolbarButton[]
  arananSetter?: (s: string) => void
  aranan?: string
}) {
  return (
    <div className="flex items-center gap-2 mb-4 flex-wrap">
      {birincil && (
        <Button
          color="primary"
          onClick={birincil.onClick}
          disabled={birincil.disabled}
          title={birincil.ipucu}
          ikon={birincil.ikon ?? I.arti}
        >
          {birincil.etiket}
        </Button>
      )}
      {(butonlar || []).map((b, i) => (
        <Button
          key={i}
          variant="outlined"
          onClick={b.onClick}
          disabled={b.disabled}
          title={b.ipucu}
          ikon={b.ikon}
        >
          {b.etiket}
        </Button>
      ))}
      {arananSetter !== undefined && (
        <div className="ml-auto relative">
          <Ikon d={I.ara} className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500" />
          <input
            type="text"
            value={aranan || ''}
            onChange={(e) => arananSetter(e.target.value)}
            placeholder="Ara..."
            className="pl-8 pr-3 py-1.5 text-sm w-56 border border-slate-200 dark:border-dark-600 rounded-full focus:border-brand-400 focus:ring-2 focus:ring-brand-500/15 outline-none transition"
          />
        </div>
      )}
    </div>
  )
}