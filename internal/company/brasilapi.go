package company

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BrasilAPI implements Directory using brasilapi.com.br, a free mirror of the
// Receita Federal's public data.
type BrasilAPI struct {
	baseURL string
	http    *http.Client
}

// NewBrasilAPI returns a BrasilAPI that calls baseURL.
func NewBrasilAPI(baseURL string) *BrasilAPI {
	return &BrasilAPI{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 5 * time.Second}}
}

type brasilAPIResponse struct {
	CNPJ                  string  `json:"cnpj"`
	RazaoSocial           string  `json:"razao_social"`
	NomeFantasia          string  `json:"nome_fantasia"`
	SituacaoCadastral     string  `json:"descricao_situacao_cadastral"`
	DataSituacaoCadastral string  `json:"data_situacao_cadastral"`
	DataAbertura          string  `json:"data_inicio_atividade"`
	NaturezaJuridica      string  `json:"natureza_juridica"`
	Porte                 string  `json:"porte"`
	CapitalSocial         float64 `json:"capital_social"`
	OpcaoMEI              *bool   `json:"opcao_pelo_mei"`
	OpcaoSimples          *bool   `json:"opcao_pelo_simples"`
	MatrizFilial          string  `json:"descricao_identificador_matriz_filial"`
	CNAE                  int     `json:"cnae_fiscal"`
	CNAEDescricao         string  `json:"cnae_fiscal_descricao"`
	CNAEsSecundarios      []struct {
		Codigo    int    `json:"codigo"`
		Descricao string `json:"descricao"`
	} `json:"cnaes_secundarios"`
	TipoLogradouro string  `json:"descricao_tipo_de_logradouro"`
	Logradouro     string  `json:"logradouro"`
	Numero         string  `json:"numero"`
	Complemento    string  `json:"complemento"`
	Bairro         string  `json:"bairro"`
	Municipio      string  `json:"municipio"`
	UF             string  `json:"uf"`
	CEP            string  `json:"cep"`
	Telefone       string  `json:"ddd_telefone_1"`
	Email          *string `json:"email"`
}

// Lookup implements Directory.
func (b *BrasilAPI) Lookup(ctx context.Context, cnpj string) (Company, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/cnpj/v1/"+url.PathEscape(cnpj), nil)
	if err != nil {
		return Company{}, err
	}
	res, err := b.http.Do(req)
	if err != nil {
		return Company{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return Company{}, ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return Company{}, fmt.Errorf("brasilapi: status %d", res.StatusCode)
	}
	var r brasilAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return Company{}, fmt.Errorf("brasilapi: %w", err)
	}

	c := Company{
		CNPJ:                   cnpj,
		RazaoSocial:            r.RazaoSocial,
		NomeFantasia:           r.NomeFantasia,
		SituacaoCadastral:      r.SituacaoCadastral,
		DataSituacaoCadastral:  r.DataSituacaoCadastral,
		DataAbertura:           r.DataAbertura,
		NaturezaJuridica:       r.NaturezaJuridica,
		Porte:                  r.Porte,
		CapitalSocial:          r.CapitalSocial,
		IsMEI:                  r.OpcaoMEI != nil && *r.OpcaoMEI,
		IsSimples:              r.OpcaoSimples != nil && *r.OpcaoSimples,
		MatrizFilial:           r.MatrizFilial,
		CNAEPrincipal:          strconv.Itoa(r.CNAE),
		CNAEPrincipalDescricao: r.CNAEDescricao,
		CNAEsSecundarios:       []CNAE{},
		Logradouro:             strings.TrimSpace(r.TipoLogradouro + " " + r.Logradouro),
		Numero:                 r.Numero,
		Complemento:            r.Complemento,
		Bairro:                 r.Bairro,
		Municipio:              r.Municipio,
		UF:                     r.UF,
		CEP:                    r.CEP,
		Telefone:               r.Telefone,
	}
	if r.Email != nil {
		c.Email = *r.Email
	}
	for _, s := range r.CNAEsSecundarios {
		if s.Codigo != 0 {
			c.CNAEsSecundarios = append(c.CNAEsSecundarios, CNAE{Code: strconv.Itoa(s.Codigo), Description: s.Descricao})
		}
	}
	return c, nil
}
