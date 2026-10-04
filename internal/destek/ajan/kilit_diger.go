//go:build !linux

package ajan

import "errors"

// KilitYolu — yalnız Linux'ta anlamlı.
const KilitYolu = "/run/girginospanel/destek-ajan.lock"

// ErrKilitTutuluyor — başka bir ajan örneği çalışıyor.
var ErrKilitTutuluyor = errors.New("başka bir destek ajanı örneği çalışıyor")

// TekOrnekKilidi — Linux dışında desteklenmez (kapalı başarısız).
func TekOrnekKilidi(string) (func(), error) {
	return nil, errors.New("tek örnek kilidi yalnız Linux")
}
