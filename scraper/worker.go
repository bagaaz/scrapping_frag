package scraper

import (
	"fmt"
	"fragrantica-scraper/client"
	"fragrantica-scraper/database"
	"fragrantica-scraper/parser"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Job representa um trabalho para o worker
type Job struct {
	Type string // "brand_detail" ou "fragrance_detail"

	// Para brand_detail
	Brand *database.Brand

	// Para fragrance_detail
	Fragrance *database.Fragrance
}

// WorkerPool gerencia workers concorrentes
type WorkerPool struct {
	db         *database.DB
	httpClient *client.Client
	workerCount int
	jobs       chan Job
	wg         sync.WaitGroup

	// Contadores atômicos para stats
	Processed atomic.Int64
	Failed    atomic.Int64
	Total     atomic.Int64
}

// NewWorkerPool cria um novo pool de workers
func NewWorkerPool(db *database.DB, httpClient *client.Client, workerCount int) *WorkerPool {
	return &WorkerPool{
		db:          db,
		httpClient:  httpClient,
		workerCount: workerCount,
		jobs:        make(chan Job, workerCount*2), // Buffer de 2x workers
	}
}

// Start inicia os workers
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

// Submit envia um job para processamento
func (wp *WorkerPool) Submit(job Job) {
	wp.jobs <- job
}

// Close fecha o canal de jobs e espera todos terminarem
func (wp *WorkerPool) Close() {
	close(wp.jobs)
	wp.wg.Wait()
}

// worker é a goroutine que processa jobs
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for job := range wp.jobs {
		switch job.Type {
		case "brand_detail":
			wp.processBrand(id, job.Brand)
		case "fragrance_detail":
			wp.processFragrance(id, job.Fragrance)
		}

		// Delay entre requests para evitar rate limiting
		// 1-3 segundos aleatório por worker
		delay := time.Duration(1000+rand.Intn(2000)) * time.Millisecond
		time.Sleep(delay)
	}
}

func (wp *WorkerPool) processBrand(workerID int, brand *database.Brand) {
	url := brand.URL
	log.Printf("[W%d] Marca: %s (%s)", workerID, brand.Name, url)

	result, err := wp.httpClient.Fetch(url)
	if err != nil {
		log.Printf("[W%d] ERRO marca %s: %v", workerID, brand.Name, err)
		wp.db.MarkBrandStatus(brand.ID, "failed", err.Error())
		wp.db.Log("error", "brand_detail", err.Error(), map[string]interface{}{
			"brand_id": brand.ID, "url": url,
		})
		wp.Failed.Add(1)
		return
	}

	data, err := parser.ParseBrandDetail(result.HTML)
	if err != nil {
		log.Printf("[W%d] ERRO parse marca %s: %v", workerID, brand.Name, err)
		wp.db.MarkBrandStatus(brand.ID, "failed", fmt.Sprintf("parse error: %v", err))
		wp.Failed.Add(1)
		return
	}

	// Salvar detalhes da marca
	wp.db.UpdateBrandDetails(int64(brand.ID), data.Country, data.Description,
		data.MainActivity, data.GroupCompany)

	// Salvar fragrâncias
	fragCount := 0
	for _, frag := range data.Fragrances {
		_, err := wp.db.UpsertFragrance(
			int64(brand.ID), frag.Name, frag.Slug, frag.URL,
			frag.ImageURL, frag.Gender, frag.Year, frag.Collection,
		)
		if err != nil {
			log.Printf("[W%d] ERRO salvando fragrância %s: %v", workerID, frag.Name, err)
			continue
		}
		fragCount++
	}

	wp.db.Log("info", "brand_detail",
		fmt.Sprintf("%s: %d fragrâncias via %s", brand.Name, fragCount, result.ProxyUsed),
		map[string]interface{}{"brand_id": brand.ID, "count": fragCount})

	log.Printf("[W%d] OK marca %s: %d fragrâncias (proxy: %s)",
		workerID, brand.Name, fragCount, result.ProxyUsed)

	wp.Processed.Add(1)
}

func (wp *WorkerPool) processFragrance(workerID int, frag *database.Fragrance) {
	url := frag.URL
	log.Printf("[W%d] Fragrância: %s - %s", workerID, frag.BrandName, frag.Name)

	result, err := wp.httpClient.Fetch(url)
	if err != nil {
		log.Printf("[W%d] ERRO fragrância %s: %v", workerID, frag.Name, err)
		wp.db.MarkFragranceStatus(frag.ID, "failed", err.Error())
		wp.db.Log("error", "fragrance_detail", err.Error(), map[string]interface{}{
			"fragrance_id": frag.ID, "url": url,
		})
		wp.Failed.Add(1)
		return
	}

	data, err := parser.ParseFragranceDetail(result.HTML)
	if err != nil {
		log.Printf("[W%d] ERRO parse fragrância %s: %v", workerID, frag.Name, err)
		wp.db.MarkFragranceStatus(frag.ID, "failed", fmt.Sprintf("parse error: %v", err))
		wp.Failed.Add(1)
		return
	}

	// Salvar detalhes (incluindo imagem de alta resolução se disponível)
	wp.db.UpdateFragranceDetails(int64(frag.ID), data.Description, data.Year, data.ImageURL)

	// Perfumistas
	for _, perf := range data.Perfumers {
		perfID, err := wp.db.UpsertPerfumer(perf.Name, perf.URL)
		if err != nil {
			continue
		}
		wp.db.LinkFragrancePerfumer(int64(frag.ID), perfID)
	}

	// Notas
	for _, note := range data.Notes.Top {
		noteID, err := wp.db.UpsertNote(note.Name, note.URL, note.ImageURL)
		if err != nil {
			continue
		}
		wp.db.LinkFragranceNote(int64(frag.ID), noteID, "top")
	}
	for _, note := range data.Notes.Heart {
		noteID, err := wp.db.UpsertNote(note.Name, note.URL, note.ImageURL)
		if err != nil {
			continue
		}
		wp.db.LinkFragranceNote(int64(frag.ID), noteID, "heart")
	}
	for _, note := range data.Notes.Base {
		noteID, err := wp.db.UpsertNote(note.Name, note.URL, note.ImageURL)
		if err != nil {
			continue
		}
		wp.db.LinkFragranceNote(int64(frag.ID), noteID, "base")
	}
	for _, note := range data.Notes.General {
		noteID, err := wp.db.UpsertNote(note.Name, note.URL, note.ImageURL)
		if err != nil {
			continue
		}
		wp.db.LinkFragranceNote(int64(frag.ID), noteID, "general")
	}

	// Acordes
	for _, accord := range data.Accords {
		accordID, err := wp.db.UpsertAccord(accord.Name)
		if err != nil {
			continue
		}
		wp.db.LinkFragranceAccord(int64(frag.ID), accordID, accord.Percentage)
	}

	nPerfumers := len(data.Perfumers)
	nNotes := len(data.Notes.Top) + len(data.Notes.Heart) + len(data.Notes.Base) + len(data.Notes.General)
	nAccords := len(data.Accords)

	wp.db.Log("info", "fragrance_detail",
		fmt.Sprintf("%s: %dp, %dn, %da via %s", frag.Name, nPerfumers, nNotes, nAccords, result.ProxyUsed),
		map[string]interface{}{"fragrance_id": frag.ID})

	log.Printf("[W%d] OK %s: %d perfumistas, %d notas, %d acordes (proxy: %s)",
		workerID, frag.Name, nPerfumers, nNotes, nAccords, result.ProxyUsed)

	wp.Processed.Add(1)
}
