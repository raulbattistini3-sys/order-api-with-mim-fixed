import { describe, expect, it } from "vitest";
import { z } from "zod";
import { formatCnpj, isValidCnpj, normalizeCnpj } from "./cnpj";
import { cnpjSchema, phoneSchema, registerSchema } from "./schemas";

describe("formatCnpj", () => {
  it.each([
    ["4", "4"],
    ["453", "45.3"],
    ["45334465", "45.334.465"],
    ["453344650001", "45.334.465/0001"],
    ["45334465000117", "45.334.465/0001-17"],
    ["45.334.465/0001-17", "45.334.465/0001-17"],
    ["45 334 465 0001 17 999", "45.334.465/0001-17"],
    ["abc!@#", "AB.C"],
    ["12abc34501de35", "12.ABC.345/01DE-35"],
  ])("%s → %s", (input, expected) => {
    expect(formatCnpj(input)).toBe(expected);
  });
});

// Same cases as TestValidCNPJ in internal/company/company_test.go.
describe("isValidCnpj", () => {
  it.each([
    ["00000000000191", true],
    ["45334465000117", true],
    ["12ABC34501DE35", true],
    ["00000000000192", false],
    ["12ABC34501DE36", false],
    ["00000000000000", false],
    ["123", false],
    ["0000000000019A", false],
  ])("%s → %s", (cnpj, expected) => {
    expect(isValidCnpj(cnpj)).toBe(expected);
  });
});

describe("cnpjSchema", () => {
  it("saves only letters and digits", () => {
    expect(cnpjSchema.parse("45.334.465/0001-17")).toBe(normalizeCnpj("45334465000117"));
  });
});

describe("phoneSchema", () => {
  it.each([
    ["+55 11 98765-4321", "+5511987654321"],
    ["+5511987654321", "+5511987654321"],
    ["+55 (21) 3333-4444", "+552133334444"],
    ["+1 415 555 2671", "+14155552671"],
    ["", ""],
  ])("%s → %s", (input, expected) => {
    expect(phoneSchema.parse(input)).toBe(expected);
  });

  it.each(["+55 11 1234", "+55 00 98765-4321", "11987654321", "abc"])("rejects %s", (input) => {
    expect(phoneSchema.safeParse(input).success).toBe(false);
  });
});

describe("registerSchema", () => {
  const valid = {
    name: "Ana",
    email: "ana@example.com",
    password: "correct-horse-battery",
    confirm: "correct-horse-battery",
    phone: "",
    job_function: "",
    job_title: "",
    seniority: "",
    buying_roles: [],
    cnpj: "00000000000191",
    razao_social: "",
  };

  it("accepts every optional field empty", () => {
    expect(registerSchema.safeParse(valid).success).toBe(true);
  });

  it("accepts several buying roles and a job function", () => {
    const result = registerSchema.safeParse({
      ...valid,
      job_function: "data_analytics",
      buying_roles: ["DECISION_MAKER", "END_USER"],
    });
    expect(result.success).toBe(true);
  });

  it("rejects unknown values", () => {
    expect(registerSchema.safeParse({ ...valid, job_function: "astronaut" }).success).toBe(false);
    expect(registerSchema.safeParse({ ...valid, buying_roles: ["KING"] }).success).toBe(false);
  });

  it("can produce JSON Schema for the docs", () => {
    const schema = z.toJSONSchema(registerSchema, { io: "input", unrepresentable: "any" }) as {
      properties: Record<string, { description?: string }>;
    };
    expect(schema.properties.phone.description).toContain("E.164");
    expect(schema.properties.cnpj.description).toContain("CNPJ");
  });
});
