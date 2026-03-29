package opds

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nfnt/resize"
	"github.com/vinser/flibgolite/pkg/config"
	cfb2 "github.com/vinser/flibgolite/pkg/conv/fb2"
	"github.com/vinser/flibgolite/pkg/database"
	"github.com/vinser/flibgolite/pkg/epub"
	"github.com/vinser/flibgolite/pkg/fb2"
	"github.com/vinser/flibgolite/pkg/fb3"
	"github.com/vinser/flibgolite/pkg/genres"
	"github.com/vinser/flibgolite/pkg/model"
	"github.com/vinser/flibgolite/pkg/parser"
	"github.com/vinser/flibgolite/pkg/rlog"
	"github.com/vinser/flibgolite/pkg/mobi"
	"github.com/vinser/u8xml"

	_ "image/gif"
	_ "image/png"

	"github.com/mozillazg/go-unidecode"
	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/message"
)

type Handler struct {
	CFG *config.Config
	LOG *rlog.Log
	DB  *database.DB
	GT  *genres.GenresTree
	MP  map[string]*message.Printer
	CoverSema chan struct{} 
	DownloadSema chan struct{}
}

func init() {
	_ = mime.AddExtensionType(".mobi", "application/x-mobipocket-ebook")
	_ = mime.AddExtensionType(".prc", "application/x-mobipocket-ebook")
	_ = mime.AddExtensionType(".azw", "application/vnd.amazon.ebook")
	_ = mime.AddExtensionType(".azw3", "application/x-mobi8-ebook")
	_ = mime.AddExtensionType(".epub", "application/epub+zip")
	_ = mime.AddExtensionType(".cbz", "application/x-cbz")
	_ = mime.AddExtensionType(".cbr", "application/x-cbr")
	_ = mime.AddExtensionType(".fb2", "application/fb2")
	_ = mime.AddExtensionType(".fb2.zip", "application/fb2+zip")  
	_ = mime.AddExtensionType(".fb2.epub", "application/epub+zip")
	_ = mime.AddExtensionType(".fb3", "application/fb3")
	_ = mime.AddExtensionType(".pdf", "application/pdf")           
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.LOG.I.Println(commentURL("Router", r))
	
	switch strings.ReplaceAll(r.URL.Path, "//", "/") { 
	case "/favicon.ico":
		h.unloadFavicon(w)
	case "/opds":
		h.root(w, r)
	case "/opds/languages":
		h.languages(w, r)
	case "/opds/language_menu":
		h.languageMenu(w, r)
	case "/opds/language_books": 
		h.languageBooks(w, r)    
	case "/opds/latest":
		h.latestMenu(w, r)
	case "/opds/latestlanguages":
		h.latestLanguages(w, r)	
	case "/opds/opensearch":
		h.openSearch(w, r)
	case "/opds/search":
		h.search(w, r)
	case "/opds/authors":
		h.authors(w, r)
	case "/opds/latestauthors":
		h.listLatestAuthors(w, r)	
	case "/opds/latestseries":
		h.listLatestSeries(w, r)	
	case "/opds/new":
		h.latest(w, r)	
	case "/opds/genres":
		h.genres(w, r)
	case "/opds/latestgenres":
		h.latestGenres(w, r)	
	case "/opds/series":
		h.series(w, r)
	case "/opds/latestrandom": 
		h.latestRandom(w, r)		
	case "/opds/books":
		h.books(w, r)
	case "/opds/folders":
		h.folders(w, r)
	case "/opds/covers":
		h.covers(w, r)
	default:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error": "Bad request"}`)
		return
	}
}

// Helpers for Language parameters
func (h *Handler) getInterfaceLanguage(r *http.Request) string {
	return h.CFG.Locales.DEFAULT
}

func (h *Handler) getBookLanguage(r *http.Request) string {
	return strings.TrimSpace(r.FormValue("blang"))
}

func withBlang(href, blang string) string {
	if blang == "" {
		return href
	}
	if strings.Contains(href, "?") {
		return href + "&blang=" + url.QueryEscape(blang)
	}
	return href + "?blang=" + url.QueryEscape(blang)
}

func idWithBlang(id, blang string) string {
	if blang == "" {
		return id
	}
	return id + "/blang=" + blang
}

