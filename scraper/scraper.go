package scraper

import (
	"fmt"
	"fragrantica-scraper/client"
	"fragrantica-scraper/config"
	"fragrantica-scraper/database"
	"fragrantica-scraper/parser"
	"fragrantica-scraper/proxy"
	"log"
	"time"
)

const (
	BaseURL      = "https://www.fragrantica.com.br"
	BrandsPages  = 11
)

// Scraper é o orquestrador principal
type Scraper struct {
	cfg          *config.Config
	db           *database.DB
	proxyPool    *proxy.Pool
	httpClient   *client.Client
	directClient *client.Client // client sem proxy
}

// New cria um novo scraper
func New(cfg *config.Config, db *database.DB, proxyPool *proxy.Pool) *Scraper {
	var httpClient *client.Client
	if proxyPool != nil {
		httpClient = client.New(proxyPool, cfg.RequestTimeout, cfg.MaxRetries)
	}
	directClient := client.NewDirect(cfg.RequestTimeout, cfg.MaxRetries)

	return &Scraper{
		cfg:          cfg,
		db:           db,
		proxyPool:    proxyPool,
		httpClient:   httpClient,
		directClient: directClient,
	}
}

// TestConnection testa a conexão com e sem proxy
func (s *Scraper) TestConnection() {
	testURL := fmt.Sprintf("%s/designers-1/", BaseURL)

	// Teste 1: Conexão direta
	log.Println("=== TESTE 1: Conexão DIRETA (sem proxy) ===")
	result, err := s.directClient.Fetch(testURL)
	if err != nil {
		log.Printf("  FALHOU: %v", err)
	} else {
		log.Printf("  Status: %d | HTML: %d bytes", result.StatusCode, len(result.HTML))
		if len(result.HTML) > 200 {
			log.Printf("  Primeiros 200 chars: %.200s", result.HTML)
		}
	}

	// Teste 2: Com proxy (se disponível)
	if s.httpClient != nil {
		log.Println("\n=== TESTE 2: Com PROXY ===")
		result, err := s.httpClient.Fetch(testURL)
		if err != nil {
			log.Printf("  FALHOU: %v", err)
		} else {
			log.Printf("  Status: %d | HTML: %d bytes | Proxy: %s",
				result.StatusCode, len(result.HTML), result.ProxyUsed)
		}
	}
}

// =========================================================
// STEP 1: Coletar marcas (11 páginas) - SEM PROXY (poucas requests)
// =========================================================
func (s *Scraper) Step1CollectBrands() error {
	log.Println("========== STEP 1: Coletando marcas ==========")

	// Registrar páginas
	for i := 1; i <= BrandsPages; i++ {
		url := fmt.Sprintf("%s/designers-%d/", BaseURL, i)
		s.db.RegisterScrapePage("brands_list", url, i)
	}

	// Processar pendentes
	pending, err := s.db.GetPendingPages("brands_list")
	if err != nil {
		return fmt.Errorf("erro ao buscar páginas pendentes: %w", err)
	}

	if len(pending) == 0 {
		log.Println("Todas as páginas de marcas já foram processadas!")
		return nil
	}

	log.Printf("Páginas pendentes: %d", len(pending))

	for _, page := range pending {
		log.Printf("Página %d/11: %s", page.PageNumber.Int32, page.URL)

		// Step 1 usa conexão direta (são só 11 páginas, não precisa proxy)
		result, err := s.directClient.Fetch(page.URL)
		if err != nil {
			log.Printf("  ERRO: %v", err)
			s.db.MarkPageFailed(page.ID, err.Error())
			s.db.Log("error", "brands_list", err.Error(), map[string]interface{}{"url": page.URL})
			continue
		}

		brands, err := parser.ParseBrandsList(result.HTML)
		if err != nil {
			log.Printf("  ERRO parse: %v", err)
			s.db.MarkPageFailed(page.ID, fmt.Sprintf("parse: %v", err))
			continue
		}

		if len(brands) == 0 {
			log.Printf("  AVISO: Nenhuma marca encontrada na página %d", page.PageNumber.Int32)
			s.db.MarkPageCompleted(page.ID)
			continue
		}

		count := 0
		for _, brand := range brands {
			_, err := s.db.UpsertBrand(brand.Name, brand.Slug, brand.URL, brand.ImageURL, brand.FragranceCount)
			if err != nil {
				log.Printf("  ERRO salvando marca %s: %v", brand.Name, err)
				continue
			}
			count++
		}

		s.db.MarkPageCompleted(page.ID)
		s.db.Log("info", "brands_list",
			fmt.Sprintf("Página %d: %d marcas", page.PageNumber.Int32, count),
			map[string]interface{}{"url": page.URL, "count": count})

		log.Printf("  OK: %d marcas salvas", count)

		// Delay mínimo entre páginas (são só 11)
		time.Sleep(2 * time.Second)
	}

	return nil
}

