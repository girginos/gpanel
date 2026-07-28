package provisioner

import (
	"fmt"
	"os"
	"path/filepath"
)

// ── Marka sayfalari: yeni-domain karsilama + sunucu geneli 404 ───────────────
// Ikisi de TAM SAYFA iki-sutunlu duzen: solda Lottie animasyonu, sagda icerik.
// Mobilde tek sutuna iner (animasyon ustte serit olur).
//
// Animasyon + oynatici PAYLASILAN dizinden gelir (bkz. marka.go / EnsureMarkaAssets):
// `/_gosp/lottie.min.js` + `/_gosp/hazir.json`. Yuklenemezse (eski vhost'ta
// `location ^~ /_gosp/` yoksa, ya da JS kapaliysa) sayfa satir-ici SVG cizime
// duser — hicbir kosulda kirik gorsel cikmaz. DIS KAYNAK (CDN/webfont) YOK.

const (
	hataSayfaDizin = "/usr/share/girginospanel/errors"
	hataSayfaAd    = "_gosp_404.html"
	// HataSayfaYolu: nginx location = /_gosp_404.html icin dosya adi (root ile birleşir).
	HataSayfaYolu = "/" + hataSayfaAd
)

// markaStil: iki sayfanin ortak CSS'i (tek kaynak → gorsel tutarlilik).
// Sol panel HER IKI temada da acik yuzey: animasyonlarin hem koyu hem beyaz
// ogeleri var; acik-notr zemin ikisini de dogru gosteren tek secim.
const markaStil = `
  *{box-sizing:border-box;margin:0;padding:0}
  :root{
    --zemin:#ffffff; --zemin2:#f8fafc; --cizgi:#e7e5e4;
    --baslik:#1c1917; --metin:#57534e; --soluk:#a8a29e;
    --vurgu:#ea580c; --vurgu2:#f59e0b;
    --panel1:#f2f4fd; --panel2:#e6ebf9; --panelYazi:rgba(28,25,23,.32);
  }
  @media (prefers-color-scheme: dark){
    :root{
      --zemin:#0c0a09; --zemin2:#1c1917; --cizgi:#292524;
      --baslik:#fafaf9; --metin:#a8a29e; --soluk:#78716c;
      --vurgu:#fb923c; --vurgu2:#fbbf24;
      --panel1:#dfe4f4; --panel2:#ccd4ec; --panelYazi:rgba(28,25,23,.38);
    }
  }
  html,body{height:100%}
  body{
    font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
    background:var(--zemin);color:var(--baslik);-webkit-font-smoothing:antialiased;
  }
  .sayfa{min-height:100vh;min-height:100dvh;display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr)}

  /* ── Sol: animasyon paneli ── */
  .gorsel{
    position:relative;overflow:hidden;display:flex;align-items:center;justify-content:center;
    padding:24px;background:linear-gradient(150deg,var(--panel1),var(--panel2));
  }
  .anim{position:relative;z-index:1;width:min(88%,560px);aspect-ratio:1/1;display:none}
  .anim-var .anim{display:block}
  .anim-var .cizim{display:none}
  .cizim{position:relative;z-index:1;width:min(62%,360px);height:auto}
  .halka{transform-origin:150px 150px;animation:donen 26s linear infinite}
  .halka2{transform-origin:150px 150px;animation:donen 34s linear infinite reverse}
  .uydu{transform-origin:150px 150px;animation:donen 12s linear infinite}
  @keyframes donen{to{transform:rotate(360deg)}}
  .dev{
    position:absolute;z-index:1;bottom:26px;left:0;right:0;text-align:center;
    font-size:12px;letter-spacing:.16em;text-transform:uppercase;color:var(--panelYazi);
  }

  /* ── Sag: icerik ── */
  .icerik{
    display:flex;flex-direction:column;justify-content:center;
    padding:clamp(28px,6vw,84px);max-width:660px;
    animation:yuksel .7s cubic-bezier(.2,.7,.3,1) both;
  }
  @keyframes yuksel{from{opacity:0;transform:translateY(16px)}to{opacity:1;transform:none}}
  .ust{
    display:inline-flex;align-items:center;gap:9px;align-self:flex-start;
    font-size:11px;font-weight:700;letter-spacing:.16em;text-transform:uppercase;color:var(--soluk);
    margin-bottom:20px;
  }
  .canli{width:7px;height:7px;border-radius:50%;background:#22c55e;box-shadow:0 0 0 3px rgba(34,197,94,.16);animation:nabiz 2.4s ease-in-out infinite}
  @keyframes nabiz{0%,100%{opacity:1}50%{opacity:.45}}
  h1{
    font-size:clamp(30px,4.4vw,48px);line-height:1.1;letter-spacing:-.025em;
    font-weight:800;text-wrap:balance;word-break:break-word;margin-bottom:14px;
  }
  h1 .alan{
    background:linear-gradient(100deg,var(--vurgu),var(--vurgu2));
    -webkit-background-clip:text;background-clip:text;color:transparent;
  }
  .kod-buyuk{
    font-size:clamp(64px,11vw,120px);font-weight:800;line-height:.92;letter-spacing:-.05em;
    background:linear-gradient(100deg,var(--vurgu),var(--vurgu2));
    -webkit-background-clip:text;background-clip:text;color:transparent;margin-bottom:8px;
  }
  .spot{font-size:clamp(16px,1.6vw,19px);color:var(--metin);line-height:1.65;max-width:46ch}
  .adimlar{list-style:none;display:flex;flex-direction:column;gap:14px;margin:32px 0 28px}
  .adimlar li{display:flex;gap:14px;align-items:flex-start;font-size:15px;color:var(--metin);line-height:1.5}
  .no{
    flex:none;width:26px;height:26px;border-radius:9px;display:grid;place-items:center;
    background:var(--zemin2);border:1px solid var(--cizgi);
    font-size:12px;font-weight:700;color:var(--vurgu);font-variant-numeric:tabular-nums;
  }
  .adimlar b{color:var(--baslik);font-weight:600}
  code{
    background:var(--zemin2);border:1px solid var(--cizgi);padding:2px 7px;border-radius:6px;
    font-size:13px;color:var(--baslik);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
  }
  .dugme{
    display:inline-flex;align-items:center;gap:8px;align-self:flex-start;
    padding:11px 18px;border-radius:10px;text-decoration:none;font-size:15px;font-weight:600;
    color:#fff;background:linear-gradient(100deg,var(--vurgu),var(--vurgu2));
    box-shadow:0 8px 22px rgba(234,88,12,.26);transition:transform .15s ease,box-shadow .15s ease;
  }
  .dugme:hover{transform:translateY(-1px);box-shadow:0 12px 26px rgba(234,88,12,.32)}
  .dugme:focus-visible{outline:2px solid var(--vurgu);outline-offset:3px}
  .cizgi{height:1px;background:var(--cizgi);margin:34px 0 16px}
  .alt{font-size:13px;color:var(--soluk)}
  .alt a{color:var(--vurgu);text-decoration:none;font-weight:600}
  .alt a:hover{text-decoration:underline}
  .alt a:focus-visible{outline:2px solid var(--vurgu);outline-offset:2px;border-radius:3px}

  /* ── Mobil: tek sutun, animasyon ustte serit ── */
  @media (max-width:900px){
    .sayfa{grid-template-columns:1fr;grid-template-rows:minmax(210px,32vh) 1fr}
    .gorsel{padding:12px}
    .anim{width:min(70%,300px)}
    .cizim{width:min(46%,200px)}
    .dev{bottom:10px;font-size:11px}
    .icerik{padding:32px 24px 44px;max-width:none}
    .adimlar{margin:24px 0 22px}
  }
  @media (max-width:900px) and (max-height:560px){
    .sayfa{grid-template-rows:0 1fr}
    .gorsel{display:none}
  }
  @media (prefers-reduced-motion: reduce){
    .halka,.halka2,.uydu,.icerik,.canli{animation:none}
  }
`

