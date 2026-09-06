// P8 Task 6：上传组件 UploadPanel —— 小文件 XHR 直传（字节级进度）+ 大文件分片编排（3 并发/断点重试/取消/秒传）
// 依赖 Task 3 契约：api.js（uploadFile/initMultipartUpload/uploadPart/completeMultipartUpload/abortMultipartUpload/getBucketList）、
// store.js（store.uploads / store.buckets / store.token）、common.js（formatSize/fileIcon/toast）
//
// 【curl 实测结论（2026-09-05，gateway go-zero + protoset）】
// 1. /file/upload 不接受 multipart FormData：curl -F 实测返回 {"code":50000,"message":"文件内容为空"}
//    （go-zero 网关按 JSON 反序列化，multipart 被当作空 body）。故直传走 JSON base64（proto bytes 字段标准编码）。
// 2. 网关 HTTP body 上限：P9 起在 gateway.yaml 显式配置 MaxBytes=110MB（go-zero 默认 1MB 已放开，实测 2.8MB JSON 200），
//    但 go-zero zrpc 无 gRPC 消息上限配置（默认 4MB，>4MB 报 ResourceExhausted），放开需改 cloud 代码，本批不动。
//    ⇒ 直传阈值维持 DIRECT_MAX=700KB；PART_SIZE_GUESS=700KB（init 返回 partSize=ceil(size/totalParts) 恒 ≤ GUESS，
//       JSON body ≤ ~932KB，实测 932150B < 1MB 通过；6MB 文件 partSize=699051B 实测 200）。
//    ⇒ SAFETY：若 init 返回 partSize 超过安全上限（MAX_CONTENT_BYTES），前端重新 init（调大 totalParts）钳制（正常不触发）。
// 3. 分片三步/秒传/取消实测：init→{uploadId,partSize}；uploadPart(content=base64) 200；complete→{file,instant}
//    （重复上传 instant:true）；abort 200（之后 part 被拒）；配额不足 code 20004「桶容量不足」。
const { ref, computed, onMounted, onUnmounted } = Vue;
import {
  initMultipartUpload, uploadPart, completeMultipartUpload,
  abortMultipartUpload, errText,
} from '../api.js';
import { store } from '../store.js';
import { formatSize, fileIcon, toast } from '../common.js';

// ===== 常量（数值依据见文件头实测结论） =====
const BODY_SAFE_LIMIT = 4194304;                  // gRPC 消息上限 4MB（go-zero zrpc 默认，实测 429 ResourceExhausted）
const BASE64_INFLATE = 4 / 3;                     // base64 膨胀系数
const JSON_OVERHEAD = 512;                        // JSON 包装字段余量（uploadId/name 等）
// 单个分片/直传内容安全上限：内容字节 + 包装 ≤ 4MB（网关 MaxBytes=110MB 不再是瓶颈）
const MAX_CONTENT_BYTES = Math.floor(BODY_SAFE_LIMIT - JSON_OVERHEAD); // ≈4MB
const DIRECT_MAX = 5 * 1024 * 1024;               // 直传阈值（cloud gRPC MaxRecvMsgSize 已放宽至 110MB，P9 恢复规格值）
const PART_SIZE_GUESS = 5 * 1024 * 1024;          // init 估值：partSize=ceil(size/totalParts) 恒 ≤ GUESS
const CONCURRENCY = 3;                            // 分片并发
const MAX_TOTAL_PARTS = 10000;                    // proto 硬限制
const SUFFIX_RE = /^[A-Za-z0-9]{1,8}$/;           // 后缀白名单（服务端 suffixRe 单段）
const NAME_MAX = 100;                             // proto name max_len

