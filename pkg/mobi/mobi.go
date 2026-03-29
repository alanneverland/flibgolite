package mobi

import (
	"strings"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
)

func (m *MOBI) GetFormat() string {
	ext := m.mobiPath[strings.LastIndex(m.mobiPath, ".")+1:]
	return strings.ToLower(ext)
}

func (m *MOBI) GetTitle() string {
	return strings.TrimSpace(m.Title)
}

func (m *MOBI) GetSort() string {
	return parser.GetSortTitle(m.GetTitle(), parser.GetLanguageTag(m.Language))
}

func (m *MOBI) GetYear() string {
	return parser.PickYear(m.Year)
}

func (m *MOBI) GetPlot() string {
	desc := m.Plot
	desc = strings.ReplaceAll(desc, "<div>", "\n")
	desc = strings.ReplaceAll(desc, "<p>", "\n")
	desc = strings.ReplaceAll(desc, "<br>", "\n")
	desc = strings.ReplaceAll(desc, "<br/>", "\n")

	desc = parser.StripHTMLTags(desc)
	desc = strings.ReplaceAll(desc, "\r", "")
	desc = strings.ReplaceAll(desc, "\t", " ")

	for strings.Contains(desc, "  ") {
		desc = strings.ReplaceAll(desc, "  ", " ")
	}
	for strings.Contains(desc, "\n\n") {
		desc = strings.ReplaceAll(desc, "\n\n", "\n")
	}

	return strings.TrimSpace(desc)
}

func (m *MOBI) GetCover() string {
	return m.CoverIdx
}

func (m *MOBI) GetLanguage() *model.Language {
	return parser.GetLanguage(m.Language)
}

func (m *MOBI) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0)
	
	lang := m.Language
	
	for _, authorStr := range m.Authors {
		a := &model.Author{}
		name := parser.ParseFullName(authorStr)
		
		fullName := strings.TrimSpace(name.Last + " " + name.First + " " + name.Middle)
		
		for strings.Contains(fullName, "  ") {
			fullName = strings.ReplaceAll(fullName, "  ", " ")
		}
		
		
		a.Name = strings.TrimSpace(fullName)
		a.Sort = parser.GetSortSeriesOrAuthor(a.Name, lang)
		
		if len(a.Name) > 0 {
			authors = append(authors, a)
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

func isSeparator(r rune) bool {
	return r == ',' || r == ';' || r == '-' 
}

func (m *MOBI) GetGenres() []string {
	var finalGenres []string

	for _, raw := range m.Genres {
		processed := parser.ProcessGenres(raw)
		finalGenres = append(finalGenres, strings.Fields(processed)...)
	}

	return finalGenres
}

func (m *MOBI) GetKeywords() string {
	return strings.Join(m.GetGenres(), ", ")
}

func (m *MOBI) GetSequences() []*model.Sequence {
	return []*model.Sequence{}
}