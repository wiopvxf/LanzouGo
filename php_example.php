<?php
/**
 * LanzouGo PHP 调用示例（原生PHP，无框架，无curl依赖）
 * 使用方法：修改 $API 与 $COOKIE，放到任意PHP环境直接运行
 * 依赖：PHP 已开启 allow_url_fopen（默认开启）
 */

$API   = 'http://127.0.0.1:8080';                    // Go服务地址
$COOKIE = 'phpdisk_info=你的蓝奏云Cookie';            // 蓝奏云Cookie

// ---------- HTTP 工具（GET / POST，返回解码后的JSON数组） ----------
function api_call($url, $post = null) {
    $opts = ['http' => [
        'method'  => $post === null ? 'GET' : 'POST',
        'timeout' => 30,
        'header'  => "Content-Type: application/x-www-form-urlencoded\r\n",
        'content' => $post === null ? '' : http_build_query($post),
    ]];
    $ctx = stream_context_create($opts);
    $json = @file_get_contents($url, false, $ctx);
    if ($json === false) return ['code' => -1, 'msg' => '无法连接Go服务，请确认已启动'];
    return json_decode($json, true);
}

echo "========== ① 写入Cookie（自动检测，无效返回 Cookie异常） ==========\n";
$r = api_call("$API/api/cookie", ['cookie' => $COOKIE]);
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";
if (($r['code'] ?? '') === 401) { echo "→ Cookie异常，流程终止\n"; exit; }

echo "\n========== ② 获取文件列表（根目录） ==========\n";
$r = api_call("$API/api/list?folder=0");
if (($r['code'] ?? '') === 401) { echo "→ Cookie异常\n"; exit; }
echo "文件总数: ", $r['data']['count'] ?? 0, "\n";
foreach (($r['data']['list'] ?? []) as $f) {
    $type = ($f['dir_type'] ?? '') == 1 ? '[文件夹]' : '[文件]';
    echo "  $type ", $f['name'] ?? '', "  (id=", $f['id'] ?? '', ")\n";
}

echo "\n========== ③ 上传文件（服务器本地路径，自动生成分享链接） ==========\n";
$r = api_call("$API/api/upload", ['path' => '/tmp/test.zip', 'folder' => '0']);
if (($r['code'] ?? '') === 401) { echo "→ Cookie异常\n"; exit; }
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";
$shareUrl = $r['data']['share_url'] ?? '';

echo "\n========== ④ 解析分享链接为直链 ==========\n";
$r = api_call("$API/api/parse?url=" . urlencode($shareUrl ?: 'https://wwi.lanzou.com/XXXX') . "&pwd=");
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";

echo "\n完成\n";
