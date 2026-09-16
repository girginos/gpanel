// Package surum — panelin TEK kanonik surum dizesi.
//
// healthz, /ozet (dashboard footer + Izleme/Istatistikler "Panel surumu"),
// lisans el sikismasi vb. HEPSI buradan okur. Surum yalniz BURADA degisir;
// eskiden cmd/server (0.3.0-f3) ile internal/system (0.2.0) ayri dususlerdi.
package surum

// Panel — Linux panel surumu (whitelabel marka adindan AYRI; UI markayi ayri gosterir).
// Panel varsayilan; YAYIN sirasinda girginospanel-paketle ldflags ile
// "0.3.0-YYYYMMDD" enjekte eder (her yayin benzersiz+monoton). var OLMALI
// (const ldflags -X ile degistirilemez).
var Panel = "0.3.0-f3"
