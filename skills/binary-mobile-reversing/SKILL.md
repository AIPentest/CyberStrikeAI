---
name: binary-mobile-reversing
description: >-
  APK/EXE/binaries: UniApp/DCloud/Flutter reversing, certificate-pinning bypass, exported components, memory-corruption exploit chains, IoT firmware. Use when reversing APK/EXE, UniApp/Flutter, native .so, or memory-corruption exploits.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## APK / EXE / Binary Reversing

```
=== APK/EXE/binary ===
APK: apktool d / jadx | Manifest for exported components/deeplink/debuggable | rg for hardcoded keys + API addresses | .so strings/Ghidra | frida/objection dynamic
🚨UniApp/DCloud APK reversing (H5 hybrid app, business logic entirely in JS not in DEX):
  Identification: __UNI__XXXXXXX + assets/apps/ + uni-jsframework.js (NOT Flutter, no libapp.so) | manifest.json contains a "uni-app" field
  Key file: assets/apps/__UNI__XXX/www/app-service.js (800KB+ obfuscated JS = all the business logic) | jadx only sees the shell (native plugin registration), the real logic is in JS
  JS deobfuscation (RC4 + string-array rotation): extract the a0G() large array (10000+ elements) + the a0m(idx,key) decoding function (base64→RC4) + the rotation IIFE → assemble executable JS → node bulk-decode → JSON dictionary (offset→plaintext)
    🔴Trap: the rotation IIFE may end with a comma (not a semicolon! it belongs to a larger expression) → node reports "Unexpected token" → extract according to the actual terminator
  Encrypted config zlsioh.dat/dcloud3.dat: header (96B offset table: off40 = block1 uncompressed/44 = compressed/60 = block2 offset/64 = block2 size/80 = block3 offset) + block1 (zlib→DEX) + block2 (encrypted, contains the API domain list ← key, needs .so decryption) + block3 (zlib→AndroidX class mapping)
    zlib magic 78DA = unencrypted, decompress directly | no magic = encrypted block (key in .so .rodata/assembly immediates)
  .so string deobfuscation: extract even-position characters → reverse (e.g. "mAojcl.dubdFiHaebP.nwywfwb" → "www.baidu.com") | strings -n8 lib*.so for long dot-containing strings
  ⚠️Trap: IP strings separated by x in the .so (like "x111.230.69.120x118.126.105.164") = DCloud HTTPDNS nodes (NOT the API backend! 140.205.11.x = Alibaba Cloud DNS, 111.230/118.126/106.52/42.193 = Tencent Cloud) | the resources.arsc error is an intentional anti-compilation protection (does not affect jadx) | assets pseudo-PNG (1-8px) = integrity check, not data | fcapp.run is only an APK download proxy
  Key API variables: $apiHost/$opHost (set dynamically at runtime, not hardcoded)/urlList/checkAvailableDomainList (HEAD domain/favicon.ico connectivity test, 200-399 = available)
  High-value endpoints: /app_init /user/gustRegister (guest registration without verification) /user/getOssSts (OSS temporary credentials!) /uploadFile2
  Configuration: manifest.json (appId/version/nativePlugins → wrs-httpserver = embedded HTTP server!) | supplierconfig.json (vivo/xiaomi/huawei/oppo appid) | dcloud_uniplugins.json (plugin manifest)
  ChengZi SDK (ChengZi site builder): the init3 interface returns XOR single-byte encrypted JSON (key = 0x96, see chengzi_decrypt.py) → decrypt to get the real APK download link (fu field) + channel code
  Domain fallback: when $apiHost is not available statically → Android emulator (Waydroid/AVD) + tcpdump/mitmproxy to capture runtime DNS
  See: references/uniapp-apk-reverse-engineering.md, references/uniapp-apk-reversing.md, scripts/js_rc4_deobfuscate.js, scripts/chengzi_decrypt.py
🚨Flutter APK quick reversing (no unpacking needed): hardening (MogoSec/Bangbang/360) protects the DEX layer, but Flutter's lib/arm64-v8a/libapp.so (Dart AOT) is usually not encrypted
  strings -n8 libapp.so | grep 'https\?://' → all hardcoded URLs/API domains/S3 addresses/CDN
  strings -n8 libapp.so | grep -E '^/' → API path matrix (/Member/Login, /Web/VideoList, /BBS/GetSTSToken, etc.)
  strings -n8 libapp.so | grep -iE 'key|secret|token|aws|bucket' → credential/key leaks
  assets/config_*.xml + assets/data_*.dat → encrypted configuration (may contain domains/API addresses) | .DS_Store → macOS developer information leak | assets/xinstall* → channel-tracking SDK
🚨Native .so string deobfuscation (generic pattern): the obfuscated strings live in the .rodata section
  Common patterns: extract even-position characters + reverse (like "mAojcl.dubdFiHaebP..." → take even positions → reverse = plaintext domain) | XOR constant | RC4+base64
  Identification: strings -n8 lib*.so to find 16-byte equal-length strings (AES key/IV)/dot-containing strings (domains)/x-separated IP strings (HTTPDNS) | nm --dynamic to find obfuscated exported symbols (16 random mixed-case characters)
  Verification: match an obfuscated version against a known string ("classes.dex"/"io.dcloud.application") → reverse-derive the algorithm → bulk decode
🚨Mobile generic (ordinary apps other than UniApp/Flutter):
  Certificate-pinning bypass (to capture HTTPS): objection android sslpinning disable | frida universal-unpinning | modify smali to delete pinning and repackage
  Exported-component privilege escalation: Manifest exported=true Activity/Service/Provider/Receiver → drozer/adb am start cross-application calls | ContentProvider SQLi/path traversal
  Deeplink hijacking: scheme:// without validation → WebView loads an arbitrary URL (XSS/file read) | parameters flow into intent → component hijacking
  Insecure storage: /data/data/pkg/ (shared_prefs plaintext tokens/db unencrypted/logcat leaks) | sdcard world-readable
  WebView: addJavascriptInterface (<4.2 RCE)/file:// read local/setAllowFileAccess | static: MobSF one-stop | iOS: shell-breaking (frida-ios-dump) + Ghidra on Mach-O + objection dynamic
EXE/PE: file/strings | Ghidra/IDA decompile to find hardcoded/encryption/network logic | x64dbg live-debug | vulnerabilities: overflow/format string/UAF/DLL hijacking
🚨Memory-corruption exploit chain (crash→RCE): checksec to see protections → determine the primitive (stack overflow/UAF/format string = arbitrary read/write)
  → information leak to bypass ASLR → ROP chain (ROPgadget/pwntools ret2libc) → heap exploitation (tcache poison/__free_hook hijacking)
  → arbitrary write to modify GOT/hook/vtable/exit_funcs → control-flow hijack | IoT firmware: binwalk -Me extract + qemu-user debug (protection weak, often a direct stack overflow)
```
