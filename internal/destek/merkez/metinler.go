package merkez

import "fmt"

// KvkkMetinSurumu — bot içi KVKK/aydınlatma metninin (metinler["kvkk"]) sürüm etiketi. Metnin İÇİNDE aynı
// etiket yazılıdır; TestKvkkMetinSurumuMetinleEsit ikisi ayrışırsa DÜŞER (sürümü değiştirmeden metin
// değişemez, tersi de).
// TODO(KVKK): portal aydınlatma metni sürümü "taslak-2"ye geçtiğinde bot metni portalla eşitlenecek
// (tek kaynak); o adımda hem metin hem bu sabit birlikte güncellenir. Metin içeriği bu işte DEĞİŞMEDİ.
const KvkkMetinSurumu = "taslak-2"

// KvkkKaynakTelegram — Baglanti.KVKKKaynak: kabul bot içi düğmeyle alındı (portal: "portal").
const KvkkKaynakTelegram = "telegram"

// Kullanıcıya giden sabit metinler (tr/en). HTML kipinde gönderilenlerde
// dinamik parçalar çağıran tarafta htmlKacis'ten geçirilir.
var metinler = map[string][2]string{
	"hosgeldin": {
		"Merhaba! Ben GirginOSPanel destek asistanıyım.\n\nBağlanmak için müşteri portalında (<b>app.girginos.io</b>) sunucunuzu seçip eşleştirme kodu oluşturun ve bağlantıya tıklayın (ya da <code>/start KOD</code> yazın). Eşleştirmeyi yalnız sunucunun sahibi yapabilir.\n\nKomutlar: /hesaplarim /durum /insan /dil /unutbeni",
		"Hello! I am the GirginOSPanel support assistant.\n\nTo connect, select your server in the customer portal (<b>app.girginos.io</b>), create a pairing code and open the link (or send <code>/start CODE</code>). Only the server owner can pair.\n\nCommands: /hesaplarim /durum /insan /dil /unutbeni",
	},
	"yardim": {
		"Komutlar:\n/hesaplarim — bağlı sunucular ve hesaplar\n/durum — bağlantı durumu\n/insan — insan desteğine aktar\n/dil tr|en — dil\n/unutbeni — bağlantıları ve kayıtları sil",
		"Commands:\n/hesaplarim — linked servers and accounts\n/durum — connection status\n/insan — hand over to a human\n/dil tr|en — language\n/unutbeni — delete links and records",
	},
	"kod_gecersiz":     {"Eşleştirme kodu geçersiz ya da süresi dolmuş. Portaldan yeni kod oluşturun.", "The pairing code is invalid or expired. Create a new code in the portal."},
	"kod_bicim":        {"Kod biçimi geçersiz.", "Invalid code format."},
	"kod_cok_deneme":   {"Çok fazla deneme. Biraz sonra tekrar deneyin.", "Too many attempts. Please try again later."},
	"ajan_cevrimdisi":  {"Sunucunuz şu an destek merkezine bağlı değil. Sunucunun açık ve internete erişebildiğinden emin olun; panel kapalı olsa da destek servisi çalışır.", "Your server is not connected to the support hub right now. Make sure the server is up and has internet access; the support service keeps running even if the panel is down."},
	"eslesme_bekliyor": {"Kod doğrulandı. Sunucunuzla bağlantı kuruluyor…", "Code verified. Connecting to your server…"},
	"baglanti_yok":     {"Henüz bağlı bir sunucunuz yok. Müşteri portalından eşleştirme kodu oluşturup /start KOD yazın.", "You have no linked server yet. Create a pairing code in the customer portal and send /start CODE."},
	"onay_bekliyor":    {"Bağlantınız henüz etkinleşmedi; eşleştirmeyi app.girginos.io üzerinden yeniden başlatın.", "Your link is not active yet; please restart pairing from app.girginos.io."},
	"destek_kapali":    {"Bu sunucuda destek botu yerel olarak kapatılmış (yönetici tarafından).", "The support bot has been switched off locally on this server (by the administrator)."},
	"kvkk": {
		"<b>Bağlantı etkin: %s</b>\n\n<b>Aydınlatma metni (taslak — hukuki onay bekliyor)</b> — sürüm: taslak-2\nSorununuzu teşhis etmek için sunucunuzdan alınan günlük ve durum bilgileri ile yazdığınız mesajlar, otomatik maskelenerek Telegram üzerinden iletilir ve yanıt üretimi amacıyla yurt dışındaki yapay zekâ sağlayıcısına (Anthropic, ABD) aktarılır. Maskeleme otomatiktir ve kusursuz değildir; parola, kart, IBAN gibi bilgileri yazmayın. Konuşma kayıtları maskelenerek 30 gün saklanır. /unutbeni ile Telegram kullanıcı numaranız, kullanıcı adınız, bağlantılarınız ve konuşma kayıtlarınız silinir (Telegram'ın kendi sohbet geçmişi ve sağlayıcıların kayıtları bizim silebildiğimiz veriler değildir). Tam metin ve haklarınız: app.girginos.io → Telegram Destek.\n\nDevam etmek için kabul edin.",
		"<b>Link active: %s</b>\n\n<b>Privacy notice (draft — pending legal approval)</b> — version: taslak-2\nTo diagnose your issue, log and status data from your server and the messages you write are automatically masked, sent via Telegram, and transferred to an AI provider abroad (Anthropic, USA) to generate answers. Masking is automatic and not perfect; do not type passwords, card numbers, IBANs or similar. Conversation records are masked and kept for 30 days. /unutbeni deletes your Telegram user ID, username, links and conversation records (Telegram's own chat history and the providers' records are not data we can delete). Full text and your rights: app.girginos.io → Telegram Support.\n\nPlease accept to continue.",
	},
	"baglanti_hazir":      {"<b>Bağlantı hazır: %s</b>\n\nAydınlatma metnini sunucu sahibi olarak müşteri portalında kabul ettiniz. Sorununuzu yazabilirsiniz.", "<b>Link ready: %s</b>\n\nYou accepted the privacy notice as the server owner in the customer portal. You can describe your issue now."},
	"sunucu_dogrulanmadi": {"Sunucunuzun lisansı doğrulanamadığı için eşleştirme yapılamadı. Portaldan sunucunuzun lisansını kontrol edip yeni kod oluşturun.", "The pairing could not be completed because your server's license could not be verified. Check the license in the portal and create a new code."},
	"kvkk_dugme":          {"✅ Kabul ediyorum", "✅ I accept"},
	"kvkk_gerekli":        {"Devam etmeden önce aydınlatma metnini kabul etmelisiniz.", "Please accept the privacy notice before continuing."},
	"kvkk_tamam":          {"Teşekkürler. Sorununuzu yazabilirsiniz.", "Thank you. You can describe your issue now."},
	"hiz":                 {"Çok hızlı mesaj gönderiyorsunuz; lütfen biraz bekleyin.", "You are sending messages too fast; please wait a moment."},
	"mesgul":              {"Bu sunucuda şu an başka destek oturumları sürüyor. Birkaç dakika sonra tekrar yazın.", "Other support sessions are active on this server. Please write again in a few minutes."},
	"isleniyor":           {"Önceki mesajınız hâlâ işleniyor.", "Your previous message is still being processed."},
	"inceleniyor":         {"🔎 İnceleniyor…", "🔎 Looking into it…"},
	"arac_calisiyor":      {"🔎 İnceleniyor… (%s)", "🔎 Looking into it… (%s)"},
	"yukseltme":           {"🔎 Daha ayrıntılı inceleniyor…", "🔎 Taking a deeper look…"},
	"selam":               {"Merhaba! Barındırma, alan adı, e-posta, SSL ya da site sorununuzu yazın; birlikte bakalım.", "Hello! Describe your hosting, domain, email, SSL or website issue and we will look into it."},
	"konu_disi":           {"Yalnız GirginOSPanel barındırma ve sunucu konularında yardımcı olabiliyorum.", "I can only help with GirginOSPanel hosting and server topics."},
	"zararli":             {"Bu isteğe yardımcı olamıyorum.", "I cannot help with this request."},
	"butce":               {"Şu an otomatik destek kullanılamıyor; sizi insan desteğine aktarıyorum.", "Automated support is unavailable right now; I am handing you over to human support."},
	"gecici_hata":         {"Geçici bir sorun oluştu; sizi insan desteğine aktarıyorum.", "A temporary problem occurred; I am handing you over to human support."},
	"refusal":             {"Bu konuda otomatik yanıt veremiyorum; sizi insan desteğine aktarıyorum.", "I cannot answer this automatically; I am handing you over to human support."},
	"cikis_red":           {"Yanıt güvenlik denetiminden geçmedi; sizi insan desteğine aktarıyorum. Kimseyle parola, kod ya da kart bilgisi paylaşmayın.", "The answer did not pass the safety check; I am handing you over to human support. Never share passwords, codes or card details."},
	"kesildi":             {"\n\n(yanıt kısaltıldı)", "\n\n(answer truncated)"},
	"bos_yanit":           {"Bir yanıt üretemedim; sizi insan desteğine aktarıyorum.", "I could not produce an answer; I am handing you over to human support."},
	"devir":               {"İnsan destek ekibine aktarıldınız; en kısa sürede dönüş yapılacak.", "You have been handed over to the human support team; they will get back to you soon."},
	"devir_grup_yok":      {"İnsan destek ekibine bu kanaldan şu an ulaşılamıyor; lütfen app.girginos.io üzerinden destek talebi açın.", "The human support team is not reachable via this channel right now; please open a ticket from app.girginos.io."},
	"dil_tamam":           {"Dil: Türkçe.", "Language: English."},
	"dil_bicim":           {"Kullanım: /dil tr ya da /dil en", "Usage: /dil tr or /dil en"},
	"unut_sor": {
		"Tüm bağlantılarınız, tercihleriniz ve konuşma kayıtlarınız bu botun kayıtlarından silinecek; sunucunuzdaki destek kaydından Telegram numaranız ve kullanıcı adınız çıkarılacak. Telegram'daki sohbet geçmişi, Anthropic tarafı ve (varsa) destek grubuna giden devir mesajı silinemez. Emin misiniz?",
		"All your links, preferences and conversation records will be deleted from this bot's records, and your Telegram ID and username will be removed from the support record on your server. Chat history in Telegram, the Anthropic side and (if any) a handover message sent to the support group cannot be deleted. Are you sure?",
	},
	"unut_dugme": {"🗑 Evet, sil", "🗑 Yes, delete"},
	"unut_tamam": {
		"Silindi.\n\nMerkezden silinenler: bağlantılarınız, dil/seçim tercihleriniz, konuşma kayıtlarınız (mesajlarınız, bot yanıtları, araç kayıtları) ve bekleyen eşleştirmeleriniz.\nSunucunuza iletilen: destek kaydınızdaki Telegram numaranız ve kullanıcı adınızın silinmesi. Güvenlik denetim günlüğünde yalnız ne yapıldığı kalır, kimin yaptığı \"tg:silindi\" olarak anonimleştirilir.\nSilinemeyenler: Telegram'daki sohbet geçmişiniz (Telegram'dan kendiniz silebilirsiniz), insan desteğine devir yaptıysanız destek grubundaki mesaj, Anthropic/Telegram tarafındaki kayıtlar.\n\nYeniden bağlanmak için portaldan yeni kod oluşturun.",
		"Deleted.\n\nDeleted from the hub: your links, language/selection preferences, conversation records (your messages, bot replies, tool records) and pending pairings.\nSent to your server: removal of your Telegram ID and username from its support record. In the security audit log only what was done remains; who did it is anonymised as \"tg:silindi\".\nNot deletable: your chat history in Telegram (you can delete it yourself in Telegram), a handover message in the support group if you requested a human, and records on the Anthropic/Telegram side.\n\nCreate a new code in the portal to reconnect.",
	},
	"unut_tamam_bekliyor": {
		"Merkezdeki kayıtlarınız silindi: bağlantılarınız, dil/seçim tercihleriniz, konuşma kayıtlarınız ve bekleyen eşleştirmeleriniz.\nSunucunuz şu an çevrimdışı olduğundan, destek kaydınızdaki Telegram numaranızın ve kullanıcı adınızın silinmesi sunucu yeniden bağlandığında otomatik tamamlanacak (bu sırada kayıt kullanılamaz). Güvenlik denetim günlüğünde yalnız ne yapıldığı kalır, kimin yaptığı anonimleştirilir.\nSilinemeyenler: Telegram'daki sohbet geçmişiniz, destek grubundaki devir mesajı (varsa), Anthropic/Telegram tarafındaki kayıtlar.",
		"Your records on the hub were deleted: links, language/selection preferences, conversation records and pending pairings.\nYour server is offline right now, so removal of your Telegram ID and username from its support record will complete automatically when it reconnects (the record cannot be used meanwhile). In the security audit log only what was done remains; who did it is anonymised.\nNot deletable: your chat history in Telegram, a handover message in the support group (if any), records on the Anthropic/Telegram side.",
	},
	"unut_hata":         {"Silme tamamlanamadı; lütfen biraz sonra /unutbeni ile yeniden deneyin.", "Deletion could not be completed; please try /unutbeni again shortly."},
	"secildi":           {"Etkin bağlantı: %s", "Active link: %s"},
	"metin_yok":         {"Lütfen sorununuzu metin olarak yazın.", "Please describe your issue as text."},
	"kapsam_degisti":    {"Bağlantınızın hesap listesi ya da yetkisi değişti; güncel bilgilerle incelemek için mesajınızı yeniden gönderin.", "The account list or permission of your link changed; please send your message again to continue with the current details."},
	"eslesme_red":       {"Sunucunuz eşleştirmeyi reddetti (%s). Portaldan yeni kod oluşturup yeniden deneyin.", "Your server rejected the pairing (%s). Create a new code in the portal and try again."},
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
