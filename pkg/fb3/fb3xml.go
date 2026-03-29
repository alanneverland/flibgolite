package fb3

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"path"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/vinser/flibgolite/pkg/model"
	"golang.org/x/net/html/charset"
)

// Relationships (Open XML _rels/.rels)
type Relationships struct {
	XMLName      xml.Name       `xml:"Relationships"`
	Relationship []Relationship `xml:"Relationship"`
}

type Relationship struct {
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Id     string `xml:"Id,attr"`
}

type FB3Subject struct {
	Link       string `xml:"link,attr"`
	FirstName  string `xml:"first-name"`
	MiddleName string `xml:"middle-name"`
	LastName   string `xml:"last-name"`
	Nickname   string `xml:"nickname"`
	Title      struct {
		Main string `xml:"main"`
	} `xml:"title"`
}

// FB3 Description Document
type FB3Description struct {
	descPath  string
	coverPath string

	XMLName xml.Name `xml:"fb3-description"`

	Title struct {
		Main string `xml:"main"`
	} `xml:"title"`

	Sequence []struct {
		Number int `xml:"number,attr"`
		Title  struct {
			Main string `xml:"main"`
		} `xml:"title"`
	} `xml:"sequence"`

	Relations struct {
		Subjects []FB3Subject `xml:"subject"`
	} `xml:"relations"`

	Classification struct {
		Subjects []string `xml:"subject"`
	} `xml:"classification"`

	RelationsFb3 struct {
		Subjects []FB3Subject `xml:"subject"`
	} `xml:"fb3-relations"`

	ClassificationFb3 struct {
		Subjects []string `xml:"subject"`
	} `xml:"fb3-classification"`

	Lang string `xml:"lang"`

	Written struct {
		Lang string `xml:"lang"`
		Date struct {
			Value string `xml:"value,attr"`
			Text  string `xml:",chardata"`
		} `xml:"date"`
	} `xml:"written"`

	Keywords string `xml:"keywords"`
	Annotation struct {
		P []string `xml:"p"`
	} `xml:"annotation"`
}

func (fb *FB3Description) String() string {
	return fmt.Sprintf(
		"=========FB3===================\nTitle: %s\nLang: %s\nCover: %s\n===============================\n",
		fb.Title.Main, fb.Lang, fb.coverPath,
	)
}

// Creates an FB3 package object from a ZIP file.
func NewFB3(zr *zip.ReadCloser) (*FB3Description, error) {
	fb3 := &FB3Description{}

	f, err := zr.Open("_rels/.rels")
	if err == nil {
		rels := &Relationships{}
		if err = decodeXML(f, &rels); err == nil {
			for _, r := range rels.Relationship {
				if strings.Contains(r.Type, "thumbnail") {
					fb3.coverPath = strings.TrimPrefix(r.Target, "/")
				}
				if strings.Contains(r.Type, "description") || strings.HasSuffix(strings.ToLower(r.Target), "description.xml") {
					fb3.descPath = strings.TrimPrefix(r.Target, "/")
				}
			}
		}
		f.Close()
	}

	if fb3.descPath == "" {
		fb3.descPath = "fb3/description.xml"
	}

	descFile, err := zr.Open(fb3.descPath)
	if err != nil {
		return nil, fmt.Errorf("could not open FB3 description %s: %w", fb3.descPath, err)
	}
	defer descFile.Close()

	if err := decodeXML(descFile, &fb3); err != nil {
		return nil, err
	}

	return fb3, nil
}

func GetCoverImage(stock string, book *model.Book) (image.Image, error) {
	zr, err := zip.OpenReader(path.Join(stock, book.File))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	rc, err := zr.Open(book.Cover)
	if err == nil {
		defer rc.Close()
		img, _, err := image.Decode(bufio.NewReader(rc))
		if err == nil {
			return img, nil
		}
	}

	for _, file := range zr.File {
		if strings.Contains(file.Name, book.Cover) {
			rcOld, err := file.Open()
			if err != nil {
				continue
			}

			img, _, err := image.Decode(bufio.NewReader(rcOld))
			rcOld.Close()

			if err == nil {
				return img, nil
			}
		}
	}

	return nil, fmt.Errorf("could not find or decode cover %s", book.Cover)
}

// Utils
func decodeXML(r io.Reader, v interface{}) error {
	decoder := xml.NewDecoder(r)
	decoder.Entity = xml.HTMLEntity
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder.Decode(v)
}