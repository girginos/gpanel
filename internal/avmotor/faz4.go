package avmotor

// FAZ 4 — AST/semantik katman. Regex'in DOĞASI GEREĞİ göremediği kaçışları yener:
//
//  1. CONCAT-BÖLME: `('sy'.'st'.'em')($_GET[x])`, `$f='ev'.'al'`, `$c='sy';$c.='stem'`
//     — tehlikeli ismi string parçalarına bölme. SABİT string birleştirmelerini
//     KATLA + `$v=literal` / `$v.=literal` ata-izle → yeniden-kurulan adı yakala.
//  2. YAPISAL kaçışlar (regex İFADE EDEMEZ): değişken-fonksiyon `$f(...)` (sabit
//     sink adı), süperglobal-çağrılabilir `$_GET[x](...)`, eval(concat-kod).
//
// 🔴 FP DİSİPLİNİ (ders: regex-taint gerçek WP'de 21 FP): (a) `$v(` çağrısı ÜYE
// erişimiyse (`->`/`?->`/`::`) ATLA — dinamik METOT sevki (`$this->$action()`)
// meşru framework desenidir, arbitrary kod DEĞİL. (b) rescan YALNIZ eval'lenen
// kodu tarar (dokümantasyon/UI stringleri parcali'ye girmez). (c) süperglobal
// doğrudan-çağrı zaten meşru kodda yoktur.
//
// 🔴 DoS: katlanan string + symtab değeri semMaxKatlaBoyut ile SINIRLI
// (billion-laughs `$b=$a.$a;$c=$b.$b` üstel şişme → OOM önlemi). Süperglobal
// indeks atlama token-sınırlı (O(n^2) önlemi). Tokenizer O(input), sonlu.
//
// BİLİNEN/ERTELENEN KAÇIŞLAR (roadmap dürüstlüğü — Faz 4.x / 5): chr()/implode()/
// pack()/sprintf ile kurulan adlar (string-katlayıcı doğası gereği katlanmaz),
// değişken-değişken `$$x` ve `${'_GET'}`, sanitizer-şekilli sarmalayıcı taint'i
// temizler (`$c=strtolower($_POST[x])`), token-tavanı sonrası kuyruk (ham regex+
// decode katmanları yine tüm içeriği tarar), `call_user_func($_GET[x])` geri-çağrı
// sevki (array_map veri-argümanı FP riski → dikkatli ayrı detektör gerekir).

import (
	"strings"
)

const (
	semRescanTavan   = 60      // concat-yeniden-tara katkısı — EsikKritik(100) ALTINDA
	semMaxToken      = 200000  // token tavanı (DoS)
	semMaxParca      = 4096    // rescan parça sayısı tavanı
	semMaxKatlaBoyut = 64 << 10 // katlanan string / symtab değeri BAYT tavanı (OOM önlemi)
	semMaxIndeksTok  = 256     // süperglobal [ ... ] atlama token tavanı (O(n^2) önlemi)
)

// sertSink — ADI gizlenip çağrıldığında neredeyse HER ZAMAN zararlı gerçek PHP
// fonksiyonları. call_user_func/array_map DIŞTA (framework FP). assert dahil (PHP<8 fn).
var sertSink = map[string]bool{
	"system": true, "exec": true, "shell_exec": true, "passthru": true,
	"popen": true, "proc_open": true, "pcntl_exec": true, "assert": true,
	"create_function": true,
}

var superglobal = map[string]bool{
	"$_GET": true, "$_POST": true, "$_REQUEST": true, "$_COOKIE": true,
	"$_SERVER": true, "$_FILES": true, "$_ENV": true,
}

// evalIdent — argümanı concat'la KURULMUŞ kod ise gizlenmiş-yürütme.
var evalIdent = map[string]bool{
	"eval": true, "assert": true, "create_function": true,
}

type tokTur int

const (
	tkStr tokTur = iota
	tkDot        // .
	tkDotEq      // .=
	tkVar        // $ad
	tkIdent
	tkAssign // =
	tkLParen
	tkRParen
	tkSemi
	tkOther // operatör/sayı/[ ]/-> /:: /?-> ...
)

type tok struct {
	tur   tokTur
	deger string
	sabit bool // tkStr: interpolasyonsuz mu
}

