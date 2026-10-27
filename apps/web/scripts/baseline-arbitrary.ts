// Rewrites the arbitrary-values baseline (src/ui/arbitrary-values/) for the current counts. It lowers counts,
// deletes the JSONs of files that have none left, and refuses to raise any count: a count that goes up is a
// new arbitrary value, which needs a token from styles.css. Run it to settle a baseline conflict, after
// taking main's JSONs.
import {
  baselineDir,
  formatChange,
  lowered,
  measure,
  raised,
  readBaseline,
  srcDir,
  writeBaseline,
} from './arbitrary-values.ts';

const allowed = readBaseline(baselineDir);
const measured = measure(srcDir);

const up = raised(allowed, measured);
if (up.length > 0) {
  console.error('Not written: these counts would go up. A new arbitrary value needs a token from styles.css.');
  for (const change of up) console.error(`  ${formatChange(change)}`);
  process.exit(1);
}

const down = lowered(allowed, measured);
writeBaseline(measured, baselineDir);
console.log(`Baseline written: ${down.length} count(s) lowered.`);