func formatLanguageName(code string) string {
	if code == "" {
		return ""
	}
	langTag := language.Make(code)
	name := cases.Title(langTag).String(display.Self.Name(langTag))
	
	if name != "" {
		return fmt.Sprintf("%s (%s)", name, code)
	}
	return code
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	selfHref := "/opds"
	f := NewFeed(h.CFG.OPDS.TITLE, "", selfHref)
	searchLink := &Link{Rel: FeedSearchLinkRel, Href: "/opds/search?q={searchTerms}", Type: "application/atom+xml"}
	f.Link = append(f.Link, *searchLink)
	searchDescLink := &Link{Rel: FeedSearchLinkRel, Href: "/opds/opensearch", Type: FeedSearchDescriptionLinkType, Title: "Search on catalog"}
	f.Link = append(f.Link, *searchDescLink)
	
	booksCountNew := h.DB.GetLatestBooksCount(h.CFG.OPDS.LATEST_DAYS, "")	
	stats := h.DB.GetGlobalStats()
	
	genresDesc := h.MP[lang].Sprintf("~Titles - %d", stats.TotalGenres)

	f.Entry = []*Entry{
		{
			Title:   h.MP[lang].Sprintf("~Latest"),
			ID:      "latest",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/latest",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", booksCountNew),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Authors"),
			ID:      "authors",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/authors",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Authors - %d", stats.TotalAuthors),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Series"),
			ID:      "series",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/series",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Series - %d", stats.TotalSeries),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Genres"),
			ID:      "genres",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/genres",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: genresDesc,
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Folders"),
			ID:      "folders",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/folders?id=0",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", stats.TotalBooks),
			},
		},
	}
	if stats.TotalLanguages > 1 {
		entry := &Entry{
			Title:   h.MP[lang].Sprintf("~Languages"),
			ID:      "languages",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: "/opds/languages",
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Languages - %d", stats.TotalLanguages),
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) languages(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	selfHref := "/opds/languages"
	f := NewFeed(h.MP[lang].Sprintf("~Languages"), "", selfHref)

	langs := h.DB.GetBookLanguages()

	for _, l := range langs {
		langName := formatLanguageName(l.Code)

		entry := &Entry{
			Title:   langName,
			ID:      "/opds/languages/" + l.Code,
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: fmt.Sprintf("/opds/language_menu?blang=%s", l.Code),
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", l.Count),
			},
		}
		f.Entry = append(f.Entry, entry)
	}

	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) languageBooks(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	
	bc := h.DB.CountLanguageBooks(blang)
	
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil || page < 1 {
		page = 1
	}
	offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE

	books := h.DB.PageLanguageBooks(blang, h.CFG.OPDS.PAGE_SIZE+1, offset)
	
	selfHref := withBlang(fmt.Sprintf("/opds/language_books?page=%d", page), blang)
	f := NewFeed(h.MP[lang].Sprintf("~Titles"), "", selfHref)
	
	if len(books) > h.CFG.OPDS.PAGE_SIZE {
		nextRef := withBlang(fmt.Sprintf("/opds/language_books?page=%d", page+1), blang)
		nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
		f.Link = append(f.Link, *nextLink)
		books = books[:h.CFG.OPDS.PAGE_SIZE]
	}
	
	if int(bc) > h.CFG.OPDS.PAGE_SIZE {
		if page > 1 {
			f.Link = append(f.Link, Link{Rel: FeedFirstLinkRel, Href: withBlang("/opds/language_books?page=1", blang), Type: FeedNavigationLinkType})
			f.Link = append(f.Link, Link{Rel: FeedPrevLinkRel, Href: withBlang(fmt.Sprintf("/opds/language_books?page=%d", page-1), blang), Type: FeedNavigationLinkType})
		}
		lastPage := int(math.Ceil(float64(bc) / float64(h.CFG.OPDS.PAGE_SIZE)))
		if page < lastPage {
			f.Link = append(f.Link, Link{Rel: FeedLastLinkRel, Href: withBlang(fmt.Sprintf("/opds/language_books?page=%d", lastPage), blang), Type: FeedNavigationLinkType})
		}
	}

	h.feedBookEntries(r, books, f)
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) languageMenu(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	
	selfHref := withBlang("/opds/language_menu", blang)
	
	langName := formatLanguageName(blang)
	if langName == "" {
		langName = h.MP[lang].Sprintf("~Unknown language") 
	}

	f := NewFeed(langName, "", selfHref)
	stats := h.DB.GetLatestStats(0, blang) 

	booksCountNew := h.DB.GetLatestBooksCount(h.CFG.OPDS.LATEST_DAYS, blang)	
	
	f.Entry = []*Entry{
		{
			Title:   h.MP[lang].Sprintf("~Latest"),
			ID:      idWithBlang("latest", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/latest", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", booksCountNew),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Titles"), 
			ID:      idWithBlang("all_titles", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/language_books", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", stats.TotalBooks), 
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Authors"),
			ID:      idWithBlang("authors", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/authors", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Authors - %d", stats.TotalAuthors),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Series"),
			ID:      idWithBlang("series", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/series", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Series - %d", stats.TotalSeries),
			},
		},
		{
			Title:   h.MP[lang].Sprintf("~Genres"),
			ID:      idWithBlang("genres", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/genres", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", stats.TotalGenres),
			},
		},
	}
	
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) latestMenu(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	
	selfHref := withBlang("/opds/latest", blang)
	
	days := h.CFG.OPDS.LATEST_DAYS
	stats := h.DB.GetLatestStats(days, blang)

	f := NewFeed(h.MP[lang].Sprintf("~Latest"), "", selfHref)
	
	entryAll := &Entry{
		Title:   h.MP[lang].Sprintf("~Titles"),
		ID:      idWithBlang("/opds/latest/all", blang),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/new", blang), Type: FeedNavigationLinkType},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Titles - %d", stats.TotalBooks),
		},
	}
	f.Entry = append(f.Entry, entryAll)

	entryAuthors := &Entry{
		Title:   h.MP[lang].Sprintf("~Authors"),
		ID:      idWithBlang("/opds/latest/authors", blang),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/latestauthors", blang), Type: FeedNavigationLinkType},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Authors - %d", stats.TotalAuthors),
		},
	}
	f.Entry = append(f.Entry, entryAuthors)

	entrySeries := &Entry{
		Title:   h.MP[lang].Sprintf("~Series"),
		ID:      idWithBlang("/opds/latest/series", blang),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/latestseries", blang), Type: FeedNavigationLinkType},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Series - %d", stats.TotalSeries),
		},
	}
	f.Entry = append(f.Entry, entrySeries)

	entryGenres := &Entry{
		Title:   h.MP[lang].Sprintf("~Genres"),
		ID:      idWithBlang("/opds/latest/genres", blang),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/latestgenres", blang), Type: FeedNavigationLinkType},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Titles - %d", stats.TotalGenres),
		},
	}
	f.Entry = append(f.Entry, entryGenres)
	
	if blang == "" && stats.TotalLanguages > 1 {
		entryLanguages := &Entry{
			Title:   h.MP[lang].Sprintf("~Languages"),
			ID:      "latest_languages",
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: "/opds/latestlanguages", Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Languages - %d", stats.TotalLanguages),
			},
		}
		f.Entry = append(f.Entry, entryLanguages)
	}
	
	if stats.TotalBooks > 1 {
		entryRandom := &Entry{
			Title:   h.MP[lang].Sprintf("~Random books"), 
			ID:      idWithBlang("/opds/latest/random", blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang("/opds/latestrandom", blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~I'm feeling lucky"), 
			},
		}
		f.Entry = append(f.Entry, entryRandom)
	}
	
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) latestLanguages(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	selfHref := "/opds/latestlanguages"
	f := NewFeed(h.MP[lang].Sprintf("~Languages"), "", selfHref)

	langs := h.DB.GetLatestBookLanguages(h.CFG.OPDS.LATEST_DAYS)

	for _, l := range langs {
		langName := formatLanguageName(l.Code)
		if langName == "" {
			langName = h.MP[lang].Sprintf("~Unknown language")
		}

		entry := &Entry{
			Title:   langName,
			ID:      "/opds/latestlanguages/" + l.Code,
			Updated: f.Time(time.Now()),
			Links: []Link{
				{
					Rel:  FeedSubsectionLinkRel,
					Href: fmt.Sprintf("/opds/latest?blang=%s", l.Code), 
					Type: FeedNavigationLinkType,
				},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", l.Count), 
			},
		}
		f.Entry = append(f.Entry, entry)
	}

	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) latestRandom(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	days := h.CFG.OPDS.LATEST_DAYS
	
	selfHref := withBlang("/opds/latestrandom", blang)
	f := NewFeed(h.MP[lang].Sprintf("~Random book"), "", selfHref)
	
	book := h.DB.GetRandomLatestBook(days, blang)
	
	if book != nil {
		h.feedBookEntries(r, []*model.Book{book}, f)
	} else {
		entry := &Entry{
			Title: "Nothing new",
			ID:    idWithBlang("nothing", blang),
		}
		f.Entry = append(f.Entry, entry)
	}

	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) latest(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	h.LOG.D.Println(commentURL("new", r))
	selfHref := ""
	bc := h.DB.GetLatestBooksCount(h.CFG.OPDS.LATEST_DAYS, blang)

	switch {
	case bc != 0: 
		page, err := strconv.Atoi(r.FormValue("page"))
		if err != nil {
			page = 1
		}
		offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
		books := h.DB.PageLatestBooks(h.CFG.OPDS.LATEST_DAYS, h.CFG.OPDS.PAGE_SIZE+1, offset, blang)
		selfHref = withBlang(fmt.Sprintf("/opds/new?page=%d", page), blang)
		f := NewFeed(h.MP[lang].Sprintf("~Titles - %d", bc), "", selfHref)
		if len(books) > h.CFG.OPDS.PAGE_SIZE {
			nextRef := withBlang(fmt.Sprintf("/opds/new?page=%d", page+1), blang)
			nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *nextLink)
			books = books[:h.CFG.OPDS.PAGE_SIZE-1]
		}
		if int(bc) > h.CFG.OPDS.PAGE_SIZE {
			if page > 1 {
				firstRef := withBlang("/opds/new?page=1", blang)
				firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *firstLink)

				prevRef := withBlang(fmt.Sprintf("/opds/new?page=%d", page-1), blang)
				prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *prevLink)
			}
			lastPage := int(math.Ceil(float64(bc) / float64(h.CFG.OPDS.PAGE_SIZE)))
			if page < lastPage {
				lastRef := withBlang(fmt.Sprintf("/opds/new?page=%d", lastPage), blang)
				lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *lastLink)
			}
		}

		h.feedBookEntries(r, books, f)
		writeFeed(w, http.StatusOK, *f)
	default:
		selfHref = withBlang("/opds/new", blang)
		f := NewFeed(h.MP[lang].Sprintf("~Nothing new"), "", selfHref)
		writeFeed(w, http.StatusOK, *f)
	}
}

func (h *Handler) openSearch(w http.ResponseWriter, r *http.Request) {
	data :=
		`
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
<ShortName>` + h.CFG.OPDS.TITLE + `</ShortName>
<Description>Search on catalog</Description>
<InputEncoding>UTF-8</InputEncoding>
<OutputEncoding>UTF-8</OutputEncoding>
<Url type="application/atom+xml" template="/opds/search?q={searchTerms}"/>
</OpenSearchDescription>	
`
	s := fmt.Sprintf("%s%s", xml.Header, data)
	w.Header().Add("Content-Type", "application/atom+xml")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, s)

}

func (h *Handler) foundAuthorsEntry(f *Feed, lang, queryString string, authorCount int64) *Entry {
	return &Entry{
		Title:   h.MP[lang].Sprintf("~Authors"),
		ID:      fmt.Sprintf("/opds/search/author=%s", queryString),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{
				Rel:  FeedSubsectionLinkRel,
				Href: fmt.Sprintf("/opds/search?author=%s", queryString),
				Type: FeedNavigationLinkType,
			},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Authors - %d", authorCount),
		},
	}
}

func (h *Handler) foundBooksEntry(f *Feed, lang, queryString string, titleCount int64) *Entry {
	return &Entry{
		Title:   h.MP[lang].Sprintf("~Titles"),
		ID:      fmt.Sprintf("/opds/search/book=%s", queryString),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{
				Rel:  FeedSubsectionLinkRel,
				Href: fmt.Sprintf("/opds/search?book=%s", queryString),
				Type: FeedNavigationLinkType,
			},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Titles - %d", titleCount),
		},
	}
}

func (h *Handler) foundKeywordsEntry(f *Feed, lang, queryString string, keywordCount int64) *Entry {
	return &Entry{
		Title:   h.MP[lang].Sprintf("~Keywords"),
		ID:      fmt.Sprintf("/opds/search/keywords=%s", queryString),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{
				Rel:  FeedSubsectionLinkRel,
				Href: fmt.Sprintf("/opds/search?keywords=%s", queryString),
				Type: FeedNavigationLinkType,
			},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Titles - %d", keywordCount),
		},
	}
}

