//go:build !linux

package uzak

// DusukOncelikThread — linux dışında işlem yok.
func DusukOncelikThread() {}

// BosAlan — linux dışında sınırsız varsayılır.
func BosAlan(string) (uint64, error) { return ^uint64(0), nil }
