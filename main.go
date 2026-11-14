/*
 * LanzouGo v1.0.0
 * 蓝奏云三合一管理服务：上传 + 文件列表 + 分享链接解析直链
 * 统一使用同一个 Cookie（phpdisk_info），启动自动检测有效性，无效返回"Cookie异常"
 * 原生 Go 标准库实现，无任何第三方依赖，内存占用低（适配 2G 服务器）
 * 提供 HTTP JSON 接口，支持 PHP 直接调用
 *
 * 接口：
 *   POST /api/cookie                写入Cookie（自动检测，有效才保存）
 *   GET  /api/status                查看当前Cookie状态
 *   GET  /api/list?folder=0         获取网盘文件/文件夹列表
 *   POST /api/upload                上传文件（同机模式：path=服务器本地文件路径）
 *   GET  /api/parse?url=..&pwd=..   解析蓝奏云分享链接为直链
 *   GET  /api/help                  接口说明
 *
 * 命令行：
 *   ./lanzou-server -cookie "phpdisk_info=xxx" -port 8080
 */
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const version = "1.0.0"

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

const apiUpHost = "https://up.woozooo.com"

// 新版蓝奏接口域名（2025+ 改版后文件列表/上传均走 pc.woozooo.com）
const apiHost = "https://pc.woozooo.com"

// uid / vei 签名缓存（从 mydisk.php 页面提取，进程内缓存）
var cachedUid, cachedVei string

// getUidVei 访问 mydisk.php 提取 uid 与 vei（新版接口签名参数）
func getUidVei(cookie string) (string, string, error) {
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
		return "", "", err
	}
	s := string(body)
	m := regexp.MustCompile(`uid=([^'"&;]+)`).FindStringSubmatch(s)
	if len(m) < 2 {
		return "", "", fmt.Errorf("uid 提取失败")
	}
	m2 := regexp.MustCompile(`'vei':'([^']+)'`).FindStringSubmatch(s)
	if len(m2) < 2 {
		return "", "", fmt.Errorf("vei 提取失败")
	}
	return m[1], m2[1], nil
}

// ensureUidVei 确保已缓存 uid/vei，未缓存则抓取
func ensureUidVei(cookie string) error {
	if cachedUid != "" && cachedVei != "" {
		return nil
	}
	uid, vei, err := getUidVei(cookie)
	if err != nil {
		return err
	}
	cachedUid, cachedVei = uid, vei
	return nil
}

// ---------- 配置 ----------

type Config struct {
	Cookie    string `json:"cookie"`
	UpdatedAt string `json:"updated_at"`
	Valid     bool   `json:"valid"`
}

var cfgPath = "lanzou.json"
var conf Config
var client *http.Client

// ---------- 工具 ----------

func initClient() {
	client = &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 手动处理重定向（解析分享链接需要抓 iframe 地址）
			return http.ErrUseLastResponse
		},
	}
}

// ---------- acw_sc__v2 反爬挑战处理（蓝奏云接口前置验证） ----------

var findAcwScV2Reg = regexp.MustCompile(`arg1='([0-9A-Z]+)'`)

// CalcAcwScV2 根据挑战页内嵌的 arg1 计算出 acw_sc__v2 cookie 值
func CalcAcwScV2(html string) (string, error) {
	acwScV2s := findAcwScV2Reg.FindStringSubmatch(html)
	if len(acwScV2s) != 2 {
		return "", fmt.Errorf("无法匹配acw_sc__v2")
	}
	return HexXor(Unbox(acwScV2s[1]), "3000176000856006061501533003690027800375"), nil
}

func Unbox(hex string) string {
	var box = []int{6, 28, 34, 31, 33, 18, 30, 23, 9, 8, 19, 38, 17, 24, 0, 5, 32, 21, 10, 22, 25, 14, 15, 3, 16, 27, 13, 35, 2, 29, 11, 26, 4, 36, 1, 39, 37, 7, 20, 12}
	var newBox = make([]byte, len(hex))
	for i := 0; i < len(box); i++ {
		j := box[i]
		if len(newBox) > j {
			newBox[j] = hex[i]
		}
	}
	return string(newBox)
}

func HexXor(hex1, hex2 string) string {
	var out strings.Builder
	out.Grow(len(hex1))
	for i := 0; i+2 <= len(hex1) && i+2 <= len(hex2); i += 2 {
		v1, _ := strconv.ParseInt(hex1[i:i+2], 16, 64)
		v2, _ := strconv.ParseInt(hex2[i:i+2], 16, 64)
		// 必须补足前导 0，否则异或结果 < 0x10 时只输出一个字符，导致整个 hex 串错位
		fmt.Fprintf(&out, "%02x", v1^v2)
	}
	return out.String()
}

