import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata, fetchAskiKontrol, COK_UZUN_ISTEK, UZUN_ISTEK } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import ConfirmDialog from '@/components/ConfirmDialog'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { useAuth } from '@/store/auth'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
type Yedek = { id: number; domain_id: number; tip: string; kaynak?: 'sistem' | 'manuel'; dosya: string; boyut_b: number; notlar: string; olusturma: string; mevcut?: 'yerel' | 'uzak_iddia' | 'yok' }
// duzenlenebilir: YALNIZ gerçek yönetici (sunucu söyler). Müşteri / bayi / müşteri-adına
// görünümünde plan seçici çizilmez — müşteri yedeği yalnız manuel (2026-10-03).
type ManuelDurum = { bekleme_sn: number; bekleme_saat: number; eszamanli: number; calisan: number; sira: number }
type Schedule = {
  freq: 'none' | 'daily' | 'weekly'; hour: number; retention: number; last_backup_at?: string
  duzenlenebilir?: boolean; son_sistem_yedek?: string; manuel?: ManuelDurum
}
type Destination = {
  yok?: boolean
  id?: number; tip?: 'ftp' | 'sftp'; host?: string; port?: number
  kullanici?: string; uzak_dizin?: string; aktif?: boolean
  son_yukleme?: string; son_durum?: string; son_hata?: string
}