// =========================================================
// STEP 2: Detalhar marcas + listar fragrâncias - COM WORKERS
// =========================================================
func (s *Scraper) Step2DetailBrands(limit int) error {
	log.Println("========== STEP 2: Detalhando marcas (workers) ==========")
	log.Printf("Workers: %d | Proxies ativos: %d", s.cfg.WorkerCount, s.proxyPool.ActiveCount())

	pool := NewWorkerPool(s.db, s.httpClient, s.cfg.WorkerCount)
	pool.Start()

	batchSize := 50
	totalProcessed := 0

	for {
		brands, err := s.db.GetPendingBrandsForUpdate(batchSize)
		if err != nil {
			log.Printf("ERRO ao buscar marcas: %v", err)
			break
		}

		if len(brands) == 0 {
			log.Println("Todas as marcas foram processadas!")
			break
		}

		pool.Total.Add(int64(len(brands)))

		for _, brand := range brands {
			if limit > 0 && totalProcessed >= limit {
				log.Printf("Limite de %d marcas atingido.", limit)
				pool.Close()
				return nil
			}

			b := brand // cópia para closure
			pool.Submit(Job{Type: "brand_detail", Brand: &b})
			totalProcessed++
		}

		// Log intermediário
		log.Printf("Progresso: %d processados, %d falhas",
			pool.Processed.Load(), pool.Failed.Load())

		if limit > 0 && totalProcessed >= limit {
			break
		}
	}

	pool.Close()
	log.Printf("Step 2 concluído: %d processados, %d falhas",
		pool.Processed.Load(), pool.Failed.Load())
	return nil
}

// =========================================================
// STEP 3: Detalhar fragrâncias individuais - COM WORKERS
// =========================================================
func (s *Scraper) Step3DetailFragrances(limit int) error {
	log.Println("========== STEP 3: Detalhando fragrâncias (workers) ==========")
	log.Printf("Workers: %d | Proxies ativos: %d", s.cfg.WorkerCount, s.proxyPool.ActiveCount())

	pool := NewWorkerPool(s.db, s.httpClient, s.cfg.WorkerCount)
	pool.Start()

	batchSize := 100
	totalProcessed := 0

	for {
		frags, err := s.db.GetPendingFragrancesForUpdate(batchSize)
		if err != nil {
			log.Printf("ERRO ao buscar fragrâncias: %v", err)
			break
		}

		if len(frags) == 0 {
			log.Println("Todas as fragrâncias foram processadas!")
			break
		}

		pool.Total.Add(int64(len(frags)))

		for _, frag := range frags {
			if limit > 0 && totalProcessed >= limit {
				log.Printf("Limite de %d fragrâncias atingido.", limit)
				pool.Close()
				return nil
			}

			f := frag // cópia
			pool.Submit(Job{Type: "fragrance_detail", Fragrance: &f})
			totalProcessed++
		}

		log.Printf("Progresso: %d processados, %d falhas de %d total",
			pool.Processed.Load(), pool.Failed.Load(), pool.Total.Load())

		if limit > 0 && totalProcessed >= limit {
			break
		}
	}

	pool.Close()
	log.Printf("Step 3 concluído: %d processados, %d falhas",
		pool.Processed.Load(), pool.Failed.Load())
	return nil
}

// =========================================================
// STATUS
// =========================================================
func (s *Scraper) ShowProgress() {
	progress, err := s.db.GetProgress()
	if err != nil {
		log.Printf("Erro ao obter progresso: %v", err)
		return
	}

	fmt.Println("\n╔══════════════════════════════════════════╗")
	fmt.Println("║         PROGRESSO DO SCRAPING            ║")
	fmt.Println("╠══════════════════════════════════════════╣")

	// Brands
	bTotal := sumMap(progress.Brands)
	fmt.Printf("║ Marcas:      %6d total                ║\n", bTotal)
	fmt.Printf("║   Pendentes: %6d | Concluídas: %6d ║\n",
		progress.Brands["pending"], progress.Brands["completed"])
	fmt.Printf("║   Em progr.: %6d | Falhas:     %6d ║\n",
		progress.Brands["in_progress"], progress.Brands["failed"])

	fmt.Println("╠══════════════════════════════════════════╣")

	// Fragrances
	fTotal := sumMap(progress.Fragrances)
	fmt.Printf("║ Fragrâncias: %6d total                ║\n", fTotal)
	fmt.Printf("║   Pendentes: %6d | Concluídas: %6d ║\n",
		progress.Fragrances["pending"], progress.Fragrances["completed"])
	fmt.Printf("║   Em progr.: %6d | Falhas:     %6d ║\n",
		progress.Fragrances["in_progress"], progress.Fragrances["failed"])

	fmt.Println("╠══════════════════════════════════════════╣")
	fmt.Printf("║ Perfumistas: %6d                      ║\n", progress.Perfumers)
	fmt.Printf("║ Notas:       %6d                      ║\n", progress.Notes)
	fmt.Printf("║ Acordes:     %6d                      ║\n", progress.Accords)
	fmt.Println("╚══════════════════════════════════════════╝")

	if fTotal > 0 {
		pct := float64(progress.Fragrances["completed"]) / float64(fTotal) * 100
		fmt.Printf("\nProgresso geral fragrâncias: %.1f%%\n", pct)
	}
}

func sumMap(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
