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
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0, user-scalable=no" />
<title>LanzouGo 面板</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Crect width='24' height='24' rx='5' fill='%232563eb'/%3E%3Ctext x='12' y='17' font-size='12' font-family='monospace' font-weight='bold' text-anchor='middle' fill='%23fff'%3ELZ%3C/text%3E%3C/svg%3E" />
<style>
*{margin:0;padding:0;box-sizing:border-box;-webkit-tap-highlight-color:transparent}
:root{--bg:#0b0d12;--card:#14161d;--line:#222634;--txt:#e6e8ee;--sub:#8a90a3;--blue:#3b82f6;--green:#16a34a;--red:#dc2626}
body{background:var(--bg);color:var(--txt);font-family:system-ui,-apple-system,'PingFang SC','Microsoft YaHei',sans-serif;min-height:100vh;padding-bottom:70px}
.wrap{max-width:640px;margin:0 auto;padding:14px 12px 20px}
.hd{display:flex;align-items:center;gap:8px;padding:4px 2px 12px}
.hd .logo{width:28px;height:28px;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#1d4ed8);display:flex;align-items:center;justify-content:center;font-size:12px;font-weight:800;color:#fff}
.hd h1{font-size:18px;font-weight:700;letter-spacing:.5px}
.searchbar{display:flex;gap:8px;margin-bottom:10px}
.searchbar input{flex:1;background:#0d0f16;border:1px solid var(--line);color:var(--txt);border-radius:10px;padding:11px 13px;font-size:14px;outline:none}
.searchbar input:focus{border-color:var(--blue)}
.searchbar button{background:#1e293b;border:none;color:#cbd5e1;border-radius:10px;padding:0 14px;font-size:13px;cursor:pointer}
.toolrow{display:flex;gap:8px;margin-bottom:12px}
.toolrow button{flex:1;display:flex;align-items:center;justify-content:center;gap:6px;padding:12px;border:none;border-radius:10px;font-size:14px;cursor:pointer;color:#fff}
.btn-dir{background:linear-gradient(135deg,#4f46e5,#4338ca)}
.btn-up{background:linear-gradient(135deg,#16a34a,#15803d)}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px;margin-bottom:12px}
.card .t{font-size:13px;font-weight:600;color:#9aa0b4;margin-bottom:10px}
table{width:100%;border-collapse:collapse;font-size:14px}
th{text-align:left;color:var(--sub);font-weight:500;font-size:12px;padding:6px 8px;border-bottom:1px solid var(--line)}
td{padding:10px 8px;border-bottom:1px solid #1a1e2a;vertical-align:middle}
tr:last-child td{border-bottom:none}
.fname{display:flex;align-items:center;gap:8px}
.fname .ic{width:30px;height:30px;border-radius:8px;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.ic.dir{background:#1c2a4a;color:#60a5fa}
.ic.file{background:#1a2a22;color:#4ade80}
.nm{word-break:break-all;line-height:1.35}
.nm .nm-click{cursor:pointer}
.nm .nm-click:hover{color:#60a5fa}
.fmeta{font-size:11px;color:#6b7280;margin-top:2px}
.op{display:flex;gap:5px;justify-content:flex-end;flex-wrap:wrap}
.op button{padding:5px 8px;font-size:12px;border-radius:6px;border:none;cursor:pointer;color:#fff}
.op .b-info{background:#2563eb}
.op .b-del{background:#b91c1c}
.op .b-dl{background:#15803d}
.empty{padding:24px 0;text-align:center;color:#6b7280;font-size:13px}
.inrow{display:flex;gap:8px;margin-bottom:8px}
.inrow input{flex:1;min-width:0;background:#0d0f16;border:1px solid var(--line);color:var(--txt);border-radius:10px;padding:11px 12px;font-size:14px;outline:none}
.inrow input:focus{border-color:var(--blue)}
.inrow button{white-space:nowrap;background:var(--blue);border:none;color:#fff;border-radius:10px;padding:0 16px;font-size:14px;cursor:pointer}
.btn{display:inline-block;background:#2563eb;color:#fff;border:none;border-radius:10px;padding:11px 18px;font-size:14px;cursor:pointer}
.btn.green{background:var(--green)}
.btn.red{background:var(--red)}
.btn.gray{background:#334155}
.btn.full{width:100%}
.toast{position:fixed;top:14px;left:50%;transform:translateX(-50%);z-index:200;background:#1a2233;border:1px solid #2c3a66;color:#8fb0ff;padding:11px 16px;border-radius:10px;font-size:13px;max-width:92%;word-break:break-all;display:none;box-shadow:0 6px 24px rgba(0,0,0,.5)}
.toast.err{background:#2a1818;border-color:#5a2424;color:#fca5a5}
.toast.ok{background:#132a1a;border-color:#1e4d2c;color:#86efac}
.mask{display:none;position:fixed;inset:0;background:rgba(0,0,0,.72);z-index:99;align-items:center;justify-content:center;padding:16px}
.mask.on{display:flex}
.dialog{background:var(--card);border:1px solid #2a3040;border-radius:14px;padding:18px;width:100%;max-width:430px;max-height:86vh;overflow:auto}
.dialog h3{font-size:16px;margin-bottom:12px;display:flex;align-items:center;gap:8px}
.dialog h3 .x{margin-left:auto;cursor:pointer;color:var(--sub);font-size:20px;padding:0 4px}
.dline{margin:8px 0;font-size:13px;color:var(--sub)}
.dline b{color:var(--txt);font-weight:600}
.dval{background:#0d0f16;border:1px solid var(--line);border-radius:8px;padding:8px 10px;font-size:12px;color:#8fb0ff;word-break:break-all;margin:6px 0}
.dacts{display:flex;gap:8px;margin-top:14px;flex-wrap:wrap}
.dacts button{flex:1;min-width:86px;border:none;border-radius:9px;padding:10px;font-size:13px;color:#fff;cursor:pointer}
.inrow.small{margin:6px 0}
.inrow.small input{padding:8px 10px;font-size:13px}
.upmask{position:fixed;inset:0;background:rgba(0,0,0,.78);z-index:150;display:none;align-items:center;justify-content:center;padding:20px}
.upmask.on{display:flex}
.upbox{background:var(--card);border:1px solid #2a3040;border-radius:14px;padding:22px;width:100%;max-width:340px;text-align:center}
.upbox .fname{font-size:14px;word-break:break-all;margin-bottom:12px}
.upbox .upbar{height:10px;background:#0d0f16;border-radius:6px;overflow:hidden;margin:10px 0}
.upbox .upbar i{display:block;height:100%;width:0;background:linear-gradient(90deg,#3b82f6,#22d3ee);border-radius:6px;transition:width .15s}
.upbox .pct{font-size:15px;font-weight:700;color:#60a5fa;margin-bottom:14px}
.upbox .cancel{background:#334155;border:none;color:#e2e8f0;border-radius:9px;padding:9px 20px;font-size:13px;cursor:pointer}
.tabbar{position:fixed;bottom:0;left:0;right:0;background:#10131b;border-top:1px solid var(--line);z-index:50;display:flex}
.tabbar .tb{flex:1;padding:9px 0 10px;display:flex;flex-direction:column;align-items:center;gap:2px;color:#7c8296;font-size:11px;cursor:pointer;user-select:none}
.tabbar .tb svg{width:21px;height:21px}
.tabbar .tb.on{color:#3b82f6}
.view{display:none}
.view.on{display:block}
.sres{border:1px solid #1a2a22;background:#0d0f16;border-radius:10px;padding:12px;margin-bottom:8px}
.sres .sn{font-size:14px;word-break:break-all;margin-bottom:4px}
.sres .sm{font-size:12px;color:#6b7280}
.sres .sa{display:flex;gap:8px;margin-top:8px}
.sres .sa button{flex:1;border:none;border-radius:8px;padding:8px;font-size:12px;color:#fff;cursor:pointer}
.foldersel .fit{display:flex;align-items:center;gap:8px;padding:10px;border-bottom:1px solid #1a1e2a;cursor:pointer}
.foldersel .fit:hover{background:#1a2233}
.foldersel .fit .fname{flex:1}
.selbar{position:sticky;bottom:0;background:var(--card);padding-top:10px}
.spin{display:inline-block;width:14px;height:14px;border:2px solid rgba(255,255,255,.25);border-top-color:#fff;border-radius:50%;animation:sp .8s linear infinite;vertical-align:middle}
@keyframes sp{to{transform:rotate(360deg)}}
@media (max-width:400px){.op button{font-size:11px;padding:4px 6px}}
</style>
</head>
<body>

<div class="toast" id="toast"></div>

<div class="wrap">
  <div class="hd">
    <div class="logo">LZ</div>
    <h1 id="pageTitle">文件列表</h1>
  </div>

  <!-- ========== Tab1 文件列表 ========== -->
  <div class="view on" id="view-files">
    <div class="searchbar">
      <input type="text" id="fsSearch" placeholder="搜索当前文件夹的文件..." />
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
      <input type="file" id="fileInput" style="display:none" />
    </div>
    <div class="card">
      <div class="t" id="listTitle">根目录</div>
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
      <div class="t">解析分享链接（支持文件 / 文件夹）</div>
      <div class="inrow"><input type="text" id="shareUrl" placeholder="蓝奏分享链接" /></div>
      <div class="inrow"><input type="text" id="sharePwd" placeholder="提取码（无密码留空）" /></div>
      <div class="inrow"><button style="flex:1" onclick="doParse()">解析</button></div>
    </div>
    <div class="card">
      <div class="t">解析结果 <button class="btn gray" style="float:right;padding:3px 10px;font-size:11px" onclick="clearShareResult()">清空</button></div>
      <div class="searchbar">
        <input type="text" id="srSearch" placeholder="搜索解析结果..." />
      </div>
      <div id="shareResult"></div>
      <div class="empty" id="shareEmpty" style="display:none">暂无结果，先在上方输入分享链接解析</div>
    </div>
  </div>

  <!-- ========== Tab3 设置 ========== -->
  <div class="view" id="view-settings">
    <div class="card">
      <div class="t">API 服务</div>
      <div class="dval" id="apiBox" style="font-size:12px;line-height:1.9">加载中...</div>
    </div>
    <div class="card">
      <div class="t">当前 Cookie 状态</div>
      <div id="statusBox" class="dval">加载中...</div>
    </div>
    <div class="card">
      <div class="t">配置 Cookie（自动检测可用性，有效才保存；失效则沿用之前的）</div>
      <textarea id="cookieInput" rows="4" style="width:100%;background:#0d0f16;border:1px solid var(--line);color:var(--txt);border-radius:10px;padding:10px;font-size:12px;outline:none;resize:vertical" placeholder="粘贴完整 Cookie，例如：phpdisk_info=xxx; PHPSESSID=xxx; ..."></textarea>
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
  <div class="tb on" data-tab="files" onclick="switchTab('files')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
    文件列表
  </div>
  <div class="tb" data-tab="share" onclick="switchTab('share')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.6" y1="13.5" x2="15.4" y2="17.5"/><line x1="15.4" y1="6.5" x2="8.6" y2="10.5"/></svg>
    分享
  </div>
  <div class="tb" data-tab="settings" onclick="switchTab('settings')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    设置
  </div>
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
  document.getElementById('listBody').innerHTML = '<tr><td colspan="2" style="text-align:center;color:#6b7280"><span class="spin"></span> 加载中...</td></tr>';
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
      html += '<tr><td><div class="fname"><span class="ic dir">' + folderIco() + '</span><span class="nm"><span class="nm-click" onclick="openFolder(\'' + escId(it.id) + '\')">' + escHtml(name) + '</span><div class="fmeta">文件夹</div></span></div></td>' +
        '<td><div class="op"><button class="b-info" onclick="openDirDetail(\'' + escId(it.id) + '\',\'' + escJs(name) + '\')">详情</button><button class="b-del" onclick="delItem(\'' + escId(it.id) + '\',\'folder\')">删除</button></div></td></tr>';
    } else {
      const meta = [it.size, it.time].filter(Boolean).join(' · ');
      html += '<tr><td><div class="fname"><span class="ic file">' + fileIco() + '</span><span class="nm"><span class="nm-click" onclick="openFileDetail(\'' + escId(it.id) + '\',\'' + escJs(name) + '\')">' + escHtml(name) + '</span><div class="fmeta">' + escHtml(meta) + (it.onof && it.onof !== '0' ? ' · <span style="color:#eab308">有提取码</span>' : '') + '</div></span></div></td>' +
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
    '<h3>创建文件夹<span class="x" onclick="closeDlg()">&times;</span></h3>' +
    '<div class="inrow"><input type="text" id="mkdirName" placeholder="文件夹名称" /></div>' +
    '<div class="dacts"><button style="background:#2563eb" onclick="doMkdir()">创建</button><button class="btn gray" style="flex:1" onclick="closeDlg()">取消</button></div>';
  document.getElementById('dlgMask').classList.add('on');
  setTimeout(() => document.getElementById('mkdirName').focus(), 100);
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
  dlg.innerHTML = '<h3>文件详情<span class="x" onclick="closeDlg()">&times;</span></h3>' +
    '<div class="dline">名称：<b>' + escHtml(nameEnc) + '</b></div>' +
    '<div class="dline">提取码：<b id="dPwd">加载中...</b></div>' +
    '<div class="dline">分享链接</div><div class="dval" id="dUrl">加载中...</div>' +
    '<div class="dline">更改提取码</div><div class="inrow small"><input type="text" id="dNewPwd" placeholder="新提取码" /><button style="font-size:12px;padding:0 10px" onclick="changePwd(winDlgId,\'file\')">设置</button></div>' +
    '<div class="dline">重命名</div><div class="inrow small"><input type="text" id="dNewName" placeholder="新文件名" /><button style="font-size:12px;padding:0 10px" onclick="doRename(winDlgId,winDlgName)">重命名</button></div>' +
    '<div class="dacts">' +
    '<button style="background:#15803d" onclick="dlFromDlg()">下载</button>' +
    '<button style="background:#1e293b" onclick="copyShare()">复制链接</button>' +
    '<button style="background:#b91c1c" onclick="delDlg()">删除</button>' +
    '</div><div class="dline" id="dErr" style="color:#fca5a5"></div>';
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
  dlg.innerHTML = '<h3>文件夹详情<span class="x" onclick="closeDlg()">&times;</span></h3>' +
    '<div class="dline">名称：<b>' + escHtml(nameEnc) + '</b></div>' +
    '<div class="dline">提取码：<b id="dPwd">加载中...</b></div>' +
    '<div class="dline">分享链接</div><div class="dval" id="dUrl">加载中...</div>' +
    '<div class="dline">更改提取码</div><div class="inrow small"><input type="text" id="dNewPwd" placeholder="新提取码" /><button style="font-size:12px;padding:0 10px" onclick="changePwd(winDlgId,\'folder\')">设置</button></div>' +
    '<div class="dacts">' +
    '<button style="background:#1e293b" onclick="copyShare()">复制链接</button>' +
    '<button style="background:#b91c1c" onclick="delDlgDir(winDlgId)">删除</button>' +
    '</div><div class="dline" id="dErr" style="color:#fca5a5"></div>';
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
  box.innerHTML = '<div style="text-align:center;color:#6b7280"><span class="spin"></span> 解析中...</div>';
  document.getElementById('shareEmpty').style.display = 'none';
  const fd = new FormData();
  fd.append('url', url);
  fd.append('pwd', pwd);
  fetch('/lanzou-test.php?ajax=parse', { method: 'POST', body: fd })
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) {
        box.innerHTML = '<div class="sres"><div class="sn" style="color:#fca5a5">解析失败：' + escHtml(j.detail || j.msg || '') + '</div></div>';
        return;
      }
      shareData = j.data || {};
      renderShare();
    })
    .catch(e => { box.innerHTML = '<div class="sres"><div class="sn" style="color:#fca5a5">请求失败</div></div>'; });
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
      '<button style="background:#2563eb" onclick="openSaveSel(sfSrc,sfPwd,false)">转存</button>' +
      '<button style="background:#15803d" onclick="openUrl(sfDirect)">下载</button>' +
      '</div></div>';
    return;
  }
  if (shareData.type === 'folder') {
    const files = (shareData.files || []).filter(f => !kw || (f.name || '').toLowerCase().includes(kw));
    const src = window._shareSrc || '';
    const pwd = window._sharePwd || '';
    let html = '<div class="sres"><div class="sn">文件夹：' + escHtml(shareData.folder_name || '') + '（' + (shareData.files || []).length + ' 个文件）</div>' +
      '<div class="sa"><button style="background:#2563eb" onclick="openSaveSel(sfSrc2,sfPwd2,true)">全部转存</button>' +
      '<button style="background:#334155" onclick="copyText(sfSrc2)">复制链接</button></div></div>';
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
        '<button style="background:#2563eb" onclick="openSaveSel(window.' + key + ',window.' + keyP + ',false)">转存</button>' +
        '<button style="background:#15803d" onclick="parseAndOpen(window.' + key + ',window.' + keyP + ')">下载</button>' +
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
  dlg.innerHTML = '<h3>选择转存位置<span class="x" onclick="closeDlg()">&times;</span></h3>' +
    '<div class="foldersel" id="selBody" style="max-height:40vh;overflow:auto"><div style="text-align:center;color:#6b7280;padding:14px"><span class="spin"></span> 加载中...</div></div>' +
    '<div class="selbar"><button class="btn green full" onclick="doSave()">转存到此文件夹</button></div>';
  document.getElementById('dlgMask').classList.add('on');
  renderSel();
}
function renderSel() {
  const body = document.getElementById('selBody');
  if (!body) return;
  body.innerHTML = '<div style="text-align:center;color:#6b7280;padding:14px"><span class="spin"></span> 加载中...</div>';
  fetch('/lanzou-test.php?ajax=list&folder=' + encodeURIComponent(saveSel.cur))
    .then(r => r.json())
    .then(j => {
      if (j.code !== 0) { body.innerHTML = '<div style="color:#fca5a5;padding:10px">加载失败</div>'; return; }
      const list = (j.data && j.data.list) || [];
      let html = '<div style="font-size:12px;color:#9aa0b4;padding:8px 2px">当前：' + escHtml(saveSel.path.join(' / ')) + '</div>';
      const dirs = list.filter(x => x.is_dir || x.type === 'folder');
      if (saveSel.cur !== '0') {
        html += '<div class="fit" onclick="selUp()"><span class="ic dir">' + folderIco() + '</span><div class="fname"><span class="nm">.. 返回上级</span></div></div>';
      }
      if (dirs.length === 0) html += '<div style="color:#6b7280;padding:10px;font-size:13px">此位置没有子文件夹（可直接转存到当前文件夹）</div>';
      dirs.forEach(function(d) {
        const dId = d.id || '';
        const dName = d.name_all || d.name || '';
        const safeKey = 'seld_' + (dName + '_' + dId).replace(/[^a-zA-Z0-9]/g, '_');
        window[safeKey] = { id: dId, name: dName };
        html += '<div class="fit" onclick="selInto(window.' + safeKey + ')"><span class="ic dir">' + folderIco() + '</span><div class="fname"><span class="nm">' + escHtml(dName) + '</span></div></div>';
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
  btn.innerHTML = '<span class="spin"></span> 转存中...';
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

/* ============ 解析并打开直链 ============ */
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
      if (d.direct_url) openUrl(d.direct_url);
      else toast('未获取到直链', 'err');
    })
    .catch(e => toast('请求失败', 'err'));
}
/* 页内直接打开/下载：隐藏 iframe 加载（自动过蓝奏 acw 反爬后触发下载），不跳转新页面 */
function openUrl(u) {
  if (!u) { toast('无链接', 'err'); return; }
  let f = document.getElementById('dlFrame');
  if (!f) {
    f = document.createElement('iframe');
    f.id = 'dlFrame';
    f.style.display = 'none';
    f.style.position = 'fixed';
    f.style.width = '1px';
    f.style.height = '1px';
    f.style.opacity = '0';
    document.body.appendChild(f);
  }
  f.src = u;
  toast('已在当前页面打开/开始下载');
}

/* ============ 设置 ============ */
function loadStatus() {
  fetch('/lanzou-test.php?ajax=status')
    .then(r => r.json())
    .then(j => {
      const d = j.data || {};
      const box = document.getElementById('statusBox');
      const abox = document.getElementById('apiBox');
      if (abox) {
        abox.innerHTML = '服务：LanzouGo（Go 原生后端）<br />接口：/api/*<br />状态：' + (j.code === 0 ? '<span style="color:#86efac">在线</span>' : '<span style="color:#fca5a5">异常</span>') +
          '<br />完整接口文档：<a href="javascript:void(0)" onclick="showApiHelp()" style="color:#93c5fd">查看 API 说明</a>';
      }
      if (j.code !== 0 || !d.valid) {
        box.innerHTML = '状态：<span style="color:#fca5a5">Cookie 无效</span><br />原因：' + escHtml(j.msg || d.detail || '未知');
        return;
      }
      let html = '状态：<span style="color:#86efac">有效</span>';
      if (d.uid) html += '<br />UID：' + escHtml(d.uid);
      if (d.updated_at) html += '<br />更新时间：' + escHtml(d.updated_at);
      if (d.cookie_preview) html += '<br />Cookie 摘要：' + escHtml(d.cookie_preview);
      box.innerHTML = html;
    })
    .catch(e => {
      document.getElementById('statusBox').textContent = '获取状态失败';
      const abox = document.getElementById('apiBox');
      if (abox) abox.innerHTML = '服务：LanzouGo<br />状态：<span style="color:#fca5a5">无法连接</span>';
    });
}
/* API 说明弹窗 */
function showApiHelp() {
  const dlg = document.getElementById('dlgBox');
  dlg.innerHTML = '<h3>API 接口说明<span class="x" onclick="closeDlg()">&times;</span></h3>' +
    '<div class="dval" style="font-size:12px;line-height:2;text-align:left;max-height:60vh;overflow:auto">' +
    'POST /api/cookie — 写入并检测 Cookie<br />' +
    'GET  /api/status — Cookie 状态<br />' +
    'GET  /api/list?folder=0 — 文件列表（0=根目录，子文件夹用正值 ID）<br />' +
    'POST /api/upload（path=本地路径,folder=目录）— 上传<br />' +
    'POST /api/mkdir（name,parent）— 创建文件夹<br />' +
    'POST /api/share（id,type,pwd 可选）— 分享信息 / 设置提取码<br />' +
    'GET  /api/parse?url=分享链接&pwd=密码 — 解析直链（文件/文件夹）<br />' +
    'POST /api/rename（id,newname,folder）— 重命名<br />' +
    'POST /api/save（url,pwd,folder）— 转存分享<br />' +
    'POST /api/delete（id,type）— 删除<br />' +
    'GET  /api/help — 完整接口文档</div>';
  document.getElementById('dlgMask').classList.add('on');
}
function saveCookie() {
  const ck = document.getElementById('cookieInput').value.trim();
  if (!ck) { toast('请粘贴 Cookie', 'err'); return; }
  toast('检测中，请稍候...');
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

/* 初始加载 */
loadList('0');
</script>
</body>
</html>
