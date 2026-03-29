package genres

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/rlog"
	//"github.com/vinser/flibgolite/pkg/parser"
)

type GenresTree struct {
	XMLName xml.Name `xml:"fbgenrestransfer"`
	Genres  []Genre  `xml:"genre"`
	Accepted map[string]bool
	Rejected  map[string]bool
	AcceptAny bool            
}

type Genre struct {
	Value        string      `xml:"value,attr"`
	Descriptions []RootDescr `xml:"root-descr"`
	Subgenres    []Subgenre  `xml:"subgenres>subgenre"`
}

type RootDescr struct {
	Lang     string `xml:"lang,attr"`
	Title    string `xml:"genre-title,attr"`
	Detailed string `xml:"detailed,attr"`
}

type Subgenre struct {
	Value        string       `xml:"value,attr"`
	Descriptions []GenreDescr `xml:"genre-descr"`
	Alts         []GenreAlt   `xml:"genre-alt"`
}

type GenreDescr struct {
	Lang  string `xml:"lang,attr"`
	Title string `xml:"title,attr"`
}

type GenreAlt struct {
	Value  string `xml:"value,attr"`
	Format string `xml:"format,attr"`
}

//go:embed genres.xml
var GENRES_XML string

func NewGenresTree(treeFile, acceptStr, rejectStr string, logger *rlog.Log) *GenresTree {
	var b []byte
	var err error
	gt := &GenresTree{}
	b, err = os.ReadFile(treeFile)
	if err != nil {
		err = os.WriteFile(treeFile, []byte(GENRES_XML), 0664)
		if err != nil {
			log.Fatal(err)
		}
		b, err = os.ReadFile(treeFile)
		if err != nil {
			return gt
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	decoder.Strict = false
	decoder.Decode(&gt)

	parseList := func(listStr string, listName string) map[string]bool {
		res := make(map[string]bool)
		listStr = strings.ToLower(strings.TrimSpace(listStr))

		if listStr == "" || listStr == "any" {
			return res
		}

		tokens := strings.Fields(strings.ReplaceAll(listStr, ",", " "))
		for _, token := range tokens {
			if token == "" {
				continue
			}

			foundInTree := false

			for _, g := range gt.Genres {
				if g.Value == token {
					foundInTree = true
					for _, sg := range g.Subgenres {
						res[sg.Value] = true
					}
					break
				}
				for _, sg := range g.Subgenres {
					if sg.Value == token {
						foundInTree = true
						res[sg.Value] = true
						break
					}
				}
				if foundInTree {
					break
				}
			}

			if !foundInTree {
				if logger != nil {
					logger.W.Printf("genre '%s' from %s config not found in genres tree, ignored\n", token, listName)
				} else {
					log.Printf("W: genre '%s' from %s config not found in genres tree, ignored\n", token, listName)
				}
			}
		}
		return res
	}

	gt.Rejected = parseList(rejectStr, "REJECT_LIST")

	isAcceptEmpty := strings.TrimSpace(acceptStr) == "" || strings.ToLower(strings.TrimSpace(acceptStr)) == "any"

	if isAcceptEmpty {
		gt.AcceptAny = true
	} else {
		gt.Accepted = parseList(acceptStr, "ACCEPT_LIST")

		if len(gt.Accepted) == 0 {
			if logger != nil {
				logger.W.Println("no valid genres in ACCEPT_LIST, fallback to 'any'")
			} else {
				log.Println("W: no valid genres in ACCEPT_LIST, fallback to 'any'")
			}
			gt.AcceptAny = true
		}
	}

	return gt
}

func (gt *GenresTree) IsAccepted(refinedGenres []string) bool {

	if len(refinedGenres) == 0 {
		return gt.AcceptAny 
	}

	for _, g := range refinedGenres {
		if gt.Rejected[g] {
			return false
		}
	}

	if gt.AcceptAny {
		return true
	}

	for _, g := range refinedGenres {
		if gt.Accepted[g] {
			return true
		}
	}

	return false
}

func (gt *GenresTree) Refine(b *model.Book) {
	genres := make(map[string]struct{})
	for i := len(b.Genres) - 1; i >= 0; i-- {

		found := false
	Found:
		for _, g := range gt.Genres {
			for _, sg := range g.Subgenres {
				if sg.Value == b.Genres[i] {
					found = true // Found in subgenres
					break Found
				}
				for _, sga := range sg.Alts {
					if sga.Value == b.Genres[i] {
						b.Genres[i], found = sg.Value, true // Replace alt-genre with subgenre
						break Found
					}
				}
			}
		}
		_, double := genres[b.Genres[i]]
		if !double {
			genres[b.Genres[i]] = struct{}{}
		}
		if !found || double { // If genre was not found or is duplicate then remove it from book genre
			b.Genres = append(b.Genres[:i], b.Genres[i+1:]...)
		}
	}
}

func (gt *GenresTree) ListGenres() []Genre {
	return gt.Genres
}

func (gt *GenresTree) ListSubGenres(genre string) []Subgenre {
	sg := []Subgenre{}
	for _, g := range gt.Genres {
		if g.Value == genre {
			return g.Subgenres
		}
	}
	return sg
}

func (gt *GenresTree) GenreName(genre, lang string) string {
	for _, g := range gt.Genres {
		for _, sg := range g.Subgenres {
			if sg.Value == genre {
				for _, sgd := range sg.Descriptions {
					if sgd.Lang == lang {
						return sgd.Title
					}
				}
			}
		}
	}
	return ""
}

func (gt *GenresTree) SubgenreName(sg *Subgenre, lang string) string {
	for _, sgd := range sg.Descriptions {
		if sgd.Lang == lang {
			return sgd.Title
		}
	}
	return ""
}

func (gt *GenresTree) String() string {
	s := ""
	for _, g := range gt.Genres {
		s += fmt.Sprintln("genre:", g.Value)
		for _, rd := range g.Descriptions {
			s += fmt.Sprintf("\troot-descr: lang=%s title=%s detailed=%s\n", rd.Lang, rd.Title, rd.Detailed)
		}
		s += fmt.Sprintln("\tsubgenres ---")
		for _, sg := range g.Subgenres {
			s += fmt.Sprintln("\t\tsubgenre: value=", sg.Value)
			for _, sgd := range sg.Descriptions {
				s += fmt.Sprintf("\t\t\tgenre-descr lang=%s title=%s\n", sgd.Lang, sgd.Title)
			}
			for _, sga := range sg.Alts {
				s += fmt.Sprintf("\t\t\tgenre-alt value=%s format=%s\n", sga.Value, sga.Format)
			}
		}
	}
	return s
}
