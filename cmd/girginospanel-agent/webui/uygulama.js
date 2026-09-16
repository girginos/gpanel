console.log('GOSP-WIN-UI 0.3.0b');
/* gPanel Windows — gömülü yerel panel (go:embed ile Go binary içine girer).
   Sade ES2019: framework yok, modül yok, dış istek yok.
   GÜVENLİK (XSS): API'den gelen HER metin textContent / .title ile basılır;
   bu dosyada innerHTML HİÇ kullanılmaz (el + bosalt yardımcıları). */
(function () {
  'use strict';

  function $(id) { return document.getElementById(id); }

  // Güvenli eleman kurucu: metin daima textContent ile yazılır
  function el(etiket, sinif, metin) {
    var d = document.createElement(etiket);
    if (sinif) d.className = sinif;
    if (metin !== undefined && metin !== null) d.textContent = metin;
    return d;
  }

  function bosalt(dugum) { while (dugum.firstChild) dugum.removeChild(dugum.firstChild); }

  function cip(metin, tur) { return el('span', 'cip cip-' + tur, metin); }

  function yukleniyorYaz(kap) { bosalt(kap); kap.appendChild(el('div', 'yukleniyor', 'Yükleniyor…')); }

  function bosYaz(kap, mesaj) { bosalt(kap); kap.appendChild(el('div', 'yukleniyor', mesaj)); }

  function tabloKur(basliklar) {
    var tablo = el('table', 'tablo'), bas = el('thead'), tr = el('tr'), govde = el('tbody');
    basliklar.forEach(function (b) { tr.appendChild(el('th', null, b)); });
    bas.appendChild(tr);
    tablo.appendChild(bas);
    tablo.appendChild(govde);
    return { tablo: tablo, govde: govde };
  }

  // RFC3339 ise yerel biçime çevir; değilse olduğu gibi bas (yanlış ayrıştırma riski yok)
  function zamanBicimle(z) {
    if (!z) return '—';
    if (/^\d{4}-\d{2}-\d{2}T/.test(z)) {
      var t = new Date(z);
      if (!isNaN(t.getTime())) return t.toLocaleString('tr-TR');
    }
    return z;
  }

  // ---------- Tost (sağ üst, 4 sn) ----------
  function tost(mesaj, tur) {
    var kutu = el('div', 'tost ' + (tur === 'hata' ? 'tost-hata' : 'tost-basari'), mesaj);
    $('tost-kabi').appendChild(kutu);
    setTimeout(function () {
      kutu.classList.add('tost-gidiyor');
      setTimeout(function () { if (kutu.parentNode) kutu.parentNode.removeChild(kutu); }, 250);
    }, 4000);
  }

  function hataGoster(h) {
    if (h && h.yetkisiz) return; // 401: zaten giriş görünümüne düşüldü, ayrıca tost yok
    tost(h && h.message ? h.message : 'Bilinmeyen hata', 'hata');
  }

  // ---------- API sarmalayıcı ----------
  async function api(yol, ayar) {
    ayar = ayar || {};
    var istek = { method: ayar.method || (ayar.govde ? 'POST' : 'GET'), credentials: 'same-origin' };
    if (ayar.govde) {
      istek.headers = { 'Content-Type': 'application/json' };
      istek.body = JSON.stringify(ayar.govde);
    }
    var yanit;
    try { yanit = await fetch(yol, istek); }
    catch (agH) { throw new Error('Sunucuya ulaşılamadı'); }
    if (yanit.status === 401 && yol !== '/api/yerel/giris') {
      gosterGiris(); // herhangi bir uçtan 401 → giriş görünümüne düş
      var y = new Error('Oturum gerekli');
      y.yetkisiz = true;
      throw y;
    }
    var metin = await yanit.text();
    var veri = null;
    if (metin) { try { veri = JSON.parse(metin); } catch (e) { veri = null; } }
    if (!yanit.ok) {
      var hata = new Error(veri && veri.hata ? veri.hata : 'Sunucu hatası (' + yanit.status + ')');
      hata.kod = yanit.status; // sihirbazın 409 yeniden denemesi durum koduna bakar
      hata.uzunKod = veri && veri.kod; // makine-okunur kod (KURULUM_ASILI vb.) — hata zarfı (B-04)
      throw hata;
    }
    return veri;
  }

  // ---------- Görünüm geçişleri ----------
  function gosterGiris() {
    $('gorunum-panel').hidden = true;
    $('gorunum-giris').hidden = false;
    $('giris-parola').value = '';
    $('giris-kullanici').focus();
  }

  function gosterPanel() {
    $('gorunum-giris').hidden = true;
    $('gorunum-panel').hidden = false;
  }

  // ---------- Çok dil altyapısı ----------
  // Yeni dil eklemek = DILLER'e yeni bir sözlük nesnesi eklemek (ör. de:{...}).
  // Anahtarlar: Windows durum terimleri (Running…) VE arayüzün Türkçe metinleri.
  // 'tr' yalnız Windows terimlerini taşır; TR arayüz metni için anahtarın kendisi döner.
  var DILLER = {
    tr: {
      Running: 'Çalışıyor', Stopped: 'Durduruldu', Started: 'Başlatıldı', Paused: 'Duraklatıldı',
      StartPending: 'Başlatılıyor', StopPending: 'Durduruluyor',
      Auto: 'Otomatik', Automatic: 'Otomatik', Manual: 'El ile', Disabled: 'Devre dışı', Boot: 'Önyükleme', System: 'Sistem'
    },
    en: {
      Running: 'Running', Stopped: 'Stopped', Started: 'Started', Paused: 'Paused',
      StartPending: 'Starting', StopPending: 'Stopping',
      Auto: 'Automatic', Automatic: 'Automatic', Manual: 'Manual', Disabled: 'Disabled', Boot: 'Boot', System: 'System',
      'Genel Bakış': 'Overview', 'Siteler': 'Sites', 'Olay Günlüğü': 'Event Log', 'Görevler': 'Tasks',
      'Veritabanları': 'Databases', 'Servisler': 'Services', 'Kurulum': 'Setup', 'Çıkış': 'Sign out',
      'Genel': 'General', 'Barındırma': 'Hosting', 'Sunucu': 'Server', 'Profil': 'Profile',
      'Ara: site, veritabanı, sayfa…': 'Search: site, database, page…',
      'Yenile': 'Refresh', 'Sunucu': 'Server', 'Yetenekler': 'Capabilities', 'Zamanlanmış Görevler': 'Scheduled Tasks',
      'Kurulum Sihirbazı': 'Setup Wizard', 'Kullanıcı adı': 'Username', 'Parola': 'Password', 'Giriş yap': 'Sign in',
      'Site Ekle': 'Add Site', 'Oluştur': 'Create', 'Devam': 'Continue', 'Geri': 'Back',
      'Kurulumu Başlat': 'Start Setup', "Genel Bakış'a Dön": 'Back to Overview', 'Durdur': 'Stop',
      'Devam Et (kalanları kur)': 'Continue (install rest)', 'Microsoft görevleri': 'Microsoft tasks',
      'Hostname': 'Hostname', 'Sürüm': 'Version', 'Kanal': 'Channel',
      'Seç': 'Select', 'Onay': 'Confirm', 'Özet': 'Summary', 'Bileşen ara…': 'Search components…',
      'Tümü': 'All', 'Hata': 'Error', 'Uyarı': 'Warning',
      'Önce Kurulum sekmesinden bir veritabanı motoru kurun': 'First install a database engine from the Setup tab',
      'Deneysel bileşenler uzun sürebilir ve elle yapılandırma gerektirebilir': 'Experimental components may take long and require manual configuration',
      'Eşleşen bileşen yok': 'No matching component',
      'indiriliyor': 'downloading', 'kuruluyor': 'installing', 'indi': 'downloaded',
      'kalan —': 'ETA —', 'kalan ~': 'ETA ~', 'geçen': 'elapsed', 'tahmini': 'est.'
    }
  };

  function dilBul() {
    var k = null;
    try { k = localStorage.getItem('gosp_win_dil'); } catch (e) { k = null; }
    if (k && DILLER[k]) return k;
    var t = ((navigator.language || 'tr').slice(0, 2)).toLowerCase(); // tarayıcı dili
    return DILLER[t] ? t : 'tr';
  }
  var aktifDil = dilBul();

  // Aktif dile göre çevir; anahtar yoksa anahtarın kendisi döner (mevcut çağrıları bozmaz)
  function cevir(s) {
    if (s == null) return '';
    var d = DILLER[aktifDil] || DILLER.tr;
    return (d && d[s]) || s;
  }

  // Türkçe-duyarsız küçük harf (arama karşılaştırması için)
  function trk(s) { return (s || '').toLocaleLowerCase('tr'); }

  // data-i18n / data-i18n-ph taşıyan tüm statik metinleri aktif dile göre yeniden yaz
  function metinUygula() {
    var e = document.querySelectorAll('[data-i18n]');
    for (var i = 0; i < e.length; i++) e[i].textContent = cevir(e[i].getAttribute('data-i18n'));
    var p = document.querySelectorAll('[data-i18n-ph]');
    for (var j = 0; j < p.length; j++) p[j].placeholder = cevir(p[j].getAttribute('data-i18n-ph'));
    var s = document.querySelectorAll('.dil-sec');
    for (var m = 0; m < s.length; m++) s[m].value = aktifDil;
    document.documentElement.lang = aktifDil;
  }

  function dilDegistir(d) {
    if (!DILLER[d]) return;
    aktifDil = d;
    try { localStorage.setItem('gosp_win_dil', d); } catch (e) { /* depolama yoksa yut */ }
    metinUygula();
    if ($('gorunum-panel').hidden) return;                 // giriş ekranı: yalnız statik metin
    if (aktifSekme === 'kurulum') { if (sihirbaz.adim === 1) katalogBas(); return; } // sihirbazı bozma
    if (yuklendi[aktifSekme] && yukleyici[aktifSekme]) yukleyici[aktifSekme](); // dinamik içeriği tazele
  }

  // ---------- Sekmeler ----------
  var yukleyici = { ozet: yukleOzet, siteler: yukleSiteler, olaylar: yukleOlaylar, gorevler: yukleGorevler, vt: yukleVt, servisler: yukleServisler, kurulum: yukleKurulum, plan: yuklePlan, ayarlar: yukleAyarlar };
  var yuklendi = {};
  var aktifSekme = 'ozet'; // kaynak oto-yenileme yalnız Servisler aktifken çalışır

  // ── İki katmanlı sidebar: ray bölümü ↔ sekme eşlemesi ──
  // Her data-sekme bir bölüme aittir; ray ikonu o bölümün etiketli panelini açar.
  var SEKME_BOLUM = {
    ozet: 'genel',
    siteler: 'barindirma', vt: 'barindirma', plan: 'barindirma',
    servisler: 'sunucu', kurulum: 'sunucu', gorevler: 'sunucu', olaylar: 'sunucu',
    ayarlar: 'profil'
  };
  var BOLUM_BASLIK = { genel: 'Genel', barindirma: 'Barındırma', sunucu: 'Sunucu', profil: 'Profil' };

  // Ray bölümünü etkinleştir: ray ikonu vurgulanır, o bölümün sekme grubu görünür,
  // panel başlığı (caption) güncellenir. İçerik sekmesini DEĞİŞTİRMEZ (onu sekmeAc yapar).
  function bolumAc(anahtar) {
    if (!BOLUM_BASLIK[anahtar]) anahtar = 'genel';
    var raylar = document.querySelectorAll('.ray-dugme');
    for (var i = 0; i < raylar.length; i++) {
      var ra = raylar[i].getAttribute('data-bolum') === anahtar;
      raylar[i].classList.toggle('aktif', ra);
      raylar[i].setAttribute('aria-current', ra ? 'true' : 'false');
    }
    var gruplar = document.querySelectorAll('.sekme-grup');
    for (var j = 0; j < gruplar.length; j++) {
      gruplar[j].hidden = gruplar[j].getAttribute('data-grup') !== anahtar;
    }
    var cap = $('panel-baslik-metin');
    if (cap) { cap.setAttribute('data-i18n', BOLUM_BASLIK[anahtar]); cap.textContent = cevir(BOLUM_BASLIK[anahtar]); }
  }

  function sekmeAc(ad) {
    aktifSekme = ad;
    try { localStorage.setItem('gosp_win_aktif_sekme', ad); } catch (e) { /* depolama yoksa yut */ }
    var dugmeler = document.querySelectorAll('.sekme');
    for (var i = 0; i < dugmeler.length; i++) {
      var aktif = dugmeler[i].getAttribute('data-sekme') === ad;
      dugmeler[i].classList.toggle('aktif', aktif);
      dugmeler[i].setAttribute('aria-current', aktif ? 'true' : 'false');
    }
    bolumAc(SEKME_BOLUM[ad] || 'genel'); // içerik sekmesi ↔ ray bölümü senkron kalsın
    ['ozet', 'siteler', 'olaylar', 'gorevler', 'vt', 'servisler', 'kurulum', 'plan', 'ayarlar'].forEach(function (s) { $('sekme-' + s).hidden = (s !== ad); });
    if (!yuklendi[ad]) { yuklendi[ad] = true; yukleyici[ad](); } // her sekme ilk açılışta bir kez çeker
  }

  // ---------- Genel Bakış ----------
  var YETENEKLER = [
    [1, 'Site'], [128, 'Olay Günlüğü'], [256, 'Görevler'], [512, 'MSSQL'],
    [1024, 'FTP'], [2048, 'DNS'], [4096, '.NET'], [8192, 'MySQL'], [16384, 'PgSQL']
  ];

  function ozetBas(o) {
    sonOzet = o || {};
    $('ust-hostname').textContent = o.hostname || '';
    $('ust-rozet').textContent = (o.surum || '?') + ' · ' + (o.kanal || '?');
  }

  async function yukleOzet() {
    try { ozetBas(await api('/api/yerel/ozet')); }
    catch (h) { hataGoster(h); }
    dashDurumYukle();
  }

  // Dashboard "Sistem Durumu": CPU/RAM/Disk gauge + site sayisi (best-effort, sessiz hata).
  // ═══════════════════════════════════════════════════════════════════════
  // DASHBOARD — Linux gPanel "gp-dash" tasariminin birebir gorsel karsiligi.
  // recharts yok (CDN yasak) → grafik/donut/halka inline SVG. Veri: /kaynak
  // (cpu_yuzde, ram{yuzde,kul_mb,top_mb}, disk[]), /siteler, /servisler, /ozet.
  // ═══════════════════════════════════════════════════════════════════════
  var sonOzet = null;      // /ozet son yaniti (host/surum/kanal/yetenekler)
  var kaynakBuf = [];      // son <=24 ornek {cpu,ram,disk} — spark + grafik
  var dashTab = 'cpu';     // Sistem Kaynaklari grafik sekmesi

  // ikonlar: SABIT SVG dizeleri (sunucu verisi DEGIL → innerHTML guvenli).
  var DIK = {
    site: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><line x1="6" y1="7" x2="6.01" y2="7"/><line x1="6" y1="17" x2="6.01" y2="17"/></svg>',
    servis: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8v13H3V8"/><path d="M1 3h22v5H1z"/><line x1="10" y1="12" x2="14" y2="12"/></svg>',
    cpu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v2M15 2v2M9 20v2M15 20v2M2 9h2M2 15h2M20 9h2M20 15h2"/></svg>',
    ram: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 7V5M12 7V5M17 7V5M6 17v2M18 17v2"/></svg>',
    disk: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="12" x2="2" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" y1="16" x2="6.01" y2="16"/><line x1="10" y1="16" x2="10.01" y2="16"/></svg>',
    saglik: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>',
    grafik: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
    vt: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14a9 3 0 0 0 18 0V5"/><path d="M3 12a9 3 0 0 0 18 0"/></svg>',
    kurulum: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>',
    ayar: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
    yildirim: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>',
    globe: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a13.5 13.5 0 0 1 0 18 13.5 13.5 0 0 1 0-18z"/></svg>'
  };

  function svgDugum(str) { var d = el('div'); d.innerHTML = str; return d.firstChild; }
  function saten(v) { return Math.max(0, Math.min(100, v || 0)); }

  // kart ici sparkline (0..100 dizisi)
  function sparkSVG(pts, renk, id) {
    var W = 300, H = 46;
    if (!pts || pts.length < 2) return svgDugum('<svg class="stat-spark" viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none"></svg>');
    var n = pts.length, mx = W / (n - 1), yy = function (v) { return (H - 4) * (1 - saten(v) / 100) + 2; };
    var line = 'M0 ' + yy(pts[0]).toFixed(1);
    for (var i = 1; i < n; i++) line += ' L' + (i * mx).toFixed(1) + ' ' + yy(pts[i]).toFixed(1);
    var area = line + ' L' + W + ' ' + H + ' L0 ' + H + ' Z';
    return svgDugum('<svg class="stat-spark" viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none">' +
      '<defs><linearGradient id="sp-' + id + '" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="' + renk + '" stop-opacity="0.34"/><stop offset="100%" stop-color="' + renk + '" stop-opacity="0"/></linearGradient></defs>' +
      '<path d="' + area + '" fill="url(#sp-' + id + ')"/><path d="' + line + '" fill="none" stroke="' + renk + '" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"/></svg>');
  }

  // buyuk alan grafik (Sistem Kaynaklari)
  function alanGrafikSVG(vals, renk) {
    var W = 600, H = 180, grid = '';
    [25, 50, 75].forEach(function (g) { var y = H * (1 - g / 100); grid += '<line x1="0" y1="' + y.toFixed(1) + '" x2="' + W + '" y2="' + y.toFixed(1) + '" stroke="rgba(255,255,255,.06)" stroke-dasharray="3 3"/>'; });
    if (!vals || vals.length < 2) return svgDugum('<svg viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none" style="width:100%;height:100%">' + grid + '</svg>');
    var n = vals.length, mx = W / (n - 1), yy = function (v) { return H * (1 - saten(v) / 100); };
    var line = 'M0 ' + yy(vals[0]).toFixed(1);
    for (var i = 1; i < n; i++) line += ' L' + (i * mx).toFixed(1) + ' ' + yy(vals[i]).toFixed(1);
    var area = line + ' L' + W + ' ' + H + ' L0 ' + H + ' Z';
    return svgDugum('<svg viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none" style="width:100%;height:100%">' +
      '<defs><linearGradient id="ag" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="' + renk + '" stop-opacity="0.3"/><stop offset="100%" stop-color="' + renk + '" stop-opacity="0"/></linearGradient></defs>' +
      grid + '<path d="' + area + '" fill="url(#ag)"/><path d="' + line + '" fill="none" stroke="' + renk + '" stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke"/></svg>');
  }

  // donut (segments: [{ad,deger,renk}])
  function donutSVG(segs) {
    var R = 54, C = 2 * Math.PI * R, acc = 0, top = segs.reduce(function (a, x) { return a + x.deger; }, 0) || 1;
    var parts = '<circle cx="70" cy="70" r="' + R + '" fill="none" stroke="#18232e" stroke-width="16"/>';
    segs.forEach(function (x) { var dash = C * (x.deger / top); parts += '<circle cx="70" cy="70" r="' + R + '" fill="none" stroke="' + x.renk + '" stroke-width="16" stroke-dasharray="' + dash.toFixed(2) + ' ' + (C - dash).toFixed(2) + '" stroke-dashoffset="' + (-acc).toFixed(2) + '"/>'; acc += dash; });
    return svgDugum('<svg viewBox="0 0 140 140"><g transform="rotate(-90 70 70)">' + parts + '</g></svg>');
  }

  function ilerleme(v, varyant) {
    var p = el('div', 'progress'), f = el('div', 'progress-fill' + (varyant ? ' ' + varyant : ''));
    f.style.width = Math.max(2, saten(v)) + '%'; p.appendChild(f); return p;
  }

  // stat karti
  function statKarti(baslik, deger, alt, ikon, sparkPts, sparkRenk, sparkId) {
    var c = el('div', 'stat-card');
    c.appendChild(el('div', 'glow'));
    var copy = el('div', 'stat-copy');
    copy.appendChild(el('div', 'muted', cevir(baslik)));
    var vr = el('div', 'stat-value-row'); vr.appendChild(el('strong', null, deger == null ? '—' : String(deger)));
    copy.appendChild(vr);
    copy.appendChild(el('div', 'tiny muted', alt || ''));
    c.appendChild(copy);
    var ic = el('div', 'stat-icon'); ic.appendChild(svgDugum(ikon)); c.appendChild(ic);
    if (sparkPts) c.appendChild(sparkSVG(sparkPts, sparkRenk || '#3b82f6', sparkId || 'x'));
    return c;
  }

  // kart kabugu (baslik + govde cagiran tarafindan eklenir)
  function dashKart(baslik, ikon, aksiyon, fn) {
    var s = el('section', 'card');
    var head = el('div', 'card-head'), title = el('div', 'card-title');
    if (ikon) title.appendChild(svgDugum(ikon));
    title.appendChild(el('span', null, cevir(baslik)));
    head.appendChild(title);
    if (aksiyon) { var b = el('button', 'ghost-button', cevir(aksiyon)); b.type = 'button'; if (fn) b.addEventListener('click', fn); head.appendChild(b); }
    s.appendChild(head);
    return s;
  }

  // ── ANA CIZIM (dashDurumYukle adi KORUNUR — yukleOzet + oto-yenile bunu cagirir) ──
  async function dashDurumYukle() {
    var kap = $('gp-dash-kap');
    if (!kap) return;
    // 🔴 PARALEL + ZAMAN ASIMLI: bir uc yavaslar/asilirsa panel BLANK kalmasin (6sn -> null).
    var za = function (p) { return Promise.race([Promise.resolve(p), new Promise(function (rz) { setTimeout(function () { rz(null); }, 6000); })]).catch(function () { return null; }); };
    var _r = await Promise.all([za(api('/api/yerel/kaynak')), za(api('/api/yerel/siteler')), za(api('/api/yerel/servisler'))]);
    var k = _r[0] || null;
    var siteSay = _r[1] ? (((_r[1].siteler) || []).length) : null;
    var servisler = _r[2] ? ((_r[2].servisler) || []) : [];

    var cpuY = k ? Math.round(k.cpu_yuzde || 0) : null;
    var ramY = (k && k.ram) ? Math.round(k.ram.yuzde || 0) : null;   // 🔴 nested (bug fix)
    var disk0 = (k && k.disk && k.disk[0]) || null;
    var diskY = disk0 ? Math.round(disk0.Yuzde || 0) : null;
    if (k) {
      var ornek = { cpu: cpuY || 0, ram: ramY || 0, disk: diskY || 0 };
      if (kaynakBuf.length === 0) { for (var sd = 0; sd < 7; sd++) kaynakBuf.push(ornek); } // ilk yukte grafik bos kalmasin (duz baslangic)
      kaynakBuf.push(ornek);
      while (kaynakBuf.length > 24) kaynakBuf.shift();
    }

    var calisiyorMu = function (s) { return s.Durum === 'Running' || s.Durum === 'Çalışıyor'; };
    var sAktif = servisler.filter(calisiyorMu).length, sTop = servisler.length, sDown = sTop - sAktif;
    var skor = k ? Math.max(0, Math.min(100, 100 - sDown * 12 - (diskY > 90 ? 15 : diskY > 80 ? 6 : 0) - (cpuY > 92 ? 8 : 0))) : 0;
    var skorRenk = skor >= 85 ? '#10b981' : skor >= 60 ? '#f59e0b' : '#f43f5e';
    var skorAd = skor >= 85 ? cevir('Mükemmel') : skor >= 60 ? cevir('İyi') : skor >= 40 ? cevir('Dikkat') : cevir('Kritik');
    var o = sonOzet || {};

    try {
    bosalt(kap);
    var kok = el('div', 'gp-dash');

    // ── sayfa basligi ──
    var pt = el('div', 'page-title'), ptsol = el('div');
    var h1 = el('h1', null, cevir('Genel Bakış')); ptsol.appendChild(h1);
    ptsol.appendChild(el('p', null, (o.hostname || '—') + ' · ' + (o.surum || '?') + ' · ' + (o.kanal || '?')));
    pt.appendChild(ptsol);
    var acts = el('div', 'title-actions');
    var yb = el('button', 'secondary'); yb.type = 'button'; yb.appendChild(svgDugum(DIK.grafik)); yb.appendChild(document.createTextNode(' ' + cevir('Yenile')));
    yb.addEventListener('click', function () { yukleOzet(); });
    var eb = el('button', 'primary'); eb.type = 'button'; eb.appendChild(svgDugum(DIK.globe)); eb.appendChild(document.createTextNode(' ' + cevir('Site Ekle')));
    eb.addEventListener('click', function () { sekmeAc('siteler'); });
    acts.appendChild(yb); acts.appendChild(eb); pt.appendChild(acts);
    kok.appendChild(pt);

    // ── stat grid ──
    var sg = el('div', 'stats-grid');
    sg.appendChild(statKarti('Siteler', siteSay == null ? '—' : siteSay, cevir('IIS sitesi'), DIK.site));
    sg.appendChild(statKarti('Servisler', k ? (sAktif + '/' + sTop) : '—', sDown === 0 ? cevir('Tümü çalışıyor') : (sDown + ' ' + cevir('kapalı')), DIK.servis));
    sg.appendChild(statKarti('CPU', cpuY == null ? '—' : ('%' + cpuY), cevir('Anlık kullanım'), DIK.cpu, kaynakBuf.map(function (d) { return d.cpu; }), '#10b981', 'cpu'));
    sg.appendChild(statKarti('RAM', ramY == null ? '—' : ('%' + ramY), (k && k.ram) ? (mb(k.ram.kul_mb) + ' / ' + mb(k.ram.top_mb)) : '…', DIK.ram, kaynakBuf.map(function (d) { return d.ram; }), '#8b5cf6', 'ram'));
    sg.appendChild(statKarti('Disk', diskY == null ? '—' : ('%' + diskY), disk0 ? (disk0.BosGB + ' / ' + disk0.TopGB + ' GB ' + cevir('boş')) : '…', DIK.disk, kaynakBuf.map(function (d) { return d.disk; }), '#f59e0b', 'disk'));
    var skorK = statKarti('Sistem Sağlığı', k ? skor : '—', skorAd, DIK.saglik); sg.appendChild(skorK);
    kok.appendChild(sg);

    // ── satir 1 ──
    var g1 = el('div', 'grid grid-main');
    // Sistem Kaynaklari (grafik)
    var kkart = dashKart('Sistem Kaynakları', DIK.grafik, cevir('Son 24 örnek'));
    var TABRENK = { cpu: '#10b981', ram: '#8b5cf6', disk: '#f59e0b' };
    var tabs = el('div', 'tabs');
    [['cpu', 'CPU'], ['ram', cevir('Bellek')], ['disk', cevir('Disk')]].forEach(function (t) {
      var tb = el('button', dashTab === t[0] ? 'selected' : '', t[1]); tb.type = 'button';
      if (dashTab === t[0]) { tb.style.color = TABRENK[t[0]]; tb.style.borderBottomColor = TABRENK[t[0]]; }
      tb.addEventListener('click', function () { dashTab = t[0]; dashDurumYukle(); });
      tabs.appendChild(tb);
    });
    kkart.appendChild(tabs);
    var cw = el('div', 'chart');
    cw.appendChild(alanGrafikSVG(kaynakBuf.map(function (d) { return d[dashTab]; }), TABRENK[dashTab]));
    kkart.appendChild(cw); g1.appendChild(kkart);

    // Sunucu & Servisler
    var skart = dashKart('Sunucu & Servisler', DIK.sunucu || DIK.site, cevir('Tümünü Gör'), function () { sekmeAc('servisler'); });
    var sl = el('div', 'server-list');
    if (!k) { var b = el('div', 'alerts'); b.appendChild(el('div', 'bos', cevir('Yükleniyor…'))); sl.appendChild(b); }
    else {
      var hr = el('div', 'server-row'), hm = el('div', 'server-main');
      var hi = el('div', 'server-icon'); hi.appendChild(svgDugum(DIK.site)); hm.appendChild(hi);
      var hd = el('div'); hd.appendChild(el('strong', null, o.hostname || '—')); hd.appendChild(el('small', null, (o.surum || ''))); hm.appendChild(hd);
      hr.appendChild(hm); hr.appendChild(el('span', 'status', cevir('Aktif')));
      var met = el('div', 'metrics');
      var m1 = el('div'); var l1 = el('label'); l1.appendChild(document.createTextNode('CPU ')); l1.appendChild(el('b', null, '%' + (cpuY || 0))); m1.appendChild(l1); m1.appendChild(ilerleme(cpuY, cpuY > 70 ? 'orange' : '')); met.appendChild(m1);
      var m2 = el('div'); var l2 = el('label'); l2.appendChild(document.createTextNode('RAM ')); l2.appendChild(el('b', null, '%' + (ramY || 0))); m2.appendChild(l2); m2.appendChild(ilerleme(ramY, ramY > 75 ? 'orange' : 'purple')); met.appendChild(m2);
      hr.appendChild(met); sl.appendChild(hr);
      servisler.forEach(function (sv) {
        var r = el('div', 'server-row'), mn = el('div', 'server-main');
        var ic = el('div', 'server-icon'); ic.appendChild(svgDugum(DIK.servis)); mn.appendChild(ic);
        var dv = el('div'); dv.appendChild(el('strong', null, sv.GoruntuAd || sv.Ad || '—')); dv.appendChild(el('small', null, sv.Ad || '')); mn.appendChild(dv);
        r.appendChild(mn);
        r.appendChild(el('span', 'status' + (calisiyorMu(sv) ? '' : ' down'), calisiyorMu(sv) ? cevir('Aktif') : cevir('Kapalı')));
        sl.appendChild(r);
      });
    }
    skart.appendChild(sl); g1.appendChild(skart);

    // Sistem Sagligi (halka)
    var hkart = dashKart('Sistem Sağlığı', DIK.saglik);
    var hlth = el('div', 'health');
    var ring = el('div', 'health-ring'); ring.style.background = 'conic-gradient(' + skorRenk + ' 0 ' + skor + '%, #18232e ' + skor + '%)';
    var ri = el('div'); ri.appendChild(el('strong', null, k ? String(skor) : '—')); var rs = el('small', null, k ? skorAd : ''); rs.style.color = skorRenk; ri.appendChild(rs); ring.appendChild(ri);
    hlth.appendChild(ring);
    var hlist = el('div', 'health-list');
    [[cevir('Servisler'), k ? (sAktif + ' / ' + sTop) : '—', sDown === 0],
     [cevir('Disk Durumu'), diskY == null ? '—' : (diskY < 90 ? cevir('Sağlıklı') : '%' + diskY), diskY != null && diskY < 90],
     [cevir('CPU'), cpuY == null ? '—' : (cpuY < 85 ? cevir('Normal') : '%' + cpuY), cpuY != null && cpuY < 85],
     [cevir('Bellek'), ramY == null ? '—' : (ramY < 85 ? cevir('Normal') : '%' + ramY), ramY != null && ramY < 85]
    ].forEach(function (row) {
      var d = el('div'); d.appendChild(el('span', null, row[0]));
      var bb = el('b', row[2] ? '' : 'warn', String(row[1])); if (!row[2]) bb.style.color = '#fb923c'; d.appendChild(bb); hlist.appendChild(d);
    });
    hlth.appendChild(hlist); hkart.appendChild(hlth); g1.appendChild(hkart);
    kok.appendChild(g1);

    // ── satir 2 ──
    var g2 = el('div', 'grid grid-second');
    // Depolama (donut)
    var dkart = dashKart('Depolama Kullanımı', DIK.disk);
    if (disk0) {
      var kullB = Math.max(0, (disk0.TopGB - disk0.BosGB));
      var segs = [{ ad: cevir('Kullanılan'), deger: kullB, renk: '#3b82f6' }, { ad: cevir('Boş'), deger: disk0.BosGB, renk: '#10b981' }];
      var depo = el('div', 'depo'), dring = el('div', 'depo-ring'); dring.appendChild(donutSVG(segs));
      var dic = el('div', 'depo-ic'), dicin = el('div'); dicin.appendChild(el('strong', null, kullB.toFixed(1) + ' GB')); dicin.appendChild(el('small', null, cevir('Kullanılan'))); dic.appendChild(dicin); dring.appendChild(dic);
      depo.appendChild(dring);
      var dlist = el('div', 'depo-list');
      segs.forEach(function (x) { var row = el('div'); var i = el('i'); i.style.background = x.renk; row.appendChild(i); row.appendChild(el('span', null, x.ad)); row.appendChild(el('b', null, x.deger.toFixed(1) + ' GB')); dlist.appendChild(row); });
      depo.appendChild(dlist); dkart.appendChild(depo);
      var dalt = el('div', 'depo-alt'); var db = el('span', 'depo-bos'); db.appendChild(el('b', null, disk0.BosGB + ' GB')); db.appendChild(document.createTextNode(' ' + cevir('boş') + ' · ' + disk0.TopGB + ' GB ' + cevir('toplam'))); dalt.appendChild(db); dkart.appendChild(dalt);
    } else { var bd = el('div', 'alerts'); bd.appendChild(el('div', 'bos', cevir('Disk verisi yok'))); dkart.appendChild(bd); }
    g2.appendChild(dkart);

    // Hizli islemler
    var qkart = dashKart('Hızlı İşlemler', DIK.yildirim);
    var qg = el('div', 'quick-grid');
    [[DIK.globe, cevir('Site Ekle'), 'siteler'], [DIK.vt, cevir('Veritabanı'), 'vt'], [DIK.disk, cevir('Planlar'), 'plan'],
     [DIK.servis, cevir('Servisler'), 'servisler'], [DIK.kurulum, cevir('Kurulum'), 'kurulum'], [DIK.ayar, cevir('Ayarlar'), 'ayarlar']
    ].forEach(function (q) {
      var b = el('button'); b.type = 'button'; b.appendChild(svgDugum(q[0])); b.appendChild(el('span', null, q[1]));
      b.addEventListener('click', function () { sekmeAc(q[2]); }); qg.appendChild(b);
    });
    qkart.appendChild(qg); g2.appendChild(qkart);

    // Yetenekler
    var ykart = dashKart('Yetenekler', DIK.saglik);
    var yc = el('div', 'cip-dizi'); var bitler = o.yetenekler || 0, adet = 0;
    if (typeof YETENEKLER !== 'undefined') YETENEKLER.forEach(function (y) { if (bitler & y[0]) { yc.appendChild(cip(y[1], 'mavi')); adet++; } });
    if (!adet) yc.appendChild(el('span', 'soluk', cevir('Etkin yetenek yok')));
    ykart.appendChild(yc); g2.appendChild(ykart);
    kok.appendChild(g2);

    // ── footer ──
    var ft = el('footer', 'dash-footer');
    var fd = el('div'); var od = el('i', 'online-dot' + (sDown === 0 ? '' : ' warn')); fd.appendChild(od);
    fd.appendChild(document.createTextNode(' ' + (sDown === 0 ? cevir('Tüm sistemler çalışıyor') : (sDown + ' ' + cevir('servis kapalı'))))); ft.appendChild(fd);
    ft.appendChild(el('span', null, (o.hostname || ''))); ft.appendChild(el('span', null, (o.surum || '') + (o.kanal ? ' · ' + o.kanal : '')));
    kok.appendChild(ft);

    kap.appendChild(kok);
    // sinanmadi uyarisi (test edilmemis host)
    if (o && (o.sinanmis_mi === false || (o.yetenekler != null && (o.yetenekler & 1) === 0))) {
      var uy = el('div', 'band band-uyari'); uy.style.marginTop = '12px';
      uy.appendChild(el('span', null, cevir('Sınanmadı — sunucuda `girginospanel-agent kendini-sina` çalıştırın')));
      kok.appendChild(uy);
    }
    } catch (_e) {
      bosalt(kap);
      var _uy = el('div', 'band band-uyari'); _uy.style.margin = '16px';
      _uy.appendChild(el('span', null, 'Panel cizilemedi: ' + (_e && _e.message ? _e.message : _e)));
      kap.appendChild(_uy);
    }
  }

  async function yukleSiteler() {
    var kap = $('siteler-kap');
    yukleniyorYaz(kap);
    try {
      var v = await api('/api/yerel/siteler');
      var liste = (v && v.siteler) || [];
      bosalt(kap);
      if (!liste.length) { bosYaz(kap, 'Kayıtlı site yok'); return; }
      var t = tabloKur(['Ad', 'Durum', 'Bağlama', '']);
      liste.forEach(function (s) {
        var tr = el('tr');
        tr.appendChild(el('td', 'mono', s.ad));
        var durum = el('td');
        durum.appendChild(cip(s.durum || '—', s.durum === 'Started' ? 'yesil' : 'notr'));
        tr.appendChild(durum);
        var bag = el('td', 'mono kes', s.baglama);
        bag.title = s.baglama || ''; // tam değer ipucunda
        tr.appendChild(bag);
        var islem = el('td', 'islem');
        var detayBtn = el('button', 'btn btn-cerceve btn-kucuk', 'Detay');
        detayBtn.type = 'button';
        islem.appendChild(detayBtn);
        islem.appendChild(silDugmesi(s.ad));
        tr.appendChild(islem);
        // accordion detay satırı (ilk açılışta bir kez çekilir; ikinci tık kapatır)
        var detayTr = el('tr', 'site-detay');
        detayTr.hidden = true;
        var dtd = el('td');
        dtd.colSpan = 4;
        detayTr.appendChild(dtd);
        var detayYuklendi = false;
        detayBtn.addEventListener('click', async function () {
          detayTr.hidden = !detayTr.hidden;
          detayBtn.textContent = detayTr.hidden ? 'Detay' : 'Kapat';
          if (!detayTr.hidden && !detayYuklendi) { detayYuklendi = await siteDetayDoldur(s.ad, dtd); }
        });
        t.govde.appendChild(tr);
        t.govde.appendChild(detayTr);
      });
      kap.appendChild(t.tablo);
    } catch (h) { bosYaz(kap, 'Yüklenemedi'); hataGoster(h); }
  }

  // ── PLAN (paket) sekmesi: disk kotası + IIS limitleri preset'i, siteye atama ──
  // 0 = SINIRSIZ. Limitler her zaman appcmd ile uygulanır; disk kotası yalnız
  // FSRM kuruluysa uygulanır (durum "Kota motoru" kartında açıkça gösterilir).
  function planDeger(v, birim) { return (!v || v <= 0) ? cevir('sınırsız') : (v + (birim ? ' ' + birim : '')); }

  function planAtaSonucTost(r) {
    if (!r) { tost(cevir('Plan atandı'), 'basari'); return; }
    var m = cevir('Plan atandı') + ' — ' + (r.limitUygulandi ? cevir('limitler uygulandı') : cevir('limit uygulanamadı'));
    if (r.kotaNot) m += '. ' + r.kotaNot;
    if (r.uyarilar && r.uyarilar.length) m += ' — ' + r.uyarilar.join('; ');
    tost(m, 'basari');
  }

  async function yuklePlan() {
    var kap = $('plan-kap');
    yukleniyorYaz(kap);
    try {
      var d = await api('/api/yerel/planlar');
      var siteV = await api('/api/yerel/siteler');
      var siteler = (siteV && siteV.siteler) || [];
      var planlar = (d && d.planlar) || [];
      var atamalar = (d && d.atamalar) || {};
      bosalt(kap);

      // 1) Kota motoru (FSRM) durumu
      var mk = el('div', 'kart tablolu');
      var mkb = el('div', 'kart-baslik'); mkb.appendChild(el('h2', null, cevir('Kota motoru'))); mk.appendChild(mkb);
      var mki = el('div', 'kart-ic');
      var msat = el('div', 'detay-havuz');
      if (d.kotaMotoru) {
        msat.appendChild(cip('FSRM', 'yesil'));
        msat.appendChild(el('span', 'soluk', cevir('Disk kotası uygulanabilir (FSRM kurulu).')));
      } else {
        msat.appendChild(cip(cevir('Yok'), 'notr'));
        msat.appendChild(el('span', 'soluk', cevir('FSRM kurulu değil — disk kotası uygulanmaz; yalnız IIS limitleri çalışır.')));
        var kurBtn = el('button', 'btn btn-cerceve btn-kucuk', cevir('Kota motorunu kur'));
        kurBtn.type = 'button';
        kurBtn.addEventListener('click', async function () {
          kurBtn.disabled = true; var e0 = kurBtn.textContent; kurBtn.textContent = cevir('Kuruluyor… (birkaç dakika)');
          try {
            var r = await api('/api/yerel/kota-motoru-kur', { govde: {} });
            tost(r && r.restart ? cevir('FSRM kuruldu — sunucu yeniden başlatılmalı') : cevir('FSRM kuruldu'), 'basari');
            yuklePlan();
          } catch (h) { hataGoster(h); kurBtn.textContent = e0; kurBtn.disabled = false; }
        });
        msat.appendChild(kurBtn);
      }
      mki.appendChild(msat); mk.appendChild(mki); kap.appendChild(mk);

      // 2) Planlar tablosu + ekle/düzenle formu
      var pk = el('div', 'kart tablolu');
      var pkb = el('div', 'kart-baslik'); pkb.appendChild(el('h2', null, cevir('Planlar'))); pk.appendChild(pkb);
      var pki = el('div', 'kart-ic');
      if (!planlar.length) {
        pki.appendChild(el('div', 'yukleniyor', cevir('Henüz plan yok')));
      } else {
        var tkap = el('div', 'tablo-kap');
        var t = tabloKur([cevir('Ad'), cevir('Disk'), cevir('Bağlantı'), cevir('Bant'), 'CPU', cevir('Bellek'), '']);
        planlar.forEach(function (p) {
          var tr = el('tr');
          tr.appendChild(el('td', 'mono', p.ad));
          tr.appendChild(el('td', 'mono', planDeger(p.diskKotaMB, 'MB')));
          tr.appendChild(el('td', 'mono', planDeger(p.maxBaglanti)));
          tr.appendChild(el('td', 'mono', planDeger(p.maxBantGenisligiKBs, 'KB/s')));
          tr.appendChild(el('td', 'mono', planDeger(p.cpuLimitYuzde, '%')));
          tr.appendChild(el('td', 'mono', planDeger(p.bellekMB, 'MB')));
          var islem = el('td', 'islem');
          var duzBtn = el('button', 'btn btn-cerceve btn-kucuk', cevir('Düzenle')); duzBtn.type = 'button';
          duzBtn.addEventListener('click', function () { planFormDoldur(p); });
          islem.appendChild(duzBtn);
          islem.appendChild(silBtn(async function () { await api('/api/yerel/plan-sil', { govde: { ad: p.ad } }); tost(cevir('Plan silindi'), 'basari'); yuklePlan(); }));
          tr.appendChild(islem);
          t.govde.appendChild(tr);
        });
        tkap.appendChild(t.tablo); pki.appendChild(tkap);
      }
      var f = el('form', 'plan-form');
      function alan(ad, etiket, ph) {
        var s = el('label', 'plan-alan'); s.appendChild(el('span', 'plan-etiket', cevir(etiket)));
        var i = el('input');
        if (ad === 'ad') { i.type = 'text'; i.maxLength = 40; } else { i.type = 'number'; i.min = '0'; i.step = '1'; }
        i.placeholder = ph || ''; i.setAttribute('data-alan', ad);
        s.appendChild(i); f.appendChild(s); return i;
      }
      var gAd = alan('ad', 'Plan adı', 'Başlangıç');
      var gDisk = alan('diskKotaMB', 'Disk MB (0=∞)');
      var gBag = alan('maxBaglanti', 'Bağlantı (0=∞)');
      var gBant = alan('maxBantGenisligiKBs', 'Bant KB/s (0=∞)');
      var gCpu = alan('cpuLimitYuzde', 'CPU % (0=∞)');
      var gBel = alan('bellekMB', 'Bellek MB (0=∞)');
      function planFormDoldur(p) { gAd.value = p.ad; gDisk.value = p.diskKotaMB || 0; gBag.value = p.maxBaglanti || 0; gBant.value = p.maxBantGenisligiKBs || 0; gCpu.value = p.cpuLimitYuzde || 0; gBel.value = p.bellekMB || 0; gAd.focus(); }
      var kaydet = el('button', 'btn btn-dolgulu btn-kucuk', cevir('Planı kaydet')); kaydet.type = 'submit';
      f.appendChild(kaydet);
      f.addEventListener('submit', async function (e) {
        e.preventDefault(); kaydet.disabled = true;
        var govde = {
          ad: gAd.value.trim(),
          diskKotaMB: parseInt(gDisk.value || '0', 10) || 0,
          maxBaglanti: parseInt(gBag.value || '0', 10) || 0,
          maxBantGenisligiKBs: parseInt(gBant.value || '0', 10) || 0,
          cpuLimitYuzde: parseInt(gCpu.value || '0', 10) || 0,
          bellekMB: parseInt(gBel.value || '0', 10) || 0
        };
        try { await api('/api/yerel/plan-kaydet', { govde: govde }); tost(cevir('Plan kaydedildi'), 'basari'); yuklePlan(); }
        catch (h) { hataGoster(h); kaydet.disabled = false; }
      });
      pki.appendChild(f); pk.appendChild(pki); kap.appendChild(pk);

      // 3) Site atamaları
      var ak = el('div', 'kart tablolu');
      var akb = el('div', 'kart-baslik'); akb.appendChild(el('h2', null, cevir('Site atamaları'))); ak.appendChild(akb);
      var aki = el('div', 'kart-ic');
      if (!siteler.length) {
        aki.appendChild(el('div', 'yukleniyor', cevir('Kayıtlı site yok')));
      } else {
        var atkap = el('div', 'tablo-kap');
        var at = tabloKur([cevir('Site'), cevir('Plan'), '']);
        siteler.forEach(function (s) {
          var tr = el('tr');
          tr.appendChild(el('td', 'mono', s.ad));
          var pl = el('td');
          var sec = el('select');
          var yok = el('option', null, cevir('— plan yok —')); yok.value = ''; sec.appendChild(yok);
          planlar.forEach(function (p) { var op = el('option', null, p.ad); op.value = p.ad; if (atamalar[s.ad] === p.ad) op.selected = true; sec.appendChild(op); });
          sec.addEventListener('change', async function () {
            sec.disabled = true;
            try {
              if (sec.value === '') { await api('/api/yerel/plan-kaldir', { govde: { site: s.ad } }); tost(cevir('Plan ataması kaldırıldı'), 'basari'); }
              else { var r = await api('/api/yerel/plan-ata', { govde: { site: s.ad, plan: sec.value } }); planAtaSonucTost(r); }
              yuklePlan();
            } catch (h) { hataGoster(h); sec.disabled = false; }
          });
          pl.appendChild(sec); tr.appendChild(pl);
          tr.appendChild(el('td', 'islem', ''));
          at.govde.appendChild(tr);
        });
        atkap.appendChild(at.tablo); aki.appendChild(atkap);
      }
      ak.appendChild(aki); kap.appendChild(ak);
    } catch (h) { bosYaz(kap, cevir('Yüklenemedi')); hataGoster(h); }
  }

  // ── GENEL AYARLAR sekmesi: ajan bilgisi + dil + panel parolası değiştir ──
  var DIL_AD = { tr: 'Türkçe', en: 'English' };

  async function yukleAyarlar() {
    var kap = $('ayarlar-kap');
    yukleniyorYaz(kap);
    try {
      var o = {};
      try { o = (await api('/api/yerel/ozet')) || {}; } catch (e) { o = {}; }
      bosalt(kap);

      function bilgiSatir(et, deg) {
        var s = el('div', 'detay-blok');
        s.appendChild(el('div', 'detay-etiket', cevir(et)));
        s.appendChild(el('div', 'mono', (deg == null || deg === '') ? '—' : String(deg)));
        return s;
      }

      // 1) Ajan bilgisi
      var bk = el('div', 'kart tablolu');
      var bkb = el('div', 'kart-baslik'); bkb.appendChild(el('h2', null, cevir('Ajan bilgisi'))); bk.appendChild(bkb);
      var bki = el('div', 'kart-ic');
      var izg = el('div', 'stat-izgara');
      izg.appendChild(bilgiSatir('Sunucu adı', o.hostname));
      izg.appendChild(bilgiSatir('Sürüm', o.surum));
      izg.appendChild(bilgiSatir('Kanal', o.kanal));
      izg.appendChild(bilgiSatir('Sınanmış', o.sinanmis_mi ? cevir('Evet') : cevir('Hayır')));
      izg.appendChild(bilgiSatir('Panel portu', '8443'));
      izg.appendChild(bilgiSatir('Merkezi API portu', '8460'));
      bki.appendChild(izg); bk.appendChild(bki); kap.appendChild(bk);

      // 2) Dil (mevcut i18n; üst-bardaki seçiciyle aynı davranış)
      var dk = el('div', 'kart tablolu');
      var dkb = el('div', 'kart-baslik'); dkb.appendChild(el('h2', null, cevir('Dil'))); dk.appendChild(dkb);
      var dki = el('div', 'kart-ic');
      var dsat = el('div', 'detay-havuz');
      dsat.appendChild(el('span', 'soluk', cevir('Panel dili')));
      var dsec = el('select', 'dil-sec');
      Object.keys(DILLER).forEach(function (k) { var op = el('option', null, DIL_AD[k] || k.toUpperCase()); op.value = k; if (k === aktifDil) op.selected = true; dsec.appendChild(op); });
      dsec.addEventListener('change', function () { dilDegistir(dsec.value); });
      dsat.appendChild(dsec);
      dki.appendChild(dsat); dk.appendChild(dki); kap.appendChild(dk);

      // 3) Panel parolasını değiştir
      var pk = el('div', 'kart tablolu');
      var pkb = el('div', 'kart-baslik'); pkb.appendChild(el('h2', null, cevir('Panel parolasını değiştir'))); pk.appendChild(pkb);
      var pki = el('div', 'kart-ic');
      var f = el('form', 'plan-form');
      function pAlan(et) {
        var s = el('label', 'plan-alan'); s.appendChild(el('span', 'plan-etiket', cevir(et)));
        var i = el('input'); i.type = 'password'; i.autocomplete = 'new-password'; i.setAttribute('autocapitalize', 'off'); i.spellcheck = false;
        s.appendChild(i); f.appendChild(s); return i;
      }
      var eski = pAlan('Mevcut parola');
      var yeni = pAlan('Yeni parola (en az 8)');
      var yeni2 = pAlan('Yeni parola (tekrar)');
      var kaydet = el('button', 'btn btn-dolgulu btn-kucuk', cevir('Parolayı değiştir')); kaydet.type = 'submit';
      f.appendChild(kaydet);
      f.addEventListener('submit', async function (e) {
        e.preventDefault();
        if (yeni.value.length < 8) { tost(cevir('Yeni parola en az 8 karakter olmalı'), 'hata'); return; }
        if (yeni.value !== yeni2.value) { tost(cevir('Yeni parolalar eşleşmiyor'), 'hata'); return; }
        kaydet.disabled = true;
        try {
          await api('/api/yerel/parola-degistir', { govde: { eskiParola: eski.value, yeniParola: yeni.value } });
          tost(cevir('Parola değiştirildi'), 'basari');
          eski.value = ''; yeni.value = ''; yeni2.value = '';
        } catch (h) { hataGoster(h); }
        kaydet.disabled = false;
      });
      pki.appendChild(f); pk.appendChild(pki); kap.appendChild(pk);
    } catch (h) { bosYaz(kap, cevir('Yüklenemedi')); hataGoster(h); }
  }

  // confirm() yerine iki aşamalı silme: ilk tık "Emin misin?"e döner, 3 sn içinde ikinci tık onaylar.
  // onayla: silmeyi yapan async geri çağrı; hata olursa düğme sıfırlanır.
  function silBtn(onayla) {
    var b = el('button', 'btn btn-tehlike btn-kucuk', 'Sil');
    b.type = 'button';
    var hazir = false, sayac = 0;
    function sifirla() { hazir = false; b.textContent = 'Sil'; b.classList.remove('emin'); }
    b.addEventListener('click', async function () {
      if (!hazir) {
        hazir = true;
        b.textContent = 'Emin misin?';
        b.classList.add('emin');
        sayac = setTimeout(sifirla, 3000);
        return;
      }
      clearTimeout(sayac);
      b.disabled = true;
      try { await onayla(); }
      catch (h) { hataGoster(h); b.disabled = false; sifirla(); } // 422: Türkçe mesaj aynen tost
    });
    return b;
  }

  function silDugmesi(ad) {
    return silBtn(async function () {
      await api('/api/yerel/siteler?ad=' + encodeURIComponent(ad), { method: 'DELETE' });
      tost('Site silindi: ' + ad, 'basari');
      yukleSiteler();
    });
  }

  // Site detay panelini çizer; başarıda true döner (accordion bir daha çekmez)
  async function siteDetayDoldur(ad, hucre) {
    yukleniyorYaz(hucre);
    try {
      var d = await api('/api/yerel/site-detay?ad=' + encodeURIComponent(ad));
      bosalt(hucre);
      var sar = el('div', 'detay-sar');
      var altSekmeler = el('div', 'alt-sekmeler');
      var altIcerik = el('div', 'alt-sekme-icerik');
      var pnl = {};
      var tanimlar = [['genel', 'Genel'], ['baglama', 'Bağlamalar'], ['ssl', 'SSL'], ['havuz', 'Havuz']];
      tanimlar.forEach(function (t, i) {
        var btn = el('button', 'alt-sekme' + (i === 0 ? ' aktif' : ''), cevir(t[1]));
        btn.type = 'button';
        var p = el('div', 'alt-panel'); p.hidden = (i !== 0);
        pnl[t[0]] = p;
        btn.addEventListener('click', function () {
          var hepsi = altSekmeler.querySelectorAll('.alt-sekme');
          for (var k = 0; k < hepsi.length; k++) hepsi[k].classList.remove('aktif');
          btn.classList.add('aktif');
          tanimlar.forEach(function (tt) { pnl[tt[0]].hidden = (tt[0] !== t[0]); });
        });
        altSekmeler.appendChild(btn);
        altIcerik.appendChild(p);
      });
      sar.appendChild(altSekmeler);
      sar.appendChild(altIcerik);
      function kv(et, degerDugum) {
        var b = el('div', 'detay-blok');
        b.appendChild(el('div', 'detay-etiket', cevir(et)));
        b.appendChild(degerDugum);
        return b;
      }
      // ---- GENEL ----
      var g = pnl['genel'];
      var scal = d.Durum === 'Started' || d.Durum === 'Running';
      g.appendChild(kv('Durum', cip(cevir(d.Durum) || '—', scal ? 'yesil' : 'notr')));
      var yolD = el('div', 'mono kes', d.FizikselYol || '—'); yolD.title = d.FizikselYol || '';
      g.appendChild(kv('Fiziksel yol', yolD));
      var teh = el('div', 'detay-blok');
      teh.appendChild(el('div', 'detay-etiket', cevir('Tehlikeli bölge')));
      teh.appendChild(el('div', 'soluk', cevir('Site IIS yapılandırmasından kaldırılır; dosyalar (webroot) SİLİNMEZ.')));
      teh.appendChild(silDugmesi(ad));
      g.appendChild(teh);
      // ---- HAVUZ ----
      var hv = d.Havuz || {};
      var hp = pnl['havuz'];
      var hsat = el('div', 'detay-havuz');
      hsat.appendChild(el('span', 'mono', hv.Ad || '—'));
      var hcal = hv.Durum === 'Started' || hv.Durum === 'Running';
      hsat.appendChild(cip(cevir(hv.Durum) || '—', hcal ? 'yesil' : 'notr'));
      if (hv.Ad) {
        hsat.appendChild(havuzBtn(hv.Ad, 'baslat', 'Başlat', ad, hucre));
        hsat.appendChild(havuzBtn(hv.Ad, 'durdur', 'Durdur', ad, hucre));
        hsat.appendChild(havuzBtn(hv.Ad, 'geridonustur', 'Geri dönüştür', ad, hucre));
        var sel = el('select');
        [['v4.0', '.NET 4.x'], ['v2.0', '.NET 2.x'], ['', 'Yönetilen kod yok']].forEach(function (o) {
          var op = el('option', null, o[1]); op.value = o[0];
          if ((hv.DotNetSurum || '') === o[0]) op.selected = true;
          sel.appendChild(op);
        });
        sel.addEventListener('change', async function () {
          try { await api('/api/yerel/havuz-dotnet', { govde: { ad: hv.Ad, surum: sel.value } }); tost(cevir('Havuz .NET sürümü güncellendi'), 'basari'); }
          catch (h) { hataGoster(h); }
        });
        hsat.appendChild(sel);
      }
      hp.appendChild(kv('Uygulama havuzu', hsat));
      // ---- BAĞLAMALAR ----
      var bp = pnl['baglama'];
      var bkap = el('div', 'tablo-kap');
      var tb = tabloKur(['Protokol', 'Port', 'Host', 'SSL', '']);
      (d.Baglamalar || []).forEach(function (bg) {
        var r = el('tr');
        r.appendChild(el('td', 'mono', bg.Protokol));
        r.appendChild(el('td', 'mono', bg.Port));
        var hostTd = el('td', 'mono kes', bg.Host || '*'); hostTd.title = bg.Host || '*';
        r.appendChild(hostTd);
        var sslTd = el('td'); sslTd.appendChild(bg.SSL ? cip('SSL', 'yesil') : el('span', 'soluk', '—')); r.appendChild(sslTd);
        var silTd = el('td', 'islem');
        silTd.appendChild(silBtn(async function () {
          await api('/api/yerel/baglama?site=' + encodeURIComponent(ad) + '&protokol=' + encodeURIComponent(bg.Protokol) + '&port=' + encodeURIComponent(bg.Port) + '&host=' + encodeURIComponent(bg.Host || ''), { method: 'DELETE' });
          tost(cevir('Bağlama silindi'), 'basari'); siteDetayDoldur(ad, hucre);
        }));
        r.appendChild(silTd);
        tb.govde.appendChild(r);
      });
      bkap.appendChild(tb.tablo);
      bp.appendChild(bkap);
      var f = el('form', 'mini-form');
      var psel = el('select'); ['http', 'https'].forEach(function (pp) { var op = el('option', null, pp); op.value = pp; psel.appendChild(op); });
      var port = el('input', 'port'); port.type = 'text'; port.placeholder = 'port'; port.setAttribute('aria-label', 'Port');
      var host = el('input'); host.type = 'text'; host.placeholder = 'host (ör. ornek.com)'; host.setAttribute('aria-label', 'Host');
      var ekleBtn = el('button', 'btn btn-dolgulu btn-kucuk', cevir('Bağlama ekle')); ekleBtn.type = 'submit';
      f.appendChild(psel); f.appendChild(port); f.appendChild(host); f.appendChild(ekleBtn);
      f.addEventListener('submit', async function (e) {
        e.preventDefault(); ekleBtn.disabled = true;
        try { await api('/api/yerel/baglama-ekle', { govde: { site: ad, protokol: psel.value, port: port.value.trim(), host: host.value.trim() } }); tost(cevir('Bağlama eklendi'), 'basari'); siteDetayDoldur(ad, hucre); }
        catch (h) { hataGoster(h); ekleBtn.disabled = false; }
      });
      bp.appendChild(f);
      // ---- SSL ----
      var sp = pnl['ssl'];
      var sslVar = (d.Baglamalar || []).some(function (bg) { return bg.SSL; });
      sp.appendChild(kv('Mevcut durum', sslVar ? cip(cevir('En az bir HTTPS bağlaması var'), 'yesil') : el('span', 'soluk', cevir('HTTPS bağlaması yok'))));
      var b4 = el('div', 'detay-blok');
      var sslBtn = el('button', 'btn btn-cerceve', "Let's Encrypt SSL al"); sslBtn.type = 'button';
      sslBtn.addEventListener('click', async function () {
        sslBtn.disabled = true; var eski = sslBtn.textContent; sslBtn.textContent = cevir('Alınıyor…');
        try { var r = await api('/api/yerel/site-ssl', { govde: { site: ad } }); var msg = (r && r.mesaj) || cevir('Tamamlandı'); tost(msg, 'basari'); sslMesajYaz(b4, msg); }
        catch (h) { hataGoster(h); sslMesajYaz(b4, h.message); }
        sslBtn.textContent = eski; sslBtn.disabled = false;
      });
      b4.appendChild(sslBtn);
      sp.appendChild(b4);
      hucre.appendChild(sar);
      return true;
    } catch (h) { bosYaz(hucre, cevir('Detay yüklenemedi')); hataGoster(h); return false; }
  }
  function sslMesajYaz(blok, metin) {
    var v = blok.querySelector('.ssl-mesaj'); // yeni değilse tek satırı güncelle (yığılma olmaz)
    if (!v) { v = el('div', 'ssl-mesaj'); blok.appendChild(v); }
    v.textContent = metin;
  }

  function havuzBtn(havuz, islem, etiket, siteAd, hucre) {
    var b = el('button', 'btn btn-cerceve btn-kucuk', etiket);
    b.type = 'button';
    b.addEventListener('click', async function () {
      b.disabled = true;
      try {
        await api('/api/yerel/havuz-islem', { govde: { ad: havuz, islem: islem } });
        tost('Havuz: ' + etiket + ' uygulandı', 'basari');
        siteDetayDoldur(siteAd, hucre); // durum çipini tazele
      } catch (h) { hataGoster(h); b.disabled = false; }
    });
    return b;
  }

  // ---------- Olay Günlüğü ----------
  var olayVeri = [];
  var SEVIYE = { hata: ['kirmizi', 'Hata'], uyari: ['amber', 'Uyarı'], bilgi: ['notr', 'Bilgi'] };

  async function yukleOlaylar() {
    var kap = $('olaylar-kap');
    yukleniyorYaz(kap);
    try {
      var gunluk = $('olay-gunluk').value;
      var v = await api('/api/yerel/olaylar?gunluk=' + encodeURIComponent(gunluk) + '&adet=50');
      olayVeri = (v && v.olaylar) || [];
      olaylarBas();
    } catch (h) { bosYaz(kap, 'Yüklenemedi'); hataGoster(h); }
  }

  // Seviye filtresi istemci tarafında uygulanır; veri yeniden çekilmez
  function olaylarBas() {
    var kap = $('olaylar-kap');
    bosalt(kap);
    var filtre = $('olay-seviye').value;
    var liste = olayVeri.filter(function (o) { return !filtre || o.Seviye === filtre; });
    if (!liste.length) { bosYaz(kap, 'Gösterilecek olay yok'); return; }
    var t = tabloKur(['Zaman', 'Seviye', 'Kaynak', 'Olay ID', 'Mesaj']);
    liste.forEach(function (o) {
      var tr = el('tr', 'tiklanir');
      tr.tabIndex = 0; // klavye ile de açılabilsin
      tr.setAttribute('aria-expanded', 'false');
      tr.appendChild(el('td', 'mono', zamanBicimle(o.Zaman)));
      var sv = SEVIYE[o.Seviye] || ['notr', o.Seviye || '?'];
      var svTd = el('td');
      svTd.appendChild(cip(sv[1], sv[0]));
      tr.appendChild(svTd);
      tr.appendChild(el('td', null, o.Kaynak));
      tr.appendChild(el('td', 'mono', (o.OlayID === undefined || o.OlayID === null) ? '—' : o.OlayID));
      tr.appendChild(el('td', 'kes', o.Mesaj));
      var detay = el('tr', 'olay-detay');
      detay.hidden = true;
      var dtd = el('td', null, o.Mesaj || '—'); // tam mesaj, yine textContent
      dtd.colSpan = 5;
      detay.appendChild(dtd);
      function acKapa() {
        detay.hidden = !detay.hidden;
        tr.setAttribute('aria-expanded', detay.hidden ? 'false' : 'true');
      }
      tr.addEventListener('click', acKapa);
      tr.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); acKapa(); }
      });
      t.govde.appendChild(tr);
      t.govde.appendChild(detay);
    });
    kap.appendChild(t.tablo);
  }

  // ---------- Görevler ----------
  async function yukleGorevler() {
    var kap = $('gorevler-kap');
    yukleniyorYaz(kap);
    try {
      var v = await api('/api/yerel/gorevler?tumu=' + ($('gorev-tumu').checked ? '1' : '0'));
      var liste = (v && v.gorevler) || [];
      bosalt(kap);
      if (!liste.length) { bosYaz(kap, 'Gösterilecek görev yok'); return; }
      var t = tabloKur(['Ad', 'Durum', 'Son Koşum', 'Sonraki', 'Son Sonuç']);
      liste.forEach(function (g) {
        var tr = el('tr');
        var adTd = el('td', 'mono kes', g.Ad);
        adTd.title = g.Ad || ''; // tam ad ipucunda
        tr.appendChild(adTd);
        var durum = el('td');
        var tur = g.Durum === 'Running' ? 'yesil' : (g.Durum === 'Disabled' ? 'amber' : 'notr');
        durum.appendChild(cip(g.Durum || '—', tur));
        tr.appendChild(durum);
        tr.appendChild(el('td', 'mono', zamanBicimle(g.SonKosum)));
        tr.appendChild(el('td', 'mono', zamanBicimle(g.Sonraki)));
        tr.appendChild(sonucTd(g.SonSonuc));
        t.govde.appendChild(tr);
      });
      kap.appendChild(t.tablo);
    } catch (h) { bosYaz(kap, 'Yüklenemedi'); hataGoster(h); }
  }

  function sonucTd(v) {
    var td = el('td', 'mono');
    if (v === 0) td.appendChild(el('span', 'metin-yesil', '0'));
    else if (v === undefined || v === null) td.textContent = '—';
    else td.appendChild(el('span', 'metin-kirmizi', '0x' + (Number(v) >>> 0).toString(16).toUpperCase()));
    return td;
  }

  // ---------- Veritabanları ----------
  async function yukleVt() {
    var sel = $('vt-motor');
    try {
      var v = await api('/api/yerel/vt-motorlar');
      var motorlar = ((v && v.motorlar) || []).filter(function (m) { return m.Kurulu; });
      bosalt(sel);
      if (!motorlar.length) { // hiç kurulu motor yoksa yönlendirici bilgi bloğu
        $('vt-motor-yok').hidden = false;
        $('vt-govde').hidden = true;
        return;
      }
      $('vt-motor-yok').hidden = true;
      $('vt-govde').hidden = false;
      motorlar.forEach(function (m) { var op = el('option', null, m.Ad); op.value = m.Tur; sel.appendChild(op); });
      vtListeYukle();
    } catch (h) { hataGoster(h); }
  }

  function vtFormPasif(pasif) {
    ['vt-ad', 'vt-kullanici', 'vt-parola', 'btn-vt-olustur'].forEach(function (id) { $(id).disabled = pasif; });
  }

  async function vtListeYukle() {
    var motor = $('vt-motor').value;
    var kap = $('vt-liste-kap');
    yukleniyorYaz(kap);
    $('vt-uyari').hidden = true;
    vtFormPasif(false);
    try {
      var v = await api('/api/yerel/vt-liste?motor=' + encodeURIComponent(motor));
      var liste = (v && v.veritabanlari) || [];
      bosalt(kap);
      if (!liste.length) { bosYaz(kap, 'Bu motorda veritabanı yok'); return; }
      var t = tabloKur(['Ad', 'Boyut (MB)', '']);
      liste.forEach(function (db) {
        var tr = el('tr');
        tr.appendChild(el('td', 'mono', db.Ad));
        tr.appendChild(el('td', 'mono', (db.BoyutMB === undefined || db.BoyutMB === null) ? '—' : db.BoyutMB));
        var islem = el('td', 'islem');
        islem.appendChild(silBtn(async function () {
          await api('/api/yerel/vt-sil?motor=' + encodeURIComponent(motor) + '&ad=' + encodeURIComponent(db.Ad), { method: 'DELETE' });
          tost('Veritabanı silindi: ' + db.Ad, 'basari');
          vtListeYukle();
        }));
        tr.appendChild(islem);
        t.govde.appendChild(tr);
      });
      kap.appendChild(t.tablo);
    } catch (h) {
      if (h && h.yetkisiz) return;
      if (h && h.kod === 422) { // motor parola/erişim bekliyor: gelen Türkçe mesaj + form pasif
        $('vt-uyari-metin').textContent = h.message;
        $('vt-uyari').hidden = false;
        vtFormPasif(true);
        bosalt(kap);
        return;
      }
      bosYaz(kap, 'Yüklenemedi');
      hataGoster(h);
    }
  }

  // ---------- Servisler + Kaynak ----------
  async function yukleServisler() {
    kaynakYukle(); // stat kartları da bu sekmede
    var kap = $('servisler-kap');
    yukleniyorYaz(kap);
    try {
      var v = await api('/api/yerel/servisler');
      var liste = (v && v.servisler) || [];
      bosalt(kap);
      if (!liste.length) { bosYaz(kap, 'Servis yok'); return; }
      var t = tabloKur(['Servis', 'Durum', 'Başlangıç', '']);
      liste.forEach(function (s) {
        var tr = el('tr');
        var adTd = el('td', 'kes', s.GoruntuAd || s.Ad);
        adTd.title = s.Ad || '';
        tr.appendChild(adTd);
        var calisiyor = s.Durum === 'Running' || s.Durum === 'Çalışıyor';
        var dt = el('td');
        dt.appendChild(cip(cevir(s.Durum) || '—', calisiyor ? 'yesil' : 'notr'));
        tr.appendChild(dt);
        tr.appendChild(el('td', null, cevir(s.BaslangicTuru) || '—'));
        var islem = el('td', 'islem');
        if (calisiyor) {
          islem.appendChild(servisBtn(s.Ad, 'durdur', 'Durdur', 'btn-cerceve'));
          islem.appendChild(servisBtn(s.Ad, 'yeniden', 'Yeniden', 'btn-cerceve'));
        } else {
          islem.appendChild(servisBtn(s.Ad, 'baslat', 'Başlat', 'btn-dolgulu'));
        }
        tr.appendChild(islem);
        t.govde.appendChild(tr);
      });
      kap.appendChild(t.tablo);
    } catch (h) { bosYaz(kap, 'Yüklenemedi'); hataGoster(h); }
  }

  function servisBtn(ad, islem, etiket, stil) {
    var b = el('button', 'btn ' + stil + ' btn-kucuk', etiket);
    b.type = 'button';
    b.addEventListener('click', async function () {
      b.disabled = true;
      try {
        await api('/api/yerel/servis-islem', { govde: { ad: ad, islem: islem } });
        tost(etiket + ' uygulandı: ' + ad, 'basari');
        yukleServisler();
      } catch (h) { hataGoster(h); b.disabled = false; }
    });
    return b;
  }

  function mb(v) { if (v === undefined || v === null) return '—'; return v >= 1024 ? (v / 1024).toFixed(1) + ' GB' : v + ' MB'; }

  function statKart(baslik, buyuk, alt) {
    var d = el('div', 'stat');
    d.appendChild(el('div', 'stat-baslik', baslik));
    d.appendChild(el('div', 'stat-buyuk', buyuk));
    d.appendChild(el('div', 'stat-alt', alt));
    return d;
  }

  // Kaynak kutuları: hata olursa SESSİZ (5 sn'de bir çekildiği için tost/band spam'i olmaz)
  async function kaynakYukle() {
    try {
      var k = await api('/api/yerel/kaynak');
      var kap = $('kaynak-dizi');
      bosalt(kap);
      kap.appendChild(statKart('CPU', (k.cpu_yuzde == null ? '—' : k.cpu_yuzde + '%'), 'Anlık kullanım'));
      kap.appendChild(statKart('RAM', (k.ram_yuzde == null ? '—' : k.ram_yuzde + '%'), mb(k.ram_kul_mb) + ' / ' + mb(k.ram_top_mb)));
      var disk = (k.disk && k.disk[0]) || null;
      var diskAlt = disk ? (disk.BosGB + ' GB boş / ' + disk.TopGB + ' GB') : '—';
      if (k.disk && k.disk.length > 1) diskAlt += '  ·  +' + (k.disk.length - 1) + ' sürücü';
      kap.appendChild(statKart('Disk' + (disk ? ' (' + disk.Surucu + ')' : ''), disk ? disk.Yuzde + '%' : '—', diskAlt));
    } catch (h) { /* sessiz: yetkisiz zaten api() içinde giriş görünümüne düşürür */ }
  }

  // ---------- Kurulum Sihirbazı ----------
  // Arka uç tek uçuşlu: seçilen kalemler SIRAYLA kurulur, iş 2 sn'de bir yoklanır.
  // Sekme değişiminde bölüm yalnız gizlenir; yoklama zinciri ve DOM durumu bellekte
  // kaldığı için ADIM 3 kaldığı işten kesintisiz devam eder.
  var sihirbaz = { adim: 1, katalog: [], secili: [], secim: {}, terim: '', sira: 0, sonuc: {}, isId: null, aktif: false, otoKaydir: true, hataUst: 0, segment: null, akis: null, akisBekci: 0, kalemBasZaman: 0 };

  // ---------- Kurulum ilerleme biçimleyicileri (ETA çubuğu) ----------
  function mbYaz(b) {
    b = b || 0;
    if (b >= (1 << 30)) return (b / (1 << 30)).toFixed(2) + ' GB';
    return (b / (1 << 20)).toFixed(1) + ' MB';
  }
  function hizYaz(bps) {
    if (!bps || bps <= 0) return '—';
    if (bps >= (1 << 20)) return (bps / (1 << 20)).toFixed(1) + ' MB/s';
    return Math.round(bps / 1024) + ' KB/s';
  }
  function sureBicim(sn) {
    sn = Math.max(0, Math.round(sn));
    var d = Math.floor(sn / 60), s = sn % 60;
    return d + ':' + (s < 10 ? '0' : '') + s;
  }
  function etaYaz(sn) {
    if (sn == null || sn < 0) return cevir('kalan —');
    if (sn < 60) return cevir('kalan ~') + Math.round(sn) + ' sn';
    return cevir('kalan ~') + sureBicim(sn);
  }
  function gecenYaz() {
    if (!sihirbaz.kalemBasZaman) return '0:00';
    return sureBicim((Date.now() - sihirbaz.kalemBasZaman) / 1000);
  }

  async function yukleKurulum() {
    var kap = $('kur-liste');
    if (sihirbaz.aktif) return; // kurulum sürerken katalog yeniden çekilmez
    yukleniyorYaz(kap);
    adimGoster(1);
    sihirbaz.terim = '';
    $('kur-ara').value = '';
    $('btn-kur-devam').disabled = true;
    try {
      var v = await api('/api/yerel/katalog');
      sihirbaz.katalog = (v && v.kalemler) || [];
      katalogBas();
    } catch (h) { bosYaz(kap, 'Yüklenemedi'); hataGoster(h); }
  }

  // SureTahmini aralığının üst değeri ("2-5 dk" → 5, "3 dk" → 3)
  function sureUst(s) {
    var m = (s || '').match(/\d+/g);
    if (!m) return 0;
    var mx = 0;
    for (var i = 0; i < m.length; i++) { var n = parseInt(m[i], 10); if (n > mx) mx = n; }
    return mx;
  }

  function seciliAnahtarlar() {
    return Object.keys(sihirbaz.secim).filter(function (k) { return sihirbaz.secim[k]; });
  }

  // Devam düğmesi durumu + "Seçili: N bileşen · ~X dk" canlı özeti
  function secimGuncelle() {
    var anahtarlar = seciliAnahtarlar();
    $('btn-kur-devam').disabled = anahtarlar.length === 0;
    var dk = 0;
    sihirbaz.katalog.forEach(function (k) { if (anahtarlar.indexOf(k.Anahtar) >= 0) dk += sureUst(k.SureTahmini); });
    $('kur-sec-ozet').textContent = 'Seçili: ' + anahtarlar.length + ' bileşen · ~' + dk + ' dk';
  }

  function katalogBas() {
    var kap = $('kur-liste');
    bosalt(kap);
    if (!sihirbaz.katalog.length) { bosYaz(kap, 'Katalogda kalem yok'); secimGuncelle(); return; }
    var terim = sihirbaz.terim;
    // ada VE açıklamaya göre Türkçe-duyarsız canlı filtre (ağa çıkmadan mevcut diziyi süz)
    var liste = sihirbaz.katalog.filter(function (k) {
      return !terim || trk(k.Ad).indexOf(terim) >= 0 || trk(k.Aciklama).indexOf(terim) >= 0;
    });
    if (!liste.length) { bosYaz(kap, cevir('Eşleşen bileşen yok')); secimGuncelle(); return; }
    liste.forEach(function (k) {
      var secilebilir = k.Kurulabilir && !k.Kurulu && k.Kademe !== 'karar-bekliyor';
      var satir = el(secilebilir ? 'label' : 'div', 'kur-kalem' + (secilebilir ? '' : ' pasif'));
      if (secilebilir) { // onay kutusu yalnız kurulabilir kalemlerde
        var kutu = el('input', 'kur-sec');
        kutu.type = 'checkbox';
        kutu.value = k.Anahtar;
        kutu.checked = !!sihirbaz.secim[k.Anahtar]; // arama/dil yeniden çiziminde seçim korunur
        satir.appendChild(kutu);
      }
      var govde = el('div', 'kur-govde');
      var bas = el('div', 'kur-baslik-satir');
      bas.appendChild(el('span', null, k.Ad));
      if (k.Kurulu) bas.appendChild(cip('KURULU', 'yesil'));
      if (k.Kademe === 'deneysel') bas.appendChild(cip('DENEYSEL', 'amber'));
      if (k.Kademe === 'karar-bekliyor') bas.appendChild(cip('YAKINDA', 'notr'));
      govde.appendChild(bas);
      govde.appendChild(el('p', null, k.Aciklama)); // açıklama (.kur-govde p)
      satir.appendChild(govde);
      satir.appendChild(el('span', 'kur-sure', k.SureTahmini || '')); // süre: ayrı sağ sütun (saat ikonu CSS)
      kap.appendChild(satir);
    });
    secimGuncelle();
  }

  function adimGoster(n) {
    sihirbaz.adim = n;
    for (var i = 1; i <= 4; i++) $('kur-adim-' + i).hidden = (i !== n);
    var liler = document.querySelectorAll('#kur-adimlar .adim');
    for (var j = 0; j < liler.length; j++) {
      liler[j].classList.toggle('aktif', j === n - 1);
      liler[j].classList.toggle('tamam', j < n - 1);
    }
  }

  function logSatir(metin, sinif) {
    var kutu = $('kur-log');
    kutu.appendChild(el('div', sinif || null, metin));
    if (sihirbaz.otoKaydir) kutu.scrollTop = kutu.scrollHeight;
  }

  // ---------- Genel (segmentli) ilerleme çubuğu ----------
  // Her seçili kalem için bir segment; renk kalemin durumunu taşır (bekliyor/
  // aktif/tamam/kısmi/hata). Kurulum başlarken kurulur, kalem bitince güncellenir.
  function genelCubukKur() {
    var kap = $('kur-genel-cubuk');
    bosalt(kap);
    sihirbaz.secili.forEach(function (k) {
      var s = el('div', 'seg');
      s.title = k.Ad;
      kap.appendChild(s);
    });
    $('kur-genel').hidden = sihirbaz.secili.length < 2; // tek kalemde genel çubuk gereksiz
  }
  function genelSeg(i, sinif) {
    var kap = $('kur-genel-cubuk');
    var s = kap.children[i];
    if (s) s.className = 'seg' + (sinif ? ' ' + sinif : '');
  }

  // ---------- Kalem ilerleme (canlı ETA) çubuğu ----------
  function ilerlemeSifirla() {
    var dolgu = $('ilrl-dolgu');
    dolgu.classList.remove('belirsiz');
    dolgu.style.width = '0';
    $('ilrl-etiket').textContent = '—';
    $('ilrl-yuzde').textContent = '';
    $('ilrl-alt').textContent = '';
    $('kur-ilerleme').hidden = true;
  }
  // ilerlemeGuncelle — arka uçtan gelen yapısal ilerlemeyi çubuğa yansıtır.
  // "indiriliyor" + boyut biliniyor → gerçek yüzde + hız + ETA. Boyut yoksa ya da
  // "kuruluyor" → belirsiz (sweep) çubuk: SÜRE UYDURMAYIZ (dürüstlük), yerine
  // geçen süre + katalog süre tahminini gösteririz.
  function ilerlemeGuncelle(ilr) {
    var kap = $('kur-ilerleme');
    if (!ilr || !ilr.asama) { kap.hidden = true; return; }
    kap.hidden = false;
    var dolgu = $('ilrl-dolgu'), asamaCip = $('ilrl-asama');
    var simdikiKalem = sihirbaz.secili[sihirbaz.sira] || {};
    if (ilr.asama === 'indiriliyor' && ilr.byte_toplam > 0 && ilr.yuzde >= 0) {
      dolgu.classList.remove('belirsiz');
      dolgu.style.width = ilr.yuzde + '%';
      asamaCip.textContent = cevir('indiriliyor'); asamaCip.className = 'cip cip-mavi';
      $('ilrl-etiket').textContent = ilr.etiket || simdikiKalem.Ad || '';
      $('ilrl-yuzde').textContent = ilr.yuzde + '%';
      $('ilrl-alt').textContent = mbYaz(ilr.byte_inen) + ' / ' + mbYaz(ilr.byte_toplam)
        + '   ·   ' + hizYaz(ilr.hiz_bps) + '   ·   ' + etaYaz(ilr.kalan_sn);
    } else if (ilr.asama === 'indiriliyor') { // boyut bilinmiyor
      dolgu.classList.add('belirsiz');
      asamaCip.textContent = cevir('indiriliyor'); asamaCip.className = 'cip cip-mavi';
      $('ilrl-etiket').textContent = ilr.etiket || simdikiKalem.Ad || '';
      $('ilrl-yuzde').textContent = '';
      $('ilrl-alt').textContent = mbYaz(ilr.byte_inen) + ' ' + cevir('indi') + '   ·   ' + hizYaz(ilr.hiz_bps);
    } else { // kuruluyor — süre gerçekten kestirilemez → belirsiz + geçen/tahmini
      dolgu.classList.add('belirsiz');
      asamaCip.textContent = cevir('kuruluyor'); asamaCip.className = 'cip cip-amber';
      $('ilrl-etiket').textContent = simdikiKalem.Ad || cevir('kuruluyor');
      $('ilrl-yuzde').textContent = '';
      $('ilrl-alt').textContent = cevir('geçen') + ' ' + gecenYaz()
        + '   ·   ' + cevir('tahmini') + ' ' + (simdikiKalem.SureTahmini || '—');
    }
    if (sihirbaz.otoKaydir) $('kur-log').scrollTop = $('kur-log').scrollHeight;
  }

  // isUygula — bir anlık görüntüyü (SSE veya yoklama) ekrana işler: log bölmesini
  // tazeler, ETA çubuğunu günceller, iş bittiyse akışı kapatıp kalemBitti çağırır.
  function isUygula(k, v) {
    bosalt(sihirbaz.segment); // iş logu her yanıtta bütün gelir, bölme tazelenir
    ((v && v.log) || []).forEach(function (s) { sihirbaz.segment.appendChild(el('div', null, s)); });
    ilerlemeGuncelle(v && v.ilerleme);
    if (sihirbaz.otoKaydir) $('kur-log').scrollTop = $('kur-log').scrollHeight;
    if (v && v.bitti) { akisKapat(); kalemBitti(k, v.durum, null); return true; }
    return false;
  }

  function akisKapat() {
    if (sihirbaz.akisBekci) { clearTimeout(sihirbaz.akisBekci); sihirbaz.akisBekci = 0; }
    if (sihirbaz.akis) { try { sihirbaz.akis.close(); } catch (e) { } sihirbaz.akis = null; }
  }

  // isIzle — GERÇEK ZAMANLI: SSE ile iş akışına bağlanır. 3.5 sn'de mesaj gelmezse
  // ya da bağlantı KAPANDIysa (kurtarılamaz) YOKLAMAYA düşer (isYokla). SSE bir
  // kez çalıştıysa geçici kopmada EventSource kendiliğinden yeniden bağlanır
  // (sunucu bağlanınca güncel anlık görüntüyü hemen yollar).
  function isIzle(k) {
    akisKapat();
    if (typeof EventSource === 'undefined') { isYokla(k); return; } // tarayıcı desteklemiyor
    var es;
    try { es = new EventSource('/api/yerel/katalog/is-akis?id=' + encodeURIComponent(sihirbaz.isId)); }
    catch (e) { isYokla(k); return; }
    sihirbaz.akis = es;
    var dustu = false;
    function dus() { if (dustu) return; dustu = true; akisKapat(); isYokla(k); }
    sihirbaz.akisBekci = setTimeout(dus, 3500);
    es.onmessage = function (ev) {
      if (sihirbaz.akis !== es) return; // eski akış (kalem değişti) — yok say
      clearTimeout(sihirbaz.akisBekci); sihirbaz.akisBekci = 0;
      sihirbaz.hataUst = 0;
      var v; try { v = JSON.parse(ev.data); } catch (x) { return; }
      isUygula(k, v);
    };
    es.onerror = function () {
      if (sihirbaz.akis !== es) return;
      if (!sihirbaz.aktif) { akisKapat(); return; }
      if (es.readyState === 2) dus(); // CLOSED: kurtarılamaz → yoklamaya düş
      // CONNECTING(0): EventSource kendiliğinden yeniden bağlanır — bekçi korur
    };
  }

  function kurSiradaki() {
    if (sihirbaz.sira >= sihirbaz.secili.length) { kurulumBitti(); return; }
    var k = sihirbaz.secili[sihirbaz.sira];
    $('kur-simdiki').textContent = 'Kuruluyor: ' + k.Ad + ' (' + (sihirbaz.sira + 1) + '/' + sihirbaz.secili.length + ')';
    genelSeg(sihirbaz.sira, 'aktif');
    sihirbaz.kalemBasZaman = Date.now();
    ilerlemeSifirla();
    logSatir('— ' + k.Ad + ' kurulumu başlıyor —', 'log-baslik');
    sihirbaz.segment = el('div'); // bu kalemin canlı log bölmesi (her yoklamada tazelenir)
    $('kur-log').appendChild(sihirbaz.segment);
    kurDene(k, 3);
  }

  async function kurDene(k, kalanHak) {
    try {
      var v = await api('/api/yerel/katalog/kur', { govde: { anahtar: k.Anahtar } });
      sihirbaz.isId = v && v.is_id;
      sihirbaz.hataUst = 0;
      isIzle(k); // gerçek zamanlı (SSE); başarısız olursa isYokla'ya (yoklama) düşer
    } catch (h) {
      if (h && h.yetkisiz) { sihirbaz.aktif = false; return; } // oturum düştü, sihirbaz durur
      if (h && h.uzunKod === 'KURULUM_ASILI') { // asılı kilit: retry ETME, operatör temizlemeli
        logSatir('⚠ ' + (h.message || 'önceki kurulum yarım kalmış (asılı kilit)'), 'log-uyari');
        asiliTemizleGoster(k);
        return;
      }
      if (h && h.kod === 409 && kalanHak > 0) { // başka kurulum sürüyor: 5 sn bekle, en çok 3 kez
        logSatir('Başka bir kurulum sürüyor, 5 sn sonra yeniden denenecek… (' + (4 - kalanHak) + '/3)');
        setTimeout(function () { kurDene(k, kalanHak - 1); }, 5000);
        return;
      }
      kalemBitti(k, 'basarisiz', h.message);
    }

  // asiliTemizleGoster — asılı kurulum kilidi 409'unda: operatöre "temizle+yeniden
  // dene" düğmesi. /kurulum-kilit-temizle uca POST eder (B-05), sonra kurDene tekrar.
  function asiliTemizleGoster(k) {
    var kap = el('div', 'uyari-satir');
    kap.appendChild(el('span', null, 'Önceki kurulum yarım kalmış. Çalışan bir yükleyici olmadığından eminseniz kilidi temizleyip yeniden deneyin.'));
    var btn = el('button', 'btn btn-cerceve btn-kucuk', 'Asılı kilidi temizle');
    btn.addEventListener('click', async function () {
      btn.disabled = true;
      try {
        await api('/api/yerel/kurulum-kilit-temizle', { govde: {} });
        tost('Asılı kilit temizlendi', 'basari');
        if (kap.parentNode) kap.parentNode.removeChild(kap);
        kurDene(k, 3); // temizlendi → yeniden dene
      } catch (e) { btn.disabled = false; tost('Temizlenemedi: ' + (e.message || ''), 'hata'); }
    });
    kap.appendChild(btn);
    $('kur-log').appendChild(kap);
  }
  }

  // isYokla — YOKLAMA yedeği (SSE kurulamazsa): 2 sn'de bir iş durumunu çeker.
  async function isYokla(k) {
    try {
      var v = await api('/api/yerel/katalog/is?id=' + encodeURIComponent(sihirbaz.isId));
      sihirbaz.hataUst = 0;
      if (isUygula(k, v)) return; // bitti → kalemBitti içeride çağrıldı
      setTimeout(function () { isYokla(k); }, 2000);
    } catch (h) {
      if (h && h.yetkisiz) { sihirbaz.aktif = false; return; }
      sihirbaz.hataUst++;
      if (sihirbaz.hataUst < 3) { setTimeout(function () { isYokla(k); }, 2000); return; }
      kalemBitti(k, 'basarisiz', 'İş durumu okunamadı: ' + h.message);
    }
  }

  function kalemBitti(k, durum, ekMesaj) {
    akisKapat();               // SSE bu kalem için kapanır (varsa)
    sihirbaz.isId = null;
    $('kur-ilerleme').hidden = true; // biten kalemin ETA çubuğu kalmasın
    var basarili = (durum === 'basarili'), kismi = (durum === 'kismi');
    sihirbaz.sonuc[k.Anahtar] = durum || 'basarisiz';
    genelSeg(sihirbaz.sira, basarili ? 'tamam' : kismi ? 'kismi' : 'hata');
    if (ekMesaj) logSatir(ekMesaj, 'log-hata');
    if (basarili) {
      logSatir('✓ ' + k.Ad + ' kuruldu', 'log-basari');
      sihirbaz.sira++;
      kurSiradaki();
      return;
    }
    if (kismi) { // adım geçti ama tam değil (B-07): başarısız DEĞİL, uyarıyla ilerle
      logSatir('◐ ' + k.Ad + ' kısmi kuruldu — ek yapılandırma gerekli', 'log-uyari');
      sihirbaz.sira++;
      kurSiradaki();
      return;
    }
    logSatir('✗ ' + k.Ad + ' kurulamadı', 'log-hata');
    tost(k.Ad + ' kurulumu başarısız', 'hata');
    if (sihirbaz.sira + 1 < sihirbaz.secili.length) $('kur-karar').hidden = false; // Devam Et / Durdur
    else kurulumBitti(); // son kalemdi, karar gereksiz
  }

  function kurulumBitti() {
    sihirbaz.aktif = false;
    akisKapat();
    $('kur-ilerleme').hidden = true;
    var kap = $('kur-sonuc-kap');
    bosalt(kap);
    var t = tabloKur(['Bileşen', 'Sonuç']);
    sihirbaz.secili.forEach(function (k) {
      var tr = el('tr');
      tr.appendChild(el('td', null, k.Ad));
      var td = el('td'), d = sihirbaz.sonuc[k.Anahtar];
      if (d === 'basarili') td.appendChild(cip('✓ Kuruldu', 'yesil'));
      else if (d === 'kismi') td.appendChild(cip('◐ Kısmi (yapılandırma gerekli)', 'amber'));
      else if (d === 'basarisiz') td.appendChild(cip('✗ Başarısız', 'kirmizi'));
      else td.appendChild(cip('— Atlandı', 'notr'));
      tr.appendChild(td);
      t.govde.appendChild(tr);
    });
    kap.appendChild(t.tablo);
    adimGoster(4);
  }

  // Onay kutusu değişince seçim haritasını güncelle (arama süzmesi seçimi bozmaz)
  $('kur-liste').addEventListener('change', function (e) {
    var t = e.target;
    if (!t || !t.classList || !t.classList.contains('kur-sec')) return;
    if (t.checked) sihirbaz.secim[t.value] = true; else delete sihirbaz.secim[t.value];
    secimGuncelle();
  });

  // Canlı arama: ada VE açıklamaya göre süz (istemci tarafı)
  $('kur-ara').addEventListener('input', function () {
    sihirbaz.terim = trk(this.value.trim());
    katalogBas();
  });

  $('btn-kur-devam').addEventListener('click', function () {
    var anahtarlar = seciliAnahtarlar();
    sihirbaz.secili = sihirbaz.katalog.filter(function (k) { return anahtarlar.indexOf(k.Anahtar) !== -1; });
    if (!sihirbaz.secili.length) return;
    var kap = $('kur-ozet-liste'), toplam = 0, deneyselVar = false;
    bosalt(kap);
    sihirbaz.secili.forEach(function (k) {
      var satir = el('div', 'kur-ozet-satir');
      satir.appendChild(el('span', null, k.Ad));
      satir.appendChild(el('span', 'soluk mono', k.SureTahmini || ''));
      kap.appendChild(satir);
      toplam += sureUst(k.SureTahmini); // Adım 1 özetiyle aynı: aralığın üst değeri
      if (k.Kademe === 'deneysel') deneyselVar = true;
    });
    kap.appendChild(el('div', 'kur-ozet-satir kur-toplam', 'Toplam tahmini süre: ~' + toplam + ' dk'));
    $('kur-deneysel-uyari').hidden = !deneyselVar;
    adimGoster(2);
  });

  $('btn-kur-geri').addEventListener('click', function () { adimGoster(1); });

  $('btn-kur-baslat').addEventListener('click', function () {
    akisKapat();
    sihirbaz.sira = 0;
    sihirbaz.sonuc = {};
    sihirbaz.aktif = true;
    sihirbaz.otoKaydir = true;
    bosalt($('kur-log'));
    $('kur-karar').hidden = true;
    genelCubukKur();   // seçili kalem sayısınca segment
    ilerlemeSifirla(); // ETA çubuğu temiz başlar
    adimGoster(3);
    kurSiradaki();
  });

  $('btn-kur-devam-et').addEventListener('click', function () {
    $('kur-karar').hidden = true;
    sihirbaz.sira++;
    kurSiradaki();
  });

  $('btn-kur-durdur').addEventListener('click', function () {
    $('kur-karar').hidden = true;
    kurulumBitti(); // başarısızdan sonrakiler "Atlandı" kalır
  });

  $('btn-kur-bitir').addEventListener('click', function () {
    sihirbaz.secim = {}; // tamamlanan kurulumdan sonra seçim temizlenir
    yukleKurulum();      // katalog tazelenir (Kurulu bayrakları değişti)
    yukleOzet();         // yetenek çipleri yeniden çekilir
    sekmeAc('ozet');
  });

  // Kullanıcı yukarı kaydırdıysa oto-kaydırma bırakılır; en alta dönünce yeniden başlar
  $('kur-log').addEventListener('scroll', function () {
    var kutu = $('kur-log');
    sihirbaz.otoKaydir = kutu.scrollTop + kutu.clientHeight >= kutu.scrollHeight - 24;
  });

  // ---------- Başlatma ve olay bağlama ----------
  async function panelBaslat() {
    var o = await api('/api/yerel/ozet'); // 401 ise api() giriş görünümüne düşürür
    yuklendi = { ozet: true };
    ozetBas(o);
    dashDurumYukle();   // 🔴 dashboard'i BOOT'ta HEMEN ciz (5sn timer'i bekleme = giriste blank kalmasin)
    gosterPanel();
    // Aktif sekme hatırlanır: kayıtlı değer geçerli bir sekmeyse ona dön, değilse Genel Bakış.
    // (Yalnız hangi sekme; sihirbaz iç-adımı YAZILMAZ — o mantık bellekte kalır.)
    var hedef = 'ozet';
    try { var kayit = localStorage.getItem('gosp_win_aktif_sekme'); if (kayit && yukleyici[kayit]) hedef = kayit; } catch (e) { /* depolama yoksa yut */ }
    sekmeAc(hedef);
  }

  $('giris-form').addEventListener('submit', async function (e) {
    e.preventDefault(); // Enter ile de çalışır (gerçek form submit)
    var b = $('btn-giris');
    b.disabled = true;
    try {
      await api('/api/yerel/giris', {
        govde: { kullanici: $('giris-kullanici').value.trim(), parola: $('giris-parola').value }
      });
      await panelBaslat();
      tost('Giriş başarılı', 'basari');
    } catch (h) { hataGoster(h); }
    b.disabled = false;
  });

  $('btn-cikis').addEventListener('click', async function () {
    try { await api('/api/yerel/cikis', { method: 'POST' }); }
    catch (h) { /* çıkış hatası yutulur, yine giriş görünümüne dönülür */ }
    gosterGiris();
    tost('Oturum kapatıldı', 'basari');
  });

  var sekmeler = document.querySelectorAll('.sekme');
  for (var i = 0; i < sekmeler.length; i++) {
    sekmeler[i].addEventListener('click', function (e) { sekmeAc(e.currentTarget.getAttribute('data-sekme')); });
  }

  // Ray bölüm ikonları: tıklayınca o bölümün etiketli paneli açılır. İçerik o an o
  // bölümde DEĞİLSE bölümün ilk sekmesine geçilir (Linux: bölüme gir); zaten
  // oradaysan içerik değişmez, yalnız panel senkronlanır.
  var raylar = document.querySelectorAll('.ray-dugme');
  for (var ri = 0; ri < raylar.length; ri++) {
    raylar[ri].addEventListener('click', function (e) {
      var anahtar = e.currentTarget.getAttribute('data-bolum');
      if (SEKME_BOLUM[aktifSekme] === anahtar) { bolumAc(anahtar); return; }
      var ilk = document.querySelector('.sekme-grup[data-grup="' + anahtar + '"] .sekme');
      if (ilk) { sekmeAc(ilk.getAttribute('data-sekme')); } else { bolumAc(anahtar); }
    });
  }

  // Dil seçiciler (üst bar + giriş); değişince aktif dil yazılır + görünen metinler tazelenir
  var dilSecs = document.querySelectorAll('.dil-sec');
  for (var di = 0; di < dilSecs.length; di++) {
    dilSecs[di].addEventListener('change', function (e) { dilDegistir(e.target.value); });
  }
  metinUygula(); // kayıtlı/tarayıcı dilini açılışta uygula

  $('site-ekle-form').addEventListener('submit', async function (e) {
    e.preventDefault();
    var girdi = $('site-alan'), b = $('btn-site-ekle');
    var alan = girdi.value.trim();
    if (!alan) return;
    b.disabled = true;
    try {
      await api('/api/yerel/siteler', { govde: { alan_adi: alan } });
      tost('Site eklendi: ' + alan, 'basari');
      girdi.value = '';
      yukleSiteler();
    } catch (h) { hataGoster(h); } // 422: mühür vb. Türkçe mesaj aynen tost edilir
    b.disabled = false;
  });

  $('btn-siteler-yenile').addEventListener('click', yukleSiteler);
  $('btn-olaylar-yenile').addEventListener('click', yukleOlaylar);
  $('btn-gorevler-yenile').addEventListener('click', yukleGorevler);
  $('olay-gunluk').addEventListener('change', yukleOlaylar);
  $('olay-seviye').addEventListener('change', olaylarBas);
  $('gorev-tumu').addEventListener('change', yukleGorevler);

  $('btn-vt-yenile').addEventListener('click', yukleVt);
  $('vt-motor').addEventListener('change', vtListeYukle);
  $('vt-olustur-form').addEventListener('submit', async function (e) {
    e.preventDefault();
    var ad = $('vt-ad').value.trim();
    if (!ad) return;
    var b = $('btn-vt-olustur');
    b.disabled = true;
    try {
      await api('/api/yerel/vt-olustur', { govde: { motor: $('vt-motor').value, ad: ad, kullanici: $('vt-kullanici').value.trim(), parola: $('vt-parola').value } });
      tost('Veritabanı oluşturuldu: ' + ad, 'basari');
      $('vt-ad').value = ''; $('vt-kullanici').value = ''; $('vt-parola').value = '';
      vtListeYukle();
    } catch (h) { hataGoster(h); }
    b.disabled = false;
  });

  $('btn-servisler-yenile').addEventListener('click', yukleServisler);
  // Kaynak kutuları 5 sn'de bir: yalnız Servisler aktif, sekme görünür ve panel açıkken
  setInterval(function () {
    if (aktifSekme === 'servisler' && !document.hidden && !$('gorunum-panel').hidden && yuklendi.servisler) kaynakYukle();
    if (aktifSekme === 'ozet' && !document.hidden && !$('gorunum-panel').hidden && yuklendi.ozet) dashDurumYukle();
  }, 5000);

  // Açılış: özeti dene; 401 ise giriş görünümüne düş
  panelBaslat().catch(function (h) {
    if (h && h.yetkisiz) return;
    gosterGiris();
    hataGoster(h);
  });
})();
