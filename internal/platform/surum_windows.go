//go:build windows

package platform

// Windows tarafi kendi hattinda ilerler; Linux surumuyle hizalanma
// ZORUNLULUGU YOKTUR ve hizalanmaya calisilMAMALIdir.
const (
	Surum = "0.3.0-temel"
	Kanal = "windows/dev"
)
