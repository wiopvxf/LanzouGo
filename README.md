# LanzouGo v1.0.0 — 蓝奏云三合一管理服务（Go）

把三个开源模块的能力整合进**一个Go二进制**：**上传（transfer lanzous）+ 文件列表（Alist ILanZou）+ 分享解析直链（go-lanzou）**。
统一使用**同一个 Cookie**，一条命令自动检测并写入；Cookie 无效时所有接口统一返回 **`Cookie异常`**。

- 纯 Go 标准库实现，**零第三方依赖**，原生 net/http
- 内存占用极低（空闲几十 MB），适配 2G 低配服务器
- 提供 HTTP JSON 接口，**PHP 可直接调用**（见 php_example.php）

## 快速开始（Linux / Termux）

```bash
# 1. 设置Cookie并自动检测（有效才写入，无效退出并提示 Cookie异常）
./lanzou-server -cookie "phpdisk_info=你的Cookie值"

# 2. 启动服务（默认端口8080）
./lanzou-server -port 8080
```

启动时也会自动检测已有配置的 Cookie。

## 接口一览

| 接口 | 方法 | 说明 |
|---|---|---|
| `/api/cookie` | POST | 写入Cookie，自动检测有效性，无效返回 `Cookie异常` |
| `/api/status` | GET | 查看当前Cookie状态 |
| `/api/list?folder=0` | GET | 获取网盘文件/文件夹列表（0=根目录） |
| `/api/upload` | POST | 上传服务器本地文件，自动开启分享返回短链 |
| `/api/parse?url=..&pwd=..` | GET | 解析蓝奏云分享链接为直链 |
| `/api/help` | GET | 接口说明 |

### 调用示例

```bash
# 写入Cookie（自动检测）
curl -X POST -d "cookie=phpdisk_info=xxx" http://127.0.0.1:8080/api/cookie

# 文件列表
curl "http://127.0.0.1:8080/api/list?folder=0"

# 上传（path = 服务器本地文件路径）
curl -X POST -d "path=/tmp/test.zip&folder=0" http://127.0.0.1:8080/api/upload

# 解析直链
curl "http://127.0.0.1:8080/api/parse?url=https://wwi.lanzou.com/xxxx&pwd=密码"
```

### PHP 调用（php_example.php）

```php
$r = json_decode(file_get_contents(
    "http://127.0.0.1:8080/api/cookie",
    false,
    stream_context_create(['http' => [
        'method' => 'POST',
        'header' => "Content-Type: application/x-www-form-urlencoded\r\n",
        'content' => 'cookie=' . urlencode($COOKIE),
    ]])
), true);
// code==0 成功；code==401 表示 Cookie异常
```

完整示例直接运行 `php php_example.php`。

## 配置说明

配置文件 `lanzou.json` 自动生成（自动保存Cookie与有效性状态）：

```json
{
  "cookie": "phpdisk_info=xxx",
  "updated_at": "2026-10-11 14:30:00",
  "valid": true
}
```

命令行参数：

| 参数 | 说明 |
|---|---|
| `-cookie "phpdisk_info=xxx"` | 设置Cookie并自动检测后写入 |
| `-port 8080` | 监听端口（默认8080） |
| `-cfg lanzou.json` | 配置文件路径（默认当前目录 lanzou.json） |

## 构建

```bash
# 当前平台
go build -ldflags "-s -w" -o lanzou-server main.go

# Linux amd64（x86服务器）
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o lanzou-server-amd64 main.go

# Linux arm64（ARM服务器/Termux）
GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o lanzou-server-arm64 main.go
```

## 安全提醒

1. **接口无鉴权**，仅适合内网/本机使用；对外暴露请自行加一层 Nginx Basic Auth 或防火墙白名单。
2. Cookie 含登录凭证，`lanzou.json` 权限已设为 600，请勿外传。
3. 蓝奏云网页接口会改版，失效时按返回的 `detail` 提示调整对应模块的请求参数。
4. 上传接口仅支持服务器本地文件（PHP 与 Go 同机部署时最省内存，不缓存文件）。

## 三合一对应关系

| 功能 | 来源逻辑 | 模块 |
|---|---|---|
| 文件上传 + 自动分享 | transfer `lanzous` | `uploadFile` / `openShare` |
| 文件列表 | Alist `ILanZou` folder 接口 | `fileList` |
| 分享链接解析直链 | go-lanzou | `parseShare` |
| Cookie 统一检测 | 三模块共用 | `checkCookie` |

## 开源与致谢

本项目整合了以下开源项目的核心逻辑，均为其原作者成果：

| 上游项目 | 用途 | 许可 |
|---|---|---|
| [Alist](https://github.com/AlistGo/alist) `drivers/ilanzou` | 文件列表接口 | AGPL-3.0 |
| transfer（`lanzous` 模块） | 分块上传 + 自动分享 | 见上游 |
| [go-lanzou](https://github.com/) | 分享页解析直链 + `acw_sc__v2` 挑战 | 见上游 |

因整合了 AGPL-3.0 许可的 Alist 逻辑，本项目整体以 **AGPL-3.0** 发布，详见 `LICENSE`。
二次开发请同样以 AGPL-3.0 开放源代码。

## 安全提示（重要）

- `/api/*` **没有任何鉴权**，拿到端口即可读写你的网盘。请只监听 `127.0.0.1`，或用防火墙/Nginx 白名单限制来源。
- `lanzou.json` 里是登录 Cookie，已被 `.gitignore` 排除，**不要提交到仓库**。
- 本服务仅面向自建自用，请勿部署为公开服务。
