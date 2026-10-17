import { readdir, readFile, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

const textExtensions = new Set(['.html', '.js', '.css', '.json', '.svg', '.txt', '.xml']);

// Keep Vite's public output intact. Only the Go embedding directory uses gzip
// files; the server negotiates encoding and can restore identity responses.
export async function packFrontend(source, destination) {
  source = path.resolve(source);
  destination = path.resolve(destination);
  if (source === destination || source.startsWith(destination + path.sep) || destination.startsWith(source + path.sep) || destination === path.parse(destination).root) {
    throw new Error('Frontend source and embedding directories must be separate');
  }
  const entries = await readdir(source, { withFileTypes: true });
  await mkdir(destination, { recursive: true });
  for (const entry of await readdir(destination)) {
    if (entry !== '.gitkeep') await rm(path.join(destination, entry), { recursive: true, force: true });
  }
  const stats = { files: 0, compressedFiles: 0, sourceBytes: 0, embeddedBytes: 0 };
  async function copyEntries(items, inputDir, outputDir) {
    for (const entry of items) {
      const input = path.join(inputDir, entry.name);
      const output = path.join(outputDir, entry.name);
      if (entry.isDirectory()) {
        await mkdir(output, { recursive: true });
        await copyEntries(await readdir(input, { withFileTypes: true }), input, output);
      } else if (entry.isFile()) {
        const raw = await readFile(input);
        const compressed = textExtensions.has(path.extname(entry.name)) ? gzipSync(raw, { level: 9 }) : raw;
        const useGzip = compressed.length + 32 < raw.length;
        await writeFile(output + (useGzip ? '.gz' : ''), useGzip ? compressed : raw);
        stats.files++;
        stats.compressedFiles += Number(useGzip);
        stats.sourceBytes += raw.length;
        stats.embeddedBytes += useGzip ? compressed.length : raw.length;
      } else {
        throw new Error(`Unexpected frontend entry: ${entry.name}`);
      }
    }
  }
  await copyEntries(entries, source, destination);
  return stats;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = fileURLToPath(new URL('../', import.meta.url));
  console.log(JSON.stringify(await packFrontend(path.join(root, 'frontend/dist'), path.join(root, 'internal/server/static/dist'))));
}
