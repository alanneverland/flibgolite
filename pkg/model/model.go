package model

type Language struct {
	ID   int64
	Code string
	Name string
}

type LanguageStat struct {
	Code  string	
	Name string
	Count int
}

type Author struct {
	ID    int64
	Name  string
	Sort  string
	Count int 
}

type Sequence struct {
	ID   int64
	Name string
	Sort  string
	Num  int
}

type Book struct {
	ID       int64
	FolderID int64
	File     string
	Archive  string
	Size     int64
	Format   string
	Title    string
	Sort     string
	Year     string
	Plot     string
	Cover    string
	Language *Language
	Authors  []*Author
	Genres   []string
	Keywords string	
	Updated  int64
	Sequences []*Sequence
	IsMultiArchive bool
}

type Genre struct {
	ID    int64
	Code  string
	Bunch string
	Name  string
}

type Serie struct {
	ID    int64
	Name  string	
	Sort  string
	Count int 
}

type Folder struct {
	ID       int64
	ParentID int64
	Name     string	
	Count    int
}