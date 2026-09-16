import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'

type Kurulum = { dizin: string; site_url: string; admin_url: string; surum: string; admin_kullanici?: string; parola_var?: boolean }
type Sonuc = { site_url: string; admin_url: string; admin_kullanici: string; admin_parola?: string; surum: string }
type Durum = { surum: string; guncelleme_var: boolean; hedef_surum: string; php: string; db_mb: string; bakim: boolean }
type Paket = { name: string; status: string; version: string; update: string; update_version: string }
type Kullanici = { ID: number; user_login: string; user_email: string; display_name: string; roles: string }


const WP_EN: Record<string, string> = {
  "Admin kullanıcı": "Admin user",
  "örn. site-yoneticisi": "e.g. site-admin",
  "Alt dizin (isteğe bağlı)": "Subdirectory (optional)",
  "Aşağıdaki formdan tek tıkla kurabilirsiniz.": "You can install with one click from the form below.",
  "Bakım modu": "Maintenance mode",
  "Bakım modu açıldı.": "Maintenance mode enabled.",
  "Bakım modu kapatıldı.": "Maintenance mode disabled.",
  "Bakım moduna al": "Enable maintenance mode",
  "Bakım modunu kapat": "Disable maintenance mode",
  "Bu domainde henüz WordPress yok": "No WordPress on this domain yet",
  "Devre dışı": "Disabled",
  "Dosyalar silindi, veritabanı KALDI": "Files deleted, the database REMAINS",
  "Hızlı bakım işlemleri. Sürüm güncellemesi varsa üstteki metrikte görünür.": "Quick maintenance operations. If a version update is available it shows in the metric above.",
  "Kullanıcı bulunamadı.": "User not found.",
  "Kullanıcılar": "Users",
  "Kurulum başarısız": "Installation failed",
  "İşlem başarısız": "Operation failed",
  "Parola sıfırla": "Reset password",
  "Parola sıfırlanamadı": "Failed to reset password",
  "Parolayı şimdi kaydedin — tekrar gösterilmez.": "Save the password now — it won't be shown again.",
  "Site başlığı": "Site title",
  "Sürüm, eklenti, tema ve kullanıcıları tek yerden yönetin.": "Manage version, plugins, themes and users in one place.",
  "Tümü güncellendi.": "All updated.",
  "Tümünü güncelle": "Update all",
  "WordPress çekirdeği güncellendi.": "WordPress core updated.",
  "Yönetim": "Management",
  "boş = kök · örn: blog": "empty = root · e.g: blog",
  "devre dışı bırakıldı": "disabled",
  "etkinleştirildi": "enabled",
  "güncel": "up to date",
  "kök": "root",
  "veritabanı silinir": "the database is deleted",
  "Çekirdek onarımı tamamlandı.": "Core repair completed.",
  "Çekirdek, eklenti ve temalar güncellendi.": "Core, plugins and themes updated.",
  "Çekirdeği onar": "Repair core",
  "Önbellek temizlendi.": "Cache cleared.",
  "Önbelleği temizle": "Clear cache",
  "İşlem çıktısı": "Operation output",
  "İşleniyor…": "Processing…",
  "Türkçe": "English",
  "Anasayfa": "Home",
  "Abonelik": "Subscription",
  "Yeni WordPress": "New WordPress",
  "Eklentiler": "Plugins",
  "Temalar": "Themes",
  "Onay gerekiyor": "Confirmation required",
  "Kök dizindeki WordPress kaldırılsın mı?\nWordPress dosyaları (wp-admin, wp-includes, wp-content, wp-*.php) ve veritabanı silinir.\nDizinin kendisi ve sizin eklediğiniz diğer dosyalar korunur. Geri alınamaz.": "Remove WordPress in the root directory?\nThe WordPress files (wp-admin, wp-includes, wp-content, wp-*.php) and the database are deleted.\nThe directory itself and other files you added are preserved. This cannot be undone.",
  "{0} altındaki WordPress silinsin mi?\nBu dizindeki tüm dosyalar ve veritabanı kaldırılır. Geri alınamaz.": "Delete WordPress under {0}?\nAll files in this directory and the database are removed. This cannot be undone.",
  "Emin misiniz?": "Are you sure?",
  "\n\nVeritabanını Veritabanları sayfasından elle kaldırabilirsiniz.": "\n\nYou can remove the database manually from the Databases page.",
  "Silinemedi": "Could not be deleted",
  "Açık": "On",
  "aktif": "active",
  "Sürümü güncelle": "Update version",
  "Eklenti": "Plugin",
  "Tema": "Theme",
  "bulunamadı.": "not found.",
  "güncelleme mevcut": "update available",
  "Etkin": "Enabled",
  "Sürüm": "Version",
  "mevcut": "available",
  "Devre dışı bırak": "Disable",
  "Aktif tema": "Active theme",
  "Yeni WordPress kurulumu": "New WordPress installation",
  "Benim Blogum": "My Blog",
  "Admin e-posta": "Admin email",
  "Kuruluyor… (~30 sn)": "Installing… (~30 s)",
  "WordPress kur": "Install WordPress",
  "kuruldu": "installed",
  "Yeni parola": "New password",
  "{0} için yeni parola": "New password for {0}",
  "Yeni parolayı yazın (en az 8 karakter). Mevcut parola geçersiz olacak.": "Type the new password (at least 8 characters). The current password will become invalid.",
  "Yeni parola gerekli.": "New password is required.",
  "{0} parolası güncellendi.": "Password for {0} updated.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainWordPressPage() {
  useTranslation() // dil re-render aboneligi
  const { onay, bilgi } = useDialog()
  const toast = useToast()
  const { id, sid } = useParams()
  const base = sid ? `/domains/${id}/subdomain/${sid}/wordpress` : `/domains/${id}/wordpress`
  const [liste, setListe] = useState<Kurulum[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata artik sag ust toast'ta gosterilir; state mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [kuruyor, setKuruyor] = useState(false)
  const [sonuc, setSonuc] = useState<Sonuc | null>(null)
  const [formAcik, setFormAcik] = useState(false)

  const [alanAdi, setAlanAdi] = useState('')
  const [altDizin, setAltDizin] = useState('')
  const [baslik, setBaslik] = useState('')
  // 'admin' brute-force'un bir numarali hedefi — varsayilan BOS, alan zorunlu.
  const [adminK, setAdminK] = useState('')
  const [adminE, setAdminE] = useState('')

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<{ alan_adi: string }>(`/domains/${id}`).then(r => { if (iptal) return; setAlanAdi(r.data.alan_adi || '') }).catch(e => { if (!iptal) hataYakala(cevir("Alan adı bilgisi alınamadı"))(e) })
    return () => { iptal = true }
  }, [id])

  const listeleNesli = useRef(0)
  const listele = useCallback(() => {
    if (!id) return
    setYuk(true)
    const _n = ++listeleNesli.current
    api.get<Kurulum[]>(`${base}`).then(r => { if (_n !== listeleNesli.current) return; setListe(r.data || []) }).catch(() => { if (_n !== listeleNesli.current) return; setListe([]) }).finally(() => { if (_n !== listeleNesli.current) return; setYuk(false) })
  }, [id])
  useEffect(() => { listele(); return () => { listeleNesli.current++ } }, [listele])

  async function kur(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setSonuc(null); setKuruyor(true)
    try {
      const { data } = await api.post<Sonuc>(`${base}`, {
        alt_dizin: altDizin.trim(), site_basligi: baslik.trim(), admin_kullanici: adminK.trim(), admin_email: adminE.trim(),
      })
      setSonuc(data); setBaslik(''); setAltDizin(''); setFormAcik(false)
      listele()
    } catch (err) {
      const m = apiHata(err, cevir("Kurulum başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setKuruyor(false) }
  }

  const bosDurum = !yuk && liste.length === 0

  return (
    <div className="px-6 py-6 max-w-5xl">
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: alanAdi || cevir('Abonelik'), href: `/abonelikler/${id}` },
        { etiket: 'WordPress' },
      ]} />
      <div className="flex items-center justify-between gap-4 mb-6 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 tracking-tight">WordPress Toolkit</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">{cevir("Sürüm, eklenti, tema ve kullanıcıları tek yerden yönetin.")}</p>
        </div>
        {!bosDurum && !formAcik && (
          <button onClick={() => { setFormAcik(true); setSonuc(null) }}
            className="w-full sm:w-auto inline-flex items-center justify-center gap-1.5 px-4 py-2.5 rounded-full bg-dark-800 dark:bg-dark-600 text-white dark:text-slate-100 text-sm font-medium hover:bg-dark-700 dark:hover:bg-slate-600 transition">
            <span className="text-base leading-none">+</span> {cevir("Yeni WordPress")}
          </button>
        )}
      </div>

      {sonuc && <KurulumSonuc s={sonuc} kapat={() => setSonuc(null)} />}

      {yuk ? (
        <div className="rounded-lg border border-slate-200/70 dark:border-dark-600/60 bg-white dark:bg-dark-700/40 p-10 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : bosDurum ? (
        <div className="rounded-lg border border-dashed border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700/40 p-12 text-center mb-5">
          <div className="w-12 h-12 mx-auto rounded-lg bg-slate-100 dark:bg-dark-600/50 flex items-center justify-center mb-3"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6 text-slate-400 dark:text-slate-500"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4 12.5-12.5z"/></svg></div>
          <p className="text-base font-medium text-slate-800 dark:text-slate-100">{cevir("Bu domainde henüz WordPress yok")}</p>
          <p className="text-sm text-slate-400 mt-1">{cevir("Aşağıdaki formdan tek tıkla kurabilirsiniz.")}</p>
        </div>
      ) : (
        <div className="space-y-5">
          {liste.map(k => <Toolkit key={k.dizin} id={id!} base={base} kurulum={k} onDegisti={listele} />)}
        </div>
      )}

      {(bosDurum || formAcik) && (
        <div className="mt-5">
          <KurulumFormu baslik={baslik} setBaslik={setBaslik} altDizin={altDizin} setAltDizin={setAltDizin}
            adminK={adminK} setAdminK={setAdminK} adminE={adminE} setAdminE={setAdminE}
            kur={kur} kuruyor={kuruyor} kapat={bosDurum ? undefined : () => setFormAcik(false)} />
        </div>
      )}

      <div className="mt-6"><Link to={`/abonelikler/${id}`} className="text-sm text-slate-500 hover:text-slate-800 dark:hover:text-slate-200 transition">{cevir("← Aboneliğe dön")}</Link></div>
    </div>
  )
}

