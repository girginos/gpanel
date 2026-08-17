package provisioner

// panelhost paketi kendi :80 vhost'unu üretir. Bu dosya kaldırılabilir;
// ileriye dönük olarak provisioner'ın acme akışına dokunduğu yeri sabit
// tutmak için mevcut heal desenine bir kanca bıraktık.

// HealPanelAcmeChallengeOnStartup — no-op. panelhost.WebrootHazirla() ilk
// Ayarla() çağrısında bu işi yapıyor.
func HealPanelAcmeChallengeOnStartup() {}
