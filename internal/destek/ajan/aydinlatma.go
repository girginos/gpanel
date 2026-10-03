package ajan

// Aydinlatma: KVKK aydınlatma metni TASLAĞI (sürümlü). Panelde ikinci onay bu
// sürümün kabulüyle verilir; sürüm değişirse yeni eşleştirmeler yeni metni kabul eder
// (eski bağlantıların kabul ettiği sürüm destek_baglantilari.aydinlatma_surum'da kalır).
//
// 🔴 TASLAK: hukuki onay olmadan canlıda gerçek müşteriye açılmaz (tasarım kararı).
type Aydinlatma struct {
	Surum  string `json:"surum"`
	Taslak bool   `json:"taslak"`
	TR     string `json:"tr"`
	EN     string `json:"en"`
}

// AydinlatmaMetni: geçerli sürüm.
func AydinlatmaMetni() Aydinlatma {
	return Aydinlatma{Surum: aydinlatmaSurum, Taslak: true, TR: aydinlatmaTR, EN: aydinlatmaEN}
}

const aydinlatmaTR = `Telegram Destek Botu — Kişisel Verilerin İşlenmesine İlişkin Aydınlatma Metni (TASLAK)

1. Veri sorumlusu: Bu paneli size sunan hizmet sağlayıcı (sunucu sahibi) ve bot altyapısını işleten GirginOS.
2. İşlenen veriler: Telegram kullanıcı kimliğiniz ve kullanıcı adınız; destek sırasında yazdığınız mesajlar; botun sizin adınıza okuduğu hesap bilgileri ve günlük (log) kayıtlarından maskelenmiş ve kısaltılmış bölümler. Parola, anahtar, çerez, kart numarası gibi gizli değerler sunucudan çıkmadan maskelenir; IP adresleri kısaltılır, e-posta adreslerinin yerel kısmı gizlenir.
3. Amaç: Barındırma hesabınızla ilgili sorunların teşhisi ve size anlık destek verilmesi.
4. Aktarım: Mesajlarınız ve maskelenmiş araç çıktıları, yanıt üretmek için yapay zekâ hizmet sağlayıcısına (Anthropic, ABD) aktarılır. Bu aktarım KVKK md. 9 kapsamında açık rızanıza ve/veya standart sözleşmeye dayanır.
5. Saklama: Destek konuşmaları merkezde en çok 30 gün, panelde yalnız işlem kayıtları (araç adı, sonuç, süre; ham çıktı değil) en çok 180 gün saklanır.
6. Haklarınız: KVKK md. 11 kapsamındaki haklarınızı kullanabilir; bağlantıyı bu sayfadan ya da Telegram'da /unutbeni komutuyla dilediğiniz an kaldırabilirsiniz.
7. Sınırlar: Bot yalnız bu bağlantıya verdiğiniz kapsamda ve seçtiğiniz düzeyde çalışır; parola okuyamaz, veri silemez, yedek geri yükleyemez. Parolanız değişirse ya da hesabınız askıya alınırsa bağlantı kendiliğinden kaldırılır.`

const aydinlatmaEN = `Telegram Support Bot — Privacy Notice on the Processing of Personal Data (DRAFT)

1. Controller: The hosting provider who offers you this panel (the server owner) and GirginOS, which operates the bot infrastructure.
2. Data processed: Your Telegram user ID and username; the messages you write during support; masked and truncated excerpts of account information and log records that the bot reads on your behalf. Secrets such as passwords, keys, cookies and card numbers are masked before leaving the server; IP addresses are shortened and the local part of e-mail addresses is hidden.
3. Purpose: Diagnosing problems with your hosting account and giving you instant support.
4. Transfer: Your messages and masked tool output are transferred to an AI service provider (Anthropic, USA) to generate replies. This transfer relies on your explicit consent and/or standard contractual clauses under Article 9 of the Turkish Personal Data Protection Law (KVKK).
5. Retention: Support conversations are kept at the hub for at most 30 days; the panel keeps only operation records (tool name, result, duration — not raw output) for at most 180 days.
6. Your rights: You may exercise your rights under Article 11 of KVKK; you can remove the connection at any time from this page or with the /unutbeni command in Telegram.
7. Limits: The bot works only within the scope and at the level you grant to this connection; it cannot read passwords, delete data or restore backups. If your password changes or your account is suspended, the connection is removed automatically.`