// markaCizim: animasyon yuklenemezse gorunen satir-ici SVG yedegi.
const markaCizim = `  <svg class="cizim" viewBox="0 0 300 300" fill="none" aria-hidden="true">
    <g class="halka" opacity=".5"><circle cx="150" cy="150" r="128" stroke="url(#g1)" stroke-width="1" stroke-dasharray="3 9"/></g>
    <g class="halka2" opacity=".65"><circle cx="150" cy="150" r="98" stroke="url(#g1)" stroke-width="1.2" stroke-dasharray="26 14"/></g>
    <g class="uydu"><circle cx="150" cy="52" r="5" fill="url(#g2)"/><circle cx="150" cy="52" r="11" fill="url(#g2)" opacity=".2"/></g>
    <rect x="92" y="112" width="116" height="30" rx="9" fill="#1c1917" fill-opacity=".06" stroke="#1c1917" stroke-opacity=".12"/>
    <rect x="92" y="150" width="116" height="30" rx="9" fill="#1c1917" fill-opacity=".06" stroke="#1c1917" stroke-opacity=".12"/>
    <rect x="92" y="188" width="116" height="30" rx="9" fill="#1c1917" fill-opacity=".06" stroke="#1c1917" stroke-opacity=".12"/>
    <circle cx="108" cy="127" r="3.5" fill="url(#g2)"/>
    <circle cx="108" cy="165" r="3.5" fill="url(#g2)" opacity=".7"/>
    <circle cx="108" cy="203" r="3.5" fill="url(#g2)" opacity=".45"/>
    <defs>
      <linearGradient id="g1" x1="22" y1="22" x2="278" y2="278" gradientUnits="userSpaceOnUse">
        <stop stop-color="#ea580c" stop-opacity=".8"/><stop offset="1" stop-color="#f59e0b" stop-opacity=".15"/>
      </linearGradient>
      <linearGradient id="g2" x1="139" y1="41" x2="161" y2="63" gradientUnits="userSpaceOnUse">
        <stop stop-color="#ea580c"/><stop offset="1" stop-color="#f59e0b"/>
      </linearGradient>
    </defs>
  </svg>`

