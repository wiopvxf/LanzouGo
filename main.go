/*
 * LanzouGo v1.1.0 —— 蓝奏云三合一管理服务（纯 Go 标准库，零第三方依赖）
 *
 * v1.1 相比 v1.0 的主要变化：
 *   1. 新增账号登录：登录后自动获取 Cookie；Cookie 失效自动重登；登录失败直接提示「账号登录失败」
 *   2. 账号信息（UID / VIP / 上传上限）自动获取，/api/status 与 /api/account 不再返回 Cookie
 *   3. 上传限制：非 VIP 单文件固定 100MB；VIP 可用 /api/limits 调整
 *   4. 重命名：优先调用蓝奏自带重命名，不可用时回退「下载→改名→上传→删原」
 *   5. 安全加固：SSRF 域名后缀白名单、不再把登录 Cookie 发给第三方、默认只监听 127.0.0.1、
 *      上传只允许白名单目录、可选访问令牌
 *   6. 稳定性：全局状态加锁、配置原子写入、上传流式（不再整文件进内存）、Unbox 越界修复
 *
 * 编译：go build -o lanzou main.go
 */
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const version = "1.2.0"

const (
	repoOwner = "wiopvxf"
	repoName  = "LanzouGo"

	apiHost     = "https://pc.woozooo.com"
	apiUpHost   = "https://up.woozooo.com"
	accountsURL = "https://accounts.woozooo.com/accounts.php?action=login&ref=up.woozooo.com"

	defaultMaxUploadMB = 100
)

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	reUID      = regexp.MustCompile(`uid=(["']?)(\d{4,})`)
	reVei      = regexp.MustCompile(`'vei'\s*:\s*'([^']+)'`)
	reFormHash = regexp.MustCompile(`name="formhash"\s+value="([^"]+)"`)
	reArg1     = regexp.MustCompile(`arg1='([0-9A-Z]+)'`)

	reIfr      = regexp.MustCompile(`src="?/fn\?([^"'#]+)"?`)
	reAjax     = regexp.MustCompile(`ajaxfile\s*=\s*'(\d+)'`)
	reDom      = regexp.MustCompile(`dom\s*=\s*'([^']+)'`)
	reTParam   = regexp.MustCompile(`var\s+\w+\s*=\s*'(\d{10})'`)
	reFid2     = regexp.MustCompile(`ajaxdata\s*=\s*'([^']*)'`)
	reFolderID = regexp.MustCompile(`(?i)mbxfolder\s*=\s*'?([0-9]+)'?`)
	reTitleTag = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	reSubID    = regexp.MustCompile(`(?i)task=(\d+)&folder_id=(\d+)`)
)

var errCookie = errors.New("cookie 已失效")

// ======================================================================
// HTTP 客户端
// ======================================================================

var (
	// 业务接口：不跟随重定向（统一走手工校验，避免 SSRF 绕过）
	apiClient = &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// 上传：大文件需要长超时
	uploadClient = &http.Client{Timeout: 30 * time.Minute}
	// 下载：需要跟随重定向，且不限总时长（大文件），但每一跳都校验域名
	dlClient = &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("重定向次数过多")
			}
			if !hostAllowed(req.URL.String()) {
				return fmt.Errorf("重定向到非蓝奏域名：%s", req.URL.Host)
			}
			return nil
		},
	}
)

func hostAllowed(raw string) bool {
	pu, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || pu.Host == "" {
		return false
	}
	if pu.Scheme != "http" && pu.Scheme != "https" {
		return false
	}
	h := strings.ToLower(pu.Hostname())
	if h == "" {
		return false
	}
	for _, suf := range allowHostSuffix {
		if h == strings.TrimPrefix(suf, ".") || strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

var allowHostSuffix = []string{
	".woozooo.com", ".lanzou.com", ".lanzoua.com", ".lanzoub.com", ".lanzouc.com",
	".lanzoud.com", ".lanzoue.com", ".lanzouf.com", ".lanzoug.com", ".lanzouh.com",
	".lanzoui.com", ".lanzouj.com", ".lanzouk.com", ".lanzoul.com", ".lanzoum.com",
	".lanzoup.com", ".lanzouq.com", ".lanzous.com", ".lanzout.com", ".lanzouu.com",
	".lanzouv.com", ".lanzouw.com", ".lanzouy.com", ".lanzouz.com",
	".lanzn.com", ".lanrar.com", ".webgetstore.com",
}

// ======================================================================
// acw_sc__v2 反爬挑战
// ======================================================================

// 旧站点使用的置换表（0 基）
var acwTableOld = []int{6, 28, 34, 31, 33, 18, 30, 23, 9, 8, 19, 38, 17, 24, 0, 5, 32, 21, 10, 22, 25, 14, 15, 3, 16, 27, 13, 35, 2, 29, 11, 26, 4, 36, 1, 39, 37, 7, 20, 12}

// 2026 年新版挑战使用的置换表（源码里是 1 基，此处已减 1）
var acwTableNew = []int{14, 34, 28, 23, 32, 15, 0, 37, 9, 8, 18, 30, 39, 26, 21, 22, 24, 12, 5, 10, 38, 17, 19, 7, 13, 20, 31, 25, 1, 29, 6, 3, 16, 4, 2, 27, 33, 36, 11, 35}

var (
	acwTables = [][]int{acwTableOld, acwTableNew}
	acwKeys   = []string{
		"3000176000856006061501533003690027800375",
		"8552f8f60096b330d29b77b368ed99d5e8730a11",
	}
)

func unbox(h string, table []int) string {
	if len(h) < 40 || len(table) < 40 {
		return ""
	}
	out := make([]byte, 40)
	for i := 0; i < 40; i++ {
		j := table[i]
		if j < 0 || j >= 40 {
			continue
		}
		out[j] = h[i]
	}
	return string(out)
}

func hexXor(h1, h2 string) string {
	n := len(h1)
	if len(h2) < n {
		n = len(h2)
	}
	var b strings.Builder
	for i := 0; i+1 < n; i += 2 {
		a, err1 := strconv.ParseInt(h1[i:i+2], 16, 32)
		c, err2 := strconv.ParseInt(h2[i:i+2], 16, 32)
		if err1 != nil || err2 != nil {
			return ""
		}
		b.WriteString(fmt.Sprintf("%02x", a^c))
	}
	return b.String()
}

// acwCandidates 返回挑战页所有可尝试的 acw_sc__v2 取值（多表 × 多 key）
func acwCandidates(page string) []string {
	m := reArg1.FindStringSubmatch(page)
	if len(m) < 2 {
		return nil
	}
	arg1 := m[1]
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, tb := range acwTables {
		u := unbox(arg1, tb)
		if u == "" {
			continue
		}
		for _, k := range acwKeys {
			v := hexXor(u, k)
			if len(v) != 40 || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func appendCookie(cookie, name, val string) string {
	if cookie == "" {
		return name + "=" + val
	}
	return cookie + "; " + name + "=" + val
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return body, err
	}
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") && len(body) > 2 {
		if gz, gerr := gzip.NewReader(bytes.NewReader(body)); gerr == nil {
			dec, derr := io.ReadAll(gz)
			_ = gz.Close()
			if derr == nil {
				return dec, nil
			}
		}
	}
	return body, nil
}

// doReqWith 发起请求并在遇到 acw 挑战时自动计算补发（每次重试都重新构造请求）
func doReqWith(cl *http.Client, makeReq func(cookie string) (*http.Request, error), cookie string, tries int) ([]byte, error) {
	tried := map[string]bool{}
	for i := 0; i < tries; i++ {
		req, err := makeReq(cookie)
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := readBody(resp)
		if err != nil {
			return body, err
		}
		if !reArg1.Match(body) {
			return body, nil
		}
		next := ""
		for _, cand := range acwCandidates(string(body)) {
			if !tried[cand] {
				next = cand
				break
			}
		}
		if next == "" {
			return body, errors.New("acw_sc__v2 挑战未通过（站点算法可能又变了）")
		}
		tried[next] = true
		cookie = appendCookie(cookie, "acw_sc__v2", next)
	}
	return nil, errors.New("acw_sc__v2 挑战处理失败（重试超限）")
}

func doReq(makeReq func(cookie string) (*http.Request, error), cookie string) ([]byte, error) {
	return doReqWith(apiClient, makeReq, cookie, 6)
}

// ======================================================================
// 配置
// ======================================================================

type Config struct {
	Username     string         `json:"username,omitempty"`
	PasswordEnc  string         `json:"password_enc,omitempty"` // AES-GCM 加密后的密码
	Cookie       string         `json:"cookie,omitempty"`
	Valid        bool           `json:"valid"`
	UID          string         `json:"uid,omitempty"`
	VIP          bool           `json:"vip"` // Cookie 检测结果
	VIPLevel     int            `json:"vip_level,omitempty"`
	VIPExpire    string         `json:"vip_expire,omitempty"`
	VIPCheckedAt string         `json:"vip_checked_at,omitempty"`
	RenameMode   int            `json:"rename_mode"`            // 1=原生重命名(VIP) 0=下载改名上传(非VIP)
	MaxUploadMB  int            `json:"max_upload_mb"`          // VIP 默认上限，非 VIP 固定 100
	LevelLimits  map[string]int `json:"level_limits,omitempty"` // 按 VIP 等级定制上限，如 {"1":200,"2":500}
	AutoShare    bool           `json:"auto_share"`             // 上传后是否自动开启分享
	SharePwd     string         `json:"share_pwd"`              // 自动分享使用的提取码
	BlockedExt   []string       `json:"blocked_ext,omitempty"`  // 追加“不支持后缀”，命中后上传名会加 (c).zip
	AllowUpdate  bool           `json:"allow_update"`           // 是否允许 /api/update 自更新
	GitHubToken  string         `json:"github_token,omitempty"` // 可选：GitHub Token，避免 API 限流
	Token        string         `json:"token,omitempty"`        // 访问令牌，为空则不校验
	UpdatedAt    string         `json:"updated_at,omitempty"`
}

var (
	cfgPath = "lanzou.json"
	keyPath = "lanzou.key"
	cfgMu   sync.RWMutex
	conf    = Config{MaxUploadMB: defaultMaxUploadMB, AutoShare: true, SharePwd: "001", AllowUpdate: true}
)

func cfgGet() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return conf
}

func cfgUpdate(fn func(*Config)) error {
	cfgMu.Lock()
	fn(&conf)
	snapshot := conf
	cfgMu.Unlock()
	return writeCfg(snapshot)
}

func writeCfg(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

func loadCfg() error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	if c.MaxUploadMB <= 0 {
		c.MaxUploadMB = defaultMaxUploadMB
	}
	if c.SharePwd == "" {
		c.SharePwd = "001"
	}
	cfgMu.Lock()
	conf = c
	cfgMu.Unlock()
	return nil
}

// ======================================================================
// 密码本地加密（用于自动重登）
// ======================================================================

var (
	keyOnce sync.Once
	encKey  []byte
)

func loadKey() []byte {
	keyOnce.Do(func() {
		if b, err := os.ReadFile(keyPath); err == nil && len(b) == 32 {
			encKey = b
			return
		}
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			encKey = []byte("lanzougo-fallback-key-32bytes!!")
			return
		}
		_ = os.WriteFile(keyPath, k, 0600)
		encKey = k
	})
	return encKey
}

func encryptPwd(p string) string {
	if p == "" {
		return ""
	}
	block, err := aes.NewCipher(loadKey())
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(p), nil))
}

