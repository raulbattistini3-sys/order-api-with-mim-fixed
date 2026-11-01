// Package company is the domain of the companies users belong to. Registration
// data comes from the public CNPJ registry, not from what a client claims.
package company

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrDuplicate means a company with this CNPJ already exists.
	ErrDuplicate = errors.New("company: cnpj already registered")
	// ErrNotFound means the CNPJ is unknown.
	ErrNotFound = errors.New("company: not found")
)

// CNAE is one economic activity.
type CNAE struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// Company is one company record.
type Company struct {
	ID                     string  `json:"id,omitempty"`
	CNPJ                   string  `json:"cnpj"`
	RazaoSocial            string  `json:"razao_social"`
	NomeFantasia           string  `json:"nome_fantasia"`
	SituacaoCadastral      string  `json:"situacao_cadastral"`
	DataSituacaoCadastral  string  `json:"data_situacao_cadastral"`
	DataAbertura           string  `json:"data_abertura"`
	NaturezaJuridica       string  `json:"natureza_juridica"`
	Porte                  string  `json:"porte"`
	CapitalSocial          float64 `json:"capital_social"`
	IsMEI                  bool    `json:"is_mei"`
	IsSimples              bool    `json:"is_simples"`
	MatrizFilial           string  `json:"matriz_filial"`
	CNAEPrincipal          string  `json:"cnae_principal"`
	CNAEPrincipalDescricao string  `json:"cnae_principal_descricao"`
	CNAEsSecundarios       []CNAE  `json:"cnaes_secundarios"`
	Logradouro             string  `json:"logradouro"`
	Numero                 string  `json:"numero"`
	Complemento            string  `json:"complemento"`
	Bairro                 string  `json:"bairro"`
	Municipio              string  `json:"municipio"`
	UF                     string  `json:"uf"`
	CEP                    string  `json:"cep"`
	Telefone               string  `json:"telefone"`
	Email                  string  `json:"email"`
	Dominio                string  `json:"dominio"`
	Setor                  string  `json:"setor"`
	URL                    string  `json:"url"`
}

// Repository stores companies. Create returns ErrDuplicate for a taken CNPJ.
type Repository interface {
	Create(ctx context.Context, c Company) (Company, error)
	Get(ctx context.Context, id string) (Company, error)
	Delete(ctx context.Context, id string) error
}

// Directory looks up a CNPJ in the public registry. It returns ErrNotFound for
// an unknown CNPJ.
type Directory interface {
	Lookup(ctx context.Context, cnpj string) (Company, error)
}

var (
	cnpjSeparators = strings.NewReplacer(".", "", "/", "", "-", "", " ", "")
	cnpjPattern    = regexp.MustCompile(`^[0-9A-Z]{12}[0-9]{2}$`)
)

// NormalizeCNPJ strips punctuation and uppercases s.
func NormalizeCNPJ(s string) string {
	return strings.ToUpper(cnpjSeparators.Replace(s))
}

// ValidCNPJ checks the format and check digits of a normalized CNPJ. It accepts
// the alphanumeric format as well as the numeric one: a character's value is
// its ASCII code minus 48, which leaves digits unchanged.
func ValidCNPJ(s string) bool {
	if !cnpjPattern.MatchString(s) || s == strings.Repeat("0", 14) {
		return false
	}
	return checkDigit(s[:12]) == int(s[12]-'0') && checkDigit(s[:13]) == int(s[13]-'0')
}

func checkDigit(s string) int {
	sum, weight := 0, 2
	for i := len(s) - 1; i >= 0; i-- {
		sum += int(s[i]-'0') * weight
		if weight++; weight > 9 {
			weight = 2
		}
	}
	if rest := sum % 11; rest >= 2 {
		return 11 - rest
	}
	return 0
}
