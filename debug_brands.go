//go:build ignore

package main

import (
	"fmt"
	"io"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	"github.com/PuerkitoBio/goquery"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func main() {
	jar := tls_client.NewCookieJar()
	client, _ := tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithClientProfile(profiles.Chrome_124),
		tls_client.WithCookieJar(jar),
		tls_client.WithRandomTLSExtensionOrder(),
	)

	req, _ := http.NewRequest("GET", "https://www.fragrantica.com.br/designers-1/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("ERRO:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	// 1. Total de links /desenhista/ na página inteira
	allLinks := doc.Find("a[href*='/desenhista/']")
	fmt.Printf("1) Total links /desenhista/ na pagina: %d\n\n", allLinks.Length())

	// 2. Links dentro de div.grid
	gridLinks := doc.Find("div.grid a[href*='/desenhista/']")
	fmt.Printf("2) Links /desenhista/ dentro de div.grid: %d\n\n", gridLinks.Length())

	// 3. div.h-full dentro de div.grid
	gridCards := doc.Find("div.grid div.h-full")
	fmt.Printf("3) div.h-full dentro de div.grid: %d\n\n", gridCards.Length())

	// 4. div.mt-8 > div.grid
	mt8Grids := doc.Find("div.mt-8 div.grid a[href*='/desenhista/']")
	fmt.Printf("4) Links /desenhista/ dentro de div.mt-8 > div.grid: %d\n\n", mt8Grids.Length())

	// 5. Mostrar os primeiros 5 links GLOBAIS com contexto
	fmt.Println("=== Primeiros 5 links GLOBAIS ===")
	allLinks.Each(func(i int, s *goquery.Selection) {
		if i < 5 {
			href, _ := s.Attr("href")
			p1class, _ := s.Parent().Attr("class")
			p2class, _ := s.Parent().Parent().Attr("class")
			p3class, _ := s.Parent().Parent().Parent().Attr("class")
			fmt.Printf("  [%d] %s\n      parent: %.80s\n      grandparent: %.80s\n      great-gp: %.80s\n",
				i, href, p1class, p2class, p3class)
		}
	})

	fmt.Println("\n=== Primeiros 5 links dentro de div.grid ===")
	gridLinks.Each(func(i int, s *goquery.Selection) {
		if i < 5 {
			href, _ := s.Attr("href")
			name := strings.TrimSpace(s.Text())
			fmt.Printf("  [%d] %s -> %q\n", i, href, name)
		}
	})

	// 6. Contar quantos div.grid existem na página
	grids := doc.Find("div.grid")
	fmt.Printf("\n6) Total de div.grid na pagina: %d\n", grids.Length())
	grids.Each(func(i int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		links := s.Find("a[href*='/desenhista/']").Length()
		fmt.Printf("   grid[%d] classes=%q links_desenhista=%d\n", i, class, links)
	})

	// 7. div.mt-8 com letra
	mt8Divs := doc.Find("div.mt-8")
	fmt.Printf("\n7) Total div.mt-8: %d\n", mt8Divs.Length())
	totalInMt8 := 0
	mt8Divs.Each(func(i int, s *goquery.Selection) {
		links := s.Find("a[href*='/desenhista/']").Length()
		totalInMt8 += links
		letterSpan := s.Find("span").First()
		letter := strings.TrimSpace(letterSpan.Text())
		if len(letter) > 5 {
			letter = letter[:5]
		}
		fmt.Printf("   mt-8[%d] letra=%q links=%d\n", i, letter, links)
	})
	fmt.Printf("   TOTAL em mt-8: %d\n", totalInMt8)

	// 8. Salvar HTML para inspeção
	fmt.Printf("\n8) Tamanho total HTML: %d bytes\n", len(html))
}