func decryptPwd(s string) string {
	if s == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(loadKey())
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// ======================================================================
// 会话：Cookie / UID / VIP 统一管理
// ======================================================================

type acctMeta struct {
	UID       string
	Vei       string
	VIP       bool
	VIPLevel  int
	VIPExpire string
	VIPDays   int
	Capacity  string
}

type Session struct {
	mu        sync.RWMutex
	cookie    string
	uid       string
	vei       string
	vip       bool
	vipLevel  int
	vipExpire string
	vipDays   int
	metaAt    time.Time
	valid     bool
	lastErr   string
	updated   time.Time
	loginMu   sync.Mutex
}

var sess = &Session{}

func (s *Session) snapshot() (cookie, uid, vei string, vip, valid bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cookie, s.uid, s.vei, s.vip, s.valid
}

func (s *Session) vipSnapshot() (vip bool, level int, expire string, days int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vip, s.vipLevel, s.vipExpire, s.vipDays
}

// clearCookieCache 换 Cookie 时必须调用，否则会把旧账号的 uid/vei 带到新账号
func (s *Session) clearCookieCache() {
	s.mu.Lock()
	s.uid, s.vei = "", ""
	s.vip, s.vipLevel, s.vipExpire, s.vipDays = false, 0, "", 0
	s.metaAt = time.Time{}
	s.mu.Unlock()
}

func (s *Session) setCookie(cookie string, m acctMeta) {
	s.mu.Lock()
	s.cookie = cookie
	s.applyMetaLocked(m)
	s.valid = cookie != ""
	s.metaAt = time.Now()
	s.updated = time.Now()
	s.lastErr = ""
	s.mu.Unlock()
	_ = cfgUpdate(func(c *Config) {
		c.Cookie = cookie
		c.Valid = cookie != ""
		c.UID = m.UID
		c.VIP = m.VIP
		c.VIPLevel = m.VIPLevel
		c.VIPExpire = m.VIPExpire
		c.VIPCheckedAt = time.Now().Format(time.RFC3339)
		c.RenameMode = renameModeFor(m.VIP)
		c.UpdatedAt = time.Now().Format(time.RFC3339)
	})
}

func (s *Session) applyMetaLocked(m acctMeta) {
	s.uid, s.vei = m.UID, m.Vei
	s.vip, s.vipLevel, s.vipExpire, s.vipDays = m.VIP, m.VIPLevel, m.VIPExpire, m.VIPDays
}

// applyMeta 刷新账号信息（不写配置）
func (s *Session) applyMeta(m acctMeta) {
	s.mu.Lock()
	s.applyMetaLocked(m)
	s.metaAt = time.Now()
	s.mu.Unlock()
}

func (s *Session) invalidate(detail string) {
	s.mu.Lock()
	s.valid = false
	s.lastErr = detail
	s.uid, s.vei = "", ""
	s.mu.Unlock()
	_ = cfgUpdate(func(c *Config) {
		c.Valid = false
		c.Cookie = ""
	})
}

// ensureIDs 取（必要时抓取）uid/vei，同时刷新 VIP 等级与到期时间
func (s *Session) ensureIDs(cookie string) (string, string, error) {
	s.mu.RLock()
	uid, vei := s.uid, s.vei
	s.mu.RUnlock()
	if uid != "" && vei != "" {
		return uid, vei, nil
	}
	m, err := fetchAccountMeta(cookie)
	if err != nil {
		return "", "", err
	}
	s.applyMeta(m)
	_ = cfgUpdate(func(c *Config) {
		c.UID = m.UID
		c.VIP = m.VIP
		c.VIPLevel = m.VIPLevel
		c.VIPExpire = m.VIPExpire
		c.VIPCheckedAt = time.Now().Format(time.RFC3339)
		c.RenameMode = renameModeFor(m.VIP)
	})
	return m.UID, m.Vei, nil
}

// refreshAccount 主动检测一次 Cookie 与 VIP 状态（/api/check_vip 用）
func (s *Session) refreshAccount() (acctMeta, error) {
	cookie, err := s.ensureCookie()
	if err != nil {
		return acctMeta{}, err
	}
	m, err := fetchAccountMeta(cookie)
	if err != nil {
		return acctMeta{}, err
	}
	s.setCookie(cookie, m)
	return m, nil
}

// ensureCookie 返回可用 Cookie；失效则自动用账号密码重登
func (s *Session) ensureCookie() (string, error) {
	if c, _, _, _, ok := s.snapshot(); ok && c != "" {
		return c, nil
	}
	cfg := cfgGet()
	if cfg.Cookie != "" {
		// 用持久化的 Cookie 先试一下
		if _, _, err := s.ensureIDs(cfg.Cookie); err == nil {
			s.mu.Lock()
			s.cookie = cfg.Cookie
			s.valid = true
			s.mu.Unlock()
			c, _, _, _, _ := s.snapshot()
			return c, nil
		}
		s.mu.Lock()
		s.uid, s.vei = "", ""
		s.mu.Unlock()
	}
	return s.login()
}

// login 用账号密码登录（并发安全，重复调用只登录一次）
func (s *Session) login() (string, error) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if c, _, _, _, ok := s.snapshot(); ok && c != "" {
		return c, nil
	}
	cfg := cfgGet()
	pwd := decryptPwd(cfg.PasswordEnc)
	if cfg.Username == "" || pwd == "" {
		msg := "账号登录失败：尚未配置账号密码，请先调用 /api/login 或写入 Cookie"
		s.mu.Lock()
		s.lastErr = msg
		s.mu.Unlock()
		return "", errors.New(msg)
	}
	cookie, m, err := lanzouLogin(cfg.Username, pwd)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.valid = false
		s.mu.Unlock()
		return "", err
	}
	s.setCookie(cookie, m)
	return cookie, nil
}

// isVIP 当前账号是否 VIP（到期时间已过视为非 VIP）
func (s *Session) isVIP() bool {
	vip, _, _, _ := s.vipSnapshot()
	if !vip {
		vip = cfgGet().VIP
	}
	if vip && s.vipExpired() {
		return false
	}
	return vip
}

func (s *Session) vipExpired() bool {
	_, _, expire, _ := s.vipSnapshot()
	if expire == "" {
		return false
	}
	t, err := parseDate(expire)
	if err != nil {
		return false
	}
	return time.Now().After(t.Add(24 * time.Hour))
}

// renameMode 1 = 蓝奏原生重命名（VIP）；0 = 下载→改名上传→删原（非 VIP）
func (s *Session) renameMode() int {
	if s.isVIP() {
		return 1
	}
	return 0
}

func renameModeFor(vip bool) int {
	if vip {
		return 1
	}
	return 0
}

// effectiveLimitMB 上传上限：非 VIP 固定 100MB；VIP 按等级上限（未配置等级则用 MaxUploadMB）
func (s *Session) effectiveLimitMB() int64 {
	limit := int64(defaultMaxUploadMB)
	if !s.isVIP() {
		return limit
	}
	cfg := cfgGet()
	_, level, _, _ := s.vipSnapshot()
	if level <= 0 {
		level = cfg.VIPLevel
	}
	if level > 0 && cfg.LevelLimits != nil {
		if mb, ok := cfg.LevelLimits[strconv.Itoa(level)]; ok && mb > defaultMaxUploadMB {
			return int64(mb)
		}
	}
	if mb := int64(cfg.MaxUploadMB); mb > defaultMaxUploadMB {
		limit = mb
	}
	return limit
}

// ======================================================================
// 账号登录
// ======================================================================

func jarCookie(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	seen := map[string]bool{}
	parts := make([]string, 0, 8)
	for _, host := range []string{"https://pc.woozooo.com/", "https://up.woozooo.com/", "https://accounts.woozooo.com/"} {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if seen[c.Name] || strings.HasPrefix(c.Name, "acw_") || c.Name == "cdn_sec_tc" {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func setJarCookie(jar http.CookieJar, name, val string) {
	if jar == nil {
		return
	}
	for _, host := range []string{"https://accounts.woozooo.com/", "https://pc.woozooo.com/", "https://up.woozooo.com/"} {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		jar.SetCookies(u, []*http.Cookie{{
			Name: name, Value: val, Path: "/", Domain: ".woozooo.com",
		}})
	}
}

func acwLoop(cl *http.Client, jar http.CookieJar, build func() (*http.Request, error)) ([]byte, error) {
	tried := map[string]bool{}
	for i := 0; i < 6; i++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := readBody(resp)
		if err != nil {
			return body, err
		}
		if !reArg1.Match(body) {
			return body, nil
		}
		next := ""
		for _, cand := range acwCandidates(string(body)) {
			if !tried[cand] {
				next = cand
				break
			}
		}
		if next == "" {
			return body, errors.New("acw_sc__v2 挑战未通过")
		}
		tried[next] = true
		setJarCookie(jar, "acw_sc__v2", next)
	}
	return nil, errors.New("acw_sc__v2 挑战处理失败（重试超限）")
}

func loginErrorText(body []byte) string {
	s := string(body)
	switch {
	case strings.Contains(s, "密码不正确"), strings.Contains(s, "密码错误"), strings.Contains(s, "账号或密码"):
		return "账号登录失败：账号或密码不正确"
	case strings.Contains(s, "账号不正确"), strings.Contains(s, "用户不存在"), strings.Contains(s, "账号不存在"):
		return "账号登录失败：账号不存在"
	case strings.Contains(s, "已停用"), strings.Contains(s, "封禁"):
		return "账号登录失败：账号已被限制"
	}
	return ""
}

func isLoginPage(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "未登录") || strings.Contains(s, "请先登录") ||
		strings.Contains(s, "account.php?action=login") || strings.Contains(s, "网盘用户登录")
}

// lanzouLogin 依次尝试新版 accounts 接口与旧版 account.php 接口
func lanzouLogin(username, password string) (string, acctMeta, error) {
	if c, m, err := loginViaAccounts(username, password); err == nil {
		return c, m, nil
	} else {
		firstErr := err
		if c2, m2, err2 := loginViaLegacy(username, password); err2 == nil {
			return c2, m2, nil
		}
		return "", acctMeta{}, firstErr
	}
}

func loginViaAccounts(username, password string) (string, acctMeta, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", acctMeta{}, fmt.Errorf("账号登录失败：%v", err)
	}
	cl := &http.Client{Timeout: 40 * time.Second, Jar: jar}

	body, err := acwLoop(cl, jar, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", accountsURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		return req, nil
	})
	if err != nil {
		return "", acctMeta{}, fmt.Errorf("账号登录失败：%v", err)
	}

	formhash := ""
	if m := reFormHash.FindStringSubmatch(string(body)); len(m) > 1 {
		formhash = m[1]
	}

	form := url.Values{}
	form.Set("action", "login")
	form.Set("task", "login")
	form.Set("ref", "up.woozooo.com")
	form.Set("username", username)
	form.Set("password", password)
	if formhash != "" {
		form.Set("formhash", formhash)
	}

	respBody, err := acwLoop(cl, jar, func() (*http.Request, error) {
		req, err := http.NewRequest("POST", accountsURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", accountsURL)
		req.Header.Set("Origin", "https://accounts.woozooo.com")
		return req, nil
	})
	if err != nil {
		return "", acctMeta{}, fmt.Errorf("账号登录失败：%v", err)
	}
	if msg := loginErrorText(respBody); msg != "" {
		return "", acctMeta{}, errors.New(msg)
	}

	cookie := jarCookie(jar)
	if !strings.Contains(cookie, "phpdisk_info") {
		page := string(respBody)
		if reArg1.MatchString(page) || len(page) < 300 {
			return "", acctMeta{}, errors.New("账号登录失败：登录接口被风控拦截（当前服务器出口 IP 可能不在白名单），请换 IP 重试，或在设置页改用 Cookie 方式")
		}
		return "", acctMeta{}, errors.New("账号登录失败：未能获取登录凭证（登录接口可能已改版）")
	}
	m, err := fetchAccountMeta(cookie)
	if err != nil {
		return "", acctMeta{}, fmt.Errorf("账号登录失败：%v", err)
	}
	return cookie, m, nil
}

