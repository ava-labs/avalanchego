// Runs the demo page's own inline script headlessly with a fake MetaMask
// (ewoq key) against a proxy, then drives the four flows and presses every
// stage button the page renders. usage: node page_test.mjs <www dir> <base url>
import fs from 'fs';
const ethersSrc = fs.readFileSync(process.argv[2] + '/ethers.umd.min.js', 'utf8');
const ethers = (new Function(ethersSrc + '; return ethers;'))();
const html = fs.readFileSync(process.argv[2] + '/index.html', 'utf8');
const script = html.split('<script>')[1].split('</script>')[0];
const BASE = process.argv[3];
const wallet = new ethers.Wallet('0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027');
const provider = new ethers.JsonRpcProvider(BASE + '/ext/bc/C/rpc');

const el = () => ({ textContent: '', innerHTML: '', className: '', value: '', hidden: false, children: [],
  appendChild(c) { this.children.push(c); }, querySelector() { return el(); }, remove() { this.onclick = null; } });
const els = {};
const document = { getElementById: id => els[id] || (els[id] = el()), createElement: () => el(),
  querySelectorAll: q => q.startsWith('[data-k') ? [els[q.slice(9, -2)] || (els[q.slice(9, -2)] = el())] : [] };
const ethereum = { on() {}, async request({ method, params }) {
  if (method === 'eth_chainId') return '0x' + (await provider.getNetwork()).chainId.toString(16);
  if (method === 'eth_requestAccounts') return [wallet.address];
  if (method === 'wallet_addEthereumChain') return null;
  if (method === 'eth_sendTransaction') {
    const t = params[0];
    const tx = await wallet.connect(provider).sendTransaction({ to: t.to, data: t.data, value: t.value ? BigInt(t.value) : 0n, gasLimit: BigInt(t.gas) });
    return tx.hash;
  }
  throw new Error('unexpected ' + method);
} };
const shims = { ethers, document, ethereum, location: { origin: BASE, search: '' }, navigator: {}, setInterval: () => 0,
  fetch, alert: m => { throw new Error(m); } };
const api = new Function(...Object.keys(shims), script + '\nreturn { run, connect, exportToP, importOnP, exportFromP, importOnC, refresh, refreshActivity };')(...Object.values(shims));
// mirror what the page prints: log() spans and stepper descriptions
const panels = ['fb-setup', 'fb-export', 'fb-importp', 'fb-exportp', 'fb-import'];
for (const id of panels) document.getElementById(id).appendChild = c => { if (c.className === 'steps') els[id].steps = c; else process.stdout.write(c.textContent); };
const stepEl = () => ({ className: '', children: [], d: el(), t: el(), appendChild(c) { this.children.push(c); }, querySelector(q) { return q === '.d' ? this.d : this.t; } });
document.createElement = tag => tag === 'div' ? stepEl() : el();
let failed = false;
const origWrite = process.stdout.write.bind(process.stdout);
process.stdout.write = s => { if (/timed out|reverted|Error|error/.test(s)) failed = true; return origWrite(s); };

await new Promise(r => setTimeout(r, 1500)); // let the boot IIFE fetch network.json
document.getElementById('amt').value = '30';
await api.connect();
const dump = box => { for (const st of box.children) console.log('   [' + st.className + '] ' + st.t.textContent + (st.d.textContent ? ' :: ' + st.d.textContent.replace(/\n/g, ' | ') : '')); };
for (const f of ['exportToP', 'importOnP', 'exportFromP', 'importOnC']) {
  const t0 = Date.now();
  const panel = { exportToP: 'fb-export', importOnP: 'fb-importp', exportFromP: 'fb-exportp', importOnC: 'fb-import' }[f];
  console.log('== ' + f);
  await api.run(api[f]);
  const box = els[panel].steps;
  for (;;) { // press every stage button the page renders, in order
    const btn = box.children.flatMap(st => st.children).find(b => b.onclick);
    if (!btn) break;
    console.log('   > click ' + btn.textContent);
    await btn.onclick();
  }
  dump(box);
  console.log(`   [${f} took ${((Date.now() - t0) / 1000).toFixed(1)}s]`);
}
await api.refresh(); await api.refreshActivity();
console.log('C balance:', els.cbal.textContent, '| P balance:', els.pbal.textContent, '| c2p:', els.c2p.textContent, '| p2c:', els.p2c.textContent);
console.log('activity rows:', (els.activity.children[0] || { children: [] }).children.length);
if (failed) { console.log('FAILED'); process.exit(1); }
console.log('ALL FLOWS OK');