func semantikTara(icerik []byte, kurallar []Kural, ext string, disGoruldu map[string]bool) (int, []string) {
	if !phpUzanti(ext) {
		return 0, nil
	}
	toks := phpTokenize(icerik)
	if len(toks) == 0 {
		return 0, nil
	}

	symtab := map[string]string{}  // $v -> sabit string değeri (hepsi <= semMaxKatlaBoyut)
	taint := map[string]bool{}     // $v süperglobal kaynaklı mı
	concatVar := map[string]bool{} // $v ≥2 parçalı concat'tan mı (gizlenmiş kod adayı)
	var parcali []string           // YALNIZ eval'lenen katlanan kod (rescan için)

	puan := 0
	var ids []string
	gorulen := map[string]bool{}
	ekle := func(p int, id string) {
		if gorulen[id] {
			return
		}
		gorulen[id] = true
		puan += p
		ids = append(ids, id)
	}
	sinkAdi := func(s string) bool { return sertSink[strings.ToLower(strings.TrimSpace(s))] }
	parcaEkle := func(s string) {
		if s != "" && len(parcali) < semMaxParca {
			parcali = append(parcali, s)
		}
	}

	i := 0
	for i < len(toks) {
		t := toks[i]

		// ── Atama: $v = <concat> ──  (RHS'yi ANA DÖNGÜ yeniden yürür: i+=2)
		if t.tur == tkVar && i+1 < len(toks) && toks[i+1].tur == tkAssign {
			deger, parca, sabit, tainted, _ := ifadeKatla(toks, i+2, symtab, taint)
			atamaUygula(t.deger, deger, parca, sabit, tainted, symtab, taint, concatVar)
			if sabit && parca >= 2 && sinkAdi(deger) {
				ekle(100, "GOSP-SEMANTIK-CONCAT-SINK")
			}
			i += 2 // yalnız $v ve = geç; RHS'deki $g( / $_GET[x]( yapısal detektörlerce görülür
			continue
		}

		// ── Birleşik atama: $v .= <concat> ──
		if t.tur == tkVar && i+1 < len(toks) && toks[i+1].tur == tkDotEq {
			rhs, _, sabit, tainted, _ := ifadeKatla(toks, i+2, symtab, taint)
			if sabit {
				onceki := symtab[t.deger]
				yeni := onceki + rhs
				if len(yeni) <= semMaxKatlaBoyut {
					symtab[t.deger] = yeni
					concatVar[t.deger] = true // en az 2 parça (mevcut + eklenen)
					delete(taint, t.deger)
					if sinkAdi(yeni) {
						ekle(100, "GOSP-SEMANTIK-CONCAT-SINK")
					}
				} else {
					delete(symtab, t.deger)
					delete(concatVar, t.deger)
				}
			} else if tainted {
				taint[t.deger] = true
			}
			i += 2
			continue
		}

		// ── Değişken-fonksiyon / taint çağrısı: $v (   (ÜYE erişimi HARİÇ) ──
		if t.tur == tkVar && i+1 < len(toks) && toks[i+1].tur == tkLParen && !uyeErisim(toks, i) {
			if ad, ok := symtab[t.deger]; ok && sinkAdi(ad) {
				ekle(70, "GOSP-SEMANTIK-VARFUNC-SINK")
			}
			if taint[t.deger] {
				ekle(100, "GOSP-SEMANTIK-TAINT-CALL")
			}
		}

		// ── Süperglobal doğrudan çağrılabilir: $_GET [ ... ] (   (ÜYE erişimi HARİÇ) ──
		if t.tur == tkVar && superglobal[t.deger] && !uyeErisim(toks, i) {
			j := i + 1
			if j < len(toks) && toks[j].tur == tkOther && toks[j].deger == "[" {
				derinlik, adim := 0, 0
				for j < len(toks) && adim < semMaxIndeksTok {
					if toks[j].tur == tkOther && toks[j].deger == "[" {
						derinlik++
					} else if toks[j].tur == tkOther && toks[j].deger == "]" {
						derinlik--
						if derinlik == 0 {
							j++
							break
						}
					}
					j++
					adim++
				}
			}
			if j < len(toks) && toks[j].tur == tkLParen {
				ekle(100, "GOSP-SEMANTIK-SG-CALL")
			}
		}

		// ── Gizlenmiş-kod yürütme: eval/assert/create_function ( concat ) ──
		if t.tur == tkIdent && evalIdent[strings.ToLower(t.deger)] &&
			i+2 < len(toks) && toks[i+1].tur == tkLParen {
			if toks[i+2].tur == tkVar && concatVar[toks[i+2].deger] {
				ekle(100, "GOSP-SEMANTIK-EVAL-CONCAT")
				parcaEkle(symtab[toks[i+2].deger]) // eval'lenen kodu rescan'e ver
			} else if toks[i+2].tur == tkStr && i+3 < len(toks) && toks[i+3].tur == tkDot {
				deger, parca, sabit, _, _ := ifadeKatla(toks, i+2, symtab, taint)
				if sabit && parca >= 2 {
					ekle(100, "GOSP-SEMANTIK-EVAL-CONCAT")
					parcaEkle(deger)
				}
			}
		}

		// ── Doğrudan concat-sink çağrısı: 'sy'.'stem' ( ...  veya  ( 'sy'.'stem' ) ( ──
		if t.tur == tkStr && i+1 < len(toks) && toks[i+1].tur == tkDot {
			deger, parca, sabit, _, ilerle := ifadeKatla(toks, i, symtab, taint)
			if sabit && parca >= 2 && sinkAdi(deger) && ilerle < len(toks) && toks[ilerle].tur == tkLParen {
				ekle(100, "GOSP-SEMANTIK-CONCAT-SINK")
			}
			if ilerle > i {
				i = ilerle
				continue
			}
		}
		if t.tur == tkLParen && i+2 < len(toks) && toks[i+1].tur == tkStr && toks[i+2].tur == tkDot {
			deger, parca, sabit, _, ilerle := ifadeKatla(toks, i+1, symtab, taint)
			if sabit && parca >= 2 {
				son := ilerle
				if son < len(toks) && toks[son].tur == tkRParen {
					son++
				}
				if sinkAdi(deger) && son < len(toks) && toks[son].tur == tkLParen {
					ekle(100, "GOSP-SEMANTIK-CONCAT-SINK")
				}
			}
		}

		i++
	}

	// ── Concat-yeniden-tara: YALNIZ eval'lenen katlanan kodu mevcut kurallarla tara ──
	if len(parcali) > 0 {
		blob := []byte(strings.Join(parcali, "\n"))
		rp := 0
		for _, k := range kurallar {
			id := "SEMANTIK-CONCAT:" + k.ID
			if disGoruldu[k.ID] || gorulen[id] {
				continue
			}
			if len(k.Uzanti) > 0 && !icerirStr(k.Uzanti, ext) {
				continue
			}
			if k.re != nil && k.re.Match(blob) {
				add := k.Puan
				if rp+add > semRescanTavan {
					add = semRescanTavan - rp
				}
				if add <= 0 {
					break
				}
				rp += add
				gorulen[id] = true
				puan += add
				ids = append(ids, id)
			}
		}
	}

	return puan, ids
}

