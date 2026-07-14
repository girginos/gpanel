// gosp-dark-swept
// gosp-dark-swept-v2
export type Domain = {
  id: number
  alan_adi: string
  php_surum: string
  ssl: boolean
  ssl_bitis?: string
  durum: 'aktif' | 'pasif' | string
  sistem_kullanici: string
  boyut_kb: number
  trafik_kb: number
  olusturulma: string
  ipv4: string
  ftp_host: string
  ftp_user: string
  db_host: string
  db_user: string
  db_adi: string
  web_root: string
  notlar?: string
  ssh_erisim?: boolean
}

export default function DomainList({
  items, seciliId, onSec, yukleniyor,
}: {
  items: Domain[]
  seciliId?: number
  onSec: (id: number) => void
  yukleniyor?: boolean
}) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          Domainler {!yukleniyor && <span className="text-slate-400 dark:text-slate-500 font-normal">({items.length})</span>}
        </h3>
        <button
          type="button"
          className="text-xs px-2 py-1 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 rounded font-medium shadow-sm transition"
          title="F2'de aktif olacak"
          disabled
        >
          + Domain Ekle
        </button>
      </div>

      <ul className="max-h-[640px] overflow-auto divide-y divide-slate-100 dark:divide-slate-800">
        {yukleniyor && (
          <li className="px-4 py-6 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</li>
        )}
        {!yukleniyor && items.length === 0 && (
          <li className="px-4 py-6 text-center text-sm text-slate-500 dark:text-slate-500">Henüz domain yok</li>
        )}
        {items.map((d) => {
          const sec = d.id === seciliId
          return (
            <li key={d.id}>
              <button
                type="button"
                onClick={() => onSec(d.id)}
                className={`w-full text-left px-4 py-3 transition ${
                  sec ? 'bg-brand-50 dark:bg-brand-900/20 border-l-4 border-brand-500 pl-3' : 'hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border-l-4 border-transparent'
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className={`text-sm font-medium truncate ${sec ? 'text-brand-700 dark:text-brand-300' : 'text-slate-900 dark:text-slate-100'}`}>
                    {d.alan_adi}
                  </span>
                  <span
                    className={`text-[10px] px-1.5 py-0.5 rounded uppercase font-semibold tracking-wider flex-shrink-0 ${
                      d.durum === 'aktif'
                        ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300'
                        : 'bg-slate-200 text-slate-600 dark:text-slate-400 dark:text-slate-500'
                    }`}
                  >
                    {d.durum}
                  </span>
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-slate-500 dark:text-slate-500">
                  <span className="font-mono">PHP {d.php_surum}</span>
                  {d.ssl ? (
                    <span className="text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>SSL
                    </span>
                  ) : (
                    <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1">
                      <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>SSL yok
                    </span>
                  )}
                  <span className="ml-auto">{Math.round(d.boyut_kb / 1024)} MB</span>
                </div>
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}