// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Mod = 'devral' | 'kapali' | 'engelle' | 'denetle'
type Ayar = { mod: Mod; paranoya: number }
type ModBilgi = { aktif: boolean; mod: string; paranoya: number; ad?: string }
type Efektif = { aktif: boolean; engine: string; paranoya: number }
type Yanit = {
  alan_adi: string
  ayar: Ayar
  plan: ModBilgi
  efektif: Efektif
  modul_yuklu: boolean
}

const MODLAR: { key: Mod; ad: string; ikon: string; aciklama: string; renk: string }[] = [
  { key: 'devral', ad: 'Plandan Devral', ikon: '↩︎',
    aciklama: 'Bu domain, bağlı olduğu hizmet planının WAF varsayılanını kullanır.', renk: 'slate' },
  { key: 'engelle', ad: 'Engelle', ikon: '🛡️',
    aciklama: 'Kötü amaçlı istekler (SQLi, XSS, RCE…) 403 ile bloklanır. SecRuleEngine On.', renk: 'emerald' },
  { key: 'denetle', ad: 'Denetle', ikon: '👁️',
    aciklama: 'İstekler bloklanmaz; yalnızca eşleşen kurallar audit log’a yazılır. DetectionOnly — kural ayarlamak için ideal.', renk: 'indigo' },
  { key: 'kapali', ad: 'Kapalı', ikon: '⛔',
    aciklama: 'WAF bu domain için tamamen devre dışı (plan açık olsa bile).', renk: 'rose' },
]

const PARANOYA_ACIKLAMA: Record<number, string> = {
  0: 'Plan varsayılanı kullanılır.',
  1: 'Düşük — temel saldırı imzaları. Neredeyse hiç yanlış-pozitif. (önerilen)',
  2: 'Orta — daha fazla kural. Bazı meşru istekler engellenebilir.',
  3: 'Yüksek — agresif. Uygulamaya göre ayarlama (exclusion) gerekebilir.',
  4: 'Sıkı — en agresif. Yalnızca sıkı denetim gereken durumlar için.',
}

