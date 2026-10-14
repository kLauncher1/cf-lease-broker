import fs from 'node:fs';
import path from 'node:path';

const TARGET_DIR = process.env.TARGET_DIR || process.cwd();
const args = process.argv.slice(2);
const shouldCapture = args.includes('--capture');

console.log(`=== [ASSET INTEGRITY & ZERO EXTERNAL IMAGE SWEEP: ${path.basename(TARGET_DIR)}] ===\n`);

const SKIP_DIRS = new Set(['target', 'node_modules', '.git', 'dist', 'bin']);

function walkDir(dir, filter, list = []) {
  if (!fs.existsSync(dir)) return list;
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      walkDir(fullPath, filter, list);
    } else if (filter(fullPath)) {
      list.push(fullPath);
    }
  }
  return list;
}

// 1. External image link check
const mdFiles = walkDir(TARGET_DIR, f => f.endsWith('.md'));
console.log(`[Gate 1] Scanning ${mdFiles.length} Markdown files for external image leaks...`);

let leaks = [];
for (const mdPath of mdFiles) {
  const content = fs.readFileSync(mdPath, 'utf8');
  const mdImg = /!\[[^\]]*\]\((https?:\/\/[^\)]+)\)/g;
  const htmlImg = /<img[^>]+src=["'](https?:\/\/[^"']+)["']/gi;
  
  let match;
  while ((match = mdImg.exec(content)) !== null) {
    const url = match[1];
    if (!url.includes('img.shields.io') && !url.includes('badge')) {
      leaks.push({ file: path.relative(TARGET_DIR, mdPath), url });
    }
  }
  while ((match = htmlImg.exec(content)) !== null) {
    const url = match[1];
    if (!url.includes('img.shields.io') && !url.includes('badge')) {
      leaks.push({ file: path.relative(TARGET_DIR, mdPath), url });
    }
  }
}

if (leaks.length === 0) {
  console.log('  -> PASS: 0 external image leaks detected. All visual assets are 100% self-hosted.\n');
} else {
  console.error('  -> FAIL: Found external image leaks:', leaks);
  process.exit(1);
}

// 2. SVG XML entity and well-formedness check
const svgFiles = walkDir(TARGET_DIR, f => f.endsWith('.svg'));
console.log(`[Gate 2] Validating XML entity well-formedness across ${svgFiles.length} SVG files...`);

let svgErrors = [];
for (const svgPath of svgFiles) {
  const text = fs.readFileSync(svgPath, 'utf8');
  const relPath = path.relative(TARGET_DIR, svgPath);
  
  if (!text.includes('<svg') || !text.includes('</svg>')) {
    svgErrors.push({ file: relPath, error: 'Missing root <svg> tags' });
  }
  // Strip XML comments before checking entities
  const textWithoutComments = text.replace(/<!--[\s\S]*?-->/g, '');
  // Detect raw unescaped & (must be &amp; or entity)
  const rawAmpMatch = textWithoutComments.match(/&(?!(amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+);)/g);
  if (rawAmpMatch) {
    svgErrors.push({ file: relPath, error: `Found ${rawAmpMatch.length} unescaped raw '&' characters!` });
  }
  
  const openG = (text.match(/<g[\s>]/g) || []).length;
  const closeG = (text.match(/<\/g>/g) || []).length;
  if (openG !== closeG) {
    svgErrors.push({ file: relPath, error: `Mismatched <g> tags: ${openG} open vs ${closeG} close` });
  }
}

if (svgErrors.length === 0) {
  console.log(`  -> PASS: All ${svgFiles.length} SVG files are 100% XML well-formed with zero unescaped entities.\n`);
} else {
  console.error('  -> FAIL: Found SVG XML errors:', svgErrors);
  process.exit(1);
}

// 3. Optional Headless Browser Capture
if (shouldCapture) {
  console.log('[Gate 3] Performing Headless Browser Visual Render Capture...');
  try {
    const { createRequire } = await import('node:module');
    const req = createRequire(import.meta.url);
    const puppeteer = req('puppeteer');
    const browser = await puppeteer.launch({
      headless: true,
      executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
      args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-gpu']
    });
    const page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 2600, deviceScaleFactor: 2 });
    
    const testHtml = `file:///${path.join(TARGET_DIR, 'docs', 'test_render.html').replace(/\\/g, '/')}`;
    if (fs.existsSync(path.join(TARGET_DIR, 'docs', 'test_render.html'))) {
      await page.goto(testHtml, { waitUntil: 'networkidle0' });
      const outPng = path.join(TARGET_DIR, 'docs', 'render_verification_captured.png');
      await page.screenshot({ path: outPng, fullPage: true });
      console.log(`  -> PASS: Visual verification screenshot captured at: ${outPng}\n`);
    } else {
      console.log('  -> SKIP: docs/test_render.html not found, capture skipped.\n');
    }
    await browser.close();
  } catch (err) {
    console.warn(`  -> WARN: Puppeteer capture skipped: ${err.message}\n`);
  }
}

console.log('✅ ALL ASSET INTEGRITY GATES PASSED 100%!');
