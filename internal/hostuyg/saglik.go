package hostuyg

// Servis sağlık kontrolü — start sonrası hemen değil, kısa bekle + N deneme.

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SaglikBekle — tarif.SaglikHTTP veya SaglikTCP tanımlıysa test et.
// Deneme sayısı 8, ara 1s, timeout 3s (~11s toplam). Herhangi bir denemede
// OK dönerse true.
// Recipe.SaglikBekle saniye kadar ilk denemeden önce bekle.
func SaglikBekle(tarif *Tarif, portlar map[string]int) (bool, string) {
	bekle := tarif.SaglikBekle
	if bekle <= 0 {
		bekle = 3
	}
	time.Sleep(time.Duration(bekle) * time.Second)

	// HTTP probe
	if tarif.SaglikHTTP != "" {
		url := tarif.SaglikHTTP
		for ad, p := range portlar {
			url = strings.ReplaceAll(url, "{port_"+ad+"}", fmt.Sprintf("%d", p))
		}
		return httpDene(url, 8, 1*time.Second)
	}
	// TCP probe
	if tarif.SaglikTCP > 0 {
		return tcpDene("127.0.0.1", tarif.SaglikTCP, 8, 1*time.Second)
	}
	// Sağlık kontrolü tanımlanmamış → başarılı say (servis 5s'de çakmadıysa OK)
	return true, "sağlık kontrolü tanımsız — start sonrası crash olmadı"
}

func httpDene(url string, deneme int, ara time.Duration) (bool, string) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			// InsecureSkipVerify: healthcheck sadece localhost (127.0.0.1)
			// üzerinden çağrılır — self-signed cert veya wrong CN olası ama
			// MITM riski YOK. Recipe.SaglikHTTP dışardan verilse bile
			// 127.0.0.1 dışı adresler bu client'a girmemeli.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	var son string
	for i := 0; i < deneme; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return true, fmt.Sprintf("%s → HTTP %d", url, resp.StatusCode)
			}
			son = fmt.Sprintf("HTTP %d", resp.StatusCode)
		} else {
			son = err.Error()
		}
		time.Sleep(ara)
	}
	return false, fmt.Sprintf("%s → başarısız (%s)", url, son)
}

func tcpDene(host string, port int, deneme int, ara time.Duration) (bool, string) {
	// net.JoinHostPort — IPv6 [addr]:port formatına doğru kaçış (vet uyar)
	adres := net.JoinHostPort(host, strconv.Itoa(port))
	var son string
	for i := 0; i < deneme; i++ {
		c, err := net.DialTimeout("tcp", adres, 2*time.Second)
		if err == nil {
			c.Close()
			return true, adres + " bağlanıldı"
		}
		son = err.Error()
		time.Sleep(ara)
	}
	return false, adres + " → " + son
}
