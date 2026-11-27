package ai

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Folder names compared the way a person reads them, so AI reuses a folder
// instead of making a near-duplicate: case, accents, separators and numbering
// don't count, plural and singular are the same, and common folders are one
// concept across languages.

type concept struct {
	key, en, de           string
	english, german, rest []string
}

func list(values string) []string {
	if values == "" {
		return nil
	}
	return strings.Split(values, ",")
}

func newConcept(key, en, de, english, german, rest string) concept {
	return concept{key, en, de, list(english), list(german), list(rest)}
}

var concepts = []concept{
	newConcept("photos", "Photos", "Fotos", "photos,pictures,images,pics,camera roll,camera uploads,camera,dcim,gallery", "fotos,bilder,bild,kamera,kamera uploads,kamerauploads,galerie", "imagenes,photographies,foto"),
	newConcept("screenshots", "Screenshots", "Bildschirmfotos", "screenshots,screen shots", "bildschirmfotos,bildschirmfoto", ""),
	newConcept("videos", "Videos", "Videos", "videos,movies,clips", "filme,film", ""),
	newConcept("documents", "Documents", "Dokumente", "documents,docs,paperwork,papers", "dokumente,unterlagen,papiere,akten", "documentos,documenti"),
	newConcept("invoices", "Invoices", "Rechnungen", "invoices,bills", "rechnungen", "factures,facturas,fatture"),
	newConcept("receipts", "Receipts", "Belege", "receipts", "belege,quittungen,kassenbons,kassenzettel,kassenbelege", "recus,recibos"),
	newConcept("contracts", "Contracts", "Verträge", "contracts,agreements", "vertrage", "contrats,contratos"),
	newConcept("payslips", "Payslips", "Gehaltsabrechnungen", "payslips,pay slips,paychecks,salary", "gehaltsabrechnungen,lohnabrechnungen,gehalt,lohn,entgeltabrechnungen", ""),
	newConcept("statements", "Bank statements", "Kontoauszüge", "statements,bank statements", "kontoauszuge", ""),
	newConcept("taxes", "Taxes", "Steuern", "taxes,tax,tax returns", "steuern,steuer,steuererklarung,steuererklarungen", "impots"),
	newConcept("insurance", "Insurance", "Versicherungen", "insurance,insurances", "versicherungen", "assurances,seguros"),
	newConcept("letters", "Letters", "Briefe", "letters,correspondence", "briefe,korrespondenz,schreiben", ""),
	newConcept("certificates", "Certificates", "Zeugnisse", "certificates,diplomas", "zeugnisse,urkunden,zertifikate", ""),
	newConcept("manuals", "Manuals", "Anleitungen", "manuals,instructions,user guides", "anleitungen,bedienungsanleitungen,handbucher,handbuch", ""),
	newConcept("finance", "Finance", "Finanzen", "finance,finances,banking,money", "finanzen,geld", "finanzas"),
	newConcept("music", "Music", "Musik", "music,songs", "musik", "musique,musica"),
	newConcept("downloads", "Downloads", "Downloads", "downloads", "", ""),
	newConcept("scans", "Scans", "Scans", "scans,scanned", "gescannt", ""),
	newConcept("health", "Health", "Gesundheit", "health,medical", "gesundheit,arzt,medizin", ""),
	newConcept("travel", "Travel", "Reisen", "travel,trips,vacation,holidays", "reisen,urlaub", ""),
	newConcept("work", "Work", "Arbeit", "work", "arbeit,beruf", ""),
	newConcept("car", "Car", "Auto", "car,cars,vehicle", "auto,fahrzeug,kfz", ""),
	newConcept("home", "Home", "Wohnung", "house,housing,apartment", "wohnung,haus,miete", ""),
	newConcept("presentations", "Presentations", "Präsentationen", "presentations,slides", "prasentationen", ""),
}

// Folders that collect unsorted things: files in them may be filed anywhere.
var inboxStems = []string{"inbox", "eingang", "unsorted", "unsortiert", "misc", "sonstig", "sonstige", "desktop", "schreibtisch", "new", "neu", "temp", "tmp", "to sort", "zu sortieren"}

var months = [][]string{
	list("jan,januar,january,janvier,enero"), list("feb,februar,february,fevrier,febrero"), list("mar,marz,march,mars,marzo,maerz"),
	list("apr,april,avril,abril"), list("may,mai,mayo"), list("jun,juni,june,juin,junio"), list("jul,juli,july,juillet,julio"),
	list("aug,august,aout,agosto"), list("sep,sept,september,septembre,septiembre"), list("oct,okt,oktober,october,octobre,octubre"),
	list("nov,november,novembre,noviembre"), list("dec,dez,dezember,december,decembre,diciembre"),
}

var (
	nameSeparators   = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	leadingNumbering = regexp.MustCompile(`^\d{1,3} (\p{L})`)
)

