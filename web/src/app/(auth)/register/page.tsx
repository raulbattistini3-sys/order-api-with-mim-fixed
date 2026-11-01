"use client";

import Link from "next/link";
import { useActionState, useRef, useState } from "react";
import { lookupCnpj, register, type FormState } from "@/app/actions/auth";
import { PhoneField } from "@/components/phone-field";
import { Alert, Button, Card, Checkbox, Field, SelectField } from "@/components/ui";
import { formatCnpj, isValidCnpj, normalizeCnpj } from "@/lib/cnpj";
import { BUYING_ROLES, JOB_FUNCTIONS, SENIORITIES } from "@/lib/roles";

const CNPJ_HINTS = {
  idle: "Digite só os números. A pontuação entra sozinha.",
  loading: "Consultando a Receita Federal…",
  found: "Dados da empresa encontrados.",
  missing: "Não conseguimos consultar. Informe a razão social.",
  invalid: "Este CNPJ não é válido. Confira os números.",
};

export default function RegisterPage() {
  const [state, action, pending] = useActionState(register, {} as FormState);
  const [cnpj, setCnpj] = useState("");
  const [razaoSocial, setRazaoSocial] = useState("");
  const [lookup, setLookup] = useState<keyof typeof CNPJ_HINTS>("idle");
  const latest = useRef("");
  const value = (name: string) => String(state.values?.[name] ?? "");

  async function onCnpjChange(input: string) {
    const normalized = normalizeCnpj(input);
    setCnpj(formatCnpj(normalized));
    if (normalized === latest.current) return;
    latest.current = normalized;
    setRazaoSocial("");

    if (normalized.length < 14) return setLookup("idle");
    if (!isValidCnpj(normalized)) return setLookup("invalid");

    setLookup("loading");
    const found = await lookupCnpj(normalized);
    if (latest.current !== normalized) return; // the user kept typing
    setRazaoSocial(found?.razao_social ?? "");
    setLookup(found ? "found" : "missing");
  }

  return (
    <Card>
      <h1 className="mb-1 text-xl font-semibold">Criar conta</h1>
      <p className="mb-5 text-sm text-muted">Cadastre sua empresa. Quem cadastra se torna o administrador.</p>
      <form action={action} className="space-y-4">
        <Field
          label="CNPJ"
          name="cnpj"
          required
          autoCapitalize="characters"
          value={cnpj}
          onChange={(e) => onCnpjChange(e.target.value)}
          placeholder="00.000.000/0000-00"
          hint={CNPJ_HINTS[lookup]}
        />
        <Field
          label="Razão social"
          name="razao_social"
          value={razaoSocial}
          onChange={(e) => setRazaoSocial(e.target.value)}
          readOnly={lookup === "found"}
        />
        <Field label="Nome" name="name" autoComplete="name" required defaultValue={value("name")} />
        <Field label="E-mail" name="email" type="email" autoComplete="email" required defaultValue={value("email")} />
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Senha" name="password" type="password" autoComplete="new-password" required minLength={10} maxLength={72} hint="Pelo menos 10 caracteres." />
          <Field label="Confirmar senha" name="confirm" type="password" autoComplete="new-password" required />
        </div>
        <PhoneField label="Telefone" name="phone" />
        <div className="grid gap-4 sm:grid-cols-2">
          <SelectField label="Área de atuação" name="job_function" key={`function-${value("job_function")}`} defaultValue={value("job_function")}>
            <option value="">Selecione</option>
            {Object.entries(JOB_FUNCTIONS).map(([id, label]) => (
              <option key={id} value={id}>
                {label}
              </option>
            ))}
          </SelectField>
          <SelectField label="Senioridade" name="seniority" key={`seniority-${value("seniority")}`} defaultValue={value("seniority")}>
            <option value="">Selecione</option>
            {Object.entries(SENIORITIES).map(([id, label]) => (
              <option key={id} value={id}>
                {label}
              </option>
            ))}
          </SelectField>
        </div>
        <Field label="Cargo" name="job_title" autoComplete="organization-title" placeholder="Ex.: Analista de dados pleno" defaultValue={value("job_title")} />
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-ink">Papel na decisão de compra</legend>
          <p className="text-xs text-muted">Marque todos que se aplicam.</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {Object.entries(BUYING_ROLES).map(([id, label]) => (
              <Checkbox
                key={id}
                label={label}
                name="buying_roles"
                value={id}
                defaultChecked={state.values?.buying_roles?.includes(id)}
              />
            ))}
          </div>
        </fieldset>
        {state.error && <Alert>{state.error}</Alert>}
        <Button type="submit" disabled={pending} className="w-full">
          {pending ? "Criando…" : "Criar conta"}
        </Button>
      </form>
      <p className="mt-5 text-center text-sm text-muted">
        Já tem conta?{" "}
        <Link href="/login" className="font-medium text-accent">
          Entrar
        </Link>
      </p>
    </Card>
  );
}
