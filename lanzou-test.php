<?php
/**
 * lanzou-test.php v3 — 蓝奏云(LanzouGo)文件管理面板
 * 底部导航三 Tab：文件列表 / 分享 / 设置
 * 后端透传本机 LanzouGo 服务 http://127.0.0.1:8080
 * 纯原生 PHP + curl，无外部依赖。测试用，可随时删除。
 */
error_reporting(0);
ini_set('display_errors', '0');
header('Content-Type: text/html; charset=utf-8');

$API = 'http://127.0.0.1:8080';

function apiReq($url, $post = null, $timeout = 180)
{
    $ch = curl_init($url);
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    curl_setopt($ch, CURLOPT_TIMEOUT, $timeout);
    curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
    curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
    if ($post !== null) {
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS, http_build_query($post));
    }
    $resp = curl_exec($ch);
    $err  = curl_error($ch);
    curl_close($ch);
    if ($err) return ['code' => -1, 'msg' => '请求失败: ' . $err];
    $d = json_decode($resp, true);
    if (!is_array($d)) return ['code' => -1, 'msg' => '响应异常: ' . mb_substr($resp, 0, 120)];
    return $d;
}

/* ================= AJAX 端点（全部返回 JSON） ================= */
$ajax = $_GET['ajax'] ?? '';
if ($ajax !== '') {
    header('Content-Type: application/json; charset=utf-8');

    if ($ajax === 'list') {
        $folder = isset($_GET['folder']) ? trim($_GET['folder']) : '0';
        echo json_encode(apiReq($API . '/api/list?folder=' . urlencode($folder), null, 60), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'mkdir') {
        echo json_encode(apiReq($API . '/api/mkdir', ['name' => $_POST['name'] ?? '', 'folder' => $_POST['folder'] ?? '0']), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'delete') {
        echo json_encode(apiReq($API . '/api/delete', ['id' => $_POST['id'] ?? '', 'type' => $_POST['type'] ?? 'file']), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'share') {
        echo json_encode(apiReq($API . '/api/share', ['id' => $_POST['id'] ?? '', 'type' => $_POST['type'] ?? 'file', 'pwd' => $_POST['pwd'] ?? '']), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'parse') {
        echo json_encode(apiReq($API . '/api/parse?url=' . urlencode($_POST['url'] ?? '') . '&pwd=' . urlencode($_POST['pwd'] ?? ''), null, 60), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'rename') {
        echo json_encode(apiReq($API . '/api/rename', ['id' => $_POST['id'] ?? '', 'newname' => $_POST['newname'] ?? '', 'folder' => $_POST['folder'] ?? '0'], 300), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'save') {
        echo json_encode(apiReq($API . '/api/save', ['url' => $_POST['url'] ?? '', 'pwd' => $_POST['pwd'] ?? '', 'folder' => $_POST['folder'] ?? '0'], 600), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'status') {
        echo json_encode(apiReq($API . '/api/status'), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'cookie') {
        $cookie = $_POST['cookie'] ?? '';
        echo json_encode(apiReq($API . '/api/cookie', ['cookie' => $cookie], 90), JSON_UNESCAPED_UNICODE);
        exit;
    }
    if ($ajax === 'upload') {
        if (!isset($_FILES['file']) || $_FILES['file']['error'] !== UPLOAD_ERR_OK) {
            echo json_encode(['code' => -1, 'msg' => '未收到文件或上传被中断'], JSON_UNESCAPED_UNICODE);
            exit;
        }
        $upName = $_FILES['file']['name'];
        $safeDir = '/tmp/lzweb_' . bin2hex(random_bytes(6));
        if (!@mkdir($safeDir, 0700)) {
            echo json_encode(['code' => -1, 'msg' => '临时目录创建失败'], JSON_UNESCAPED_UNICODE);
            exit;
        }
        $safe = $safeDir . '/' . preg_replace('/[\/\\\\]/', '_', $upName);
        if (!move_uploaded_file($_FILES['file']['tmp_name'], $safe)) {
            @rmdir($safeDir);
            echo json_encode(['code' => -1, 'msg' => '临时文件写入失败'], JSON_UNESCAPED_UNICODE);
            exit;
        }
        $folder = $_POST['folder'] ?? '0';
        $r = apiReq($API . '/api/upload', ['path' => $safe, 'folder' => $folder], 300);
        @unlink($safe);
        @rmdir($safeDir);
        echo json_encode($r, JSON_UNESCAPED_UNICODE);
        exit;
    }
    echo json_encode(['code' => 400, 'msg' => '未知ajax动作']);
    exit;
}

/* ================= 下载代理：?dl=1&u=<直链>&name=<文件名> =================
 * 同源代理 Go /api/download（已处理蓝奏 acw 挑战 + 验证页 + 302），
 * 强制 Content-Disposition: attachment 触发浏览器下载框，不跳转、不落盘。
 */
$dl = $_GET['dl'] ?? '';
if ($dl !== '') {
    $u = $_GET['u'] ?? '';
    $name = $_GET['name'] ?? 'download';
    if ($u === '') { http_response_code(400); exit('missing url'); }
    $host = strtolower((string)parse_url($u, PHP_URL_HOST));
    if ($host === '' || (strpos($host, 'lanrar.com') === false && strpos($host, 'lanzou') === false && strpos($host, 'webgetstore') === false)) {
        http_response_code(400); exit('bad url');
    }
    header('Content-Type: application/octet-stream');
    header('Content-Disposition: attachment; filename="download"; filename*=UTF-8\'\'' . rawurlencode($name));
    header('X-Accel-Buffering: no');
    $ch = curl_init($API . '/api/download?url=' . rawurlencode($u));
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, false);
    curl_setopt($ch, CURLOPT_TIMEOUT, 300);
    curl_setopt($ch, CURLOPT_FOLLOWLOCATION, true);
    curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
    curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
    curl_exec($ch);
    curl_close($ch);
    exit;
}
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<meta name="theme-color" content="#05080f" />
<title>LanzouGo 面板</title>
<link rel="preconnect" href="https://fonts.googleapis.com" />
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
<link href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@500;600;700&family=IBM+Plex+Mono:wght@400;500&display=swap" rel="stylesheet" />
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop stop-color='%233b82f6'/%3E%3Cstop offset='1' stop-color='%2322d3ee'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='24' height='24' rx='6' fill='url(%23g)'/%3E%3Ctext x='12' y='17' font-size='12' font-family='monospace' font-weight='bold' text-anchor='middle' fill='%23fff'%3ELZ%3C/text%3E%3C/svg%3E" />
<style>
*{margin:0;padding:0;box-sizing:border-box;-webkit-tap-highlight-color:transparent}
:root{
  color-scheme:dark;
  --bg0:#05080f; --bg1:#0a1120;
  --card:#0e1626; --card2:#101b30;
  --line:#1b2940; --line2:#253750;
  --txt:#e6edf7; --sub:#93a4c0; --weak:#5d7090;
  --blue:#3b82f6; --cyan:#22d3ee; --indigo:#6366f1;
  --green:#22c55e; --red:#ef4444; --amber:#eab308;
  --font-d:'Space Grotesk','PingFang SC','Microsoft YaHei',system-ui,-apple-system,sans-serif;
  --font-m:'IBM Plex Mono','Cascadia Code',Consolas,'Courier New',monospace;
  --grad:linear-gradient(135deg,#3b82f6,#22d3ee);
  --shadow:0 10px 34px rgba(2,6,16,.55);
}
html{-webkit-text-size-adjust:100%}
body{background:var(--bg0);color:var(--txt);font-family:var(--font-d);min-height:100vh;padding-bottom:calc(78px + env(safe-area-inset-bottom));overflow-x:hidden}
button{font-family:inherit}
button:focus-visible,input:focus-visible,textarea:focus-visible{outline:2px solid rgba(34,211,238,.75);outline-offset:2px}
button,input,textarea,.nm-click,.fit{touch-action:manipulation}
/* ===== 背景：数据网格 + 流动光晕（云存储母题） ===== */
.bgfx{position:fixed;inset:0;z-index:-1;overflow:hidden;background:radial-gradient(120% 90% at 50% -10%,#0c1730 0%,#070c18 46%,#05080f 100%)}
.bgfx::before{content:"";position:absolute;inset:0;background-image:linear-gradient(rgba(59,130,246,.05) 1px,transparent 1px),linear-gradient(90deg,rgba(59,130,246,.05) 1px,transparent 1px);background-size:44px 44px;mask-image:radial-gradient(90% 80% at 50% 0%,#000 0%,transparent 78%)}
.bgfx .glow{position:absolute;border-radius:50%;filter:blur(70px);opacity:.65;will-change:transform}
.bgfx .g1{width:440px;height:440px;left:-140px;top:-120px;background:radial-gradient(circle,rgba(59,130,246,.55),transparent 70%);animation:drift1 16s ease-in-out infinite alternate}
.bgfx .g2{width:420px;height:420px;right:-150px;bottom:-140px;background:radial-gradient(circle,rgba(34,211,238,.38),transparent 70%);animation:drift2 20s ease-in-out infinite alternate}
.bgfx .g3{width:460px;height:460px;right:-110px;top:-150px;background:radial-gradient(circle,rgba(56,189,248,.5),transparent 70%);animation:drift3 18s ease-in-out infinite alternate}
@keyframes drift1{to{transform:translate(70px,46px) scale(1.15)}}
@keyframes drift2{to{transform:translate(-60px,-40px) scale(1.12)}}
@keyframes drift3{to{transform:translate(-70px,60px) scale(1.14)}}
/* ===== 顶部品牌栏 ===== */
.wrap{max-width:640px;margin:0 auto;padding:18px 12px 24px}
.hd{display:flex;align-items:center;gap:11px;padding:2px 2px 16px;position:relative}
.hd .logo{width:38px;height:38px;border-radius:11px;background:var(--grad);display:flex;align-items:center;justify-content:center;font-size:13px;font-weight:700;color:#fff;font-family:var(--font-m);letter-spacing:.5px;box-shadow:0 6px 18px rgba(59,130,246,.38);position:relative;flex-shrink:0}
.hd .logo::after{content:"";position:absolute;inset:0;border-radius:11px;border:1px solid rgba(255,255,255,.22)}
.hd .ht{flex:1;min-width:0}
.hd h1{font-size:19px;font-weight:700;letter-spacing:.3px;line-height:1.2;display:flex;align-items:center;gap:8px}
.hd .sub{font-size:10.5px;color:var(--weak);font-family:var(--font-m);letter-spacing:2.2px;text-transform:uppercase;margin-top:3px}
.badge{display:inline-flex;align-items:center;gap:5px;font-size:10px;font-weight:600;color:#7dd3fc;border:1px solid rgba(34,211,238,.35);background:rgba(34,211,238,.08);padding:3px 8px;border-radius:999px;letter-spacing:.4px;white-space:nowrap}
.badge i{width:6px;height:6px;border-radius:50%;background:var(--cyan);animation:pulse 2.2s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1;box-shadow:0 0 0 0 rgba(34,211,238,.5)}50%{opacity:.55;box-shadow:0 0 0 5px rgba(34,211,238,0)}}
/* ===== 搜索与工具行 ===== */
.searchbar{display:flex;gap:8px;margin-bottom:11px}
.searchbar input{flex:1;background:rgba(9,15,27,.8);border:1px solid var(--line);color:var(--txt);border-radius:11px;padding:11px 13px;font-size:14px;transition:border-color .2s,box-shadow .2s}
.searchbar input:focus{border-color:rgba(34,211,238,.6);box-shadow:0 0 0 3px rgba(34,211,238,.12)}
.searchbar button{background:rgba(59,130,246,.12);border:1px solid rgba(59,130,246,.28);color:#93c5fd;border-radius:11px;padding:0 15px;font-size:13px;cursor:pointer;transition:background .18s,transform .12s;font-weight:500}
.searchbar button:active{transform:scale(.96);background:rgba(59,130,246,.2)}
.toolrow{display:flex;gap:9px;margin-bottom:13px}
.toolrow button{flex:1;display:flex;align-items:center;justify-content:center;gap:7px;padding:12.5px;border:none;border-radius:12px;font-size:14px;cursor:pointer;color:#fff;font-weight:600;transition:transform .12s,filter .18s,box-shadow .2s}
.toolrow button:active{transform:scale(.97);filter:brightness(1.12)}
.btn-dir{background:linear-gradient(135deg,#4f46e5,#6366f1);box-shadow:0 6px 16px rgba(99,102,241,.3)}
.btn-up{background:linear-gradient(135deg,#16a34a,#22c55e);box-shadow:0 6px 16px rgba(34,197,94,.28)}
/* ===== 卡片 ===== */
.card{background:linear-gradient(180deg,var(--card2),var(--card));border:1px solid var(--line);border-radius:15px;padding:15px;margin-bottom:13px;box-shadow:var(--shadow);position:relative}
.card::before{content:"";position:absolute;inset:0 0 auto 0;height:1px;border-radius:15px 15px 0 0;background:linear-gradient(90deg,transparent,rgba(148,197,255,.28),transparent)}
.card .t{font-size:12px;font-weight:600;color:var(--sub);margin-bottom:11px;display:flex;align-items:center;gap:7px;letter-spacing:.3px}
.card .t svg{flex-shrink:0;opacity:.85}
/* ===== 文件列表表格 ===== */
table{width:100%;border-collapse:collapse;font-size:14px}
th{text-align:left;color:var(--weak);font-weight:500;font-size:11px;padding:6px 8px;border-bottom:1px solid var(--line2);letter-spacing:.6px}
td{padding:10px 8px;border-bottom:1px solid rgba(27,41,64,.6);vertical-align:middle}
tbody tr{animation:rowIn .28s ease both}
tbody tr:hover{background:rgba(59,130,246,.045)}
tbody tr:last-child td{border-bottom:none}
@keyframes rowIn{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:none}}
.fname{display:flex;align-items:center;gap:9px}
.fname .ic{width:34px;height:34px;border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0;transition:transform .15s}
.ic.dir{background:linear-gradient(135deg,#4f46e5,#3b82f6);color:#fff;box-shadow:0 3px 12px rgba(99,102,241,.35)}
.ic.file{background:linear-gradient(135deg,#16a34a,#22c55e);color:#fff;box-shadow:0 3px 12px rgba(34,197,94,.3)}
.nm{word-break:break-all;line-height:1.35;min-width:0}
.nm .nm-click{cursor:pointer;transition:color .15s;background:none;border:none;padding:0;font:inherit;color:inherit;text-align:left}
.nm .nm-click:hover{color:#7dd3fc}
.fmeta{font-size:11px;color:var(--weak);margin-top:3px;font-family:var(--font-m);letter-spacing:.2px}
.fmeta .pwd-tag{color:var(--amber);font-weight:600}
.op{display:flex;gap:6px;justify-content:flex-end;flex-wrap:wrap}
.op button{padding:5.5px 10px;font-size:11.5px;border-radius:8px;border:none;cursor:pointer;color:#fff;font-weight:600;letter-spacing:.2px;transition:transform .12s,filter .18s;white-space:nowrap}
.op button:active{transform:scale(.94)}
.op .b-info{background:rgba(59,130,246,.16);color:#93c5fd;border:1px solid rgba(59,130,246,.34)}
.op .b-del{background:rgba(239,68,68,.13);color:#fca5a5;border:1px solid rgba(239,68,68,.3)}
.op .b-dl{background:rgba(34,197,94,.15);color:#86efac;border:1px solid rgba(34,197,94,.32)}
.empty{padding:26px 0;text-align:center;color:var(--weak);font-size:13px}
/* ===== 输入行 ===== */
.inrow{display:flex;gap:8px;margin-bottom:8px}
.inrow input,.inrow textarea{flex:1;min-width:0;background:rgba(9,15,27,.8);border:1px solid var(--line);color:var(--txt);border-radius:11px;padding:11px 12px;font-size:14px;transition:border-color .2s,box-shadow .2s}
.inrow input:focus{border-color:rgba(34,211,238,.6);box-shadow:0 0 0 3px rgba(34,211,238,.12)}
#cookieInput:focus{border-color:rgba(34,211,238,.6);box-shadow:0 0 0 3px rgba(34,211,238,.12)}
.inrow button{white-space:nowrap;background:var(--grad);border:none;color:#fff;border-radius:11px;padding:0 17px;font-size:14px;cursor:pointer;font-weight:600;transition:transform .12s,filter .18s}
.inrow button:active{transform:scale(.96);filter:brightness(1.12)}
.btn{display:inline-block;background:linear-gradient(135deg,#2563eb,#3b82f6);color:#fff;border:none;border-radius:11px;padding:11px 18px;font-size:14px;cursor:pointer;font-weight:600;transition:transform .12s,filter .18s,box-shadow .2s}
.btn:active{transform:scale(.97)}
.btn.green{background:linear-gradient(135deg,#16a34a,#22c55e)}
.btn.red{background:linear-gradient(135deg,#dc2626,#ef4444)}
.btn.gray{background:#334155;color:#e2e8f0}
.btn.full{width:100%}
/* ===== hover 视觉反馈 ===== */
.toolrow button:hover{filter:brightness(1.1);box-shadow:0 8px 22px rgba(59,130,246,.35)}
.op button:hover{filter:brightness(1.18)}
.searchbar button:hover{background:rgba(59,130,246,.22)}
.btn:hover,.dacts button:hover,.sres .sa button:hover{filter:brightness(1.1)}
/* ===== Toast ===== */
.toast{position:fixed;top:14px;left:50%;transform:translateX(-50%);z-index:200;background:rgba(17,26,45,.94);border:1px solid rgba(59,130,246,.4);color:#93c5fd;padding:11px 16px;border-radius:11px;font-size:13px;max-width:92%;word-break:break-all;display:none;box-shadow:0 10px 30px rgba(0,0,0,.55);animation:toastIn .24s ease both;backdrop-filter:blur(8px)}
.toast.err{background:rgba(42,20,24,.94);border-color:rgba(239,68,68,.45);color:#fda4af}
.toast.ok{background:rgba(14,35,26,.94);border-color:rgba(34,197,94,.42);color:#86efac}
@keyframes toastIn{from{opacity:0;transform:translate(-50%,-10px)}to{opacity:1;transform:translate(-50%,0)}}
/* ===== 遮罩与对话框 ===== */
.mask{display:none;position:fixed;inset:0;background:rgba(4,8,16,.72);backdrop-filter:blur(5px);z-index:99;align-items:center;justify-content:center;padding:16px}
.mask.on{display:flex}
.dialog{background:linear-gradient(180deg,var(--card2),var(--card));border:1px solid var(--line2);border-radius:16px;padding:19px;width:100%;max-width:430px;max-height:86vh;overflow:auto;overscroll-behavior:contain;box-shadow:0 24px 60px rgba(0,0,0,.6);animation:popIn .22s cubic-bezier(.2,.9,.3,1.2) both}
@keyframes popIn{from{opacity:0;transform:scale(.94) translateY(10px)}to{opacity:1;transform:none}}
.dialog h3{font-size:16px;margin-bottom:13px;display:flex;align-items:center;gap:8px;font-weight:700}
.dialog h3 .x{margin-left:auto;cursor:pointer;color:var(--sub);font-size:21px;padding:0 4px;line-height:1;transition:color .15s;background:none;border:none;font-family:inherit}
.dialog h3 .x:hover{color:var(--txt)}
.dline{margin:9px 0;font-size:13px;color:var(--sub)}
.dline b{color:var(--txt);font-weight:600}
.dval{background:rgba(9,15,27,.85);border:1px solid var(--line);border-radius:9px;padding:9px 11px;font-size:12px;color:#93c5fd;word-break:break-all;margin:7px 0;font-family:var(--font-m)}
.dacts{display:flex;gap:8px;margin-top:15px;flex-wrap:wrap}
.dacts button{flex:1;min-width:86px;border:none;border-radius:10px;padding:10.5px;font-size:13px;color:#fff;cursor:pointer;font-weight:600;transition:transform .12s,filter .18s}
.dacts button:active{transform:scale(.96)}
.inrow.small{margin:6px 0}
.inrow.small input{padding:8px 10px;font-size:13px}
/* ===== 上传浮层 ===== */
.upmask{position:fixed;inset:0;background:rgba(4,8,16,.78);backdrop-filter:blur(6px);z-index:150;display:none;align-items:center;justify-content:center;padding:20px}
.upmask.on{display:flex}
.upbox{background:linear-gradient(180deg,var(--card2),var(--card));border:1px solid var(--line2);border-radius:16px;padding:24px;width:100%;max-width:340px;text-align:center;overscroll-behavior:contain;box-shadow:0 24px 60px rgba(0,0,0,.6);animation:popIn .22s cubic-bezier(.2,.9,.3,1.2) both}
.upbox .fname{font-size:14px;word-break:break-all;margin-bottom:13px;color:var(--txt)}
.upbox .upbar{height:9px;background:rgba(9,15,27,.9);border-radius:6px;overflow:hidden;margin:11px 0;border:1px solid var(--line)}
.upbox .upbar i{display:block;height:100%;width:0;background:linear-gradient(90deg,#3b82f6,#22d3ee);border-radius:6px;transition:width .15s;position:relative;overflow:hidden}
.upbox .upbar i::after{content:"";position:absolute;top:0;bottom:0;left:-60%;width:60%;background:linear-gradient(110deg,transparent 30%,rgba(255,255,255,.55) 50%,transparent 70%);animation:shine 1.4s linear infinite}
@keyframes shine{from{transform:translateX(0)}to{transform:translateX(430%)}}
.upbox .pct{font-size:16px;font-weight:700;color:#7dd3fc;margin-bottom:15px;font-family:var(--font-m)}
.upbox .cancel{background:#334155;border:none;color:#e2e8f0;border-radius:10px;padding:9px 22px;font-size:13px;cursor:pointer;transition:transform .12s,filter .18s;font-weight:500}
.upbox .cancel:active{transform:scale(.95)}
/* ===== 底部导航 ===== */
.tabbar{position:fixed;bottom:0;left:0;right:0;background:rgba(10,16,30,.52);backdrop-filter:blur(22px) saturate(150%);-webkit-backdrop-filter:blur(22px) saturate(150%);border-top:1px solid rgba(59,130,246,.16);z-index:50;display:flex;box-shadow:0 -10px 36px rgba(2,6,16,.55);padding-bottom:env(safe-area-inset-bottom)}
.tabbar::before{content:"";position:absolute;top:-34px;left:0;right:0;height:34px;pointer-events:none;background:linear-gradient(180deg,transparent,rgba(10,16,30,.34))}
.tabbar .tb{flex:1;padding:10px 0 12px;display:flex;flex-direction:column;align-items:center;gap:3px;color:#6e7f9c;font-size:11px;cursor:pointer;user-select:none;position:relative;transition:color .2s;font-weight:500;background:none;border:none;font-family:inherit}
.tabbar .tb svg{width:21px;height:21px;transition:transform .2s}
.tabbar .tb::before{content:"";position:absolute;top:-1px;left:50%;transform:translateX(-50%) scaleX(0);width:34px;height:3px;border-radius:0 0 4px 4px;background:var(--grad);transition:transform .22s ease}
.tabbar .tb.on{color:#7dd3fc}
.tabbar .tb.on::before{transform:translateX(-50%) scaleX(1)}
.tabbar .tb:active svg{transform:scale(.9)}
/* ===== 视图切换 ===== */
.view{display:none}
.view.on{display:block;animation:fadeUp .26s ease both}
@keyframes fadeUp{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:none}}
/* ===== 分享结果 ===== */
.sres{border:1px solid rgba(34,197,94,.22);background:linear-gradient(180deg,rgba(13,24,19,.55),rgba(9,15,27,.7));border-radius:11px;padding:13px;margin-bottom:9px;transition:border-color .2s,transform .15s}
.sres:hover{border-color:rgba(34,211,238,.35)}
.sres .sn{font-size:14px;word-break:break-all;margin-bottom:4px;font-weight:600}
.sres .sm{font-size:12px;color:var(--weak);font-family:var(--font-m)}
.sres .sa{display:flex;gap:8px;margin-top:9px}
.sres .sa button{flex:1;border:none;border-radius:9px;padding:9px;font-size:12.5px;color:#fff;cursor:pointer;font-weight:600;transition:transform .12s,filter .18s}
.sres .sa button:active{transform:scale(.96)}
/* ===== 文件夹选择 ===== */
.foldersel .fit{display:flex;align-items:center;gap:9px;padding:10px;border:0;border-bottom:1px solid rgba(27,41,64,.6);background:none;width:100%;font:inherit;color:inherit;text-align:left;cursor:pointer;border-radius:8px;transition:background .15s}
.foldersel .fit:hover{background:rgba(59,130,246,.08)}
.foldersel .fit:last-child{border-bottom:none}
.foldersel .fit .fname{flex:1}
.selbar{position:sticky;bottom:0;background:var(--card);padding-top:11px;border-top:1px solid var(--line)}
/* ===== 加载指示 ===== */
.spin{display:inline-block;width:14px;height:14px;border:2px solid rgba(125,211,252,.25);border-top-color:#7dd3fc;border-radius:50%;animation:sp .8s linear infinite;vertical-align:middle}
@keyframes sp{to{transform:rotate(360deg)}}
/* ===== 滚动条 ===== */
::-webkit-scrollbar{width:8px;height:8px}
::-webkit-scrollbar-thumb{background:#1e2c46;border-radius:8px}
::-webkit-scrollbar-thumb:hover{background:#2a3c5e}
::-webkit-scrollbar-track{background:transparent}
/* ===== 小屏适配 ===== */
@media (max-width:400px){.op button{font-size:10.5px;padding:4.5px 7px}}
@media (prefers-reduced-motion:reduce){*,*::before,*::after{animation-duration:.01ms !important;animation-iteration-count:1 !important;transition-duration:.01ms !important}}
</style>
</head>
<body>

<div class="bgfx" aria-hidden="true"><div class="glow g1"></div><div class="glow g2"></div><div class="glow g3"></div></div>
<div class="toast" id="toast" role="status" aria-live="polite"></div>

<div class="wrap">
  <div class="hd">
    <div class="logo">LZ</div>
    <div class="ht">
      <h1 id="pageTitle">文件列表</h1>
      <div class="sub" translate="no">LANZOUGO LOCAL</div>
    </div>
    <span class="badge"><i></i>LOCAL</span>
  </div>

  <!-- ========== Tab1 文件列表 ========== -->
  <div class="view on" id="view-files">
    <div class="searchbar">
      <input type="text" id="fsSearch" placeholder="搜索当前文件夹的文件…" autocomplete="off" aria-label="搜索当前文件夹的文件" />
      <button onclick="clearSearch()">清空</button>
    </div>
    <div class="toolrow">
      <button class="btn-dir" onclick="openMkdir()">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/></svg>
        创建文件夹
      </button>
      <button class="btn-up" onclick="document.getElementById('fileInput').click()">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        上传文件
      </button>
      <input type="file" id="fileInput" style="display:none" aria-label="选择上传文件" />
    </div>
    <div class="card">
      <div class="t"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/></svg><span id="listTitle">根目录</span></div>
      <div id="backRow" style="display:none;margin-bottom:8px"><button class="btn gray" style="width:100%" onclick="openFolder('0')">← 返回根目录</button></div>
      <table>
        <thead><tr><th>名称</th><th style="width:128px">操作</th></tr></thead>
        <tbody id="listBody"></tbody>
      </table>
      <div class="empty" id="listEmpty" style="display:none">此文件夹为空</div>
    </div>
  </div>

  <!-- ========== Tab2 分享 ========== -->
  <div class="view" id="view-share">
    <div class="card">
      <div class="t"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.6" y1="13.5" x2="15.4" y2="17.5"/><line x1="15.4" y1="6.5" x2="8.6" y2="10.5"/></svg>解析分享链接（支持文件 / 文件夹）</div>
      <div class="inrow"><input type="url" id="shareUrl" placeholder="蓝奏分享链接…" autocomplete="off" aria-label="蓝奏分享链接" /></div>
      <div class="inrow"><input type="text" id="sharePwd" placeholder="提取码（无密码留空）" autocomplete="off" spellcheck="false" aria-label="提取码" /></div>
      <div class="inrow"><button style="flex:1" onclick="doParse()">解析</button></div>
    </div>
    <div class="card">
      <div class="t"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>解析结果 <button class="btn gray" style="float:right;padding:3px 10px;font-size:11px" onclick="clearShareResult()">清空</button></div>
      <div class="searchbar">
        <input type="text" id="srSearch" placeholder="搜索解析结果…" autocomplete="off" aria-label="搜索解析结果" />
      </div>
      <div id="shareResult"></div>
      <div class="empty" id="shareEmpty" style="display:none">暂无结果，先在上方输入分享链接解析</div>
    </div>
  </div>

  <!-- ========== Tab3 设置 ========== -->
  <div class="view" id="view-settings">
    <div class="card">
      <div class="t"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>当前 Cookie 状态</div>
      <div id="statusBox" class="dval">加载中…</div>
    </div>
    <div class="card">
      <div class="t"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>配置 Cookie（自动检测可用性，有效才保存；失效则沿用之前的）</div>
      <textarea id="cookieInput" rows="4" style="width:100%;background:rgba(9,15,27,.8);border:1px solid var(--line);color:var(--txt);border-radius:11px;padding:10px;font-size:12px;font-family:var(--font-m);resize:vertical" placeholder="粘贴完整 Cookie，例如：phpdisk_info=xxx; PHPSESSID=xxx; ..." aria-label="Cookie" autocomplete="off" spellcheck="false"></textarea>
      <div class="inrow" style="margin-top:10px"><button style="flex:1" onclick="saveCookie()">保存并检测</button></div>
    </div>
  </div>
</div>

<!-- 上传浮层 -->
<div class="upmask" id="upMask">
  <div class="upbox">
    <div class="fname" id="upFileName">文件上传</div>
    <div class="upbar"><i id="upBarFill"></i></div>
    <div class="pct" id="upPct">0%</div>
    <button class="cancel" onclick="abortUpload()">中断上传</button>
  </div>
</div>

<!-- 通用弹窗 -->
<div class="mask" id="dlgMask" onclick="if(event.target===this)closeDlg()">
  <div class="dialog" id="dlgBox"></div>
</div>

<!-- 底部导航 -->
<nav class="tabbar">
  <button type="button" class="tb on" data-tab="files" onclick="switchTab('files')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
    文件列表
  </button>
  <button type="button" class="tb" data-tab="share" onclick="switchTab('share')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.6" y1="13.5" x2="15.4" y2="17.5"/><line x1="15.4" y1="6.5" x2="8.6" y2="10.5"/></svg>
    分享
  </button>
  <button type="button" class="tb" data-tab="settings" onclick="switchTab('settings')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    设置
  </button>
</nav>

<script>
/* ============ 全局状态 ============ */
let folder = '0';
let fileList = [];
let tab = 'files';
let shareData = null;
let upXhr = null;

/* ============ Tab 切换 ============ */
function switchTab(t) {
  tab = t;
  document.querySelectorAll('.tb').forEach(b => b.classList.toggle('on', b.dataset.tab === t));
  document.querySelectorAll('.view').forEach(v => v.classList.remove('on'));
  document.getElementById('view-' + t).classList.add('on');
  const titles = { files: '文件列表', share: '分享', settings: '设置' };
  document.getElementById('pageTitle').textContent = titles[t];
  if (history.replaceState) history.replaceState(null, '', '?tab=' + t);
  if (t === 'files') loadList();
  if (t === 'settings') loadStatus();
}

/* ============ Toast ============ */
let toastTimer = null;
function toast(msg, type) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast' + (type ? ' ' + type : '');
  t.style.display = 'block';
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.style.display = 'none', 4200);
}

/* ============ 文件列表 ============ */
function loadList(f) {
  if (f !== undefined) folder = f;
  document.getElementById('listBody').innerHTML = '<tr><td colspan="2" style="text-align:center;color:#64748b"><span class="spin"></span> 加载中…</td></tr>';
  document.getElementById('listEmpty').style.display = 'none';
  fetch('/lanzou-test.php?ajax=list&folder=' + encodeURIComponent(folder))
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { toast('获取列表失败：' + (j.msg || ''), 'err'); return; }
      fileList = (j.data && j.data.list) || [];
      renderList();
    })
    .catch(e => toast('网络错误：' + e, 'err'));
}

function renderList() {
  const kw = document.getElementById('fsSearch').value.trim().toLowerCase();
  const list = kw ? fileList.filter(x => (x.name_all || x.name || '').toLowerCase().includes(kw)) : fileList;
  const body = document.getElementById('listBody');
  const empty = document.getElementById('listEmpty');
  document.getElementById('backRow').style.display = (folder === '0' || folder === '') ? 'none' : 'block';
  if (list.length === 0) {
    body.innerHTML = '';
    empty.style.display = 'block';
    document.getElementById('listTitle').textContent = folder === '0' ? '根目录（空）' : ('文件夹（空）');
    return;
  }
  document.getElementById('listTitle').textContent = folder === '0' ? '根目录' : '文件夹';
  empty.style.display = 'none';
  let html = '';
  list.forEach(it => {
    const name = it.name_all || it.name || '';
    const isDir = !!it.is_dir || it.type === 'folder';
    if (isDir) {
      html += '<tr><td><div class="fname"><span class="ic dir">' + folderIco() + '</span><span class="nm"><button type="button" class="nm-click" onclick="openFolder(\'' + escId(it.id) + '\')">' + escHtml(name) + '</button><div class="fmeta">文件夹</div></span></div></td>' +
        '<td><div class="op"><button class="b-info" onclick="openDirDetail(\'' + escId(it.id) + '\',\'' + escJs(name) + '\')">详情</button><button class="b-del" onclick="delItem(\'' + escId(it.id) + '\',\'folder\')">删除</button></div></td></tr>';
    } else {
      const metaParts = [];
      if (it.size) metaParts.push('大小：' + escHtml(it.size));
      if (it.time) metaParts.push('时间：' + escHtml(it.time));
      const meta = metaParts.join('<br/>');
      html += '<tr><td><div class="fname"><span class="ic file">' + fileIco() + '</span><span class="nm"><button type="button" class="nm-click" onclick="openFileDetail(\'' + escId(it.id) + '\',\'' + escJs(name) + '\')">' + escHtml(name) + '</button><div class="fmeta">' + meta + (it.onof && it.onof !== '0' ? ' · <span class="pwd-tag">有提取码</span>' : '') + '</div></span></div></td>' +
        '<td><div class="op"><button class="b-dl" onclick="quickDownload(\'' + escId(it.id) + '\')">下载</button><button class="b-info" onclick="openFileDetail(\'' + escId(it.id) + '\',\'' + escJs(name) + '\')">详情</button><button class="b-del" onclick="delItem(\'' + escId(it.id) + '\',\'file\')">删除</button></div></td></tr>';
    }
  });
  body.innerHTML = html;
}

function openFolder(id) { loadList(id); document.getElementById('fsSearch').value = ''; }
function clearSearch() {
  document.getElementById('fsSearch').value = '';
  renderList();
}
document.getElementById('fsSearch').addEventListener('input', renderList);

function folderIco() { return '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/></svg>'; }
function fileIco() { return '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>'; }

/* ============ 上传（带进度条） ============ */
document.getElementById('fileInput').addEventListener('change', function() {
  const f = this.files[0];
  if (!f) return;
  startUpload(f);
  this.value = '';
});

function startUpload(file) {
  document.getElementById('upMask').classList.add('on');
  document.getElementById('upFileName').textContent = '文件上传：' + file.name;
  document.getElementById('upBarFill').style.width = '0%';
  document.getElementById('upPct').textContent = '0%';
  const fd = new FormData();
  fd.append('file', file);
  fd.append('folder', folder);
  const xhr = new XMLHttpRequest();
  upXhr = xhr;
  xhr.open('POST', '/lanzou-test.php?ajax=upload');
  xhr.upload.onprogress = function(e) {
    if (!e.lengthComputable) return;
    const p = Math.round(e.loaded / e.total * 100);
    document.getElementById('upBarFill').style.width = p + '%';
    document.getElementById('upPct').textContent = p + '%';
  };
  xhr.onload = function() {
    document.getElementById('upMask').classList.remove('on');
    let j = {};
    try { j = JSON.parse(xhr.responseText); } catch (e) {}
    if (j.code === 0) {
      const d = j.data || {};
      toast('上传成功：' + (d.name || file.name) + ' 提取码：' + (d.pwd || '001'), 'ok');
    } else {
      toast('上传失败：' + (j.msg || xhr.responseText || '未知错误'), 'err');
    }
    loadList();
  };
  xhr.onerror = function() {
    document.getElementById('upMask').classList.remove('on');
    toast('上传失败（可能已中断或网络错误）', 'err');
  };
  xhr.onabort = function() {
    document.getElementById('upMask').classList.remove('on');
    toast('上传已中断', 'err');
  };
  xhr.send(fd);
}
function abortUpload() { if (upXhr) upXhr.abort(); }

/* ============ 创建文件夹 ============ */
function openMkdir() {
  document.getElementById('dlgBox').innerHTML =
    '<h3>创建文件夹<button type="button" class="x" onclick="closeDlg()" aria-label="关闭">&times;</button></h3>' +
    '<div class="inrow"><input type="text" id="mkdirName" placeholder="文件夹名称…" autocomplete="off" aria-label="文件夹名称" /></div>' +
    '<div class="dacts"><button type="button" style="background:linear-gradient(135deg,#2563eb,#3b82f6)" onclick="doMkdir()">创建</button><button type="button" class="btn gray" style="flex:1" onclick="closeDlg()">取消</button></div>';
  document.getElementById('dlgMask').classList.add('on');
  if (window.matchMedia && window.matchMedia('(pointer: fine)').matches) {
    setTimeout(() => document.getElementById('mkdirName').focus(), 100);
  }
}
function doMkdir() {
  const name = document.getElementById('mkdirName').value.trim();
  if (!name) { toast('请输入文件夹名称', 'err'); return; }
  const fd = new FormData();
  fd.append('name', name);
  fd.append('folder', folder);
  fetch('/lanzou-test.php?ajax=mkdir', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      closeDlg();
      if (j.code === 0) { toast('已创建文件夹：' + name, 'ok'); loadList(); }
      else toast('创建失败：' + (j.msg || ''), 'err');
    });
}

/* ============ 删除 ============ */
function delItem(id, type) {
  if (!confirm('确认删除？此操作不可恢复')) return;
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', type);
  fetch('/lanzou-test.php?ajax=delete', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code === 0) { toast('已删除', 'ok'); loadList(); }
      else toast('删除失败：' + (j.msg || ''), 'err');
    });
}

/* ============ 文件详情 ============ */
function openFileDetail(id, nameEnc) {
  const dlg = document.getElementById('dlgBox');
  dlg.innerHTML = '<h3>文件详情<button type="button" class="x" onclick="closeDlg()" aria-label="关闭">&times;</button></h3>' +
    '<div class="dline">名称：<b>' + escHtml(nameEnc) + '</b></div>' +
    '<div class="dline">提取码：<b id="dPwd">加载中…</b></div>' +
    '<div class="dline">分享链接</div><div class="dval" id="dUrl">加载中…</div>' +
    '<div class="dline">更改提取码</div><div class="inrow small"><input type="text" id="dNewPwd" placeholder="新提取码" autocomplete="off" aria-label="新提取码" /><button type="button" style="font-size:12px;padding:0 10px" onclick="changePwd(winDlgId,\'file\')">设置</button></div>' +
    '<div class="dline">重命名</div><div class="inrow small"><input type="text" id="dNewName" placeholder="新文件名" autocomplete="off" aria-label="新文件名" /><button type="button" style="font-size:12px;padding:0 10px" onclick="doRename(winDlgId,winDlgName)">重命名</button></div>' +
    '<div class="dacts">' +
    '<button type="button" style="background:linear-gradient(135deg,#16a34a,#22c55e)" onclick="dlFromDlg()">下载</button>' +
    '<button type="button" style="background:#334155" onclick="copyShare()">复制链接</button>' +
    '<button type="button" style="background:linear-gradient(135deg,#dc2626,#ef4444)" onclick="delDlg()">删除</button>' +
    '</div><div class="dline" id="dErr" style="color:#fda4af"></div>';
  document.getElementById('dlgMask').classList.add('on');
  window.winDlgId = id;
  window.winDlgName = nameEnc;
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', 'file');
  fetch('/lanzou-test.php?ajax=share', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { document.getElementById('dErr').textContent = '获取分享信息失败：' + (j.msg || ''); return; }
      const d = j.data || {};
      window._dlg = { id, shareUrl: d.share_url || '', pwd: d.pwd || '' };
      document.getElementById('dPwd').textContent = d.pwd || '（无）';
      document.getElementById('dUrl').textContent = d.share_url || '（未分享）';
    })
    .catch(e => document.getElementById('dErr').textContent = '请求失败：' + e);
}

function changePwd(id, type) {
  const p = document.getElementById('dNewPwd').value.trim();
  if (!p) { toast('请输入新提取码', 'err'); return; }
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', type);
  fd.append('pwd', p);
  fetch('/lanzou-test.php?ajax=share', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code === 0) { toast('提取码已改为：' + p, 'ok'); document.getElementById('dPwd').textContent = p; }
      else toast('设置失败：' + (j.msg || ''), 'err');
    });
}

function doRename(id, oldName) {
  const n = document.getElementById('dNewName').value.trim();
  if (!n) { toast('请输入新文件名', 'err'); return; }
  const fd = new FormData();
  fd.append('id', id);
  fd.append('newname', n);
  fd.append('folder', folder);
  fetch('/lanzou-test.php?ajax=rename', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code === 0) { closeDlg(); toast('重命名成功：' + n + '（已保留原提取码）', 'ok'); loadList(); }
      else toast('重命名失败：' + (j.msg || '') + (j.detail ? ' ' + j.detail : ''), 'err');
    });
}

function dlFromDlg() {
  const d = window._dlg;
  if (!d || !d.shareUrl) { document.getElementById('dErr').textContent = '无分享链接'; return; }
  parseAndOpen(d.shareUrl, d.pwd);
}
function copyShare() {
  const d = window._dlg;
  if (!d || !d.shareUrl) { toast('无分享链接', 'err'); return; }
  copyText(d.shareUrl + ' 提取码:' + d.pwd);
}
function delDlg() {
  const d = window._dlg;
  if (!d || !confirm('确认删除此文件？')) return;
  const fd = new FormData();
  fd.append('id', d.id);
  fd.append('type', 'file');
  fetch('/lanzou-test.php?ajax=delete', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      closeDlg();
      if (j.code === 0) { toast('已删除', 'ok'); loadList(); }
      else toast('删除失败：' + (j.msg || ''), 'err');
    });
}

/* 列表快速下载 */
function quickDownload(id) {
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', 'file');
  fetch('/lanzou-test.php?ajax=share', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0 || !(j.data || {}).share_url) { toast('无法下载：未分享或获取失败', 'err'); return; }
      parseAndOpen(j.data.share_url, j.data.pwd || '');
    });
}

/* ============ 文件夹详情 ============ */
function openDirDetail(id, nameEnc) {
  const dlg = document.getElementById('dlgBox');
  dlg.innerHTML = '<h3>文件夹详情<button type="button" class="x" onclick="closeDlg()" aria-label="关闭">&times;</button></h3>' +
    '<div class="dline">名称：<b>' + escHtml(nameEnc) + '</b></div>' +
    '<div class="dline">提取码：<b id="dPwd">加载中…</b></div>' +
    '<div class="dline">分享链接</div><div class="dval" id="dUrl">加载中…</div>' +
    '<div class="dline">更改提取码 <span style="color:#64748b;font-size:11px">（文件夹为会员功能，不可用）</span></div><div class="inrow small"><input type="text" id="dNewPwd" placeholder="新提取码" disabled aria-label="新提取码" /><button type="button" style="font-size:12px;padding:0 10px" disabled onclick="changePwd(winDlgId,\'folder\')">设置</button></div>' +
    '<div class="dacts">' +
    '<button type="button" style="background:#334155" onclick="copyShare()">复制链接</button>' +
    '<button type="button" style="background:linear-gradient(135deg,#dc2626,#ef4444)" onclick="delDlgDir(winDlgId)">删除</button>' +
    '</div><div class="dline" id="dErr" style="color:#fda4af"></div>';
  document.getElementById('dlgMask').classList.add('on');
  window.winDlgId = id;
  window.winDlgName = nameEnc;
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', 'folder');
  fetch('/lanzou-test.php?ajax=share', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { document.getElementById('dErr').textContent = '获取分享信息失败：' + (j.msg || ''); return; }
      const d = j.data || {};
      window._dlg = { id, shareUrl: d.share_url || '', pwd: d.pwd || '' };
      document.getElementById('dPwd').textContent = d.pwd || '（无）';
      document.getElementById('dUrl').textContent = d.share_url || '（未分享）';
    })
    .catch(e => document.getElementById('dErr').textContent = '请求失败：' + e);
}
function delDlgDir(id) {
  if (!confirm('确认删除此文件夹及其中全部文件？')) return;
  const fd = new FormData();
  fd.append('id', id);
  fd.append('type', 'folder');
  fetch('/lanzou-test.php?ajax=delete', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      closeDlg();
      if (j.code === 0) { toast('已删除', 'ok'); loadList(); }
      else toast('删除失败：' + (j.msg || ''), 'err');
    });
}

/* ============ 解析（分享 Tab） ============ */
function doParse() {
  const url = document.getElementById('shareUrl').value.trim();
  const pwd = document.getElementById('sharePwd').value.trim();
  if (!url) { toast('请输入分享链接', 'err'); return; }
  window._shareSrc = url;
  window._sharePwd = pwd;
  const box = document.getElementById('shareResult');
  box.innerHTML = '<div style="text-align:center;color:#64748b"><span class="spin"></span> 解析中…</div>';
  document.getElementById('shareEmpty').style.display = 'none';
  const fd = new FormData();
  fd.append('url', url);
  fd.append('pwd', pwd);
  fetch('/lanzou-test.php?ajax=parse', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) {
        box.innerHTML = '<div class="sres"><div class="sn" style="color:#fda4af">解析失败：' + escHtml(j.msg || '') + '</div></div>';
        return;
      }
      shareData = j.data || {};
      renderShare();
    })
    .catch(e => { box.innerHTML = '<div class="sres"><div class="sn" style="color:#fda4af">请求失败</div></div>'; });
}

function renderShare() {
  const box = document.getElementById('shareResult');
  const kw = document.getElementById('srSearch').value.trim().toLowerCase();
  if (!shareData) { box.innerHTML = ''; document.getElementById('shareEmpty').style.display = 'block'; return; }
  document.getElementById('shareEmpty').style.display = 'none';
  if (shareData.type === 'file') {
    window.sfSrc = window._shareSrc || '';
    window.sfPwd = window._sharePwd || '';
    window.sfDirect = shareData.direct_url || '';
    box.innerHTML = '<div class="sres"><div class="sn">' + escHtml(shareData.filename || '文件') + '</div>' +
      '<div class="sa">' +
      '<button type="button" style="background:linear-gradient(135deg,#2563eb,#3b82f6)" onclick="openSaveSel(sfSrc,sfPwd,false)">转存</button>' +
      '<button type="button" style="background:linear-gradient(135deg,#16a34a,#22c55e)" onclick="openUrl(sfDirect)">下载</button>' +
      '</div></div>';
    return;
  }
  if (shareData.type === 'folder') {
    const files = (shareData.files || []).filter(f => !kw || (f.name || '').toLowerCase().includes(kw));
    const src = window._shareSrc || '';
    const pwd = window._sharePwd || '';
    let html = '<div class="sres"><div class="sn">文件夹：' + escHtml(shareData.folder_name || '') + '（' + (shareData.files || []).length + ' 个文件）</div>' +
      '<div class="sa"><button type="button" style="background:linear-gradient(135deg,#2563eb,#3b82f6)" onclick="openSaveSel(sfSrc2,sfPwd2,true)">全部转存</button>' +
      '<button type="button" style="background:#334155" onclick="copyText(sfSrc2)">复制链接</button></div></div>';
    window.sfSrc2 = src;
    window.sfPwd2 = pwd;
    if (files.length === 0) { html += '<div class="empty">无匹配文件</div>'; }
    files.forEach(function(f) {
      const fUrl = f.url || '';
      const fPwd = pwd || '001';
      const fName = f.name || '';
      window['fUrl_' + fName.replace(/[^a-zA-Z0-9]/g, '_')] = fUrl;
      window['fPwd_' + fName.replace(/[^a-zA-Z0-9]/g, '_')] = fPwd;
      const key = 'fUrl_' + fName.replace(/[^a-zA-Z0-9]/g, '_');
      const keyP = 'fPwd_' + fName.replace(/[^a-zA-Z0-9]/g, '_');
      html += '<div class="sres"><div class="sn">' + escHtml(fName) + '</div><div class="sm">' + escHtml(f.size || '') + ' · ' + escHtml(f.time || '') + '</div>' +
        '<div class="sa">' +
        '<button type="button" style="background:linear-gradient(135deg,#2563eb,#3b82f6)" onclick="openSaveSel(window.' + key + ',window.' + keyP + ',false)">转存</button>' +
        '<button type="button" style="background:linear-gradient(135deg,#16a34a,#22c55e)" onclick="parseAndOpen(window.' + key + ',window.' + keyP + ')">下载</button>' +
        '</div></div>';
    });
    box.innerHTML = html;
  }
}
document.getElementById('srSearch').addEventListener('input', renderShare);
function clearShareResult() { shareData = null; document.getElementById('shareResult').innerHTML = ''; document.getElementById('shareEmpty').style.display = 'block'; document.getElementById('srSearch').value = ''; }

/* ============ 转存（选目标文件夹） ============ */
let saveSel = { url: '', pwd: '', isFolder: false, cur: '0', path: ['根目录'] };
function openSaveSel(url, pwd, isFolder) {
  saveSel = { url: url || '', pwd: pwd || '', isFolder: !!isFolder, cur: '0', path: ['根目录'] };
  const dlg = document.getElementById('dlgBox');
  dlg.innerHTML = '<h3>选择转存位置<button type="button" class="x" onclick="closeDlg()" aria-label="关闭">&times;</button></h3>' +
    '<div class="foldersel" id="selBody" style="max-height:40vh;overflow:auto;overscroll-behavior:contain"><div style="text-align:center;color:#64748b;padding:14px"><span class="spin"></span> 加载中…</div></div>' +
    '<div class="selbar"><button type="button" class="btn green full" onclick="doSave()">转存到此文件夹</button></div>';
  document.getElementById('dlgMask').classList.add('on');
  renderSel();
}
function renderSel() {
  const body = document.getElementById('selBody');
  if (!body) return;
  body.innerHTML = '<div style="text-align:center;color:#64748b;padding:14px"><span class="spin"></span> 加载中…</div>';
  fetch('/lanzou-test.php?ajax=list&folder=' + encodeURIComponent(saveSel.cur))
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { body.innerHTML = '<div style="color:#fda4af;padding:10px">加载失败</div>'; return; }
      const list = (j.data && j.data.list) || [];
      let html = '<div style="font-size:12px;color:#94a3b8;padding:8px 2px">当前：' + escHtml(saveSel.path.join(' / ')) + '</div>';
      const dirs = list.filter(x => x.is_dir || x.type === 'folder');
      if (saveSel.cur !== '0') {
        html += '<button type="button" class="fit" onclick="selUp()"><span class="ic dir">' + folderIco() + '</span><div class="fname"><span class="nm">.. 返回上级</span></div></button>';
      }
      if (dirs.length === 0) html += '<div style="color:#64748b;padding:10px;font-size:13px">此位置没有子文件夹（可直接转存到当前文件夹）</div>';
      dirs.forEach(function(d) {
        const dId = d.id || '';
        const dName = d.name_all || d.name || '';
        const safeKey = 'seld_' + (dName + '_' + dId).replace(/[^a-zA-Z0-9]/g, '_');
        window[safeKey] = { id: dId, name: dName };
        html += '<button type="button" class="fit" onclick="selInto(window.' + safeKey + ')"><span class="ic dir">' + folderIco() + '</span><div class="fname"><span class="nm">' + escHtml(dName) + '</span></div></button>';
      });
      body.innerHTML = html;
    });
}
function selInto(o) { saveSel.cur = o.id; saveSel.path.push(o.name); renderSel(); }
function selUp() {
  const curNum = Math.abs(parseInt(saveSel.cur, 10) || 0);
  if (saveSel.cur === '0') return;
  if (curNum === 0) { saveSel.cur = '0'; saveSel.path = ['根目录']; renderSel(); return; }
  /* 从 -12345 推到上一级：重新从根目录加载一层（简化：回根目录） */
  saveSel.cur = '0';
  saveSel.path = ['根目录'];
  renderSel();
}
function doSave() {
  const fd = new FormData();
  fd.append('url', saveSel.url);
  fd.append('pwd', saveSel.pwd);
  fd.append('folder', saveSel.cur);
  const btn = event.target;
  btn.disabled = true;
  btn.innerHTML = '<span class="spin"></span> 转存中…';
  fetch('/lanzou-test.php?ajax=save', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      btn.disabled = false;
      btn.textContent = '转存到此文件夹';
      if (j.code === 0) {
        const d = j.data || {};
        let m;
        if (d.mode === 'folder') m = '转存完成：成功 ' + d.ok + ' / ' + d.total + ' 个文件';
        else {
          const saved = (d.saved && d.saved[0]) || {};
          m = '转存成功：' + (saved.name || '');
        }
        toast(m, 'ok');
        closeDlg();
      } else {
        toast('转存失败：' + (j.msg || '') + (j.detail ? ' ' + j.detail : ''), 'err');
      }
    })
    .catch(e => { btn.disabled = false; btn.textContent = '转存到此文件夹'; toast('请求失败', 'err'); });
}

/* ============ 解析并下载（同源 dl 代理，hidden iframe 触发下载，不跳转） ============ */
function downloadViaProxy(u, name) {
  if (!u) { toast('无下载链接', 'err'); return; }
  const ifr = document.createElement('iframe');
  ifr.style.display = 'none';
  ifr.setAttribute('aria-hidden', 'true');
  ifr.src = '/lanzou-test.php?dl=1&u=' + encodeURIComponent(u) + '&name=' + encodeURIComponent(name || 'download');
  document.body.appendChild(ifr);
  toast('正在获取下载…', 'ok');
  setTimeout(() => { if (ifr.parentNode) ifr.parentNode.removeChild(ifr); }, 120000);
}
function parseAndOpen(url, pwd) {
  if (!url) { toast('无链接', 'err'); return; }
  toast('解析直链中...');
  const fd = new FormData();
  fd.append('url', url);
  fd.append('pwd', pwd || '');
  fetch('/lanzou-test.php?ajax=parse', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { toast('解析失败：' + (j.msg || ''), 'err'); return; }
      const d = j.data || {};
      if (d.direct_url) downloadViaProxy(d.direct_url, d.filename || 'download');
      else toast('未获取到直链', 'err');
    })
    .catch(e => toast('请求失败', 'err'));
}
function openUrl(u) { downloadViaProxy(u); }

/* ============ 设置 ============ */
function loadStatus() {
  fetch('/lanzou-test.php?ajax=status')
    .then(r => r.json())
    .then(j => {
      const d = j.data || {};
      const box = document.getElementById('statusBox');
      if (j.code !== 0 || !d.valid) {
        box.innerHTML = '状态：<span style="color:#fda4af">Cookie 无效</span><br />原因：' + escHtml(j.msg || d.detail || '未知');
        return;
      }
      let html = '状态：<span style="color:#86efac">有效</span>';
      if (d.uid) html += '<br />UID：' + escHtml(d.uid);
      if (d.updated_at) html += '<br />更新时间：' + escHtml(d.updated_at);
      if (d.cookie_preview) html += '<br />Cookie 摘要：' + escHtml(d.cookie_preview);
      box.innerHTML = html;
    })
    .catch(e => document.getElementById('statusBox').textContent = '获取状态失败');
}
function saveCookie() {
  const ck = document.getElementById('cookieInput').value.trim();
  if (!ck) { toast('请粘贴 Cookie', 'err'); return; }
  toast('检测中，请稍候…');
  const fd = new FormData();
  fd.append('cookie', ck);
  fetch('/lanzou-test.php?ajax=cookie', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code === 0) {
        toast('Cookie 有效，已保存并生效', 'ok');
        loadStatus();
      } else {
        toast('Cookie 无效：' + (j.msg || j.detail || '检测失败') + '（已保留之前的 Cookie）', 'err');
        loadStatus();
      }
    })
    .catch(e => toast('请求失败', 'err'));
}

/* ============ 工具 ============ */
function closeDlg() { document.getElementById('dlgMask').classList.remove('on'); }
function copyText(t) {
  if (!t) return;
  const ta = document.createElement('textarea');
  ta.value = t;
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); toast('已复制', 'ok'); } catch (e) { toast('复制失败', 'err'); }
  ta.remove();
}
function escId(s) { return String(s).replace(/[^a-zA-Z0-9\-]/g, ''); }
function escHtml(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;'); }
function escJs(s) { return String(s).replace(/\\/g, '\\\\').replace(/'/g, '\\\''); }

/* 初始加载：支持 ?tab= 深链接 */
(function() {
  const m = location.search.match(/[?&]tab=([a-z]+)/i);
  if (m && ['files', 'share', 'settings'].indexOf(m[1]) >= 0) { switchTab(m[1]); } else { loadList('0'); }
})();
</script>
</body>
</html>
