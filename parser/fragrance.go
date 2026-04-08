package parser

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FragranceDetail contém todos os detalhes de uma fragrância
type FragranceDetail struct {
	Description string
	ImageURL    string
	Year        int
	Perfumers   []PerfumerInfo
	Notes       NotesInfo
	Accords     []AccordInfo
}

type PerfumerInfo struct {
	Name string
	URL  string
}

type NotesInfo struct {
	Top     []NoteInfo
	Heart   []NoteInfo
	Base    []NoteInfo
	General []NoteInfo // Notas sem classificação (quando não há pirâmide)
}

type NoteInfo struct {
	Name     string
	URL      string
	ImageURL string
}

type AccordInfo struct {
	Name       string
	Percentage float64
}

// ParseFragranceDetail extrai todos os detalhes de uma fragrância
//
// Estrutura real do HTML (fragrantica.com.br):
//
// Notas (pirâmide):
//
//	div#pyramid contém seções separadas por h4 > span com texto:
//	  "Notas de Topo" / "Notas de Coração" / "Notas de Base"
//	Cada seção tem div.pyramid-level-container com:
//	  a.pyramid-note-link[href="/notas/Nome-ID.html"]
//	    img[alt="NomeDaNota"]
//	    span.pyramid-note-label "Nome da Nota"
//
// Perfumistas: a[href*="/noses/"] ou a[href*="/nariz/"]
// Acordes: barras com style="width: XX%"
func ParseFragranceDetail(html string) (*FragranceDetail, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	detail := &FragranceDetail{}
	yearRe := regexp.MustCompile(`\b(19|20)\d{2}\b`)

	// --- Descrição ---
	// Estrutura real: <div id="perfume-description-content" itemprop="description"><p>...</p></div>
	// O texto da descrição está dentro do <p>, não do div inteiro (para evitar pegar lixo)
	descSel := doc.Find("#perfume-description-content p, [itemprop='description'] p")
	if descSel.Length() > 0 {
		detail.Description = strings.TrimSpace(descSel.First().Text())
	} else {
		// Fallback: pegar o div inteiro
		descDiv := doc.Find("#perfume-description-content, [itemprop='description']")
		if descDiv.Length() > 0 {
			detail.Description = strings.TrimSpace(descDiv.First().Text())
		}
	}

	// --- Imagem ---
	// Estrutura: <img itemprop="image" src="https://fimgs.net/mdimg/perfume-thumbs/375x500.ID.jpg">
	if img := doc.Find("img[itemprop='image']").First(); img.Length() > 0 {
		detail.ImageURL = getImgSrc(img)
	}

	// --- Ano ---
	title := doc.Find("h1, title").First()
	if title.Length() > 0 {
		if match := yearRe.FindString(title.Text()); match != "" {
			detail.Year, _ = strconv.Atoi(match)
		}
	}

	// --- Perfumistas ---
	// Site BR usa /narizes/ para perfumistas
	// Estrutura: a[href*='/narizes/'] > span (nome) + img (foto)
	perfSeen := make(map[string]bool)
	doc.Find("a[href*='/narizes/'], a[href*='/noses/']").Each(func(i int, s *goquery.Selection) {
		// Nome: span dentro do link ou texto direto
		name := ""
		if span := s.Find("span").First(); span.Length() > 0 {
			name = strings.TrimSpace(span.Text())
		}
		if name == "" {
			name = strings.TrimSpace(s.Text())
		}
		if name == "" || perfSeen[name] {
			return
		}
		href, _ := s.Attr("href")
		perfSeen[name] = true
		detail.Perfumers = append(detail.Perfumers, PerfumerInfo{
			Name: name,
			URL:  absoluteURL(href),
		})
	})

	// --- Notas (pirâmide) ---
	parseNotesPyramid(doc, &detail.Notes)

	// --- Acordes ---
	parseAccords(doc, &detail.Accords)

	return detail, nil
}

// parseNotesPyramid extrai notas da pirâmide do perfume
// Dois formatos possíveis:
//  1. COM pirâmide: h4 headers "Notas de Topo/Coração/Base" + a.pyramid-note-link
//  2. SEM pirâmide: lista simples de a.pyramid-note-link ou a[href*='/notas/'] sem categorização
func parseNotesPyramid(doc *goquery.Document, notes *NotesInfo) {
	pyramid := doc.Find("#pyramid")
	if pyramid.Length() == 0 {
		return
	}

	// Verificar se tem headers de pirâmide (Topo/Coração/Base)
	hasPyramidHeaders := false
	pyramid.Find("h4").Each(func(i int, s *goquery.Selection) {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if strings.Contains(text, "topo") || strings.Contains(text, "coração") || strings.Contains(text, "base") {
			hasPyramidHeaders = true
		}
	})

	if hasPyramidHeaders {
		// Formato 1: COM pirâmide — extrair notas categorizadas
		parsePyramidWithHeaders(pyramid, notes)
	} else {
		// Formato 2: SEM pirâmide — todas as notas vão como "general" (sem tipo)
		parseFlatNotesList(pyramid, notes)
	}

	// Fallback: se nenhuma estratégia funcionou, busca sequencial
	if len(notes.Top)+len(notes.Heart)+len(notes.Base)+len(notes.General) == 0 {
		parseNotesFallback(pyramid, notes)
	}

	// Fallback 2: links /notas/ dentro do pyramid
	if len(notes.Top)+len(notes.Heart)+len(notes.Base)+len(notes.General) == 0 {
		parseNotesFromLinks(doc, notes)
	}
}

