// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'

type Entry = { adi: string; yol: string; tip: 'klasor' | 'dosya' | 'sembolik' }
type ListResp = { yol: string; icerik: Entry[] }

interface Props {
  domainId: number | string
  secili: string
  onSec: (yol: string) => void
  yenileme?: number // değişince yeniden çek (yeni klasör/silme sonrası)
}

export default function DizinAgac({ domainId, secili, onSec, yenileme }: Props) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-2 text-sm overflow-auto min-h-[220px] lg:min-h-[400px]">
      <TreeNode
        domainId={domainId}
        yol="/"
        ad="~"
        secili={secili}
        onSec={onSec}
        baslangicAcik={true}
        derinlik={0}
        yenileme={yenileme}
      />
    </div>
  )
}

function TreeNode({
  domainId, yol, ad, secili, onSec, baslangicAcik, derinlik, yenileme
}: {
  domainId: number | string
  yol: string
  ad: string
  secili: string
  onSec: (yol: string) => void
  baslangicAcik: boolean
  derinlik: number
  yenileme?: number
}) {
  const [acik, setAcik] = useState(baslangicAcik)
  const [klasorler, setKlasorler] = useState<Entry[]>([])
  const [yuklendi, setYuklendi] = useState(false)
  const [yukleniyor, setYukleniyor] = useState(false)

  function fetchAlt() {
    setYukleniyor(true)
    api.get<ListResp>(`/domains/${domainId}/files`, { params: { yol } })
      .then(r => setKlasorler(r.data.icerik.filter(e => e.tip === 'klasor')))
      .catch(() => setKlasorler([]))
      .finally(() => { setYuklendi(true); setYukleniyor(false) })
  }

  useEffect(() => {
    if (baslangicAcik && !yuklendi) fetchAlt()
  }, []) // eslint-disable-line

  // yenileme sayacı değişince zaten yüklenmişse yeniden çek
  useEffect(() => {
    if (yuklendi) fetchAlt()
  }, [yenileme]) // eslint-disable-line

  function chevronTikla(e: React.MouseEvent) {
    e.stopPropagation()
    if (!acik && !yuklendi) fetchAlt()
    setAcik(!acik)
  }

  const seciliMi = yol === secili || (yol === '/' && (secili === '' || secili === '/'))
  const altVar = !yuklendi || klasorler.length > 0

  return (
    <div>
      <div
        onClick={() => onSec(yol)}
        className={`flex items-center gap-1 px-2 py-1 rounded cursor-pointer transition ${
          seciliMi ? 'bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300' : 'hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300'
        }`}
        style={{ paddingLeft: 8 + derinlik * 14 }}
        title={yol}
      >
        {altVar ? (
          <button
            onClick={chevronTikla}
            className="w-4 h-4 flex items-center justify-center text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300"
          >
            <svg
              className={`w-3 h-3 transition-transform ${acik ? 'rotate-90' : ''}`}
              fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
            </svg>
          </button>
        ) : (
          <span className="w-4" />
        )}
        <svg className="w-4 h-4 text-amber-500 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
          <path d="M2 6a2 2 0 012-2h5l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" />
        </svg>
        <span className="truncate text-sm">{ad}</span>
      </div>

      {acik && (
        <div>
          {yukleniyor && klasorler.length === 0 && (
            <div className="px-3 py-1 text-xs text-slate-400 dark:text-slate-500" style={{ paddingLeft: 24 + derinlik * 14 }}>
              yükleniyor…
            </div>
          )}
          {klasorler.map(k => (
            <TreeNode
              key={k.yol}
              domainId={domainId}
              yol={k.yol}
              ad={k.adi}
              secili={secili}
              onSec={onSec}
              baslangicAcik={false}
              derinlik={derinlik + 1}
              yenileme={yenileme}
            />
          ))}
        </div>
      )}
    </div>
  )
}