func (h *Handler) foundSeriesEntry(f *Feed, lang, queryString string, serieCount int64) *Entry {
	return &Entry{
		Title:   h.MP[lang].Sprintf("~Series"),
		ID:      fmt.Sprintf("/opds/search/serie=%s", queryString),
		Updated: f.Time(time.Now()),
		Links: []Link{
			{
				Rel:  FeedSubsectionLinkRel,
				Href: fmt.Sprintf("/opds/search?serie=%s", queryString),
				Type: FeedNavigationLinkType,
			},
		},
		Content: &Content{
			Type:    FeedTextContentType,
			Content: h.MP[lang].Sprintf("~Series - %d", serieCount),
		},
	}
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	h.LOG.D.Println(commentURL("Search", r))
	selfHref := ""
	queryString := ""
	
	var authorCount, titleCount, keywordCount, serieCount int64
	
	switch {
	case r.FormValue("q") != "":
		queryString = r.FormValue("q")
		if utf8.RuneCountInString(queryString) < 3 {
			authorCount = 0
			titleCount = 0
			keywordCount = 0
			serieCount = 0
		} else {
			authorCount = h.DB.SearchAuthorsCount(queryString)
			titleCount = h.DB.SearchBooksCountByTitle(queryString)
			keywordCount = h.DB.SearchBooksCountByKeyword(queryString)
			serieCount = h.DB.SearchSeriesCount(queryString)
		}
	case r.FormValue("author") != "":
		queryString = r.FormValue("author")
		authorCount = h.DB.SearchAuthorsCount(queryString)
	case r.FormValue("book") != "":
		queryString = r.FormValue("book")
		titleCount = h.DB.SearchBooksCountByTitle(queryString)
	case r.FormValue("keywords") != "":
		queryString = r.FormValue("keywords")
		keywordCount = h.DB.SearchBooksCountByKeyword(queryString)
	case r.FormValue("serie") != "":
		queryString = r.FormValue("serie")
		serieCount = h.DB.SearchSeriesCount(queryString)
	}
	
	switch {
	case (authorCount == 0 && titleCount == 0 && keywordCount == 0 && serieCount == 0): 
		selfHref = "/opds/search?q={searchTerms}"
		f := NewFeed(h.MP[lang].Sprintf("~Nothing found"), "", selfHref)
		writeFeed(w, http.StatusOK, *f)
		
	case authorCount > 0 && titleCount == 0 && keywordCount == 0 && serieCount == 0: 
		page, err := strconv.Atoi(r.FormValue("page"))
		if err != nil {
			page = 1
		}
		offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
		authors := h.DB.PageFoundAuthors(queryString, h.CFG.OPDS.PAGE_SIZE+1, offset)
		selfHref = fmt.Sprintf("/opds/search?author=%s&page=%d", queryString, page)
		f := NewFeed(h.MP[lang].Sprintf("~Authors - %d", authorCount), "", selfHref)
		if len(authors) > h.CFG.OPDS.PAGE_SIZE {
			nextRef := fmt.Sprintf("/opds/search?author=%s&page=%d", queryString, page+1)
			nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *nextLink)
			authors = authors[:h.CFG.OPDS.PAGE_SIZE-1]
		}
		if int(authorCount) > h.CFG.OPDS.PAGE_SIZE {
			if page > 1 {
				firstRef := fmt.Sprintf("/opds/search?author=%s&page=1", queryString)
				firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *firstLink)

				prevRef := fmt.Sprintf("/opds/search?author=%s&page=%d", queryString, page-1)
				prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *prevLink)
			}
			lastPage := int(math.Ceil(float64(authorCount) / float64(h.CFG.OPDS.PAGE_SIZE)))
			if page < lastPage {
				lastRef := fmt.Sprintf("/opds/search?author=%s&page=%d", queryString, lastPage)
				lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *lastLink)
			}
		}

		for _, author := range authors {
			entry := &Entry{
				Title:   author.Name,
				ID:      fmt.Sprintf("/opds/authors/author=%d", author.ID),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: fmt.Sprintf("/opds/authors?id=%d", author.ID), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Titles - %d", author.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		writeFeed(w, http.StatusOK, *f)
		
	case authorCount == 0 && titleCount > 0 && keywordCount == 0 && serieCount == 0: 
		page, err := strconv.Atoi(r.FormValue("page"))
		if err != nil {
			page = 1
		}
		offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
		books := h.DB.PageFoundBooksByTitle(queryString, h.CFG.OPDS.PAGE_SIZE+1, offset)
		selfHref = fmt.Sprintf("/opds/search?book=%s&page=%d", queryString, page)
		f := NewFeed(h.MP[lang].Sprintf("~Titles - %d", titleCount), "", selfHref)
		if len(books) > h.CFG.OPDS.PAGE_SIZE {
			nextRef := fmt.Sprintf("/opds/search?book=%s&page=%d", queryString, page+1)
			nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *nextLink)
			books = books[:h.CFG.OPDS.PAGE_SIZE-1]
		}
		if int(titleCount) > h.CFG.OPDS.PAGE_SIZE {
			if page > 1 {
				firstRef := fmt.Sprintf("/opds/search?book=%s&page=1", queryString)
				firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *firstLink)

				prevRef := fmt.Sprintf("/opds/search?book=%s&page=%d", queryString, page-1)
				prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *prevLink)
			}
			lastPage := int(math.Ceil(float64(titleCount) / float64(h.CFG.OPDS.PAGE_SIZE)))
			if page < lastPage {
				lastRef := fmt.Sprintf("/opds/search?book=%s&page=%d", queryString, lastPage)
				lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *lastLink)
			}
		}

		h.feedBookEntries(r, books, f)
		writeFeed(w, http.StatusOK, *f)
		
	case authorCount == 0 && titleCount == 0 && keywordCount > 0 && serieCount == 0: 
		page, err := strconv.Atoi(r.FormValue("page"))
		if err != nil {
			page = 1
		}
		offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
		books := h.DB.PageFoundBooksByKeywords(queryString, h.CFG.OPDS.PAGE_SIZE+1, offset)
		selfHref = fmt.Sprintf("/opds/search?keywords=%s&page=%d", queryString, page)
		f := NewFeed(h.MP[lang].Sprintf("~Titles - %d", keywordCount), "", selfHref)
		if len(books) > h.CFG.OPDS.PAGE_SIZE {
			nextRef := fmt.Sprintf("/opds/search?keywords=%s&page=%d", queryString, page+1)
			nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *nextLink)
			books = books[:h.CFG.OPDS.PAGE_SIZE-1]
		}
		if int(keywordCount) > h.CFG.OPDS.PAGE_SIZE {
			if page > 1 {
				firstRef := fmt.Sprintf("/opds/search?keywords=%s&page=1", queryString)
				firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *firstLink)

				prevRef := fmt.Sprintf("/opds/search?keywords=%s&page=%d", queryString, page-1)
				prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *prevLink)
			}
			lastPage := int(math.Ceil(float64(keywordCount) / float64(h.CFG.OPDS.PAGE_SIZE)))
			if page < lastPage {
				lastRef := fmt.Sprintf("/opds/search?keywords=%s&page=%d", queryString, lastPage)
				lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *lastLink)
			}
		}

		h.feedBookEntries(r, books, f)
		writeFeed(w, http.StatusOK, *f)
		
	case authorCount == 0 && titleCount == 0 && keywordCount == 0 && serieCount > 0: 
		page, err := strconv.Atoi(r.FormValue("page"))
		if err != nil {
			page = 1
		}
		offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
		series := h.DB.PageFoundSeries(queryString, h.CFG.OPDS.PAGE_SIZE+1, offset)
		selfHref = fmt.Sprintf("/opds/search?serie=%s&page=%d", queryString, page)
		f := NewFeed(h.MP[lang].Sprintf("~Series - %d", serieCount), "", selfHref)
		
		if len(series) > h.CFG.OPDS.PAGE_SIZE {
			nextRef := fmt.Sprintf("/opds/search?serie=%s&page=%d", queryString, page+1)
			nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *nextLink)
			series = series[:h.CFG.OPDS.PAGE_SIZE-1]
		}
		if int(serieCount) > h.CFG.OPDS.PAGE_SIZE {
			if page > 1 {
				firstRef := fmt.Sprintf("/opds/search?serie=%s&page=1", queryString)
				firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *firstLink)

				prevRef := fmt.Sprintf("/opds/search?serie=%s&page=%d", queryString, page-1)
				prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *prevLink)
			}
			lastPage := int(math.Ceil(float64(serieCount) / float64(h.CFG.OPDS.PAGE_SIZE)))
			if page < lastPage {
				lastRef := fmt.Sprintf("/opds/search?serie=%s&page=%d", queryString, lastPage)
				lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
				f.Link = append(f.Link, *lastLink)
			}
		}

		for _, serie := range series {
			entry := &Entry{
				Title:   serie.Name,
				ID:      fmt.Sprintf("/opds/series/serie=%d", serie.ID),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: fmt.Sprintf("/opds/series?id=%d", serie.ID), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Titles - %d", serie.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		writeFeed(w, http.StatusOK, *f)
		
	default: 
		selfHref = "/opds/search?q={searchTerms}"
		f := NewFeed(h.MP[lang].Sprintf("~Choose from the found ones"), "", selfHref)
		f.Entry = []*Entry{}
		if authorCount > 0 {
			f.Entry = append(f.Entry, h.foundAuthorsEntry(f, lang, queryString, authorCount))
		}
		if titleCount > 0 {
			f.Entry = append(f.Entry, h.foundBooksEntry(f, lang, queryString, titleCount))
		}
		if serieCount > 0 {
			f.Entry = append(f.Entry, h.foundSeriesEntry(f, lang, queryString, serieCount))
		}
		if keywordCount > 0 {
			f.Entry = append(f.Entry, h.foundKeywordsEntry(f, lang, queryString, keywordCount))
		}
		
		writeFeed(w, http.StatusOK, *f)
	}
}

