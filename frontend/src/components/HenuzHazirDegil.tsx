// gosp-dark-swept
// gosp-dark-swept-v2
export default function HenuzHazirDegil({
  baslik, faz, aciklama,
}: { baslik: string; faz: string; aciklama: string }) {
  return (
    <div className="px-8 py-6">
      <div className="flex items-center mb-1">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{baslik}</h1>
        <span className="ml-3 px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wider bg-amber-100 dark:bg-amber-900/30 text-amber-800 dark:text-amber-200 rounded">
          {faz} · Hazır Değil
        </span>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-8">{aciklama}</p>

      <div className="bg-white dark:bg-slate-800 border-2 border-dashed border-slate-200 dark:border-slate-700 rounded-2xl p-12 text-center">
        <div className="w-16 h-16 mx-auto rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center mb-4">
          <svg className="w-8 h-8 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">Yapım aşamasında</h3>
        <p className="text-sm text-slate-500 dark:text-slate-500 max-w-md mx-auto">
          Bu modül <span className="font-mono text-brand-700 dark:text-brand-300">{faz}</span> fazında devreye girecek.
          Şu an sadece arayüz iskeleti görülmektedir.
        </p>
      </div>
    </div>
  )
}