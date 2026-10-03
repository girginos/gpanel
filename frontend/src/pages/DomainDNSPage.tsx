import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import ConfirmDialog from '@/components/ConfirmDialog'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import type { Color } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'

type Kayit = {
  id: number
  domain_id: number
  ad: string
  tip: string
  deger: string
  ttl: number
  oncelik: number
  aktif: boolean
  olusturma: string
}

type Domain = { id: number; alan_adi: string; ipv4: string }

type DNSSEC = { aktif: boolean; imzali: boolean; ds: string[]; durum: string }

const TIPLER = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR', 'DS', 'TLSA', 'SSHFP', 'NAPTR']

// Her tip için Değer alanına gösterilecek format ipucu
const DEGER_IPUCU: Record<string, string> = {
  A:     'IPv4 adresi — ör. 203.0.113.10',
  AAAA:  'IPv6 adresi — ör. 2a01:4f8:1c1c::1',
  CNAME: 'Hedef alan adı — ör. hedef.example.com',
  MX:    'Mail sunucusu — ör. mail.example.com (öncelik ayrı alanda)',
  TXT:   'Serbest metin — ör. v=spf1 mx ~all',
  NS:    'Ad sunucusu — ör. ns1.example.com',
  SRV:   'ağırlık port hedef — ör. 5 5060 sip.example.com (öncelik ayrı alanda)',
  CAA:   'flags tag "değer" — ör. 0 issue "letsencrypt.org"',
  PTR:   'Hedef alan adı — ör. host.example.com',
  DS:    'keytag alg digest-tip digest — ör. 12345 13 2 49FD46E6C4B45C55D4AC…',
  TLSA:  'kullanım seçici eşleşme veri — ör. 3 1 1 0B9FA5A59EED715C26C1020C…',
  SSHFP: 'algoritma tip parmak-izi — ör. 4 2 123456789ABCDEF…',
  NAPTR: 'order pref "flags" "servis" "regexp" replacement',
}

// Kayıt tipi rozet rengi — tip grubuna göre (adres / takma ad / posta-servis /
// güvenlik). Renk yalnız yardımcı; tip adı rozet metninde her zaman yazılı.
function tipRenk(tip: string): Color {
  switch (tip) {
    case 'A': case 'AAAA': return 'primary'
    case 'CNAME': case 'PTR': case 'NS': return 'info'
    case 'MX': case 'SRV': case 'NAPTR': return 'secondary'
    case 'CAA': case 'DS': case 'TLSA': case 'SSHFP': return 'success'
    default: return 'neutral'
  }
}