func loginViaLegacy(username, password string) (string, acctMeta, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", acctMeta{}, err
	}
	cl := &http.Client{Timeout: 40 * time.Second, Jar: jar}
	form := url.Values{}
	form.Set("action", "login")
	form.Set("task", "3")
	form.Set("setlogin", "1")
	form.Set("uid", username)
	form.Set("pwd", password)

	body, err := acwLoop(cl, jar, func() (*http.Request, error) {
		req, err := http.NewRequest("POST", apiUpHost+"/account.php?action=login", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", apiUpHost+"/")
		return req, nil
	})
	if err != nil {
		return "", acctMeta{}, err
	}
	if msg := loginErrorText(body); msg != "" {
		return "", acctMeta{}, errors.New(msg)
	}
	cookie := jarCookie(jar)
	if !strings.Contains(cookie, "phpdisk_info") {
		return "", acctMeta{}, errors.New("旧版接口未返回登录 Cookie")
	}
	m, err := fetchAccountMeta(cookie)
	if err != nil {
		return "", acctMeta{}, err
	}
	return cookie, m, nil
}

// fetchAccountMeta 打开 mydisk 页面提取 uid / vei，并解析 VIP 等级与到期时间
func fetchAccountMeta(cookie string) (acctMeta, error) {
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("GET", apiHost+"/mydisk.php?item=files&action=index", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return acctMeta{}, err
	}
	s := string(body)
	if isLoginPage(body) {
		return acctMeta{}, errCookie
	}
	uid := ""
	if m := reUID.FindStringSubmatch(s); len(m) > 2 {
		uid = m[2]
	}
	vei := ""
	if m := reVei.FindStringSubmatch(s); len(m) > 1 {
		vei = m[1]
	}
	if uid == "" || vei == "" {
		return acctMeta{}, errors.New("账号页面解析失败（Cookie 可能无效）")
	}
	m := acctMeta{UID: uid, Vei: vei}
	m.VIP, m.VIPLevel, m.VIPExpire, m.VIPDays, m.Capacity = detectVIPInfo(s)
	return m, nil
}

var (
	reVIPLevelTag = regexp.MustCompile(`(?i)(?:VIP|会员等级|会员级别)\s*[：:=\s]{0,4}([1-9])`)
	reVIPExpire   = regexp.MustCompile(`(?:到期|有效期|截止|过期)[^0-9]{0,12}([0-9]{4}[-/年][0-9]{1,2}[-/月][0-9]{1,2})`)
	reAnyDate     = regexp.MustCompile(`([0-9]{4}-[0-9]{2}-[0-9]{2})`)
	reCapacity    = regexp.MustCompile(`(?:总空间|空间大小|容量)[^0-9]{0,12}([0-9.]+\s*(?:G|M|T|GB|MB|TB))`)
	reLevelText   = regexp.MustCompile(`(?:VIP|会员)\s*(\d)\s*级`)
)

// detectVIPInfo 从账号页面解析：是否 VIP、VIP 等级、到期时间、剩余天数、容量
func detectVIPInfo(page string) (vip bool, level int, expire string, days int, capacity string) {
	low := strings.ToLower(page)
	for _, marker := range []string{"高级会员", "vip会员", "会员有效期", "会员到期", "svip", "\"vip\":1", "vip_ico"} {
		if strings.Contains(low, strings.ToLower(marker)) {
			vip = true
			break
		}
	}
	if m := reLevelText.FindStringSubmatch(page); len(m) > 1 {
		vip = true
		level = int(m[1][0] - '0')
	}
	if level == 0 {
		if m := reVIPLevelTag.FindStringSubmatch(page); len(m) > 1 {
			vip = true
			level = int(m[1][0] - '0')
		}
	}
	if vip && level <= 0 {
		level = 1
	}
	if m := reVIPExpire.FindStringSubmatch(page); len(m) > 1 {
		expire = normalizeDate(m[1])
	} else if vip {
		if m := reAnyDate.FindStringSubmatch(page); len(m) > 1 {
			expire = normalizeDate(m[1])
		}
	}
	if m := reCapacity.FindStringSubmatch(page); len(m) > 1 {
		capacity = strings.TrimSpace(m[1])
	}
	if expire != "" {
		if t, err := parseDate(expire); err == nil {
			days = int(math.Ceil(time.Until(t.Add(24*time.Hour)).Hours() / 24))
			if days <= 0 {
				vip = false
				level = 0
			}
		}
	}
	return vip, level, expire, days, capacity
}

func normalizeDate(v string) string {
	v = strings.ReplaceAll(v, "年", "-")
	v = strings.ReplaceAll(v, "月", "-")
	v = strings.ReplaceAll(v, "日", "")
	v = strings.ReplaceAll(v, "/", "-")
	v = strings.TrimSuffix(v, "-")
	if t, err := parseDate(v); err == nil {
		return t.Format("2006-01-02")
	}
	return v
}

func parseDate(v string) (time.Time, error) {
	return parseDateStr(v)
}

var dateLayouts = []string{"2006-01-02", "2006-1-2", "2006-01-02 15:04:05", "2006-1-2 15:04:05"}

func parseDateStr(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析日期：%s", v)
}

// ======================================================================
// 蓝奏业务接口
// ======================================================================

// doupload 统一 POST doupload.php，自动携带 uid/vei 并识别登录失效
func doupload(cookie string, form url.Values) ([]byte, error) {
	uid, vei, err := sess.ensureIDs(cookie)
	if err != nil {
		return nil, err
	}
	form.Set("vei", vei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+url.QueryEscape(uid), strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, err
	}
	if isLoginPage(body) {
		return nil, errCookie
	}
	return body, nil
}

