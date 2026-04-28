import { JsonRpcProvider, Wallet, parseEther, formatEther } from "ethers";
import { writeFileSync } from "fs";

const config = JSON.parse(process.argv[2]);
const { nodes, ewoqKey, rpcUrls, fundAmount } = config;

const results = [];

for (const rpcUrl of rpcUrls) {
  console.log(`\n=== Funding on ${rpcUrl} ===`);

  const provider = new JsonRpcProvider(rpcUrl);
  const ewoqWallet = new Wallet(ewoqKey, provider);

  try {
    const ewoqBal = await provider.getBalance(ewoqWallet.address);
    console.log(`Ewoq balance: ${formatEther(ewoqBal)} AVAX`);
  } catch (e) {
    console.log(`Skipping ${rpcUrl}: ${e.message}`);
    continue;
  }

  let nonce = await provider.getTransactionCount(ewoqWallet.address);

  for (const node of nodes) {
    const wallet = new Wallet(node.privateKey, provider);
    const address = wallet.address;

    const currentBal = await provider.getBalance(address);
    const currentBalEth = parseFloat(formatEther(currentBal));

    if (currentBalEth < parseFloat(fundAmount) * 0.9) {
      const needed = parseFloat(fundAmount) - currentBalEth;
      console.log(`  Node ${node.index}: ${address} — funding ${needed.toFixed(2)} AVAX...`);
      const tx = await ewoqWallet.sendTransaction({
        to: address,
        value: parseEther(needed.toFixed(18)),
        nonce: nonce++,
      });
      await tx.wait();
      console.log(`    tx: ${tx.hash}`);
    } else {
      console.log(`  Node ${node.index}: ${address} — already funded (${currentBalEth.toFixed(2)} AVAX)`);
    }

    // Only build results from the first RPC (C-Chain)
    if (rpcUrl === rpcUrls[0]) {
      results.push({ uri: node.uri, address });
    }
  }
}

const outputPath = process.argv[3];
writeFileSync(outputPath, JSON.stringify(results, null, 2));
console.log(`\nWrote ${results.length} nodes with addresses to ${outputPath}`);
