package protokol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ClaudeArac: Messages API "tools" dizisinin bir elemanı (strict tool use).
type ClaudeArac struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Strict      bool            `json:"strict"`
}

type semaOzellik struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Enum        any    `json:"enum,omitempty"`
}

// ClaudeAraclari: TÜM roller ve oturumlar için AYNI dizi (prompt cache öneki).
// Oturum verisi (hesap sayısı, rol, dil, saat) burada YOKTUR; rol/düzey/hesap kapsamı
// AracGirdisiCoz'da (ajanda bağlayıcı, merkezde ön süzgeç) zorlanır.
func ClaudeAraclari() []ClaudeArac {
	out := make([]ClaudeArac, 0, len(katalog))
	for _, t := range katalog {
		out = append(out, ClaudeArac{
			Name:        t.Ad,
			Description: t.Aciklama.TR,
			InputSchema: girdiSemasi(t),
			Strict:      true,
		})
	}
	return out
}

var (
	toolsOnce sync.Once
	toolsJSON []byte
)

// ToolsJSON: ClaudeAraclari()'nın bayt-bayt deterministik JSON'u. Merkez istek gövdesine
// BUNU olduğu gibi koyar (yeniden serileştirmez). Değişirse prompt cache düşer — altın test korur.
func ToolsJSON() []byte {
	toolsOnce.Do(func() {
		b, err := json.Marshal(ClaudeAraclari())
		if err != nil {
			panic(err)
		}
		toolsJSON = b
	})
	return bytes.Clone(toolsJSON)
}

// girdiSemasi: sıralı (map'siz) şema baytları. Strict'in desteklemediği
// kısıtlar (minLength/maxLength/pattern/minimum/maximum) bilerek YOK.
func girdiSemasi(t AracTanimi) json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"object","properties":{`)
	zorunlu := []string{}
	for i, p := range t.Parametreler {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.Ad)
		b.Write(k)
		b.WriteByte(':')
		o := semaOzellik{Type: string(p.Tur), Description: p.claudeAciklama()}
		if len(p.Enum) > 0 {
			o.Enum = p.Enum
		} else if len(p.TamsayiEnum) > 0 {
			o.Enum = p.TamsayiEnum
		}
		v, err := json.Marshal(o)
		if err != nil {
			panic(err)
		}
		b.Write(v)
		if p.Zorunlu {
			zorunlu = append(zorunlu, p.Ad)
		}
	}
	b.WriteString(`},"required":`)
	z, _ := json.Marshal(zorunlu)
	b.Write(z)
	b.WriteString(`,"additionalProperties":false}`)
	return b.Bytes()
}

func (p ParamTanimi) claudeAciklama() string {
	s := p.Aciklama.TR
	if len(p.Enum) == 0 && p.Tur == TurDizge {
		s += fmt.Sprintf(" En çok %d karakter.", p.dizgeSiniri())
	}
	if p.Yol {
		s += " '/' ile başlamalı; '..' ve '//' içeremez."
	}
	return s
}

func (p ParamTanimi) dizgeSiniri() int {
	if p.EnUzun > 0 {
		return p.EnUzun
	}
	return varsayilanDizge
}

// OturumBaglami: AracGirdisiCoz'un kapsam bağlamı. AJANDA bu, merkezden gelen
// değil YEREL bağlantı kaydından kurulur (merkez ele geçse bile kapsam aşılamaz).
type OturumBaglami struct {
	Rol      Rol
	Duzey    Duzey
	Hesaplar []HesapEtiketi
	Dil      string // "tr" | "en" (yalnız OturumBlogu için)
}

func (o OturumBaglami) dogrula() error {
	if !o.Rol.Gecerli() || !o.Duzey.Gecerli() {
		return hataf(HataYetki, "oturum rol/düzey")
	}
	if len(o.Hesaplar) > EnCokHesap {
		return hataf(HataKapsam, "hesap sayısı")
	}
	return nil
}

// AracGirdisi: doğrulanmış araç girdisi. Yalnız AracGirdisiCoz üretir.
type AracGirdisi struct {
	Arac      string
	Risk      Risk
	dizgeler  map[string]string
	sayilar   map[string]int64
	mantiksal map[string]bool
}

func (g *AracGirdisi) Dizge(ad string) (string, bool)   { v, ok := g.dizgeler[ad]; return v, ok }
func (g *AracGirdisi) Tamsayi(ad string) (int64, bool)  { v, ok := g.sayilar[ad]; return v, ok }
func (g *AracGirdisi) Mantiksal(ad string) (bool, bool) { v, ok := g.mantiksal[ad]; return v, ok }

// Hesap: "hesap" parametresinin işaret ettiği oturum hesabı.
func (g *AracGirdisi) Hesap(o OturumBaglami) (HesapEtiketi, bool) {
	n, ok := g.sayilar["hesap"]
	if !ok || n < 1 || n > int64(len(o.Hesaplar)) {
		return HesapEtiketi{}, false
	}
	return o.Hesaplar[n-1], true
}

var tamsayiDeseni = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,17})$`)

