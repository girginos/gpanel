import type { FormEvent } from 'react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, apiHata } from '@/lib/api'
import { cevirT } from '@/lib/cevirT'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Badge, Button } from '@/components/ui'

/*
 * Panel kapıları (ADR-GP-001) — Port Yönetimi'nin kapı modu kartı.
 *
 * Yönetici, bayi ve müşteri panele AYRI portlardan girer. Yanlış kapıda giriş formu
 * yoktur (sunucu 404), oturum yalnız alındığı kapıda geçerlidir. Geçişte eski port
 * "geçiş kapısı" olarak açık kalır ve yönetici yeni adresinden bir kez girene dek
 * KAPANMAZ (sağlayıcı firewall'u yeni portu kesiyorsa kilitlenme olmasın).
 */

export type KapiDurum = {
  admin: number
  bayi: number
  musteri: number
  eski_portlar: number[]
  eski_bitis: number
  eski_acik: boolean
  admin_dogrulandi: number
  // Bayi/müşteri kapısından (o rolün son port değişikliğinden beri) ilk dış giriş; 0/yok = görülmedi.
  ilk_giris?: { bayi?: number; musteri?: number }
  zorlama: boolean
  hata?: string
}

type Rol = 'admin' | 'bayi' | 'musteri'
type RolSonuc = { rol: Rol; eski_port: number; yeni_port: number; zorlama: boolean; uyari?: string }

