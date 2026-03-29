package epub

import (
	"strings"

	"path"
	"fmt"
	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
)

func (ep *OPF) GetFormat() string {
	return "epub"
}

func (ep *OPF) GetTitle() string {
	if len(ep.Metadata.Title) > 0 {
		return strings.TrimSpace(ep.Metadata.Title[0])
	}
	return ""
}

func (ep *OPF) GetSort() string {
	l := ep.Lang
	if len(ep.Metadata.Language) > 0 {
		l = ep.Metadata.Language[0]
	}
	return parser.GetSortTitle(ep.GetTitle(), parser.GetLanguageTag(l))
}

func (ep *OPF) GetYear() string {
	return parser.PickYear(ep.Metadata.Date)
}

func (ep *OPF) GetPlot() string {
	desc := strings.Join(ep.Metadata.Description, "\n")

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

func (ep *OPF) GetCover() string {
	coverHref := ""
	
	for _, item := range ep.Manifest.Item {
		if strings.Contains(item.Properties, "cover-image") {
			coverHref = strings.TrimSpace(item.Href)
			break
		}
	}
	
	if coverHref == "" {
		content := ""
		for _, meta := range ep.Metadata.Meta {
			if meta.Name == "cover" {
				content = strings.TrimSpace(meta.Content)
				break
			}
		}
		if content != "" {			
			for _, item := range ep.Manifest.Item {
				if item.ID == content {
					coverHref = strings.TrimSpace(item.Href)
					break
				}
			}
		}
	}
	
		
	if coverHref == "" {
		for _, item := range ep.Manifest.Item {
			id := strings.ToLower(item.ID)
			href := strings.ToLower(item.Href)
			isImage := strings.HasPrefix(item.MediaType, "image/")

			if isImage && (strings.Contains(id, "cover") || strings.Contains(href, "cover")) {
				coverHref = strings.TrimSpace(item.Href)
				break
			}
		}
	}
	
	if coverHref == "" {
		for _, ref := range ep.Guide.Reference {
			if ref.Type == "cover" {
				coverHref = strings.TrimSpace(ref.Href)
				break
			}
		}
	}
	
	if coverHref != "" {		
		return path.Join(path.Dir(ep.opfPath), coverHref)
	}
	
	return ""
}

func (ep *OPF) GetLanguage() *model.Language {
	l := ep.Lang
	if len(ep.Metadata.Language) > 0 {
		l = ep.Metadata.Language[0]
	}
	return parser.GetLanguage(l)
}

func (ep *OPF) GetAuthors() []*model.Author {
	authors := make([]*model.Author, 0)
	
	lang := ep.Lang
	if len(ep.Metadata.Language) > 0 {
		lang = ep.Metadata.Language[0]
	}
	
	for _, cr := range ep.Metadata.Creator {
		a := &model.Author{}
		
		for _, meta := range ep.Metadata.Meta {
			if meta.Refines == "#"+cr.ID && meta.Property == "role" && meta.Text == "aut" {
				cr.Role = "aut"
			}
		}
		
		if cr.Role == "aut" || cr.Role == "" || len(ep.Metadata.Creator) == 1 {
			name := parser.ParseFullName(cr.Text)
			
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

func (ep *OPF) GetGenres() []string {
	var finalGenres []string

	for _, raw := range ep.Metadata.Subject {
		processed := parser.ProcessGenres(raw)

		finalGenres = append(finalGenres, strings.Fields(processed)...)
	}

	return finalGenres
}

func (ep *OPF) GetKeywords() string {
	return strings.Join(ep.GetGenres(), ", ")
}

func (ep *OPF) GetSequences() []*model.Sequence {
	var seqs []*model.Sequence
	seen := make(map[string]bool)
	
	lang := ep.Lang
	if len(ep.Metadata.Language) > 0 {
		lang = ep.Metadata.Language[0]
	}

	parseNum := func(s string) int {
		if s == "" {
			return 0
		}
		s = strings.ReplaceAll(s, ",", ".")
		var index float64
		fmt.Sscanf(s, "%f", &index)
		return int(index)
	}

	for _, meta := range ep.Metadata.Meta {
		if meta.Property == "belongs-to-collection" {
			name := parser.Title(strings.TrimSpace(meta.Text), lang)
			if name == "" {
				continue
			}

			num := 0
			if meta.ID != "" {
				serieID := "#" + meta.ID
				for _, m := range ep.Metadata.Meta {
					if m.Property == "group-position" && m.Refines == serieID {
						num = parseNum(m.Text)
						break
					}
				}
			}

			if !seen[name] {
				seqs = append(seqs, &model.Sequence{Name: name, Sort: parser.GetSortSeriesOrAuthor(name, lang), Num: num})
				seen[name] = true
			}
		}
	}

	var calName string
	var calNum int
	for _, meta := range ep.Metadata.Meta {
		if meta.Name == "calibre:series" {
			calName = parser.Title(strings.TrimSpace(meta.Content), lang)
		}
		if meta.Name == "calibre:series_index" {
			calNum = parseNum(meta.Content)
		}
	}
	if calName != "" && !seen[calName] {
		seqs = append(seqs, &model.Sequence{Name: calName, Sort: parser.GetSortSeriesOrAuthor(calName, lang), Num: calNum})
		seen[calName] = true
	}

	var genName string
	var genNum int
	for _, meta := range ep.Metadata.Meta {
		if meta.Name == "series" {
			genName = parser.Title(strings.TrimSpace(meta.Content), lang)
		}
		if meta.Name == "series_index" {
			genNum = parseNum(meta.Content)
		}
	}
	if genName != "" && !seen[genName] {
		seqs = append(seqs, &model.Sequence{Name: genName, Sort: parser.GetSortSeriesOrAuthor(genName, lang), Num: genNum})
	}

	return seqs
}