package parser

import (
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const BaseURL = "https://www.fragrantica.com.br"

// BrandListItem é uma marca extraída da listagem
type BrandListItem struct {
	Name           string
	Slug           string
	URL            string
	ImageURL       string
	FragranceCount int
}

// ParseBrandsList extrai marcas de uma página de listagem (designers-{N}/)
//
// Estrutura real do HTML (dentro de div.grid):
//
//	<div class="mt-8">
//	  <span id="A" class="text-2xl ..."> A </span>
//	  <div class="grid grid-cols-2 md:grid-cols-3 gap-2 auto-rows-fr">
//	    <div class="h-full">
//	      <div class="h-full p-4 relative flex flex-row items-center ...">
//	        <img src="https://fimgs.net/mdimg/dizajneri/t.XXXX.jpg" alt="Brand" class="w-16 ...">
//	        <a href="/desenhista/Slug.html" class="text-teal-600 ...">
//	          <span class="absolute inset-0"></span>
//	          Brand Name
//	        </a>
//	        <span class="... absolute top-1 right-1 ...">COUNT</span>
//	      </div>
//	    </div>
//	  </div>
//	</div>
func ParseBrandsList(html string) ([]BrandListItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var brands []BrandListItem
	seen := make(map[string]bool)

	// Iterar sobre cada card dentro dos grids de conteúdo
	// Isso ignora links /desenhista/ que estejam em sidebar/navegação
	doc.Find("div.grid div.h-full").Each(func(i int, card *goquery.Selection) {
		// Procurar o link da marca dentro deste card
		link := card.Find("a[href*='/desenhista/']").First()
		if link.Length() == 0 {
			return
		}

		href, exists := link.Attr("href")
		if !exists || !strings.Contains(href, "/desenhista/") {
			return
		}

		url := absoluteURL(href)
		if seen[url] {
			return
		}

		// Nome: texto direto do link (ignorando spans internos vazios)
		name := ""
		link.Contents().Each(func(i int, c *goquery.Selection) {
			if goquery.NodeName(c) == "#text" {
				name += c.Text()
			}
		})
		name = strings.TrimSpace(name)

		// Fallback: texto completo
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		if name == "" {
			return
		}

		// Imagem: <img> dentro do mesmo card
		imageURL := ""
		if img := card.Find("img").First(); img.Length() > 0 {
			imageURL = getImgSrc(img)
		}

		// Contagem de fragrâncias: span com "absolute top-1 right-1"
		fragCount := 0
		card.Find("span").Each(func(i int, span *goquery.Selection) {
			class, _ := span.Attr("class")
			if strings.Contains(class, "top-1") && strings.Contains(class, "right-1") {
				countText := strings.TrimSpace(span.Text())
				if n, err := strconv.Atoi(countText); err == nil {
					fragCount = n
				}
			}
		})

		slug := extractSlug(href)

		seen[url] = true
		brands = append(brands, BrandListItem{
			Name:           name,
			Slug:           slug,
			URL:            url,
			ImageURL:       imageURL,
			FragranceCount: fragCount,
		})
	})

	return brands, nil
}
