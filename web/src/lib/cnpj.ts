// Mirror of ValidCNPJ in internal/company/company.go. Accepts the alphanumeric
// format: a character's value is its ASCII code minus 48.

export function normalizeCnpj(value: string) {
  return value.replace(/[^0-9A-Za-z]/g, "").toUpperCase().slice(0, 14);
}

function checkDigit(value: string) {
  let sum = 0;
  let weight = 2;
  for (let i = value.length - 1; i >= 0; i--) {
    sum += (value.charCodeAt(i) - 48) * weight;
    weight = weight === 9 ? 2 : weight + 1;
  }
  const rest = sum % 11;
  return rest < 2 ? 0 : 11 - rest;
}

export function isValidCnpj(value: string) {
  if (!/^[0-9A-Z]{12}[0-9]{2}$/.test(value) || value === "0".repeat(14)) return false;
  return (
    checkDigit(value.slice(0, 12)) === Number(value[12]) &&
    checkDigit(value.slice(0, 13)) === Number(value[13])
  );
}

/** Formats as the user types: 00000000 → 00.000.000, and so on up to 00.000.000/0000-00. */
export function formatCnpj(value: string) {
  const v = normalizeCnpj(value);
  const parts = [v.slice(0, 2), v.slice(2, 5), v.slice(5, 8)].filter(Boolean).join(".");
  const branch = v.slice(8, 12);
  const digits = v.slice(12);
  return parts + (branch && `/${branch}`) + (digits && `-${digits}`);
}
