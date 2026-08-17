package ipyonetim

// Cross-package concurrency yardimcilari — portyonetim (backend port degisikligi
// panel restart eder) IP admin islemleri sirasinda calisamamali diye kilit
// paylasilir. yazKilit persist.go icinde tanimli sync.Mutex.

// YazKilitTryLock — kilit alinabilirse true. Portyonetim degistir.go bunu
// backend port degisikligi ONCESINDE cagirir; false ise 400 doner (IP islemi
// devam ediyor).
func YazKilitTryLock() bool { return yazKilit.TryLock() }

// YazKilitUnlock — TryLock ile alinan kilidi geri birak. Portyonetim iyi/kotu
// yolunda mutlaka bunu cagirmali (defer).
func YazKilitUnlock() { yazKilit.Unlock() }
