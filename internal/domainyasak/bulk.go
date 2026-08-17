package domainyasak

// Toplu ekleme — kullanıcı bir metin bloğu (textarea veya .txt upload) verir,
// backend bunu satır satır parse edip her geçerli domain için UPSERT eder.
//
// Kabul edilen ayırıcılar: satır sonu, virgül, noktalı virgül, sekme, boşluk.
// Bu, kullanıcının başka bir yerden (blocklist repo'su, spreadsheet, e-posta)
// yapıştırdığı listeye tolerant olmayı sağlar.
//
// 🔴 Kısıtlar:
//   * En fazla 5000 domain / istek — kötü niyetli veya kazayla milyonlarca
//     kayıt gönderilirse hem DB'yi hem cache'i şişirir.
//   * Yorum satırı `#` ile başlayan satırlar atlanır (hosts-file uyumlu).
//   * Her domain aynı description ve match_subdomains değerini alır
//     (form üstünden tek seferde belirlenir).
//   * Aynı domain listede birden çok kez varsa TEK sefer işlenir (dedup).

import (
	"encoding/json"
	"net/http"
	"strings"
)

const bulkMaxKayit = 5000

type bulkGovde struct {
	// Ham metin — satır/virgül/boşluk ile ayrılabilir. Dosya upload'ı da
	// istemci tarafından okunup buraya string olarak gönderilir; bu, çok
	// tenanth server'da bekleyen multipart handler'ı gerektirmez.
	Domains         string `json:"domains"`
	Description     string `json:"description"`
	MatchSubdomains *bool  `json:"match_subdomains"`
}

type bulkHata struct {
	Domain string `json:"domain"`
	Sebep  string `json:"sebep"`
}

type bulkYanit struct {
	Toplam    int        `json:"toplam"`
	Islendi   int        `json:"islendi"` // upsert başarılı (yeni + güncellenmiş)
	Yoksayild int        `json:"yoksayildi"` // boş / yorum satırı / duplicate
	Basarisiz []bulkHata `json:"basarisiz"`
}

// tokenAyir — İKİ AŞAMA:
//  1. Önce SATIR bazında böl (\n, \r). Yorum ve boş satırlar burada elenir —
//     "# bu yorumdur" satırı bir bütün, sonraki adımda kelime kelime bölünmez.
//  2. Kalan her satırı virgül/noktalı virgül/sekme/boşluk ile parçala.
//
// Yorum konsepti "satır #-ile-başlar" — dolayısıyla ayrıştırıcı boşluğu
// yorum sınırı olarak GÖREMEZ. Bunu bir arada yapan tek FieldsFunc yanlış
// sonuç veriyordu: canlı testte yakalandı.
func tokenAyir(ham string) []string {
	var out []string
	for _, satir := range strings.Split(ham, "\n") {
		satir = strings.TrimRight(satir, "\r")
		s := strings.TrimSpace(satir)
		if s == "" || strings.HasPrefix(s, "#") {
			out = append(out, "") // boş placeholder: BulkCreate yoksayildi say
			continue
		}
		for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
			switch r {
			case ',', ';', ' ', '\t':
				return true
			}
			return false
		}) {
			out = append(out, tok)
		}
	}
	return out
}

func (h *Handler) BulkCreate(w http.ResponseWriter, r *http.Request) {
	// Body limit — 2 MB. bulkMaxKayit ile birlikte iki katmanlı savunma.
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var g bulkGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, http.StatusBadRequest, "geçersiz istek gövdesi: "+err.Error())
		return
	}
	if len(g.Description) > 255 {
		g.Description = g.Description[:255]
	}
	ms := 1
	if g.MatchSubdomains != nil && !*g.MatchSubdomains {
		ms = 0
	}

	var uid *int64
	if h.Aktor != nil {
		if u, ok := h.Aktor(r); ok {
			uid = &u
		}
	}

	// Giriş metni tamamen boş/beyaz ise erken çık — token parser boş
	// satırlar için placeholder üretiyor, len(tokenler) o yüzden yanıltıcı.
	if strings.TrimSpace(g.Domains) == "" {
		hataYaz(w, http.StatusBadRequest, "içerik boş")
		return
	}
	tokenler := tokenAyir(g.Domains)
	// 🔴 Basarisiz NIL slice'ı JSON'a `null` diye serialize olur (kullanıcı
	// arayüzü `.length` erişince crash eder). Boş slice ile init et.
	yanit := bulkYanit{Toplam: len(tokenler), Basarisiz: []bulkHata{}}
	if len(tokenler) > bulkMaxKayit {
		hataYaz(w, http.StatusRequestEntityTooLarge,
			"tek seferde en fazla "+itoa(bulkMaxKayit)+" domain işlenebilir")
		return
	}

	gorulen := make(map[string]struct{}, len(tokenler))

	// Tek transaction — arka arkaya 5000 exec yerine bir batch daha verimli
	// olur ama duplicate/hata satır satır raporlanmalı; INSERT ... ON DUPL
	// UPDATE her bir satır için ayrı çalıştırıyoruz. Prepared statement ile
	// yine hızlı: MySQL protokolü query'yi bir kez parse eder.
	stmt, err := h.DB.Prepare(
		`INSERT INTO cp_banned_domains (domain, description, match_subdomains, created_by)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE description=VALUES(description), match_subdomains=VALUES(match_subdomains)`,
	)
	if err != nil {
		hataYaz(w, http.StatusInternalServerError, "prepare hatası: "+err.Error())
		return
	}
	defer stmt.Close()

	for _, t := range tokenler {
		s := strings.TrimSpace(t)
		if s == "" {
			yanit.Yoksayild++
			continue
		}
		// Hosts-file uyumlu yorum satırı
		if strings.HasPrefix(s, "#") {
			yanit.Yoksayild++
			continue
		}
		s = domainTemizle(s)
		if s == "" {
			yanit.Yoksayild++
			continue
		}
		if _, dup := gorulen[s]; dup {
			yanit.Yoksayild++
			continue
		}
		gorulen[s] = struct{}{}
		if !domainGecerliMi(s) {
			yanit.Basarisiz = append(yanit.Basarisiz, bulkHata{Domain: s, Sebep: "geçersiz biçim"})
			continue
		}
		if _, err := stmt.Exec(s, g.Description, ms, uid); err != nil {
			yanit.Basarisiz = append(yanit.Basarisiz, bulkHata{Domain: s, Sebep: err.Error()})
			continue
		}
		yanit.Islendi++
	}
	gecersizKil()
	jsonYaz(w, http.StatusOK, yanit)
}

// itoa — kısa stdlib alternatifi, strconv import'unu bu dosyaya sokmamak için.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
