// Package orderimport turns an uploaded spreadsheet of orders into real orders,
// processing the rows with a bounded pool of workers.
//
// Re-importing the same file is safe: each row gets a deterministic idempotency
// key, so the orders.idempotency_key unique index collapses repeats. That is the
// same guarantee the gateway gives one request, applied to a whole file.
package orderimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// MaxRows caps how many rows one import may hold, to bound memory and time.
// Large enough for the 300k-row product exports.
const MaxRows = 500_000

// ErrTooManyRows means the file has more than MaxRows data rows.
var ErrTooManyRows = fmt.Errorf("orderimport: more than %d rows", MaxRows)

// Row is one parsed line. Line is 1-based, counting the header.
type Row struct {
	Item     string
	Quantity int
	Line     int
}

// RowError is a row that couldn't be parsed. Such rows are reported, not
// imported, so one bad line doesn't sink the whole file.
type RowError struct {
	Line    int
	Message string
}

// zip local file header: .xlsx is a zip, CSV is not.
var zipMagic = []byte{'P', 'K', 0x03, 0x04}

// Parse reads rows from data, choosing CSV or XLSX by content. It returns the
// valid rows and, separately, the lines it couldn't read.
func Parse(data []byte) ([]Row, []RowError, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return parseXLSX(data)
	}
	return parseCSV(data)
}

// header maps the item and quantity columns to their positions. It understands
// the simple (item, quantity) layout and the ERP produtos export, where the
// name is the item and quantidade_em_estoque is the quantity.
type header struct{ item, quantity int }

var (
	itemAliases     = []string{"item", "produto", "product", "name", "nome"}
	quantityAliases = []string{"quantity", "quantidade", "qty", "qtd", "quantidade_em_estoque", "estoque", "stock"}
)

func matches(cell string, aliases []string) bool {
	return slices.Contains(aliases, strings.ToLower(strings.TrimSpace(cell)))
}

func mapHeader(cells []string) (header, error) {
	h := header{item: -1, quantity: -1}
	for i, c := range cells {
		if h.item < 0 && matches(c, itemAliases) {
			h.item = i
		}
		if h.quantity < 0 && matches(c, quantityAliases) {
			h.quantity = i
		}
	}
	if h.item < 0 || h.quantity < 0 {
		return h, errors.New("orderimport: header needs an item column (item/name) and a quantity column (quantity/quantidade_em_estoque)")
	}
	return h, nil
}

// parseQuantity reads an int or a decimal, in dot or pt-BR comma form, and
// floors it. Stock columns are doubles, so 100.0 and 100,0 both mean 100.
func parseQuantity(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if strings.ContainsRune(s, ',') && !strings.ContainsRune(s, '.') {
		s = strings.ReplaceAll(s, ",", ".")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return int(math.Floor(f)), true
}

func toRow(h header, cells []string, line int) (Row, *RowError) {
	get := func(i int) string {
		if i >= 0 && i < len(cells) {
			return strings.TrimSpace(cells[i])
		}
		return ""
	}
	item := get(h.item)
	rawQty := get(h.quantity)
	if item == "" && rawQty == "" {
		return Row{}, nil // blank line: skip silently
	}
	if item == "" || len(item) > 200 {
		return Row{}, &RowError{Line: line, Message: "item is empty or too long"}
	}
	qty, ok := parseQuantity(rawQty)
	if !ok || qty < 1 || qty > 10_000 {
		return Row{}, &RowError{Line: line, Message: fmt.Sprintf("quantity %q must be a number from 1 to 10000", rawQty)}
	}
	return Row{Item: item, Quantity: qty, Line: line}, nil
}

// sniffDelimiter picks ';' when the first line has more of them than commas,
// which is how Excel saves CSV in pt-BR locales.
func sniffDelimiter(data []byte) rune {
	line, _, _ := bytes.Cut(data, []byte{'\n'})
	if bytes.Count(line, []byte{';'}) > bytes.Count(line, []byte{','}) {
		return ';'
	}
	return ','
}

func parseCSV(data []byte) ([]Row, []RowError, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = sniffDelimiter(data)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	first, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, errors.New("orderimport: file is empty")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("orderimport: %w", err)
	}
	h, err := mapHeader(first)
	if err != nil {
		return nil, nil, err
	}

	var rows []Row
	var bad []RowError
	line := 1
	for {
		line++
		cells, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			bad = append(bad, RowError{Line: line, Message: "malformed CSV line"})
			continue
		}
		if len(rows) >= MaxRows {
			return nil, nil, ErrTooManyRows
		}
		if row, re := toRow(h, cells, line); re != nil {
			bad = append(bad, *re)
		} else if row.Item != "" {
			rows = append(rows, row)
		}
	}
	return rows, bad, nil
}

func parseXLSX(data []byte) ([]Row, []RowError, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("orderimport: not a valid spreadsheet: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, errors.New("orderimport: spreadsheet has no sheets")
	}
	it, err := f.Rows(sheets[0])
	if err != nil {
		return nil, nil, fmt.Errorf("orderimport: %w", err)
	}
	defer it.Close()

	var h header
	var rows []Row
	var bad []RowError
	line := 0
	for it.Next() {
		line++
		cells, err := it.Columns()
		if err != nil {
			bad = append(bad, RowError{Line: line, Message: "could not read row"})
			continue
		}
		if line == 1 {
			if h, err = mapHeader(cells); err != nil {
				return nil, nil, err
			}
			continue
		}
		if len(rows) >= MaxRows {
			return nil, nil, ErrTooManyRows
		}
		if row, re := toRow(h, cells, line); re != nil {
			bad = append(bad, *re)
		} else if row.Item != "" {
			rows = append(rows, row)
		}
	}
	if line == 0 {
		return nil, nil, errors.New("orderimport: file is empty")
	}
	return rows, bad, it.Error()
}