// ================= Toolkit: tek kurulum kartı =================

type AltTab = 'genel' | 'eklentiler' | 'temalar' | 'kullanicilar'
const TABLAR: { k: AltTab; ad: string }[] = [
  { k: 'genel', ad: cevir("Genel") }, { k: 'eklentiler', ad: cevir("Eklentiler") },
  { k: 'temalar', ad: cevir("Temalar") }, { k: 'kullanicilar', ad: cevir("Kullanıcılar") },
]

function Toolkit({ id, base, kurulum, onDegisti }: { id: string; base: string; kurulum: Kurulum; onDegisti: () => void }) {
  const { onay, sor, bilgi } = useDialog()
  const toast = useToast()
  const dizin = kurulum.dizin
  const kok = dizin.includes(cevir("kök"))
  const [tab, setTab] = useState<AltTab>('genel')
  const [durum, setDurum] = useState<Durum | null>(null)
  const [eklentiler, setEklentiler] = useState<Paket[] | null>(null)
  const [temalar, setTemalar] = useState<Paket[] | null>(null)
  const [kullanicilar, setKullanicilar] = useState<Kullanici[] | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [cikti, setCikti] = useState<string | null>(null)

  const qp = { params: { dizin } }

  const durumYukleNesli = useRef(0)
  const durumYukle = useCallback(() => {
    const _n = ++durumYukleNesli.current
    api.get<Durum>(`${base}/durum`, qp).then(r => { if (_n !== durumYukleNesli.current) return; setDurum(r.data) }).catch(() => { if (_n !== durumYukleNesli.current) return; setDurum(null) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, dizin])
  useEffect(() => { durumYukle(); return () => { durumYukleNesli.current++ } }, [durumYukle])

  useEffect(() => {
    let iptal = false
    if (tab === 'eklentiler' && eklentiler === null) api.get<Paket[]>(`${base}/eklentiler`, qp).then(r => { if (iptal) return; setEklentiler(r.data || []) }).catch(() => { if (iptal) return; setEklentiler([]) })
    if (tab === 'temalar' && temalar === null) api.get<Paket[]>(`${base}/temalar`, qp).then(r => { if (iptal) return; setTemalar(r.data || []) }).catch(() => { if (iptal) return; setTemalar([]) })
    if (tab === 'kullanicilar' && kullanicilar === null) api.get<Kullanici[]>(`${base}/kullanicilar`, qp).then(r => { if (iptal) return; setKullanicilar(r.data || []) }).catch(() => { if (iptal) return; setKullanicilar([]) })
    return () => { iptal = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

  async function calistir(anahtar: string, istek: () => Promise<{ cikti?: string }>, basariMsj: string, sonra?: () => void) {
    setMesgul(anahtar); setHata(null); setBasari(null); setCikti(null)
    try {
      const data = await istek()
      setBasari(basariMsj)
      toast.basari(basariMsj)
      if (data?.cikti) setCikti(data.cikti)
      sonra?.()
    } catch (err) {
      const m = apiHata(err, cevir("İşlem başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  const surumGuncelle = () => calistir('surum', async () => (await api.post(`${base}/guncelle`, { dizin })).data, cevir("WordPress çekirdeği güncellendi."), () => { durumYukle(); onDegisti() })
  const tumunuGuncelle = () => calistir('tumu', async () => (await api.post(`${base}/arac`, { dizin, islem: 'tumunu-guncelle' })).data, cevir("Çekirdek, eklenti ve temalar güncellendi."), () => { durumYukle(); setEklentiler(null); setTemalar(null); onDegisti() })
  const bakimTogle = () => calistir('bakim', async () => (await api.post(`${base}/arac`, { dizin, islem: durum?.bakim ? 'bakim-kapat' : 'bakim-ac' })).data, durum?.bakim ? cevir("Bakım modu kapatıldı.") : cevir("Bakım modu açıldı."), durumYukle)
  const cacheTemizle = () => calistir('cache', async () => (await api.post(`${base}/arac`, { dizin, islem: 'cache-temizle' })).data, cevir("Önbellek temizlendi."))
  const onar = () => calistir('onar', async () => (await api.post(`${base}/onar`, { dizin })).data, cevir("Çekirdek onarımı tamamlandı."), durumYukle)

  const paketGuncelle = (tur: 'eklenti' | 'tema', ad: string) => calistir(`${tur}:${ad}`, async () => (await api.post(`${base}/${tur}`, { dizin, islem: 'guncelle', ad })).data, cevirT(cevir("{0} güncellendi."), ad), () => { tur === 'eklenti' ? setEklentiler(null) : setTemalar(null) })
  const paketTumu = (tur: 'eklenti' | 'tema') => calistir(`${tur}:tum`, async () => (await api.post(`${base}/${tur}`, { dizin, islem: 'tumunu-guncelle' })).data, cevir("Tümü güncellendi."), () => { tur === 'eklenti' ? setEklentiler(null) : setTemalar(null) })
  const eklentiTogle = (p: Paket) => calistir(`ekl:${p.name}`, async () => (await api.post(`${base}/eklenti`, { dizin, islem: p.status === 'active' ? 'pasif' : 'aktif', ad: p.name })).data, `${p.name} ${p.status === 'active' ? cevir("devre dışı bırakıldı") : cevir("etkinleştirildi")}.`, () => setEklentiler(null))
  const temaAktif = (p: Paket) => calistir(`tema:${p.name}`, async () => (await api.post(`${base}/tema`, { dizin, islem: 'aktif', ad: p.name })).data, cevirT(cevir("{0} etkinleştirildi."), p.name), () => setTemalar(null))

  async function parolaSifirla(u: Kullanici) {
    // WRITE-ONLY: kullanıcı yeni parolayı KENDİ yazar; sunucu yanıtta parola DÖNMEZ.
    const girilen = await sor({
      baslik: cevirT(cevir("{0} için yeni parola"), u.user_login),
      mesaj: cevir("Yeni parolayı yazın (en az 8 karakter). Mevcut parola geçersiz olacak."),
      tur: 'password',
      yerTutucu: cevir("Yeni parola"),
      onayEtiketi: cevir("Parola sıfırla"),
    })
    if (girilen === null) return
    const parola = girilen.trim()
    if (!parola) { toast.hata(cevir("İşlem başarısız"), cevir("Yeni parola gerekli.")); return }
    setMesgul(`pw:${u.ID}`); setHata(null); setBasari(null)
    try {
      await api.post<{ ok: boolean; kullanici: string }>(`${base}/kullanici-parola`, { dizin, user_id: u.ID, parola })
      toast.basari(cevirT(cevir("{0} parolası güncellendi."), u.user_login))
    } catch (err) {
      const m = apiHata(err, cevir("Parola sıfırlanamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  async function sil() {
    const msj = kok
      ? cevir("Kök dizindeki WordPress kaldırılsın mı?\nWordPress dosyaları (wp-admin, wp-includes, wp-content, wp-*.php) ve veritabanı silinir.\nDizinin kendisi ve sizin eklediğiniz diğer dosyalar korunur. Geri alınamaz.")
      : cevirT(cevir("{0} altındaki WordPress silinsin mi?\nBu dizindeki tüm dosyalar ve veritabanı kaldırılır. Geri alınamaz."), dizin)
    if (!(await onay({ baslik: cevir('Emin misiniz?'), mesaj: msj, tehlike: true }))) return
    setMesgul('sil'); setHata(null)
    try {
      // 🔴 Yanıt ATILMAMALI. Dosyalar silinse bile veritabanı
      // düşmemiş olabilir (yetki hatası, sahiplik reddi, wp-config
      // okunamadı). Sunucu bunu `db_uyari` ile bildiriyor ama eskiden
      // yanıt tamamen atılıyordu: HTTP 200 + ok:true olduğu için
      // `catch` de tetiklenmiyor ve kullanıcı düz BAŞARI görüyordu —
      // üstelik onay diyaloğu ona cevir("veritabanı silinir") demişti.
      const r = await api.delete<{ ok: boolean; db_uyari?: string }>(
        `${base}`, { data: { dizin, db_sil: true } })
      if (r?.data?.db_uyari) {
        await bilgi({
          baslik: cevir("Dosyalar silindi, veritabanı KALDI"),
          mesaj: r.data.db_uyari + cevir('\n\nVeritabanını Veritabanları sayfasından elle kaldırabilirsiniz.'),
        })
      }
      onDegisti()
    }
    catch (err) {
      const m = apiHata(err, cevir('Silinemedi'))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  const eklGuncel = (eklentiler || []).filter(p => p.update === 'available').length
  const temaGuncel = (temalar || []).filter(p => p.update === 'available').length
  const rozet: Record<string, number> = { eklentiler: eklGuncel, temalar: temaGuncel }

  return (
    <div className="rounded-lg border border-slate-200/70 dark:border-dark-600/60 bg-white dark:bg-dark-700/40 overflow-hidden">
      {/* başlık şeridi */}
      <div className="flex items-center justify-between gap-3 px-5 pt-5 pb-4 flex-wrap">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="w-9 h-9 rounded-lg bg-slate-100 dark:bg-dark-600/50 flex items-center justify-center shrink-0"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5 text-slate-500 dark:text-slate-300"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4 12.5-12.5z"/></svg></div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">WordPress <span className="text-slate-400 font-normal font-mono text-xs">· {dizin}</span></div>
            <div className="text-xs text-slate-400 mt-0.5 truncate">{kurulum.site_url}</div>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {kurulum.admin_url && (
            <a href={kurulum.admin_url} target="_blank" rel="noreferrer"
              className="inline-flex items-center gap-1 px-3.5 py-2 rounded-full bg-dark-800 dark:bg-dark-600 text-white dark:text-slate-100 text-xs font-medium hover:bg-dark-700 dark:hover:bg-slate-600 transition">
              {cevir(cevir("Yönetim paneli"))} <span className="opacity-70">↗</span>
            </a>
          )}
          {<button disabled={!!mesgul} onClick={sil} className="px-3 py-2 rounded-full border border-slate-200 dark:border-dark-600 text-xs text-slate-500 hover:border-red-300 hover:text-red-600 dark:hover:border-red-800 dark:hover:text-red-400 disabled:opacity-50 transition">{mesgul === 'sil' ? '…' : cevir("Kaldır")}</button>}
        </div>
      </div>

      {/* metrikler */}
      <div className="px-5 grid grid-cols-2 lg:grid-cols-4 gap-3">
        <Metrik label={cevir("Sürüm")} v={durum?.surum ? durum.surum : '…'}
          pill={durum ? (durum.guncelleme_var ? { t: `↑ ${durum.hedef_surum}`, c: 'amber' } : { t: cevir("güncel"), c: 'green' }) : undefined} />
        <Metrik label="PHP" v={durum?.php || '…'} />
        <Metrik label={cevir("Veritabanı")} v={durum ? `${durum.db_mb} MB` : '…'} />
        <Metrik label={cevir("Bakım modu")} v={durum?.bakim ? cevir('Açık') : cevir("Kapalı")}
          pill={durum?.bakim ? { t: cevir('aktif'), c: 'amber' } : undefined} />
      </div>

      {/* segment sekmeler */}
      <div className="px-5 pt-5">
        <div className="inline-flex items-center gap-1 p-1 rounded-full bg-slate-100 dark:bg-dark-800/50">
          {TABLAR.map(t => (
            <button key={t.k} onClick={() => setTab(t.k)}
              className={`px-3.5 py-1.5 rounded-full text-sm font-medium transition ${tab === t.k
                ? 'bg-white dark:bg-dark-600 text-slate-900 dark:text-slate-100 shadow-xs'
                : 'text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'}`}>
              {t.ad}
              {!!rozet[t.k] && rozet[t.k] > 0 && <span className="ml-1.5 text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300 font-semibold align-middle">{rozet[t.k]}</span>}
            </button>
          ))}
        </div>
      </div>

      <div className="p-5">
        {tab === 'genel' && (
          <div>
            <div className="flex flex-wrap gap-2">
              {durum?.guncelleme_var && <Btn onClick={surumGuncelle} bekle={mesgul === 'surum'} tur="primary">{cevir("Sürümü güncelle")} · v{durum.hedef_surum}</Btn>}
              <Btn onClick={tumunuGuncelle} bekle={mesgul === 'tumu'} tur={durum?.guncelleme_var ? 'outline' : 'primary'}>{cevir("Tümünü güncelle")}</Btn>
              <Btn onClick={bakimTogle} bekle={mesgul === 'bakim'}>{durum?.bakim ? cevir('Bakım modunu kapat') : cevir("Bakım moduna al")}</Btn>
              <Btn onClick={cacheTemizle} bekle={mesgul === 'cache'}>{cevir("Önbelleği temizle")}</Btn>
              <Btn onClick={onar} bekle={mesgul === 'onar'}>{cevir("Çekirdeği onar")}</Btn>
            </div>
            {cikti && <Cikti metin={cikti} />}
            {!cikti && <p className="text-xs text-slate-400 mt-4">{cevir("Hızlı bakım işlemleri. Sürüm güncellemesi varsa üstteki metrikte görünür.")}</p>}
          </div>
        )}

        {tab === 'eklentiler' && (
          <PaketTablo tur="eklenti" liste={eklentiler} mesgul={mesgul}
            onTumu={() => paketTumu('eklenti')} onGuncelle={(p) => paketGuncelle('eklenti', p.name)} onTogle={eklentiTogle} />
        )}
        {tab === 'temalar' && (
          <PaketTablo tur="tema" liste={temalar} mesgul={mesgul}
            onTumu={() => paketTumu('tema')} onGuncelle={(p) => paketGuncelle('tema', p.name)} onAktif={temaAktif} />
        )}
        {tab === 'kullanicilar' && <KullaniciListe liste={kullanicilar} mesgul={mesgul} onReset={parolaSifirla} />}

        {tab !== 'genel' && cikti && <Cikti metin={cikti} />}
      </div>
    </div>
  )
}

// ================= parçalar =================

function Metrik({ label, v, pill }: { label: string; v: string; pill?: { t: string; c: 'green' | 'amber' | 'red' } }) {
  return (
    <div className="rounded-lg bg-slate-50 dark:bg-dark-800/40 p-4">
      <div className="text-xs text-slate-400 font-medium">{label}</div>
      <div className="flex items-center gap-2 mt-1.5">
        <span className="text-xl font-semibold text-slate-900 dark:text-slate-100 tracking-tight">{v}</span>
        {pill && <StatusPill t={pill.t} c={pill.c} />}
      </div>
    </div>
  )
}

function StatusPill({ t, c }: { t: string; c: 'green' | 'amber' | 'red' | 'slate' }) {
  const cls = {
    green: 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-300',
    amber: 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-300',
    red: 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-300',
    slate: 'bg-slate-100 dark:bg-dark-600 text-slate-500 dark:text-slate-300',
  }[c]
  return <span className={`text-[11px] font-medium px-2 py-0.5 rounded-full ${cls}`}>{t}</span>
}

function Btn({ onClick, bekle, children, tur }: { onClick: () => void; bekle: boolean; children: React.ReactNode; tur?: 'primary' | 'outline' }) {
  const cls = tur === 'primary'
    ? 'bg-dark-800 dark:bg-dark-600 text-white dark:text-slate-100 hover:bg-dark-700 dark:hover:bg-slate-600 border-transparent'
    : 'bg-white dark:bg-dark-700 border-slate-200 dark:border-dark-600 text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-dark-600/50'
  return (
    <button onClick={onClick} disabled={bekle} className={`text-sm px-4 py-2 rounded-full border font-medium disabled:opacity-50 transition ${cls}`}>
      {bekle ? cevir('İşleniyor…') : children}
    </button>
  )
}

function Cikti({ metin }: { metin: string }) {
  const temiz = metin.replace(/\[[0-9;]*m/g, '')
  return (
    <details className="mt-4 group" open>
      <summary className="text-xs text-slate-400 cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-300">{cevir("İşlem çıktısı")}</summary>
      <pre className="mt-2 max-h-44 overflow-auto text-[12px] leading-relaxed bg-slate-50 dark:bg-dark-800/60 border border-slate-100 dark:border-dark-600/60 text-slate-600 dark:text-slate-300 rounded-lg p-3 whitespace-pre-wrap break-words">{temiz}</pre>
    </details>
  )
}

function PaketTablo({ tur, liste, mesgul, onTumu, onGuncelle, onTogle, onAktif }: {
  tur: 'eklenti' | 'tema'; liste: Paket[] | null; mesgul: string | null
  onTumu: () => void; onGuncelle: (p: Paket) => void; onTogle?: (p: Paket) => void; onAktif?: (p: Paket) => void
}) {
  if (liste === null) return <div className="text-sm text-slate-400 py-4">{cevir("Yükleniyor…")}</div>
  if (liste.length === 0) return <div className="text-sm text-slate-400 py-4">{tur === 'eklenti' ? cevir('Eklenti') : cevir('Tema')} {cevir("bulunamadı.")}</div>
  const guncellenebilir = liste.filter(p => p.update === 'available').length
  return (
    <div>
      {guncellenebilir > 0 && (
        <div className="flex items-center justify-between mb-4 px-4 py-3 rounded-lg bg-amber-50 dark:bg-amber-900/15 border border-amber-100 dark:border-amber-800/50">
          <span className="text-sm text-amber-700 dark:text-amber-300 font-medium">{guncellenebilir} {cevir("güncelleme mevcut")}</span>
          <button disabled={!!mesgul} onClick={onTumu} className="text-sm px-4 py-1.5 rounded-full bg-dark-800 dark:bg-dark-600 text-white dark:text-slate-100 font-medium hover:bg-dark-700 dark:hover:bg-slate-600 disabled:opacity-50 transition">{mesgul === `${tur}:tum` ? '…' : cevir("Tümünü güncelle")}</button>
        </div>
      )}
      <div className="divide-y divide-slate-100 dark:divide-dark-600/50">
        {liste.map(p => {
          const aktif = p.status === 'active'
          const guncel = p.update === 'available'
          return (
            <div key={p.name} className="flex items-center justify-between gap-3 py-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{p.name}</span>
                  <StatusPill t={aktif ? cevir('Etkin') : cevir("Devre dışı")} c={aktif ? 'green' : 'slate'} />
                </div>
                <div className="text-xs text-slate-400 mt-0.5">
                  {cevir("Sürüm")} {p.version}{guncel && <span className="text-amber-600 dark:text-amber-400"> → {p.update_version} {cevir("mevcut")}</span>}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 shrink-0">
                {guncel && <button disabled={!!mesgul} onClick={() => onGuncelle(p)} className="text-xs px-3 py-1.5 rounded-full bg-amber-500 hover:bg-amber-600 text-white font-medium disabled:opacity-50 transition">{mesgul === `${tur}:${p.name}` ? '…' : cevir("Güncelle")}</button>}
                {onTogle && <button disabled={!!mesgul} onClick={() => onTogle(p)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-dark-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-dark-600/50 disabled:opacity-50 transition">{mesgul === `ekl:${p.name}` ? '…' : aktif ? cevir('Devre dışı bırak') : cevir("Etkinleştir")}</button>}
                {onAktif && !aktif && <button disabled={!!mesgul} onClick={() => onAktif(p)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-dark-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-dark-600/50 disabled:opacity-50 transition">{mesgul === `tema:${p.name}` ? '…' : cevir("Etkinleştir")}</button>}
                {onAktif && aktif && <StatusPill t={cevir("Aktif tema")} c="green" />}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function KullaniciListe({ liste, mesgul, onReset }: { liste: Kullanici[] | null; mesgul: string | null; onReset: (u: Kullanici) => void }) {
  if (liste === null) return <div className="text-sm text-slate-400 py-4">{cevir("Yükleniyor…")}</div>
  if (liste.length === 0) return <div className="text-sm text-slate-400 py-4">{cevir("Kullanıcı bulunamadı.")}</div>
  return (
    <div className="divide-y divide-slate-100 dark:divide-dark-600/50">
      {liste.map(u => (
        <div key={u.ID} className="flex items-center justify-between gap-3 py-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="w-9 h-9 rounded-full bg-slate-100 dark:bg-dark-600 flex items-center justify-center text-xs font-semibold text-slate-500 dark:text-slate-300 shrink-0">
              {(u.display_name || u.user_login).slice(0, 1).toUpperCase()}
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{u.user_login}</span>
                <StatusPill t={u.roles} c="slate" />
              </div>
              <div className="text-xs text-slate-400 truncate">{u.user_email}</div>
            </div>
          </div>
          <button disabled={!!mesgul} onClick={() => onReset(u)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-dark-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-dark-600/50 disabled:opacity-50 transition shrink-0">{mesgul === `pw:${u.ID}` ? '…' : cevir("Parola sıfırla")}</button>
        </div>
      ))}
    </div>
  )
}

// ================= kurulum formu · sonuç · modal =================

function KurulumFormu(p: {
  baslik: string; setBaslik: (s: string) => void; altDizin: string; setAltDizin: (s: string) => void
  adminK: string; setAdminK: (s: string) => void; adminE: string; setAdminE: (s: string) => void
  kur: (e: React.FormEvent) => void; kuruyor: boolean; kapat?: () => void
}) {
  return (
    <form onSubmit={p.kur} className="rounded-lg border border-slate-200/70 dark:border-dark-600/60 bg-white dark:bg-dark-700/40 p-5">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Yeni WordPress kurulumu")}</h3>
        {p.kapat && <button type="button" onClick={p.kapat} className="text-xs text-slate-400 hover:text-slate-600"><span className="inline-flex items-center gap-1.5"><Ikon d={I.kapat} /> {cevir("Kapat")}</span></button>}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Girdi et={cevir("Site başlığı")} v={p.baslik} set={p.setBaslik} zorunlu ph={cevir("Benim Blogum")} />
        <Girdi et={cevir("Alt dizin (isteğe bağlı)")} v={p.altDizin} set={p.setAltDizin} ph={cevir("boş = kök · örn: blog")} mono />
        <Girdi et={cevir("Admin kullanıcı")} v={p.adminK} set={p.setAdminK} zorunlu mono ph={cevir("örn. site-yoneticisi")} />
        <Girdi et={cevir("Admin e-posta")} v={p.adminE} set={p.setAdminE} zorunlu type="email" ph="admin@site.com" />
      </div>
      <button disabled={p.kuruyor || !p.adminK.trim()} className="mt-5 px-5 py-2.5 rounded-full bg-dark-800 dark:bg-dark-600 text-white dark:text-slate-100 text-sm font-medium hover:bg-dark-700 dark:hover:bg-slate-600 disabled:opacity-50 transition">
        {p.kuruyor ? cevir('Kuruluyor… (~30 sn)') : cevir('WordPress kur')}
      </button>
    </form>
  )
}

function Girdi({ et, v, set, zorunlu, ph, mono, type }: { et: string; v: string; set: (s: string) => void; zorunlu?: boolean; ph?: string; mono?: boolean; type?: string }) {
  return (
    <label className="block">
      <span className="text-xs text-slate-500 dark:text-slate-400 font-medium">{et}</span>
      <input value={v} onChange={e => set(e.target.value)} required={zorunlu} placeholder={ph} type={type || 'text'}
        className={`mt-1.5 w-full px-3.5 py-2.5 rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800 text-sm text-slate-800 dark:text-slate-100 placeholder:text-slate-300 dark:placeholder:text-slate-600 focus:border-slate-400 dark:focus:border-slate-500 focus:ring-4 focus:ring-slate-100 dark:focus:ring-dark-600 outline-none transition ${mono ? 'font-mono' : ''}`} />
    </label>
  )
}

function KurulumSonuc({ s, kapat }: { s: Sonuc; kapat: () => void }) {
  // TEK SEFERLIK: admin_parola yalnızca kurulum yanıtında gelir; sonradan alınamaz.
  const parola = s.admin_parola
  return (
    <div className="mb-5 rounded-lg border border-emerald-100 dark:border-emerald-800/60 bg-emerald-50/60 dark:bg-emerald-900/15 p-5">
      <div className="flex items-center justify-between mb-3">
        <div className="text-sm font-semibold text-emerald-700 dark:text-emerald-300">WordPress {s.surum} {cevir("kuruldu")}</div>
        <button onClick={kapat} className="text-xs text-emerald-600/70 hover:text-emerald-700"><Ikon d={I.kapat} /></button>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-2 text-sm">
        <Satir et="Site" v={s.site_url} link />
        <Satir et={cevir("Yönetim")} v={s.admin_url} link />
        <Satir et={cevir("Kullanıcı")} v={s.admin_kullanici} mono />
        {parola && (
          <div className="flex items-baseline gap-2 min-w-0">
            <span className="text-xs text-slate-400 shrink-0 w-16">{cevir("Parola")}</span>
            <span className="flex items-center gap-2 min-w-0">
              <span className="text-sm text-slate-800 dark:text-slate-100 font-mono break-all">{parola}</span>
              <button type="button" onClick={() => navigator.clipboard?.writeText(parola)}
                className="shrink-0 text-xs px-2.5 py-1 rounded-full border border-emerald-200 dark:border-emerald-800/60 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-100/60 dark:hover:bg-emerald-900/30 transition">{cevir("Kopyala")}</button>
            </span>
          </div>
        )}
      </div>
      <p className="text-xs text-amber-700 dark:text-amber-400 mt-3">{cevir("Parolayı şimdi kaydedin — tekrar gösterilmez.")}</p>
    </div>
  )
}

function Satir({ et, v, mono, link }: { et: string; v: string; mono?: boolean; link?: boolean }) {
  return (
    <div className="flex items-baseline gap-2 min-w-0">
      <span className="text-xs text-slate-400 shrink-0 w-16">{et}</span>
      {link ? <a href={v} target="_blank" rel="noreferrer" className="text-sm text-slate-700 dark:text-slate-200 hover:underline truncate">{v}</a>
        : <span className={`text-sm text-slate-800 dark:text-slate-100 truncate ${mono ? 'font-mono' : ''}`}>{v}</span>}
    </div>
  )
}
