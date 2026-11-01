/// <reference path="../pb_data/types.d.ts" />

// Accounts: companies, users and to-dos. Every rule is null, including the users
// collection's, so a client can't reach PocketBase and set its own permission.
// Only the Go API writes here, authenticated as a superuser.
migrate((app) => {
  const companies = new Collection({
    type: "base",
    name: "companies",
    listRule: null,
    viewRule: null,
    createRule: null,
    updateRule: null,
    deleteRule: null,
    fields: [
      { name: "cnpj", type: "text", required: true, min: 14, max: 14 },
      { name: "razao_social", type: "text", required: true, max: 200 },
      { name: "nome_fantasia", type: "text", max: 200 },
      { name: "situacao_cadastral", type: "text", max: 60 },
      { name: "data_situacao_cadastral", type: "text", max: 10 },
      { name: "data_abertura", type: "text", max: 10 },
      { name: "natureza_juridica", type: "text", max: 120 },
      { name: "porte", type: "text", max: 60 },
      { name: "capital_social", type: "number" },
      { name: "is_mei", type: "bool" },
      { name: "is_simples", type: "bool" },
      { name: "matriz_filial", type: "text", max: 20 },
      { name: "cnae_principal", type: "text", max: 10 },
      { name: "cnae_principal_descricao", type: "text", max: 300 },
      { name: "cnaes_secundarios", type: "json", maxSize: 20000 },
      { name: "logradouro", type: "text", max: 200 },
      { name: "numero", type: "text", max: 30 },
      { name: "complemento", type: "text", max: 200 },
      { name: "bairro", type: "text", max: 120 },
      { name: "municipio", type: "text", max: 120 },
      { name: "uf", type: "text", max: 2 },
      { name: "cep", type: "text", max: 8 },
      { name: "telefone", type: "text", max: 30 },
      { name: "email", type: "text", max: 200 },
      // From the ERP's empresas table.
      { name: "dominio", type: "text", max: 200 },
      { name: "setor", type: "text", max: 120 },
      { name: "url", type: "text", max: 300 },
      { name: "created", type: "autodate", onCreate: true, onUpdate: false },
      { name: "updated", type: "autodate", onCreate: true, onUpdate: true },
    ],
    indexes: ["CREATE UNIQUE INDEX idx_companies_cnpj ON companies (cnpj)"],
  });
  app.save(companies);

  const users = app.findCollectionByNameOrId("users");
  users.listRule = null;
  users.viewRule = null;
  users.createRule = null;
  users.updateRule = null;
  users.deleteRule = null;
  users.authToken.duration = 8 * 60 * 60;
  users.resetPasswordTemplate.subject = "Redefina sua senha";
  users.resetPasswordTemplate.body =
    '<p>Recebemos um pedido para redefinir sua senha.</p>' +
    '<p><a href="{APP_URL}/reset-password?token={TOKEN}">Redefinir senha</a></p>' +
    "<p>Se não foi você, ignore este e-mail.</p>";
  // ERP fields. sessao_versao has no column here: PocketBase rotates tokenKey
  // when the password changes, which ends older sessions the same way.
  users.fields.add(new SelectField({ name: "permission", required: true, maxSelect: 1, values: ["ADM", "Estoque", "SU"] }));
  users.fields.add(new BoolField({ name: "active" }));
  users.fields.add(new RelationField({ name: "company", required: true, maxSelect: 1, collectionId: companies.id }));
  // HubSpot contact fields.
  users.fields.add(new TextField({ name: "phone", max: 30 }));
  users.fields.add(new TextField({ name: "job_title", max: 120 }));
  users.fields.add(new TextField({ name: "seniority", max: 40 }));
  users.fields.add(new BoolField({ name: "is_decision_maker" }));
  users.fields.add(new BoolField({ name: "is_stakeholder" }));
  app.save(users);

  const todos = new Collection({
    type: "base",
    name: "todos",
    listRule: null,
    viewRule: null,
    createRule: null,
    updateRule: null,
    deleteRule: null,
    fields: [
      { name: "user", type: "relation", required: true, maxSelect: 1, collectionId: users.id, cascadeDelete: true },
      { name: "title", type: "text", required: true, max: 200 },
      { name: "done", type: "bool" },
      { name: "created", type: "autodate", onCreate: true, onUpdate: false },
    ],
    indexes: ["CREATE INDEX idx_todos_user ON todos (user)"],
  });
  app.save(todos);

  const orders = app.findCollectionByNameOrId("orders");
  orders.fields.add(new TextField({ name: "company", max: 15 })); // empty for the demo's orders
  app.save(orders);

  const settings = app.settings();
  settings.meta.appURL = $os.getenv("APP_URL") || "http://localhost:3000";
  app.save(settings);
}, (app) => {
  const orders = app.findCollectionByNameOrId("orders");
  orders.fields.removeByName("company");
  app.save(orders);

  app.delete(app.findCollectionByNameOrId("todos"));

  const users = app.findCollectionByNameOrId("users");
  for (const name of ["permission", "active", "company", "phone", "job_title", "seniority", "is_decision_maker", "is_stakeholder"]) {
    users.fields.removeByName(name);
  }
  app.save(users);

  app.delete(app.findCollectionByNameOrId("companies"));
});
