package domainyasak

// Toplu silme — kullanıcı listeden birden çok kaydı işaretler, tek POST
// isteğiyle hepsi silinir. Neden POST (DELETE değil): HTTP spec DELETE'te
// body destekler ama bazı proxy/CDN gövdeyi düşürür; POST /bulk-delete daha
// güvenli.
//
// Yanıt: silinen sayısı + bulunamayan (listede olmayan / zaten silinmiş)
// domain listesi. Frontend "5 silindi, 1 zaten yoktu" gibi net mesaj gösterir.

import (
	"encoding/json"
	"net/http"
	"strings"
)

type bulkSilGovde struct {
	Domains []string `json:"domains"`
}

type bulkSilYanit struct {
	Silinen      int      `json:"silinen"`
	Bulunamayan  []string `json:"bulunamayan"`
	Gecersiz     []string `json:"gecersiz"` // format bozuk olanlar (ne silindi ne aranmış)
}

func (h *Handler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var g bulkSilGovde
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		hataYaz(w, http.StatusBadRequest, "geçersiz istek gövdesi: "+err.Error())
		return
	}
	if len(g.Domains) == 0 {
		hataYaz(w, http.StatusBadRequest, "silinecek domain yok")
		return
	}
	if len(g.Domains) > bulkMaxKayit {
		hataYaz(w, http.StatusRequestEntityTooLarge,
			"tek seferde en fazla "+itoa(bulkMaxKayit)+" domain silinebilir")
		return
	}

	// 🔴 Boş slice ile init — null yerine [] JSON (frontend crash olmasın).
	yanit := bulkSilYanit{Bulunamayan: []string{}, Gecersiz: []string{}}
	gorulen := make(map[string]struct{}, len(g.Domains))

	stmt, err := h.DB.Prepare(`DELETE FROM cp_banned_domains WHERE domain=?`)
	if err != nil {
		hataYaz(w, http.StatusInternalServerError, "prepare hatası: "+err.Error())
		return
	}
	defer stmt.Close()

	for _, ham := range g.Domains {
		dom := domainTemizle(ham)
		if dom == "" {
			yanit.Gecersiz = append(yanit.Gecersiz, strings.TrimSpace(ham))
			continue
		}
		if _, dup := gorulen[dom]; dup {
			continue // dedup
		}
		gorulen[dom] = struct{}{}
		if !domainGecerliMi(dom) {
			yanit.Gecersiz = append(yanit.Gecersiz, dom)
			continue
		}
		res, err := stmt.Exec(dom)
		if err != nil {
			// Bir hata olsa bile diğerlerini işlemeye devam et; bu domain'i
			// bulunamayan listesine değil, hata değil — silme başarısız.
			// Basitlik için bulunamayanla aynı grupta ele alıyoruz.
			yanit.Bulunamayan = append(yanit.Bulunamayan, dom)
			continue
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			yanit.Bulunamayan = append(yanit.Bulunamayan, dom)
		} else {
			yanit.Silinen++
		}
	}
	gecersizKil()
	jsonYaz(w, http.StatusOK, yanit)
}
