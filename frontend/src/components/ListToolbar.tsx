// gosp-dark-swept
// gosp-dark-swept-v2
import { useState } from 'react'

export type ToolbarButton = { etiket: string; onClick?: () => void; disabled?: boolean; ipucu?: string }

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
        <button
          onClick={birincil.onClick}
          disabled={birincil.disabled}
          title={birincil.ipucu}
          className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-full shadow-sm disabled:shadow-none transition"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
          </svg>
          {birincil.etiket}
        </button>
      )}
      {(butonlar || []).map((b, i) => (
        <button
          key={i}
          onClick={b.onClick}
          disabled={b.disabled}
          title={b.ipucu}
          className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:opacity-50 disabled:bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-full transition"
        >
          {b.etiket}
        </button>
      ))}
      {arananSetter !== undefined && (
        <div className="ml-auto relative">
          <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            value={aranan || ''}
            onChange={(e) => arananSetter(e.target.value)}
            placeholder="Ara..."
            className="pl-8 pr-3 py-1.5 text-sm w-56 border border-slate-200 dark:border-slate-700 rounded-full focus:border-brand-400 focus:ring-2 focus:ring-brand-500/15 outline-none transition"
          />
        </div>
      )}
    </div>
  )
}