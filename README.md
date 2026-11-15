# LanzouGo v1.2.0 — 蓝奏云三合一管理服务（Go）

把三块能力整合进**一个 Go 二进制**：**上传 + 文件列表 + 分享解析直链**。
v1.1 起支持**账号密码登录**：登录后自动获取 Cookie，Cookie 失效自动重新登录，账号信息（UID / VIP / 上传上限）自动获取。
v1.2 加入 **VIP 自动检测**（等级 / 到期时间 / 剩余天数 / 容量）、**按 VIP 切换重命名机制与上传上限**、**受限后缀自动加 `(c).zip`**、**上传文件名校验**、**检查更新 + 一键自更新**。

- 纯 Go 标准库实现，**零第三方依赖**，原生 net/http
- 内存占用低，上传走流式（大文件不再整包读进内存）
- 提供 HTTP JSON 接口，PHP 可直接调用（见 `php_example.php`、`lanzou-test.php` 面板）
- **任何接口都不会返回 Cookie 原文**

## 快速开始（Linux / Termux）

```bash
# 方式一：账号密码登录（推荐）
go build -ldflags "-s -w" -o lanzou-server main.go
./lanzou-server -listen 127.0.0.1 -port 8080
curl -X POST -d "username=你的账号&password=你的密码" http://127.0.0.1:8080/api/login
# 登录成功后，账号密码会加密写入 lanzou.json，之后 Cookie 失效会自动重登

# 方式二：只用 Cookie（不想存密码时）
./lanzou-server -port 8080
curl -X POST -d "cookie=phpdisk_info=xxx" http://127.0.0.1:8080/api/cookie
```

启动时会自动检测已保存的 Cookie；不可用且有账号密码时自动重登。

## 核心行为

| 行为 | 说明 |
|---|---|
| 账号登录 | `POST /api/login`，走 `accounts.woozooo.com` 新版登录表单，失败自动回退旧版 `up.woozooo.com/account.php` |
| Cookie 失效自动重登 | 业务接口返回登录页 → 标记失效 → 用保存的账号密码重登 → **自动重试一次**；并发请求只触发一次重登 |
| 登录失败提示 | 统一返回 `{"code":401,"msg":"账号登录失败","detail":"<原因>"}` |
| 账号信息自动获取 | 登录后抓 `mydisk.php` 解析 `uid` / `vei`，并尽力识别 VIP |
| VIP 检测 | 登录/写入 Cookie 后自动解析是否 VIP、**VIP 几**、**到期时间**、剩余天数、容量；`GET /api/check_vip` 可强制重测 |
| 上传限制 | 非 VIP 单文件固定 **100MB**；VIP 可用 `max_upload_mb` 或按等级 `level_limits`（如 `1=200,2=500`）设置，**检测到 VIP 后上限自动跟随等级** |
| 重命名机制 | **VIP → 机制 1**：优先调蓝奏原生重命名；**非 VIP → 机制 0**：下载 → 改名上传 → 删原文件。机制 1 原生失败也会自动回退机制 0，并**保留原提取码** |
| 后缀修复 | 上传名为受限后缀（exe/bat/apk 等，内置清单 + `blocked_ext` 可扩展）时，实际存为 `原名.exe(c).zip`；**列表展示与下载自动去掉 `(c).zip`**，用户无感 |
| 文件名校验 | 上传/重命名时文件名不能为空白，且**至少 2 个字符**，否则直接 400，不会白传 |
| 检查更新 | `GET /api/version` 对比 GitHub Release（限流时可配 `github_token` 或用 `-check-update`） |
| 自更新 | `POST /api/update`（`confirm=1`）下载对应平台二进制 → 替换 → 自动重启；可 `allow_update=0` 关闭 |
| Cookie 不外泄 | 所有响应不含 Cookie；`/api/status` 只给 `sha256:` 前 8 位摘要；下载代理不携带登录 Cookie |

## 接口一览

