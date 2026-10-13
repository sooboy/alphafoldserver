// Resolve npm's JS entry point instead of passing file operations through cmd.exe.
import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
let command = 'npm',
  args = process.argv.slice(2)
if (process.platform === 'win32') {
  const js = [
    process.env.npm_execpath,
    join(dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js'),
  ].find((p) => p && existsSync(p))
  if (!js) throw Error('npm CLI not found beside Node; run npm directly from your shell')
  command = process.execPath
  args = [js, ...args]
}
const r = spawnSync(command, args, { stdio: 'inherit', windowsHide: true })
if (r.error) throw r.error
process.exit(r.status ?? 1)
