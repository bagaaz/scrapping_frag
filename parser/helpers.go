package parser

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// absoluteURL converte URL relativa para absoluta
func absoluteURL(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return BaseURL + href
}

// extractSlug extrai slug da URL
func extractSlug(url string) string {
	if url == "" {
		return ""
	}
	clean := strings.TrimRight(url, "/")
	clean = strings.Replace(clean, ".html", "", 1)
	parts := strings.Split(clean, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// getImgSrc extrai src de uma imagem, tentando vários atributos
func getImgSrc(img *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src"} {
		if val, exists := img.Attr(attr); exists && val != "" {
			return absoluteURL(val)
		}
	}
	return ""
}
