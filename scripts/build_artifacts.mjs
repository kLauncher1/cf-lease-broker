#!/usr/bin/env node

import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

const REPO_ROOT = process.cwd();
const pkgJsonPath = path.join(REPO_ROOT, 'package.json');

// Read version from package.json or environment
let version = '0.6.0';
if (fs.existsSync(pkgJsonPath)) {
  try {
    const pkg = JSON.parse(fs.readFileSync(pkgJsonPath, 'utf8'));
    if (pkg.version) version = pkg.version;
  } catch {}
}

// Parse command line arguments
const args = process.argv.slice(2);
let targetFilter = null;
let outDir = path.join(REPO_ROOT, 'dist');
let shouldClean = false;
let verifyOnly = false;
let skipChecksums = false;

for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === '--help' || arg === '-h') {
    console.log(`
CF-Lease-Broker Standalone Cross-Compilation & Artifacts Pipeline

Usage:
  node scripts/build_artifacts.mjs [options]

Options:
  --target=<os/arch>     Build only specified target (e.g., linux/amd64)
  --out-dir=<path>       Output directory for compiled binaries (default: dist)
  --clean                Remove output directory before building
  --verify               Verify SHA-256 integrity of existing artifacts in out-dir
  --skip-checksums       Skip checksum manifest generation
  --version=<ver>        Override build version string
  --help, -h             Show this help message
`);
    process.exit(0);
  } else if (arg.startsWith('--target=')) {
    targetFilter = arg.split('=')[1].trim();
  } else if (arg.startsWith('--out-dir=')) {
    outDir = path.resolve(REPO_ROOT, arg.split('=')[1].trim());
  } else if (arg.startsWith('--version=')) {
    version = arg.split('=')[1].trim();
  } else if (arg === '--clean') {
    shouldClean = true;
  } else if (arg === '--verify') {
    verifyOnly = true;
  } else if (arg === '--skip-checksums') {
    skipChecksums = true;
  }
}

// Define the 4 target platforms mandated by Milestone 6
const ALL_TARGETS = [
  { os: 'linux', arch: 'amd64', binName: 'cf-lease-broker-linux-amd64' },
  { os: 'linux', arch: 'arm64', binName: 'cf-lease-broker-linux-arm64' },
  { os: 'darwin', arch: 'arm64', binName: 'cf-lease-broker-darwin-arm64' },
  { os: 'windows', arch: 'amd64', binName: 'cf-lease-broker-windows-amd64.exe' },
];

