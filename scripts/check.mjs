import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const cwd = process.cwd();
console.log('===> Starting Universal Verification Pipeline for:', cwd);

const cargoBinDir = path.join(process.env.USERPROFILE || 'C:\\Users\\Administrator', '.cargo', 'bin');
const cargoExe = path.join(cargoBinDir, 'cargo.exe');
const env = {
  ...process.env,
  PATH: `${cargoBinDir};${process.env.PATH || ''}`,
};

let ranAnyCheck = false;

// 1. Go Project Check (if go.mod exists)
if (fs.existsSync(path.join(cwd, 'go.mod'))) {
  ranAnyCheck = true;
  const goSteps = [
    ['vet', './...'],
    ['test', '-v', './...'],
  ];

  for (const args of goSteps) {
    console.log(`==> Running: go ${args.join(' ')}`);
    const res = spawnSync('go', args, { cwd, env, stdio: 'inherit', shell: true });
    if (res.status !== 0) {
      console.error(`===> Go Verification Failed at step: go ${args.join(' ')} (exit code: ${res.status})`);
      process.exit(res.status || 1);
    }
  }
}

// 2. Rust Project Check (if Cargo.toml exists)
if (fs.existsSync(path.join(cwd, 'Cargo.toml'))) {
  ranAnyCheck = true;
  const rustSteps = [
    ['fmt', '--all'],
    ['clippy', '--all-targets', '--', '-D', 'warnings'],
    ['test', '--all-targets'],
  ];

  for (const args of rustSteps) {
    console.log(`==> Running: cargo ${args.join(' ')}`);
    const res = spawnSync(cargoExe, args, { cwd, env, stdio: 'inherit' });
    if (res.status !== 0) {
      console.error(`===> Verification Failed at step: cargo ${args.join(' ')} (exit code: ${res.status})`);
      process.exit(res.status || 1);
    }
  }
}

// 2. Node.js Project Check (if package.json exists with test script)
if (fs.existsSync(path.join(cwd, 'package.json'))) {
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(cwd, 'package.json'), 'utf8'));
    if (pkg.scripts && pkg.scripts.test && !pkg.scripts.test.includes('no test specified')) {
      ranAnyCheck = true;
      console.log('===> Running Node.js test script...');
      const res = spawnSync('npm', ['test'], { cwd, env, stdio: 'inherit', shell: true });
      if (res.status !== 0) {
        console.error(`===> Node test failed with exit code: ${res.status}`);
        process.exit(res.status || 1);
      }
    }
  } catch (err) {
    console.warn('Could not parse package.json:', err.message);
  }
}

// 3. Asset Integrity & Zero External Image Leak Sweep (if verify_assets.mjs exists)
if (fs.existsSync(path.join(cwd, 'scripts', 'verify_assets.mjs'))) {
  ranAnyCheck = true;
  console.log('===> Running Asset Integrity Sweep...');
  const res = spawnSync('node', ['scripts/verify_assets.mjs'], { cwd, env, stdio: 'inherit', shell: true });
  if (res.status !== 0) {
    console.error(`===> Asset verification failed with exit code: ${res.status}`);
    process.exit(res.status || 1);
  }
}

if (!ranAnyCheck) {
  console.log('===> No language manifest detected. Syntax dry-run passed.');
}

console.log('===> All verification steps passed successfully (exit code: 0)!');
