// Uygulama geneli hata sınırı (React Error Boundary).
//
// 🔴 NEDEN: React'te render sırasında atılan HERHANGİ bir hata (bir alanın
// beklenmedik şekilde undefined gelmesi, hook sırası bozulması, vb.) tüm ağacı
// söker ve kullanıcı BEMBEYAZ bir sayfa görür. Ne olduğu yalnız tarayıcı
// konsolunda kalır; kullanıcı "panel bozuldu" der, biz de sebebi göremeyiz.
// Bu sınır, çökmeyi okunabilir bir karta çevirir: ne olduğu yazar, sayfa
// yenilenebilir ve teknik detay istenirse açılır.
//
// Hata sınırı YALNIZCA render/lifecycle hatalarını yakalar; olay işleyicileri
// ve async kodun hataları buraya düşmez (onlar zaten sayfayı çökertmez).
import { Component } from 'react'
import type { ErrorInfo, ReactNode } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { TonRozeti } from '@/components/UyariKutusu'
import { rolAnaSayfa } from '@/lib/rol'

const HATASINIRI_EN: Record<string, string> = {
  "Bu sayfa görüntülenemedi": "This page could not be displayed",
  "Beklenmeyen bir hata oluştu. Panelin diğer bölümleri çalışmaya devam ediyor — sayfayı yenilemeyi ya da bir önceki ekrana dönmeyi deneyin.": "An unexpected error occurred. Other parts of the panel keep working — try refreshing the page or returning to the previous screen.",
  "Sayfayı yenile": "Refresh page",
  "Anasayfaya dön": "Return to home",
  "Teknik ayrıntı (destek için)": "Technical detail (for support)",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (HATASINIRI_EN[tr] || ORTAK_EN[tr] || tr) : tr)

interface Props { children: ReactNode }
interface State { hata: Error | null; nerede: string }

export class HataSiniri extends Component<Props, State> {
  state: State = { hata: null, nerede: '' }

  static getDerivedStateFromError(hata: Error): Partial<State> {
    return { hata }
  }

  componentDidCatch(hata: Error, bilgi: ErrorInfo) {
    // Konsola tam izi bırak (destek isterse buradan okunur).
    console.error('Panel render hatası:', hata, bilgi.componentStack)
    this.setState({ nerede: (bilgi.componentStack || '').split('\n').slice(0, 6).join('\n') })
  }

  render() {
    const { hata, nerede } = this.state
    if (!hata) return this.props.children

    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-50 dark:bg-dark-800 p-4">
        <div className="max-w-lg w-full bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg shadow-xs p-6">
          <div className="flex items-start gap-3">
            <TonRozeti ton="tehlike" />
            <div className="min-w-0">
              <h1 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Bu sayfa görüntülenemedi")}</h1>
              <p className="mt-1.5 text-sm text-slate-500 dark:text-slate-400">
                {cevir("Beklenmeyen bir hata oluştu. Panelin diğer bölümleri çalışmaya devam ediyor — sayfayı yenilemeyi ya da bir önceki ekrana dönmeyi deneyin.")}
              </p>
            </div>
          </div>

          <div className="mt-4 flex items-center gap-2">
            <Button color="primary" ikon={I.yenile} onClick={() => window.location.reload()}>
              {cevir("Sayfayı yenile")}
            </Button>
            <Button variant="outlined" ikon={I.geri} onClick={() => { window.location.href = rolAnaSayfa() }}>
              {cevir("Anasayfaya dön")}
            </Button>
          </div>

          <details className="mt-4">
            <summary className="cursor-pointer text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300">
              {cevir("Teknik ayrıntı (destek için)")}
            </summary>
            <pre className="mt-2 max-h-52 overflow-auto text-[11px] font-mono text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-dark-800/50 rounded-lg p-3 border border-slate-200 dark:border-dark-600 whitespace-pre-wrap break-words">
{String(hata?.message || hata)}{nerede ? '\n' + nerede : ''}
            </pre>
          </details>
        </div>
      </div>
    )
  }
}