function formatBytes(bytes) {
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function calculateFileHash(filePath, algorithm = 'sha256') {
  const hash = crypto.createHash(algorithm);
  const data = fs.readFileSync(filePath);
  hash.update(data);
  return hash.digest('hex');
}

// Verification Routine
function runVerification() {
  console.log(`=== [VERIFYING ARTIFACT CHECKSUMS: ${path.relative(REPO_ROOT, outDir)}] ===\n`);
  const checksumFile = path.join(outDir, 'checksums.sha256');

  if (!fs.existsSync(checksumFile)) {
    console.error(`[Error] Checksums manifest not found: ${checksumFile}`);
    process.exit(1);
  }

  const lines = fs.readFileSync(checksumFile, 'utf8').split('\n').filter(Boolean);
  let passed = 0;
  let failed = 0;

  for (const line of lines) {
    const parts = line.trim().split(/\s+/);
    if (parts.length < 2) continue;
    const [expectedHash, binName] = parts;
    const targetFile = path.join(outDir, binName);

    if (!fs.existsSync(targetFile)) {
      console.error(`  ❌ MISSING: ${binName} (file does not exist)`);
      failed++;
      continue;
    }

    const actualHash = calculateFileHash(targetFile, 'sha256');
    if (actualHash.toLowerCase() === expectedHash.toLowerCase()) {
      const stats = fs.statSync(targetFile);
      console.log(`  ✅ PASS: ${binName} (${formatBytes(stats.size)}) -> ${actualHash.slice(0, 16)}...`);
      passed++;
    } else {
      console.error(`  ❌ MISMATCH: ${binName}`);
      console.error(`     Expected: ${expectedHash}`);
      console.error(`     Actual  : ${actualHash}`);
      failed++;
    }
  }

  console.log(`\nVerification Result: ${passed} passed, ${failed} failed.`);
  if (failed > 0) {
    process.exit(1);
  }
  console.log('✅ All artifact checksums verified with 100% cryptographic integrity!');
}

if (verifyOnly) {
  runVerification();
  process.exit(0);
}

// Build Pipeline Execution
console.log('=================================================================');
console.log(` ⚡ CF-Lease-Broker Standalone Build Pipeline (v${version})`);
console.log(` 📁 Output Directory: ${outDir}`);
console.log('=================================================================\n');

if (shouldClean && fs.existsSync(outDir)) {
  console.log(`==> Cleaning output directory: ${outDir}`);
  fs.rmSync(outDir, { recursive: true, force: true });
}

if (!fs.existsSync(outDir)) {
  fs.mkdirSync(outDir, { recursive: true });
}

const targetsToBuild = targetFilter
  ? ALL_TARGETS.filter((t) => `${t.os}/${t.arch}` === targetFilter)
  : ALL_TARGETS;

if (targetsToBuild.length === 0) {
  console.error(`[Error] Unknown target filter: ${targetFilter}`);
  console.error(`Available targets: ${ALL_TARGETS.map((t) => `${t.os}/${t.arch}`).join(', ')}`);
  process.exit(1);
}

const buildTime = new Date().toISOString();
const gitCommit = process.env.GITHUB_SHA || 'standalone';
const ldflags = `-s -w -X main.Version=${version} -X main.BuildTime=${buildTime}`;

const builtArtifacts = [];
let buildFailed = false;

for (const target of targetsToBuild) {
  const targetId = `${target.os}/${target.arch}`;
  const outPath = path.join(outDir, target.binName);
  console.log(`==> Building target [${targetId}] -> ${target.binName}...`);

  const startTime = Date.now();
  const env = {
    ...process.env,
    GOOS: target.os,
    GOARCH: target.arch,
    CGO_ENABLED: '0',
  };

  const res = spawnSync(
    'go',
    ['build', '-trimpath', `-ldflags=${ldflags}`, '-o', outPath, '.'],
    {
      cwd: REPO_ROOT,
      env,
      stdio: 'inherit',
      shell: false,
    }
  );

  const duration = ((Date.now() - startTime) / 1000).toFixed(2);

  if (res.status !== 0) {
    console.error(`❌ Build failed for target ${targetId} with exit code ${res.status}`);
    buildFailed = true;
    break;
  }

  const stat = fs.statSync(outPath);
  const sha256 = calculateFileHash(outPath, 'sha256');
  const sha512 = calculateFileHash(outPath, 'sha512');

  console.log(
    `  ✅ Built: ${target.binName} (${formatBytes(stat.size)}) in ${duration}s [SHA256: ${sha256.slice(0, 12)}...]\n`
  );

  builtArtifacts.push({
    filename: target.binName,
    target: targetId,
    os: target.os,
    arch: target.arch,
    size_bytes: stat.size,
    size_formatted: formatBytes(stat.size),
    sha256,
    sha512,
  });
}

if (buildFailed) {
  console.error('\n❌ Build pipeline failed! Halting execution.');
  process.exit(1);
}

// Generate Checksums and Artifact Manifests
if (!skipChecksums && builtArtifacts.length > 0) {
  console.log('==> Generating SHA-256 and SHA-512 integrity manifests...');

  // 1. checksums.sha256 in standard format (<hash>  <filename>)
  const sha256Lines = builtArtifacts.map((a) => `${a.sha256}  ${a.filename}`).join('\n') + '\n';
  fs.writeFileSync(path.join(outDir, 'checksums.sha256'), sha256Lines, 'utf8');
  fs.writeFileSync(path.join(outDir, 'checksums.txt'), sha256Lines, 'utf8');

  // 2. artifacts_manifest.json with structured metadata
  const manifest = {
    project: 'cf-lease-broker',
    version,
    build_timestamp: buildTime,
    git_commit: gitCommit,
    total_artifacts: builtArtifacts.length,
    artifacts: builtArtifacts,
  };
  fs.writeFileSync(
    path.join(outDir, 'artifacts_manifest.json'),
    JSON.stringify(manifest, null, 2),
    'utf8'
  );

  console.log(`  -> Saved: ${path.join(outDir, 'checksums.sha256')}`);
  console.log(`  -> Saved: ${path.join(outDir, 'artifacts_manifest.json')}\n`);
}

console.log('=================================================================');
console.log(' ✅ All standalone cross-platform artifacts built successfully!');
console.log(` 📦 Binaries packaged: ${builtArtifacts.length} targets`);
console.log('=================================================================');
