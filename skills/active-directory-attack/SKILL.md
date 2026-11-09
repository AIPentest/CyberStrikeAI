---
name: active-directory-attack
description: >-
  Internal domain attack: BloodHound, Kerberoast, ADCS ESC1/ESC8, NTLM Relay, Coerce, DACL, DCSync, Zerologon/NoPac/PrintNightmare, mitm6, LLMNR, Linux internal network. Use when attacking Active Directory, ADCS, NTLM relay, or internal domain.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Internal Domain Attack

```
=== Internal domain (2023+ true main battleground) ===
Recon: BloodHound (SharpHound collection → attack paths) | Kerberos: GetNPUsers (AS-REP)/GetUserSPNs (Kerberoast)
🚨ADCS (Certipy one-stop): certipy find -vulnerable | ESC1 with a specified SAN claims a Domain Admin certificate | ESC8 relay to the CA to grab the DC certificate
🚨NTLM Relay (more important than PtH, which EDR often blocks): ntlmrelayx -t ldap --escalate-user / -t http CA --adcs (ESC8) / RBCD
🚨Forced authentication Coerce: PetitPotam (MS-EFSRPC)/coercer full-protocol spray/printerbug → feed to relay
🚨DACL abuse: WriteDACL → add DCSync for yourself | shadow credentials certipy shadow (only needs GenericWrite, does not change the password and leaves no trace)
DCSync: secretsdump -just-dc → krbtgt hash → Golden Ticket
🚨One-shot domain-fatal CVEs (test first; a hit is a direct Domain Admin): Zerologon (CVE-2020-1472, sets an empty DC machine account password → DCSync) | NoPac (CVE-2021-42278/42287, machine account renamed claims a DC TGT) | PrintNightmare (CVE-2021-34527, background-print RCE/load a malicious driver) | EternalBlue (MS17-010, old SMBv1 direct RCE)
🚨IPv6/mitm6 (dual-stack default in the internal network, must attack): mitm6 hijacks DHCPv6+DNS → WPAD → ntlmrelayx to LDAP/ADCS (more stealthy than LLMNR, first choice in modern internal networks)
LLMNR/NBT-NS poisoning: responder captures NetNTLMv2 → hashcat crack/relay
Linux internal network: unauthenticated Redis (CONFIG SET dir to write an SSH key) | NFS showmount | Docker 2375
```
