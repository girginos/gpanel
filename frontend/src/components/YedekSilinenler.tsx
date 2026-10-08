import { useCallback, useEffect, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api, apiHata, sonucBelirsiz, UZUN_ISTEK } from '@/lib/api'
import { farkliSahipler, grupSahipleri, sahipAlanAdlari, sahipOzeti, yedekAdedi } from '@/lib/silinenYedek'
import type { Sahip } from '@/lib/silinenYedek'
import { useAuth } from '@/store/auth'
import { Link } from 'react-router-dom'
import { Button } from '@/components/ui'
import { I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { useDialog } from '@/components/Dialog'

// Silinmiş hesap yedekleri (çöp kutusu). Hosting ya da bayi silinince yedekleri SİLİNMEZ; burada
// ayarlanan süre boyunca saklanır (boş = hostingin yedek slotu kadar gün, en az 7; 0 = otomatik silme yok).
// Süre dolunca otomatik, ya da buradan elle kalıcı silinir. Yöneticiye özel.
//
// Dayanıklılık kuralları (doğrulama turu O9/D7/D9):
//  - Liste yüklenemezse "yedek yok" DEĞİL, hata + "Yeniden dene" gösterilir; ayar okunamazsa Kaydet kapalıdır
//    (kayıtlı ayar sessizce {gun:null} ile sıfırlanamaz).
//  - Kalıcı silme ve domaine bağlama uzun sürebilir (uzak depo): UZUN_ISTEK ile beklenir; satır kilidi
//    (ref = senkron, state = görünüm) ikinci eşzamanlı işlemi engeller. Her hata yolunda liste yenilenir.
//  - 409'da "uzak kopyayı bırak" ikinci onayı YALNIZ sunucu uzagi_yoksay === true derse sorulur.
//  - Bir sk (sistem kullanıcısı) birden çok sahibe (alan adı + bayi) ait olabilir: başlık, silme ve bağlama
//    onayları TÜM sahipleri gösterir. Saf metin yardımcıları: lib/silinenYedek.ts.

const EN: Record<string, string> = {
  'Silinmiş hesap yedekleri': 'Deleted account backups',
  'Silinmiş hesap yedekleri yüklenemedi': 'Could not load deleted account backups',
  'Aşağıda gösterilen liste güncel olmayabilir.': 'The list shown below may be out of date.',
  'Yeniden dene': 'Retry',
  'Beklenmeyen yanıt biçimi': 'Unexpected response format',
  'Hosting ya da bayi silinince yedekleri silinmez; burada saklanır ve süre dolunca otomatik temizlenir.': 'When a hosting account or reseller is deleted its backups are not deleted; they are kept here and cleaned up automatically when the period ends.',
  'Saklama süresi (gün)': 'Retention (days)',
  'Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.': 'Empty = the hosting’s backup slot (at least 7 days). 0 = never delete automatically.',
  'Değişiklik yalnız bundan sonra silinen hesaplara uygulanır.': 'The change applies only to accounts deleted from now on.',
  'Ayar yüklenemedi — kayıtlı değer bilinmiyor, bu yüzden kaydedilemez.': 'The setting could not be loaded — the stored value is unknown, so it cannot be saved.',
  'Kaydet': 'Save',
  'Kaydedildi': 'Saved',
  'Kaydedilemedi': 'Could not save',
  'Silinmiş hesap yedeği yok.': 'No deleted account backups.',
  'Silinme': 'Deleted',
  'Son saklama': 'Keep until',
  'Sahipler': 'Owners',
  'Yedek dosyalarını göster': 'Show backup files',
  'Yedek dosyalarını gizle': 'Hide backup files',
  'Yerel kopya yok': 'No local copy',
  'Domaine bağla': 'Link to domain',
  'Bu alan adının yedek sayfasında görünür ve oradan geri yüklenir.': 'Shows up on this domain’s backup page, where it can be restored.',
  '{0} bağlandı': '{0} linked',
  'Bağlanacak yedek bulunamadı': 'No backups left to link',
  'Yedek sayfasına git': 'Open backup page',
  'Bağlanamadı': 'Could not link',
  'Farklı bir müşterinin hesabına bağlanıyor': 'Linking to a different customer’s account',
  'Yedek sahibi: {0}. Hedef hosting: {1}. Onaylarsanız bu gruptaki tüm yedekler ({2}) hedef hostinge bağlanır; dosyalar ve veritabanı (kullanıcı parola özetleri dahil) başka bir müşterinin hesabına geri yüklenebilir. Devam edilsin mi?': 'Backup owner: {0}. Target hosting: {1}. If you confirm, all backups in this group ({2}) are linked to the target hosting; the files and the database (including user password hashes) could be restored into another customer’s account. Continue?',
  'Yine de bağla': 'Link anyway',
  'Bağlanacak hosting: {0} — yedeğin alan adından veya sahibinden farklı': 'Target hosting: {0} — differs from the backup’s domain or owner',
  'otomatik silinmez': 'never automatic',
  'İndir': 'Download',
  'Kalıcı sil': 'Delete permanently',
  'Emin misiniz?': 'Are you sure?',
  '"{0}" için saklanan {1} KALICI silinsin mi?': 'Permanently delete the {1} stored for "{0}"?',
  'Bu grupta birden çok hesabın yedekleri var: {0}. Toplam {1} KALICI silinsin mi?': 'This group holds backups of several accounts: {0}. Permanently delete all {1} stored?',
  'Alan adına özel ve genel FTP/SFTP depolarındaki kopyalar da silinir; Drive ve şifreli uzak kopyalar ayrı saklama kuralına tabidir. Geri alınamaz.': 'Copies in the domain-specific and global FTP/SFTP storage are removed too; Drive and encrypted remote copies follow their own retention rules. This cannot be undone.',
  'Uzak kopya silinemedi. Yine de yerel yedekleri silip uzak kopyaları BIRAKMAK ister misiniz?': 'The remote copy could not be deleted. Delete the local backups anyway and LEAVE the remote copies?',
  'Silindi': 'Deleted',
  'İşlem başarısız': 'Operation failed',
  'Sonuç belirsiz': 'Result unknown',
  'İndirme başarısız': 'Download failed',
  'yönetici': 'admin',
  'bayi #{0}': 'reseller #{0}',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)
const enMi = (): boolean => i18n.language === 'en'

type Yedek = { id: number; dosya: string; boyut_b: number; olusturma: string; yerel: boolean }
type Grup = {
  sk: string; alan_adi: string; reseller_id?: number | null; silinme: string; son_kullanma: string
  dosya_sayisi: number; boyut_b: number; yerel_var: boolean; baglanabilir_domain_id: number | null
  // Hedef hostingin alan adı; baglanabilir_farkli = alan adı VEYA sahip yedeklerinkinden farklı
  // (aynı sistem kullanıcısı aynı müşteri demek değildir: girgin.com / girgin.net).
  baglanabilir_alan_adi?: string; baglanabilir_farkli?: boolean
  // sk'daki TÜM farklı sahipler (alan adı + bayi + yedek sayısı). Eski sunucuda yok → grupSahipleri türetir.
  sahipler?: Sahip[]
  yedekler: Yedek[]
}
// bağla 409 (alan_adi_farkli): farklilar = hedeften farklı TÜM (alan adı, bayi) çiftleri; hedef/yedek_* = ilk çift + hedef.
type BaglaHata = { response?: { status?: number; data?: {
  alan_adi_farkli?: boolean; yedek_alan_adi?: string; hedef_alan_adi?: string
  yedek_reseller_id?: number; hedef_reseller_id?: number; farklilar?: Sahip[]
} } }
// silme 409: uzagi_yoksay YALNIZ başarısızlık uzak silmeden geliyorsa true (yerel/DB hatasında false).
type SilHata = { response?: { status?: number; data?: { uzagi_yoksay?: boolean } } }

function fmtB(b: number): string {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(1) + ' GB'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + ' MB'
  return Math.max(1, Math.round(b / 1024)) + ' KB'
}

// Bayi etiketi: reseller_id 0 = yönetici (kök hesap), >0 = bayi.
const bayiAdi = (rid: number): string => (rid > 0 ? cevirT(cevir('bayi #{0}'), rid) : cevir('yönetici'))

// Kalıcı silme onayı: tüm sahipler + toplam yedek sayısı; uzak kopya kapsamı DÜRÜST (yalnız FTP/SFTP depoları).
function silOnayMetni(s: Grup, sahipler: Sahip[]): string {
  const toplam = yedekAdedi(s.dosya_sayisi, enMi())
  const bas = sahipler.length > 1
    ? cevirT(cevir('Bu grupta birden çok hesabın yedekleri var: {0}. Toplam {1} KALICI silinsin mi?'),
      sahipOzeti(sahipler, bayiAdi, n => yedekAdedi(n, enMi())), toplam)
    : cevirT(cevir('"{0}" için saklanan {1} KALICI silinsin mi?'), sahipler[0].alan_adi, toplam)
  return `${bas} ${cevir('Alan adına özel ve genel FTP/SFTP depolarındaki kopyalar da silinir; Drive ve şifreli uzak kopyalar ayrı saklama kuralına tabidir. Geri alınamaz.')}`
}

export default function YedekSilinenler({ onDegisti }: { onDegisti?: () => void }) {
  useTranslation()
  const toast = useToast()
  const { onay } = useDialog()
  const en = enMi()
  const [satirlar, setSatirlar] = useState<Grup[]>([])
  // Liste durumu: yuklendi = bir yanıt (başarılı ya da hatalı) geldi; listeHata dolu = SON yükleme başarısız.
  // "Silinmiş hesap yedeği yok" yalnız başarılı + boş yanıtta çizilir; hata asla boş liste gibi görünmez.
  const [yuklendi, setYuklendi] = useState(false)
  const [listeHata, setListeHata] = useState('')
  const [listeYukleniyor, setListeYukleniyor] = useState(false)
  const [gun, setGun] = useState('')
  // Kayıtlı ayar BAŞARIYLA okunmadan Kaydet kapalı: okunamayan ayar boş sanılıp {gun:null} ile sıfırlanamasın.
  const [ayarYuklendi, setAyarYuklendi] = useState(false)
  const [ayarHata, setAyarHata] = useState('')
  const [kaydediyor, setKaydediyor] = useState(false)
  const [acik, setAcik] = useState<Record<string, boolean>>({})
  const [bagli, setBagli] = useState<Record<string, number>>({})
  // Satır kilidi: sk → süren işlem. `kilit` (ref) çift tıklamayı SENKRON engeller (state bir sonraki çizime kadar
  // eski kalır); `mesgul` (state) yalnız görünümdür (spinner + devre dışı düğmeler).
  const [mesgul, setMesgul] = useState<Record<string, 'bagla' | 'sil'>>({})
  const kilit = useRef<Set<string>>(new Set())
  // Eşzamanlı yüklemelerde yalnız en son istek sonucu yazar (eski yanıt yeniyi ezmesin).
  const listeNo = useRef(0)

  const yukleListe = useCallback(async () => {
    const no = ++listeNo.current
    setListeYukleniyor(true)
    try {
      const r = await api.get<Grup[]>('/admin/silinen-yedekler')
      if (no !== listeNo.current) return
      if (!Array.isArray(r.data)) throw new Error(cevir('Beklenmeyen yanıt biçimi'))
      setSatirlar(r.data)
      setListeHata('')
    } catch (e) {
      if (no !== listeNo.current) return
      setListeHata(apiHata(e))
    } finally {
      if (no === listeNo.current) { setListeYukleniyor(false); setYuklendi(true) }
    }
  }, [])

  const yukleAyar = useCallback(async () => {
    try {
      const r = await api.get<{ gun: number | null }>('/admin/silinen-yedekler/ayar')
      // gun yalnız null (slot) ya da sayı olabilir; başka her şey okunamamış sayılır (Kaydet açılmaz).
      const g: unknown = (r.data as { gun?: unknown } | null)?.gun
      if (g !== null && typeof g !== 'number') throw new Error(cevir('Beklenmeyen yanıt biçimi'))
      setGun(g === null ? '' : String(g))
      setAyarYuklendi(true); setAyarHata('')
    } catch (e) {
      setAyarYuklendi(false); setAyarHata(apiHata(e))
    }
  }, [])

  const yukle = useCallback(() => { void yukleListe(); void yukleAyar() }, [yukleListe, yukleAyar])
  useEffect(() => { yukle() }, [yukle])

  // İstek atılan her işlemden (başarı ya da hata) sonra liste ve üstteki özet yenilenir.
  function degistiBildir() { void yukleListe(); onDegisti?.() }

  async function ayarKaydet() {
    if (!ayarYuklendi || kaydediyor) return
    const v = gun.trim()
    if (v !== '' && !/^\d{1,4}$/.test(v)) { toast.hata(cevir('Kaydedilemedi'), cevir('Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.')); return }
    setKaydediyor(true)
    try {
      await api.put('/admin/silinen-yedekler/ayar', { gun: v === '' ? null : Number(v) })
      if (v !== '') setGun(String(Number(v)))
      toast.basari(cevir('Kaydedildi'), cevir('Değişiklik yalnız bundan sonra silinen hesaplara uygulanır.'))
    } catch (e) { toast.hata(cevir('Kaydedilemedi'), apiHata(e)) }
    finally { setKaydediyor(false) }
  }

  // Satırı kilitler; zaten kilitliyse false → ikinci eşzamanlı işlem BAŞLAMAZ.
  function kilitle(sk: string): boolean {
    if (kilit.current.has(sk)) return false
    kilit.current.add(sk)
    return true
  }
  function mesgulYaz(sk: string, tur?: 'bagla' | 'sil') {
    setMesgul(m => { const n = { ...m }; if (tur) n[sk] = tur; else delete n[sk]; return n })
  }
  function kilitAc(sk: string) { kilit.current.delete(sk); mesgulYaz(sk) }

  async function sil(s: Grup) {
    if (!kilitle(s.sk)) return
    let istekYapildi = false
    try {
      if (!(await onay({ baslik: cevir('Emin misiniz?'), mesaj: silOnayMetni(s, grupSahipleri(s)), ton: 'tehlike', onayIkonu: I.cop }))) return
      let uzagiYoksay = false
      for (;;) {
        mesgulYaz(s.sk, 'sil')
        istekYapildi = true
        try {
          const { data } = await api.delete<{ silinen?: number }>(`/admin/silinen-yedekler/${encodeURIComponent(s.sk)}${uzagiYoksay ? '?uzagi_yoksay=1' : ''}`, UZUN_ISTEK)
          toast.basari(cevir('Silindi'), typeof data?.silinen === 'number' ? yedekAdedi(data.silinen, enMi()) : undefined)
          return
        } catch (e) {
          const x = e as SilHata
          // İkinci onay YALNIZ sunucu "uzak silme başarısız" dediyse; yerel/DB hatasında uzağı bırakmak çözüm değildir.
          if (!uzagiYoksay && x.response?.status === 409 && x.response.data?.uzagi_yoksay === true) {
            toast.hata(cevir('İşlem başarısız'), apiHata(e))
            mesgulYaz(s.sk)
            if (await onay({ baslik: cevir('Emin misiniz?'), mesaj: cevir('Uzak kopya silinemedi. Yine de yerel yedekleri silip uzak kopyaları BIRAKMAK ister misiniz?'), ton: 'tehlike', onayIkonu: I.cop })) { uzagiYoksay = true; continue }
            return
          }
          if (sonucBelirsiz(e)) toast.uyari(cevir('Sonuç belirsiz'), apiHata(e))
          else toast.hata(cevir('İşlem başarısız'), apiHata(e))
          return
        }
      }
    } finally {
      kilitAc(s.sk)
      if (istekYapildi) degistiBildir()
    }
  }

  // Farklı alan adı/sahip: TÜM farklı (alan adı, bayi) çiftlerini + hedef hostingi gösteren tehlike tonlu AÇIK onay
  // (iptal = false). Onay `zorla` ile gruptaki HEPSİNİ bağlar; bu yüzden toplam yedek sayısı da söylenir.
  function farkliOnayi(yedekSahipleri: Sahip[], hedefAlan: string, hedefBayi: number | null | undefined, adet: number): Promise<boolean> {
    const hedef = hedefBayi == null ? hedefAlan : `${hedefAlan} (${bayiAdi(hedefBayi)})`
    return onay({
      baslik: cevir('Farklı bir müşterinin hesabına bağlanıyor'),
      mesaj: cevirT(cevir('Yedek sahibi: {0}. Hedef hosting: {1}. Onaylarsanız bu gruptaki tüm yedekler ({2}) hedef hostinge bağlanır; dosyalar ve veritabanı (kullanıcı parola özetleri dahil) başka bir müşterinin hesabına geri yüklenebilir. Devam edilsin mi?'),
        sahipOzeti(yedekSahipleri, bayiAdi), hedef, yedekAdedi(adet, enMi())),
      onayEtiketi: cevir('Yine de bağla'), ton: 'tehlike', onayIkonu: I.link,
    })
  }

  async function bagla(s: Grup) {
    const hedefID = s.baglanabilir_domain_id
    if (hedefID == null) return
    if (!kilitle(s.sk)) return
    let istekYapildi = false
    try {
      const sahipler = grupSahipleri(s)
      let zorla = false
      // Liste "hedef farklı" diyorsa istek atmadan önce açık onay: grubun TÜM sahipleri gösterilir.
      if (s.baglanabilir_farkli) {
        if (!(await farkliOnayi(sahipler, s.baglanabilir_alan_adi || '', undefined, s.dosya_sayisi))) return
        zorla = true
      }
      for (;;) {
        mesgulYaz(s.sk, 'bagla')
        istekYapildi = true
        try {
          const { data } = await api.post<{ ok: boolean; baglanan: number }>(`/admin/silinen-yedekler/${encodeURIComponent(s.sk)}/bagla`,
            zorla ? { domain_id: hedefID, zorla: true } : { domain_id: hedefID }, UZUN_ISTEK)
          const n = Number(data?.baglanan ?? 0)
          if (n > 0) {
            toast.basari(cevirT(cevir('{0} bağlandı'), yedekAdedi(n, enMi())))
            setBagli(b => ({ ...b, [s.sk]: hedefID }))
          } else {
            toast.uyari(cevir('Bağlanacak yedek bulunamadı'))
          }
          return
        } catch (e) {
          // Liste eskiydi / sahip sunucuda değişti: sunucu 409 alan_adi_farkli döner → aynı açık onayı (sunucunun
          // `farklilar[]` listesiyle: TÜM farklı çiftler) göster.
          const x = e as BaglaHata
          if (!zorla && x.response?.status === 409 && x.response.data?.alan_adi_farkli) {
            const d = x.response.data
            mesgulYaz(s.sk)
            if (await farkliOnayi(farkliSahipler(d, sahipler), d.hedef_alan_adi || s.baglanabilir_alan_adi || '', d.hedef_reseller_id, s.dosya_sayisi)) { zorla = true; continue }
            return
          }
          if (sonucBelirsiz(e)) toast.uyari(cevir('Sonuç belirsiz'), apiHata(e))
          else toast.hata(cevir('Bağlanamadı'), apiHata(e))
          return
        }
      }
    } finally {
      kilitAc(s.sk)
      if (istekYapildi) degistiBildir()
    }
  }

  function indir(y: Yedek) {
    const tok = useAuth.getState().token || ''
    fetch(`/api/v1/admin/silinen-yedekler/yedek/${y.id}/indir`, { headers: { Authorization: `Bearer ${tok}` } })
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.blob() })
      .then(blob => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob); a.download = y.dosya; a.click()
        setTimeout(() => URL.revokeObjectURL(a.href), 1000)
      })
      .catch(e => toast.hata(cevir('İndirme başarısız'), e instanceof Error ? e.message : String(e)))
  }

  return (
    <div className="mt-4 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60">
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir('Silinmiş hesap yedekleri')}</h3>
        <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{cevir('Hosting ya da bayi silinince yedekleri silinmez; burada saklanır ve süre dolunca otomatik temizlenir.')}</p>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <label className="text-xs text-slate-600 dark:text-slate-300">
            {cevir('Saklama süresi (gün)')}
            <input value={gun} onChange={e => setGun(e.target.value)} inputMode="numeric" placeholder="slot" disabled={!ayarYuklendi}
              className="block mt-1 w-28 px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded-md text-sm bg-white dark:bg-dark-800 focus:border-brand-500 outline-none disabled:opacity-60 disabled:cursor-not-allowed" />
          </label>
          <Button color="primary" boyut="sm" onClick={ayarKaydet} yukleniyor={kaydediyor} disabled={!ayarYuklendi}>{cevir('Kaydet')}</Button>
          <span className="text-[11px] text-slate-500 dark:text-slate-400 basis-full sm:basis-auto">
            {cevir('Boş = hostingin yedek slotu kadar (en az 7 gün). 0 = otomatik silme yok.')} {cevir('Değişiklik yalnız bundan sonra silinen hesaplara uygulanır.')}
          </span>
        </div>
        {ayarHata && (
          <UyariKutusu ton="uyari" kucuk className="mt-2">
            <span className="break-words">{cevir('Ayar yüklenemedi — kayıtlı değer bilinmiyor, bu yüzden kaydedilemez.')} {ayarHata}</span>
            <Button boyut="sm" variant="outlined" ikon={I.yenile} onClick={() => void yukleAyar()} className="ms-2">{cevir('Yeniden dene')}</Button>
          </UyariKutusu>
        )}
      </div>
      <div className="divide-y divide-slate-100 dark:divide-dark-600/60">
        {!yuklendi && <div className="px-4 py-6 text-center text-sm text-slate-500 dark:text-slate-400">{cevir('Yükleniyor…')}</div>}
        {listeHata && (
          <div className="px-4 py-3">
            <UyariKutusu ton="tehlike" baslik={cevir('Silinmiş hesap yedekleri yüklenemedi')} kucuk>
              <span className="break-words">{listeHata}{satirlar.length > 0 ? ` ${cevir('Aşağıda gösterilen liste güncel olmayabilir.')}` : ''}</span>
              <div className="mt-2">
                <Button boyut="sm" variant="outlined" ikon={I.yenile} yukleniyor={listeYukleniyor} onClick={() => void yukleListe()}>{cevir('Yeniden dene')}</Button>
              </div>
            </UyariKutusu>
          </div>
        )}
        {yuklendi && !listeHata && satirlar.length === 0 && <div className="px-4 py-6 text-center text-sm text-slate-500 dark:text-slate-400">{cevir('Silinmiş hesap yedeği yok.')}</div>}
        {satirlar.map(s => {
          const ac = !!acik[s.sk]
          const bagliId = bagli[s.sk]
          const mes = mesgul[s.sk]
          const sahipler = grupSahipleri(s)
          const adlar = sahipAlanAdlari(sahipler)
          return (
            <div key={s.sk} className="px-4 py-2.5 text-sm" aria-busy={mes ? true : undefined}>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                {/* Erişilebilir ad = görünen ad (alan adları); aria-label onu EZMEZ. Eylem ipucu title'da, durum aria-expanded'da. */}
                <button type="button" onClick={() => setAcik(a => ({ ...a, [s.sk]: !ac }))} aria-expanded={ac}
                  title={cevir(ac ? 'Yedek dosyalarını gizle' : 'Yedek dosyalarını göster')}
                  className="inline-flex min-w-0 items-center gap-1 font-medium text-slate-800 dark:text-slate-200 break-all text-left">
                  <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0 fill-none stroke-current" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={ac ? 'm6 9 6 6 6-6' : 'm9 18 6-6-6-6'} /></svg>
                  {adlar}
                </button>
                <span className="font-mono text-xs text-slate-500 dark:text-slate-400">{cevir('Silinme')}: {s.silinme}</span>
                <span className="font-mono text-xs text-slate-500 dark:text-slate-400">{cevir('Son saklama')}: {s.son_kullanma || cevir('otomatik silinmez')}</span>
                <span className="font-mono text-xs text-slate-500 dark:text-slate-400">{yedekAdedi(s.dosya_sayisi, en)} · {fmtB(s.boyut_b)}</span>
                {s.baglanabilir_domain_id != null && s.baglanabilir_farkli && (
                  <span className="min-w-0 break-words text-xs text-amber-700 dark:text-amber-400">{cevirT(cevir('Bağlanacak hosting: {0} — yedeğin alan adından veya sahibinden farklı'), s.baglanabilir_alan_adi || '')}</span>
                )}
                <span className="ml-auto flex flex-wrap items-center gap-2">
                  {bagliId != null && (
                    <Link to={`/abonelikler/${bagliId}/yedekler`} className="text-xs text-brand-600 dark:text-brand-400 hover:underline">{cevir('Yedek sayfasına git')}</Link>
                  )}
                  {s.baglanabilir_domain_id != null && (
                    <Button type="button" variant="outlined" boyut="sm" yukleniyor={mes === 'bagla'} disabled={!!mes} onClick={() => bagla(s)}
                      title={cevir('Bu alan adının yedek sayfasında görünür ve oradan geri yüklenir.')} aria-label={`${cevir('Domaine bağla')}: ${adlar}`}>{cevir('Domaine bağla')}</Button>
                  )}
                  <Button type="button" color="error" variant="soft" boyut="sm" ikon={I.cop} yukleniyor={mes === 'sil'} disabled={!!mes} onClick={() => sil(s)}
                    title={cevir('Kalıcı sil')} aria-label={`${cevir('Kalıcı sil')}: ${adlar}`}>{cevir('Kalıcı sil')}</Button>
                </span>
              </div>
              {ac && (
                <div className="mt-2">
                  {sahipler.length > 1 && (
                    <p className="text-xs text-slate-500 dark:text-slate-400 break-words">
                      <span className="font-medium">{cevir('Sahipler')}:</span> {sahipOzeti(sahipler, bayiAdi, n => yedekAdedi(n, en))}
                    </p>
                  )}
                  <ul className={`${sahipler.length > 1 ? 'mt-2 ' : ''}space-y-1`}>
                    {s.yedekler.map(y => (
                      <li key={y.id} className={`flex flex-wrap items-center gap-x-3 gap-y-1 text-xs ${y.yerel ? '' : 'opacity-50'}`}>
                        <span className="font-mono text-slate-600 dark:text-slate-300 break-all min-w-0">{y.dosya}</span>
                        <span className="font-mono text-slate-500 dark:text-slate-400">{y.olusturma}</span>
                        <span className="font-mono text-slate-500 dark:text-slate-400">{fmtB(y.boyut_b)}</span>
                        <Button type="button" variant="outlined" boyut="sm" className="ml-auto" disabled={!y.yerel || !!mes} onClick={() => indir(y)}
                          title={y.yerel ? y.dosya : cevir('Yerel kopya yok')} aria-label={`${cevir('İndir')}: ${y.dosya}`}>{cevir('İndir')}</Button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
