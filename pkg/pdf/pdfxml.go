package pdf

import (
	"fmt"
	"os"
	"path/filepath"

	pdf "github.com/sassoftware/pdf-xtract"
)

type PDF struct {
	FileName string
	Info     map[string]string
}

func NewPDF(path string) (p *PDF, err error) {

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdf-xtract panic on file %s: %v", path, r)
			p = nil
		}
	}()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	r, err := pdf.NewReader(f, fi.Size())
	if err != nil {
		return nil, err
	}

	p = &PDF{
		FileName: filepath.Base(path),
		Info:     make(map[string]string),
	}

	infoDict := r.Trailer().Key("Info")
	if !infoDict.IsNull() {
		fields := []string{"Title", "Author", "Subject", "CreationDate", "Keywords"}
		for _, field := range fields {
			if val := infoDict.Key(field); !val.IsNull() {
				p.Info[field] = val.String()
			}
		}
	}

	rootDict := r.Trailer().Key("Root")
	if !rootDict.IsNull() {
		langField := rootDict.Key("Lang")
		if !langField.IsNull() {
			p.Info["Lang"] = langField.String()
		}
	}
	
	firstPage := r.Page(1)
    if !firstPage.V.IsNull() {
        if text, err := firstPage.GetPlainText(nil); err == nil {
            p.Info["FirstPageText"] = text
        }
    }

	return p, nil
}