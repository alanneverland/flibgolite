package mobi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/vinser/flibgolite/pkg/model"
)

type MOBI struct {
	mobiPath string
	Title    string
	Authors  []string
	Year     string
	Language string
	Plot     string
	Genres   []string
	CoverIdx string 
}

func decode2int(src string, verifyPresent bool) uint64 {
	var res uint64 = 0
	var shift uint64 = 0

	last := len(src) - 1
	if verifyPresent {
		last = len(src) - 2
	}

	for i := last; i >= 0; i-- {
		ch := src[i]
		var val uint64

		switch {
		case ch >= '0' && ch <= '9':
			val = uint64(ch - '0')
		case ch >= 'a' && ch <= 'v':
			val = uint64(ch - 'a' + 10)
		case ch >= 'A' && ch <= 'V':
			val = uint64(ch - 'A' + 10)
		default:
			return 0
		}

		res |= (val << shift)
		shift += 5
	}

	return res
}

func verifyAndSetCover(reader *bytes.Reader, coverOffset uint32, firstImageRec uint32, recordOffsets []uint32, mobi *MOBI) bool {
	
	if firstImageRec == 0 {
		return false
	}

	imageRecordIndex := int(firstImageRec + coverOffset)
	
	if imageRecordIndex >= len(recordOffsets)-1 {
		return false 
	}

	imgStart := int64(recordOffsets[imageRecordIndex])
	var imgEnd int64

	if imageRecordIndex == len(recordOffsets)-1 {
		imgEnd = reader.Size()
	} else {
		imgEnd = int64(recordOffsets[imageRecordIndex+1])
	}
	
	imgSize := imgEnd - imgStart
	
	if imgSize < 4 {
		return false 
	}

	currentPos, _ := reader.Seek(0, io.SeekCurrent)
	defer reader.Seek(currentPos, io.SeekStart)

	reader.Seek(imgStart, io.SeekStart)
	magic := make([]byte, 4)
	reader.Read(magic)
	
	ext := ""
	isImage := false
	if magic[0] == 0xFF && magic[1] == 0xD8 && magic[2] == 0xFF {
		isImage = true
		ext = ".jpg"
	} else if string(magic) == "\x89PNG" {
		isImage = true
		ext = ".png"
	} else if string(magic) == "GIF8" {
		isImage = true
		ext = ".gif"
	} else {
		//fmt.Println("-> Формат НЕ определен (неизвестные магические байты)")
	}

	if isImage {
		mobi.CoverIdx = strconv.Itoa(imageRecordIndex) + ext
		return true
	}

	return false
}

func NewMOBI(r io.Reader, filePath string) (*MOBI, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(b)

	mobi := &MOBI{mobiPath: filePath, CoverIdx: ""}

	if _, err := reader.Seek(0x4C, io.SeekStart); err != nil {
		return nil, err
	}

	var numRecords uint16
	if err := binary.Read(reader, binary.BigEndian, &numRecords); err != nil {
		return nil, err
	}

	recordOffsets := make([]uint32, numRecords)
	for i := 0; i < int(numRecords); i++ {
		binary.Read(reader, binary.BigEndian, &recordOffsets[i])
		reader.Seek(4, io.SeekCurrent)
	}

	if len(recordOffsets) < 2 {
		return nil, fmt.Errorf("invalid mobi: not enough records")
	}

	rec0Offset := int64(recordOffsets[0])

	reader.Seek(rec0Offset+0x10, io.SeekStart)
	magic := make([]byte, 4)
	reader.Read(magic)
	if string(magic) != "MOBI" {
		return nil, fmt.Errorf("not a valid MOBI format")
	}

	var headerLength uint32
	binary.Read(reader, binary.BigEndian, &headerLength)

	reader.Seek(rec0Offset+0x54, io.SeekStart)
	var titleOffset, titleLength uint32
	binary.Read(reader, binary.BigEndian, &titleOffset)
	binary.Read(reader, binary.BigEndian, &titleLength)

	if titleLength > 0 {
		titleBytes := make([]byte, titleLength)
		reader.Seek(rec0Offset+int64(titleOffset), io.SeekStart)
		reader.Read(titleBytes)
		mobi.Title = string(titleBytes)
	}

	reader.Seek(rec0Offset+0x6C, io.SeekStart)
	var firstImageRec uint32
	binary.Read(reader, binary.BigEndian, &firstImageRec)

	exthOffset := rec0Offset + 0x10 + int64(headerLength)
	reader.Seek(exthOffset, io.SeekStart)
	exthMagic := make([]byte, 4)
	reader.Read(exthMagic)

	if string(exthMagic) == "EXTH" {
		var exthLength, exthCount uint32
		binary.Read(reader, binary.BigEndian, &exthLength)
		binary.Read(reader, binary.BigEndian, &exthCount)

		foundCover := false 

		for i := 0; i < int(exthCount); i++ {
			var recType, recLength uint32
			binary.Read(reader, binary.BigEndian, &recType)
			binary.Read(reader, binary.BigEndian, &recLength)

			if recLength < 8 {
				break
			}
			data := make([]byte, recLength-8)
			reader.Read(data)

			switch recType {
			case 100:
				mobi.Authors = append(mobi.Authors, string(data))
			case 103:
				mobi.Plot = string(data)
			case 105:
				mobi.Genres = append(mobi.Genres, string(data))
			case 106:
				mobi.Year = string(data)
			case 503:
				mobi.Title = string(data)
			case 524:
				mobi.Language = string(bytes.TrimRight(data, "\x00"))
				
			case 201:
				if !foundCover && len(data) == 4 {
					offset := binary.BigEndian.Uint32(data)
					foundCover = verifyAndSetCover(reader, offset, firstImageRec, recordOffsets, mobi)
				}
				
			case 129:
				if !foundCover {
					s := string(data)
					if strings.HasPrefix(s, "kindle:embed:") {
						s = strings.TrimPrefix(s, "kindle:embed:")
						if idx := strings.IndexByte(s, '?'); idx != -1 {
							s = s[:idx]
						}
						k := decode2int(s, false)
						if k > 0 {
							foundCover = verifyAndSetCover(reader, uint32(k-1), firstImageRec, recordOffsets, mobi)
						}
					}
				}
			}
		}
	}

	return mobi, nil
}