export default function DomainWafPage() {
  const { id } = useParams()
  const [y, setY] = useState<Yanit | null>(null)
  const [ayar, setAyar] = useState<Ayar | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Yanit>(`/domains/${id}/waf`)
      .then(r => { setY(r.data); setAyar(r.data.ayar) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function kaydet() {
    if (!ayar) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const r = await api.put<{ efektif: Efektif; modul_yuklu: boolean }>(`/domains/${id}/waf`, { ayar })
      const ef = r.data.efektif
      setBasari(ef.aktif
        ? `✓ WAF uygulandı — ${ef.engine === 'On' ? 'Engelleme' : 'Denetleme'} modu, paranoya ${ef.paranoya}`
        : '✓ Ayar kaydedildi — WAF bu domain için pasif')
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Kaydetme başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: y?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Web Uygulama Güvenlik Duvarı (WAF)' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Web Uygulama Güvenlik Duvarı</h1>
      {y && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 font-medium">{y.alan_adi}</Link>
        {' · '}ModSecurity v3 + OWASP Core Rule Set. Kaydedince nginx vhost yeniden render edilir (sıfır kesinti).
      </p>}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {y && !y.modul_yuklu && (
        <div className="mb-5 px-3 py-2.5 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md text-xs text-amber-800 dark:text-amber-200">
          <strong>ModSecurity modülü sunucuda kurulu değil.</strong> Ayarlar kaydedilir ancak WAF uygulanmaz.
          Sunucuda <code className="font-mono">girginospanel-waf-setup</code> çalıştırıldığında otomatik etkinleşir (mevcut siteler etkilenmez).
        </div>
      )}

      {yuk || !ayar || !y ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : (
        <>
          {/* Efektif durum + plan bilgisi */}
          <div className="mb-4 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">Etkin Durum:</span>
              {y.efektif.aktif ? (
                <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold ${
                  y.efektif.engine === 'On'
                    ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300'
                    : 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300'
                }`}>
                  ● {y.efektif.engine === 'On' ? 'Aktif · Engelleme' : 'Aktif · Denetleme'} · Paranoya {y.efektif.paranoya}
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400">○ Pasif</span>
              )}
              <span className="text-xs text-slate-400 dark:text-slate-500 ml-auto">
                Plan varsayılanı ({y.plan.ad || '—'}):{' '}
                {y.plan.aktif ? `${y.plan.mod === 'denetle' ? 'Denetle' : 'Engelle'} · PL${y.plan.paranoya}` : 'Kapalı'}
              </span>
            </div>
          </div>

          {/* Mod seçici */}
          <Kart baslik="WAF Modu">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {MODLAR.map(m => {
                const aktif = ayar.mod === m.key
                const renk: Record<string, string> = {
                  slate:   aktif ? 'border-slate-500 bg-slate-100 dark:bg-slate-700/40 ring-2 ring-slate-400/20' : 'border-slate-200 dark:border-slate-700 hover:border-slate-400',
                  emerald: aktif ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20 ring-2 ring-emerald-500/20' : 'border-slate-200 dark:border-slate-700 hover:border-emerald-300',
                  indigo:  aktif ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20 ring-2 ring-indigo-500/20' : 'border-slate-200 dark:border-slate-700 hover:border-indigo-300',
                  rose:    aktif ? 'border-rose-500 bg-rose-50 dark:bg-rose-900/20 ring-2 ring-rose-500/20' : 'border-slate-200 dark:border-slate-700 hover:border-rose-300',
                }
                return (
                  <button key={m.key} type="button" onClick={() => setAyar({ ...ayar, mod: m.key })}
                    className={`text-left p-4 border rounded-xl transition ${renk[m.renk]}`}>
                    <div className="flex items-center justify-between mb-1">
                      <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ikon} {m.ad}</span>
                      {aktif && <span className="text-[10px] uppercase tracking-wider font-semibold text-slate-500 dark:text-slate-400">● Seçili</span>}
                    </div>
                    <div className="text-[11px] text-slate-600 dark:text-slate-400 leading-snug">{m.aciklama}</div>
                  </button>
                )
              })}
            </div>
          </Kart>

          {/* Paranoya */}
          <Kart baslik="Paranoya Seviyesi (CRS)">
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
              Daha yüksek seviye = daha çok kural + daha güçlü koruma, ancak yanlış-pozitif olasılığı artar.
              Yalnızca WAF <strong>Engelle</strong> veya <strong>Denetle</strong> modundayken etkilidir.
            </p>
            <div className="flex flex-wrap items-center gap-3">
              <select
                value={ayar.paranoya}
                onChange={e => setAyar({ ...ayar, paranoya: parseInt(e.target.value) })}
                disabled={ayar.mod === 'devral' || ayar.mod === 'kapali'}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-800 rounded text-sm font-mono disabled:opacity-50">
                <option value={0}>Plandan devral</option>
                <option value={1}>Seviye 1 (Düşük)</option>
                <option value={2}>Seviye 2 (Orta)</option>
                <option value={3}>Seviye 3 (Yüksek)</option>
                <option value={4}>Seviye 4 (Sıkı)</option>
              </select>
              <span className="text-xs text-slate-500 dark:text-slate-400">{PARANOYA_ACIKLAMA[ayar.paranoya]}</span>
            </div>
          </Kart>

          <div className="flex gap-3 mt-6">
            <button onClick={kaydet} disabled={isleniyor}
              className="px-6 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
              {isleniyor ? 'Uygulanıyor…' : '💾 Kaydet ve Uygula'}
            </button>
            <button onClick={yukle} disabled={isleniyor}
              className="px-4 py-2.5 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-300 text-sm rounded-md">
              Yeniden Yükle
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function Kart({ baslik, children }: { baslik: string; children: any }) {
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-slate-800">{baslik}</h3>
      {children}
    </div>
  )
}
