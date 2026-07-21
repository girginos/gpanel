import { useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'

// Dashboard güvenlik-açığı (CVE) widget'ı.
// Backend: GET /system/cve (cache'li dnf updateinfo özeti) · POST /system/cve/guncelle
// (arka planda `dnf update --security`, sekme kapansa da sürer) · GET /system/cve/log (durum).
// Kart kabuğu HomePage'teki <Kart> ile görsel olarak eşleşir (ayrı bileşen olduğu için tekrar tanımlı).

type CveKayit = { id: string; severity: string; paket: string }
type CveOzet = {
  kritik: number
  onemli: number
  orta: number
  dusuk: number
  toplam_cve: number
  toplam_danisman: number
  son_tarama: string
  top_cve: CveKayit[] | null
  guncelleme_calisiyor: boolean
  reboot_gerekli: boolean
  kernelcare: {
    kurulu: boolean
    aktif: boolean
    kayitli: boolean
    efektif_kernel: string
    yamali_cve: string[] | null
    calisiyor: boolean
  }
}

const SHIELD = 'M12 3 4.5 6v5.5c0 4.2 3.2 7.1 7.5 8.5 4.3-1.4 7.5-4.3 7.5-8.5V6L12 3Z'
const CHECK = 'M9 12.5l2 2 4.5-4.5'
const ALERT = 'M12 9v3.5m0 3h.01'

// Önem etiketine göre renk/metin (semantik — marka turuncusundan ayrı).
const ONEM: Record<string, { ad: string; nokta: string; metin: string }> = {
  kritik: { ad: 'Kritik', nokta: 'bg-red-500', metin: 'text-red-600 dark:text-red-400' },
  onemli: { ad: 'Önemli', nokta: 'bg-amber-500', metin: 'text-amber-600 dark:text-amber-400' },
  orta: { ad: 'Orta', nokta: 'bg-sky-500', metin: 'text-sky-600 dark:text-sky-400' },
  dusuk: { ad: 'Düşük', nokta: 'bg-slate-400', metin: 'text-slate-500 dark:text-slate-400' },
}

export default function CveWidget() {
  const [veri, setVeri] = useState<CveOzet | null>(null)
  const [hata, setHata] = useState('')
  const [taraniyor, setTaraniyor] = useState(false)
  const [guncelleniyor, setGuncelleniyor] = useState(false)
  const [kcCalisiyor, setKcCalisiyor] = useState(false)
  const [mesaj, setMesaj] = useState('')
  const pollRef = useRef<number | null>(null)
  const kcPollRef = useRef<number | null>(null)

  async function getir(yenile: boolean) {
    try {
      const { data } = await api.get<CveOzet>(`/system/cve${yenile ? '?yenile=1' : ''}`, { timeout: 120_000 })
      setVeri(data)
      setHata('')
      if (data.guncelleme_calisiyor) baslatPoll()
    } catch (e) {
      setHata(apiHata(e, 'CVE bilgisi alınamadı'))
    }
  }

  useEffect(() => {
    getir(false)
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
      if (kcPollRef.current) window.clearInterval(kcPollRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function baslatPoll() {
    setGuncelleniyor(true)
    if (pollRef.current) return
    pollRef.current = window.setInterval(async () => {
      try {
        const { data } = await api.get<{ calisiyor: boolean }>('/system/cve/log')
        if (!data.calisiyor) {
          if (pollRef.current) { window.clearInterval(pollRef.current); pollRef.current = null }
          setGuncelleniyor(false)
          setMesaj('Güvenlik güncellemeleri tamamlandı.')
          getir(true)
        }
      } catch { /* geçici — bir sonraki tick tekrar dener */ }
    }, 5000)
  }

  async function yenidenTara() {
    setTaraniyor(true)
    setMesaj('')
    await getir(true)
    setTaraniyor(false)
  }

  async function guncelle() {
    if (!window.confirm(
      'Sunucudaki güvenlik güncellemeleri (dnf --security) kurulacak. ' +
      'Çekirdek (kernel) güncellemesi varsa etkin olması için yeniden başlatma gerekebilir. Devam edilsin mi?',
    )) return
    setHata('')
    setMesaj('')
    try {
      await api.post('/system/cve/guncelle')
      baslatPoll()
    } catch (e) {
      setHata(apiHata(e, 'Güncelleme başlatılamadı'))
    }
  }

  function baslatKcPoll() {
    setKcCalisiyor(true)
    if (kcPollRef.current) return
    kcPollRef.current = window.setInterval(async () => {
      try {
        const { data } = await api.get<{ calisiyor: boolean }>('/system/kernelcare')
        if (!data.calisiyor) {
          if (kcPollRef.current) { window.clearInterval(kcPollRef.current); kcPollRef.current = null }
          setKcCalisiyor(false)
          setMesaj('Canlı çekirdek yaması uygulandı.')
          getir(true)
        }
      } catch { /* geçici — bir sonraki tick tekrar dener */ }
    }, 5000)
  }

  async function canliYamala() {
    setHata('')
    setMesaj('')
    try {
      await api.post('/system/kernelcare/yamala')
      baslatKcPoll()
    } catch (e) {
      setHata(apiHata(e, 'Canlı yama başlatılamadı'))
    }
  }

  // Başlık ikonu rengi: kritik varsa kırmızı, önemli varsa amber, temizse yeşil.
  const durumRenk = !veri ? 'text-slate-400 dark:text-slate-500'
    : veri.kritik > 0 ? 'text-red-500'
      : veri.onemli > 0 ? 'text-amber-500'
        : 'text-emerald-500'

  const temiz = veri !== null && veri.toplam_cve === 0
  const top = veri?.top_cve ?? []

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900/60">
      {/* başlık */}
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="relative inline-flex">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round"
              className={`h-5 w-5 ${durumRenk}`}>
              <path d={SHIELD} />
              <path d={temiz ? CHECK : ALERT} />
            </svg>
          </span>
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Güvenlik Açıkları</h3>
            <p className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">
              {veri ? `${veri.toplam_danisman} güvenlik danışmanı` : 'CVE denetimi (AlmaLinux)'}
            </p>
          </div>
        </div>
        <button
          type="button"
          onClick={yenidenTara}
          disabled={taraniyor || guncelleniyor}
          className="shrink-0 rounded-lg px-2 py-1 text-[11px] font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700 disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200">
          {taraniyor ? 'Taranıyor…' : 'Yeniden tara'}
        </button>
      </div>

      {/* güncelleme her durumda üstte görünür (yükleme/temiz dahil) */}
      {guncelleniyor && (
        <div className="mb-3 flex items-center gap-2 rounded-xl border border-brand-200 bg-brand-50 px-3 py-2.5 text-[12px] font-medium text-brand-700 dark:border-brand-900/50 dark:bg-brand-900/15 dark:text-brand-300">
          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-brand-400 border-t-transparent" />
          Güvenlik güncellemeleri kuruluyor… (arka planda sürer)
        </div>
      )}

      {/* KernelCare canlı yama uygulanıyor */}
      {kcCalisiyor && (
        <div className="mb-3 flex items-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2.5 text-[12px] font-medium text-emerald-700 dark:border-emerald-800/50 dark:bg-emerald-900/15 dark:text-emerald-300">
          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-emerald-400 border-t-transparent" />
          Canlı çekirdek yaması uygulanıyor… (KernelCare — reboot gerekmez)
        </div>
      )}

      {/* KernelCare aktif — çekirdek canlı yamalı */}
      {!kcCalisiyor && veri?.kernelcare?.aktif && (
        <div className="mb-3 flex items-start gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2.5 text-[11px] text-emerald-700 dark:border-emerald-800/50 dark:bg-emerald-900/15 dark:text-emerald-300">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="mt-0.5 h-4 w-4 shrink-0"><path d={SHIELD} /><path d={CHECK} /></svg>
          <span>
            <strong>Çekirdek canlı yamalı (KernelCare).</strong> Çekirdek güvenlik açıkları sunucu yeniden başlatılmadan kapatıldı.
            {veri.kernelcare.efektif_kernel ? <> Efektif çekirdek: <span className="font-mono">{veri.kernelcare.efektif_kernel}</span>.</> : null}
            {veri.kernelcare.yamali_cve?.length ? <> {veri.kernelcare.yamali_cve.length} CVE canlı yamalı.</> : null}
          </span>
        </div>
      )}

      {/* KernelCare kurulu ama lisans kayıtlı değil */}
      {!kcCalisiyor && veri?.kernelcare?.kurulu && !veri.kernelcare.kayitli && (
        <div className="mb-3 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2.5 text-[11px] text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/15 dark:text-amber-300">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} className="mt-0.5 h-3.5 w-3.5 shrink-0"><path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m0 3.75h.008M10.36 3.6 2.26 17.66A1.5 1.5 0 0 0 3.56 19.9h16.88a1.5 1.5 0 0 0 1.3-2.25L13.64 3.6a1.5 1.5 0 0 0-2.6 0Z" /></svg>
          <span><strong>KernelCare kurulu ancak lisans kayıtlı değil.</strong> Rebootsuz çekirdek yaması için TuxCare lisans anahtarıyla kaydedilmeli.</span>
        </div>
      )}

      {/* yeniden başlatma gerekli — yamalı çekirdek kurulu ama henüz etkin değil */}
      {!guncelleniyor && veri?.reboot_gerekli && (
        <div className="mb-3 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2.5 text-[11px] text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/15 dark:text-amber-300">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} className="mt-0.5 h-3.5 w-3.5 shrink-0"><path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m0 3.75h.008M10.36 3.6 2.26 17.66A1.5 1.5 0 0 0 3.56 19.9h16.88a1.5 1.5 0 0 0 1.3-2.25L13.64 3.6a1.5 1.5 0 0 0-2.6 0Z" /></svg>
          <span>
            <strong>Yeniden başlatma gerekli.</strong> Güvenlik yamalı yeni çekirdek kurulu ancak sistem hâlâ eski çekirdekle çalışıyor —
            aşağıdaki açıkların çoğu çekirdek kaynaklı ve <strong>sunucu yeniden başlatılana kadar</strong> açık görünür.
            Bakım penceresinde reboot önerilir.
          </span>
        </div>
      )}

      {/* gövde */}
      {hata ? (
        <div className="flex items-start gap-2 rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-[11px] text-red-700 dark:border-red-900/50 dark:bg-red-900/15 dark:text-red-300">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} className="mt-0.5 h-3.5 w-3.5 shrink-0"><path strokeLinecap="round" d="M12 9v3.75m0 3.75h.008M10.36 3.6 2.26 17.66A1.5 1.5 0 0 0 3.56 19.9h16.88a1.5 1.5 0 0 0 1.3-2.25L13.64 3.6a1.5 1.5 0 0 0-2.6 0Z" /></svg>
          <span>{hata}</span>
        </div>
      ) : veri === null ? (
        <div className="flex items-center justify-center gap-2 py-6 text-xs text-slate-400">
          <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-slate-300 border-t-transparent dark:border-slate-600 dark:border-t-transparent" />
          Sunucu taranıyor…
        </div>
      ) : temiz ? (
        <div className="flex flex-col items-center gap-1.5 py-5 text-center">
          <span className="flex h-10 w-10 items-center justify-center rounded-full bg-emerald-50 dark:bg-emerald-900/25">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5 text-emerald-500"><path d="M20 6 9 17l-5-5" /></svg>
          </span>
          <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Sistem güncel</p>
          <p className="text-[11px] text-slate-400 dark:text-slate-500">Bilinen bir güvenlik açığı yok</p>
        </div>
      ) : (
        <>
          {/* önem özeti */}
          <div className="mb-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
            {(['kritik', 'onemli', 'orta'] as const).map((k) => (
              <div key={k} className="rounded-xl border border-slate-100 bg-slate-50 p-3 text-center dark:border-slate-800 dark:bg-slate-950/40">
                <div className={`text-2xl font-bold tabular-nums ${ONEM[k].metin}`}>{veri[k]}</div>
                <div className="mt-0.5 flex items-center justify-center gap-1 text-[11px] text-slate-400 dark:text-slate-500">
                  <span className={`h-1.5 w-1.5 rounded-full ${ONEM[k].nokta}`} />{ONEM[k].ad}
                </div>
              </div>
            ))}
          </div>
          <p className="mb-3 text-[11px] text-slate-400 dark:text-slate-500">
            Toplam <strong className="text-slate-600 dark:text-slate-300">{veri.toplam_cve}</strong> benzersiz CVE
            {veri.son_tarama ? <> · son tarama {veri.son_tarama}</> : null}
          </p>

          {/* öne çıkan CVE'ler */}
          {top.length > 0 && (
            <div className="mb-3 space-y-0.5">
              {top.slice(0, 4).map((c) => (
                <div key={c.id} className="-mx-2 flex items-center justify-between gap-2 rounded-lg px-2 py-1.5">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ONEM[c.severity]?.nokta ?? 'bg-slate-400'}`} />
                    <span className="font-mono text-[12px] text-slate-700 dark:text-slate-200">{c.id}</span>
                  </span>
                  <span className="min-w-0 truncate text-right text-[10px] text-slate-400 dark:text-slate-500" title={c.paket}>{c.paket}</span>
                </div>
              ))}
            </div>
          )}

          {/* aksiyon (güncelleme sürerken banner yukarıda gösterilir) */}
          {!guncelleniyor && (
            <button
              type="button"
              onClick={guncelle}
              className="flex w-full items-center justify-center gap-2 rounded-xl bg-brand-600 px-3 py-2.5 text-[13px] font-semibold text-white shadow-sm transition-colors hover:bg-brand-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><path d={SHIELD} /><path d={CHECK} /></svg>
              Güvenlik güncellemelerini kur
            </button>
          )}
          {/* KernelCare — rebootsuz canlı çekirdek yaması aksiyonu */}
          {!kcCalisiyor && veri.kernelcare?.kurulu && veri.kernelcare.kayitli && (
            <button
              type="button"
              onClick={canliYamala}
              className="mt-2 flex w-full items-center justify-center gap-2 rounded-xl border border-emerald-300 bg-emerald-50 px-3 py-2.5 text-[13px] font-semibold text-emerald-700 transition-colors hover:bg-emerald-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-500 dark:border-emerald-800/60 dark:bg-emerald-900/20 dark:text-emerald-300 dark:hover:bg-emerald-900/30">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><path d={SHIELD} /><path d={CHECK} /></svg>
              Canlı çekirdek yamalarını güncelle (reboot yok)
            </button>
          )}
          {mesaj && <p className="mt-2 text-center text-[11px] font-medium text-emerald-600 dark:text-emerald-400">{mesaj}</p>}
        </>
      )}
    </div>
  )
}
