package netutil

import (
	"fmt"
	"io"
	"os"
	"strings"
	//	"path/filepath"
	. "github.com/leanote/leanote/app/lea"
	"io/ioutil"
	"net"
	"net/http"
	neturl "net/url"
	"time"
)

// 单次远程抓取的上限与超时, 避免被慢连接/超大文件拖垮服务
const maxDownloadSize = 20 * 1024 * 1024

var httpClient = &http.Client{Timeout: 15 * time.Second}

// net的util

// toPath 文件保存的目录
// 默认是/tmp
// 返回文件的完整目录
func WriteUrl(url string, toPath string) (length int64, newFilename, path string, ok bool) {
	if url == "" {
		return
	}
	content, err := GetContent(url)
	if err != nil {
		return
	}

	length = int64(len(content))

	// a.html?a=a11&xxx
	url = trimQueryParams(url)
	_, ext := SplitFilename(url)
	if toPath == "" {
		toPath = "/tmp"
	}
	//	dir := filepath.Dir(toPath)
	newFilename = NewGuid() + ext
	fullPath := toPath + "/" + newFilename

	// 写到文件中
	file, err := os.Create(fullPath)
	if err != nil {
		return
	}
	defer file.Close()
	file.Write(content)

	path = fullPath
	ok = true
	return
}

// 得到内容
func GetContent(rawUrl string) (content []byte, err error) {
	u, err := neturl.Parse(rawUrl)
	if err != nil {
		return nil, err
	}
	// 只允许 http/https, 避免 file:// 等协议被用来读取本地文件
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}

	resp, err := httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// 限制读取大小, 超出部分直接截断
	return ioutil.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
}

// 将url ?, #后面的字符串去掉
func trimQueryParams(url string) string {
	pos := strings.Index(url, "?")
	if pos != -1 {
		url = Substr(url, 0, pos)
	}

	pos = strings.Index(url, "#")
	if pos != -1 {
		url = Substr(url, 0, pos)
	}

	pos = strings.Index(url, "!")
	if pos != -1 {
		url = Substr(url, 0, pos)
	}
	return url
}

// 通过domain得到ip
func GetIpFromDomain(domain string) string {
	ip, _ := net.LookupIP(domain)
	if ip != nil && len(ip) > 0 {
		return ip[0].String()
	}
	return ""
}
