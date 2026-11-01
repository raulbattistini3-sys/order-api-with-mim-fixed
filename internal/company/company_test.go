package company

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidCNPJ(t *testing.T) {
	tests := []struct {
		cnpj string
		want bool
	}{
		{"00000000000191", true},
		{"45334465000117", true},
		{"12ABC34501DE35", true}, // Receita's alphanumeric example
		{"00000000000192", false},
		{"12ABC34501DE36", false},
		{"00000000000000", false},
		{"123", false},
		{"0000000000019A", false}, // check digits are always numeric
	}
	for _, tt := range tests {
		if got := ValidCNPJ(tt.cnpj); got != tt.want {
			t.Errorf("ValidCNPJ(%q) = %v, want %v", tt.cnpj, got, tt.want)
		}
	}
}

func TestNormalizeCNPJ(t *testing.T) {
	if got := NormalizeCNPJ("12.abc.345/01de-35"); got != "12ABC34501DE35" {
		t.Fatalf("NormalizeCNPJ = %q", got)
	}
}

func TestBrasilAPILookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cnpj/v1/00000000000191" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"razao_social":"BANCO","opcao_pelo_mei":null,"email":null,"cnae_fiscal":6422100,
			"descricao_tipo_de_logradouro":"SAUS","logradouro":"QUADRA 1",
			"cnaes_secundarios":[{"codigo":0,"descricao":""},{"codigo":6499999,"descricao":"Outras"}]}`))
	}))
	defer srv.Close()
	dir := NewBrasilAPI(srv.URL)

	c, err := dir.Lookup(context.Background(), "00000000000191")
	if err != nil {
		t.Fatal(err)
	}
	if c.RazaoSocial != "BANCO" || c.Logradouro != "SAUS QUADRA 1" || c.CNAEPrincipal != "6422100" || c.IsMEI {
		t.Fatalf("unexpected company: %+v", c)
	}
	if len(c.CNAEsSecundarios) != 1 || c.CNAEsSecundarios[0].Code != "6499999" {
		t.Fatalf("secondary CNAEs = %+v", c.CNAEsSecundarios)
	}

	if _, err := dir.Lookup(context.Background(), "45334465000117"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown CNPJ: err = %v", err)
	}
}