| 接口 | 方法 | 说明 |
|---|---|---|
| `/api/login` | POST | `username`+`password` 登录，自动保存并维护 Cookie |
| `/api/logout` | POST | 清除账号密码与 Cookie |
| `/api/cookie` | POST | 手动写入 Cookie（自动验证） |
| `/api/status` | GET | 账号状态（uid / vip / VIP 等级 / 到期 / 重命名机制 / 上传上限 / 版本），无 Cookie 原文 |
| `/api/check_vip` | GET | 强制检测 Cookie：VIP 状态、VIP 几、到期时间、剩余天数、容量 |
| `/api/version` | GET | 检查更新（对比 GitHub Release，含更新包名与大小） |
| `/api/update` | POST | 自更新（`confirm=1`，需 `allow_update` 开启） |
| `/api/account` | GET | 账号信息（uid / vip / 上传上限） |
| `/api/limits` | GET/POST | 查看/修改上传上限（VIP 专属）、`level_limits` 按等级上限、`allow_update` 开关、自动分享 |
| `/api/list?folder=0` | GET | 文件 + 文件夹列表 |
| `/api/upload` | POST | 上传服务器本地文件（`path` 必须在白名单目录内） |
| `/api/mkdir` | POST | 新建文件夹（`name`、`parent`） |
| `/api/delete` | POST | 删除（`id`、`type=file|folder`） |
| `/api/share` | POST | 生成分享（`file_id`、`pwd`、`type=file|folder`） |
| `/api/parse?url=..&pwd=..` | GET | 解析分享链接为直链 |
| `/api/download?url=..` | GET | 代理下载（仅蓝奏域名，逐跳校验） |
| `/api/save` | POST | 转存（`url`/`direct_url`、`pwd`、`folder`） |
| `/api/rename` | POST | 重命名文件（`file_id`、`name`、`folder`） |
| `/api/rename_folder` | POST | 重命名文件夹（`folder_id`、`name`、`parent`） |
| `/api/help` | GET | 接口说明 |

### 调用示例

```bash
# 登录
curl -X POST -d "username=账号&password=密码" http://127.0.0.1:8080/api/login

# 状态（不含 Cookie）
curl http://127.0.0.1:8080/api/status

# 文件列表
curl "http://127.0.0.1:8080/api/list?folder=0"

# 上传（path 必须在允许目录：程序目录、/tmp，或 -upload-dir 指定的目录）
curl -X POST -d "path=/tmp/test.zip&folder=0" http://127.0.0.1:8080/api/upload

# 重命名（先原生，失败自动回退重传）
curl -X POST -d "file_id=123456&name=新名字.zip&folder=0" http://127.0.0.1:8080/api/rename

# 解析直链
curl "http://127.0.0.1:8080/api/parse?url=https://wwi.lanzou.com/xxxx&pwd=密码"
```

## 命令行参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-listen` | `127.0.0.1` | 监听地址。**默认只监听本机**；对外提供服务请配合 Nginx/防火墙并设置 `-token` |
| `-port` | `8080` | 监听端口 |
| `-cfg` | `lanzou.json` | 配置文件路径 |
| `-upload-dir` | 无 | 额外允许上传的目录（逗号分隔），默认允许程序目录与 `/tmp` |
| `-token` | 无 | 访问令牌，设置后所有接口需带 `X-Token` 头或 `?token=` |
| `-maxupload` | 100 | VIP 账号单文件上限（MB），非 VIP 始终 100MB |
| `-check-update` | 无 | 打印当前版本与 GitHub 最新版本后退出 |
| `-update` | 无 | 检查并直接执行自更新（替换二进制）后退出 |
| `-no-update` | 无 | 关闭自更新接口（写回配置 `allow_update=false`） |

## 配置说明

配置文件 `lanzou.json`（0600，已被 `.gitignore` 排除）：