// normalizeFolderName lowercases, folds accents and ß, and drops separators and
// leading numbering ("01_", "2. ").
func normalizeFolderName(name string) string {
	folded := strings.Map(func(char rune) rune {
		if char >= 0x300 && char <= 0x36f {
			return -1
		}
		return char
	}, norm.NFKD.String(name))
	folded = strings.ToLower(strings.ReplaceAll(folded, "ß", "ss"))
	folded = strings.TrimSpace(nameSeparators.ReplaceAllString(folded, " "))
	return leadingNumbering.ReplaceAllString(folded, "$1")
}

// singular, by rules that are safe in English and German.
func singular(word string) string {
	length := utf8.RuneCountInString
	if length(word) <= 3 {
		return word
	}
	if strings.HasSuffix(word, "ungen") {
		return word[:len(word)-2]
	}
	if strings.HasSuffix(word, "ies") && length(word) > 4 {
		return word[:len(word)-3] + "y"
	}
	for _, suffix := range []string{"sses", "ches", "shes", "xes"} {
		if strings.HasSuffix(word, suffix) {
			return word[:len(word)-2]
		}
	}
	if strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && !strings.HasSuffix(word, "us") {
		word = word[:len(word)-1]
	}
	if strings.HasSuffix(word, "e") && length(word) > 5 {
		word = word[:len(word)-1]
	}
	return word
}

func stemName(name string) string {
	words := strings.Fields(normalizeFolderName(name))
	for index, word := range words {
		words[index] = singular(word)
	}
	return strings.Join(words, " ")
}

var conceptByStem, languageByStem = func() (map[string]*concept, map[string]string) {
	byStem, languages := map[string]*concept{}, map[string]string{}
	for index := range concepts {
		item := &concepts[index]
		for _, group := range []struct {
			language string
			terms    []string
		}{{"en", item.english}, {"de", item.german}, {"", item.rest}} {
			for _, term := range append(slices.Clone(group.terms), item.en, item.de) {
				key := stemName(term)
				if byStem[key] == nil {
					byStem[key] = item
				}
				if group.language != "" && languages[key] == "" && slices.Contains(group.terms, term) {
					languages[key] = group.language
				}
			}
		}
	}
	return byStem, languages
}()

// conceptOf is the concept a folder name stands for, or "".
func conceptOf(name string) string {
	if item := conceptByStem[stemName(name)]; item != nil {
		return item.key
	}
	return ""
}

// folderKey is what two folder names are compared by; equal keys mean "the same
// folder".
func folderKey(name string) string {
	value := stemName(name)
	if item := conceptByStem[value]; item != nil {
		return "concept:" + item.key
	}
	return value
}

func localizedConceptName(name, language string) string {
	item := conceptByStem[stemName(name)]
	switch {
	case item == nil:
		return ""
	case language == "de":
		return item.de
	}
	return item.en
}

// folderLanguage is the language the person names folders in: "de", "en" or ""
// when the names don't say.
func folderLanguage(names []string) string {
	de, en := 0, 0
	for _, name := range names {
		if strings.ContainsAny(name, "äöüÄÖÜß") {
			de++
		}
		switch languageByStem[stemName(name)] {
		case "de":
			de++
		case "en":
			en++
		}
	}
	switch {
	case de > en:
		return "de"
	case en > de:
		return "en"
	}
	return ""
}

func isInboxName(name string) bool {
	value := stemName(name)
	found := conceptOf(name)
	return slices.Contains(inboxStems, value) || slices.Contains(inboxStems, normalizeFolderName(name)) || found == "downloads" || found == "scans"
}

// isMediaName: folders of these concepts hold photos and videos.
func isMediaName(name string) bool {
	found := conceptOf(name)
	return found == "photos" || found == "screenshots" || found == "videos"
}

func isYearName(name string, year int) bool { return normalizeFolderName(name) == strconv.Itoa(year) }

// isMonthName: "03", "3", "2026-03", "März", "03 March".
func isMonthName(name string, month int) bool {
	value := normalizeFolderName(name)
	if regexp.MustCompile(fmt.Sprintf(`^(\d{4} )?0?%d( |$)`, month)).MatchString(value) || value == fmt.Sprintf("%02d", month) {
		return true
	}
	return slices.ContainsFunc(strings.Split(value, " "), func(word string) bool { return slices.Contains(months[month-1], word) })
}

var forbiddenInName = regexp.MustCompile(`[\x00-\x1f\x7f/\\:*?"<>|]`)

// cleanNewFolderName makes a name the model offered safe and generic: at most
// three words and 40 characters, no digits, no path characters; else "".
func cleanNewFolderName(value string) string {
	name := strings.Join(strings.Fields(forbiddenInName.ReplaceAllString(value, "")), " ")
	if name == "" || utf8.RuneCountInString(name) > 40 || name == "." || name == ".." {
		return ""
	}
	if strings.ContainsFunc(name, unicode.IsDigit) || len(strings.Split(name, " ")) > 3 {
		return ""
	}
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}