// AracGirdisiCoz: Claude tool_use girdisini ya da is_emri.param'ı katalog tanımına ve
// oturum bağlamına göre KATI doğrular: bilinmeyen araç/parametre, yinelenen anahtar,
// tür uyuşmazlığı, enum dışı değer, uzun dizge, yasak karakter, '../', kapsam dışı hesap,
// rol ve düzey → ret. Hata kodu: arac_bilinmiyor | yetki_yok | duzey_yetersiz | kapsam_disi | param_gecersiz.
func AracGirdisiCoz(o OturumBaglami, arac string, girdi []byte) (*AracGirdisi, error) {
	if err := o.dogrula(); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(katalog, func(t AracTanimi) bool { return t.Ad == arac })
	if i < 0 {
		return nil, hataf(HataAracBilinmiyor, "%q", arac)
	}
	t := katalog[i]
	if !t.RolIzinli(o.Rol) {
		return nil, hataf(HataYetki, "%s rolü %s çağıramaz", o.Rol, t.Ad)
	}
	if !o.Duzey.IzinVerir(t.Risk) {
		return nil, hataf(HataDuzey, "%s düzeyi %s izni vermez", o.Duzey, t.Risk)
	}
	if len(girdi) > ParamSiniri {
		return nil, hataf(HataParam, "girdi %d bayt > %d", len(girdi), ParamSiniri)
	}
	if !utf8.Valid(girdi) {
		return nil, hataf(HataParam, "UTF-8 değil")
	}
	d := json.NewDecoder(bytes.NewReader(girdi))
	if err := denetle(d, nil, 0); err != nil {
		return nil, hataf(HataParam, "%v", err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, hataf(HataParam, "nesneden sonra ek veri")
	}
	d2 := json.NewDecoder(bytes.NewReader(girdi))
	d2.UseNumber()
	var ham map[string]any
	if err := d2.Decode(&ham); err != nil || ham == nil {
		return nil, hataf(HataParam, "girdi bir JSON nesnesi olmalı")
	}
	g := &AracGirdisi{Arac: t.Ad, Risk: t.Risk, dizgeler: map[string]string{}, sayilar: map[string]int64{}, mantiksal: map[string]bool{}}
	anahtarlar := make([]string, 0, len(ham))
	for k := range ham {
		anahtarlar = append(anahtarlar, k)
	}
	sort.Strings(anahtarlar)
	for _, k := range anahtarlar {
		p, ok := t.param(k)
		if !ok {
			return nil, hataf(HataParam, "bilinmeyen parametre %q", k)
		}
		if err := p.degerCoz(t.Ad, o, ham[k], g); err != nil {
			return nil, err
		}
	}
	for _, p := range t.Parametreler {
		if _, var_ := ham[p.Ad]; p.Zorunlu && !var_ {
			return nil, hataf(HataParam, "eksik parametre %q", p.Ad)
		}
	}
	return g, nil
}

func (p ParamTanimi) degerCoz(arac string, o OturumBaglami, v any, g *AracGirdisi) error {
	switch p.Tur {
	case TurDizge:
		s, ok := v.(string)
		if !ok {
			return hataf(HataParam, "%s: dizge bekleniyor", p.Ad)
		}
		if err := p.dizgeDenetle(arac, s); err != nil {
			return err
		}
		g.dizgeler[p.Ad] = s
	case TurTamsayi:
		n, ok := v.(json.Number)
		if !ok || !tamsayiDeseni.MatchString(string(n)) {
			return hataf(HataParam, "%s: tamsayı bekleniyor", p.Ad)
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return hataf(HataParam, "%s: tamsayı", p.Ad)
		}
		if p.Hesap {
			if i < 1 || i > int64(len(o.Hesaplar)) {
				return hataf(HataKapsam, "%s: %d oturumda yok (1..%d)", p.Ad, i, len(o.Hesaplar))
			}
		} else if len(p.TamsayiEnum) > 0 && !slices.Contains(p.TamsayiEnum, i) {
			return hataf(HataParam, "%s: %d izinli değerlerden değil", p.Ad, i)
		}
		g.sayilar[p.Ad] = i
	case TurMantiksal:
		b, ok := v.(bool)
		if !ok {
			return hataf(HataParam, "%s: mantıksal bekleniyor", p.Ad)
		}
		g.mantiksal[p.Ad] = b
	default:
		return hataf(HataIc, "%s: tür", p.Ad)
	}
	return nil
}

func (p ParamTanimi) dizgeDenetle(arac, s string) error {
	if len(p.Enum) > 0 {
		if !slices.Contains(p.Enum, s) {
			return hataf(HataParam, "%s: izinli değerlerden değil", p.Ad)
		}
		return nil
	}
	if len(s) > p.dizgeSiniri() {
		return hataf(HataParam, "%s: %d bayt > %d", p.Ad, len(s), p.dizgeSiniri())
	}
	for _, r := range s {
		if yasakRune(r) {
			return hataf(HataParam, "%s: yasak karakter %U", p.Ad, r)
		}
	}
	if strings.Contains(s, "../") || strings.Contains(s, `..\`) || strings.Contains(s, "\\") {
		return hataf(HataParam, "%s: yol geçişi", p.Ad)
	}
	if p.Yol {
		if !strings.HasPrefix(s, "/") || strings.Contains(s, "//") {
			return hataf(HataParam, "%s: yol biçimi", p.Ad)
		}
		for _, kesim := range strings.Split(s, "/") {
			if kesim == "." || kesim == ".." {
				return hataf(HataParam, "%s: '.'/'..' kesimi", p.Ad)
			}
		}
	}
	if d := desenAl(arac, p.Ad); d != nil && !d.MatchString(s) {
		return hataf(HataParam, "%s: biçim", p.Ad)
	}
	return nil
}
