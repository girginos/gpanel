// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useState } from 'react'
import CodeMirror, { EditorView } from '@uiw/react-codemirror'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { php } from '@codemirror/lang-php'
import { markdown } from '@codemirror/lang-markdown'
import { sql } from '@codemirror/lang-sql'
import { xml } from '@codemirror/lang-xml'
import { oneDark } from '@codemirror/theme-one-dark'

type Dil = 'html' | 'css' | 'js' | 'json' | 'php' | 'md' | 'sql' | 'xml' | 'text'

const DILLER: { kod: Dil; ad: string; uzantilar: string[] }[] = [
  { kod: 'html', ad: 'HTML',       uzantilar: ['html', 'htm'] },
  { kod: 'css',  ad: 'CSS',        uzantilar: ['css', 'scss', 'sass', 'less'] },
  { kod: 'js',   ad: 'JavaScript', uzantilar: ['js', 'jsx', 'mjs', 'ts', 'tsx'] },
  { kod: 'json', ad: 'JSON',       uzantilar: ['json'] },
  { kod: 'php',  ad: 'PHP',        uzantilar: ['php', 'phtml', 'phps'] },
  { kod: 'md',   ad: 'Markdown',   uzantilar: ['md', 'markdown'] },
  { kod: 'sql',  ad: 'SQL',        uzantilar: ['sql'] },
  { kod: 'xml',  ad: 'XML',        uzantilar: ['xml', 'svg'] },
  { kod: 'text', ad: 'Düz Metin',  uzantilar: ['txt', 'log', 'ini', 'conf', 'env'] },
]

function dilTespit(yol: string): Dil {
  const m = yol.toLowerCase().match(/\.([a-z0-9]+)$/)
  if (!m) return 'text'
  const u = m[1]
  for (const d of DILLER) if (d.uzantilar.includes(u)) return d.kod
  return 'text'
}

function dilExt(kod: Dil) {
  switch (kod) {
    case 'html': return [html()]
    case 'css':  return [css()]
    case 'js':   return [javascript({ jsx: true, typescript: true })]
    case 'json': return [json()]
    case 'php':  return [php()]
    case 'md':   return [markdown()]
    case 'sql':  return [sql()]
    case 'xml':  return [xml()]
    default:     return []
  }
}

interface Props {
  yol: string
  icerik: string
  onChange: (s: string) => void
  onKaydet: () => Promise<void> | void
  onKapat: () => void
}