// atamaUygula — `$v = deger` sonucunu symtab/taint/concatVar'a işler (boyut-sınırlı).
func atamaUygula(v, deger string, parca int, sabit, tainted bool,
	symtab map[string]string, taint map[string]bool, concatVar map[string]bool) {
	if sabit && len(deger) <= semMaxKatlaBoyut {
		symtab[v] = deger
		delete(taint, v)
		if parca >= 2 && deger != "" {
			concatVar[v] = true
		} else {
			delete(concatVar, v)
		}
		return
	}
	delete(symtab, v)
	delete(concatVar, v)
	if !sabit && tainted {
		taint[v] = true
	} else {
		delete(taint, v)
	}
}

// uyeErisim — toks[i]'den ÖNCEKİ token bir üye-erişim operatörü mü (`->`/`?->`/`::`).
// Dinamik metot sevkini (`$obj->$m()`) düz fonksiyon çağrısından ayırır (FP önlemi).
func uyeErisim(toks []tok, i int) bool {
	if i == 0 {
		return false
	}
	p := toks[i-1]
	return p.tur == tkOther && (p.deger == "->" || p.deger == "?->" || p.deger == "::")
}

// ifadeKatla — toks[i]'den başlayan `A . B . C` concat'ını katlar (boyut-sınırlı).
// Dönüş: (değer, parçaSayısı, sabitMi, süperglobalTaintMi, ilerlenenIndeks).
func ifadeKatla(toks []tok, i int, symtab map[string]string, taint map[string]bool) (string, int, bool, bool, int) {
	var sb strings.Builder
	parca := 0
	sabit := true
	tainted := false
	beklenenTerim := true
	j := i
	for j < len(toks) {
		t := toks[j]
		if beklenenTerim {
			switch {
			case t.tur == tkStr && t.sabit:
				sb.WriteString(t.deger)
				parca++
			case t.tur == tkVar:
				// Bir sonraki token '(' veya '[' ise bu bir ÇAĞRI/INDEKS ifadesidir,
				// düz değer değil → sabit sayma (dönüş-değeri yanlış bağlama önlemi).
				sonraki := tokTur(tkOther)
				if j+1 < len(toks) {
					sonraki = toks[j+1].tur
				}
				if v, ok := symtab[t.deger]; ok && sonraki != tkLParen {
					sb.WriteString(v)
					parca++
				} else if taint[t.deger] || superglobal[t.deger] {
					tainted = true
					sabit = false
				} else {
					sabit = false
				}
			default:
				sabit = false
				return sb.String(), parca, sabit, tainted, j
			}
			// 🔴 OOM önlemi: katlanan boyut tavanı aşılırsa sabit-DEĞİL döndür.
			if sb.Len() > semMaxKatlaBoyut {
				return "", parca, false, tainted, j + 1
			}
			beklenenTerim = false
			j++
		} else {
			if t.tur == tkDot {
				beklenenTerim = true
				j++
				continue
			}
			break
		}
	}
	return sb.String(), parca, sabit, tainted, j
}

