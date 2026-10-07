# -*- coding: utf-8 -*-
# ONE_CLOUD E2E 冒烟脚本（纯标准库，无第三方依赖）
# 用法：
#   1. 启动 cloud（cloud/etc/storage.yaml）与 gateway（gateway/gateway.yaml）
#   2. python scripts/smoke_e2e.py
# 覆盖：认证/桶/直传与秒传/一元下载/直传限幅/分片全周期与重复 complete 门闩/
#       流式下载/分享（download+read 权限+匿名流式）/回收站闭环/统计/预签名回落/
#       登录锁定/匿名限流 429
# 注：无 Redis 时亦可运行（顺带验证各降级路径）；验证码类接口不在覆盖范围
import base64, json, time, urllib.request, urllib.error, sys, hashlib, io

BASE = "http://127.0.0.1:8888"
Pace = 0.14  # < 10 QPS，避开限流
results = []

def req(method, path, body=None, token=None, params=None, raw=False):
    url = BASE + path
    if params:
        url += "?" + "&".join(f"{k}={v}" for k, v in params.items())
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Content-Type", "application/json")
    if token:
        r.add_header("Authorization", "Bearer " + token)
    try:
        resp = urllib.request.urlopen(r, timeout=60)
        payload = resp.read()
        code = resp.status
    except urllib.error.HTTPError as e:
        payload = e.read()
        code = e.code
    if raw:
        return code, payload
    try:
        j = json.loads(payload)
    except Exception:
        j = {"_raw": payload[:200].decode(errors="replace"), "_http": code}
    time.sleep(Pace)
    return code, j

def check(name, ok, detail=""):
    results.append((name, ok, detail))
    print(("PASS " if ok else "FAIL ") + name + (("  -> " + str(detail)[:160]) if (not ok and detail) else ""))

def get_any(d, *keys):
    for k in keys:
        if isinstance(d, dict) and k in d and d[k] not in (None, ""):
            return d[k]
    return None

ts = str(int(time.time()))[-7:]
PWD = "smoke-Pass-123"

# ---------- 1. 认证 ----------
code, j = req("POST", "/user/register", {"name": "smk" + ts, "password": PWD})
check("注册", code == 200 and j.get("code") == 200, j)
tokenA = get_any(j.get("data", {}), "token")
check("注册返回 token", bool(tokenA), j.get("data"))

code, j = req("POST", "/user/loginByAccount", {"account": "smk" + ts, "password": PWD})
tokenA = get_any(j.get("data", {}), "token") or tokenA
check("登录", j.get("code") == 200 and tokenA, j.get("message"))

code, j = req("GET", "/file/list", params={"page": 1, "page_size": 5})
check("无 token 受保护接口 401", code == 401, code)

# ---------- 2. 桶 ----------
code, j = req("POST", "/bucket/create", {"name": "smoke-bucket-" + ts, "visibility": "private"}, tokenA)
bk = (j.get("data") or {}).get("bucket") or {}
bucket_id = get_any(bk, "bucketId", "bucket_id", "id")
check("创建桶", j.get("code") == 200 and bucket_id, j.get("data"))

# ---------- 3. 直传 + 秒传（无 Redis → DB 回退路径） ----------
blob1 = ("ONE_CLOUD smoke %s\n" % ts).encode() * 10  # ~170B
md5_1 = hashlib.md5(blob1).hexdigest()
content_b64 = base64.b64encode(blob1).decode()
code, j = req("POST", "/file/upload", {"bucketId": bucket_id, "name": "smoke-a.txt", "suffix": "txt",
                                       "size": len(blob1), "content": content_b64}, tokenA)
f = ((j.get("data") or {}).get("file") or {}) if j.get("code") == 200 else {}
file_a = get_any(f, "fileId", "file_id", "id")
check("直传上传", j.get("code") == 200 and file_a, j.get("message") or j.get("data"))

code, j = req("POST", "/file/upload", {"bucketId": bucket_id, "name": "smoke-a-copy.txt", "suffix": "txt",
                                       "size": len(blob1), "content": content_b64}, tokenA)
d = j.get("data") or {}
check("秒传命中（DB 回退）", j.get("code") == 200 and d.get("instant") is True, {"instant": d.get("instant"), "msg": j.get("message")})