export default function KodEditor({ yol, icerik, onChange, onKaydet, onKapat }: Props) {
  const [tamEkran, setTamEkran] = useState(false)
  const [dil, setDil] = useState<Dil>(() => dilTespit(yol))
  const [kayitDurum, setKayitDurum] = useState<'temiz' | 'kirli' | 'kaydediliyor' | 'kaydedildi'>('temiz')
  const [cursor, setCursor] = useState({ satir: 1, kolon: 1 })
  const [ilkIcerik] = useState(icerik)

  useEffect(() => {
    if (icerik !== ilkIcerik && kayitDurum !== 'kaydediliyor') {
      setKayitDurum('kirli')
    }
  }, [icerik, ilkIcerik, kayitDurum])

  // CTRL+S
  useEffect(() => {
    function ks(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        kaydet()
      }
      if (e.key === 'Escape') onKapat()
    }
    window.addEventListener('keydown', ks)
    return () => window.removeEventListener('keydown', ks)
  })

  async function kaydet() {
    setKayitDurum('kaydediliyor')
    try {
      await onKaydet()
      setKayitDurum('kaydedildi')
      setTimeout(() => setKayitDurum('temiz'), 1200)
    } catch {
      setKayitDurum('kirli')
    }
  }

  const ext = useMemo(() => [
    ...dilExt(dil),
    EditorView.theme({
      '&': { fontSize: '13px' },
      '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace' },
    }),
    EditorView.updateListener.of(u => {
      if (u.selectionSet || u.docChanged) {
        const sel = u.state.selection.main
        const line = u.state.doc.lineAt(sel.head)
        setCursor({ satir: line.number, kolon: sel.head - line.from + 1 })
      }
    }),
  ], [dil])

  const dosyaAdi = yol.split('/').filter(Boolean).pop() || yol

  // Boyut bilgisi
  const baytSayi = new TextEncoder().encode(icerik).length

  return (
    <div
      className={`fixed inset-0 z-50 bg-black/50 flex items-center justify-center ${tamEkran ? '' : 'p-4'}`}
      onClick={onKapat}
    >
      <div
        className={`bg-slate-900 shadow-2xl flex flex-col text-slate-100 ${tamEkran ? 'w-full h-full' : 'w-full h-[85vh] rounded-2xl overflow-hidden'}`}
        onClick={e => e.stopPropagation()}
      >
        {/* Üst bar */}
        <div className="flex items-center justify-between px-3 py-2 bg-slate-800 border-b border-slate-700">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            {/* "Dot trafik isigi" stil */}
            <div className="flex items-center gap-1 mr-2">
              <span className="w-2.5 h-2.5 rounded-full bg-red-500/80" />
              <span className="w-2.5 h-2.5 rounded-full bg-amber-500/80" />
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-500/80" />
            </div>
            <svg className="w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            <span className="text-sm font-semibold text-slate-100 truncate">{dosyaAdi}</span>
            <span className="text-xs text-slate-500 dark:text-slate-500 truncate min-w-0 hidden md:inline">— {yol}</span>
            {kayitDurum === 'kirli' && <span className="text-[10px] uppercase tracking-wider text-amber-400 bg-amber-500/15 px-1.5 py-0.5 rounded">Değişiklik var</span>}
            {kayitDurum === 'kaydediliyor' && <span className="text-[10px] uppercase tracking-wider text-sky-400 bg-sky-500/15 px-1.5 py-0.5 rounded">Kaydediliyor…</span>}
            {kayitDurum === 'kaydedildi' && <span className="text-[10px] uppercase tracking-wider text-emerald-400 bg-emerald-500/15 px-1.5 py-0.5 rounded">✓ Kaydedildi</span>}
          </div>

          <div className="flex flex-wrap items-center gap-1.5 flex-shrink-0">
            <select
              value={dil}
              onChange={e => setDil(e.target.value as Dil)}
              className="text-xs bg-slate-700 text-slate-100 border border-slate-600 rounded px-2 py-1 focus:outline-none focus:border-slate-400"
              title="Sözdizimi"
            >
              {DILLER.map(d => <option key={d.kod} value={d.kod}>{d.ad}</option>)}
            </select>
            <button
              onClick={() => setTamEkran(!tamEkran)}
              className="text-xs px-2 py-1 bg-slate-700 hover:bg-slate-600 text-slate-100 rounded"
              title={tamEkran ? 'Pencerele' : 'Tam ekran'}
            >
              {tamEkran ? '⛶' : '⛶'}
            </button>
            <button
              onClick={kaydet}
              disabled={kayitDurum === 'kaydediliyor' || kayitDurum === 'temiz'}
              className="text-xs px-3 py-1 bg-emerald-600 hover:bg-emerald-700 disabled:bg-slate-700 disabled:text-slate-500 dark:text-slate-500 text-white rounded font-medium"
              title="Ctrl+S"
            >
              💾 Kaydet
            </button>
            <button
              onClick={onKapat}
              className="text-xs px-3 py-1 bg-slate-700 hover:bg-slate-600 text-slate-100 rounded"
              title="ESC"
            >
              Kapat
            </button>
          </div>
        </div>

        {/* Editor */}
        <div className="flex-1 min-h-0 overflow-hidden">
          <CodeMirror
            value={icerik}
            height="100%"
            theme={oneDark}
            extensions={ext}
            onChange={onChange}
            basicSetup={{
              lineNumbers: true,
              highlightActiveLineGutter: true,
              highlightSpecialChars: true,
              foldGutter: true,
              drawSelection: true,
              dropCursor: true,
              allowMultipleSelections: true,
              indentOnInput: true,
              syntaxHighlighting: true,
              bracketMatching: true,
              closeBrackets: true,
              autocompletion: true,
              rectangularSelection: true,
              highlightActiveLine: true,
              highlightSelectionMatches: true,
              tabSize: 2,
            }}
            style={{ height: '100%' }}
          />
        </div>

        {/* Status bar */}
        <div className="flex items-center justify-between gap-4 px-3 py-1.5 bg-slate-800 border-t border-slate-700 text-[11px] text-slate-400 dark:text-slate-500 font-mono">
          <div className="flex items-center gap-4">
            <span>Satır {cursor.satir}, Kolon {cursor.kolon}</span>
            <span>{icerik.split('\n').length} satır</span>
            <span>{baytSayi.toLocaleString('tr-TR')} bayt</span>
          </div>
          <div className="flex items-center gap-3">
            <span>UTF-8</span>
            <span>LF</span>
            <span className="text-slate-300">{DILLER.find(d => d.kod === dil)?.ad}</span>
            <span className="text-slate-500 dark:text-slate-500">Ctrl+S: kaydet · Esc: kapat</span>
          </div>
        </div>
      </div>
    </div>
  )
}