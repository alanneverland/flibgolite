package fb2

import (
	//"fmt"
	"strconv"
	"strings"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
)

func (fb *FB2) GetFormat() string {
	return "fb2"
}

func (fb *FB2) GetTitle() string {
	return strings.TrimSpace(fb.Description.TitleInfo.BookTitle)
}

func (fb *FB2) GetSort() string {
	return parser.GetSortTitle(fb.Description.TitleInfo.BookTitle, parser.GetLanguageTag(fb.Description.TitleInfo.Lang))
}

func (fb *FB2) GetYear() string {
	year := strconv.Itoa(fb.Description.PublishInfo.Year)
	if year == "" {
		year = fb.Description.TitleInfo.Date
	}
	rYear := []rune(year)
	if len(rYear) > 4 {
		rYear = rYear[len(rYear)-4:]
	}
	return strings.TrimSpace(string(rYear))
}

func (fb *FB2) GetPlot() string {
	return parser.StripHTMLTags(strings.Join(fb.Description.TitleInfo.Annotation.P, "\n"))
}

func (fb *FB2) GetCover() string {
	return strings.TrimPrefix(fb.Description.TitleInfo.CoverPage.Image.Href, "#")
}

func (fb *FB2) GetLanguage() *model.Language {
	return parser.GetLanguage(fb.Description.TitleInfo.Lang)
}


/*func (fb *FB2) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0, len(fb.Description.TitleInfo.Authors))
	if len(fb.Description.TitleInfo.Authors) == 1 &&
		fb.Description.TitleInfo.Authors[0].FirstName == "" &&
		fb.Description.TitleInfo.Authors[0].MiddleName == "" &&
		fb.Description.TitleInfo.Authors[0].LastName != "" &&
		strings.Contains(fb.Description.TitleInfo.Authors[0].LastName, ",") { 
		aLN := strings.Split(fb.Description.TitleInfo.Authors[0].LastName, ",")
		for _, a := range aLN {
			author := parser.AuthorByFullName(a)
			if author.Sort != "" {
				authors = append(authors, author)
			}
		}
		return authors
	}
	for _, a := range fb.Description.TitleInfo.Authors {
		author := parser.AuthorByFullName(fmt.Sprintf("%s %s %s", a.FirstName, a.MiddleName, a.LastName))
		if author.Sort != "" {
			authors = append(authors, author)
		}
	}
	if len(authors) == 0 {
		authors = append(authors,
			&model.Author{
				Name: "[author not specified]",
				Sort: "[author not specified]",
			},
		)
	}
	return authors
}
*/

func (fb *FB2) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0, len(fb.Description.TitleInfo.Authors))
	
	lang := fb.Description.TitleInfo.Lang

	if len(fb.Description.TitleInfo.Authors) == 1 &&
		fb.Description.TitleInfo.Authors[0].FirstName == "" &&
		fb.Description.TitleInfo.Authors[0].MiddleName == "" &&
		fb.Description.TitleInfo.Authors[0].LastName != "" &&
		strings.Contains(fb.Description.TitleInfo.Authors[0].LastName, ",") {
		
		aLN := strings.Split(fb.Description.TitleInfo.Authors[0].LastName, ",")
		for _, aStr := range aLN {
			name := parser.ParseFullName(aStr)
			
			fullName := strings.TrimSpace(name.Last + " " + name.First + " " + name.Middle)
			for strings.Contains(fullName, "  ") {
				fullName = strings.ReplaceAll(fullName, "  ", " ")
			}

			fullName = strings.TrimSpace(fullName)
			
			if fullName != "" {
				authors = append(authors, &model.Author{
					Name: fullName,
					Sort: parser.GetSortSeriesOrAuthor(fullName, lang),
				})
			}
		}
		return authors
	}

	for _, a := range fb.Description.TitleInfo.Authors {

		fullName := strings.TrimSpace(a.LastName + " " + a.FirstName + " " + a.MiddleName)

		for strings.Contains(fullName, "  ") {
			fullName = strings.ReplaceAll(fullName, "  ", " ")
		}
		
		if fullName != "" {
			authors = append(authors, &model.Author{
				Name: fullName,
				Sort: parser.GetSortSeriesOrAuthor(fullName, lang),
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

func (fb *FB2) GetGenres() []string {
	var finalGenres []string

	for _, raw := range fb.Description.TitleInfo.Genres {
		processed := parser.ProcessGenres(raw)
		finalGenres = append(finalGenres, strings.Fields(processed)...)
	}

	return finalGenres
}

func (fb *FB2) GetKeywords() string {
	var parts []string

	kw := strings.TrimSpace(fb.Description.TitleInfo.Keywords)
	if kw != "" {
		parts = append(parts, kw)
	}

	genresStr := strings.Join(fb.GetGenres(), ", ")
	if genresStr != "" {
		parts = append(parts, genresStr)
	}

	return strings.Join(parts, ", ")
}

func (fb *FB2) GetSequences() []*model.Sequence {
	var seqs []*model.Sequence
	seen := make(map[string]bool)
	
	lang := fb.Description.TitleInfo.Lang

	for _, s := range fb.Description.TitleInfo.Series {
		name := strings.TrimSpace(s.Name)
		name = parser.Title(name, lang)

		if name != "" && !seen[name] {
			seqs = append(seqs, &model.Sequence{
				Name: name,
				Num:  s.Number,
				Sort: parser.GetSortSeriesOrAuthor(name, lang),
			})
			seen[name] = true
		}
	}

	/*
	for _, s := range fb.Description.PublishInfo.Series {
		name := strings.TrimSpace(s.Name)
		name = parser.Title(name, lang)
		
		if name != "" && !seen[name] {
			seqs = append(seqs, &model.Sequence{
				Name: name,
				Num:  s.Number,
				Sort: parser.GetSortSeriesOrAuthor(name, lang),
			})
			seen[name] = true
		}
	}
	*/

	return seqs
}