```json
{
  "username": "你的账号",
  "password_enc": "AES-GCM 密文（密钥在 lanzou.key，0600）",
  "cookie": "phpdisk_info=xxx",
  "valid": true,
  "uid": "123456",
  "vip": true,
  "vip_level": 2,
  "vip_expire": "2027-01-01",
  "vip_checked_at": "2026-11-15T09:00:00+08:00",
  "rename_mode": 1,
  "max_upload_mb": 500,
  "level_limits": { "1": 200, "2": 500 },
  "blocked_ext": [],
  "auto_share": true,
  "share_pwd": "001",
  "allow_update": true,
  "github_token": "可选：ghp_xxx，仅用于避免 GitHub API 限流",
  "updated_at": "2026-11-15T09:00:00+08:00"
}
```

- 密码只用于 Cookie 失效时自动重登，用 AES-GCM 加密后落盘；`lanzou.key` 是加密密钥，**请与 `lanzou.json` 一起妥善保管**。
- 不想保存密码就不调用 `/api/login`，改用 `/api/cookie` 手动写入（此时 Cookie 失效会返回 `账号登录失败` 而不会自动重登）。

## PHP 面板（lanzou-test.php）

`lanzou-test.php` 是单文件 H5 面板（三 Tab：文件列表 / 分享 / 设置），直接透传本机 Go 服务。

- 设置页可直接用**账号 + 密码**登录，登录后 Cookie 由 Go 服务端维护，面板不再需要手填 Cookie
- 状态卡显示 UID / VIP / 上传上限 / Cookie 摘要（不含原文）
- 上传前按服务端返回的上限做预检，超限直接提示，避免白传
- 重命名按钮会显示本次走的是「蓝奏原生」还是「回退重传」
- v1.2 设置页新增「VIP 状态」卡（立即检测 / 显示 VIP 几、到期时间、剩余天数、当前机制、按等级设上限）和「检查更新」卡（检查 + 一键更新）
- PHP 侧限制：`php.ini` 的 `upload_max_filesize` / `post_max_size` 必须大于你要传的文件，否则 PHP 会先拒绝（面板会明确提示这两个值）

## 构建

```bash
# 当前平台
go build -ldflags "-s -w" -o lanzou-server main.go

# Linux amd64
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o lanzou-server-amd64 main.go

# Linux arm64（ARM 服务器 / Termux）
GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o lanzou-server-arm64 main.go
```

## 安全说明

1. **默认只监听 127.0.0.1**。需要对外时请设置 `-token`，并用 Nginx Basic Auth / 防火墙白名单再包一层。
2. `-upload-dir` 之外的文件无法通过 `/api/upload` 读取（路径白名单 + 软链接解析校验）。
3. `/api/download` 与 `/api/parse` 只接受蓝奏域名（后缀白名单，逐跳校验重定向），**不能**拿它当内网 SSRF 跳板。
4. Cookie 不会出现在任何响应里；日志里也不打印 Cookie。
5. 蓝奏网页接口会持续改版（登录域已迁到 `accounts.woozooo.com`，且带 `acw_sc__v2` 新变体挑战）。程序内置多套挑战表/key 与新旧两套登录接口，仍失败时按返回的 `detail` 提示调整。
6. 自建自用，请勿部署为公开服务。

## 三合一对应关系

| 功能 | 来源逻辑 | 模块 |
|---|---|---|
| 文件上传 + 自动分享 | transfer `lanzous` | `uploadFile` / `shareInfo` |
| 文件列表 | Alist `ILanZou` folder 接口 | `fileList` |
| 分享解析直链 | go-lanzou | `parseShare` |
| Cookie / 登录统一维护 | 新增 | `Session` / `lanzouLogin` |

## 开源与致谢

| 上游项目 | 用途 | 许可 |
|---|---|---|
| [Alist](https://github.com/AlistGo/alist) `drivers/ilanzou` | 文件列表接口 | AGPL-3.0 |
| transfer（`lanzous` 模块） | 上传 + 自动分享 | 见上游 |
| go-lanzou | 分享页解析直链 + `acw_sc__v2` 挑战 | 见上游 |

因整合了 AGPL-3.0 许可的 Alist 逻辑，本项目整体以 **AGPL-3.0** 发布，详见 `LICENSE`。