const DNS_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "Birincil NS": "Primary NS",
  "Hostmaster (e-posta)": "Hostmaster (e-mail)",
  "Kaydediliyor…": "Saving…",
  "SOA Kaydet": "Save SOA",
  "SOA kaydedilemedi": "Failed to save SOA",
  "Kopyala": "Copy",
  "Kopyalandı": "Copied",
  "Aktif Yap": "Enable",
  "Pasif Yap": "Disable",
  "Alan adı bilgisi alınamadı": "Failed to get domain info",
  "varsayılan kayıt eklendi": "default records added",
  "kayıt silindi": "records deleted",
  "kayıt": "records",
  "kayıt seçildi": "records selected",
  "Silme başarısız": "Delete failed",
  "Ad (alt-ad)": "Name (sub-name)",
  "Tip": "Type",
  "Kayıt Ekle": "Add Record",
  "Ekle": "Add",
  "Bilgi:": "Info:",
  "Bu sunucu": "This server is",
  "'tir (BIND) — kayıtlar kaydedildiği anda yayınlanır. Domainin çalışması için alan adı operatöründe NS kayıtlarını bu sunucunun": " (BIND) — records are published the instant they are saved. For the domain to work, point the NS records at your domain registrar to this server's",
  "adreslerine yönlendirin.": "addresses.",
  "\"@\" = ana domain, \"www\", \"mail\" vs.": "\"@\" = root domain, \"www\", \"mail\", etc.",
  "Seçilenleri Sil": "Delete Selected",
  "Evet, sil": "Yes, delete",
  "DNSSEC'i kapat": "Disable DNSSEC",
  "DS'i sildim, kapat": "I deleted the DS, disable",
  "İçe aktarma tamam — {0} eklendi, {1} atlandı ({2}){3}": "Import complete — {0} added, {1} skipped ({2}){3}",
  "{0} kayıt {1} yapıldı": "{0} records set to {1}",
  "{0} DNS kaydı kalıcı olarak silinecek. Bu işlem geri alınamaz. Devam edilsin mi?": "{0} DNS records will be permanently deleted. This cannot be undone. Continue?",
  "Evet, {0} kaydı sil": "Yes, delete {0} records",
  "İmzalama sürüyor; DS kaydı henüz hazır değil. Birkaç saniye sonra “Durum”a basın.": "Signing is in progress; the DS record is not ready yet. Press “Status” after a few seconds.",
  "Mevcut kayıtların üzerine yaz — işaretlenmezse birleştirilir (yeni kayıtlar eklenir)": "Overwrite existing records — if unchecked they are merged (new records added)",
  "\"{0} {1} {2}\" silinsin mi?": "Delete \"{0} {1} {2}\"?",
  "Bu domainin TÜM mevcut DNS kayıtları silinip dosyadakilerle değiştirilecek. Önce “Dışa Aktar” ile yedek almanız önerilir.": "ALL current DNS records of this domain will be deleted and replaced with those in the file. Backing up first with “Export” is recommended.",
  "(başlangıç yetki kaydı — refresh/retry/expire/NS)": "(start of authority record — refresh/retry/expire/NS)",
  "A/MX/TXT/NS varsayılan kayıtlarını ekler (idempotent)": "Adds default A/MX/TXT/NS records (idempotent)",
  "Ad sunucusu — ör. ns1.example.com": "Nameserver — e.g. ns1.example.com",
  "Aktarılıyor…": "Transferring…",
  "BIND zone dosyası yükle": "Upload BIND zone file",
  "Bu DS kaydını alan adınızın operatöründe (registrar) DNSSEC/DS alanına girin. Yayılması TTL süresi kadar sürebilir.": "Enter this DS record in the DNSSEC/DS field at your domain registrar. Propagation may take as long as the TTL.",
  "DNS Ayarları": "DNS Settings",
  "DNS Kaydını Düzenle": "Edit DNS Record",
  "DNS kaydını sil": "Delete DNS record",
  "DNS kayıtlarını BIND zone dosyası olarak indir": "Download DNS records as a BIND zone file",
  "DNSSEC durumu alınamadı": "Failed to get DNSSEC status",
  "DNSSEC etkinleştirildi. Aşağıdaki DS kaydını alan adı operatörünüze (registrar) girin.": "DNSSEC enabled. Enter the DS record below at your domain registrar.",
  "DNSSEC güncellenemedi": "Failed to update DNSSEC",
  "DNSSEC kapatıldı.": "DNSSEC disabled.",
  "DS Kaydı — registrar'a girin": "DS Record — enter at registrar",
  "Dışa Aktar": "Export",
  "Dışa aktarma başarısız": "Export failed",
  "Etkinleştiriliyor…": "Enabling…",
  "Hedef alan adı — ör. hedef.example.com": "Target domain — e.g. target.example.com",
  "Hedef alan adı — ör. host.example.com": "Target domain — e.g. host.example.com",
  "Henüz DNS kaydı yok.": "No DNS records yet.",
  "IPv4 adresi — ör. 203.0.113.10": "IPv4 address — e.g. 203.0.113.10",
  "IPv6 adresi — ör. 2a01:4f8:1c1c::1": "IPv6 address — e.g. 2a01:4f8:1c1c::1",
  "Kayıt başarısız": "Failed to save record",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Mail sunucusu — ör. mail.example.com (öncelik ayrı alanda)": "Mail server — e.g. mail.example.com (priority in a separate field)",
  "SOA Ayarları": "SOA Settings",
  "SOA ayarları kaydedildi ve zone yeniden yazıldı.": "SOA settings saved and the zone was rewritten.",
  "SOA kaydı alınamadı": "Failed to get SOA record",
  "Serbest metin — ör. v=spf1 mx ~all": "Free text — e.g. v=spf1 mx ~all",
  "Seçili DNS kayıtlarını sil": "Delete selected DNS records",
  "Seçimi Temizle": "Clear Selection",
  "Standart BIND zone dosyası (.zone / .txt) yükleyin. Kayıtlar bu domaine aktarılır; sonra zone doğrulanıp yeniden yüklenir.": "Upload a standard BIND zone file (.zone / .txt). Records are imported to this domain; then the zone is validated and reloaded.",
  "Toplu güncelleme başarısız": "Bulk update failed",
  "Toplu silme başarısız": "Bulk delete failed",
  "Varsayılan Şablonu Uygula": "Apply Default Template",
  "Yeni DNS Kaydı": "New DNS Record",
  "Zone Dosyası İçe Aktar": "Import Zone File",
  "Zone imzalama (BIND inline-signing). Açtıktan sonra oluşan DS kaydını alan adı operatörünüze girin.": "Zone signing (BIND inline-signing). After enabling, enter the resulting DS record at your domain registrar.",
  "algoritma tip parmak-izi — ör. 4 2 123456789ABCDEF…": "algorithm type fingerprint — e.g. 4 2 123456789ABCDEF…",
  "ağırlık port hedef — ör. 5 5060 sip.example.com (öncelik ayrı alanda)": "weight port target — e.g. 5 5060 sip.example.com (priority in a separate field)",
  "değer": "value",
  "imzalanıyor…": "signing…",
  "imzalı": "signed",
  "kapalı": "off",
  "keytag alg digest-tip digest — ör. 12345 13 2 49FD46E6C4B45C55D4AC…": "keytag alg digest-type digest — e.g. 12345 13 2 49FD46E6C4B45C55D4AC…",
  "kullanım seçici eşleşme veri — ör. 3 1 1 0B9FA5A59EED715C26C1020C…": "usage selector matching data — e.g. 3 1 1 0B9FA5A59EED715C26C1020C…",
  "ÖNEMLİ: DNSSEC'i kapatmadan ÖNCE alan adı operatörünüzden (registrar) DS kaydını SİLİN ve TTL süresi kadar bekleyin. Aksi halde alan adınız çözümlenemez hale gelir (SERVFAIL). DS kaydını sildiyseniz devam edin.": "IMPORTANT: BEFORE disabling DNSSEC, DELETE the DS record at your domain registrar and wait for the TTL. Otherwise your domain becomes unresolvable (SERVFAIL). If you have deleted the DS record, continue.",
  "Öncelik": "Priority",
  "İçe Aktar": "Import",
  "İçe aktarma başarısız": "Import failed",
  "Şablon Uygula": "Apply Template",
  "Şablon uygulanamadı": "Failed to apply template",
  "düzenle": "edit",
  "gizle": "hide",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DNS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainDNSPage() {
  useTranslation() // dil re-render aboneligi
  const { bilgi } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [kayitlar, setKayitlar] = useState<Kayit[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [duzenle, setDuzenle] = useState<Kayit | null>(null)
  const [silinecek, setSilinecek] = useState<Kayit | null>(null)
  const [secili, setSecili] = useState<Set<number>>(new Set())
  const [topluSilOnay, setTopluSilOnay] = useState(false)
  const [soa, setSoa] = useState<{ primary_ns: string; hostmaster: string; refresh: number; retry: number; expire: number; minimum: number; ttl: number } | null>(null)
  const [soaAcik, setSoaAcik] = useState(false)
  const [soaKaydediyor, setSoaKaydediyor] = useState(false)
  const [dnssec, setDnssec] = useState<DNSSEC | null>(null)
  const [dnssecIsliyor, setDnssecIsliyor] = useState(false)
  const [dnssecKapatOnay, setDnssecKapatOnay] = useState(false)
  const [dsKopyalandi, setDsKopyalandi] = useState(false)
  const [iceAcik, setIceAcik] = useState(false)
  const [iceDosya, setIceDosya] = useState<File | null>(null)
  const [iceDegistir, setIceDegistir] = useState(false)
  const [iceYuk, setIceYuk] = useState(false)

  async function disaAktar() {
    try {
      const r = await api.get(`/domains/${id}/dns/disa-aktar`, { responseType: 'blob' })
      const url = URL.createObjectURL(r.data as Blob)
      const a = document.createElement('a')
      a.href = url; a.download = `${domain?.alan_adi || 'zone'}.zone`
      document.body.appendChild(a); a.click(); a.remove()
      URL.revokeObjectURL(url)
    } catch (e) {
      const m = apiHata(e, cevir("Dışa aktarma başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  async function iceAktar() {
    if (!iceDosya) return
    setIceYuk(true); setHata(null); setBasari(null)
    try {
      const fd = new FormData(); fd.append('dosya', iceDosya)
      const mod = iceDegistir ? 'degistir' : 'birlestir'
      const { data } = await api.post<{ eklenen: number; atlanan: number; mod: string; uyari?: string }>(`/domains/${id}/dns/ice-aktar?mod=${mod}`, fd, UZUN_ISTEK)
      const m = cevirT(cevir("İçe aktarma tamam — {0} eklendi, {1} atlandı ({2}){3}"), data.eklenen, data.atlanan, data.mod, data.uyari ? ' · ' + data.uyari : '')
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
      setIceAcik(false); setIceDosya(null); setIceDegistir(false)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("İçe aktarma başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally { setIceYuk(false) }
  }

  const yukleNesli = useRef(0)
  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    const _n = ++yukleNesli.current
    api.get<Kayit[]>(`/domains/${id}/dns`)
      .then(r => { if (_n !== yukleNesli.current) return; setKayitlar(r.data); setSecili(new Set()) })
      .catch(e => {
        if (_n !== yukleNesli.current) return
        const m = apiHata(e)
        setHata(m)
        toast.hata(cevir("İşlem başarısız"), m)
      })
      .finally(() => { if (_n === yukleNesli.current) setYuk(false) })
  }

  function secimDegistir(rid: number) {
    setSecili(prev => {
      const n = new Set(prev)
      if (n.has(rid)) n.delete(rid); else n.add(rid)
      return n
    })
  }
  function hepsiniSec() {
    setSecili(prev => prev.size === kayitlar.length ? new Set() : new Set(kayitlar.map(k => k.id)))
  }

  async function topluSil() {
    if (!id || secili.size === 0) return
    setHata(null); setBasari(null); setTopluSilOnay(false)
    try {
      const { data } = await api.post(`/domains/${id}/dns/toplu-sil`, { ids: [...secili] })
      setBasari(`${data.silinen} ${cevir("kayıt silindi")}`)
      toast.basari(`${data.silinen} ${cevir("kayıt silindi")}`)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Toplu silme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }
  async function topluDurum(aktif: boolean) {
    if (!id || secili.size === 0) return
    setHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/domains/${id}/dns/toplu-durum`, { ids: [...secili], aktif })
      const m = cevirT(cevir("{0} kayıt {1} yapıldı"), data.guncellenen, aktif ? 'aktif' : 'pasif')
      setBasari(m)
      toast.basari(m)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Toplu güncelleme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }
  useEffect(() => {
    let iptal = false
    if (id) {
      api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("Alan adı bilgisi alınamadı"))(e) })
      api.get<typeof soa>(`/domains/${id}/dns/soa`).then(r => { if (iptal) return; setSoa(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("SOA kaydı alınamadı"))(e) })
      api.get<DNSSEC>(`/domains/${id}/dns/dnssec`).then(r => { if (iptal) return; setDnssec(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("DNSSEC durumu alınamadı"))(e) })
    }
    yukle()
    return () => { iptal = true; yukleNesli.current++ }
  }, [id])

  async function dnssecDegistir(aktif: boolean) {
    if (!id) return
    setHata(null); setBasari(null); setDnssecKapatOnay(false); setDnssecIsliyor(true)
    try {
      const { data } = await api.post<DNSSEC>(`/domains/${id}/dns/dnssec`, { aktif }, UZUN_ISTEK)
      setDnssec(data)
      const m = aktif
        ? cevir("DNSSEC etkinleştirildi. Aşağıdaki DS kaydını alan adı operatörünüze (registrar) girin.")
        : cevir("DNSSEC kapatıldı.")
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
    } catch (e) {
      const m = apiHata(e, cevir("DNSSEC güncellenemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setDnssecIsliyor(false) }
  }
  async function dnssecDurumYenile() {
    if (!id) return
    try { const { data } = await api.get<DNSSEC>(`/domains/${id}/dns/dnssec`); setDnssec(data) } catch { /* yut */ }
  }

  async function soaKaydet(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !soa) return
    setHata(null); setBasari(null); setSoaKaydediyor(true)
    try {
      const { data } = await api.put(`/domains/${id}/dns/soa`, soa)
      setSoa(data)
      setBasari(cevir("SOA ayarları kaydedildi ve zone yeniden yazıldı."))
      toast.basari(cevir("Kaydedildi"), cevir("SOA ayarları kaydedildi ve zone yeniden yazıldı."))
    } catch (e) {
      const m = apiHata(e, cevir("SOA kaydedilemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setSoaKaydediyor(false) }
  }

  async function sablonUygula() {
    if (!id) return
    setHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/domains/${id}/dns/sablon`, undefined, UZUN_ISTEK)
      setBasari(`${data.eklenen} ${cevir("varsayılan kayıt eklendi")}`)
      toast.basari(`${data.eklenen} ${cevir("varsayılan kayıt eklendi")}`)
      yukle()
    } catch (e) {
      const m = apiHata(e, cevir("Şablon uygulanamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  async function sil() {
    if (!silinecek || !id) return
    try {
      await api.delete(`/domains/${id}/dns/${silinecek.id}`)
      setSilinecek(null); yukle()
    } catch (e) {
      (await bilgi({ baslik: 'Bilgi', mesaj: apiHata(e, cevir("Silme başarısız")), ton: 'tehlike' }))
    }
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("DNS Ayarları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("DNS Ayarları")}</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}IP: <span className="font-mono">{domain.ipv4}</span>
        </p>
      )}

      <UyariKutusu ton="bilgi" className="mb-4" kucuk>
        <strong>{cevir("Bilgi:")}</strong> {cevir("Bu sunucu")} <strong>authoritative DNS</strong>{cevir("'tir (BIND) — kayıtlar kaydedildiği anda yayınlanır. Domainin çalışması için alan adı operatöründe NS kayıtlarını bu sunucunun")} <span className="font-mono">ns1.{domain?.alan_adi || 'alaniniz'}</span> / <span className="font-mono">ns2.{domain?.alan_adi || 'alaniniz'}</span> {cevir("adreslerine yönlendirin.")}
      </UyariKutusu>

      {soa && (
        <div className="border border-slate-200 dark:border-dark-600 rounded-lg mb-4 overflow-hidden">
          <button type="button" onClick={() => setSoaAcik(v => !v)} aria-expanded={soaAcik} className="w-full flex items-center justify-between px-4 py-2.5 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-dark-700/50 transition">
            <span className="inline-flex items-center gap-1.5"><Ikon d={I.ayar} /> {cevir("SOA Ayarları")} <span className="text-xs text-slate-400 font-normal">{cevir("(başlangıç yetki kaydı — refresh/retry/expire/NS)")}</span></span>
            <span className="inline-flex items-center gap-1 text-slate-400 text-xs"><Ikon d={soaAcik ? I.yukari : I.asagi} className="size-3.5 shrink-0" />{soaAcik ? cevir("gizle") : cevir("düzenle")}</span>
          </button>
          {soaAcik && (
            <form onSubmit={soaKaydet} className="px-4 pb-4 pt-3 grid grid-cols-2 md:grid-cols-4 gap-3 border-t border-slate-100 dark:border-dark-600">
              <label className="col-span-2">
                <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{cevir("Birincil NS")}</span>
                <input value={soa.primary_ns} onChange={e => setSoa({ ...soa, primary_ns: e.target.value })}
                  className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded text-sm font-mono outline-none focus:border-brand-500" />
              </label>
              <label className="col-span-2">
                <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{cevir("Hostmaster (e-posta)")}</span>
                <input value={soa.hostmaster} onChange={e => setSoa({ ...soa, hostmaster: e.target.value })} placeholder="admin@alan.com"
                  className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded text-sm font-mono outline-none focus:border-brand-500" />
              </label>
              {(['refresh', 'retry', 'expire', 'minimum', 'ttl'] as const).map(f => (
                <label key={f}>
                  <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{f} (sn)</span>
                  <input type="number" min={0} value={soa[f]} onChange={e => setSoa({ ...soa, [f]: parseInt(e.target.value) || 0 })}
                    className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded text-sm font-mono outline-none focus:border-brand-500" />
                </label>
              ))}
              <div className="col-span-2 md:col-span-4 flex justify-end">
                <Button type="submit" color="primary" yukleniyor={soaKaydediyor} ikon={I.disket}>
                  {soaKaydediyor ? cevir("Kaydediliyor…") : cevir("SOA Kaydet")}
                </Button>
              </div>
            </form>
          )}
        </div>
      )}

      {dnssec && (
        <div className="border border-slate-200 dark:border-dark-600 rounded-lg mb-4 overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3 gap-3 flex-wrap">
            <div>
              <div className="text-sm font-medium text-slate-700 dark:text-slate-200 flex flex-wrap items-center gap-2">
                <Ikon d={I.kilit} /> DNSSEC
                {dnssec.aktif ? (
                  dnssec.imzali
                    ? <Badge color="success" variant="soft">{cevir("imzalı")}</Badge>
                    : <Badge color="warning" variant="soft">{cevir("imzalanıyor…")}</Badge>
                ) : (
                  <Badge variant="soft">{cevir("kapalı")}</Badge>
                )}
              </div>
              <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{cevir("Zone imzalama (BIND inline-signing). Açtıktan sonra oluşan DS kaydını alan adı operatörünüze girin.")}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {dnssec.aktif && (
                <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={dnssecDurumYenile}>{cevir("Durum")}</Button>
              )}
              {dnssec.aktif ? (
                <Button color="error" variant="soft" boyut="sm" ikon={I.ban} yukleniyor={dnssecIsliyor} onClick={() => setDnssecKapatOnay(true)}>{cevir("Kapat")}</Button>
              ) : (
                <Button color="primary" boyut="sm" ikon={I.onay} yukleniyor={dnssecIsliyor} onClick={() => dnssecDegistir(true)}>{dnssecIsliyor ? cevir("Etkinleştiriliyor…") : cevir("Etkinleştir")}</Button>
              )}
            </div>
          </div>
          {dnssec.aktif && (
            <div className="px-4 pb-4 border-t border-slate-100 dark:border-dark-600 pt-3">
              {dnssec.ds && dnssec.ds.length > 0 ? (
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">{cevir("DS Kaydı — registrar'a girin")}</div>
                  {dnssec.ds.map((d, i) => (
                    <div key={i} className="flex items-center gap-2 mb-1">
                      <code className="flex-1 text-xs font-mono bg-slate-50 dark:bg-dark-800 border border-slate-200 dark:border-dark-600 rounded px-2 py-1 break-all text-slate-800 dark:text-slate-200">{d}</code>
                      <Button variant="outlined" boyut="sm" ikon={dsKopyalandi ? I.onay : I.kopya} onClick={() => { navigator.clipboard?.writeText(d); setDsKopyalandi(true); setTimeout(() => setDsKopyalandi(false), 1500) }}
                        className="shrink-0 whitespace-nowrap">{dsKopyalandi ? cevir("Kopyalandı") : cevir("Kopyala")}</Button>
                    </div>
                  ))}
                  <p className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">{cevir("Bu DS kaydını alan adınızın operatöründe (registrar) DNSSEC/DS alanına girin. Yayılması TTL süresi kadar sürebilir.")}</p>
                </div>
              ) : (
                <p className="text-xs text-amber-600 dark:text-amber-400">{cevir("İmzalama sürüyor; DS kaydı henüz hazır değil. Birkaç saniye sonra “Durum”a basın.")}</p>
              )}
              {dnssec.durum && (
                <pre className="mt-2 text-[10px] font-mono text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-dark-800 border border-slate-200 dark:border-dark-600 rounded p-2 overflow-x-auto max-h-44">{dnssec.durum}</pre>
              )}
            </div>
          )}
        </div>
      )}

      <div className="grid grid-cols-2 gap-2 mb-4 sm:flex sm:flex-wrap sm:items-center">
        <Button
          color="primary"
          onClick={() => setDuzenle({} as Kayit)}
          ikon={I.arti}
          className="col-span-2 sm:col-auto whitespace-nowrap"
        >
          {cevir(cevir("Kayıt Ekle"))}
        </Button>
        <Button
          variant="outlined"
          onClick={sablonUygula}
          ikon={I.pano}
          className="whitespace-nowrap"
          title={cevir("A/MX/TXT/NS varsayılan kayıtlarını ekler (idempotent)")}
        >
          <span className="sm:hidden">{cevir("Şablon Uygula")}</span>
          <span className="hidden sm:inline">{cevir("Varsayılan Şablonu Uygula")}</span>
        </Button>
        <Button variant="outlined" onClick={yukle} ikon={I.yenile} yukleniyor={yuk} className="whitespace-nowrap">{cevir("Yenile")}</Button>
        <Button variant="outlined" onClick={disaAktar} ikon={I.indir} title={cevir("DNS kayıtlarını BIND zone dosyası olarak indir")} className="whitespace-nowrap">{cevir("Dışa Aktar")}</Button>
        <Button variant="outlined" onClick={() => setIceAcik(true)} ikon={I.yukle} title={cevir("BIND zone dosyası yükle")} className="whitespace-nowrap">{cevir("İçe Aktar")}</Button>
        <span className="col-span-2 text-right sm:col-auto sm:ml-auto sm:text-left text-sm text-slate-500 dark:text-slate-500">{kayitlar.length} {cevir("kayıt")}</span>
      </div>

      {iceAcik && (
        <Modal acik={true} baslik={cevir("Zone Dosyası İçe Aktar")} onKapat={() => setIceAcik(false)} genislik="md" kapatilamaz={iceYuk}
          alt={<>
            <Button variant="outlined" onClick={() => setIceAcik(false)} disabled={iceYuk} ikon={I.kapat}>{cevir("İptal")}</Button>
            <Button color="primary" disabled={!iceDosya} yukleniyor={iceYuk} onClick={iceAktar} ikon={I.yukle}>{iceYuk ? cevir("Aktarılıyor…") : cevir("İçe Aktar")}</Button>
          </>}>
          <div className="space-y-4">
            <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Standart BIND zone dosyası (.zone / .txt) yükleyin. Kayıtlar bu domaine aktarılır; sonra zone doğrulanıp yeniden yüklenir.")}</p>
            <input type="file" accept=".zone,.txt,.db,.bind,text/plain" onChange={e => setIceDosya(e.target.files?.[0] || null)}
              className="block w-full text-sm text-slate-600 dark:text-slate-300 file:mr-3 file:px-3 file:py-2 file:rounded-lg file:border-0 file:font-medium file:bg-gray-150 file:text-gray-900 hover:file:bg-gray-200 dark:file:bg-surface-2 dark:file:text-dark-50 dark:hover:file:bg-surface-1 file:cursor-pointer" />
            <label className="flex items-center gap-2 text-sm text-slate-600 dark:text-slate-300 cursor-pointer">
              <input type="checkbox" checked={iceDegistir} onChange={e => setIceDegistir(e.target.checked)} className="rounded" />
              {cevir(cevir("Mevcut kayıtların üzerine yaz — işaretlenmezse birleştirilir (yeni kayıtlar eklenir)"))}
            </label>
            {iceDegistir && <UyariKutusu ton="uyari">{cevir("Bu domainin TÜM mevcut DNS kayıtları silinip dosyadakilerle değiştirilecek. Önce “Dışa Aktar” ile yedek almanız önerilir.")}</UyariKutusu>}
          </div>
        </Modal>
      )}
      {secili.size > 0 && (
        <div className="mb-3 px-3 py-2 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-md flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-brand-800 dark:text-brand-200">{secili.size} {cevir("kayıt seçildi")}</span>
          <div className="ml-auto flex items-center gap-2 flex-wrap">
            <Button color="success" variant="outlined" boyut="sm" ikon={I.onay} onClick={() => topluDurum(true)}>{cevir("Aktif Yap")}</Button>
            <Button variant="outlined" boyut="sm" ikon={I.durakla} onClick={() => topluDurum(false)}>{cevir("Pasif Yap")}</Button>
            <Button color="error" boyut="sm" ikon={I.cop} onClick={() => setTopluSilOnay(true)}>{cevir("Seçilenleri Sil")} ({secili.size})</Button>
            <Button variant="flat" boyut="sm" ikon={I.kapat} onClick={() => setSecili(new Set())}>{cevir("Seçimi Temizle")}</Button>
          </div>
        </div>
      )}

      {/* Kapsayıcı çerçeve yalnız masaüstünde; mobilde satırlar kart olduğu için
          ikinci bir çerçeve iç içe görünürdü. */}
      <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
        {yuk ? (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
        ) : kayitlar.length === 0 ? (
          <div className="py-12 text-center">
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-3">{cevir("Henüz DNS kaydı yok.")}</p>
            <Button color="primary" onClick={sablonUygula} ikon={I.pano}>
              {cevir("Varsayılan Şablonu Uygula")}
            </Button>
          </div>
        ) : (
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
                <tr>
                  <th className={`${T.baslik} w-10 text-center`}>
                    <input type="checkbox" aria-label={cevir("Tümünü seç")} checked={kayitlar.length > 0 && secili.size === kayitlar.length}
                      ref={el => { if (el) el.indeterminate = secili.size > 0 && secili.size < kayitlar.length }}
                      onChange={hepsiniSec} className="rounded border-slate-300 dark:border-slate-600 cursor-pointer" />
                  </th>
                  <th className={`${T.baslik} lg:min-w-[190px] lg:whitespace-nowrap`}>{cevir("Ad")}</th>
                  <th className={`${T.baslik} lg:whitespace-nowrap`}>{cevir("Tip")}</th>
                  <th className={`${T.baslik} lg:w-full`}>{cevir("Değer")}</th>
                  <th className={`${T.baslik} lg:whitespace-nowrap`}>TTL</th>
                  <th className={`${T.baslik} lg:whitespace-nowrap`}>{cevir("Öncelik")}</th>
                  <th className={`${T.baslik} lg:whitespace-nowrap`}>{cevir("Durum")}</th>
                  <th className={`${T.baslik} text-right`}>{cevir("İşlemler")}</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {kayitlar.map(k => (
                  <tr key={k.id} className={`${T.satir} ${secili.has(k.id) ? 'ring-2 ring-brand-400 lg:ring-0 bg-brand-50/60 dark:bg-brand-900/10' : 'lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700/60'}`}>
                    <td className={T.hucreSecim}>
                      <input type="checkbox" aria-label={cevirT("{0} {1} seç", k.ad, k.tip)} checked={secili.has(k.id)} onChange={() => secimDegistir(k.id)}
                        className="rounded border-slate-300 dark:border-slate-600 cursor-pointer" />
                    </td>
                    {/* Birincil tanımlayıcı: kayıt adı — mobilde kart başlığı olur. */}
                    <td className={`${T.hucreBaslik} font-mono break-all lg:break-normal lg:whitespace-nowrap`}>{k.ad}</td>
                    <td className={T.hucre} data-etiket={cevir("Tip")}>
                      <Badge color={tipRenk(k.tip)} variant="soft" className="font-mono font-semibold">{k.tip}</Badge>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Değer")}>
                      <span className="text-sm font-mono text-slate-800 dark:text-slate-200 break-all text-right lg:text-left">{k.deger}</span>
                    </td>
                    <td className={T.hucre} data-etiket="TTL">
                      <span className="text-sm font-mono text-slate-600 dark:text-slate-400">{k.ttl}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Öncelik")}>
                      <span className="text-sm font-mono text-slate-600 dark:text-slate-400">{k.tip === 'MX' || k.tip === 'SRV' ? k.oncelik : '—'}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Durum")}>
                      {k.aktif ? (
                        <Badge color="success" variant="soft" className="gap-1"><span className="size-1.5 rounded-full bg-current" aria-hidden="true" />{cevir("Aktif")}</Badge>
                      ) : (
                        <Badge variant="soft">{cevir("Pasif")}</Badge>
                      )}
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right lg:space-x-1`}>
                      <Button variant="flat" boyut="sm" ikon={I.kalem} onClick={() => setDuzenle(k)}>{cevir("Düzenle")}</Button>
                      <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => setSilinecek(k)}>{cevir("Sil")}</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {duzenle && (
        <KayitModal
          mevcut={duzenle}
          domainId={Number(id)}
          ipv4={domain?.ipv4 || ''}
          onKapat={() => setDuzenle(null)}
          onKayit={() => { setDuzenle(null); yukle() }}
        />
      )}

      <ConfirmDialog
        acik={!!silinecek}
        baslik={cevir("DNS kaydını sil")}
        mesaj={cevirT(cevir("\"{0} {1} {2}\" silinsin mi?"), silinecek?.ad, silinecek?.tip, silinecek?.deger.slice(0,40))}
        ton="tehlike"
        onayIkonu={I.cop}
        onayMetni={cevir("Evet, sil")}
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />

      <ConfirmDialog
        acik={topluSilOnay}
        baslik={cevir("Seçili DNS kayıtlarını sil")}
        mesaj={cevirT(cevir("{0} DNS kaydı kalıcı olarak silinecek. Bu işlem geri alınamaz. Devam edilsin mi?"), secili.size)}
        ton="tehlike"
        onayIkonu={I.cop}
        onayMetni={cevirT(cevir("Evet, {0} kaydı sil"), secili.size)}
        onOnay={topluSil}
        onIptal={() => setTopluSilOnay(false)}
      />

      <ConfirmDialog
        acik={dnssecKapatOnay}
        baslik={cevir("DNSSEC'i kapat")}
        mesaj={cevir("ÖNEMLİ: DNSSEC'i kapatmadan ÖNCE alan adı operatörünüzden (registrar) DS kaydını SİLİN ve TTL süresi kadar bekleyin. Aksi halde alan adınız çözümlenemez hale gelir (SERVFAIL). DS kaydını sildiyseniz devam edin.")}
        ton="tehlike"
        onayIkonu={I.ban}
        onayMetni={cevir("DS'i sildim, kapat")}
        onOnay={() => dnssecDegistir(false)}
        onIptal={() => setDnssecKapatOnay(false)}
      />
    </div>
  )
}

function KayitModal({ mevcut, domainId, ipv4, onKapat, onKayit }: {
  mevcut: Kayit; domainId: number; ipv4: string; onKapat: () => void; onKayit: () => void
}) {
  const toast = useToast()
  const yeni = !mevcut.id
  const [form, setForm] = useState<Kayit>({
    id: mevcut.id || 0,
    domain_id: domainId,
    ad: mevcut.ad || '@',
    tip: mevcut.tip || 'A',
    deger: mevcut.deger || ipv4,
    ttl: mevcut.ttl || 3600,
    oncelik: mevcut.oncelik || 0,
    aktif: mevcut.aktif !== false,
    olusturma: '',
  })
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      if (yeni) await api.post(`/domains/${domainId}/dns`, form)
      else      await api.put(`/domains/${domainId}/dns/${form.id}`, form)
      onKayit()
    } catch (e) {
      const m = apiHata(e, cevir("Kayıt başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={yeni ? cevir("Yeni DNS Kaydı") : cevir("DNS Kaydını Düzenle")} onKapat={onKapat} genislik="md" kapatilamaz={isleniyor}
      alt={<>
        <Button type="button" variant="outlined" onClick={onKapat} disabled={isleniyor} ikon={I.kapat}>{cevir("İptal")}</Button>
        <Button type="submit" form="dns-kayit-formu" color="primary" disabled={!form.deger.trim()} yukleniyor={isleniyor} ikon={yeni ? I.arti : I.disket}>{isleniyor ? cevir("Kaydediliyor…") : (yeni ? cevir("Ekle") : cevir("Güncelle"))}</Button>
      </>}>
      <form id="dns-kayit-formu" onSubmit={gonder} className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <div className="col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Ad (alt-ad)")}</label>
            <input type="text" value={form.ad} onChange={e => setForm({ ...form, ad: e.target.value })} required
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            <p className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5">{cevir("\"@\" = ana domain, \"www\", \"mail\" vs.")}</p>
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Tip")}</label>
            <select value={form.tip} onChange={e => { const t = e.target.value; setForm(f => ({ ...f, tip: t, oncelik: (t === 'MX' || t === 'SRV') ? (f.oncelik || 10) : 0 })) }}
              className="w-full px-2 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono bg-white dark:bg-dark-700">
              {TIPLER.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
          </div>
        </div>

        <div>
          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Değer")}</label>
          <input type="text" value={form.deger} onChange={e => setForm({ ...form, deger: e.target.value })} required
            className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          {DEGER_IPUCU[form.tip] && <p className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5">{DEGER_IPUCU[form.tip]}</p>}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">TTL (sn)</label>
            <input type="number" min={60} value={form.ttl} onChange={e => setForm({ ...form, ttl: parseInt(e.target.value) || 3600 })}
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono" />
          </div>
          {(form.tip === 'MX' || form.tip === 'SRV') && (
            <div>
              <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{cevir("Öncelik")}</label>
              <input type="number" min={0} value={form.oncelik} onChange={e => setForm({ ...form, oncelik: parseInt(e.target.value) || 0 })}
                className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono" />
            </div>
          )}
        </div>

        <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
          <input type="checkbox" checked={form.aktif} onChange={e => setForm({ ...form, aktif: e.target.checked })} className="rounded" />
          {cevir("Aktif")}
        </label>

        {hata && <UyariKutusu ton="tehlike">{hata}</UyariKutusu>}
      </form>
    </Modal>
  )
}