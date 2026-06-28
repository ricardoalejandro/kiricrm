import { z } from 'zod'

const booleanFromEnv = z.preprocess((value) => {
  if (value === undefined || value === '') return undefined
  if (typeof value === 'boolean') return value
  if (typeof value !== 'string') return value
  return ['1', 'true', 'yes', 'on'].includes(value.toLowerCase())
}, z.boolean())

const intFromEnv = z.preprocess((value) => {
  if (value === undefined || value === '') return undefined
  if (typeof value === 'number') return value
  return Number(value)
}, z.number().int().positive())

const numberFromEnv = z.preprocess((value) => {
  if (value === undefined || value === '') return undefined
  if (typeof value === 'number') return value
  return Number(value)
}, z.number().positive())

const runtimeEnvSchema = z.object({
  NODE_ENV: z.string().default('development'),
  PUBLIC_URL: z.string().url().default('http://localhost:8080'),
  DATABASE_URL: z.string().min(1),
  META_GRAPH_API_VERSION: z.string().min(1).default('v25.0'),
  WA_WEBHOOK_VERIFY_TOKEN: z.string().min(16).optional(),
  WA_WEBHOOK_APP_SECRET: z.string().min(16).optional(),
  WA_TOKEN_ENCRYPTION_KEY: z.string().optional(),
  WA_SENDING_ENABLED: booleanFromEnv.default(false),
  WA_TEMPLATES_ENABLED: booleanFromEnv.default(false),
  WA_DEFAULT_DAILY_LIMIT: intFromEnv.default(100),
  WA_DEFAULT_RATE_LIMIT_PER_SECOND: numberFromEnv.default(1),
  WA_DEV_ACCOUNT_SLUG: z.string().min(1).default('dev'),
  WA_DEV_ACCOUNT_NAME: z.string().min(1).default('Kiri CRM Dev'),
})

export type RuntimeEnv = z.infer<typeof runtimeEnvSchema>

export function loadRuntimeEnv(): RuntimeEnv {
  const parsed = runtimeEnvSchema.safeParse(process.env)

  if (!parsed.success) {
    const fields = parsed.error.issues.map((issue) => issue.path.join('.') || 'env').join(', ')
    throw new Error(`Invalid runtime environment: ${fields}`)
  }

  return parsed.data
}