type lzResp struct {
	Zt   int             `json:"zt"`
	Code int             `json:"code"`
	Info json.RawMessage `json:"info"`
	Text json.RawMessage `json:"text"`
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// fileList 拉取目录列表（文件夹 + 文件），不再有 20 页硬上限
func fileList(folderID, cookie string) ([]map[string]interface{}, bool, error) {
	folderListID, fileListID := folderID, folderID
	if folderID == "0" || folderID == "" {
		folderListID, fileListID = "-1", "0"
	}
	out := make([]map[string]interface{}, 0, 64)

	fv := url.Values{"task": {"47"}, "folder_id": {folderListID}}
	if body, err := doupload(cookie, fv); err == nil {
		var r struct {
			Text []map[string]interface{} `json:"text"`
		}
		if json.Unmarshal(body, &r) == nil && r.Text != nil {
			out = append(out, r.Text...)
		}
	} else if errors.Is(err, errCookie) {
		return nil, false, err
	}

	truncated := false
	pages := 0
	for pg := 1; pg <= 200; pg++ {
		pages = pg
		fv := url.Values{"task": {"5"}, "folder_id": {fileListID}, "pg": {strconv.Itoa(pg)}}
		body, err := doupload(cookie, fv)
		if err != nil {
			if errors.Is(err, errCookie) {
				return nil, false, err
			}
			break
		}
		var r struct {
			Text []map[string]interface{} `json:"text"`
		}
		if json.Unmarshal(body, &r) != nil || len(r.Text) == 0 {
			break
		}
		out = append(out, r.Text...)
		if len(r.Text) < 60 {
			break
		}
	}
	if pages == 200 && !truncated {
		// 达到页数上限，可能仍有数据
		truncated = true
	}
	for _, it := range out {
		raw := firstNonEmpty(asString(it["name_all"]), asString(it["name"]))
		if raw == "" {
			continue
		}
		disp := displayName(raw)
		if disp != raw {
			it["raw_name"] = raw
		}
		it["name_all"] = disp
		it["name"] = disp
	}
	return out, truncated, nil
}

// ======================================================================
// 文件名后缀修复
//   蓝奏对部分后缀（exe/bat/...）限制上传：上传时改名为 "原名.exe(c).zip"，
//   列表展示与下载时再把这几个字去掉，用户看不到 "(c).zip"。
// ======================================================================

const repairTag = "(c).zip"

var defaultBlockedExt = map[string]bool{
	".exe": true, ".bat": true, ".cmd": true, ".com": true, ".msi": true, ".scr": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".hta": true, ".reg": true,
	".ps1": true, ".sh": true, ".php": true, ".asp": true, ".aspx": true, ".jsp": true,
	".cgi": true, ".pl": true, ".py": true, ".dll": true, ".sys": true, ".ocx": true,
	".jar": true, ".class": true, ".pif": true, ".cpl": true,
}

func blockedExt() map[string]bool {
	m := make(map[string]bool, len(defaultBlockedExt)+8)
	for k, v := range defaultBlockedExt {
		m[k] = v
	}
	for _, e := range cfgGet().BlockedExt {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m[e] = true
	}
	return m
}

// repairUploadName 需要修复时返回 "名字.exe(c).zip"
func repairUploadName(name string) string {
	if name == "" {
		return name
	}
	if strings.HasSuffix(strings.ToLower(name), repairTag) {
		return name
	}
	if blockedExt()[strings.ToLower(filepath.Ext(name))] {
		return name + repairTag
	}
	return name
}

// displayName 展示/下载时去掉 (c).zip
func displayName(name string) string {
	if strings.HasSuffix(strings.ToLower(name), repairTag) {
		return name[:len(name)-len(repairTag)]
	}
	return name
}

func uploadFile(filePath, folderID, cookie string) (map[string]interface{}, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("本地文件不存在：%s", filePath)
	}
	if fi.IsDir() {
		return nil, errors.New("暂不支持上传文件夹")
	}
	limitMB := sess.effectiveLimitMB()
	if fi.Size() > limitMB<<20 {
		return nil, fmt.Errorf("文件大小 %.2fMB 超过上传上限 %dMB（非 VIP 单文件固定 100MB，VIP 可通过 /api/limits 调整）",
			float64(fi.Size())/1048576, limitMB)
	}
	if folderID == "0" || folderID == "" {
		folderID = "-1"
	}
	uid, _, err := sess.ensureIDs(cookie)
	if err != nil {
		return nil, err
	}

	makeReq := func(c string) (*http.Request, error) {
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		go func() {
			if err := writeUploadBody(mw, filePath, folderID); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			_ = pw.Close()
		}()
		req, err := http.NewRequest("POST", apiHost+"/html5up.php?uid="+url.QueryEscape(uid), pr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}

	body, err := doReqWith(uploadClient, makeReq, cookie, 2)
	if err != nil {
		return nil, err
	}
	if isLoginPage(body) {
		return nil, errCookie
	}

	var r struct {
		Zt   int                    `json:"zt"`
		Info map[string]interface{} `json:"info"`
		Text string                 `json:"text"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("上传响应解析失败：%s", strings.TrimSpace(string(body)))
	}
	if r.Zt != 1 {
		return nil, fmt.Errorf("上传失败：%s", firstNonEmpty(r.Text, asString(r.Info["inf"]), "服务端拒绝"))
	}

	base := filepath.Base(filePath)
	remoteName := firstNonEmpty(asString(r.Info["name"]), repairUploadName(base))
	dispName := displayName(remoteName)
	id := asString(r.Info["id"])
	result := map[string]interface{}{
		"id":         id,
		"name_all":   dispName,
		"name":       dispName,
		"raw_name":   remoteName,
		"size":       asString(r.Info["size"]),
		"time":       asString(r.Info["time"]),
		"folder_id":  folderID,
		"share_url":  "",
		"share_pwd":  "",
		"shareReady": false,
	}

	cfg := cfgGet()
	if cfg.AutoShare && id != "" {
		if info, err := shareInfo(id, cookie); err == nil {
			result["share_url"] = info["share_url"]
			result["share_pwd"] = info["pwd"]
			result["shareReady"] = true
			pwd := firstNonEmpty(info["pwd"], cfg.SharePwd)
			if pwd == "" {
				pwd = "001"
			}
			if err := setSharePwd(id, pwd, cookie); err == nil {
				result["share_pwd"] = pwd
			}
		}
	}
	return result, nil
}

func writeUploadBody(mw *multipart.Writer, filePath, folderID string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	name := repairUploadName(filepath.Base(filePath))
	fw, err := mw.CreateFormFile("upload_file", name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	fields := map[string]string{
		"task":           "1",
		"vie":            "2",
		"ve":             "2",
		"id":             "WU_FILE_0",
		"name":           name,
		"type":           "application/octet-stream",
		"folder_id_bb_n": folderID,
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	return mw.Close()
}

func shareInfo(fileID, cookie string) (map[string]string, error) {
	body, err := doupload(cookie, url.Values{"task": {"22"}, "file_id": {fileID}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Zt   int    `json:"zt"`
		Info string `json:"info"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Zt != 1 {
		return nil, fmt.Errorf("获取分享信息失败")
	}
	info := map[string]string{"share_url": "", "pwd": ""}
	if m := reIfr.FindStringSubmatch(r.Info); len(m) > 1 {
		info["share_url"] = "https://www.lanzou.com/" + strings.TrimPrefix(m[1], "?")
	}
	if strings.Contains(info["share_url"], "fn?") {
		info["share_url"] = strings.Replace(info["share_url"], "fn?", "", 1)
	}
	if strings.Contains(r.Info, "pwd") {
		info["pwd"] = "001"
	}
	return info, nil
}

// shareInfoFolder 文件夹分享（task=18，文件夹不区分提取码，会员才可自定义）
func shareInfoFolder(folderID, cookie string) (map[string]string, error) {
	body, err := doupload(cookie, url.Values{"task": {"18"}, "folder_id": {folderID}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Zt   int    `json:"zt"`
		Info string `json:"info"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Zt != 1 {
		return nil, errors.New("获取文件夹分享信息失败")
	}
	info := map[string]string{"share_url": "", "pwd": ""}
	if m := reIfr.FindStringSubmatch(r.Info); len(m) > 1 {
		info["share_url"] = "https://www.lanzou.com/" + strings.TrimPrefix(m[1], "?")
	}
	info["share_url"] = strings.Replace(info["share_url"], "fn?", "", 1)
	return info, nil
}

func setSharePwd(fileID, pwd, cookie string) error {
	body, err := doupload(cookie, url.Values{
		"task": {"23"}, "file_id": {fileID}, "shows": {"1"}, "shownames": {"1"}, "pwd": {pwd},
	})
	if err != nil {
		return err
	}
	var r struct {
		Zt int `json:"zt"`
	}
	if json.Unmarshal(body, &r) != nil || r.Zt != 1 {
		return errors.New("设置提取码失败")
	}
	return nil
}

func mkdir(name, parentID, cookie string) (string, error) {
	if parentID == "0" || parentID == "" {
		parentID = "-1"
	}
	body, err := doupload(cookie, url.Values{
		"task": {"2"}, "folder_id": {parentID}, "folder_name": {name}, "folder_description": {""},
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Zt   int    `json:"zt"`
		Info string `json:"info"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("创建响应解析失败")
	}
	if r.Zt != 1 {
		return "", fmt.Errorf("创建文件夹失败：%s", r.Info)
	}
	return name, nil
}

func delObj(id, objType, cookie string) error {
	task := "6"
	if objType == "folder" {
		task = "3"
	}
	key := "file_id"
	if objType == "folder" {
		key = "folder_id"
	}
	body, err := doupload(cookie, url.Values{"task": {task}, key: {id}})
	if err != nil {
		return err
	}
	var r struct {
		Zt   int             `json:"zt"`
		Info json.RawMessage `json:"info"`
	}
	if json.Unmarshal(body, &r) != nil || r.Zt != 1 {
		return errors.New("删除失败：" + strings.TrimSpace(string(r.Info)))
	}
	return nil
}

// ======================================================================
// 重命名：优先原生接口，失败回退「下载→改上传→删原」
// ======================================================================

type renameCombo struct {
	Task  string
	Field string
}

var (
	renameMu      sync.Mutex
	fileRenameOK  *renameCombo
	dirRenameOK   *renameCombo
	fileRenameBad = false
	dirRenameBad  = false
)

var fileRenameCandidates = []renameCombo{
	{"46", "file_name"}, {"46", "name"}, {"36", "file_name"}, {"11", "file_name"},
}

var dirRenameCandidates = []renameCombo{
	{"4", "folder_name"}, {"4", "name"}, {"46", "folder_name"},
}

func findName(items []map[string]interface{}, id string) string {
	for _, it := range items {
		if asString(it["id"]) == id {
			return firstNonEmpty(asString(it["name_all"]), asString(it["name"]))
		}
	}
	return ""
}

func nativeRename(id, folderID, newName string, isDir bool, cookie string) error {
	renameMu.Lock()
	defer renameMu.Unlock()

	bad := fileRenameBad
	known := fileRenameOK
	cands := fileRenameCandidates
	if isDir {
		bad = dirRenameBad
		known = dirRenameOK
		cands = dirRenameCandidates
	}
	if bad {
		return errors.New("该账号已确认不支持原生重命名")
	}
	try := make([]renameCombo, 0, len(cands)+1)
	if known != nil {
		try = append(try, *known)
	} else {
		try = append(try, cands...)
	}

	for _, c := range try {
		form := url.Values{"task": {c.Task}}
		if isDir {
			form.Set("folder_id", id)
		} else {
			form.Set("file_id", id)
		}
		form.Set(c.Field, newName)
		body, err := doupload(cookie, form)
		if err != nil {
			if errors.Is(err, errCookie) {
				return err
			}
			continue
		}
		var r struct {
			Zt int `json:"zt"`
		}
		if json.Unmarshal(body, &r) != nil || (r.Zt != 1 && r.Zt != 0) {
			continue
		}
		items, _, err := fileList(folderID, cookie)
		if err != nil {
			continue
		}
		if findName(items, id) == newName {
			if isDir {
				dirRenameOK = &c
			} else {
				fileRenameOK = &c
			}
			return nil
		}
	}
	if isDir {
		dirRenameBad = true
	} else {
		fileRenameBad = true
	}
	return errors.New("原生重命名不可用")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ======================================================================
// 分享解析
// ======================================================================

func hostOf(rawURL string) (string, error) {
	pu, err := url.Parse(rawURL)
	if err != nil || pu.Host == "" {
		return "", errors.New("链接格式不正确")
	}
	if !hostAllowed(rawURL) {
		return "", fmt.Errorf("不是受支持的蓝奏域名：%s", pu.Host)
	}
	return pu.Host, nil
}

func getSharePage(shareURL string, cookie string) (string, error) {
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("GET", shareURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", "https://www.lanzou.com/")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return "", fmt.Errorf("访问分享链接失败：%v", err)
	}
	return string(body), nil
}

func reTParamValue(page string) string {
	if m := reTParam.FindStringSubmatch(page); len(m) > 1 {
		return m[1]
	}
	return ""
}

func parseShare(shareURL, pwd, cookie string) (map[string]interface{}, error) {
	if _, err := hostOf(shareURL); err != nil {
		return nil, err
	}
	page, err := getSharePage(shareURL, cookie)
	if err != nil {
		return nil, err
	}
	if strings.Contains(page, "取消分享") || strings.Contains(page, "文件不存在") {
		return nil, errors.New("分享已取消或文件不存在")
	}

	reIfr := regexp.MustCompile(`(?i)<iframe[^>]+src="?/fn\?([^"'#\s>]+)"?`)
	if m := reIfr.FindStringSubmatch(page); len(m) > 1 {
		param := m[1]
		fileID := ""
		host := ""
		for _, kv := range strings.Split(param, "&") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if parts[0] == "id" {
				fileID = parts[1]
			}
			if parts[0] == "host" {
				host = parts[1]
			}
		}
		if fileID != "" {
			return parseFileShare(fileID, host, pwd, shareURL, cookie)
		}
	}
	if m := reFid2.FindStringSubmatch(page); len(m) > 1 {
		return parseFileShare(strings.TrimSpace(m[1]), "", pwd, shareURL, cookie)
	}
	if m := reFolderID.FindStringSubmatch(page); len(m) > 1 {
		return parseFolderShare(m[1], pwd, shareURL, cookie)
	}
	return nil, errors.New("页面解析失败，可能是分享已失效或域名已更改")
}

func parseFileShare(fileID, host, pwd, shareURL, cookie string) (map[string]interface{}, error) {
	if host == "" {
		host, _ = hostOf(shareURL)
	}
	page, err := getSharePage(shareURL, cookie)
	if err != nil {
		return nil, err
	}
	t := reTParamValue(page)

	ajax := ""
	if m := reAjax.FindStringSubmatch(page); len(m) > 1 {
		ajax = m[1]
	}
	dom := ""
	if m := reDom.FindStringSubmatch(page); len(m) > 1 {
		dom = m[1]
	}
	if ajax == "" {
		return nil, errors.New("未获取到文件信息（分享页结构可能已变化）")
	}
	if dom == "" {
		dom = "https://" + host
	}

	ajaxURL := fmt.Sprintf("https://%s/ajaxm.php?file=%s", host, ajax)
	form := url.Values{"action": {"downprocess"}, "sign": {t}, "ves": {"1"}, "websign": {""}, "websignkey": {""}, "pwd": {pwd}}
	data, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", ajaxURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", shareURL)
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, fmt.Errorf("获取下载地址失败：%v", err)
	}
	var r struct {
		Zt   int    `json:"zt"`
		Dom  string `json:"dom"`
		URL  string `json:"url"`
		Inf  int    `json:"inf"`
		Info string `json:"info"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("解析下载地址失败：%s", strings.TrimSpace(string(data)))
	}
	if r.Zt != 1 {
		msg := firstNonEmpty(r.Info, "需要提取码或链接已失效")
		return nil, errors.New(msg)
	}
	title := ""
	if m := reTitleTag.FindStringSubmatch(page); len(m) > 1 {
		title = strings.TrimSpace(m[1])
		for _, suf := range []string{" - 蓝奏云", "- 蓝奏云", " - 蓝奏云网盘", "-蓝奏云", "_蓝奏云", " - 蓝奏云盘"} {
			title = strings.TrimSuffix(title, suf)
		}
		title = strings.TrimSpace(title)
	}
	directURL := r.URL
	if !strings.HasPrefix(directURL, "http") {
		directURL = r.Dom + "/file/" + r.URL
	}
	return map[string]interface{}{
		"type": "file", "id": fileID, "share_url": shareURL,
		"direct_url": directURL, "filename": title, "file_name": title,
	}, nil
}

func parseFolderShare(folderID, pwd, shareURL, cookie string) (map[string]interface{}, error) {
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("GET", shareURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", "https://www.lanzou.com/")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, fmt.Errorf("访问分享链接失败：%v", err)
	}
	sub := ""
	if m := reSubID.FindStringSubmatch(string(body)); len(m) > 2 {
		sub = m[2]
	} else {
		sub = folderID
	}
	form := url.Values{"task": {"47"}, "folder_id": {sub}, "pwd": {pwd}}
	if uid, _, err := sess.ensureIDs(cookie); err == nil {
		form.Set("uid", uid)
	}
	out, err := doupload(cookie, form)
	if err != nil {
		return nil, err
	}
	var r struct {
		Zt   int                      `json:"zt"`
		Text []map[string]interface{} `json:"text"`
	}
	if json.Unmarshal(out, &r) != nil {
		return nil, errors.New("文件夹分享解析失败")
	}
	if r.Zt != 1 {
		return nil, errors.New("提取码不正确或文件夹已失效")
	}
	list := make([]map[string]interface{}, 0, len(r.Text))
	for _, it := range r.Text {
		it["is_dir"] = true
		it["type"] = "folder"
		list = append(list, it)
	}
	folderName := ""
	if m := reTitleTag.FindStringSubmatch(string(body)); len(m) > 1 {
		folderName = strings.TrimSpace(m[1])
		for _, suf := range []string{" - 蓝奏云", "- 蓝奏云", " - 蓝奏云网盘", "-蓝奏云", "_蓝奏云", " - 蓝奏云盘"} {
			folderName = strings.TrimSuffix(folderName, suf)
		}
		folderName = strings.TrimSpace(folderName)
	}
	return map[string]interface{}{
		"type": "folder", "id": sub, "share_url": shareURL,
		"list": list, "files": list, "folder_name": folderName,
	}, nil
}

// ======================================================================
// 下载 / 直链
// ======================================================================

func downloadToTemp(directURL, hintName string) (string, error) {
	if !hostAllowed(directURL) {
		return "", fmt.Errorf("下载地址不在允许的域名内：%s", directURL)
	}
	resp, err := dlClient.Get(directURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "lanzougo-dl-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	_ = tmp.Close()
	_ = hintName
	return tmpPath, nil
}

func apiDownload(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(r.URL.Query().Get("url"))
	if u == "" {
		respErr(w, 400, "缺少 url 参数")
		return
	}
	target := u
	for hop := 0; hop < 6; hop++ {
		if !hostAllowed(target) {
			respErr(w, 400, "非法的下载地址（只允许蓝奏域名）")
			return
		}
		body, hdr, status, next, err := fetchOnce(target)
		if err != nil {
			respErr(w, 502, "下载失败："+err.Error())
			return
		}
		if isRedirect(status) && next != "" {
			target = next
			continue
		}

		if ct := hdr.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		disp := hdr.Get("Content-Disposition")
		if disp == "" {
			disp = `attachment; filename="lanzou-download"`
		}
		w.Header().Set("Content-Disposition", disp)
		if _, err := w.Write(body); err != nil {
			return
		}
		return
	}
	respErr(w, 502, "重定向次数过多")
}

func fetchOnce(target string) ([]byte, http.Header, int, string, error) {
	cookie := ""
	for attempt := 0; attempt < 6; attempt++ {
		req, err := http.NewRequest("GET", target, nil)
		if err != nil {
			return nil, nil, 0, "", err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", "https://www.lanzou.com/")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := apiClient.Do(req)
		if err != nil {
			return nil, nil, 0, "", err
		}
		hdr := resp.Header
		if isRedirect(resp.StatusCode) {
			loc := resp.Header.Get("Location")
			code := resp.StatusCode
			_ = resp.Body.Close()
			next := ""
			if loc != "" {
				if pu, e := resp.Request.URL.Parse(loc); e == nil {
					next = pu.String()
				}
			}
			return nil, hdr, code, next, nil
		}
		body, rerr := readBody(resp)
		if rerr != nil {
			return body, hdr, resp.StatusCode, "", rerr
		}
		if reArg1.Match(body) {
			next := ""
			for _, cand := range acwCandidates(string(body)) {
				if !strings.Contains(cookie, cand) {
					next = cand
					break
				}
			}
			if next == "" {
				return body, hdr, resp.StatusCode, "", errors.New("acw 挑战未通过")
			}
			cookie = appendCookie(cookie, "acw_sc__v2", next)
			continue
		}
		return body, hdr, resp.StatusCode, "", nil
	}
	return nil, nil, 0, "", errors.New("acw 挑战处理失败")
}

func isRedirect(code int) bool { return code >= 300 && code < 400 }

// ======================================================================
// HTTP 层
// ======================================================================

func writeJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func respOK(w http.ResponseWriter, msg string, data interface{}) {
	writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": msg, "data": data})
}

func respErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"code": code, "msg": msg})
}

// respLoginFail 登录/凭证类失败的统一出口（绝不回显 Cookie）
func respLoginFail(w http.ResponseWriter, detail string) {
	cfg := cfgGet()
	msg := "账号登录失败"
	if cfg.Username == "" {
		msg = "Cookie 已失效，且未配置账号密码，请先调用 /api/login 登录"
	}
	payload := map[string]interface{}{"code": 401, "msg": msg}
	if detail != "" {
		payload["detail"] = detail
	}
	writeJSON(w, 401, payload)
}

type bizFunc func(cookie string) (interface{}, error)

// runWithSession 执行业务函数：Cookie 失效自动重登并重试一次
func runWithSession(w http.ResponseWriter, fn bizFunc) {
	cookie, err := sess.ensureCookie()
	if err != nil {
		respLoginFail(w, err.Error())
		return
	}
	res, err := fn(cookie)
	if err != nil && errors.Is(err, errCookie) {
		sess.invalidate("Cookie 失效，正在重新登录")
		cookie2, err2 := sess.ensureCookie()
		if err2 != nil {
			respLoginFail(w, err2.Error())
			return
		}
		res, err = fn(cookie2)
	}
	if err != nil {
		if errors.Is(err, errCookie) {
			respLoginFail(w, "重登后 Cookie 仍然失效")
			return
		}
		respErr(w, 500, err.Error())
		return
	}
	respOK(w, "ok", res)
}

func apiList(w http.ResponseWriter, r *http.Request) {
	folder := strings.TrimSpace(r.URL.Query().Get("folder"))
	if folder == "" {
		folder = "0"
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		items, truncated, err := fileList(folder, cookie)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"folder": folder, "list": items, "truncated": truncated, "count": len(items)}, nil
	})
}

func apiStatus(w http.ResponseWriter, r *http.Request) {
	cookie, uid, _, _, valid := sess.snapshot()
	cfg := cfgGet()
	vip := sess.isVIP()
	vLevel, vExpire, vDays := func() (int, string, int) {
		_, l, e, d := sess.vipSnapshot()
		if l <= 0 {
			l = cfg.VIPLevel
		}
		if e == "" {
			e = cfg.VIPExpire
		}
		return l, e, d
	}()
	payload := map[string]interface{}{
		"version":         version,
		"valid":           valid && cookie != "",
		"username":        cfg.Username,
		"uid":             firstNonEmpty(uid, cfg.UID),
		"vip":             vip,
		"vip_level":       vLevel,
		"vip_expire":      vExpire,
		"vip_expire_days": vDays,
		"vip_checked_at":  cfg.VIPCheckedAt,
		"rename_mode":     sess.renameMode(),
		"upload_limit_mb": sess.effectiveLimitMB(),
		"auto_share":      cfg.AutoShare,
		"updated_at":      firstNonEmpty(cfg.UpdatedAt, ""),
		"has_password":    cfg.PasswordEnc != "",
		"cookie_masked":   maskToken(cookie),
		"allow_update":    cfg.AllowUpdate,
	}
	sess.mu.RLock()
	payload["last_error"] = sess.lastErr
	sess.mu.RUnlock()
	respOK(w, "ok", payload)
}

func maskToken(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return "sha256:" + fmt.Sprintf("%x", h[:4])
}

func apiAccount(w http.ResponseWriter, r *http.Request) {
	runWithSession(w, func(cookie string) (interface{}, error) {
		uid, vei, err := sess.ensureIDs(cookie)
		if err != nil {
			return nil, err
		}
		_, _, _, vip, _ := sess.snapshot()
		if cfgGet().VIP {
			vip = true
		}
		return map[string]interface{}{
			"uid": uid, "vei_len": len(vei), "vip": vip,
			"username": cfgGet().Username, "upload_limit_mb": sess.effectiveLimitMB(),
		}, nil
	})
}

// apiCheckVIP 主动用当前 Cookie 检测一次账号：是否 VIP、VIP 几、到期时间、上传上限、重命名机制
func apiCheckVIP(w http.ResponseWriter, r *http.Request) {
	m, err := sess.refreshAccount()
	if err != nil {
		respLoginFail(w, err.Error())
		return
	}
	respOK(w, "ok", map[string]interface{}{
		"uid":             m.UID,
		"vip":             m.VIP,
		"vip_level":       m.VIPLevel,
		"vip_expire":      m.VIPExpire,
		"vip_expire_days": m.VIPDays,
		"capacity":        m.Capacity,
		"rename_mode":     sess.renameMode(),
		"upload_limit_mb": sess.effectiveLimitMB(),
		"checked_at":      time.Now().Format(time.RFC3339),
	})
}

func apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	user := strings.TrimSpace(r.PostFormValue("username"))
	pass := r.PostFormValue("password")
	if user == "" || pass == "" {
		respErr(w, 400, "缺少 username 或 password")
		return
	}
	cookie, m, err := lanzouLogin(user, pass)
	if err != nil {
		sess.mu.Lock()
		sess.lastErr = err.Error()
		sess.mu.Unlock()
		writeJSON(w, 401, map[string]interface{}{
			"code": 401, "msg": "账号登录失败", "detail": err.Error(),
		})
		return
	}
	sess.setCookie(cookie, m)
	_ = cfgUpdate(func(c *Config) {
		c.Username = user
		c.PasswordEnc = encryptPwd(pass)
		c.Cookie = cookie
		c.Valid = true
		c.UID = m.UID
		c.VIP = m.VIP
		c.VIPLevel = m.VIPLevel
		c.VIPExpire = m.VIPExpire
		c.VIPCheckedAt = time.Now().Format(time.RFC3339)
		c.RenameMode = renameModeFor(m.VIP)
		c.UpdatedAt = time.Now().Format(time.RFC3339)
		if c.MaxUploadMB < defaultMaxUploadMB {
			c.MaxUploadMB = defaultMaxUploadMB
		}
	})
	respOK(w, "账号登录成功", map[string]interface{}{
		"uid":             m.UID,
		"username":        user,
		"vip":             m.VIP,
		"vip_level":       m.VIPLevel,
		"vip_expire":      m.VIPExpire,
		"vip_expire_days": m.VIPDays,
		"rename_mode":     sess.renameMode(),
		"upload_limit_mb": sess.effectiveLimitMB(),
	})
}

func apiLogout(w http.ResponseWriter, r *http.Request) {
	sess.mu.Lock()
	sess.cookie, sess.uid, sess.vei, sess.valid, sess.vip = "", "", "", false, false
	sess.mu.Unlock()
	_ = cfgUpdate(func(c *Config) {
		c.Cookie = ""
		c.Valid = false
		c.Username = ""
		c.PasswordEnc = ""
	})
	respOK(w, "已退出登录", nil)
}

func apiCookieSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	cookie := strings.TrimSpace(r.PostFormValue("cookie"))
	if cookie == "" {
		respErr(w, 400, "cookie 为空")
		return
	}
	m, err := fetchAccountMeta(cookie)
	if err != nil {
		respErr(w, 401, "Cookie 无效："+err.Error())
		return
	}
	sess.setCookie(cookie, m)
	respOK(w, "Cookie 已保存并验证通过", map[string]interface{}{
		"uid": m.UID, "vip": m.VIP, "vip_level": m.VIPLevel, "vip_expire": m.VIPExpire,
		"vip_expire_days": m.VIPDays, "rename_mode": renameModeFor(m.VIP),
		"upload_limit_mb": sess.effectiveLimitMB(),
	})
}

func apiLimits(w http.ResponseWriter, r *http.Request) {
	cfg := cfgGet()
	vip := sess.isVIP()
	level, _, _ := func() (int, string, int) {
		_, l, e, d := sess.vipSnapshot()
		if l <= 0 {
			l = cfg.VIPLevel
		}
		return l, e, d
	}()
	if r.Method == "GET" {
		respOK(w, "ok", map[string]interface{}{
			"vip":                vip,
			"vip_level":          level,
			"rename_mode":        sess.renameMode(),
			"max_upload_mb":      cfg.MaxUploadMB,
			"level_limits":       cfg.LevelLimits,
			"effective_limit_mb": sess.effectiveLimitMB(),
			"default_limit_mb":   defaultMaxUploadMB,
			"note":               "非 VIP 单文件固定 100MB；VIP 可用 max_upload_mb 或按等级 level_limits（如 1=200,2=500）调整",
		})
		return
	}
	_ = r.ParseForm()
	updated := false
	if v := r.PostFormValue("vip"); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		if err := cfgUpdate(func(c *Config) {
			c.VIP = b
			c.RenameMode = renameModeFor(b)
			if !b {
				c.VIPLevel = 0
			}
		}); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		sess.mu.Lock()
		sess.vip = b
		if !b {
			sess.vipLevel = 0
		}
		sess.mu.Unlock()
		updated = true
	}
	if lv := r.PostFormValue("vip_level"); lv != "" {
		n, err := strconv.Atoi(lv)
		if err != nil || n < 0 || n > 9 {
			respErr(w, 400, "vip_level 必须为 0-9")
			return
		}
		if err := cfgUpdate(func(c *Config) { c.VIPLevel = n }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		sess.mu.Lock()
		sess.vipLevel = n
		sess.mu.Unlock()
		updated = true
	}
	if mb := r.PostFormValue("max_upload_mb"); mb != "" {
		n, err := strconv.Atoi(mb)
		if err != nil || n <= 0 {
			respErr(w, 400, "max_upload_mb 必须是正整数")
			return
		}
		if !sess.isVIP() {
			respErr(w, 403, fmt.Sprintf("非 VIP 账号上传上限固定 %dMB，不可调整", defaultMaxUploadMB))
			return
		}
		if n < defaultMaxUploadMB {
			respErr(w, 400, fmt.Sprintf("VIP 上限不能低于非 VIP 的 %dMB", defaultMaxUploadMB))
			return
		}
		if err := cfgUpdate(func(c *Config) { c.MaxUploadMB = n }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		updated = true
	}
	if ll := r.PostFormValue("level_limits"); ll != "" {
		m := map[string]int{}
		for _, kv := range strings.Split(ll, ",") {
			kv = strings.TrimSpace(kv)
			if kv == "" {
				continue
			}
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				respErr(w, 400, "level_limits 格式应为 1=200,2=500")
				return
			}
			k, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			v, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 != nil || err2 != nil || k < 1 || k > 9 || v < defaultMaxUploadMB {
				respErr(w, 400, "level_limits 等级需 1-9，上限需 >= 100")
				return
			}
			m[strconv.Itoa(k)] = v
		}
		if err := cfgUpdate(func(c *Config) { c.LevelLimits = m }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		updated = true
	}
	if v := r.PostFormValue("auto_share"); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		if err := cfgUpdate(func(c *Config) { c.AutoShare = b }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		updated = true
	}
	if p := r.PostFormValue("share_pwd"); p != "" {
		if err := cfgUpdate(func(c *Config) { c.SharePwd = p }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		updated = true
	}
	if v := r.PostFormValue("allow_update"); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		if err := cfgUpdate(func(c *Config) { c.AllowUpdate = b }); err != nil {
			respErr(w, 500, "写入配置失败："+err.Error())
			return
		}
		updated = true
	}
	if !updated {
		respErr(w, 400, "没有需要更新的字段")
		return
	}
	respOK(w, "已更新", map[string]interface{}{
		"vip":                sess.isVIP(),
		"vip_level":          cfgGet().VIPLevel,
		"rename_mode":        sess.renameMode(),
		"max_upload_mb":      cfgGet().MaxUploadMB,
		"level_limits":       cfgGet().LevelLimits,
		"effective_limit_mb": sess.effectiveLimitMB(),
		"auto_share":         cfgGet().AutoShare,
		"allow_update":       cfgGet().AllowUpdate,
	})
}

func apiUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	rawPath := r.PostFormValue("path")
	filePath := strings.TrimSpace(rawPath)
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	if folder == "" {
		folder = "0"
	}
	if filePath == "" {
		respErr(w, 400, "缺少 path 参数")
		return
	}
	if !pathAllowed(filePath) {
		respErr(w, 403, "路径不在允许的上传目录内（可用 -upload-dir 追加白名单目录）")
		return
	}
	base := filepath.Base(rawPath)
	trimmed := strings.TrimSpace(base)
	if trimmed == "" || trimmed == "." || trimmed == string(filepath.Separator) {
		respErr(w, 400, "文件名不能为空白")
		return
	}
	if utf8.RuneCountInString(trimmed) < 2 {
		respErr(w, 400, "文件名至少需要 2 个字符（当前只有 1 个）")
		return
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		return uploadFile(filePath, folder, cookie)
	})
}

func apiMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.PostFormValue("name"))
	parent := strings.TrimSpace(r.PostFormValue("parent"))
	if name == "" {
		respErr(w, 400, "缺少 name 参数")
		return
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		got, err := mkdir(name, parent, cookie)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"name": got}, nil
	})
}

func apiDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	id := strings.TrimSpace(r.PostFormValue("id"))
	objType := strings.TrimSpace(r.PostFormValue("type"))
	if id == "" {
		respErr(w, 400, "缺少 id 参数")
		return
	}
	if objType == "" {
		objType = "file"
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		if err := delObj(id, objType, cookie); err != nil {
			return nil, err
		}
		return map[string]interface{}{"deleted": id, "type": objType}, nil
	})
}

func apiShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	fileID := strings.TrimSpace(r.PostFormValue("file_id"))
	pwd := r.PostFormValue("pwd")
	objType := strings.TrimSpace(r.PostFormValue("type"))
	if fileID == "" {
		respErr(w, 400, "缺少 file_id 参数")
		return
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		if objType == "folder" {
			info, err := shareInfoFolder(fileID, cookie)
			if err != nil {
				return nil, err
			}
			info["pwd"] = ""
			return info, nil
		}
		info, err := shareInfo(fileID, cookie)
		if err != nil {
			return nil, err
		}
		if pwd == "" {
			pwd = firstNonEmpty(cfgGet().SharePwd, "001")
		}
		if err := setSharePwd(fileID, pwd, cookie); err != nil {
			return nil, err
		}
		info["pwd"] = pwd
		return info, nil
	})
}

func apiParse(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(r.URL.Query().Get("url"))
	pwd := strings.TrimSpace(r.URL.Query().Get("pwd"))
	if u == "" {
		respErr(w, 400, "缺少 url 参数")
		return
	}
	res, err := parseShare(u, pwd, "")
	if err != nil {
		respErr(w, 400, err.Error())
		return
	}
	respOK(w, "ok", res)
}

func apiSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	shareURL := strings.TrimSpace(r.PostFormValue("url"))
	pwd := strings.TrimSpace(r.PostFormValue("pwd"))
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	fileID := strings.TrimSpace(r.PostFormValue("file_id"))
	directURL := strings.TrimSpace(r.PostFormValue("direct_url"))
	if folder == "" {
		folder = "0"
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		if directURL == "" {
			if shareURL == "" {
				return nil, errors.New("缺少 url 或 direct_url")
			}
			info, err := parseShare(shareURL, pwd, cookie)
			if err != nil {
				return nil, err
			}
			directURL = asString(info["direct_url"])
			if directURL == "" {
				return nil, errors.New("文件夹分享暂不支持转存")
			}
			fileID = asString(info["id"])
		}
		targetName := fileID
		if !strings.Contains(directURL, "/") || targetName == "" {
			targetName = "lanzou-file"
		}
		tmpPath, err := downloadToTemp(directURL, targetName)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpPath)
		up, err := uploadFile(tmpPath, folder, cookie)
		if err != nil {
			return nil, err
		}
		return up, nil
	})
}

func apiRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	fileID := strings.TrimSpace(r.PostFormValue("file_id"))
	newName := strings.TrimSpace(r.PostFormValue("name"))
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	if folder == "" {
		folder = "0"
	}
	if fileID == "" || newName == "" {
		respErr(w, 400, "缺少 file_id 或 name")
		return
	}
	if utf8.RuneCountInString(newName) < 2 {
		respErr(w, 400, "新文件名至少需要 2 个字符")
		return
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		mode := sess.renameMode()
		// mode=1（VIP）：优先调用蓝奏自带重命名
		if mode == 1 {
			if err := nativeRename(fileID, folder, newName, false, cookie); err == nil {
				return map[string]interface{}{"id": fileID, "name": newName, "method": "native", "rename_mode": 1}, nil
			} else if errors.Is(err, errCookie) {
				return nil, err
			}
		}
		// mode=0（非 VIP）或原生失败：下载 → 改名上传 → 删原
		oldPwd := ""
		if info, e := shareInfo(fileID, cookie); e == nil {
			oldPwd = info["pwd"]
		}
		res, err := reuploadRename(fileID, newName, folder, oldPwd, cookie)
		if err != nil {
			return nil, err
		}
		if mm, ok := res.(map[string]interface{}); ok {
			mm["rename_mode"] = mode
		}
		return res, nil
	})
}

func apiRenameFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	_ = r.ParseForm()
	id := strings.TrimSpace(r.PostFormValue("folder_id"))
	newName := strings.TrimSpace(r.PostFormValue("name"))
	parent := strings.TrimSpace(r.PostFormValue("parent"))
	if parent == "" {
		parent = "0"
	}
	if id == "" || newName == "" {
		respErr(w, 400, "缺少 folder_id 或 name")
		return
	}
	runWithSession(w, func(cookie string) (interface{}, error) {
		if err := nativeRename(id, parent, newName, true, cookie); err != nil {
			if errors.Is(err, errCookie) {
				return nil, err
			}
			return nil, errors.New("文件夹重命名失败：" + err.Error())
		}
		return map[string]interface{}{"id": id, "name": newName, "method": "native"}, nil
	})
}

func reuploadRename(fileID, newName, folder, pwd, cookie string) (interface{}, error) {
	info, err := shareInfo(fileID, cookie)
	if err != nil {
		return nil, err
	}
	shareURL := asString(info["share_url"])
	if shareURL == "" {
		return nil, errors.New("无法获取该文件的分享链接，改名前请先分享")
	}
	parsed, err := parseShare(shareURL, pwd, cookie)
	if err != nil {
		return nil, err
	}
	direct := asString(parsed["direct_url"])
	if direct == "" {
		return nil, errors.New("无法获取直链")
	}
	tmpPath, err := downloadToTemp(direct, newName)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)

	// 用一个名字完全正确（含后缀修复）的临时文件再上传，避免上传名带随机后缀
	dir, err := os.MkdirTemp("", "lanzougo-rename-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	remoteName := repairUploadName(sanitizeFileName(strings.TrimSpace(newName)))
	target := filepath.Join(dir, remoteName)
	if err := os.Rename(tmpPath, target); err != nil {
		if cerr := copyFile(tmpPath, target); cerr != nil {
			return nil, fmt.Errorf("准备上传文件失败：%v", cerr)
		}
	}

	up, err := uploadFile(target, folder, cookie)
	if err != nil {
		return nil, errors.New("重新上传失败（原文件仍保留）：" + err.Error())
	}
	if err := delObj(fileID, "file", cookie); err != nil {
		return nil, errors.New("新文件已上传，但删除原文件失败：" + err.Error())
	}
	if pwd != "" {
		if nid := asString(up["id"]); nid != "" {
			_ = setSharePwd(nid, pwd, cookie)
		}
	}
	return map[string]interface{}{
		"id":        asString(up["id"]),
		"name":      displayName(remoteName),
		"raw_name":  remoteName,
		"method":    "reupload",
		"pwd":       pwd,
		"share_url": up["share_url"],
	}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func sanitizeFileName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if s == "" {
		return "renamed"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// ======================================================================
// 检查更新 / 自更新（GitHub Release）
// ======================================================================

var ghClient = &http.Client{Timeout: 30 * time.Second}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt string    `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Assets      []ghAsset `json:"assets"`
}

func ghToken() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(cfgGet().GitHubToken)
}

func fetchLatestRelease() (*ghRelease, error) {
	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
	req, err := http.NewRequest("GET", api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LanzouGo/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	if t := ghToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := ghClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub 失败：%v", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case 200:
	case 404:
		return nil, errors.New("仓库还没有发布 Release")
	case 403, 429:
		// API 限流（共享 IP 常见）：退化为读 releases/latest 的 302 跳转取 tag
		if rel, e := fetchLatestTagByRedirect(); e == nil {
			return rel, nil
		}
		msg := "GitHub API 限流"
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &m) == nil && m.Message != "" {
			msg = m.Message
		}
		return nil, fmt.Errorf("%s（可在 lanzou.json 配置 github_token 或设置环境变量 GITHUB_TOKEN 提升额度）", msg)
	default:
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("解析 Release 失败：%v", err)
	}
	return &rel, nil
}

