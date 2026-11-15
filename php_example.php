<?php
/**
 * LanzouGo PHP 调用示例（原生 PHP，无框架、无 curl 依赖）
 * 配合 LanzouGo v1.2：账号密码登录 → 自动维护 Cookie → VIP 自动检测 → 失效服务端自动重登
 * 用法：修改 $API / $USER / $PASS，放到任意 PHP 环境直接运行
 * 依赖：PHP 已开启 allow_url_fopen（默认开启）
 */

$API  = 'http://127.0.0.1:8080';   // Go 服务地址
$USER = '你的蓝奏云账号';           // 手机号 / 邮箱
$PASS = '你的密码';                 // 仅发给本机 Go 服务，不落任何文件

// ---------- HTTP 工具（GET / POST，返回解码后的 JSON 数组） ----------
function api_call($url, $post = null, $timeout = 60)
{
    $opts = ['http' => [
        'method'  => $post === null ? 'GET' : 'POST',
        'timeout' => $timeout,
        'header'  => "Content-Type: application/x-www-form-urlencoded\r\n",
        'content' => $post === null ? '' : http_build_query($post),
    ]];
    $ctx = stream_context_create($opts);
    $json = @file_get_contents($url, false, $ctx);
    if ($json === false) return ['code' => -1, 'msg' => '无法连接 Go 服务，请确认已启动'];
    $d = json_decode($json, true);
    return is_array($d) ? $d : ['code' => -1, 'msg' => '响应异常'];
}

/** 统一处理 401：服务端已明确提示「账号登录失败 / Cookie 失效」 */
function need_login($r, $step)
{
    if (($r['code'] ?? 0) === 401) {
        echo "→ [$step] ", ($r['msg'] ?? '账号登录失败'), "\n";
        if (!empty($r['detail'])) echo "  原因：", $r['detail'], "\n";
        return true;
    }
    return false;
}

echo "========== ① 账号登录（登录后自动获取并维护 Cookie） ==========\n";
$r = api_call("$API/api/login", ['username' => $USER, 'password' => $PASS], 120);
if (($r['code'] ?? 0) === 401) {
    echo "→ 账号登录失败：", ($r['detail'] ?? ($r['msg'] ?? '未知原因')), "\n流程终止\n";
    exit;
}
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";

echo "\n========== ② 账号状态（不含 Cookie，只给摘要） ==========\n";
$r = api_call("$API/api/status");
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";

echo "\n========== ③ 上传上限 ==========\n";
$r = api_call("$API/api/limits");
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";

echo "\n========== ④ 文件列表（根目录） ==========\n";
$r = api_call("$API/api/list?folder=0");
if (need_login($r, '文件列表')) exit;
echo "条目总数：", ($r['data']['count'] ?? 0), "\n";
foreach (($r['data']['list'] ?? []) as $f) {
    $type = (!empty($f['is_dir']) || ($f['type'] ?? '') === 'folder') ? '[文件夹]' : '[文件]';
    echo "  $type ", ($f['name_all'] ?? ($f['name'] ?? '')), "  (id=", ($f['id'] ?? ''), ")\n";
}

echo "\n========== ⑤ 上传文件（服务器本地路径，自动生成分享链接） ==========\n";
$r = api_call("$API/api/upload", ['path' => '/tmp/test.zip', 'folder' => '0'], 600);
if (need_login($r, '上传')) exit;
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";
$shareUrl = $r['data']['share_url'] ?? '';

echo "\n========== ⑥ 重命名（先蓝奏原生，失败自动回退重传） ==========\n";
$fileId = $r['data']['id'] ?? '';
if ($fileId !== '') {
    $r2 = api_call("$API/api/rename", ['file_id' => $fileId, 'name' => '改名测试.zip', 'folder' => '0'], 600);
    echo json_encode($r2, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";
}

echo "\n========== ⑦ 解析分享链接为直链 ==========\n";
$r = api_call("$API/api/parse?url=" . urlencode($shareUrl ?: 'https://wwi.lanzou.com/XXXX') . "&pwd=");
echo json_encode($r, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT), "\n";

echo "\n完成\n";