// parsePyramidWithHeaders extrai notas COM separação Topo/Coração/Base
func parsePyramidWithHeaders(pyramid *goquery.Selection, notes *NotesInfo) {
	currentType := ""

	pyramid.Find("h4, div.pyramid-level-container, a.pyramid-note-link").Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "h4" {
			text := strings.ToLower(strings.TrimSpace(s.Text()))
			switch {
			case strings.Contains(text, "topo"):
				currentType = "top"
			case strings.Contains(text, "coração") || strings.Contains(text, "coracao"):
				currentType = "heart"
			case strings.Contains(text, "base") || strings.Contains(text, "fundo"):
				currentType = "base"
			}
			return
		}

		if s.HasClass("pyramid-level-container") {
			s.Find("a.pyramid-note-link").Each(func(j int, link *goquery.Selection) {
				addNote(link, notes, currentType)
			})
			return
		}

		if s.HasClass("pyramid-note-link") && currentType != "" {
			addNote(s, notes, currentType)
		}
	})
}

// parseFlatNotesList extrai notas SEM separação (lista simples)
func parseFlatNotesList(pyramid *goquery.Selection, notes *NotesInfo) {
	pyramid.Find("a.pyramid-note-link").Each(func(i int, link *goquery.Selection) {
		addNote(link, notes, "general")
	})

	// Fallback: se não achou por classe, tentar links /notas/
	if len(notes.General) == 0 {
		pyramid.Find("a[href*='/notas/']").Each(func(i int, link *goquery.Selection) {
			addNote(link, notes, "general")
		})
	}
}

// addNote adiciona uma nota à lista correta
func addNote(link *goquery.Selection, notes *NotesInfo, noteType string) {
	if noteType == "" {
		noteType = "general"
	}

	// Nome: span.pyramid-note-label ou alt da imagem
	name := ""
	if label := link.Find("span.pyramid-note-label, .pyramid-note-label").First(); label.Length() > 0 {
		name = strings.TrimSpace(label.Text())
	}
	if name == "" {
		if img := link.Find("img").First(); img.Length() > 0 {
			name, _ = img.Attr("alt")
			name = strings.TrimSpace(name)
		}
	}
	if name == "" {
		return
	}

	href, _ := link.Attr("href")
	imageURL := ""
	if img := link.Find("img").First(); img.Length() > 0 {
		imageURL = getImgSrc(img)
	}

	note := NoteInfo{
		Name:     name,
		URL:      absoluteURL(href),
		ImageURL: imageURL,
	}

	switch noteType {
	case "top":
		if !noteExists(notes.Top, name) {
			notes.Top = append(notes.Top, note)
		}
	case "heart":
		if !noteExists(notes.Heart, name) {
			notes.Heart = append(notes.Heart, note)
		}
	case "base":
		if !noteExists(notes.Base, name) {
			notes.Base = append(notes.Base, note)
		}
	case "general":
		if !noteExists(notes.General, name) {
			notes.General = append(notes.General, note)
		}
	}
}

// parseNotesFallback tenta extrair notas percorrendo a árvore do pyramid sequencialmente
func parseNotesFallback(pyramid *goquery.Selection, notes *NotesInfo) {
	currentType := ""

	// Percorrer TODOS os elementos descendentes procurando headers e links
	pyramid.Find("*").Each(func(i int, s *goquery.Selection) {
		text := strings.ToLower(strings.TrimSpace(s.Text()))

		// Detectar headers
		if len(text) < 30 {
			switch {
			case strings.Contains(text, "notas de topo"):
				currentType = "top"
				return
			case strings.Contains(text, "notas de coração") || strings.Contains(text, "notas de coracao"):
				currentType = "heart"
				return
			case strings.Contains(text, "notas de base"):
				currentType = "base"
				return
			}
		}

		// Processar links de notas
		if currentType != "" && goquery.NodeName(s) == "a" {
			href, _ := s.Attr("href")
			if strings.Contains(href, "/notas/") || strings.Contains(href, "/notes/") || strings.Contains(href, "/ingredients/") {
				addNote(s, notes, currentType)
			}
		}
	})
}

