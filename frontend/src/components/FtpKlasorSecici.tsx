// gosp-dark-swept
// gosp-dark-swept-v2
// Ek FTP hesabı için klasör seçici: yol kutusu + bulunulan klasörün alt klasörleri.
// Dosya yöneticisi ucunu kullanır (GET /domains/{id}/files?yol=); yol ev dizinine
// göredir ("/public_html/foto"). Seçimin geçerliliğini SUNUCU doğrular (.. / gizli
// klasör / sembolik bağ reddedilir); burada yalnız sade bir ön kontrol yapılır.
import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { Button, Input } from '@/components/ui'
import { I } from '@/components/Ikon'

type Girdi = { adi: string; yol: string; tip: 'klasor' | 'dosya' | 'sembolik' }

function ust(yol: string): string {
  const p = yol.split('/').filter(Boolean)
  p.pop()
  return '/' + p.join('/')
}

export default function FtpKlasorSecici({
  domainId, deger, onDeger, olustur, onOlustur, t,
}: {
  domainId: number | string
  deger: string
  onDeger: (yol: string) => void
  olustur: boolean
  onOlustur: (v: boolean) => void
  t: (tr: string) => string
}) {
  const [gezin, setGezin] = useState('/')
  const [klasorler, setKlasorler] = useState<Girdi[]>([])
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState(false)

  useEffect(() => {
    let iptal = false
    setYukleniyor(true)
    setHata(false)
    api.get<{ icerik: Girdi[] }>(`/domains/${domainId}/files`, { params: { yol: gezin } })
      .then(r => { if (!iptal) setKlasorler((r.data.icerik || []).filter(e => e.tip === 'klasor' && !e.adi.startsWith('.'))) })
      .catch(() => { if (!iptal) { setKlasorler([]); setHata(true) } })
      .finally(() => { if (!iptal) setYukleniyor(false) })
    return () => { iptal = true }
  }, [domainId, gezin])

  return (
    <div className="space-y-3">
      <Input
        label={t('Klasör')}
        value={deger}
        onChange={e => onDeger(e.target.value)}
        placeholder="/public_html/foto"
        spellCheck={false}
        autoComplete="off"
        classNames={{ input: 'font-mono text-sm' }}
      />
      <div className="rounded-lg border border-gray-200 dark:border-dark-500">
        <div className="flex flex-wrap items-center gap-2 border-b border-gray-200 px-3 py-2 dark:border-dark-500">
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-gray-500 dark:text-dark-300" title={gezin}>~{gezin === '/' ? '' : gezin}</span>
          {gezin !== '/' && (
            <Button variant="outlined" boyut="sm" ikon={I.yukari} onClick={() => setGezin(ust(gezin))}>{t('Üst klasör')}</Button>
          )}
        </div>
        <div className="max-h-44 space-y-0.5 overflow-auto p-1.5">
          {yukleniyor && <p className="px-2 py-1.5 text-xs text-gray-500 dark:text-dark-300">{t('yükleniyor…')}</p>}
          {!yukleniyor && hata && <p className="px-2 py-1.5 text-xs text-gray-500 dark:text-dark-300">{t('Klasörler okunamadı; yolu elle yazabilirsiniz.')}</p>}
          {!yukleniyor && !hata && klasorler.length === 0 && <p className="px-2 py-1.5 text-xs text-gray-500 dark:text-dark-300">{t('Alt klasör yok.')}</p>}
          {klasorler.map(k => (
            <Button key={k.yol} variant="flat" boyut="sm" ikon={I.klasor} className="w-full justify-start"
              onClick={() => { setGezin(k.yol); onDeger(k.yol) }} title={k.yol}>
              <span className="truncate">{k.adi}</span>
            </Button>
          ))}
        </div>
      </div>
      <label className="flex items-start gap-2 text-sm text-gray-700 dark:text-dark-100">
        <input type="checkbox" className="mt-0.5 size-4 rounded border-gray-300" checked={olustur} onChange={e => onOlustur(e.target.checked)} />
        <span>{t('Klasör yoksa oluştur')}</span>
      </label>
      <p className="text-xs text-gray-500 dark:text-dark-300">{t('Hesap yalnız bu klasörü ve altındakileri görür; ev dizininin kendisi ya da gizli (.) klasörler seçilemez.')}</p>
    </div>
  )
}
