import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'

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

const MODLAR: { key: Mod; ad: string; ikon: React.ReactNode; aciklama: string; renk: string }[] = [
  { key: 'devral', ad: 'Plandan Devral', ikon: <Ikon d={I.geriAl} className="inline-block h-4 w-4 align-[-3px]" />,
    aciklama: 'Bu domain, bağlı olduğu hizmet planının WAF varsayılanını kullanır.', renk: 'slate' },
  { key: 'engelle', ad: 'Engelle', ikon: <Ikon d={I.kalkan} className="inline-block h-4 w-4 align-[-3px]" />,
    aciklama: 'Kötü amaçlı istekler (SQLi, XSS, RCE…) 403 ile bloklanır. SecRuleEngine On.', renk: 'emerald' },
  { key: 'denetle', ad: 'Denetle', ikon: <Ikon d={I.goz} className="inline-block h-4 w-4 align-[-3px]" />,
    aciklama: 'İstekler bloklanmaz; yalnızca eşleşen kurallar audit log’a yazılır. DetectionOnly — kural ayarlamak için ideal.', renk: 'indigo' },
  { key: 'kapali', ad: 'Kapalı', ikon: <Ikon d={I.ban} className="inline-block h-4 w-4 align-[-3px]" />,
    aciklama: 'WAF bu domain için tamamen devre dışı (plan açık olsa bile).', renk: 'rose' },
]

const PARANOYA_ACIKLAMA: Record<number, string> = {
  0: 'Plan varsayılanı kullanılır.',
  1: 'Düşük — temel saldırı imzaları. Neredeyse hiç yanlış-pozitif. (önerilen)',
  2: 'Orta — daha fazla kural. Bazı meşru istekler engellenebilir.',
  3: 'Yüksek — agresif. Uygulamaya göre ayarlama (exclusion) gerekebilir.',
  4: 'Sıkı — en agresif. Yalnızca sıkı denetim gereken durumlar için.',
}