// markaAlt: her iki sayfanin ortak "Powered by" bloğu.
const markaAlt = `    <div class="cizgi"></div>
    <p class="alt">Powered by <a href="https://girginos.io" target="_blank" rel="noopener">girginos.io</a></p>`

// animScript: Lottie yukleyici. Basarisiz olursa (dosya yok / JS kapali /
// hareket-azaltma tercihi) SVG yedegi ekranda kalir.
func animScript(dosya string) string {
	return fmt.Sprintf(`<script src="/_gosp/lottie.min.js" defer></script>
<script>
window.addEventListener('load', function () {
  try {
    var az = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (az || !window.lottie) return;               // yedek SVG kalsin
    var kutu = document.getElementById('anim');
    if (!kutu) return;
    var a = window.lottie.loadAnimation({
      container: kutu, renderer: 'svg', loop: true, autoplay: true, path: '/_gosp/%s'
    });
    a.addEventListener('DOMLoaded', function () {
      document.documentElement.className += ' anim-var';   // yedegi gizle, animasyonu goster
    });
  } catch (e) { /* yedek SVG kalsin */ }
});
</script>`, dosya)
}

// welcomeHTML: yeni olusturulan domainin public_html/index.html karsilama sayfasi.
func welcomeHTML(domain string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="tr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>%s</title>
<style>%s</style>
</head>
<body>
<div class="sayfa">
  <aside class="gorsel">
    <div class="anim" id="anim" aria-hidden="true"></div>
%s
    <div class="dev">girginos</div>
  </aside>
  <main class="icerik">
    <span class="ust"><span class="canli"></span>Yayında</span>
    <h1>Web siteniz hazır<br><span class="alan">%s</span></h1>
    <p class="spot">Alan adınız sunucuya bağlandı ve isteklere yanıt veriyor. Şimdi içeriğinizi yükleyip yayına geçebilirsiniz.</p>
    <ol class="adimlar">
      <li><span class="no">1</span><span><b>Dosyalarınızı yükleyin</b> — FTP ya da panelin dosya yöneticisiyle <code>public_html/</code> klasörüne.</span></li>
      <li><span class="no">2</span><span><b>Veritabanınızı oluşturun</b> — panelden tek tıkla; bağlantı bilgileri hemen görüntülenir.</span></li>
      <li><span class="no">3</span><span><b>SSL sertifikanızı alın</b> — ücretsiz Let's Encrypt, otomatik yenilemeli.</span></li>
    </ol>
    <a class="dugme" href="https://girginos.io" target="_blank" rel="noopener">Kontrol paneline git &rarr;</a>
%s
  </main>
</div>
%s
</body>
</html>`, domain, markaStil, markaCizim, domain, markaAlt, animScript("hazir.json"))
}

// WelcomeHTML: disa acik sarmalayici — subdomain paketi de ayni marka sayfasini kullanir.
func WelcomeHTML(domain string) string { return welcomeHTML(domain) }

// error404HTML: sunucu geneli 404 sayfasi (tum domainler + subdomainler).
func error404HTML() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="tr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>404 — Sayfa bulunamadı</title>
<style>%s</style>
</head>
<body>
<div class="sayfa">
  <aside class="gorsel">
    <div class="anim" id="anim" aria-hidden="true"></div>
%s
    <div class="dev">girginos</div>
  </aside>
  <main class="icerik">
    <span class="ust">Hata 404</span>
    <div class="kod-buyuk">404</div>
    <h1>Sayfa bulunamadı</h1>
    <p class="spot">Aradığınız sayfa taşınmış, adı değişmiş ya da hiç var olmamış olabilir. Adresi kontrol edip yeniden deneyebilirsiniz.</p>
    <a class="dugme" href="/">&larr; Ana sayfaya dön</a>
%s
  </main>
</div>
%s
</body>
</html>`, markaStil, markaCizim, markaAlt, animScript("yok404.json"))
}

