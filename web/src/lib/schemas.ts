import { z } from "zod";

// The .meta() descriptions also feed z.toJSONSchema(), so these schemas can
// generate the API docs later.

const email = z
  .email({ error: "Informe um e-mail válido." })
  .meta({ description: "E-mail da conta.", examples: ["ana@empresa.com.br"] });

const password = z
  .string()
  .min(10, { error: "A senha precisa ter pelo menos 10 caracteres." })
  .max(72, { error: "A senha pode ter no máximo 72 caracteres." })
  .meta({ description: "Senha, de 10 a 72 caracteres." });

export const loginSchema = z.object({
  email,
  password: z.string().min(1, { error: "Informe sua senha." }),
});

export const forgotSchema = z.object({ email });

export const resetSchema = z
  .object({ token: z.string().min(1), password, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { error: "As senhas não coincidem." });