const BACKUP_EN: Record<string, string> = {
  "(boş bırakırsanız mevcut korunur)": "(leave empty to keep the current one)",
  "İşlem başarısız": "Operation failed",
  "Kaydedildi": "Saved",
  "Ayrı klasöre": "To a separate folder",
  "Bağlantı Testi": "Connection Test",
  "Belirli dosya/klasörleri geri al": "Restore specific files/folders",
  "Bir veritabanı seçin": "Select a database",
  "Bu sayıyı aşan eski oto-yedekler silinir. Manuel yedekler korunur.": "Auto-backups older than this count are deleted. Manual backups are kept.",
  "Dosya Seç": "Select File",
  "Dosyalar + tüm veritabanları": "Files + all databases",
  "En az bir dosya/klasör seçin": "Select at least one file/folder",
  "Eşleşme yok": "No matches",
  "Geri Yükle": "Restore",
  "Geri yükleme başarısız": "Restore failed",
  "Geri yükleme tamam": "Restore complete",
  "Günlük": "Daily",
  "Haftalık": "Weekly",
  "Henüz yedek yok": "No backups yet",
  "Her 7 günde bir yedek, daha ekonomik disk kullanımı.": "A backup every 7 days, more economical disk usage.",
  "Her gün seçilen saatte yedek üretilir, son N tutulur.": "A backup is produced every day at the selected time, the last N are kept.",
  "Liste kısaltıldı — daha spesifik aramak için filtreleyin.": "List truncated — filter for a more specific search.",
  "Otomatik Yedekleme Planı": "Automatic Backup Schedule",
  "Otomatik yedek kapalı": "Automatic backup off",
  "Otomatik yedek kapatıldı": "Automatic backup disabled",
  "Planlı": "Scheduled",
  "Tek DB — üzerine yaz veya yeni ada": "Single DB — overwrite or to a new name",
  "Temiz geri yükleme": "Clean restore",
  "Tutulacak yedek sayısı (retention)": "Number of backups to keep (retention)",
  "Uzak yedek hedefi silinsin mi? Mevcut yedekler etkilenmez, sadece bundan sonraki otomatik gönderim durur.": "Delete the remote backup target? Existing backups are unaffected, only future automatic sending stops.",
  "Veritabanı Seç": "Select Database",
  "Veritabanına dokunma": "Don't touch the database",
  "Yalnız Dosyalar": "Files Only",
  "Yalnız Veritabanı": "Database Only",
  "Yedek dosyasını sil": "Delete backup file",
  "Yedek içeriği okunamadı": "Failed to read backup contents",
  "Yedek oluşturulamadı": "Failed to create backup",
  "Yedek oluşturuldu": "Backup created",
  "Yedekte veritabanı yok": "No database in the backup",
  "Yedekten geri yükle": "Restore from backup",
  "Yeni veritabanı adı girin": "Enter a new database name",
  "geri-yukleme-…/ (hiçbir şey ezilmez)": "restore-…/ (nothing is overwritten)",
  "orijinal konuma (üzerine yaz)": "to the original location (overwrite)",
  "Çalışma saati (yerel)": "Run time (local)",
  "Üzerine yaz": "Overwrite",
  "Şimdi Yedekle": "Back Up Now",
  "Hesabınız sistem tarafından otomatik yedeklenir; bu yedekler aşağıda listelenir. İhtiyaç duyduğunuzda 'Şimdi Yedekle' ile ek yedek alabilirsiniz.": "Your account is backed up automatically by the system; these backups are listed below. When you need one, you can take an extra backup with 'Back Up Now'.",
  "Otomatik yedek": "Automatic backup",
  "Sistem tarafından": "By the system",
  "Son sistem yedeği:": "Last system backup:",
  "Henüz sistem yedeği yok": "No system backup yet",
  "Manuel yedek": "Manual backup",
  "Manuel yedekler sunucuyu yormamak için düşük öncelikle ve sırayla alınır; aynı hesap için {0} saatte bir alınabilir.": "Manual backups run one at a time at low priority so the server is not overloaded; one can be taken every {0} hours per account.",
  "Yeni manuel yedek {0} sonra alınabilir.": "A new manual backup can be taken in {0}.",
  "Plan (yalnız yönetici)": "Schedule (admin only)",
  "Kapalı": "Off",
  "Saat": "Time",
  "Saklanacak": "Keep",
  "Manuel yedeklerim bu hedefe de gönderilsin": "Also send my manual backups to this target",
  "Sistem yedekleri de bu hedefe kopyalanır. Gönderim, yedekle aynı sırada ve düşük öncelikle yapılır.": "System backups are also copied to this target. Uploads run in the same queue as the backup, at low priority.",
  "Yedekler alındıktan sonra uzak sunucunuza da gönderilir — disk arızasına karşı ek koruma.": "Backups are also sent to your remote server after they are taken — extra protection against disk failure.",
  "Yedek sırada": "Backup queued",
  "{0}. sırada — sırası gelince başlayacak": "position {0} — starts when its turn comes",
  "sırada": "queued",
  "Sistem": "System",
  "Manuel": "Manual",
  "Kaynak": "Source",
  "Yedek": "Backup",
  "{0} sa {1} dk": "{0} h {1} min",
  "{0} dk": "{0} min",
  "Son: başarılı": "Last: successful",
  "Bağlantı OK": "Connection OK",
  "Son: hata": "Last: error",
  "Hata": "Error",
  "seçili": "selected",
  "Tip": "Type",
  "Evet, sil": "Yes, delete",
  "Dosya ara…": "Search files…",
  "backup.firma.com": "backup.company.com",
  "Son oto-yedek:": "Last auto-backup:",
  "Uzak Yedek Hedefi (FTP / SFTP)": "Remote Backup Target (FTP / SFTP)",
  "Uzak dizin": "Remote directory",
  "Uzak hedef kaydedildi": "Remote target saved",
  "Uzak hedef silindi": "Remote target deleted",
  "Yedekler": "Backups",
  "Protokol": "Protocol",
  "Dosya": "File",
  "Yerinde": "In place",
  "Anasayfa": "Home",
  "Hedef kaydedilemedi": "Failed to save target",
  "Emin misiniz?": "Are you sure?",
  "Otomatik yedek aktif: {0}, {1}:00, son {2} yedek tutulur": "Automatic backup active: {0}, {1}:00, last {2} backups kept",
  "Plan kaydedilemedi": "Failed to save schedule",
  "Bilgi": "Info",
  "Kısmen silindi": "Partially deleted",
  "Uzak kopya silinemedi": "Remote copy could not be deleted",
  "Yine de yedek kaydını silmek istiyor musunuz? Uzak depodaki dosya KALACAK ve orada yer kaplamaya devam edecek.": "Delete the backup record anyway? The file on remote storage WILL REMAIN and keep using space there.",
  "Silindi": "Deleted",
  "DOSYA YOK": "FILE MISSING",
  "uzakta (doğrulanmadı)": "remote (unverified)",
  "Yedek uzak depoya taşınmış — önce geri getirilmeli": "Backup moved to remote storage — must be fetched first",
  "Yedek dosyası bulunamadı": "Backup file not found",
  "Yedek silinemedi": "Backup could not be deleted",
  "{0} {1}:00 · son {2} oto-yedek korunur": "{0} {1}:00 · last {2} auto-backups kept",
  "Otomatik yedek yok. Yalnız manuel “Şimdi Yedekle”.": "No automatic backup. Only manual “Back Up Now”.",
  "Yedek üretildikten sonra arkaplanda uzak sunucuya yüklenir — disk arızasına karşı off-site koruma.": "After a backup is produced it is uploaded to the remote server in the background — off-site protection against disk failure.",
  "Son yükleme:": "Last upload:",
  "Parola": "Password",
  "Aktif (her yedek bu hedefe gönderilsin)": "Active (send every backup to this target)",
  "Test ediliyor…": "Testing…",
  "Hedefi sil": "Delete target",
  "Aktif": "Active",
  "Yedekleniyor…": "Backing up…",
  "hazırlanıyor": "preparing",
  "veritabanları alınıyor": "dumping databases",
  "dosyalar arşivleniyor": "archiving files",
  "bütünlük doğrulanıyor": "verifying integrity",
  "uzak hedefe yükleniyor": "uploading off-site",
  "yedek uzak hedeften indiriliyor": "downloading from off-site",
  "arşiv açılıyor": "extracting archive",
  "dosyalar geri yükleniyor": "restoring files",
  "veritabanı içe aktarılıyor": "importing database",
  "tamamlandı": "completed",
  "başarısız": "failed",
  "Geri yükleniyor…": "Restoring…",
  "yedek": "backups",
  "\"{0}\" silinsin mi?": "Delete \"{0}\"?",
  "Tam": "Full",
  "Dosyalara dokunma": "Don't touch files",
  " — yedekte olmayan dosyaları SİL. Kapalıyken (önerilen) aktif uygulama korunur, yalnız yedektekiler üzerine yazılır.": " — DELETE files not in the backup. When off (recommended) the active app is preserved, only files in the backup are overwritten.",
  "Domaine ait tüm veritabanları yedekteki haline döndürülür. Dosyalara dokunulmaz.": "All databases of the domain are reverted to their state in the backup. Files are not touched.",
  "Yeni veritabanına (güvenli, orijinal korunur)": "To a new database (safe, original is preserved)",
  " — mevcut ‘{0}’ silinip yüklenir": " — the existing ‘{0}’ is dropped and loaded",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BACKUP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

type Ilerleme = {
  aktif: boolean; bitti: boolean; islem: string; asama: string
  yapilan: number; toplam: number; yuzde: number; gecen_sn: number
  sonuc?: string; hata?: string
  sirada?: boolean; sira_no?: number
}

const AYLAR: Record<'tr' | 'en', string[]> = {
  tr: ['Oca', 'Şub', 'Mar', 'Nis', 'May', 'Haz', 'Tem', 'Ağu', 'Eyl', 'Eki', 'Kas', 'Ara'],
  en: ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'],
}

// "2026-08-31 03:01" → "31 Ağu 2026 · 03:01" (saat dilimi dönüşümü YOK: sunucunun
// verdiği yerel saat aynen gösterilir).
function tarihEtiket(s?: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(s || '')
  if (!m) return s || ''
  const ay = AYLAR[i18n.language === 'en' ? 'en' : 'tr'][Number(m[2]) - 1] || m[2]
  return `${Number(m[3])} ${ay} ${m[1]} · ${m[4]}:${m[5]}`
}

function sureEtiket(sn: number): string {
  const dk = Math.max(1, Math.ceil(sn / 60))
  return dk >= 60 ? cevirT(cevir("{0} sa {1} dk"), Math.floor(dk / 60), dk % 60) : cevirT(cevir("{0} dk"), dk)
}

export default function DomainBackupsPage() {
  useTranslation() // dil re-render aboneligi
  const { onay, bilgi } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [yedekler, setYedekler] = useState<Yedek[]>([])
  const [yuk, setYuk] = useState(true)
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  // 🔴 Uzun islem gorunurlugu: yedek/geri-yukleme dakikalarca surer. Tek bir
  // "Yedekleniyor…" yazisi kullaniciya ilerleyip ilerlemedigini SOYLEMIYORDU.
  const [ilerleme, setIlerleme] = useState<Ilerleme | null>(null)
  const ilerlemeTimer = useRef<number | null>(null)
  const [silinecek, setSilinecek] = useState<Yedek | null>(null)
  const [geriYukle, setGeriYukle] = useState<Yedek | null>(null)

  const [sched, setSched] = useState<Schedule>({ freq: 'none', hour: 3, retention: 7 })
  const [schedKayit, setSchedKayit] = useState(false)

  const [dest, setDest] = useState<Destination>({ yok: true })
  // Kaydedilmemis taslak "uzak yedekleme acik" izlenimi vermemeli: aktif=false.
  // Kayitli hedef yuklenince gercek deger yukle() icinde geri yazilir.
  const [destForm, setDestForm] = useState({ tip: 'sftp' as 'ftp'|'sftp', host: '', port: 22, kullanici: '', parola: '', uzak_dizin: '/', aktif: false })
  const [destKayit, setDestKayit] = useState(false)
  const [destTest, setDestTest] = useState<{ ok: boolean; hata?: string } | null>(null)
  // Yalnız görünüm: destKayit üç işte ortak — spinner tetikleyen butonda dönsün.
  const [destIslem, setDestIslem] = useState<'test' | 'kaydet' | 'sil' | null>(null)

  // Yeni manuel yedek için kalan bekleme (sunucu ölçer; yönetici muaf → 0).
  const beklemeSn = sched.manuel?.bekleme_sn ?? 0

  const yukleNesli = useRef(0)
  function yukle() {
    if (!id) return
    setYuk(true)
    const _n = ++yukleNesli.current
    Promise.all([
      api.get<Yedek[]>(`/domains/${id}/backups`),
      api.get<Schedule>(`/domains/${id}/backup-schedule`).catch(() => ({ data: { freq: 'none', hour: 3, retention: 7 } as Schedule })),
      api.get<Destination>(`/domains/${id}/backup-destination`).catch(() => ({ data: { yok: true } as Destination })),
    ]).then(([y, s, d]) => {
      if (_n !== yukleNesli.current) return
      setYedekler(y.data)
      setSched(s.data)
      setDest(d.data)
      if (!d.data.yok) {
        setDestForm({
          tip: (d.data.tip || 'sftp') as 'ftp'|'sftp',
          host: d.data.host || '',
          port: d.data.port || (d.data.tip === 'ftp' ? 21 : 22),
          kullanici: d.data.kullanici || '',
          parola: '',  // güvenlik: boş bırak, kullanıcı isterse yeniden girer
          uzak_dizin: d.data.uzak_dizin || '/',
          aktif: !!d.data.aktif,
        })
      }
    })
      .catch(e => { if (_n !== yukleNesli.current) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => { if (_n === yukleNesli.current) setYuk(false) })
  }

  async function destKaydet() {
    setDestKayit(true); setHata(null); setBasari(null); setDestTest(null)
    try {
      const r = await api.put<Destination>(`/domains/${id}/backup-destination`, destForm)
      setDest(r.data)
      setBasari(cevir("Uzak hedef kaydedildi"))
      toast.basari(cevir("Uzak hedef kaydedildi"))
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      const m = apiHata(e, cevir("Hedef kaydedilemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setDestKayit(false)
    }
  }

  async function destBaglantiTesti() {
    setDestKayit(true); setDestTest(null)
    try {
      const r = await api.post<{ ok: boolean; hata?: string }>(`/domains/${id}/backup-destination/test`, destForm)
      setDestTest(r.data)
      setTimeout(() => setDestTest(null), 8000)
    } catch (e) {
      setDestTest({ ok: false, hata: apiHata(e) })
    } finally {
      setDestKayit(false)
    }
  }

  async function destSil() {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevir("Uzak yedek hedefi silinsin mi? Mevcut yedekler etkilenmez, sadece bundan sonraki otomatik gönderim durur."), ton: 'tehlike', onayIkonu: I.cop }))) return
    setDestKayit(true)
    try {
      await api.delete(`/domains/${id}/backup-destination`)
      setDest({ yok: true })
      setDestForm({ tip: 'sftp', host: '', port: 22, kullanici: '', parola: '', uzak_dizin: '/', aktif: false })
      setBasari(cevir("Uzak hedef silindi"))
      toast.basari(cevir("Uzak hedef silindi"))
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setDestKayit(false)
    }
  }
  useEffect(() => {
    let iptal = false
    if (id) api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("Alan adı bilgisi alınamadı"))(e) })
    yukle()
    // Sayfa yenilendiğinde süren/sıradaki yedek görünür kalsın (sıra no + aşama).
    if (id) api.get<Ilerleme>(`/domains/${id}/backups/ilerleme`).then(r => {
      if (iptal || !r.data.aktif || r.data.bitti) return
      setIlerleme(r.data); setIsleniyor(true); ilerlemeIzle()
    }).catch(() => { /* yoklama isteğe bağlı */ })
    return () => { iptal = true; yukleNesli.current++ }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  async function scheduleKaydet(yeni: Schedule) {
    setSchedKayit(true); setHata(null); setBasari(null)
    try {
      const r = await api.put<{ schedule: Schedule }>(`/domains/${id}/backup-schedule`, yeni)
      setSched(r.data.schedule)
      const mesaj = yeni.freq === 'none'
        ? cevir("Otomatik yedek kapatıldı")
        : cevirT(cevir("Otomatik yedek aktif: {0}, {1}:00, son {2} yedek tutulur"), yeni.freq === 'daily' ? cevir("Günlük") : cevir("Haftalık"), String(yeni.hour).padStart(2,'0'), yeni.retention)
      setBasari(mesaj)
      toast.basari(cevir("Kaydedildi"), mesaj)
      setTimeout(() => setBasari(null), 5000)
    } catch (e) {
      const m = apiHata(e, cevir("Plan kaydedilemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setSchedKayit(false)
    }
  }

  // Ilerlemeyi 1.5 sn'de bir yoklar; is bitince durur ve listeyi tazeler.
  // 🔴 Yedek/geri-yukleme dakikalarca surer; tek bir "Yedekleniyor…" yazisi
  // kullaniciya ilerleyip ilerlemedigini SOYLEMIYORDU.
  function ilerlemeIzle() {
    if (ilerlemeTimer.current) window.clearInterval(ilerlemeTimer.current)
    ilerlemeTimer.current = window.setInterval(async () => {
      try {
        const { data } = await api.get<Ilerleme>(`/domains/${id}/backups/ilerleme`)
        setIlerleme(data.aktif ? data : null)
        if (!data.aktif || data.bitti) {
          if (ilerlemeTimer.current) window.clearInterval(ilerlemeTimer.current)
          ilerlemeTimer.current = null
          setIsleniyor(false)
          yukle()
          if (data.hata) { setHata(data.hata); toast.hata(cevir("İşlem başarısız"), data.hata) }
          else if (data.sonuc) { setBasari(data.sonuc); toast.basari(cevir("Kaydedildi"), data.sonuc) }
          window.setTimeout(() => setIlerleme(null), 6000)
        }
      } catch { /* gecici hata: bir sonraki tur tekrar dener */ }
    }, 1500)
  }

  async function olustur() {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      // Uc 202 doner: is arka planda calisir, ilerleme cubugu devralir.
      await api.post(`/domains/${id}/backups`)
      ilerlemeIzle()
    } catch (e) {
      const m = apiHata(e, cevir("Yedek oluşturulamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
      setIsleniyor(false)
    }
  }

  async function sil() {
    if (!silinecek) return
    try {
      const { data } = await api.delete<{ ok?: boolean; uyari?: string }>(`/domains/${id}/backups/${silinecek.id}`, UZUN_ISTEK)
      setSilinecek(null); yukle()
      // 🔴 KISMİ BAŞARI SESSİZ GEÇMEZ. Sunucu "silindi" derken yerel dosya
      // silinememiş olabilir; eskiden yanıt hiç okunmuyordu ve kullanıcı
      // temizlendiğini sanıyordu. (Uzak kopya silinemezse sunucu 409 döner
      // ve aşağıdaki catch dalı çalışır.)
      if (data?.uyari) {
        await bilgi({ baslik: cevir("Kısmen silindi"), mesaj: data.uyari, ton: 'uyari' })
      }
    } catch (e) {
      // 🔴 KULLANICI KİLİTLENMEZ. Uzak depo erişilemezse sunucu 409 döner ve
      // kaydı korur — doğru davranış, ama tek başına bırakılırsa "FTP'yi
      // düzeltene kadar disk temizleyemiyorum" durumu doğar. Açık onayla
      // silmeye izin verilir; uzak kopyanın KALDIĞI net söylenir ve sunucu
      // bunu denetim kaydına yazar.
      const y = (e as { response?: { status?: number; data?: { uzagi_yoksay?: boolean; hata?: string } } })?.response
      if (y?.status === 409 && y?.data?.uzagi_yoksay) {
        const devam = await onay({
          baslik: cevir("Uzak kopya silinemedi"),
          mesaj: (y.data.hata || '') + ' ' + cevir("Yine de yedek kaydını silmek istiyor musunuz? Uzak depodaki dosya KALACAK ve orada yer kaplamaya devam edecek."),
          ton: 'tehlike', onayIkonu: I.cop,
        })
        if (devam) {
          try {
            const { data } = await api.delete<{ uyari?: string }>(
              `/domains/${id}/backups/${silinecek.id}?uzagi_yoksay=1`)
            setSilinecek(null); yukle()
            if (data?.uyari) await bilgi({ baslik: cevir("Silindi"), mesaj: data.uyari, ton: 'uyari' })
          } catch (e2) {
            await bilgi({ baslik: cevir("Yedek silinemedi"), mesaj: apiHata(e2), ton: 'tehlike' })
          }
        }
        return
      }
      (await bilgi({ baslik: cevir("Yedek silinemedi"), mesaj: apiHata(e), ton: 'tehlike' }))
    }
  }

  function indir(y: Yedek) {
    const tok = useAuth.getState().token || ''
    fetch(`/api/v1/domains/${id}/backups/${y.id}/indir`, { headers: { Authorization: `Bearer ${tok}` } })
      // Askı 403'ü: askı sayfasına geçilir; hata gövdesi dosya diye indirilmez.
      .then(async r => ((await fetchAskiKontrol(r, tok)) ? null : r.blob()))
      .then(blob => {
        if (!blob) return
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = y.dosya
        a.click()
      })
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Yedekler") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Yedekler")}</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-1">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
      </p>}
      <p className="text-sm text-slate-600 dark:text-slate-400 mb-5 max-w-3xl">
        {cevir("Hesabınız sistem tarafından otomatik yedeklenir; bu yedekler aşağıda listelenir. İhtiyaç duyduğunuzda 'Şimdi Yedekle' ile ek yedek alabilirsiniz.")}
      </p>

      {/* Tek kart, masaüstünde 2 sütun: SOL yedekleme durumu/plan + Şimdi Yedekle, SAĞ uzak FTP/SFTP hedefi. */}
      <div className="mb-5 grid grid-cols-1 lg:grid-cols-2 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg divide-y lg:divide-y-0 lg:divide-x divide-slate-200 dark:divide-dark-600">
        {/* SOL: otomatik yedek durumu (yönetici: kompakt plan seçici) + manuel yedek */}
        <section className="p-4 sm:p-5 min-w-0 flex flex-col gap-4" aria-labelledby="yedek-durum-baslik">
          <div>
            <h3 id="yedek-durum-baslik" className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Otomatik yedek")}</h3>
            {sched.duzenlenebilir ? (
              <div className="mt-2 space-y-3">
                <div role="radiogroup" aria-label={cevir("Plan (yalnız yönetici)")} className="inline-grid grid-cols-3 gap-0.5 rounded-lg bg-slate-100 dark:bg-dark-800 p-0.5 w-full sm:w-auto">
                  {(['none','daily','weekly'] as const).map(f => {
                    const aktif = sched.freq === f
                    return (
                      <button key={f} type="button" role="radio" aria-checked={aktif} disabled={schedKayit}
                        onClick={() => { if (!aktif) scheduleKaydet({ ...sched, freq: f }) }}
                        className={`px-3 py-1.5 text-xs font-medium rounded-md transition disabled:opacity-60 ${aktif
                          ? 'bg-white dark:bg-dark-600 text-slate-900 dark:text-slate-100 shadow-sm'
                          : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'}`}>
                        {f === 'none' ? cevir("Kapalı") : f === 'daily' ? cevir("Günlük") : cevir("Haftalık")}
                      </button>
                    )
                  })}
                </div>
                {sched.freq !== 'none' && (
                  <div className="grid grid-cols-2 gap-3 max-w-sm">
                    <label className="block">
                      <span className="text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Saat")}</span>
                      <select value={sched.hour} disabled={schedKayit}
                        onChange={e => scheduleKaydet({ ...sched, hour: Number(e.target.value) })}
                        className="mt-1 w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100">
                        {Array.from({length:24},(_,i)=>i).map(h => <option key={h} value={h}>{String(h).padStart(2,'0')}:00</option>)}
                      </select>
                    </label>
                    <label className="block">
                      <span className="text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Saklanacak")}</span>
                      <input type="number" min={1} max={90} value={sched.retention} disabled={schedKayit}
                        onChange={e => setSched(s => ({...s, retention: Math.max(1, Math.min(90, Number(e.target.value)||1))}))}
                        onBlur={() => scheduleKaydet(sched)}
                        title={cevir("Bu sayıyı aşan eski oto-yedekler silinir. Manuel yedekler korunur.")}
                        className="mt-1 w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
                    </label>
                  </div>
                )}
              </div>
            ) : (
              <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                <dt className="text-slate-500 dark:text-slate-400">{cevir("Otomatik yedek")}:</dt>
                <dd className="text-slate-800 dark:text-slate-200 font-medium">{cevir("Sistem tarafından")}</dd>
                <dt className="text-slate-500 dark:text-slate-400">{cevir("Son sistem yedeği:")}</dt>
                <dd className="text-slate-800 dark:text-slate-200 tabular-nums">{sched.son_sistem_yedek ? tarihEtiket(sched.son_sistem_yedek) : cevir("Henüz sistem yedeği yok")}</dd>
              </dl>
            )}
          </div>

          <div className="border-t border-slate-100 dark:border-dark-600 pt-4">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Manuel yedek")}</h3>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              {cevirT(cevir("Manuel yedekler sunucuyu yormamak için düşük öncelikle ve sırayla alınır; aynı hesap için {0} saatte bir alınabilir."), sched.manuel?.bekleme_saat ?? 6)}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <Button onClick={olustur} yukleniyor={isleniyor} disabled={isleniyor || beklemeSn > 0} color="primary" ikon={I.arti}
                title={beklemeSn > 0 ? cevirT(cevir("Yeni manuel yedek {0} sonra alınabilir."), sureEtiket(beklemeSn)) : undefined}>
                {isleniyor ? (ilerleme?.sirada ? cevir("Yedek sırada") : cevir("Yedekleniyor…")) : cevir("Şimdi Yedekle")}
              </Button>
              <Button onClick={yukle} yukleniyor={yuk} variant="outlined" ikon={I.yenile}>{cevir("Yenile")}</Button>
            </div>
            {beklemeSn > 0 && !isleniyor && (
              <p className="mt-2 text-xs text-amber-700 dark:text-amber-300" role="status">
                {cevirT(cevir("Yeni manuel yedek {0} sonra alınabilir."), sureEtiket(beklemeSn))}
              </p>
            )}
          </div>
        </section>

        {/* SAĞ: Uzak Yedek Hedefi (FTP/SFTP) — sıkı düzen */}
        <section className="p-4 sm:p-5 min-w-0" aria-labelledby="uzak-hedef-baslik">
          <div className="flex items-start justify-between gap-3 mb-3">
            <div className="min-w-0">
              <h3 id="uzak-hedef-baslik" className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Uzak Yedek Hedefi (FTP / SFTP)")}</h3>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                {cevir("Yedekler alındıktan sonra uzak sunucunuza da gönderilir — disk arızasına karşı ek koruma.")}
              </p>
            </div>
            {!dest.yok && dest.son_durum && (
              <Badge variant="soft" color={dest.son_durum === 'basarili' ? 'success' : dest.son_durum === 'hata' ? 'error' : 'neutral'}
                className="shrink-0 gap-1 text-[10px] uppercase tracking-wider font-semibold">
                {dest.son_durum === 'basarili' ? <><span className="inline-block size-2 rounded-full bg-current" aria-hidden />{cevir('Son: başarılı')}</>
                  : dest.son_durum === 'hata' ? <><Ikon d={I.kapat} className="size-3 shrink-0" />{cevir('Son: hata')}</> : dest.son_durum}
              </Badge>
            )}
          </div>

          {!dest.yok && dest.son_yukleme && (
            <div className="mb-3 text-xs text-slate-500 dark:text-slate-400">
              {cevir("Son yükleme:")} <span className="font-mono">{dest.son_yukleme}</span>
              {dest.son_durum === 'hata' && dest.son_hata && (
                <UyariKutusu ton="tehlike" className="mt-1">
                  <span className="font-mono text-[11px] whitespace-pre-wrap">{dest.son_hata}</span>
                </UyariKutusu>
              )}
            </div>
          )}

          <div className="space-y-2.5 mb-3">
            <div role="radiogroup" aria-label={cevir("Protokol")} className="inline-grid grid-cols-2 gap-0.5 rounded-lg bg-slate-100 dark:bg-dark-800 p-0.5">
              {(['sftp','ftp'] as const).map(t => {
                const aktif = destForm.tip === t
                return (
                  <button key={t} type="button" role="radio" aria-checked={aktif}
                    onClick={() => setDestForm(f => ({...f, tip: t, port: t === 'sftp' ? 22 : 21}))}
                    className={`px-3 py-1 text-xs font-medium rounded-md transition inline-flex items-center gap-1.5 ${aktif
                      ? 'bg-white dark:bg-dark-600 text-slate-900 dark:text-slate-100 shadow-sm'
                      : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'}`}>
                    {t === 'sftp' && <Ikon d={I.kilit} className="size-3.5" />}{t.toUpperCase()}
                  </button>
                )
              })}
            </div>
            <div className="grid grid-cols-[1fr_5.5rem] gap-2">
              <label className="block min-w-0">
                <span className="sr-only">Host</span>
                <input type="text" value={destForm.host} placeholder={'Host · ' + cevir("backup.firma.com")} aria-label="Host"
                  onChange={e => setDestForm(f => ({...f, host: e.target.value}))}
                  className="w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
              </label>
              <label className="block">
                <span className="sr-only">Port</span>
                <input type="number" value={destForm.port} aria-label="Port" placeholder="Port"
                  onChange={e => setDestForm(f => ({...f, port: Number(e.target.value)||0}))}
                  className="w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
              </label>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              <input type="text" value={destForm.kullanici} autoComplete="off" aria-label={cevir("Kullanıcı")} placeholder={cevir("Kullanıcı")}
                onChange={e => setDestForm(f => ({...f, kullanici: e.target.value}))}
                className="w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
              <input type="password" value={destForm.parola} autoComplete="new-password" aria-label={cevir("Parola")}
                placeholder={!dest.yok ? cevir("(boş bırakırsanız mevcut korunur)") : cevir("Parola")}
                onChange={e => setDestForm(f => ({...f, parola: e.target.value}))}
                className="w-full px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
            </div>
            <label className="flex items-center gap-2">
              <span className="text-xs font-medium text-slate-600 dark:text-slate-400 shrink-0">{cevir("Uzak dizin")}</span>
              <input type="text" value={destForm.uzak_dizin}
                onChange={e => setDestForm(f => ({...f, uzak_dizin: e.target.value}))}
                className="min-w-0 flex-1 px-2.5 py-1.5 border border-slate-300 dark:border-dark-500 rounded text-sm font-mono bg-white dark:bg-dark-700 text-slate-900 dark:text-slate-100"/>
            </label>
          </div>

          <label className="flex items-start gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer mb-1">
            <input type="checkbox" checked={destForm.aktif}
              onChange={e => setDestForm(f => ({...f, aktif: e.target.checked}))}
              className="mt-0.5 cursor-pointer"/>
            <span>{cevir("Manuel yedeklerim bu hedefe de gönderilsin")}</span>
          </label>
          <p className="text-[11px] text-slate-500 dark:text-slate-400 mb-3 ps-6">{cevir("Sistem yedekleri de bu hedefe kopyalanır. Gönderim, yedekle aynı sırada ve düşük öncelikle yapılır.")}</p>

          <div className="flex flex-wrap items-center gap-2">
            {destTest && (
              <Badge variant="soft" color={destTest.ok ? 'success' : 'error'} className="gap-1 text-xs leading-snug">
                <Ikon d={destTest.ok ? I.onay : I.kapat} className="size-3.5 shrink-0" />
                {destTest.ok ? cevir('Bağlantı OK') : (destTest.hata?.slice(0, 80) || cevir('Hata'))}
              </Badge>
            )}
            <Button onClick={() => { setDestIslem('test'); destBaglantiTesti() }} disabled={destKayit || !destForm.host || !destForm.kullanici}
              yukleniyor={destKayit && destIslem === 'test'}
              variant="outlined" boyut="sm" ikon={I.yenile}>
              {destKayit && destIslem === 'test' ? cevir("Test ediliyor…") : cevir("Bağlantı Testi")}
            </Button>
            <Button onClick={() => { setDestIslem('kaydet'); destKaydet() }} disabled={destKayit || !destForm.host || !destForm.kullanici}
              yukleniyor={destKayit && destIslem === 'kaydet'}
              color="primary" boyut="sm" ikon={I.disket}>
              {cevir("Kaydet")}
            </Button>
            {!dest.yok && (
              <Button onClick={() => { setDestIslem('sil'); destSil() }} disabled={destKayit}
                yukleniyor={destKayit && destIslem === 'sil'}
                color="error" variant="soft" boyut="sm" ikon={I.cop}>
                {cevir("Hedefi sil")}
              </Button>
            )}
          </div>
        </section>
      </div>

      <div className="flex items-center mb-2">
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{yedekler.length} {cevir("yedek")}</span>
      </div>

      {/* 🔴 Uzun islem gorunurlugu: asama + yuzde + gecen sure. Yuzde YALNIZ
          beklenen boyut bilindiginde gosterilir; bilinmiyorsa hareketli cizgi ve
          yazilan bayt gosterilir ki kullanici "takildi mi" diye dusunmesin. */}
      {ilerleme && (
        <div className="mb-4 px-4 py-3 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/60">
          <div className="flex items-center gap-2 flex-wrap mb-2">
            <span className={`w-2 h-2 rounded-full ${ilerleme.bitti ? (ilerleme.hata ? 'bg-red-500' : 'bg-emerald-500') : ilerleme.sirada ? 'bg-slate-400' : 'bg-amber-500 animate-pulse'}`} />
            <span className="text-sm font-medium text-slate-800 dark:text-slate-100" role="status">
              {ilerleme.islem === 'geri' ? cevir("Geri yükleniyor…") : ilerleme.sirada ? cevir("Yedek sırada") : cevir("Yedekleniyor…")}
            </span>
            <span className="text-sm text-slate-500 dark:text-slate-400">· {ilerleme.sirada && ilerleme.sira_no
              ? cevirT(cevir("{0}. sırada — sırası gelince başlayacak"), ilerleme.sira_no)
              : cevir(ilerleme.asama)}</span>
            <span className="ml-auto text-xs font-mono text-slate-500 dark:text-slate-400 tabular-nums">
              {ilerleme.yuzde > 0 ? `${ilerleme.yuzde}%` : ''}
              {ilerleme.yapilan > 0 ? ` · ${formatBoyut(ilerleme.yapilan)}${ilerleme.toplam > 0 ? ' / ' + formatBoyut(ilerleme.toplam) : ''}` : ''}
              {` · ${ilerleme.gecen_sn}s`}
            </span>
          </div>
          <div className="h-1.5 rounded-full bg-slate-100 dark:bg-dark-600 overflow-hidden">
            {ilerleme.yuzde > 0 ? (
              <div className={`h-full rounded-full transition-all duration-700 ${ilerleme.hata ? 'bg-red-500' : ilerleme.bitti ? 'bg-emerald-500' : 'bg-amber-400'}`} style={{ width: `${ilerleme.yuzde}%` }} />
            ) : (
              <div className="h-full w-1/3 rounded-full bg-amber-400 animate-pulse" />
            )}
          </div>
        </div>
      )}

      {/* Mobilde kartlar zaten kendi çerçevelerini taşıyor — dış kapsayıcı yalnız lg'de çerçeve verir */}
      <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
        {/* Durum mesajları: dış kapsayıcının çerçevesi artık lg:-only olduğu için
            mobilde kendi kart çerçevelerini taşırlar (aksi halde çıplak metin kalırdı). */}
        {yuk ? <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 lg:rounded-none lg:border-0 lg:bg-transparent dark:lg:bg-transparent py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> :
         yedekler.length === 0 ? <div className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 lg:rounded-none lg:border-0 lg:bg-transparent dark:lg:bg-transparent py-16 text-center text-sm text-slate-500 dark:text-slate-500">{cevir("Henüz yedek yok")}</div> :
        <div className="lg:overflow-x-auto">
          <table className={T.tablo}>
          <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
            <tr>
              <th className={T.baslik}>{cevir("Yedek")}</th>
              <th className={T.baslik}>{cevir("Kaynak")}</th>
              <th className={T.baslik}>{cevir("Boyut")}</th>
              <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
            </tr>
          </thead>
          <tbody className={T.govde}>
            {yedekler.map(y => (
              <tr key={y.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700`}>
                {/* Birincil tanımlayıcı: hesap + okunur tarih — mobilde kart başlığı.
                    Tam dosya adı yalnız title'da (İndir aynı adla kaydeder). */}
                <td className={`${T.hucreBaslik}`} title={y.dosya}>
                  <span className="font-medium text-slate-900 dark:text-slate-100">{domain?.alan_adi || hesapAdi(y.dosya)}</span>
                  <span className="ml-2 text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap tabular-nums">{tarihEtiket(y.olusturma)}</span>
                  {/* 🔴 HAYALET KAYIT GÖRÜNÜR OLMALI. Dosyası ne yerelde ne
                      uzakta olan kayıtlar geçerli yedek gibi listeleniyordu;
                      kullanıcı "yedeğim var" sanıp İndir'e basınca 500
                      alıyordu. Liste, elindekini değil GERÇEĞİ göstermeli. */}
                  {y.mevcut === 'yok' && (
                    <Badge variant="soft" color="error" className="ml-2 px-1.5 py-0.5 text-[10px] font-semibold">
                      {cevir("DOSYA YOK")}
                    </Badge>
                  )}
                  {/* 🔴 "uzakta" DEĞİL "uzakta (doğrulanmadı)".
                      Sunucu bu değeri kaydın `notlar` etiketinden okuyor; ağa
                      çıkmıyor. Kesin bilgi gibi göstermek, hiçbir yerde
                      olmayan bir yedeği geçerli sandırırdı. */}
                  {y.mevcut === 'uzak_iddia' && (
                    <Badge variant="soft" color="warning" className="ml-2 px-1.5 py-0.5 text-[10px] font-semibold">
                      {cevir("uzakta (doğrulanmadı)")}
                    </Badge>
                  )}
                </td>
                <td className={T.hucre} data-etiket={cevir("Kaynak")}>
                  <Badge variant="soft" color={kaynakOf(y) === 'sistem' ? 'info' : 'neutral'}
                    className="px-1.5 py-0.5 text-[11px] font-semibold">{kaynakOf(y) === 'sistem' ? cevir('Sistem') : cevir('Manuel')}</Badge>
                </td>
                <td className={T.hucre} data-etiket={cevir("Boyut")}>
                  <span className="font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{formatBoyut(y.boyut_b)}</span>
                </td>
                <td className={`${T.hucreAksiyon} lg:text-right lg:space-x-1`}>
                  {/* İndirme YEREL dosyayı okur; uzağa taşınmış kayıt
                      indirilemez, açık bırakmak 500 üretirdi. */}
                  <Button
                    onClick={() => indir(y)}
                    disabled={y.mevcut !== 'yerel'}
                    title={y.mevcut === 'yok' ? cevir("Yedek dosyası bulunamadı")
                      : y.mevcut === 'uzak_iddia' ? cevir("Yedek uzak depoya taşınmış — önce geri getirilmeli") : undefined}
                    variant="outlined" boyut="sm" ikon={I.indir}
                    className="disabled:opacity-40"
                  >{cevir("İndir")}</Button>
                  <Button onClick={() => setGeriYukle(y)} color="warning" variant="soft" boyut="sm" ikon={I.geriAl}>{cevir("Geri Yükle")}</Button>
                  <Button onClick={() => setSilinecek(y)} color="error" variant="soft" boyut="sm" ikon={I.cop}>{cevir("Sil")}</Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>}
      </div>

      <ConfirmDialog
        acik={!!silinecek}
        baslik={cevir("Yedek dosyasını sil")}
        mesaj={cevirT(cevir("\"{0}\" silinsin mi?"), silinecek ? `${domain?.alan_adi || hesapAdi(silinecek.dosya)} · ${tarihEtiket(silinecek.olusturma)}` : '')}
        ton="tehlike" onayIkonu={I.cop} onayMetni={cevir("Evet, sil")}
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />

      {geriYukle && id && (
        <RestoreModal
          yedek={geriYukle}
          domainId={id}
          onClose={() => setGeriYukle(null)}
          onDone={(m) => { setBasari(m); toast.basari(cevir("Kaydedildi"), m); setGeriYukle(null); yukle() }}
          onErr={(m) => { setHata(m); toast.hata(cevir("İşlem başarısız"), m) }}
        />
      )}
    </div>
  )
}

// RestoreModal: granüler geri yükleme — tam / yalnız dosyalar / yalnız DB / dosya seç / DB seç.
// Varsayılanlar non-destructive: tam/dosyalar EZMEZ, dosya seçimi ayrı klasöre, DB seçimi yeni ada.
type Icerik = {
  dosyalar: { yol: string; boyut: number; dizin: boolean }[]
  veritabanlari: { ad: string; boyut: number }[]
  kesildi: boolean
}
const MODLAR: { deger: RMod; etiket: string; aciklama: string }[] = [
  { deger: 'tam', etiket: cevir("Tam"), aciklama: cevir("Dosyalar + tüm veritabanları") },
  { deger: 'dosyalar', etiket: cevir("Yalnız Dosyalar"), aciklama: cevir("Veritabanına dokunma") },
  { deger: 'veritabani', etiket: cevir("Yalnız Veritabanı"), aciklama: cevir("Dosyalara dokunma") },
  { deger: 'dosya', etiket: cevir("Dosya Seç"), aciklama: cevir("Belirli dosya/klasörleri geri al") },
  { deger: 'db', etiket: cevir("Veritabanı Seç"), aciklama: cevir("Tek DB — üzerine yaz veya yeni ada") },
]
type RMod = 'tam' | 'dosyalar' | 'veritabani' | 'dosya' | 'db'

function RestoreModal({ yedek, domainId, onClose, onDone, onErr }: {
  yedek: Yedek; domainId: string
  onClose: () => void; onDone: (m: string) => void; onErr: (m: string) => void
}) {
  const [mod, setMod] = useState<RMod>('tam')
  const [temiz, setTemiz] = useState(false)
  const [busy, setBusy] = useState(false)
  const [icerik, setIcerik] = useState<Icerik | null>(null)
  const [icerikYuk, setIcerikYuk] = useState(false)
  const [ara, setAra] = useState('')
  const [secili, setSecili] = useState<Set<string>>(new Set())
  const [hedef, setHedef] = useState<'klasor' | 'yerinde'>('klasor')
  const [db, setDb] = useState('')
  const [dbHedef, setDbHedef] = useState<'ustune' | 'yeni'>('yeni')
  const [yeniDb, setYeniDb] = useState('')

  const granuler = mod === 'dosya' || mod === 'db'
  useEffect(() => {
    let iptal = false
    if (granuler && !icerik && !icerikYuk) {
      setIcerikYuk(true)
      api.get<Icerik>(`/domains/${domainId}/backups/${yedek.id}/icerik`)
        .then(r => { if (iptal) return; setIcerik(r.data) })
        .catch(e => { if (iptal) return; onErr(apiHata(e, cevir("Yedek içeriği okunamadı"))) })
        .finally(() => { if (!iptal) setIcerikYuk(false) })
    }
    return () => { iptal = true }
  }, [mod]) // eslint-disable-line react-hooks/exhaustive-deps

  const filtreli = (() => {
    if (!icerik) return []
    const q = ara.trim().toLowerCase()
    const list = q ? icerik.dosyalar.filter(d => d.yol.toLowerCase().includes(q)) : icerik.dosyalar
    return list.slice(0, 400)
  })()

  function toggle(y: string) {
    setSecili(prev => { const n = new Set(prev); if (n.has(y)) n.delete(y); else n.add(y); return n })
  }

  async function gonder() {
    const body: Record<string, unknown> = { mod }
    if (mod === 'tam' || mod === 'dosyalar') body.temiz = temiz
    if (mod === 'dosya') {
      if (secili.size === 0) { onErr(cevir("En az bir dosya/klasör seçin")); return }
      body.yollar = [...secili]; body.hedef = hedef
    }
    if (mod === 'db') {
      if (!db) { onErr(cevir("Bir veritabanı seçin")); return }
      body.db = db
      if (dbHedef === 'yeni') {
        if (!yeniDb.trim()) { onErr(cevir("Yeni veritabanı adı girin")); return }
        body.hedef_db = yeniDb.trim()
      }
    }
    setBusy(true)
    try {
      const { data } = await api.post(`/domains/${domainId}/backups/${yedek.id}/geriyukle`, body, COK_UZUN_ISTEK)
      let msg = cevir("Geri yükleme tamam")
      if (data.uyari) msg = data.uyari
      else if (typeof data.db === 'string') msg = data.db
      onDone(msg)
    } catch (e) {
      onErr(apiHata(e, cevir("Geri yükleme başarısız")))
    } finally {
      setBusy(false)
    }
  }

  const secBtn = (aktif: boolean) =>
    `text-left rounded-lg border px-3 py-2 transition ${aktif
      ? 'border-primary-600 bg-primary-600 text-white dark:border-primary-500'
      : 'border-slate-200 dark:border-dark-500 bg-white dark:bg-dark-700 hover:border-slate-400 dark:hover:border-dark-400'}`

  return (
    <Modal acik baslik={cevir("Yedekten geri yükle")} onKapat={onClose} genislik="lg" kapatilamaz={busy}
      alt={<>
        <Button onClick={onClose} disabled={busy} variant="outlined" ikon={I.kapat}>{cevir("İptal")}</Button>
        <Button onClick={gonder} yukleniyor={busy} color="primary" ikon={I.geriAl}>
          {busy ? cevir("Geri yükleniyor…") : cevir("Geri Yükle")}
        </Button>
      </>}>
        <div className="space-y-4">
          <p className="-mt-1 text-xs font-mono text-slate-500 dark:text-slate-400 break-all">{yedek.dosya}</p>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            {MODLAR.map(m => (
              <button key={m.deger} type="button" onClick={() => setMod(m.deger)} className={secBtn(mod === m.deger)}>
                <div className="text-sm font-medium">{m.etiket}</div>
                <div className={`text-[11px] mt-0.5 ${mod === m.deger ? 'text-white/75' : 'text-slate-400 dark:text-dark-300'}`}>{m.aciklama}</div>
              </button>
            ))}
          </div>

          {(mod === 'tam' || mod === 'dosyalar') && (
            <UyariKutusu ton="uyari">
              <label className="flex items-start gap-2 cursor-pointer">
                <input type="checkbox" checked={temiz} onChange={e => setTemiz(e.target.checked)} className="mt-0.5" />
                <span className="text-xs">
                  <b>{cevir("Temiz geri yükleme")}</b>{cevir(" — yedekte olmayan dosyaları SİL. Kapalıyken (önerilen) aktif uygulama korunur, yalnız yedektekiler üzerine yazılır.")}
                </span>
              </label>
            </UyariKutusu>
          )}

          {mod === 'veritabani' && (
            <p className="text-xs text-slate-500 dark:text-slate-400 rounded-lg bg-slate-50 dark:bg-dark-700/60 p-3">
              {cevir("Domaine ait tüm veritabanları yedekteki haline döndürülür. Dosyalara dokunulmaz.")}
            </p>
          )}

          {mod === 'dosya' && (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <input value={ara} onChange={e => setAra(e.target.value)} placeholder={cevir("Dosya ara…")}
                  className="flex-1 rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 px-3 py-1.5 text-sm" />
                <span className="text-xs text-slate-400 shrink-0">{secili.size} {cevir('seçili')}</span>
              </div>
              <div className="rounded-lg border border-slate-200 dark:border-dark-600 max-h-56 overflow-y-auto divide-y divide-slate-100 dark:divide-dark-600">
                {icerikYuk && <div className="p-3 text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
                {!icerikYuk && filtreli.map(d => (
                  <label key={d.yol} className="flex items-center gap-2 px-3 py-1.5 text-sm cursor-pointer hover:bg-slate-50 dark:hover:bg-dark-700/50">
                    <input type="checkbox" checked={secili.has(d.yol)} onChange={() => toggle(d.yol)} />
                    <span className={`truncate ${d.dizin ? 'font-medium' : ''}`}>{d.dizin ? <Ikon d={I.klasor} className="inline-block h-3.5 w-3.5 mr-1 align-text-bottom" /> : null}{d.yol}</span>
                  </label>
                ))}
                {!icerikYuk && filtreli.length === 0 && <div className="p-3 text-sm text-slate-400">{cevir("Eşleşme yok")}</div>}
              </div>
              {icerik?.kesildi && <p className="text-[11px] text-slate-400">{cevir("Liste kısaltıldı — daha spesifik aramak için filtreleyin.")}</p>}
              <div className="flex gap-2">
                <button type="button" onClick={() => setHedef('klasor')} className={`flex-1 ${secBtn(hedef === 'klasor')}`}>
                  <div className="text-sm font-medium">{cevir("Ayrı klasöre")}</div>
                  <div className={`text-[11px] ${hedef === 'klasor' ? 'text-white/75' : 'text-slate-400 dark:text-dark-300'}`}>{cevir("geri-yukleme-…/ (hiçbir şey ezilmez)")}</div>
                </button>
                <button type="button" onClick={() => setHedef('yerinde')} className={`flex-1 ${secBtn(hedef === 'yerinde')}`}>
                  <div className="text-sm font-medium">{cevir("Yerinde")}</div>
                  <div className={`text-[11px] ${hedef === 'yerinde' ? 'text-white/75' : 'text-slate-400 dark:text-dark-300'}`}>{cevir("orijinal konuma (üzerine yaz)")}</div>
                </button>
              </div>
            </div>
          )}

          {mod === 'db' && (
            <div className="space-y-2">
              {icerikYuk && <div className="text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
              <div className="rounded-lg border border-slate-200 dark:border-dark-600 divide-y divide-slate-100 dark:divide-dark-600">
                {icerik?.veritabanlari.map(x => (
                  <label key={x.ad} className="flex items-center gap-2 px-3 py-2 text-sm cursor-pointer hover:bg-slate-50 dark:hover:bg-dark-700/50">
                    <input type="radio" name="db" checked={db === x.ad} onChange={() => { setDb(x.ad); setYeniDb(x.ad + '_geri') }} />
                    <span className="font-mono">{x.ad}</span>
                    <span className="ml-auto text-xs text-slate-400">{formatBoyut(x.boyut)}</span>
                  </label>
                ))}
                {!icerikYuk && !icerik?.veritabanlari.length && <div className="p-3 text-sm text-slate-400">{cevir("Yedekte veritabanı yok")}</div>}
              </div>
              {db && (
                <div className="flex flex-col gap-2 rounded-lg bg-slate-50 dark:bg-dark-700/60 p-3">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="dbh" checked={dbHedef === 'yeni'} onChange={() => setDbHedef('yeni')} />
                    {cevir("Yeni veritabanına (güvenli, orijinal korunur)")}
                  </label>
                  {dbHedef === 'yeni' && (
                    <input value={yeniDb} onChange={e => setYeniDb(e.target.value)}
                      className="rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700 px-3 py-1.5 text-sm font-mono ml-6" />
                  )}
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="dbh" checked={dbHedef === 'ustune'} onChange={() => setDbHedef('ustune')} />
                    <span><b className="text-amber-700 dark:text-amber-300">{cevir("Üzerine yaz")}</b>{cevirT(cevir(" — mevcut ‘{0}’ silinip yüklenir"), db)}</span>
                  </label>
                </div>
              )}
            </div>
          )}
        </div>
    </Modal>
  )
}

// kaynakOf: sunucu "kaynak" döndürmeyen eski sürümde tipten türetilir.
function kaynakOf(y: Yedek): 'sistem' | 'manuel' {
  if (y.kaynak) return y.kaynak
  return y.tip === 'oto' || y.tip === 'planli' ? 'sistem' : 'manuel'
}

// hesapAdi: "c_kaan_com-20260831-030125-planli.tar.gz" → "c_kaan_com" (alan adı yüklenmeden önce).
function hesapAdi(dosya: string): string {
  return dosya.replace(/\.tar\.gz$/, '').replace(/(-auto)?-\d{8}-\d{6}.*$/, '')
}

function formatBoyut(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}