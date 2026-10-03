package protokol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"
)

const enDerin = 16

var hamMesajTipi = reflect.TypeFor[json.RawMessage]()

// katiCoz: veri → hedef (struct işaretçisi). encoding/json'un gevşek yanlarını kapatır:
// yinelenen anahtar, büyük/küçük harf duyarsız alan eşleşmesi, bilinmeyen alan, ek veri.
func katiCoz(veri []byte, hedef any) error {
	if len(veri) > CerceveSiniri {
		return hataf(HataCerceveBuyuk, "%d bayt", len(veri))
	}
	if !utf8.Valid(veri) {
		return hataf(HataBicim, "UTF-8 değil")
	}
	d := json.NewDecoder(bytes.NewReader(veri))
	if err := denetle(d, reflect.TypeOf(hedef), 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return hataf(HataBicim, "nesneden sonra ek veri")
	}
	d2 := json.NewDecoder(bytes.NewReader(veri))
	d2.DisallowUnknownFields()
	if err := d2.Decode(hedef); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return hataf(HataBilinmeyenAlan, "%v", err)
		}
		return hataf(HataBicim, "%v", err)
	}
	return nil
}

// denetle: belirteç akışını yürür; her nesnede yinelenen anahtarı, t bir struct ise
// anahtarın json etiketiyle BİREBİR (büyük/küçük harf dahil) eşleşmesini zorlar.
// t == nil ya da json.RawMessage → yalnız yinelenen anahtar denetimi.
func denetle(d *json.Decoder, t reflect.Type, derinlik int) error {
	if derinlik > enDerin {
		return hataf(HataBicim, "iç içe derinlik > %d", enDerin)
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == hamMesajTipi {
		t = nil
	}
	tok, err := d.Token()
	if err != nil {
		return hataf(HataBicim, "%v", err)
	}
	dl, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch dl {
	case '{':
		var alanlar map[string]reflect.Type
		if t != nil && t.Kind() == reflect.Struct {
			alanlar = alanHaritasi(t)
		}
		goruldu := map[string]bool{}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return hataf(HataBicim, "%v", err)
			}
			ad, _ := kt.(string)
			if goruldu[ad] {
				return hataf(HataBicim, "yinelenen anahtar %q", ad)
			}
			goruldu[ad] = true
			var alt reflect.Type
			if alanlar != nil {
				ft, ok := alanlar[ad]
				if !ok {
					return hataf(HataBilinmeyenAlan, "%q", ad)
				}
				alt = ft
			}
			if err := denetle(d, alt, derinlik+1); err != nil {
				return err
			}
		}
	case '[':
		var el reflect.Type
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			el = t.Elem()
		}
		for d.More() {
			if err := denetle(d, el, derinlik+1); err != nil {
				return err
			}
		}
	}
	if _, err := d.Token(); err != nil { // kapanış
		return hataf(HataBicim, "%v", err)
	}
	return nil
}

var alanOnbellek sync.Map // reflect.Type → map[string]reflect.Type

func alanHaritasi(t reflect.Type) map[string]reflect.Type {
	if v, ok := alanOnbellek.Load(t); ok {
		return v.(map[string]reflect.Type)
	}
	m := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		etiket := f.Tag.Get("json")
		if etiket == "-" {
			continue
		}
		ad, _, _ := strings.Cut(etiket, ",")
		if f.Anonymous && ad == "" && f.Type.Kind() == reflect.Struct {
			for k, v := range alanHaritasi(f.Type) {
				m[k] = v
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if ad == "" {
			ad = f.Name
		}
		m[ad] = f.Type
	}
	alanOnbellek.Store(t, m)
	return m
}
