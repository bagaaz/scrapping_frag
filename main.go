package main

import (
	"fmt"
	"fragrantica-scraper/config"
	"fragrantica-scraper/database"
	"fragrantica-scraper/proxy"
	"fragrantica-scraper/scraper"
	"log"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Carregar config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Erro ao carregar config: %v", err)
	}

	// Conectar banco
	db, err := database.New(cfg.DSN())
	if err != nil {
		log.Fatalf("Erro ao conectar banco: %v", err)
	}
	defer db.Close()

	// Parse do --limit (se existir)
	limit := parseLimit(os.Args)

	switch command {
	case "test":
		// Testa conexão direta e com proxy para diagnosticar Cloudflare
		proxyPool, _ := loadProxies(cfg)
		s := scraper.New(cfg, db, proxyPool)
		s.TestConnection()

	case "step1":
		// Step 1 usa conexão direta (só 11 páginas)
		s := scraper.New(cfg, db, nil)
		if err := s.Step1CollectBrands(); err != nil {
			log.Fatalf("Erro no Step 1: %v", err)
		}
		s.ShowProgress()

	case "step2":
		proxyPool, err := loadProxies(cfg)
		if err != nil {
			log.Fatalf("Step 2 requer proxies: %v", err)
		}
		s := scraper.New(cfg, db, proxyPool)
		if err := s.Step2DetailBrands(limit); err != nil {
			log.Fatalf("Erro no Step 2: %v", err)
		}
		s.ShowProgress()

	case "step3":
		proxyPool, err := loadProxies(cfg)
		if err != nil {
			log.Fatalf("Step 3 requer proxies: %v", err)
		}
		s := scraper.New(cfg, db, proxyPool)
		if err := s.Step3DetailFragrances(limit); err != nil {
			log.Fatalf("Erro no Step 3: %v", err)
		}
		s.ShowProgress()

	case "all":
		proxyPool, err := loadProxies(cfg)
		if err != nil {
			log.Fatalf("Execução completa requer proxies: %v", err)
		}
		s := scraper.New(cfg, db, proxyPool)
		log.Println("========== EXECUÇÃO COMPLETA ==========")
		if err := s.Step1CollectBrands(); err != nil {
			log.Fatalf("Erro no Step 1: %v", err)
		}
		if err := s.Step2DetailBrands(limit); err != nil {
			log.Fatalf("Erro no Step 2: %v", err)
		}
		if err := s.Step3DetailFragrances(limit); err != nil {
			log.Fatalf("Erro no Step 3: %v", err)
		}
		s.ShowProgress()

	case "status":
		// Status não precisa de proxy
		proxyPool := createDirectPool()
		s := scraper.New(cfg, db, proxyPool)
		s.ShowProgress()

	case "retry":
		entity := "all"
		if len(os.Args) > 2 {
			entity = os.Args[2]
		}
		count, err := db.RetryFailed(entity)
		if err != nil {
			log.Fatalf("Erro no retry: %v", err)
		}
		log.Printf("Resetados %d itens para retry", count)

		proxyPool := createDirectPool()
		s := scraper.New(cfg, db, proxyPool)
		s.ShowProgress()

	default:
		fmt.Printf("Comando desconhecido: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func loadProxies(cfg *config.Config) (*proxy.Pool, error) {
	return proxy.NewPool(cfg.ProxyFile, cfg.ProxyCooldown)
}

// createDirectPool cria um pool com "proxy" direto (sem proxy real)
// Útil para step1 e status que não precisam de proxy
func createDirectPool() *proxy.Pool {
	// Criar arquivo temporário com proxy "direto"
	tmpFile := "/tmp/fragrantica_direct_proxy.txt"
	os.WriteFile(tmpFile, []byte("127.0.0.1:0"), 0644)
	pool, _ := proxy.NewPool(tmpFile, 0)
	return pool
}

func parseLimit(args []string) int {
	for i, arg := range args {
		if arg == "--limit" && i+1 < len(args) {
			limit, err := strconv.Atoi(args[i+1])
			if err == nil {
				return limit
			}
		}
	}
	return 0
}

func printUsage() {
	fmt.Println(`
Fragrantica Scraper - Go Edition

USO:
    fragrantica-scraper <comando> [opções]

COMANDOS:
    test                Testar conexão (direta + proxy) para diagnosticar Cloudflare
    step1               Coletar marcas (páginas 1-11) - conexão direta
    step2               Detalhar marcas + listar fragrâncias (usa proxies)
    step3               Detalhar fragrâncias individuais (usa proxies)
    all                 Rodar tudo em sequência
    status              Ver progresso atual
    retry [entity]      Resetar falhas (all, brands, fragrances, pages)

OPÇÕES:
    --limit N           Limitar quantidade de itens a processar

EXEMPLOS:
    fragrantica-scraper step1
    fragrantica-scraper step2 --limit 5
    fragrantica-scraper step3 --limit 100
    fragrantica-scraper status
    fragrantica-scraper retry brands

CONFIGURAÇÃO:
    Copie .env.example para .env e configure.
    Coloque proxies em proxies.txt (um por linha).
	`)
}
