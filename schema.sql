-- ============================================================
-- FRAGRANTICA SCRAPER - MySQL Schema
-- ============================================================
-- Filosofia do design:
-- 1. Cada entidade tem um `fragrantica_url` UNIQUE para evitar duplicatas
-- 2. Tabelas de scraping têm `scrape_status` para rastrear progresso
-- 3. Tabelas pivot sem duplicatas via chaves compostas
-- 4. Índices estratégicos para consultas frequentes do scraper
-- ============================================================

CREATE DATABASE IF NOT EXISTS fragrantica_db
    CHARACTER SET utf8mb4
    COLLATE utf8mb4_unicode_ci;

USE fragrantica_db;

-- ============================================================
-- TABELA: brands (Marcas)
-- Fonte: https://www.fragrantica.com.br/designers-{1..11}/
-- ============================================================
CREATE TABLE brands (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    slug            VARCHAR(255) NOT NULL,
    url             VARCHAR(500) NOT NULL,
    image_url       VARCHAR(500) NULL,
    fragrance_count INT UNSIGNED DEFAULT 0 COMMENT 'Qtd de perfumes listada no Fragrantica',
    country         VARCHAR(100) NULL,
    description     TEXT NULL,
    main_activity   VARCHAR(255) NULL COMMENT 'Atividade principal',
    group_company   VARCHAR(255) NULL COMMENT 'Empresa do grupo',

    -- Progress tracking
    scrape_status   ENUM('pending', 'in_progress', 'completed', 'failed') DEFAULT 'pending',
    scrape_error    TEXT NULL,
    attempts        TINYINT UNSIGNED DEFAULT 0,
    scraped_at      TIMESTAMP NULL COMMENT 'Quando o detalhe da marca foi scrapeado',

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_brand_url (url),
    UNIQUE KEY uk_brand_slug (slug),
    INDEX idx_brand_status (scrape_status),
    INDEX idx_brand_name (name)
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: fragrances (Fragrâncias/Perfumes)
-- Fonte: Página da marca (listagem) + página individual
-- ============================================================
CREATE TABLE fragrances (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    brand_id        INT UNSIGNED NOT NULL,
    name            VARCHAR(500) NOT NULL,
    slug            VARCHAR(500) NOT NULL,
    url             VARCHAR(500) NOT NULL,
    image_url       VARCHAR(500) NULL,
    gender          VARCHAR(50) NULL COMMENT 'masculino, feminino, unissex, etc.',
    year            SMALLINT UNSIGNED NULL COMMENT 'Ano de lançamento',
    description     TEXT NULL,

    -- Progress tracking
    scrape_status   ENUM('pending', 'in_progress', 'completed', 'failed') DEFAULT 'pending',
    scrape_error    TEXT NULL,
    attempts        TINYINT UNSIGNED DEFAULT 0,
    scraped_at      TIMESTAMP NULL COMMENT 'Quando o detalhe da fragrância foi scrapeado',

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_fragrance_url (url),
    INDEX idx_fragrance_brand (brand_id),
    INDEX idx_fragrance_status (scrape_status),
    INDEX idx_fragrance_gender (gender),
    INDEX idx_fragrance_year (year),

    CONSTRAINT fk_fragrance_brand
        FOREIGN KEY (brand_id) REFERENCES brands(id)
        ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: perfumers (Perfumistas/Noses)
-- Fonte: Página da fragrância
-- ============================================================
CREATE TABLE perfumers (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    url             VARCHAR(500) NULL,

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_perfumer_name (name),
    INDEX idx_perfumer_url (url)
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: fragrance_perfumers (Pivot: fragrância <-> perfumista)
-- ============================================================
CREATE TABLE fragrance_perfumers (
    fragrance_id    INT UNSIGNED NOT NULL,
    perfumer_id     INT UNSIGNED NOT NULL,

    PRIMARY KEY (fragrance_id, perfumer_id),

    CONSTRAINT fk_fp_fragrance
        FOREIGN KEY (fragrance_id) REFERENCES fragrances(id)
        ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_fp_perfumer
        FOREIGN KEY (perfumer_id) REFERENCES perfumers(id)
        ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: notes (Notas olfativas)
-- Fonte: Página da fragrância
-- ============================================================
CREATE TABLE notes (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    url             VARCHAR(500) NULL,
    image_url       VARCHAR(500) NULL,

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_note_name (name)
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: fragrance_notes (Pivot: fragrância <-> nota)
-- note_type: 'top' (saída), 'heart' (coração), 'base' (fundo)
-- ============================================================
CREATE TABLE fragrance_notes (
    fragrance_id    INT UNSIGNED NOT NULL,
    note_id         INT UNSIGNED NOT NULL,
    note_type       ENUM('top', 'heart', 'base') NOT NULL,

    PRIMARY KEY (fragrance_id, note_id, note_type),

    CONSTRAINT fk_fn_fragrance
        FOREIGN KEY (fragrance_id) REFERENCES fragrances(id)
        ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_fn_note
        FOREIGN KEY (note_id) REFERENCES notes(id)
        ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: accords (Acordes)
-- Fonte: Página da fragrância
-- ============================================================
CREATE TABLE accords (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_accord_name (name)
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: fragrance_accords (Pivot: fragrância <-> acorde)
-- Inclui a porcentagem/intensidade do acorde
-- ============================================================
CREATE TABLE fragrance_accords (
    fragrance_id    INT UNSIGNED NOT NULL,
    accord_id       INT UNSIGNED NOT NULL,
    percentage      DECIMAL(5,2) NOT NULL DEFAULT 0 COMMENT 'Porcentagem/intensidade do acorde',

    PRIMARY KEY (fragrance_id, accord_id),

    CONSTRAINT fk_fa_fragrance
        FOREIGN KEY (fragrance_id) REFERENCES fragrances(id)
        ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_fa_accord
        FOREIGN KEY (accord_id) REFERENCES accords(id)
        ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: scrape_pages (Controle de páginas de listagem)
-- Rastreia quais páginas de designers já foram processadas
-- ============================================================
CREATE TABLE scrape_pages (
    id              INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    page_type       VARCHAR(50) NOT NULL COMMENT 'brands_list, brand_fragrances',
    page_number     INT UNSIGNED NULL,
    url             VARCHAR(500) NOT NULL,
    status          ENUM('pending', 'completed', 'failed') DEFAULT 'pending',
    error_message   TEXT NULL,
    processed_at    TIMESTAMP NULL,

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_scrape_page (page_type, url),
    INDEX idx_scrape_status (status)
) ENGINE=InnoDB;

-- ============================================================
-- TABELA: scrape_log (Log de atividades do scraper)
-- ============================================================
CREATE TABLE scrape_log (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    level           ENUM('info', 'warning', 'error') DEFAULT 'info',
    step            VARCHAR(50) NOT NULL COMMENT 'brands_list, brand_detail, fragrance_detail',
    message         TEXT NOT NULL,
    context_json    JSON NULL COMMENT 'Dados extras em JSON',
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_log_level (level),
    INDEX idx_log_step (step),
    INDEX idx_log_created (created_at)
) ENGINE=InnoDB;

-- ============================================================
-- VIEWS úteis para monitoramento
-- ============================================================

-- Progresso geral por etapa
CREATE OR REPLACE VIEW v_scrape_progress AS
SELECT
    'brands' AS entity,
    COUNT(*) AS total,
    SUM(scrape_status = 'completed') AS completed,
    SUM(scrape_status = 'pending') AS pending,
    SUM(scrape_status = 'in_progress') AS in_progress,
    SUM(scrape_status = 'failed') AS failed,
    ROUND(SUM(scrape_status = 'completed') / COUNT(*) * 100, 2) AS progress_pct
FROM brands
UNION ALL
SELECT
    'fragrances' AS entity,
    COUNT(*) AS total,
    SUM(scrape_status = 'completed') AS completed,
    SUM(scrape_status = 'pending') AS pending,
    SUM(scrape_status = 'in_progress') AS in_progress,
    SUM(scrape_status = 'failed') AS failed,
    ROUND(SUM(scrape_status = 'completed') / COUNT(*) * 100, 2) AS progress_pct
FROM fragrances;

-- Marcas com perfumes pendentes
CREATE OR REPLACE VIEW v_brands_pending_fragrances AS
SELECT
    b.id,
    b.name,
    b.fragrance_count AS expected,
    COUNT(f.id) AS scraped_total,
    SUM(f.scrape_status = 'completed') AS scraped_done,
    SUM(f.scrape_status = 'pending') AS scraped_pending
FROM brands b
LEFT JOIN fragrances f ON f.brand_id = b.id
WHERE b.scrape_status = 'completed'
GROUP BY b.id, b.name, b.fragrance_count
HAVING scraped_pending > 0 OR scraped_total < b.fragrance_count;