// doReq 执行请求并自动处理 acw_sc__v2 反爬挑战（挑战页→算cookie→带cookie重试，最多3次）
func doReq(makeReq func(cookie string) (*http.Request, error), cookie string) ([]byte, error) {
	for i := 0; i < 3; i++ {
		req, err := makeReq(cookie)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 蓝奏部分域名（如 developer4.lanrar.com）无视 Accept-Encoding 强制返回 gzip
		if resp.Header.Get("Content-Encoding") == "gzip" {
			if gz, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
				body, _ = io.ReadAll(gz)
				gz.Close()
			}
		}
		if findAcwScV2Reg.Match(body) {
			v, err := CalcAcwScV2(string(body))
			if err != nil {
				return body, err
			}
			cookie = cookie + "; acw_sc__v2=" + v
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("acw_sc__v2 挑战处理失败")
}

// doReqTimeout 带自定义超时的 doReq（用于多域名回退探测）
func doReqTimeout(makeReq func(cookie string) (*http.Request, error), cookie string, timeout time.Duration) ([]byte, error) {
	tmp := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for i := 0; i < 3; i++ {
		req, err := makeReq(cookie)
		if err != nil {
			return nil, err
		}
		resp, err := tmp.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 蓝奏部分域名（如 developer4.lanrar.com）无视 Accept-Encoding 强制返回 gzip
		if resp.Header.Get("Content-Encoding") == "gzip" {
			if gz, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
				body, _ = io.ReadAll(gz)
				gz.Close()
			}
		}
		if findAcwScV2Reg.Match(body) {
			v, err := CalcAcwScV2(string(body))
			if err != nil {
				return body, err
			}
			cookie = cookie + "; acw_sc__v2=" + v
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("acw_sc__v2 挑战处理失败")
}

func writeJSON(w http.ResponseWriter, code int, obj interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(obj)
}

func respOK(w http.ResponseWriter, msg string, data interface{}) {
	writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": msg, "data": data})
}

func respCookieErr(w http.ResponseWriter, detail string) {
	writeJSON(w, 401, map[string]interface{}{"code": 401, "msg": "Cookie异常", "detail": detail})
}

func loadCfg() {
	if b, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(b, &conf)
	}
}

func saveCfg() {
	b, _ := json.MarshalIndent(conf, "", "  ")
	os.WriteFile(cfgPath, b, 0600)
}

// ---------- 模块1：Cookie检测（统一入口） ----------

// checkCookie 通过新版接口验证Cookie是否有效
// 有效返回 (true, "有效")；无效返回 (false, 原因)
func checkCookie(cookie string) (bool, string) {
	if err := ensureUidVei(cookie); err != nil {
		return false, "uid/vei 获取失败: " + err.Error()
	}
	// 拉取根目录第一页文件验证
	form := url.Values{}
	form.Set("task", "5")
	form.Set("folder_id", "0")
	form.Set("pg", "1")
	form.Set("vei", cachedVei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return false, "网络请求失败: " + err.Error()
	}

	var r struct {
		Text json.RawMessage `json:"text"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return false, "响应解析失败（可能是网络劫持或接口改版）"
	}
	low := strings.ToLower(string(body))
	if strings.Contains(low, "login") || strings.Contains(low, "未登录") || strings.Contains(low, "登录") {
		return false, "未登录或Cookie已失效"
	}
	return true, "有效"
}

// ---------- 模块2：文件列表（新版接口 task=47 文件夹 + task=5 文件分页） ----------

// fileList 获取文件夹下文件/文件夹列表（文件夹在前，文件在后）
// folderID: 0/-1=根目录，或文件夹ID
func fileList(folderID, cookie string) ([]map[string]interface{}, error) {
	if err := ensureUidVei(cookie); err != nil {
		return nil, err
	}
	// 根目录：文件夹列表用 -1，文件列表用 0；子文件夹直接用正值 ID
	folderListID := folderID
	fileListID := folderID
	if folderID == "0" || folderID == "" {
		folderListID = "-1"
		fileListID = "0"
	}
	result := make([]map[string]interface{}, 0)

	// 1. 文件夹列表 task=47
	folderForm := url.Values{}
	folderForm.Set("task", "47")
	folderForm.Set("folder_id", folderListID)
	folderForm.Set("vei", cachedVei)
	folderBody, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(folderForm.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, err
	}
	var fr struct {
		Text []map[string]interface{} `json:"text"`
	}
	if err := json.Unmarshal(folderBody, &fr); err == nil {
		for _, f := range fr.Text {
			f["is_dir"] = true
			f["type"] = "folder"
			// 文件夹 ID 直接用蓝奏正值 fol_id
			if _, ok := f["id"]; !ok {
				if folID, ok2 := f["fol_id"].(string); ok2 {
					f["id"] = folID
				} else if folID, ok2 := f["fol_id"].(float64); ok2 {
					f["id"] = fmt.Sprintf("%d", int(folID))
				}
			}
			if _, ok := f["name_all"]; !ok {
				f["name_all"] = f["name"]
			}
			result = append(result, f)
		}
	}

	// 2. 文件列表 task=5（分页循环，参数名 folder_id）
	for pg := 1; pg <= 20; pg++ {
		fileForm := url.Values{}
		fileForm.Set("task", "5")
		fileForm.Set("folder_id", fileListID)
		fileForm.Set("pg", fmt.Sprintf("%d", pg))
		fileForm.Set("vei", cachedVei)
		fileBody, err := doReq(func(c string) (*http.Request, error) {
			req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
				strings.NewReader(fileForm.Encode()))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("User-Agent", UA)
			req.Header.Set("Referer", apiHost+"/mydisk.php")
			req.Header.Set("Cookie", c)
			return req, nil
		}, cookie)
		if err != nil {
			return nil, err
		}
		var xr struct {
			Text []map[string]interface{} `json:"text"`
		}
		if err := json.Unmarshal(fileBody, &xr); err != nil {
			break
		}
		if len(xr.Text) == 0 {
			break
		}
		for _, f := range xr.Text {
			f["is_dir"] = false
			f["type"] = "file"
			if _, ok := f["name"]; !ok {
				f["name"] = f["name_all"]
			}
			result = append(result, f)
		}
		if len(xr.Text) < 60 { // 蓝奏单页约 60 条，不足则到底
			break
		}
	}
	return result, nil
}

// ---------- 模块3：文件上传（对应 transfer lanzous 逻辑） ----------

// uploadFile 上传本地文件到指定文件夹（新版 html5up.php 接口），上传成功后自动开启分享并返回分享链接
func uploadFile(filePath, folderID, cookie string) (map[string]interface{}, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("本地文件不存在: %s", filePath)
	}
	if folderID == "0" || folderID == "" {
		folderID = "-1"
	}
	if err := ensureUidVei(cookie); err != nil {
		return nil, err
	}

	data, err := doReq(func(c string) (*http.Request, error) {
		body := new(bytes.Buffer)
		mw := multipart.NewWriter(body)
		fw, err := mw.CreateFormFile("upload_file", filepath.Base(filePath))
		if err != nil {
			return nil, err
		}
		f, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err := io.Copy(fw, f); err != nil {
			return nil, err
		}
		mw.WriteField("task", "1")
		mw.WriteField("vie", "2")
		mw.WriteField("ve", "2")
		mw.WriteField("id", "WU_FILE_0")
		mw.WriteField("name", filepath.Base(filePath))
		mw.WriteField("folder_id_bb_n", folderID)
		mw.Close()

		req, err := http.NewRequest("POST", apiHost+"/html5up.php?uid="+cachedUid, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, err
	}

	var r struct {
		Text []map[string]interface{} `json:"text"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("上传响应解析失败: %s", string(data[:min(len(data), 200)]))
	}
	if len(r.Text) == 0 {
		return nil, fmt.Errorf("上传失败，蓝奏云可能限制文件类型/大小: %s", string(data[:min(len(data), 160)]))
	}
	item := r.Text[0]

	result := map[string]interface{}{
		"name":      item["name_all"],
		"id":        item["id"],
		"size":      item["size"],
		"time":      item["time"],
		"share_url": "",
	}

	// 上传成功后自动开启分享并设置提取码 001
	if id, ok := item["id"].(string); ok && id != "" {
		if si, err := shareInfo(id, false, cookie); err == nil {
			result["share_url"] = si["share_url"]
			result["f_id"] = si["f_id"]
			if pwd, _ := si["pwd"].(string); pwd != "" {
				result["pwd"] = pwd
			}
			// 统一设置提取码为 001（用户要求：上传的所有文件密码都改成001，只改一次）
			if err2 := setSharePwd(id, false, "001", cookie); err2 == nil {
				result["pwd"] = "001"
			}
		}
	}
	return result, nil
}

// shareInfo 通过ID获取文件/文件夹分享信息（新版 task=22 文件 / task=18 文件夹）
func shareInfo(id string, isDir bool, cookie string) (map[string]interface{}, error) {
	if err := ensureUidVei(cookie); err != nil {
		return nil, err
	}
	form := url.Values{}
	if isDir {
		form.Set("task", "18")
		form.Set("folder_id", id)
	} else {
		form.Set("task", "22")
		form.Set("file_id", id)
	}
	form.Set("vei", cachedVei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return nil, err
	}
	var r struct {
		Zt   int `json:"zt"`
		Info struct {
			Pwd    string `json:"pwd"`
			Onof   string `json:"onof"`
			FID    string `json:"f_id"`
			NewURL string `json:"new_url"`
			IsNewd string `json:"is_newd"`
			Name   string `json:"name"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		// 未分享的文件夹/文件，蓝奏返回 HTML 错误页（"文件取消分享"/405），按未分享处理
		if bytes.Contains(body, []byte("取消分享")) || bytes.Contains(body, []byte("405")) || bytes.Contains(body, []byte("<html")) {
			return map[string]interface{}{
				"pwd":       "",
				"onof":      "0",
				"is_dir":    isDir,
				"share_url": "",
				"name":      "",
			}, nil
		}
		return nil, fmt.Errorf("分享信息解析失败: %s", string(body[:min(len(body), 160)]))
	}
	if r.Zt != 1 {
		return nil, fmt.Errorf("获取分享信息失败（zt=%d）", r.Zt)
	}
	result := map[string]interface{}{
		"pwd":    r.Info.Pwd,
		"onof":   r.Info.Onof,
		"is_dir": isDir,
		"name":   r.Info.Name,
	}
	if r.Info.Onof != "1" {
		// 未开启分享：无分享链接
		result["share_url"] = ""
		return result, nil
	}
	if isDir {
		result["share_url"] = r.Info.NewURL
	} else {
		result["f_id"] = r.Info.FID
		result["share_url"] = r.Info.IsNewd + "/" + r.Info.FID
	}
	return result, nil
}

// setSharePwd 设置文件/文件夹提取码（新版 task=23 文件 / task=16 文件夹）
func setSharePwd(id string, isDir bool, pwd, cookie string) error {
	if err := ensureUidVei(cookie); err != nil {
		return err
	}
	form := url.Values{}
	if isDir {
		form.Set("task", "16")
		form.Set("folder_id", id)
	} else {
		form.Set("task", "23")
		form.Set("file_id", id)
	}
	form.Set("shows", "1")
	form.Set("shownames", pwd)
	form.Set("vei", cachedVei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return err
	}
	var r struct {
		Zt int `json:"zt"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("设置密码失败，响应: %s", string(body[:min(len(body), 160)]))
	}
	if r.Zt != 1 {
		return fmt.Errorf("设置密码失败（zt=%d）", r.Zt)
	}
	return nil
}

// ---------- 模块4：分享链接解析（新版接口） ----------

var (
	reAjaxFile     = regexp.MustCompile(`ajaxfile\.php\?file=(\d+)`)
	reIsngis       = regexp.MustCompile(`isngis\s*=\s*'([^']+)'`)
	reFileMoreAjax = regexp.MustCompile(`filemoreajax\.php\?file=(\d+)`)
	// 蓝奏分享页 t/k 的 JS 变量名每次刷新随机，按值特征匹配：t=10位时间戳、k=32位md5
	reTParam     = regexp.MustCompile(`var\s+\w+\s*=\s*'(\d{10})'`)
	reKParam     = regexp.MustCompile(`var\s+\w+\s*=\s*'([a-f0-9]{32})'`)
	rePuid       = regexp.MustCompile(`'puid'\s*:\s*'([^']+)'`)
	reFolderMark = regexp.MustCompile(`filemoreajax|id="infos"`)
	reTitle      = regexp.MustCompile(`<title>([^<]+)</title>`)
	// 分享文件夹内嵌套的子文件夹：分享页 HTML 的 folderlink/mbxfolder 链接（子文件夹有独立分享ID）
	reSubFolder = regexp.MustCompile(`(?i)(?:folderlink|mbxfolder).+?href="/([^"/]+)"(?:.+?filename")?>(.+?)<`)
	// 蓝奏直链"验证并下载"页参数（网络异常验证）
	reDownFile = regexp.MustCompile(`'file':'([^']+)'`)
	reDownSign = regexp.MustCompile(`'sign':'([^']+)'`)
	// 兼容老版 ajaxm 流程
	reFileID  = regexp.MustCompile(`'file_id'\s*[:=]\s*'([^']+)'`)
	reSign    = regexp.MustCompile(`'sign'\s*[:=]\s*'([^']+)'`)
	reWebsign = regexp.MustCompile(`'websign'\s*[:=]\s*'([^']+)'`)
	reWebkey  = regexp.MustCompile(`'websignkey'\s*[:=]\s*'([^']+)'`)
	reIframe  = regexp.MustCompile(`<iframe[^>]+src="([^"]+)"`)
)

// getSharePage 访问分享页并处理 acw_sc__v2 挑战，返回页面 HTML 与 acw cookie
func getSharePage(shareURL string) (string, string, error) {
	acwCookie := ""
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest("GET", shareURL, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", "https://pc.woozooo.com/")
		if acwCookie != "" {
			req.Header.Set("Cookie", "acw_sc__v2="+acwCookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("访问分享链接失败: %s", err.Error())
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return "", "", fmt.Errorf("分享链接不存在或已被删除")
		}
		// 蓝奏"文件取消分享"提示页（分享可能被取消/风控下线）
		if bytes.Contains(b, []byte("取消分享")) || bytes.Contains(b, []byte("来晚啦")) {
			return "", "", fmt.Errorf("该分享已被取消或链接已失效")
		}
		if findAcwScV2Reg.Match(b) {
			v, err := CalcAcwScV2(string(b))
			if err != nil {
				return "", "", err
			}
			acwCookie = v
			continue
		}
		return string(b), acwCookie, nil
	}
	return "", "", fmt.Errorf("分享页访问失败（反爬挑战处理异常）")
}

// parseShare 解析蓝奏云分享链接
// 返回：
//
//	type=file:   {type:"file", file_id, filename, direct_url}
//	type=folder: {type:"folder", folder_name, files:[{id,name,size,time,url}]}
func parseShare(shareURL, pwd string) (map[string]interface{}, error) {
	if !strings.Contains(shareURL, "lanzou") {
		return nil, fmt.Errorf("不是有效的蓝奏云分享链接")
	}
	// 文件夹分享页返回的文件链接可能带 webpage 签名参数，剥离后按普通文件分享解析
	if idx := strings.Index(shareURL, "?webpage="); idx > 0 {
		shareURL = shareURL[:idx]
	}
	htmlStr, acwCookie, err := getSharePage(shareURL)
	if err != nil {
		return nil, err
	}
	// 文件夹分享页特征
	if reFolderMark.MatchString(htmlStr) {
		return parseFolderShare(shareURL, htmlStr, pwd, acwCookie)
	}
	return parseFileShare(shareURL, htmlStr, pwd, acwCookie)
}

// parseFileShare 解析文件分享页（新版 ajaxfile.php 流程，兼容老 ajaxm）
func parseFileShare(shareURL, htmlStr, pwd, acwCookie string) (map[string]interface{}, error) {
	m := reAjaxFile.FindStringSubmatch(htmlStr)
	fileID := ""
	if len(m) > 1 {
		fileID = m[1]
	}
	sign := firstMatch(reIsngis, htmlStr)
	if fileID == "" || sign == "" {
		// 兼容老版本 ajaxm 流程
		return parseShareLegacy(shareURL, htmlStr, pwd, acwCookie)
	}

	form := url.Values{}
	form.Set("action", "downprocess")
	form.Set("sign", sign)
	form.Set("kd", "1")
	form.Set("p", pwd)
	// ajaxfile 接口域名：优先页面里写明的完整域名（通常 apifile.woozooo.com），
	// 该域名 DNS 可能解析到不可达节点，失败回退 developer4.lanrar.com（同集群可达）
	pageHost := ""
	if m := regexp.MustCompile(`https?://([^/"']+)/ajax(?:file|m)\.php\?file=\d+`).FindStringSubmatch(htmlStr); len(m) > 1 {
		pageHost = m[1]
	} else {
		pageHost = hostOf(shareURL)
	}
	hosts := []string{pageHost, "developer4.lanrar.com"}
	acw := acwCookie
	if acw == "" {
		acw = acwCookieOf(shareURL)
	}
	var lastErr error
	// 提取码候选：先用调用方给的密码，失败再自动兜底蓝奏默认提取码 001
	pwds := []string{pwd}
	if pwd != "001" {
		pwds = append(pwds, "001")
	}
	for _, pp := range pwds {
		form.Set("p", pp)
		for _, h := range hosts {
			if h == "" {
				continue
			}
			ajaxURL := "https://" + h + "/ajaxfile.php?file=" + fileID
			data, err := doReqTimeout(func(c string) (*http.Request, error) {
				req, err := http.NewRequest("POST", ajaxURL, strings.NewReader(form.Encode()))
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("User-Agent", UA)
				req.Header.Set("Referer", shareURL)
				req.Header.Set("X-Requested-With", "XMLHttpRequest")
				req.Header.Set("Cookie", c)
				return req, nil
			}, "acw_sc__v2="+acw, 10*time.Second)
			if err != nil {
				lastErr = err
				continue
			}
			var r struct {
				Zt  int    `json:"zt"`
				Dom string `json:"dom"`
				URL string `json:"url"`
				Inf string `json:"inf"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				os.WriteFile("/tmp/aj_debug.log", data, 0600)
				bodyStr := string(data)
				if strings.Contains(bodyStr, "提取码") || strings.Contains(bodyStr, "密码") {
					lastErr = fmt.Errorf("提取码错误或链接已失效")
				} else {
					lastErr = fmt.Errorf("下载信息解析失败（接口可能改版）")
				}
				continue
			}
			if r.Zt != 1 || r.Dom == "" || r.URL == "" {
				os.WriteFile("/tmp/aj_debug.log", data, 0600)
				lastErr = fmt.Errorf("未获取到下载链接（提取码错误或链接失效）")
				continue
			}
			directURL := r.Dom + "/file/" + r.URL
			return map[string]interface{}{
				"type":       "file",
				"file_id":    fileID,
				"filename":   r.Inf,
				"direct_url": directURL,
			}, nil
		}
	}
	return nil, lastErr
}

// parseFolderShare 解析文件夹分享页（新版 filemoreajax.php 流程）
func parseFolderShare(shareURL, htmlStr, pwd, acwCookie string) (map[string]interface{}, error) {
	m := reFileMoreAjax.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil, fmt.Errorf("文件夹页面解析失败（接口可能改版）")
	}
	fid := m[1]
	t := firstMatch(reTParam, htmlStr)
	k := firstMatch(reKParam, htmlStr)
	puid := firstMatch(rePuid, htmlStr)
	folderName := firstMatch(reTitle, htmlStr)
	host := hostOf(shareURL)
	if host == "" {
		return nil, fmt.Errorf("分享链接域名解析失败")
	}

	form := url.Values{}
	form.Set("lx", "2")
	form.Set("fid", fid)
	form.Set("uid", cachedUid)
	form.Set("puid", puid)
	form.Set("pg", "1")
	form.Set("rep", "0")
	form.Set("t", t)
	form.Set("k", k)
	form.Set("up", "1")
	form.Set("ls", "1")
	form.Set("pwd", pwd)
	ajaxURL := "https://" + host + "/filemoreajax.php?file=" + fid
	acw := acwCookie
	if acw == "" {
		acw = acwCookieOf(shareURL)
	}
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", ajaxURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", shareURL)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Cookie", c)
		return req, nil
	}, "acw_sc__v2="+acw)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Zt   int             `json:"zt"`
		Info string          `json:"info"`
		Text json.RawMessage `json:"text"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("文件夹文件列表解析失败: %s", string(body[:min(len(body), 160)]))
	}
	if raw.Zt == 3 {
		return nil, fmt.Errorf("提取码错误")
	}
	if raw.Zt != 1 && raw.Zt != 2 {
		return nil, fmt.Errorf("获取文件列表失败（zt=%d）%s", raw.Zt, raw.Info)
	}
	// zt=2 / text 为 "no file" 字符串：空文件夹，按空列表处理
	var textArr []map[string]interface{}
	if err := json.Unmarshal(raw.Text, &textArr); err != nil {
		textArr = nil
	}
	rr := struct {
		Zt   int                      `json:"zt"`
		Info string                   `json:"info"`
		Text []map[string]interface{} `json:"text"`
	}{raw.Zt, raw.Info, textArr}
	files := make([]map[string]interface{}, 0, len(rr.Text))
	for _, f := range rr.Text {
		idStr, _ := f["id"].(string)
		if idStr == "-1" || idStr == "" {
			continue
		}
		files = append(files, map[string]interface{}{
			"id":   idStr,
			"name": f["name_all"],
			"size": f["size"],
			"time": f["time"],
			"url":  "https://" + host + "/" + idStr,
		})
	}
	// 嵌套子文件夹：从分享页 HTML 提取（folderlink/mbxfolder 链接，子文件夹有独立分享ID）
	subFolders := reSubFolder.FindAllStringSubmatch(htmlStr, -1)
	for _, sf := range subFolders {
		if len(sf) < 3 || sf[1] == "" {
			continue
		}
		files = append(files, map[string]interface{}{
			"id":     sf[1],
			"name":   strings.TrimSpace(sf[2]),
			"size":   "",
			"time":   "",
			"url":    "https://" + host + "/" + sf[1],
			"is_dir": true,
		})
	}
	return map[string]interface{}{
		"type":        "folder",
		"folder_id":   fid,
		"folder_name": folderName,
		"files":       files,
	}, nil
}

// parseShareLegacy 兼容老版本 ajaxm.php 流程
func parseShareLegacy(shareURL, htmlStr, pwd, acwCookie string) (map[string]interface{}, error) {
	fileID := firstMatch(reFileID, htmlStr)
	sign := firstMatch(reSign, htmlStr)
	websign := firstMatch(reWebsign, htmlStr)
	websignkey := firstMatch(reWebkey, htmlStr)
	if fileID == "" || sign == "" {
		return nil, fmt.Errorf("页面解析失败：未提取到签名参数（链接可能需密码或接口改版）")
	}
	ajaxURL := "https://" + hostOf(shareURL) + "/ajaxm.php?file=" + fileID + "&pwd=" + url.QueryEscape(pwd)
	form := url.Values{}
	form.Set("file", fileID)
	form.Set("action", "downprocess")
	form.Set("sign", sign)
	form.Set("pwd", pwd)
	form.Set("websign", websign)
	form.Set("websignkey", websignkey)
	acw := acwCookie
	if acw == "" {
		acw = acwCookieOf(shareURL)
	}
	data, err := doReq(func(c string) (*http.Request, error) {
		req3, err := http.NewRequest("POST", ajaxURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req3.Header.Set("User-Agent", UA)
		req3.Header.Set("Referer", shareURL)
		req3.Header.Set("Cookie", c)
		return req3, nil
	}, conf.Cookie+"; acw_sc__v2="+acw)
	if err != nil {
		return nil, err
	}
	var r struct {
		Dom string `json:"dom"`
		URL string `json:"url"`
		Inf string `json:"inf"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("下载信息解析失败（接口可能改版）")
	}
	if r.Dom == "" || r.URL == "" {
		return nil, fmt.Errorf("未获取到下载链接，可能链接失效或需密码")
	}
	directURL := r.Dom
	if strings.HasPrefix(r.URL, "/") {
		directURL += r.URL
	} else {
		directURL += "/file/" + r.URL
	}
	return map[string]interface{}{
		"type":       "file",
		"file_id":    fileID,
		"filename":   r.Inf,
		"direct_url": directURL,
	}, nil
}

// acwCookieOf 记录分享页 acw cookie（由 getSharePage 内部维护，这里从 html 无法取回，
// 直接返回空字符串，ajaxfile/ajaxm 请求不依赖 acw 也能成功）
func acwCookieOf(u string) string {
	return ""
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func hostOf(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return pu.Host
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// negID 蓝奏文件夹 ID 取负（task=47 返回正值 fol_id，实际文件夹 ID 为负值）
func negID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "-") {
		return s
	}
	if s == "0" || s == "-1" {
		return s
	}
	return "-" + s
}

// ---------- HTTP 接口（PHP 直接调用） ----------

func apiCookieSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	cookie := strings.TrimSpace(r.PostFormValue("cookie"))
	if cookie == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少cookie参数"})
		return
	}
	// 自动检测
	ok, detail := checkCookie(cookie)
	if !ok {
		respCookieErr(w, detail)
		return
	}
	conf.Cookie = cookie
	conf.Valid = true
	conf.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	saveCfg()
	respOK(w, "Cookie已写入并验证有效", map[string]interface{}{
		"cookie":     cookie,
		"updated_at": conf.UpdatedAt,
	})
}

func apiStatus(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" {
		respOK(w, "尚未配置Cookie", map[string]interface{}{"valid": false})
		return
	}
	if !conf.Valid {
		respCookieErr(w, "Cookie已失效，请通过 /api/cookie 重新写入")
		return
	}
	respOK(w, "Cookie状态", map[string]interface{}{
		"valid":      true,
		"cookie":     conf.Cookie,
		"updated_at": conf.UpdatedAt,
	})
}

// mkdir 创建文件夹（新版 task=2）
func mkdir(folderName, parentID, cookie string) (string, error) {
	if err := ensureUidVei(cookie); err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("task", "2")
	form.Set("parent_id", parentID)
	form.Set("folder_name", folderName)
	form.Set("folder_description", "")
	form.Set("vei", cachedVei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return "", err
	}
	var r struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("创建失败，响应: %s", string(body[:min(len(body), 160)]))
	}
	return r.Text, nil
}

// delObj 删除文件/文件夹（新版 task=6 文件 / task=3 文件夹）
func delObj(id string, isDir bool, cookie string) error {
	if err := ensureUidVei(cookie); err != nil {
		return err
	}
	form := url.Values{}
	if isDir {
		form.Set("task", "3")
		form.Set("folder_id", id)
	} else {
		form.Set("task", "6")
		form.Set("file_id", id)
	}
	form.Set("vei", cachedVei)
	body, err := doReq(func(c string) (*http.Request, error) {
		req, err := http.NewRequest("POST", apiHost+"/doupload.php?uid="+cachedUid,
			strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", apiHost+"/mydisk.php")
		req.Header.Set("Cookie", c)
		return req, nil
	}, cookie)
	if err != nil {
		return err
	}
	var r struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("删除失败，响应: %s", string(body[:min(len(body), 160)]))
	}
	if r.Code != 0 {
		return fmt.Errorf("删除失败 code=%d", r.Code)
	}
	return nil
}

func apiList(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "0"
	}
	list, err := fileList(folder, conf.Cookie)
	if err != nil {
		// 列表接口失败可能因Cookie失效
		ok, d := checkCookie(conf.Cookie)
		if !ok {
			conf.Valid = false
			saveCfg()
			respCookieErr(w, d)
			return
		}
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "获取列表失败", "detail": err.Error()})
		return
	}
	respOK(w, "文件列表", map[string]interface{}{"folder": folder, "count": len(list), "list": list})
}

func apiUpload(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	filePath := strings.TrimSpace(r.PostFormValue("path"))
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	if filePath == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少path参数（服务器本地文件路径）"})
		return
	}
	if folder == "" {
		folder = "0"
	}
	result, err := uploadFile(filePath, folder, conf.Cookie)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "上传失败", "detail": err.Error()})
		return
	}
	respOK(w, "上传成功", result)
}

func apiMkdir(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	name := strings.TrimSpace(r.PostFormValue("name"))
	parent := strings.TrimSpace(r.PostFormValue("folder"))
	if name == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少name参数"})
		return
	}
	if parent == "" || parent == "0" {
		parent = "-1"
	}
	newID, err := mkdir(name, parent, conf.Cookie)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "创建文件夹失败", "detail": err.Error()})
		return
	}
	// 蓝奏创建返回正值，即为文件夹 ID
	respOK(w, "创建成功", map[string]interface{}{"folder_id": newID, "name": name})
}

func apiDelete(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	id := strings.TrimSpace(r.PostFormValue("id"))
	isDir := strings.TrimSpace(r.PostFormValue("type")) == "folder"
	if id == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少id参数"})
		return
	}
	if err := delObj(id, isDir, conf.Cookie); err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "删除失败", "detail": err.Error()})
		return
	}
	respOK(w, "删除成功", nil)
}

func apiShare(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	id := strings.TrimSpace(r.PostFormValue("id"))
	isDir := strings.TrimSpace(r.PostFormValue("type")) == "folder"
	pwd := strings.TrimSpace(r.PostFormValue("pwd"))
	if id == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少id参数"})
		return
	}
	// 可选：设置提取码后再返回分享信息
	if pwd != "" {
		if err := setSharePwd(id, isDir, pwd, conf.Cookie); err != nil {
			writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "设置提取码失败", "detail": err.Error()})
			return
		}
	}
	si, err := shareInfo(id, isDir, conf.Cookie)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "获取分享信息失败", "detail": err.Error()})
		return
	}
	respOK(w, "分享信息", si)
}

// ---------- 模块10：重命名 / 转存 ----------

// safeName 文件名安全化（去路径分隔符）
func safeName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "file"
	}
	return s
}

// downloadToTemp 下载直链到临时目录（保留期望文件名），返回临时文件路径
func downloadToTemp(directURL, filename string) (string, error) {
	if directURL == "" {
		return "", fmt.Errorf("直链为空")
	}
	dir, err := os.MkdirTemp("", "lzdl_")
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, safeName(filename))
	f, err := os.Create(tmp)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	req, err := http.NewRequest("GET", directURL, nil)
	if err != nil {
		f.Close()
		os.RemoveAll(dir)
		return "", err
	}
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Referer", "https://pc.woozooo.com/")
	resp, err := client.Do(req)
	if err != nil {
		f.Close()
		os.RemoveAll(dir)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		f.Close()
		os.RemoveAll(dir)
		return "", fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.RemoveAll(dir)
		return "", err
	}
	f.Close()
	return tmp, nil
}

// apiRename 重命名文件：下载→改名上传→删原→保留提取码
func apiRename(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	id := strings.TrimSpace(r.PostFormValue("id"))
	newname := strings.TrimSpace(r.PostFormValue("newname"))
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	if id == "" || newname == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少id/newname参数"})
		return
	}
	if folder == "" {
		folder = "0"
	}
	if folder == "0" {
		folder = "-1"
	}

	// 1. 获取原分享信息（拿到分享链接与提取码）
	si, err := shareInfo(id, false, conf.Cookie)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "获取原分享信息失败", "detail": err.Error()})
		return
	}
	oldPwd, _ := si["pwd"].(string)
	shareURL, _ := si["share_url"].(string)
	if shareURL == "" {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "该文件未开启分享，无法重命名（请先开启分享）"})
		return
	}

	// 2. 解析直链
	pr, err := parseShare(shareURL, oldPwd)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "解析原文件失败", "detail": err.Error()})
		return
	}
	directURL, _ := pr["direct_url"].(string)

	// 3. 下载到服务器临时文件（文件名用新名）
	tmpPath, err := downloadToTemp(directURL, newname)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "下载原文件失败", "detail": err.Error()})
		return
	}
	defer os.RemoveAll(filepath.Dir(tmpPath))

	// 4. 以新名字上传到原文件夹（自动分享+提取码001）
	up, err := uploadFile(tmpPath, folder, conf.Cookie)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "上传失败", "detail": err.Error()})
		return
	}
	newID, _ := up["id"].(string)

	// 5. 删除原文件
	if err := delObj(id, false, conf.Cookie); err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "删除原文件失败", "detail": err.Error()})
		return
	}

	// 6. 保留原提取码
	if newID != "" && oldPwd != "" {
		if err2 := setSharePwd(newID, false, oldPwd, conf.Cookie); err2 == nil {
			up["pwd"] = oldPwd
		}
	}
	respOK(w, "重命名成功", up)
}

// apiSave 转存分享文件/文件夹到指定文件夹（下载→上传）
func apiSave(w http.ResponseWriter, r *http.Request) {
	if conf.Cookie == "" || !conf.Valid {
		respCookieErr(w, "未配置有效Cookie")
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]interface{}{"code": 405, "msg": "请使用POST"})
		return
	}
	r.ParseForm()
	shareURL := strings.TrimSpace(r.PostFormValue("url"))
	pwd := strings.TrimSpace(r.PostFormValue("pwd"))
	folder := strings.TrimSpace(r.PostFormValue("folder"))
	if shareURL == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少url参数"})
		return
	}
	if folder == "" {
		folder = "0"
	}
	if folder == "0" {
		folder = "-1"
	}

	pr, err := parseShare(shareURL, pwd)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "解析失败", "detail": err.Error()})
		return
	}

	// 文件转存
	if pr["type"] == "file" {
		directURL, _ := pr["direct_url"].(string)
		filename, _ := pr["filename"].(string)
		if filename == "" {
			filename = "file"
		}
		tmpPath, err := downloadToTemp(directURL, filename)
		if err != nil {
			writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "下载失败", "detail": err.Error()})
			return
		}
		defer os.RemoveAll(filepath.Dir(tmpPath))
		up, err := uploadFile(tmpPath, folder, conf.Cookie)
		if err != nil {
			writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "转存上传失败", "detail": err.Error()})
			return
		}
		respOK(w, "转存成功", map[string]interface{}{"mode": "file", "total": 1, "ok": 1, "saved": []interface{}{up}})
		return
	}

	// 文件夹转存：逐个文件 下载→上传
	files, _ := pr["files"].([]interface{})
	if len(files) == 0 {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "分享文件夹为空"})
		return
	}
	saved := make([]interface{}, 0, len(files))
	failed := make([]map[string]interface{}, 0)
	for _, fRaw := range files {
		f, _ := fRaw.(map[string]interface{})
		fURL, _ := f["url"].(string)
		fName, _ := f["name"].(string)
		if fURL == "" {
			failed = append(failed, map[string]interface{}{"name": fName, "ok": false, "err": "无分享短链"})
			continue
		}
		// 文件夹内文件用其自身分享短链解析（无密码时默认 001）
		fpwd := pwd
		if fpwd == "" {
			fpwd = "001"
		}
		fpr, err := parseShare(fURL, fpwd)
		if err != nil {
			failed = append(failed, map[string]interface{}{"name": fName, "ok": false, "err": err.Error()})
			continue
		}
		directURL, _ := fpr["direct_url"].(string)
		if directURL == "" {
			failed = append(failed, map[string]interface{}{"name": fName, "ok": false, "err": "无直链"})
			continue
		}
		tmpPath, err := downloadToTemp(directURL, fName)
		if err != nil {
			failed = append(failed, map[string]interface{}{"name": fName, "ok": false, "err": err.Error()})
			continue
		}
		up, err := uploadFile(tmpPath, folder, conf.Cookie)
		os.RemoveAll(filepath.Dir(tmpPath))
		if err != nil {
			failed = append(failed, map[string]interface{}{"name": fName, "ok": false, "err": err.Error()})
			continue
		}
		up["ok"] = true
		saved = append(saved, up)
	}
	respOK(w, "文件夹转存完成", map[string]interface{}{
		"mode":   "folder",
		"total":  len(files),
		"ok":     len(saved),
		"failed": failed,
		"saved":  saved,
	})
}

func apiParse(w http.ResponseWriter, r *http.Request) {
	shareURL := strings.TrimSpace(r.URL.Query().Get("url"))
	pwd := r.URL.Query().Get("pwd")
	if shareURL == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少url参数"})
		return
	}
	result, err := parseShare(shareURL, pwd)
	if err != nil {
		writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "解析失败", "detail": err.Error()})
		return
	}
	respOK(w, "解析成功", result)
}

// apiDownload 流式代理蓝奏直链下载：处理 acw_sc__v2 挑战与 302 跳转，文件流不落盘
func apiDownload(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(r.URL.Query().Get("url"))
	if u == "" {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "缺少url参数"})
		return
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "url必须以http开头"})
		return
	}
	// 仅允许蓝奏系域名，防止开放代理
	if !strings.Contains(u, "lanrar.com") && !strings.Contains(u, "lanzou") && !strings.Contains(u, "woozooo.com") {
		writeJSON(w, 400, map[string]interface{}{"code": 400, "msg": "仅允许蓝奏云下载链接"})
		return
	}
	acw := ""
	cur := u
	for i := 0; i < 4; i++ {
		req, err := http.NewRequest("GET", cur, nil)
		if err != nil {
			writeJSON(w, 500, map[string]interface{}{"code": 500, "msg": "请求构造失败", "detail": err.Error()})
			return
		}
		req.Header.Set("User-Agent", UA)
		req.Header.Set("Referer", "https://pc.woozooo.com/")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Encoding", "gzip, deflate")
		if acw != "" {
			req.Header.Set("Cookie", "acw_sc__v2="+acw)
		}
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "获取下载失败", "detail": err.Error()})
			return
		}
		// 3xx 手动跟随
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if loc == "" {
				writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "下载重定向失败"})
				return
			}
			cur = loc
			continue
		}
		// 循环读到 64KB 或 EOF，用于判断 acw 挑战 / 验证页（响应可能被 CDN 强制 gzip，
		// 且验证页 JS（down_r/ajax.php）位于 HTML 尾部，只读前 8KB 会漏判透传 HTML）
		buf := make([]byte, 0, 65536)
		tmp := make([]byte, 8192)
		for len(buf) < 65536 {
			n, rerr := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		chunk := buf
		challenge := ""
		verifyBody := ""
		if resp.Header.Get("Content-Encoding") == "gzip" {
			if gz, gerr := gzip.NewReader(bytes.NewReader(chunk)); gerr == nil {
				dec, _ := io.ReadAll(gz)
				gz.Close()
				if findAcwScV2Reg.Match(dec) {
					rest, _ := io.ReadAll(resp.Body)
					gz2, gerr2 := gzip.NewReader(bytes.NewReader(append(chunk, rest...)))
					if gerr2 == nil {
						full, _ := io.ReadAll(gz2)
						gz2.Close()
						challenge = string(full)
					}
				} else if strings.Contains(string(dec), "ajax.php") && strings.Contains(string(dec), "down_r") {
					rest, _ := io.ReadAll(resp.Body)
					gz2, gerr2 := gzip.NewReader(bytes.NewReader(append(chunk, rest...)))
					if gerr2 == nil {
						full, _ := io.ReadAll(gz2)
						gz2.Close()
						verifyBody = string(full)
					}
				}
			}
		} else {
			s := string(chunk)
			if findAcwScV2Reg.Match(chunk) {
				rest, _ := io.ReadAll(resp.Body)
				challenge = string(append(chunk, rest...))
			} else if strings.Contains(s, "ajax.php") && strings.Contains(s, "down_r") {
				rest, _ := io.ReadAll(resp.Body)
				verifyBody = string(append(chunk, rest...))
			}
		}
		if challenge != "" {
			resp.Body.Close()
			v, err := CalcAcwScV2(challenge)
			if err != nil {
				writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "反爬挑战处理失败", "detail": err.Error()})
				return
			}
			acw = v
			continue
		}
		if verifyBody != "" {
			// 蓝奏"验证并下载"页：POST ajax.php 换取最终下载地址。
			// 实测：ajax.php 不能带 Referer（带了必 SignError），且需连续重试数次才会返回真实下载地址。
			resp.Body.Close()
			f := firstMatch(reDownFile, verifyBody)
			sign := firstMatch(reDownSign, verifyBody)
			if f == "" || sign == "" {
				writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "下载验证参数解析失败"})
				return
			}
			ajaxURL := "https://" + hostOf(cur) + "/file/ajax.php"
			form := url.Values{}
			form.Set("file", f)
			form.Set("el", "2")
			form.Set("sign", sign)
			finalURL := ""
			for attempt := 0; attempt < 6; attempt++ {
				aj, err := doReq(func(c string) (*http.Request, error) {
					req, err := http.NewRequest("POST", ajaxURL, strings.NewReader(form.Encode()))
					if err != nil {
						return nil, err
					}
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					req.Header.Set("User-Agent", UA)
					req.Header.Set("X-Requested-With", "XMLHttpRequest")
					req.Header.Set("Cookie", c)
					// 注意：不设置 Referer（蓝奏校验 Referer，带任意 Referer 均返回 SignError）
					return req, nil
				}, "acw_sc__v2="+acw)
				if err == nil {
					var vr struct {
						Zt  int    `json:"zt"`
						URL string `json:"url"`
						Inf string `json:"inf"`
					}
					if json.Unmarshal(aj, &vr) == nil && vr.Zt == 1 && strings.HasPrefix(vr.URL, "http") {
						finalURL = vr.URL
						break
					}
				}
				time.Sleep(300 * time.Millisecond)
			}
			if finalURL == "" {
				writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "下载验证失败（蓝奏风控未通过）"})
				return
			}
			cur = finalURL
			continue
		}
		// 正常文件流：透传响应头（剔除 hop-by-hop），已读部分回填后流式转发
		for k, vs := range resp.Header {
			switch http.CanonicalHeaderKey(k) {
			case "Transfer-Encoding", "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Upgrade", "Content-Encoding", "Content-Length":
				// Content-Encoding/Content-Length 由本代理重建，避免下游（PHP）丢失编码头导致文件损坏
				continue
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		var bodyReader io.Reader = io.MultiReader(bytes.NewReader(chunk), resp.Body)
		if resp.Header.Get("Content-Encoding") == "gzip" {
			// gzip 文件流：解压后转发（蓝奏文本文件常以 gzip 传输，下游无法保留编码头）
			if gz, gerr := gzip.NewReader(io.MultiReader(bytes.NewReader(chunk), resp.Body)); gerr == nil {
				bodyReader = gz
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, bodyReader)
		resp.Body.Close()
		return
	}
	writeJSON(w, 502, map[string]interface{}{"code": 502, "msg": "下载重试次数超限"})
}

func apiHelp(w http.ResponseWriter, r *http.Request) {
	help := map[string]interface{}{
		"name":    "LanzouGo",
		"version": version,
		"apis": map[string]string{
			"POST /api/cookie": "body: cookie=phpdisk_info=xxx （自动检测，有效才写入）",
			"GET  /api/status": "查看当前Cookie状态",
			"GET  /api/list":   "?folder=0 获取文件列表（0=根目录，也可填文件夹ID）",
			"POST /api/upload": "body: path=服务器本地文件路径&folder=0 上传并自动开启分享+设置提取码001",
			"POST /api/mkdir":  "body: name=文件夹名&folder=0 创建文件夹",
			"POST /api/delete": "body: id=文件/文件夹ID&type=file|folder 删除",
			"GET  /api/parse":  "?url=分享链接&pwd=密码 解析直链（支持文件/文件夹分享链接）",
			"POST /api/share":  "body: id=文件/文件夹ID&type=file|folder&pwd=新提取码(可选) 获取/设置分享信息",
			"POST /api/rename": "body: id=文件ID&newname=新名字&folder=所在文件夹 重命名（下载→上传→删原，保留提取码）",
			"POST /api/save":   "body: url=分享链接&pwd=提取码&folder=目标文件夹 转存分享文件/文件夹到网盘",
			"PHP 调用":           "见 php_example.php",
		},
	}
	writeJSON(w, 200, help)
}

func withCookieCheck(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/cookie") ||
			strings.HasPrefix(r.URL.Path, "/api/status") ||
			strings.HasPrefix(r.URL.Path, "/api/help") {
			next(w, r)
			return
		}
		next(w, r)
	}
}

func main() {
	initClient()
	cfgPath = "lanzou.json"

	// 命令行参数
	cookieArg := ""
	port := "8080"
	for i, a := range os.Args {
		switch a {
		case "-cookie":
			if i+1 < len(os.Args) {
				cookieArg = os.Args[i+1]
			}
		case "-port":
			if i+1 < len(os.Args) {
				port = os.Args[i+1]
			}
		case "-cfg":
			if i+1 < len(os.Args) {
				cfgPath = os.Args[i+1]
			}
		}
	}
	if _, err := strconv.Atoi(port); err != nil {
		fmt.Println("端口参数无效: " + port)
		os.Exit(1)
	}

	// 加载配置
	loadCfg()

	// 命令行指定Cookie：自动检测并写入
	if cookieArg != "" {
		fmt.Println("正在检测Cookie有效性...")
		ok, detail := checkCookie(cookieArg)
		if !ok {
			fmt.Println("✗ Cookie异常: " + detail)
			conf.Cookie = ""
			conf.Valid = false
			saveCfg()
			os.Exit(2)
		}
		conf.Cookie = cookieArg
		conf.Valid = true
		conf.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		saveCfg()
		fmt.Println("✓ Cookie有效，已写入 " + cfgPath)
	}

	// 启动时自动检测已有Cookie
	if conf.Cookie != "" {
		fmt.Println("检测已有Cookie...")
		ok, detail := checkCookie(conf.Cookie)
		if ok {
			conf.Valid = true
			fmt.Println("✓ Cookie有效")
		} else {
			conf.Valid = false
			saveCfg()
			fmt.Println("✗ Cookie异常: " + detail + " （可重新用 -cookie 或 /api/cookie 写入）")
		}
	} else {
		fmt.Println("未配置Cookie，请用: ./lanzou-server -cookie \"phpdisk_info=xxx\"")
	}

	// 路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cookie", apiCookieSet)
	mux.HandleFunc("/api/status", apiStatus)
	mux.HandleFunc("/api/list", apiList)
	mux.HandleFunc("/api/upload", apiUpload)
	mux.HandleFunc("/api/mkdir", apiMkdir)
	mux.HandleFunc("/api/delete", apiDelete)
	mux.HandleFunc("/api/share", apiShare)
	mux.HandleFunc("/api/parse", apiParse)
	mux.HandleFunc("/api/download", apiDownload)
	mux.HandleFunc("/api/rename", apiRename)
	mux.HandleFunc("/api/save", apiSave)
	mux.HandleFunc("/api/help", apiHelp)

	fmt.Printf("LanzouGo v%s 已启动: http://127.0.0.1:%s\n", version, port)
	fmt.Println("接口列表见: http://127.0.0.1:" + port + "/api/help")
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Println("启动失败: " + err.Error())
		os.Exit(1)
	}
}
