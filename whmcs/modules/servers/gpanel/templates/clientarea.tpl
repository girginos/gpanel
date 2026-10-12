{* GirginOSPanel — müşteri alanı. Tüm değerler |escape ile kaçışlanır.
   WHMCS "Six" (Bootstrap 3) ve "Twenty-One" (Bootstrap 4) temalarında aynı görünsün diye
   tema sınıflarına dayanmaz; .gpca kapsamlı kendi CSS'ini taşır. Panel bağlantıları YENİ
   SEKMEDE açılır (WHMCS sayfası kaybolmaz). *}
{literal}<style>
.gpca{--gb:#e5e7eb;--gs:#f3f4f6;--gm:#6b7280;--gt:#111827;--gp:#2563eb;--gph:#1d4ed8;--gok:#047857;--gokb:#ecfdf5;--gw:#b45309;--gwb:#fffbeb;--ge:#b91c1c;--geb:#fef2f2;
  color:var(--gt);font-size:14px;line-height:1.5;text-align:left;margin:4px 0 8px}
.gpca *,.gpca *::before,.gpca *::after{box-sizing:border-box}
.gpca-kart{background:#fff;border:1px solid var(--gb);border-radius:12px;padding:20px;margin:0 0 16px;box-shadow:0 1px 2px rgba(17,24,39,.04)}
.gpca-kart h3{font-size:15px;font-weight:600;margin:0 0 14px;color:var(--gt)}
.gpca-ust{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:16px}
.gpca-kimlik{display:flex;align-items:center;gap:14px;min-width:0;flex:1 1 280px}
.gpca-ikon{width:46px;height:46px;border-radius:12px;background:#eff6ff;color:var(--gp);display:flex;align-items:center;justify-content:center;flex:none}
.gpca-ad{font-size:19px;font-weight:700;margin:0;line-height:1.25;word-break:break-all}
.gpca-rozetler{display:flex;flex-wrap:wrap;gap:6px;margin-top:6px}
.gpca-rozet{display:inline-flex;align-items:center;gap:6px;font-size:12px;font-weight:600;padding:3px 10px;border-radius:999px;background:var(--gs);color:#374151;white-space:nowrap}
.gpca-rozet.ok{background:var(--gokb);color:var(--gok)}.gpca-rozet.uyari{background:var(--gwb);color:var(--gw)}.gpca-rozet.hata{background:var(--geb);color:var(--ge)}
.gpca-nokta{width:7px;height:7px;border-radius:50%;background:currentColor;flex:none}
.gpca-eylem{display:flex;flex-wrap:wrap;gap:8px}
.gpca-btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;padding:10px 16px;border-radius:9px;font-weight:600;font-size:14px;line-height:1.2;border:1px solid transparent;text-decoration:none!important;cursor:pointer;transition:background .15s,border-color .15s}
.gpca-btn svg{flex:none}
.gpca-btn.birincil{background:var(--gp);color:#fff!important}.gpca-btn.birincil:hover,.gpca-btn.birincil:focus{background:var(--gph)}
.gpca-btn.ikincil{background:#fff;color:var(--gt)!important;border-color:var(--gb)}.gpca-btn.ikincil:hover,.gpca-btn.ikincil:focus{background:#f9fafb;border-color:#d1d5db}
.gpca-btn.pasif{background:var(--gs);color:var(--gm)!important;cursor:not-allowed}
.gpca-btn:focus-visible{outline:2px solid var(--gp);outline-offset:2px}
.gpca-uyari{display:flex;gap:10px;align-items:flex-start;border-radius:10px;padding:12px 14px;margin:0 0 16px;font-size:13.5px}
.gpca-uyari.hata{background:var(--geb);color:var(--ge);border:1px solid #fecaca}.gpca-uyari.uyari{background:var(--gwb);color:var(--gw);border:1px solid #fde68a}
.gpca-uyari strong{display:block;margin-bottom:2px}
.gpca-olcerler{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}
.gpca-olcer{border:1px solid var(--gs);border-radius:10px;padding:14px 16px;background:#fcfcfd}
.gpca-olcer-ust{display:flex;justify-content:space-between;align-items:baseline;gap:8px}
.gpca-olcer-ad{font-size:12px;font-weight:600;color:var(--gm);text-transform:uppercase;letter-spacing:.05em}
.gpca-olcer-yuzde{font-size:12px;font-weight:700;color:var(--gm)}
.gpca-olcer-deger{font-size:20px;font-weight:700;margin-top:4px;font-variant-numeric:tabular-nums}
.gpca-olcer-deger small{font-size:13px;font-weight:500;color:var(--gm)}
.gpca-cubuk{height:8px;border-radius:99px;background:var(--gs);overflow:hidden;margin-top:10px}
.gpca-cubuk span{display:block;height:100%;border-radius:99px;background:var(--gp);min-width:2px}
.gpca-olcer.uyari .gpca-cubuk span{background:#f59e0b}.gpca-olcer.uyari .gpca-olcer-yuzde{color:var(--gw)}
.gpca-olcer.kritik .gpca-cubuk span{background:#ef4444}.gpca-olcer.kritik .gpca-olcer-yuzde{color:var(--ge)}
.gpca-liste{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));column-gap:28px;margin:0}
.gpca-satir{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:10px 0;border-bottom:1px solid var(--gs);min-width:0}
.gpca-satir dt{font-weight:500;color:var(--gm);margin:0;white-space:nowrap}
.gpca-satir dd{margin:0;font-weight:600;text-align:right;min-width:0;word-break:break-all}
.gpca-kod{font-family:SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;background:var(--gs);border-radius:6px;padding:2px 7px;user-select:all;-webkit-user-select:all}
.gpca-ipucu{font-size:12.5px;color:var(--gm);margin:12px 0 0}
.gpca details>summary{cursor:pointer;list-style:none;display:flex;align-items:center;justify-content:space-between;gap:12px;font-weight:600;font-size:15px}
.gpca details>summary::-webkit-details-marker{display:none}
.gpca details>summary .gpca-ok{transition:transform .15s;color:var(--gm)}
.gpca details[open]>summary .gpca-ok{transform:rotate(180deg)}
.gpca details>summary:focus-visible{outline:2px solid var(--gp);outline-offset:4px;border-radius:6px}
.gpca-form{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px 16px;margin-top:16px}
.gpca-form label{display:block;font-size:13px;font-weight:600;margin:0 0 6px;color:#374151}
.gpca-form input[type=password]{width:100%;height:40px;padding:8px 12px;border:1px solid #d1d5db;border-radius:8px;font-size:14px;background:#fff;color:var(--gt)}
.gpca-form input[type=password]:focus{border-color:var(--gp);outline:none;box-shadow:0 0 0 3px rgba(37,99,235,.15)}
.gpca-form-alt{grid-column:1/-1;display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px}
.gpca-form-alt p{margin:0;font-size:12.5px;color:var(--gm)}
@media (max-width:575px){.gpca-kart{padding:16px}.gpca-eylem{width:100%}.gpca-eylem .gpca-btn{flex:1 1 100%}.gpca-satir{flex-wrap:wrap}.gpca-satir dd{text-align:left}}
</style>{/literal}

<div class="gpca">

  {if $hata}
    <div class="gpca-uyari hata" role="alert">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M12 8v4M12 16h.01"/></svg>
      <div><strong>{$L.hata_baslik|escape}</strong>{$hata|escape}</div>
    </div>
  {/if}

  {if $hesap && $hesap.durum eq 'askida'}
    <div class="gpca-uyari uyari" role="status">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></svg>
      <div>{$L.askida_uyari|escape}</div>
    </div>
  {/if}

  {* ── Üst kart: kimlik + rozetler + panel eylemleri ── *}
  <div class="gpca-kart">
    <div class="gpca-ust">
      <div class="gpca-kimlik">
        <div class="gpca-ikon" aria-hidden="true">
          {if $tur eq 'bayi'}
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8"/></svg>
          {else}
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10Z"/></svg>
          {/if}
        </div>
        <div style="min-width:0">
          <p class="gpca-ad">{if $hesap}{$hesap.ad|escape}{else}{if $tur eq 'bayi'}{$L.bayi_baslik|escape}{else}{$L.baslik|escape}{/if}{/if}</p>
          {if $hesap}
            <div class="gpca-rozetler">
              {if $hesap.durum eq 'askida'}
                <span class="gpca-rozet uyari"><span class="gpca-nokta"></span>{$L.durum_askida|escape}</span>
              {else}
                <span class="gpca-rozet ok"><span class="gpca-nokta"></span>{$L.durum_aktif|escape}</span>
              {/if}
              {if $hesap.plan}<span class="gpca-rozet">{$hesap.plan|escape}</span>{/if}
              {if $tur neq 'bayi'}
                {if $hesap.php}<span class="gpca-rozet">PHP {$hesap.php|escape}</span>{/if}
                {if $hesap.ssl}
                  <span class="gpca-rozet ok">{$L.ssl_var|escape}{if $hesap.ssl_bitis} · {$hesap.ssl_bitis|escape}{/if}</span>
                {else}
                  <span class="gpca-rozet uyari">{$L.ssl_yok|escape}</span>
                {/if}
              {/if}
            </div>
          {/if}
        </div>
      </div>

      <div class="gpca-eylem">
        {if $tur neq 'bayi'}
          {if $hesap && $hesap.durum eq 'askida'}
            <span class="gpca-btn pasif" aria-disabled="true">{$L.panele_giris|escape}</span>
          {else}
            <a class="gpca-btn birincil" href="clientarea.php?action=productdetails&amp;id={$serviceid|escape}&amp;dosinglesignon=1" target="_blank" rel="noopener" title="{$L.yeni_sekme|escape}">
              {$L.panele_giris|escape}
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true"><path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/></svg>
            </a>
          {/if}
        {/if}
        {if $panel_url}
          <a class="gpca-btn {if $tur eq 'bayi'}birincil{else}ikincil{/if}" href="{$panel_url|escape}" target="_blank" rel="noopener noreferrer" title="{$L.yeni_sekme|escape}">
            {if $tur eq 'bayi'}{$L.bayi_paneli|escape}{else}{$L.panele_git|escape}{/if}
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true"><path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/></svg>
          </a>
        {/if}
      </div>
    </div>
  </div>

  {if $hesap}
    {* ── Kaynak kullanımı ── *}
    {if $olcerler}
      <div class="gpca-kart">
        <h3>{$L.kullanim_baslik|escape}</h3>
        <div class="gpca-olcerler">
          {foreach from=$olcerler item=o}
            <div class="gpca-olcer {$o.sinif|escape}">
              <div class="gpca-olcer-ust">
                <span class="gpca-olcer-ad">{$L[$o.ad]|escape}</span>
                {if $o.limitli}<span class="gpca-olcer-yuzde">{$o.yuzde|escape}%</span>{/if}
              </div>
              <div class="gpca-olcer-deger">{$o.kullanim_metin|escape} <small>/ {if $o.limitli}{$o.limit_metin|escape}{else}{$L.sinirsiz|escape}{/if}</small></div>
              {if $o.limitli}
                <div class="gpca-cubuk" role="progressbar" aria-label="{$L[$o.ad]|escape}" aria-valuenow="{$o.yuzde|escape}" aria-valuemin="0" aria-valuemax="100"><span style="width: {$o.yuzde|escape}%"></span></div>
              {/if}
            </div>
          {/foreach}
        </div>
      </div>
    {/if}

    {* ── Hesap bilgileri ── *}
    <div class="gpca-kart">
      <h3>{$L.hesap_bilgileri|escape}</h3>
      <dl class="gpca-liste">
        {if $tur eq 'bayi'}
          <div class="gpca-satir"><dt>{$L.kullanici|escape}</dt><dd><span class="gpca-kod">{$hesap.ad|escape}</span></dd></div>
          <div class="gpca-satir"><dt>{$L.plan|escape}</dt><dd>{$hesap.plan|escape}</dd></div>
          <div class="gpca-satir"><dt>{$L.alan_sayisi|escape}</dt><dd>{$hesap.alan_sayisi|escape}{if $hesap.max_domain} / {$hesap.max_domain|escape}{else} / {$L.sinirsiz|escape}{/if}</dd></div>
        {else}
          <div class="gpca-satir"><dt>{$L.alan_adi|escape}</dt><dd>{$hesap.ad|escape}</dd></div>
          <div class="gpca-satir"><dt>{$L.ip|escape}</dt><dd><span class="gpca-kod">{$hesap.ipv4|escape}</span></dd></div>
          <div class="gpca-satir"><dt>{$L.ftp_host|escape}</dt><dd><span class="gpca-kod">{$hesap.ftp_host|escape}</span></dd></div>
          <div class="gpca-satir"><dt>{$L.ftp_user|escape}</dt><dd><span class="gpca-kod">{$hesap.ftp_user|escape}</span></dd></div>
          <div class="gpca-satir"><dt>{$L.php|escape}</dt><dd>{$hesap.php|escape}</dd></div>
          <div class="gpca-satir"><dt>{$L.plan|escape}</dt><dd>{$hesap.plan|escape}</dd></div>
        {/if}
      </dl>
      {if $tur neq 'bayi'}<p class="gpca-ipucu">{$L.giris_ipucu|escape}</p>{/if}
    </div>
  {/if}

  {* ── Parola ── *}
  {if $tur neq 'bayi'}
    <div class="gpca-kart">
      <details>
        <summary>
          <span>{$L.parola_baslik|escape}</span>
          <svg class="gpca-ok" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>
        </summary>
        <form class="gpca-form" method="post" action="clientarea.php?action=productdetails" autocomplete="off">
          <input type="hidden" name="token" value="{$token|escape}">
          <input type="hidden" name="id" value="{$serviceid|escape}">
          <input type="hidden" name="modop" value="custom">
          <input type="hidden" name="a" value="parolaDegistir">
          <div>
            <label for="gpanel-yeni-parola">{$L.parola_yeni|escape}</label>
            <input type="password" id="gpanel-yeni-parola" name="yeni_parola" minlength="12" maxlength="128" required autocomplete="new-password">
          </div>
          <div>
            <label for="gpanel-yeni-parola2">{$L.parola_tekrar|escape}</label>
            <input type="password" id="gpanel-yeni-parola2" name="yeni_parola2" minlength="12" maxlength="128" required autocomplete="new-password">
          </div>
          <div class="gpca-form-alt">
            <p>{$L.parola_kural|escape} {$L.parola_kapsam|escape}</p>
            <button type="submit" class="gpca-btn birincil">{$L.parola_gonder|escape}</button>
          </div>
        </form>
      </details>
    </div>
  {else}
    <p class="gpca-ipucu">{$L.bayi_parola_not|escape}</p>
  {/if}

</div>
