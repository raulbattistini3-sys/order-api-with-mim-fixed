// The keys must match internal/auth/roles.go and the user_roles migration.

export const JOB_FUNCTIONS = {
  executive: "Diretoria / C-level",
  sales: "Vendas",
  marketing: "Marketing",
  customer_success: "Sucesso do cliente",
  product: "Produto",
  design: "Design",
  software_engineering: "Engenharia de software",
  data_analytics: "Análise de dados",
  it_security: "TI e segurança",
  finance: "Financeiro",
  human_resources: "Recursos humanos",
  operations: "Operações e logística",
  procurement: "Compras",
  legal: "Jurídico",
  other: "Outra",
} as const;

export const BUYING_ROLES = {
  DECISION_MAKER: "Decisor(a)",
  STAKEHOLDER: "Stakeholder",
  BUDGET_HOLDER: "Responsável pelo orçamento",
  INFLUENCER: "Influenciador(a)",
  CHAMPION: "Defensor(a) da solução",
  EXECUTIVE_SPONSOR: "Patrocinador(a) executivo(a)",
  END_USER: "Usuário(a) final",
  LEGAL_AND_COMPLIANCE: "Jurídico e compliance",
} as const;

export const SENIORITIES = {
  junior: "Júnior",
  pleno: "Pleno",
  senior: "Sênior",
  gerente: "Gerente",
  diretor: "Diretor(a)",
  executivo: "Executivo(a)",
} as const;

export type JobFunction = keyof typeof JOB_FUNCTIONS;
export type BuyingRole = keyof typeof BUYING_ROLES;
export type Seniority = keyof typeof SENIORITIES;