func phpUzanti(ext string) bool {
	switch ext {
	case "php", "phtml", "php5", "php7", "php8", "phar", "inc", "module", "install":
		return true
	}
	return false
}

// phpTokenize — SINIRLI PHP tokenizer. Yalnız <?php ... ?> bölgelerini işler.
func phpTokenize(src []byte) []tok {
	var out []tok
	n := len(src)
	i := 0
	inPhp := false
	for i < n && len(out) < semMaxToken {
		c := src[i]
		if !inPhp {
			if c == '<' && i+1 < n && src[i+1] == '?' {
				if i+5 <= n && strings.EqualFold(string(src[i:i+5]), "<?php") {
					i += 5
					inPhp = true
					continue
				}
				if i+3 <= n && string(src[i:i+3]) == "<?=" {
					i += 3
					inPhp = true
					continue
				}
				// <?xml PHP kısa etiketi DEĞİL (PHP de böyle ele alır)
				if i+5 <= n && strings.EqualFold(string(src[i+2:i+5]), "xml") {
					i += 2
					continue
				}
				i += 2
				inPhp = true
				continue
			}
			i++
			continue
		}
		switch {
		case c == '?' && i+1 < n && src[i+1] == '>':
			inPhp = false
			i += 2
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '#':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'':
			val, ni := tekTirnak(src, i)
			out = append(out, tok{tur: tkStr, deger: val, sabit: true})
			i = ni
		case c == '"':
			val, sabit, ni := ciftTirnak(src, i)
			out = append(out, tok{tur: tkStr, deger: val, sabit: sabit})
			i = ni
		case c == '<' && i+2 < n && src[i+1] == '<' && src[i+2] == '<':
			val, sabit, ni := heredoc(src, i)
			out = append(out, tok{tur: tkStr, deger: val, sabit: sabit})
			i = ni
		case c == '$':
			j := i + 1
			for j < n && (harfMi(src[j]) || rakamMi(src[j]) || src[j] == '_') {
				j++
			}
			out = append(out, tok{tur: tkVar, deger: string(src[i:j])})
			i = j
		case harfMi(c) || c == '_':
			j := i
			for j < n && (harfMi(src[j]) || rakamMi(src[j]) || src[j] == '_') {
				j++
			}
			out = append(out, tok{tur: tkIdent, deger: string(src[i:j])})
			i = j
		case c == '.':
			if i+1 < n && src[i+1] == '=' {
				out = append(out, tok{tur: tkDotEq, deger: ".="})
				i += 2
			} else {
				out = append(out, tok{tur: tkDot})
				i++
			}
		case c == '=':
			if i+1 < n && (src[i+1] == '=' || src[i+1] == '>') {
				out = append(out, tok{tur: tkOther, deger: string(src[i : i+2])})
				i += 2
			} else {
				out = append(out, tok{tur: tkAssign})
				i++
			}
		case c == '-' && i+1 < n && src[i+1] == '>':
			out = append(out, tok{tur: tkOther, deger: "->"})
			i += 2
		case c == '?' && i+2 < n && src[i+1] == '-' && src[i+2] == '>':
			out = append(out, tok{tur: tkOther, deger: "?->"})
			i += 3
		case c == ':' && i+1 < n && src[i+1] == ':':
			out = append(out, tok{tur: tkOther, deger: "::"})
			i += 2
		case c == '(':
			out = append(out, tok{tur: tkLParen})
			i++
		case c == ')':
			out = append(out, tok{tur: tkRParen})
			i++
		case c == ';':
			out = append(out, tok{tur: tkSemi})
			i++
		case c == '[' || c == ']':
			out = append(out, tok{tur: tkOther, deger: string(c)})
			i++
		default:
			out = append(out, tok{tur: tkOther, deger: string(c)})
			i++
		}
	}
	return out
}