// fetchLatestTagByRedirect 不调 API，靠 /releases/latest 的跳转地址拿版本号
func fetchLatestTagByRedirect() (*ghRelease, error) {
	cl := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	u := fmt.Sprintf("https://github.com/%s/%s/releases/latest", repoOwner, repoName)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LanzouGo/"+version)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil, errors.New("无法获取最新版本号")
	}
	if strings.HasSuffix(strings.TrimSuffix(loc, "/"), "/releases") {
		return nil, errors.New("仓库还没有发布 Release")
	}
	tag := loc[strings.LastIndex(strings.TrimSuffix(loc, "/"), "/")+1:]
	if tag == "" || strings.Contains(tag, "latest") {
		return nil, errors.New("无法解析最新版本号")
	}
	return &ghRelease{TagName: tag, HTMLURL: loc}, nil
}

func parseVer(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "-", 2)[0]
	for i, part := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out[i] = n
	}
	return out
}

func versionNewer(cur, latest string) bool {
	a, b := parseVer(cur), parseVer(latest)
	for i := 0; i < 3; i++ {
		if b[i] > a[i] {
			return true
		}
		if b[i] < a[i] {
			return false
		}
	}
	return false
}

func pickAsset(rel *ghRelease) *ghAsset {
	tag := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	for i := range rel.Assets {
		if strings.Contains(strings.ToLower(rel.Assets[i].Name), tag) {
			return &rel.Assets[i]
		}
	}
	for i := range rel.Assets {
		n := strings.ToLower(rel.Assets[i].Name)
		if strings.Contains(n, runtime.GOOS) && strings.Contains(n, runtime.GOARCH) {
			return &rel.Assets[i]
		}
	}
	return nil
}

type updateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	HasUpdate   bool   `json:"has_update"`
	PublishedAt string `json:"published_at"`
	ReleaseURL  string `json:"release_url"`
	Notes       string `json:"notes"`
	AssetName   string `json:"asset_name"`
	AssetSize   int64  `json:"asset_size"`
	CheckedAt   string `json:"checked_at"`
	AllowUpdate bool   `json:"allow_update"`
}

func buildUpdateInfo() (*updateInfo, error) {
	rel, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	info := &updateInfo{
		Current:     version,
		Latest:      rel.TagName,
		HasUpdate:   versionNewer(version, rel.TagName),
		PublishedAt: rel.PublishedAt,
		ReleaseURL:  rel.HTMLURL,
		Notes:       rel.Body,
		CheckedAt:   time.Now().Format(time.RFC3339),
		AllowUpdate: cfgGet().AllowUpdate,
	}
	if a := pickAsset(rel); a != nil {
		info.AssetName = a.Name
		info.AssetSize = a.Size
	}
	return info, nil
}

func downloadAsset(a *ghAsset) ([]byte, error) {
	req, err := http.NewRequest("GET", a.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LanzouGo/"+version)
	resp, err := ghClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载更新包失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("下载更新包失败 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return nil, err
	}
	if len(data) < 1024 {
		return nil, errors.New("更新包内容异常（过小）")
	}
	if a.Digest != "" && strings.HasPrefix(a.Digest, "sha256:") {
		sum := sha256.Sum256(data)
		if got := fmt.Sprintf("%x", sum[:]); got != strings.TrimPrefix(a.Digest, "sha256:") {
			return nil, errors.New("更新包校验失败（sha256 不匹配）")
		}
	}
	return data, nil
}

func applyUpdate(data []byte) (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	if real, e := filepath.EvalSymlinks(exe); e == nil {
		exe = real
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return "", "", fmt.Errorf("写入新版本失败：%v", err)
	}
	_ = os.Chmod(tmp, 0755)
	if err := os.Rename(tmp, exe); err != nil {
		return exe, tmp, fmt.Errorf("无法覆盖正在运行的程序（可手动替换 %s）：%v", tmp, err)
	}
	return exe, tmp, nil
}

func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir, _ = os.Getwd()
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func apiVersion(w http.ResponseWriter, r *http.Request) {
	info, err := buildUpdateInfo()
	if err != nil {
		writeJSON(w, 502, map[string]interface{}{
			"code": 502, "msg": err.Error(),
			"data": map[string]interface{}{"current": version, "allow_update": cfgGet().AllowUpdate},
		})
		return
	}
	respOK(w, "ok", info)
}

func apiUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respErr(w, 405, "请使用 POST")
		return
	}
	if !cfgGet().AllowUpdate {
		respErr(w, 403, "自更新已关闭（可在 /api/limits POST allow_update=1 开启）")
		return
	}
	_ = r.ParseForm()
	if r.PostFormValue("confirm") != "1" {
		respErr(w, 400, "需要 confirm=1 才会真正执行更新")
		return
	}
	rel, err := fetchLatestRelease()
	if err != nil {
		respErr(w, 502, err.Error())
		return
	}
	if !versionNewer(version, rel.TagName) {
		respOK(w, "已是最新版本", map[string]interface{}{"current": version, "latest": rel.TagName})
		return
	}
	a := pickAsset(rel)
	if a == nil {
		respErr(w, 400, fmt.Sprintf("最新 Release 没有适配 %s-%s 的二进制，请手动更新", runtime.GOOS, runtime.GOARCH))
		return
	}
	data, err := downloadAsset(a)
	if err != nil {
		respErr(w, 502, err.Error())
		return
	}
	exe, tmp, err := applyUpdate(data)
	if err != nil {
		respErr(w, 500, err.Error())
		return
	}
	respOK(w, "更新完成，正在重启", map[string]interface{}{
		"from": version, "to": rel.TagName, "binary": exe, "backup": tmp,
	})
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if err := restartSelf(); err != nil {
			fmt.Printf("[warn] 自动重启失败（%v），请手动重启服务\n", err)
		}
	}()
}

