const agent = process.env.npm_config_user_agent || ''

if (!agent.startsWith('pnpm/')) {
  console.error('Kiri CRM usa pnpm obligatoriamente. No uses npm install ni yarn install.')
  console.error('Comando correcto: corepack enable && corepack prepare pnpm@10.34.3 --activate && pnpm install')
  process.exit(1)
}
