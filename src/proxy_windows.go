package main

import (
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"unsafe"
)

type ieProxyConfig struct {
	AutoDetect                        int32
	AutoConfigURL, Proxy, ProxyBypass *uint16
}

// Honor a static Windows user proxy (common for local VPN clients), unless an
// HTTP_PROXY/HTTPS_PROXY environment setting takes precedence. PAC is not evaluated.
func configureNativeHTTP() {
	dll := syscall.NewLazyDLL("winhttp.dll")
	var cfg ieProxyConfig
	ok, _, _ := dll.NewProc("WinHttpGetIEProxyConfigForCurrentUser").Call(uintptr(unsafe.Pointer(&cfg)))
	if ok == 0 {
		return
	}
	defer func() {
		for _, p := range []*uint16{cfg.AutoConfigURL, cfg.Proxy, cfg.ProxyBypass} {
			if p != nil {
				kernel32.NewProc("GlobalFree").Call(uintptr(unsafe.Pointer(p)))
			}
		}
	}()
	if cfg.Proxy == nil {
		return
	}
	n, _, _ := kernel32.NewProc("lstrlenW").Call(uintptr(unsafe.Pointer(cfg.Proxy)))
	if n == 0 || n > 32768 {
		return
	}
	setting := syscall.UTF16ToString(unsafe.Slice(cfg.Proxy, int(n)))
	candidate := ""
	if !strings.Contains(setting, "=") {
		candidate = setting
	} else {
		for _, part := range strings.Split(setting, ";") {
			pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(pair) == 2 && strings.EqualFold(pair[0], "https") {
				candidate = pair[1]
				break
			}
			if len(pair) == 2 && strings.EqualFold(pair[0], "http") {
				candidate = pair[1]
			}
		}
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	proxy, err := url.Parse(candidate)
	if err != nil || proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5") {
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		p, e := http.ProxyFromEnvironment(r)
		if p != nil || e != nil {
			return p, e
		}
		return proxy, nil
	}
	httpClient.Transport = transport
}
