// Package stealth provides anti-detection capabilities for browser automation.
// It configures headless Chrome/Chromium to evade bot detection systems by
// randomizing fingerprints, rotating user agents, and applying evasion scripts.
package stealth

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Config holds stealth browser launch configuration.
type Config struct {
	Enabled          bool     `json:"enabled"`
	ProxyURL         string   `json:"proxy_url,omitempty"`
	UserAgent        string   `json:"user_agent,omitempty"`
	ViewportWidth    int      `json:"viewport_width,omitempty"`
	ViewportHeight   int      `json:"viewport_height,omitempty"`
	Locale           string   `json:"locale,omitempty"`
	Timezone         string   `json:"timezone,omitempty"`
	DisableWebRTC    bool     `json:"disable_webrtc"`
	DisableWebGL     bool     `json:"disable_webgl"`
	BlockFingerprint bool     `json:"block_fingerprint"`
}

// DefaultConfig returns a stealth config with reasonable defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:          true,
		ViewportWidth:    1920,
		ViewportHeight:   1080,
		Locale:           "en-US",
		Timezone:         "America/New_York",
		DisableWebRTC:    true,
		BlockFingerprint: true,
	}
}

// BrowserArgs returns Chrome/Chromium launch arguments for stealth operation.
func (c Config) BrowserArgs() []string {
	args := []string{
		"--headless=new",
		"--no-sandbox",
		"--disable-blink-features=AutomationControlled",
		"--disable-features=IsolateOrigins,site-per-process",
		"--disable-infobars",
		"--disable-dev-shm-usage",
		"--disable-background-networking",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-sync",
		"--disable-translate",
		"--metrics-recording-only",
		"--no-first-run",
		"--hide-scrollbars",
		"--mute-audio",
		fmt.Sprintf("--window-size=%d,%d", c.ViewportWidth, c.ViewportHeight),
	}

	ua := c.UserAgent
	if ua == "" {
		ua = RandomUserAgent()
	}
	args = append(args, fmt.Sprintf("--user-agent=%s", ua))

	if c.Locale != "" {
		args = append(args, fmt.Sprintf("--lang=%s", c.Locale))
	}

	if c.Timezone != "" {
		args = append(args, fmt.Sprintf("--timezone=%s", c.Timezone))
	}

	if c.DisableWebRTC {
		args = append(args, "--disable-features=WebRtcHideLocalIpsWithMdns")
		args = append(args, "--enforce-webrtc-ip-permission-check")
		args = append(args, "--webrtc-ip-handling-policy=disable_non_proxied_udp")
	}

	if c.DisableWebGL {
		args = append(args, "--disable-webgl", "--disable-webgl2")
	}

	if c.ProxyURL != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", c.ProxyURL))
	}

	return args
}

// HTTPHeaders returns browser-like request headers for stealth HTTP requests.
func (c Config) HTTPHeaders() map[string]string {
	ua := c.UserAgent
	if ua == "" {
		ua = RandomUserAgent()
	}
	locale := c.Locale
	if locale == "" {
		locale = "en-US"
	}

	return map[string]string{
		"User-Agent":                ua,
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":           locale + "," + strings.Split(locale, "-")[0] + ";q=0.9",
		"Accept-Encoding":           "gzip, deflate, br",
		"Cache-Control":             "no-cache",
		"Pragma":                    "no-cache",
		"Sec-Ch-Ua":                 randomSecChUA(),
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        randomPlatform(),
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}
}

// EvasionScript returns JavaScript to inject into pages to bypass bot detection.
// This covers navigator.webdriver, chrome.runtime, permissions, plugins, etc.
func EvasionScript() string {
	return `
// Remove webdriver flag
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });

// Mock chrome runtime
if (!window.chrome) { window.chrome = {}; }
if (!window.chrome.runtime) {
  window.chrome.runtime = {
    connect: function() { return { onMessage: { addListener: function() {} }, postMessage: function() {} }; },
    sendMessage: function() {},
    onMessage: { addListener: function() {} },
    id: undefined
  };
}

// Mock plugins
Object.defineProperty(navigator, 'plugins', {
  get: () => {
    const plugins = [
      { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
      { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
      { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' }
    ];
    plugins.length = 3;
    return plugins;
  }
});

// Mock languages
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });

// Override permissions query
const originalQuery = window.navigator.permissions.query;
window.navigator.permissions.query = (parameters) => (
  parameters.name === 'notifications' ?
    Promise.resolve({ state: Notification.permission }) :
    originalQuery(parameters)
);

// Prevent canvas fingerprinting
const origGetContext = HTMLCanvasElement.prototype.getContext;
HTMLCanvasElement.prototype.getContext = function(type, attrs) {
  const ctx = origGetContext.call(this, type, attrs);
  if (type === '2d' && ctx) {
    const origGetImageData = ctx.getImageData;
    ctx.getImageData = function() {
      const imageData = origGetImageData.apply(this, arguments);
      for (let i = 0; i < imageData.data.length; i += 4) {
        imageData.data[i] ^= 1;
      }
      return imageData;
    };
  }
  return ctx;
};

// Prevent WebGL fingerprinting
const getParameter = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function(parameter) {
  if (parameter === 37445) return 'Intel Inc.';
  if (parameter === 37446) return 'Intel Iris OpenGL Engine';
  return getParameter.call(this, parameter);
};
`
}

// ---------------------------------------------------------------------------
// User Agent Rotation
// ---------------------------------------------------------------------------

var chromeVersions = []string{
	"130.0.6723.58", "130.0.6723.69", "130.0.6723.91",
	"131.0.6778.69", "131.0.6778.86", "131.0.6778.109",
	"132.0.6834.57", "132.0.6834.83", "132.0.6834.110",
	"133.0.6943.53", "133.0.6943.98", "133.0.6943.127",
}

var windowsVersions = []string{
	"10.0; Win64; x64",
	"10.0; WOW64",
}

var macVersions = []string{
	"Macintosh; Intel Mac OS X 10_15_7",
	"Macintosh; Intel Mac OS X 13_6_1",
	"Macintosh; Intel Mac OS X 14_3_1",
	"Macintosh; Intel Mac OS X 14_5",
}

var linuxVersions = []string{
	"X11; Linux x86_64",
	"X11; Ubuntu; Linux x86_64",
}

// RandomUserAgent returns a realistic Chrome user agent string.
func RandomUserAgent() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chrome := chromeVersions[r.Intn(len(chromeVersions))]

	// 50% Windows, 35% Mac, 15% Linux
	roll := r.Intn(100)
	var os string
	switch {
	case roll < 50:
		os = windowsVersions[r.Intn(len(windowsVersions))]
	case roll < 85:
		os = macVersions[r.Intn(len(macVersions))]
	default:
		os = linuxVersions[r.Intn(len(linuxVersions))]
	}

	return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", os, chrome)
}

// RandomViewport returns a common screen resolution.
func RandomViewport() (width, height int) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	viewports := [][2]int{
		{1920, 1080}, {1366, 768}, {1536, 864}, {1440, 900},
		{1280, 720}, {2560, 1440}, {1680, 1050}, {1600, 900},
	}
	v := viewports[r.Intn(len(viewports))]
	return v[0], v[1]
}

func randomSecChUA() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chrome := chromeVersions[r.Intn(len(chromeVersions))]
	major := strings.Split(chrome, ".")[0]
	return fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not?A_Brand";v="99"`, major, major)
}

func randomPlatform() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	platforms := []string{`"Windows"`, `"macOS"`, `"Linux"`}
	return platforms[r.Intn(len(platforms))]
}
