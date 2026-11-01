import type { BuyingRole, JobFunction, Seniority } from "./roles";

export type User = {
  id: string;
  email: string;
  name: string;
  permission: "ADM" | "Estoque" | "SU";
  active: boolean;
  company: string;
  phone: string;
  job_function: JobFunction | "";
  job_title: string;
  seniority: Seniority | "";
  buying_roles: BuyingRole[] | null;
};

export type Company = {
  id: string;
  cnpj: string;
  razao_social: string;
  nome_fantasia: string;
  situacao_cadastral: string;
  data_situacao_cadastral: string;
  data_abertura: string;
  natureza_juridica: string;
  porte: string;
  capital_social: number;
  is_mei: boolean;
  is_simples: boolean;
  matriz_filial: string;
  cnae_principal: string;
  cnae_principal_descricao: string;
  cnaes_secundarios: { code: string; description: string }[] | null;
  logradouro: string;
  numero: string;
  complemento: string;
  bairro: string;
  municipio: string;
  uf: string;
  cep: string;
  telefone: string;
  email: string;
};

export type Me = { user: User; company: Company };

export type Session = { token: string; user: User };

export type Order = {
  id: string;
  item: string;
  quantity: number;
  created: string;
};

export type OrderPage = { orders: Order[]; next_cursor: string };

export type OrderSummary = {
  orders: number;
  units: number;
  items: number;
  top_items: { item: string; units: number; orders: number }[];
};

export type Todo = {
  id: string;
  title: string;
  done: boolean;
  created?: string;
};
