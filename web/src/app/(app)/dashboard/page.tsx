import { parsePhoneNumberFromString } from "libphonenumber-js";
import { redirect } from "next/navigation";
import { Card } from "@/components/ui";
import { formatCnpj } from "@/lib/cnpj";
import { BUYING_ROLES, JOB_FUNCTIONS, SENIORITIES } from "@/lib/roles";
import { getMe } from "@/lib/session";

export const metadata = { title: "Início · orderguard" };

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="text-sm">{value || "—"}</dd>
    </div>
  );
}

const money = new Intl.NumberFormat("pt-BR", { style: "currency", currency: "BRL" });

// 1966-08-01 → 01/08/1966
const date = (iso: string) => iso.split("-").reverse().join("/");

export default async function DashboardPage() {
  const me = await getMe();
  if (!me) redirect("/login");
  const { user, company } = me;
  const address = [
    [company.logradouro, company.numero].filter(Boolean).join(", "),
    company.complemento,
    company.bairro,
    [company.municipio, company.uf].filter(Boolean).join(" - "),
    company.cep,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Olá, {user.name}</h1>
      <div className="grid gap-6 md:grid-cols-[1fr_2fr]">
        <Card title="Seu perfil">
          <dl className="space-y-3">
            <Row label="E-mail" value={user.email} />
            <Row label="Permissão" value={user.permission} />
            <Row label="Telefone" value={parsePhoneNumberFromString(user.phone)?.formatInternational() ?? user.phone} />
            <Row label="Área de atuação" value={user.job_function ? JOB_FUNCTIONS[user.job_function] : ""} />
            <Row label="Cargo" value={user.job_title} />
            <Row label="Senioridade" value={user.seniority ? SENIORITIES[user.seniority] : ""} />
            <Row label="Papel na compra" value={user.buying_roles?.map((role) => BUYING_ROLES[role]).join(", ")} />
          </dl>
        </Card>
        <Card title={company.nome_fantasia || company.razao_social}>
          <dl className="grid gap-3 sm:grid-cols-2">
            <Row label="Razão social" value={company.razao_social} />
            <Row label="CNPJ" value={formatCnpj(company.cnpj)} />
            <Row label="Situação cadastral" value={company.situacao_cadastral} />
            <Row label="Abertura" value={date(company.data_abertura)} />
            <Row label="Natureza jurídica" value={company.natureza_juridica} />
            <Row label="Porte" value={company.porte} />
            <Row label="Capital social" value={money.format(company.capital_social)} />
            <Row label="MEI" value={company.is_mei ? "Sim" : "Não"} />
            <Row label="Simples Nacional" value={company.is_simples ? "Sim" : "Não"} />
            <Row label="Matriz ou filial" value={company.matriz_filial} />
            <div className="sm:col-span-2">
              <Row label="Atividade principal" value={[company.cnae_principal, company.cnae_principal_descricao].filter(Boolean).join(" · ")} />
            </div>
            <div className="sm:col-span-2">
              <Row label="Endereço" value={address} />
            </div>
          </dl>
          {!!company.cnaes_secundarios?.length && (
            <details className="mt-4 text-sm">
              <summary className="cursor-pointer text-muted">
                {company.cnaes_secundarios.length} atividade(s) secundária(s)
              </summary>
              <ul className="mt-2 space-y-1">
                {company.cnaes_secundarios.map((cnae) => (
                  <li key={cnae.code}>
                    {cnae.code} · {cnae.description}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </Card>
      </div>
    </div>
  );
}
