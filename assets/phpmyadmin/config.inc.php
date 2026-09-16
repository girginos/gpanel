<?php
declare(strict_types=1);

// Blowfish secret (sessions imzalama icin)
$cfg['blowfish_secret'] = 'BLOWFISH_SECRET_BURAYA';

$i = 1;

// Tek server, signon auth
$cfg['Servers'][$i]['auth_type']     = 'signon';
$cfg['Servers'][$i]['SignonURL']     = '/pma-signon.php';
$cfg['Servers'][$i]['SignonSession'] = 'pma_signon';
$cfg['Servers'][$i]['LogoutURL']     = '/abonelikler';
$cfg['Servers'][$i]['host']          = 'localhost';
$cfg['Servers'][$i]['compress']      = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;

// Genel ayarlar
$cfg['ServerDefault']             = 1;
$cfg['blowfish_secret_length']    = 32;
$cfg['UploadDir']                 = '';
$cfg['SaveDir']                   = '';
$cfg['ShowDatabasesNavigationAsTree'] = true;
$cfg['DefaultLang']               = 'tr';
$cfg['Lang']                      = 'tr';
$cfg['MaxNavigationItems']        = 100;
$cfg['CheckConfigurationPermissions'] = false;
$cfg['ShowPhpInfo']               = false;
$cfg['ShowChgPassword']           = false;
$cfg['Servers'][$i]['hide_db']    = '^(mysql|information_schema|performance_schema|sys|panel|phpmyadmin)$';

// Cookie session timeout (saniye) — 3600 = 1 saat
$cfg['LoginCookieValidity']       = 3600;
$cfg['LoginCookieStore']          = 0;

// tmp dir (templates, twig cache)
$cfg['TempDir'] = '/var/lib/phpmyadmin/tmp';

// ---- Yapılandırma depolaması (pmadb) — gelişmiş özellikler ----
$cfg['Servers'][$i]['controluser'] = 'pma';
$cfg['Servers'][$i]['controlpass'] = 'PMA_CONTROL_PASS_BURAYA';
$cfg['Servers'][$i]['pmadb']       = 'phpmyadmin';
$cfg['Servers'][$i]['bookmarktable'] = 'pma__bookmark';
$cfg['Servers'][$i]['relation']      = 'pma__relation';
$cfg['Servers'][$i]['table_info']    = 'pma__table_info';
$cfg['Servers'][$i]['table_coords']  = 'pma__table_coords';
$cfg['Servers'][$i]['pdf_pages']     = 'pma__pdf_pages';
$cfg['Servers'][$i]['column_info']   = 'pma__column_info';
$cfg['Servers'][$i]['history']       = 'pma__history';
$cfg['Servers'][$i]['table_uiprefs'] = 'pma__table_uiprefs';
$cfg['Servers'][$i]['tracking']      = 'pma__tracking';
$cfg['Servers'][$i]['userconfig']    = 'pma__userconfig';
$cfg['Servers'][$i]['recent']        = 'pma__recent';
$cfg['Servers'][$i]['favorite']      = 'pma__favorite';
$cfg['Servers'][$i]['users']         = 'pma__users';
$cfg['Servers'][$i]['usergroups']    = 'pma__usergroups';
$cfg['Servers'][$i]['navigationhiding'] = 'pma__navigationhiding';
$cfg['Servers'][$i]['savedsearches']    = 'pma__savedsearches';
$cfg['Servers'][$i]['central_columns']  = 'pma__central_columns';
$cfg['Servers'][$i]['designer_settings'] = 'pma__designer_settings';
$cfg['Servers'][$i]['export_templates']  = 'pma__export_templates';
