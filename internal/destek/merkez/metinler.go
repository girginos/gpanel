package merkez

import "fmt"

// Kullanıcıya giden sabit metinler (tr/en). HTML kipinde gönderilenlerde
// dinamik parçalar çağıran tarafta htmlKacis'ten geçirilir.
var metinler = map[string][2]string{
	"hosgeldin": {
		"Merhaba! Ben GirginOSPanel destek asistanıyım.\n\nBağlanmak için panelde <b>Telegram Destek</b> sayfasından eşleştirme kodu oluşturun ve bağlantıya tıklayın (ya da <code>/start KOD</code> yazın).\n\nKomutlar: /hesaplarim /durum /insan /dil /unutbeni",
		"Hello! I am the GirginOSPanel support assistant.\n\nTo connect, create a pairing code on the <b>Telegram Support</b> page in your panel and open the link (or send <code>/start CODE</code>).\n\nCommands: /hesaplarim /durum /insan /dil /unutbeni",
	},
	"yardim": {
		"Komutlar:\n/hesaplarim — bağlı sunucular ve hesaplar\n/durum — bağlantı durumu\n/insan — insan desteğine aktar\n/dil tr|en — dil\n/unutbeni — bağlantıları ve kayıtları sil",
		"Commands:\n/hesaplarim — linked servers and accounts\n/durum — connection status\n/insan — hand over to a human\n/dil tr|en — language\n/unutbeni — delete links and records",
	},
	"kod_gecersiz":     {"Eşleştirme kodu geçersiz ya da süresi dolmuş. Panelden yeni kod oluşturun.", "The pairing code is invalid or expired. Create a new code in your panel."},
	"kod_bicim":        {"Kod biçimi geçersiz.", "Invalid code format."},
	"kod_cok_deneme":   {"Çok fazla deneme. Biraz sonra tekrar deneyin.", "Too many attempts. Please try again later."},
	"ajan_cevrimdisi":  {"Sunucunuz şu an destek merkezine bağlı değil. Panelde Telegram Destek'in açık olduğundan emin olun.", "Your server is not connected to the support hub right now. Make sure Telegram Support is enabled in your panel."},
	"eslesme_bekliyor": {"Kod doğrulandı. Eşleştirmeyi tamamlamak için panelde onay verin.", "Code verified. Approve the pairing in your panel to finish."},
	"baglanti_yok":     {"Henüz bağlı bir sunucunuz yok. Panelden eşleştirme kodu oluşturup /start KOD yazın.", "You have no linked server yet. Create a pairing code in your panel and send /start CODE."},
	"onay_bekliyor":    {"Bağlantınız panel onayı bekliyor.", "Your link is waiting for approval in the panel."},
	"destek_kapali":    {"Bu bağlantı için destek botu panelde kapalı.", "The support bot is disabled for this link in the panel."},
	"kvkk": {
		"<b>Bağlantı etkin: %s</b>\n\n<b>Aydınlatma metni (taslak — hukuki onay bekliyor)</b>\nSorununuzu teşhis etmek için sunucunuzdan alınan maskelenmiş günlük ve durum bilgileri, yanıt üretimi amacıyla yurt dışındaki yapay zekâ sağlayıcısına (Anthropic) aktarılır. Konuşma kayıtları maskelenerek 30 gün saklanır. /unutbeni ile bağlantılarınızı ve kayıtlarınızı silebilirsiniz.\n\nDevam etmek için kabul edin.",
		"<b>Link active: %s</b>\n\n<b>Privacy notice (draft — pending legal approval)</b>\nTo diagnose your issue, masked log and status data from your server is sent to an AI provider abroad (Anthropic) to generate answers. Conversation records are masked and kept for 30 days. Use /unutbeni to delete your links and records.\n\nPlease accept to continue.",
	},
	"kvkk_dugme":        {"✅ Kabul ediyorum", "✅ I accept"},
	"kvkk_gerekli":      {"Devam etmeden önce aydınlatma metnini kabul etmelisiniz.", "Please accept the privacy notice before continuing."},
	"kvkk_tamam":        {"Teşekkürler. Sorununuzu yazabilirsiniz.", "Thank you. You can describe your issue now."},
	"hiz":               {"Çok hızlı mesaj gönderiyorsunuz; lütfen biraz bekleyin.", "You are sending messages too fast; please wait a moment."},
	"mesgul":            {"Bu sunucuda şu an başka destek oturumları sürüyor. Birkaç dakika sonra tekrar yazın.", "Other support sessions are active on this server. Please write again in a few minutes."},
	"isleniyor":         {"Önceki mesajınız hâlâ işleniyor.", "Your previous message is still being processed."},
	"inceleniyor":       {"🔎 İnceleniyor…", "🔎 Looking into it…"},
	"arac_calisiyor":    {"🔎 İnceleniyor… (%s)", "🔎 Looking into it… (%s)"},
	"yukseltme":         {"🔎 Daha ayrıntılı inceleniyor…", "🔎 Taking a deeper look…"},
	"selam":             {"Merhaba! Barındırma, alan adı, e-posta, SSL ya da site sorununuzu yazın; birlikte bakalım.", "Hello! Describe your hosting, domain, email, SSL or website issue and we will look into it."},
	"konu_disi":         {"Yalnız GirginOSPanel barındırma ve sunucu konularında yardımcı olabiliyorum.", "I can only help with GirginOSPanel hosting and server topics."},
	"zararli":           {"Bu isteğe yardımcı olamıyorum.", "I cannot help with this request."},
	"butce":             {"Şu an otomatik destek kullanılamıyor; sizi insan desteğine aktarıyorum.", "Automated support is unavailable right now; I am handing you over to human support."},
	"gecici_hata":       {"Geçici bir sorun oluştu; sizi insan desteğine aktarıyorum.", "A temporary problem occurred; I am handing you over to human support."},
	"refusal":           {"Bu konuda otomatik yanıt veremiyorum; sizi insan desteğine aktarıyorum.", "I cannot answer this automatically; I am handing you over to human support."},
	"cikis_red":         {"Yanıt güvenlik denetiminden geçmedi; sizi insan desteğine aktarıyorum. Kimseyle parola, kod ya da kart bilgisi paylaşmayın.", "The answer did not pass the safety check; I am handing you over to human support. Never share passwords, codes or card details."},
	"kesildi":           {"\n\n(yanıt kısaltıldı)", "\n\n(answer truncated)"},
	"bos_yanit":         {"Bir yanıt üretemedim; sizi insan desteğine aktarıyorum.", "I could not produce an answer; I am handing you over to human support."},
	"devir":             {"İnsan destek ekibine aktarıldınız; en kısa sürede dönüş yapılacak.", "You have been handed over to the human support team; they will get back to you soon."},
	"devir_grup_yok":    {"İnsan destek ekibine bu kanaldan şu an ulaşılamıyor; lütfen panelden destek talebi açın.", "The human support team is not reachable via this channel right now; please open a ticket from your panel."},
	"dil_tamam":         {"Dil: Türkçe.", "Language: English."},
	"dil_bicim":         {"Kullanım: /dil tr ya da /dil en", "Usage: /dil tr or /dil en"},
	"unut_sor":          {"Tüm bağlantılarınız, tercihleriniz ve konuşma kayıtlarınız silinecek. Emin misiniz?", "All your links, preferences and conversation records will be deleted. Are you sure?"},
	"unut_dugme":        {"🗑 Evet, sil", "🗑 Yes, delete"},
	"unut_tamam":        {"Silindi. Yeniden bağlanmak için panelden yeni kod oluşturun.", "Deleted. Create a new code in your panel to reconnect."},
	"secildi":           {"Etkin bağlantı: %s", "Active link: %s"},
	"metin_yok":         {"Lütfen sorununuzu metin olarak yazın.", "Please describe your issue as text."},
	"baglanti_iptal":    {"Destek bağlantınız kapatıldı (%s).", "Your support link was closed (%s)."},
	"hesaplarim_baslik": {"<b>Bağlantılarınız</b>", "<b>Your links</b>"},
	"durum_baslik":      {"<b>Durum</b>", "<b>Status</b>"},
	"cevrimici":         {"çevrimiçi", "online"},
	"cevrimdisi":        {"çevrimdışı", "offline"},
}

func dilIndeks(dil string) int {
	if dil == "en" {
		return 1
	}
	return 0
}

// metin: anahtar yoksa anahtarın kendisi (testte yakalanır).
func metin(dil, anahtar string, a ...any) string {
	m, ok := metinler[anahtar]
	if !ok {
		return anahtar
	}
	s := m[dilIndeks(dil)]
	if len(a) > 0 {
		return fmt.Sprintf(s, a...)
	}
	return s
}

// kullaniciDili: tercih > Telegram dili > tr.
func kullaniciDili(k Kullanici, tgDil string) string {
	if k.Dil == "tr" || k.Dil == "en" {
		return k.Dil
	}
	if len(tgDil) >= 2 && tgDil[:2] == "en" {
		return "en"
	}
	return "tr"
}
