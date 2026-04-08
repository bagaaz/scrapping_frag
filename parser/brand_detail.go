package parser

import (
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// BrandDetail contém os detalhes da marca + lista de fragrâncias
type BrandDetail struct {
	Country      string
	Description  string
	MainActivity string
	GroupCompany string
	Collections  []string
	Fragrances   []FragranceListItem
}

// FragranceListItem é uma fragrância extraída da página da marca
type FragranceListItem struct {
	Name       string
	Slug       string
	URL        string
	ImageURL   string
	Gender     string
	Year       int
	Collection string
}

// ParseBrandDetail extrai detalhes da marca e lista de fragrâncias
//
// Estrutura real do HTML:
//
// Coleções: div#collections-section contém links <a href="#CollectionName">
//   - Algumas ficam escondidas (display:none) mas estão no HTML
//
// Fragrâncias: div#list-view contém:
//   - div.tw-gridlist-section-header > h2.tw-gridlist-section-title (separador de coleção)
//   - a.prefumeHbox.tw-listview-item (cada fragrância)
//     - h3.tw-perfume-title (nome)
//     - p.tw-perfume-designer (marca)
//     - span.tw-year-badge (ano)
//     - Gênero na classe: tw-listview-item-unisex / tw-listview-item-male / tw-listview-item-female
//     - img dentro de picture (imagem)
func ParseBrandDetail(html string) (*BrandDetail, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	detail := &BrandDetail{}

	// --- Descrição ---
	// Estrutura real: <div id="descAAA"><p>...</p></div>
	descSel := doc.Find("#descAAA p, .designer-description, #textblock, div[itemprop='description']")
	if descSel.Length() > 0 {
		detail.Description = strings.TrimSpace(descSel.First().Text())
	} else {
		doc.Find("p").EachWithBreak(func(i int, s *goquery.Selection) bool {
			text := strings.TrimSpace(s.Text())
			if len(text) > 100 {
				detail.Description = text
				return false
			}
			return true
		})
	}

	// --- Dados estruturados (País, Atividade, Grupo) ---
	// Estrutura real: texto "País:" seguido de <a href="/country/...">Value</a><br>
	// Extrair via links de /country/ e /industry/
	doc.Find("a[href*='/country/']").Each(func(i int, s *goquery.Selection) {
		if detail.Country == "" {
			detail.Country = strings.TrimSpace(s.Text())
		}
	})
	doc.Find("a[href*='/industry/']").Each(func(i int, s *goquery.Selection) {
		if detail.MainActivity == "" {
			detail.MainActivity = strings.TrimSpace(s.Text())
		}
	})
	// Grupo/parent company: procurar no texto da sidebar
	doc.Find("a[href*='/group/']").Each(func(i int, s *goquery.Selection) {
		if detail.GroupCompany == "" {
			detail.GroupCompany = strings.TrimSpace(s.Text())
		}
	})

	// --- Coleções ---
	doc.Find("#collections-section a[href^='#']").Each(func(i int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Text())
		if name != "" && name != "All Fragrances" {
			detail.Collections = append(detail.Collections, name)
		}
	})

	// --- Fragrâncias ---
	seen := make(map[string]bool)
	currentCollection := "All Fragrances"

	// Iterar sobre os filhos do #list-view para rastrear separadores de coleção
	doc.Find("#list-view > div#list-view > *").Each(func(i int, s *goquery.Selection) {
		// Verificar se é um separador de coleção
		if s.HasClass("tw-gridlist-section-header") {
			title := s.Find("h2.tw-gridlist-section-title, .tw-gridlist-section-title").First()
			if title.Length() > 0 {
				currentCollection = strings.TrimSpace(title.Text())
			}
			return
		}

		// Verificar se é um link de fragrância
		if !s.Is("a[href*='/perfume/']") {
			return
		}

		href, exists := s.Attr("href")
		if !exists {
			return
		}

		url := absoluteURL(href)
		if seen[url] {
			return
		}

		// Nome: h3.tw-perfume-title
		name := ""
		if title := s.Find("h3.tw-perfume-title").First(); title.Length() > 0 {
			name = strings.TrimSpace(title.Text())
		}
		if name == "" {
			return
		}

		// Imagem: img dentro do link
		imageURL := ""
		if img := s.Find("img").First(); img.Length() > 0 {
			imageURL = getImgSrc(img)
		}

		// Gênero: pela classe do link
		gender := detectGenderFromClass(s)

		// Ano: span.tw-year-badge
		year := 0
		if yearSpan := s.Find("span.tw-year-badge, .tw-year-badge").First(); yearSpan.Length() > 0 {
			yearText := strings.TrimSpace(yearSpan.Text())
			if y, err := strconv.Atoi(yearText); err == nil {
				year = y
			}
		}

		slug := extractSlug(href)
		seen[url] = true

		detail.Fragrances = append(detail.Fragrances, FragranceListItem{
			Name:       name,
			Slug:       slug,
			URL:        url,
			ImageURL:   imageURL,
			Gender:     gender,
			Year:       year,
			Collection: currentCollection,
		})
	})

	// Fallback: se o seletor acima não pegou nada, tentar seletor mais genérico
	if len(detail.Fragrances) == 0 {
		currentCollection = "All Fragrances"

		// Iterar sobre todos os filhos diretos do list-view
		listView := doc.Find("#list-view").Last()
		if listView.Length() > 0 {
			listView.Children().Each(func(i int, s *goquery.Selection) {
				// Separador de coleção
				if s.HasClass("tw-gridlist-section-header") {
					title := s.Find("h2, .tw-gridlist-section-title").First()
					if title.Length() > 0 {
						currentCollection = strings.TrimSpace(title.Text())
					}
					return
				}

				// Link de fragrância
				if s.Is("a") {
					href, _ := s.Attr("href")
					if !strings.Contains(href, "/perfume/") {
						return
					}

					url := absoluteURL(href)
					if seen[url] {
						return
					}

					name := ""
					if title := s.Find(".tw-perfume-title").First(); title.Length() > 0 {
						name = strings.TrimSpace(title.Text())
					}
					if name == "" {
						name = strings.TrimSpace(s.Find("h3").First().Text())
					}
					if name == "" {
						return
					}

					imageURL := ""
					if img := s.Find("img").First(); img.Length() > 0 {
						imageURL = getImgSrc(img)
					}

					gender := detectGenderFromClass(s)

					year := 0
					if yearSpan := s.Find(".tw-year-badge").First(); yearSpan.Length() > 0 {
						yearText := strings.TrimSpace(yearSpan.Text())
						if y, err := strconv.Atoi(yearText); err == nil {
							year = y
						}
					}

					slug := extractSlug(href)
					seen[url] = true

					detail.Fragrances = append(detail.Fragrances, FragranceListItem{
						Name:       name,
						Slug:       slug,
						URL:        url,
						ImageURL:   imageURL,
						Gender:     gender,
						Year:       year,
						Collection: currentCollection,
					})
				}
			})
		}
	}

	return detail, nil
}

// detectGenderFromClass detecta gênero pela classe CSS do elemento
func detectGenderFromClass(s *goquery.Selection) string {
	class, _ := s.Attr("class")
	class = strings.ToLower(class)

	switch {
	case strings.Contains(class, "tw-listview-item-unisex"):
		return "unissex"
	case strings.Contains(class, "tw-listview-item-male") || strings.Contains(class, "tw-listview-item-men"):
		return "masculino"
	case strings.Contains(class, "tw-listview-item-female") || strings.Contains(class, "tw-listview-item-women"):
		return "feminino"
	}

	// Fallback: procurar no texto
	return detectGenderFromText(s)
}

func detectGenderFromText(s *goquery.Selection) string {
	searchArea := strings.ToLower(s.Text())

	switch {
	case strings.Contains(searchArea, "masculino") || strings.Contains(searchArea, "para homens"):
		return "masculino"
	case strings.Contains(searchArea, "feminino") || strings.Contains(searchArea, "para mulheres"):
		return "feminino"
	case strings.Contains(searchArea, "unissex") || strings.Contains(searchArea, "unisex"):
		return "unissex"
	}
	return ""
}
