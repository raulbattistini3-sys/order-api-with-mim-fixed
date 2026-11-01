package orderimport

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/xuri/excelize/v2"
	"github.com/you/orderguard/internal/order"
)

// fakeRepo mimics the orders store: a unique index on the idempotency key.
type fakeRepo struct {
	mu   sync.Mutex
	keys map[string]bool
	fail bool
}

func (r *fakeRepo) Create(_ context.Context, o order.Order) (order.Order, error) {
	if r.fail {
		return order.Order{}, fmt.Errorf("db down")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys[o.IdempotencyKey] {
		return order.Order{}, order.ErrDuplicate
	}
	r.keys[o.IdempotencyKey] = true
	return o, nil
}

func (r *fakeRepo) ListByCustomer(context.Context, string, int) ([]order.Order, int, error) {
	return nil, 0, nil
}

func TestParseCSV(t *testing.T) {
	csv := "item,quantity\nTeclado,2\n Mouse , 3 \n,\nMonitor,0\nCadeira,x\nMesa,4\n"
	rows, bad, err := Parse([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 { // Teclado, Mouse, Mesa
		t.Fatalf("rows = %d (%+v)", len(rows), rows)
	}
	if rows[0] != (Row{Item: "Teclado", Quantity: 2, Line: 2}) || rows[1].Item != "Mouse" || rows[1].Quantity != 3 {
		t.Fatalf("parsed rows = %+v", rows)
	}
	if len(bad) != 2 { // quantity 0, quantity x
		t.Fatalf("bad = %+v", bad)
	}
}

func TestParseColumnsInAnyOrder(t *testing.T) {
	rows, _, err := Parse([]byte("quantidade,produto\n5,Teclado\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Item != "Teclado" || rows[0].Quantity != 5 {
		t.Fatalf("rows = %+v", rows)
	}
}

// The ERP produtos export: semicolon-delimited, pt-BR decimals, many extra
// columns, name as the item and quantidade_em_estoque as the quantity.
func TestParseProdutosExport(t *testing.T) {
	csv := "name;ativo;sku;categoria_id;valor_de_custo;icms;valor_de_venda;data_de_cadastro;quantidade_em_estoque\n" +
		"Teclado Mecânico;true;SKU-1;abc;10,50;0,18;29,90;2024-01-01 10:00:00;120,0\n" +
		"Mouse;true;SKU-2;abc;5,00;0,18;19,90;2024-01-02 11:00:00;3\n" +
		"Sem estoque;false;SKU-3;abc;1,00;0,18;2,00;2024-01-03 09:00:00;0\n"
	rows, bad, err := Parse([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Item != "Teclado Mecânico" || rows[0].Quantity != 120 || rows[1].Quantity != 3 {
		t.Fatalf("parsed = %+v", rows)
	}
	if len(bad) != 1 || bad[0].Line != 4 { // stock 0 is not a valid order quantity
		t.Fatalf("bad = %+v", bad)
	}
}

func TestParseXLSX(t *testing.T) {
	f := excelize.NewFile()
	f.SetSheetRow("Sheet1", "A1", &[]any{"item", "quantity"})
	f.SetSheetRow("Sheet1", "A2", &[]any{"Teclado", 2})
	f.SetSheetRow("Sheet1", "A3", &[]any{"Monitor", 5})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	rows, bad, err := Parse(buf.Bytes())
	if err != nil || len(bad) != 0 {
		t.Fatalf("err=%v bad=%+v", err, bad)
	}
	if len(rows) != 2 || rows[1].Item != "Monitor" || rows[1].Quantity != 5 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestImportIsIdempotentPerFile(t *testing.T) {
	repo := &fakeRepo{keys: map[string]bool{}}
	im := NewImporter(repo, 8)
	rows, bad, _ := Parse([]byte("item,quantity\nA,1\nB,2\nC,3\n"))
	ctx := context.Background()

	first, err := im.Import(ctx, "user1", "co1", "user1:filehash", rows, bad)
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 3 || first.Duplicates != 0 || first.Total != 3 {
		t.Fatalf("first import = %+v", first)
	}
	// Same file, same batch id: every row is a duplicate now.
	second, _ := im.Import(ctx, "user1", "co1", "user1:filehash", rows, bad)
	if second.Created != 0 || second.Duplicates != 3 {
		t.Fatalf("re-import = %+v", second)
	}
	// Another user's identical file is independent.
	other, _ := im.Import(ctx, "user2", "co2", "user2:filehash", rows, bad)
	if other.Created != 3 {
		t.Fatalf("other user import = %+v", other)
	}
}

func TestImportReportsFailures(t *testing.T) {
	repo := &fakeRepo{keys: map[string]bool{}, fail: true}
	im := NewImporter(repo, 4)
	rows, bad, _ := Parse([]byte("item,quantity\nA,1\nB,2\n"))
	s, _ := im.Import(context.Background(), "u", "c", "b", rows, bad)
	if s.Failed != 2 || s.Created != 0 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestTooManyRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("item,quantity\n")
	for i := range MaxRows + 10 {
		fmt.Fprintf(&b, "item%d,1\n", i)
	}
	if _, _, err := Parse([]byte(b.String())); err != ErrTooManyRows {
		t.Fatalf("err = %v, want ErrTooManyRows", err)
	}
}