const WAF_EN: Record<string, string> = {
  "Türkçe": "English",
  "Plandan Devral": "Inherit from Plan",
  "Engelle": "Block",
  "Denetle": "Audit",
  "Kapalı": "Off",
  "WAF uygulandı — {0} modu, paranoya {1}": "WAF applied — {0} mode, paranoia {1}",
  "Engelleme": "Blocking",
  "Denetleme": "Auditing",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Kaydedince nginx vhost yeniden render edilir (sıfır kesinti).": "On save the nginx vhost is re-rendered (zero downtime).",
  "Ayarlar kaydedilir ancak WAF uygulanmaz.": "Settings are saved but the WAF is not applied.",
  "Sunucuda": "When",
  "çalıştırıldığında otomatik etkinleşir (mevcut siteler etkilenmez).": "is run on the server, it activates automatically (existing sites are not affected).",
  "Etkin Durum:": "Effective Status:",
  "Aktif · Engelleme": "Active · Blocking",
  "Aktif · Denetleme": "Active · Auditing",
  "Paranoya": "Paranoia",
  "Pasif": "Passive",
  "Plan varsayılanı": "Plan default",
  "WAF Modu": "WAF Mode",
  "Paranoya Seviyesi (CRS)": "Paranoia Level (CRS)",
  "Daha yüksek seviye = daha çok kural + daha güçlü koruma, ancak yanlış-pozitif olasılığı artar.": "Higher level = more rules + stronger protection, but the chance of false positives increases.",
  "Yalnızca WAF": "Only when the WAF is in",
  "veya": "or",
  "modundayken etkilidir.": "mode.",
  "Plandan devral": "Inherit from plan",
  "Seviye 2 (Orta)": "Level 2 (Medium)",
  "Uygulanıyor…": "Applying…",
  "Kaydet ve Uygula": "Save and Apply",
  "Yeniden Yükle": "Reload",
  "Bu domain, bağlı olduğu hizmet planının WAF varsayılanını kullanır.": "This domain uses the WAF default of its associated service plan.",
  "Düşük — temel saldırı imzaları. Neredeyse hiç yanlış-pozitif. (önerilen)": "Low — basic attack signatures. Almost no false positives. (recommended)",
  "Kötü amaçlı istekler (SQLi, XSS, RCE…) 403 ile bloklanır. SecRuleEngine On.": "Malicious requests (SQLi, XSS, RCE…) are blocked with 403. SecRuleEngine On.",
  "ModSecurity modülü sunucuda kurulu değil.": "The ModSecurity module is not installed on the server.",
  "Orta — daha fazla kural. Bazı meşru istekler engellenebilir.": "Medium — more rules. Some legitimate requests may be blocked.",
  "Plan varsayılanı kullanılır.": "The plan default is used.",
  "Seviye 1 (Düşük)": "Level 1 (Low)",
  "Seviye 3 (Yüksek)": "Level 3 (High)",
  "Seviye 4 (Sıkı)": "Level 4 (Strict)",
  "Sıkı — en agresif. Yalnızca sıkı denetim gereken durumlar için.": "Strict — most aggressive. Only for cases requiring strict inspection.",
  "WAF bu domain için tamamen devre dışı (plan açık olsa bile).": "WAF is fully disabled for this domain (even if the plan is on).",
  "Web Uygulama Güvenlik Duvarı": "Web Application Firewall",
  "Web Uygulama Güvenlik Duvarı (WAF)": "Web Application Firewall (WAF)",
  "Yüksek — agresif. Uygulamaya göre ayarlama (exclusion) gerekebilir.": "High — aggressive. May need per-application tuning (exclusions).",
  "İstekler bloklanmaz; yalnızca eşleşen kurallar audit log’a yazılır. DetectionOnly — kural ayarlamak için ideal.": "Requests are not blocked; only matching rules are written to the audit log. DetectionOnly — ideal for tuning rules.",
  "Seçili": "Selected",
  "Ayar kaydedildi — WAF bu domain için pasif": "Setting saved — WAF is passive for this domain",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WAF_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainWafPage() {
  useTranslation() // dil re-render aboneligi
  const toast = useToast()
  const { id } = useParams()
  const [y, setY] = useState<Yanit | null>(null)
  const [ayar, setAyar] = useState<Ayar | null>(null)
  const [yuk, setYuk] = useState(true)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Yanit>(`/domains/${id}/waf`)
      .then(r => { setY(r.data); setAyar(r.data.ayar) })
      .catch(e => {
        const m = apiHata(e)
        setHata(m)
        toast.hata(cevir("İşlem başarısız"), m)
      })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function kaydet() {
    if (!ayar) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const r = await api.put<{ efektif: Efektif; modul_yuklu: boolean }>(`/domains/${id}/waf`, { ayar })
      const ef = r.data.efektif
      const mesaj = ef.aktif
        ? cevirT(cevir("WAF uygulandı — {0} modu, paranoya {1}"), ef.engine === 'On' ? cevir("Engelleme") : cevir("Denetleme"), ef.paranoya)
        : cevir("Ayar kaydedildi — WAF bu domain için pasif")
      setBasari(mesaj)
      toast.basari(cevir("Kaydedildi"), mesaj)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Kaydetme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: y?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Web Uygulama Güvenlik Duvarı (WAF)") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Web Uygulama Güvenlik Duvarı")}</h1>
      {y && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 font-medium">{y.alan_adi}</Link>
        {' · '}ModSecurity v3 + OWASP Core Rule Set. {cevir("Kaydedince nginx vhost yeniden render edilir (sıfır kesinti).")}
      </p>}

      {y && !y.modul_yuklu && (
        <UyariKutusu ton="uyari" className="mb-5" baslik={cevir("ModSecurity modülü sunucuda kurulu değil.")}>
          <p className="text-xs">
            {cevir("Ayarlar kaydedilir ancak WAF uygulanmaz.")}
            {cevir("Sunucuda")} <code className="font-mono">girginospanel-waf-setup</code> {cevir("çalıştırıldığında otomatik etkinleşir (mevcut siteler etkilenmez).")}
          </p>
        </UyariKutusu>
      )}

      {yuk || !ayar || !y ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : (
        <>
          {/* Efektif durum + plan bilgisi */}
          <div className="mb-4 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Etkin Durum:")}</span>
              {y.efektif.aktif ? (
                <Badge color={y.efektif.engine === 'On' ? 'success' : 'info'} variant="soft" className="gap-1.5 px-2.5 font-semibold">
                  <span className="inline-block size-2 rounded-full bg-current" aria-hidden />
                  {y.efektif.engine === 'On' ? cevir("Aktif · Engelleme") : cevir("Aktif · Denetleme")} · {cevir("Paranoya")} {y.efektif.paranoya}
                </Badge>
              ) : (
                <Badge variant="soft" className="gap-1.5 px-2.5 font-semibold"><span className="inline-block size-2 rounded-full bg-slate-400 dark:bg-dark-300" aria-hidden />{cevir("Pasif")}</Badge>
              )}
              <span className="text-xs text-slate-400 dark:text-slate-500 ml-auto">
                {cevir("Plan varsayılanı")} ({y.plan.ad || '—'}):{' '}
                {y.plan.aktif ? `${y.plan.mod === 'denetle' ? cevir("Denetle") : cevir("Engelle")} · PL${y.plan.paranoya}` : cevir("Kapalı")}
              </span>
            </div>
          </div>

          {/* Mod seçici */}
          <Kart baslik={cevir("WAF Modu")}>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {MODLAR.map(m => {
                const aktif = ayar.mod === m.key
                const renk: Record<string, string> = {
                  slate:   aktif ? 'border-slate-500 bg-slate-100 dark:bg-dark-600/40 ring-2 ring-slate-400/20' : 'border-slate-200 dark:border-dark-600 hover:border-slate-400',
                  emerald: aktif ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20 ring-2 ring-emerald-500/20' : 'border-slate-200 dark:border-dark-600 hover:border-emerald-300',
                  indigo:  aktif ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20 ring-2 ring-indigo-500/20' : 'border-slate-200 dark:border-dark-600 hover:border-indigo-300',
                  rose:    aktif ? 'border-rose-500 bg-rose-50 dark:bg-rose-900/20 ring-2 ring-rose-500/20' : 'border-slate-200 dark:border-dark-600 hover:border-rose-300',
                }
                return (
                  <button key={m.key} type="button" onClick={() => setAyar({ ...ayar, mod: m.key })}
                    className={`text-left p-4 border rounded-lg transition ${renk[m.renk]}`}>
                    <div className="flex items-center justify-between mb-1">
                      <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ikon} {cevir(m.ad)}</span>
                      {aktif && <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wider font-semibold text-slate-500 dark:text-slate-400"><span className="inline-block size-2 rounded-full bg-current" aria-hidden />{cevir("Seçili")}</span>}
                    </div>
                    <div className="text-[11px] text-slate-600 dark:text-slate-400 leading-snug">{cevir(m.aciklama)}</div>
                  </button>
                )
              })}
            </div>
          </Kart>

          {/* Paranoya */}
          <Kart baslik={cevir("Paranoya Seviyesi (CRS)")}>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
              {cevir("Daha yüksek seviye = daha çok kural + daha güçlü koruma, ancak yanlış-pozitif olasılığı artar.")}
              {' '}{cevir("Yalnızca WAF")} <strong>{cevir("Engelle")}</strong> {cevir("veya")} <strong>{cevir("Denetle")}</strong> {cevir("modundayken etkilidir.")}
            </p>
            <div className="flex flex-wrap items-center gap-3">
              <select
                value={ayar.paranoya}
                onChange={e => setAyar({ ...ayar, paranoya: parseInt(e.target.value) })}
                disabled={ayar.mod === 'devral' || ayar.mod === 'kapali'}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-700 rounded text-sm font-mono disabled:opacity-50">
                <option value={0}>{cevir("Plandan devral")}</option>
                <option value={1}>{cevir("Seviye 1 (Düşük)")}</option>
                <option value={2}>{cevir("Seviye 2 (Orta)")}</option>
                <option value={3}>{cevir("Seviye 3 (Yüksek)")}</option>
                <option value={4}>{cevir("Seviye 4 (Sıkı)")}</option>
              </select>
              <span className="text-xs text-slate-500 dark:text-slate-400">{cevir(PARANOYA_ACIKLAMA[ayar.paranoya])}</span>
            </div>
          </Kart>

          <div className="flex gap-3 mt-6">
            <Button color="primary" onClick={kaydet} yukleniyor={isleniyor} ikon={I.disket}>
              {isleniyor ? cevir("Uygulanıyor…") : cevir("Kaydet ve Uygula")}
            </Button>
            <Button variant="outlined" onClick={yukle} disabled={isleniyor} ikon={I.yenile}>
              {cevir("Yeniden Yükle")}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function Kart({ baslik, children }: { baslik: string; children: React.ReactNode }) {
  return (
    <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-dark-600">{baslik}</h3>
      {children}
    </div>
  )
}