func (h *Handler) authors(w http.ResponseWriter, r *http.Request) {
	switch {
	default: 
		h.listAuthors(w, r)
		h.LOG.D.Println("ListAuthors")
	case r.FormValue("exact_sort") != "": 
		h.listExactAuthors(w, r, 0)
	case r.FormValue("id") != "" && r.FormValue("anthology") == "" && r.FormValue("serie") == "":
		h.authorAnthology(w, r)
		h.LOG.D.Println("AuthorAnthology")
	case r.FormValue("id") != "" && r.FormValue("anthology") == "series": 
		h.authorAnthologySeries(w, r)
		h.LOG.D.Println("AuthorAnthologySeries")
	case r.FormValue("id") != "" && (r.FormValue("anthology") == "alphabet" || r.FormValue("serie") != ""):
		h.authorBooks(w, r, 0)
		h.LOG.D.Println("AuthorBooks")
	}
}

func (h *Handler) listAuthors(w http.ResponseWriter, r *http.Request) {
	h.realListAuthors(w, r, 0)
}

func (h *Handler) listLatestAuthors(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("exact_sort") != "" { 
		h.listExactAuthors(w, r, h.CFG.OPDS.LATEST_DAYS)
		return
	}
	if r.FormValue("id") != "" {
		h.authorBooks(w, r, h.CFG.OPDS.LATEST_DAYS)
		return
	}
	h.realListAuthors(w, r, h.CFG.OPDS.LATEST_DAYS)
}

