---
name: blockchain-contract-attack
description: >-
  Blockchain/smart contracts: Etherscan, slither/mythril, reentrancy/access control/oracles/flash loans, cross-chain bridges, exposed RPC. Use when auditing smart contracts, DeFi, or blockchain attack surfaces.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Blockchain / Smart Contracts

```
=== Blockchain/smart contracts ===
Source: Etherscan getsourcecode API | Audit slither/mythril/manticore
Manual: reentrancy (.call{value} transfers first and then modifies state, violating C-E-I) | access control (onlyOwner missing) | integer overflow (<0.8 without SafeMath)
  Oracle manipulation (flash-loan instant manipulation of AMM price) | randomness (block.timestamp controllable) | approval abuse (infinite approve/permit replay) | delegatecall proxy storage collision
DeFi: flash-loan attack/sandwich (front-running)/governance attack/signature replay | cross-chain bridge: signature threshold bypass/replay | RPC: exposed 8545 directly eth_sendTransaction
```