// 无构建链：直接注入全局 <style>，类名 p8- 前缀防冲突
if (!document.getElementById('p8-upload-style')) {
  const s = document.createElement('style');
  s.id = 'p8-upload-style';
  s.textContent = `
    .p8-up-toolbar { display:flex; gap:12px; align-items:center; margin-bottom:20px; }
    .p8-up-toolbar label { font-size:14px; color:#555; white-space:nowrap; }
    .p8-up-toolbar select { padding:8px 12px; border:1px solid #ddd; border-radius:8px; font-size:14px;
      color:#333; background:#fff; outline:none; min-width:220px; }
    .p8-up-toolbar select:focus { border-color:#667eea; }
    .p8-up-area.dragover { border-color:#667eea; background:#f5f7ff; }
    .p8-up-hint { color:#bbb; font-size:12px; margin-top:8px; }
    .p8-up-queue { margin-top:20px; }
    .p8-up-queue-title { font-size:14px; color:#666; margin-bottom:10px; }
    .p8-up-row { display:flex; align-items:center; gap:12px; padding:10px 14px; border:1px solid #eee;
      border-radius:10px; margin-bottom:10px; background:#fafafa; }
    .p8-up-icon { font-size:22px; flex-shrink:0; }
    .p8-up-info { flex:1; min-width:0; }
    .p8-up-name { font-size:14px; color:#333; font-weight:500; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .p8-up-size { font-size:12px; color:#999; margin-top:2px; }
    .p8-up-barwrap { flex:1.2; min-width:140px; height:14px; background:#ececec; border-radius:7px; overflow:hidden; }
    .p8-up-bar { height:100%; background:linear-gradient(135deg,#667eea 0%,#764ba2 100%); width:0%;
      transition:width .25s; }
    .p8-up-bar.error { background:#f56c6c; }
    .p8-up-pct { width:44px; text-align:right; font-size:13px; color:#667eea; flex-shrink:0; }
    .p8-up-tag { padding:3px 10px; border-radius:10px; font-size:12px; flex-shrink:0; white-space:nowrap; }
    .p8-up-tag.uploading { background:#ecf5ff; color:#409eff; }
    .p8-up-tag.done { background:#f0f9eb; color:#67c23a; }
    .p8-up-tag.error { background:#fef0f0; color:#f56c6c; }
    .p8-up-tag.canceled { background:#f4f4f5; color:#909399; }
    .p8-up-cancel { border:1px solid #ddd; background:#fff; border-radius:6px; padding:5px 12px;
      font-size:13px; cursor:pointer; color:#f56c6c; flex-shrink:0; transition:all .2s; }
    .p8-up-cancel:hover { border-color:#f56c6c; background:#fef0f0; }
    .p8-up-err { color:#f56c6c; font-size:12px; flex-shrink:0; max-width:220px; overflow:hidden;
      text-overflow:ellipsis; white-space:nowrap; }
    .p8-up-instant { padding:2px 8px; border-radius:8px; font-size:11px; background:#fdf6ec; color:#e6a23c; flex-shrink:0; }
  `;
  document.head.appendChild(s);
}

// ===== 工具 =====
// 取文件名最后一个扩展段；无扩展名用 bin；存在但不在白名单（含 / 或超长）返回 null（调用方拦截）
function extOf(name) {
  const base = name.split('/').pop().split('\\').pop();
  const m = base.match(/\.([A-Za-z0-9]+)$/); // 只取最后一个扩展段（tar.gz → gz）
  if (!m) return 'bin';
  return SUFFIX_RE.test(m[1]) ? m[1] : null;
}
// ArrayBuffer → base64（分块 btoa，避免 String.fromCharCode 展开栈溢出）
function bufToBase64(buffer) {
  const bytes = new Uint8Array(buffer);
  const CHUNK = 0x8000;
  let bin = '';
  for (let i = 0; i < bytes.length; i += CHUNK) {
    bin += String.fromCharCode.apply(null, bytes.subarray(i, Math.min(i + CHUNK, bytes.length)));
  }
  return btoa(bin);
}
const readFileBuf = (file) => new Promise((res, rej) => {
  const r = new FileReader();
  r.onload = () => res(r.result);
  r.onerror = () => rej({ code: -1, message: '文件读取失败' });
  r.readAsArrayBuffer(file);
});

