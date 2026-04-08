package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type DB struct {
	conn *sql.DB
}

// Modelos
type Brand struct {
	ID             int
	Name           string
	Slug           string
	URL            string
	ImageURL       sql.NullString
	FragranceCount int
	Country        sql.NullString
	Description    sql.NullString
	MainActivity   sql.NullString
	GroupCompany   sql.NullString
	ScrapeStatus   string
	ScrapeError    sql.NullString
	Attempts       int
}

type Fragrance struct {
	ID           int
	BrandID      int
	BrandName    string // join field
	Name         string
	Slug         string
	URL          string
	ImageURL     sql.NullString
	Gender       sql.NullString
	Year         sql.NullInt16
	Description  sql.NullString
	ScrapeStatus string
	ScrapeError  sql.NullString
	Attempts     int
}

type Progress struct {
	Brands     map[string]int
	Fragrances map[string]int
	Perfumers  int
	Notes      int
	Accords    int
}

// New cria nova conexão com o banco
func New(dsn string) (*DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar MySQL: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("erro ao pingar MySQL: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() {
	db.conn.Close()
}

// =========================================================
// SCRAPE PAGES
// =========================================================

type ScrapePage struct {
	ID         int
	PageType   string
	PageNumber sql.NullInt32
	URL        string
	Status     string
}

func (db *DB) RegisterScrapePage(pageType, url string, pageNumber int) error {
	_, err := db.conn.Exec(`
		INSERT IGNORE INTO scrape_pages (page_type, page_number, url, status)
		VALUES (?, ?, ?, 'pending')
	`, pageType, pageNumber, url)
	return err
}

func (db *DB) GetPendingPages(pageType string) ([]ScrapePage, error) {
	rows, err := db.conn.Query(`
		SELECT id, page_type, page_number, url, status
		FROM scrape_pages
		WHERE page_type = ? AND status = 'pending'
		ORDER BY page_number ASC, id ASC
	`, pageType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []ScrapePage
	for rows.Next() {
		var p ScrapePage
		if err := rows.Scan(&p.ID, &p.PageType, &p.PageNumber, &p.URL, &p.Status); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func (db *DB) MarkPageCompleted(id int) error {
	_, err := db.conn.Exec(`
		UPDATE scrape_pages SET status = 'completed', processed_at = NOW() WHERE id = ?
	`, id)
	return err
}

func (db *DB) MarkPageFailed(id int, errMsg string) error {
	_, err := db.conn.Exec(`
		UPDATE scrape_pages SET status = 'failed', error_message = ? WHERE id = ?
	`, errMsg, id)
	return err
}

// =========================================================
// BRANDS
// =========================================================

func (db *DB) UpsertBrand(name, slug, url, imageURL string, fragranceCount int) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO brands (name, slug, url, image_url, fragrance_count)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			image_url = COALESCE(VALUES(image_url), image_url),
			fragrance_count = VALUES(fragrance_count),
			updated_at = NOW()
	`, name, slug, url, nullStr(imageURL), fragranceCount)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// ON DUPLICATE KEY UPDATE: buscar pelo url
		row := db.conn.QueryRow("SELECT id FROM brands WHERE url = ?", url)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (db *DB) UpdateBrandDetails(brandID int64, country, description, mainActivity, groupCompany string) error {
	_, err := db.conn.Exec(`
		UPDATE brands SET
			country = COALESCE(?, country),
			description = COALESCE(?, description),
			main_activity = COALESCE(?, main_activity),
			group_company = COALESCE(?, group_company),
			scrape_status = 'completed',
			scraped_at = NOW(),
			updated_at = NOW()
		WHERE id = ?
	`, nullStr(country), nullStr(description), nullStr(mainActivity), nullStr(groupCompany), brandID)
	return err
}

func (db *DB) GetPendingBrands(limit int) ([]Brand, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, slug, url, image_url, fragrance_count,
			   country, description, main_activity, group_company,
			   scrape_status, scrape_error, attempts
		FROM brands
		WHERE scrape_status IN ('pending', 'failed')
		AND attempts < 3
		ORDER BY attempts ASC, id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var brands []Brand
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.Name, &b.Slug, &b.URL, &b.ImageURL,
			&b.FragranceCount, &b.Country, &b.Description, &b.MainActivity,
			&b.GroupCompany, &b.ScrapeStatus, &b.ScrapeError, &b.Attempts); err != nil {
			return nil, err
		}
		brands = append(brands, b)
	}
	return brands, nil
}

// GetPendingBrandsForUpdate pega marcas pendentes com lock para workers concorrentes
func (db *DB) GetPendingBrandsForUpdate(limit int) ([]Brand, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(`
		SELECT id, name, slug, url, image_url, fragrance_count,
			   scrape_status, attempts
		FROM brands
		WHERE scrape_status = 'pending'
		AND attempts < 3
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	var brands []Brand
	var ids []interface{}
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.Name, &b.Slug, &b.URL, &b.ImageURL,
			&b.FragranceCount, &b.ScrapeStatus, &b.Attempts); err != nil {
			rows.Close()
			tx.Rollback()
			return nil, err
		}
		brands = append(brands, b)
		ids = append(ids, b.ID)
	}
	rows.Close()

	// Marcar como in_progress
	if len(ids) > 0 {
		query := "UPDATE brands SET scrape_status = 'in_progress' WHERE id IN (?" + repeatSQL(len(ids)-1) + ")"
		_, err = tx.Exec(query, ids...)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return brands, nil
}

func (db *DB) MarkBrandStatus(brandID int, status string, errMsg string) error {
	if status == "failed" {
		_, err := db.conn.Exec(`
			UPDATE brands SET scrape_status = ?, scrape_error = ?,
			attempts = attempts + 1, updated_at = NOW()
			WHERE id = ?
		`, status, errMsg, brandID)
		return err
	}
	_, err := db.conn.Exec(`
		UPDATE brands SET scrape_status = ?, scrape_error = NULL, updated_at = NOW()
		WHERE id = ?
	`, status, brandID)
	return err
}

// =========================================================
// FRAGRANCES
// =========================================================

func (db *DB) UpsertFragrance(brandID int64, name, slug, url, imageURL, gender string, year int, collection string) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO fragrances (brand_id, name, slug, url, image_url, gender, year, collection)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			image_url = COALESCE(VALUES(image_url), image_url),
			gender = COALESCE(VALUES(gender), gender),
			year = COALESCE(VALUES(year), year),
			collection = COALESCE(VALUES(collection), collection),
			updated_at = NOW()
	`, brandID, name, slug, url, nullStr(imageURL), nullStr(gender), nullInt(year), nullStr(collection))
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		row := db.conn.QueryRow("SELECT id FROM fragrances WHERE url = ?", url)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (db *DB) UpdateFragranceDetails(fragID int64, description string, year int, imageURL string) error {
	_, err := db.conn.Exec(`
		UPDATE fragrances SET
			description = COALESCE(?, description),
			year = COALESCE(?, year),
			image_url = COALESCE(?, image_url),
			scrape_status = 'completed',
			scraped_at = NOW(),
			updated_at = NOW()
		WHERE id = ?
	`, nullStr(description), nullInt(year), nullStr(imageURL), fragID)
	return err
}

func (db *DB) GetPendingFragrancesForUpdate(limit int) ([]Fragrance, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(`
		SELECT f.id, f.brand_id, b.name as brand_name, f.name, f.slug, f.url,
			   f.image_url, f.gender, f.year, f.scrape_status, f.attempts
		FROM fragrances f
		JOIN brands b ON b.id = f.brand_id
		WHERE f.scrape_status = 'pending'
		AND f.attempts < 3
		ORDER BY f.id ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	var frags []Fragrance
	var ids []interface{}
	for rows.Next() {
		var f Fragrance
		if err := rows.Scan(&f.ID, &f.BrandID, &f.BrandName, &f.Name, &f.Slug, &f.URL,
			&f.ImageURL, &f.Gender, &f.Year, &f.ScrapeStatus, &f.Attempts); err != nil {
			rows.Close()
			tx.Rollback()
			return nil, err
		}
		frags = append(frags, f)
		ids = append(ids, f.ID)
	}
	rows.Close()

	if len(ids) > 0 {
		query := "UPDATE fragrances SET scrape_status = 'in_progress' WHERE id IN (?" + repeatSQL(len(ids)-1) + ")"
		_, err = tx.Exec(query, ids...)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return frags, nil
}

func (db *DB) MarkFragranceStatus(fragID int, status string, errMsg string) error {
	if status == "failed" {
		_, err := db.conn.Exec(`
			UPDATE fragrances SET scrape_status = ?, scrape_error = ?,
			attempts = attempts + 1, updated_at = NOW()
			WHERE id = ?
		`, status, errMsg, fragID)
		return err
	}
	_, err := db.conn.Exec(`
		UPDATE fragrances SET scrape_status = ?, scrape_error = NULL, updated_at = NOW()
		WHERE id = ?
	`, status, fragID)
	return err
}

// =========================================================
// PERFUMERS
// =========================================================

func (db *DB) UpsertPerfumer(name, url string) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO perfumers (name, url)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE url = COALESCE(VALUES(url), url), updated_at = NOW()
	`, name, nullStr(url))
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		row := db.conn.QueryRow("SELECT id FROM perfumers WHERE name = ?", name)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (db *DB) LinkFragrancePerfumer(fragID, perfID int64) error {
	_, err := db.conn.Exec(`
		INSERT IGNORE INTO fragrance_perfumers (fragrance_id, perfumer_id) VALUES (?, ?)
	`, fragID, perfID)
	return err
}

// =========================================================
// NOTES
// =========================================================

func (db *DB) UpsertNote(name, url, imageURL string) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO notes (name, url, image_url)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			url = COALESCE(VALUES(url), url),
			image_url = COALESCE(VALUES(image_url), image_url),
			updated_at = NOW()
	`, name, nullStr(url), nullStr(imageURL))
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		row := db.conn.QueryRow("SELECT id FROM notes WHERE name = ?", name)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (db *DB) LinkFragranceNote(fragID, noteID int64, noteType string) error {
	_, err := db.conn.Exec(`
		INSERT IGNORE INTO fragrance_notes (fragrance_id, note_id, note_type) VALUES (?, ?, ?)
	`, fragID, noteID, noteType)
	return err
}

// =========================================================
// ACCORDS
// =========================================================

func (db *DB) UpsertAccord(name string) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO accords (name) VALUES (?)
		ON DUPLICATE KEY UPDATE name = VALUES(name)
	`, name)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		row := db.conn.QueryRow("SELECT id FROM accords WHERE name = ?", name)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (db *DB) LinkFragranceAccord(fragID, accordID int64, percentage float64) error {
	_, err := db.conn.Exec(`
		INSERT INTO fragrance_accords (fragrance_id, accord_id, percentage)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE percentage = VALUES(percentage)
	`, fragID, accordID, percentage)
	return err
}

// =========================================================
// LOGGING
// =========================================================

func (db *DB) Log(level, step, message string, context map[string]interface{}) error {
	var ctxJSON []byte
	if context != nil {
		ctxJSON, _ = json.Marshal(context)
	}
	_, err := db.conn.Exec(`
		INSERT INTO scrape_log (level, step, message, context_json) VALUES (?, ?, ?, ?)
	`, level, step, message, ctxJSON)
	return err
}

// =========================================================
// PROGRESS
// =========================================================

func (db *DB) GetProgress() (*Progress, error) {
	p := &Progress{
		Brands:     make(map[string]int),
		Fragrances: make(map[string]int),
	}

	// Brands
	rows, err := db.conn.Query("SELECT scrape_status, COUNT(*) FROM brands GROUP BY scrape_status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		p.Brands[status] = count
	}
	rows.Close()

	// Fragrances
	rows, err = db.conn.Query("SELECT scrape_status, COUNT(*) FROM fragrances GROUP BY scrape_status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		p.Fragrances[status] = count
	}
	rows.Close()

	db.conn.QueryRow("SELECT COUNT(*) FROM perfumers").Scan(&p.Perfumers)
	db.conn.QueryRow("SELECT COUNT(*) FROM notes").Scan(&p.Notes)
	db.conn.QueryRow("SELECT COUNT(*) FROM accords").Scan(&p.Accords)

	return p, nil
}

// =========================================================
// RETRY
// =========================================================

func (db *DB) RetryFailed(entity string) (int64, error) {
	var total int64

	if entity == "all" || entity == "brands" {
		res, _ := db.conn.Exec(`
			UPDATE brands SET scrape_status = 'pending', scrape_error = NULL
			WHERE scrape_status = 'failed' AND attempts < 5
		`)
		n, _ := res.RowsAffected()
		total += n
	}

	if entity == "all" || entity == "fragrances" {
		res, _ := db.conn.Exec(`
			UPDATE fragrances SET scrape_status = 'pending', scrape_error = NULL
			WHERE scrape_status = 'failed' AND attempts < 5
		`)
		n, _ := res.RowsAffected()
		total += n
	}

	if entity == "all" || entity == "pages" {
		res, _ := db.conn.Exec(`
			UPDATE scrape_pages SET status = 'pending', error_message = NULL
			WHERE status = 'failed'
		`)
		n, _ := res.RowsAffected()
		total += n
	}

	return total, nil
}

// =========================================================
// HELPERS
// =========================================================

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullInt(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

func repeatSQL(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += ",?"
	}
	return s
}
