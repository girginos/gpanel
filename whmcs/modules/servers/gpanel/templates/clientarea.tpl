{* GirginOSPanel — müşteri alanı. Tüm değerler |escape ile kaçışlanır. *}
<div class="panel panel-default gpanel-hesap">
  <div class="panel-heading">
    <h3 class="panel-title">{if $tur eq 'bayi'}{$L.bayi_baslik|escape}{else}{$L.baslik|escape}{/if}</h3>
  </div>
  <div class="panel-body">
    {if $hata}
      <div class="alert alert-warning"><strong>{$L.hata_baslik|escape}</strong><br>{$hata|escape}</div>
    {/if}

    {if $hesap}
      <table class="table table-condensed">
        {if $tur eq 'bayi'}
          <tr><th>{$L.kullanici|escape}</th><td>{$hesap.ad|escape}</td></tr>
          <tr><th>{$L.plan|escape}</th><td>{$hesap.plan|escape}</td></tr>
          <tr><th>{$L.alan_sayisi|escape}</th><td>{$hesap.alan_sayisi|escape}{if $hesap.max_domain} / {$hesap.max_domain|escape}{/if}</td></tr>
        {else}
          <tr><th>{$L.alan_adi|escape}</th><td>{$hesap.ad|escape}</td></tr>
          <tr><th>{$L.durum|escape}</th><td>{if $hesap.durum eq 'askida'}{$L.durum_askida|escape}{else}{$L.durum_aktif|escape}{/if}</td></tr>
          <tr><th>{$L.plan|escape}</th><td>{$hesap.plan|escape}</td></tr>
          <tr><th>{$L.php|escape}</th><td>{$hesap.php|escape}</td></tr>
          <tr><th>{$L.ftp_host|escape}</th><td>{$hesap.ftp_host|escape}</td></tr>
          <tr><th>{$L.ftp_user|escape}</th><td>{$hesap.ftp_user|escape}</td></tr>
          <tr><th>{$L.ip|escape}</th><td>{$hesap.ipv4|escape}</td></tr>
        {/if}
      </table>

      {foreach from=$cubuklar item=c}
        <p><strong>{$L[$c.ad]|escape}</strong>:
          {$c.kullanim|escape} {$L.mb|escape} /
          {if $c.limit > 0}{$c.limit|escape} {$L.mb|escape}{else}{$L.sinirsiz|escape}{/if}</p>
        {if $c.limit > 0}
        <div class="progress">
          <div class="progress-bar{if $c.yuzde >= 90} progress-bar-danger{elseif $c.yuzde >= 75} progress-bar-warning{/if}"
               role="progressbar" aria-valuenow="{$c.yuzde|escape}" aria-valuemin="0" aria-valuemax="100"
               style="width: {$c.yuzde|escape}%">{$c.yuzde|escape}%</div>
        </div>
        {/if}
      {/foreach}
    {/if}

    <p>
      {if $tur neq 'bayi'}
        <a class="btn btn-primary" href="clientarea.php?action=productdetails&amp;id={$serviceid|escape}&amp;dosinglesignon=1">{$L.panele_giris|escape}</a>
      {/if}
      {if $panel_url}
        <a class="btn btn-default" href="{$panel_url|escape}" target="_blank" rel="noopener noreferrer">{$L.panele_git|escape}</a>
      {/if}
    </p>

    {if $tur neq 'bayi'}
      <hr>
      <h4>{$L.parola_baslik|escape}</h4>
      <form method="post" action="clientarea.php?action=productdetails" autocomplete="off">
        <input type="hidden" name="token" value="{$token|escape}">
        <input type="hidden" name="id" value="{$serviceid|escape}">
        <input type="hidden" name="modop" value="custom">
        <input type="hidden" name="a" value="parolaDegistir">
        <div class="form-group">
          <label for="gpanel-yeni-parola">{$L.parola_yeni|escape}</label>
          <input type="password" class="form-control" id="gpanel-yeni-parola" name="yeni_parola" minlength="12" maxlength="128" required autocomplete="new-password">
        </div>
        <div class="form-group">
          <label for="gpanel-yeni-parola2">{$L.parola_tekrar|escape}</label>
          <input type="password" class="form-control" id="gpanel-yeni-parola2" name="yeni_parola2" minlength="12" maxlength="128" required autocomplete="new-password">
          <p class="help-block">{$L.parola_kural|escape}</p>
        </div>
        <button type="submit" class="btn btn-default">{$L.parola_gonder|escape}</button>
      </form>
    {else}
      <p class="text-muted">{$L.bayi_parola_not|escape}</p>
    {/if}
  </div>
</div>