func GetCoverImage(stock string, book *model.Book) (image.Image, error) {
	if book.Cover == "" {
		return nil, fmt.Errorf("cover index is empty") //
	}
	
	coverStr := book.Cover
	if dotIdx := strings.Index(coverStr, "."); dotIdx != -1 {
		coverStr = coverStr[:dotIdx]
	}

	recordIndex, err := strconv.Atoi(coverStr) 
	if err != nil {
		return nil, fmt.Errorf("invalid cover index %q: %v", book.Cover, err) //
	}

	var seeker io.ReadSeeker
	var closer io.Closer

	if book.Archive != "" {
		zipPath := path.Join(stock, book.Archive)
		zr, err := zip.OpenReader(zipPath)
		if err != nil {
			return nil, err //
		}
		defer zr.Close()

		rc, err := zr.Open(book.File) 
		if err != nil {
			return nil, fmt.Errorf("file %s not found in archive %s: %v", book.File, book.Archive, err)
		}
		defer rc.Close()

		b, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		seeker = bytes.NewReader(b)
	} else {
		f, err := os.Open(path.Join(stock, book.File))
		if err != nil {
			return nil, err
		}
		closer = f
		seeker = f
	}
	if closer != nil {
		defer closer.Close() //
	}

	if _, err := seeker.Seek(0x4C, io.SeekStart); err != nil {
		return nil, err
	}
	var numRecords uint16
	binary.Read(seeker, binary.BigEndian, &numRecords) //

	if recordIndex >= int(numRecords) {
		return nil, fmt.Errorf("record index %d out of bounds (total %d)", recordIndex, numRecords)
	}

	offsetPos := int64(0x4E + recordIndex*8)
	if _, err := seeker.Seek(offsetPos, io.SeekStart); err != nil {
		return nil, err
	}

	var imgStart, attributes uint32
	binary.Read(seeker, binary.BigEndian, &imgStart)
	binary.Read(seeker, binary.BigEndian, &attributes) //
	
	var imgSize int64
	if recordIndex == int(numRecords)-1 {
		currPos, _ := seeker.Seek(0, io.SeekCurrent)
		fileSize, _ := seeker.Seek(0, io.SeekEnd)
		seeker.Seek(currPos, io.SeekStart)
		imgSize = fileSize - int64(imgStart)
	} else {
		var imgEnd uint32
		binary.Read(seeker, binary.BigEndian, &imgEnd)
		imgSize = int64(imgEnd) - int64(imgStart)
	}
	
	if imgSize <= 4 {
		return nil, fmt.Errorf("invalid image size: %d", imgSize) //
	}

	imgBytes := make([]byte, imgSize)
	seeker.Seek(int64(imgStart), io.SeekStart)
	seeker.Read(imgBytes)

	img, _, err := image.Decode(bytes.NewReader(imgBytes)) //
	return img, err
}
