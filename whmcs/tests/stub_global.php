<?php
/** Global WHMCS islev saplamalari. */

if (!defined('WHMCS')) {
    define('WHMCS', true);
}

$GLOBALS['T'] = ['log' => [], 'localapi' => [], 'activity' => [], 'check_token' => 0];

function logModuleCall($module, $action, $request, $response, $processed = '', $replace = [])
{
    $GLOBALS['T']['log'][] = compact('module', 'action', 'request', 'response', 'processed', 'replace');
}

function localAPI($cmd, $vals = [], $admin = '')
{
    $GLOBALS['T']['localapi'][] = [$cmd, $vals];
    return ['result' => 'success'];
}

function logActivity($m)
{
    $GLOBALS['T']['activity'][] = $m;
}

function generate_token($t = 'link')
{
    return 'TOKEN123';
}

function check_token($ns = 'WHMCS.default')
{
    $GLOBALS['T']['check_token']++;
}
