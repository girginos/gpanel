package arsiv

import "strings"

// profil.go — faz 2 kullanım profilleri (sınırlar) ve manifestten arşiv üye adı türeten
// yardımcılar. Hiçbiri diske/ağa dokunmaz.

// Arşiv dosyası boyut tavanları (kullanıcı kararı): sunucudaki hazır dosya yolu 500 GiB,
// panel üzerinden yükleme 50 GiB. Dosya boyutu çağıranca (tasima) denetlenir; burada bu
// tavanlara göre AÇILMIŞ boyut / üye / üst veri sınırları türetilir.
const (
	YolTavan     int64 = 500 << 30
	YuklemeTavan int64 = 50 << 30
)

// acilimCarpani: açılmış toplam, arşiv dosyası tavanının en çok bu katı olabilir. Site
// verisi (PHP, görsel, zaten sıkışmış medya, sıkışmış yedekler) gzip'te çoğunlukla 1-4×
// açılır; 4× makul tavandır. Bomba koruması AYRICA sürer: oran sınırı (BombaOrani), her
// geçişte arşiv dosyasının KENDİ boyutuna göre denetlenir (düşük sıkıştırma beklenen
// meşru yedekte 500× dolaylı olarak hiç yaklaşılmaz; bomba ToplamBayt'a takılır).
const acilimCarpani = 4

// SecenekYol: sunucudaki hazır dosya yolu için Oku seçenekleri (arşiv ≤ YolTavan).
func SecenekYol() Secenek {
	return Secenek{
		Sinirlar: Sinirlar{
			ToplamBayt: YolTavan * acilimCarpani,
			UyeSayisi:  1_500_000,
			AdBayt:     128 << 20,
			// UstVeriBayt, BombaOrani, BombaEsik, DiskPayi, BagSayisi, BagBilesen: varsayılan.
		},
		DumpSinir: 64 << 30,
	}
}

// SecenekYukleme: bayi/kök yüklemesi için Oku seçenekleri (arşiv ≤ YuklemeTavan).
func SecenekYukleme() Secenek {
	return Secenek{
		Sinirlar: Sinirlar{
			ToplamBayt: YuklemeTavan * acilimCarpani,
			UyeSayisi:  600_000,
			AdBayt:     64 << 20,
		},
		DumpSinir: 32 << 30,
	}
}

// EvOnek: alanın "ev" ağacının arşiv üye öneki. cPanel: <kök>/homedir (tüm hesap evi);
// DirectAdmin: domains/<alan> (evin karşılığı alan dizinidir; docroot onun altındadır).
func (m *Manifest) EvOnek(alan string) string {
	if m.Tur == TurCPanel {
		return m.Kok + "/homedir"
	}
	return "domains/" + alan
}

// DosyaOnek: belge kökünün (Alan.BelgeKoku) arşiv üye öneki; belge kökü yoksa "".
func (m *Manifest) DosyaOnek(belgeKoku string) string {
	if belgeKoku == "" {
		return ""
	}
	if m.Tur == TurCPanel {
		return m.Kok + "/homedir/" + belgeKoku
	}
	return belgeKoku
}

// PostaOnek: kutunun maildir'inin arşiv üye öneki; yedekte yoksa "". DirectAdmin'de
// kutu dizininin altında "Maildir" varsa o seçilir (biçim doğrulanmadı: iki yerleşim de denenir).
func (m *Manifest) PostaOnek(a *Arsiv, p PostaHesabi) string {
	var on string
	if m.Tur == TurCPanel {
		on = m.Kok + "/homedir/mail/" + p.Alan + "/" + p.Yerel
	} else {
		on = "imap/" + p.Alan + "/" + p.Yerel
		if uyeVar(a, on+"/Maildir") {
			on += "/Maildir"
		}
	}
	if !uyeVar(a, on) {
		return ""
	}
	return on
}

// DNSUye: cPanel bölge dosyasının üst veri anahtarı ("" = DirectAdmin: bölge okunmaz).
func (m *Manifest) DNSUye(alan string) string {
	if m.Tur != TurCPanel {
		return ""
	}
	return m.Kok + "/dnszones/" + alan + ".db"
}

// Var: ad'ın kendisi ya da altında bir üye var mı (envanter; diske dokunmaz).
func (a *Arsiv) Var(ad string) bool { return uyeVar(a, strings.TrimPrefix(ad, "./")) }

// PostaAlanOnek: alanın TÜM kutularının üst dizini (tek geçişte çıkarmak için).
func (m *Manifest) PostaAlanOnek(alan string) string {
	if m.Tur == TurCPanel {
		return m.Kok + "/homedir/mail/" + alan
	}
	return "imap/" + alan
}

// UstVeriAl: bellekte tutulan üst veri dosyası (hesap kaydı, bölge, passwd, cron...).
func (a *Arsiv) UstVeriAl(ad string) ([]byte, bool) {
	b, ok := a.UstVeri[strings.TrimPrefix(ad, "./")]
	return b, ok
}