// Ensure404Page: sunucu geneli 404 sayfasini root-sahipli dizine yazar (idempotent).
// Init'ten cagrilir. Tenant bu dosyayi DEGISTIREMEZ (kendi home'unda degil).
func Ensure404Page() {
	if err := os.MkdirAll(hataSayfaDizin, 0o755); err != nil {
		return
	}
	yol := filepath.Join(hataSayfaDizin, hataSayfaAd)
	yeni := []byte(error404HTML())
	if mevcut, err := os.ReadFile(yol); err == nil && string(mevcut) == string(yeni) {
		return // degismemis
	}
	_ = os.WriteFile(yol, yeni, 0o644)
	_ = os.Chmod(hataSayfaDizin, 0o755)
}

// hata404Blok: vhost'lara eklenen nginx bloğu (marka 404 + animasyon varliklari).
// ACME/panel etkilenmez; 404 yalnizca nginx-seviyesi 404'lerde (dosya yok)
// devreye girer — uygulama kendi 404'unu uretiyorsa (WordPress vb.) o gecerlidir.
const hata404Blok = `    error_page 404 /_gosp_404.html;
    location = /_gosp_404.html {
        root /usr/share/girginospanel/errors;
        internal;
        access_log off;
    }
    location ^~ /_gosp/ {
        alias /usr/share/girginospanel/errors/;
        access_log off;
        expires 7d;
        gzip on;
        gzip_types application/json application/javascript;
    }
`