// parseNotesFromLinks fallback final: pegar todos os links de /notas/ na página
func parseNotesFromLinks(doc *goquery.Document, notes *NotesInfo) {
	doc.Find("a[href*='/notas/']").Each(func(i int, s *goquery.Selection) {
		// Verificar se está dentro do pyramid
		if s.Closest("#pyramid").Length() == 0 {
			return
		}

		name := ""
		if label := s.Find(".pyramid-note-label").First(); label.Length() > 0 {
			name = strings.TrimSpace(label.Text())
		}
		if name == "" {
			if img := s.Find("img").First(); img.Length() > 0 {
				name, _ = img.Attr("alt")
				name = strings.TrimSpace(name)
			}
		}
		if name == "" {
			name = strings.TrimSpace(s.Text())
		}
		if name == "" {
			return
		}

		href, _ := s.Attr("href")
		imageURL := ""
		if img := s.Find("img").First(); img.Length() > 0 {
			imageURL = getImgSrc(img)
		}

		// Sem informação de tipo, colocar tudo em top
		if !noteExists(notes.Top, name) {
			notes.Top = append(notes.Top, NoteInfo{
				Name:     name,
				URL:      absoluteURL(href),
				ImageURL: imageURL,
			})
		}
	})
}

func parseAccords(doc *goquery.Document, accords *[]AccordInfo) {
	pctRe := regexp.MustCompile(`width:\s*([\d.]+)%`)

	// Estratégia 1: Extrair dos query params do link /accords-search/
	// O link contém percentuais exatos: ?woody=100&aromatic=88&fresh+spicy=51&...
	doc.Find("a[href*='/accords-search/']").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		// Extrair query string
		qIdx := strings.Index(href, "?")
		if qIdx < 0 {
			return
		}
		query := href[qIdx+1:]
		pairs := strings.Split(query, "&")

		for _, pair := range pairs {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.ReplaceAll(kv[0], "+", " ")
			// Ignorar parâmetros que não são acordes
			if key == "f_from_perfume_id" || key == "" {
				continue
			}

			val, err := strconv.ParseFloat(kv[1], 64)
			if err != nil || val <= 0 {
				continue
			}

			*accords = append(*accords, AccordInfo{
				Name:       key,
				Percentage: val,
			})
		}
	})

	// Se conseguiu do link, retornar
	if len(*accords) > 0 {
		return
	}

	// Estratégia 2: Extrair das barras visuais
	// Estrutura: h6 "Principais Acordes" seguido de divs com style="width: XX%"
	// Nome dentro de span.truncate
	doc.Find("h6").Each(func(i int, s *goquery.Selection) {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if !strings.Contains(text, "acorde") && !strings.Contains(text, "accord") {
			return
		}

		// Container dos acordes é o parent do h6
		container := s.Parent()
		container.Find("div[style*='width']").Each(func(j int, bar *goquery.Selection) {
			// Nome dentro de span.truncate
			name := ""
			if span := bar.Find("span.truncate").First(); span.Length() > 0 {
				name = strings.TrimSpace(span.Text())
			}
			if name == "" {
				name = strings.TrimSpace(bar.Text())
			}
			if name == "" || len(name) > 100 {
				return
			}

			style, _ := bar.Attr("style")
			if matches := pctRe.FindStringSubmatch(style); len(matches) > 1 {
				pct, _ := strconv.ParseFloat(matches[1], 64)
				if pct > 0 {
					*accords = append(*accords, AccordInfo{
						Name:       name,
						Percentage: pct,
					})
				}
			}
		})
	})

	// Estratégia 3: Fallback genérico — qualquer div com width% e texto curto
	if len(*accords) == 0 {
		doc.Find("div[style*='width']").Each(func(i int, s *goquery.Selection) {
			name := ""
			if span := s.Find("span.truncate").First(); span.Length() > 0 {
				name = strings.TrimSpace(span.Text())
			}
			if name == "" {
				name = strings.TrimSpace(s.Text())
			}
			if name == "" || len(name) > 50 {
				return
			}

			style, _ := s.Attr("style")
			if matches := pctRe.FindStringSubmatch(style); len(matches) > 1 {
				pct, _ := strconv.ParseFloat(matches[1], 64)
				if pct > 0 {
					*accords = append(*accords, AccordInfo{
						Name:       name,
						Percentage: pct,
					})
				}
			}
		})
	}
}

func noteExists(notes []NoteInfo, name string) bool {
	for _, n := range notes {
		if n.Name == name {
			return true
		}
	}
	return false
}
