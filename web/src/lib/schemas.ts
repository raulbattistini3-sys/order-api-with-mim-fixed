import { isValidPhoneNumber, parsePhoneNumber } from "libphonenumber-js/max";
import { z } from "zod";
import { isValidCnpj, normalizeCnpj } from "./cnpj";
import { BUYING_ROLES, JOB_FUNCTIONS, SENIORITIES } from "./roles";

// The .meta() descriptions also feed z.toJSONSchema(), so these schemas can
// generate the API docs later.

const keys = <T extends object>(values: T) => Object.keys(values) as [keyof T & string, ...(keyof T & string)[]];

const email = z
  .email({ error: "Informe um e-mail válido." })
  .meta({ description: "E-mail da conta.", examples: ["ana@empresa.com.br"] });

const password = z
  .string()
  .min(10, { error: "A senha precisa ter pelo menos 10 caracteres." })
  .max(72, { error: "A senha pode ter no máximo 72 caracteres." })
  .meta({ description: "Senha, de 10 a 72 caracteres." });

export const phoneSchema = z
  .string()
  .trim()
  .refine((value) => value === "" || isValidPhoneNumber(value), { error: "Informe um telefone válido." })
  .transform((value) => (value ? parsePhoneNumber(value).number : ""))
  .meta({
    description: "Telefone com DDI. Aceita qualquer formatação e é salvo em E.164. Opcional.",
    examples: ["+55 11 98765-4321"],
  });

export const cnpjSchema = z
  .string()
  .transform(normalizeCnpj)
  .refine(isValidCnpj, { error: "Informe um CNPJ válido." })
  .meta({
    description: "CNPJ numérico ou alfanumérico, com ou sem pontuação. É salvo só com letras e números.",
    examples: ["45.334.465/0001-17"],
  });

export const loginSchema = z.object({
  email,
  password: z.string().min(1, { error: "Informe sua senha." }),
});

export const forgotSchema = z.object({ email });

export const resetSchema = z
  .object({ token: z.string().min(1), password, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { error: "As senhas não coincidem." });

export const registerSchema = z
  .object({
    name: z.string().trim().min(1, { error: "Informe seu nome." }).max(120).meta({ description: "Nome completo." }),
    email,
    password,
    confirm: z.string().meta({ description: "Repetição da senha." }),
    phone: phoneSchema,
    job_function: z
      .enum(keys(JOB_FUNCTIONS), { error: "Selecione uma área válida." })
      .or(z.literal(""))
      .meta({ description: "Área de atuação na empresa. Opcional." }),
    job_title: z.string().trim().max(120).meta({ description: "Cargo, em texto livre. Opcional." }),
    seniority: z
      .enum(keys(SENIORITIES), { error: "Selecione uma senioridade válida." })
      .or(z.literal(""))
      .meta({ description: "Senioridade. Opcional." }),
    buying_roles: z
      .array(z.enum(keys(BUYING_ROLES), { error: "Selecione papéis válidos." }))
      .meta({ description: "Papéis na decisão de compra. Aceita vários." }),
    cnpj: cnpjSchema,
    razao_social: z
      .string()
      .trim()
      .max(200)
      .meta({ description: "Usada apenas se a consulta de CNPJ estiver indisponível." }),
  })
  .refine((v) => v.password === v.confirm, { error: "As senhas não coincidem." });