// tekTirnak — '...' okur; kaçış yalnız \\ ve \'.
func tekTirnak(src []byte, i int) (string, int) {
	n := len(src)
	var sb strings.Builder
	i++
	for i < n {
		c := src[i]
		if c == '\\' && i+1 < n && (src[i+1] == '\\' || src[i+1] == '\'') {
			sb.WriteByte(src[i+1])
			i += 2
			continue
		}
		if c == '\'' {
			i++
			break
		}
		sb.WriteByte(c)
		i++
		if sb.Len() > semMaxKatlaBoyut {
			// aşırı uzun literal: kalanını atla (değer zaten tavanı aştı)
			for i < n && src[i] != '\'' {
				i++
			}
			if i < n {
				i++
			}
			break
		}
	}
	return sb.String(), i
}

// ciftTirnak — "..." okur. $ veya { interpolasyonu → SABİT DEĞİL. \xNN/\NNN çözülür
// (gizli sink adı "\x73\x79...m" → "system" katlanabilsin).
func ciftTirnak(src []byte, i int) (string, bool, int) {
	n := len(src)
	var sb strings.Builder
	sabit := true
	i++
	for i < n {
		c := src[i]
		if c == '\\' && i+1 < n {
			nx := src[i+1]
			switch {
			case nx == '\\' || nx == '"' || nx == '$':
				sb.WriteByte(nx)
				i += 2
			case nx == 'n':
				sb.WriteByte('\n')
				i += 2
			case nx == 't':
				sb.WriteByte('\t')
				i += 2
			case nx == 'x' && i+2 < n && hexMi(src[i+2]):
				h := i + 2
				val := 0
				say := 0
				for h < n && say < 2 && hexMi(src[h]) {
					val = val*16 + hexDeger(src[h])
					h++
					say++
				}
				sb.WriteByte(byte(val))
				i = h
			case nx >= '0' && nx <= '7':
				h := i + 1
				val := 0
				say := 0
				for h < n && say < 3 && src[h] >= '0' && src[h] <= '7' {
					val = val*8 + int(src[h]-'0')
					h++
					say++
				}
				sb.WriteByte(byte(val))
				i = h
			default:
				sb.WriteByte(nx)
				i += 2
			}
			if sb.Len() > semMaxKatlaBoyut {
				sabit = false
			}
			continue
		}
		if c == '$' || c == '{' {
			sabit = false
		}
		if c == '"' {
			i++
			break
		}
		sb.WriteByte(c)
		i++
		if sb.Len() > semMaxKatlaBoyut {
			sabit = false
			for i < n && src[i] != '"' {
				i++
			}
			if i < n {
				i++
			}
			break
		}
	}
	return sb.String(), sabit, i
}

// heredoc — <<<EOT / <<<'EOT' (nowdoc). Nowdoc sabit; heredoc $ içermezse sabit.
func heredoc(src []byte, i int) (string, bool, int) {
	n := len(src)
	j := i + 3
	for j < n && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	nowdoc := false
	if j < n && (src[j] == '\'' || src[j] == '"') {
		nowdoc = src[j] == '\''
		j++
	}
	etBas := j
	for j < n && (harfMi(src[j]) || rakamMi(src[j]) || src[j] == '_') {
		j++
	}
	etiket := string(src[etBas:j])
	if etiket == "" {
		return "", false, i + 3
	}
	for j < n && src[j] != '\n' {
		j++
	}
	if j < n {
		j++
	}
	govdeBas := j
	for j < n {
		satBas := j
		for j < n && src[j] != '\n' {
			j++
		}
		satir := strings.TrimRight(string(src[satBas:j]), "\r")
		trimmed := strings.TrimLeft(satir, " \t")
		if trimmed == etiket || strings.HasPrefix(trimmed, etiket+";") {
			govde := string(src[govdeBas:satBas])
			sabit := nowdoc || !strings.ContainsAny(govde, "${")
			if len(govde) > semMaxKatlaBoyut {
				sabit = false
			}
			if j < n {
				j++
			}
			return govde, sabit, j
		}
		if j < n {
			j++
		}
	}
	return "", false, j
}

func harfMi(c byte) bool  { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80 }
func rakamMi(c byte) bool { return c >= '0' && c <= '9' }
func hexMi(c byte) bool {
	return rakamMi(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func hexDeger(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
