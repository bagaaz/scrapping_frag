package proxy

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Proxy representa um proxy individual
type Proxy struct {
	Address  string // ip:porta
	Username string
	Password string
	URL      string // URL formatada para uso no client

	mu         sync.Mutex
	lastUsed   time.Time
	failCount  int
	cooldownAt time.Time
}

// Pool gerencia um pool de proxies com rotação e cooldown
type Pool struct {
	proxies []*Proxy
	mu      sync.Mutex
	index   int
	cooldown time.Duration
}

// NewPool cria um pool de proxies a partir de um arquivo
func NewPool(filePath string, cooldownSec float64) (*Pool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir arquivo de proxies: %w", err)
	}
	defer file.Close()

	var proxies []*Proxy
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := parseProxyLine(line)
		if p != nil {
			proxies = append(proxies, p)
		}
	}

	if len(proxies) == 0 {
		return nil, fmt.Errorf("nenhum proxy válido encontrado em %s", filePath)
	}

	return &Pool{
		proxies:  proxies,
		cooldown: time.Duration(cooldownSec * float64(time.Second)),
	}, nil
}

// Get retorna o próximo proxy disponível (round-robin com cooldown)
func (p *Pool) Get() *Proxy {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	tried := 0

	for tried < len(p.proxies) {
		proxy := p.proxies[p.index]
		p.index = (p.index + 1) % len(p.proxies)

		proxy.mu.Lock()
		// Pular proxies em cooldown excessivo (muitas falhas)
		if proxy.failCount >= 10 && now.Before(proxy.cooldownAt) {
			proxy.mu.Unlock()
			tried++
			continue
		}

		// Respeitar cooldown entre uses
		if now.Sub(proxy.lastUsed) >= p.cooldown {
			proxy.lastUsed = now
			proxy.mu.Unlock()
			return proxy
		}
		proxy.mu.Unlock()
		tried++
	}

	// Se todos em cooldown, pegar o primeiro disponível (sem cooldown de tempo)
	proxy := p.proxies[p.index]
	p.index = (p.index + 1) % len(p.proxies)
	return proxy
}

// MarkSuccess marca uso bem-sucedido
func (p *Pool) MarkSuccess(proxy *Proxy) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.failCount = 0
}

// MarkFailed marca falha no proxy
func (p *Pool) MarkFailed(proxy *Proxy) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.failCount++
	// Cooldown exponencial: 60s * failCount (até 10min)
	cd := time.Duration(proxy.failCount) * 60 * time.Second
	if cd > 10*time.Minute {
		cd = 10 * time.Minute
	}
	proxy.cooldownAt = time.Now().Add(cd)
}

// Count retorna a quantidade de proxies no pool
func (p *Pool) Count() int {
	return len(p.proxies)
}

// ActiveCount retorna quantos proxies estão ativos (não em cooldown pesado)
func (p *Pool) ActiveCount() int {
	now := time.Now()
	count := 0
	for _, proxy := range p.proxies {
		proxy.mu.Lock()
		if proxy.failCount < 10 || now.After(proxy.cooldownAt) {
			count++
		}
		proxy.mu.Unlock()
	}
	return count
}

// parseProxyLine faz parse de diferentes formatos de proxy
func parseProxyLine(line string) *Proxy {
	p := &Proxy{}

	// Remover prefixo http:// se existir
	line = strings.TrimPrefix(line, "http://")
	line = strings.TrimPrefix(line, "https://")

	// Formato: user:pass@ip:porta
	if strings.Contains(line, "@") {
		parts := strings.SplitN(line, "@", 2)
		authParts := strings.SplitN(parts[0], ":", 2)
		if len(authParts) == 2 {
			p.Username = authParts[0]
			p.Password = authParts[1]
		}
		p.Address = parts[1]
	} else {
		// Formato: ip:porta ou ip:porta:user:pass
		parts := strings.Split(line, ":")
		switch len(parts) {
		case 2:
			// ip:porta
			p.Address = line
		case 4:
			// ip:porta:user:pass
			p.Address = parts[0] + ":" + parts[1]
			p.Username = parts[2]
			p.Password = parts[3]
		default:
			return nil
		}
	}

	// Montar URL
	if p.Username != "" {
		p.URL = fmt.Sprintf("http://%s:%s@%s", p.Username, p.Password, p.Address)
	} else {
		p.URL = fmt.Sprintf("http://%s", p.Address)
	}

	return p
}
