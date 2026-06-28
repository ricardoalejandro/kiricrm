#!/usr/bin/env node

const { spawnSync } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const repoRoot = path.resolve(__dirname, '..')
const apiDir = path.join(repoRoot, 'apps', 'api')
const envPath = path.join(repoRoot, '.env')

if (fs.existsSync(envPath)) {
  for (const line of fs.readFileSync(envPath, 'utf8').split(/\n/)) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#') || !line.includes('=')) continue

    const index = line.indexOf('=')
    const key = line.slice(0, index).trim()
    let value = line.slice(index + 1)

    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1)
    }

    if (key && process.env[key] === undefined) {
      process.env[key] = value
    }
  }
}

process.env.XDG_CACHE_HOME = process.env.XDG_CACHE_HOME || path.join(repoRoot, '.cache')

const prismaCli = path.join(apiDir, 'node_modules', 'prisma', 'build', 'index.js')
const result = spawnSync(process.execPath, [prismaCli, ...process.argv.slice(2)], {
  cwd: apiDir,
  env: process.env,
  stdio: 'inherit',
})

process.exit(result.status ?? 1)
