// IP değiştir diyaloğu — yönetici (DomainsPage, toplu iş "ip") ve bayi (BayiDomainler,
// öğe başına PUT /domains/{id}/ip) toplu işlem çubuklarının ORTAK diyaloğu.
// Seçenekler GET /domains/ip-secenekleri'den gelir: sunucu rolün seçebileceği IP'leri
// döndürür (yönetici: tüm sunucu IP'leri; bayi: birincil + kendi kullandığı). Liste
// yalnız kolaylıktır — hedef IP her istekte sunucuda yeniden doğrulanır.
import { useCallback, useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, apiHata } from '@/lib/api'
import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { Button } from '@/components/ui'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I } from '@/components/Ikon'

export type IPSecenek = { ip: string; iface: string; primary: boolean; panel_ip: boolean; kullanim: number }
export type IPHedef = { id: number; alan_adi: string; ipv4?: string }

const IP_EN: Record<string, string> = {
  'IP değiştir': 'Change IP',
  'Hedef IP': 'Target IP',
  'birincil': 'primary',
  '{0} alan adı': '{0} domain(s)',
  'IP listesi alınamadı': 'Could not load the IP list',
  'Seçilebilecek IP yok': 'No selectable IP',
  'Etkilenecek alan adları': 'Affected domains',
  'zaten bu IP’de': 'already on this IP',
  'Alan adlarının DNS A kayıtları (www, mail, alt alan adları dahil) yeni IP’ye çevrilir, SPF kaydına yeni IP eklenir. Değişikliğin yayılması TTL süresine bağlıdır (genellikle 1 saate kadar).':
    'DNS A records of the domains (including www, mail and subdomains) are pointed to the new IP and the new IP is added to the SPF record. Propagation depends on the TTL (usually up to 1 hour).',
  'Alan adı dış bir DNS sağlayıcısı kullanıyorsa A kaydını orada kendiniz güncellemelisiniz.':
    'If a domain uses an external DNS provider, you must update its A record there yourself.',
  'Vazgeç': 'Cancel',
  'Yükleniyor…': 'Loading…',
}

function useIPCevir() {
  const { i18n } = useTranslation()
  const dil = i18n.language
  return useCallback((tr: string) => (dil === 'en' ? (IP_EN[tr] || ORTAK_EN[tr] || tr) : tr), [dil])
}

export default function IPDegistirDiyalog({ liste, kapat, onayla, yukleniyor }: {
  liste: IPHedef[]
  kapat: () => void
  onayla: (ip: string) => void
  yukleniyor?: boolean
}) {
  const t = useIPCevir()
  const sid = useId()
  const [secenekler, setSecenekler] = useState<IPSecenek[] | null>(null)
  const [hata, setHata] = useState('')
  const [ip, setIp] = useState('')
  useEffect(() => {
    let iptal = false
    api.get<{ ipler: IPSecenek[] }>('/domains/ip-secenekleri')
      .then((r) => {
        if (iptal) return
        const l = r.data?.ipler ?? []
        setSecenekler(l)
        // Varsayılan: seçili alan adlarının ÇOĞUNUN kullanmadığı ilk IP.
        const mevcut = new Set(liste.map((h) => h.ipv4).filter(Boolean))
        setIp((l.find((x) => !mevcut.has(x.ip)) ?? l[0])?.ip ?? '')
      })
      .catch((e) => { if (!iptal) { setSecenekler([]); setHata(apiHata(e, t('IP listesi alınamadı'))) } })
    return () => { iptal = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- liste diyalog açıkken sabit
  }, [])
  const ayni = liste.filter((h) => h.ipv4 === ip).length
  return (
    <Modal acik baslik={t('IP değiştir')} onKapat={kapat} kapatilamaz={yukleniyor} genislik="lg"
      alt={<>
        <Button variant="outlined" onClick={kapat} disabled={yukleniyor}>{t('Vazgeç')}</Button>
        <Button color="primary" ikon={I.kure} disabled={!ip || ayni === liste.length} yukleniyor={yukleniyor} onClick={() => ip && onayla(ip)}>
          {t('IP değiştir')} ({liste.length - ayni})
        </Button>
      </>}>
      <div className="space-y-4 text-sm">
        <div>
          <label htmlFor={sid} className="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-300">{t('Hedef IP')}</label>
          <select id={sid} value={ip} onChange={(e) => setIp(e.target.value)} disabled={!secenekler?.length}
            className="h-10 w-full rounded-lg border border-gray-300 bg-white px-3 font-mono text-sm text-gray-900 outline-none focus-visible:ring-2 focus-visible:ring-primary-600/70 dark:border-dark-450 dark:bg-dark-800 dark:text-dark-50">
            {secenekler === null && <option>{t('Yükleniyor…')}</option>}
            {secenekler?.length === 0 && <option value="">{t('Seçilebilecek IP yok')}</option>}
            {secenekler?.map((s) => (
              <option key={s.ip} value={s.ip}>
                {s.ip} · {s.iface}{s.primary ? ` · ${t('birincil')}` : ''} · {cevirT(t('{0} alan adı'), s.kullanim)}
              </option>
            ))}
          </select>
          {hata && <p className="mt-1.5 text-xs font-medium text-error">{hata}</p>}
        </div>
        <div>
          <div className="mb-1.5 text-xs font-medium text-gray-500 dark:text-dark-300">{t('Etkilenecek alan adları')} ({liste.length})</div>
          <ul className="max-h-40 divide-y divide-gray-200 overflow-y-auto rounded-lg border border-gray-200 dark:divide-dark-600 dark:border-dark-600" aria-label={t('Etkilenecek alan adları')}>
            {liste.map((h) => (
              <li key={h.id} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-0.5 px-3 py-1.5">
                <span className="min-w-0 truncate font-medium text-gray-900 dark:text-dark-50">{h.alan_adi}</span>
                <span className="shrink-0 font-mono text-xs text-gray-500 dark:text-dark-300">
                  {h.ipv4 === ip ? t('zaten bu IP’de') : <>{h.ipv4 || '—'} → <span className="text-gray-900 dark:text-dark-50">{ip || '—'}</span></>}
                </span>
              </li>
            ))}
          </ul>
        </div>
        <UyariKutusu ton="uyari" kucuk>
          <span className="leading-relaxed">
            {t('Alan adlarının DNS A kayıtları (www, mail, alt alan adları dahil) yeni IP’ye çevrilir, SPF kaydına yeni IP eklenir. Değişikliğin yayılması TTL süresine bağlıdır (genellikle 1 saate kadar).')}{' '}
            <b className="font-semibold">{t('Alan adı dış bir DNS sağlayıcısı kullanıyorsa A kaydını orada kendiniz güncellemelisiniz.')}</b>
          </span>
        </UyariKutusu>
      </div>
    </Modal>
  )
}
