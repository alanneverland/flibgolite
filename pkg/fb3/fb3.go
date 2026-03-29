package fb3

import (
	"strings"
	"unicode"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
)

func (fb *FB3Description) GetFormat() string {
	return "fb3"
}

func (fb *FB3Description) GetTitle() string {
	return strings.TrimSpace(fb.Title.Main)
}

func (fb *FB3Description) GetSort() string {
	return parser.GetSortTitle(fb.GetTitle(), parser.GetLanguageTag(fb.Lang))
}

func (fb *FB3Description) GetYear() string {
	year := parser.PickYear(fb.Written.Date.Value)
	
	if year == "" {
		year = parser.PickYear(fb.Written.Date.Text)
	}

	return year
}

func (fb *FB3Description) GetPlot() string {

	desc := strings.Join(fb.Annotation.P, "\n")
	
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

func (fb *FB3Description) GetCover() string {
	return fb.coverPath
}

func (fb *FB3Description) GetLanguage() *model.Language {
	return parser.GetLanguage(fb.Lang)
}

func (fb *FB3Description) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0)

	subjects := append(fb.Relations.Subjects, fb.RelationsFb3.Subjects...)

	for _, s := range subjects {

		if s.Link != "author" {
			continue
		}

		firstName := strings.TrimSpace(s.FirstName)
		middleName := strings.TrimSpace(s.MiddleName)
		lastName := strings.TrimSpace(s.LastName)

		fullName := strings.TrimSpace(lastName + " " + firstName + " " + middleName)

		if fullName == "" && s.Title.Main != "" {
			name := parser.ParseFullName(s.Title.Main)
			fullName = strings.TrimSpace(name.Last + " " + name.First + " " + name.Middle)
		}

		for strings.Contains(fullName, "  ") {
			fullName = strings.ReplaceAll(fullName, "  ", " ")
		}

		fullName = strings.TrimSpace(fullName)
		

		if len(fullName) > 0 {
			authors = append(authors, &model.Author{
				Name: fullName,
				Sort: parser.GetSortSeriesOrAuthor(fullName, fb.Lang),
			})
		}
	}

	if len(authors) == 0 {
		authors = append(authors,
			&model.Author{
				Name: "[author not specified]",
				Sort: "[AUTHOR NOT SPECIFIED]",
			},
		)
	}

	return authors
}

func isSeparator(r rune) bool {
	return r == ',' || r == ';' || r == '-' || unicode.IsSpace(r)
}

func (fb *FB3Description) GetGenres() []string {
	var finalGenres []string


	rawTags := make([]string, 0, len(fb.Classification.Subjects)+len(fb.ClassificationFb3.Subjects))
	rawTags = append(rawTags, fb.Classification.Subjects...)
	rawTags = append(rawTags, fb.ClassificationFb3.Subjects...)

	for _, raw := range rawTags {
		processed := parser.ProcessGenres(raw)
		finalGenres = append(finalGenres, strings.Fields(processed)...)
	}

	return finalGenres
}

func (fb *FB3Description) GetKeywords() string {
	var parts []string

	kw := strings.TrimSpace(fb.Keywords)
	if kw != "" {
		parts = append(parts, kw)
	}

	genresStr := strings.Join(fb.GetGenres(), ", ")
	if genresStr != "" {
		parts = append(parts, genresStr)
	}

	return strings.Join(parts, ", ")
}

func (fb *FB3Description) GetSequences() []*model.Sequence {
	var seqs []*model.Sequence
	seen := make(map[string]bool)

	for _, s := range fb.Sequence {
		name := parser.Title(strings.TrimSpace(s.Title.Main), fb.Lang)
		if name != "" && !seen[name] {
			seqs = append(seqs, &model.Sequence{
				Name: name,
				Sort: parser.GetSortSeriesOrAuthor(name, fb.Lang),
				Num:  s.Number,
			})
			seen[name] = true
		}
	}
	
	return seqs
}