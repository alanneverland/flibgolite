package pdf

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
)

func (p *PDF) GetFormat() string {
	return "pdf"
}

func (p *PDF) GetTitle() string {
	title := strings.TrimSpace(p.Info["Title"])
	if title == "" {
		return strings.TrimSuffix(p.FileName, filepath.Ext(p.FileName))
	}
	return title
}

func (p *PDF) GetSort() string {

	langTag := parser.GetLanguageTag(p.getLangCode())
	
	return parser.GetSortTitle(p.GetTitle(), langTag)
}

func (p *PDF) GetYear() string {
	return parser.PickYear(p.Info["CreationDate"])
}

func (p *PDF) GetPlot() string {
	return strings.TrimSpace(p.Info["Subject"])
}

func (p *PDF) GetCover() string {
	return ""
}

func (p *PDF) GetLanguage() *model.Language {
	return parser.GetLanguage(p.getLangCode())
}

func (p *PDF) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0)
	rawAuthor := strings.TrimSpace(p.Info["Author"])
	
	lang := p.getLangCode()

	if rawAuthor != "" {
		name := parser.ParseFullName(rawAuthor)

		if name.First != "" || name.Last != "" {

			fullName := strings.TrimSpace(name.Last + " " + name.First + " " + name.Middle)

			for strings.Contains(fullName, "  ") {
				fullName = strings.ReplaceAll(fullName, "  ", " ")
			}

			authors = append(authors, &model.Author{
				Name: fullName,
				Sort: parser.GetSortSeriesOrAuthor(fullName, lang),
			})
		} else {

			authors = append(authors, &model.Author{
				Name: rawAuthor,
				Sort: parser.GetSortSeriesOrAuthor(rawAuthor, lang),
			})
		}
	}

	if len(authors) == 0 {
		authors = append(authors, &model.Author{
			Name: "[author not specified]",
			Sort: "[AUTHOR NOT SPECIFIED]",
		})
	}
	return authors
}

func (p *PDF) GetGenres() []string {
	var finalGenres []string

	keywords := p.Info["Keywords"]
	if keywords != "" {
		processed := parser.ProcessGenres(keywords)
		finalGenres = append(finalGenres, strings.Fields(processed)...)
	}

	return finalGenres
}

func (p *PDF) GetKeywords() string {
	return strings.Join(p.GetGenres(), ", ")
}

func (m *PDF) GetSequences() []*model.Sequence {
	return []*model.Sequence{}
}

func detectLanguageByAlphabet(text string) string {
	text = strings.ToLower(text)
	
	var hasCyrillic, hasLatin bool

	for _, r := range text {

		switch r {
		case 'і', 'ї', 'є', 'ґ':
			return "uk" 
		case 'ў':
			return "be" 
		case 'ъ', 'ы', 'э':
			return "ru" 
		case 'ä', 'ö', 'ü', 'ß':
			return "de" 
		case 'é', 'à', 'è', 'ù', 'â', 'ê', 'î', 'ô', 'û', 'ç':
			return "fr" 
		case 'ñ', 'á', 'í', 'ó', 'ú', '¿', '¡':
			return "es" 
		}

		if unicode.Is(unicode.Cyrillic, r) {
			hasCyrillic = true
		} else if unicode.Is(unicode.Latin, r) && unicode.IsLetter(r) {
			hasLatin = true
		} else if unicode.Is(unicode.Han, r) {
			return "zh" 
		} else if unicode.Is(unicode.Arabic, r) {
			return "ar" 
		}
	}

	if hasCyrillic {

		return "ru"
	}
	
	if hasLatin {

		return "en"
	}

	return "en" 
}

func (p *PDF) getLangCode() string {

	headerText := p.GetTitle() + " " + p.Info["Author"]
	if lang := detectLanguageByAlphabet(headerText); lang != "en" {
		return lang
	}

	if pageText := p.Info["FirstPageText"]; pageText != "" {
		if lang := detectLanguageByAlphabet(pageText); lang != "en" {
			return lang
		}
	}

	lang := strings.TrimSpace(p.Info["Lang"])
	if lang != "" {
		if idx := strings.Index(lang, "-"); idx != -1 {
			lang = lang[:idx]
		}
		return strings.ToLower(lang)
	}

	return "en"
}