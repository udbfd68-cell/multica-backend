package stealth

import (
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ProxyPool manages a pool of proxies for rotation.
type ProxyPool struct {
	mu      sync.Mutex
	proxies []ProxyEntry
	idx     int
}

// ProxyEntry represents a single proxy configuration.
type ProxyEntry struct {
	URL      string `json:"url"`
	Protocol string `json:"protocol"` // http, https, socks5
	Country  string `json:"country,omitempty"`
	Healthy  bool   `json:"healthy"`
}

// NewProxyPool creates a proxy pool from a list of proxy URLs.
func NewProxyPool(urls []string) *ProxyPool {
	entries := make([]ProxyEntry, 0, len(urls))
	for _, u := range urls {
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}
		entries = append(entries, ProxyEntry{
			URL:      u,
			Protocol: parsed.Scheme,
			Healthy:  true,
		})
	}
	// Shuffle initial order
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
	return &ProxyPool{proxies: entries}
}

// Next returns the next healthy proxy in round-robin order.
// Returns empty string if no proxies available.
func (p *ProxyPool) Next() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.proxies) == 0 {
		return ""
	}

	// Try up to len(proxies) times to find a healthy one
	for range p.proxies {
		entry := p.proxies[p.idx%len(p.proxies)]
		p.idx++
		if entry.Healthy {
			return entry.URL
		}
	}
	// All unhealthy — return first anyway
	return p.proxies[0].URL
}

// MarkUnhealthy marks a proxy as unhealthy.
func (p *ProxyPool) MarkUnhealthy(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.proxies {
		if p.proxies[i].URL == proxyURL {
			p.proxies[i].Healthy = false
			return
		}
	}
}

// Size returns the number of proxies in the pool.
func (p *ProxyPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.proxies)
}

// Transport returns an http.Transport configured to use the next proxy.
func (p *ProxyPool) Transport() *http.Transport {
	proxyURL := p.Next()
	if proxyURL == "" {
		return &http.Transport{}
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return &http.Transport{}
	}
	return &http.Transport{
		Proxy: http.ProxyURL(parsed),
	}
}