const EN: Record<string, string> = {
  'Panel kapıları': 'Panel gates',
  'Yönetici, bayi ve müşteri panele ayrı portlardan girer. Yanlış kapıda giriş formu yoktur; oturum yalnız alındığı kapıda geçerlidir.':
    'Administrators, resellers and customers reach the panel on separate ports. A gate has no sign-in for other roles, and a session only works on the gate it was issued on.',
  'Etkin': 'Active',
  'Devre dışı': 'Inactive',
  'Kapı kuralları şu an devre dışı: nginx zinciri doğrulanamadı, panel tek-port davranışında (kimse kilitlenmez). Ayrıntı:':
    'Gate rules are currently inactive: the nginx chain could not be verified, so the panel behaves as a single port (nobody is locked out). Details:',
  'Kapı ayarı okunamadı': 'Gate settings could not be read',
  'Yönetici': 'Administrator',
  'Bayi': 'Reseller',
  'Müşteri': 'Customer',
  'Gizli tutun': 'Keep private',
  'Bayilerinize verin': 'Give to your resellers',
  'Müşterilerinize verin': 'Give to your customers',
  'Kopyala': 'Copy',
  'Adres kopyalandı': 'Address copied',
  'Değiştir': 'Change',
  'Vazgeç': 'Cancel',
  'Yeni port': 'New port',
  'rastgele': 'random',
  'Boş bırakırsanız kapı aralığından rastgele bir port atanır.': 'Leave it empty to get a random port from the gate range.',
  'Portu değiştir': 'Change port',
  'Geçersiz port': 'Invalid port',
  '1024–65535 arası bir port girin ya da boş bırakın.': 'Enter a port between 1024 and 65535, or leave it empty.',
  '{0} kapısı değişsin mi?': 'Change the {0} gate?',
  'Yeni bir yönetici adresi oluşur. Şu anki adres, yeni adresten bir kez giriş yapana dek geçiş kapısı olarak AÇIK kalır — kilitlenmezsiniz.':
    'A new administrator address is created. The current address stays OPEN as a transition gate until you sign in once at the new address — you cannot lock yourself out.',
  'Önceki port HEMEN kapanır; bu roldeki kullanıcılar yeni adresi kullanmalıdır.':
    'The previous port closes IMMEDIATELY; users of this role must use the new address.',
  'Bulut sağlayıcınızın güvenlik duvarı varsa yeni portu orada da açın.':
    'If your cloud provider has a firewall, open the new port there too.',
  'Kapı portu değişti': 'Gate port changed',
  'Port değiştirilemedi': 'Could not change the port',
  'Değişiklik kaydedildi ama uygulanamadı': 'The change was saved but could not be applied',
  'Yeni yönetici adresi': 'New administrator address',
  'Bu sekmedeki oturum sona erecek. Yeni adreste giriş yapın:': 'The session in this tab will end. Sign in at the new address:',
  'Yeni adrese git': 'Go to the new address',
  'Bu sayfada kal': 'Stay on this page',
  'Geçiş kapısı (eski adres)': 'Transition gate (old address)',
  'Açık': 'Open',
  'Kapanıyor': 'Closing',
  'Geçiş süresince her rol bu adresten girebilir; girenlere kendi yeni adresleri gösterilir.':
    'During the transition every role can sign in here; users who do are shown their new address.',
  'En erken kapanış': 'Earliest closing',
  'Yönetici yeni kapıdan girdi': 'Administrator used the new gate',
  'Henüz girmedi — eski adres, siz yeni yönetici adresinden bir kez giriş yapana dek AÇIK kalır.':
    'Not yet — the old address stays OPEN until you sign in once at the new administrator address.',
  'Süreyi uzat': 'Extend by',
  'gün': 'days',
  'Uzat': 'Extend',
  'Şimdi kapat': 'Close now',
  'Doğrulamadan zorla kapat…': 'Force close without verification…',
  'Geçiş süresi uzatıldı': 'Transition extended',
  'Geçiş kapısı kapatılıyor': 'Transition gate is closing',
  'İşlem başarısız': 'Operation failed',
  'Geçiş kapısı şimdi kapatılsın mı?': 'Close the transition gate now?',
  'Eski adresi kullanmaya devam eden kullanıcılar panele o adresten ulaşamaz.':
    'Users still on the old address will no longer reach the panel there.',
  'Yönetici doğrulaması OLMADAN kapatılsın mı?': 'Close WITHOUT administrator verification?',
  'Yeni yönetici adresinden henüz hiç giriş yapılmadı. Sağlayıcınızın güvenlik duvarı yeni portu kesiyorsa eski adres de kapanınca panele HİÇ ulaşamazsınız (yalnız SSH ile düzeltilir). Emin değilseniz önce yeni adresten giriş yapın.':
    'Nobody has signed in at the new administrator address yet. If your provider firewall blocks the new port and the old address closes too, you will NOT reach the panel at all (fixable only over SSH). If unsure, sign in at the new address first.',
  'Zorla kapat': 'Force close',
  'Kapat': 'Close',
  'Değişiklik kaydedildi': 'Change saved',
  'Kapı zorlaması şu an kapalı — panel tek-port davranışında, bu sekmedeki mevcut adres çalışmaya devam eder. Kapı kuralları, zorlama doğrulandığında devreye girer.':
    'Gate enforcement is currently off — the panel behaves as a single port and the current address in this tab keeps working. Gate rules take effect once enforcement is verified.',
  'Geçersiz süre': 'Invalid duration',
  '1–365 arası tam gün sayısı girin.': 'Enter a whole number of days between 1 and 365.',
  'Yeni kapıdan ilk giriş:': 'First sign-in on the new gate:',
  'Yeni kapıdan henüz giriş yok': 'No sign-in on the new gate yet',
  '{0}: yeni kapıdan henüz hiç giriş görülmedi — bu kullanıcılar hâlâ eski adresi kullanıyor olabilir.':
    '{0}: no sign-in has been seen on the new gate yet — these users may still be using the old address.',
}

