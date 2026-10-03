package tasima

// hedef.go — kaynak sunucu HEDEF denetimi ve kiracıya özel SSH dosyaları.
//
// 🔴 Bayi için (kullanıcı kararı: yalnız genel IP):
//   - Host bir kez çözülür; çözülen TÜM adresler agkapi katı modundan geçer:
//     loopback, RFC1918, CGNAT, bağlantı-yerel/bulut metadata, ULA, multicast,
//     belirtilmemiş, gömülü-v4 biçimleri ve sunucunun KENDİ adresleri reddedilir.
//     Tek bir izinsiz yanıt hepsini reddettirir.
//   - ssh/rsync ADA değil doğrulanan IP'ye bağlanır (-o HostKeyAlias=<ad>) →
//     denetim ile bağlantı arasında DNS yeniden bağlama (rebinding) yok.
//   - known_hosts kiracıya özeldir (bir bayinin kabul ettiği anahtar ötekini ya da
//     kökü etkilemez); dosya yolu bayiye gösterilmez.
// Kök için bugünkü davranış korunur (yalnız loopback koruması, Dogrula).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"girginospanel/internal/agkapi"
)

// errHedefIzinsiz — bayiye açıkça gösterilen ret (sabit metin).
var errHedefIzinsiz = errors.New("kaynak sunucu adresi internete açık (genel) bir IP adresine çözülmelidir; özel/iç ağ, loopback ve bu sunucunun kendi adresleri kullanılamaz")

// hedefCozucu — bayi hedef çözümleyicisi (nil → sistem DNS). Testler sahte yanıt verir.
var hedefCozucu func(ctx context.Context, host string) ([]net.IPAddr, error)

// hedefKendiAdresleri — sunucunun kendi adresleri (nil → arayüzler). Testler değiştirir.
var hedefKendiAdresleri func() ([]net.Addr, error)

// kokKnownHosts — kökün (eski, paylaşılan) known_hosts dosyası.
const kokKnownHosts = "/root/.ssh/known_hosts_tasima"

// kiraciDizin — bayinin taşıma dosyaları (anahtar çifti, known_hosts): <tasimaKeyDir>/<rid>.
func kiraciDizin(kp Kapsam) string {
	if !kp.Bayi() {
		return tasimaKeyDir
	}
	return filepath.Join(tasimaKeyDir, fmt.Sprintf("%d", kp.RID))
}

// hedefHazirla — kaynağı kapsama göre bağlantıya hazırlar: kiracı known_hosts'u ve
// (bayide) doğrulanmış bağlantı IP'si. Bayide izinsiz hedef → errHedefIzinsiz.
func (k *Kaynak) hedefHazirla(ctx context.Context, kp Kapsam) error {
	if !kp.Bayi() {
		k.knownHosts = kokKnownHosts
		return nil
	}
	k.bayi = true
	kapi := agkapi.Yeni(agkapi.Secenek{Cozucu: hedefCozucu, KendiAdresKaynagi: hedefKendiAdresleri})
	ipler, err := kapi.Coz(ctx, k.Host)
	if err != nil || len(ipler) == 0 {
		return errHedefIzinsiz
	}
	k.baglanIP = ipler[0].String()
	dizin := kiraciDizin(kp)
	if err := os.MkdirAll(dizin, 0o700); err != nil {
		return fmt.Errorf("taşıma dizini hazırlanamadı")
	}
	k.knownHosts = filepath.Join(dizin, "known_hosts")
	return nil
}

// sshHedefi — ssh'in bağlanacağı adres: bayide doğrulanmış IP, kökte girilen ad.
func (k *Kaynak) sshHedefi() string {
	if k.baglanIP != "" {
		return k.baglanIP
	}
	return k.Host
}

// rsyncHedefi — rsync uzak belirtimi için adres (IPv6 köşeli parantezle).
func (k *Kaynak) rsyncHedefi() string {
	h := k.sshHedefi()
	if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
		return "[" + h + "]"
	}
	return h
}

// knownHostsDosyasi — bu kaynağın known_hosts'u (boşsa kökün eski dosyası).
func (k *Kaynak) knownHostsDosyasi() string {
	if k.knownHosts != "" {
		return k.knownHosts
	}
	return kokKnownHosts
}
