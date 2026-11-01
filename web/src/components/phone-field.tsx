"use client";

import { useId, useState } from "react";
import PhoneInput from "react-phone-number-input";
import flags from "react-phone-number-input/flags";
import labels from "react-phone-number-input/locale/pt-BR.json";
import "react-phone-number-input/style.css";

// With +55 locked, pasting 5511987654321 arrives as +555511987654321. A Brazilian
// number has at most 13 digits, so a longer one has the prefix twice. Checking
// the length keeps real numbers in DDD 55 intact.
export function dropRepeatedPrefix(value: string | undefined) {
  return value?.startsWith("+5555") && value.length > 14 ? `+${value.slice(3)}` : value;
}

/**
 * Phone input with Brazil preselected and its +55 locked. Passing `flags` keeps
 * the flags in the bundle, instead of loading them from a third-party site.
 * Submits E.164 through a hidden input.
 */
export function PhoneField({ label, name }: { label: string; name: string }) {
  const id = useId();
  const [value, setValue] = useState<string | undefined>();
  const [corrections, setCorrections] = useState(0);

  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="block text-sm font-medium text-ink">
        {label}
      </label>
      <PhoneInput
        key={corrections}
        id={id}
        international
        countryCallingCodeEditable={false}
        defaultCountry="BR"
        flags={flags}
        labels={labels}
        value={value}
        onChange={(next) => {
          const fixed = dropRepeatedPrefix(next);
          // The corrected value can equal the old one, which wouldn't re-render.
          if (fixed !== next) setCorrections((count) => count + 1);
          setValue(fixed);
        }}
        className="rounded-lg border border-border bg-surface px-3 py-2 text-sm transition focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/25"
        numberInputProps={{
          className: "w-full bg-transparent text-ink outline-none",
          autoComplete: "tel",
          autoFocus: corrections > 0,
        }}
      />
      <input type="hidden" name={name} value={value ?? ""} />
    </div>
  );
}
