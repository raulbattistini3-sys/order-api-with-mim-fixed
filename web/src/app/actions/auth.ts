"use server";

import { redirect } from "next/navigation";
import { gateway } from "@/lib/gateway";
import { isValidCnpj, normalizeCnpj } from "@/lib/cnpj";
import { forgotSchema, loginSchema, registerSchema, resetSchema } from "@/lib/schemas";
import { clearSession, setSession } from "@/lib/session";
import type { Company, Session } from "@/lib/types";

export type FormState = {
  error?: string;
  success?: boolean;
  values?: Record<string, string | string[]>;
};

const UNAVAILABLE = "Serviço indisponível. Tente novamente em instantes.";

function text(form: FormData, name: string) {
  const value = form.get(name);
  return typeof value === "string" ? value : "";
}

// next comes from the URL, so only same-site paths are accepted.
function safePath(path: string) {
  return path.startsWith("/") && !path.startsWith("//") && !path.includes("\\") ? path : "/dashboard";
}

export async function login(_: FormState, form: FormData): Promise<FormState> {
  const email = text(form, "email");
  const parsed = loginSchema.safeParse({ email, password: text(form, "password") });
  if (!parsed.success) return { error: parsed.error.issues[0].message, values: { email } };

  const res = await gateway<Session>("POST", "/auth/login", { body: parsed.data });
  if (res.status === 401) return { error: "E-mail ou senha incorretos.", values: { email } };
  if (res.status !== 200) return { error: UNAVAILABLE, values: { email } };

  await setSession(res.data.token);
  redirect(safePath(text(form, "next")));
}

export async function register(_: FormState, form: FormData): Promise<FormState> {
  const values = {
    name: text(form, "name"),
    email: text(form, "email"),
    phone: text(form, "phone"),
    job_function: text(form, "job_function"),
    job_title: text(form, "job_title"),
    seniority: text(form, "seniority"),
    buying_roles: form.getAll("buying_roles").map(String),
    cnpj: text(form, "cnpj"),
    razao_social: text(form, "razao_social"),
  };
  const parsed = registerSchema.safeParse({
    ...values,
    password: text(form, "password"),
    confirm: text(form, "confirm"),
  });
  if (!parsed.success) return { error: parsed.error.issues[0].message, values };

  const res = await gateway<Session & { error?: string }>("POST", "/auth/register", {
    body: { ...parsed.data, confirm: undefined },
  });
  if (res.status === 409) {
    const taken = res.data.error?.includes("CNPJ") ? "Este CNPJ já está cadastrado." : "Este e-mail já está cadastrado.";
    return { error: taken, values };
  }
  if (res.status === 422) return { error: "CNPJ não encontrado na Receita Federal.", values };
  if (res.status === 502) return { error: "Consulta de CNPJ indisponível. Informe a razão social.", values };
  if (res.status !== 201) return { error: UNAVAILABLE, values };

  await setSession(res.data.token);
  redirect("/dashboard");
}

export async function forgotPassword(_: FormState, form: FormData): Promise<FormState> {
  const email = text(form, "email");
  const parsed = forgotSchema.safeParse({ email });
  if (!parsed.success) return { error: parsed.error.issues[0].message, values: { email } };

  const res = await gateway("POST", "/auth/forgot-password", { body: parsed.data });
  if (res.status !== 204) return { error: UNAVAILABLE, values: { email } };
  return { success: true };
}

export async function resetPassword(_: FormState, form: FormData): Promise<FormState> {
  const parsed = resetSchema.safeParse({
    token: text(form, "token"),
    password: text(form, "password"),
    confirm: text(form, "confirm"),
  });
  if (!parsed.success) return { error: parsed.error.issues[0].message };

  const { token, password } = parsed.data;
  const res = await gateway("POST", "/auth/reset-password", { body: { token, password } });
  if (res.status === 400) return { error: "Este link é inválido ou expirou. Peça um novo." };
  if (res.status !== 204) return { error: UNAVAILABLE };

  await clearSession();
  redirect("/login?reset=1");
}

export async function logout() {
  await clearSession();
  redirect("/login");
}

export async function lookupCnpj(value: string): Promise<Pick<Company, "razao_social" | "nome_fantasia"> | null> {
  const cnpj = normalizeCnpj(value);
  if (!isValidCnpj(cnpj)) return null;
  const res = await gateway<Company>("GET", `/auth/cnpj/${cnpj}`);
  if (res.status !== 200) return null;
  return { razao_social: res.data.razao_social, nome_fantasia: res.data.nome_fantasia };
}
