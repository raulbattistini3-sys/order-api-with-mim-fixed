/// <reference path="../pb_data/types.d.ts" />

// Replaces the two role flags with a list of buying roles, like the ERP's papeis
// (HubSpot's hs_buying_role), and adds the area the user works in.
// The values must match internal/auth/roles.go and web/src/lib/roles.ts.
const BUYING_ROLES = [
  "DECISION_MAKER",
  "STAKEHOLDER",
  "BUDGET_HOLDER",
  "INFLUENCER",
  "CHAMPION",
  "EXECUTIVE_SPONSOR",
  "END_USER",
  "LEGAL_AND_COMPLIANCE",
];
const JOB_FUNCTIONS = [
  "executive",
  "sales",
  "marketing",
  "customer_success",
  "product",
  "design",
  "software_engineering",
  "data_analytics",
  "it_security",
  "finance",
  "human_resources",
  "operations",
  "procurement",
  "legal",
  "other",
];

migrate((app) => {
  const users = app.findCollectionByNameOrId("users");
  users.fields.add(new SelectField({ name: "buying_roles", maxSelect: BUYING_ROLES.length, values: BUYING_ROLES }));
  users.fields.add(new SelectField({ name: "job_function", maxSelect: 1, values: JOB_FUNCTIONS }));
  app.save(users);

  for (const user of app.findAllRecords("users")) {
    const roles = [];
    if (user.getBool("is_decision_maker")) roles.push("DECISION_MAKER");
    if (user.getBool("is_stakeholder")) roles.push("STAKEHOLDER");
    user.set("buying_roles", roles);
    app.saveNoValidate(user);
  }

  users.fields.removeByName("is_decision_maker");
  users.fields.removeByName("is_stakeholder");
  app.save(users);
}, (app) => {
  const users = app.findCollectionByNameOrId("users");
  users.fields.add(new BoolField({ name: "is_decision_maker" }));
  users.fields.add(new BoolField({ name: "is_stakeholder" }));
  app.save(users);

  for (const user of app.findAllRecords("users")) {
    const roles = user.getStringSlice("buying_roles");
    user.set("is_decision_maker", roles.includes("DECISION_MAKER"));
    user.set("is_stakeholder", roles.includes("STAKEHOLDER"));
    app.saveNoValidate(user);
  }

  users.fields.removeByName("buying_roles");
  users.fields.removeByName("job_function");
  app.save(users);
});