func apiHelp(w http.ResponseWriter, r *http.Request) {
	respOK(w, "lanzou v"+version, map[string]interface{}{
		"apis": map[string]string{
			"GET  /api/status":        "账号状态（不含 Cookie）",
			"GET  /api/check_vip":     "强制检测 Cookie：VIP 状态 / 等级 / 到期时间 / 上传上限",
			"GET  /api/version":       "检查更新（对比 GitHub Release）",
			"POST /api/update":        "自更新（需 confirm=1）",
			"GET  /api/account":       "账号信息（uid / vip / 上传上限）",
			"POST /api/login":         "username&password 登录并自动保存 Cookie",
			"POST /api/logout":        "退出并清除账号密码与 Cookie",
			"POST /api/cookie":        "cookie=... 手动写入 Cookie（会自动验证）",
			"GET  /api/limits":        "查看上传限制",
			"POST /api/limits":        "VIP 可调 max_upload_mb；也可 auto_share/share_pwd",
			"GET  /api/list":          "?folder=0 文件列表（Cookie 失效自动重登）",
			"POST /api/upload":        "path=&folder=0 上传（非 VIP 单文件 100MB）",
			"POST /api/mkdir":         "name=&parent=0 新建文件夹",
			"POST /api/delete":        "id=&type=file|folder 删除",
			"POST /api/share":         "file_id=&pwd= 生成分享",
			"GET  /api/parse":         "?url=&pwd= 解析分享链接",
			"GET  /api/download":      "?url= 代理下载（仅蓝奏域名）",
			"POST /api/save":          "url=&pwd=&folder= 转存",
			"POST /api/rename":        "file_id=&name=&folder= 重命名（优先原生，失败回退重传）",
			"POST /api/rename_folder": "folder_id=&name=&parent= 文件夹重命名",
		},
		"version": version,
	})
}