export default function PanelKapilari({ kapi, onDegisti }: { kapi: KapiDurum; onDegisti: () => void }) {
  const { i18n } = useTranslation()
  const cevir = (tr: string) => (i18n.language === 'en' ? EN[tr] || tr : tr)
  const dialog = useDialog()
  const toast = useToast()
  const [duzenlenen, setDuzenlenen] = useState<Rol | null>(null)
  const [yeniPort, setYeniPort] = useState('')
  const [gun, setGun] = useState('30')
  const [mesgul, setMesgul] = useState(false)

  const adres = (p: number) => `https://${window.location.hostname}:${p}`
  const yerel = i18n.language === 'en' ? 'en-GB' : 'tr-TR'
  const tarih = (ts: number) => new Date(ts * 1000).toLocaleDateString(yerel)
  const tarihSaat = (ts: number) => new Date(ts * 1000).toLocaleString(yerel)

  const roller: { rol: Rol; etiket: string; not: string; port: number }[] = [
    { rol: 'admin', etiket: cevir('Yönetici'), not: cevir('Gizli tutun'), port: kapi.admin },
    { rol: 'bayi', etiket: cevir('Bayi'), not: cevir('Bayilerinize verin'), port: kapi.bayi },
    { rol: 'musteri', etiket: cevir('Müşteri'), not: cevir('Müşterilerinize verin'), port: kapi.musteri },
  ]
  // Rolün yeni kapısından ilk giriş (unix sn; 0 = görülmedi). Yönetici için admin_dogrulandi.
  const ilkGiris = (rol: Rol): number => (rol === 'admin' ? kapi.admin_dogrulandi : kapi.ilk_giris?.[rol] || 0)

  async function kopyala(metin: string) {
    try {
      await navigator.clipboard.writeText(metin)
      toast.basari(cevir('Adres kopyalandı'), metin)
    } catch { /* pano izni yok — adres zaten ekranda seçilebilir */ }
  }

  async function rolDegistir(ev: FormEvent, rol: Rol, etiket: string) {
    ev.preventDefault()
    const p = yeniPort.trim() === '' ? 0 : parseInt(yeniPort, 10)
    if (p !== 0 && !(p >= 1024 && p <= 65535)) {
      toast.hata(cevir('Geçersiz port'), cevir('1024–65535 arası bir port girin ya da boş bırakın.'))
      return
    }
    const ok = await dialog.onay({
      baslik: cevirT(cevir('{0} kapısı değişsin mi?'), etiket),
      mesaj: [
        rol === 'admin'
          ? cevir('Yeni bir yönetici adresi oluşur. Şu anki adres, yeni adresten bir kez giriş yapana dek geçiş kapısı olarak AÇIK kalır — kilitlenmezsiniz.')
          : cevir('Önceki port HEMEN kapanır; bu roldeki kullanıcılar yeni adresi kullanmalıdır.'),
        cevir('Bulut sağlayıcınızın güvenlik duvarı varsa yeni portu orada da açın.'),
      ].join(' '),
      onayEtiketi: cevir('Portu değiştir'), iptalEtiketi: cevir('Vazgeç'), tehlike: rol !== 'admin',
    })
    if (!ok) return
    setMesgul(true)
    try {
      const r = await api.post<RolSonuc>('/portlar/kapi/rol', { rol, yeni_port: p })
      setDuzenlenen(null)
      setYeniPort('')
      toast.basari(cevir('Kapı portu değişti'), `${etiket}: ${r.data.eski_port} → ${r.data.yeni_port}`)
      // Sonraki adım yanıta göre: uyarı varsa kurallar uygulanamadı (yeni port dinlenmiyor
      // olabilir) → yalnız uyarı; zorlama kapalıysa panel tek-port davranışında, mevcut adres
      // çalışır → yalnız bilgi. Yeni yönetici adresine davet YALNIZ uyarısız + zorlama açıkken.
      if (r.data.uyari) {
        await dialog.bilgi({ baslik: cevir('Değişiklik kaydedildi ama uygulanamadı'), mesaj: r.data.uyari })
      } else if (!r.data.zorlama) {
        await dialog.bilgi({
          baslik: cevir('Değişiklik kaydedildi'),
          mesaj: cevir('Kapı zorlaması şu an kapalı — panel tek-port davranışında, bu sekmedeki mevcut adres çalışmaya devam eder. Kapı kuralları, zorlama doğrulandığında devreye girer.'),
        })
      } else if (rol === 'admin') {
        const yeni = adres(r.data.yeni_port)
        const git = await dialog.onay({
          baslik: cevir('Yeni yönetici adresi'),
          mesaj: `${cevir('Bu sekmedeki oturum sona erecek. Yeni adreste giriş yapın:')} ${yeni}`,
          onayEtiketi: cevir('Yeni adrese git'), iptalEtiketi: cevir('Bu sayfada kal'),
        })
        if (git) {
          window.location.assign(yeni)
          return
        }
      }
      onDegisti()
    } catch (e) {
      toast.hata(cevir('Port değiştirilemedi'), apiHata(e))
    } finally {
      setMesgul(false)
    }
  }

  async function uzat() {
    const g = Number(gun.trim())
    if (!Number.isInteger(g) || g < 1 || g > 365) {
      toast.hata(cevir('Geçersiz süre'), cevir('1–365 arası tam gün sayısı girin.'))
      return
    }
    setMesgul(true)
    try {
      const r = await api.post<{ uyari?: string }>('/portlar/kapi/uzat', { gun: g })
      toast.basari(cevir('Geçiş süresi uzatıldı'), `+${g} ${cevir('gün')}`)
      if (r.data.uyari) await dialog.bilgi({ baslik: cevir('Değişiklik kaydedildi ama uygulanamadı'), mesaj: r.data.uyari })
      onDegisti()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setMesgul(false)
    }
  }

  async function kapat(zorla: boolean) {
    // Kapanış yalnız yönetici doğrulamasına bağlı; eski kapı açıkken yeni kapısından hiç
    // giriş görülmeyen bayi/müşteri rolleri onay metninde BİLDİRİLİR (engellemez).
    const girissiz = kapi.eski_acik ? roller.filter((r) => r.rol !== 'admin' && !ilkGiris(r.rol)) : []
    const rolUyarisi = girissiz.length
      ? ' ' + cevirT(cevir('{0}: yeni kapıdan henüz hiç giriş görülmedi — bu kullanıcılar hâlâ eski adresi kullanıyor olabilir.'), girissiz.map((r) => r.etiket).join(', '))
      : ''
    const ok = await dialog.onay(zorla
      ? {
          baslik: cevir('Yönetici doğrulaması OLMADAN kapatılsın mı?'),
          mesaj: cevir('Yeni yönetici adresinden henüz hiç giriş yapılmadı. Sağlayıcınızın güvenlik duvarı yeni portu kesiyorsa eski adres de kapanınca panele HİÇ ulaşamazsınız (yalnız SSH ile düzeltilir). Emin değilseniz önce yeni adresten giriş yapın.') + rolUyarisi,
          onayEtiketi: cevir('Zorla kapat'), iptalEtiketi: cevir('Vazgeç'), tehlike: true,
        }
      : {
          baslik: cevir('Geçiş kapısı şimdi kapatılsın mı?'),
          mesaj: cevir('Eski adresi kullanmaya devam eden kullanıcılar panele o adresten ulaşamaz.') + rolUyarisi,
          onayEtiketi: cevir('Kapat'), iptalEtiketi: cevir('Vazgeç'), tehlike: true,
        })
    if (!ok) return
    setMesgul(true)
    try {
      const r = await api.post<{ uyari?: string }>('/portlar/kapi/eski-kapat', { zorla })
      toast.basari(cevir('Geçiş kapısı kapatılıyor'))
      if (r.data.uyari) await dialog.bilgi({ baslik: cevir('Değişiklik kaydedildi ama uygulanamadı'), mesaj: r.data.uyari })
      onDegisti()
    } catch (e) {
      toast.hata(cevir('İşlem başarısız'), apiHata(e))
    } finally {
      setMesgul(false)
    }
  }

  if (kapi.hata) {
    return (
      <section className="rounded-lg border border-red-200 bg-red-50 p-5 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
        <h2 className="text-base font-semibold">{cevir('Kapı ayarı okunamadı')}</h2>
        <p className="mt-1 font-mono text-xs">{kapi.hata}</p>
      </section>
    )
  }

  return (
    <section className="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-600 dark:bg-dark-800" aria-labelledby="panel-kapilari">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="panel-kapilari" className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir('Panel kapıları')}</h2>
        <Badge color={kapi.zorlama ? 'success' : 'warning'} variant="soft" className="px-2 py-0.5 text-[11px]">
          {kapi.zorlama ? cevir('Etkin') : cevir('Devre dışı')}
        </Badge>
      </div>
      <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
        {cevir('Yönetici, bayi ve müşteri panele ayrı portlardan girer. Yanlış kapıda giriş formu yoktur; oturum yalnız alındığı kapıda geçerlidir.')}
      </p>
      {!kapi.zorlama && (
        <p className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-2.5 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
          {cevir('Kapı kuralları şu an devre dışı: nginx zinciri doğrulanamadı, panel tek-port davranışında (kimse kilitlenmez). Ayrıntı:')}{' '}
          <code className="font-mono">journalctl -u girginospanel | grep kapi</code>
        </p>
      )}

      <ul className="mt-4 divide-y divide-slate-100 rounded-lg border border-slate-200 dark:divide-dark-600 dark:border-dark-600">
        {roller.map((r) => (
          <li key={r.rol} className="px-3 py-3">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
              <div className="w-32 shrink-0">
                <div className="text-sm font-medium text-slate-800 dark:text-slate-200">{r.etiket}</div>
                <div className="text-[11px] text-slate-500 dark:text-slate-400">{r.not}</div>
              </div>
              <span className="rounded-md bg-slate-100 px-2 py-1 font-mono text-sm tabular-nums dark:bg-dark-700">:{r.port}</span>
              <button
                type="button"
                onClick={() => void kopyala(adres(r.port))}
                title={cevir('Kopyala')}
                className="min-w-0 truncate rounded px-1 font-mono text-xs text-brand-700 underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-500 dark:text-brand-300"
              >
                {adres(r.port)}
              </button>
              <Button
                type="button" color="neutral" variant="outlined"
                className="ml-auto px-3 py-1 text-xs"
                disabled={mesgul}
                onClick={() => { setDuzenlenen(duzenlenen === r.rol ? null : r.rol); setYeniPort('') }}
                aria-expanded={duzenlenen === r.rol}
              >
                {duzenlenen === r.rol ? cevir('Vazgeç') : cevir('Değiştir')}
              </Button>
            </div>
            {r.rol !== 'admin' && (
              <p className={`mt-1.5 text-[11px] ${ilkGiris(r.rol) ? 'text-emerald-700 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400'}`}>
                {ilkGiris(r.rol) ? (
                  <>
                    {cevir('Yeni kapıdan ilk giriş:')}{' '}
                    <span className="font-medium tabular-nums">{tarihSaat(ilkGiris(r.rol))}</span>
                  </>
                ) : cevir('Yeni kapıdan henüz giriş yok')}
              </p>
            )}
            {duzenlenen === r.rol && (
              <form onSubmit={(ev) => void rolDegistir(ev, r.rol, r.etiket)} className="mt-3">
                <label htmlFor={`kapi-port-${r.rol}`} className="block text-xs font-medium text-slate-600 dark:text-slate-400">
                  {cevir('Yeni port')}
                </label>
                <div className="mt-1 flex flex-wrap items-center gap-2">
                  <input
                    id={`kapi-port-${r.rol}`} type="number" min={1024} max={65535} value={yeniPort} autoFocus
                    onChange={(e) => setYeniPort(e.target.value)}
                    placeholder={cevir('rastgele')}
                    aria-describedby={`kapi-port-${r.rol}-yardim`}
                    className="w-36 rounded-lg border border-slate-300 bg-white px-3 py-1.5 font-mono text-sm dark:border-dark-600 dark:bg-dark-900"
                  />
                  <Button type="submit" color={r.rol === 'admin' ? 'primary' : 'error'} disabled={mesgul} yukleniyor={mesgul} className="px-3 py-1.5 text-sm">
                    {cevir('Portu değiştir')}
                  </Button>
                </div>
                <p id={`kapi-port-${r.rol}-yardim`} className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                  {cevir('Boş bırakırsanız kapı aralığından rastgele bir port atanır.')}
                </p>
              </form>
            )}
          </li>
        ))}
      </ul>

      {kapi.eski_portlar.length > 0 && (
        <div className="mt-5 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-amber-900 dark:text-amber-200">
              {cevir('Geçiş kapısı (eski adres)')}{' '}
              <span className="font-mono tabular-nums">{kapi.eski_portlar.map((p) => ':' + p).join(', ')}</span>
            </h3>
            <Badge color={kapi.eski_acik ? 'warning' : 'neutral'} variant="soft" className="px-2 py-0.5 text-[11px]">
              {kapi.eski_acik ? cevir('Açık') : cevir('Kapanıyor')}
            </Badge>
          </div>
          <p className="mt-1 text-xs text-amber-800/90 dark:text-amber-300/90">
            {cevir('Geçiş süresince her rol bu adresten girebilir; girenlere kendi yeni adresleri gösterilir.')}
          </p>
          <dl className="mt-3 grid gap-3 text-xs sm:grid-cols-2">
            <div>
              <dt className="text-amber-800/80 dark:text-amber-300/80">{cevir('En erken kapanış')}</dt>
              <dd className="font-medium text-amber-950 dark:text-amber-100">{kapi.eski_bitis ? tarih(kapi.eski_bitis) : '—'}</dd>
            </div>
            <div>
              <dt className="text-amber-800/80 dark:text-amber-300/80">{cevir('Yönetici yeni kapıdan girdi')}</dt>
              <dd className="font-medium text-amber-950 dark:text-amber-100">
                {kapi.admin_dogrulandi
                  ? tarihSaat(kapi.admin_dogrulandi)
                  : cevir('Henüz girmedi — eski adres, siz yeni yönetici adresinden bir kez giriş yapana dek AÇIK kalır.')}
              </dd>
            </div>
          </dl>
          <div className="mt-4 flex flex-wrap items-end gap-2">
            <label className="flex flex-col text-xs font-medium text-amber-900 dark:text-amber-200">
              {cevir('Süreyi uzat')}
              <span className="mt-1 flex items-center gap-1.5">
                <input
                  type="number" min={1} max={365} value={gun}
                  onChange={(e) => setGun(e.target.value)}
                  className="w-20 rounded-lg border border-amber-300 bg-white px-2 py-1.5 font-mono text-sm dark:border-amber-800 dark:bg-dark-900"
                />
                {cevir('gün')}
              </span>
            </label>
            <Button type="button" color="warning" variant="soft" disabled={mesgul} onClick={() => void uzat()} className="px-3 py-1.5 text-sm">
              {cevir('Uzat')}
            </Button>
            <Button type="button" color="error" variant="soft" disabled={mesgul || !kapi.admin_dogrulandi} onClick={() => void kapat(false)} className="px-3 py-1.5 text-sm">
              {cevir('Şimdi kapat')}
            </Button>
            {!kapi.admin_dogrulandi && (
              <button
                type="button" disabled={mesgul} onClick={() => void kapat(true)}
                className="px-1 text-xs text-red-700 underline underline-offset-2 hover:text-red-800 disabled:opacity-50 dark:text-red-400"
              >
                {cevir('Doğrulamadan zorla kapat…')}
              </button>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
