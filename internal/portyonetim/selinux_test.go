package portyonetim

import "testing"

// RHEL/AlmaLinux `semanage port -l` çıktısından özet (yerel 10080 özelleştirmesi dahil).
const ornekSemanage = `SELinux Port Type              Proto    Port Number

afs3_callback_port_t           tcp      7001
http_port_t                    tcp      10080, 80, 81, 443, 488, 8008, 8009, 8443, 9000
http_port_t                    tcp      28519
mountd_port_t                  tcp      20048
gluster_port_t                 tcp      38465-38469, 24007-24027
unreserved_port_t              tcp      61000-65535, 1024-32767
unreserved_port_t              udp      61000-65535, 1024-32767
dns_port_t                     udp      20053
`

func TestSELinuxPortDurumu(t *testing.T) {
	cases := []struct {
		port  int
		http  bool
		baska string
	}{
		{28519, true, ""},                // yerel özelleştirme: zaten etiketli
		{8443, true, ""},                 // politikada http_port_t
		{21001, false, ""},               // yalnız unreserved aralığı → eklenebilir
		{20048, false, "mountd_port_t"},  // başka servise ayrılmış tek port
		{24010, false, "gluster_port_t"}, // başka servise ayrılmış aralık
		{20053, false, ""},               // yalnız udp'de tanımlı → tcp için serbest
	}
	for _, c := range cases {
		http, baska := SELinuxPortDurumu(ornekSemanage, c.port)
		if http != c.http || baska != c.baska {
			t.Errorf(":%d → (%v,%q) beklenir, got (%v,%q)", c.port, c.http, c.baska, http, baska)
		}
	}
}
