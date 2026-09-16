//go:build linux

package platform

import "girginospanel/internal/provisioner"

// linuxSaglayici — mevcut provisioner'a INCE bir koprü.
//
// 🔴 BURADA IS MANTIGI YOK, OLMAYACAK. Bu dosya yalnizca delegasyon yapar;
// davranis degisikligi sifirdir cunku cagrilan fonksiyonlar bugun panelin
// zaten kullandiklaridir. Mantik eklemek "Linux'u bozunca Windows bozulmasin"
// sartini ters yonde kirar: koprude yapilan hata iki tarafi da vururdu.
type linuxSaglayici struct{}

var aktifSaglayici Saglayici = linuxSaglayici{}

func (linuxSaglayici) Ad() string { return "linux" }

func (linuxSaglayici) Yetenekler() Yetenek {
	return YetSite | YetSSL | YetIzolasyon | YetKota | YetMail | YetPHPSurumSecimi | YetZorunluErisim
}

// Dogrula — Linux tarafinda ortam denetimi zaten kurulum kapisinda yapiliyor
// (girginospanel-dogrula). Burada tekrar etmek iki dogruluk kaynagi yaratirdi.
func (linuxSaglayici) Dogrula() error { return nil }

func (linuxSaglayici) SiteOlustur(ist SiteIstek) (SiteSonuc, error) {
	r, err := provisioner.Provision(ist.AlanAdi, ist.PHPSurum)
	if err != nil {
		return SiteSonuc{}, err
	}
	return SiteSonuc{
		SistemKullanici: r.SistemKullanici,
		WebRoot:         r.WebRoot,
		FTPHost:         r.FTPHost,
		PHPSurum:        r.PHPSurum,
		PHPSocket:       r.PHPSocket,
	}, nil
}

func (linuxSaglayici) SiteSil(k SiteKimlik) error {
	return provisioner.Deprovision(k.AlanAdi, k.SistemKullanici)
}

func (linuxSaglayici) SSLVer(ist SSLIstek) (Sertifika, error) {
	crt, key, err := provisioner.EnableLetsEncrypt(ist.AlanAdi, ist.SistemKullanici, ist.PHPSurum, ist.Backend)
	if err != nil {
		return Sertifika{}, err
	}
	return Sertifika{SertifikaYolu: crt, AnahtarYolu: key}, nil
}