# ---------- 4. 一元下载 + 5MB 直传限幅 ----------
code, j = req("GET", "/file/download", params={"file_id": file_a}, token=tokenA)
content = get_any(j.get("data", {}), "content")
ok = False
if content:
    ok = base64.b64decode(content) == blob1
check("一元下载内容一致", ok, j.get("message"))

big = b"Z" * (5 * 1024 * 1024 + 100)
code, j = req("POST", "/file/upload", {"bucketId": bucket_id, "name": "big.bin", "suffix": "bin",
                                       "size": len(big), "content": base64.b64encode(big).decode()}, tokenA)
check("直传 5MB+ 被限幅 30012", j.get("code") == 30012, j.get("message"))

# ---------- 5. 分片全周期（2 片 × 3MB） + MinIO 之外正常路径 ----------
blob2 = (b"PARTDATA-" + ts.encode()) * (3 * 1024 * 1024 // 12)  # 恰好 3MiB
blob2 += b"X" * (6 * 1024 * 1024 - len(blob2))
assert len(blob2) == 6 * 1024 * 1024
p1, p2 = blob2[:3 * 1024 * 1024], blob2[3 * 1024 * 1024:]
code, j = req("POST", "/file/multipart/init", {"bucketId": bucket_id, "name": "smoke-mp.bin", "suffix": "bin",
                                               "size": len(blob2), "totalParts": 2}, tokenA)
upload_id = get_any(j.get("data", {}), "uploadId", "upload_id")
check("分片 init", j.get("code") == 200 and upload_id, j.get("message") or j.get("data"))

for idx, part in ((1, p1), (2, p2)):
    code, j = req("POST", "/file/multipart/upload", {"uploadId": upload_id, "partNumber": idx,
                                                     "size": len(part), "content": base64.b64encode(part).decode()}, tokenA)
    check(f"分片 {idx} 上传", j.get("code") == 200, j.get("message"))

code, j = req("POST", "/file/multipart/complete", {"uploadId": upload_id}, tokenA)
f = ((j.get("data") or {}).get("file") or {}) if j.get("code") == 200 else {}
file_mp = get_any(f, "fileId", "file_id", "id")
check("分片 complete", j.get("code") == 200 and file_mp, j.get("message"))

# 重复 complete → 门闩应拒绝
code, j = req("POST", "/file/multipart/complete", {"uploadId": upload_id}, tokenA)
check("重复 complete 被门闩拒绝", j.get("code") != 200, {"code": j.get("code"), "msg": j.get("message")})

# 分片秒传：init 带 md5 → 空 uploadId
code, j = req("POST", "/file/multipart/init", {"bucketId": bucket_id, "name": "smoke-mp2.bin", "suffix": "bin",
                                               "size": len(blob2), "totalParts": 2, "md5": hashlib.md5(blob2).hexdigest()}, tokenA)
check("分片秒传（init md5 命中）", j.get("code") == 200 and not get_any(j.get("data", {}), "uploadId", "upload_id"),
      j.get("data"))

# ---------- 6. 流式下载（登录态） ----------
code, raw = req("POST", "/file/download_stream", {"file_id": file_mp}, token=tokenA, raw=True)
body = raw.decode(errors="replace")
check("流式下载 200 且有分块", code == 200 and body.count('"data"') >= 2, f"http={code} chunks={body.count(chr(34)+'data'+chr(34))}")

# ---------- 7. 分享链路 ----------
code, j = req("POST", "/file/share/create", {"fileId": file_mp, "permission": "download", "expireSeconds": 3600}, tokenA)
share_code = get_any(j.get("data", {}), "shareCode", "share_code")
check("创建分享(download)", j.get("code") == 200 and share_code, j.get("data"))

code, j = req("GET", "/file/share/access", params={"share_code": share_code})
perm = get_any(j.get("data", {}), "permission")
check("匿名访问分享", j.get("code") == 200 and perm == "download", {"perm": perm, "msg": j.get("message")})

code, j = req("GET", "/file/share/download", params={"share_code": share_code})
content = get_any(j.get("data", {}), "content")
check("匿名一元分享下载内容一致", bool(content) and base64.b64decode(content) == blob2, j.get("message"))

code, raw = req("POST", "/file/share/download_stream", {"share_code": share_code}, raw=True)
check("匿名流式分享下载", code == 200 and raw.decode(errors="replace").count('"data"') >= 2, f"http={code}")

code, j = req("POST", "/file/share/create", {"fileId": file_mp, "permission": "read", "expireSeconds": 3600}, tokenA)
share_read = get_any(j.get("data", {}), "shareCode", "share_code")
check("创建分享(read)", j.get("code") == 200 and share_read, j.get("data"))
code, j = req("GET", "/file/share/download", params={"share_code": share_read})
check("read 分享下载被拒", j.get("code") == 30002, {"code": j.get("code"), "msg": j.get("message")})

# ---------- 8. 回收站闭环 ----------
code, j = req("POST", "/file/delete", {"fileId": file_mp}, tokenA)
check("软删除", j.get("code") == 200, j.get("message"))
code, j = req("GET", "/file/info", params={"file_id": file_mp}, token=tokenA)
check("回收站文件详情按不存在", j.get("code") == 30001, {"code": j.get("code"), "msg": j.get("message")})
code, j = req("GET", "/file/recycle/list", params={"page": 1, "page_size": 50}, token=tokenA)
items = (j.get("data") or {}).get("files") or (j.get("data") or {}).get("items") or []
ids = [get_any(it, "fileId", "file_id", "id") for it in items] if isinstance(items, list) else []
check("回收站列表包含", file_mp in ids, f"count={len(ids)}")
code, j = req("POST", "/file/recycle/restore", {"fileIds": [file_mp]}, tokenA)
check("还原", j.get("code") == 200, j.get("message"))
code, j = req("GET", "/file/info", params={"file_id": file_mp}, token=tokenA)
check("还原后可见", j.get("code") == 200, j.get("message"))
code, j = req("POST", "/file/delete", {"fileId": file_mp}, tokenA)
check("再次软删", j.get("code") == 200, j.get("message"))
code, j = req("POST", "/file/recycle/purge", {"fileIds": [file_mp]}, tokenA)
check("彻底删除", j.get("code") == 200, j.get("message"))
code, j = req("GET", "/file/recycle/list", params={"page": 1, "page_size": 50}, token=tokenA)
items = (j.get("data") or {}).get("files") or (j.get("data") or {}).get("items") or []
ids = [get_any(it, "fileId", "file_id", "id") for it in items] if isinstance(items, list) else []
check("purge 后移出回收站", file_mp not in ids, f"still_in={file_mp in ids}")

# ---------- 9. 统计 / 趋势 / 预签名回落 ----------
code, j = req("GET", "/file/stats", token=tokenA)
check("用量统计", j.get("code") == 200, j.get("message"))
code, j = req("GET", "/file/stats/trend", params={"days": 7}, token=tokenA)
check("趋势", j.get("code") == 200, j.get("message"))
code, j = req("GET", "/file/presign", params={"file_id": file_a}, token=tokenA)
check("本地驱动预签名 40005", j.get("code") == 40005, {"code": j.get("code")})

# ---------- 10. 登录锁定（无 Redis → 进程内降级路径） ----------
code, j = req("POST", "/user/register", {"name": "smklk" + ts, "password": PWD})
tokenB = get_any(j.get("data", {}), "token")
locked_msg = ""
for i in range(5):
    code, j = req("POST", "/user/loginByAccount", {"account": "smklk" + ts, "password": "wrong-" + str(i)})
    locked_msg = j.get("message") or ""
code, j = req("POST", "/user/loginByAccount", {"account": "smklk" + ts, "password": PWD})
check("连续 5 次失败后锁定（正确密码也被拒）", "锁定" in (j.get("message") or ""), j.get("message"))

# ---------- 11. 匿名限流 429（故意突发 > Burst 20） ----------
seen429 = 0
urllib.request.install_opener(urllib.request.build_opener())
for i in range(30):
    try:
        resp = urllib.request.urlopen(BASE + "/file/share/access?share_code=nope" + str(i), timeout=20)
        resp.read()
    except urllib.error.HTTPError as e:
        if e.code == 429:
            seen429 += 1
    except Exception:
        pass
check("突发请求触发 429", seen429 > 0, f"429_count={seen429}")

# ---------- 汇总 ----------
fails = [r for r in results if not r[1]]
print("\n===== E2E 汇总: %d/%d 通过 =====" % (len(results) - len(fails), len(results)))
for name, ok, detail in fails:
    print("FAILED:", name, "->", str(detail)[:200])
sys.exit(1 if fails else 0)