// ======================================================================
// 上传目录白名单
// ======================================================================

var uploadRoots []string

func initUploadRoots(extra []string) {
	uploadRoots = nil
	if wd, err := os.Getwd(); err == nil {
		uploadRoots = append(uploadRoots, wd)
	}
	uploadRoots = append(uploadRoots, os.TempDir())
	for _, e := range extra {
		for _, part := range strings.Split(e, ",") {
			if p := strings.TrimSpace(part); p != "" {
				uploadRoots = append(uploadRoots, p)
			}
		}
	}
}

func pathAllowed(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	if real, err2 := filepath.EvalSymlinks(abs); err2 == nil {
		abs = real
	}
	for _, root := range uploadRoots {
		ra, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if rr, err2 := filepath.EvalSymlinks(ra); err2 == nil {
			ra = rr
		}
		if abs == ra || strings.HasPrefix(abs, ra+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// ======================================================================
// 入口
// ======================================================================

func main() {
	port := "8080"
	listen := "127.0.0.1"
	uploadDir := ""
	token := ""
	maxUpload := 0
	checkUpdateFlag := false
	updateFlag := false
	noUpdate := false

	for i, a := range os.Args {
		switch a {
		case "-port":
			if i+1 < len(os.Args) {
				port = os.Args[i+1]
			}
		case "-listen":
			if i+1 < len(os.Args) {
				listen = os.Args[i+1]
			}
		case "-cfg":
			if i+1 < len(os.Args) {
				cfgPath = os.Args[i+1]
			}
		case "-upload-dir":
			if i+1 < len(os.Args) {
				uploadDir = os.Args[i+1]
			}
		case "-token":
			if i+1 < len(os.Args) {
				token = os.Args[i+1]
			}
		case "-check-update":
			checkUpdateFlag = true
		case "-update":
			updateFlag = true
		case "-no-update":
			noUpdate = true
		case "-maxupload":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil && n > 0 {
					maxUpload = n
				}
			}
		}
	}

	initUploadRoots([]string{uploadDir})

	if err := loadCfg(); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[warn] 读取配置失败：%v\n", err)
	}
	if token != "" {
		cfgTok := cfgGet().Token
		if cfgTok == "" {
			_ = cfgUpdate(func(c *Config) { c.Token = token })
		}
	}
	if maxUpload > 0 {
		_ = cfgUpdate(func(c *Config) {
			if maxUpload > c.MaxUploadMB {
				c.MaxUploadMB = maxUpload
			}
		})
	}
	if noUpdate {
		_ = cfgUpdate(func(c *Config) { c.AllowUpdate = false })
	}

	if checkUpdateFlag || updateFlag {
		info, err := buildUpdateInfo()
		if err != nil {
			fmt.Printf("检查更新失败：%v\n", err)
			return
		}
		fmt.Printf("当前版本：%s\n最新版本：%s\n是否可更新：%v\n发布信息：%s\n", info.Current, info.Latest, info.HasUpdate, info.ReleaseURL)
		if updateFlag && info.HasUpdate && info.AllowUpdate {
			rel, err := fetchLatestRelease()
			if err != nil {
				fmt.Printf("更新失败：%v\n", err)
				return
			}
			a := pickAsset(rel)
			if a == nil {
				fmt.Printf("该 Release 没有适配 %s-%s 的二进制\n", runtime.GOOS, runtime.GOARCH)
				return
			}
			data, err := downloadAsset(a)
			if err != nil {
				fmt.Printf("更新失败：%v\n", err)
				return
			}
			if _, _, err := applyUpdate(data); err != nil {
				fmt.Printf("更新失败：%v\n", err)
				return
			}
			fmt.Println("更新完成，请重启服务")
		}
		return
	}

	cfg := cfgGet()
	if cfg.Cookie != "" {
		if m, err := fetchAccountMeta(cfg.Cookie); err == nil {
			sess.setCookie(cfg.Cookie, m)
		} else {
			fmt.Printf("[warn] 已保存的 Cookie 不可用（%v），将尝试用账号密码自动登录\n", err)
			if cfg.Username != "" {
				if _, err := sess.login(); err != nil {
					fmt.Printf("[warn] 自动登录失败：%v\n", err)
				}
			}
		}
	} else if cfg.Username != "" {
		if _, err := sess.login(); err != nil {
			fmt.Printf("[warn] 自动登录失败：%v\n", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", apiStatus)
	mux.HandleFunc("/api/check_vip", apiCheckVIP)
	mux.HandleFunc("/api/version", apiVersion)
	mux.HandleFunc("/api/update", apiUpdate)
	mux.HandleFunc("/api/account", apiAccount)
	mux.HandleFunc("/api/login", apiLogin)
	mux.HandleFunc("/api/logout", apiLogout)
	mux.HandleFunc("/api/cookie", apiCookieSet)
	mux.HandleFunc("/api/limits", apiLimits)
	mux.HandleFunc("/api/list", apiList)
	mux.HandleFunc("/api/upload", apiUpload)
	mux.HandleFunc("/api/mkdir", apiMkdir)
	mux.HandleFunc("/api/delete", apiDelete)
	mux.HandleFunc("/api/share", apiShare)
	mux.HandleFunc("/api/parse", apiParse)
	mux.HandleFunc("/api/save", apiSave)
	mux.HandleFunc("/api/rename", apiRename)
	mux.HandleFunc("/api/rename_folder", apiRenameFolder)
	mux.HandleFunc("/api/download", apiDownload)
	mux.HandleFunc("/api/help", apiHelp)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok := cfgGet().Token; tok != "" {
			got := r.Header.Get("X-Token")
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
				writeJSON(w, 401, map[string]interface{}{"code": 401, "msg": "未授权（缺少或错误的 token）"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})

	_, _, _, vip, valid := sess.snapshot()
	fmt.Printf("LanzouGo v%s 启动：监听 %s:%s（%s），账号状态：%v，VIP：%v，上传上限：%dMB\n",
		version, listen, port, cfgPath, valid, vip, sess.effectiveLimitMB())
	if err := http.ListenAndServe(listen+":"+port, handler); err != nil {
		fmt.Printf("服务启动失败：%v\n", err)
	}
}