func (h *Handler) listExactAuthors(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	exactSort := r.FormValue("exact_sort")
	
	basePath := "authors"
	if days > 0 {
		basePath = "latestauthors"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?exact_sort=%s", basePath, url.QueryEscape(exactSort)), blang)
	f := NewFeed(exactSort, "", selfHref)
	
	authors := h.DB.ListAuthorsByExactSort(exactSort, days, blang)
	
	for _, author := range authors {
		entry := &Entry{
			Title:   author.Name, 
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/author=%d", basePath, author.ID), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, author.ID), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", author.Count),
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) listExactSeries(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	exactSort := r.FormValue("exact_sort")
	
	basePath := "series"
	if days > 0 {
		basePath = "latestseries"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?exact_sort=%s", basePath, url.QueryEscape(exactSort)), blang)
	f := NewFeed(exactSort, "", selfHref)
	
	series := h.DB.ListSeriesByExactSort(exactSort, days, blang)
	
	for _, serie := range series {
		entry := &Entry{
			Title:   serie.Name,
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/serie=%d", basePath, serie.ID), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, serie.ID), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", serie.Count),
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) realListAuthors(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	prefix := r.FormValue("author")
	abc := h.CFG.Languages[lang].Abc
	if r.Form.Has("all") {
		abc = ""
	}
	authors := h.DB.ListAuthors(prefix, abc, days, blang)
	if len(authors) == 0 {
		return
	}
	
	sortAuthors(authors, h.CFG.Locales.Languages[lang].Tag)
	totalAuthors := 0
	for _, a := range authors {
		totalAuthors += a.Count
	}
	
	basePath := "authors"
	if days > 0 {
		basePath = "latestauthors"
	}
	
	var selfHref string
	if prefix == "" {
		selfHref = fmt.Sprintf("/opds/%s", basePath)
		if abc == "" {
			selfHref += "?all"
		}
	} else {
		selfHref = fmt.Sprintf("/opds/%s?author=%s", basePath, url.QueryEscape(prefix))
	}
	selfHref = withBlang(selfHref, blang)

	f := NewFeed(h.MP[lang].Sprintf("~Authors"), "", selfHref)            
	addNotSpecLink := func() {
		if utf8.RuneCountInString(prefix) > 0 {
			return
		}
		notSpecId := h.DB.AuthorNotSpecifiedId()
		if notSpecId > 0 {
			entry := &Entry{
				Title:   h.MP[lang].Sprintf("~Author not specified"),
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/id=%d", basePath, notSpecId), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, notSpecId), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: "-",
				},
			}
			f.Entry = append(f.Entry, entry)
		}
	}
	addAllAuthorsLinks := func() {
		if abc == "" || prefix != "" {
			return
		}
		entry := &Entry{
			Title:   h.MP[lang].Sprintf("~All authors"),
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/all", basePath), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?all", basePath), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: "-",
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	switch {
	case totalAuthors <= h.CFG.OPDS.PAGE_SIZE:
		authors = h.DB.ListAuthorWithTotals(prefix, days, blang)
		for _, author := range authors {
			entry := &Entry{
				Title:   author.Name,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/author=%d", basePath, author.ID), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, author.ID), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Titles - %d", author.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		writeFeed(w, http.StatusOK, *f)
		default:
		for _, author := range authors {
			if author.Sort == prefix {
				if author.Count == 1 {
					bookCount := 0
					if realAuthor := h.DB.AuthorByID(author.ID, blang); realAuthor != nil {
						bookCount = realAuthor.Count
					}

					entry := &Entry{
						Title:   author.Name, 
						ID:      idWithBlang(fmt.Sprintf("/opds/%s/author=%d", basePath, author.ID), blang),
						Updated: f.Time(time.Now()),
						Links: []Link{
							{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, author.ID), blang), Type: FeedNavigationLinkType},
						},
						Content: &Content{
							Type:    FeedTextContentType,
							Content: h.MP[lang].Sprintf("~Titles - %d", bookCount),
						},
					}
					f.Entry = append(f.Entry, entry)
					continue
				}
				entry := &Entry{
					Title:   author.Sort,
					ID:      idWithBlang(fmt.Sprintf("/opds/%s/exact_sort=%s", basePath, author.Sort), blang),
					Updated: f.Time(time.Now()),
					Links: []Link{
						{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?exact_sort=%s", basePath, url.QueryEscape(author.Sort)), blang), Type: FeedNavigationLinkType},
					},
					Content: &Content{
						Type:    FeedTextContentType,
						Content: h.MP[lang].Sprintf("~Authors - %d", author.Count), 
					},
				}
				f.Entry = append(f.Entry, entry)
				continue
			}

			entry := &Entry{
				Title:   author.Sort,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/author=%s", basePath, author.Sort), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?author=%s", basePath, url.QueryEscape(author.Sort)), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Authors - %d", author.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		addNotSpecLink() 
		addAllAuthorsLinks() 
		writeFeed(w, http.StatusOK, *f)
	}
}

func (h *Handler) authorAnthology(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	authorId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	authorSeries := h.DB.AuthorBookSeries(authorId, blang)
	
	if len(authorSeries) > 0 {
		selfHref := withBlang(fmt.Sprintf("/opds/authors?id=%d", authorId), blang)
		author := h.fixIfNoSpecAuthorName(h.DB.AuthorByID(authorId, blang), lang)
		f := NewFeed(author.Name, "", selfHref)
		
		totalBooks := author.Count       
		totalSeries := len(authorSeries) 

		f.Entry = []*Entry{
			{
				Title:   h.MP[lang].Sprintf("~Alphabet"),
				ID:      idWithBlang(fmt.Sprintf("/opds/authors/id=%d/anthology=alphabet", authorId), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{
						Rel:  FeedSubsectionLinkRel,
						Href: withBlang(fmt.Sprintf("/opds/authors?id=%d&anthology=alphabet", authorId), blang),
						Type: FeedNavigationLinkType,
					},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Titles - %d", totalBooks),
				},
			},
			{
				Title:   h.MP[lang].Sprintf("~Series"),
				ID:      idWithBlang(fmt.Sprintf("/opds/authors/id=%d/anthology=series", authorId), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{
						Rel:  FeedSubsectionLinkRel,
						Href: withBlang(fmt.Sprintf("/opds/authors?id=%d&anthology=series", authorId), blang), Type: FeedNavigationLinkType,
					},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Series - %d", totalSeries),
				},
			},
		}
		writeFeed(w, http.StatusOK, *f)
	} else {
		h.authorBooks(w, r, 0)
	}
}

func (h *Handler) authorAnthologySeries(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	authorId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	author := h.fixIfNoSpecAuthorName(h.DB.AuthorByID(authorId, blang), lang)
	selfHref := withBlang(fmt.Sprintf("/opds/authors?id=%d&anthology=series", authorId), blang)
	f := NewFeed(author.Name, "", selfHref)
	f.Entry = []*Entry{}
	var entry *Entry
	series := h.DB.AuthorBookSeries(authorId, blang)
	for _, serie := range series {
		entry = &Entry{
			Title:   serie.Name,
			ID:      idWithBlang(fmt.Sprintf("/opds/authors/id=%d/serie=%d", authorId, serie.ID), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/authors?id=%d&serie=%d", authorId, serie.ID), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", serie.Count),
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) authorBooks(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	authorId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	serieId, _ := strconv.ParseInt(r.FormValue("serie"), 10, 64)
	author := h.fixIfNoSpecAuthorName(h.DB.AuthorByID(authorId, blang), lang)
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil {
		page = 1
	}
	offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE

	books := h.DB.ListAuthorBooks(authorId, serieId, days, h.CFG.OPDS.PAGE_SIZE+1, offset, blang)
	
	basePath := "authors"
	if days > 0 {
		basePath = "latestauthors"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?id=%d&anthology=alphabet&page=%d", basePath, authorId, page), blang)
	f := NewFeed(author.Name, "", selfHref)
	if len(books) > h.CFG.OPDS.PAGE_SIZE {
		nextRef := withBlang(fmt.Sprintf("/opds/%s?id=%d&anthology=alphabet&page=%d", basePath, authorId, page+1), blang)
		nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
		f.Link = append(f.Link, *nextLink)
		books = books[:h.CFG.OPDS.PAGE_SIZE-1]
	}

	h.feedBookEntries(r, books, f)
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) fixIfNoSpecAuthorName(author *model.Author, lang string) *model.Author {
	if author == nil {
		return &model.Author{Name: h.MP[lang].Sprintf("~Author not specified"), Sort: h.MP[lang].Sprintf("~Author not specified")}
	}
	if author.Sort == "[author not specified]" || author.Name == "[author not specified]" {
		author.Name = h.MP[lang].Sprintf("~Author not specified")
		author.Sort = h.MP[lang].Sprintf("~Author not specified")
	}
	return author
}

func (h *Handler) genres(w http.ResponseWriter, r *http.Request) {
	switch {
	default:
		h.realListGenres(w, r, 0)
		h.LOG.D.Println("ListGenres")
	case r.FormValue("random_bunch") != "": 
		h.randomBunchBook(w, r, 0)
		h.LOG.D.Println("RandomBunchBook")	
	case r.FormValue("random_code") != "": 
		h.randomCodeBook(w, r, 0)
		h.LOG.D.Println("RandomCodeBook")		
	case r.FormValue("bunch") != "":
		h.realListSubgenres(w, r, 0)
		h.LOG.D.Println("ListSubgenres")
	case r.FormValue("code") != "":
		h.realGenreBooks(w, r, 0)
		h.LOG.D.Println("GenreBooks")
	}
}

func (h *Handler) latestGenres(w http.ResponseWriter, r *http.Request) {
	switch {
	default:
		h.realListGenres(w, r, h.CFG.OPDS.LATEST_DAYS)
	case r.FormValue("random_bunch") != "": 
		h.randomBunchBook(w, r, h.CFG.OPDS.LATEST_DAYS)
	case r.FormValue("random_code") != "": 
		h.randomCodeBook(w, r, h.CFG.OPDS.LATEST_DAYS)	
	case r.FormValue("bunch") != "":
		h.realListSubgenres(w, r, h.CFG.OPDS.LATEST_DAYS)
	case r.FormValue("code") != "":
		h.realGenreBooks(w, r, h.CFG.OPDS.LATEST_DAYS)
	}
}

func (h *Handler) realListGenres(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	basePath := "genres"
	if days > 0 {
		basePath = "latestgenres"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s", basePath), blang)
	f := NewFeed(h.MP[lang].Sprintf("~Genres"), "", selfHref)
	f.Entry = []*Entry{}
	var entry *Entry
	genres := h.GT.ListGenres()
	
	var bunchCounts map[string]int64

	bunchesMap := make(map[string][]string)
	for _, genre := range genres {
		subgenres := h.GT.ListSubGenres(genre.Value)
		codes := make([]string, len(subgenres))
		for i, sg := range subgenres {
			codes[i] = sg.Value
		}
		bunchesMap[genre.Value] = codes
	}
	
	if days > 0 {
		bunchCounts = h.DB.CountBunchesBooks(bunchesMap, days, blang)
	} else {
		bunchCounts = h.DB.GetBunchesStats(blang)
	}

	
	for _, genre := range genres {
		
		if bunchCounts[genre.Value] == 0 {
			continue
		}

		title := ""
		content := ""
		for _, gd := range genre.Descriptions {
			if gd.Lang == lang {
				title = gd.Title
				content = gd.Detailed
				break
			}
		}
		
		if title != "" {
			
			displayContent := h.MP[lang].Sprintf("~Titles - %d", bunchCounts[genre.Value])
			if content != "" {
				displayContent = displayContent + "\n" + content
			}		

			entry = &Entry{
				Title:   title,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/bunch=%s", basePath, genre.Value), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?bunch=%s", basePath, genre.Value), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Content: displayContent,
					Type:    FeedTextContentType,
				},
			}
			f.Entry = append(f.Entry, entry)
		}
	}
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) randomCodeBook(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	genreCode := r.FormValue("random_code")
	
	basePath := "genres"
	if days > 0 {
		basePath = "latestgenres"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?random_code=%s", basePath, genreCode), blang)
	f := NewFeed(h.MP[lang].Sprintf("~Random book"), "", selfHref)
	
	book := h.DB.GetRandomBookByGenre(genreCode, days, blang)
	
	if book != nil {
		h.feedBookEntries(r, []*model.Book{book}, f)
	} else {
		entry := &Entry{
			Title: h.MP[lang].Sprintf("~Nothing found"),
			ID:    idWithBlang("nothing", blang),
		}
		f.Entry = append(f.Entry, entry)
	}
	
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) randomBunchBook(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	bunch := r.FormValue("random_bunch")
	
	basePath := "genres"
	if days > 0 {
		basePath = "latestgenres"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?random_bunch=%s", basePath, bunch), blang)
	f := NewFeed(h.MP[lang].Sprintf("~Random book"), "", selfHref)

	var codes []string
	
	subgenres := h.GT.ListSubGenres(bunch)
	codes = make([]string, len(subgenres))
	for i, sg := range subgenres {
		codes[i] = sg.Value
	}

	book := h.DB.GetRandomBookByBunch(bunch, codes, days, blang)
	
	if book != nil {
		h.feedBookEntries(r, []*model.Book{book}, f)
	} else {
		entry := &Entry{
			Title: h.MP[lang].Sprintf("~Nothing found"),
			ID:    idWithBlang("nothing", blang),
		}
		f.Entry = append(f.Entry, entry)
	}
	
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) realListSubgenres(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	bunch := r.FormValue("bunch")
	basePath := "genres"
	if days > 0 {
		basePath = "latestgenres"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?bunch=%s", basePath, bunch), blang)
	f := NewFeed(h.MP[lang].Sprintf("~Genres"), "", selfHref)
	
	subgenres := h.GT.ListSubGenres(bunch)

	codes := make([]string, len(subgenres))
	for i, sg := range subgenres {
		codes[i] = sg.Value
	}

	genreCounts := h.DB.GetGenreCountsInBunch(bunch, codes, days, blang)
	
	totalBooksInBunch := int64(0)
	for _, count := range genreCounts {
		totalBooksInBunch += count
	}

	if totalBooksInBunch > 1 {
		entryRandom := &Entry{
			Title:   h.MP[lang].Sprintf("~Random book"), 
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/random_bunch=%s", basePath, bunch), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?random_bunch=%s", basePath, bunch), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~I'm feeling lucky"), 
			},
		}
		f.Entry = append(f.Entry, entryRandom)
	}
	
	for _, sg := range subgenres {
		gbc, hasBooks := genreCounts[sg.Value]

		if (!hasBooks) || gbc == 0 {
			continue
		}

		title := h.GT.SubgenreName(&sg, lang)
		if title != "" {

			entry := &Entry{
				Title:   title,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/code=%s", basePath, sg.Value), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?code=%s", basePath, sg.Value), blang), Type: FeedAcquisitionLinkType},
				},
				Content: &Content{
					Content: h.MP[lang].Sprintf("~Titles - %d", gbc),
					Type:    FeedTextContentType,
				},
			}
			f.Entry = append(f.Entry, entry)
		}
	}
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) realGenreBooks(w http.ResponseWriter, r *http.Request, days int) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	genreCode := r.FormValue("code")
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil {
		page = 1
	}
	offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE
	
	books := h.DB.PageGenreBooks(genreCode, days, h.CFG.OPDS.PAGE_SIZE+1, offset, blang) 
	
	basePath := "genres"
	if days > 0 {
		basePath = "latestgenres"
	}
	
	genreName := h.GT.GenreName(genreCode, lang)	
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?code=%s&page=%d", basePath, genreCode, page), blang)
	f := NewFeed(genreName, "", selfHref)
	if len(books) > h.CFG.OPDS.PAGE_SIZE {
		nextRef := withBlang(fmt.Sprintf("/opds/%s?code=%s&page=%d", basePath, genreCode, page+1), blang)
		nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
		f.Link = append(f.Link, *nextLink)
		books = books[:h.CFG.OPDS.PAGE_SIZE]
	}
	
	if page == 1 && len(books) > 1 {
		entryRandom := &Entry{
			Title:   h.MP[lang].Sprintf("~Random book"), 
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/random_code=%s", basePath, genreCode), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?random_code=%s", basePath, genreCode), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~I'm feeling lucky"), 
			},
		}
		f.Entry = append(f.Entry, entryRandom)
	}
	
	if gbc := h.DB.CountGenreBooks(genreCode, days, blang); int(gbc) > h.CFG.OPDS.PAGE_SIZE {
		if page > 1 {
			firstRef := withBlang(fmt.Sprintf("/opds/%s?code=%s&page=1", basePath, genreCode), blang)
			firstLink := &Link{Rel: FeedFirstLinkRel, Href: firstRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *firstLink)

			prevRef := withBlang(fmt.Sprintf("/opds/%s?code=%s&page=%d", basePath, genreCode, page-1), blang)
			prevLink := &Link{Rel: FeedPrevLinkRel, Href: prevRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *prevLink)
		}
		lastPage := int(math.Ceil(float64(gbc) / float64(h.CFG.OPDS.PAGE_SIZE)))
		if page < lastPage {
			lastRef := withBlang(fmt.Sprintf("/opds/%s?code=%s&page=%d", basePath, genreCode, lastPage), blang)
			lastLink := &Link{Rel: FeedLastLinkRel, Href: lastRef, Type: FeedNavigationLinkType}
			f.Link = append(f.Link, *lastLink)
		}
	}

	h.feedBookEntries(r, books, f)
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) series(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.FormValue("exact_sort") != "":
		h.listExactSeries(w, r, 0)
	case r.FormValue("id") != "":
		h.serieBooks(w, r, 0)
		h.LOG.D.Println("serieBooks")
	default:
		h.listSeries(w, r)
		h.LOG.D.Println("listSeries")	
	}
}

func (h *Handler) listSeries(w http.ResponseWriter, r *http.Request) {
	h.realListSeries(w, r, 0)
}

func (h *Handler) listLatestSeries(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("exact_sort") != "" { 
		h.listExactSeries(w, r, h.CFG.OPDS.LATEST_DAYS)
		return
	}
	if r.FormValue("id") != "" {
		h.serieBooks(w, r, h.CFG.OPDS.LATEST_DAYS)
		return
	}
	h.realListSeries(w, r, h.CFG.OPDS.LATEST_DAYS)
}

func (h *Handler) realListSeries(w http.ResponseWriter, r *http.Request, days int) {
	prefix := r.FormValue("serie")
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	var (
		abc   string
		all   string
	)
	if r.Form.Has("all") {
		abc = ""
		all = "&all"
	} else {
		abc = h.CFG.Languages[lang].Abc + `'0','1','2','3','4','5','6','7','8','9','0'`		
	}
	
	series := h.DB.ListSeries(prefix, abc, days, blang)
	if len(series) == 0 {
		return
	}
	sortSeries(series, h.CFG.Locales.Languages[lang].Tag)
	totalSeries := 0
	for _, s := range series {
		totalSeries += s.Count
	}

	basePath := "series"
	if days > 0 {
		basePath = "latestseries"
	}

	selfHref := ""
	if prefix == "" {
		selfHref = fmt.Sprintf("/opds/%s", basePath)
	} else {
		selfHref = fmt.Sprintf("/opds/%s?serie=%s", basePath, url.QueryEscape(prefix))
	}
	selfHref = withBlang(selfHref, blang)

	f := NewFeed(h.MP[lang].Sprintf("~Series"), "", selfHref)
		
	addAllSeriesLink := func() {
		if abc == "" || prefix != "" {
			return
		}
		entry := &Entry{
			Title:   h.MP[lang].Sprintf("~All series"),
			ID:      idWithBlang(fmt.Sprintf("/opds/%s/all", basePath), blang),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?all", basePath), blang), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: "-",
			},
		}
		f.Entry = append(f.Entry, entry)
	}
	
	switch {
	case totalSeries <= h.CFG.OPDS.PAGE_SIZE:
		series = h.DB.ListSeriesWithTotals(prefix, days, blang)
		for _, serie := range series {
			entry := &Entry{
				Title:   serie.Name,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/serie=%s", basePath, serie.Name), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d%s", basePath, serie.ID, all), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Titles - %d", serie.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		writeFeed(w, http.StatusOK, *f)
	default:
		for _, serie := range series {
			if serie.Sort == prefix { 
				if serie.Count == 1 {
					bookCount := 0
					if realSerie := h.DB.SerieByID(serie.ID, blang); realSerie != nil {
						bookCount = realSerie.Count
					}

					entry := &Entry{
						Title:   serie.Name,
						ID:      idWithBlang(fmt.Sprintf("/opds/%s/serie=%d", basePath, serie.ID), blang),
						Updated: f.Time(time.Now()),
						Links: []Link{
							{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?id=%d", basePath, serie.ID), blang), Type: FeedNavigationLinkType},
						},
						Content: &Content{
							Type:    FeedTextContentType,
							Content: h.MP[lang].Sprintf("~Titles - %d", bookCount),
						},
					}
					f.Entry = append(f.Entry, entry)
					continue
				}

				entry := &Entry{
					Title:   serie.Sort,
					ID:      idWithBlang(fmt.Sprintf("/opds/%s/exact_sort=%s", basePath, serie.Sort), blang),
					Updated: f.Time(time.Now()),
					Links: []Link{
						{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?exact_sort=%s", basePath, url.QueryEscape(serie.Sort)), blang), Type: FeedNavigationLinkType},
					},
					Content: &Content{
						Type:    FeedTextContentType,
						Content: h.MP[lang].Sprintf("~Series - %d", serie.Count), 
					},
				}
				f.Entry = append(f.Entry, entry)
				continue
			}
			
			entry := &Entry{
				Title:   serie.Sort,
				ID:      idWithBlang(fmt.Sprintf("/opds/%s/serie=%s", basePath, serie.Sort), blang),
				Updated: f.Time(time.Now()),
				Links: []Link{
					{Rel: FeedSubsectionLinkRel, Href: withBlang(fmt.Sprintf("/opds/%s?serie=%s%s", basePath, url.QueryEscape(serie.Sort), all), blang), Type: FeedNavigationLinkType},
				},
				Content: &Content{
					Type:    FeedTextContentType,
					Content: h.MP[lang].Sprintf("~Series - %d", serie.Count),
				},
			}
			f.Entry = append(f.Entry, entry)
		}
		addAllSeriesLink() 
		writeFeed(w, http.StatusOK, *f)
	}
}

func (h *Handler) serieBooks(w http.ResponseWriter, r *http.Request, days int) {
	blang := h.getBookLanguage(r)
	serieId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	serie := h.DB.SerieByID(serieId, blang)
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil {
		page = 1
	}
	offset := (page - 1) * h.CFG.OPDS.PAGE_SIZE

	books := h.DB.ListSerieBooks(serieId, days, h.CFG.OPDS.PAGE_SIZE+1, offset, blang)
	
	basePath := "series"
	if days > 0 {
		basePath = "latestseries"
	}
	
	selfHref := withBlang(fmt.Sprintf("/opds/%s?id=%d&page=%d", basePath, serieId, page), blang)
	f := NewFeed(serie.Name, "", selfHref)
	if len(books) > h.CFG.OPDS.PAGE_SIZE {
		nextRef := withBlang(fmt.Sprintf("/opds/%s?id=%d&page=%d", basePath, serieId, page+1), blang)
		nextLink := &Link{Rel: FeedNextLinkRel, Href: nextRef, Type: FeedNavigationLinkType}
		f.Link = append(f.Link, *nextLink)
		books = books[:h.CFG.OPDS.PAGE_SIZE-1]
	}

	h.feedBookEntries(r, books, f)
	writeFeed(w, http.StatusOK, *f)
}

func (h *Handler) books(w http.ResponseWriter, r *http.Request) {
	switch {
	default:
	case r.FormValue("id") != "":
		h.unloadBook(w, r)
		h.LOG.D.Println("UnloadBook")
	}
}

func (h *Handler) feedBookEntries(r *http.Request, books []*model.Book, f *Feed) {
	lang := h.getInterfaceLanguage(r)
	blang := h.getBookLanguage(r)
	
	for _, book := range books {
		book.Sequences = h.DB.SeriesByBookID(book.ID)

		var authorsList []Author
		var authorsLinks []Link
		authors := h.DB.AuthorsByBookId(book.ID)
		for _, a := range authors {
			a = h.fixIfNoSpecAuthorName(a, lang)
			author := Author{
				Name: a.Name,
			}
			authorLink := Link{
				Title: fmt.Sprintf("%s - %s", h.MP[lang].Sprintf("~All books by the author"), a.Name),
				Rel:   FeedRelatedLinkRel,
				Href:  withBlang(fmt.Sprintf("/opds/authors?id=%d", a.ID), blang),
				Type:  FeedNavigationLinkType,
			}

			authorsList = append(authorsList, author)
			authorsLinks = append(authorsLinks, authorLink)
		}

		links := append(authorsLinks, h.acquisitionLinks(book)...)
		
		for _, seq := range book.Sequences {
			serieLink := Link{
				Title: fmt.Sprintf("%s - %s", h.MP[lang].Sprintf("~All books in the series"), seq.Name),
				Rel:   FeedRelatedLinkRel,
				Href:  withBlang(fmt.Sprintf("/opds/series?id=%d&page=1", seq.ID), blang),
				Type:  FeedNavigationLinkType,
			}
			links = append(links, serieLink)
		}

		bookLang := ""
		if book.Language != nil && book.Language.Code != "" {
			bookLang = book.Language.Code
		}

		bookYear := ""
		if book.Year != "" && book.Year != "0" {
			bookYear = book.Year
		}
		
		bookFormat := book.Format
		if book.Format == "fb2" {
			bookFormat = "fb2+zip"
		}
		
		entry := &Entry{
			Title:   book.Title,
			ID:      fmt.Sprintf("/opds/books/id=%d", book.ID),
			Updated: f.Time(time.Now()),
			Links:   links,
			Authors: authorsList,
			DcLanguage: bookLang, 
			DcIssued:   bookYear, 
			DcFormat:   bookFormat,
			Content: &Content{
				Type:    FeedTextHtmlContentType,
				Content: h.contentInfo(r, book),
			},
		}
		f.Entry = append(f.Entry, entry)
	}
}

func (h *Handler) acquisitionLinks(book *model.Book) []Link {
	rel := "http://opds-spec.org/acquisition/open-access"
	link := []Link{}
	switch book.Format {
	case "fb2":
		linkFunc := func(convert string) Link {
			return Link{
				Rel:  rel,
				Href: fmt.Sprintf("/opds/books?id=%d&convert=%s", book.ID, convert),
				Type: mime.TypeByExtension(fmt.Sprintf(".fb2.%s", convert)),
			}
		}
		if h.CFG.OPDS.NO_CONVERSION {
			link = append(link, linkFunc("zip"))
		} else {
			link = append(link, linkFunc("epub"), linkFunc("zip"))
		}
	case "mobi", "azw", "azw3", "prc":
		link = append(link,
			Link{
				Rel:  rel,
				Href: fmt.Sprintf("/opds/books?id=%d", book.ID),
				Type: mime.TypeByExtension("." + book.Format), 
			},
		)	
	default:
		link = append(link,
			Link{
				Rel:  rel,
				Href: fmt.Sprintf("/opds/books?id=%d", book.ID),
				Type: mime.TypeByExtension("." + book.Format),
			},
		)

	}
	if book.Cover != "" {
		link = append(link,
			Link{
				Rel:  "http://opds-spec.org/image",
				Href: fmt.Sprintf("/opds/covers?cover=%d&h=%x", book.ID, book.Size),
				Type: mime.TypeByExtension(path.Ext(book.Cover)),
			},
		)
		link = append(link,
			Link{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/opds/covers?thumbnail=%d&h=%x", book.ID, book.Size),
				Type: mime.TypeByExtension(path.Ext(book.Cover)),
			},
		)
	}
	return link
}

func (h *Handler) unloadBook(w http.ResponseWriter, r *http.Request) {
	h.DownloadSema <- struct{}{}
	defer func() { <-h.DownloadSema }()
	
	lang := h.getInterfaceLanguage(r)
	bookId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	book := h.DB.FindBookById(bookId)
	if book == nil {
		writeMessage(w, http.StatusNotFound, h.MP[lang].Sprintf("~Book not found"))
		return
	}
	var rc io.ReadCloser
	if book.Archive == "" {
		var err error
		rc, err = os.Open(path.Join(h.CFG.Library.STOCK_DIR, book.File))
		if err != nil {
			writeMessage(w, http.StatusNotFound, h.MP[lang].Sprintf("~Book not found"))
			return
		}
	} else {
		zr, err := zip.OpenReader(path.Join(h.CFG.Library.STOCK_DIR, book.Archive))
		if err != nil {
			writeMessage(w, http.StatusNotFound, h.MP[lang].Sprintf("~Book not found"))
			return
		}
		defer zr.Close()
		for _, file := range zr.File {
			if file.Name == book.File {
				rc, _ = file.Open()
				break
			}
		}
	}

	if rc == nil {
		writeMessage(w, http.StatusNotFound, h.MP[lang].Sprintf("~Book not found"))
		return
	}
	defer rc.Close()

	convert := r.FormValue("convert")
	ext := ""
	switch convert {
	case "epub":
		ext = ".epub"
	case "zip":
		ext = ".zip"
	}
	authors := h.DB.AuthorsByBookId(bookId)
	authorName := ""
	switch {
	case len(authors) == 0:
		authorName = "Author not specified"
	case len(authors) > 1:
		authorName = "Group of authors"
	default:
		authorName = authors[0].Sort
	}

	w.Header().Add("Content-Type", mime.TypeByExtension("."+book.Format+ext))
	w.Header().Add("Content-Transfer-Encoding", "binary")
	w.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileNameByAuthorTitle(authorName, book.Title)+"."+book.Format+ext))
	w.WriteHeader(http.StatusOK)

	switch convert {
	case "epub":
		rsc, err := NewReadSeekCloser(rc)
		if err != nil {
			h.LOG.E.Println(err)
			return
		}
		wc := NewWriteCloser(w)
		err = h.ConvertFb2Epub(wc, rsc, bookId)
		if err != nil {
			h.LOG.E.Println(err)
		}
	case "zip":
		zipWriter := zip.NewWriter(w)
		defer zipWriter.Close()
		fileWriter, _ := zipWriter.CreateHeader(
			&zip.FileHeader{
				Name:   book.File,
				Method: zip.Deflate,
			},
		)
		io.Copy(fileWriter, rc)
		zipWriter.Flush()
	default:
		io.Copy(w, rc)
	}
}

var rxNotFileName = regexp.MustCompile(`[^0-9a-zA-Z-_]`)

const MAX_FILE_NAME_LEN = 232

func fileNameByAuthorTitle(author, title string) string {
	if author != "" {
		names := strings.Split(unidecode.Unidecode(parser.CollapseSpaces(strings.ReplaceAll(author, ",", " "))), " ")
		for i := range names {
			if names[i] != "" {
				names[i] = strings.ToLower(names[i])
				names[i] = strings.ToUpper(names[i][:1]) + names[i][1:]
			}
		}
		author = strings.Join(names, "-")
	}
	if title != "" {
		words := strings.Split(unidecode.Unidecode(strings.ReplaceAll(parser.CollapseSpaces(title), ",", " ")), " ")
		title = strings.Join(words, "-")
	}
	fileName := rxNotFileName.ReplaceAllString(unidecode.Unidecode(parser.CollapseSpaces(author+"_"+title)), "")
	switch {
	case len(fileName) == 0:
		fileName = "book"
	case len(fileName) > MAX_FILE_NAME_LEN:
		fileName = fileName[:MAX_FILE_NAME_LEN]
	}
	return fileName
}

func (h *Handler) covers(w http.ResponseWriter, r *http.Request) {

	h.CoverSema <- struct{}{}

	defer func() { <-h.CoverSema }()
	switch {
	case r.FormValue("cover") != "":
		h.LOG.D.Println(commentURL("Cover", r))
		h.unloadCover(w, r)
	case r.FormValue("thumbnail") != "":
		h.LOG.D.Println(commentURL("Thumbnail", r))
		h.unloadThumbnail(w, r)
	default:
		return
	}

}

func (h *Handler) unloadCover(w http.ResponseWriter, r *http.Request) {
	bookId, _ := strconv.ParseInt(r.FormValue("cover"), 10, 64)
	img := h.getCoverImage(bookId)
	if img == nil {
		return
	}
	w.Header().Add("Content-Disposition", "attachment; filename=cover.jpg")
	w.Header().Add("Content-Type", "image/jpeg")
	jpeg.Encode(w, img, nil)
}

func (h *Handler) unloadThumbnail(w http.ResponseWriter, r *http.Request) {
	bookId, _ := strconv.ParseInt(r.FormValue("thumbnail"), 10, 64)
	img := h.getCoverImage(bookId)
	if img == nil {
		return
	}
	img = resize.Resize(100, 0, img, resize.NearestNeighbor)
	w.Header().Add("Content-Disposition", "attachment; filename=thumbnail.jpg")
	w.Header().Add("Content-Type", "image/jpeg")
	jpeg.Encode(w, img, nil)
}

func (h *Handler) getCoverImage(bookId int64) (img image.Image) {
	book := h.DB.FindBookById(bookId)
	if book == nil {
		return nil
	}
	if book.Cover == "" {
		return nil
	}

	switch book.Format {
	case "fb2":
		img, err := fb2.GetCoverImage(h.CFG.Library.STOCK_DIR, book)
		if err != nil {
			h.LOG.D.Print(err)
			return nil
		}
		return img
	case "fb3":
		img, err := fb3.GetCoverImage(h.CFG.Library.STOCK_DIR, book)
		if err != nil {
			h.LOG.D.Print(err)
			return nil
		}
		return img	
	case "epub":
		img, err := epub.GetCoverImage(h.CFG.Library.STOCK_DIR, book)
		if err != nil {
			h.LOG.D.Print(err)
			return nil
		}
		return img
	case "mobi", "azw", "azw3", "prc":
		img, err := mobi.GetCoverImage(h.CFG.Library.STOCK_DIR, book)
		if err != nil {
			h.LOG.D.Print(err)
			return nil
		}
		return img		
	case "pdf":
		return nil
	}
	return nil
}

func sortAuthors(s []*model.Author, t language.Tag) {
	c := collate.New(t, collate.Force)
	sort.Slice(s, func(i, j int) bool {
		return c.CompareString(s[i].Sort, s[j].Sort) < 0
	})
}

func sortSeries(s []*model.Serie, t language.Tag) {
	c := collate.New(t, collate.Force)
	sort.Slice(s, func(i, j int) bool {
		return c.CompareString(s[i].Sort, s[j].Sort) < 0
	})
}

func (h *Handler) contentInfo(r *http.Request, b *model.Book) (info string) {
	lang := h.getInterfaceLanguage(r)
	info = "<div>"
	if b.Plot != "" {
		info += fmt.Sprintf("<p>%s</p>", b.Plot)
	}
	if b.Language.Code != "" {
		info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~Language"), formatLanguageName(b.Language.Code))
	}
	if b.Year != "0" {
		info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~Year"), b.Year)
	}
	if b.Archive != "" {
		info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~Archive"), b.Archive)
	}
	info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~File"), b.File)
	info += fmt.Sprintf("<br/>%s: %d Kb", h.MP[lang].Sprintf("~Size"), int(float32(b.Size)/1024))
	for _, seq := range b.Sequences {
		info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~Serie"), seq.Name)
		if seq.Num > 0 {
			info += fmt.Sprintf(" #%d", seq.Num)
		}
	}
	
	if b.Keywords != "" {
		info += fmt.Sprintf("<br/>%s: %s", h.MP[lang].Sprintf("~Keywords"), b.Keywords)
	}
	info += "<br/>"
	
	
	return info + "</div>"
}


func (h *Handler) ConvertFb2Epub(w io.WriteCloser, r io.ReadSeekCloser, b int64) error {
	fb := &cfb2.FB2Parser{
		BookId:  b,
		LOG:     h.LOG,
		DB:      h.DB,
		RC:      r,
		Decoder: u8xml.NewDecoder(r),
	}

	if err := fb.MakeEpub(w); err != nil {
		return err
	}
	return nil
}

type ResponseWriteCloser struct {
	http.ResponseWriter
}

func NewWriteCloser(w http.ResponseWriter) *ResponseWriteCloser {
	return &ResponseWriteCloser{
		ResponseWriter: w,
	}
}

func (w ResponseWriteCloser) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

func (w ResponseWriteCloser) Close() error {
	return nil
}

type BufferedReadSeekCloser struct {
	io.ReadSeeker
}

func NewReadSeekCloser(r io.ReadCloser) (*BufferedReadSeekCloser, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return &BufferedReadSeekCloser{
			ReadSeeker: rs,
		}, nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	rs := bytes.NewReader(b)

	return &BufferedReadSeekCloser{
		ReadSeeker: rs,
	}, nil
}

func (r BufferedReadSeekCloser) Close() error {
	return nil
}

func (h *Handler) folders(w http.ResponseWriter, r *http.Request) {
	lang := h.getInterfaceLanguage(r)
	
	folderId, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil || page < 1 {
		page = 1
	}

	currentFolder := h.DB.GetFolder(folderId)
	if currentFolder == nil {
		writeMessage(w, http.StatusNotFound, h.MP[lang].Sprintf("~Folder not found"))
		return
	}

	selfHref := fmt.Sprintf("/opds/folders?id=%d&page=%d", folderId, page)
	title := h.MP[lang].Sprintf("~Folders")
	if folderId != 0 {
		title = currentFolder.Name
	}
	f := NewFeed(title, "", selfHref)

	foldersCount, booksCount := h.DB.CountFolderItems(folderId)
	totalItems := foldersCount + booksCount

	if totalItems == 0 {
		writeFeed(w, http.StatusOK, *f)
		return
	}

	limit := h.CFG.OPDS.PAGE_SIZE
	offset := (page - 1) * limit

	var folders []*model.Folder
	var books []*model.Book

	if offset < foldersCount {
		fLimit := limit
		if offset + fLimit > foldersCount {
			fLimit = foldersCount - offset
		}
		folders = h.DB.PageFolders(folderId, fLimit, offset)
		
		remLimit := limit - len(folders)
		if remLimit > 0 && booksCount > 0 {
			books = h.DB.PageFolderBooks(folderId, remLimit, 0)
		}
	} else {
		bOffset := offset - foldersCount
		books = h.DB.PageFolderBooks(folderId, limit, bOffset)
	}

	for _, subFolder := range folders {
		entry := &Entry{
			Title:   subFolder.Name,
			ID:      fmt.Sprintf("/opds/folders/id=%d", subFolder.ID),
			Updated: f.Time(time.Now()),
			Links: []Link{
				{Rel: FeedSubsectionLinkRel, Href: fmt.Sprintf("/opds/folders?id=%d", subFolder.ID), Type: FeedNavigationLinkType},
			},
			Content: &Content{
				Type:    FeedTextContentType,
				Content: h.MP[lang].Sprintf("~Titles - %d", subFolder.Count),
			},
		}
		f.Entry = append(f.Entry, entry)
	}

	if len(books) > 0 {
		h.feedBookEntries(r, books, f)
	}

	if totalItems > limit {
		if page > 1 {
			f.Link = append(f.Link, Link{Rel: FeedFirstLinkRel, Href: fmt.Sprintf("/opds/folders?id=%d&page=1", folderId), Type: FeedNavigationLinkType})
			f.Link = append(f.Link, Link{Rel: FeedPrevLinkRel, Href: fmt.Sprintf("/opds/folders?id=%d&page=%d", folderId, page-1), Type: FeedNavigationLinkType})
		}
		
		lastPage := int(math.Ceil(float64(totalItems) / float64(limit)))
		if page < lastPage {
			f.Link = append(f.Link, Link{Rel: FeedNextLinkRel, Href: fmt.Sprintf("/opds/folders?id=%d&page=%d", folderId, page+1), Type: FeedNavigationLinkType})
			f.Link = append(f.Link, Link{Rel: FeedLastLinkRel, Href: fmt.Sprintf("/opds/folders?id=%d&page=%d", folderId, lastPage), Type: FeedNavigationLinkType})
		}
	}

	writeFeed(w, http.StatusOK, *f)
}