export const UploadPanel = {
  name: 'UploadPanel',
  setup() {
    const bucketId = ref('');
    const dragOver = ref(false);
    const fileInput = ref(null);
    let dragCounter = 0;
    let uidSeq = 0;

    const bucketIdOf = (b) => b.bucketId || b.bucket_id || '';
    const hasBuckets = computed(() => store.buckets.length > 0);
    const queueActive = computed(() => store.uploads.some((u) => u.status === 'uploading'));

    // 桶列表：进入面板时若为空则拉取；仍为空 toast 引导建桶
    // Task 7：实现收敛到 store.loadBuckets()（FileList/UploadPanel/BucketPanel 三处共用），本组件保留空桶引导 + 默认选中
    async function loadBuckets(silent) {
      await store.loadBuckets({ silent });
      if (!store.buckets.length && !silent) toast('暂无桶，请先创建桶后再上传', 'error');
      if (!bucketId.value && store.buckets.length) bucketId.value = bucketIdOf(store.buckets[0]);
    }

    // ===== 队列结束：刷新文件列表 + 桶列表（用量变化）=====
    function refreshAfterAll() {
      if (store.uploads.some((u) => u.status === 'uploading')) return; // 还有进行中的
      if (store.refreshFiles) store.refreshFiles();
      loadBuckets(true);
    }

    // ===== 直传分支（JSON base64 + XHR 字节级进度；FormData 已实测被网关拒绝，见文件头）=====
    function directUpload(item, bid) {
      return new Promise((resolve, reject) => {
        readFileBuf(item.file).then((buf) => {
          const content = bufToBase64(buf);
          const xhr = new XMLHttpRequest();
          item.ctrl = { abort: () => xhr.abort() };
          item.loaded = 0;
          xhr.upload.onprogress = (e) => {
            item.loaded = e.loaded;
            // 发送进度封顶 99，成功后置 100（发送完成 ≠ 服务端确认）
            item.progress = Math.min(99, Math.round(e.loaded / e.total * 100));
          };
          xhr.onload = () => {
            // 网关对无效 token 回 HTTP 401 + text/plain（非 JSON），先于 JSON 解析拦截做全局登出
            if (xhr.status === 401) {
              store.logout();
              reject({ code: 10101, message: '登录已失效，请重新登录' });
              return;
            }
            try {
              const r = JSON.parse(xhr.responseText);
              if (r.code === 200) {
                item.loaded = item.size; item.progress = 100;
                resolve(r.data || {});
              } else {
                if ([10101, 10102, 10103].includes(r.code) && store.route === 'app') store.logout();
                reject({ code: r.code, message: errText(r.code, r.message) });
              }
            } catch { reject({ code: -1, message: '响应解析失败' }); }
          };
          xhr.onerror = () => reject({ code: -1, message: '网络错误' });
          xhr.onabort = () => reject({ code: -1, canceled: true, message: '已取消' });
          xhr.open('POST', '/api/file/upload');
          xhr.setRequestHeader('Content-Type', 'application/json');
          if (store.token) xhr.setRequestHeader('Authorization', `Bearer ${store.token}`);
          xhr.send(JSON.stringify({ bucketId: bid, name: item.name, suffix: item.suffix, size: item.size, content }));
        }).catch(reject);
      });
    }

    // ===== 分片分支（>DIRECT_MAX）：init → 3 并发 worker（next 指针，每片失败重试 1 次）→ complete =====
    async function multipartUpload(item, bid) {
      const size = item.size;
      // totalParts 估值：保证 partSize ≤ PART_SIZE_GUESS（网关 1MB body 安全），同时 ≤ 10000
      let totalParts = Math.ceil(size / PART_SIZE_GUESS);
      if (totalParts > MAX_TOTAL_PARTS) throw { code: 30012, message: '文件过大' };

      let init = await initMultipartUpload({ bucketId: bid, name: item.name, suffix: item.suffix, size, totalParts });
      if (!init.uploadId) return { instant: true }; // 秒传命中（客户端预传 md5 的路径，本实现未传 md5，正常不走此分支）
      let uploadId = init.uploadId;
      let partSize = init.partSize;

      // 安全钳制：init 返回的 partSize 若超出网关 body 安全上限，按安全值调大 totalParts 重新 init
      if (partSize > MAX_CONTENT_BYTES) {
        const safeParts = Math.ceil(size / MAX_CONTENT_BYTES);
        if (safeParts > MAX_TOTAL_PARTS) throw { code: 30012, message: '文件过大' };
        try { await abortMultipartUpload({ uploadId }); } catch { /* 旧任务清理失败不影响重开 */ }
        init = await initMultipartUpload({ bucketId: bid, name: item.name, suffix: item.suffix, size, totalParts: safeParts });
        if (!init.uploadId) return { instant: true };
        uploadId = init.uploadId;
        partSize = init.partSize;
        totalParts = safeParts;
      }

      const realParts = Math.ceil(size / partSize); // 实测恒等于 totalParts（partSize=ceil(size/totalParts) 数学保证）
      item.total = realParts;
      item.loaded = 0;

      let next = 1; // 分片指针：单线程事件循环内取号，无遗漏/重复
      const worker = async () => {
        while (next <= realParts && item.status === 'uploading') {
          const n = next++;
          const start = (n - 1) * partSize;
          const blob = item.file.slice(start, Math.min(start + partSize, size));
          const content = bufToBase64(await readFileBuf(blob));
          let lastErr = null;
          for (let attempt = 0; attempt < 2; attempt++) { // 断点重试 1 次（服务端按 part 幂等，不会重复计账）
            if (item.status !== 'uploading') return;
            try {
              await uploadPart({ uploadId, partNumber: n, size: blob.size, content });
              item.loaded += blob.size; // 仅成功后累计，重试不重复计 loaded
              item.progress = Math.min(99, Math.round(item.loaded / size * 100));
              lastErr = null;
              break;
            } catch (e) { lastErr = e; }
          }
          if (lastErr) throw lastErr;
        }
      };

      try {
        await Promise.all(Array.from({ length: Math.min(CONCURRENCY, realParts) }, worker));
        if (item.status === 'canceled') throw { code: -1, canceled: true, message: '已取消' };
        const res = await completeMultipartUpload({ uploadId });
        item.loaded = size;
        item.progress = 100;
        return res;
      } catch (e) {
        // 失败与取消都要清理服务端分片临时目录
        try { await abortMultipartUpload({ uploadId }); } catch { /* 清理失败忽略 */ }
        throw e;
      }
    }

    // ===== 单项调度 =====
    async function run(item) {
      try {
        const res = item.size > DIRECT_MAX
          ? await multipartUpload(item, item.bucketId)
          : await directUpload(item, item.bucketId);
        if (item.status !== 'uploading') return; // 已被取消
        // 秒传命中路径（multipartUpload 提前 return {instant:true}）不经过 complete 后的收尾赋值，这里补齐满进度
        if (res && res.instant) { item.progress = 100; item.loaded = item.size; }
        item.status = 'done';
        item.instant = !!(res && res.instant);
        toast(item.instant ? `「${item.name}」秒传命中` : `「${item.name}」上传完成`, 'success');
      } catch (e) {
        if (e && e.canceled) { item.status = 'canceled'; toast(`「${item.name}」已取消`, 'info'); }
        else if (item.status === 'canceled') { /* 取消竞态：保持 canceled，不追加失败提示 */ }
        else {
          item.status = 'error';
          item.error = (e && e.message) || '上传失败';
          toast(`「${item.name}」上传失败：${item.error}`, 'error');
        }
      } finally {
        item.ctrl = null;
        refreshAfterAll();
      }
    }

    // ===== 入口：校验 → 入队即启动 =====
    function enqueue(file) {
      if (!file.size) { toast(`「${file.name}」为空文件，已跳过`, 'error'); return; }
      if (file.name.length > NAME_MAX) { toast(`「${file.name}」文件名超过 100 字符，已跳过`, 'error'); return; }
      const suffix = extOf(file.name);
      if (!suffix) { toast(`「${file.name}」文件后缀非法（仅限字母数字，≤8 位），已跳过`, 'error'); return; }
      const item = {
        id: ++uidSeq,
        name: file.name,
        size: file.size,
        suffix,
        file,
        loaded: 0,
        progress: 0,
        status: 'uploading',
        error: '',
        instant: false,
        bucketId: bucketId.value,
        ctrl: null,
      };
      store.uploads.push(item);
      run(item);
    }

    function addFiles(fileList) {
      if (!bucketId.value) { toast('请先选择目标桶', 'error'); return; }
      const files = Array.from(fileList || []);
      if (!files.length) return;
      files.forEach(enqueue);
    }

    function onPick(e) {
      addFiles(e.target.files);
      e.target.value = ''; // 允许重复选择同一文件
    }

    function onDrop(e) {
      dragOver.value = false;
      dragCounter = 0;
      addFiles(e.dataTransfer.files);
    }
    function onDragEnter() { dragCounter++; dragOver.value = true; }
    function onDragLeave() { dragCounter = Math.max(0, dragCounter - 1); if (!dragCounter) dragOver.value = false; }

    // ===== 取消：直传 abort XHR；分片置 canceled，编排器检测后 abort 清理服务端分片 =====
    function cancelUpload(item) {
      if (item.status !== 'uploading') return;
      item.status = 'canceled';
      if (item.ctrl && item.ctrl.abort) item.ctrl.abort();
    }

    const statusText = { uploading: '上传中', done: '完成', error: '失败', canceled: '已取消' };

    // 直传阈值文案：从 DIRECT_MAX 派生，调常量时提示文案自动同步
    const directMaxKb = `${Math.round(DIRECT_MAX / 1024)}KB`;

    function clearFinished() {
      store.uploads = store.uploads.filter((u) => u.status === 'uploading');
    }

    onMounted(() => { loadBuckets(false); });
    // UploadPanel 自身清理义务：组件卸载时撤回本组件族暴露的刷新钩子（不动 FileList 的挂载）
    onUnmounted(() => { if (store.refreshFiles) store.refreshFiles = null; });

    return {
      store, bucketId, bucketIdOf, hasBuckets, dragOver, fileInput, queueActive,
      addFiles, onPick, onDrop, onDragEnter, onDragLeave, cancelUpload, clearFinished,
      formatSize, fileIcon, statusText, directMaxKb,
    };
  },
  template: `
    <div class="panel">
      <div class="panel-header"><h2>上传</h2></div>

      <!-- 目标桶（必选） -->
      <div class="p8-up-toolbar">
        <label>目标桶：</label>
        <select v-model="bucketId">
          <option value="" disabled>请选择目标桶（必选）</option>
          <option v-for="b in store.buckets" :key="bucketIdOf(b)" :value="bucketIdOf(b)">
            {{ b.name }}（已用 {{ formatSize(Number(b.usedSize)) }} / {{ formatSize(Number(b.sizeLimit)) }}）
          </option>
        </select>
      </div>

      <!-- 拖拽 + 多选 -->
      <div class="upload-area p8-up-area" :class="{ 'dragover': dragOver }"
        @click="fileInput && fileInput.click()"
        @dragenter.prevent="onDragEnter" @dragover.prevent
        @dragleave.prevent="onDragLeave" @drop.prevent="onDrop">
        <div class="upload-icon">📤</div>
        <p><strong>点击选择文件</strong> 或将文件拖拽到此处</p>
        <p class="p8-up-hint">支持多选 · ≤{{ directMaxKb }} 走直传（字节级进度），更大文件自动分片上传（3 并发，失败自动重试）</p>
        <input ref="fileInput" type="file" multiple style="display:none" @change="onPick">
      </div>

      <!-- 上传队列 -->
      <div class="p8-up-queue" v-if="store.uploads.length">
        <div class="p8-up-queue-title">
          上传队列（{{ store.uploads.length }} 项<template v-if="queueActive">，进行中…</template>）
          <button v-if="!queueActive" class="p8-up-cancel" style="margin-left:12px;color:#909399" @click="clearFinished">清空列表</button>
        </div>
        <div class="p8-up-row" v-for="item in store.uploads" :key="item.id">
          <span class="p8-up-icon">{{ fileIcon(item.suffix) }}</span>
          <div class="p8-up-info">
            <div class="p8-up-name" :title="item.name">{{ item.name }}</div>
            <div class="p8-up-size">{{ formatSize(item.size) }}<template v-if="item.error"> · {{ item.error }}</template></div>
          </div>
          <div class="p8-up-barwrap"><div class="p8-up-bar" :class="{error: item.status==='error'}" :style="{width: item.progress + '%'}"></div></div>
          <span class="p8-up-pct">{{ item.progress }}%</span>
          <span v-if="item.instant" class="p8-up-instant">秒传</span>
          <span class="p8-up-tag" :class="item.status">{{ statusText[item.status] }}</span>
          <button v-if="item.status==='uploading'" class="p8-up-cancel" @click="cancelUpload(item)">取消</button>
        </div>
      </div>
      <div class="empty-state" v-else>暂无上传任务，选择文件开始上传</div>
    </div>
  `,
};
