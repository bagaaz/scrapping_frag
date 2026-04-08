package client

import (
	"fmt"
	"fragrantica-scraper/proxy"
	"io"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// Client é o HTTP client com TLS fingerprint impersonation e proxy rotation
type Client struct {
	proxyPool  *proxy.Pool
	timeout    int
	maxRetries int
	useProxy   bool

	// Sessões persistentes por proxy (mantém cookies do Cloudflare)
	sessions   map[string]tls_client.HttpClient
	sessionsMu sync.Mutex

	// Sessão direta (sem proxy)
	directSession tls_client.HttpClient
	directMu      sync.Mutex
}

// FetchResult é o resultado de um fetch
type FetchResult struct {
	HTML       string
	StatusCode int
	ProxyUsed  string
}

// New cria um novo client com proxy
func New(proxyPool *proxy.Pool, timeout, maxRetries int) *Client {
	return &Client{
		proxyPool:  proxyPool,
		timeout:    timeout,
		maxRetries: maxRetries,
		useProxy:   true,
		sessions:   make(map[string]tls_client.HttpClient),
	}
}

// NewDirect cria um client SEM proxy (conexão direta)
func NewDirect(timeout, maxRetries int) *Client {
	return &Client{
		timeout:    timeout,
		maxRetries: maxRetries,
		useProxy:   false,
		sessions:   make(map[string]tls_client.HttpClient),
	}
}

// getSession retorna ou cria uma sessão TLS persistente para o proxy
func (c *Client) getSession(proxyURL string) (tls_client.HttpClient, error) {
	if proxyURL == "" {
		// Sessão direta
		c.directMu.Lock()
		defer c.directMu.Unlock()

		if c.directSession != nil {
			return c.directSession, nil
		}

		session, err := c.createSession("")
		if err != nil {
			return nil, err
		}
		c.directSession = session
		return session, nil
	}

	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	if session, ok := c.sessions[proxyURL]; ok {
		return session, nil
	}

	session, err := c.createSession(proxyURL)
	if err != nil {
		return nil, err
	}
	c.sessions[proxyURL] = session
	return session, nil
}

// createSession cria uma nova sessão TLS com cookie jar persistente
func (c *Client) createSession(proxyURL string) (tls_client.HttpClient, error) {
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(c.timeout),
		tls_client.WithClientProfile(profiles.Chrome_124),
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithCookieJar(tls_client.NewCookieJar()), // Cookie jar persistente
	}

	if proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(proxyURL))
	}

	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
}

// resetSession descarta a sessão de um proxy (para forçar nova após bloqueio)
func (c *Client) resetSession(proxyURL string) {
	if proxyURL == "" {
		c.directMu.Lock()
		c.directSession = nil
		c.directMu.Unlock()
		return
	}

	c.sessionsMu.Lock()
	delete(c.sessions, proxyURL)
	c.sessionsMu.Unlock()
}

// Fetch faz GET com retry automático, rotação de proxy e TLS fingerprint
func (c *Client) Fetch(url string) (*FetchResult, error) {
	var lastErr error

	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		var proxyURL string
		var proxyAddr string
		var p *proxy.Proxy

		if c.useProxy && c.proxyPool != nil {
			p = c.proxyPool.Get()
			proxyURL = p.URL
			proxyAddr = p.Address
		} else {
			proxyAddr = "direct"
		}

		result, err := c.doRequest(url, proxyURL)
		if err != nil {
			lastErr = fmt.Errorf("[%s] %w", proxyAddr, err)
			if p != nil {
				c.proxyPool.MarkFailed(p)
			}
			// Reset sessão em caso de erro de conexão
			c.resetSession(proxyURL)
			if attempt < c.maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			continue
		}

		result.ProxyUsed = proxyAddr

		// Verificar status codes problemáticos
		switch result.StatusCode {
		case 403:
			lastErr = fmt.Errorf("403 Forbidden (Cloudflare block) via %s", proxyAddr)
			if p != nil {
				c.proxyPool.MarkFailed(p)
			}
			// Reset sessão para tentar cookies limpos com outro proxy
			c.resetSession(proxyURL)
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
			continue
		case 429:
			lastErr = fmt.Errorf("429 Rate Limited via %s", proxyAddr)
			if p != nil {
				c.proxyPool.MarkFailed(p)
			}
			// NÃO resetar sessão para 429 — cookies ainda valem, só precisa esperar
			time.Sleep(time.Duration(attempt) * 5 * time.Second)
			continue
		case 503:
			lastErr = fmt.Errorf("503 Cloudflare Challenge via %s", proxyAddr)
			if p != nil {
				c.proxyPool.MarkFailed(p)
			}
			c.resetSession(proxyURL)
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
			continue
		}

		if result.StatusCode >= 400 {
			lastErr = fmt.Errorf("HTTP %d via %s", result.StatusCode, proxyAddr)
			if p != nil {
				c.proxyPool.MarkFailed(p)
			}
			continue
		}

		if p != nil {
			c.proxyPool.MarkSuccess(p)
		}
		return result, nil
	}

	return nil, fmt.Errorf("falhou após %d tentativas. Último erro: %v", c.maxRetries, lastErr)
}

// doRequest executa um único request usando sessão persistente
func (c *Client) doRequest(url string, proxyURL string) (*FetchResult, error) {
	session, err := c.getSession(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter sessão: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar request: %w", err)
	}

	// Headers que imitam um browser real — ordem importa para Cloudflare
	req.Header = http.Header{
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		"accept-language":           {"pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7"},
		"accept-encoding":           {"gzip, deflate, br"},
		"cache-control":             {"max-age=0"},
		"sec-ch-ua":                 {`"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"sec-fetch-dest":            {"document"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-user":            {"?1"},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"},
		http.HeaderOrderKey: {
			"accept",
			"accept-encoding",
			"accept-language",
			"cache-control",
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"sec-fetch-dest",
			"sec-fetch-mode",
			"sec-fetch-site",
			"sec-fetch-user",
			"upgrade-insecure-requests",
			"user-agent",
		},
	}

	resp, err := session.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro no request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler body: %w", err)
	}

	return &FetchResult{
		HTML:       string(bodyBytes),
		StatusCode: resp.StatusCode,
	}, nil
}
